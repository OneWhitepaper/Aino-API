import { describe, expect, it } from 'vitest'
import { groupRadarModels, selectRadarPoint, groupCommunityModels } from '../radarData'
import type { RadarPoint } from '@/api/iqRadar'

const point = (model: string, effort: string, score: number | null): RadarPoint => ({
  model, effort, score, coverage: 64, required_tasks: 64, score_status: 'complete',
  average_price_usd: null, average_minutes: null, price_samples: 0, duration_samples: 0,
  valid_grade_events: 1, latest_graded_at: null, fetched_at: null, stale: false, history: [],
})

describe('radar selections', () => {
  it('sorts newer model generations first and keeps every effort together', () => {
    const rows = groupRadarModels([point('gpt-6-astra', 'high', 100), point('gpt-6.1-sol', 'low', 76), point('gpt-6-astra', 'low', 0)])
    expect(rows.map(row => row.model)).toEqual(['gpt-6.1-sol', 'gpt-6-astra'])
    expect(rows[1].points.map(p => p.effort)).toEqual(['low', 'high'])
    expect(rows[1].points[0].score).toBe(0)
  })

  it('honors a shared model/effort selection and otherwise chooses the best valid score', () => {
    const points = [point('gpt-6-astra', 'low', null), point('gpt-6-astra', 'high', 110), point('gpt-6.1-sol', 'low', 0)]
    expect(selectRadarPoint(points, 'gpt-6.1-sol', 'low')?.score).toBe(0)
    expect(selectRadarPoint(points, 'unknown', 'low')?.score).toBe(110)
    expect(selectRadarPoint([point('gpt-6-astra', 'low', null)])?.score).toBeNull()
    expect(selectRadarPoint([])).toBeNull()
  })

  it('groups community votes without dropping models absent from the benchmark', () => {
    const groups = groupCommunityModels([
      { id: 'gpt-6.1-terra-high', label: 'GPT-6.1 Terra high', group: 'GPT-6.1 Terra', average: 5, count: 2 },
      { id: 'gpt-6.1-terra-low', label: 'GPT-6.1 Terra low', group: 'GPT-6.1 Terra', average: null, count: 0 },
    ])
    expect(groups).toHaveLength(1)
    expect(groups[0].votes).toBe(2)
    expect(groups[0].models[0].average).toBeNull()
  })
})
