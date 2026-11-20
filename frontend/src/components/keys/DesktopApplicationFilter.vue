<template>
  <div
    role="group"
    :aria-label="t('keys.desktopApplications.label')"
    class="flex flex-wrap items-center gap-2"
  >
    <button
      v-for="application in applications"
      :key="application.id"
      type="button"
      :data-testid="`desktop-app-filter-${application.id || 'all'}`"
      :aria-pressed="modelValue === application.id"
      class="inline-flex min-h-11 items-center justify-center gap-2 rounded-xl border px-4 py-2 text-sm font-medium transition-colors duration-150 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary-600 motion-reduce:transition-none"
      :class="modelValue === application.id
        ? 'border-primary-300 bg-primary-50 text-primary-800 dark:border-primary-700 dark:bg-primary-900/25 dark:text-primary-200'
        : 'border-gray-200 bg-white text-gray-600 hover:border-gray-300 hover:bg-gray-50 hover:text-gray-900 dark:border-dark-600 dark:bg-dark-800 dark:text-dark-300 dark:hover:border-dark-500 dark:hover:bg-dark-700 dark:hover:text-white'"
      @click="selectApplication(application.id)"
    >
      <img v-if="application.icon" :src="application.icon" alt="" class="h-5 w-5 shrink-0 rounded-md object-contain" />
      <span>{{ application.name }}</span>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { DESKTOP_APPLICATIONS } from '@/constants/desktopApplications'

const props = defineProps<{ modelValue: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const { t } = useI18n()

const applications = computed(() => [
  { id: '', name: t('keys.desktopApplications.all'), icon: '' },
  ...DESKTOP_APPLICATIONS,
  { id: 'other', name: t('keys.desktopApplications.other'), icon: '' }
])

function selectApplication(applicationId: string) {
  if (props.modelValue !== applicationId) emit('update:modelValue', applicationId)
}
</script>
