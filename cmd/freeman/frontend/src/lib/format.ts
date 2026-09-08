// Presentation helpers shared by App.svelte and its components.

// Each HTTP method gets one consistent color everywhere it appears
// (sidebar row accent, method tag, the method select) — a real
// structural device: the method is the single most important fact
// about a saved request, so it's the one thing this UI color-codes.
// POST/PATCH/DELETE deliberately reuse the accent/success/error tokens
// rather than getting their own hues, so the method system and the
// status/action system read as one palette, not two.
export const methodColorVar: Record<string, string> = {
  GET: 'var(--fm-method-get)',
  POST: 'var(--fm-accent)',
  PUT: 'var(--fm-method-put)',
  PATCH: 'var(--fm-success)',
  DELETE: 'var(--fm-error)',
}
export function methodColor(method: string): string {
  return methodColorVar[method] ?? 'var(--fm-method-neutral)'
}

// Four characters, so every badge is the same width and the names beside
// them line up in a column instead of starting wherever the method
// happened to end. The three that don't fit get the abbreviation people
// already use for them; anything else — a custom method — is simply cut,
// which is enough to tell two rows apart. The full method stays in the
// row's tooltip.
const methodShort: Record<string, string> = {
  DELETE: 'DEL',
  OPTIONS: 'OPTS',
  PATCH: 'PTCH',
  CONNECT: 'CONN',
  TRACE: 'TRCE',
}
export function methodLabel(method: string | undefined): string {
  const m = (method || 'GET').toUpperCase()
  return methodShort[m] ?? m.slice(0, 4)
}

// Duration in the largest unit that still reads as a number rather than
// a lot of zeroes. Sub-millisecond keeps a decimal instead of rounding
// to "0 ms", which would say a request took no time at all; past a
// minute it splits, because "83.4 s" is arithmetic the reader shouldn't
// have to do. Exact milliseconds stay available as a tooltip — see
// durationMillis.
export function formatDuration(ns: number): string {
  const ms = ns / 1e6
  if (ms < 1) return `${ms.toFixed(2)} ms`
  if (ms < 1000) return `${Math.round(ms)} ms`
  if (ms < 60_000) return `${(ms / 1000).toFixed(2)} s`
  const totalSeconds = ms / 1000
  const minutes = Math.floor(totalSeconds / 60)
  return `${minutes} m ${Math.round(totalSeconds - minutes * 60)} s`
}

// The unrounded figure behind formatDuration, for the tooltip.
export function durationMillis(ns: number): string {
  const ms = ns / 1e6
  return `${ms < 1 ? ms.toFixed(3) : Math.round(ms).toLocaleString()} ms`
}

// Binary multiples, since this counts bytes off a wire and out of a
// buffer. One decimal past KB: three significant figures is as much as
// anyone reads off a size.
const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB'] as const

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`
  let value = n
  let unit = 0
  while (value >= 1024 && unit < BYTE_UNITS.length - 1) {
    value /= 1024
    unit += 1
  }
  return `${value.toFixed(1)} ${BYTE_UNITS[unit]}`
}

// The exact count behind formatBytes, for the tooltip. Grouped, because
// a bare seven-digit run is unreadable.
export function exactBytes(n: number): string {
  return `${n.toLocaleString()} bytes`
}

// Go's http.Response.Status (httpengine.Response.status) is already
// "<code> <reason>", e.g. "200 OK" — this strips the leading code so
// it isn't shown twice next to statusCode ("200 200 OK").
export function reasonPhrase(status: string): string {
  return status.replace(/^\d+\s*/, '')
}

// Standard HTTP status-class semantics, for coloring the status badge.
export function statusTone(code: number): 'success' | 'info' | 'warning' | 'error' {
  if (code >= 200 && code < 300) return 'success'
  if (code >= 300 && code < 400) return 'info'
  if (code >= 400 && code < 500) return 'warning'
  return 'error'
}
