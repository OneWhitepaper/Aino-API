import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import AccountTestModal from '../AccountTestModal.vue'
import AdminAccountTestModal from '../../admin/account/AccountTestModal.vue'

const { getAvailableModels } = vi.hoisted(() => ({ getAvailableModels: vi.fn() }))

vi.mock('@/api/admin', () => ({ adminAPI: { accounts: { getAvailableModels } } }))
vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn() })
}))
vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))

const SelectStub = defineComponent({
  props: {
    modelValue: { type: String, default: '' },
    options: { type: Array, default: () => [] },
    valueKey: { type: String, default: 'value' },
    labelKey: { type: String, default: 'label' }
  },
  emits: ['update:modelValue'],
  template: `<select :value="modelValue" @change="$emit('update:modelValue', $event.target.value)">
    <option v-for="option in options" :key="option[valueKey]" :value="option[valueKey]">{{ option[labelKey] }}</option>
  </select>`
})

const account = {
  id: 54, name: 'Image API test', platform: 'openai', type: 'apikey', status: 'active'
}

describe.each([
  ['account modal', AccountTestModal],
  ['admin account modal', AdminAccountTestModal]
] as const)('%s image quality', (_name, component) => {
  const wrappers: ReturnType<typeof mount>[] = []

  beforeEach(() => {
    getAvailableModels.mockResolvedValue([
      { id: 'gpt-image-2.5-sunburst', display_name: 'GPT Image 2.5 Sunburst' },
      { id: 'gpt-5.4', display_name: 'GPT-5.4' }
    ])
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      body: new ReadableStream({
        start(controller) {
          controller.enqueue(new TextEncoder().encode('data: {"type":"test_complete","success":true}\n\n'))
          controller.close()
        }
      })
    }))
  })

  afterEach(() => {
    wrappers.splice(0).forEach(wrapper => wrapper.unmount())
    vi.unstubAllGlobals()
  })

  async function openModal(type = 'apikey') {
    const wrapper = mount(component, {
      props: { show: false, account: { ...account, type } } as any,
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
          Select: SelectStub,
          TextArea: true,
          Icon: true
        }
      }
    })
    wrappers.push(wrapper)
    await wrapper.setProps({ show: true })
    await flushPromises()
    return wrapper
  }

  it.each(['1k', '2k', '4k', 'auto', 'low', 'medium', 'high'])('sends selected %s quality in the image test request', async quality => {
    const wrapper = await openModal()
    const qualitySelect = wrapper.find('[data-testid="image-test-quality"]')
    expect(qualitySelect.exists()).toBe(true)
    await qualitySelect.setValue(quality)
    const startButton = wrapper.findAll('button').find(button => button.text().includes('admin.accounts.startTest'))!
    await startButton.trigger('click')
    await flushPromises()

    const [, request] = vi.mocked(fetch).mock.calls[0]!
    expect(JSON.parse(request!.body as string)).toMatchObject({
      model_id: 'gpt-image-2.5-sunburst', image_quality: quality
    })
  })

  it('omits quality when the upstream default is selected', async () => {
    const wrapper = await openModal()
    const startButton = wrapper.findAll('button').find(button => button.text().includes('admin.accounts.startTest'))!
    await startButton.trigger('click')
    await flushPromises()
    const [, request] = vi.mocked(fetch).mock.calls[0]!
    expect(JSON.parse(request!.body as string)).not.toHaveProperty('image_quality')
  })

  it('clears quality on model changes and reopening the modal', async () => {
    const wrapper = await openModal()
    await wrapper.get('[data-testid="image-test-quality"]').setValue('4k')
    const modelSelect = wrapper.findAll('select')[0]!
    await modelSelect.setValue('gpt-5.4')
    expect(wrapper.find('[data-testid="image-test-quality"]').exists()).toBe(false)
    await modelSelect.setValue('gpt-image-2.5-sunburst')
    expect(wrapper.get<HTMLSelectElement>('[data-testid="image-test-quality"]').element.value).toBe('')
    await wrapper.get('[data-testid="image-test-quality"]').setValue('2k')
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()
    expect(wrapper.get<HTMLSelectElement>('[data-testid="image-test-quality"]').element.value).toBe('')
  })

  it('does not offer API-key quality tiers for OAuth accounts or compact probes', async () => {
    const oauthModal = await openModal('oauth')
    expect(oauthModal.find('[data-testid="image-test-quality"]').exists()).toBe(false)
    const apiKeyModal = await openModal()
    await apiKeyModal.findAll('select')[1]!.setValue('compact')
    expect(apiKeyModal.find('[data-testid="image-test-quality"]').exists()).toBe(false)
  })
})
