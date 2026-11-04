import { beforeEach, describe, expect, it, vi } from 'vitest'
import { computed } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import DashboardView from '../DashboardView.vue'

const mocks = vi.hoisted(() => ({
  stats: vi.fn(), trend: vi.fn(), models: vi.fn(), recent: vi.fn(), quotas: vi.fn(), refresh: vi.fn(), copy: vi.fn(),
  auth: { isSimpleMode: false, user: { balance: 12.5, frozen_balance: 2 } },
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ ...mocks.auth, refreshUser: mocks.refresh }) }))
vi.mock('@/api/usage', () => ({ usageAPI: { getDashboardStats: mocks.stats, getDashboardTrend: mocks.trend, getDashboardModels: mocks.models, getByDateRange: mocks.recent } }))
vi.mock('@/api/user', () => ({ getMyPlatformQuotas: mocks.quotas }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copied: false, copyToClipboard: mocks.copy }) }))
vi.mock('@/composables/useWorkspaceLinks', () => ({ useWorkspaceLinks: () => ({
  catalog: computed(() => null), endpoint: computed(() => 'https://api.example.com'), billing: computed(() => null),
}) }))
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

const render = () => mount(DashboardView, { global: { stubs: {
  AppLayout: { template: '<main><slot /></main>' },
  RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
  Icon: true, BaseDialog: true, ConnectionGuide: true, WorkspaceModels: true,
  UserDashboardStats: true, UserDashboardCharts: true, UserDashboardRecentUsage: true,
} } })

beforeEach(() => {
  vi.clearAllMocks()
  mocks.auth.isSimpleMode = false
  mocks.stats.mockResolvedValue({ active_api_keys: 3, today_requests: 17, today_actual_cost: 0.125 })
  mocks.refresh.mockResolvedValue(undefined)
  mocks.trend.mockResolvedValue({ trend: [] })
  mocks.models.mockResolvedValue({ models: [] })
  mocks.recent.mockResolvedValue({ items: [] })
  mocks.quotas.mockResolvedValue({ platform_quotas: [] })
})

describe('Aino workspace', () => {
  it('renders real balances and summaries, loading detailed charts only when opened', async () => {
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('.workspace-account').text()).toContain('$12.50')
    expect(wrapper.get('.workspace-account').text()).toContain('$2.00')
    expect(wrapper.get('.workspace-metrics').text()).toContain('$0.1250')
    expect(mocks.trend).not.toHaveBeenCalled()
    await wrapper.get('[aria-controls="workspace-analytics-content"]').trigger('click')
    await flushPromises()
    expect(mocks.trend).toHaveBeenCalledTimes(1)
    expect(mocks.quotas).toHaveBeenCalledTimes(1)
    expect(wrapper.find('#workspace-analytics-content').exists()).toBe(true)
    await wrapper.get('[aria-controls="workspace-analytics-content"]').trigger('click')
    await wrapper.get('[aria-controls="workspace-analytics-content"]').trigger('click')
    expect(mocks.trend).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('keeps connection actions available when statistics fail, with a retry instead of fabricated zeroes', async () => {
    mocks.stats.mockRejectedValueOnce(new Error('unavailable'))
    const wrapper = render()
    await flushPromises()
    expect(wrapper.get('[role="status"]').text()).toContain('workspace.loadError')
    expect(wrapper.get('.workspace-metrics').text()).not.toContain('$0.0000')
    expect(wrapper.find('a[href="/keys?create=1"]').exists()).toBe(true)
    await wrapper.get('.workspace-error button').trigger('click')
    await flushPromises()
    expect(wrapper.find('.workspace-error').exists()).toBe(false)
    await wrapper.get('.workspace-endpoint button').trigger('click')
    expect(mocks.copy).toHaveBeenCalledWith('https://api.example.com')
    wrapper.unmount()
  })

  it('does not expose billing links or fetch recent usage in simple mode', async () => {
    mocks.auth.isSimpleMode = true
    const wrapper = render()
    await flushPromises()
    expect(wrapper.find('a[href="/usage"]').exists()).toBe(false)
    expect(wrapper.get('.workspace-account').text()).not.toContain('$12.50')
    expect(mocks.recent).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
