import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import PhoneLoginForm from '@/components/auth/PhoneLoginForm.vue'
import en from '@/i18n/locales/en/common'
import zh from '@/i18n/locales/zh/common'
import { useAppStore } from '@/stores/app'

const { sendPhoneCode } = vi.hoisted(() => ({ sendPhoneCode: vi.fn() }))

vi.mock('@/api/auth', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/api/auth')>(),
  sendPhoneCode
}))

describe('PhoneLoginForm SMS error messages', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    sendPhoneCode.mockReset()
  })

  afterEach(() => {
    vi.clearAllTimers()
    vi.useRealTimers()
  })

  it.each([
    ['SMS_DELIVERY_FAILED', '短信验证码发送失败，请稍后重试，或使用邮箱登录。'],
    ['SMS_DELIVERY_UNKNOWN', '短信发送状态暂时无法确认，请稍候查收，避免重复发送。'],
    ['SMS_NOT_CONFIGURED', '短信登录暂不可用，请使用邮箱登录或联系管理员。'],
    ['SMS_DISABLED', '短信登录暂未开启，请使用邮箱登录。'],
    ['SMS_UNAVAILABLE', '短信服务暂不可用，请稍后重试，或使用邮箱登录。'],
    ['SMS_RATE_LIMITED', '验证码请求过于频繁，请稍后重试。']
  ])('shows a Chinese message for %s instead of the provider fallback', async (reason, message) => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const appStore = useAppStore(pinia)
    sendPhoneCode.mockRejectedValue({
      status: reason === 'SMS_RATE_LIMITED' ? 429 : 503,
      code: reason === 'SMS_RATE_LIMITED' ? 429 : 503,
      reason,
      message: 'verification message could not be submitted',
      metadata: reason === 'SMS_RATE_LIMITED' ? { retry_after: '3' } : undefined
    })
    const wrapper = mount(PhoneLoginForm, {
      props: {
        codeLength: 6,
        registrationEnabled: false,
        invitationCodeEnabled: false,
        promoCodeEnabled: false
      },
      global: {
        plugins: [pinia, createI18n({
          legacy: false,
          locale: 'zh',
          fallbackLocale: 'en',
          messages: { en, zh },
          missingWarn: false,
          fallbackWarn: false
        })],
        stubs: { Icon: true }
      }
    })

    await wrapper.get('[data-testid="phone-login-phone"]').setValue('13900000000')
    await wrapper.get('[data-testid="phone-login-send"]').trigger('click')
    await flushPromises()

    expect(appStore.toasts.map(toast => toast.message)).toEqual([message])
    expect(wrapper.get('[data-testid="phone-login-submit"]').attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })
})
