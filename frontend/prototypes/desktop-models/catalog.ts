import type { DesktopModelEntry, DesktopSettings } from '@/api/admin/settings'

export type Purpose = 'chat' | 'image'
export interface CatalogGroup { id: number; name: string; platform: DesktopModelEntry['platform'] }
export const groups: CatalogGroup[] = [
  { id: 1, name: 'DeepSeek', platform: 'deepseek' },
  { id: 2, name: 'Claude', platform: 'anthropic' },
  { id: 3, name: 'OpenAI', platform: 'openai' },
  { id: 4, name: 'Gemini', platform: 'gemini' },
  { id: 5, name: '图像生成', platform: 'openai' },
]

export function purposeOf(model: DesktopModelEntry): Purpose {
  return model.api_mode === 'images_generations' ? 'image' : 'chat'
}
export function canDefault(model: DesktopModelEntry): boolean {
  return purposeOf(model) === 'image'
    ? !!model.capabilities.image_generation
    : model.agent_verified && model.capabilities.tools
}
export function clone<T>(value: T): T { return JSON.parse(JSON.stringify(value)) as T }

function entry(id: string, name: string, model: string, group: number, verified = true): DesktopModelEntry {
  const platform = groups.find(item => item.id === group)!.platform
  return {
    id, display_name: name, model, group_id: group,
    provider_label: groups.find(item => item.id === group)!.name,
    platform, api_mode: platform === 'anthropic' ? 'anthropic_messages' : platform === 'openai' ? 'responses' : 'chat_completions',
    sort_order: 0, agent_verified: verified, context_window: 128000, max_output_tokens: 8192,
    capabilities: { tools: true, vision: platform !== 'deepseek', reasoning: platform !== 'anthropic' },
  }
}

// All records are local interaction fixtures, never a production catalog or capability assertion.
export function initialSettings(): DesktopSettings {
  const models = [
    entry('deepseek-chat', 'DeepSeek Chat', 'deepseek-chat', 1),
    entry('claude-sonnet', 'Claude Sonnet 4.5', 'claude-sonnet-4-5', 2),
    entry('gpt-5', 'GPT-5', 'gpt-5', 3),
    entry('gemini-pro', 'Gemini 2.5 Pro', 'gemini-2.5-pro', 4),
    entry('deepseek-reasoner', 'DeepSeek Reasoner', 'deepseek-reasoner', 1),
    entry('claude-haiku', 'Claude Haiku 4.5', 'claude-haiku-4-5', 2, false),
    { ...entry('gpt-image', 'GPT Image', 'gpt-image-1', 5), api_mode: 'images_generations' as const,
      context_window: null, max_output_tokens: null, agent_verified: false,
      capabilities: { tools: false, vision: false, reasoning: false, image_generation: true, image_quality_options: ['auto', 'low', 'medium', 'high'] } },
    { ...entry('gpt-image-mini', 'GPT Image Mini', 'gpt-image-1-mini', 5), api_mode: 'images_generations' as const,
      context_window: null, max_output_tokens: null, agent_verified: false,
      capabilities: { tools: false, vision: false, reasoning: false, image_generation: true, image_quality_options: ['auto', 'low', 'medium', 'high'] } },
  ].map((model, index) => ({ ...model, sort_order: index }))
  return { enabled: true, models, default_model_id: 'deepseek-chat', default_image_model_id: 'gpt-image', credential_ttl_seconds: 3600 }
}

export function candidates(purpose: Purpose, groupID: number): DesktopModelEntry[] {
  const options = purpose === 'image'
    ? ['gpt-image-1', 'gpt-image-1-mini']
    : groupID === 1 ? ['deepseek-chat', 'deepseek-reasoner']
      : groupID === 2 ? ['claude-sonnet-4-5', 'claude-opus-4-5', 'claude-haiku-4-5']
        : groupID === 4 ? ['gemini-2.5-pro', 'gemini-2.5-flash'] : ['gpt-5', 'gpt-5-mini', 'gpt-4.1']
  return options.map((model, index) => ({
    ...entry(`import-${groupID}-${model}`, model, model, groupID, false), sort_order: index,
    ...(purpose === 'image' ? {
      api_mode: 'images_generations' as const, context_window: null, max_output_tokens: null,
      capabilities: { tools: false, vision: false, reasoning: false, image_generation: true, image_quality_options: [] },
    } : { capabilities: { tools: false, vision: false, reasoning: false } }),
  }))
}

export function replaceEntry(settings: DesktopSettings, previousID: string | null, next: DesktopModelEntry): DesktopSettings {
  const result = clone(settings)
  const index = result.models.findIndex(model => model.id === previousID)
  if (index < 0) result.models.push(clone(next))
  else {
    const previous = result.models[index]
    const routeChanged = (['model', 'group_id', 'platform', 'api_mode'] as const).some(key => previous[key] !== next[key])
    result.models[index] = { ...clone(next), agent_verified: routeChanged ? false : next.agent_verified }
  }
  return normalizeDefaults(result)
}

export function normalizeDefaults(settings: DesktopSettings): DesktopSettings {
  const result = clone(settings)
  if (!result.models.some(model => model.id === result.default_model_id && purposeOf(model) === 'chat' && canDefault(model))) result.default_model_id = null
  if (!result.models.some(model => model.id === result.default_image_model_id && purposeOf(model) === 'image' && canDefault(model))) result.default_image_model_id = null
  return result
}

export function validateEntry(model: DesktopModelEntry, models: DesktopModelEntry[], previousID: string | null): Record<string, string> {
  const errors: Record<string, string> = {}
  for (const key of ['id', 'display_name', 'model', 'provider_label'] as const) if (!model[key].trim()) errors[key] = '请填写此项。'
  if (!/^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$/.test(model.id)) errors.id = '使用字母、数字、短横线或下划线，最多 128 个字符。'
  if (models.some(item => item.id === model.id && item.id !== previousID)) errors.id = '目录 ID 已存在，请使用其他 ID。'
  if (!groups.some(group => group.id === model.group_id)) errors.group_id = '请选择已有分组。'
  if (purposeOf(model) === 'chat') {
    if (model.context_window != null && (!Number.isInteger(model.context_window) || model.context_window < 1)) errors.context_window = '请输入大于 0 的整数，未知时留空。'
    if (model.max_output_tokens != null && (!Number.isInteger(model.max_output_tokens) || model.max_output_tokens < 1)) errors.max_output_tokens = '请输入大于 0 的整数，未知时留空。'
    if (model.context_window && model.max_output_tokens && model.max_output_tokens > model.context_window) errors.max_output_tokens = '最大输出不能超过上下文长度。'
  }
  return errors
}

export const protocolLabels: Record<DesktopModelEntry['api_mode'], string> = {
  chat_completions: 'Chat Completions', responses: 'Responses', anthropic_messages: 'Messages', images_generations: 'Images API',
}
