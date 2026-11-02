<template>
  <div class="space-y-4" data-testid="video-account-protocol">
    <div>
      <label class="input-label">{{ t('admin.accounts.accountType') }}</label>
      <div class="mt-2 flex items-center gap-3 rounded-lg border-2 border-teal-500 bg-teal-50 p-3 dark:bg-teal-900/20">
        <Icon name="key" size="sm" class="text-teal-600 dark:text-teal-400" />
        <div>
          <span class="block text-sm font-medium text-gray-900 dark:text-white">API Key</span>
          <span class="text-xs text-gray-500 dark:text-gray-400">{{ t('admin.accounts.video.apiKeyDescription') }}</span>
        </div>
      </div>
    </div>
    <div>
      <label class="input-label" for="video-api-protocol">{{ t('admin.accounts.video.protocol') }}</label>
      <select id="video-api-protocol" class="input" value="videos">
        <option value="videos">Videos API</option>
      </select>
      <p class="input-hint">{{ t('admin.accounts.video.protocolHint') }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
const { t } = useI18n()
</script>
