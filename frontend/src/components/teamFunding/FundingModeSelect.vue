<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { fundingModes, type FundingMode } from '@/api/teamFunding'
defineProps<{ modelValue: FundingMode | null; id: string; label: string; optional?: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: FundingMode | null] }>()
const { t } = useI18n()
function update(event: Event) { emit('update:modelValue', ((event.target as HTMLSelectElement).value || null) as FundingMode | null) }
</script>
<template>
  <label :for="id" class="block space-y-2 text-sm font-medium">
    <span>{{ label }}</span>
    <select :id="id" class="input w-full" :value="modelValue ?? ''" @change="update">
      <option v-if="optional" value="">{{ t('teamFunding.inherit') }}</option>
      <option v-for="mode in fundingModes" :key="mode" :value="mode">{{ t(`teamFunding.modes.${mode}`) }}</option>
    </select>
  </label>
</template>
