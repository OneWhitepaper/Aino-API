import type { DesktopModelEntry } from '@/api/admin/settings'
import type { SyncUpstreamModelsResult } from '@/api/admin/accounts'
import type { AccountListItem, AdminGroup } from '@/types'

export const DESKTOP_MODEL_LIMIT = 200

export function sameDesktopModel(a: DesktopModelEntry, b: DesktopModelEntry): boolean {
  return a.group_id === b.group_id && a.platform === b.platform && a.api_mode === b.api_mode && a.model === b.model
}

function textWithinLimit(value: string): string {
  let result = ''
  for (const character of value.trim()) {
    if (new TextEncoder().encode(result + character).length > 256) break
    const code = character.charCodeAt(0)
    if (code >= 32 && (code < 127 || code > 159)) result += character
  }
  return result
}

function positiveLimit(value: number | undefined): number | null {
  return value !== undefined && Number.isSafeInteger(value) && value > 0 ? value : null
}

// Keep these candidates aligned with groupModelAllowlistCandidates in the server.
const claudeModelAliases = new Map([
  ['claude-sonnet-4-5', 'claude-sonnet-4-5-20250929'],
  ['claude-opus-4-5', 'claude-opus-4-5-20251101'],
  ['claude-haiku-4-5', 'claude-haiku-4-5-20251001']
])

function normalizeOpenAIReasoningModel(model: string): string {
  const modelID = model.split('/').pop()!.trim()
  const suffix = modelID.toLowerCase().split(/[-_ ]+/).filter(Boolean).pop() || ''
  // NormalizeOpenAICompatRequestedModel only invokes Codex normalization for
  // GPT reasoning aliases. In particular, max and arbitrary suffixes stay exact.
  if (!/^gpt-/i.test(modelID) || !/^(none|minimal|low|medium|high|xhigh|extrahigh)$/.test(suffix)) return model
  if (/^gpt-image-/i.test(modelID)) return modelID

  let normalized = modelID.toLowerCase().replace(/_/g, '-').replace(/\s+/g, '-').replace(/-+/g, '-')
  for (const [from, to] of [
    ['gpt-5.4mini', 'gpt-5.4-mini'],
    ['gpt-5.4nano', 'gpt-5.4-nano'],
    ['gpt-5.3-codexspark', 'gpt-5.3-codex-spark'],
    ['gpt-5.3codexspark', 'gpt-5.3-codex-spark'],
    ['gpt-5.3codex', 'gpt-5.3-codex']
  ]) normalized = normalized.split(from).join(to)

  const gpt6 = normalized.match(/^(gpt-6-(?:sol|luna))-(?:none|minimal|low|medium|high|xhigh)$/)
  if (gpt6) return gpt6[1]
  const gpt56 = ['gpt-5.6-sol', 'gpt-5.6-terra', 'gpt-5.6-luna'].find(base => normalized.includes(base))
  if (gpt56) return gpt56
  if (normalized.startsWith('gpt-5.6-')) {
    return /^gpt-5\.6-(?:none|minimal|low|medium|high|xhigh)$/.test(normalized) ? 'gpt-5.6-sol' : modelID
  }

  // Match the server's ordered family rules; unknown names retain their suffix.
  const family = ['gpt-5.5-pro', 'gpt-5.5', 'gpt-5.4-mini', 'gpt-5.4-nano', 'gpt-5.4', 'gpt-5.2', 'gpt-5.3-codex-spark', 'gpt-5.3-codex'].find(base => normalized.includes(base))
  if (family) return family
  if (normalized.includes('gpt-5.3') || normalized.includes('codex')) return 'gpt-5.3-codex'
  if (normalized.includes('gpt-5')) return 'gpt-5.4'
  return modelID
}

export function modelAllowedForImport(group: AdminGroup, model: string): boolean {
  if (!group.model_allowlist?.enabled) return true
  model = model.trim()
  const withoutThinking = model.replace(/-thinking$/, '')
  const candidates = [
    model,
    model.startsWith('models/') ? model.slice(7) : model,
    claudeModelAliases.get(withoutThinking) || withoutThinking,
    normalizeOpenAIReasoningModel(model)
  ].map(value => value.trim().toLowerCase())
  return group.model_allowlist.models.some(raw => {
    const pattern = raw.trim().toLowerCase()
    return pattern && candidates.some(candidate => pattern.endsWith('*') ? candidate.startsWith(pattern.slice(0, -1)) : candidate === pattern)
  })
}

export function buildDesktopModelDrafts(
  catalog: SyncUpstreamModelsResult,
  group: AdminGroup,
  account: Pick<AccountListItem, 'platform'>,
  apiMode: DesktopModelEntry['api_mode']
): DesktopModelEntry[] {
  const models = [...new Set(catalog.models.map(model => model.trim()))]
    .filter(model => model && new TextEncoder().encode(model).length <= 256 && ![...model].some(character => {
      const code = character.charCodeAt(0)
      return code < 32 || (code >= 127 && code <= 159) || character === '*'
    }))
  return models.map((model, index) => {
    const metadata = catalog.metadata?.[model]
    const contextWindow = positiveLimit(metadata?.context_window) ?? positiveLimit(metadata?.max_context_window)
    const outputLimit = positiveLimit(metadata?.max_output_tokens)
    return {
      id: `g${group.id}-${model.replace(/[^a-zA-Z0-9._-]/g, '-')}`.slice(0, 128),
      group_id: group.id,
      model,
      display_name: textWithinLimit(metadata?.display_name || model) || model,
      provider_label: account.platform,
      platform: account.platform,
      api_mode: apiMode,
      sort_order: index,
      agent_verified: false,
      capabilities: { tools: false, vision: metadata?.input_modalities?.includes('image') === true, reasoning: metadata?.reasoning === true },
      context_window: contextWindow,
      max_output_tokens: contextWindow && outputLimit && outputLimit > contextWindow ? null : outputLimit
    }
  })
}

export function prepareDesktopModelImport(existing: DesktopModelEntry[], selected: DesktopModelEntry[]): DesktopModelEntry[] {
  const ids = new Set(existing.map(model => model.id))
  const additions: DesktopModelEntry[] = []
  let order = Math.max(-1, ...existing.map(model => Number.isFinite(model.sort_order) ? model.sort_order : -1)) + 1
  for (const draft of selected) {
    if (existing.length + additions.length >= DESKTOP_MODEL_LIMIT) break
    if ([...existing, ...additions].some(model => sameDesktopModel(model, draft))) continue
    let id = draft.id
    for (let suffix = 2; ids.has(id); suffix++) {
      const ending = `-${suffix}`
      id = draft.id.slice(0, 128 - ending.length) + ending
    }
    ids.add(id)
    additions.push({ ...draft, id, sort_order: order++, agent_verified: false, capabilities: { ...draft.capabilities, tools: false } })
  }
  return additions
}
