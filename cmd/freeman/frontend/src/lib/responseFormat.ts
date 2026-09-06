// How a response body is classified and rendered: the lightweight
// kind sniffing behind the "Type" badge, the JSON/XML pretty-printers,
// and the token highlighting the response pane drops in with {@html}.
//
// Safe for an untrusted body by construction: every highlighter escapes
// &, < and > *first* and only then inserts its own <span class="syntax-*">
// wrappers, so nothing from the response can close a tag or open one.
import type { httpengine } from '../../wailsjs/go/models'

// Above this the pretty view isn't worth the JSON.parse + regex pass on
// every render — show raw instead. (Anything over the truncation
// threshold never reaches the inline view at all; this is a lower cap
// just for keeping the formatted path snappy.)
export const RESPONSE_PRETTY_MAX = 256 * 1024

export type ResponseKind = 'json' | 'xml' | 'html' | 'image' | 'text'

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
  view: 'pretty' | 'raw',
): { kind: ResponseKind; canPretty: boolean; text: string } {
  const body = r?.body ?? ''
  const kind = detectResponseKind(r)
  const inRange = !!r && !r.truncated && body.length <= RESPONSE_PRETTY_MAX

  if (inRange && kind === 'json') {
    try {
      const parsed = JSON.parse(body)
      return { kind, canPretty: true, text: view === 'pretty' ? JSON.stringify(parsed, null, 2) : body }
    } catch {
      // Declared as JSON but isn't — show it as it came rather than
      // claiming a pretty view that would fail.
      return { kind: 'text', canPretty: false, text: body }
    }
  }

  if (inRange && kind === 'xml') {
    const doc = new DOMParser().parseFromString(body, 'application/xml')
    if (doc.getElementsByTagName('parsererror').length > 0) {
      return { kind: 'text', canPretty: false, text: body }
    }
    return { kind, canPretty: true, text: view === 'pretty' ? prettyXml(body) : body }
  }

  return { kind, canPretty: false, text: body }
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
