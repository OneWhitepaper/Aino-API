import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import DesktopModelImporter from '../DesktopModelImporter.vue'
import Select from '@/components/common/Select.vue'
import type { AdminGroup } from '@/types'
import type { DesktopModelEntry } from '@/api/admin/settings'

const { list, syncUpstreamModels } = vi.hoisted(() => ({ list: vi.fn(), syncUpstreamModels: vi.fn() }))
vi.mock('@/api/admin/accounts', () => ({ list, syncUpstreamModels }))
vi.mock('vue-i18n', async original => ({ ...await original<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key }) }))
const groups = [{ id: 17, name: 'Group', platform: 'openai', status: 'active', model_allowlist: { enabled: true, models: ['allowed*'] } }, { id: 18, name: 'Other', platform: 'anthropic', status: 'active' }] as AdminGroup[]
const source = { id: 9, name: 'Source', platform: 'openai', type: 'apikey', status: 'active' }
const saved: DesktopModelEntry = { id: 'saved', model: 'allowed-existing', display_name: 'Preserved', provider_label: 'Existing', group_id: 17, platform: 'openai', api_mode: 'chat_completions', sort_order: 0, agent_verified: true, capabilities: { tools: true, vision: false, reasoning: false }, context_window: null, max_output_tokens: null }

function mountImporter(models: DesktopModelEntry[] = []) {
  return mount(DesktopModelImporter, { props: { groups, groupsLoading: false, models } })
}
async function selectGroup(wrapper: ReturnType<typeof mountImporter>, id: number) {
  wrapper.findAllComponents(Select).find(select => select.props('id') === 'desktop-import-group')!.vm.$emit('update:modelValue', id)
  await flushPromises()
}

describe('DesktopModelImporter', () => {
  beforeEach(() => {
    list.mockReset().mockResolvedValue({ items: [source], pages: 1 })
    syncUpstreamModels.mockReset().mockResolvedValue({ models: ['allowed-existing', 'allowed-new', 'blocked'], metadata: { 'allowed-new': { id: 'allowed-new', display_name: 'New model', context_window: 32000, max_output_tokens: 4000, reasoning: true, input_modalities: ['image'] } } })
  })

  it('previews real discovery results and imports only selected permitted new models', async () => {
    const wrapper = mountImporter([saved])
    await selectGroup(wrapper, 17)
    await wrapper.get('[data-testid="desktop-import-fetch"]').trigger('click')
    await flushPromises()
    const choices = wrapper.findAll('[data-testid="desktop-import-choice"]')
    expect(choices.map(choice => (choice.element as HTMLInputElement).disabled)).toEqual([true, false, true])
    expect(wrapper.emitted('import')).toBeUndefined()
    await wrapper.get('[data-testid="desktop-import-confirm"]').trigger('click')
    const imported = wrapper.emitted('import')![0][0] as DesktopModelEntry[]
    expect(imported).toHaveLength(1)
    expect(imported[0]).toMatchObject({ model: 'allowed-new', display_name: 'New model', context_window: 32000, max_output_tokens: 4000, group_id: 17, agent_verified: false, capabilities: { tools: false, vision: true, reasoning: true } })
    expect(saved).toMatchObject({ display_name: 'Preserved', agent_verified: true, capabilities: { tools: true } })
    await wrapper.setProps({ models: [saved, ...imported] })
    expect((wrapper.get('[data-testid="desktop-import-confirm"]').element as HTMLButtonElement).disabled).toBe(true)
  })

  it('ignores a discovery response after the selected group changes', async () => {
    let resolve!: (value: { models: string[] }) => void
    syncUpstreamModels.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const wrapper = mountImporter()
    await selectGroup(wrapper, 17)
    await wrapper.get('[data-testid="desktop-import-fetch"]').trigger('click')
    list.mockResolvedValueOnce({ items: [{ ...source, id: 10, platform: 'anthropic' }], pages: 1 })
    await selectGroup(wrapper, 18)
    resolve({ models: ['old-source-model'] })
    await flushPromises()
    expect(wrapper.find('[data-testid="desktop-import-choice"]').exists()).toBe(false)
    expect(wrapper.emitted('import')).toBeUndefined()
    const protocol = wrapper.findAllComponents(Select).find(select => select.props('id') === 'desktop-import-protocol')!
    expect(protocol.props('modelValue')).toBe('auto')
    await wrapper.get('[data-testid="desktop-import-fetch"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="desktop-import-confirm"]').trigger('click')
    expect((wrapper.emitted('import')![0][0] as DesktopModelEntry[]).every(entry => entry.group_id === 18 && entry.api_mode === 'anthropic_messages')).toBe(true)
  })

  it('preserves the catalog on discovery failure and permits an explicit retry', async () => {
    syncUpstreamModels.mockRejectedValueOnce(new Error('offline'))
    const wrapper = mountImporter([saved])
    await selectGroup(wrapper, 17)
    await wrapper.get('[data-testid="desktop-import-fetch"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('fetchFailed')
    expect(wrapper.emitted('import')).toBeUndefined()
    expect(wrapper.props('models')).toEqual([saved])
    await wrapper.get('[data-testid="desktop-import-fetch"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-testid="desktop-import-choice"]')).toHaveLength(3)
  })

  it('loads all account pages and blocks oversized bulk imports', async () => {
    list.mockResolvedValueOnce({ items: [source], pages: 2 }).mockResolvedValueOnce({ items: [{ ...source, id: 10, name: 'Second page' }], pages: 2 })
    syncUpstreamModels.mockResolvedValueOnce({ models: ['allowed-a', 'allowed-b'] })
    const models = Array.from({ length: 199 }, (_, index) => ({ ...saved, id: `id-${index}`, model: `saved-${index}` }))
    const wrapper = mountImporter(models)
    await selectGroup(wrapper, 17)
    const accounts = wrapper.findAllComponents(Select).find(select => select.props('id') === 'desktop-import-account')!
    expect(accounts.props('options')).toHaveLength(3)
    accounts.vm.$emit('update:modelValue', 10)
    await flushPromises()
    await wrapper.get('[data-testid="desktop-import-fetch"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('input[type="checkbox"]').filter(input => (input.element as HTMLInputElement).checked)).toHaveLength(1)
    await wrapper.get('[data-testid="desktop-import-select-all"]').trigger('click')
    expect((wrapper.get('[data-testid="desktop-import-confirm"]').element as HTMLButtonElement).disabled).toBe(true)
    expect(wrapper.emitted('import')).toBeUndefined()
  })

  it('keeps image imports on the Images API after account selection', async () => {
    const wrapper = mount(DesktopModelImporter, { props: { groups, groupsLoading: false, models: [saved], purpose: 'image' } })
    await selectGroup(wrapper, 17)
    const protocol = wrapper.findAllComponents(Select).find(select => select.props('id') === 'desktop-import-protocol')!
    expect(protocol.props('modelValue')).toBe('images_generations')
    expect(protocol.props('options')).toEqual([{ value: 'images_generations', label: 'Images API' }])
    await wrapper.get('[data-testid="desktop-import-fetch"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="desktop-import-confirm"]').trigger('click')
    const imported = wrapper.emitted('import')![0][0] as DesktopModelEntry[]
    expect(imported.length).toBeGreaterThan(0)
    expect(imported.every(entry => entry.api_mode === 'images_generations' && entry.capabilities.image_generation === true && !entry.capabilities.tools)).toBe(true)
    expect(saved.api_mode).toBe('chat_completions')
  })

  it.each(['chat', 'image'] as const)('discovers the whole %s group by default and keeps deduplicated results when one source fails', async purpose => {
    list.mockResolvedValueOnce({ items: [source, { ...source, id: 10 }, { ...source, id: 11 }], pages: 1 })
    syncUpstreamModels.mockImplementation(async (id: number) => {
      if (id === 10) throw new Error('unavailable account')
      return { models: id === 9 ? ['allowed-shared', 'allowed-first'] : ['allowed-shared', 'allowed-last'] }
    })
    const wrapper = mount(DesktopModelImporter, { props: { groups, groupsLoading: false, models: [], purpose } })
    await selectGroup(wrapper, 17)
    expect((wrapper.get('[data-testid="desktop-import-fetch"]').element as HTMLButtonElement).disabled).toBe(false)
    await wrapper.get('[data-testid="desktop-import-fetch"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="desktop-import-partial"]').text()).toContain('partialFailure')
    expect(wrapper.findAll('[data-testid="desktop-import-choice"]')).toHaveLength(3)
    await wrapper.get('[data-testid="desktop-import-confirm"]').trigger('click')
    const imported = wrapper.emitted('import')![0][0] as DesktopModelEntry[]
    expect(imported.map(entry => entry.model)).toEqual(['allowed-shared', 'allowed-first', 'allowed-last'])
    expect(imported.every(entry => entry.group_id === 17 && !('account_id' in entry))).toBe(true)
    expect(imported.every(entry => entry.api_mode === (purpose === 'image' ? 'images_generations' : 'chat_completions'))).toBe(true)
    expect(wrapper.get('details').attributes('open')).toBeUndefined()
  })

  it('discards in-flight discovery when protocol changes and refetches using the selected protocol', async () => {
    let resolve!: (value: { models: string[] }) => void
    syncUpstreamModels.mockImplementationOnce(() => new Promise(done => { resolve = done }))
    const wrapper = mountImporter()
    await selectGroup(wrapper, 17)
    await wrapper.get('[data-testid="desktop-import-fetch"]').trigger('click')
    wrapper.findAllComponents(Select).find(select => select.props('id') === 'desktop-import-protocol')!.vm.$emit('update:modelValue', 'responses')
    await flushPromises()
    resolve({ models: ['allowed-stale'] })
    await flushPromises()
    expect(wrapper.find('[data-testid="desktop-import-choice"]').exists()).toBe(false)
    await wrapper.get('[data-testid="desktop-import-fetch"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="desktop-import-confirm"]').trigger('click')
    const imported = wrapper.emitted('import')![0][0] as DesktopModelEntry[]
    expect(imported.every(entry => entry.api_mode === 'responses' && entry.model !== 'allowed-stale')).toBe(true)
  })

  it('selects same-named models on different routes independently', async () => {
    list.mockResolvedValueOnce({ items: [source, { ...source, id: 10, platform: 'anthropic' }], pages: 1 })
    syncUpstreamModels.mockResolvedValue({ models: ['allowed-shared'] })
    const wrapper = mount(DesktopModelImporter, { props: { groups: [{ ...groups[0], platform: 'composite' } as AdminGroup], groupsLoading: false, models: [] } })
    await selectGroup(wrapper, 17)
    await wrapper.get('[data-testid="desktop-import-fetch"]').trigger('click')
    await flushPromises()
    const choices = wrapper.findAll('[data-testid="desktop-import-choice"]')
    expect(choices).toHaveLength(2)
    await choices[0].setValue(false)
    expect((choices[1].element as HTMLInputElement).checked).toBe(true)
    await wrapper.get('[data-testid="desktop-import-confirm"]').trigger('click')
    const imported = wrapper.emitted('import')![0][0] as DesktopModelEntry[]
    expect(imported).toHaveLength(1)
    expect(imported[0]).toMatchObject({ model: 'allowed-shared', group_id: 17, platform: 'anthropic', api_mode: 'anthropic_messages' })
  })
})
