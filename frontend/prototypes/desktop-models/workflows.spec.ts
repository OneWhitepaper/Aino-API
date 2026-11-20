import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import App from './App.vue'
import type { DesktopSettings } from '@/api/admin/settings'

const storageKey = 'omniflow:desktop-models-design:v1'
enableAutoUnmount(afterEach)

beforeEach(() => {
  localStorage.clear()
  vi.stubGlobal('fetch', vi.fn(() => { throw new Error('Preview must remain offline') }))
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value() { this.setAttribute('open', '') } })
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value() { this.removeAttribute('open') } })
})
afterEach(() => { vi.unstubAllGlobals() })
function start() { return mount(App, { attachTo: document.body }) }
function persisted(): DesktopSettings { return JSON.parse(localStorage.getItem(storageKey)!) }

describe('native desktop catalog interaction preview', () => {
  it('keeps text/image defaults separate and restores the complete list after a no-match search', async () => {
    const app = start()
    expect(app.findAll('tbody tr')).toHaveLength(6)
    await app.get('[data-testid="model-search"]').setValue('not-a-model')
    expect(app.findAll('tbody tr')).toHaveLength(0)
    expect(app.text()).toContain('没有匹配的模型')
    await app.get('[data-testid="model-search"]').setValue('')
    expect(app.findAll('tbody tr')).toHaveLength(6)
    await app.get('[data-testid="tab-image"]').trigger('click')
    expect(app.findAll('tbody tr')).toHaveLength(2)
    expect((app.get('[data-testid="default-model"]').element as HTMLSelectElement).value).toBe('gpt-image')
    await app.get('[data-testid="tab-chat"]').trigger('click')
    expect((app.get('[data-testid="default-model"]').element as HTMLSelectElement).value).toBe('deepseek-chat')
    expect(fetch).not.toHaveBeenCalled()
  })

  it('stages an edited display name before explicitly persisting only to local storage', async () => {
    const app = start()
    await app.get('[data-testid="edit-deepseek-chat"]').trigger('click')
    await app.get('[data-testid="editor-name"]').setValue('DeepSeek · 团队推荐')
    await app.get('.editor-form').trigger('submit')
    await flushPromises()
    expect(app.get('[data-testid="edit-deepseek-chat"]').text()).toContain('DeepSeek · 团队推荐')
    expect(localStorage.getItem(storageKey)).toBeNull()
    await app.get('[data-testid="save-catalog"]').trigger('click')
    expect(persisted().models.find(model => model.id === 'deepseek-chat')!.display_name).toBe('DeepSeek · 团队推荐')
    expect(persisted().default_image_model_id).toBe('gpt-image')
    expect(fetch).not.toHaveBeenCalled()
  })

  it('clears prior route verification and default when the actual upstream model is changed', async () => {
    const app = start()
    await app.get('[data-testid="edit-deepseek-chat"]').trigger('click')
    await app.get('[data-testid="editor-model"]').setValue('another-upstream-model')
    await app.get('.editor-form').trigger('submit')
    await flushPromises()
    expect((app.get('[data-testid="default-model"]').element as HTMLSelectElement).value).toBe('')
    await app.get('[data-testid="save-catalog"]').trigger('click')
    expect(persisted().models.find(model => model.id === 'deepseek-chat')!.agent_verified).toBe(false)
    expect(persisted().default_model_id).toBeNull()
  })

  it('imports only missing group/model/protocol combinations and leaves them unverified', async () => {
    const app = start()
    await app.get('[data-testid="import-open"]').trigger('click')
    await app.get('[data-testid="import-fetch"]').trigger('click')
    const candidates = app.findAll('.import-model input')
    expect(candidates).toHaveLength(3)
    expect(candidates.filter(input => input.attributes('disabled') !== undefined)).toHaveLength(2)
    await app.get('[data-testid="import-confirm"]').trigger('click')
    expect(app.findAll('tbody tr')).toHaveLength(7)
    await app.get('[data-testid="save-catalog"]').trigger('click')
    const model = persisted().models.find(model => model.model === 'claude-opus-4-5')!
    expect(model.agent_verified).toBe(false)
    expect(model.capabilities.tools).toBe(false)
    expect(persisted().default_model_id).toBe('deepseek-chat')
    expect(fetch).not.toHaveBeenCalled()
  })

  it('confirms removal and clears only the removed default, retaining the image catalog', async () => {
    const app = start()
    await app.get('button[aria-label="移除 DeepSeek Chat"]').trigger('click')
    expect(app.findAll('tbody tr')).toHaveLength(6)
    await app.get('[data-testid="remove-confirm"]').trigger('click')
    expect(app.findAll('tbody tr')).toHaveLength(5)
    await app.get('[data-testid="save-catalog"]').trigger('click')
    expect(persisted().default_model_id).toBeNull()
    expect(persisted().default_image_model_id).toBe('gpt-image')
    expect(persisted().models.filter(model => model.api_mode === 'images_generations')).toHaveLength(2)
  })

  it('keeps invalid model edits open and protects unsaved fields when closing the editor', async () => {
    const app = start()
    await app.get('[data-testid="add-model"]').trigger('click')
    await app.get('.editor-form').trigger('submit')
    await flushPromises()
    expect(app.findAll('.editor-form [aria-invalid="true"]').length).toBeGreaterThan(0)
    expect(app.findAll('tbody tr')).toHaveLength(6)
    await app.get('[data-testid="editor-name"]').setValue('尚未保存的模型')
    await app.get('button[aria-label="关闭模型编辑"]').trigger('click')
    expect(app.text()).toContain('放弃当前模型编辑？')
    expect(app.get('[data-testid="editor-name"]').element).toHaveProperty('value', '尚未保存的模型')
    expect(localStorage.getItem(storageKey)).toBeNull()
  })

  it('persists the global catalog switch without modifying any model records', async () => {
    const app = start()
    await app.get('[data-testid="catalog-enabled"]').trigger('click')
    await app.get('[data-testid="preview-open"]').trigger('click')
    expect(app.text()).toContain('当前模型目录已关闭。')
    await app.get('button[aria-label="关闭桌面端预览"]').trigger('click')
    await app.get('[data-testid="save-catalog"]').trigger('click')
    expect(persisted().enabled).toBe(false)
    expect(persisted().models).toHaveLength(8)
    expect(fetch).not.toHaveBeenCalled()
  })

  it('lets a desktop user preview a different selection without changing the administrator default', async () => {
    const app = start()
    await app.get('[data-testid="preview-open"]').trigger('click')
    const option = app.findAll('.desktop-model-option').find(option => option.text().includes('Claude Sonnet'))!
    await option.trigger('click')
    expect(app.get('.desktop-model-trigger').text()).toContain('Claude Sonnet')
    expect(app.find('.desktop-model-menu').exists()).toBe(false)
    await app.get('.desktop-model-trigger').trigger('click')
    expect(app.find('.desktop-model-menu').exists()).toBe(true)
    expect((app.get('[data-testid="default-model"]').element as HTMLSelectElement).value).toBe('deepseek-chat')
    expect(app.find('[data-testid="save-catalog"]').exists()).toBe(false)
    expect(fetch).not.toHaveBeenCalled()
  })
})
