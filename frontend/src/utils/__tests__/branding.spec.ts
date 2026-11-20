import { beforeEach, describe, expect, it } from 'vitest'
import { DEFAULT_SITE_LOGO, resolveSiteName, resolveSiteLogo, resolveSiteSubtitle, updateFavicon } from '@/utils/branding'

describe('website branding', () => {
  it('upgrades legacy website settings together, including a saved old logo', () => {
    for (const name of ['Aino', 'Aino API', 'Aino-API', 'Sub2API', ' aino api ']) {
      expect(resolveSiteName(name)).toBe('OmniFlow API')
      expect(resolveSiteLogo('data:image/jpeg;base64,old-logo', name)).toBe(DEFAULT_SITE_LOGO)
    }
    expect(resolveSiteSubtitle('Aino Subscription to API Conversion Platform')).toBe('AI Model Gateway')
  })

  it('preserves separately configured website branding', () => {
    expect(resolveSiteName('Custom Site')).toBe('Custom Site')
    expect(resolveSiteLogo('https://example.com/custom-logo.png', 'Custom Site')).toBe('https://example.com/custom-logo.png')
    expect(resolveSiteSubtitle('Custom description')).toBe('Custom description')
  })

  it('uses the new logo for missing, unsafe and old default image URLs', () => {
    for (const logo of ['', '/logo.svg', '/aino-app-icon.png', 'javascript:alert(1)', '//example.com/logo.png']) {
      expect(resolveSiteLogo(logo, 'OmniFlow API')).toBe(DEFAULT_SITE_LOGO)
    }
    expect(resolveSiteName('')).toBe('OmniFlow API')
  })
})

describe('updateFavicon', () => {
  beforeEach(() => {
    document.head.innerHTML = '<link rel="icon" href="/logo.svg">'
  })

  it('replaces the default favicon with the configured logo', () => {
    updateFavicon('https://example.com/custom-logo.png')

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.href).toBe('https://example.com/custom-logo.png')
    expect(link?.type).toBe('image/png')
  })

  it('ignores unsafe logo URLs', () => {
    updateFavicon('javascript:alert(1)')

    const link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
    expect(link?.getAttribute('href')).toBe('/logo.svg')
  })
})
