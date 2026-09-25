import { describe, expect, it } from 'vitest'
import { buildDesktopModelDrafts, modelAllowedForImport, prepareDesktopModelImport } from '../desktopModelImport'
import type { AdminGroup } from '@/types'

const group = { id: 17, platform: 'openai' } as AdminGroup
const account = { name: 'Source', platform: 'openai' as const }

describe('desktop model imports', () => {
  it('uses returned metadata, rejects unusable IDs, and leaves Agent verification unknown', () => {
    const drafts = buildDesktopModelDrafts({ models: [' team/model ', 'team/model', '', '*', 'x'.repeat(257), 'missing'], metadata: { 'team/model': { id: 'team/model', display_name: 'Team model', context_window: 32000, max_output_tokens: 4000, input_modalities: ['text', 'image'], reasoning: true } } }, group, account, 'responses')
    expect(drafts).toHaveLength(2)
    expect(drafts[0]).toMatchObject({ id: 'g17-team-model', model: 'team/model', group_id: 17, display_name: 'Team model', provider_label: 'openai', context_window: 32000, max_output_tokens: 4000, agent_verified: false, capabilities: { tools: false, vision: true, reasoning: true } })
    expect(drafts[1]).toMatchObject({ context_window: null, max_output_tokens: null, agent_verified: false, capabilities: { tools: false, vision: false, reasoning: false } })
  })

  it('skips existing routes and resolves ID collisions without changing saved models', () => {
    const drafts = buildDesktopModelDrafts({ models: ['a/b', 'a-b', 'new'] }, group, account, 'responses')
    const saved = { ...drafts[0], agent_verified: true, sort_order: 10, capabilities: { tools: true, vision: true, reasoning: true } }
    const result = prepareDesktopModelImport([saved], drafts)
    expect(result.map(entry => entry.id)).toEqual(['g17-a-b-2', 'g17-new'])
    expect(result.map(entry => entry.sort_order)).toEqual([11, 12])
    expect(saved.agent_verified).toBe(true)
    expect(saved.id).toBe('g17-a-b')
    expect(prepareDesktopModelImport([saved], [{ ...drafts[0], api_mode: 'chat_completions' }])).toHaveLength(1)
  })

  it('keeps colliding long IDs valid and never exceeds catalog capacity', () => {
    const drafts = buildDesktopModelDrafts({ models: ['x'.repeat(180), 'x'.repeat(181)] }, group, account, 'responses')
    const imported = prepareDesktopModelImport([], drafts)
    expect(imported[0].id).toHaveLength(128)
    expect(imported[1].id).toHaveLength(128)
    expect(imported[0].id).not.toBe(imported[1].id)
    const existing = Array.from({ length: 199 }, (_, index) => ({ ...imported[0], id: `saved-${index}`, model: `saved-${index}` }))
    expect(prepareDesktopModelImport(existing, imported)).toHaveLength(1)
    expect(existing).toHaveLength(199)
  })

  it('respects group restrictions and discards inconsistent output limits', () => {
    const restricted = { ...group, model_allowlist: { enabled: true, models: ['GPT-*', 'gemini-test'] } }
    expect(modelAllowedForImport(restricted, 'gpt-test')).toBe(true)
    expect(modelAllowedForImport(restricted, 'models/gemini-test')).toBe(true)
    expect(modelAllowedForImport(restricted, 'other')).toBe(false)
    expect(modelAllowedForImport({ ...group, model_allowlist: { enabled: true, models: [] } }, 'gpt-test')).toBe(false)
    const [draft] = buildDesktopModelDrafts({ models: ['invalid'], metadata: { invalid: { id: 'invalid', context_window: 100, max_output_tokens: 200 } } }, group, account, 'responses')
    expect(draft.context_window).toBe(100)
    expect(draft.max_output_tokens).toBeNull()
  })

  it.each([
    ['gpt-5.4', 'gpt-5.4-high'],
    ['gpt-5.4-mini', 'openai/GPT-5.4mini_xhigh'],
    ['gpt-5.3-codex', 'gpt-5.3-high'],
    ['gpt-5.3-codex-spark', 'gpt-5.3codexspark-high'],
    ['gpt-5.2', 'gpt-5.2-codex-high'],
    ['gpt-5.5', 'gpt-5.5-codex-high'],
    ['gpt-5.5-pro', 'gpt-5.5-pro-low'],
    ['gpt-5.6-sol', 'gpt-5.6-high'],
    ['gpt-5.6-terra', 'gpt-5.6-terra-medium'],
    ['gpt-6-sol', 'gpt-6-sol-minimal'],
    ['gpt-6-luna', 'gpt-6-luna-xhigh'],
    ['claude-sonnet-4-5-20250929', 'claude-sonnet-4-5'],
    ['claude-opus-4-5-20251101', 'claude-opus-4-5-thinking'],
    ['claude-haiku-4-5-20251001', 'claude-haiku-4-5']
  ])('permits backend-supported alias %s <- %s', (allowed, model) => {
    const restricted = { ...group, model_allowlist: { enabled: true, models: [allowed] } }
    expect(modelAllowedForImport(restricted, model)).toBe(true)
  })

  it.each([
    ['gpt-4.1', 'gpt-4.1-high'],
    ['gpt-5.4', 'gpt-5.4-max'],
    ['gpt-5.6-sol', 'gpt-5.6-extra-high'],
    ['gpt-6-astra', 'gpt-6-astra-high'],
    ['custom-model', 'custom-model-high'],
    ['claude-sonnet-4-5', 'claude-sonnet-4-5-thinking'],
    ['claude-sonnet-4-5-20250929', 'CLAUDE-SONNET-4-5']
  ])('keeps server-rejected alias %s <- %s unavailable', (allowed, model) => {
    const restricted = { ...group, model_allowlist: { enabled: true, models: [allowed] } }
    expect(modelAllowedForImport(restricted, model)).toBe(false)
  })
})
