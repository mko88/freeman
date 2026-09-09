import { GetTheme } from '$backend'

// Must match internal/theme.Dark's keys and theme.css's --fm-<key> custom
// properties.
const TOKEN_NAMES = [
  'bg',
  'bgPanel',
  'bgElevated',
  'bgHover',
  'bgResponse',
  'border',
  'borderSubtle',
  'text',
  'textMuted',
  'accent',
  'error',
  'success',
  'warning',
  'methodGet',
  'methodPut',
  'methodNeutral',
]

// Applies any tokens the Go backend resolved (see internal/theme —
// theme.yaml's active palette, falling back to Dark) that differ from
// theme.css's built-in dark values, as inline overrides on <html>. Not
// awaited by main.ts: theme.css's :root already renders the correct dark
// theme with zero delay, so there's nothing to block startup on — this
// only has visible work to do when theme.yaml selects a custom palette.
export async function applyTheme(): Promise<void> {
  let palette: Record<string, string>
  try {
    palette = await GetTheme()
  } catch {
    return
  }
  const root = document.documentElement
  for (const name of TOKEN_NAMES) {
    const value = palette[name]
    // theme.css's custom properties are kebab-case (--fm-bg-panel); Go's
    // Colors map and this file's TOKEN_NAMES are camelCase (bgPanel) to
    // match Go/JS naming conventions — convert here rather than making
    // either side use the other's case convention.
    if (value) root.style.setProperty(`--fm-${name.replace(/[A-Z]/g, (c) => `-${c.toLowerCase()}`)}`, value)
  }
}

// The interface scale's bounds and step, matching internal/settings.Clamp
// — Go is the authority, these keep the controls from offering a value it
// would refuse. Here rather than in either caller because both the
// settings window's buttons and App.svelte's Ctrl +/- reach for them.
export const FONT_SCALE = { min: 70, max: 200, step: 5, default: 100 } as const

// The fallback stacks the settings' font names are prepended to, kept
// identical to style.css's :root — a named font that isn't installed
// then degrades to the bundled face rather than to whatever the platform
// picks for a missing family.
const UI_STACK = `"IBM Plex Sans", -apple-system, "Segoe UI", Roboto, sans-serif`
const MONO_STACK = `"IBM Plex Mono", "Cascadia Code", Consolas, "SF Mono", "Liberation Mono", monospace`

/**
 * Applies the workspace's typography to <html>: the two font stacks, and
 * the scale.
 *
 * The scale is one custom property rather than a pass over every rule
 * because every font size in this app is expressed in rem — style.css's
 * `html { font-size: calc(100% * var(--fm-font-scale)) }` is the whole
 * mechanism. Spacing is in rem too, so the interface scales as a piece
 * instead of growing text inside boxes that stayed the same size.
 */
export function applyTypography(s: { fontUi?: string; fontMono?: string; fontScalePercent?: number }): void {
  const root = document.documentElement
  const ui = (s.fontUi ?? '').trim()
  const mono = (s.fontMono ?? '').trim()
  root.style.setProperty('--fm-font-ui', ui ? `"${ui}", ${UI_STACK}` : UI_STACK)
  root.style.setProperty('--fm-font-mono', mono ? `"${mono}", ${MONO_STACK}` : MONO_STACK)
  // Go clamps this on save; the fallback is for the moment before the
  // settings have loaded, and for a value that never went through Go.
  const scale = s.fontScalePercent && s.fontScalePercent > 0 ? s.fontScalePercent : 100
  root.style.setProperty('--fm-font-scale', String(scale / 100))
}
