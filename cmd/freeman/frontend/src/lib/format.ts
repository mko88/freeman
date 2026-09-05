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
