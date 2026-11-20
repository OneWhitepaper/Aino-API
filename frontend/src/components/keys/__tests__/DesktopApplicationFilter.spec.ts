import { afterEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import DesktopApplicationFilter from '../DesktopApplicationFilter.vue'
import { getDesktopApplication, getDesktopApplicationId } from '@/constants/desktopApplications'
import type { ApiKey } from '@/types'
import zh from '@/i18n/locales/zh'
import en from '@/i18n/locales/en'

enableAutoUnmount(afterEach)

describe.each([
  { locale: 'zh', groupLabel: '按桌面应用筛选', labels: ['全部应用', 'Aino', 'MoMo', '其他应用'] },
  { locale: 'en', groupLabel: 'Filter by desktop app', labels: ['All apps', 'Aino', 'MoMo', 'Other apps'] }
])('DesktopApplicationFilter ($locale)', ({ locale, groupLabel, labels }) => {
  function render(modelValue = '') {
    return mount(DesktopApplicationFilter, {
      props: { modelValue },
      global: {
        plugins: [createI18n({ legacy: false, locale, fallbackLocale: 'en', messages: { zh, en } })]
      }
    })
  }

  it('names the application choices and uses the real Aino and MoMo icons', () => {
    const wrapper = render()
    expect(wrapper.get('[role="group"]').attributes('aria-label')).toBe(groupLabel)
    expect(wrapper.findAll('button').map(button => button.text())).toEqual(labels)
    expect(wrapper.findAll('button').every(button => button.attributes('type') === 'button')).toBe(true)
    expect(wrapper.get('[data-testid="desktop-app-filter-aino"] img').attributes('src')).toBe('/aino-app-icon.png')
    expect(wrapper.get('[data-testid="desktop-app-filter-momo"] img').attributes('src')).toBe('/momo-app-icon.png')
    expect(wrapper.findAll('img').every(image => image.attributes('alt') === '')).toBe(true)
    expect(wrapper.get('[data-testid="desktop-app-filter-all"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.findAll('[aria-pressed="true"]')).toHaveLength(1)
  })

  it('emits application filters while keeping the selected state controlled by its parent', async () => {
    const wrapper = render()
    for (const application of ['aino', 'momo', 'other', '']) {
      await wrapper.get(`[data-testid="desktop-app-filter-${application || 'all'}"]`).trigger('click')
      expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([application])
      await wrapper.setProps({ modelValue: application })
      expect(wrapper.get(`[data-testid="desktop-app-filter-${application || 'all'}"]`).attributes('aria-pressed')).toBe('true')
      expect(wrapper.findAll('[aria-pressed="true"]')).toHaveLength(1)
    }
    expect(wrapper.emitted('update:modelValue')).toHaveLength(4)
  })

  it('does not reload the selected filter or turn an unknown application into a known one', async () => {
    const wrapper = render('momo')
    await wrapper.get('[data-testid="desktop-app-filter-momo"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    await wrapper.setProps({ modelValue: 'future-client' })
    expect(wrapper.findAll('[aria-pressed="true"]')).toHaveLength(0)
    await wrapper.get('[data-testid="desktop-app-filter-other"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toEqual([['other']])
  })
})

describe('desktop application identity', () => {
  const managedKey = (desktopApp?: string, name = 'Device authorization') => ({
    desktop_managed: true, desktop_app: desktopApp, name
  } as ApiKey)

  it('keeps legacy managed authorizations in Aino without guessing from the key name', () => {
    expect(getDesktopApplicationId(managedKey())).toBe('aino')
    expect(getDesktopApplicationId(managedKey(''))).toBe('aino')
    expect(getDesktopApplicationId(managedKey(undefined, 'MoMo desktop'))).toBe('aino')
  })

  it('preserves explicit known and future application IDs', () => {
    for (const id of ['aino', 'momo', 'future-client']) {
      expect(getDesktopApplicationId(managedKey(id))).toBe(id)
    }
    expect(getDesktopApplication('aino')?.name).toBe('Aino')
    expect(getDesktopApplication('momo')?.name).toBe('MoMo')
    expect(getDesktopApplication('future-client')).toBeUndefined()
  })

  it('never classifies ordinary keys as a desktop application', () => {
    expect(getDesktopApplicationId({ desktop_managed: false, desktop_app: 'momo' } as ApiKey)).toBe('')
    expect(getDesktopApplicationId({ name: 'Aino desktop' } as ApiKey)).toBe('')
  })
})
