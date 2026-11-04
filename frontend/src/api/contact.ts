import { apiClient } from './client'

export type ContactType = 'wechat' | 'qq' | 'email' | 'phone' | 'link' | 'other'
export type ContactGroupPlatform = 'wechat' | 'qq' | 'other'

export interface ContactMethod {
  type: ContactType
  label: string
  value: string
  url: string
  enabled: boolean
}

export interface ContactGroup {
  platform: ContactGroupPlatform
  name: string
  description: string
  qr_code: string
  group_number: string
  join_url: string
  enabled: boolean
}

export interface ContactPageSettings {
  description: string
  service_hours: string
  methods: ContactMethod[]
  groups: ContactGroup[]
}

export async function getContactPage(): Promise<ContactPageSettings> {
  const { data } = await apiClient.get<ContactPageSettings>('/contact')
  return data
}

export async function getContactPageSettings(): Promise<ContactPageSettings> {
  const { data } = await apiClient.get<ContactPageSettings>('/admin/settings/contact')
  return data
}

export async function updateContactPageSettings(settings: ContactPageSettings): Promise<ContactPageSettings> {
  const { data } = await apiClient.put<ContactPageSettings>('/admin/settings/contact', settings)
  return data
}
