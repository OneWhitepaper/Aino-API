import type { PublicSettings } from '@/types'
import { FeatureFlags, resolveFeatureFlag } from './featureFlags'

export function groupWorkspaceNavigation<T extends { path: string }>(items: T[]) {
  const sections = [
    { key: 'overview', paths: ['/dashboard'] },
    { key: 'access', paths: ['/keys', '/model-plaza', '/available-channels', '/monitor', '/batch-image'] },
    { key: 'billing', paths: ['/usage', '/purchase', '/orders', '/subscriptions', '/redeem'] },
    { key: 'account', paths: ['/profile', '/affiliate'] },
    { key: 'resources', paths: [] as string[] },
  ]
  const known = new Set(sections.flatMap(section => section.paths))
  return sections.map(section => ({
    key: section.key,
    items: section.key === 'resources'
      ? items.filter(item => !known.has(item.path))
      : section.paths.flatMap(path => items.filter(item => item.path === path)),
  })).filter(section => section.items.length > 0)
}

export function workspaceCatalog(settings: Partial<PublicSettings> | null | undefined, simple = false) {
  if (!simple && resolveFeatureFlag(settings, FeatureFlags.availableChannels)) {
    return { source: 'channels' as const, to: { path: '/available-channels' } }
  }
  if (resolveFeatureFlag(settings, FeatureFlags.modelPlaza)) {
    return { source: 'plaza' as const, to: { path: '/model-plaza', query: { embedded: '1' } } }
  }
  return null
}

// These are examples only: never interpolate a real API key into the dashboard.
export function buildConnectionExample(baseUrl: string, protocol: 'openai' | 'anthropic') {
  const root = baseUrl.trim().replace(/\/+$/, '').replace(/\/v1$/, '')
  const endpoint = `${root}/v1/${protocol === 'anthropic' ? 'messages' : 'chat/completions'}`
  const headers = protocol === 'anthropic'
    ? { 'Content-Type': 'application/json', 'x-api-key': 'YOUR_API_KEY', 'anthropic-version': '2023-06-01' }
    : { 'Content-Type': 'application/json', Authorization: 'Bearer YOUR_API_KEY' }
  const body = {
    model: 'MODEL_ID',
    ...(protocol === 'anthropic' ? { max_tokens: 1024 } : {}),
    messages: [{ role: 'user', content: 'Hello' }],
  }
  return `const response = await fetch(${JSON.stringify(endpoint)}, {\n  method: "POST",\n  headers: ${JSON.stringify(headers, null, 2).replace(/\n/g, '\n  ')},\n  body: JSON.stringify(${JSON.stringify(body, null, 2).replace(/\n/g, '\n  ')})\n});\nconsole.log(await response.json());`
}
