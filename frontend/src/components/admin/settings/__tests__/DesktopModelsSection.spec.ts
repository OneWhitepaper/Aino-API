import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import DesktopModelsSection from '../DesktopModelsSection.vue'
import type { DesktopSettings } from '@/api/admin/settings'

const { getAll } = vi.hoisted(() => ({ getAll: vi.fn() }))
vi.mock('@/api/admin/groups', () => ({ getAll }))
vi.mock('vue-i18n', async (original) => ({
  ...(await original<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))

function settings(): DesktopSettings {
  return { enabled: false, models: [], default_model_id: null, credential_ttl_seconds: 3600 }
}

enableAutoUnmount(afterEach)

describe('DesktopModelsSection', () => {
  it('offers an image default separately and exposes the Images protocol without Agent verification', async () => {
    const value = { ...settings(), default_image_model_id: 'image', models: [{ id: 'image', group_id: 17, model: 'fixture-image', display_name: 'Image', provider_label: 'Aino', platform: 'openai', api_mode: 'images_generations', sort_order: 0, agent_verified: false, context_window: null, max_output_tokens: null, capabilities: { tools: false, vision: false, reasoning: false, image_generation: true } }] } as unknown as DesktopSettings
    const wrapper = mount(DesktopModelsSection, { props: { modelValue: value } })
    await flushPromises()
    const chatDefault = wrapper.findAllComponents({ name: 'Select' }).find(select => select.props('id') === 'desktop-default-model')!
    expect(chatDefault.props('options')).not.toContainEqual({ value: 'image', label: 'Image' })
    await wrapper.get('[data-testid="desktop-model-purpose-image"]').trigger('click')
    const imageDefault = wrapper.findAllComponents({ name: 'Select' }).find(select => select.props('id') === 'desktop-default-image-model')
    expect(imageDefault).toBeDefined()
    expect(imageDefault!.props('options')).toContainEqual({ value: 'image', label: 'Image' })
    await imageDefault!.vm.$emit('update:modelValue', 'image')
    expect(wrapper.emitted('update:modelValue')!.at(-1)![0]).toMatchObject({ default_image_model_id: 'image' })
    await wrapper.get('[data-testid="desktop-model-remove"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')!.at(-1)![0]).toMatchObject({ default_image_model_id: null })
  })

  it('separates text and image catalogs while keeping unsaved edits and defaults in both', async () => {
    const chat = { id: 'chat', group_id: 17, model: 'chat-wire', display_name: 'Text model', provider_label: 'Aino', platform: 'openai', api_mode: 'responses', sort_order: 0, agent_verified: true, context_window: 32000, max_output_tokens: 4000, capabilities: { tools: true, vision: false, reasoning: false } }
    const image = { ...chat, id: 'image', model: 'image-wire', display_name: 'Image model', api_mode: 'images_generations', agent_verified: false, capabilities: { tools: false, vision: false, reasoning: false, image_generation: true } }
    const value = { ...settings(), models: [chat, image], default_model_id: 'chat', default_image_model_id: 'image' } as DesktopSettings
    const wrapper = mount(DesktopModelsSection, { attachTo: document.body, props: { modelValue: value, 'onUpdate:modelValue': (next: DesktopSettings) => wrapper.setProps({ modelValue: next }) } })
    await flushPromises()
    const cards = () => wrapper.findAll('[data-testid="desktop-model-card"]').filter(card => card.isVisible())
    expect(cards().map(card => card.text())).toHaveLength(1)
    expect(cards()[0].text()).toContain('Text model')
    await wrapper.get('[data-testid="desktop-model-purpose-image"]').trigger('click')
    expect(cards()).toHaveLength(1)
    expect(cards()[0].text()).toContain('Image model')
    await cards()[0].get('[data-testid="desktop-model-toggle"]').trigger('click')
    await cards()[0].get('[data-testid="desktop-model-name"]').setValue('Edited image')
    const protocol = wrapper.findAllComponents({ name: 'Select' }).find(select => select.props('id') === 'desktop-protocol-1')!
    expect(protocol.props('options')).toEqual([{ value: 'images_generations', label: 'Images API' }])
    expect(cards()[0].findAll('[data-image-quality]')).toHaveLength(6)
    await cards()[0].get('[data-image-quality="high"]').setValue(true)
    expect(wrapper.props('modelValue').models[1].capabilities.image_quality_options).toEqual(['high'])
    await cards()[0].get('[data-image-quality="high"]').setValue(false)
    expect(wrapper.props('modelValue').models[1].capabilities.image_quality_options).toEqual([])
    await wrapper.get('[data-testid="desktop-model-purpose-chat"]').trigger('click')
    expect(cards()[0].text()).toContain('Text model')
    expect(wrapper.props('modelValue')).toMatchObject({ default_model_id: 'chat', default_image_model_id: 'image', models: [chat, { ...image, display_name: 'Edited image' }] })
    await wrapper.get('[data-testid="desktop-model-purpose-image"]').trigger('click')
    await wrapper.get('[data-testid="desktop-model-add"]').trigger('click')
    expect(wrapper.props('modelValue').models[2]).toMatchObject({ api_mode: 'images_generations', capabilities: { image_generation: true, tools: false } })
    expect(wrapper.props('modelValue').models[0]).toEqual(chat)
  })

  it('reveals an invalid field in the other catalog instead of trapping save on a hidden tab', async () => {
    const value = { ...settings(), models: [{ id: 'image', group_id: 17, model: 'image-wire', display_name: '', provider_label: 'Aino', platform: 'openai', api_mode: 'images_generations', sort_order: 0, agent_verified: false, context_window: null, max_output_tokens: null, capabilities: { tools: false, vision: false, reasoning: false, image_generation: true } }] } as DesktopSettings
    const wrapper = mount(DesktopModelsSection, { attachTo: document.body, props: { modelValue: value } })
    await flushPromises()
    expect(wrapper.get('[data-testid="desktop-model-name"]').isVisible()).toBe(false)
    const input = wrapper.get('[data-testid="desktop-model-name"]').element as HTMLInputElement
    expect(input.checkValidity()).toBe(false)
    await flushPromises()
    expect(wrapper.get('[data-testid="desktop-model-purpose-image"]').attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-testid="desktop-model-name"]').isVisible()).toBe(true)
    expect(document.activeElement).toBe(input)
  })
  beforeEach(() => {
    getAll.mockReset().mockResolvedValue([{ id: 17, name: 'Fixture group', platform: 'openai', status: 'active' }])
  })

  it('collapses saved models, expands their editor, and finds models by ID', async () => {
    const value: DesktopSettings = { ...settings(), models: [{ id: 'saved', group_id: 17, model: 'fixture-v1', display_name: 'Saved model', provider_label: 'Fixture', platform: 'openai', api_mode: 'responses', sort_order: 0, agent_verified: false, context_window: null, max_output_tokens: null, capabilities: { tools: false, vision: false, reasoning: false } }] }
    const wrapper = mount(DesktopModelsSection, { attachTo: document.body, props: { modelValue: value } })
    await flushPromises()
    expect(wrapper.find('[data-testid="desktop-model-toggle"]').exists()).toBe(true)
    expect(wrapper.get('[data-testid="desktop-model-toggle"]').attributes('aria-expanded')).toBe('false')
    await wrapper.get('[data-testid="desktop-model-toggle"]').trigger('click')
    expect(wrapper.get('[data-testid="desktop-model-name"]').isVisible()).toBe(true)
    await wrapper.get('[data-testid="desktop-model-collapse-all"]').trigger('click')
    expect(wrapper.get('[data-testid="desktop-model-toggle"]').attributes('aria-expanded')).toBe('false')
    expect(wrapper.get('#desktop-model-editor-0').attributes('style')).toContain('display: none')
    await wrapper.get('[data-testid="desktop-model-search"]').setValue('not-present')
    expect(wrapper.get('[data-testid="desktop-model-card"]').isVisible()).toBe(false)
    await wrapper.get('[data-testid="desktop-model-search"]').setValue('fixture-v1')
    expect(wrapper.get('[data-testid="desktop-model-card"]').isVisible()).toBe(true)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('offers automatic model discovery alongside manual creation', async () => {
    const wrapper = mount(DesktopModelsSection, { props: { modelValue: settings() } })
    await flushPromises()
    expect(wrapper.find('[data-testid="desktop-model-import-open"]').exists()).toBe(true)
    await wrapper.get('[data-testid="desktop-model-import-open"]').trigger('click')
    expect(wrapper.find('[data-testid="desktop-model-import"]').exists()).toBe(true)
  })

  it('reveals and focuses invalid fields even when their model is collapsed and filtered out', async () => {
    const value: DesktopSettings = { ...settings(), models: [{ id: 'saved', group_id: 17, model: 'fixture', display_name: '', provider_label: 'Fixture', platform: 'openai', api_mode: 'responses', sort_order: 0, agent_verified: false, context_window: null, max_output_tokens: null, capabilities: { tools: false, vision: false, reasoning: false } }] }
    const wrapper = mount(DesktopModelsSection, { attachTo: document.body, props: { modelValue: value } })
    await flushPromises()
    await wrapper.get('[data-testid="desktop-model-search"]').setValue('not-present')
    const input = wrapper.get('[data-testid="desktop-model-name"]').element as HTMLInputElement
    expect(input.checkValidity()).toBe(false)
    await flushPromises()
    expect(wrapper.get('[data-testid="desktop-model-toggle"]').attributes('aria-expanded')).toBe('true')
    expect(wrapper.get('[data-testid="desktop-model-search"]').element).toHaveProperty('value', '')
    expect(wrapper.get('[data-testid="desktop-model-name"]').isVisible()).toBe(true)
    expect(document.activeElement).toBe(input)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('keeps new capabilities unverified and clears the default when its entry is removed', async () => {
    const wrapper = mount(DesktopModelsSection, { props: { modelValue: settings() } })
    await flushPromises()
    await wrapper.get('[data-testid="desktop-model-add"]').trigger('click')
    const draft = wrapper.emitted('update:modelValue')!.at(-1)![0] as DesktopSettings
    expect(draft.models[0].capabilities).toEqual({ tools: false, vision: false, reasoning: false })
    expect(draft.models[0].agent_verified).toBe(false)
    const selected = { ...draft, default_model_id: 'fixture', models: [{ ...draft.models[0], id: 'fixture', group_id: 17, agent_verified: true, capabilities: { tools: true, vision: false, reasoning: false } }] }
    await wrapper.setProps({ modelValue: selected })
    await wrapper.get('[data-testid="desktop-model-remove"]').trigger('click')
    const removed = wrapper.emitted('update:modelValue')!.at(-1)![0] as DesktopSettings
    expect(removed.models).toEqual([])
    expect(removed.default_model_id).toBeNull()
    expect(selected.models).toHaveLength(1)
  })

  it('keeps configured models on a group load failure and retries only on an explicit action', async () => {
    getAll.mockRejectedValueOnce(new Error('offline'))
    const value: DesktopSettings = { ...settings(), models: [{ id: 'saved', group_id: 17, model: 'fixture', display_name: 'Saved model', provider_label: 'Fixture', platform: 'openai', api_mode: 'responses', sort_order: 0, agent_verified: false, context_window: null, max_output_tokens: null, capabilities: { tools: false, vision: false, reasoning: false } }] }
    const wrapper = mount(DesktopModelsSection, { props: { modelValue: value } })
    await flushPromises()
    expect(getAll).toHaveBeenCalledTimes(1)
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.get('[data-testid="desktop-model-name"]').element).toHaveProperty('value', 'Saved model')
    await wrapper.get('[data-testid="desktop-groups-retry"]').trigger('click')
    await flushPromises()
    expect(getAll).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-testid="desktop-groups-retry"]').exists()).toBe(false)
  })
})
