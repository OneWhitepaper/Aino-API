<template>
  <div class="space-y-6">
    <div class="card p-6 sm:p-7">
      <div class="flex items-start gap-4">
        <div class="flex h-12 w-12 shrink-0 items-center justify-center rounded-2xl bg-primary-50 text-primary-600 dark:bg-primary-900/30 dark:text-primary-400">
          <Icon name="chat" class="h-6 w-6" />
        </div>
        <div class="min-w-0">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('contact.title') }}</h2>
          <p class="mt-2 whitespace-pre-line break-words text-sm leading-6 text-gray-500 dark:text-dark-400">{{ settings.description || t('contact.introduction') }}</p>
          <p v-if="settings.service_hours" class="mt-3 flex items-start gap-2 text-sm text-primary-700 dark:text-primary-300">
            <Icon name="clock" size="sm" class="mt-0.5 shrink-0" />
            <span class="whitespace-pre-line break-words">{{ t('contact.serviceHours') }}：{{ settings.service_hours }}</span>
          </p>
        </div>
      </div>
    </div>

    <section v-if="methods.length" :aria-label="t('contact.methods')" class="space-y-4">
      <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('contact.methods') }}</h3>
      <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        <article v-for="(method, index) in methods" :key="index" class="card flex min-w-0 flex-col p-5">
          <div class="flex items-center gap-3">
            <div class="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-primary-50 text-primary-600 dark:bg-primary-900/30 dark:text-primary-400">
              <Icon :name="method.type === 'email' ? 'mail' : method.type === 'link' ? 'link' : 'chat'" class="h-5 w-5" />
            </div>
            <h4 class="min-w-0 break-words font-medium text-gray-900 dark:text-white">{{ method.label }}</h4>
          </div>
          <p class="my-4 break-all text-sm text-gray-600 dark:text-dark-300">{{ method.value }}</p>
          <div class="mt-auto flex flex-wrap gap-2">
            <button type="button" class="btn btn-secondary btn-sm" data-testid="copy-contact" @click="copyToClipboard(method.value)">
              <Icon name="copy" size="sm" />{{ t('contact.copy') }}
            </button>
            <a v-if="contactLink(method)" :href="contactLink(method)" :target="method.type === 'email' ? undefined : '_blank'" rel="noopener noreferrer" class="btn btn-primary btn-sm">
              <Icon :name="method.type === 'email' ? 'mail' : 'externalLink'" size="sm" />{{ t(method.type === 'email' ? 'contact.sendEmail' : 'contact.openLink') }}
            </a>
          </div>
        </article>
      </div>
    </section>

    <section v-if="groups.length" :aria-label="t('contact.communities')" class="space-y-4">
      <div>
        <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('contact.communities') }}</h3>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('contact.communityHint') }}</p>
      </div>
      <div class="grid gap-5 md:grid-cols-2 xl:grid-cols-3">
        <article v-for="(group, index) in groups" :key="index" class="card flex min-w-0 flex-col items-center p-6 text-center">
          <span class="mb-3 inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1 text-xs font-medium" :class="group.platform === 'qq' ? 'bg-blue-50 text-blue-600 dark:bg-blue-900/30 dark:text-blue-300' : 'bg-primary-50 text-primary-600 dark:bg-primary-900/30 dark:text-primary-400'">
            <Icon :name="group.platform === 'wechat' ? 'chat' : 'users'" size="sm" />{{ t(`contact.platforms.${group.platform}`) }}
          </span>
          <h4 class="break-words font-semibold text-gray-900 dark:text-white">{{ group.name }}</h4>
          <p v-if="group.description" class="mt-2 whitespace-pre-line break-words text-sm leading-6 text-gray-500 dark:text-dark-400">{{ group.description }}</p>
          <button v-if="group.qr_code" type="button" class="mt-5 rounded-2xl border border-gray-200 bg-white p-3 transition-shadow hover:shadow-md focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:border-dark-600" :aria-label="t('contact.enlargeQR', { name: group.name })" @click="selectedGroup = group">
            <img :src="group.qr_code" :alt="group.name" class="h-48 w-48 max-w-full object-contain" loading="lazy" />
          </button>
          <p v-if="group.qr_code" class="mt-3 text-xs text-gray-400 dark:text-dark-400">{{ t('contact.scanHint') }}</p>
          <button v-if="group.group_number" type="button" class="mt-4 inline-flex max-w-full items-center gap-2 text-sm text-primary-700 hover:text-primary-600 dark:text-primary-300" @click="copyToClipboard(group.group_number)">
            <span class="break-all">{{ t('contact.groupNumber') }}：{{ group.group_number }}</span><Icon name="copy" size="sm" class="shrink-0" />
          </button>
          <a v-if="sanitizeUrl(group.join_url)" :href="sanitizeUrl(group.join_url)" target="_blank" rel="noopener noreferrer" class="btn btn-secondary btn-sm mt-4">
            <Icon name="externalLink" size="sm" />{{ t('contact.joinGroup') }}
          </a>
        </article>
      </div>
      <p class="flex items-start gap-2 text-xs leading-5 text-gray-400 dark:text-dark-400"><Icon name="infoCircle" size="sm" class="mt-0.5 shrink-0" />{{ t('contact.expiredHint') }}</p>
    </section>

    <div v-if="!methods.length && !groups.length" class="card px-6 py-12 text-center">
      <Icon name="chat" class="mx-auto mb-4 h-10 w-10 text-gray-300 dark:text-dark-500" />
      <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('contact.empty') }}</p>
    </div>

    <BaseDialog :show="!!selectedGroup" :title="selectedGroup?.name || ''" width="narrow" @close="selectedGroup = null">
      <template v-if="selectedGroup">
        <div class="rounded-2xl bg-white p-4"><img :src="selectedGroup.qr_code" :alt="selectedGroup.name" class="mx-auto max-h-[60vh] w-full object-contain" /></div>
        <p class="mt-4 text-center text-sm text-gray-500 dark:text-dark-400">{{ t('contact.scanHint') }}</p>
      </template>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useClipboard } from '@/composables/useClipboard'
import { sanitizeUrl } from '@/utils/url'
import type { ContactGroup, ContactMethod, ContactPageSettings } from '@/api/contact'

const props = defineProps<{ settings: ContactPageSettings }>()
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const selectedGroup = ref<ContactGroup | null>(null)
const methods = computed(() => props.settings.methods.filter(method => method.enabled))
const groups = computed(() => props.settings.groups.filter(group => group.enabled))

function contactLink(method: ContactMethod): string {
  if (method.type === 'email' && /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(method.value)) {
    return `mailto:${encodeURIComponent(method.value)}`
  }
  return sanitizeUrl(method.url)
}
</script>
