export const IMAGE_QUALITY_MAPPING_CREDENTIAL_KEY = 'image_quality_mapping'

export const REQUESTED_IMAGE_QUALITIES = ['auto', 'low', 'medium', 'high', 'xhigh', 'max'] as const
export const UPSTREAM_IMAGE_QUALITIES = [...REQUESTED_IMAGE_QUALITIES, '1k', '2k', '4k'] as const

export interface ImageQualityMappingRow {
  model: string
  requestedQuality: string
  upstreamQuality: string
}

export type ImageQualityMappingError =
  | 'invalidModel'
  | 'invalidRequestedQuality'
  | 'invalidUpstreamQuality'
  | 'duplicate'
  | 'tooManyModels'

const isRecord = (value: unknown): value is Record<string, unknown> =>
  value !== null && typeof value === 'object' && !Array.isArray(value)

/** Preserve invalid stored entries as editable rows so saving cannot silently erase them. */
export function readImageQualityMapping(value: unknown): ImageQualityMappingRow[] {
  if (value === undefined || value === null) return []
  if (!isRecord(value)) return [{ model: '', requestedQuality: '', upstreamQuality: '' }]
  return Object.entries(value).flatMap(([model, qualities]) => {
    if (!isRecord(qualities) || Object.keys(qualities).length === 0) {
      return [{ model, requestedQuality: '', upstreamQuality: '' }]
    }
    return Object.entries(qualities).map(([requestedQuality, upstreamQuality]) => ({
      model,
      requestedQuality,
      upstreamQuality: typeof upstreamQuality === 'string' ? upstreamQuality : ''
    }))
  })
}

export function validateImageQualityMappingRows(
  rows: ImageQualityMappingRow[]
): ImageQualityMappingError | null {
  const seen = new Map<string, Set<string>>()
  for (const row of rows) {
    const model = row.model.trim()
    // Match the API's exact-name contract; wildcards and control characters are not supported.
    const hasControlCharacter = [...model].some((character) => {
      const code = character.codePointAt(0) ?? 0
      return code < 32 || (code >= 127 && code <= 159)
    })
    if (!model || new TextEncoder().encode(model).length > 256 || /[*?]/u.test(model) || hasControlCharacter) {
      return 'invalidModel'
    }
    if (!(REQUESTED_IMAGE_QUALITIES as readonly string[]).includes(row.requestedQuality)) {
      return 'invalidRequestedQuality'
    }
    if (!(UPSTREAM_IMAGE_QUALITIES as readonly string[]).includes(row.upstreamQuality)) {
      return 'invalidUpstreamQuality'
    }
    const qualities = seen.get(model) ?? new Set<string>()
    if (qualities.has(row.requestedQuality)) return 'duplicate'
    qualities.add(row.requestedQuality)
    seen.set(model, qualities)
  }
  return seen.size > 64 ? 'tooManyModels' : null
}

/** Build only after validation. Empty rows intentionally serialize to {} to clear a saved mapping. */
export function buildImageQualityMapping(rows: ImageQualityMappingRow[]): Record<string, Record<string, string>> {
  const models = new Map<string, Map<string, string>>()
  for (const row of rows) {
    const model = row.model.trim()
    const qualities = models.get(model) ?? new Map<string, string>()
    qualities.set(row.requestedQuality, row.upstreamQuality)
    models.set(model, qualities)
  }
  return Object.fromEntries([...models].map(([model, qualities]) => [model, Object.fromEntries(qualities)]))
}
