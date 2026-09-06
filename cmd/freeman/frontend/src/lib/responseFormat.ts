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

// The bytes behind a data: URI. CodeMirror has no hex or binary mode —
// it's a text editor — so the dump below is built here and handed to it
// as plain text.
function bytesFromDataUri(uri: string): Uint8Array | null {
  const comma = uri.indexOf(',')
  if (comma < 0) return null
  try {
    const binary = atob(uri.slice(comma + 1))
    const out = new Uint8Array(binary.length)
    for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i)
    return out
  } catch {
    return null
  }
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
export function formatResponse(
  r: httpengine.Response | null,
  view: ResponseView,
  dataUri: string | null = null,
): { kind: ResponseKind; hasPretty: boolean; text: string } {
  const body = r?.body ?? ''
  const kind = detectResponseKind(r)
  const inRange = !!r && !r.truncated && body.length <= RESPONSE_PRETTY_MAX
  // Which kinds have a reading distinct from their payload. An image's
  // is the picture; JSON's and XML's is the reindented text, once
  // they're small enough and actually parse.
  const hasPretty =
    kind === 'image' ||
    (inRange && kind === 'json' && parses(body, 'json')) ||
    (inRange && kind === 'xml' && parses(body, 'xml'))

  // Both the hex view and an image's payload need the bytes as they
  // arrived, and response.body can't carry them: Wails marshals Body as
  // JSON, and JSON replaces every byte that isn't valid UTF-8 with
  // U+FFFD, so response.body for a PNG is a wall of replacement
  // characters. The data URI is read back from the response cache file,
  // so it's byte-faithful.
  if (view === 'hex') {
    const bytes = dataUri ? bytesFromDataUri(dataUri) : null
    return { kind, hasPretty, text: bytes ? hexDump(bytes) : 'Reading the cached response…' }
  }

  if (kind === 'image') {
    return { kind, hasPretty, text: dataUri ?? '' }
  }

  if (inRange && kind === 'json') {
    try {
      const parsed = JSON.parse(body)
      return { kind, hasPretty, text: view === 'pretty' ? JSON.stringify(parsed, null, 2) : body }
    } catch {
      // Declared as JSON but isn't — show it as it came rather than
      // claiming a pretty view that would fail.
      return { kind: 'text', hasPretty: false, text: body }
    }
  }

  if (inRange && kind === 'xml') {
    if (!parses(body, 'xml')) return { kind: 'text', hasPretty: false, text: body }
    return { kind, hasPretty, text: view === 'pretty' ? prettyXml(body) : body }
  }

  return { kind, hasPretty, text: body }
}

function parses(body: string, as: 'json' | 'xml'): boolean {
  if (as === 'json') {
    try {
      JSON.parse(body)
      return true
    } catch {
      return false
    }
  }
  const doc = new DOMParser().parseFromString(body, 'application/xml')
  return doc.getElementsByTagName('parsererror').length === 0
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
