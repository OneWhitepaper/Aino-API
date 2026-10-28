import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { effectScope, ref } from 'vue'
import { flushPromises } from '@vue/test-utils'

const { getSiteCacheRate, updateSiteCacheRateVisibility, showError } = vi.hoisted(() => ({
  getSiteCacheRate: vi.fn(), updateSiteCacheRateVisibility: vi.fn(), showError: vi.fn(),
}))
vi.mock('@/api/usage', () => ({ getSiteCacheRate }))
vi.mock('@/api/admin/settings', () => ({ updateSiteCacheRateVisibility }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

import { useSiteCacheRate } from '../useSiteCacheRate'

describe('useSiteCacheRate', () => {
  const scopes: ReturnType<typeof effectScope>[] = []
  function setup(manage = false) {
    const start = ref('2026-10-27')
    const end = ref('2026-10-28')
    const scope = effectScope()
    scopes.push(scope)
    return { ...scope.run(() => useSiteCacheRate(start, end, manage))!, start, end }
  }
  beforeEach(() => {
    getSiteCacheRate.mockReset().mockResolvedValue({ visible_to_users: true, cache_hit_rate: 75 })
    updateSiteCacheRateVisibility.mockReset().mockImplementation(async (visible: boolean) => ({ visible_to_users: visible }))
    showError.mockReset()
  })
  afterEach(() => scopes.splice(0).forEach(scope => scope.stop()))

  it('requests only the date range and follows date changes', async () => {
    const state = setup()
    await flushPromises()
    expect(getSiteCacheRate).toHaveBeenCalledWith({ start_date: '2026-10-27', end_date: '2026-10-28' }, expect.anything())
    expect(state.showSiteCacheRate.value).toBe(true)
    expect(state.siteCacheRate.value?.cache_hit_rate).toBe(75)
    state.start.value = '2026-10-26'
    await flushPromises()
    expect(getSiteCacheRate).toHaveBeenLastCalledWith({ start_date: '2026-10-26', end_date: '2026-10-28' }, expect.anything())
  })

  it('ignores a late response from the previous date range', async () => {
    let resolveFirst!: (data: { visible_to_users: boolean; cache_hit_rate: number }) => void
    getSiteCacheRate.mockReturnValueOnce(new Promise(resolve => { resolveFirst = resolve }))
    const state = setup()
    state.end.value = '2026-10-29'
    await flushPromises()
    resolveFirst({ visible_to_users: true, cache_hit_rate: 5 })
    await flushPromises()
    expect(state.siteCacheRate.value?.cache_hit_rate).toBe(75)
  })

  it('hides disabled or failed responses from users and prevents settings writes', async () => {
    getSiteCacheRate.mockResolvedValue({ visible_to_users: false })
    const state = setup()
    await flushPromises()
    expect(state.showSiteCacheRate.value).toBe(false)
    await state.setSiteCacheVisibility(true)
    expect(updateSiteCacheRateVisibility).not.toHaveBeenCalled()
    getSiteCacheRate.mockRejectedValue(new Error('offline'))
    await state.refreshSiteCacheRate()
    expect(state.siteCacheRate.value).toBeNull()
    expect(state.showSiteCacheRate.value).toBe(false)
  })

  it('keeps the card visible to admins and only applies successfully saved settings', async () => {
    getSiteCacheRate.mockResolvedValue({ visible_to_users: false, cache_hit_rate: 75 })
    const state = setup(true)
    await flushPromises()
    expect(state.showSiteCacheRate.value).toBe(true)
    await state.setSiteCacheVisibility(true)
    expect(updateSiteCacheRateVisibility).toHaveBeenCalledWith(true)
    expect(state.siteCacheRate.value?.visible_to_users).toBe(true)
    updateSiteCacheRateVisibility.mockRejectedValue(new Error('save failed'))
    await state.setSiteCacheVisibility(false)
    expect(state.siteCacheRate.value?.visible_to_users).toBe(true)
    expect(showError).toHaveBeenCalledWith('usage.siteCacheRateSaveFailed')
  })
})
