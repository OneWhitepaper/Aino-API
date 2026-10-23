<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="space-y-3">
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.diagnostics.privacy') }}</p>
          <form class="flex flex-wrap items-center gap-3" @submit.prevent="search">
            <input v-model="query" class="input max-w-md" :placeholder="t('admin.diagnostics.search')" :aria-label="t('admin.diagnostics.search')" />
            <button class="btn btn-primary" type="submit" :disabled="loading">{{ t('common.search') }}</button>
            <button class="btn btn-secondary" type="button" :disabled="loading" @click="load">{{ t('common.refresh') }}</button>
          </form>
          <p v-if="error" class="text-sm text-red-600" role="alert">{{ error }}</p>
        </div>
      </template>
      <template #table>
        <DataTable :columns="columns" :data="items" :loading="loading">
          <template #cell-id="{ value }"><code class="break-all text-xs">{{ value }}</code></template>
          <template #cell-created_at="{ value }">{{ formatDateTime(value) }}</template>
          <template #cell-expires_at="{ value }">{{ formatDateTime(value) }}</template>
          <template #cell-actions="{ row }">
            <div class="flex items-center gap-2">
              <button data-diagnostic-view class="btn btn-secondary btn-sm" @click="view(row)">{{ t('admin.diagnostics.view') }}</button>
              <button class="btn btn-danger btn-sm" :disabled="deleting" @click="deleteTarget = row">{{ t('common.delete') }}</button>
            </div>
          </template>
        </DataTable>
      </template>
      <template #pagination>
        <Pagination :total="total" :page="page" :page-size="pageSize" @update:page="changePage" @update:page-size="changeSize" />
      </template>
    </TablePageLayout>
    <BaseDialog :show="selected !== null" :title="t('admin.diagnostics.title')" width="extra-wide" @close="closeDetail">
      <p v-if="selected" class="mb-3 break-all font-mono text-xs">{{ selected.id }}</p>
      <p v-if="detailLoading" role="status">{{ t('common.loading') }}</p>
      <p v-if="detailError" class="text-red-600" role="alert">{{ detailError }}</p>
      <div v-for="(content, name) in detail?.files" :key="name" class="mb-4">
        <h4 class="mb-2 font-medium">{{ name }}</h4>
        <pre class="max-h-[50vh] overflow-auto whitespace-pre-wrap break-words rounded-lg bg-gray-100 p-4 text-xs dark:bg-dark-900">{{ content }}</pre>
      </div>
    </BaseDialog>
    <ConfirmDialog :show="deleteTarget !== null" :title="t('common.delete')" :message="t('admin.diagnostics.deleteConfirm')" danger @confirm="remove" @cancel="deleteTarget = null" />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { listDiagnostics, readDiagnostic, deleteDiagnostic, type DiagnosticDetail, type DiagnosticMetadata } from '@/api/admin/diagnostics'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { formatDateTime } from '@/utils/format'
import { useAppStore } from '@/stores/app'

const { t } = useI18n()
const app = useAppStore()
const items = ref<DiagnosticMetadata[]>([])
const query = ref('')
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const loading = ref(false)
const error = ref('')
const selected = ref<DiagnosticMetadata | null>(null)
const detail = ref<DiagnosticDetail | null>(null)
const detailLoading = ref(false)
const detailError = ref('')
const deleteTarget = ref<DiagnosticMetadata | null>(null)
const deleting = ref(false)
let listRequest: AbortController | undefined
let detailRequest: AbortController | undefined

const columns = computed(() => [
  { key: 'id', label: t('admin.diagnostics.id') },
  { key: 'user_id', label: t('admin.diagnostics.account') },
  { key: 'size_bytes', label: t('admin.diagnostics.size') },
  { key: 'created_at', label: t('admin.diagnostics.created') },
  { key: 'expires_at', label: t('admin.diagnostics.expires') },
  { key: 'actions', label: t('common.actions') }
])

async function load() {
  listRequest?.abort()
  const request = new AbortController()
  listRequest = request
  loading.value = true
  error.value = ''
  try {
    const result = await listDiagnostics(page.value, pageSize.value, query.value.trim().toLowerCase(), request.signal)
    if (request.signal.aborted) return
    items.value = result.items
    total.value = result.total
  } catch {
    if (!request.signal.aborted) error.value = t('admin.diagnostics.loadFailed')
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}

function search() { page.value = 1; void load() }
function changePage(value: number) { page.value = value; void load() }
function changeSize(value: number) { pageSize.value = value; page.value = 1; void load() }

function closeDetail() {
  detailRequest?.abort()
  selected.value = null
  detail.value = null
}

async function view(row: DiagnosticMetadata) {
  closeDetail()
  const request = new AbortController()
  detailRequest = request
  selected.value = row
  detailLoading.value = true
  detailError.value = ''
  try {
    const result = await readDiagnostic(row.id, request.signal)
    if (!request.signal.aborted) detail.value = result
  } catch {
    if (!request.signal.aborted) detailError.value = t('admin.diagnostics.readFailed')
  } finally {
    if (!request.signal.aborted) detailLoading.value = false
  }
}

async function remove() {
  const target = deleteTarget.value
  if (!target || deleting.value) return
  deleting.value = true
  deleteTarget.value = null
  try {
    await deleteDiagnostic(target.id)
    if (selected.value?.id === target.id) closeDetail()
    await load()
  } catch {
    app.showError(t('admin.diagnostics.deleteFailed'))
  } finally {
    deleting.value = false
  }
}

onMounted(load)
onUnmounted(() => { listRequest?.abort(); detailRequest?.abort() })
</script>
