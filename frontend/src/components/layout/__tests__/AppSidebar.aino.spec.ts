import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import { createMemoryHistory, createRouter } from 'vue-router'
import { createI18n } from 'vue-i18n'
import AppSidebar from '../AppSidebar.vue'
import { useAdminSettingsStore, useAppStore } from '@/stores'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

const { authState } = vi.hoisted(() => ({
  authState: { isAdmin: true, isSimpleMode: false }
}))

// Keep real Pinia UI state and routing, without starting authentication or
// unrelated network-backed access checks while exercising the sidebar.
vi.mock('@/stores', async (importOriginal) => ({
  ...await importOriginal<typeof import('@/stores')>(),
  useAuthStore: () => authState
}))
vi.mock('@/composables/useBatchImageAccess', () => ({
  useBatchImageAccess: () => ({
    canUseBatchImage: { value: false },
    refreshBatchImageAccess: vi.fn().mockResolvedValue(false)
  })
}))

enableAutoUnmount(afterEach)

type Locale = 'zh' | 'en'

async function renderSidebar(options: {
  locale: Locale
  path?: string
  collapsed?: boolean
  mobileOpen?: boolean
}) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const app = useAppStore(pinia)
  app.setSidebarCollapsed(options.collapsed ?? false)
  app.setMobileOpen(options.mobileOpen ?? false)
  const settings = useAdminSettingsStore(pinia)
  vi.spyOn(settings, 'fetch').mockResolvedValue(undefined)

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }]
  })
  await router.push(options.path ?? '/admin/desktop-models')
  await router.isReady()

  const wrapper = mount(AppSidebar, {
    attachTo: document.body,
    global: {
      plugins: [pinia, router, createI18n({
        legacy: false,
        locale: options.locale,
        fallbackLocale: 'en',
        messages: { zh, en }
      })],
      stubs: { VersionBadge: true, transition: true }
    }
  })
  await flushPromises()
  return { wrapper, app, router }
}

function groupFor(wrapper: VueWrapper, label: string) {
  const button = wrapper.get(`button[aria-label="${label}"]`)
  const panelId = button.attributes('aria-controls')
  expect(panelId).toBeTruthy()
  return { button, panel: wrapper.get(`[id="${panelId}"]`) }
}

beforeEach(() => {
  authState.isAdmin = true
  authState.isSimpleMode = false
  localStorage.clear()
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe.each([
  { locale: 'zh' as const, application: 'Aino 桌面端', modelLabel: '内置模型', momoApplication: 'MoMo 桌面端', momoEmpty: '暂无配置项' },
  { locale: 'en' as const, application: 'Aino desktop', modelLabel: 'Built-in models', momoApplication: 'MoMo desktop', momoEmpty: 'No configuration items yet' }
])('Desktop app sidebar navigation ($locale)', ({ locale, application, modelLabel, momoApplication, momoEmpty }) => {
  it('expands for the existing model URL and marks its child as the current page', async () => {
    const { wrapper, router } = await renderSidebar({ locale })
    const { button, panel } = groupFor(wrapper, application)
    const link = panel.get('a[href="/admin/desktop-models"]')

    expect(button.attributes('aria-expanded')).toBe('true')
    expect(panel.isVisible()).toBe(true)
    expect(link.text()).toBe(modelLabel)
    expect(link.classes()).toContain('sidebar-link-active')
    expect(link.attributes('aria-current')).toBe('page')
    expect(wrapper.findAll('a[href="/admin/desktop-models"]')).toHaveLength(1)
    expect(router.currentRoute.value.path).toBe('/admin/desktop-models')
  })

  it('allows collapsing the active group and reopening it without navigation', async () => {
    const { wrapper, router } = await renderSidebar({ locale })
    const { button, panel } = groupFor(wrapper, application)
    const push = vi.spyOn(router, 'push')

    await button.trigger('click')
    expect(button.attributes('aria-expanded')).toBe('false')
    expect(panel.isVisible()).toBe(false)
    expect(button.classes()).toContain('sidebar-link-active')

    await button.trigger('click')
    expect(button.attributes('aria-expanded')).toBe('true')
    expect(panel.isVisible()).toBe(true)
    expect(push).not.toHaveBeenCalled()
    expect(router.currentRoute.value.path).toBe('/admin/desktop-models')
  })

  it('opens the sidebar and group when its icon is clicked in the collapsed sidebar', async () => {
    const { wrapper, app, router } = await renderSidebar({
      locale, path: '/admin/dashboard', collapsed: true
    })
    const { button, panel } = groupFor(wrapper, application)
    expect(button.attributes('aria-expanded')).toBe('false')
    expect(panel.isVisible()).toBe(false)
    expect(app.sidebarCollapsed).toBe(true)

    await button.get('img').trigger('click')
    expect(app.sidebarCollapsed).toBe(false)
    expect(button.attributes('aria-expanded')).toBe('true')
    expect(panel.isVisible()).toBe(true)
    expect(router.currentRoute.value.path).toBe('/admin/dashboard')
  })

  it('keeps the active software visible when collapsing the sidebar on its model page', async () => {
    const { wrapper, app, router } = await renderSidebar({ locale })
    const { button, panel } = groupFor(wrapper, application)
    expect(panel.isVisible()).toBe(true)
    expect(panel.get('a[href="/admin/desktop-models"]').attributes('aria-current')).toBe('page')

    app.setSidebarCollapsed(true)
    await nextTick()
    expect(panel.isVisible()).toBe(false)
    expect(button.attributes('aria-expanded')).toBe('false')
    expect(button.classes()).toContain('sidebar-link-active')

    await button.get('img').trigger('click')
    expect(app.sidebarCollapsed).toBe(false)
    expect(button.attributes('aria-expanded')).toBe('true')
    expect(panel.isVisible()).toBe(true)
    expect(panel.get('a[href="/admin/desktop-models"]').classes()).toContain('sidebar-link-active')
    expect(router.currentRoute.value.path).toBe('/admin/desktop-models')
  })

  it('keeps the mobile menu open for the group and closes it after choosing its child', async () => {
    const { wrapper, app, router } = await renderSidebar({
      locale, path: '/admin/dashboard', mobileOpen: true
    })
    const { button, panel } = groupFor(wrapper, application)
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })

    await button.trigger('click')
    await vi.advanceTimersByTimeAsync(150)
    expect(app.mobileOpen).toBe(true)
    expect(panel.isVisible()).toBe(true)

    await panel.get('a[href="/admin/desktop-models"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/admin/desktop-models')
    await vi.advanceTimersByTimeAsync(149)
    expect(app.mobileOpen).toBe(true)
    await vi.advanceTimersByTimeAsync(1)
    expect(app.mobileOpen).toBe(false)
  })

  it('does not expose Aino administration in the ordinary user workspace', async () => {
    authState.isAdmin = false
    const { wrapper } = await renderSidebar({ locale, path: '/dashboard' })

    expect(wrapper.find(`button[aria-label="${application}"]`).exists()).toBe(false)
    expect(wrapper.find('a[href="/admin/desktop-models"]').exists()).toBe(false)
  })

  it('keeps Aino model management accessible to administrators in simple mode', async () => {
    authState.isSimpleMode = true
    const { wrapper } = await renderSidebar({ locale })
    const { button, panel } = groupFor(wrapper, application)

    expect(button.attributes('aria-expanded')).toBe('true')
    expect(panel.isVisible()).toBe(true)
    expect(panel.get('a[href="/admin/desktop-models"]').text()).toBe(modelLabel)
  })

  it('presents Aino and MoMo as sibling software categories with their own icons', async () => {
    const { wrapper } = await renderSidebar({ locale })
    const aino = groupFor(wrapper, application)
    const momo = groupFor(wrapper, momoApplication)

    expect(momo.button.element.parentElement).toBe(aino.button.element.parentElement)
    expect(aino.button.get('img').attributes('src')).toBe('/aino-app-icon.png')
    expect(momo.button.get('img').attributes('src')).toBe('/momo-app-icon.png')
    expect(momo.button.attributes('aria-controls')).not.toBe(aino.button.attributes('aria-controls'))
    expect(aino.panel.find(`button[aria-label="${momoApplication}"]`).exists()).toBe(false)
    expect(momo.panel.find(`button[aria-label="${application}"]`).exists()).toBe(false)
  })

  it('toggles the empty MoMo category without navigating to an unavailable page', async () => {
    const { wrapper, router } = await renderSidebar({ locale })
    const { button, panel } = groupFor(wrapper, momoApplication)
    const push = vi.spyOn(router, 'push')

    expect(button.attributes('aria-expanded')).toBe('false')
    expect(panel.isVisible()).toBe(false)
    await button.trigger('click')
    expect(button.attributes('aria-expanded')).toBe('true')
    expect(panel.isVisible()).toBe(true)
    expect(panel.text()).toBe(momoEmpty)
    expect(panel.findAll('a')).toHaveLength(0)

    await button.trigger('click')
    expect(button.attributes('aria-expanded')).toBe('false')
    expect(panel.isVisible()).toBe(false)
    expect(push).not.toHaveBeenCalled()
    expect(router.currentRoute.value.path).toBe('/admin/desktop-models')
  })

  it('preserves Aino selection and independent expansion while toggling MoMo', async () => {
    const { wrapper } = await renderSidebar({ locale })
    const aino = groupFor(wrapper, application)
    const momo = groupFor(wrapper, momoApplication)
    const ainoLink = aino.panel.get('a[href="/admin/desktop-models"]')

    await momo.button.trigger('click')
    expect(aino.button.attributes('aria-expanded')).toBe('true')
    expect(aino.panel.isVisible()).toBe(true)
    expect(ainoLink.classes()).toContain('sidebar-link-active')
    expect(ainoLink.attributes('aria-current')).toBe('page')
    expect(momo.button.classes()).not.toContain('sidebar-link-active')

    await aino.button.trigger('click')
    expect(momo.button.attributes('aria-expanded')).toBe('true')
    await momo.button.trigger('click')
    await momo.button.trigger('click')
    expect(aino.button.attributes('aria-expanded')).toBe('false')
    expect(aino.panel.isVisible()).toBe(false)
    expect(aino.button.classes()).toContain('sidebar-link-active')
    expect(ainoLink.attributes('aria-current')).toBe('page')
  })

  it('does not expose the MoMo administration category to ordinary users', async () => {
    authState.isAdmin = false
    const { wrapper } = await renderSidebar({ locale, path: '/dashboard' })

    expect(wrapper.find(`button[aria-label="${momoApplication}"]`).exists()).toBe(false)
    expect(wrapper.find('a[href="/admin/apps/momo"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain(momoEmpty)
  })
})
