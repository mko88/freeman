// The shape of the request being edited, and the fixed option lists the
// editor offers for it. Shared by App.svelte — which owns the draft, the
// control-API dispatcher that mutates it, and the state mirror that
// reports it — and RequestEditor.svelte, which renders it.
import type { domain } from '../../wailsjs/go/models'

// Mirrors domain.BodyMode's / domain.FormFieldType's string constants —
// Wails' binding generator doesn't emit a type for a named string type,
// only struct classes, so these are redefined here (domain.Body.mode
// and domain.FormField.type are themselves typed as plain strings in
// models.ts).
export type BodyMode = 'none' | 'raw' | 'form-data' | 'x-www-form-urlencoded' | 'binary'
export type FormFieldType = 'text' | 'file'
export type RequestTab = 'params' | 'headers' | 'auth' | 'body' | 'options' | 'code'

// How the request is sent, as opposed to what is sent. Mirrors
// domain.Options; the defaults here are the ones httpengine applies to a
// request that has none.
export type RequestOptions = {
  followRedirects: boolean
  maxRedirects: number
  storeCookies: boolean
  timeoutMs: number
  skipTlsVerify: boolean
  caCertFile: string
  useCustomCA: boolean
  clientCertFile: string
  clientCertKeyFile: string
}

// What a new request starts with — every option, taken from the
// workspace's settings (see internal/settings) rather than fixed here,
// so Settings → Requests is what decides. App.svelte passes them in;
// these fallbacks are only what stands before the settings have loaded,
// and match internal/settings.Defaults.
export const defaultOptions = (workspace?: WorkspaceDefaults): RequestOptions => ({
  followRedirects: workspace?.followRedirects ?? true,
  maxRedirects: workspace?.maxRedirects ?? 10,
  storeCookies: workspace?.storeCookies ?? true,
  timeoutMs: workspace?.requestTimeoutMs ?? 30_000,
  skipTlsVerify: workspace?.skipTlsVerify ?? false,
  caCertFile: workspace?.caCertFile ?? '',
  useCustomCA: workspace?.useCustomCA ?? false,
  clientCertFile: workspace?.clientCertFile ?? '',
  clientCertKeyFile: workspace?.clientCertKeyFile ?? '',
})

// The part of internal/settings.Settings a request can override — which
// is now every option on the Options tab. The rest of Settings is about
// responses, which a request has no say in.
export type WorkspaceDefaults = {
  maxRedirects: number
  requestTimeoutMs: number
  followRedirects: boolean
  storeCookies: boolean
  skipTlsVerify: boolean
  caCertFile: string
  useCustomCA: boolean
  clientCertFile: string
  clientCertKeyFile: string
}

// Which settings differ from the defaults — the Options tab's badge, so
// a tab nobody has touched says so rather than looking the same as one
// that has, and what decides whether a saved request carries an options
// block at all.
//
// The defaults are required, unlike defaultOptions' own parameter: a
// caller that omits them gets a count measured against the built-in
// fallbacks instead of the workspace's own, which is wrong rather than
// merely approximate — a request matching a workspace default of 5
// redirects would read as "1 changed". That is exactly what the badge
// did until the editor was given these.
export function changedOptionCount(o: RequestOptions, workspace: WorkspaceDefaults): number {
  const d = defaultOptions(workspace)
  return (Object.keys(d) as (keyof RequestOptions)[]).filter((k) => o[k] !== d[k]).length
}
export type AuthType = 'none' | 'bearer' | 'basic' | 'apikey' | 'oauth2'
export type CodeFormat = 'bash' | 'powershell' | 'python' | 'javascript'
// Labelled by the language, lowercase throughout — these read as a row
// of formats, not as four differently-capitalised product names.
export const codeFormats: { value: CodeFormat; label: string }[] = [
  { value: 'bash', label: 'bash' },
  { value: 'powershell', label: 'powershell' },
  { value: 'python', label: 'python' },
  { value: 'javascript', label: 'javascript' },
]

export const bodyModes: { value: BodyMode; label: string }[] = [
  { value: 'none', label: 'none' },
  { value: 'raw', label: 'raw' },
  { value: 'form-data', label: 'form-data' },
  { value: 'x-www-form-urlencoded', label: 'x-www-form-urlencoded' },
  { value: 'binary', label: 'binary' },
]

// The methods the editor's dropdown offers.
export const methods = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS']

// The request currently in the editor, before it's saved. One object
// rather than a field per variable for two reasons: RequestEditor
// takes it as a single bound prop instead of eleven, and reportUIState
// can spread it — its keys are deliberately the same names
// GET /api/ui/state reports and setRequestField accepts, so the mirror
// is `...draft` and can't drift from the thing it mirrors.
//
// params: appended to the URL at execution time (see
// internal/httpengine.buildURL) — the Params tab edits them as a list
// rather than syncing them into the URL string field.
//
// auth: type 'none' means "leave the Authorization header alone"; the
// other types are turned into a header at execute time by
// internal/httpengine.applyAuth (with {{var}} substitution), and a
// configured auth overrides a hand-written Authorization row. Only the
// fields the current type uses are read — the rest are kept so
// switching type and back doesn't lose what was typed.
export type RequestAuth = {
  type: AuthType
  token: string
  username: string
  password: string
  key: string
  value: string
  // OAuth2 client credentials — the grant that needs no browser.
  tokenUrl: string
  clientId: string
  clientSecret: string
  scope: string
}
export type RequestDraft = {
  name: string
  method: string
  url: string
  params: domain.QueryParam[]
  headers: domain.Header[]
  auth: RequestAuth
  bodyMode: BodyMode
  bodyRaw: string
  formFields: domain.FormField[]
  binaryFilePath: string
  options: RequestOptions
}

export const emptyAuth = (): RequestAuth => ({
  type: 'none',
  token: '',
  username: '',
  password: '',
  key: '',
  value: '',
  tokenUrl: '',
  clientId: '',
  clientSecret: '',
  scope: '',
})

export const emptyDraft = (workspace?: WorkspaceDefaults): RequestDraft => ({
  name: 'New Request',
  method: 'GET',
  url: '',
  params: [],
  headers: [],
  auth: emptyAuth(),
  bodyMode: 'none',
  bodyRaw: '',
  formFields: [],
  binaryFilePath: '',
  options: defaultOptions(workspace),
})
