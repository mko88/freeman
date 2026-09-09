// How a response body is classified and reshaped for display: the
// lightweight kind sniffing behind the "Type" badge, and the JSON/XML
// pretty-printers. Everything here returns plain text — colouring it is
// CodeMirror's job, so an untrusted body is never turned into markup.
import type { httpengine } from '../../wailsjs/go/models'

// Above this the pretty view isn't worth the JSON.parse + regex pass on
// every render — show raw instead. (Anything over the truncation
// threshold never reaches the inline view at all; this is a lower cap
// just for keeping the formatted path snappy.)
export const RESPONSE_PRETTY_MAX = 256 * 1024

// How much of the formatted body GET /api/ui/state carries. The state
// object is read after every action, so it stays a readable answer
// rather than a copy of the response — enough to tell a reindented body
// from an untouched one, and to check the head of a long one.
export const RESPONSE_STATE_MAX = 16 * 1024

export type ResponseKind = 'json' | 'xml' | 'html' | 'image' | 'text'

// How the body panel is showing what came back. 'pretty' is the
// reading of it (reindented, coloured, or an <img>), 'raw' the payload
// as text, 'hex' the bytes.
export type ResponseView = 'pretty' | 'raw' | 'hex'

// CodeMirror has no hex or binary mode — it's a text editor — so the
// dump below is built here and handed to it as plain text.
function bytesFromBase64(b64: string): Uint8Array | null {
  try {
    const binary = atob(b64)
    const out = new Uint8Array(binary.length)
    for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i)
    return out
  } catch {
    return null
  }
}

// Accepts either a bare base64 payload (httpengine.Response.bodyBase64)
// or a full data: URI (the response cache's), which is the same thing
// behind a prefix.
function bytesFrom(b64OrDataUri: string): Uint8Array | null {
  const comma = b64OrDataUri.indexOf(',')
  return bytesFromBase64(comma < 0 ? b64OrDataUri : b64OrDataUri.slice(comma + 1))
}

// `hexdump -C` layout: offset, sixteen bytes in two groups of eight,
// then the printable-ASCII gutter. The app is monospace throughout, so
// the columns line up without any help.
export function hexDump(bytes: Uint8Array): string {
  const lines: string[] = []
  for (let i = 0; i < bytes.length; i += 16) {
    const row = bytes.subarray(i, i + 16)
    const hex = Array.from(row, (b) => b.toString(16).padStart(2, '0'))
    // padEnd keeps the gutter aligned on the final short row.
    const left = hex.slice(0, 8).join(' ').padEnd(23)
    const right = hex.slice(8).join(' ').padEnd(23)
    const ascii = Array.from(row, (b) => (b >= 0x20 && b < 0x7f ? String.fromCharCode(b) : '.')).join('')
    lines.push(`${i.toString(16).padStart(8, '0')}  ${left}  ${right}  |${ascii}|`)
  }
  return lines.length ? lines.join('\n') : '(empty body)'
}

export function responseHeader(r: httpengine.Response | null, name: string): string {
  if (!r?.headers) return ''
  const key = Object.keys(r.headers).find((k) => k.toLowerCase() === name.toLowerCase())
  return key ? (r.headers[key]?.[0] ?? '') : ''
}

// Content-Type first, then a one-character sniff of the body — enough
// to pick a renderer, not a full content classifier.
export function detectResponseKind(r: httpengine.Response | null): ResponseKind {
  const ct = responseHeader(r, 'Content-Type').toLowerCase()
  if (ct.startsWith('image/')) return 'image'
  if (ct.includes('json')) return 'json'
  if (ct.includes('html')) return 'html'
  if (ct.includes('xml')) return 'xml'
  if (ct) return 'text'
  const s = (r?.body ?? '').trimStart()
  if (s.startsWith('{') || s.startsWith('[')) return 'json'
  // A doctype or a root <html> is HTML even with nothing declaring it.
  // Any other tag stays XML — which is what an HTML document that says
  // neither would fall back to anyway, once its parse fails.
  if (/^<!doctype\s+html/i.test(s) || /^<html[\s>]/i.test(s)) return 'html'
  if (s.startsWith('<')) return 'xml'
  return 'text'
}

// Reindents XML by breaking between adjacent tags and tracking depth —
// a lightweight formatter, not a parser (comments/CDATA pass through
// as-is). Only called once the body is known to parse as XML.
export function prettyXml(xml: string): string {
  let depth = 0
  return xml
    .replace(/>\s*</g, '>\n<')
    .trim()
    .split('\n')
    .map((line) => {
      const node = line.trim()
      if (/^<\//.test(node)) depth = Math.max(depth - 1, 0)
      const out = '  '.repeat(depth) + node
      // An opening tag with no matching close on the same line and not
      // self-closing pushes the next line in a level.
      if (/^<[^!?]/.test(node) && !/\/>$/.test(node) && !/<\/[\w:.-]+>$/.test(node)) depth += 1
      return out
    })
    .join('\n')
}

// Elements that never take a closing tag, so — unlike <div> — they must
// not open a level for everything that follows them.
const HTML_VOID = new Set([
  'area', 'base', 'br', 'col', 'embed', 'hr', 'img', 'input',
  'link', 'meta', 'param', 'source', 'track', 'wbr',
])

// Elements whose content is not markup and whose whitespace carries
// meaning: reindenting inside <pre> changes what the page renders, and
// inside <script>/<style> it can change what the code does. Their
// bodies are copied through byte for byte.
const HTML_RAW_TEXT = new Set(['script', 'style', 'pre', 'textarea'])

// Elements a browser closes for you when the next one of the same name
// opens. Without these, the <p>one<p>two that real pages are full of
// would nest one level deeper on every paragraph and walk off the right
// edge of the pane.
const HTML_IMPLICIT_CLOSE = new Set(['p', 'li', 'dt', 'dd', 'option', 'tr', 'td', 'th'])

// Reindents HTML by walking it tag by tag. HTML is not well-formed XML —
// void elements never close, and real documents leave others unclosed
// too — so prettyXml's "every opening tag is a level" rule drifts
// steadily deeper down a page. This keeps a stack of the elements
// actually open, which makes a stray close tag harmless and lets the
// implicit closes above unwind properly. Like prettyXml it reshapes
// rather than parses, and returns plain text.
export function prettyHtml(html: string): string {
  const out: string[] = []
  const open: string[] = []
  const pad = () => '  '.repeat(open.length)
  const emit = (text: string) => {
    if (text) out.push(pad() + text)
  }
  // Text runs collapse onto one line; a whitespace-only run is the
  // document's own indentation and drops out entirely.
  const flat = (text: string) => text.replace(/\s+/g, ' ').trim()
  const escapeName = (name: string) => name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

  let i = 0
  while (i < html.length) {
    const lt = html.indexOf('<', i)
    if (lt < 0) {
      emit(flat(html.slice(i)))
      break
    }
    if (lt > i) emit(flat(html.slice(i, lt)))
    i = lt

    if (html.startsWith('<!--', i)) {
      const end = html.indexOf('-->', i + 4)
      const stop = end < 0 ? html.length : end + 3
      emit(html.slice(i, stop).trim())
      i = stop
      continue
    }
    // <!doctype …> and <?xml …>: neither nests.
    if (html.startsWith('<!', i) || html.startsWith('<?', i)) {
      const end = html.indexOf('>', i)
      const stop = end < 0 ? html.length : end + 1
      emit(html.slice(i, stop).trim())
      i = stop
      continue
    }

    const tag = /^<\/?[a-zA-Z][^>]*>/.exec(html.slice(i))
    if (!tag) {
      // A '<' that begins no tag at all — text, not markup.
      const next = html.indexOf('<', i + 1)
      const stop = next < 0 ? html.length : next
      emit(flat(html.slice(i, stop)))
      i = stop
      continue
    }

    const text = tag[0]
    const name = (/^<\/?\s*([a-zA-Z][\w:.-]*)/.exec(text)?.[1] ?? '').toLowerCase()
    i += text.length

    if (text.startsWith('</')) {
      // Unwind to the matching element if it is genuinely open. A close
      // tag for something that never opened is left where it is rather
      // than pulling the rest of the document out a level.
      const at = open.lastIndexOf(name)
      if (at >= 0) open.length = at
      emit(text)
      continue
    }

    // <p> after an unclosed <p> ends it, rather than nesting inside it.
    if (HTML_IMPLICIT_CLOSE.has(name) && open[open.length - 1] === name) open.pop()

    const selfClosing = /\/>$/.test(text)

    if (HTML_RAW_TEXT.has(name) && !selfClosing) {
      const close = new RegExp(`</${escapeName(name)}\\s*>`, 'i').exec(html.slice(i))
      const body = close ? html.slice(i, i + close.index) : html.slice(i)
      emit(text)
      i += body.length
      // The content goes through untouched, its own indentation
      // included, which is why these lines bypass emit(). The element
      // still opens a level so its close tag lands back where it began.
      open.push(name)
      const kept = body.replace(/^\n/, '').replace(/\s+$/, '')
      if (kept) out.push(...kept.split('\n'))
      continue
    }

    if (HTML_VOID.has(name) || selfClosing) {
      emit(text)
      continue
    }

    // An element holding nothing but text stays on one line, the way
    // prettyXml leaves <a>x</a> alone — three lines for <title>Hi</title>
    // is harder to read than the document was to begin with.
    const inline = new RegExp(`^([^<]*)</${escapeName(name)}\\s*>`).exec(html.slice(i))
    if (inline) {
      emit(text + flat(inline[1]) + `</${name}>`)
      i += inline[0].length
      continue
    }

    emit(text)
    open.push(name)
  }

  return out.join('\n')
}

// Derived once per response/view change: the detected kind, whether a
// pretty view is available (valid JSON/XML, small enough, not
// truncated), and the text to show — formatted for the pretty view,
// exactly as received for the raw one. Highlighting is CodeMirror's job
// now; this only decides what to hand it.
// All three views are always on offer, so pretty is a best effort
// rather than a promise: a body that reindents gets reindented, one
// that only has a grammar gets coloured, and one with neither reads the
// same as raw. That's the cost of a switch that keeps its shape,
// and it's cheaper than a control that comes and goes.
export function formatResponse(
  r: httpengine.Response | null,
  view: ResponseView,
  dataUri: string | null = null,
): { kind: ResponseKind; text: string } {
  const body = r?.body ?? ''
  const kind = detectResponseKind(r)
  const inRange = !!r && !r.truncated && body.length <= RESPONSE_PRETTY_MAX

  // A binary body arrives as bodyBase64 rather than body — see
  // httpengine.Response.MarshalJSON. Prefer it over the cache's data
  // URI: it's the same bytes without a round trip to disk, and it's
  // there in the web build too, which has no response cache.
  const encoded = r?.bodyBase64 || dataUri || ''

  if (view === 'hex') {
    const bytes = encoded ? bytesFrom(encoded) : new TextEncoder().encode(body)
    return { kind, text: bytes ? hexDump(bytes) : 'Reading the cached response…' }
  }

  if (kind === 'image') {
    return { kind, text: dataUri ?? '' }
  }

  // Binary that isn't an image — a PDF, an octet-stream. There's no text
  // to show, so show the payload it did arrive as.
  if (!body && r?.bodyBase64) {
    return { kind, text: r.bodyBase64 }
  }

  if (inRange && kind === 'json') {
    try {
      const parsed = JSON.parse(body)
      return { kind, text: view === 'pretty' ? JSON.stringify(parsed, null, 2) : body }
    } catch {
      // Declared as JSON but isn't — a 500 serving an HTML error page
      // under a JSON content type is the usual way this happens. Report
      // it as text so it isn't coloured against a grammar it doesn't
      // follow.
      return { kind: 'text', text: body }
    }
  }

  if (inRange && kind === 'xml') {
    const doc = new DOMParser().parseFromString(body, 'application/xml')
    if (doc.getElementsByTagName('parsererror').length > 0) return { kind: 'text', text: body }
    return { kind, text: view === 'pretty' ? prettyXml(body) : body }
  }

  // No parse gate here, unlike XML: an HTML parser accepts anything, so
  // there is no such thing as a body that "isn't HTML enough" to show.
  // prettyHtml copes with the malformed ones instead.
  if (inRange && kind === 'html') {
    return { kind, text: view === 'pretty' ? prettyHtml(body) : body }
  }

  return { kind, text: body }
}

// --- request bodies ---------------------------------------------------------

// How the raw-body editor should colour what's being typed. 'auto'
// works it out from the request's own Content-Type header and the first
// character; the others pin it, for the times detection guesses wrong or
// the body is too incomplete to guess from at all.
export type BodyLanguage = 'auto' | 'json' | 'xml' | 'plain'

// The concrete answer 'auto' resolves to. Deliberately the same two
// signals detectResponseKind uses — a declared type first, the first
// non-space character second — so a JSON request and a JSON response
// are recognised the same way.
export function resolveBodyLanguage(
  language: BodyLanguage,
  body: string,
  contentType: string,
): 'json' | 'xml' | 'plain' {
  if (language !== 'auto') return language
  const ct = contentType.toLowerCase()
  if (ct.includes('json')) return 'json'
  if (ct.includes('xml') || ct.includes('html')) return 'xml'
  const s = body.trimStart()
  if (s.startsWith('{') || s.startsWith('[')) return 'json'
  if (s.startsWith('<')) return 'xml'
  return 'plain'
}
