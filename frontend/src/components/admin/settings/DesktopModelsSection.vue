<template>
  <section class="card" aria-labelledby="desktop-models-title">
    <div class="flex items-start justify-between gap-4 border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <div>
        <h2 id="desktop-models-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.desktop.title') }}</h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.desktop.description') }}</p>
      </div>
      <Toggle :model-value="modelValue.enabled" :aria-label="t('admin.settings.desktop.enabled')" @update:model-value="update({ enabled: $event })" />
    </div>
    <div class="space-y-5 p-6">
      <div class="flex flex-col gap-4 border-b border-gray-100 pb-5 sm:flex-row sm:items-end sm:justify-between dark:border-dark-700">
        <p class="max-w-2xl text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.desktop.separateHint') }}</p>
        <label class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
          <span>{{ t('admin.settings.desktop.credentialTTL') }}</span>
          <input class="input sm:w-52" type="number" min="300" max="3600" step="1" required :value="modelValue.credential_ttl_seconds" @input="update({ credential_ttl_seconds: numberValue($event) })" />
        </label>
      </div>
      <div role="tablist" :aria-label="t('admin.settings.desktop.catalogType')" class="inline-flex gap-1 rounded-xl bg-gray-100 p-1 dark:bg-dark-800">
        <button v-for="purpose in purposes" :id="`desktop-model-purpose-${purpose}`" :key="purpose" type="button" role="tab" :data-testid="`desktop-model-purpose-${purpose}`" :aria-selected="activePurpose === purpose" aria-controls="desktop-model-purpose-panel" class="flex items-center gap-2 rounded-lg px-4 py-2 text-sm font-medium transition-colors" :class="activePurpose === purpose ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-700 dark:text-primary-300' : 'text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-white'" @click="selectPurpose(purpose)">
          {{ t(`admin.settings.desktop.catalog.${purpose}.title`) }}
          <span class="rounded-md bg-gray-100 px-1.5 py-0.5 text-xs text-gray-500 dark:bg-dark-800 dark:text-gray-400">{{ modelValue.models.filter(entry => modelPurpose(entry) === purpose).length }}</span>
        </button>
      </div>
      <div id="desktop-model-purpose-panel" role="tabpanel" :aria-labelledby="`desktop-model-purpose-${activePurpose}`" class="space-y-5">
      <div class="grid gap-4 md:grid-cols-2">
        <div>
          <h3 class="font-medium text-gray-900 dark:text-white">{{ t(`admin.settings.desktop.catalog.${activePurpose}.title`) }}</h3>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t(`admin.settings.desktop.catalog.${activePurpose}.description`) }}</p>
        </div>
        <div v-if="activePurpose === 'chat'" class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
          <label for="desktop-default-model">{{ t('admin.settings.desktop.defaultModel') }}</label>
          <Select id="desktop-default-model" :model-value="modelValue.default_model_id" :options="defaultOptions" :aria-label="t('admin.settings.desktop.defaultModel')" @update:model-value="update({ default_model_id: $event === null ? null : String($event) })" />
        </div>
        <div v-else class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
          <label for="desktop-default-image-model">{{ t('admin.settings.desktop.defaultImageModel') }}</label>
          <Select id="desktop-default-image-model" :model-value="modelValue.default_image_model_id ?? null" :options="defaultImageOptions" :aria-label="t('admin.settings.desktop.defaultImageModel')" @update:model-value="update({ default_image_model_id: $event === null ? null : String($event) })" />
        </div>
      </div>
      <div class="flex flex-wrap items-center gap-3">
        <input v-model="search" type="search" class="input min-w-0 basis-full sm:basis-64 sm:flex-1" data-testid="desktop-model-search" :placeholder="t('admin.settings.desktop.search')" :aria-label="t('admin.settings.desktop.search')" />
        <span class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.desktop.modelCount', { count: activeModels.length }) }}</span>
        <button type="button" class="btn btn-secondary" :disabled="!activeModels.length" @click="expanded = new Set(activeModels.map(item => item.index))">{{ t('admin.settings.desktop.expandAll') }}</button>
        <button type="button" class="btn btn-secondary" data-testid="desktop-model-collapse-all" :disabled="!activeModels.length" @click="expanded = new Set()">{{ t('admin.settings.desktop.collapseAll') }}</button>
      </div>
      <div class="flex flex-wrap gap-3">
        <button type="button" class="btn btn-primary" data-testid="desktop-model-import-open" :aria-expanded="importOpen" aria-controls="desktop-model-import" @click="importOpen = !importOpen">{{ t('admin.settings.desktop.import.open') }}</button>
        <button type="button" class="btn btn-secondary" data-testid="desktop-model-add" :disabled="modelValue.models.length >= 200" @click="add">{{ t(`admin.settings.desktop.catalog.${activePurpose}.add`) }}</button>
      </div>
      <DesktopModelImporter v-if="importOpen" :key="activePurpose" :purpose="activePurpose" :groups="groups" :groups-loading="groupsLoading" :models="modelValue.models" @import="importModels" />
      <div v-if="groupsError" role="alert" class="flex flex-wrap items-center gap-3 text-sm text-red-600 dark:text-red-400">
        <span>{{ t('admin.settings.desktop.groupsFailed') }}</span>
        <button type="button" class="btn btn-secondary" data-testid="desktop-groups-retry" @click="loadGroups">{{ t('common.refresh') }}</button>
      </div>
      <p v-if="!activeModels.length" class="rounded-lg border border-dashed border-gray-200 p-5 text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">{{ t(`admin.settings.desktop.catalog.${activePurpose}.empty`) }}</p>
      <p v-if="activeModels.length && !activeModels.some(item => matchesSearch(item.entry))" class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.desktop.noMatches') }}</p>
      <div v-for="(entry, index) in modelValue.models" v-show="modelPurpose(entry) === activePurpose && matchesSearch(entry)" :key="index" data-testid="desktop-model-card" class="rounded-xl border border-gray-200 p-4 dark:border-dark-600" @invalid.capture="showInvalidModel(index, $event)">
        <div class="flex items-center justify-between gap-3">
          <button type="button" class="flex min-w-0 flex-1 items-center gap-3 rounded-lg text-left focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500" data-testid="desktop-model-toggle" :aria-expanded="expanded.has(index)" :aria-controls="`desktop-model-editor-${index}`" @click="toggleModel(index)">
            <Icon name="chevronDown" size="sm" class="shrink-0 text-gray-400 transition-transform" :class="{ '-rotate-90': !expanded.has(index) }" />
            <span class="min-w-0">
              <span class="block truncate font-medium text-gray-900 dark:text-white">{{ entry.display_name || t('admin.settings.desktop.newModel') }}</span>
              <span class="mt-1 block truncate text-xs text-gray-500 dark:text-gray-400">{{ entry.model || entry.id || '—' }} · {{ groups.find(group => group.id === entry.group_id)?.name || `#${entry.group_id}` }} · {{ entry.api_mode }}</span>
              <span v-if="entry.api_mode === 'images_generations'" class="mt-1 block text-xs text-gray-500">{{ t('admin.settings.desktop.imageModel') }}</span>
              <span v-else class="mt-1 block text-xs" :class="entry.agent_verified ? 'text-emerald-600 dark:text-emerald-400' : 'text-amber-600 dark:text-amber-400'">{{ t(entry.agent_verified ? 'admin.settings.desktop.verifiedShort' : 'admin.settings.desktop.unverifiedShort') }}</span>
            </span>
          </button>
          <button type="button" class="btn btn-secondary shrink-0" data-testid="desktop-model-remove" @click="remove(index)">{{ t('common.delete') }}</button>
        </div>
        <div v-show="expanded.has(index)" :id="`desktop-model-editor-${index}`" class="mt-4 space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700">
          <div class="grid gap-4 md:grid-cols-2">
            <label v-for="field in textFields" :key="field" class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
              <span>{{ t(`admin.settings.desktop.${field}`) }}</span>
              <input class="input" :data-testid="field === 'display_name' ? 'desktop-model-name' : undefined" :value="entry[field]" :maxlength="field === 'id' ? 128 : 256" :pattern="field === 'id' ? '[a-zA-Z0-9][a-zA-Z0-9._-]*' : undefined" required @input="edit(index, { [field]: textValue($event) })" />
            </label>
            <div class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
              <label :for="`desktop-group-${index}`">{{ t('admin.settings.desktop.group') }}</label>
              <Select :id="`desktop-group-${index}`" :model-value="entry.group_id || null" :options="groupOptions(entry)" :disabled="groupsLoading || groupsError" searchable :aria-label="t('admin.settings.desktop.group')" @update:model-value="selectGroup(index, Number($event))" />
            </div>
            <div class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
              <label :for="`desktop-platform-${index}`">{{ t('admin.settings.desktop.platform') }}</label>
              <Select :id="`desktop-platform-${index}`" :model-value="entry.platform" :options="platformOptions(entry)" :disabled="groups.find(group => group.id === entry.group_id)?.platform !== 'composite'" :aria-label="t('admin.settings.desktop.platform')" @update:model-value="edit(index, { platform: $event as DesktopModelEntry['platform'] })" />
            </div>
            <div class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
              <label :for="`desktop-protocol-${index}`">{{ t('admin.settings.desktop.protocol') }}</label>
              <Select :id="`desktop-protocol-${index}`" :model-value="entry.api_mode" :options="protocolOptions(entry)" :aria-label="t('admin.settings.desktop.protocol')" @update:model-value="edit(index, { api_mode: $event as DesktopModelEntry['api_mode'] })" />
            </div>
            <label class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
              <span>{{ t('admin.settings.desktop.sortOrder') }}</span>
              <input class="input" type="number" step="1" :value="entry.sort_order" @input="edit(index, { sort_order: numberValue($event) })" />
            </label>
            <label v-for="field in entry.api_mode === 'images_generations' ? [] : limitFields" :key="field" class="space-y-1 text-sm text-gray-700 dark:text-gray-300">
              <span>{{ t(`admin.settings.desktop.${field}`) }}</span>
              <input class="input" type="number" min="1" step="1" :value="entry[field]" :placeholder="t('admin.settings.desktop.unknown')" @input="edit(index, { [field]: textValue($event) === '' ? null : numberValue($event) })" />
            </label>
          </div>
          <div v-if="entry.api_mode === 'images_generations'" class="space-y-4">
            <p class="rounded-lg bg-primary-50 p-3 text-sm text-primary-800 dark:bg-primary-900/20 dark:text-primary-300">{{ t('admin.settings.desktop.catalog.image.billingHint') }}</p>
            <fieldset class="space-y-2">
              <legend class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.settings.desktop.catalog.image.qualityTitle') }}</legend>
              <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.desktop.catalog.image.qualityHint') }}</p>
              <div class="flex flex-wrap gap-x-6 gap-y-3 text-sm text-gray-700 dark:text-gray-300">
                <label v-for="quality in imageQualities" :key="quality" class="flex items-center gap-2">
                  <input type="checkbox" :data-image-quality="quality" :checked="entry.capabilities.image_quality_options?.includes(quality) ?? false" @change="setImageQuality(index, quality, checkedValue($event))" />
                  {{ t(`admin.settings.desktop.catalog.image.qualityLabels.${quality}`) }}
                </label>
              </div>
            </fieldset>
          </div>
          <div v-else class="flex flex-wrap gap-x-6 gap-y-3 text-sm text-gray-700 dark:text-gray-300">
            <label v-for="capability in capabilityFields" :key="capability" class="flex items-center gap-2">
              <input type="checkbox" :checked="entry.capabilities[capability]" @change="edit(index, { capabilities: { ...entry.capabilities, [capability]: checkedValue($event) } })" />
              {{ t(`admin.settings.desktop.${capability}`) }}
            </label>
          </div>
          <label v-if="entry.api_mode !== 'images_generations'" class="flex items-start gap-2 text-sm text-gray-700 dark:text-gray-300">
            <input type="checkbox" class="mt-1" :checked="entry.agent_verified" @change="edit(index, { agent_verified: checkedValue($event) })" />
            <span>{{ t('admin.settings.desktop.verified') }}</span>
          </label>
        </div>
      </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import DesktopModelImporter from './DesktopModelImporter.vue'
import { getAll } from '@/api/admin/groups'
import type { DesktopModelEntry, DesktopSettings } from '@/api/admin/settings'
import type { AdminGroup } from '@/types'

const props = defineProps<{ modelValue: DesktopSettings }>()
const emit = defineEmits<{ 'update:modelValue': [value: DesktopSettings] }>()
const { t } = useI18n()
const groups = ref<AdminGroup[]>([])
const groupsLoading = ref(false)
const groupsError = ref(false)
const search = ref('')
const expanded = ref(new Set<number>())
const importOpen = ref(false)
const purposes = ['chat', 'image'] as const
type ModelPurpose = typeof purposes[number]
const activePurpose = ref<ModelPurpose>('chat')
const activeModels = computed(() => props.modelValue.models.map((entry, index) => ({ entry, index })).filter(item => modelPurpose(item.entry) === activePurpose.value))
let focusingInvalid = false
const textFields = ['id', 'display_name', 'model', 'provider_label'] as const
const limitFields = ['context_window', 'max_output_tokens'] as const
const imageQualities = ['auto', 'low', 'medium', 'high', 'xhigh', 'max'] as const
const capabilityFields = ['tools', 'vision', 'reasoning'] as const
const platforms: DesktopModelEntry['platform'][] = ['openai', 'anthropic', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go']
function platformOptions(entry: DesktopModelEntry) {
  const allowed = entry.api_mode === 'images_generations' ? platforms.filter(value => value === 'openai' || value === 'grok') : platforms
  return allowed.map(value => ({ value, label: value }))
}
const protocols = [
  { value: 'chat_completions', label: 'Chat Completions' },
  { value: 'responses', label: 'Responses' },
  { value: 'anthropic_messages', label: 'Anthropic Messages' },
  { value: 'images_generations', label: 'Images API' }
]
function protocolOptions(entry: DesktopModelEntry) {
  return protocols.filter(option => (option.value === 'images_generations') === (modelPurpose(entry) === 'image'))
}
function modelPurpose(entry: DesktopModelEntry): ModelPurpose { return entry.api_mode === 'images_generations' ? 'image' : 'chat' }
function selectPurpose(purpose: ModelPurpose): void { activePurpose.value = purpose; search.value = '' }
const defaultOptions = computed(() => [
  { value: null, label: t('admin.settings.desktop.noDefault') },
  ...props.modelValue.models.filter(entry => entry.api_mode !== 'images_generations' && entry.id && entry.agent_verified && entry.capabilities.tools).map(entry => ({ value: entry.id, label: entry.display_name || entry.id }))
])
const defaultImageOptions = computed(() => [
  { value: null, label: t('admin.settings.desktop.noDefault') },
  ...props.modelValue.models.filter(entry => entry.api_mode === 'images_generations' && entry.id && entry.capabilities.image_generation).map(entry => ({ value: entry.id, label: entry.display_name || entry.id }))
])

function update(patch: Partial<DesktopSettings>): void {
  emit('update:modelValue', { ...props.modelValue, ...patch })
}

function edit(index: number, patch: Partial<DesktopModelEntry>): void {
  const previous = props.modelValue.models[index]
  const entry = { ...previous, ...patch }
  if (patch.api_mode) entry.capabilities = { ...entry.capabilities, image_generation: patch.api_mode === 'images_generations' }
  // Changing the upstream selection invalidates its prior end-to-end verification.
  if (['model', 'group_id', 'platform', 'api_mode'].some(key => key in patch && patch[key as keyof DesktopModelEntry] !== previous[key as keyof DesktopModelEntry])) entry.agent_verified = false
  const models = props.modelValue.models.map((model, i) => i === index ? entry : model)
  let defaultID = props.modelValue.default_model_id
  if (defaultID === previous.id) defaultID = entry.api_mode !== 'images_generations' && entry.agent_verified && entry.capabilities.tools ? entry.id || null : null
  let defaultImageID = props.modelValue.default_image_model_id
  if (defaultImageID === previous.id) defaultImageID = entry.api_mode === 'images_generations' && entry.capabilities.image_generation ? entry.id || null : null
  update({ models, default_model_id: defaultID, default_image_model_id: defaultImageID })
}

function setImageQuality(index: number, quality: string, checked: boolean): void {
  const capabilities = props.modelValue.models[index].capabilities
  const previous = capabilities.image_quality_options ?? []
  const options = checked ? [...new Set([...previous, quality])] : previous.filter(value => value !== quality)
  edit(index, { capabilities: { ...capabilities, image_quality_options: options } })
}

function add(): void {
  search.value = ''
  expanded.value.add(props.modelValue.models.length)
  const image = activePurpose.value === 'image'
  update({ models: [...props.modelValue.models, { id: '', group_id: 0, model: '', display_name: '', provider_label: '', platform: 'openai', api_mode: image ? 'images_generations' : 'chat_completions', sort_order: props.modelValue.models.length, agent_verified: false, capabilities: { tools: false, vision: false, reasoning: false, ...(image ? { image_generation: true } : {}) }, context_window: null, max_output_tokens: null }] })
}

function remove(index: number): void {
  expanded.value = new Set([...expanded.value].filter(value => value !== index).map(value => value > index ? value - 1 : value))
  update({ models: props.modelValue.models.filter((_, i) => i !== index), default_model_id: props.modelValue.default_model_id === props.modelValue.models[index].id ? null : props.modelValue.default_model_id, default_image_model_id: props.modelValue.default_image_model_id === props.modelValue.models[index].id ? null : props.modelValue.default_image_model_id })
}

function matchesSearch(entry: DesktopModelEntry): boolean {
  const query = search.value.trim().toLowerCase()
  return !query || [entry.id, entry.display_name, entry.model, entry.provider_label, groups.value.find(group => group.id === entry.group_id)?.name].some(value => value?.toLowerCase().includes(query))
}

function toggleModel(index: number): void {
  if (expanded.value.has(index)) expanded.value.delete(index)
  else expanded.value.add(index)
}

function importModels(models: DesktopModelEntry[]): void {
  search.value = ''
  update({ models: [...props.modelValue.models, ...models] })
}

async function showInvalidModel(index: number, event: Event): Promise<void> {
  event.preventDefault()
  activePurpose.value = modelPurpose(props.modelValue.models[index])
  search.value = ''
  expanded.value.add(index)
  if (focusingInvalid) return
  focusingInvalid = true
  await nextTick()
  ;(event.target as HTMLElement).focus()
  focusingInvalid = false
}

function groupOptions(entry: DesktopModelEntry) {
  const available = entry.api_mode === 'images_generations' ? groups.value.filter(group => ['openai', 'grok', 'composite'].includes(group.platform)) : groups.value
  const options = available.map(group => ({ value: group.id, label: `${group.name} · ${group.platform}` }))
  if (entry.group_id && !groups.value.some(group => group.id === entry.group_id)) options.push({ value: entry.group_id, label: `${t('admin.settings.desktop.unavailableGroup')} #${entry.group_id}` })
  return options
}

function selectGroup(index: number, groupID: number): void {
  const group = groups.value.find(item => item.id === groupID)
  if (!group) return
  edit(index, { group_id: groupID, ...(group.platform !== 'composite' ? { platform: group.platform } : {}) })
}

function textValue(event: Event): string { return (event.target as HTMLInputElement).value }
function numberValue(event: Event): number { return Number(textValue(event)) }
function checkedValue(event: Event): boolean { return (event.target as HTMLInputElement).checked }

async function loadGroups(): Promise<void> {
  groupsLoading.value = true
  groupsError.value = false
  try { groups.value = await getAll() }
  catch { groupsError.value = true }
  finally { groupsLoading.value = false }
}

onMounted(loadGroups)
</script>
