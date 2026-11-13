import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import FundingPreferencesView from '../FundingPreferencesView.vue'
import messages from '@/i18n/locales/en/teamFunding'
const { getFunding, saveFunding } = vi.hoisted(() => ({ getFunding: vi.fn(), saveFunding: vi.fn() }))
vi.mock('@/api/teamFunding', async importOriginal => ({ ...await importOriginal<typeof import('@/api/teamFunding')>(), teamFundingAPI: { getFunding, saveFunding } }))
const team = { id: 7, owner_user_id: 1, name: 'Studio', role: 'member', status: 'active', shared_mode: 'balance_first', suggested_mode: 'team_first', available_balance: '500.00000000', spent: '20.00000000', spend_limit: '100.00000000', remaining_limit: '80.00000000' }
const funding = () => ({ configured: false, mode: 'personal_first', team_id: 7, allow_personal_balance: true, allow_personal_subscription: true, personal_balance: '3.00000000', personal_subscriptions: [{ id: 5, group_id: 2, group_name: 'Personal plan', expires_at: null }], teams: [team] })
function render() { return mount(FundingPreferencesView, { global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: { teamFunding: messages } }, missingWarn: false, fallbackWarn: false })], stubs: { AppLayout: { template: '<main><slot /></main>' }, RouterLink: { template: '<a><slot /></a>' } } } }) }
beforeEach(() => { vi.clearAllMocks(); getFunding.mockResolvedValue(funding()); saveFunding.mockResolvedValue({}) })
describe('funding preferences', () => {
  it('shows six modes, legacy status and exact fallback order while keeping wallets separate', async () => {
    const wrapper = render(); await flushPromises()
    expect(wrapper.findAll('#funding-mode option')).toHaveLength(6)
    expect(wrapper.get('[data-testid="configuration-state"]').text()).toContain('Not configured')
    expect(wrapper.get('[data-testid="funding-order"]').text()).toBe('1.Personal balance2.Personal subscription3.Current team (using its shared mode)')
    expect(wrapper.get('[data-testid="personal-balance"]').text()).toBe('3 credits')
    expect(wrapper.get('[data-testid="team-balance"]').text()).toContain('500 credits')
    expect(wrapper.get('[data-testid="personal-balance"]').text()).not.toContain('503')
    expect(wrapper.text()).toContain('Owner suggestion: Team first')
    wrapper.unmount()
  })
  it('does not let a suggestion authorize personal funds and saves the exact member choices', async () => {
    const data = funding(); data.mode = 'team_only'; data.allow_personal_balance = false; data.allow_personal_subscription = false
    getFunding.mockResolvedValue(data)
    const wrapper = render(); await flushPromises()
    expect((wrapper.get('#funding-mode').element as HTMLSelectElement).value).toBe('team_only')
    expect((wrapper.get('#allow-personal-balance').element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.get('[data-testid="funding-order"]').text()).toBe('1.Current team (using its shared mode)')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(saveFunding).toHaveBeenCalledWith({ mode: 'team_only', team_id: 7, allow_personal_balance: false, allow_personal_subscription: false })
    expect(wrapper.get('[data-testid="configuration-state"]').text()).toContain('configured')
    wrapper.unmount()
  })
  it('updates visible order for each mode and excludes disabled sources', async () => {
    const wrapper = render(); await flushPromises()
    const expected = { personal_first: ['Personal balance', 'Personal subscription', 'Current team'], personal_only: ['Personal balance'], team_first: ['Current team', 'Personal subscription', 'Personal balance'], team_only: ['Current team'], subscription_first: ['Personal subscription', 'Personal balance', 'Current team'], subscription_only: ['Personal subscription'] }
    for (const [mode, order] of Object.entries(expected)) {
      await wrapper.get('#funding-mode').setValue(mode)
      expect(wrapper.findAll('[data-testid="funding-order"] li').map(li => li.text().replace(/^\d\./, '').split(' (')[0])).toEqual(order)
    }
    await wrapper.get('#allow-personal-subscription').setValue(false)
    expect(wrapper.text()).toContain('No payment sources')
    wrapper.unmount()
  })
  it('handles loading, empty teams/subscriptions and recoverable load/save failures', async () => {
    let reject!: (reason: Error) => void
    getFunding.mockImplementationOnce(() => new Promise((_resolve, rejectFn) => { reject = rejectFn }))
    const wrapper = render()
    expect(wrapper.find('[role="status"]').exists()).toBe(true)
    reject(new Error('offline')); await flushPromises(); expect(wrapper.get('[role="alert"]').text()).toContain('Could not load')
    getFunding.mockResolvedValue({ ...funding(), teams: [], team_id: null, personal_subscriptions: [] })
    await wrapper.get('button').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('No available teams'); expect(wrapper.text()).toContain('No active subscriptions')
    saveFunding.mockRejectedValue(new Error('offline')); await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('Could not save')
    wrapper.unmount()
  })
})
