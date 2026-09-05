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

export function formatDuration(ns: number): string {
  return `${Math.round(ns / 1e6)} ms`
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} bytes`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
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
