<template>
  <CatalogDialog
    open
    drawer
    :label="text(previous ? 'editModel' : 'addModel')"
    @close="requestClose"
  >
    <header class="dm-panel-header">
      <div>
        <span class="dm-eyebrow">{{ t(`admin.settings.desktop.catalog.${purposeOf(entry)}.title`) }}</span>
        <h2>{{ text(previous ? 'editModel' : 'addModel') }}</h2>
        <p>{{ previous?.display_name || text('addHint') }}</p>
      </div>
      <button
        type="button"
        class="dm-icon-button"
        :aria-label="t('common.close')"
        @click="requestClose"
      >
        <Icon name="x" />
      </button>
    </header>
    <form
      ref="form"
      class="dm-panel-form"
      novalidate
      @submit.prevent="apply"
    >
      <div class="dm-panel-scroll">
        <h3 class="dm-section-title">
          <span>01</span>{{ text('identity') }}
        </h3>
        <CatalogField
          v-for="field in identityFields"
          :id="`dm-${field}`"
          :key="field"
          :label="t(`admin.settings.desktop.${field}`)"
          required
          :error="errors[field]"
          :hint="field === 'model' ? text('modelHint') : undefined"
        >
          <input
            :id="`dm-${field}`"
            v-model="entry[field]"
            :data-testid="`editor-${field}`"
            :class="{ 'dm-mono': field === 'model' }"
            autocomplete="off"
            :aria-invalid="!!errors[field]"
            :aria-describedby="errors[field] || field === 'model' ? `dm-${field}-hint` : undefined"
            @input="invalidateVerification"
          />
        </CatalogField>
        <div class="dm-field-grid">
          <CatalogField
            id="dm-id"
            :label="text('catalogID')"
            required
            :error="errors.id"
            :hint="text(previous ? 'idLocked' : 'idHint')"
          >
            <input
              id="dm-id"
              v-model="entry.id"
              class="dm-mono"
              :readonly="!!previous"
              :aria-invalid="!!errors.id"
              aria-describedby="dm-id-hint"
              autocomplete="off"
              maxlength="128"
            />
          </CatalogField>
          <CatalogField
            id="dm-provider"
            :label="t('admin.settings.desktop.provider_label')"
            required
            :error="errors.provider_label"
          >
            <input
              id="dm-provider"
              v-model="entry.provider_label"
              :aria-invalid="!!errors.provider_label"
              :aria-describedby="errors.provider_label ? 'dm-provider-hint' : undefined"
              autocomplete="off"
            />
          </CatalogField>
        </div>

        <h3 class="dm-section-title">
          <span>02</span>{{ text('routing') }}
        </h3>
        <CatalogField
          id="dm-group"
          :label="t('admin.settings.desktop.group')"
          required
          :error="errors.group_id"
          :hint="text('groupHint')"
        >
          <select
            id="dm-group"
            :value="entry.group_id"
            data-testid="editor-group"
            :aria-invalid="!!errors.group_id"
            aria-describedby="dm-group-hint"
            @change="selectGroup"
          >
            <option disabled :value="0">
              {{ text('chooseGroup') }}
            </option>
            <option v-if="entry.group_id && !groups.some(group => group.id === entry.group_id)" :value="entry.group_id">
              #{{ entry.group_id }} · {{ text('missingGroup') }}
            </option>
            <option
              v-for="group in availableGroups"
              :key="group.id"
              :value="group.id"
            >
              {{ group.name }} · {{ group.platform }}{{ group.status !== 'active' ? ` · ${text('inactive')}` : '' }}
            </option>
          </select>
        </CatalogField>
        <div class="dm-field-grid">
          <CatalogField
            id="dm-platform"
            :label="t('admin.settings.desktop.platform')"
            :error="errors.platform"
          >
            <select
              id="dm-platform"
              v-model="entry.platform"
              data-testid="editor-platform"
              :disabled="selectedGroup?.platform !== 'composite'"
              :aria-invalid="!!errors.platform"
              :aria-describedby="errors.platform ? 'dm-platform-hint' : undefined"
              @change="invalidateVerification"
            >
              <option
                v-for="platform in availablePlatforms"
                :key="platform"
                :value="platform"
              >
                {{ platform }}
              </option>
            </select>
          </CatalogField>
          <CatalogField
            id="dm-protocol"
            :label="t('admin.settings.desktop.protocol')"
            :error="errors.api_mode"
          >
            <select
              id="dm-protocol"
              v-model="entry.api_mode"
              data-testid="editor-protocol"
              :disabled="image"
              :aria-invalid="!!errors.api_mode"
              :aria-describedby="errors.api_mode ? 'dm-protocol-hint' : undefined"
              @change="invalidateVerification"
            >
              <option
                v-for="protocol in availableProtocols"
                :key="protocol"
                :value="protocol"
              >
                {{ protocolLabels[protocol] }}
              </option>
            </select>
          </CatalogField>
        </div>

        <h3 class="dm-section-title">
          <span>03</span>{{ text(image ? 'imageCapabilities' : 'capabilities') }}
        </h3>
        <template v-if="image">
          <p class="dm-note">
            {{ t('admin.settings.desktop.catalog.image.billingHint') }}
          </p>
          <fieldset class="dm-quality-options">
            <legend>{{ t('admin.settings.desktop.catalog.image.qualityTitle') }}</legend>
            <p>{{ t('admin.settings.desktop.catalog.image.qualityHint') }}</p>
            <div>
              <label v-for="quality in imageQualities" :key="quality">
                <input
                  v-model="entry.capabilities.image_quality_options"
                  type="checkbox"
                  :value="quality"
                  :data-image-quality="quality"
                />{{ t(`admin.settings.desktop.catalog.image.qualityLabels.${quality}`) }}</label>
            </div>
          </fieldset>
        </template>
        <template v-else>
          <div class="dm-field-grid">
            <CatalogField
              v-for="field in limitFields"
              :id="`dm-${field}`"
              :key="field"
              :label="t(`admin.settings.desktop.${field}`)"
              :error="errors[field]"
            >
              <input
                :id="`dm-${field}`"
                :value="entry[field]"
                type="number"
                min="1"
                step="1"
                :placeholder="t('admin.settings.desktop.unknown')"
                :aria-invalid="!!errors[field]"
                :aria-describedby="errors[field] ? `dm-${field}-hint` : undefined"
                @input="setLimit(field, $event)"
              />
            </CatalogField>
          </div>
          <div class="dm-capability-options">
            <label v-for="capability in capabilityFields" :key="capability">
              <input
                v-model="entry.capabilities[capability]"
                type="checkbox"
                :data-capability="capability"
              />{{ text(capability) }}</label>
          </div>
          <label class="dm-verification">
            <input
              v-model="entry.agent_verified"
              type="checkbox"
              data-testid="editor-verified"
              :disabled="changedRoute"
            />
            <span>{{ t('admin.settings.desktop.verified') }}<small>{{ text(changedRoute ? 'verificationReset' : 'verificationHint') }}</small>
            </span>
          </label>
        </template>
        <CatalogField
          id="dm-order"
          :label="t('admin.settings.desktop.sortOrder')"
          :error="errors.sort_order"
          class="dm-order-field"
        >
          <input
            id="dm-order"
            v-model.number="entry.sort_order"
            type="number"
            step="1"
            :aria-invalid="!!errors.sort_order"
            :aria-describedby="errors.sort_order ? 'dm-order-hint' : undefined"
          />
        </CatalogField>
      </div>
      <footer class="dm-panel-footer">
        <span>{{ text('applyHint') }}</span>
        <button
          type="button"
          class="dm-button dm-secondary"
          @click="requestClose"
        >
          {{ t('common.cancel') }}
        </button>
        <button
          type="submit"
          class="dm-button dm-primary"
          data-testid="editor-apply"
        >
          {{ text('applyModel') }}
        </button>
      </footer>
    </form>
  </CatalogDialog>
  <CatalogDialog
    :open="discardOpen"
    :label="text('discardEditorTitle')"
    @close="discardOpen = false"
  >
    <header class="dm-panel-header">
      <div>
        <h2>{{ text('discardEditorTitle') }}</h2>
        <p>{{ text('discardEditorHint') }}</p>
      </div>
    </header>
    <footer class="dm-panel-footer">
      <button class="dm-button dm-secondary" @click="discardOpen = false">
        {{ text('continueEditing') }}
      </button>
      <button
        class="dm-button dm-primary"
        data-testid="editor-discard"
        @click="$emit('close')"
      >
        {{ text('discardEditor') }}
      </button>
    </footer>
  </CatalogDialog>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DesktopModelEntry } from '@/api/admin/settings'
import type { AdminGroup } from '@/types'
import Icon from '@/components/icons/Icon.vue'
import CatalogDialog from './CatalogDialog.vue'
import CatalogField from './CatalogField.vue'
import { clone, imageQualities, platforms, protocolLabels, purposeOf, routeChanged, validateEntry } from './catalog'

const props = defineProps<{ initial: DesktopModelEntry; previous?: DesktopModelEntry; models: DesktopModelEntry[]; groups: AdminGroup[] }>()
const emit = defineEmits<{ apply: [entry: DesktopModelEntry]; close: []; dirty: [value: boolean] }>()
const { t } = useI18n()
const text = (key: string) => t(`admin.desktopModels.${key}`)
const entry = ref(clone(props.initial))
if (purposeOf(entry.value) === 'image') entry.value.capabilities.image_quality_options ??= []
const baseline = JSON.stringify(entry.value)
const errors = ref<Record<string, string>>({})
const discardOpen = ref(false)
const form = ref<HTMLFormElement>()
const identityFields = ['display_name', 'model'] as const
const limitFields = ['context_window', 'max_output_tokens'] as const
const capabilityFields = ['tools', 'vision', 'reasoning'] as const
const image = computed(() => purposeOf(entry.value) === 'image')
const changedRoute = computed(() => !!props.previous && routeChanged(props.previous, entry.value))
const selectedGroup = computed(() => props.groups.find(group => group.id === entry.value.group_id))
const availableGroups = computed(() => props.groups.filter(group => !image.value || ['openai', 'grok', 'composite'].includes(group.platform)))
const availablePlatforms = computed(() => image.value ? platforms.filter(platform => ['openai', 'grok'].includes(platform)) : platforms)
const availableProtocols = computed(() => (Object.keys(protocolLabels) as DesktopModelEntry['api_mode'][]).filter(protocol => (protocol === 'images_generations') === image.value))
watch(entry, () => emit('dirty', JSON.stringify(entry.value) !== baseline), { deep: true })
function invalidateVerification(): void { if (changedRoute.value) entry.value.agent_verified = false }
function selectGroup(event: Event): void {
  const group = props.groups.find(group => group.id === Number((event.target as HTMLSelectElement).value))
  if (!group) return
  entry.value.group_id = group.id
  if (group.platform !== 'composite') entry.value.platform = group.platform
  invalidateVerification()
}
function setLimit(field: typeof limitFields[number], event: Event): void {
  const raw = (event.target as HTMLInputElement).value
  entry.value[field] = raw === '' ? null : Number(raw)
}
function requestClose(): void {
  if (JSON.stringify(entry.value) === baseline) emit('close')
  else discardOpen.value = true
}
async function apply(): Promise<void> {
  errors.value = validateEntry(entry.value, props.models, props.previous?.id ?? null, props.groups)
  if (Object.keys(errors.value).length) {
    await nextTick()
    form.value?.querySelector<HTMLElement>('[aria-invalid="true"]')?.focus()
    return
  }
  const result = clone(entry.value)
  if (changedRoute.value) result.agent_verified = false
  emit('apply', result)
}
</script>
