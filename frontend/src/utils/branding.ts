import { sanitizeUrl } from '@/utils/url'

export const DEFAULT_SITE_NAME = 'OmniFlow API'
export const DEFAULT_SITE_LOGO = '/omniflow-logo.png'
export const DEFAULT_SITE_SUBTITLE = 'AI Model Gateway'

const legacySiteNames = new Set(['aino', 'aino api', 'aino-api', 'sub2api'])

export function isLegacySiteName(name?: string): boolean {
  return legacySiteNames.has(name?.trim().toLowerCase() || '')
}

export function resolveSiteName(name?: string): string {
  const value = name?.trim()
  return !value || isLegacySiteName(value) ? DEFAULT_SITE_NAME : value
}

export function resolveSiteLogo(logoUrl?: string, siteName?: string): string {
  if (isLegacySiteName(siteName)) return DEFAULT_SITE_LOGO

  const value = sanitizeUrl(logoUrl || '', { allowRelative: true, allowDataUrl: true })
  return !value || value === '/logo.svg' || value === '/aino-app-icon.png'
    ? DEFAULT_SITE_LOGO
    : value
}

export function resolveSiteSubtitle(subtitle?: string): string {
  const value = subtitle?.trim()
  return !value || ['subscription to api conversion platform', 'aino subscription to api conversion platform'].includes(value.toLowerCase())
    ? DEFAULT_SITE_SUBTITLE
    : value
}

export function updateFavicon(logoUrl: string): void {
  const sanitizedLogoUrl = sanitizeUrl(logoUrl, {
    allowRelative: true,
    allowDataUrl: true,
  })
  if (!sanitizedLogoUrl) {
    return
  }

  let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]')
  if (!link) {
    link = document.createElement('link')
    link.rel = 'icon'
    document.head.appendChild(link)
  }

  link.type = sanitizedLogoUrl.match(/^data:(image\/[^;,]+)/i)?.[1]
    || (/\.svg(?:[?#]|$)/i.test(sanitizedLogoUrl) ? 'image/svg+xml'
      : /\.png(?:[?#]|$)/i.test(sanitizedLogoUrl) ? 'image/png'
        : /\.jpe?g(?:[?#]|$)/i.test(sanitizedLogoUrl) ? 'image/jpeg' : 'image/x-icon')
  link.href = sanitizedLogoUrl
}
