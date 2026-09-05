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
export type RequestTab = 'params' | 'headers' | 'auth' | 'body' | 'code'
export type AuthType = 'none' | 'bearer' | 'basic' | 'apikey'
export type CodeFormat = 'curl' | 'shell' | 'powershell' | 'powershell-script'
export const codeFormats: { value: CodeFormat; label: string }[] = [
  { value: 'curl', label: 'curl' },
  { value: 'shell', label: 'shell script' },
  { value: 'powershell', label: 'PowerShell' },
  { value: 'powershell-script', label: 'PowerShell script' },
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
}

export const emptyAuth = (): RequestAuth => ({
  type: 'none',
  token: '',
  username: '',
  password: '',
  key: '',
  value: '',
})

export const emptyDraft = (): RequestDraft => ({
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
})
