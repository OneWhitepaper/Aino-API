<template>
  <div id="desktop-model-import" data-testid="desktop-model-import" class="space-y-4 rounded-xl border border-primary-200 bg-primary-50/40 p-4 dark:border-primary-800 dark:bg-primary-900/10">
    <div>
      <h3 class="font-medium text-gray-900 dark:text-white">{{ t('admin.settings.desktop.import.title') }}</h3>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.desktop.import.description') }}</p>
    </div>
    <div class="grid items-end gap-4 sm:grid-cols-[minmax(0,1fr)_auto]">
      <div class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
        <label for="desktop-import-group">{{ t('admin.settings.desktop.group') }}</label>
        <Select id="desktop-import-group" v-model="groupID" :options="groupOptions" :disabled="groupsLoading" searchable :aria-label="t('admin.settings.desktop.group')" />
      </div>
      <button type="button" class="btn btn-secondary" data-testid="desktop-import-fetch" :disabled="!sources.length || fetching || accountsLoading || accountsError" @click="fetchModels">{{ t(fetching ? 'admin.settings.desktop.import.fetching' : 'admin.settings.desktop.import.fetch') }}</button>
    </div>
    <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.desktop.import.routingHint') }}</p>
    <details class="rounded-lg border border-primary-100 bg-white/60 dark:border-primary-800 dark:bg-dark-800/40">
      <summary class="cursor-pointer px-3 py-2 text-sm font-medium text-gray-700 dark:text-gray-300">{{ t('admin.settings.desktop.import.advanced') }}</summary>
      <div class="grid gap-4 px-3 pb-3 sm:grid-cols-2">
        <div class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
          <label for="desktop-import-account">{{ t('admin.settings.desktop.import.account') }}</label>
          <Select id="desktop-import-account" v-model="accountID" :options="accountOptions" :disabled="!groupID || accountsLoading" :loading="accountsLoading" searchable :aria-label="t('admin.settings.desktop.import.account')" />
        </div>
        <div class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
          <label for="desktop-import-protocol">{{ t('admin.settings.desktop.protocol') }}</label>
          <Select id="desktop-import-protocol" v-model="apiMode" :options="protocolOptions" :aria-label="t('admin.settings.desktop.protocol')" />
        </div>
      </div>
    </details>
    <div v-if="accountsError" role="alert" class="flex flex-wrap items-center gap-3 text-sm text-red-600 dark:text-red-400">
      <span>{{ t('admin.settings.desktop.import.accountsFailed') }}</span>
      <button type="button" class="btn btn-secondary" @click="loadAccounts">{{ t('common.refresh') }}</button>
    </div>
    <p v-else-if="groupID && !accountsLoading && !accounts.length" class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.desktop.import.noAccounts') }}</p>
    <p v-if="fetchError" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ t('admin.settings.desktop.import.fetchFailed') }}</p>
    <template v-if="catalog">
      <p v-if="catalog.failed" data-testid="desktop-import-partial" role="status" class="text-sm text-amber-700 dark:text-amber-400">{{ t('admin.settings.desktop.import.partialFailure', { succeeded: catalog.succeeded, failed: catalog.failed }) }}</p>
      <p v-if="catalog.warnings?.length" role="status" class="text-sm text-amber-700 dark:text-amber-400">{{ t('admin.settings.desktop.import.metadataWarning') }}</p>
      <div class="flex flex-wrap items-center gap-3">
        <input v-model="search" type="search" class="input min-w-0 basis-full sm:basis-64 sm:flex-1" :placeholder="t('admin.settings.desktop.search')" :aria-label="t('admin.settings.desktop.import.search')" />
        <button type="button" class="btn btn-secondary" data-testid="desktop-import-select-all" @click="selectVisible">{{ t('admin.settings.desktop.import.selectAll') }}</button>
        <button type="button" class="btn btn-secondary" @click="selected = new Set()">{{ t('admin.settings.desktop.import.clearSelection') }}</button>
      </div>
      <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.settings.desktop.import.count', { total: candidates.length, selected: selectedDrafts.length, remaining }) }}</p>
      <div class="max-h-80 space-y-1 overflow-y-auto rounded-lg border border-gray-200 bg-white p-2 dark:border-dark-600 dark:bg-dark-800">
        <p v-if="!visibleCandidates.length" class="p-3 text-sm text-gray-500">{{ t('admin.settings.desktop.noMatches') }}</p>
        <label v-for="entry in visibleCandidates" :key="candidateKey(entry)" class="flex items-start gap-3 rounded-md p-3 hover:bg-gray-50 dark:hover:bg-dark-700" :class="{ 'opacity-60': unavailableReason(entry) }">
          <input type="checkbox" class="mt-1 shrink-0" data-testid="desktop-import-choice" :value="candidateKey(entry)" :checked="selected.has(candidateKey(entry)) && !unavailableReason(entry)" :disabled="!!unavailableReason(entry)" @change="toggleSelected(candidateKey(entry))" />
          <span class="min-w-0 flex-1">
            <span class="block break-words text-sm font-medium text-gray-900 dark:text-white">{{ entry.display_name }}</span>
            <span class="block break-all text-xs text-gray-500 dark:text-gray-400">{{ entry.model }}</span>
            <span class="block text-xs text-gray-500 dark:text-gray-400">{{ entry.platform }} · {{ entry.api_mode }}</span>
            <span v-if="unavailableReason(entry)" class="mt-1 block text-xs text-amber-700 dark:text-amber-400">{{ t(unavailableReason(entry)) }}</span>
          </span>
        </label>
      </div>
      <p class="text-sm text-gray-500 dark:text-gray-400">{{ t(purpose === 'image' ? 'admin.settings.desktop.import.imageReviewHint' : 'admin.settings.desktop.import.reviewHint') }}</p>
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
import { list, syncUpstreamModels } from '@/api/admin/accounts'
import type { DesktopModelEntry } from '@/api/admin/settings'
import type { AccountListItem, AdminGroup } from '@/types'
import { discoverDesktopGroupModels, DESKTOP_MODEL_LIMIT, modelAllowedForImport, prepareDesktopModelImport, sameDesktopModel } from './desktopModelImport'

const props = withDefaults(defineProps<{ groups: AdminGroup[]; groupsLoading: boolean; models: DesktopModelEntry[]; purpose?: 'chat' | 'image' }>(), { purpose: 'chat' })
const emit = defineEmits<{ import: [models: DesktopModelEntry[]] }>()
const { t } = useI18n()
const groupID = ref<number | null>(null)
const accountID = ref<number>(0)
const apiMode = ref<DesktopModelEntry['api_mode'] | 'auto'>(props.purpose === 'image' ? 'images_generations' : 'auto')
const accounts = ref<AccountListItem[]>([])
const accountsLoading = ref(false)
const accountsError = ref(false)
const fetching = ref(false)
const fetchError = ref(false)
const catalog = ref<Awaited<ReturnType<typeof discoverDesktopGroupModels>> | null>(null)
const selected = ref(new Set<string>())
const search = ref('')
const importedCount = ref<number | null>(null)
let accountsRequest = 0
let modelsRequest = 0
const group = computed(() => props.groups.find(item => item.id === groupID.value))
const sources = computed(() => accountID.value === 0 ? accounts.value : accounts.value.filter(item => item.id === accountID.value))
const groupOptions = computed(() => props.groups.filter(item => props.purpose !== 'image' || ['openai', 'grok', 'composite'].includes(item.platform)).map(item => ({ value: item.id, label: `${item.name} · ${item.platform}` })))
const accountOptions = computed(() => [{ value: 0, label: t('admin.settings.desktop.import.wholeGroup') }, ...accounts.value.map(item => ({ value: item.id, label: `${item.name} · ${item.platform} · ${item.type}` }))])
const protocolOptions = computed(() => [
  { value: 'auto', label: t('admin.settings.desktop.import.autoProtocol') },
  { value: 'chat_completions', label: 'Chat Completions' },
  { value: 'responses', label: 'Responses' },
  { value: 'anthropic_messages', label: 'Anthropic Messages' },
  { value: 'images_generations', label: 'Images API' }
].filter(option => (option.value === 'images_generations') === (props.purpose === 'image')))
const candidates = computed(() => catalog.value?.models ?? [])
const visibleCandidates = computed(() => candidates.value.filter(entry => `${entry.model} ${entry.display_name}`.toLowerCase().includes(search.value.trim().toLowerCase())))
const selectedDrafts = computed(() => candidates.value.filter(entry => selected.value.has(candidateKey(entry)) && !unavailableReason(entry)))
const remaining = computed(() => Math.max(0, DESKTOP_MODEL_LIMIT - props.models.length))

function candidateKey(entry: DesktopModelEntry): string {
  return JSON.stringify([entry.platform, entry.api_mode, entry.model])
}

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
  accountID.value = 0
  apiMode.value = props.purpose === 'image' ? 'images_generations' : 'auto'
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
    accounts.value = items.filter(item => (selectedGroup.platform === 'composite' || item.platform === selectedGroup.platform) && (props.purpose !== 'image' || ['openai', 'grok'].includes(item.platform)))
  } catch {
    if (request === accountsRequest) accountsError.value = true
  } finally {
    if (request === accountsRequest) accountsLoading.value = false
  }
}

async function fetchModels(): Promise<void> {
  if (!sources.value.length || !group.value || fetching.value || accountsLoading.value || accountsError.value) return
  const request = ++modelsRequest
  const selectedGroup = group.value
  const selectedSources = [...sources.value]
  fetching.value = true
  fetchError.value = false
  catalog.value = null
  selected.value = new Set()
  importedCount.value = null
  try {
    const result = await discoverDesktopGroupModels(selectedGroup, selectedSources, apiMode.value, syncUpstreamModels, () => request === modelsRequest)
    if (request !== modelsRequest) return
    if (!result.succeeded) {
      fetchError.value = true
      return
    }
    catalog.value = result
    selected.value = new Set(candidates.value.filter(entry => !unavailableReason(entry)).slice(0, remaining.value).map(candidateKey))
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
  visibleCandidates.value.filter(entry => !unavailableReason(entry)).forEach(entry => selected.value.add(candidateKey(entry)))
}

function importSelected(): void {
  if (!selectedDrafts.value.length || selectedDrafts.value.length > remaining.value) return
  const additions = prepareDesktopModelImport(props.models, selectedDrafts.value)
  emit('import', additions)
  selected.value = new Set()
  importedCount.value = additions.length
}

watch(groupID, loadAccounts)
watch([accountID, apiMode], resetCatalog)
onBeforeUnmount(() => { accountsRequest++; modelsRequest++ })
</script>
