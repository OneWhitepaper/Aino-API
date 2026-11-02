<template>
  <div class="space-y-4" data-testid="video-api-usage">
    <p class="text-sm text-gray-600 dark:text-gray-400">{{ t('keys.useKeyModal.video.description') }}</p>
    <div v-for="example in examples" :key="example.key" class="overflow-hidden rounded-xl border border-gray-200 dark:border-dark-600">
      <div class="flex items-center justify-between bg-gray-50 px-4 py-2 dark:bg-dark-700">
        <span class="text-sm font-medium">{{ t(`keys.useKeyModal.video.${example.key}`) }}</span>
        <button type="button" class="btn btn-secondary btn-sm" @click="copyToClipboard(example.code)">{{ t('keys.useKeyModal.copy') }}</button>
      </div>
      <pre class="overflow-x-auto bg-gray-900 p-4 text-xs leading-6 text-gray-100"><code>{{ example.code }}</code></pre>
    </div>
    <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('keys.useKeyModal.video.hint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useClipboard } from '@/composables/useClipboard'

const props = defineProps<{ apiKey: string; baseUrl: string }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const quote = (value: string) => "'" + value.replace(/'/g, "'\\''") + "'"
const examples = computed(() => {
  const base = props.baseUrl.replace(/\/+$/, '').replace(/\/v1$/, '') + '/v1/videos'
  const auth = quote('Authorization: Bearer ' + props.apiKey)
  return [
    { key: 'create', code: `curl ${quote(base)} \\\n  -H ${auth} \\\n  -H 'Content-Type: application/json' \\\n  -d '{"model":"veo-3.1","prompt":"A paper boat drifting along a stream","seconds":4,"resolution":"720p"}'` },
    { key: 'status', code: `curl ${quote(base + '/VIDEO_ID')} \\\n  -H ${auth}` },
    { key: 'download', code: `curl ${quote(base + '/VIDEO_ID/content')} \\\n  -H ${auth} \\\n  -o video.mp4` }
  ]
})
</script>
