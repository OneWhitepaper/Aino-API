/** Illustrative protocol examples only. No model discovery or API calls run on the homepage. */
export const EXAMPLE_MODELS = [
  { id: 'deepseek-v4-pro', name: 'DeepSeek V4 Pro', provider: 'DeepSeek', short: 'DeepSeek', version: 'V4 Pro', icon: 'deepseek.svg', kind: 'chat', protocol: 'Chat Completions', path: '/v1/chat/completions', search: 'deepseek chat completions 聊天 深度求索' },
  { id: 'claude-sonnet-4-6', name: 'Claude Sonnet 4.6', provider: 'Anthropic', short: 'Claude', version: 'Sonnet 4.6', icon: 'claude.svg', kind: 'messages', protocol: 'Messages', path: '/v1/messages', search: 'claude anthropic messages 消息' },
  { id: 'gemini-2.5-pro', name: 'Gemini 2.5 Pro', provider: 'Google', short: 'Gemini', version: '2.5 Pro', icon: 'gemini.svg', kind: 'native', protocol: 'Gemini', path: '/v1beta/models/gemini-2.5-pro:generateContent', search: 'google generatecontent gemini native 原生 谷歌' },
] as const

export type ModelId = typeof EXAMPLE_MODELS[number]['id']
export type CodeLanguage = 'curl' | 'python'
export type ProtocolFilter = 'all' | typeof EXAMPLE_MODELS[number]['kind']
export type SimulationState = 'idle' | 'loading' | 'done'
export const DEFAULT_MODEL: ModelId = 'deepseek-v4-pro'
export const MODEL_MEDIA = '/omniflow-home/'

export function modelFor(id: string) {
  return EXAMPLE_MODELS.find(model => model.id === id) || EXAMPLE_MODELS[0]
}

export function protocolName(id: string, chinese = true): string {
  const model = modelFor(id)
  return model.kind === 'native' ? chinese ? 'Gemini 原生' : 'Gemini native' : model.protocol
}

function requestPayload(id: ModelId, prompt: string) {
  if (id === 'gemini-2.5-pro') return { contents: [{ role: 'user', parts: [{ text: prompt }] }] }
  return { model: id, ...(id === 'claude-sonnet-4-6' ? { max_tokens: 256 } : {}), messages: [{ role: 'user', content: prompt }] }
}

/** All six examples preserve the provider's native path, body, and authentication header. */
export function requestSnippet(id: ModelId, language: CodeLanguage, baseUrl: string, prompt: string): string {
  const url = baseUrl.replace(/\/$/, '') + modelFor(id).path
  const payload = requestPayload(id, prompt)
  const pack = (value: object) => Object.entries(value).map(([key, item]) => `  ${JSON.stringify(key)}: ${JSON.stringify(item)}`).join(',\n')
  const anthropic = id === 'claude-sonnet-4-6'
  const gemini = id === 'gemini-2.5-pro'
  if (language === 'python') {
    const headers = anthropic ? '"x-api-key": key, "anthropic-version": "2023-06-01"'
      : gemini ? '"x-goog-api-key": key' : '"Authorization": f"Bearer {key}"'
    return `import json, os\nfrom urllib.request import Request, urlopen\n\nkey = os.environ["OMNIFLOW_API_KEY"]\nurl = "${url}"\npayload = {\n${pack(payload)}\n}\nreq = Request(url, data=json.dumps(payload).encode(),\n              headers={"Content-Type": "application/json",\n                       ${headers}})\nwith urlopen(req, timeout=60) as res:\n    print(json.load(res))`
  }
  const headers = anthropic ? ['-H "x-api-key: $OMNIFLOW_API_KEY"', "-H 'anthropic-version: 2023-06-01'"]
    : gemini ? ['-H "x-goog-api-key: $OMNIFLOW_API_KEY"'] : ['-H "Authorization: Bearer $OMNIFLOW_API_KEY"']
  return [`curl -X POST '${url}'`, ...headers.map(header => `  ${header}`), "  -H 'Content-Type: application/json'", "  --data-binary @- <<'OMNIFLOW_JSON'"].join(' \\\n') + `\n{\n${pack(payload)}\n}\nOMNIFLOW_JSON`
}

export function simulationAnswer(prompt: string, brand: string, chinese: boolean): string {
  const text = prompt.trim()
  if (/模型网关|模型中转/.test(text) || (text.includes(brand) && /介绍|explain|introduce/i.test(text))) {
    return chinese ? `${brand} 是连接你的应用与多种模型的 API 网关。` : `${brand} connects your app to different models through their native APIs.`
  }
  if (/^(你好|您好|hello|hi)[。.!！,，\s]*$/i.test(text)) return chinese ? '你好！这条问候已经经过本地模拟的请求和响应流程。' : 'Hello! This greeting has completed the local request and response simulation.'
  if (/密钥|api\s*key/i.test(text)) return chinese ? '正式接入时，把密钥保存在服务端的环境变量中。这里的示例只演示请求格式。' : 'Keep your key in a server environment variable when integrating. This example only demonstrates the request format.'
  const excerpt = Array.from(text.replace(/\s+/g, ' '))
  const shown = excerpt.slice(0, 65).join('') + (excerpt.length > 65 ? '…' : '')
  return chinese ? `已收到「${shown}」。这是本地模拟响应；真实回答将在接入账号 API 后由所选模型返回。` : `Received “${shown}”. This is a local simulation. Your selected model provides the real answer after you connect your account API.`
}

export function simulationPayload(id: ModelId, answer: string) {
  if (id === 'claude-sonnet-4-6') return { type: 'message', model: id, role: 'assistant', content: [{ type: 'text', text: answer }], stop_reason: 'end_turn' }
  if (id === 'gemini-2.5-pro') return { candidates: [{ content: { role: 'model', parts: [{ text: answer }] }, finishReason: 'STOP' }] }
  return { object: 'chat.completion', model: id, choices: [{ index: 0, message: { role: 'assistant', content: answer }, finish_reason: 'stop' }] }
}

export function codeLines(code: string) {
  return code.split('\n').map((line, index) => {
    const parts: { text: string; kind: string }[] = []
    const pattern = /("(?:\\.|[^"\\])*"|'(?:[^']*)'|(?<![\w-])--?[\w-]+|\b(?:curl|from|import|with|as|print|json|urlopen|Request|POST)\b|\b\d+\b)/g
    let offset = 0
    for (const match of line.matchAll(pattern)) {
      const at = match.index ?? 0
      if (at > offset) parts.push({ text: line.slice(offset, at), kind: '' })
      const token = match[0]
      parts.push({ text: token, kind: token[0] === '"' || token[0] === "'" ? 'code-string' : token[0] === '-' ? 'code-flag' : /^\d/.test(token) ? 'code-literal' : 'code-keyword' })
      offset = at + token.length
    }
    parts.push({ text: line.slice(offset) || (line.length ? '' : ' '), kind: '' })
    return { number: index + 1, parts }
  })
}

/** Clipboard fallback is scoped to the active native dialog so its focus trap stays intact. */
export async function copyText(value: string, container: HTMLElement = document.body): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) { await navigator.clipboard.writeText(value); return true }
  } catch { /* Restricted contexts can use the selection fallback. */ }
  const previous = document.activeElement
  const selection = window.getSelection()
  const ranges = selection ? Array.from({ length: selection.rangeCount }, (_, index) => selection.getRangeAt(index).cloneRange()) : []
  const helper = document.createElement('textarea')
  helper.className = 'of-copy-helper'
  helper.style.cssText = 'position:fixed;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip-path:inset(50%);white-space:nowrap;border:0'
  helper.value = value
  helper.readOnly = true
  helper.tabIndex = -1
  container.append(helper)
  try {
    helper.focus({ preventScroll: true }); helper.select()
    return typeof document.execCommand === 'function' && document.execCommand('copy')
  } catch { return false }
  finally {
    helper.remove()
    if (previous instanceof HTMLElement) previous.focus({ preventScroll: true })
    if (selection && ranges.length) { selection.removeAllRanges(); ranges.forEach(range => selection.addRange(range)) }
  }
}

export function selectCode(element: HTMLElement | null) {
  if (!element) return
  const range = document.createRange()
  range.selectNodeContents(element)
  const selection = window.getSelection()
  element.focus({ preventScroll: true })
  selection?.removeAllRanges()
  selection?.addRange(range)
}
