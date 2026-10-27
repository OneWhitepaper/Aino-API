import type { CommunityRating, RadarPoint } from '@/api/iqRadar'

export const EFFORTS = ['low', 'medium', 'high', 'xhigh', 'max', 'ultra']
const names = new Intl.Collator('en', { numeric: true, sensitivity: 'base' })

export function effortOrder(effort: string): number {
  const index = EFFORTS.indexOf(effort.toLowerCase())
  return index < 0 ? EFFORTS.length : index
}

export function modelLabel(model: string): string {
  return model.replace(/^gpt-/i, 'GPT-').replace(/-(astra|sol|luna|terra)$/i, (_, family: string) => ` ${family[0].toUpperCase()}${family.slice(1)}`)
}

export function groupRadarModels(points: RadarPoint[]) {
  const groups = new Map<string, RadarPoint[]>()
  for (const point of points) {
    const group = groups.get(point.model) ?? []
    group.push(point)
    groups.set(point.model, group)
  }
  return [...groups].sort(([a], [b]) => names.compare(b, a)).map(([model, entries]) => ({
    model, label: modelLabel(model),
    points: [...entries].sort((a, b) => effortOrder(a.effort) - effortOrder(b.effort) || names.compare(a.effort, b.effort)),
  }))
}

export function selectRadarPoint(points: RadarPoint[], model?: string, effort?: string): RadarPoint | null {
  const shared = points.find(point => point.model === model && point.effort === effort)
  if (shared) return shared
  return points.reduce<RadarPoint | null>((best, point) => {
    if (!best || (point.score !== null && (best.score === null || point.score > best.score))) return point
    return best
  }, null)
}

export function communityEffort(model: CommunityRating): string {
  return EFFORTS.find(effort => model.id.endsWith(`-${effort}`)) ?? model.label.split(' ').at(-1) ?? model.id
}

export function groupCommunityModels(models: CommunityRating[]) {
  const groups = new Map<string, CommunityRating[]>()
  for (const model of models) {
    const name = model.group || model.label
    const group = groups.get(name) ?? []
    group.push(model)
    groups.set(name, group)
  }
  return [...groups].sort(([a], [b]) => names.compare(b, a)).map(([name, entries]) => ({
    name, votes: entries.reduce((total, entry) => total + entry.count, 0),
    models: [...entries].sort((a, b) => effortOrder(communityEffort(a)) - effortOrder(communityEffort(b)) || names.compare(a.id, b.id)),
  }))
}
