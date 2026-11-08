import { describe, expect, it } from 'vitest'
import { buildImageQualityMapping, readImageQualityMapping, validateImageQualityMappingRows, type ImageQualityMappingRow } from '../imageQualityMapping'

const validRow: ImageQualityMappingRow = { model: 'Nano Banana 2', requestedQuality: 'auto', upstreamQuality: '1k' }

describe('image quality mapping credentials', () => {
  it('roundtrips exact model names and separate requested qualities without adding guesses', () => {
    const mapping = { 'Nano Banana 2': { auto: '1k', high: '4k' }, 'Other model': { auto: 'auto' } }
    const rows = readImageQualityMapping(mapping)
    expect(validateImageQualityMappingRows(rows)).toBeNull()
    expect(buildImageQualityMapping(rows)).toEqual(mapping)
    expect(buildImageQualityMapping([{ ...validRow, model: ' Nano Banana 2 ' }])).toEqual({ 'Nano Banana 2': { auto: '1k' } })
    expect(buildImageQualityMapping([])).toEqual({})
  })

  it.each([
    [{ ...validRow, model: '*' }, 'invalidModel'],
    [{ ...validRow, model: 'Nano?Banana' }, 'invalidModel'],
    [{ ...validRow, model: 'Nano\nBanana' }, 'invalidModel'],
    [{ ...validRow, model: '图'.repeat(86) }, 'invalidModel'],
    [{ ...validRow, requestedQuality: '1k' }, 'invalidRequestedQuality'],
    [{ ...validRow, upstreamQuality: '8k' }, 'invalidUpstreamQuality']
  ])('rejects unsupported mapping rows', (row, error) => {
    expect(validateImageQualityMappingRows([row as ImageQualityMappingRow])).toBe(error)
  })

  it('rejects duplicate qualities after trimming and too many models', () => {
    expect(validateImageQualityMappingRows([validRow, { ...validRow, model: ' Nano Banana 2 ' }])).toBe('duplicate')
    expect(validateImageQualityMappingRows(Array.from({ length: 65 }, (_, i) => ({ ...validRow, model: `model-${i}` })))).toBe('tooManyModels')
  })

  it.each([42, [], { model: {} }, { model: { auto: 42 } }])('does not silently discard malformed saved configuration', (mapping) => {
    expect(validateImageQualityMappingRows(readImageQualityMapping(mapping))).not.toBeNull()
  })

  it('handles object-like model names safely', () => {
    const mapping = buildImageQualityMapping([{ ...validRow, model: '__proto__' }])
    expect(Object.hasOwn(mapping, '__proto__')).toBe(true)
    expect(JSON.parse(JSON.stringify(mapping))).toEqual({ ['__proto__']: { auto: '1k' } })
  })
})
