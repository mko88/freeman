// Syntax highlighting for the Code tab's generated commands.
//
// Hand-rolled rather than pulled from a library, for two reasons. The
// input isn't arbitrary — internal/codegen produces it, in a small fixed
// grammar — so there's nothing to discover by re-parsing it with a
// general grammar. And the one construct that dominates the output is
// the one a general grammar gets wrong: highlight.js's bash mode doesn't
// model heredocs, so it highlights a `<<'BODY'` body as bash code and
// paints every JSON key in it as a shell string. An editor shows that
// body as one literal, which is what happens here.
//
// Same safety property as lib/responseFormat.ts's highlighters: the text
// is HTML-escaped first, and only known <span class="syntax-*"> wrappers
// are inserted afterwards, so a URL or body containing markup can't
// break out.
import type { CodeFormat } from './requestDraft'

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

function span(cls: string, text: string): string {
  return `<span class="syntax-${cls}">${text}</span>`
}

// --- sh ---------------------------------------------------------------------

// Ordered alternation: the string and comment branches come first so a
// '#' inside a quoted value can't start a comment, and a flag-looking
// substring inside a header value stays part of the string.
const SH_TOKEN = new RegExp(
  [
    String.raw`(?<sq>'[^']*')`,
    String.raw`(?<dq>"(?:[^"\\]|\\.)*")`,
    String.raw`(?<comment>#.*)`,
    String.raw`(?<vari>\$\{[^}]*\}|\$[A-Za-z_]\w*)`,
    String.raw`(?<assign>^[ \t]*[A-Za-z_]\w*(?==))`,
    String.raw`(?<flag>(?<=^|[ \t])--?[A-Za-z][\w-]*)`,
    String.raw`(?<cmd>\b(?:curl|cat|set)\b)`,
  ].join('|'),
  'gm',
)

const SH_VAR = /\$\{[^}]*\}|\$[A-Za-z_]\w*/g

function highlightShellLine(line: string): string {
  return line.replace(SH_TOKEN, (match, ...rest) => {
    const g = rest[rest.length - 1] as Record<string, string | undefined>
    if (g.sq) return span('str', match)
    // A double-quoted string still expands variables, and editors colour
    // them through the quotes — so does this.
    if (g.dq) return span('str', match.replace(SH_VAR, (v) => span('var', v)))
    if (g.comment) return span('comment', match)
    if (g.vari) return span('var', match)
    if (g.assign) return span('key', match)
    if (g.flag) return span('flag', match)
    if (g.cmd) return span('cmd', match)
    return match
  })
}

// A quoted heredoc opener, e.g. `body=$(cat <<'BODY'`. Only the quoted
// form is matched because that's the only form codegen emits — and the
// unquoted form would expand variables, which would need highlighting
// inside the body.
const SH_HEREDOC_OPEN = /<<'([A-Za-z_]\w*)'/

export function highlightShell(code: string): string {
  let delimiter: string | null = null
  // Structure is decided on the raw line and escaping happens per line:
  // escaping first would turn `<<` into `&lt;&lt;` and the opener would
  // never match — which is exactly how a heredoc body ends up
  // highlighted as code instead of as a literal.
  return code
    .split('\n')
    .map((raw) => {
      const line = escapeHtml(raw)
      if (delimiter !== null) {
        // Body and terminator both render as one literal, the way an
        // editor shows a quoted heredoc.
        if (raw.trimEnd() === delimiter) {
          delimiter = null
        }
        return span('str', line)
      }
      const opener = SH_HEREDOC_OPEN.exec(raw)
      if (opener) {
        delimiter = opener[1]
      }
      return highlightShellLine(line)
    })
    .join('\n')
}

// --- PowerShell -------------------------------------------------------------

const PS_TOKEN = new RegExp(
  [
    // '' is PowerShell's escape for a literal quote inside a string.
    String.raw`(?<str>'(?:[^']|'')*')`,
    String.raw`(?<comment>#.*)`,
    String.raw`(?<vari>\$[A-Za-z_]\w*)`,
    // Verb-Noun, which covers Invoke-RestMethod and Get-Item without
    // naming them — codegen may grow more.
    String.raw`(?<cmd>\b[A-Z][a-z]+-[A-Z]\w+\b)`,
    String.raw`(?<param>(?<=^|[ \t\x60])-[A-Za-z]\w*)`,
  ].join('|'),
  'gm',
)

function highlightPowerShellLine(line: string): string {
  return line.replace(PS_TOKEN, (match, ...rest) => {
    const g = rest[rest.length - 1] as Record<string, string | undefined>
    if (g.str) return span('str', match)
    if (g.comment) return span('comment', match)
    if (g.vari) return span('var', match)
    if (g.cmd) return span('cmd', match)
    if (g.param) return span('flag', match)
    return match
  })
}

export function highlightPowerShell(code: string): string {
  let inHereString = false
  // Same per-line escaping as highlightShell, for the same reason.
  return code
    .split('\n')
    .map((raw) => {
      const line = escapeHtml(raw)
      if (inHereString) {
        // A here-string ends at a line *beginning* with '@ — the same
        // rule codegen checks the body against before choosing one.
        if (raw.startsWith("'@")) {
          inHereString = false
        }
        return span('str', line)
      }
      if (/@'\s*$/.test(raw)) {
        // The opener's own tokens still highlight; only what follows is
        // literal.
        inHereString = true
      }
      return highlightPowerShellLine(line)
    })
    .join('\n')
}

// --- entry point ------------------------------------------------------------

// Which language a generated snippet is, by the format that produced it —
// no sniffing needed, the Code tab already knows.
export function highlightGeneratedCode(code: string, format: CodeFormat): string {
  if (!code) return ''
  return format === 'powershell' || format === 'powershell-script'
    ? highlightPowerShell(code)
    : highlightShell(code)
}
