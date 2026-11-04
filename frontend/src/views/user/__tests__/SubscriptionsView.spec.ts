import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import SubscriptionsView from '../SubscriptionsView.vue'

const { getSubscriptions, settings, push } = vi.hoisted(() => ({
  getSubscriptions: vi.fn(),
  settings: { payment_enabled: false, subscription_enabled: true },
  push: vi.fn(),
}))
vi.mock('@/api/subscriptions', () => ({ default: { getMySubscriptions: getSubscriptions } }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ cachedPublicSettings: settings, showError: vi.fn() }) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

const mountView = () => mount(SubscriptionsView, { global: { stubs: {
  AppLayout: { template: '<div><slot /></div>' }, Icon: true,
} } })
const plans = [
  { id: 1, group_id: 1, status: 'active', expires_at: null, group: { name: 'Current plan', platform: 'openai' } },
  { id: 2, group_id: 2, status: 'expired', expires_at: '2020-01-01T00:00:00Z', group: { name: 'Past plan', platform: 'openai' } },
]
beforeEach(() => {
  getSubscriptions.mockReset().mockResolvedValue(plans)
  push.mockReset()
  settings.payment_enabled = false
})

describe('subscription workspace', () => {
  it('switches between active, historical and all plans without refetching', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('Current plan')
    expect(wrapper.text()).not.toContain('Past plan')
    await wrapper.findAll('nav button')[2].trigger('click')
    expect(wrapper.text()).toContain('Past plan')
    expect(wrapper.text()).not.toContain('Current plan')
    await wrapper.findAll('nav button')[1].trigger('click')
    expect(wrapper.findAll('.user-subscription-card')).toHaveLength(2)
    expect(getSubscriptions).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('hides purchase and renewal actions when payments are disabled', async () => {
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).not.toContain('userPages.browsePlans')
    expect(wrapper.text()).not.toContain('payment.renewNow')
    wrapper.unmount()
  })

  it('routes renewal to the matching group when payment is enabled', async () => {
    settings.payment_enabled = true
    const wrapper = mountView()
    await flushPromises()
    await wrapper.findAll('button').find(button => button.text() === 'payment.renewNow')!.trigger('click')
    expect(push).toHaveBeenCalledWith({ path: '/purchase', query: { tab: 'subscription', group: '1' } })
    wrapper.unmount()
  })

  it('shows a retryable error instead of an empty subscription state', async () => {
    getSubscriptions.mockRejectedValueOnce(new Error('offline'))
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('userPages.subscriptionsFailed')
    expect(wrapper.text()).not.toContain('userSubscriptions.noActiveSubscriptions')
    await wrapper.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Current plan')
    wrapper.unmount()
  })
})
