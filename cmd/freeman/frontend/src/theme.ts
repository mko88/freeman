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
