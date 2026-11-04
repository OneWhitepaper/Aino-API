import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import WorkspaceModels from '../WorkspaceModels.vue'

const mocks = vi.hoisted(() => ({ channels: vi.fn(), plaza: vi.fn() }))
const catalog = ref<{ source: 'channels' | 'plaza'; to: { path: string } } | null>(null)
vi.mock('@/api/channels', () => ({ default: { getAvailable: mocks.channels } }))
vi.mock('@/api/modelPlaza', () => ({ getModelPlaza: mocks.plaza }))
vi.mock('@/composables/useWorkspaceLinks', () => ({ useWorkspaceLinks: () => ({ catalog }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const render = () => mount(WorkspaceModels, { global: { stubs: {
  RouterLink: { props: ['to'], template: '<a><slot /></a>' }, Icon: true, LoadingSpinner: true,
} } })

beforeEach(() => {
  vi.clearAllMocks()
  catalog.value = null
})

describe('workspace model discovery', () => {
  it('does not request disabled catalogs or show invented models', async () => {
    const wrapper = render()
    await flushPromises()
    expect(mocks.channels).not.toHaveBeenCalled()
    expect(mocks.plaza).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('workspace.modelsUnavailable')
    wrapper.unmount()
  })

  it('deduplicates accessible model results and provides recovery for a failed request', async () => {
    catalog.value = { source: 'channels', to: { path: '/available-channels' } }
    const model = { name: 'test-model', platform: 'openai' }
    mocks.channels.mockRejectedValueOnce(new Error('offline')).mockResolvedValue([{ platforms: [{ supported_models: [model, model] }] }])
    const wrapper = render()
    await flushPromises()
    expect(wrapper.text()).toContain('workspace.catalogError')
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.workspace-model-row')).toHaveLength(1)
    expect(wrapper.text()).toContain('test-model')
    expect(mocks.plaza).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('aborts pending discovery and ignores late results when the catalog is disabled', async () => {
    catalog.value = { source: 'plaza', to: { path: '/model-plaza' } }
    let finish!: (data: unknown) => void
    mocks.plaza.mockImplementation(() => new Promise(resolve => { finish = resolve }))
    const wrapper = render()
    const signal = mocks.plaza.mock.calls[0][0].signal
    catalog.value = null
    await flushPromises()
    expect(signal.aborted).toBe(true)
    finish({ groups: [{ models: [{ name: 'stale', platform: 'openai' }] }] })
    await flushPromises()
    expect(wrapper.text()).not.toContain('stale')
    expect(wrapper.text()).toContain('workspace.modelsUnavailable')
    wrapper.unmount()
  })
})
