import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import DiagnosticsView from '../DiagnosticsView.vue'

const api = vi.hoisted(() => ({ listDiagnostics: vi.fn(), readDiagnostic: vi.fn(), deleteDiagnostic: vi.fn() }))
vi.mock('@/api/admin/diagnostics', () => api)
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
const row = { id: '22c5a21b-bfaa-476e-92fd-3e408c6effb8', user_id: 17, size_bytes: 123, created_at: '2026-10-24T00:00:00Z', expires_at: '2026-11-07T00:00:00Z' }
function render(item = row as typeof row & { kind?: string }) {
  api.listDiagnostics.mockResolvedValue({ items: [item], total: 1, page: 1, page_size: 20 })
  return mount(DiagnosticsView, { global: {
    stubs: {
      AppLayout: { template: '<div><slot /></div>' },
      TablePageLayout: { template: '<div><slot name="filters"/><slot name="table"/><slot name="pagination"/></div>' },
      DataTable: { props: ['data', 'columns', 'loading'], template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-kind" :value="row.kind"/><slot name="cell-user_id" :value="row.user_id"/><slot name="cell-actions" :row="row"/></div></div>' },
      BaseDialog: { props: ['show'], emits: ['close'], template: '<div v-if="show" role="dialog"><button @click="$emit(\'close\')">Close</button><slot /></div>' },
      ConfirmDialog: true,
      Pagination: true
    }
  } })
}

describe('private diagnostics admin view', () => {
  it('distinguishes usage counts from support logs without displaying a fabricated account', async () => {
    const wrapper = render({ ...row, kind: 'usage_statistics', user_id: 0 })
    await flushPromises()
    expect(wrapper.text()).toContain('admin.diagnostics.statistics')
    expect(wrapper.text()).toContain('—')
    expect(wrapper.text()).not.toContain('admin.diagnostics.logs')
    wrapper.unmount()
    vi.clearAllMocks()
  })
  it('fetches content only after View and renders log markup as plain text', async () => {
    api.readDiagnostic.mockResolvedValue({ metadata: row, files: { report: '<img src=x onerror=alert(1)> synthetic log' } })
    const wrapper = render()
    await flushPromises()
    expect(api.readDiagnostic).not.toHaveBeenCalled()
    await wrapper.find('[data-diagnostic-view]').trigger('click')
    await flushPromises()
    expect(api.readDiagnostic).toHaveBeenCalledWith(row.id, expect.any(AbortSignal))
    expect(wrapper.find('pre').text()).toContain('<img src=x onerror=alert(1)>')
    expect(wrapper.find('img').exists()).toBe(false)
    wrapper.unmount()
    vi.clearAllMocks()
  })
  it('closing a pending detail does not resurrect its contents', async () => {
    let resolve!: (value: unknown) => void
    api.readDiagnostic.mockImplementation(() => new Promise(r => { resolve = r }))
    const wrapper = render()
    await flushPromises()
    await wrapper.find('[data-diagnostic-view]').trigger('click')
    await wrapper.find('[role=dialog] button').trigger('click')
    resolve({ metadata: row, files: { report: 'late private contents' } })
    await flushPromises()
    expect(wrapper.find('pre').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('late private contents')
    wrapper.unmount()
    vi.clearAllMocks()
  })
})
