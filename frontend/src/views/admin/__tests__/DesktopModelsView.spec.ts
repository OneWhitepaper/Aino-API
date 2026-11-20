import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { h } from 'vue'
import DesktopModelsView from '../DesktopModelsView.vue'
import type { DesktopModelEntry, DesktopSettings } from '@/api/admin/settings'
import { getDesktopSettings, updateDesktopSettings } from '@/api/admin/settings'
import { getAllIncludingInactive } from '@/api/admin/groups'
import { list, syncUpstreamModels } from '@/api/admin/accounts'
import type { AdminGroup } from '@/types'

vi.mock('@/api/admin/settings', async original => ({ ...await original<typeof import('@/api/admin/settings')>(), getDesktopSettings: vi.fn(), updateDesktopSettings: vi.fn() }))
vi.mock('@/api/admin/groups', async original => ({ ...await original<typeof import('@/api/admin/groups')>(), getAllIncludingInactive: vi.fn() }))
vi.mock('@/api/admin/accounts', async original => ({ ...await original<typeof import('@/api/admin/accounts')>(), list: vi.fn(), syncUpstreamModels: vi.fn() }))
vi.mock('vue-i18n', async original => ({
  ...await original<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key })
}))
enableAutoUnmount(afterEach)

function model(id: string, extra: Partial<DesktopModelEntry> = {}): DesktopModelEntry {
  return { id, group_id: 1, model: `${id}-wire`, display_name: id, provider_label: 'Provider', platform: 'openai', api_mode: 'responses', sort_order: 0, agent_verified: true, context_window: 128000, max_output_tokens: 8192, capabilities: { tools: true, vision: true, reasoning: false }, ...extra }
}
function catalog(): DesktopSettings {
  return { enabled: true, credential_ttl_seconds: 900, default_model_id: 'chat', default_image_model_id: 'image', models: [model('chat'), model('other', { sort_order: 1, agent_verified: false }), model('image', { sort_order: 2, api_mode: 'images_generations', agent_verified: false, context_window: null, max_output_tokens: null, capabilities: { tools: false, vision: false, reasoning: false, image_generation: true, image_quality_options: ['auto', 'high'] } })] }
}
const groups = [{ id: 1, name: 'Primary', platform: 'openai', status: 'active', allow_image_generation: true }, { id: 2, name: 'Composite', platform: 'composite', status: 'active', allow_image_generation: true }] as AdminGroup[]

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(getDesktopSettings).mockResolvedValue(catalog())
  vi.mocked(getAllIncludingInactive).mockResolvedValue(groups)
  vi.mocked(updateDesktopSettings).mockImplementation(async settings => JSON.parse(JSON.stringify(settings)))
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value() { this.setAttribute('open', '') } })
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value() { this.removeAttribute('open') } })
})

async function start() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: DesktopModelsView }, { path: '/elsewhere', component: { template: '<div>Elsewhere</div>' } }] })
  await router.push('/')
  await router.isReady()
  const root = mount({ render: () => h(RouterView) }, { attachTo: document.body, global: { plugins: [router, createPinia()], stubs: { transition: true, AppLayout: { template: '<div><slot /></div>' } } } })
  await flushPromises()
  return { page: root.getComponent(DesktopModelsView), router }
}

describe('real admin model workbench', () => {
  it('loads real configuration with separate catalogs, search, and independent defaults', async () => {
    const { page } = await start()
    expect(getDesktopSettings).toHaveBeenCalledOnce()
    expect(page.findAll('[data-testid="model-row"]')).toHaveLength(2)
    expect((page.get('[data-testid="default-select"]').element as HTMLSelectElement).value).toBe('chat')
    await page.get('[data-testid="model-search"]').setValue('no-match')
    expect(page.findAll('[data-testid="model-row"]')).toHaveLength(0)
    await page.get('[data-testid="tab-image"]').trigger('click')
    expect(page.findAll('[data-testid="model-row"]')).toHaveLength(1)
    expect((page.get('[data-testid="default-select"]').element as HTMLSelectElement).value).toBe('image')
    await page.get('[data-testid="tab-chat"]').trigger('click')
    expect(page.findAll('[data-testid="model-row"]')).toHaveLength(2)
    expect(updateDesktopSettings).not.toHaveBeenCalled()
  })

  it('retains edits on save failure and saves the complete catalog only after explicit save', async () => {
    const { page } = await start()
    await page.get('[data-testid="model-edit"]').trigger('click')
    await page.get('[data-testid="editor-display_name"]').setValue('Renamed model')
    await page.get('.dm-panel-form').trigger('submit')
    expect(updateDesktopSettings).not.toHaveBeenCalled()
    vi.mocked(updateDesktopSettings).mockRejectedValueOnce({ status: 400, message: 'catalog group no longer exists' })
    await page.get('[data-testid="save-catalog"]').trigger('click')
    await flushPromises()
    expect(page.get('.dm-save-error').text()).toContain('saveFailed')
    expect(page.get('.dm-save-error').text()).toContain('catalog group no longer exists')
    expect(page.text()).toContain('Renamed model')
    await page.get('[data-testid="save-catalog"]').trigger('click')
    await flushPromises()
    expect(updateDesktopSettings).toHaveBeenLastCalledWith(expect.objectContaining({ default_model_id: 'chat', default_image_model_id: 'image', credential_ttl_seconds: 900, models: expect.arrayContaining([expect.objectContaining({ id: 'chat', display_name: 'Renamed model', agent_verified: true })]) }))
    expect(page.find('[data-testid="save-catalog"]').exists()).toBe(false)
  })

  it('invalidates route verification and clears only the affected default', async () => {
    const { page } = await start()
    await page.get('[data-testid="model-edit"]').trigger('click')
    expect(page.get('#dm-id').attributes('readonly')).toBeDefined()
    await page.get('[data-testid="editor-model"]').setValue('changed-wire')
    expect(page.get('[data-testid="editor-verified"]').attributes('disabled')).toBeDefined()
    await page.get('.dm-panel-form').trigger('submit')
    await page.get('[data-testid="save-catalog"]').trigger('click')
    await flushPromises()
    const sent = vi.mocked(updateDesktopSettings).mock.calls[0][0]
    expect(sent.default_model_id).toBeNull()
    expect(sent.default_image_model_id).toBe('image')
    expect(sent.models[0]).toMatchObject({ id: 'chat', model: 'changed-wire', agent_verified: false })
  })

  it('preserves image qualities and temporary credential lifetime alongside text settings', async () => {
    const { page } = await start()
    await page.get('[data-testid="tab-image"]').trigger('click')
    await page.get('[data-testid="model-edit"]').trigger('click')
    expect(page.get('[data-testid="editor-protocol"]').attributes('disabled')).toBeDefined()
    await page.get('[data-image-quality="max"]').setValue(true)
    await page.get('.dm-panel-form').trigger('submit')
    await page.get('[data-testid="connection-open"]').trigger('click')
    await page.get('[data-testid="connection-ttl"]').setValue(1200)
    await page.get('.dm-connection-form').trigger('submit')
    await page.get('[data-testid="save-catalog"]').trigger('click')
    await flushPromises()
    const sent = vi.mocked(updateDesktopSettings).mock.calls[0][0]
    expect(sent).toMatchObject({ default_model_id: 'chat', default_image_model_id: 'image', credential_ttl_seconds: 1200 })
    expect(sent.models[2].capabilities.image_quality_options).toEqual(['auto', 'high', 'max'])
  })

  it('validates additions without changing the catalog and confirms discarding editor fields', async () => {
    const { page } = await start()
    await page.get('[data-testid="model-add"]').trigger('click')
    await page.get('.dm-panel-form').trigger('submit')
    expect(page.findAll('[aria-invalid="true"]').length).toBeGreaterThan(0)
    expect(page.find('[data-testid="save-catalog"]').exists()).toBe(false)
    await page.get('[data-testid="editor-display_name"]').setValue('Not applied')
    await page.get('.dm-panel-header .dm-icon-button').trigger('click')
    expect(page.find('[data-testid="editor-discard"]').exists()).toBe(true)
    await page.get('[data-testid="editor-discard"]').trigger('click')
    expect(page.find('.dm-panel-form').exists()).toBe(false)
    expect(page.findAll('[data-testid="model-row"]')).toHaveLength(2)
  })

  it('uses the real discovery workflow with a searchable group dropdown inside the drawer', async () => {
    vi.mocked(list).mockResolvedValue({ items: [{ id: 10, name: 'Source', platform: 'openai', type: 'apikey' }], pages: 1 } as never)
    vi.mocked(syncUpstreamModels).mockResolvedValue({ models: ['chat-wire', 'new-model'], metadata: {} } as never)
    const { page } = await start()
    await page.get('[data-testid="import-open"]').trigger('click')
    await page.get('#desktop-import-group').trigger('click')
    await flushPromises()
    const dropdown = document.querySelector('.dm-drawer .select-dropdown-portal')
    expect(dropdown).not.toBeNull()
    ;(dropdown!.querySelector('[role="option"]') as HTMLElement).click()
    await flushPromises()
    expect(list).toHaveBeenCalledWith(1, 100, { group: '1', status: 'active', lite: '1' })
    await page.get('[data-testid="desktop-import-fetch"]').trigger('click')
    await flushPromises()
    expect(syncUpstreamModels).toHaveBeenCalledWith(10)
    await page.get('[data-testid="desktop-import-confirm"]').trigger('click')
    // chat-wire uses a different protocol (chat_completions), so it is a valid distinct route.
    expect(page.findAll('[data-testid="model-row"]')).toHaveLength(4)
    await page.get('.dm-drawer .dm-panel-header .dm-icon-button').trigger('click')
    await page.get('[data-testid="save-catalog"]').trigger('click')
    await flushPromises()
    const imported = vi.mocked(updateDesktopSettings).mock.calls[0][0].models.find(model => model.model === 'new-model')!
    expect(imported).toMatchObject({ group_id: 1, api_mode: 'chat_completions', agent_verified: false, capabilities: { tools: false } })
  })

  it('removes selected models together and clears the text default while retaining images', async () => {
    const { page } = await start()
    await page.get('thead input[type="checkbox"]').setValue(true)
    await page.get('[data-testid="remove-selected"]').trigger('click')
    await page.get('[data-testid="remove-confirm"]').trigger('click')
    expect(page.findAll('[data-testid="model-row"]')).toHaveLength(0)
    await page.get('[data-testid="save-catalog"]').trigger('click')
    await flushPromises()
    const sent = vi.mocked(updateDesktopSettings).mock.calls[0][0]
    expect(sent.models.map(model => model.id)).toEqual(['image'])
    expect(sent.default_model_id).toBeNull()
    expect(sent.default_image_model_id).toBe('image')
  })

  it('keeps desktop preview selection separate from the administrator default', async () => {
    const { page } = await start()
    await page.get('[data-testid="preview-open"]').trigger('click')
    await page.findAll('[data-testid="preview-model"]')[1].trigger('click')
    await page.get('.dm-drawer .dm-panel-header .dm-icon-button').trigger('click')
    expect((page.get('[data-testid="default-select"]').element as HTMLSelectElement).value).toBe('chat')
    expect(page.find('[data-testid="save-catalog"]').exists()).toBe(false)
  })

  it('does not replace failed reads with a mock catalog, and offers retry', async () => {
    vi.mocked(getDesktopSettings).mockRejectedValueOnce(new Error('network'))
    const { page } = await start()
    expect(page.findAll('[data-testid="model-row"]')).toHaveLength(0)
    expect(page.get('[role="alert"]').text()).toContain('loadFailed')
    await page.get('[role="alert"] button').trigger('click')
    await flushPromises()
    expect(page.findAll('[data-testid="model-row"]')).toHaveLength(2)
  })

  it('preserves the loaded catalog when groups fail and recovers editing after retry', async () => {
    vi.mocked(getAllIncludingInactive).mockRejectedValueOnce(new Error('groups unavailable'))
    const { page } = await start()
    expect(page.findAll('[data-testid="model-row"]')).toHaveLength(2)
    expect(page.get('[data-testid="model-add"]').attributes('disabled')).toBeDefined()
    await page.get('[data-testid="groups-retry"]').trigger('click')
    await flushPromises()
    expect(page.get('[data-testid="model-add"]').attributes('disabled')).toBeUndefined()
  })

  it('guards unsaved navigation and restores the saved document on discard', async () => {
    const { page, router } = await start()
    await page.get('[data-testid="catalog-toggle"]').trigger('click')
    const navigation = router.push('/elsewhere')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/')
    expect(page.find('[data-testid="leave-confirm"]').exists()).toBe(true)
    await page.get('.dm-dialog[open] .dm-panel-footer .dm-secondary').trigger('click')
    await navigation
    expect(router.currentRoute.value.path).toBe('/')
    await page.get('.dm-savebar .dm-secondary').trigger('click')
    await page.get('[data-testid="discard-confirm"]').trigger('click')
    expect(page.get('[data-testid="catalog-toggle"]').attributes('aria-checked')).toBe('true')
    expect(page.find('[data-testid="save-catalog"]').exists()).toBe(false)
    await router.push('/elsewhere')
    expect(router.currentRoute.value.path).toBe('/elsewhere')
  })
})
