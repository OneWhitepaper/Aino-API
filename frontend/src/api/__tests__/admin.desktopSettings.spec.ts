import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getDesktopSettings, updateDesktopSettings, type DesktopSettings } from '../admin/settings'
import { apiClient } from '../client'

vi.mock('../client', () => ({ apiClient: { get: vi.fn(), put: vi.fn() } }))
beforeEach(() => vi.clearAllMocks())

describe('independent admin desktop catalog API', () => {
  it('reads and writes only the scoped catalog endpoint', async () => {
    const catalog: DesktopSettings = { enabled: false, models: [], default_model_id: null, default_image_model_id: null, credential_ttl_seconds: 900 }
    vi.mocked(apiClient.get).mockResolvedValue({ data: catalog })
    vi.mocked(apiClient.put).mockResolvedValue({ data: catalog })
    expect(await getDesktopSettings()).toEqual(catalog)
    expect(await updateDesktopSettings(catalog)).toEqual(catalog)
    expect(apiClient.get).toHaveBeenCalledWith('/admin/settings/desktop')
    expect(apiClient.put).toHaveBeenCalledWith('/admin/settings/desktop', catalog)
  })
  it('propagates read and save failures without fixtures or a local fallback', async () => {
    vi.mocked(apiClient.get).mockRejectedValue(new Error('unavailable'))
    await expect(getDesktopSettings()).rejects.toThrow('unavailable')
    vi.mocked(apiClient.put).mockRejectedValue(new Error('forbidden'))
    await expect(updateDesktopSettings({ enabled: false, models: [], default_model_id: null, credential_ttl_seconds: 900 })).rejects.toThrow('forbidden')
  })
})
