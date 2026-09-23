<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useWorkspaceLinks } from '@/composables/useWorkspaceLinks'
import { useClipboard } from '@/composables/useClipboard'
import { buildConnectionExample } from '@/utils/workspace'
import Icon from '@/components/icons/Icon.vue'

const { t } = useI18n()
const { endpoint, docs, catalog } = useWorkspaceLinks()
const { copyToClipboard } = useClipboard()
const protocol = ref<'openai' | 'anthropic'>('openai')
const example = computed(() => buildConnectionExample(endpoint.value, protocol.value))
</script>

<template>
  <div class="connection-guide space-y-6">
    <ol class="space-y-4">
      <li v-for="(step, index) in ['key', 'model', 'request']" :key="step" class="flex gap-4">
        <span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-primary-100 text-xs font-semibold text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">{{ index + 1 }}</span>
        <div>
          <h4 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t(`workspace.${step}Step`) }}</h4>
          <p class="mt-1 text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t(`workspace.${step}StepDescription`) }}</p>
        </div>
      </li>
    </ol>
    <div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-950">
      <div class="flex items-center justify-between gap-3">
        <span class="text-xs font-semibold text-gray-500 dark:text-dark-400">{{ t('workspace.endpoint') }}</span>
        <button type="button" class="btn btn-ghost btn-sm" @click="copyToClipboard(endpoint)">{{ t('workspace.copyEndpoint') }}</button>
      </div>
      <code class="mt-2 block break-all text-sm text-gray-900 dark:text-white">{{ endpoint }}</code>
    </div>
    <div>
      <div class="mb-3 flex flex-wrap items-center justify-between gap-3">
        <div class="inline-flex gap-1 rounded-full bg-gray-100 p-1 dark:bg-dark-950" :aria-label="t('workspace.example')" role="group">
          <button v-for="item in (['openai', 'anthropic'] as const)" :key="item" type="button" class="rounded-full px-3 py-2 text-xs font-medium" :class="protocol === item ? 'bg-gray-900 text-white dark:bg-primary-100 dark:text-gray-900' : 'text-gray-600 dark:text-dark-300'" :aria-pressed="protocol === item" @click="protocol = item">{{ item === 'openai' ? 'OpenAI' : 'Anthropic' }}</button>
        </div>
        <button type="button" class="btn btn-secondary btn-sm" @click="copyToClipboard(example)"><Icon name="copy" size="sm" />{{ t('workspace.copyExample') }}</button>
      </div>
      <pre class="max-h-80 overflow-auto rounded-xl bg-gray-950 p-4 text-xs leading-6 text-gray-100" tabindex="0" :aria-label="t('workspace.example')"><code>{{ example }}</code></pre>
      <p class="mt-3 text-xs leading-6 text-gray-500 dark:text-dark-400">{{ t('workspace.exampleHint') }}</p>
    </div>
    <p class="border-t border-gray-200 pt-4 text-sm leading-6 text-gray-600 dark:border-dark-700 dark:text-dark-300">{{ t('workspace.keyHint') }}</p>
    <div class="flex flex-wrap gap-3">
      <router-link v-if="catalog" :to="catalog.to" class="btn btn-secondary">{{ t('workspace.models') }}</router-link>
      <a v-if="docs" :href="docs" target="_blank" rel="noopener noreferrer" class="btn btn-ghost">{{ t('workspace.docs') }}<Icon name="arrowRight" size="sm" /></a>
    </div>
  </div>
</template>
