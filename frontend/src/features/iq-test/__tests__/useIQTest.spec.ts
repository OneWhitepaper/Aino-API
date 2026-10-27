import { defineComponent } from 'vue'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useIQTest } from '../useIQTest'
import type { IQTest, IQTestInput } from '@/api/iqTest'

const api = vi.hoisted(() => ({
  listIQTests: vi.fn(),
  getIQTest: vi.fn(),
  startIQTest: vi.fn(),
  cancelIQTest: vi.fn(),
  getIQTestKeys: vi.fn(),
  discoverIQModels: vi.fn(),
  getIQTestOptions: vi.fn()
}))
vi.mock('@/api/iqTest', () => api)
enableAutoUnmount(afterEach)

const task = (status = 'running', id = 'task-1'): IQTest => ({
  id,
  status,
  config: {
    kind: 'candy',
    source: 'site',
    key_id: 1,
    model: 'test-model',
    effort: '',
    service_tier: '',
    stream: true
  },
  verdict: '',
  basis: '',
  error_code: '',
  result: { output: '', usage: null, duration_ms: 0 },
  reference_cost_usd: null,
  created_at: '2026-10-27T00:00:00Z',
  deadline_at: '2026-10-27T00:10:00Z',
  finished_at: null
})
const input: IQTestInput = {
  kind: 'candy',
  source: 'site',
  key_id: 1,
  model: 'test-model',
  effort: '',
  service_tier: '',
  stream: true
}
function setup() {
  let lab!: ReturnType<typeof useIQTest>
  const wrapper = mount(
    defineComponent({
      setup() {
        lab = useIQTest()
        return () => null
      }
    })
  )
  return { lab, wrapper }
}

describe('private test lifecycle', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    api.listIQTests.mockResolvedValue({ items: [], has_more: false })
    api.getIQTestKeys.mockResolvedValue([])
    api.getIQTestOptions.mockResolvedValue({ prompts: { candy: 'question', pelican: 'draw' } })
    api.startIQTest.mockResolvedValue(task())
    api.getIQTest.mockResolvedValue(task('completed'))
  })
  afterEach(() => vi.useRealTimers())

  it('reattaches an active owned task and stops polling on unmount', async () => {
    api.listIQTests.mockResolvedValue({ items: [task()], has_more: false })
    api.getIQTest.mockResolvedValue(task())
    const { lab, wrapper } = setup()
    await flushPromises()
    expect(lab.current.value?.id).toBe('task-1')
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    const calls = api.getIQTest.mock.calls.length
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(10000)
    expect(api.getIQTest).toHaveBeenCalledTimes(calls)
  })

  it('reuses the request identifier after an ambiguous network error, then polls to completion', async () => {
    api.startIQTest.mockRejectedValueOnce(new Error('network'))
    const { lab } = setup()
    await flushPromises()
    await lab.start(input)
    expect(lab.error.value).toBe('start')
    await lab.start(input)
    expect(api.startIQTest.mock.calls[0][0].request_id).toBe(api.startIQTest.mock.calls[1][0].request_id)
    await vi.advanceTimersByTimeAsync(2000)
    await flushPromises()
    expect(lab.current.value?.status).toBe('completed')
    expect(lab.runningID.value).toBeNull()
  })

  it('persists cancellation and does not start a second active request', async () => {
    api.cancelIQTest.mockResolvedValue(task('cancelled'))
    const { lab } = setup()
    await flushPromises()
    await lab.start(input)
    await lab.start(input)
    expect(api.startIQTest).toHaveBeenCalledTimes(1)
    await lab.cancel()
    expect(lab.current.value?.status).toBe('cancelled')
    expect(lab.runningID.value).toBeNull()
  })

  it('ignores obsolete model discovery after switching a connection', async () => {
    let resolve!: (models: { id: string; efforts: string[] }[]) => void
    api.discoverIQModels.mockImplementationOnce(
      () =>
        new Promise((r) => {
          resolve = r
        })
    )
    const { lab } = setup()
    await flushPromises()
    const pending = lab.discover({ source: 'site', key_id: 1 })
    lab.clearModels()
    resolve([{ id: 'old-source', efforts: [] }])
    await pending
    expect(lab.models.value).toEqual([])
  })

  it('recovers the active server task when another page has already started a test', async () => {
    api.startIQTest.mockRejectedValueOnce({ reason: 'IQ_TEST_BUSY', status: 429 })
    const { lab } = setup()
    await flushPromises()
    api.listIQTests.mockResolvedValue({ items: [task()], has_more: false })
    api.getIQTest.mockResolvedValue(task())
    await lab.start(input)
    expect(lab.error.value).toBe('busy')
    expect(lab.runningID.value).toBe('task-1')
  })
})
