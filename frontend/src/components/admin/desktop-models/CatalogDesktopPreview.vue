<template>
  <CatalogDialog
    open
    drawer
    :label="text('desktopPreview')"
    @close="$emit('close')"
  >
    <header class="dm-panel-header">
      <div>
        <span class="dm-eyebrow">{{ text('userPerspective') }}</span>
        <h2>{{ text('desktopPreview') }}</h2>
        <p>{{ text('previewDescription') }}</p>
      </div>
      <button
        class="dm-icon-button"
        :aria-label="t('common.close')"
        @click="$emit('close')"
      >
        <Icon name="x" />
      </button>
    </header>
    <div class="dm-panel-scroll dm-preview-ground">
      <div class="dm-desktop-window">
        <div class="dm-desktop-title">
          <span class="dm-window-dots">
            <i />
            <i />
            <i />
          </span>
          <img
            src="/aino-app-icon.png"
            width="20"
            height="20"
            alt=""
          />Aino
        </div>
        <div class="dm-desktop-greeting">
          <img
            src="/aino-app-icon.png"
            width="52"
            height="52"
            alt="Aino"
          />
          <h3>{{ text('previewGreeting') }}</h3>
          <p>{{ text('previewStart') }}</p>
        </div>
        <div class="dm-desktop-composer">
          <p>{{ text('previewPlaceholder') }}</p>
          <div>
            <button
              class="dm-model-trigger"
              :aria-expanded="pickerOpen"
              aria-controls="dm-preview-models"
              @click="pickerOpen = !pickerOpen"
            >
              <Icon name="grid" />{{ selected?.display_name || text('chooseModel') }}<Icon name="chevronDown" />
            </button>
            <span class="dm-desktop-send" aria-hidden="true">
              <Icon name="arrowUp" />
            </span>
          </div>
        </div>
        <div
          v-if="pickerOpen"
          id="dm-preview-models"
          class="dm-desktop-menu"
        >
          <div class="dm-desktop-menu-title">
            <span>{{ t(`admin.settings.desktop.catalog.${purpose}.title`) }}</span>
            <span>{{ text('previewCount', { count: models.length }) }}</span>
          </div>
          <p v-if="!settings.enabled" class="dm-preview-empty">
            {{ text('catalogClosed') }}
          </p>
          <p v-else-if="!models.length" class="dm-preview-empty">
            {{ text('previewEmpty') }}
          </p>
          <button
            v-for="model in settings.enabled ? models : []"
            :key="model.id"
            class="dm-desktop-option"
            :aria-pressed="selected?.id === model.id"
            data-testid="preview-model"
            @click="selectedID = model.id; pickerOpen = false"
          >
            <CatalogProviderIcon :entry="model" />
            <span>{{ model.display_name }}<small>{{ model.id === defaultID ? text('default') : model.provider_label }}</small>
            </span>
            <Icon v-if="selected?.id === model.id" name="check" />
          </button>
        </div>
      </div>
      <p class="dm-preview-explanation">
        <Icon name="eye" />{{ text('previewHint') }}
      </p>
    </div>
  </CatalogDialog>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { DesktopSettings } from '@/api/admin/settings'
import Icon from '@/components/icons/Icon.vue'
import CatalogDialog from './CatalogDialog.vue'
import CatalogProviderIcon from './CatalogProviderIcon.vue'
import { purposeOf, type ModelPurpose } from './catalog'
const props = defineProps<{ settings: DesktopSettings; purpose: ModelPurpose }>()
defineEmits<{ close: [] }>()
const { t } = useI18n()
const text = (key: string, params: Record<string, string | number> = {}) => t(`admin.desktopModels.${key}`, params)
const selectedID = ref('')
const pickerOpen = ref(true)
const models = computed(() => props.settings.models.filter(model => purposeOf(model) === props.purpose).sort((a, b) => a.sort_order - b.sort_order))
const defaultID = computed(() => props.purpose === 'chat' ? props.settings.default_model_id : props.settings.default_image_model_id)
const selected = computed(() => props.settings.enabled ? models.value.find(model => model.id === (selectedID.value || defaultID.value)) : undefined)
</script>
