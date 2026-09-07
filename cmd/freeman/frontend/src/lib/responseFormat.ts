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
