import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'
import OmniFlowHome from '../OmniFlowHome.vue'

const models = [
  { id: 'deepseek-v4-pro', endpoint: '/v1/chat/completions', kind: 'chat', auth: 'Authorization', body: 'messages' },
  { id: 'claude-sonnet-4-6', endpoint: '/v1/messages', kind: 'messages', auth: 'x-api-key', body: 'messages' },
  { id: 'gemini-2.5-pro', endpoint: '/v1beta/models/gemini-2.5-pro:generateContent', kind: 'native', auth: 'x-goog-api-key', body: 'contents' },
] as const

type HomeProps = {
  siteName: string
  siteLogo: string
  docUrl: string
  isAuthenticated: boolean
  dashboardPath: string
  registrationEnabled: boolean
  showModelPlaza: boolean
}

const clipboardDescriptor = Object.getOwnPropertyDescriptor(navigator, 'clipboard')
const secureContextDescriptor = Object.getOwnPropertyDescriptor(window, 'isSecureContext')
const showModalDescriptor = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal')
const closeDescriptor = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close')
let copy: ReturnType<typeof vi.fn>
let fetchRequest: ReturnType<typeof vi.fn>
const mounted: { unmount: () => void }[] = []

async function mountHome(props: Partial<HomeProps> = {}, locale: 'zh' | 'en' = 'zh') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: ['/', '/login', '/register', '/dashboard', '/admin/dashboard', '/model-plaza'].map(path => ({ path, component: { template: '<div />' } })),
  })
  await router.push('/')
  await router.isReady()
  const wrapper = mount(OmniFlowHome, {
    attachTo: document.body,
    props: {
      siteName: 'OmniFlow API',
      siteLogo: '/omniflow-logo.png',
      docUrl: '',
      isAuthenticated: false,
      dashboardPath: '/dashboard',
      registrationEnabled: false,
      showModelPlaza: false,
      ...props,
    },
    global: {
      plugins: [router, createI18n({ legacy: false, locale, fallbackLocale: 'zh', messages: { zh, en } })],
      stubs: { LocaleSwitcher: { template: '<button aria-label="Language">中文</button>' } },
    },
  })
  mounted.push(wrapper)
  return wrapper
}

type HomeWrapper = Awaited<ReturnType<typeof mountHome>>

function visibleModels(wrapper: HomeWrapper) {
  return wrapper.findAll('[data-model-row]').filter(row => row.isVisible()).map(row => row.attributes('data-model-row'))
}

function renderedCode(wrapper: HomeWrapper, selector = '#quick-code') {
  const element = wrapper.get(selector).element
  const lines = [...element.querySelectorAll('.code-line, .ofsb-code-line')]
  return lines.length ? lines.map(line => {
    const text = (line.textContent || '').replace(/\n$/, '')
    return text === ' ' ? '' : text
  }).join('\n') : element.textContent || ''
}

function restoreProperty(target: object, name: string, descriptor?: PropertyDescriptor) {
  if (descriptor) Object.defineProperty(target, name, descriptor)
  else Reflect.deleteProperty(target, name)
}

beforeEach(() => {
  vi.useFakeTimers()
  copy = vi.fn().mockResolvedValue(undefined)
  fetchRequest = vi.fn().mockRejectedValue(new Error('The local demo must not send a model request'))
  vi.stubGlobal('fetch', fetchRequest)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: copy } })
  Object.defineProperty(window, 'isSecureContext', { configurable: true, value: true })
  // jsdom lacks native dialog methods. Model native open/close events, leaving
  // Escape handling, pending work cancellation and focus restoration to the component.
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value(this: HTMLDialogElement) { this.setAttribute('open', '') } })
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value(this: HTMLDialogElement) { this.removeAttribute('open'); this.dispatchEvent(new Event('close')) } })
})

afterEach(() => {
  // Dispose real components while their jsdom native-dialog shims still exist.
  mounted.splice(0).forEach(wrapper => wrapper.unmount())
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  restoreProperty(navigator, 'clipboard', clipboardDescriptor)
  restoreProperty(window, 'isSecureContext', secureContextDescriptor)
  restoreProperty(HTMLDialogElement.prototype, 'showModal', showModalDescriptor)
  restoreProperty(HTMLDialogElement.prototype, 'close', closeDescriptor)
  document.body.innerHTML = ''
})

describe('OmniFlowHome application integration', () => {
  it.each([
    { isAuthenticated: false, dashboardPath: '/dashboard', registrationEnabled: false, destination: '/login' },
    { isAuthenticated: false, dashboardPath: '/dashboard', registrationEnabled: true, destination: '/login' },
    { isAuthenticated: true, dashboardPath: '/dashboard', registrationEnabled: true, destination: '/dashboard' },
    { isAuthenticated: true, dashboardPath: '/admin/dashboard', registrationEnabled: true, destination: '/admin/dashboard' },
  ])('uses the supplied auth destination $destination (signed in=$isAuthenticated)', async ({ destination, ...props }) => {
    const wrapper = await mountHome(props)
    expect(wrapper.get('.of-login').attributes('href')).toBe(destination)
    const closingLink = wrapper.findAll('#connect a').find(link => ['/login', '/dashboard', '/admin/dashboard'].includes(link.attributes('href')))
    expect(closingLink?.attributes('href')).toBe(destination)
    expect(wrapper.findAll('a[href="https://api.agentera.com.cn/login"]')).toHaveLength(0)
    expect(wrapper.findAll('a[href="/register"]').length > 0).toBe(!props.isAuthenticated && props.registrationEnabled)
  })

  it('keeps configured branding and docs while the model plaza respects its visibility flag', async () => {
    const wrapper = await mountHome({ siteName: 'Customer Gateway', siteLogo: '/customer.svg', docUrl: 'https://docs.example.test/guide', showModelPlaza: true })
    expect(wrapper.get('.of-brand').text()).toContain('Customer Gateway')
    expect(wrapper.get('.of-brand img').attributes('src')).toBe('/customer.svg')
    expect(wrapper.find('a[href="https://docs.example.test/guide"]').exists()).toBe(true)
    expect(wrapper.find('a[href="/model-plaza"]').exists()).toBe(true)
    await wrapper.setProps({ showModelPlaza: false })
    expect(wrapper.find('a[href="/model-plaza"]').exists()).toBe(false)
  })

  it('uses the active English locale for navigation and locally simulated responses', async () => {
    const wrapper = await mountHome({}, 'en')
    expect(wrapper.attributes('lang')).toBe('en')
    expect(wrapper.get('.of-login').text()).toBe('Sign in')
    await wrapper.get('#hero-prompt').setValue('LOCALE_RESPONSE')
    await wrapper.get('#hero-prompt').trigger('keydown', { key: 'Enter' })
    await vi.advanceTimersByTimeAsync(1000)
    expect(wrapper.get('#hero-answer').text()).toContain('LOCALE_RESPONSE')
    expect(wrapper.get('#hero-status').text()).toContain('Local simulation')
  })
})

describe('OmniFlowHome model selection and code', () => {
  it.each(models)('keeps hero and quickstart on the selected native route $id', async model => {
    const wrapper = await mountHome()
    await wrapper.get('.route-choice[data-model="' + model.id + '"]').trigger('click')
    expect(wrapper.get('#route-path').text()).toBe(model.endpoint)
    expect(wrapper.get('#hero-model-id').text()).toBe(model.id)
    expect((wrapper.get('#quick-model').element as HTMLSelectElement).value).toBe(model.id)
    expect(wrapper.findAll('.route-choice[aria-pressed="true"]')).toHaveLength(1)
    expect(wrapper.get('.route-choice[data-model="' + model.id + '"]').attributes('aria-pressed')).toBe('true')
    const alternate = model.id === models[0].id ? models[1] : models[0]
    await wrapper.get('#quick-model').setValue(alternate.id)
    expect(wrapper.get('#route-path').text()).toBe(alternate.endpoint)
    expect(wrapper.get('#hero-model-id').text()).toBe(alternate.id)
  })

  it.each(models)('renders native auth/body in both code languages and copies the visible $id request', async model => {
    const wrapper = await mountHome()
    await wrapper.get('#quick-model').setValue(model.id)
    for (const language of ['curl', 'python']) {
      await wrapper.get('[data-lang="' + language + '"]').trigger('click')
      const text = renderedCode(wrapper)
      expect(text).toContain(window.location.origin + model.endpoint)
      expect(text).toContain(model.auth)
      expect(text).toContain('OMNIFLOW_API_KEY')
      expect(text).toContain('"' + model.body + '"')
      if (model.kind === 'chat') expect(text).toContain('Bearer')
      if (model.kind === 'messages') { expect(text).toContain('anthropic-version'); expect(text).toContain('2023-06-01'); expect(text).toContain('"max_tokens"') }
      if (model.kind === 'native') { expect(text).toContain('"parts"'); expect(text).not.toContain('Authorization'); expect(text).not.toContain('"messages"') }
      await wrapper.get('#quick-copy').trigger('click')
      expect(copy).toHaveBeenLastCalledWith(text)
    }
  })
})

describe('OmniFlowHome local simulation', () => {
  it('shows Enter-submitted input as inert text and never starts a real model request', async () => {
    const wrapper = await mountHome()
    const xhr = vi.spyOn(XMLHttpRequest.prototype, 'open')
    const prompt = '<img src=x onerror="window.qaInjected=true"> QA'
    await wrapper.get('#hero-prompt').setValue(prompt)
    await wrapper.get('#hero-prompt').trigger('keydown', { key: 'Enter' })
    await vi.advanceTimersByTimeAsync(1000)
    expect(wrapper.get('#hero-answer').text()).toContain(prompt)
    expect(wrapper.get('#hero-answer').find('img, script').exists()).toBe(false)
    expect(wrapper.get('#hero-send').attributes('disabled')).toBeUndefined()
    expect(fetchRequest).not.toHaveBeenCalled()
    expect(xhr).not.toHaveBeenCalled()
  })

  it('cancels pending hero output when the selected model changes', async () => {
    const wrapper = await mountHome()
    await wrapper.get('#hero-prompt').setValue('STALE_MODEL_REPLY')
    await wrapper.get('#hero-prompt').trigger('keydown', { key: 'Enter' })
    expect(wrapper.get('#hero-send').attributes('disabled')).toBeDefined()
    await wrapper.get('.route-choice[data-model="claude-sonnet-4-6"]').trigger('click')
    await wrapper.get('.route-choice[data-model="gemini-2.5-pro"]').trigger('click')
    await vi.advanceTimersByTimeAsync(1000)
    expect(wrapper.get('#hero-answer').text()).not.toContain('STALE_MODEL_REPLY')
    expect(wrapper.get('#hero-model-id').text()).toBe('gemini-2.5-pro')
    expect(wrapper.get('#hero-send').attributes('disabled')).toBeUndefined()
    await wrapper.get('#hero-prompt').setValue('CURRENT_MODEL_REPLY')
    await wrapper.get('#hero-prompt').trigger('keydown', { key: 'Enter' })
    await vi.advanceTimersByTimeAsync(1000)
    expect(wrapper.get('#hero-answer').text()).toContain('CURRENT_MODEL_REPLY')
  })

  it('cancels pending hero output when the input is edited before completion', async () => {
    const wrapper = await mountHome()
    await wrapper.get('#hero-prompt').setValue('STALE_PROMPT_REPLY')
    await wrapper.get('#hero-prompt').trigger('keydown', { key: 'Enter' })
    await wrapper.get('#hero-prompt').setValue('EDITED_PROMPT_REPLY')
    await vi.advanceTimersByTimeAsync(1000)
    expect(wrapper.get('#hero-answer').text()).not.toContain('STALE_PROMPT_REPLY')
    expect(wrapper.get('#hero-send').attributes('disabled')).toBeUndefined()
    await wrapper.get('#hero-prompt').trigger('keydown', { key: 'Enter' })
    await vi.advanceTimersByTimeAsync(1000)
    expect(wrapper.get('#hero-answer').text()).toContain('EDITED_PROMPT_REPLY')
  })

  it('rejects an empty sandbox prompt, renders native JSON and closes on Escape/cancel with focus restored', async () => {
    const wrapper = await mountHome()
    const trigger = wrapper.get('[data-sandbox="claude-sonnet-4-6"]')
    ;(trigger.element as HTMLElement).focus()
    await trigger.trigger('click')
    await nextTick()
    const dialog = document.querySelector('dialog.ofsb') as HTMLDialogElement
    expect(dialog?.open).toBe(true)
    const prompt = dialog.querySelector('#ofsb-prompt') as HTMLTextAreaElement
    prompt.value = ' '
    prompt.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    dialog.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await nextTick()
    expect(prompt.getAttribute('aria-invalid')).toBe('true')
    const input = 'sandbox <img src=x onerror="window.qaInjected=true">'
    prompt.value = input
    prompt.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    dialog.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await vi.advanceTimersByTimeAsync(1000)
    const answer = dialog.querySelector('.ofsb-answer')!.textContent
    expect(answer).toContain(input)
    expect(dialog.querySelector('.ofsb-answer img, .ofsb-answer script')).toBeNull()
    const raw = JSON.parse(dialog.querySelector('.ofsb-json')!.textContent || '{}')
    expect(raw.content[0].text).toBe(answer)
    const escape = new Event('cancel', { cancelable: true })
    if (dialog.dispatchEvent(escape)) dialog.close()
    await nextTick()
    expect(document.querySelector('dialog.ofsb[open]')).toBeNull()
    expect(document.activeElement).toBe(trigger.element)
    expect(document.documentElement.style.overflow).toBe('')
    expect(fetchRequest).not.toHaveBeenCalled()
  })

  it('switches sandbox code tabs by keyboard and copies the selected native request', async () => {
    const wrapper = await mountHome()
    await wrapper.get('[data-sandbox="gemini-2.5-pro"]').trigger('click')
    await nextTick()
    const dialog = document.querySelector('dialog.ofsb') as HTMLDialogElement
    const curl = dialog.querySelector('#ofsb-tab-curl') as HTMLButtonElement
    curl.focus()
    curl.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true, cancelable: true }))
    await nextTick()
    const python = dialog.querySelector('#ofsb-tab-python') as HTMLButtonElement
    expect(python.getAttribute('aria-selected')).toBe('true')
    expect(document.activeElement).toBe(python)
    const panel = dialog.querySelector('#ofsb-codepanel')!
    expect(panel.textContent).toContain('from urllib.request')
    expect(panel.textContent).toContain('x-goog-api-key')
    ;(dialog.querySelector('.ofsb-copy') as HTMLButtonElement).click()
    await nextTick()
    const copied = String(copy.mock.calls.at(-1)?.[0])
    expect(copied).toContain('/v1beta/models/gemini-2.5-pro:generateContent')
    expect(copied).toContain('from urllib.request')
    python.dispatchEvent(new KeyboardEvent('keydown', { key: 'Home', bubbles: true, cancelable: true }))
    await nextTick()
    expect(curl.getAttribute('aria-selected')).toBe('true')
    expect(panel.textContent).toContain('curl -X POST')
  })

  it('cancels a pending sandbox reply before switching its model', async () => {
    const wrapper = await mountHome()
    await wrapper.get('[data-sandbox="deepseek-v4-pro"]').trigger('click')
    await nextTick()
    const dialog = document.querySelector('dialog.ofsb') as HTMLDialogElement
    const prompt = dialog.querySelector('#ofsb-prompt') as HTMLTextAreaElement
    prompt.value = 'STALE_SANDBOX_RESPONSE'
    prompt.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    dialog.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await nextTick()
    const select = dialog.querySelector('#ofsb-model') as HTMLSelectElement
    select.value = 'gemini-2.5-pro'
    select.dispatchEvent(new Event('change', { bubbles: true }))
    await vi.advanceTimersByTimeAsync(1000)
    expect(dialog.querySelector('.ofsb-answer')!.textContent).not.toContain('STALE_SANDBOX_RESPONSE')
    expect(wrapper.get('#hero-model-id').text()).toBe('gemini-2.5-pro')
    expect((wrapper.get('#quick-model').element as HTMLSelectElement).value).toBe('gemini-2.5-pro')
    prompt.value = 'CURRENT_SANDBOX_RESPONSE'
    prompt.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    dialog.querySelector('form')!.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
    await vi.advanceTimersByTimeAsync(1000)
    const raw = JSON.parse(dialog.querySelector('.ofsb-json')!.textContent || '{}')
    expect(raw.candidates[0].content.parts[0].text).toContain('CURRENT_SANDBOX_RESPONSE')
  })
})

describe('OmniFlowHome catalog', () => {
  it('combines search with protocol filters and resets empty results', async () => {
    const wrapper = await mountHome()
    await wrapper.get('[data-filter="messages"]').trigger('click')
    expect(visibleModels(wrapper)).toEqual(['claude-sonnet-4-6'])
    await wrapper.get('#model-search').setValue('DeepSeek')
    expect(visibleModels(wrapper)).toEqual([])
    expect(wrapper.get('#empty-state').isVisible()).toBe(true)
    await wrapper.get('#clear-filters').trigger('click')
    expect(visibleModels(wrapper)).toEqual(['deepseek-v4-pro', 'claude-sonnet-4-6', 'gemini-2.5-pro'])
    expect((wrapper.get('#model-search').element as HTMLInputElement).value).toBe('')
    expect(wrapper.get('[data-filter="all"]').attributes('aria-pressed')).toBe('true')
    await wrapper.get('#model-search').setValue('generateContent')
    expect(visibleModels(wrapper)).toEqual(['gemini-2.5-pro'])
  })

  it('copies the selected model ID without copying neighboring table text', async () => {
    const wrapper = await mountHome()
    await wrapper.get('[data-copy-id="claude-sonnet-4-6"]').trigger('click')
    expect(copy).toHaveBeenLastCalledWith('claude-sonnet-4-6')
  })
})
