import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { ref } from 'vue'
import BatchImageGuideView from '../BatchImageGuideView.vue'

const { listKeys, listModels, listJobs, showError } = vi.hoisted(() => ({
  listKeys: vi.fn(), listModels: vi.fn(), listJobs: vi.fn(), showError: vi.fn(),
}))

vi.mock('@/api', () => ({ keysAPI: { list: listKeys } }))
vi.mock('@/api/batchImage', async importOriginal => ({
  ...await importOriginal<typeof import('@/api/batchImage')>(),
  listBatchImageModels: listModels,
  listBatchImageJobs: listJobs,
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({
  showError, showSuccess: vi.fn(), fetchPublicSettings: vi.fn(),
}) }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key, locale: ref('en') }),
}))

let wrapper: VueWrapper | undefined
beforeEach(() => {
  vi.clearAllMocks()
  listModels.mockResolvedValue({ data: [{ id: 'gemini-image' }] })
  listJobs.mockImplementation((key: string) => key ? Promise.resolve({ data: [], has_more: false }) : Promise.reject(new Error('API_KEY_REQUIRED')))
})
afterEach(() => { wrapper?.unmount() })

describe('batch image personal key selection', () => {
  it('loads models and history only with personal secrets even when an older server includes managed keys', async () => {
    const group = { platform: 'gemini', allow_batch_image_generation: true }
    listKeys.mockResolvedValue({ items: [
      { id: 1, key: '', name: 'Desktop', desktop_managed: true, status: 'active', group },
      { id: 2, key: 'sk-internal', name: 'Desktop legacy', desktop_managed: true, status: 'active', group },
      { id: 3, key: '', name: 'Empty', status: 'active', group },
      { id: 4, key: 'sk-personal', name: 'Personal', status: 'active', group },
    ] })
    wrapper = mount(BatchImageGuideView, {
      global: { stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /><slot name="pagination" /></div>' },
        BaseDialog: true, DataTable: true, Select: true, SearchInput: true, Icon: true, 'i18n-t': true,
      } },
    })
    await flushPromises()
    expect(listModels.mock.calls.map(call => call[0])).toEqual(['sk-personal'])
    expect(listJobs.mock.calls.map(call => call[0])).toEqual(['sk-personal'])
    expect(showError).not.toHaveBeenCalled()
    expect(listKeys).toHaveBeenCalledWith(1, 100, expect.objectContaining({ desktop_managed: false }))
  })
})
