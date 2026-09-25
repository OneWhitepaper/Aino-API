<template>
  <div id="desktop-model-import" data-testid="desktop-model-import" class="space-y-4 rounded-xl border border-primary-200 bg-primary-50/40 p-4 dark:border-primary-800 dark:bg-primary-900/10">
    <div>
      <h3 class="font-medium text-gray-900 dark:text-white">{{ t('admin.settings.desktop.import.title') }}</h3>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.desktop.import.description') }}</p>
    </div>
    <div class="grid gap-4 md:grid-cols-3">
      <div class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
        <label for="desktop-import-group">{{ t('admin.settings.desktop.group') }}</label>
        <Select id="desktop-import-group" v-model="groupID" :options="groupOptions" :disabled="groupsLoading" searchable :aria-label="t('admin.settings.desktop.group')" />
      </div>
      <div class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
        <label for="desktop-import-account">{{ t('admin.settings.desktop.import.account') }}</label>
        <Select id="desktop-import-account" v-model="accountID" :options="accountOptions" :disabled="!groupID || accountsLoading" :loading="accountsLoading" searchable :aria-label="t('admin.settings.desktop.import.account')" />
      </div>
      <div class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
        <label for="desktop-import-protocol">{{ t('admin.settings.desktop.protocol') }}</label>
        <Select id="desktop-import-protocol" v-model="apiMode" :options="protocolOptions" :aria-label="t('admin.settings.desktop.protocol')" />
      </div>
    </div>
    <div v-if="accountsError" role="alert" class="flex flex-wrap items-center gap-3 text-sm text-red-600 dark:text-red-400">
      <span>{{ t('admin.settings.desktop.import.accountsFailed') }}</span>
      <button type="button" class="btn btn-secondary" @click="loadAccounts">{{ t('common.refresh') }}</button>
    </div>
    <p v-else-if="groupID && !accountsLoading && !accounts.length" class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.desktop.import.noAccounts') }}</p>
    <button type="button" class="btn btn-secondary" data-testid="desktop-import-fetch" :disabled="!account || fetching || accountsLoading" @click="fetchModels">{{ t(fetching ? 'admin.settings.desktop.import.fetching' : 'admin.settings.desktop.import.fetch') }}</button>
    <p v-if="fetchError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t('admin.settings.desktop.import.fetchFailed') }}</p>
    <template v-if="catalog">
      <p v-if="catalog.warnings?.length" role="status" class="text-sm text-amber-700 dark:text-amber-400">{{ t('admin.settings.desktop.import.metadataWarning') }}</p>
      <div class="flex flex-wrap items-center gap-3">
        <input v-model="search" type="search" class="input min-w-0 basis-full sm:basis-64 sm:flex-1" :placeholder="t('admin.settings.desktop.search')" :aria-label="t('admin.settings.desktop.import.search')" />
        <button type="button" class="btn btn-secondary" data-testid="desktop-import-select-all" @click="selectVisible">{{ t('admin.settings.desktop.import.selectAll') }}</button>
        <button type="button" class="btn btn-secondary" @click="selected = new Set()">{{ t('admin.settings.desktop.import.clearSelection') }}</button>
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.desktop.import.count', { total: candidates.length, selected: selectedDrafts.length, remaining }) }}</p>
      <div class="max-h-80 space-y-1 overflow-y-auto rounded-lg border border-gray-200 bg-white p-2 dark:border-dark-600 dark:bg-dark-800">
        <p v-if="!visibleCandidates.length" class="p-3 text-sm text-gray-500">{{ t('admin.settings.desktop.noMatches') }}</p>
        <label v-for="entry in visibleCandidates" :key="entry.model" class="flex items-start gap-3 rounded-md p-3 hover:bg-gray-50 dark:hover:bg-dark-700" :class="{ 'opacity-60': unavailableReason(entry) }">
          <input type="checkbox" class="mt-1 shrink-0" data-testid="desktop-import-choice" :value="entry.model" :checked="selected.has(entry.model) && !unavailableReason(entry)" :disabled="!!unavailableReason(entry)" @change="toggleSelected(entry.model)" />
          <span class="min-w-0 flex-1">
            <span class="block break-words text-sm font-medium text-gray-900 dark:text-white">{{ entry.display_name }}</span>
            <span class="block break-all text-xs text-gray-500 dark:text-gray-400">{{ entry.model }}</span>
            <span v-if="unavailableReason(entry)" class="mt-1 block text-xs text-amber-700 dark:text-amber-400">{{ t(unavailableReason(entry)) }}</span>
          </span>
        </label>
      </div>
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.desktop.import.reviewHint') }}</p>
      <p v-if="selectedDrafts.length > remaining" role="alert" class="text-sm text-amber-700 dark:text-amber-400">{{ t('admin.settings.desktop.import.limit', { remaining }) }}</p>
      <button type="button" class="btn btn-primary" data-testid="desktop-import-confirm" :disabled="!selectedDrafts.length || selectedDrafts.length > remaining" @click="importSelected">{{ t('admin.settings.desktop.import.confirm', { count: selectedDrafts.length }) }}</button>
    </template>
    <p v-if="importedCount !== null" role="status" class="text-sm text-emerald-700 dark:text-emerald-400">{{ t('admin.settings.desktop.import.success', { count: importedCount }) }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import { list, syncUpstreamModels, type SyncUpstreamModelsResult } from '@/api/admin/accounts'
import type { DesktopModelEntry } from '@/api/admin/settings'
import type { AccountListItem, AdminGroup } from '@/types'
import { buildDesktopModelDrafts, DESKTOP_MODEL_LIMIT, modelAllowedForImport, prepareDesktopModelImport, sameDesktopModel } from './desktopModelImport'

const props = defineProps<{ groups: AdminGroup[]; groupsLoading: boolean; models: DesktopModelEntry[] }>()
const emit = defineEmits<{ import: [models: DesktopModelEntry[]] }>()
const { t } = useI18n()
const groupID = ref<number | null>(null)
const accountID = ref<number | null>(null)
const apiMode = ref<DesktopModelEntry['api_mode']>('chat_completions')
const accounts = ref<AccountListItem[]>([])
const accountsLoading = ref(false)
const accountsError = ref(false)
const fetching = ref(false)
const fetchError = ref(false)
const catalog = ref<SyncUpstreamModelsResult | null>(null)
const selected = ref(new Set<string>())
const search = ref('')
const importedCount = ref<number | null>(null)
let accountsRequest = 0
let modelsRequest = 0
const group = computed(() => props.groups.find(item => item.id === groupID.value))
const account = computed(() => accounts.value.find(item => item.id === accountID.value))
const groupOptions = computed(() => props.groups.map(item => ({ value: item.id, label: `${item.name} · ${item.platform}` })))
const accountOptions = computed(() => accounts.value.map(item => ({ value: item.id, label: `${item.name} · ${item.platform} · ${item.type}` })))
const protocolOptions = [
  { value: 'chat_completions', label: 'Chat Completions' },
  { value: 'responses', label: 'Responses' },
  { value: 'anthropic_messages', label: 'Anthropic Messages' }
]
const candidates = computed(() => catalog.value && group.value && account.value ? buildDesktopModelDrafts(catalog.value, group.value, account.value, apiMode.value) : [])
const visibleCandidates = computed(() => candidates.value.filter(entry => `${entry.model} ${entry.display_name}`.toLowerCase().includes(search.value.trim().toLowerCase())))
const selectedDrafts = computed(() => candidates.value.filter(entry => selected.value.has(entry.model) && !unavailableReason(entry)))
const remaining = computed(() => Math.max(0, DESKTOP_MODEL_LIMIT - props.models.length))

function unavailableReason(entry: DesktopModelEntry): string {
  if (!group.value || !modelAllowedForImport(group.value, entry.model)) return 'admin.settings.desktop.import.notAllowed'
  if (props.models.some(existing => sameDesktopModel(existing, entry))) return 'admin.settings.desktop.import.exists'
  return ''
}

function resetCatalog(): void {
  modelsRequest++
  catalog.value = null
  selected.value = new Set()
  search.value = ''
  fetching.value = false
  fetchError.value = false
  importedCount.value = null
}

async function loadAccounts(): Promise<void> {
  const request = ++accountsRequest
  const selectedGroup = group.value
  accounts.value = []
  accountID.value = null
  accountsError.value = false
  resetCatalog()
  accountsLoading.value = !!selectedGroup
  if (!selectedGroup) return
  try {
    const items: AccountListItem[] = []
    let page = 1
    let pages = 1
    do {
      const result = await list(page, 100, { group: String(selectedGroup.id), status: 'active', lite: '1' })
      if (request !== accountsRequest) return
      items.push(...result.items)
      pages = result.pages
      page++
    } while (page <= pages)
    accounts.value = items.filter(item => selectedGroup.platform === 'composite' || item.platform === selectedGroup.platform)
    if (accounts.value.length === 1) accountID.value = accounts.value[0].id
  } catch {
    if (request === accountsRequest) accountsError.value = true
  } finally {
    if (request === accountsRequest) accountsLoading.value = false
  }
}

async function fetchModels(): Promise<void> {
  if (!account.value || !group.value) return
  const request = ++modelsRequest
  const sourceID = account.value.id
  fetching.value = true
  fetchError.value = false
  catalog.value = null
  selected.value = new Set()
  importedCount.value = null
  try {
    const result = await syncUpstreamModels(sourceID)
    if (request !== modelsRequest) return
    catalog.value = result
    selected.value = new Set(candidates.value.filter(entry => !unavailableReason(entry)).slice(0, remaining.value).map(entry => entry.model))
  } catch {
    if (request === modelsRequest) fetchError.value = true
  } finally {
    if (request === modelsRequest) fetching.value = false
  }
}

function toggleSelected(model: string): void {
  if (selected.value.has(model)) selected.value.delete(model)
  else selected.value.add(model)
}

function selectVisible(): void {
  visibleCandidates.value.filter(entry => !unavailableReason(entry)).forEach(entry => selected.value.add(entry.model))
}

function importSelected(): void {
  if (!selectedDrafts.value.length || selectedDrafts.value.length > remaining.value) return
  const additions = prepareDesktopModelImport(props.models, selectedDrafts.value)
  emit('import', additions)
  selected.value = new Set()
  importedCount.value = additions.length
}

watch(groupID, loadAccounts)
watch(accountID, () => {
  resetCatalog()
  apiMode.value = account.value?.platform === 'anthropic' ? 'anthropic_messages' : account.value?.platform === 'openai' && account.value.type === 'oauth' ? 'responses' : 'chat_completions'
})
onBeforeUnmount(() => { accountsRequest++; modelsRequest++ })
</script>
