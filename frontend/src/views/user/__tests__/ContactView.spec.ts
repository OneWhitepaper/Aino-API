import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ContactView from '../ContactView.vue'

const { getContactPage, copyToClipboard } = vi.hoisted(() => ({
  getContactPage: vi.fn(), copyToClipboard: vi.fn(),
}))
vi.mock('@/api/contact', () => ({ getContactPage }))
vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copyToClipboard }) }))
vi.mock('vue-i18n', async (importOriginal) => ({
  ...await importOriginal<typeof import('vue-i18n')>(),
  useI18n: () => ({ t: (key: string) => key }),
}))

const mountView = () => mount(ContactView, {
  global: { stubs: { AppLayout: { template: '<main><slot /></main>' }, Icon: true } },
})

describe('ContactView', () => {
  beforeEach(() => vi.clearAllMocks())

  it('shows configured contacts and group QR codes, and copies a contact value', async () => {
    getContactPage.mockResolvedValue({
      description: '欢迎联系我们', service_hours: '工作日 9:00–18:00',
      methods: [{ type: 'wechat', label: '客服微信', value: 'aino-support', url: '', enabled: true }],
      groups: [{ platform: 'wechat', name: '微信交流群', description: '扫码交流', qr_code: 'data:image/png;base64,example', group_number: '', join_url: '', enabled: true }],
    })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('欢迎联系我们')
    expect(wrapper.text()).toContain('aino-support')
    expect(wrapper.text()).toContain('工作日 9:00–18:00')
    expect(wrapper.find('img[alt="微信交流群"]').attributes('src')).toBe('data:image/png;base64,example')
    await wrapper.get('[data-testid="copy-contact"]').trigger('click')
    expect(copyToClipboard).toHaveBeenCalledWith('aino-support')
  })

  it('shows an honest empty state when no details are configured', async () => {
    getContactPage.mockResolvedValue({ description: '', service_hours: '', methods: [], groups: [] })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('contact.empty')
    expect(wrapper.findAll('img')).toHaveLength(0)
  })

  it('shows a retry control on load failure and recovers', async () => {
    getContactPage.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ description: '恢复', service_hours: '', methods: [], groups: [] })
    const wrapper = mountView()
    await flushPromises()
    expect(wrapper.text()).toContain('contact.loadFailed')
    await wrapper.get('[data-testid="retry-contact"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('恢复')
    expect(wrapper.text()).not.toContain('contact.loadFailed')
  })
})
