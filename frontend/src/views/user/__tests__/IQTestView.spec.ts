import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import IQTestView from '../IQTestView.vue'
import messages from '@/i18n/locales/zh/iqTest'

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
vi.mock('vue-i18n', async (original) => ({
  ...(await original<typeof import('vue-i18n')>()),
  useI18n: () => ({
    locale: { value: 'zh' },
    t: (key: string, params: Record<string, string | number> = {}) => {
      const value = messages.iqTest[key.replace('iqTest.', '') as keyof typeof messages.iqTest] ?? key
      return value.replace(/\{(\w+)\}/g, (_, name: string) => String(params[name] ?? ''))
    }
  })
}))
enableAutoUnmount(afterEach)
const completed = {
  id: 'private-test',
  config: {
    kind: 'pelican',
    source: 'site',
    key_id: 1,
    model: 'gpt-6.1-sol',
    effort: 'high',
    stream: true,
    service_tier: ''
  },
  status: 'completed',
  verdict: 'review',
  basis: 'pelican_manual_review',
  error_code: '',
  result: {
    output: '<html><body><svg></svg><script>alert(1)</script></body></html>',
    usage: null,
    duration_ms: 2000
  },
  reference_cost_usd: null,
  created_at: '2026-10-27T09:00:00Z',
  deadline_at: '2026-10-27T09:10:00Z',
  finished_at: '2026-10-27T09:00:02Z'
}
function mountView() {
  return mount(IQTestView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        Icon: true,
        RouterLink: { template: '<a><slot /></a>' }
      }
    }
  })
}

describe('IQ test workspace', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.listIQTests.mockResolvedValue({ items: [], has_more: false })
    api.getIQTestKeys.mockResolvedValue([{ id: 1, name: '日常使用', masked: 'sk-••••1234' }])
    api.discoverIQModels.mockResolvedValue([{ id: 'gpt-6.1-sol', efforts: ['low', 'high'] }])
    api.getIQTestOptions.mockResolvedValue({ prompts: { candy: '糖果题目', pelican: '鹈鹕题目' } })
    api.getIQTest.mockResolvedValue(completed)
  })

  it('loads owned keys and supported model efforts, and starts the selected real test', async () => {
    api.startIQTest.mockResolvedValue(completed)
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('模型表现，现场见。')
    expect(wrapper.text()).toContain('sk-••••1234')
    await wrapper.get('[data-testid="effort-high"]').trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(api.startIQTest).toHaveBeenCalledWith(
      expect.objectContaining({
        source: 'site',
        key_id: 1,
        model: 'gpt-6.1-sol',
        effort: 'high',
        kind: 'candy'
      }),
      expect.any(AbortSignal)
    )
    expect(wrapper.text()).toContain('需人工查看')
    expect(wrapper.text()).not.toContain('$0')
    const frame = wrapper.get('iframe')
    expect(frame.attributes('sandbox')).toBe('')
    expect(frame.attributes('srcdoc')).not.toContain('<script>')
  })

  it('switches to custom API, disables start without credentials and opens the actual prompt', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="source-custom"]').trigger('click')
    expect(wrapper.get('[data-testid="start-iq-test"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="custom-api-key"]').attributes('type')).toBe('password')
    await wrapper.get('[data-testid="view-prompt"]').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('糖果题目')
  })

  it('reruns a historical model that is absent from discovery without losing its model or effort', async () => {
    const archived = { ...completed, config: { ...completed.config, model: 'archived-model', kind: 'candy' } }
    api.listIQTests.mockResolvedValue({ items: [archived], has_more: false })
    api.getIQTest.mockResolvedValue(archived)
    api.startIQTest.mockResolvedValue(archived)
    const wrapper = mountView()
    await flushPromises()
    const button = wrapper.findAll('button').find((item) => item.text() === '再测一次')!
    await button.trigger('click')
    await flushPromises()
    expect(api.startIQTest).toHaveBeenCalledWith(
      expect.objectContaining({ model: 'archived-model', effort: 'high' }),
      expect.any(AbortSignal)
    )
    expect(wrapper.get('#iq-model').element.tagName).toBe('INPUT')
    expect((wrapper.get('#iq-model').element as HTMLInputElement).value).toBe('archived-model')
    await wrapper
      .findAll('button')
      .find((item) => item.text() === '选择模型')!
      .trigger('click')
    expect((wrapper.get('#iq-model').element as HTMLSelectElement).value).toBe('gpt-6.1-sol')
  })
})
