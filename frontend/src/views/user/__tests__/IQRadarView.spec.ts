import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import IQRadarView from '../IQRadarView.vue'
import messages from '@/i18n/locales/zh/iqRadar'
import type { RadarPoint, RadarSnapshot } from '@/api/iqRadar'

const { getIQRadar } = vi.hoisted(() => ({ getIQRadar: vi.fn() }))
vi.mock('@/api/iqRadar', () => ({ getIQRadar }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({
    locale: { value: 'zh' },
    t: (key: string, params: Record<string, string | number> = {}) => {
      const message = messages.iqRadar[key.replace('iqRadar.', '') as keyof typeof messages.iqRadar] ?? key
      return message.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? ''))
    },
  }),
}))
enableAutoUnmount(afterEach)

const point = (effort: string, score: number | null): RadarPoint => ({
  model: 'gpt-6-astra', effort, score, coverage: 64, required_tasks: 64, score_status: 'complete',
  average_price_usd: null, average_minutes: 2, price_samples: 0, duration_samples: 3,
  valid_grade_events: 3, latest_graded_at: '2026-10-22T09:00:00Z', fetched_at: '2026-10-27T09:00:00Z',
  stale: false, history: [{ date: '2026-10-20', score: score ?? 20 }],
})
const snapshot = (): RadarSnapshot => ({
  source_name: 'Codex Radar', source_url: 'https://codexradar.com/', benchmark: 'bench',
  score_version: 'v150', task_set_sha256: 'hash', score_max: 150, required_tasks: 64,
  fetched_at: '2026-10-27T09:00:00Z', refreshing: false, stale: false, partial_failures: 0,
  cache_ttl_seconds: 1800, retry_after_seconds: 1800,
  points: [point('high', 114.3), point('low', 0)],
  community: { window_hours: 24, updated_at: null, fetched_at: null, stale: false, models: [
    { id: 'gpt-6.1-terra-low', label: 'GPT-6.1 Terra low', group: 'GPT-6.1 Terra', average: null, count: 0 },
  ] },
})
async function mountView(query = '') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/iq-radar', component: IQRadarView }] })
  await router.push('/iq-radar' + query)
  await router.isReady()
  const wrapper = mount(IQRadarView, { global: {
    plugins: [router],
    stubs: { AppLayout: { template: '<main><slot /></main>' }, Icon: true, RadarTrendChart: { name: 'RadarTrendChart', props: ['history', 'scoreMax'], template: '<div data-testid="chart" />' } },
  } })
  await flushPromises()
  return { wrapper, router }
}

describe('IQRadarView', () => {
  beforeEach(() => { vi.clearAllMocks(); getIQRadar.mockResolvedValue(snapshot()) })
  afterEach(() => vi.useRealTimers())

  it('selects a tier, updates the shared URL and connects its details to the chart', async () => {
    const { wrapper, router } = await mountView()
    expect(wrapper.get('[data-testid="selected-score"]').text()).toContain('114.3')
    await wrapper.findAll('[data-testid="radar-gpt-6-astra-low"]')[0].trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toMatchObject({ model: 'gpt-6-astra', effort: 'low' })
    expect(wrapper.get('[data-testid="selected-score"]').text()).toContain('0.0')
    expect(wrapper.findComponent({ name: 'RadarTrendChart' }).props('history')).toEqual([{ date: '2026-10-20', score: 0 }])
  })

  it('honors a shared tier, preserves unknown costs, and keeps unrated community families', async () => {
    const { wrapper } = await mountView('?model=gpt-6-astra&effort=low')
    expect(wrapper.get('[data-testid="selected-cost"]').text()).toBe('—')
    expect(wrapper.text()).not.toContain('$0')
    expect(wrapper.text()).toContain('GPT-6.1 Terra')
    expect(wrapper.text()).toContain('暂无评价')
    expect(wrapper.text()).toContain('2026-10-20')
  })

  it('recovers from an initial failure and retains results when a later request fails', async () => {
    getIQRadar.mockRejectedValueOnce(new Error('offline'))
    const { wrapper } = await mountView()
    expect(wrapper.get('[role="alert"]').text()).toContain('暂时无法读取')
    await wrapper.get('[data-testid="refresh-radar"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="selected-score"]').text()).toContain('114.3')
    getIQRadar.mockRejectedValueOnce(new Error('offline'))
    await wrapper.get('[data-testid="refresh-radar"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('暂时无法读取')
    expect(wrapper.get('[data-testid="selected-score"]').text()).toContain('114.3')
  })

  it('polls an initial background sync and stops polling when unmounted', async () => {
    vi.useFakeTimers()
    getIQRadar.mockResolvedValueOnce({ ...snapshot(), points: [], refreshing: true })
    const { wrapper } = await mountView()
    expect(wrapper.text()).toContain('尚未取得评测数据')
    await vi.advanceTimersByTimeAsync(5000)
    await flushPromises()
    expect(wrapper.get('[data-testid="selected-score"]').text()).toContain('114.3')
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(120000)
    expect(getIQRadar).toHaveBeenCalledTimes(2)
  })

  it('clearly labels stale benchmark and community data', async () => {
    const data = snapshot(); data.stale = true; data.partial_failures = 2; data.points[0].stale = true; data.community.stale = true
    getIQRadar.mockResolvedValue(data)
    const { wrapper } = await mountView()
    expect(wrapper.text()).toContain('部分数据暂未更新')
    expect(wrapper.text()).toContain('上次成功数据')
  })

  it('does not claim a successful result or zero samples when a tier has never loaded', async () => {
    const data = snapshot()
    data.points = [{ ...point('high', null), coverage: 0, valid_grade_events: 0, score_status: 'unavailable', fetched_at: null, latest_graded_at: null, stale: true, history: [] }]
    data.partial_failures = 1
    getIQRadar.mockResolvedValue(data)
    const { wrapper } = await mountView()
    expect(wrapper.text()).toContain('数据暂不可用')
    expect(wrapper.text()).not.toContain('上次成功数据')
    expect(wrapper.text()).not.toContain('已覆盖 0')
    expect(wrapper.text()).not.toContain('0 次有效评分')
    expect(wrapper.find('[data-testid="selected-cost"]').exists()).toBe(false)
  })

  it('shows routine cache refresh as syncing rather than as an upstream failure', async () => {
    getIQRadar.mockResolvedValue({ ...snapshot(), refreshing: true, stale: true, partial_failures: 0 })
    const { wrapper } = await mountView()
    expect(wrapper.text()).toContain('正在同步源站数据')
    expect(wrapper.text()).not.toContain('部分数据暂未更新')
  })
})
