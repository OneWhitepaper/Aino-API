<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { teamFundingAPI, displayAmount, fundingModes, sharedModes, type TeamDetail, type TeamSummary, type TeamInvitation, type FundingSubscription, type FundingMode, type TeamSettingsInput, type MemberSettingsInput } from '@/api/teamFunding'
import AppLayout from '@/components/layout/AppLayout.vue'
import UserPageHeader from '@/components/user/workspace/UserPageHeader.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TeamMemberEditor from '@/components/teamFunding/TeamMemberEditor.vue'
import TeamLedger from '@/components/teamFunding/TeamLedger.vue'
const { t } = useI18n()
const auth = useAuthStore()
const owned = ref<TeamSummary[]>([])
const joined = ref<TeamSummary[]>([])
const invitations = ref<TeamInvitation[]>([])
const subscriptions = ref<FundingSubscription[]>([])
const loading = ref(true)
const failed = ref(false)
const detailLoading = ref(false)
const detailFailed = ref(false)
const detail = ref<TeamDetail | null>(null)
const selectedId = ref<number | null>(null)
const createOpen = ref(false)
const newName = ref('')
const inviteId = ref('')
const busy = ref('')
const message = ref('')
const operationFailed = ref(false)
const deletionTarget = ref<{ teamId: number; name: string } | null>(null)
const removalTarget = ref<{ teamId: number; userId: number; leaving: boolean } | null>(null)
const ledgerRevision = ref(0)
const settings = reactive<Omit<TeamSettingsInput, 'suggested_mode'> & { suggested_mode: FundingMode | null }>({ name: '', shared_mode: 'balance_first', shared_subscription_ids: [], suggested_mode: null })
const isOwner = computed(() => detail.value?.role === 'owner')
const unavailableSharedIds = computed(() => settings.shared_subscription_ids.filter(id => !subscriptions.value.some(subscription => subscription.id === id)))
const pending = computed(() => invitations.value.filter(invitation => invitation.status === 'pending' && new Date(invitation.expires_at).getTime() > Date.now()))
const outgoing = computed(() => detail.value?.invitations.filter(invitation => invitation.status === 'pending' && new Date(invitation.expires_at).getTime() > Date.now()) ?? [])
let detailGeneration = 0
function applyDetail(value: TeamDetail) {
  detail.value = value
  Object.assign(settings, { name: value.name, shared_mode: value.shared_mode, shared_subscription_ids: [...value.shared_subscription_ids], suggested_mode: value.suggested_mode || null })
}
async function selectTeam(id: number | null) {
  removalTarget.value = null
  deletionTarget.value = null
  const generation = ++detailGeneration
  selectedId.value = id; detail.value = null; detailFailed.value = false
  if (id === null) { detailLoading.value = false; return }
  detailLoading.value = true
  try { const value = await teamFundingAPI.getTeam(id); if (generation === detailGeneration) applyDetail(value) }
  catch { if (generation === detailGeneration) detailFailed.value = true }
  finally { if (generation === detailGeneration) detailLoading.value = false }
}
async function load() {
  loading.value = true; failed.value = false
  try {
    const [teams, received, funding] = await Promise.all([teamFundingAPI.listTeams(), teamFundingAPI.listInvitations(), teamFundingAPI.getFunding()])
    owned.value = teams.owned; joined.value = teams.joined; invitations.value = received.items; subscriptions.value = funding.personal_subscriptions
    const all = [...teams.owned, ...teams.joined]
    await selectTeam(all.some(team => team.id === selectedId.value) ? selectedId.value : (all[0]?.id ?? null))
  } catch { failed.value = true }
  finally { loading.value = false }
}
async function run(key: string, action: () => Promise<unknown>, after?: () => void) {
  if (busy.value) return
  busy.value = key; message.value = ''; operationFailed.value = false
  try { await action(); after?.(); message.value = t('teamFunding.operationSuccess') }
  catch { operationFailed.value = true; message.value = t('teamFunding.operationFailed') }
  finally { busy.value = '' }
}
async function create() {
  const name = newName.value.trim()
  if (!name) return
  await run('create', async () => {
    const value = await teamFundingAPI.createTeam(name)
    selectedId.value = value.id; createOpen.value = false; newName.value = ''; await load()
  })
}
async function saveTeam() {
  if (!detail.value || !isOwner.value || !settings.name.trim() || unavailableSharedIds.value.length > 0) return
  const id = detail.value.id
  await run('team', async () => { await teamFundingAPI.updateTeam(id, { ...settings, name: settings.name.trim(), shared_subscription_ids: [...settings.shared_subscription_ids], suggested_mode: settings.suggested_mode || '' }); await selectTeam(id) })
}
async function invite() {
  if (!detail.value || !isOwner.value) return
  const userId = Number(inviteId.value)
  if (!/^\d+$/.test(inviteId.value) || !Number.isSafeInteger(userId) || userId <= 0) { operationFailed.value = true; message.value = t('teamFunding.invalidUserId'); return }
  const id = detail.value.id
  await run('invite', async () => { await teamFundingAPI.invite(id, userId); inviteId.value = ''; await selectTeam(id) })
}
async function respond(id: number, accept: boolean) {
  await run(`invitation-${id}`, async () => { if (accept) await teamFundingAPI.acceptInvitation(id); else await teamFundingAPI.declineInvitation(id); await load() })
}
async function updateMember(userId: number, input: MemberSettingsInput) {
  if (!detail.value || !isOwner.value) return
  const id = detail.value.id
  await run(`member-${userId}`, async () => { await teamFundingAPI.updateMember(id, userId, input); await selectTeam(id) })
}
function requestRemoval(userId?: number) {
  if (!detail.value || busy.value || (userId !== undefined && !isOwner.value)) return
  const targetUserId = userId ?? auth.user?.id
  if (!targetUserId) return
  deletionTarget.value = null
  removalTarget.value = { teamId: detail.value.id, userId: targetUserId, leaving: userId === undefined }
}
async function remove() {
  const target = removalTarget.value
  if (!target || busy.value) return
  await run('remove', async () => { await teamFundingAPI.removeMember(target.teamId, target.userId); removalTarget.value = null; await load(); ledgerRevision.value++ })
}
function requestTeamDeletion() {
  if (!detail.value || !isOwner.value || busy.value) return
  removalTarget.value = null
  message.value = ''; operationFailed.value = false
  deletionTarget.value = { teamId: detail.value.id, name: detail.value.name }
}
async function deleteTeam() {
  const target = deletionTarget.value
  if (!target || busy.value || !isOwner.value || detail.value?.id !== target.teamId) return
  await run('delete-team', async () => {
    await teamFundingAPI.deleteTeam(target.teamId)
    owned.value = owned.value.filter(team => team.id !== target.teamId)
    joined.value = joined.value.filter(team => team.id !== target.teamId)
    await selectTeam(null)
    await load()
  })
}
async function revoke(id: number) {
  if (!detail.value || !isOwner.value) return
  const teamId = detail.value.id
  await run(`revoke-${id}`, async () => { await teamFundingAPI.revokeInvitation(teamId, id); await selectTeam(teamId) })
}
function changeTeam(event: Event) { if (!busy.value) void selectTeam(Number((event.target as HTMLSelectElement).value)) }
onMounted(load)
</script>
<template>
  <AppLayout>
    <div class="user-page space-y-6">
      <UserPageHeader :title="t('teamFunding.teamTitle')" :description="t('teamFunding.teamDescription')"><button class="btn btn-primary" :disabled="!!busy || loading" @click="createOpen = !createOpen">{{ t('teamFunding.createTeam') }}</button></UserPageHeader>
      <form v-if="createOpen" class="card flex flex-wrap items-end gap-3 p-5 sm:p-6" @submit.prevent="create"><label for="new-team-name" class="min-w-0 flex-1 space-y-2 text-sm"><span>{{ t('teamFunding.teamName') }}</span><input id="new-team-name" v-model="newName" class="input w-full" maxlength="100" required :disabled="!!busy" /></label><button class="btn btn-primary" :disabled="!!busy || !newName.trim()" type="submit">{{ t('teamFunding.create') }}</button></form>
      <p v-if="message && deletionTarget === null" :role="operationFailed ? 'alert' : 'status'" class="rounded-lg p-3 text-sm" :class="operationFailed ? 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300' : 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'">{{ message }}</p>
      <div v-if="loading" class="flex justify-center py-12"><LoadingSpinner /></div>
      <div v-else-if="failed" class="card p-8 text-center" role="alert"><p>{{ t('teamFunding.loadFailed') }}</p><button class="btn btn-secondary mt-4" @click="load">{{ t('workspace.retry') }}</button></div>
      <template v-else>
        <section v-if="pending.length" class="card p-5 sm:p-6"><h3 class="text-lg font-semibold">{{ t('teamFunding.pendingInvitations') }}</h3><p class="mt-1 text-sm text-gray-500">{{ t('teamFunding.invitationHelp') }}</p><ul class="mt-4 space-y-3"><li v-for="invitation in pending" :key="invitation.id" class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-gray-200 p-4 dark:border-dark-600"><div><p class="font-medium">{{ invitation.team_name }}</p><p class="mt-1 text-xs text-gray-500">{{ t('teamFunding.invitationFrom', { id: invitation.owner_user_id }) }} · {{ t('teamFunding.expires', { date: new Date(invitation.expires_at).toLocaleDateString() }) }}</p></div><div class="flex gap-2"><button class="btn btn-primary btn-sm" :disabled="!!busy" :data-accept="invitation.id" @click="respond(invitation.id, true)">{{ t('teamFunding.accept') }}</button><button class="btn btn-secondary btn-sm" :disabled="!!busy" :data-decline="invitation.id" @click="respond(invitation.id, false)">{{ t('teamFunding.decline') }}</button></div></li></ul></section>
        <p v-if="!owned.length && !joined.length" class="card p-10 text-center text-sm text-gray-500">{{ t('teamFunding.noTeams') }}</p>
        <label v-else for="team-detail-selector" class="block max-w-xl space-y-2 text-sm font-medium"><span>{{ t('teamFunding.chooseTeam') }}</span><select id="team-detail-selector" class="input w-full" :value="selectedId" :disabled="!!busy" @change="changeTeam"><optgroup v-if="owned.length" :label="t('teamFunding.ownedTeams')"><option v-for="team in owned" :key="team.id" :value="team.id">{{ team.name }}</option></optgroup><optgroup v-if="joined.length" :label="t('teamFunding.joinedTeams')"><option v-for="team in joined" :key="team.id" :value="team.id">{{ team.name }} · {{ t(`teamFunding.status.${team.status}`) }}</option></optgroup></select></label>
        <div v-if="detailLoading" class="flex justify-center py-10"><LoadingSpinner /></div>
        <div v-else-if="detailFailed" class="card p-6" role="alert"><p>{{ t('teamFunding.detailFailed') }}</p><button class="btn btn-secondary mt-3" @click="selectTeam(selectedId)">{{ t('workspace.retry') }}</button></div>
        <template v-else-if="detail">
          <section class="card p-5 sm:p-6"><header class="flex flex-wrap items-center justify-between gap-3"><div><h3 class="text-xl font-semibold">{{ detail.name }}</h3><p class="mt-1 text-sm text-gray-500">{{ t(isOwner ? 'teamFunding.owner' : 'teamFunding.member') }} · #{{ detail.owner_user_id }}</p></div><button v-if="isOwner" type="button" class="btn btn-danger btn-sm" data-delete-team :disabled="!!busy" @click="requestTeamDeletion">{{ t('teamFunding.deleteTeam') }}</button><button v-else-if="detail.status !== 'removed'" class="btn btn-secondary btn-sm" :disabled="!!busy" @click="requestRemoval()">{{ t('teamFunding.leave') }}</button></header><dl class="mt-5 grid gap-5 sm:grid-cols-3"><div><dt class="text-sm text-gray-500">{{ t('teamFunding.teamBalance') }}</dt><dd class="mt-1 break-all text-2xl font-semibold">{{ displayAmount(detail.available_balance, t('teamFunding.credits')) }}</dd></div><div><dt class="text-sm text-gray-500">{{ t('teamFunding.spent') }}</dt><dd class="mt-1 break-all text-2xl font-semibold">{{ displayAmount(detail.spent, t('teamFunding.credits')) }}</dd></div><div><dt class="text-sm text-gray-500">{{ t('teamFunding.limit') }}</dt><dd class="mt-1 text-2xl font-semibold">{{ detail.spend_limit === null ? t('teamFunding.unlimited') : displayAmount(detail.spend_limit, t('teamFunding.credits')) }}</dd></div></dl><p class="mt-5 text-sm leading-relaxed text-gray-500">{{ t('teamFunding.ownerFundsHelp') }}</p></section>
          <template v-if="isOwner">
            <section class="card p-5 sm:p-6"><h3 class="text-lg font-semibold">{{ t('teamFunding.sharedSettings') }}</h3><p class="mt-1 text-sm leading-relaxed text-gray-500">{{ t('teamFunding.consentBoundary') }}</p><form class="mt-5 space-y-5" @submit.prevent="saveTeam"><fieldset :disabled="!!busy" class="grid gap-5 md:grid-cols-2"><label class="space-y-2 text-sm" for="team-name"><span>{{ t('teamFunding.teamName') }}</span><input id="team-name" v-model="settings.name" class="input w-full" maxlength="100" required /></label><label class="space-y-2 text-sm" for="team-shared-mode"><span>{{ t('teamFunding.sharedMode') }}</span><select id="team-shared-mode" v-model="settings.shared_mode" class="input w-full"><option v-for="mode in sharedModes" :key="mode" :value="mode">{{ t(`teamFunding.sharedModes.${mode}`) }}</option></select></label><label class="space-y-2 text-sm" for="team-suggested-mode"><span>{{ t('teamFunding.suggestedMode') }}</span><select id="team-suggested-mode" v-model="settings.suggested_mode" class="input w-full"><option :value="null">{{ t('teamFunding.noSuggestion') }}</option><option v-for="mode in fundingModes" :key="mode" :value="mode">{{ t(`teamFunding.modes.${mode}`) }}</option></select></label><div class="space-y-3 text-sm"><p class="font-medium">{{ t('teamFunding.sharedSubscriptions') }}</p><p v-if="!subscriptions.length" class="text-gray-500">{{ t('teamFunding.noSharedSubscriptions') }}</p><label v-for="subscription in subscriptions" :key="subscription.id" class="flex items-start gap-2" :for="`shared-subscription-${subscription.id}`"><input :id="`shared-subscription-${subscription.id}`" v-model="settings.shared_subscription_ids" type="checkbox" class="checkbox mt-0.5" :value="subscription.id" /><span>{{ subscription.group_name }} #{{ subscription.id }}<span class="mt-1 block text-xs text-gray-500">{{ subscription.expires_at ? t('teamFunding.expires', { date: new Date(subscription.expires_at).toLocaleDateString() }) : t('teamFunding.noExpiry') }}</span></span></label><div v-if="unavailableSharedIds.length" class="space-y-3 rounded-lg border border-amber-200 p-3 dark:border-amber-700"><p class="text-xs leading-relaxed text-amber-700 dark:text-amber-300">{{ t('teamFunding.unavailableSubscriptionsHelp') }}</p><label v-for="id in unavailableSharedIds" :key="id" :for="`unavailable-shared-subscription-${id}`" class="flex items-start gap-2"><input :id="`unavailable-shared-subscription-${id}`" v-model="settings.shared_subscription_ids" type="checkbox" class="checkbox mt-0.5" :value="id" /><span>{{ t('teamFunding.unavailableSubscription', { id }) }}</span></label></div><p class="text-xs leading-relaxed text-gray-500">{{ t('teamFunding.subscriptionHelp') }}</p></div><div class="md:col-span-2"><button class="btn btn-primary" type="submit" :disabled="!settings.name.trim() || unavailableSharedIds.length > 0">{{ t('teamFunding.saveTeam') }}</button></div></fieldset></form></section>
            <section class="card p-5 sm:p-6"><form class="flex flex-wrap items-end gap-3" @submit.prevent="invite"><label for="invite-user-id" class="min-w-0 flex-1 space-y-2 text-sm"><span>{{ t('teamFunding.inviteUserId') }}</span><input id="invite-user-id" v-model="inviteId" inputmode="numeric" class="input w-full" required :disabled="!!busy" /></label><button class="btn btn-primary" type="submit" :disabled="!!busy || !inviteId">{{ t('teamFunding.invite') }}</button></form><p class="mt-3 text-sm text-gray-500">{{ t('teamFunding.inviteHelp') }}</p><div v-if="outgoing.length" class="mt-5"><h4 class="text-sm font-medium">{{ t('teamFunding.pendingOutgoing') }}</h4><ul class="mt-2 divide-y divide-gray-100 dark:divide-dark-700"><li v-for="invitation in outgoing" :key="invitation.id" class="flex flex-wrap items-center justify-between gap-2 py-3 text-sm"><span>#{{ invitation.user_id }} · {{ t('teamFunding.expires', { date: new Date(invitation.expires_at).toLocaleDateString() }) }}</span><button class="btn btn-ghost btn-sm" :disabled="!!busy" @click="revoke(invitation.id)">{{ t('teamFunding.revoke') }}</button></li></ul></div></section>
            <section class="card p-5 sm:p-6"><h3 class="text-lg font-semibold">{{ t('teamFunding.members') }}</h3><p v-if="!detail.members.length" class="mt-5 text-sm text-gray-500">{{ t('teamFunding.noMembers') }}</p><div v-else class="mt-5 grid gap-4 xl:grid-cols-2"><TeamMemberEditor v-for="member in detail.members" :key="member.user_id" :member="member" :busy="!!busy" @save="input => updateMember(member.user_id, input)" @remove="requestRemoval(member.user_id)" /></div></section>
          </template>
          <section v-else class="card p-5 sm:p-6"><h3 class="font-semibold">{{ t('teamFunding.sharedMode') }}</h3><p class="mt-2 text-sm">{{ t(`teamFunding.sharedModes.${detail.shared_mode}`) }}</p><p v-if="detail.suggested_mode" class="mt-3 text-sm text-gray-500">{{ t('teamFunding.suggested', { mode: t(`teamFunding.modes.${detail.suggested_mode}`) }) }}</p><h4 class="mt-5 text-sm font-medium">{{ t('teamFunding.sharedSubscriptions') }}</h4><p v-if="!detail.subscriptions.length" class="mt-2 text-sm text-gray-500">{{ t('teamFunding.noSubscriptions') }}</p><ul v-else class="mt-2 space-y-2 text-sm"><li v-for="subscription in detail.subscriptions" :key="subscription.id">{{ subscription.group_name }} #{{ subscription.id }}</li></ul><RouterLink class="btn btn-secondary mt-5" to="/funding">{{ t('teamFunding.preferencesTitle') }}</RouterLink></section>
          <TeamLedger :team-id="detail.id" :owner="isOwner" :revision="ledgerRevision" />
        </template>
      </template>
      <ConfirmDialog :show="deletionTarget !== null" :title="t('teamFunding.deleteTeamTitle')" :message="t('teamFunding.deleteTeamMessage', { name: deletionTarget?.name ?? '' })" :confirm-text="t('teamFunding.deleteTeam')" :busy="!!busy" danger @confirm="deleteTeam" @cancel="() => { if (!busy) deletionTarget = null }"><p v-if="operationFailed" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ message }}</p></ConfirmDialog>
      <ConfirmDialog :show="removalTarget !== null" :title="t(removalTarget?.leaving ? 'teamFunding.leaveTitle' : 'teamFunding.removeTitle')" :message="t(removalTarget?.leaving ? 'teamFunding.leaveMessage' : 'teamFunding.removeMessage')" danger @confirm="remove" @cancel="() => { if (!busy) removalTarget = null }" />
    </div>
  </AppLayout>
</template>
