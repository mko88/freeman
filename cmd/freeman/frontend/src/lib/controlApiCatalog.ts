// The control API's own documentation: every REST route and every
// ui:action the app answers to, with the payload each takes.
//
// Here rather than in HelpModal.svelte, which used to own it, because
// three things read it now: the help modal renders it, App.svelte
// reports it to the Go side on mount (so GET /api/agent can serve it to
// an agent that has no way to read a Svelte component), and
// scripts/control_api/checks/consistency.py parses this file as source
// to diff it against dispatchUIAction's cases and the routes Go
// registers. Keep the two arrays plain and literal, with nothing else
// between them, so that parse stays simple.

export type ApiEndpoint = { method: string; path: string; desc: string }
export type UiAction = { action: string; payload: string; desc: string }

export const apiEndpoints: ApiEndpoint[] = [
  { method: 'GET', path: '/api/health', desc: 'Health check.' },
  {
    method: 'GET',
    path: '/api/agent',
    desc: 'These two tables as Markdown, with the guard rules and the act-then-read loop — for an AI agent to read first.',
  },
  { method: 'GET', path: '/api/workspace', desc: 'Current workspace (collection/environment summaries).' },
  { method: 'GET', path: '/api/collections', desc: 'List collections.' },
  { method: 'GET', path: '/api/collections/{id}', desc: 'Get a collection, including its requests.' },
  { method: 'POST', path: '/api/collections', desc: 'Create a collection. Body: {name}.' },
  { method: 'PATCH', path: '/api/collections/{id}', desc: 'Rename a collection. Body: {name}.' },
  {
    method: 'DELETE',
    path: '/api/collections/{id}',
    desc: 'Delete a collection and every request in it. Refused for the last one.',
  },
  { method: 'POST', path: '/api/collections/{id}/requests', desc: 'Save a request. Body: a domain.Item.' },
  { method: 'DELETE', path: '/api/collections/{id}/requests/{itemId}', desc: 'Delete a saved request.' },
  { method: 'GET', path: '/api/environments', desc: 'List environments.' },
  { method: 'GET', path: '/api/environments/{id}', desc: 'Get an environment and its variables.' },
  { method: 'POST', path: '/api/environments', desc: 'Save an environment (creates if id is empty). Body: a domain.Environment.' },
  { method: 'DELETE', path: '/api/environments/{id}', desc: 'Delete an environment (refused for the last one).' },
  { method: 'POST', path: '/api/execute', desc: 'Execute a saved request. Body: {collectionId, itemId, environmentId}.' },
  {
    method: 'POST',
    path: '/api/codegen',
    desc:
      'Render a request as a runnable script. Body: {item: domain.Item, environmentId, format}. ' +
      "format is 'bash' or 'powershell'. Returns {code}.",
  },
  { method: 'GET', path: '/api/theme', desc: 'Resolved color palette.' },
  { method: 'GET', path: '/api/headers', desc: 'Common request-header names/values for editor autocomplete (from headers.yaml).' },
  { method: 'POST', path: '/api/ui/action', desc: 'Drive the GUI itself (desktop only, see below). Body: {action, payload}.' },
  {
    method: 'GET',
    path: '/api/ui/state',
    desc:
      'Everything the editor is showing, as one object. The request draft: name, method, url, params, ' +
      'headers, auth, bodyMode, bodyRaw, formFields, binaryFilePath — these keys are exactly the ones ' +
      'setRequestField and its siblings accept, so a getter exists for every setter. ' +
      'The workspace: workspaceRoot, collections, collectionId, collectionName, environments, ' +
      'environmentId, environment (the one open in settings, which need not be the active one), ' +
      'selectedItemId. The Code tab: codeFormat and code (its rendered script, when that tab is open). ' +
      'The last response: response (truncated with bodyFile in place of body when it was too large — ' +
      'bodyFile is its path in the per-request on-disk cache, see clearResponseCache; capped when the ' +
      'response outgrew the in-memory ceiling and body holds only what was read), plus responseTab ' +
      '(body/headers) and responseView (pretty/raw/hex). ' +
      'Chrome: tab, bodyLanguage, requestPaneCollapsed, responsePaneCollapsed, ' +
      'responseHeight, showCollectionMenu, showEnvironmentMenu, showResponseActionsMenu, showSettings, ' +
      'settingsTab, showHelp, sidebarWidth, statusBarHeight, showControlApiLog. Read this after every ' +
      'action instead of screenshotting the window (desktop only).',
  },
]

export const uiActions: UiAction[] = [
  {
    action: 'toggleSettings',
    payload: '—',
    desc: 'Open/close the settings window (workspace folder, collections, environments).',
  },
  {
    action: 'selectSettingsTab',
    payload: '{ tab }',
    desc: "Switch the settings window tab. tab is 'workspace', 'collections' or 'environments'.",
  },
  { action: 'selectEnvironment', payload: '{ id }', desc: 'Switch the active environment (and open it in settings).' },
  {
    action: 'renameEnvironment',
    payload: '{ name, id? }',
    desc:
      'Rename an environment (default: the active one). Works whether or not it is open in settings — ' +
      "the settings list's name field uses this.",
  },
  {
    action: 'expandEnvironment',
    payload: '{ id? }',
    desc:
      "Open an environment's variables in the settings list without making it the active one. " +
      'No id closes whichever is open. The environment actions below all act on the open one.',
  },
  { action: 'newEnvironment', payload: '—', desc: 'Create a new environment and switch to it.' },
  {
    action: 'setEnvironmentField',
    payload: "{ field: 'name', value }",
    desc: 'Rename the environment currently in the editor (persisted by saveEnvironment).',
  },
  {
    action: 'deleteEnvironment',
    payload: '{ id? }',
    desc: 'Delete an environment by id (default: the active one). Refused for the last one; no confirmation.',
  },
  { action: 'selectCollection', payload: '{ id }', desc: 'Switch the active collection.' },
  { action: 'newCollection', payload: '{ name }', desc: 'Create a collection and switch to it.' },
  {
    action: 'renameCollection',
    payload: '{ name, id? }',
    desc: 'Rename a collection (default: the open one). Renames its folder on disk to match.',
  },
  {
    action: 'deleteCollection',
    payload: '{ id? }',
    desc:
      'Delete a collection and every request in it (default: the open one). ' +
      'Refused for the last one; no confirmation.',
  },
  {
    action: 'toggleCollectionMenu',
    payload: '—',
    desc: "Open/close the top bar's collection picker (picking only — the rest is the settings window).",
  },
  {
    action: 'toggleEnvironmentMenu',
    payload: '—',
    desc: "Open/close the top bar's environment picker.",
  },
  { action: 'selectRequest', payload: '{ id }', desc: 'Select a request in the sidebar.' },
  {
    action: 'deleteRequest',
    payload: '{ id }',
    desc: 'Delete a saved request. Unlike the sidebar’s delete button, this does not ask for confirmation.',
  },
  { action: 'newRequest', payload: '—', desc: 'Clear the editor for a new, unsaved request.' },
  { action: 'saveRequest', payload: '—', desc: 'Save the request currently in the editor.' },
  { action: 'sendRequest', payload: '—', desc: 'Save, then execute, the request currently in the editor.' },
  {
    action: 'toggleResponseActionsMenu',
    payload: '—',
    desc: 'Open/close the response pane\'s "..." actions menu.',
  },
  {
    action: 'setResponseTab',
    payload: '{ tab }',
    desc:
      "Switch the response panel. tab is 'body' or 'headers' (the response's headers). " +
      'Always expands the pane if it was collapsed.',
  },
  {
    action: 'setResponseView',
    payload: '{ view }',
    desc:
      "Switch the response view. view is 'pretty' (JSON/XML pretty-printed + highlighted, an image rendered, " +
      "headers as a table), 'raw' (the payload as text, uncoloured) or 'hex' (a hexdump of its bytes). " +
      'Applies to whichever panel is open, body or headers.',
  },
  {
    action: 'openResponseCacheExternally',
    payload: '—',
    desc: 'Open the selected request\'s cached response body in its default external application (any response, not just a truncated one; desktop only).',
  },
  {
    action: 'copyResponseCachePath',
    payload: '—',
    desc: "Copy the selected request's cached response body file path to the clipboard.",
  },
  {
    action: 'openResponseCacheInFileExplorer',
    payload: '—',
    desc: "Reveal the selected request's cached response body file in the OS file manager (desktop only).",
  },
  {
    action: 'clearCachedResponse',
    payload: '—',
    desc: "Delete just the selected request's cached response and clear the pane.",
  },
  {
    action: 'clearResponseCache',
    payload: '—',
    desc: "Delete every cached response in the open workspace (see GET /api/ui/state's response field).",
  },
  { action: 'openWorkspace', payload: '{ path }', desc: 'Open a workspace by path (no folder dialog).' },
  { action: 'toggleHelp', payload: '—', desc: 'Open/close this help panel.' },
  {
    action: 'selectRequestTab',
    payload: '{ tab }',
    desc:
      "Switch the request editor tab. tab is 'params', 'headers', 'auth', 'body' or 'code'. " +
      "'code' hides the response pane; the response itself is unaffected and still readable through /api/ui/state.",
  },
  {
    action: 'selectCodeFormat',
    payload: '{ format }',
    desc:
      "Switch the Code tab's output. format is 'bash' (a curl script) or 'powershell' " +
      "(an Invoke-RestMethod script). Read the rendered script from GET /api/ui/state's code field.",
  },
  {
    action: 'selectBodyLanguage',
    payload: '{ language }',
    desc:
      "Set how the Body tab's raw editor is syntax-highlighted. language is 'auto', 'json', 'xml', or " +
      "'plain'; 'auto' reads the request's Content-Type header, then the body's first character. A view " +
      'preference only — it changes nothing about what gets sent.',
  },
  {
    action: 'copyRequestCode',
    payload: '—',
    desc: "Copy the Code tab's rendered command to the clipboard.",
  },
  {
    action: 'toggleRequestPane',
    payload: '—',
    desc:
      'Collapse/expand the request editor’s tab content, leaving its name/URL/tab rows ' +
      '(same as the ▾ toggle at the head of the tab row). The response takes the freed space.',
  },
  {
    action: 'toggleResponsePane',
    payload: '—',
    desc: 'Collapse/expand the response body/headers, leaving its status strip (same as the ▾ toggle on that strip).',
  },
  { action: 'toggleControlApiLog', payload: '—', desc: 'Collapse/expand the control API log at the bottom.' },
  { action: 'setSidebarWidth', payload: '{ px }', desc: 'Resize the request list (clamped to a sane range).' },
  {
    action: 'setResponseHeight',
    payload: '{ px }',
    desc: 'Set the response pane’s height, i.e. the request/response splitter (clamped to a sane range).',
  },
  {
    action: 'setStatusBarHeight',
    payload: '{ px }',
    desc: 'Resize the control API log panel (clamped to a sane range).',
  },
  {
    action: 'setRequestField',
    payload: '{ field, value }',
    desc:
      "Set a field in the request currently in the editor (before saving). field is 'name', 'method', " +
      "'url', 'bodyRaw', 'bodyMode', or 'binaryFilePath'. bodyMode's value is 'none', 'raw', 'form-data', " +
      "'x-www-form-urlencoded', or 'binary'. binaryFilePath is the local path sent as the whole body when " +
      "bodyMode is 'binary'.",
  },
  {
    action: 'setRequestAuth',
    payload: "{ field, value }",
    desc:
      "Set an Auth-tab field on the request in the editor (before saving). field is 'type', 'token', " +
      "'username', 'password', 'key', or 'value'. type is 'none', 'bearer', 'basic', or 'apikey'; the " +
      "engine turns a non-none auth into a header (Bearer/Basic Authorization, or key: value) at send time.",
  },
  {
    action: 'addRequestHeader',
    payload: '{ key?, value?, enabled? }',
    desc: 'Add a header row, optionally pre-filled (all fields optional; blank if omitted).',
  },
  {
    action: 'setRequestHeader',
    payload: '{ index, key?, value?, enabled? }',
    desc: 'Populate an existing header row by its position.',
  },
  { action: 'removeRequestHeader', payload: '{ index } | { key }', desc: 'Remove a header row by its position or by key.' },
  {
    action: 'addRequestParam',
    payload: '{ key?, value?, enabled? }',
    desc: 'Add a query-param row, optionally pre-filled (all fields optional; blank if omitted).',
  },
  {
    action: 'setRequestParam',
    payload: '{ index, key?, value?, enabled? }',
    desc: 'Populate an existing query-param row by its position.',
  },
  { action: 'removeRequestParam', payload: '{ index } | { key }', desc: 'Remove a query-param row by its position or by key.' },
  {
    action: 'addRequestFormField',
    payload: '{ key?, value?, enabled?, type?, filePath? }',
    desc:
      "Add a form-data / URL-encoded body field row, optionally pre-filled. type is 'text' or 'file'; " +
      "type 'file' + filePath uploads a local file (form-data only — urlencoded can't carry one).",
  },
  {
    action: 'setRequestFormField',
    payload: '{ index, key?, value?, enabled?, type?, filePath? }',
    desc: "Populate an existing form field row by its position. type is 'text' or 'file'.",
  },
  {
    action: 'removeRequestFormField',
    payload: '{ index } | { key }',
    desc: 'Remove a form field row by its position or by key.',
  },
  {
    action: 'addEnvironmentVariable',
    payload: '{ key?, value?, enabled?, secret? }',
    desc: 'Add a variable row to the open environment, optionally pre-filled.',
  },
  {
    action: 'setEnvironmentVariable',
    payload: '{ index, key?, value?, enabled?, secret? }',
    desc: 'Populate an existing variable row by its position.',
  },
  { action: 'removeEnvironmentVariable', payload: '{ index } | { key }', desc: 'Remove a variable row by its position or by key.' },
  { action: 'saveEnvironment', payload: '—', desc: 'Save the environment currently in the editor.' },
]
