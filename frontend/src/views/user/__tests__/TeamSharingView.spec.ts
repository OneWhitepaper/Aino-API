import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import TeamSharingView from '../TeamSharingView.vue'
import messages from '@/i18n/locales/en/teamFunding'
const { api } = vi.hoisted(() => ({ api: Object.fromEntries(['getFunding','listTeams','listInvitations','getTeam','getLedger','createTeam','updateTeam','invite','acceptInvitation','declineInvitation','revokeInvitation','updateMember','removeMember'].map(key => [key, vi.fn()])) }))
vi.mock('@/api/teamFunding', async original => ({ ...await original<typeof import('@/api/teamFunding')>(), teamFundingAPI: api }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 9 } }) }))
const invitation = { id: 12, team_id: 7, team_name: 'Studio', owner_user_id: 1, user_id: 9, status: 'pending', expires_at: '2099-01-01T00:00:00Z', created_at: '2026-11-01T00:00:00Z' }
const summary = { id: 7, owner_user_id: 1, name: 'Studio', role: 'owner', status: 'active', shared_mode: 'balance_first', suggested_mode: null, available_balance: '500.00000000', spent: '20.00000000', spend_limit: null, remaining_limit: null }
const member = { user_id: 9, username: 'Member', status: 'active', spend_limit: null, spent: '20.00000000', reserved: '1.00000000', shared_mode: null, suggested_mode: null }
const detail = () => ({ ...summary, shared_subscription_ids: [], subscriptions: [], members: [{ ...member }], invitations: [] })
function render() { return mount(TeamSharingView, { global: { plugins: [createI18n({ legacy: false, locale: 'en', missingWarn: false, fallbackWarn: false, messages: { en: { teamFunding: messages, workspace: { retry: 'Retry' } } } })], stubs: { AppLayout: { template: '<main><slot /></main>' }, RouterLink: { template: '<a><slot /></a>' }, ConfirmDialog: { props: ['show', 'message'], emits: ['confirm', 'cancel'], template: '<div v-if="show" data-testid="confirm"><p>{{ message }}</p><button @click="$emit(\'confirm\')">Confirm removal</button></div>' } } } }) }
function button(wrapper: ReturnType<typeof render>, text: string) { return wrapper.findAll('button').find(item => item.text() === text)! }
beforeEach(() => {
  vi.clearAllMocks(); api.listTeams.mockResolvedValue({ owned: [summary], joined: [] }); api.listInvitations.mockResolvedValue({ items: [] }); api.getFunding.mockResolvedValue({ personal_subscriptions: [{ id: 5, group_id: 2, group_name: 'Owner plan', expires_at: null }] }); api.getTeam.mockResolvedValue(detail()); api.getLedger.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
  for (const key of ['createTeam','updateTeam','invite','acceptInvitation','declineInvitation','revokeInvitation','updateMember','removeMember']) api[key].mockResolvedValue({})
})
describe('team sharing management', () => {
  it('creates teams and sends existing account invitations without accepting for the target', async () => {
    const wrapper = render(); await flushPromises()
    await button(wrapper, 'Create team').trigger('click'); await wrapper.get('#new-team-name').setValue(' New studio ')
    api.createTeam.mockResolvedValue(detail()); await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(api.createTeam).toHaveBeenCalledWith('New studio')
    await wrapper.get('#invite-user-id').setValue('42'); await wrapper.get('#invite-user-id').element.closest('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true })); await flushPromises()
    expect(api.invite).toHaveBeenCalledWith(7, 42); expect(api.acceptInvitation).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('saves selected owner subscriptions, shared strategy and suggested mode without personal consent flags', async () => {
    const wrapper = render(); await flushPromises()
    await wrapper.get('#team-shared-mode').setValue('subscription_only'); await wrapper.get('#team-suggested-mode').setValue('team_first'); await wrapper.get('#shared-subscription-5').setValue(true)
    await button(wrapper, 'Save team settings').trigger('submit'); await flushPromises()
    expect(api.updateTeam).toHaveBeenCalledWith(7, { name: 'Studio', shared_mode: 'subscription_only', shared_subscription_ids: [5], suggested_mode: 'team_first' })
    expect(wrapper.find('#allow-personal-balance').exists()).toBe(false)
    wrapper.unmount()
  })
  it('clears a team suggestion explicitly and renders the empty DTO default', async () => {
    api.getTeam.mockResolvedValue({ ...detail(), suggested_mode: 'team_first' })
    const wrapper = render(); await flushPromises()
    await wrapper.get('#team-suggested-mode').setValue('')
    await button(wrapper, 'Save team settings').trigger('submit'); await flushPromises()
    expect(api.updateTeam).toHaveBeenCalledWith(7, expect.objectContaining({ suggested_mode: '' }))
    wrapper.unmount()
  })
  it('preserves unlimited null versus zero, validates amounts, and pauses members', async () => {
    const wrapper = render(); await flushPromises()
    await button(wrapper, 'Save member settings').trigger('submit'); await flushPromises()
    expect(api.updateMember).toHaveBeenLastCalledWith(7, 9, { spend_limit: null, shared_mode: null, suggested_mode: null })
    await wrapper.get('#unlimited-9').setValue(false); await wrapper.get('#limit-9').setValue('0'); await button(wrapper, 'Save member settings').trigger('submit'); await flushPromises()
    expect(api.updateMember).toHaveBeenLastCalledWith(7, 9, { spend_limit: '0', shared_mode: null, suggested_mode: null })
    await wrapper.get('#unlimited-9').setValue(false); await wrapper.get('#limit-9').setValue('1e2'); const calls = api.updateMember.mock.calls.length
    await button(wrapper, 'Save member settings').trigger('submit'); await flushPromises()
    expect(api.updateMember).toHaveBeenCalledTimes(calls); expect(wrapper.text()).toContain('Enter a nonnegative amount')
    await button(wrapper, 'Pause').trigger('click'); await flushPromises(); expect(api.updateMember).toHaveBeenLastCalledWith(7, 9, { status: 'paused' })
    wrapper.unmount()
  })
  it('requires confirmation before removal and explains preserved history', async () => {
    const wrapper = render(); await flushPromises()
    await button(wrapper, 'Remove').trigger('click'); expect(api.removeMember).not.toHaveBeenCalled()
    expect(wrapper.get('[data-testid="confirm"]').text()).toContain('Ledger history and cumulative spend are preserved')
    await button(wrapper, 'Confirm removal').trigger('click'); await flushPromises(); expect(api.removeMember).toHaveBeenCalledWith(7, 9)
    wrapper.unmount()
  })
  it('allows invitees to accept/decline and hides owner controls in member views', async () => {
    api.listTeams.mockResolvedValue({ owned: [], joined: [{ ...summary, role: 'member' }] }); api.getTeam.mockResolvedValue({ ...detail(), role: 'member', members: [], invitations: [] }); api.listInvitations.mockResolvedValue({ items: [invitation, { ...invitation, id: 13 }] })
    const wrapper = render(); await flushPromises()
    expect(wrapper.find('#team-name').exists()).toBe(false); expect(wrapper.find('#invite-user-id').exists()).toBe(false); expect(wrapper.find('[data-member]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Only your shared charges'); expect(api.getLedger).toHaveBeenCalledWith(7, 1, 20)
    await wrapper.get('[data-accept="12"]').trigger('click'); await flushPromises(); expect(api.acceptInvitation).toHaveBeenCalledWith(12)
    await wrapper.get('[data-decline="13"]').trigger('click'); await flushPromises(); expect(api.declineInvitation).toHaveBeenCalledWith(13)
    wrapper.unmount()
  })
  it('renders empty/error/loading states and recovers without exposing controls', async () => {
    api.listTeams.mockRejectedValueOnce(new Error('offline'))
    const wrapper = render(); expect(wrapper.find('[role="status"]').exists()).toBe(true); await flushPromises()
    expect(wrapper.find('#team-name').exists()).toBe(false); expect(wrapper.get('[role="alert"]').text()).toContain('Could not load')
    api.listTeams.mockResolvedValue({ owned: [], joined: [] }); await button(wrapper, 'Retry').trigger('click'); await flushPromises(); expect(wrapper.text()).toContain('No teams yet')
    wrapper.unmount()
  })
})
