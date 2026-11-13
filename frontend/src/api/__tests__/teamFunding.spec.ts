import { beforeEach, describe, expect, it, vi } from 'vitest'
import { teamFundingAPI } from '../teamFunding'
const { get, put, post, patch, remove } = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), post: vi.fn(), patch: vi.fn(), remove: vi.fn() }))
vi.mock('../client', () => ({ default: { get, put, post, patch, delete: remove } }))

beforeEach(() => { vi.clearAllMocks(); for (const method of [get, put, post, patch, remove]) method.mockResolvedValue({ data: {} }) })
describe('team funding HTTP contract', () => {
  it('uses user-scoped endpoints and preserves explicit personal consent', async () => {
    const preferences = { mode: 'team_only' as const, team_id: 7, allow_personal_balance: false, allow_personal_subscription: false }
    await teamFundingAPI.getFunding(); await teamFundingAPI.saveFunding(preferences)
    expect(get).toHaveBeenCalledWith('/user/funding')
    expect(put).toHaveBeenCalledWith('/user/funding', preferences)
  })
  it('keeps unlimited distinct from zero and never sends personal consent in owner patches', async () => {
    await teamFundingAPI.updateMember(7, 9, { status: 'active', spend_limit: null, shared_mode: null, suggested_mode: null })
    await teamFundingAPI.updateMember(7, 9, { spend_limit: '0' })
    expect(patch).toHaveBeenNthCalledWith(1, '/user/teams/7/members/9', { status: 'active', spend_limit: null, shared_mode: null, suggested_mode: null })
    expect(patch).toHaveBeenNthCalledWith(2, '/user/teams/7/members/9', { spend_limit: '0' })
  })
  it('maps invitation, management and paged ledger actions', async () => {
    await teamFundingAPI.listTeams(); await teamFundingAPI.createTeam('Studio'); await teamFundingAPI.getTeam(7)
    await teamFundingAPI.updateTeam(7, { name: 'Studio', shared_mode: 'balance_only', shared_subscription_ids: [3], suggested_mode: 'team_first' })
    await teamFundingAPI.invite(7, 9); await teamFundingAPI.listInvitations()
    await teamFundingAPI.acceptInvitation(12); await teamFundingAPI.declineInvitation(13)
    await teamFundingAPI.revokeInvitation(7, 14); await teamFundingAPI.removeMember(7, 9)
    await teamFundingAPI.getLedger(7, 2, 20)
    expect(post).toHaveBeenCalledWith('/user/teams', { name: 'Studio' })
    expect(post).toHaveBeenCalledWith('/user/teams/7/invitations', { user_id: 9 })
    expect(post).toHaveBeenCalledWith('/user/team-invitations/12/accept')
    expect(post).toHaveBeenCalledWith('/user/team-invitations/13/decline')
    expect(remove).toHaveBeenCalledWith('/user/teams/7/invitations/14')
    expect(remove).toHaveBeenCalledWith('/user/teams/7/members/9')
    expect(get).toHaveBeenCalledWith('/user/teams/7/ledger', { params: { page: 2, page_size: 20 } })
  })
  it('deletes only the selected team through the owner management endpoint', async () => {
    await teamFundingAPI.deleteTeam(7)
    expect(remove).toHaveBeenCalledWith('/user/teams/7')
  })

})
