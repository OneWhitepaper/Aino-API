<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { teamFundingAPI, displayAmount, type TeamLedger } from '@/api/teamFunding'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
const props = defineProps<{ teamId: number; owner: boolean; revision?: number }>()
const { t } = useI18n()
const ledger = ref<TeamLedger>({ items: [], total: 0, page: 1, page_size: 20 })
const loading = ref(false)
const failed = ref(false)
// Decimal strings stay exact, including the smallest eight-decimal overage.
function hasPendingReviewAmount(value?: string): boolean { return /^\d+(?:\.\d+)?$/.test(value ?? '') && /[1-9]/.test(value ?? '') }
let generation = 0
async function load(page = 1) {
  const current = ++generation
  loading.value = true; failed.value = false
  try { const data = await teamFundingAPI.getLedger(props.teamId, page, 20); if (current === generation) ledger.value = data }
  catch { if (current === generation) failed.value = true }
  finally { if (current === generation) loading.value = false }
}
watch(() => [props.teamId, props.revision], () => { ledger.value = { items: [], total: 0, page: 1, page_size: 20 }; void load() }, { immediate: true })
</script>
<template>
  <section class="card min-w-0 p-5 sm:p-6" :aria-busy="loading">
    <h3 class="text-lg font-semibold">{{ t('teamFunding.ledgerTitle') }}</h3>
    <p class="mt-1 text-sm text-gray-500">{{ t(owner ? 'teamFunding.ownerLedgerHelp' : 'teamFunding.memberLedgerHelp') }}</p>
    <div v-if="loading" class="flex justify-center py-8"><LoadingSpinner /></div>
    <div v-else-if="failed" role="alert" class="py-6"><p>{{ t('teamFunding.ledgerFailed') }}</p><button class="btn btn-secondary mt-3" @click="load(ledger.page)">{{ t('workspace.retry') }}</button></div>
    <p v-else-if="ledger.items.length === 0" class="py-8 text-center text-sm text-gray-500">{{ t('teamFunding.noLedger') }}</p>
    <template v-else>
      <div class="mt-4 overflow-x-auto">
        <table class="w-full text-left text-sm">
          <thead><tr class="border-b border-gray-200 text-gray-500 dark:border-dark-600"><th class="whitespace-nowrap p-3">{{ t('teamFunding.date') }}</th><th class="p-3">{{ t('teamFunding.caller') }}</th><th class="p-3">{{ t('teamFunding.payer') }}</th><th class="p-3">{{ t('teamFunding.source') }}</th><th class="p-3">{{ t('teamFunding.amount') }}</th><th class="p-3">{{ t('teamFunding.request') }}</th></tr></thead>
          <tbody><tr v-for="entry in ledger.items" :key="entry.id" class="border-b border-gray-100 dark:border-dark-700"><td class="whitespace-nowrap p-3">{{ new Date(entry.created_at).toLocaleString() }}</td><td class="p-3">#{{ entry.user_id }}</td><td class="p-3">#{{ entry.payer_user_id }}</td><td class="whitespace-nowrap p-3">{{ t(`teamFunding.sources.${entry.source}`) }}<span v-if="entry.subscription_id"> #{{ entry.subscription_id }}</span></td><td class="p-3"><p class="whitespace-nowrap font-medium" data-testid="team-charge">{{ displayAmount(entry.amount, t('teamFunding.credits')) }}</p><p v-if="hasPendingReviewAmount(entry.unauthorized_amount)" class="mt-1 min-w-40 max-w-64 text-xs leading-relaxed text-amber-700 dark:text-amber-300" data-testid="pending-review-amount">{{ t('teamFunding.pendingReviewAmount', { amount: displayAmount(entry.unauthorized_amount || '0', t('teamFunding.credits')) }) }}</p></td><td class="max-w-48 break-all p-3 font-mono text-xs">{{ entry.request_id }}</td></tr></tbody>
        </table>
      </div>
      <div class="mt-4 flex flex-wrap items-center justify-between gap-3 text-sm"><span>{{ t('teamFunding.ledgerCount', { total: ledger.total, page: ledger.page }) }}</span><div class="flex gap-2"><button class="btn btn-secondary btn-sm" :disabled="ledger.page <= 1" @click="load(ledger.page - 1)">{{ t('pagination.previous') }}</button><button class="btn btn-secondary btn-sm" :disabled="ledger.page * ledger.page_size >= ledger.total" @click="load(ledger.page + 1)">{{ t('pagination.next') }}</button></div></div>
    </template>
  </section>
</template>
