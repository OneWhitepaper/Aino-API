import { afterEach, describe, expect, it } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ConfirmDialog from '../ConfirmDialog.vue'

enableAutoUnmount(afterEach)
function render(busy: boolean) {
  return mount(ConfirmDialog, {
    props: { show: true, title: 'Delete team?', message: 'History is retained.', busy },
    global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: { common: { confirm: 'Confirm', cancel: 'Cancel' } } } })], stubs: { Teleport: true, Transition: false } }
  })
}
describe('confirmation busy state', () => {
  it('prevents confirmation, cancellation and Escape while pending, then restores controls', async () => {
    const wrapper = render(true)
    expect(wrapper.findAll('button').every(button => button.attributes('disabled') !== undefined)).toBe(true)
    expect(wrapper.find('[aria-label="Close modal"]').exists()).toBe(false)
    for (const button of wrapper.findAll('button')) await button.trigger('click')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('confirm')).toBeUndefined()
    expect(wrapper.emitted('cancel')).toBeUndefined()
    await wrapper.setProps({ busy: false })
    expect(wrapper.find('[aria-label="Close modal"]').exists()).toBe(true)
    await wrapper.findAll('button').find(button => button.text() === 'Confirm')!.trigger('click')
    expect(wrapper.emitted('confirm')).toHaveLength(1)
    await wrapper.findAll('button').find(button => button.text() === 'Cancel')!.trigger('click')
    expect(wrapper.emitted('cancel')).toHaveLength(1)
  })
})
