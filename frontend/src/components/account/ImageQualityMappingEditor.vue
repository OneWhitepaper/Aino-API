<template>
  <details class="rounded-xl border border-gray-200 dark:border-dark-600" data-testid="image-quality-mapping">
    <summary class="cursor-pointer px-4 py-3 text-sm font-medium text-gray-900 dark:text-white">
      {{ t('admin.accounts.imageQualityMapping.title') }}
      <span v-if="rows.length" class="ml-2 text-xs font-normal text-gray-500 dark:text-gray-400">{{ rows.length }}</span>
    </summary>
    <div class="space-y-3 border-t border-gray-200 p-4 dark:border-dark-600">
      <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">
        {{ t('admin.accounts.imageQualityMapping.hint') }}
      </p>
      <div v-for="(row, index) in rows" :key="index" class="grid grid-cols-1 items-end gap-2 sm:grid-cols-[minmax(0,1fr)_7rem_7rem_auto]" data-testid="image-quality-row">
        <label class="min-w-0 text-xs text-gray-600 dark:text-gray-400">
          {{ t('admin.accounts.imageQualityMapping.model') }}
          <input class="input mt-1" type="text" :value="row.model" :placeholder="t('admin.accounts.imageQualityMapping.modelPlaceholder')" data-testid="image-quality-model" @input="updateRow(index, 'model', ($event.target as HTMLInputElement).value)" />
        </label>
        <label class="text-xs text-gray-600 dark:text-gray-400">
          {{ t('admin.accounts.imageQualityMapping.requested') }}
          <select class="input mt-1" :value="row.requestedQuality" data-testid="image-quality-requested" @change="updateRow(index, 'requestedQuality', ($event.target as HTMLSelectElement).value)">
            <option v-for="quality in REQUESTED_IMAGE_QUALITIES" :key="quality" :value="quality">{{ t(`admin.accounts.imageQualityMapping.qualities.${quality}`) }}</option>
          </select>
        </label>
        <label class="text-xs text-gray-600 dark:text-gray-400">
          {{ t('admin.accounts.imageQualityMapping.upstream') }}
          <select class="input mt-1" :value="row.upstreamQuality" data-testid="image-quality-upstream" @change="updateRow(index, 'upstreamQuality', ($event.target as HTMLSelectElement).value)">
            <option v-for="quality in UPSTREAM_IMAGE_QUALITIES" :key="quality" :value="quality">{{ t(`admin.accounts.imageQualityMapping.qualities.${quality}`) }}</option>
          </select>
        </label>
        <button type="button" class="btn btn-secondary text-red-600 dark:text-red-400" data-testid="image-quality-remove" :aria-label="t('admin.accounts.imageQualityMapping.removeRow')" @click="emit('update:rows', rows.filter((_, rowIndex) => rowIndex !== index))">{{ t('common.delete') }}</button>
      </div>
      <p v-if="rows.length === 0" class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.imageQualityMapping.empty') }}</p>
      <button type="button" class="btn btn-secondary text-sm" data-testid="image-quality-add" @click="emit('update:rows', [...rows, { model: '', requestedQuality: 'auto', upstreamQuality: 'auto' }])">{{ t('admin.accounts.imageQualityMapping.addRow') }}</button>
    </div>
  </details>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { REQUESTED_IMAGE_QUALITIES, UPSTREAM_IMAGE_QUALITIES, type ImageQualityMappingRow } from './imageQualityMapping'

const props = defineProps<{ rows: ImageQualityMappingRow[] }>()
const emit = defineEmits<{ (event: 'update:rows', rows: ImageQualityMappingRow[]): void }>()
const { t } = useI18n()

function updateRow(index: number, field: keyof ImageQualityMappingRow, value: string) {
  emit('update:rows', props.rows.map((row, rowIndex) => rowIndex === index ? { ...row, [field]: value } : row))
}
</script>
