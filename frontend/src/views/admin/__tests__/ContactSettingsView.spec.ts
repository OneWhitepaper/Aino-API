import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ContactSettingsView from '../ContactSettingsView.vue'

const { getContactPageSettings, updateContactPageSettings, showSuccess, showError } = vi.hoisted(() => ({
  getContactPageSettings: vi.fn(), updateContactPageSettings: vi.fn(), showSuccess: vi.fn(), showError: vi.fn(),
}))
vi.mock('@/api/contact', () => ({ getContactPageSettings, updateContactPageSettings }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess, showError }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

const fixture = () => ({ description: '', service_hours: '', methods: [], groups: [] })
const mountView = () => mount(ContactSettingsView, {
  global: { stubs: { AppLayout: { template: '<main><slot /></main>' }, Icon: true, RouterLink: true } },
})

describe('ContactSettingsView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getContactPageSettings.mockResolvedValue(fixture())
    updateContactPageSettings.mockImplementation(async value => value)
  })

  it('adds a contact and persists only contact page settings', async () => {
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('[data-testid="add-contact"]').trigger('click')
    await wrapper.get('input[name="method-label-0"]').setValue('客服微信')
    await wrapper.get('input[name="method-value-0"]').setValue('aino-support')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(updateContactPageSettings).toHaveBeenCalledWith({
      description: '', service_hours: '',
      methods: [{ type: 'wechat', label: '客服微信', value: 'aino-support', url: '', enabled: true }], groups: [],
    })
    expect(wrapper.get('input[name="method-value-0"]').element).toHaveProperty('value', 'aino-support')
  })

  it('does not allow saving an empty draft after a load failure', async () => {
    getContactPageSettings.mockRejectedValue(new Error('offline'))
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('contact.loadFailed')
    expect(wrapper.find('form').exists()).toBe(false)
  })

  it('retains unsaved edits when saving fails', async () => {
    updateContactPageSettings.mockRejectedValue(new Error('unavailable'))
    const wrapper = mountView()
    await flushPromises()
    await wrapper.get('textarea[name="description"]').setValue('未保存的简介')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('textarea[name="description"]').element).toHaveProperty('value', '未保存的简介')
    expect(showError).toHaveBeenCalled()
  })
})
