<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { teamFundingAPI, displayAmount, fundingOrder, type FundingPreferences, type FundingPreferencesInput } from '@/api/teamFunding'
import AppLayout from '@/components/layout/AppLayout.vue'
import UserPageHeader from '@/components/user/workspace/UserPageHeader.vue'
import FundingModeSelect from '@/components/teamFunding/FundingModeSelect.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
const { t } = useI18n()
const funding = ref<FundingPreferences | null>(null)
const loading = ref(true)
const failed = ref(false)
const saving = ref(false)
const feedback = ref('')
const saveFailed = ref(false)
const form = reactive<FundingPreferencesInput>({ mode: 'personal_first', team_id: null, allow_personal_balance: true, allow_personal_subscription: true })
const teams = computed(() => funding.value?.teams.filter(team => team.role === 'member' && team.status === 'active') ?? [])
const selectedTeam = computed(() => teams.value.find(team => team.id === form.team_id))
const order = computed(() => fundingOrder(form.mode, form.allow_personal_balance, form.allow_personal_subscription).filter(source => source !== 'teamSource' || !!selectedTeam.value))
async function load() {
  loading.value = true; failed.value = false
  try {
    const data = await teamFundingAPI.getFunding(); funding.value = data
    Object.assign(form, { mode: data.mode || 'personal_first', team_id: data.team_id, allow_personal_balance: data.allow_personal_balance, allow_personal_subscription: data.allow_personal_subscription })
  } catch { failed.value = true }
  finally { loading.value = false }
}
async function save() {
  if (saving.value) return
  saving.value = true; feedback.value = ''; saveFailed.value = false
  try { await teamFundingAPI.saveFunding({ ...form }); if (funding.value) funding.value.configured = true; feedback.value = t('teamFunding.saved') }
  catch { saveFailed.value = true; feedback.value = t('teamFunding.saveFailed') }
  finally { saving.value = false }
}
onMounted(load)
</script>
<template>
  <AppLayout>
    <div class="user-page space-y-6">
      <UserPageHeader :title="t('teamFunding.preferencesTitle')" :description="t('teamFunding.preferencesDescription')" />
      <div v-if="loading" class="flex justify-center py-12"><LoadingSpinner /></div>
      <div v-else-if="failed" class="card p-8 text-center" role="alert"><p>{{ t('teamFunding.loadFailed') }}</p><button class="btn btn-secondary mt-4" @click="load">{{ t('workspace.retry') }}</button></div>
      <template v-else-if="funding">
        <div class="grid gap-6 lg:grid-cols-3">
          <section class="card p-5 sm:p-6 lg:col-span-2">
            <p class="mb-5 rounded-lg bg-gray-50 p-3 text-sm text-gray-600 dark:bg-dark-700 dark:text-gray-300" data-testid="configuration-state">{{ t(funding.configured ? 'teamFunding.configured' : 'teamFunding.legacy') }}</p>
            <form class="space-y-6" @submit.prevent="save">
              <fieldset :disabled="saving" class="space-y-6">
                <FundingModeSelect :model-value="form.mode" id="funding-mode" :label="t('teamFunding.mode')" @update:model-value="value => { if (value) form.mode = value }" />
                <div class="space-y-2"><p class="text-sm text-gray-500">{{ t('teamFunding.modeHelp') }}</p><p class="text-sm font-medium">{{ t('teamFunding.fallback') }}</p><ol v-if="order.length" class="flex flex-wrap gap-2 text-sm" data-testid="funding-order"><li v-for="(source, index) in order" :key="source" class="rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-600"><span class="mr-2 text-gray-400">{{ index + 1 }}.</span>{{ t(`teamFunding.${source}`) }}</li></ol><p v-else role="status" class="text-sm text-amber-700 dark:text-amber-300">{{ t('teamFunding.noSources') }}</p></div>
                <label for="funding-team" class="block space-y-2 text-sm font-medium"><span>{{ t('teamFunding.selectedTeam') }}</span><select id="funding-team" v-model="form.team_id" class="input w-full"><option :value="null">{{ t('teamFunding.noTeam') }}</option><option v-if="form.team_id !== null && !selectedTeam" :value="form.team_id" disabled>{{ t('teamFunding.missingTeam') }}</option><option v-for="team in teams" :key="team.id" :value="team.id">{{ team.name }}</option></select></label>
                <p v-if="!teams.length" class="text-sm text-gray-500">{{ t('teamFunding.noAvailableTeam') }} <RouterLink class="text-primary-600" to="/team-sharing">{{ t('teamFunding.teamTitle') }}</RouterLink></p>
                <div class="space-y-3 rounded-xl border border-gray-200 p-4 dark:border-dark-600"><label class="flex items-start gap-3 text-sm" for="allow-personal-balance"><input id="allow-personal-balance" v-model="form.allow_personal_balance" type="checkbox" class="checkbox mt-0.5" />{{ t('teamFunding.personalBalanceConsent') }}</label><label class="flex items-start gap-3 text-sm" for="allow-personal-subscription"><input id="allow-personal-subscription" v-model="form.allow_personal_subscription" type="checkbox" class="checkbox mt-0.5" />{{ t('teamFunding.personalSubscriptionConsent') }}</label><p class="text-xs leading-relaxed text-gray-500">{{ t('teamFunding.consentHelp') }}</p></div>
                <p v-if="selectedTeam?.suggested_mode" class="rounded-lg bg-primary-50 p-3 text-sm dark:bg-primary-900/20">{{ t('teamFunding.suggested', { mode: t(`teamFunding.modes.${selectedTeam.suggested_mode}`) }) }}</p>
                <button class="btn btn-primary" type="submit">{{ t('teamFunding.save') }}</button>
              </fieldset>
              <p v-if="feedback" :role="saveFailed ? 'alert' : 'status'" class="text-sm" :class="saveFailed ? 'text-red-600' : 'text-emerald-600'">{{ feedback }}</p>
            </form>
          </section>
          <aside class="min-w-0 space-y-6">
            <section class="card p-5 sm:p-6"><h3 class="text-sm font-medium text-gray-500">{{ t('teamFunding.personalBalance') }}</h3><p class="mt-2 break-all text-3xl font-semibold" data-testid="personal-balance">{{ displayAmount(funding.personal_balance, t('teamFunding.credits')) }}</p></section>
            <section class="card p-5 sm:p-6"><h3 class="font-semibold">{{ t('teamFunding.personalSubscriptions') }}</h3><p v-if="!funding.personal_subscriptions.length" class="mt-3 text-sm text-gray-500">{{ t('teamFunding.noSubscriptions') }}</p><ul v-else class="mt-3 space-y-3"><li v-for="subscription in funding.personal_subscriptions" :key="subscription.id" class="text-sm"><p class="font-medium">{{ subscription.group_name }} <span class="text-gray-400">#{{ subscription.id }}</span></p><p class="mt-1 text-xs text-gray-500">{{ subscription.expires_at ? t('teamFunding.expires', { date: new Date(subscription.expires_at).toLocaleDateString() }) : t('teamFunding.noExpiry') }}</p></li></ul><p class="mt-3 text-xs leading-relaxed text-gray-500">{{ t('teamFunding.subscriptionHelp') }}</p></section>
          </aside>
        </div>
        <section v-if="selectedTeam" class="card p-5 sm:p-6" data-testid="team-balance"><div class="flex flex-wrap items-center justify-between gap-3"><h3 class="text-lg font-semibold">{{ selectedTeam.name }}</h3><span class="text-sm text-gray-500">{{ t(`teamFunding.sharedModes.${selectedTeam.shared_mode}`) }}</span></div><dl class="mt-5 grid gap-5 sm:grid-cols-3"><div><dt class="text-sm text-gray-500">{{ t('teamFunding.teamBalance') }}</dt><dd class="mt-1 text-xl font-semibold">{{ displayAmount(selectedTeam.available_balance, t('teamFunding.credits')) }}</dd></div><div><dt class="text-sm text-gray-500">{{ t('teamFunding.remainingLimit') }}</dt><dd class="mt-1 text-xl font-semibold">{{ selectedTeam.remaining_limit === null ? t('teamFunding.unlimited') : displayAmount(selectedTeam.remaining_limit, t('teamFunding.credits')) }}</dd></div><div><dt class="text-sm text-gray-500">{{ t('teamFunding.spent') }}</dt><dd class="mt-1 text-xl font-semibold">{{ displayAmount(selectedTeam.spent, t('teamFunding.credits')) }}</dd></div></dl><p class="mt-5 text-sm text-gray-500">{{ t('teamFunding.ownerFundsHelp') }}</p></section>
      </template>
    </div>
  </AppLayout>
</template>
