<template>
  <AppLayout>
    <div class="dm-workspace dm-page" :style="{ '--dm-sidebar': appStore.sidebarCollapsed ? '72px' : '256px' }">
      <header class="dm-page-heading">
        <div>
          <span class="dm-eyebrow">{{ text('eyebrow') }}</span>
          <div class="dm-heading-title">
            <h1>{{ text('title') }}</h1>
            <span v-if="settings" class="dm-heading-count">{{ settings.models.length }}</span>
          </div>
          <p>{{ text('description') }}</p>
        </div>
        <div class="dm-heading-actions">
          <button
            class="dm-button dm-secondary"
            :disabled="!settings || saving"
            data-testid="preview-open"
            @click="previewOpen = true"
          >
            <Icon name="eye" />{{ text('preview') }}
          </button>
          <button
            class="dm-button dm-primary"
            :disabled="!canEdit || !settings || settings.models.length >= DESKTOP_MODEL_LIMIT"
            data-testid="model-add"
            @click="openEditor()"
          >
            <Icon name="plus" />{{ text('addModel') }}
          </button>
        </div>
      </header>

      <div
        v-if="loading"
        class="dm-load-state"
        role="status"
      >
        <Icon name="refresh" class="animate-spin" />{{ text('loading') }}
      </div>
      <div
        v-else-if="loadError"
        class="dm-load-state dm-error-state"
        role="alert"
      >
        <Icon name="exclamationTriangle" />
        <p>{{ text('loadFailed') }}</p>
        <button class="dm-button dm-secondary" @click="loadCatalog">
          {{ text('retry') }}
        </button>
      </div>

      <template v-if="settings && !loading && !loadError">
        <fieldset :disabled="saving" class="dm-controls">
          <div class="dm-connection-band">
            <div class="dm-connection-identity">
              <img
                src="/aino-app-icon.png"
                alt=""
                width="44"
                height="44"
              />
              <div>
                <h2>{{ text('connectionTitle') }}<span class="dm-small-status" :class="{ 'is-enabled': settings.enabled }">
                  <i />{{ text(settings.enabled ? 'enabled' : 'disabled') }}</span>
                </h2>
                <p>{{ text('connectionHint') }}</p>
              </div>
            </div>
            <div class="dm-connection-actions">
              <button
                class="dm-text-button"
                data-testid="connection-open"
                @click="openConnection"
              >
                <Icon name="cog" />{{ text('connectionSettings') }}
              </button>
              <span class="dm-vertical-rule" />
              <span class="dm-switch-caption">{{ text('catalogSwitch') }}</span>
              <button
                type="button"
                role="switch"
                class="dm-switch"
                :aria-checked="settings.enabled"
                :aria-label="t('admin.settings.desktop.enabled')"
                data-testid="catalog-toggle"
                @click="settings.enabled = !settings.enabled"
              >
                <span />
              </button>
            </div>
          </div>
          <div
            v-if="groupsError"
            class="dm-inline-error"
            role="alert"
          >
            <span>{{ t('admin.settings.desktop.groupsFailed') }}</span>
            <button
              class="dm-button dm-secondary"
              data-testid="groups-retry"
              @click="loadGroups"
            >
              {{ text('retry') }}
            </button>
          </div>

          <div class="dm-catalog">
            <div class="dm-tabs-row">
              <div
                role="tablist"
                :aria-label="t('admin.settings.desktop.catalogType')"
                class="dm-tabs"
              >
                <button
                  v-for="item in purposes"
                  :id="`dm-tab-${item}`"
                  :key="item"
                  role="tab"
                  :aria-selected="purpose === item"
                  :tabindex="purpose === item ? 0 : -1"
                  aria-controls="dm-model-panel"
                  :data-testid="`tab-${item}`"
                  @click="changePurpose(item)"
                  @keydown="tabKeydown($event, item)"
                >
                  <Icon :name="item === 'chat' ? 'chat' : 'grid'" />{{ t(`admin.settings.desktop.catalog.${item}.title`) }}<span>{{ settings.models.filter(model => purposeOf(model) === item).length }}</span>
                </button>
              </div>
              <label class="dm-default-picker">
                <span>{{ t(purpose === 'chat' ? 'admin.settings.desktop.defaultModel' : 'admin.settings.desktop.defaultImageModel') }}</span>
                <select
                  v-model="currentDefault"
                  :disabled="!canEdit"
                  data-testid="default-select"
                >
                  <option value="">{{ t('admin.settings.desktop.noDefault') }}</option>
                  <option
                    v-if="currentDefault && !defaultOptions.some(model => model.id === currentDefault)"
                    :value="currentDefault"
                    disabled
                  >{{ defaultName }} · {{ text('unavailable') }}</option>
                  <option
                    v-for="model in defaultOptions"
                    :key="model.id"
                    :value="model.id"
                  >{{ model.display_name }}</option>
                </select>
              </label>
            </div>
            <div
              id="dm-model-panel"
              role="tabpanel"
              :aria-labelledby="`dm-tab-${purpose}`"
            >
              <div class="dm-toolbar">
                <div class="dm-filters">
                  <label class="dm-search">
                    <Icon name="search" />
                    <input
                      ref="searchInput"
                      v-model="search"
                      type="search"
                      data-testid="model-search"
                      :placeholder="t('admin.settings.desktop.search')"
                      :aria-label="t('admin.settings.desktop.search')"
                    />
                    <kbd aria-hidden="true">/</kbd>
                  </label>
                  <label class="dm-filter">
                    <span class="sr-only">{{ t('admin.settings.desktop.group') }}</span>
                    <select v-model="groupFilter" data-testid="group-filter">
                      <option value="all">{{ text('allGroups') }}</option>
                      <option
                        v-for="group in groups"
                        :key="group.id"
                        :value="String(group.id)"
                      >{{ group.name }}</option>
                    </select>
                  </label>
                  <label v-if="purpose === 'chat'" class="dm-filter">
                    <span class="sr-only">{{ text('verification') }}</span>
                    <select v-model="statusFilter" data-testid="status-filter">
                      <option value="all">{{ text('allStatuses') }}</option>
                      <option value="verified">{{ t('admin.settings.desktop.verifiedShort') }}</option>
                      <option value="pending">{{ t('admin.settings.desktop.unverifiedShort') }}</option>
                    </select>
                  </label>
                </div>
                <button
                  class="dm-button dm-secondary dm-import-button"
                  :disabled="!canEdit || settings.models.length >= DESKTOP_MODEL_LIMIT"
                  :aria-label="text('importModels')"
                  data-testid="import-open"
                  @click="importOpen = true"
                >
                  <Icon name="download" />
                  <span>{{ text('importModels') }}</span>
                </button>
              </div>
              <div v-if="selected.size" class="dm-selection-bar">
                <span>{{ text('selectedCount', { count: selected.size }) }}</span>
                <button class="dm-text-button" @click="selected = new Set()">
                  {{ text('clearSelection') }}
                </button>
                <button
                  class="dm-text-button dm-danger-text"
                  data-testid="remove-selected"
                  @click="removeIDs = [...selected]"
                >
                  <Icon name="trash" />{{ text('removeSelected') }}
                </button>
              </div>
              <div class="dm-table-scroll">
                <table class="dm-model-table">
                  <thead>
                    <tr>
                      <th class="dm-selection-col">
                        <input
                          type="checkbox"
                          :checked="allSelected"
                          :indeterminate="selected.size > 0 && !allSelected"
                          :disabled="!filteredModels.length"
                          :aria-label="text('selectVisible')"
                          @change="toggleAll"
                        />
                      </th>
                      <th class="dm-name-col" scope="col">
                        {{ text('modelColumn') }}
                      </th>
                      <th class="dm-group-col" scope="col">
                        {{ text('routingColumn') }}
                      </th>
                      <th class="dm-capabilities-col" scope="col">
                        {{ text('capabilities') }}
                      </th>
                      <th class="dm-verification-col" scope="col">
                        {{ text('verification') }}
                      </th>
                      <th class="dm-actions-col" scope="col">
                        <span class="sr-only">{{ text('actions') }}</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    <tr
                      v-for="model in filteredModels"
                      :key="model.id"
                      :class="{ 'is-selected': selected.has(model.id) }"
                      data-testid="model-row"
                    >
                      <td class="dm-selection-col">
                        <input
                          type="checkbox"
                          :checked="selected.has(model.id)"
                          :aria-label="text('selectModel', { name: model.display_name })"
                          @change="toggleSelected(model.id)"
                        />
                      </td>
                      <td class="dm-name-col">
                        <div class="dm-model-identity">
                          <CatalogProviderIcon :entry="model" />
                          <div class="dm-model-details">
                            <div>
                              <button
                                class="dm-model-name"
                                :disabled="!canEdit"
                                @click="openEditor(model)"
                              >
                                {{ model.display_name }}
                              </button>
                              <span v-if="model.id === currentDefault" class="dm-default-tag">
                                <Icon name="star" />{{ text('default') }}</span>
                            </div>
                            <code>{{ model.model }}</code>
                          </div>
                        </div>
                      </td>
                      <td class="dm-group-col">
                        <span>{{ groupName(model.group_id) }}</span>
                        <small>{{ protocolLabels[model.api_mode] }}</small>
                      </td>
                      <td class="dm-capabilities-col">
                        <div class="dm-capability-list">
                          <template v-if="purpose === 'image'">
                            <span>{{ text('imageGeneration') }}</span>
                            <span v-for="quality in model.capabilities.image_quality_options" :key="quality">{{ t(`admin.settings.desktop.catalog.image.qualityLabels.${quality}`) }}</span>
                          </template>
                          <template v-else>
                            <span v-for="capability in capabilityFields.filter(key => model.capabilities[key])" :key="capability">{{ text(capability) }}</span>
                            <span v-if="!capabilityFields.some(key => model.capabilities[key])" class="dm-muted">—</span>
                          </template>
                        </div>
                      </td>
                      <td class="dm-verification-col">
                        <span v-if="purpose === 'image'" class="dm-muted">{{ text('notRequired') }}</span>
                        <span
                          v-else
                          class="dm-verification-badge"
                          :class="{ 'is-verified': model.agent_verified }"
                        >
                          <Icon :name="model.agent_verified ? 'checkCircle' : 'clock'" />{{ t(model.agent_verified ? 'admin.settings.desktop.verifiedShort' : 'admin.settings.desktop.unverifiedShort') }}</span>
                      </td>
                      <td class="dm-actions-col">
                        <div class="dm-row-actions">
                          <button
                            class="dm-icon-button dm-default-action"
                            :disabled="!canEdit || !canDefault(model, groups)"
                            :aria-pressed="model.id === currentDefault"
                            :title="text('setDefault')"
                            :aria-label="text('setModelDefault', { name: model.display_name })"
                            @click="currentDefault = model.id"
                          >
                            <Icon name="star" />
                          </button>
                          <button
                            class="dm-icon-button"
                            :disabled="!canEdit"
                            :title="text('editModel')"
                            :aria-label="text('editNamed', { name: model.display_name })"
                            data-testid="model-edit"
                            @click="openEditor(model)"
                          >
                            <Icon name="edit" />
                          </button>
                          <button
                            class="dm-icon-button dm-remove-action"
                            :title="text('removeModel')"
                            :aria-label="text('removeNamed', { name: model.display_name })"
                            data-testid="model-remove"
                            @click="removeIDs = [model.id]"
                          >
                            <Icon name="trash" />
                          </button>
                        </div>
                      </td>
                    </tr>
                  </tbody>
                </table>
                <div v-if="!filteredModels.length" class="dm-empty">
                  <Icon name="grid" />
                  <h3>{{ activeModels.length ? t('admin.settings.desktop.noMatches') : text('emptyTitle') }}</h3>
                  <p>{{ activeModels.length ? text('emptySearch') : t(`admin.settings.desktop.catalog.${purpose}.empty`) }}</p>
                  <button
                    v-if="activeModels.length"
                    class="dm-button dm-secondary"
                    @click="resetFilters"
                  >
                    {{ text('resetFilters') }}
                  </button>
                </div>
              </div>
              <footer class="dm-table-footer">
                <span>{{ text('resultCount', { shown: filteredModels.length, total: activeModels.length }) }}</span>
                <span>
                  <Icon name="arrowsUpDown" />{{ text('sortHint') }}</span>
              </footer>
            </div>
          </div>
          <div class="dm-guidance">
            <Icon name="shield" />
            <p>{{ text('pricingHint') }}<span>{{ text(purpose === 'chat' ? 'defaultHint' : 'imageHint') }}</span>
            </p>
            <button class="dm-text-button" @click="helpOpen = true">
              {{ text('help') }}<Icon name="arrowRight" />
            </button>
          </div>
        </fieldset>
        <Transition name="dm-savebar">
          <div
            v-if="dirty"
            class="dm-savebar"
            role="status"
          >
            <div>
              <span class="dm-unsaved-dot" />
              <span>{{ text('unsaved') }}<small>{{ text('saveHint') }}</small>
              </span>
            </div>
            <div>
              <button
                class="dm-button dm-secondary"
                :disabled="saving"
                @click="discardOpen = true"
              >
                {{ text('discard') }}
              </button>
              <button
                class="dm-button dm-primary"
                :disabled="saving || !canEdit"
                data-testid="save-catalog"
                @click="saveCatalog"
              >
                <Icon :name="saving ? 'refresh' : 'check'" :class="{ 'animate-spin': saving }" />{{ text(saving ? 'saving' : 'save') }}
              </button>
            </div>
            <p
              v-if="saveError"
              class="dm-save-error"
              role="alert"
            >
              {{ saveError }}
            </p>
          </div>
        </Transition>
      </template>
      <Transition name="dm-toast">
        <div
          v-if="notification"
          class="dm-toast"
          role="status"
        >
          <Icon name="checkCircle" />{{ notification }}
        </div>
      </Transition>

      <template v-if="settings">
        <CatalogModelEditor
          v-if="editor"
          :initial="editor.entry"
          :previous="editor.previous"
          :models="settings.models"
          :groups="groups"
          @apply="applyEntry"
          @close="closeEditor"
          @dirty="editorDirty = $event"
        />
        <CatalogDialog
          :open="importOpen"
          drawer
          :label="text('importModels')"
          @close="importOpen = false"
        >
          <header class="dm-panel-header">
            <div>
              <span class="dm-eyebrow">{{ t(`admin.settings.desktop.catalog.${purpose}.title`) }}</span>
              <h2>{{ text('importModels') }}</h2>
              <p>{{ text('importHint') }}</p>
            </div>
            <button
              class="dm-icon-button"
              :aria-label="t('common.close')"
              @click="importOpen = false"
            >
              <Icon name="x" />
            </button>
          </header>
          <div class="dm-panel-scroll dm-import-content">
            <DesktopModelImporter
              v-if="importOpen"
              :groups="groups"
              :groups-loading="groupsLoading"
              :models="settings.models"
              :purpose="purpose"
              @import="importModels"
            />
          </div>
          <footer class="dm-panel-footer">
            <span>{{ text('applyHint') }}</span>
            <button class="dm-button dm-secondary" @click="importOpen = false">
              {{ text('finishImport') }}
            </button>
          </footer>
        </CatalogDialog>
        <CatalogDesktopPreview
          v-if="previewOpen"
          :settings="settings"
          :purpose="purpose"
          @close="previewOpen = false"
        />
        <CatalogDialog
          :open="connectionOpen"
          :label="text('connectionSettings')"
          @close="closeConnection"
        >
          <header class="dm-panel-header">
            <div>
              <span class="dm-eyebrow">Aino</span>
              <h2>{{ text('connectionSettings') }}</h2>
              <p>{{ text('ttlDescription') }}</p>
            </div>
            <button
              class="dm-icon-button"
              :aria-label="t('common.close')"
              @click="closeConnection"
            >
              <Icon name="x" />
            </button>
          </header>
          <form class="dm-connection-form" @submit.prevent="applyConnection">
            <CatalogField
              id="dm-ttl"
              :label="t('admin.settings.desktop.credentialTTL')"
              :hint="text('ttlHint')"
            >
              <input
                id="dm-ttl"
                v-model.number="connectionTTL"
                type="number"
                min="300"
                max="3600"
                step="1"
                required
                data-testid="connection-ttl"
                aria-describedby="dm-ttl-hint"
              />
            </CatalogField>
            <p class="dm-note">
              <Icon name="shield" />{{ text('permissionHint') }}
            </p>
            <footer class="dm-dialog-actions">
              <button
                type="button"
                class="dm-button dm-secondary"
                @click="closeConnection"
              >
                {{ t('common.cancel') }}
              </button>
              <button class="dm-button dm-primary" data-testid="connection-apply">
                {{ text('applyConnection') }}
              </button>
            </footer>
          </form>
        </CatalogDialog>
        <CatalogDialog
          :open="removeIDs.length > 0"
          :label="text('removeTitle', { count: removeIDs.length })"
          @close="removeIDs = []"
        >
          <header class="dm-panel-header">
            <div>
              <h2>{{ text('removeTitle', { count: removeIDs.length }) }}</h2>
              <p>{{ text('removeHint') }}</p>
            </div>
            <button
              class="dm-icon-button"
              :aria-label="t('common.close')"
              @click="removeIDs = []"
            >
              <Icon name="x" />
            </button>
          </header>
          <footer class="dm-panel-footer">
            <button class="dm-button dm-secondary" @click="removeIDs = []">
              {{ t('common.cancel') }}
            </button>
            <button
              class="dm-button dm-danger"
              data-testid="remove-confirm"
              @click="removeModels"
            >
              {{ text('confirmRemove') }}
            </button>
          </footer>
        </CatalogDialog>
        <CatalogDialog
          :open="discardOpen"
          :label="text('discardTitle')"
          @close="discardOpen = false"
        >
          <header class="dm-panel-header">
            <div>
              <h2>{{ text('discardTitle') }}</h2>
              <p>{{ text('discardHint') }}</p>
            </div>
          </header>
          <footer class="dm-panel-footer">
            <button class="dm-button dm-secondary" @click="discardOpen = false">
              {{ text('continueEditing') }}
            </button>
            <button
              class="dm-button dm-primary"
              data-testid="discard-confirm"
              @click="discardChanges"
            >
              {{ text('discard') }}
            </button>
          </footer>
        </CatalogDialog>
        <CatalogDialog
          :open="leaveOpen"
          :label="text('leaveTitle')"
          @close="finishLeave(false)"
        >
          <header class="dm-panel-header">
            <div>
              <h2>{{ text('leaveTitle') }}</h2>
              <p>{{ text('leaveHint') }}</p>
            </div>
          </header>
          <footer class="dm-panel-footer">
            <button class="dm-button dm-secondary" @click="finishLeave(false)">
              {{ text('continueEditing') }}
            </button>
            <button
              class="dm-button dm-danger"
              data-testid="leave-confirm"
              @click="finishLeave(true)"
            >
              {{ text('leave') }}
            </button>
          </footer>
        </CatalogDialog>
        <CatalogDialog
          :open="helpOpen"
          :label="text('helpTitle')"
          @close="helpOpen = false"
        >
          <header class="dm-panel-header">
            <div>
              <h2>{{ text('helpTitle') }}</h2>
            </div>
            <button
              class="dm-icon-button"
              :aria-label="t('common.close')"
              @click="helpOpen = false"
            >
              <Icon name="x" />
            </button>
          </header>
          <div class="dm-help-content">
            <div v-for="topic in ['catalog', 'verification', 'billing', 'credentials']" :key="topic">
              <h3>{{ text(`helpTopics.${topic}.title`) }}</h3>
              <p>{{ text(`helpTopics.${topic}.body`) }}</p>
            </div>
          </div>
        </CatalogDialog>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { getDesktopSettings, updateDesktopSettings, type DesktopModelEntry, type DesktopSettings } from '@/api/admin/settings'
import { getAllIncludingInactive } from '@/api/admin/groups'
import type { AdminGroup } from '@/types'
import { useAppStore } from '@/stores/app'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import CatalogDialog from '@/components/admin/desktop-models/CatalogDialog.vue'
import CatalogField from '@/components/admin/desktop-models/CatalogField.vue'
import CatalogModelEditor from '@/components/admin/desktop-models/CatalogModelEditor.vue'
import CatalogDesktopPreview from '@/components/admin/desktop-models/CatalogDesktopPreview.vue'
import CatalogProviderIcon from '@/components/admin/desktop-models/CatalogProviderIcon.vue'
import DesktopModelImporter from '@/components/admin/settings/DesktopModelImporter.vue'
import { DESKTOP_MODEL_LIMIT, prepareDesktopModelImport } from '@/components/admin/settings/desktopModelImport'
import { canDefault, clone, newEntry, normalizeDefaults, protocolLabels, purposeOf, purposes, replaceEntry, validateEntry, type ModelPurpose } from '@/components/admin/desktop-models/catalog'
import '@/styles/desktop-models.css'

const { t } = useI18n()
const text = (key: string, params: Record<string, string | number> = {}) => t(`admin.desktopModels.${key}`, params)
const appStore = useAppStore()
const settings = ref<DesktopSettings | null>(null)
const saved = ref<DesktopSettings | null>(null)
const loading = ref(true)
const loadError = ref(false)
const groups = ref<AdminGroup[]>([])
const groupsLoading = ref(true)
const groupsError = ref(false)
const saving = ref(false)
const saveError = ref('')
const canEdit = computed(() => !!settings.value && !loading.value && !saving.value && !groupsLoading.value && !groupsError.value)
const dirty = computed(() => !!settings.value && JSON.stringify(settings.value) !== JSON.stringify(saved.value))
const purpose = ref<ModelPurpose>('chat')
const search = ref('')
const searchInput = ref<HTMLInputElement>()
const groupFilter = ref('all')
const statusFilter = ref('all')
const selected = ref(new Set<string>())
const capabilityFields = ['tools', 'vision', 'reasoning'] as const
const activeModels = computed(() => (settings.value?.models ?? []).filter(model => purposeOf(model) === purpose.value).sort((a, b) => a.sort_order - b.sort_order))
const filteredModels = computed(() => activeModels.value.filter(model => {
  const query = search.value.trim().toLowerCase()
  return (!query || [model.id, model.model, model.display_name, model.provider_label, groupName(model.group_id)].some(value => value.toLowerCase().includes(query)))
    && (groupFilter.value === 'all' || model.group_id === Number(groupFilter.value))
    && (purpose.value === 'image' || statusFilter.value === 'all' || (model.agent_verified ? 'verified' : 'pending') === statusFilter.value)
}))
const defaultOptions = computed(() => activeModels.value.filter(model => canDefault(model, groups.value)))
const currentDefault = computed({
  get: () => (purpose.value === 'chat' ? settings.value?.default_model_id : settings.value?.default_image_model_id) || '',
  set: (value: string) => {
    if (!settings.value || saving.value) return
    if (purpose.value === 'chat') settings.value.default_model_id = value || null
    else settings.value.default_image_model_id = value || null
  }
})
const defaultName = computed(() => settings.value?.models.find(model => model.id === currentDefault.value)?.display_name || currentDefault.value)
const allSelected = computed(() => filteredModels.value.length > 0 && filteredModels.value.every(model => selected.value.has(model.id)))
const editor = ref<{ entry: DesktopModelEntry; previous?: DesktopModelEntry } | null>(null)
const editorDirty = ref(false)
const importOpen = ref(false)
const previewOpen = ref(false)
const connectionOpen = ref(false)
const connectionTTL = ref<number | string>(3600)
const connectionDirty = computed(() => connectionOpen.value && connectionTTL.value !== settings.value?.credential_ttl_seconds)
const removeIDs = ref<string[]>([])
const discardOpen = ref(false)
const leaveOpen = ref(false)
const helpOpen = ref(false)
const notification = ref('')
let notificationTimer: ReturnType<typeof setTimeout> | undefined
let resolveLeave: ((value: boolean) => void) | undefined
let mounted = true
let groupRequest = 0
let catalogRequest = 0

function groupName(id: number): string {
  const group = groups.value.find(group => group.id === id)
  return group ? `${group.name}${group.status === 'active' ? '' : ` · ${text('inactive')}`}` : `#${id}`
}
async function loadCatalog(): Promise<void> {
  const request = ++catalogRequest
  loading.value = true; loadError.value = false
  try {
    const data = await getDesktopSettings()
    if (!mounted || request !== catalogRequest) return
    if (!Array.isArray(data?.models)) throw new Error('Invalid catalog response')
    settings.value = clone(data); saved.value = clone(data)
  } catch { if (mounted && request === catalogRequest) loadError.value = true }
  finally { if (mounted && request === catalogRequest) loading.value = false }
}
async function loadGroups(): Promise<void> {
  const request = ++groupRequest
  groupsLoading.value = true; groupsError.value = false
  try {
    const data = await getAllIncludingInactive()
    if (mounted && request === groupRequest) groups.value = data
  } catch { if (mounted && request === groupRequest) groupsError.value = true }
  finally { if (mounted && request === groupRequest) groupsLoading.value = false }
}
async function saveCatalog(): Promise<void> {
  if (!settings.value || !canEdit.value || !dirty.value) return
  const invalid = settings.value.models.find(model => Object.keys(validateEntry(model, settings.value!.models, model.id, groups.value)).length)
  if (invalid) { purpose.value = purposeOf(invalid); openEditor(invalid); return }
  if (settings.value.models.some(model => (model.id === settings.value?.default_model_id || model.id === settings.value?.default_image_model_id) && !canDefault(model, groups.value))) { saveError.value = text('defaultUnavailable'); return }
  saving.value = true; saveError.value = ''
  try {
    const data = await updateDesktopSettings(clone(settings.value))
    if (!mounted) return
    settings.value = clone(data); saved.value = clone(data); notify(text('saved'))
  } catch (error) {
    if (mounted) {
      const detail = error as { message?: string; response?: { data?: { message?: string } } }
      const message = detail?.response?.data?.message || detail?.message
      saveError.value = `${text('saveFailed')}${message ? ` ${message}` : ''}`
    }
  } finally { if (mounted) saving.value = false }
}
function resetFilters(): void { search.value = ''; groupFilter.value = 'all'; statusFilter.value = 'all'; selected.value = new Set() }
function changePurpose(value: ModelPurpose): void { purpose.value = value; resetFilters() }
async function tabKeydown(event: KeyboardEvent, value: ModelPurpose): Promise<void> {
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const next = event.key === 'Home' ? 'chat' : event.key === 'End' ? 'image' : value === 'chat' ? 'image' : 'chat'
  changePurpose(next); await nextTick(); document.getElementById(`dm-tab-${next}`)?.focus()
}
function toggleSelected(id: string): void { const next = new Set(selected.value); if (next.has(id)) next.delete(id); else next.add(id); selected.value = next }
function toggleAll(): void {
  const next = new Set(selected.value)
  for (const model of filteredModels.value) { if (allSelected.value) next.delete(model.id); else next.add(model.id) }
  selected.value = next
}
function openEditor(model?: DesktopModelEntry): void {
  if (!canEdit.value || !settings.value || (!model && settings.value.models.length >= DESKTOP_MODEL_LIMIT)) return
  editorDirty.value = false; editor.value = { entry: clone(model ?? newEntry(purpose.value, settings.value.models)), previous: model ? clone(model) : undefined }
}
function closeEditor(): void { editor.value = null; editorDirty.value = false }
function applyEntry(entry: DesktopModelEntry): void {
  if (!settings.value || !editor.value) return
  settings.value = replaceEntry(settings.value, editor.value.previous?.id ?? null, entry)
  closeEditor(); notify(text('applied'))
}
function importModels(entries: DesktopModelEntry[]): void {
  if (!settings.value) return
  const additions = prepareDesktopModelImport(settings.value.models, entries)
  settings.value.models.push(...additions); resetFilters(); notify(text('imported', { count: additions.length }))
}
function removeModels(): void {
  if (!settings.value) return
  settings.value = normalizeDefaults({ ...settings.value, models: settings.value.models.filter(model => !removeIDs.value.includes(model.id)) })
  selected.value = new Set([...selected.value].filter(id => !removeIDs.value.includes(id))); removeIDs.value = []
}
function discardChanges(): void { if (saved.value) settings.value = clone(saved.value); resetFilters(); discardOpen.value = false; saveError.value = '' }
function openConnection(): void { connectionTTL.value = settings.value?.credential_ttl_seconds ?? 3600; connectionOpen.value = true }
function closeConnection(): void { connectionOpen.value = false }
function applyConnection(): void {
  const ttl = Number(connectionTTL.value)
  if (!settings.value || !Number.isInteger(ttl) || ttl < 300 || ttl > 3600) return
  settings.value.credential_ttl_seconds = ttl; connectionOpen.value = false
}
function notify(message: string): void { notification.value = message; clearTimeout(notificationTimer); notificationTimer = setTimeout(() => { notification.value = '' }, 3500) }
function hasUnsaved(): boolean { return dirty.value || editorDirty.value || connectionDirty.value }
function beforeUnload(event: BeforeUnloadEvent): void { if (hasUnsaved() || saving.value) { event.preventDefault(); event.returnValue = '' } }
function shortcut(event: KeyboardEvent): void {
  if (event.key !== '/' || event.metaKey || event.ctrlKey || event.altKey || document.querySelector('dialog[open]') || (event.target as HTMLElement)?.closest('input, textarea, select, [contenteditable="true"]')) return
  event.preventDefault(); searchInput.value?.focus()
}
function finishLeave(value: boolean): void { leaveOpen.value = false; resolveLeave?.(value); resolveLeave = undefined }
onBeforeRouteLeave(() => {
  if (saving.value) return false
  if (!hasUnsaved()) return true
  return new Promise<boolean>(resolve => { resolveLeave = resolve; leaveOpen.value = true })
})
onMounted(() => { void loadCatalog(); void loadGroups(); window.addEventListener('beforeunload', beforeUnload); window.addEventListener('keydown', shortcut) })
onBeforeUnmount(() => { mounted = false; catalogRequest++; groupRequest++; clearTimeout(notificationTimer); resolveLeave?.(false); window.removeEventListener('beforeunload', beforeUnload); window.removeEventListener('keydown', shortcut) })
</script>
