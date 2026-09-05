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

// Wraps JSON tokens in <span class="syntax-*"> for {@html}. The whole
// string is HTML-escaped first and the replacement only ever inserts
// those known spans, so the result is safe to render as HTML even
// though the body itself is untrusted.
export function highlightJson(json: string): string {
  const escaped = json.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  return escaped.replace(
    /("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false)\b|\bnull\b|-?\d+(?:\.\d*)?(?:[eE][+-]?\d+)?)/g,
    (match) => {
      let cls = 'syntax-num'
      if (/^"/.test(match)) cls = /:$/.test(match) ? 'syntax-key' : 'syntax-str'
      else if (match === 'true' || match === 'false') cls = 'syntax-bool'
      else if (match === 'null') cls = 'syntax-null'
      return `<span class="${cls}">${match}</span>`
    },
  )
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

// Same idea as highlightJson, for XML: escape first, then wrap tag
// delimiters, names, attribute names and attribute values in the same
// syntax-* spans. XML declarations and comments are left plain.
export function highlightXml(xml: string): string {
  const escaped = xml.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  return escaped.replace(
    /(&lt;\/?)([\w:.-]+)((?:\s+[\w:.-]+(?:=(?:"[^"]*"|'[^']*'))?)*\s*)(\/?&gt;)/g,
    (_full, open, name, attrs, close) => {
      const attrsHtml = attrs.replace(
        /([\w:.-]+)(=)("[^"]*"|'[^']*')/g,
        '<span class="syntax-num">$1</span>$2<span class="syntax-str">$3</span>',
      )
      return `<span class="syntax-null">${open}</span><span class="syntax-key">${name}</span>${attrsHtml}<span class="syntax-null">${close}</span>`
    },
  )
}

// Derived once per response/view change: the detected kind, whether a
// pretty view is available (valid JSON/XML, small enough, not
// truncated), and the highlighted HTML when it's the pretty view's
// turn to render.
export function formatResponse(
  r: httpengine.Response | null,
  view: 'pretty' | 'raw',
): { kind: ResponseKind; canPretty: boolean; html: string } {
  const kind = detectResponseKind(r)
  const inRange = !!r && !r.truncated && (r.body?.length ?? 0) <= RESPONSE_PRETTY_MAX
  if (inRange && kind === 'json') {
    try {
      const parsed = JSON.parse(r!.body)
      return { kind, canPretty: true, html: view === 'pretty' ? highlightJson(JSON.stringify(parsed, null, 2)) : '' }
    } catch {
      return { kind: 'text' as ResponseKind, canPretty: false, html: '' }
    }
  }
  if (inRange && kind === 'xml') {
    const doc = new DOMParser().parseFromString(r!.body, 'application/xml')
    if (doc.getElementsByTagName('parsererror').length > 0) {
      return { kind: 'text' as ResponseKind, canPretty: false, html: '' }
    }
    return { kind, canPretty: true, html: view === 'pretty' ? highlightXml(prettyXml(r!.body)) : '' }
  }
  return { kind, canPretty: false, html: '' }
}
