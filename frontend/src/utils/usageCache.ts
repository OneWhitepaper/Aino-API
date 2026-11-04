import type { UsageLog } from '@/types'

type CacheTokenUsage = Pick<UsageLog, 'input_tokens' | 'cache_read_tokens' | 'cache_creation_tokens'>

export const formatUsageCacheHitRate = (usage: CacheTokenUsage): string => {
  // Stored input tokens exclude cache reads and writes, matching TokenUsageTrend.
  const cacheReadTokens = usage.cache_read_tokens ?? 0
  const totalPromptTokens = (usage.input_tokens ?? 0) + cacheReadTokens + (usage.cache_creation_tokens ?? 0)
  return totalPromptTokens > 0 ? `${((cacheReadTokens / totalPromptTokens) * 100).toFixed(1)}%` : '—'
}
