<template>
  <AppLayout>
    <div v-if="loading" role="status" class="flex justify-center py-16"><LoadingSpinner /></div>
    <div v-else-if="failed" role="alert" class="card p-10 text-center">
      <p class="text-sm text-gray-500 dark:text-dark-400">{{ t('contact.loadFailed') }}</p>
      <button type="button" class="btn btn-secondary mt-4" @click="load"><Icon name="refresh" size="sm" />{{ t('contact.retry') }}</button>
    </div>
    <form v-else class="space-y-6" @submit.prevent="save">
      <div class="card flex flex-col gap-4 p-6 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('contact.admin.title') }}</h2>
          <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('contact.admin.description') }}</p>
        </div>
        <div class="flex shrink-0 flex-wrap gap-2">
          <button type="button" class="btn btn-secondary" @click="preview = true"><Icon name="eye" size="sm" />{{ t('contact.admin.preview') }}</button>
          <button type="submit" class="btn btn-primary" :disabled="saving"><Icon :name="saving ? 'refresh' : 'check'" size="sm" :class="{ 'animate-spin': saving }" />{{ t(saving ? 'contact.admin.saving' : 'contact.admin.save') }}</button>
        </div>
      </div>

      <fieldset class="space-y-6" :disabled="saving">
        <div class="card space-y-5 p-6">
          <h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('contact.admin.pageSettings') }}</h3>
          <div>
            <label for="contact-description" class="input-label">{{ t('contact.admin.introduction') }}</label>
            <textarea id="contact-description" v-model="draft.description" name="description" rows="3" maxlength="2000" class="input" :placeholder="t('contact.introduction')" />
          </div>
          <div>
            <label for="contact-hours" class="input-label">{{ t('contact.serviceHours') }}</label>
            <input id="contact-hours" v-model="draft.service_hours" maxlength="200" class="input" :placeholder="t('contact.admin.hoursPlaceholder')" />
          </div>
        </div>

        <section class="card space-y-5 p-6">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div><h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('contact.methods') }}</h3><p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('contact.admin.methodsHint') }}</p></div>
            <button type="button" data-testid="add-contact" class="btn btn-secondary btn-sm" :disabled="draft.methods.length >= 12" @click="addMethod"><Icon name="plus" size="sm" />{{ t('contact.admin.addMethod') }}</button>
          </div>
          <p v-if="!draft.methods.length" class="rounded-xl border border-dashed border-gray-200 p-6 text-center text-sm text-gray-400 dark:border-dark-600">{{ t('contact.admin.noMethods') }}</p>
          <article v-for="(method, index) in draft.methods" :key="rowKey(method)" class="space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-600">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <span class="text-sm font-medium text-gray-700 dark:text-dark-200">{{ t('contact.admin.methodIndex', { index: index + 1 }) }}</span>
              <div class="flex items-center gap-2">
                <label class="flex items-center gap-2 text-xs text-gray-500 dark:text-dark-400"><Toggle v-model="method.enabled" :aria-label="t('contact.admin.visible')" />{{ t('contact.admin.visible') }}</label>
                <button type="button" class="btn btn-secondary btn-sm" :disabled="index === 0" :aria-label="t('contact.admin.moveUp')" @click="move(draft.methods, index, -1)"><Icon name="arrowUp" size="sm" /></button>
                <button type="button" class="btn btn-secondary btn-sm" :disabled="index === draft.methods.length - 1" :aria-label="t('contact.admin.moveDown')" @click="move(draft.methods, index, 1)"><Icon name="arrowDown" size="sm" /></button>
                <button type="button" class="btn btn-secondary btn-sm text-red-500" :aria-label="t('contact.admin.remove')" @click="draft.methods.splice(index, 1)"><Icon name="trash" size="sm" /></button>
              </div>
            </div>
            <div class="grid gap-4 sm:grid-cols-2">
              <div><label :for="`method-type-${index}`" class="input-label">{{ t('contact.admin.type') }}</label><select :id="`method-type-${index}`" v-model="method.type" class="input"><option v-for="type in methodTypes" :key="type" :value="type">{{ t(`contact.types.${type}`) }}</option></select></div>
              <div><label :for="`method-label-${index}`" class="input-label">{{ t('contact.admin.label') }}</label><input :id="`method-label-${index}`" v-model="method.label" :name="`method-label-${index}`" maxlength="80" :required="method.enabled" class="input" :placeholder="t('contact.admin.labelPlaceholder')" /></div>
              <div><label :for="`method-value-${index}`" class="input-label">{{ t('contact.admin.value') }}</label><input :id="`method-value-${index}`" v-model="method.value" :name="`method-value-${index}`" :type="method.type === 'email' ? 'email' : 'text'" maxlength="300" :required="method.enabled" class="input" /></div>
              <div><label :for="`method-url-${index}`" class="input-label">{{ t('contact.admin.link') }}</label><input :id="`method-url-${index}`" v-model="method.url" type="url" maxlength="2048" :required="method.enabled && method.type === 'link'" class="input" placeholder="https://" /></div>
            </div>
          </article>
        </section>

        <section class="card space-y-5 p-6">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div><h3 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('contact.communities') }}</h3><p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('contact.admin.groupsHint') }}</p></div>
            <button type="button" data-testid="add-group" class="btn btn-secondary btn-sm" :disabled="draft.groups.length >= 12" @click="addGroup"><Icon name="plus" size="sm" />{{ t('contact.admin.addGroup') }}</button>
          </div>
          <p v-if="!draft.groups.length" class="rounded-xl border border-dashed border-gray-200 p-6 text-center text-sm text-gray-400 dark:border-dark-600">{{ t('contact.admin.noGroups') }}</p>
          <article v-for="(group, index) in draft.groups" :key="rowKey(group)" class="space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-600">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <span class="text-sm font-medium text-gray-700 dark:text-dark-200">{{ t('contact.admin.groupIndex', { index: index + 1 }) }}</span>
              <div class="flex items-center gap-2">
                <label class="flex items-center gap-2 text-xs text-gray-500 dark:text-dark-400"><Toggle v-model="group.enabled" :aria-label="t('contact.admin.visible')" />{{ t('contact.admin.visible') }}</label>
                <button type="button" class="btn btn-secondary btn-sm" :disabled="index === 0" :aria-label="t('contact.admin.moveUp')" @click="move(draft.groups, index, -1)"><Icon name="arrowUp" size="sm" /></button>
                <button type="button" class="btn btn-secondary btn-sm" :disabled="index === draft.groups.length - 1" :aria-label="t('contact.admin.moveDown')" @click="move(draft.groups, index, 1)"><Icon name="arrowDown" size="sm" /></button>
                <button type="button" class="btn btn-secondary btn-sm text-red-500" :aria-label="t('contact.admin.remove')" @click="draft.groups.splice(index, 1)"><Icon name="trash" size="sm" /></button>
              </div>
            </div>
            <div class="grid gap-4 sm:grid-cols-2">
              <div><label :for="`group-platform-${index}`" class="input-label">{{ t('contact.admin.platform') }}</label><select :id="`group-platform-${index}`" v-model="group.platform" class="input"><option v-for="platform in groupPlatforms" :key="platform" :value="platform">{{ t(`contact.platforms.${platform}`) }}</option></select></div>
              <div><label :for="`group-name-${index}`" class="input-label">{{ t('contact.admin.groupName') }}</label><input :id="`group-name-${index}`" v-model="group.name" maxlength="80" :required="group.enabled" class="input" /></div>
              <div><label :for="`group-number-${index}`" class="input-label">{{ t('contact.groupNumber') }}</label><input :id="`group-number-${index}`" v-model="group.group_number" maxlength="100" class="input" /></div>
              <div><label :for="`group-url-${index}`" class="input-label">{{ t('contact.admin.joinLink') }}</label><input :id="`group-url-${index}`" v-model="group.join_url" type="url" maxlength="2048" class="input" placeholder="https://" /></div>
            </div>
            <div><label :for="`group-description-${index}`" class="input-label">{{ t('contact.admin.groupDescription') }}</label><textarea :id="`group-description-${index}`" v-model="group.description" rows="2" maxlength="500" class="input" :placeholder="t('contact.admin.groupDescriptionPlaceholder')" /></div>
            <div>
              <p class="input-label">{{ t('contact.admin.qrCode') }}</p>
              <ImageUpload v-model="group.qr_code" accept="image/png,image/jpeg,image/webp" :max-size="300 * 1024" :upload-label="t('contact.admin.uploadQR')" :hint="t('contact.admin.uploadHint')" />
            </div>
          </article>
        </section>
      </fieldset>
      <div class="flex justify-end"><button type="submit" class="btn btn-primary" :disabled="saving">{{ t(saving ? 'contact.admin.saving' : 'contact.admin.save') }}</button></div>
    </form>
    <BaseDialog :show="preview" :title="t('contact.admin.preview')" width="extra-wide" @close="preview = false"><ContactContent v-if="preview" :settings="draft" /></BaseDialog>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ImageUpload from '@/components/common/ImageUpload.vue'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import ContactContent from '@/components/contact/ContactContent.vue'
import { getContactPageSettings, updateContactPageSettings, type ContactPageSettings, type ContactType, type ContactGroupPlatform } from '@/api/contact'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()
const draft = ref<ContactPageSettings>({ description: '', service_hours: '', methods: [], groups: [] })
const loading = ref(true)
const failed = ref(false)
const saving = ref(false)
const preview = ref(false)
const methodTypes: ContactType[] = ['wechat', 'qq', 'email', 'phone', 'link', 'other']
const groupPlatforms: ContactGroupPlatform[] = ['wechat', 'qq', 'other']
const rowKeys = new WeakMap<object, number>()
let nextRowKey = 0

function rowKey(row: object) {
  if (!rowKeys.has(row)) rowKeys.set(row, ++nextRowKey)
  return rowKeys.get(row)!
}

function addMethod() {
  draft.value.methods.push({ type: 'wechat', label: '', value: '', url: '', enabled: true })
}

function addGroup() {
  draft.value.groups.push({ platform: 'wechat', name: '', description: '', qr_code: '', group_number: '', join_url: '', enabled: true })
}

function move<T>(items: T[], index: number, direction: number) {
  const target = index + direction
  if (target < 0 || target >= items.length) return
  const [item] = items.splice(index, 1)
  items.splice(target, 0, item!)
}

async function load() {
  loading.value = true
  failed.value = false
  try {
    draft.value = await getContactPageSettings()
  } catch {
    failed.value = true
  } finally {
    loading.value = false
  }
}

async function save() {
  if (saving.value || failed.value) return
  saving.value = true
  try {
    draft.value = await updateContactPageSettings(draft.value)
    appStore.showSuccess(t('contact.admin.saved'))
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error) || t('contact.admin.saveFailed'))
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
