import { beforeEach, describe, expect, it, vi } from 'vitest'

const list = vi.hoisted(() => vi.fn())
vi.mock('@/api/keys', () => ({ keysAPI: { list } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAuthenticated: true }) }))

beforeEach(() => { vi.resetModules(); list.mockReset() })

describe('batch image access lookup', () => {
  it('retries a failed lookup when another consumer requests access', async () => {
    list.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({
      items: [{ key: 'sk-personal', status: 'active', group: { platform: 'gemini', allow_batch_image_generation: true } }], pages: 1,
    })
    const { useBatchImageAccess } = await import('../useBatchImageAccess')
    const access = useBatchImageAccess()
    expect(await access.refreshBatchImageAccess()).toBe(false)
    expect(access.batchImageAccessLoaded.value).toBe(false)
    expect(await useBatchImageAccess().refreshBatchImageAccess()).toBe(true)
    expect(list).toHaveBeenCalledTimes(2)
    expect(access.canUseBatchImage.value).toBe(true)
  })

  it('continues caching a successful lookup with no eligible keys', async () => {
    list.mockResolvedValue({ items: [], pages: 1 })
    const { useBatchImageAccess } = await import('../useBatchImageAccess')
    const access = useBatchImageAccess()
    expect(await access.refreshBatchImageAccess()).toBe(false)
    expect(access.batchImageAccessLoaded.value).toBe(true)
    expect(await access.refreshBatchImageAccess()).toBe(false)
    expect(list).toHaveBeenCalledTimes(1)
  })

  it('does not expose batch generation for desktop managed or empty secrets from an older server', async () => {
    const eligibleGroup = { platform: 'gemini', allow_batch_image_generation: true }
    list.mockResolvedValue({ items: [
      { key: '', desktop_managed: true, status: 'active', group: eligibleGroup },
      { key: 'sk-internal', desktop_managed: true, status: 'active', group: eligibleGroup },
      { key: '', status: 'active', group: eligibleGroup },
    ], pages: 1 })
    const { useBatchImageAccess } = await import('../useBatchImageAccess')
    const access = useBatchImageAccess()
    expect(await access.refreshBatchImageAccess()).toBe(false)
    expect(access.canUseBatchImage.value).toBe(false)
    expect(list).toHaveBeenCalledWith(1, 100, expect.objectContaining({ desktop_managed: false }))
  })
})
