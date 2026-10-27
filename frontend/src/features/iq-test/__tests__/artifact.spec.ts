import { describe, expect, it } from 'vitest'
import { artifactDocument, extractArtifact, metricNumber, tokenSpeed } from '../artifact'

describe('IQ test output safety and metrics', () => {
  it('keeps generated markup in an isolated document with network and navigation disabled', () => {
    const doc = artifactDocument(
      '```html\n<html><head><meta http-equiv="refresh" content="0;url=https://evil.example"></head><body><svg><circle r="20" /></svg><script>fetch("https://evil.example")</script><iframe src="https://evil.example"></iframe><a href="https://evil.example">jump</a></body></html>\n```'
    )
    expect(doc).toContain("default-src 'none'")
    expect(doc).toContain("connect-src 'none'")
    expect(doc).toContain("form-action 'none'")
    expect(doc).not.toContain('http-equiv="refresh"')
    expect(doc).not.toContain('<iframe')
    expect(doc).not.toContain('href="https://evil.example"')
    expect(doc).toContain('<svg')
    expect(doc).not.toContain('<script>')
  })

  it('does not turn prose or incomplete markup into a runnable artifact', () => {
    expect(extractArtifact('Here is what a pelican could look like.')).toBeNull()
    expect(extractArtifact('<html><body>unfinished')).toBeNull()
    expect(extractArtifact('```svg\n<svg xmlns="http://www.w3.org/2000/svg"></svg>\n```')).toContain('<svg')
  })

  it('distinguishes unknown metrics from a measured zero and includes waiting in speed', () => {
    expect(metricNumber(null)).toBe('—')
    expect(metricNumber(0)).toBe('0')
    expect(tokenSpeed(null, 1000)).toBeNull()
    expect(tokenSpeed(20, 2000)).toBe(10)
    expect(tokenSpeed(20, 0)).toBeNull()
  })
})
