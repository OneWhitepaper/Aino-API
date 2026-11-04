import apiClient from './client'

export interface RadarHistoryPoint { date: string; score: number }
export interface RadarPoint {
  model: string
  effort: string
  score: number | null
  coverage: number
  required_tasks: number
  score_status: string
  average_price_usd: number | null
  average_minutes: number | null
  price_samples: number
  duration_samples: number
  valid_grade_events: number
  latest_graded_at: string | null
  fetched_at: string | null
  stale: boolean
  history: RadarHistoryPoint[]
}

export interface CommunityRating {
  id: string
  label: string
  group: string
  average: number | null
  count: number
}

export interface RadarCommunity {
  window_hours: number
  updated_at: string | null
  fetched_at: string | null
  stale: boolean
  models: CommunityRating[]
}

export interface RadarSnapshot {
  source_name: string
  source_url: string
  benchmark: string
  score_version: string
  task_set_sha256: string
  score_max: number
  required_tasks: number
  fetched_at: string | null
  refreshing: boolean
  stale: boolean
  partial_failures: number
  cache_ttl_seconds: number
  retry_after_seconds: number
  points: RadarPoint[]
  community: RadarCommunity
}

export async function getIQRadar(signal?: AbortSignal): Promise<RadarSnapshot> {
  const { data } = await apiClient.get<RadarSnapshot>('/iq-radar', { signal })
  return data
}
