<template>
  <AppLayout>
    <div v-if="loading" role="status" class="flex justify-center py-16"><LoadingSpinner /></div>
    <div v-else-if="failed" role="alert" class="card p-10 text-center">
      <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('contact.loadFailed') }}</p>
      <button type="button" data-testid="retry-contact" class="btn btn-secondary mt-4" @click="load"><Icon name="refresh" size="sm" />{{ t('contact.retry') }}</button>
    </div>
    <ContactContent v-else-if="settings" :settings="settings" />
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import ContactContent from '@/components/contact/ContactContent.vue'
import { getContactPage, type ContactPageSettings } from '@/api/contact'

const { t } = useI18n()
const settings = ref<ContactPageSettings | null>(null)
const loading = ref(true)
const failed = ref(false)

async function load() {
  loading.value = true
  failed.value = false
  try {
    settings.value = await getContactPage()
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>
