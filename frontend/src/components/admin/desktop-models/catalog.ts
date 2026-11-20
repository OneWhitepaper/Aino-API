import type { DesktopModelEntry, DesktopSettings } from '@/api/admin/settings'
import type { AdminGroup } from '@/types'
import { modelAllowedForImport } from '../settings/desktopModelImport'

export type ModelPurpose = 'chat' | 'image'
export const purposes = ['chat', 'image'] as const
export const platforms: DesktopModelEntry['platform'][] = ['openai', 'anthropic', 'gemini', 'antigravity', 'grok', 'kimi', 'zhipu', 'deepseek', 'minimax', 'opencode_go']
export const imageQualities = ['auto', 'low', 'medium', 'high', 'xhigh', 'max'] as const
export const protocolLabels: Record<DesktopModelEntry['api_mode'], string> = {
  chat_completions: 'Chat Completions', responses: 'Responses', anthropic_messages: 'Anthropic Messages', images_generations: 'Images API'
}
export function clone<T>(value: T): T { return JSON.parse(JSON.stringify(value)) as T }
export function purposeOf(entry: DesktopModelEntry): ModelPurpose { return entry.api_mode === 'images_generations' ? 'image' : 'chat' }
export function canDefault(entry: DesktopModelEntry, groups: AdminGroup[]): boolean {
  const group = groups.find(group => group.id === entry.group_id)
  if (!group || group.status !== 'active' || !modelAllowedForImport(group, entry.model)) return false
  return purposeOf(entry) === 'image'
    ? !!entry.capabilities.image_generation && group.allow_image_generation && ['openai', 'grok'].includes(entry.platform)
    : entry.agent_verified && entry.capabilities.tools
}
export function routeChanged(previous: DesktopModelEntry, next: DesktopModelEntry): boolean {
  return (['model', 'group_id', 'platform', 'api_mode'] as const).some(key => previous[key] !== next[key])
}
export function normalizeDefaults(settings: DesktopSettings): DesktopSettings {
  const result = clone(settings)
  if (!result.models.some(entry => entry.id === result.default_model_id && purposeOf(entry) === 'chat' && entry.agent_verified && entry.capabilities.tools)) result.default_model_id = null
  if (!result.models.some(entry => entry.id === result.default_image_model_id && purposeOf(entry) === 'image' && entry.capabilities.image_generation)) result.default_image_model_id = null
  return result
}
export function replaceEntry(settings: DesktopSettings, previousID: string | null, entry: DesktopModelEntry): DesktopSettings {
  const result = clone(settings)
  const index = result.models.findIndex(model => model.id === previousID)
  if (index < 0) result.models.push(clone(entry))
  else result.models[index] = { ...clone(entry), agent_verified: routeChanged(result.models[index], entry) ? false : entry.agent_verified }
  return normalizeDefaults(result)
}
export function newEntry(purpose: ModelPurpose, models: DesktopModelEntry[]): DesktopModelEntry {
  const image = purpose === 'image'
  return {
    id: '', model: '', display_name: '', provider_label: '', group_id: 0, platform: 'openai',
    api_mode: image ? 'images_generations' : 'chat_completions',
    sort_order: Math.max(-1, ...models.map(model => model.sort_order)) + 1,
    agent_verified: false, context_window: null, max_output_tokens: null,
    capabilities: { tools: false, vision: false, reasoning: false, ...(image ? { image_generation: true, image_quality_options: [] } : {}) }
  }
}
export function validateEntry(entry: DesktopModelEntry, models: DesktopModelEntry[], previousID: string | null, groups: AdminGroup[]): Record<string, string> {
  const errors: Record<string, string> = {}
  for (const key of ['display_name', 'model', 'provider_label'] as const) {
    if (!entry[key].trim()) errors[key] = 'required'
    // Server limits these strings by UTF-8 bytes, including CJK labels.
    else if (new TextEncoder().encode(entry[key]).length > 256 || /[\p{Cc}]/u.test(entry[key])) errors[key] = 'text'
  }
  if (!/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/.test(entry.id)) errors.id = 'id'
  else if (models.some(model => model.id === entry.id && model.id !== previousID)) errors.id = 'duplicateID'
  const group = groups.find(group => group.id === entry.group_id)
  if (!group) errors.group_id = 'group'
  else {
    if (group.platform !== 'composite' && group.platform !== entry.platform) errors.platform = 'platform'
    if (!modelAllowedForImport(group, entry.model)) errors.model = 'allowlist'
    if (purposeOf(entry) === 'image' && !group.allow_image_generation) errors.group_id = 'imagePermission'
  }
  if (!platforms.includes(entry.platform) || (purposeOf(entry) === 'image' && !['openai', 'grok'].includes(entry.platform))) errors.platform = 'platform'
  if (!Object.prototype.hasOwnProperty.call(protocolLabels, entry.api_mode)) errors.api_mode = 'protocol'
  if (!Number.isSafeInteger(entry.sort_order)) errors.sort_order = 'integer'
  for (const key of ['context_window', 'max_output_tokens'] as const) {
    const value = entry[key]
    if (value != null && (!Number.isSafeInteger(value) || value <= 0)) errors[key] = 'limit'
  }
  if (entry.context_window && entry.max_output_tokens && entry.max_output_tokens > entry.context_window) errors.max_output_tokens = 'outputLimit'
  if (purposeOf(entry) === 'image' ? !entry.capabilities.image_generation : entry.capabilities.image_generation) errors.api_mode = 'protocol'
  return errors
}
