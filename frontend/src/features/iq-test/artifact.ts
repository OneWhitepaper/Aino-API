import DOMPurify from 'dompurify'

export function extractArtifact(output: string): string | null {
  const fenced = output.match(/```(?:html|svg)?\s*\n([\s\S]*?)```/i)?.[1] ?? output
  return (
    fenced.match(/(?:<!doctype\s+html[^>]*>\s*)?<html\b[^>]*>[\s\S]*?<\/html\s*>/i)?.[0] ??
    fenced.match(/<svg\b[^>]*>[\s\S]*?<\/svg\s*>/i)?.[0] ??
    null
  )
}

// Static, opaque-origin preview. Scripts are intentionally disabled, including
// for downloaded previews, so generated content cannot navigate to leak data.
export function artifactDocument(output: string): string {
  const code = extractArtifact(output)
  if (!code) return ''
  const clean = DOMPurify.sanitize(code, {
    WHOLE_DOCUMENT: true,
    USE_PROFILES: { html: true, svg: true, svgFilters: true },
    FORBID_TAGS: [
      'script',
      'iframe',
      'frame',
      'frameset',
      'meta',
      'link',
      'base',
      'object',
      'embed',
      'form',
      'input',
      'button',
      'textarea',
      'select',
      'audio',
      'video',
      'source',
      'track',
      'foreignObject'
    ],
    FORBID_ATTR: ['srcset', 'ping', 'target', 'download', 'autofocus']
  })
  const doc = new DOMParser().parseFromString(clean, 'text/html')
  for (const element of doc.querySelectorAll('*')) {
    for (const name of ['href', 'xlink:href', 'src', 'action', 'formaction', 'poster', 'data']) {
      const value = element.getAttribute(name)
      if (
        value &&
        !/^#[\w.-]+$/.test(value) &&
        !(name === 'src' && /^data:image\/(?:png|jpeg|webp|gif);base64,[a-z0-9+/=\s]+$/i.test(value))
      )
        element.removeAttribute(name)
    }
  }
  const csp =
    "default-src 'none'; script-src 'none'; connect-src 'none'; img-src data:; style-src 'unsafe-inline'; font-src 'none'; frame-src 'none'; media-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'"
  return `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="${csp}"><meta name="referrer" content="no-referrer"><style>html,body{margin:0;min-height:100%;background:white}body{overflow-wrap:anywhere}svg{max-width:100%;height:auto}</style>${doc.head.innerHTML}</head><body>${doc.body.innerHTML}</body></html>`
}

export function metricNumber(value: number | null | undefined): string {
  return value == null ? '—' : new Intl.NumberFormat().format(value)
}
export function tokenSpeed(tokens: number | null | undefined, durationMS: number): number | null {
  return tokens == null || durationMS <= 0 ? null : tokens / (durationMS / 1000)
}

export function resultLabel(task: import('@/api/iqTest').IQTest): string {
  return task.status === 'completed' ? task.verdict || 'ungraded' : task.status
}
