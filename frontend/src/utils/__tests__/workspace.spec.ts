import { describe, expect, it } from 'vitest'
import { buildConnectionExample, groupWorkspaceNavigation, workspaceCatalog } from '../workspace'

describe('workspace navigation and catalog access', () => {
  it('groups visible routes without losing custom pages or duplicating entries', () => {
    const items = ['/dashboard', '/keys', '/usage', '/profile', '/custom/help', '/model-plaza', '/redeem'].map(path => ({ path }))
    const sections = groupWorkspaceNavigation(items)
    expect(sections.map(section => section.key)).toEqual(['overview', 'access', 'billing', 'account', 'resources'])
    expect(sections.flatMap(section => section.items)).toHaveLength(items.length)
    expect(sections.find(section => section.key === 'resources')?.items).toEqual([{ path: '/custom/help' }])
    expect(groupWorkspaceNavigation([{ path: '/keys' }]).map(section => section.key)).toEqual(['access'])
  })

  it('keeps disabled catalogs hidden and prioritizes models accessible to the user', () => {
    expect(workspaceCatalog(null)).toBeNull()
    expect(workspaceCatalog({ available_channels_enabled: false, model_plaza_enabled: false })).toBeNull()
    expect(workspaceCatalog({ available_channels_enabled: true, model_plaza_enabled: true })?.source).toBe('channels')
    expect(workspaceCatalog({ available_channels_enabled: true }, true)).toBeNull()
    expect(workspaceCatalog({ model_plaza_enabled: true })?.to).toEqual({ path: '/model-plaza', query: { embedded: '1' } })
  })
})

describe('connection examples', () => {
  it.each(['https://api.example.com', 'https://api.example.com/', 'https://api.example.com/v1/'])('uses one version prefix for %s', base => {
    const example = buildConnectionExample(base, 'openai')
    expect(example).toContain('https://api.example.com/v1/chat/completions')
    expect(example).toContain('Bearer YOUR_API_KEY')
    expect(example).toContain('MODEL_ID')
  })

  it('uses Anthropic headers and escapes endpoint text in generated JavaScript', () => {
    const example = buildConnectionExample('https://example.com/proxy', 'anthropic')
    expect(example).toContain('https://example.com/proxy/v1/messages')
    expect(example).toContain('"anthropic-version": "2023-06-01"')
    expect(example).toContain('"max_tokens": 1024')
    expect(example).not.toContain('Bearer')
    expect(buildConnectionExample('https://example.com/"test', 'openai')).toContain('\\"test')
  })
})
