import type { ApiKey } from '@/types'

export const DESKTOP_APPLICATIONS = [
  { id: 'aino', name: 'Aino', icon: '/aino-app-icon.png' },
  { id: 'momo', name: 'MoMo', icon: '/momo-app-icon.png' }
] as const

export function getDesktopApplication(appId: string) {
  return DESKTOP_APPLICATIONS.find(application => application.id === appId)
}

/** Existing desktop authorizations predate the application field and belong to Aino. */
export function getDesktopApplicationId(key: ApiKey): string {
  if (!key.desktop_managed) return ''
  return key.desktop_app || 'aino'
}
