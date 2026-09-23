<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useWorkspaceLinks } from '@/composables/useWorkspaceLinks'
import userChannelsAPI from '@/api/channels'
import { getModelPlaza } from '@/api/modelPlaza'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'

const { t } = useI18n()
const { catalog } = useWorkspaceLinks()
const models = ref<Array<{ name: string; platform: string }>>([])
const loading = ref(false)
const failed = ref(false)
const attempt = ref(0)

watch([() => catalog.value?.source, attempt], async ([source], _, onCleanup) => {
  models.value = []
  failed.value = false
  loading.value = false
  if (!source) return
  const controller = new AbortController()
  onCleanup(() => controller.abort())
  loading.value = true
  try {
    const result = source === 'channels'
      ? (await userChannelsAPI.getAvailable({ signal: controller.signal })).flatMap(channel => channel.platforms.flatMap(platform => platform.supported_models))
      : (await getModelPlaza({ signal: controller.signal })).groups.flatMap(group => group.models)
    if (!controller.signal.aborted) {
      models.value = [...new Map(result.map(model => [`${model.platform}:${model.name}`, { name: model.name, platform: model.platform }])).values()].slice(0, 4)
    }
  } catch {
    if (!controller.signal.aborted) failed.value = true
  } finally {
    if (!controller.signal.aborted) loading.value = false
  }
}, { immediate: true })
</script>

<template>
  <section class="workspace-models" aria-labelledby="workspace-models-title">
    <div class="workspace-section-heading">
      <div>
        <h2 id="workspace-models-title">{{ t('workspace.catalogTitle') }}</h2>
        <p>{{ t('workspace.modelsDescription') }}</p>
      </div>
      <router-link v-if="catalog" :to="catalog.to" class="workspace-text-link">{{ t('workspace.catalogAll') }}<Icon name="arrowRight" size="sm" /></router-link>
    </div>
    <div v-if="loading" class="py-8"><LoadingSpinner /></div>
    <div v-else-if="failed" class="workspace-model-empty" role="status">
      <p>{{ t('workspace.catalogError') }}</p>
      <button class="btn btn-secondary btn-sm" @click="attempt++">{{ t('workspace.retry') }}</button>
    </div>
    <div v-else-if="models.length" class="workspace-model-list">
      <router-link v-for="model in models" :key="`${model.platform}:${model.name}`" :to="catalog?.to || '/keys'" class="workspace-model-row">
        <Icon name="cube" size="md" />
        <div class="min-w-0 flex-1"><strong>{{ model.name }}</strong><span>{{ model.platform }}</span></div>
        <Icon name="arrowRight" size="sm" />
      </router-link>
    </div>
    <div v-else class="workspace-model-empty">
      <Icon name="cube" size="lg" />
      <p>{{ t(catalog ? 'workspace.catalogEmpty' : 'workspace.modelsUnavailable') }}</p>
      <router-link to="/keys?create=1" class="workspace-text-link">{{ t('workspace.create') }}<Icon name="arrowRight" size="sm" /></router-link>
    </div>
    <p v-if="models.length" class="mt-3 text-xs text-gray-500 dark:text-dark-400">{{ t('workspace.catalogNote') }}</p>
  </section>
</template>
