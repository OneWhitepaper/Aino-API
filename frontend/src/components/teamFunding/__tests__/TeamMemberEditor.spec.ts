import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import TeamMemberEditor from '../TeamMemberEditor.vue'
import messages from '@/i18n/locales/en/teamFunding'
const member = { user_id: 9, username: 'Member', status: 'active' as const, spend_limit: '0.00000000', spent: '20.00000000', reserved: '0.00000001', shared_mode: null, suggested_mode: null }
function render() { return mount(TeamMemberEditor, { props: { member, busy: false }, global: { plugins: [createI18n({ legacy: false, locale: 'en', messages: { en: { teamFunding: messages } } })] } }) }
describe('member cap editing', () => {
  it('renders persisted zero as a prohibited finite cap and only sends null after explicit unlimited selection', async () => {
    const wrapper = render()
    expect((wrapper.get('#unlimited-9').element as HTMLInputElement).checked).toBe(false)
    expect((wrapper.get('#limit-9').element as HTMLInputElement).value).toBe('0.00000000')
    await wrapper.get('form').trigger('submit'); expect(wrapper.emitted('save')![0]).toEqual([{ spend_limit: '0.00000000', shared_mode: null, suggested_mode: null }])
    await wrapper.get('#unlimited-9').setValue(true); await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')![1]).toEqual([{ spend_limit: null, shared_mode: null, suggested_mode: null }])
    expect(wrapper.text()).toContain('0.00000001 credits')
    wrapper.unmount()
  })
  it('does not round eight decimal digits and disables every mutation while busy', async () => {
    const wrapper = render(); await wrapper.get('#limit-9').setValue('999999999999.12345678'); await wrapper.get('form').trigger('submit')
    expect(wrapper.emitted('save')![0]).toEqual([{ spend_limit: '999999999999.12345678', shared_mode: null, suggested_mode: null }])
    await wrapper.setProps({ busy: true }); expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    expect(wrapper.findAll('header button').every(button => button.attributes('disabled') !== undefined)).toBe(true)
    wrapper.unmount()
  })
})
