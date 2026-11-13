import apiClient from './client'

export const fundingModes = ['personal_first', 'personal_only', 'team_first', 'team_only', 'subscription_first', 'subscription_only'] as const
export type FundingMode = typeof fundingModes[number]
export const sharedModes = ['balance_first', 'subscription_first', 'balance_only', 'subscription_only'] as const
export type SharedMode = typeof sharedModes[number]
export interface FundingSubscription { id: number; group_id: number; group_name: string; expires_at: string | null }
export interface TeamSummary {
  id: number; owner_user_id: number; name: string; role: 'owner' | 'member'; status: string
  shared_mode: SharedMode; suggested_mode: FundingMode | ''; available_balance: string
  spent: string; spend_limit: string | null; remaining_limit: string | null
}
export interface TeamMember {
  user_id: number; username: string; status: 'active' | 'paused' | 'removed'
  spend_limit: string | null; spent: string; reserved: string
  shared_mode: SharedMode | null; suggested_mode: FundingMode | null
}
export interface TeamInvitation {
  id: number; team_id: number; team_name: string; owner_user_id: number; user_id: number
  status: 'pending' | 'accepted' | 'declined' | 'revoked'; expires_at: string; created_at: string
}
export interface TeamDetail extends TeamSummary {
  shared_subscription_ids: number[]; subscriptions: FundingSubscription[]
  members: TeamMember[]; invitations: TeamInvitation[]
}
export interface FundingPreferencesInput {
  mode: FundingMode; team_id: number | null; allow_personal_balance: boolean; allow_personal_subscription: boolean
}
export interface FundingPreferences extends FundingPreferencesInput {
  configured: boolean; personal_balance: string; personal_subscriptions: FundingSubscription[]; teams: TeamSummary[]
}
export interface TeamSettingsInput {
  name: string; shared_mode: SharedMode; shared_subscription_ids: number[]; suggested_mode: FundingMode | ''
}
export interface MemberSettingsInput {
  status?: 'active' | 'paused'; spend_limit?: string | null; shared_mode?: SharedMode | null; suggested_mode?: FundingMode | null
}
export interface TeamLedgerEntry {
  id: number; request_id: string; user_id: number; payer_user_id: number
  source: 'team_balance' | 'team_subscription'; subscription_id?: number | null; amount: string; created_at: string
}
export interface TeamLedger { items: TeamLedgerEntry[]; total: number; page: number; page_size: number }
const base = '/user'
export const teamFundingAPI = {
  async getFunding(): Promise<FundingPreferences> { return (await apiClient.get(`${base}/funding`)).data },
  async saveFunding(input: FundingPreferencesInput): Promise<void> { await apiClient.put(`${base}/funding`, input) },
  async listTeams(): Promise<{ owned: TeamSummary[]; joined: TeamSummary[] }> { return (await apiClient.get(`${base}/teams`)).data },
  async createTeam(name: string): Promise<TeamDetail> { return (await apiClient.post(`${base}/teams`, { name })).data },
  async getTeam(id: number): Promise<TeamDetail> { return (await apiClient.get(`${base}/teams/${id}`)).data },
  async updateTeam(id: number, input: TeamSettingsInput): Promise<void> { await apiClient.patch(`${base}/teams/${id}`, input) },
  async invite(id: number, userId: number): Promise<TeamInvitation> { return (await apiClient.post(`${base}/teams/${id}/invitations`, { user_id: userId })).data },
  async listInvitations(): Promise<{ items: TeamInvitation[] }> { return (await apiClient.get(`${base}/team-invitations`)).data },
  async acceptInvitation(id: number): Promise<void> { await apiClient.post(`${base}/team-invitations/${id}/accept`) },
  async declineInvitation(id: number): Promise<void> { await apiClient.post(`${base}/team-invitations/${id}/decline`) },
  async revokeInvitation(teamId: number, id: number): Promise<void> { await apiClient.delete(`${base}/teams/${teamId}/invitations/${id}`) },
  async updateMember(teamId: number, userId: number, input: MemberSettingsInput): Promise<void> { await apiClient.patch(`${base}/teams/${teamId}/members/${userId}`, input) },
  async removeMember(teamId: number, userId: number): Promise<void> { await apiClient.delete(`${base}/teams/${teamId}/members/${userId}`) },
  async getLedger(id: number, page = 1, pageSize = 20): Promise<TeamLedger> { return (await apiClient.get(`${base}/teams/${id}/ledger`, { params: { page, page_size: pageSize } })).data }
}

// Do not convert billing amounts to floating point numbers, including member limits.
export function validSpendLimit(value: string): boolean { return /^\d{1,12}(\.\d{1,8})?$/.test(value) }
export function displayAmount(value: string, unit: string): string { return `${value.replace(/(\.\d*?[1-9])0+$|\.0+$/, '$1')} ${unit}` }
export function fundingOrder(mode: FundingMode, balance = true, subscription = true): string[] {
  const orders: Record<FundingMode, string[]> = {
    personal_first: ['personalBalance', 'personalSubscription', 'teamSource'],
    personal_only: ['personalBalance'], team_first: ['teamSource', 'personalSubscription', 'personalBalance'],
    team_only: ['teamSource'], subscription_first: ['personalSubscription', 'personalBalance', 'teamSource'],
    subscription_only: ['personalSubscription']
  }
  return orders[mode].filter(source => (source !== 'personalBalance' || balance) && (source !== 'personalSubscription' || subscription))
}
