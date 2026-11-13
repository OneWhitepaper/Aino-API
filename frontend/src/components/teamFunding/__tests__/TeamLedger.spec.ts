import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import TeamLedger from '../TeamLedger.vue'
import messages from '@/i18n/locales/en/teamFunding'
import zhMessages from '@/i18n/locales/zh/teamFunding'
const { getLedger } = vi.hoisted(() => ({ getLedger: vi.fn() }))
vi.mock('@/api/teamFunding', async original => ({ ...await original<typeof import('@/api/teamFunding')>(), teamFundingAPI: { getLedger } }))
const entry = { id: 1, request_id: 'req-123', user_id: 9, payer_user_id: 1, source: 'team_subscription', subscription_id: 5, amount: '0.00000001', created_at: '2026-11-01T00:00:00Z' }
const page = { items: [entry], total: 21, page: 1, page_size: 20 }
function render(owner = true, locale = 'en') { return mount(TeamLedger, { props: { teamId: 7, owner }, global: { plugins: [createI18n({ legacy: false, locale, missingWarn: false, fallbackWarn: false, messages: { en: { teamFunding: messages, workspace: { retry: 'Retry' }, pagination: { previous: 'Previous', next: 'Next' } }, zh: { teamFunding: zhMessages } } })] } }) }
beforeEach(() => { vi.clearAllMocks(); getLedger.mockResolvedValue(page) })
describe('team ledger', () => {
  it('requests the selected team and paginates server-side, preserving precise amounts and identities', async () => {
    const wrapper = render(); await flushPromises()
    expect(getLedger).toHaveBeenCalledWith(7, 1, 20)
    expect(wrapper.text()).toContain('#9'); expect(wrapper.text()).toContain('#1'); expect(wrapper.text()).toContain('Owner subscription #5'); expect(wrapper.text()).toContain('0.00000001 credits'); expect(wrapper.text()).toContain('req-123')
    getLedger.mockResolvedValue({ ...page, page: 2, items: [{ ...entry, id: 2 }] })
    await wrapper.findAll('button').find(button => button.text() === 'Next')!.trigger('click'); await flushPromises()
    expect(getLedger).toHaveBeenLastCalledWith(7, 2, 20)
    expect(wrapper.findAll('button').find(button => button.text() === 'Next')!.attributes('disabled')).toBeDefined()
    await wrapper.setProps({ teamId: 8 }); await flushPromises(); expect(getLedger).toHaveBeenLastCalledWith(8, 1, 20)
    wrapper.unmount()
  })
  it('separates a precise pending review amount from the actual team charge', async () => {
    getLedger.mockResolvedValue({ ...page, items: [{ ...entry, amount: '2.00000000', unauthorized_amount: '0.00000001' }] })
    const wrapper = render(); await flushPromises()
    expect(wrapper.find('thead').text()).toContain('Team charge')
    expect(wrapper.get('[data-testid="team-charge"]').text()).toBe('2 credits')
    expect(wrapper.get('[data-testid="pending-review-amount"]').text()).toBe('Amount pending review: 0.00000001 credits. Not included in the team charge.')
    wrapper.unmount()
  })
  it('labels the uncharged amount as pending review in Chinese', async () => {
    getLedger.mockResolvedValue({ ...page, items: [{ ...entry, amount: '10.00000000', unauthorized_amount: '5.00000000' }] })
    const wrapper = render(false, 'zh'); await flushPromises()
    expect(wrapper.find('thead').text()).toContain('团队实际扣费')
    expect(wrapper.get('[data-testid="team-charge"]').text()).toBe('10 额度')
    expect(wrapper.get('[data-testid="pending-review-amount"]').text()).toBe('待核对金额：5 额度。未计入团队实际扣费。')
    wrapper.unmount()
  })
  it.each([undefined, '0', '0.00000000'])('does not show a pending review note for %s', async unauthorizedAmount => {
    getLedger.mockResolvedValue({ ...page, items: [{ ...entry, unauthorized_amount: unauthorizedAmount }] })
    const wrapper = render(false); await flushPromises()
    expect(wrapper.find('[data-testid="pending-review-amount"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="team-charge"]').text()).toBe('0.00000001 credits')
    wrapper.unmount()
  })
  it('shows member privacy scope, loading, errors and a recoverable empty state', async () => {
    let reject!: (reason: Error) => void
    getLedger.mockImplementationOnce(() => new Promise((_resolve, rejectFn) => { reject = rejectFn }))
    const wrapper = render(false)
    expect(wrapper.text()).toContain('Only your shared charges'); expect(wrapper.find('[role="status"]').exists()).toBe(true)
    reject(new Error('offline')); await flushPromises(); expect(wrapper.get('[role="alert"]').text()).toContain('Ledger could not be loaded')
    getLedger.mockResolvedValue({ ...page, items: [], total: 0 }); await wrapper.get('button').trigger('click'); await flushPromises(); expect(wrapper.text()).toContain('No shared charges yet')
    wrapper.unmount()
  })
  it('ignores a stale response after the selected team changes', async () => {
    let resolveOld!: (value: unknown) => void
    getLedger.mockImplementationOnce(() => new Promise(resolve => { resolveOld = resolve }))
    const wrapper = render(); await wrapper.setProps({ teamId: 8 }); await flushPromises()
    resolveOld({ ...page, items: [{ ...entry, request_id: 'private-old-team' }] }); await flushPromises()
    expect(wrapper.text()).not.toContain('private-old-team'); expect(wrapper.text()).toContain('req-123')
    wrapper.unmount()
  })
})
