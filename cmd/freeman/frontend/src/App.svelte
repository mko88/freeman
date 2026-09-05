<script lang="ts">
  import { onMount, tick } from 'svelte'
  import {
    CurrentWorkspace,
    OpenWorkspace,
    SelectWorkspaceFolder,
    GetCollection,
    SaveRequest,
    DeleteRequest,
    GetEnvironment,
    SaveEnvironment,
    ExecuteRequest,
    OpenResponseExternally,
    OpenResponseInFileExplorer,
    GetCachedResponse,
    ClearResponseCache,
    ClearCachedResponse,
    OpenResponseCacheExternally,
    GetResponseCachePath,
    GetResponseCacheDataURI,
    OpenResponseCacheInFileExplorer,
  } from '$backend'
  import type { domain, httpengine, core } from '../wailsjs/go/models'
  import { EventsOn } from '../wailsjs/runtime/runtime'
  import { ControlAPIAddr, GetHeaderCatalog, SelectFile, ReportUIState } from '../wailsjs/go/wailsapp/App.js'

  // Common request headers (and, per header, common values) offered as
  // autocomplete in the header editor. Loaded from the desktop backend on
  // mount (see internal/wailsapp.GetHeaderCatalog / internal/headercatalog,
  // configurable via headers.yaml); stays [] in the web build, which just
  // means no suggestions.
  type HeaderCatalogEntry = { name: string; values?: string[] }
  let headerCatalog: HeaderCatalogEntry[] = []

  // Common values for a header the user has typed, matched case-insensitively
  // against the catalog. [] when the header isn't in the catalog or has no
  // typical values — the caller then omits the value dropdown.
  function headerValues(key: string): string[] {
    const norm = key.trim().toLowerCase()
    return headerCatalog.find((e) => e.name.toLowerCase() === norm)?.values ?? []
  }

  let workspace: core.WorkspaceInfo | null = null
  let openError = ''

  let collectionId = ''
  let collection: domain.Collection | null = null
  let selectedItemId: string | null = null

  // Mirrors domain.BodyMode's / domain.FormFieldType's string constants —
  // Wails' binding generator doesn't emit a type for a named string type,
  // only struct classes, so these are redefined here (domain.Body.mode
  // and domain.FormField.type are themselves typed as plain strings in
  // models.ts).
  type BodyMode = 'none' | 'raw' | 'form-data' | 'x-www-form-urlencoded' | 'binary'
  type FormFieldType = 'text' | 'file'
  type RequestTab = 'params' | 'headers' | 'body'

  let draftName = 'New Request'
  let draftMethod = 'GET'
  let draftUrl = ''
  // Query params are appended to the URL at execution time (see
  // internal/httpengine.buildURL) — the Params tab edits them as a list
  // rather than syncing them into the URL string field.
  let draftParams: domain.QueryParam[] = []
  let draftHeaders: domain.Header[] = []
  let draftBodyMode: BodyMode = 'none'
  let draftBodyRaw = ''
  let draftFormFields: domain.FormField[] = []
  let draftBinaryFilePath = ''

  const bodyModes: { value: BodyMode; label: string }[] = [
    { value: 'none', label: 'none' },
    { value: 'raw', label: 'raw' },
    { value: 'form-data', label: 'form-data' },
    { value: 'x-www-form-urlencoded', label: 'x-www-form-urlencoded' },
    { value: 'binary', label: 'binary' },
  ]

  let environmentId = ''
  let environment: domain.Environment | null = null
  let showSettings = false
  let settingsTab: 'workspace' | 'environments' = 'workspace'

  let response: httpengine.Response | null = null
  let sending = false
  let sendError = ''
  // The response pane's "..." menu (Open in external editor/Copy path/
  // Open in File Explorer, keyed by selectedItemId — see
  // OpenResponseCacheExternally et al.) — available for any response
  // that's ever been sent, not just a truncated one, since every
  // response gets cached to disk (see saveResponseCache) regardless of
  // size.
  let showResponseActionsMenu = false
  // 'pretty' pretty-prints + syntax-highlights a JSON response body;
  // 'raw' shows exactly what came back. Sticky across requests — a view
  // preference, not per-response state. See formattedResponse below for
  // the (lightweight) detection of whether pretty is even possible.
  let responseView: 'pretty' | 'raw' = 'pretty'
  // Which of the response's two panels is showing — the body, or its
  // headers (the response's meta info the meta stats don't cover).
  // Sticky across requests, same as responseView.
  let responseTab: 'body' | 'headers' = 'body'
  // Data URI for an image response, loaded on demand from the cache file
  // (see GetResponseCacheDataURI) — the raw bytes in response.body don't
  // survive the JSON bridge intact. null until loaded / not an image.
  let responseImageUri: string | null = null
  let activeTab: RequestTab = 'headers'
  // Collapsed by re-clicking whichever tab is already active (see
  // onRequestTabClick below) — the Headers/Body table hides, and
  // .response (already flex: 1) just grows into the freed space.
  let requestPaneCollapsed = false

  // Sidebar width, the log panel's height, and whether the log panel is
  // collapsed are pure layout comfort — remembered per-browser-profile
  // via localStorage (silently no-op if unavailable, e.g. a locked-down
  // profile) rather than round-tripped through the workspace like real
  // request/environment data.
  const LAYOUT_PREFS_KEY = 'freeman.layoutPrefs'
  function loadLayoutPrefs(): { sidebarWidth?: number; statusBarHeight?: number; showControlApiLog?: boolean } {
    try {
      return JSON.parse(localStorage.getItem(LAYOUT_PREFS_KEY) ?? '{}')
    } catch {
      return {}
    }
  }
  function saveLayoutPrefs() {
    try {
      localStorage.setItem(LAYOUT_PREFS_KEY, JSON.stringify({ sidebarWidth, statusBarHeight, showControlApiLog }))
    } catch {
      // ignore — comfort setting only, not worth surfacing an error for
    }
  }
  const layoutPrefs = loadLayoutPrefs()
  const SIDEBAR_WIDTH_RANGE = [180, 560] as const
  const STATUS_BAR_HEIGHT_RANGE = [60, 500] as const
  let sidebarWidth = clamp(layoutPrefs.sidebarWidth ?? 260, SIDEBAR_WIDTH_RANGE)
  let statusBarHeight = clamp(layoutPrefs.statusBarHeight ?? 118, STATUS_BAR_HEIGHT_RANGE)
  let showControlApiLog = layoutPrefs.showControlApiLog ?? true
  function clamp(value: number, [min, max]: readonly [number, number]): number {
    return Math.min(max, Math.max(min, value))
  }

  // Drag-resize for the two splitters (sidebar | editor, and above the
  // status bar). One pair of window-level pointer listeners handles
  // whichever splitter is currently being dragged, rather than each
  // splitter wiring its own — there's only ever one drag in flight.
  let draggingSplitter: 'sidebar' | 'statusBar' | null = null
  function onSplitterPointerDown(which: typeof draggingSplitter) {
    draggingSplitter = which
  }
  function onWindowPointerMove(e: PointerEvent) {
    if (draggingSplitter === 'sidebar') {
      sidebarWidth = clamp(e.clientX, SIDEBAR_WIDTH_RANGE)
    } else if (draggingSplitter === 'statusBar') {
      statusBarHeight = clamp(window.innerHeight - e.clientY, STATUS_BAR_HEIGHT_RANGE)
    }
  }
  function onWindowPointerUp() {
    if (draggingSplitter) saveLayoutPrefs()
    draggingSplitter = null
  }

  function toggleControlApiLog() {
    showControlApiLog = !showControlApiLog
    saveLayoutPrefs()
  }

  const methods = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS']

  // Reference shown in the help modal — must match internal/httpapi's
  // registered routes (handler.go) and main.go's POST /api/ui/action, and
  // the action names handled in dispatchUIAction below. Kept as plain
  // data here rather than generated, so keep both sides in sync by hand.
  const apiEndpoints = [
    { method: 'GET', path: '/api/health', desc: 'Health check.' },
    { method: 'GET', path: '/api/workspace', desc: 'Current workspace (collection/environment summaries).' },
    { method: 'GET', path: '/api/collections', desc: 'List collections.' },
    { method: 'GET', path: '/api/collections/{id}', desc: 'Get a collection, including its requests.' },
    { method: 'POST', path: '/api/collections/{id}/requests', desc: 'Save a request. Body: a domain.Item.' },
    { method: 'DELETE', path: '/api/collections/{id}/requests/{itemId}', desc: 'Delete a saved request.' },
    { method: 'GET', path: '/api/environments', desc: 'List environments.' },
    { method: 'GET', path: '/api/environments/{id}', desc: 'Get an environment and its variables.' },
    { method: 'POST', path: '/api/environments', desc: 'Save an environment. Body: a domain.Environment.' },
    { method: 'POST', path: '/api/execute', desc: 'Execute a saved request. Body: {collectionId, itemId, environmentId}.' },
    {
      method: 'GET',
      path: '/api/execute/body',
      desc:
        "Read back a truncated response's full body (see /api/execute's response.truncated/bodyFile). " +
        'Query: ?path={bodyFile}.',
    },
    { method: 'GET', path: '/api/theme', desc: 'Resolved color palette.' },
    { method: 'GET', path: '/api/headers', desc: 'Common request-header names/values for editor autocomplete (from headers.yaml).' },
    { method: 'POST', path: '/api/ui/action', desc: 'Drive the GUI itself (desktop only, see below). Body: {action, payload}.' },
    {
      method: 'GET',
      path: '/api/ui/state',
      desc:
        'Current editor state — workspaceRoot/name/method/url/bodyMode/bodyRaw/binaryFilePath/params/headers/' +
        'formFields/tab/requestPaneCollapsed/selected ids/the open environment/the last response ' +
        '(truncated/bodyFile in place of body when it was too large — see /api/execute/body); ' +
        'responseTab (body/headers), responseView (pretty/raw) and responseKind (json/xml/html/image/text, ' +
        'lightly autodetected); every response is also ' +
        'cached to disk per request and reloaded on reselect, see clearResponseCache)/' +
        'showResponseActionsMenu/showSettings/settingsTab/showHelp/sidebarWidth/statusBarHeight/showControlApiLog — so a script ' +
        "can read what the UI shows instead of screenshotting it (desktop only).",
    },
  ]

  // Action names spell out their target explicitly — Request or
  // Environment — wherever one applies, rather than a bare verb (e.g.
  // addRequestHeader/addEnvironmentVariable, not addHeader/addVariable),
  // so the list reads unambiguously on its own.
  const uiActions = [
    { action: 'toggleSettings', payload: '—', desc: 'Open/close the settings window (workspace folder, environments).' },
    {
      action: 'selectSettingsTab',
      payload: '{ tab }',
      desc: "Switch the settings window tab. tab is 'workspace' or 'environments'.",
    },
    { action: 'selectEnvironment', payload: '{ id }', desc: 'Switch the active environment.' },
    { action: 'selectCollection', payload: '{ id }', desc: 'Switch the active collection.' },
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
      action: 'openResponseExternally',
      payload: '—',
      desc: "Open a truncated response's full body in its default external application (desktop only).",
    },
    {
      action: 'copyResponsePath',
      payload: '—',
      desc: "Copy a truncated response's full-body file path to the clipboard.",
    },
    {
      action: 'openResponseInFileExplorer',
      payload: '—',
      desc: "Reveal a truncated response's full-body file in the OS file manager (desktop only).",
    },
    {
      action: 'toggleResponseActionsMenu',
      payload: '—',
      desc: 'Open/close the response pane\'s "..." actions menu.',
    },
    {
      action: 'setResponseTab',
      payload: '{ tab }',
      desc: "Switch the response panel. tab is 'body' or 'headers' (the response's headers).",
    },
    {
      action: 'setResponseView',
      payload: '{ view }',
      desc: "Switch the response body view. view is 'pretty' (JSON/XML pretty-printed + highlighted) or 'raw'.",
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
      desc: "Switch the request editor tab. tab is 'params', 'headers' or 'body'.",
    },
    {
      action: 'toggleRequestPane',
      payload: '—',
      desc: 'Collapse/expand the request editor’s Headers/Body content (same as clicking the active tab).',
    },
    { action: 'toggleControlApiLog', payload: '—', desc: 'Collapse/expand the control API log at the bottom.' },
    { action: 'setSidebarWidth', payload: '{ px }', desc: 'Resize the request list (clamped to a sane range).' },
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

  // Log of ui:action events received from the control API (see
  // internal/wailsapp.DispatchUIAction) — rendered in the status bar so
  // it's visible when an external script or agent is driving the app.
  // Regular in-app clicks don't go through this event bus, so they don't
  // appear here; capped so a long-running session doesn't grow forever.
  let statusLog: { time: string; text: string }[] = []
  let statusLogEl: HTMLElement
  let controlApiAddr = ''
  let showHelp = false

  function logEvent(text: string) {
    const time = new Date().toLocaleTimeString()
    statusLog = [...statusLog.slice(-49), { time, text }]
    tick().then(() => {
      if (statusLogEl) statusLogEl.scrollTop = statusLogEl.scrollHeight
    })
  }

  // DispatchUIAction's read-side counterpart: mirrors the editor's draft
  // state to the Go side, so GET /api/ui/state (see cmd/freeman/main.go)
  // can answer "what does the app currently show" without a screenshot —
  // a getter for every setRequestField `field`
  // (name/method/url/bodyRaw/bodyMode/binaryFilePath), selectRequestTab's
  // tab, selectRequest/selectCollection/selectEnvironment's ids, the
  // header/form-field rows, the open environment, and — the main point —
  // the result of the last saveRequest/sendRequest, so a script can read
  // the response instead of watching the window for it.
  //
  // A `$:` reactive statement, not a function called only from the end
  // of dispatchUIAction: the control API isn't the only way this state
  // changes — a human clicking the sidebar (selectRequest), a tab button,
  // or typing into a bound input needs to show up here too, and none of
  // those go through dispatchUIAction. Svelte only reruns a `$:`
  // statement for a variable *it directly references*, not one only read
  // inside a function it calls — so the state object has to be built
  // inline right here, with every mirrored field a literal reference in
  // the statement itself, rather than delegated to a helper function.
  $: if ('runtime' in window) {
    const state = {
      workspaceRoot: workspace?.root ?? '',
      collectionId,
      environmentId,
      selectedItemId,
      tab: activeTab,
      requestPaneCollapsed,
      name: draftName,
      method: draftMethod,
      url: draftUrl,
      bodyMode: draftBodyMode,
      bodyRaw: draftBodyRaw,
      binaryFilePath: draftBinaryFilePath,
      params: draftParams,
      headers: draftHeaders,
      formFields: draftFormFields,
      environment,
      showSettings,
      settingsTab,
      showHelp,
      sending,
      sendError,
      response,
      responseTab,
      responseView,
      responseKind: formattedResponse.kind,
      showResponseActionsMenu,
      sidebarWidth,
      statusBarHeight,
      showControlApiLog,
    }
    // Encoded here and passed as a string, not the plain object — a
    // Wails-bound method taking an object argument from the frontend
    // didn't reliably reach the Go side in testing (ReportUIState kept
    // storing whatever was first reported, never a later update); Go
    // just writes this string straight back out for GET /api/ui/state.
    ReportUIState(JSON.stringify(state)).catch((e) => logEvent(`reportUIState failed: ${e}`))
  }

  onMount(async () => {
    const ws = await CurrentWorkspace()
    if (ws) await initWorkspace(ws)

    // 'runtime' only exists on window inside the Wails-hosted desktop
    // build — the web build (cmd/freeman-server) has no Wails bridge, so
    // this control-API-driven UI dispatch is desktop-only by design (see
    // internal/wailsapp.DispatchUIAction).
    if ('runtime' in window) {
      controlApiAddr = await ControlAPIAddr()
      // Serialize ui:action events through one promise chain rather than
      // firing dispatchUIAction for each as it arrives: several actions
      // (saveRequest, sendRequest, saveEnvironment, selectEnvironment,
      // selectCollection, openWorkspace) do an async round trip to the
      // backend, and a caller sending actions back-to-back (a script, an
      // agent) got no backpressure from POST /api/ui/action, which
      // returns as soon as the event is emitted — not once the frontend
      // has applied it. Two saveEnvironment calls in quick succession
      // could race: the second one's local mutations (e.g. a
      // removeEnvironmentVariable in between) got clobbered when the
      // first call's stale `environment = saved` resolved after them.
      // Awaiting each action fully before starting
      // the next makes rapid sequences apply in order, deterministically.
      let uiActionQueue: Promise<void> = Promise.resolve()
      EventsOn('ui:action', (action: string, payload: Record<string, unknown> | null) => {
        uiActionQueue = uiActionQueue
          .then(() => dispatchUIAction(action, payload))
          .catch((e) => logEvent(`${action} failed: ${e}`))
      })
      try {
        headerCatalog = await GetHeaderCatalog()
      } catch {
        headerCatalog = []
      }
    }
  })

  // Handles events emitted by internal/wailsapp.DispatchUIAction (via
  // POST /api/ui/action on the desktop control API) — lets an external
  // script or agent drive the GUI itself (open the env editor, pick an
  // environment/collection/request, save, send), the same actions a user
  // clicking around would trigger. Curated on purpose: add a case here
  // for each new action rather than a generic selector-based mechanism.
  // Resolves a row-targeting payload to an index into `rows`. Accepts
  // either { index } (its position) or { key } (the first row with that
  // key) — the latter is friendlier for scripts/agents that know a
  // header or variable name but not its ordinal. Returns -1 if neither
  // is usable or the key isn't found.
  function rowIndex(rows: { key: string }[], payload: Record<string, unknown> | null): number {
    const key = payload?.key
    if (typeof key === 'string') return rows.findIndex((r) => r.key === key)
    const index = Number(payload?.index)
    return Number.isNaN(index) ? -1 : index
  }

  async function dispatchUIAction(action: string, payload: Record<string, unknown> | null) {
    logEvent(payload && Object.keys(payload).length ? `${action} ${JSON.stringify(payload)}` : action)
    switch (action) {
      case 'toggleSettings':
        showSettings = !showSettings
        break
      case 'selectSettingsTab': {
        const tab = payload?.tab
        if (tab === 'workspace' || tab === 'environments') settingsTab = tab
        break
      }
      case 'selectEnvironment':
        if (payload?.id) await selectEnvironment(String(payload.id))
        break
      case 'selectCollection':
        if (payload?.id) await selectCollection(String(payload.id))
        break
      case 'selectRequest': {
        const item = (collection?.items ?? []).find((i) => i.id === payload?.id)
        if (item) await selectRequest(item)
        break
      }
      case 'deleteRequest': {
        const id = payload?.id
        if (typeof id === 'string' && id) await deleteRequest(id)
        break
      }
      case 'newRequest':
        newRequest()
        break
      case 'saveRequest':
        await saveRequest()
        break
      case 'sendRequest':
        await sendRequest()
        break
      case 'openResponseExternally':
        await openResponseExternally()
        break
      case 'copyResponsePath':
        await copyResponsePath()
        break
      case 'openResponseInFileExplorer':
        await openResponseInFileExplorer()
        break
      case 'toggleResponseActionsMenu':
        toggleResponseActionsMenu()
        break
      case 'setResponseView': {
        const view = payload?.view
        if (view === 'pretty' || view === 'raw') setResponseView(view)
        break
      }
      case 'setResponseTab': {
        const tab = payload?.tab
        if (tab === 'body' || tab === 'headers') setResponseTab(tab)
        break
      }
      case 'openResponseCacheExternally':
        await openResponseCacheExternally()
        break
      case 'copyResponseCachePath':
        await copyResponseCachePath()
        break
      case 'openResponseCacheInFileExplorer':
        await openResponseCacheInFileExplorer()
        break
      case 'clearCachedResponse':
        await clearCachedResponse()
        break
      case 'clearResponseCache':
        await clearResponseCache()
        break
      case 'openWorkspace': {
        const path = payload?.path
        if (typeof path === 'string' && path) await openWorkspaceByPath(path)
        break
      }
      case 'toggleHelp':
        showHelp = !showHelp
        break
      case 'selectRequestTab': {
        const tab = payload?.tab
        if (tab === 'params' || tab === 'headers' || tab === 'body') selectRequestEditorTab(tab)
        break
      }
      case 'toggleRequestPane':
        requestPaneCollapsed = !requestPaneCollapsed
        break
      case 'toggleControlApiLog':
        toggleControlApiLog()
        break
      case 'setSidebarWidth': {
        const px = Number(payload?.px)
        if (!Number.isNaN(px)) {
          sidebarWidth = clamp(px, SIDEBAR_WIDTH_RANGE)
          saveLayoutPrefs()
        }
        break
      }
      case 'setStatusBarHeight': {
        const px = Number(payload?.px)
        if (!Number.isNaN(px)) {
          statusBarHeight = clamp(px, STATUS_BAR_HEIGHT_RANGE)
          saveLayoutPrefs()
        }
        break
      }
      case 'setRequestField': {
        const value = payload?.value
        if (typeof value !== 'string') break
        switch (payload?.field) {
          case 'name':
            draftName = value
            break
          case 'method':
            draftMethod = value
            break
          case 'url':
            draftUrl = value
            break
          case 'bodyRaw':
            draftBodyRaw = value
            break
          case 'bodyMode':
            if (bodyModes.some((m) => m.value === value)) draftBodyMode = value as BodyMode
            break
          case 'binaryFilePath':
            draftBinaryFilePath = value
            break
        }
        break
      }
      case 'addRequestHeader':
        addRequestHeader({
          key: typeof payload?.key === 'string' ? payload.key : '',
          value: typeof payload?.value === 'string' ? payload.value : '',
          enabled: typeof payload?.enabled === 'boolean' ? payload.enabled : true,
        })
        break
      case 'setRequestHeader': {
        const index = Number(payload?.index)
        if (Number.isNaN(index)) break
        const fields: Partial<domain.Header> = {}
        if (typeof payload?.key === 'string') fields.key = payload.key
        if (typeof payload?.value === 'string') fields.value = payload.value
        if (typeof payload?.enabled === 'boolean') fields.enabled = payload.enabled
        setRequestHeader(index, fields)
        break
      }
      case 'removeRequestHeader': {
        const index = rowIndex(draftHeaders, payload)
        if (index >= 0) removeRequestHeader(index)
        break
      }
      case 'addRequestParam':
        addRequestParam({
          key: typeof payload?.key === 'string' ? payload.key : '',
          value: typeof payload?.value === 'string' ? payload.value : '',
          enabled: typeof payload?.enabled === 'boolean' ? payload.enabled : true,
        })
        break
      case 'setRequestParam': {
        const index = Number(payload?.index)
        if (Number.isNaN(index)) break
        const fields: Partial<domain.QueryParam> = {}
        if (typeof payload?.key === 'string') fields.key = payload.key
        if (typeof payload?.value === 'string') fields.value = payload.value
        if (typeof payload?.enabled === 'boolean') fields.enabled = payload.enabled
        setRequestParam(index, fields)
        break
      }
      case 'removeRequestParam': {
        const index = rowIndex(draftParams, payload)
        if (index >= 0) removeRequestParam(index)
        break
      }
      case 'addRequestFormField':
        addRequestFormField({
          key: typeof payload?.key === 'string' ? payload.key : '',
          value: typeof payload?.value === 'string' ? payload.value : '',
          enabled: typeof payload?.enabled === 'boolean' ? payload.enabled : true,
          type: payload?.type === 'file' ? 'file' : 'text',
          filePath: typeof payload?.filePath === 'string' ? payload.filePath : '',
        })
        break
      case 'setRequestFormField': {
        const index = Number(payload?.index)
        if (Number.isNaN(index)) break
        const fields: Partial<domain.FormField> = {}
        if (typeof payload?.key === 'string') fields.key = payload.key
        if (typeof payload?.value === 'string') fields.value = payload.value
        if (typeof payload?.enabled === 'boolean') fields.enabled = payload.enabled
        if (payload?.type === 'text' || payload?.type === 'file') fields.type = payload.type
        if (typeof payload?.filePath === 'string') fields.filePath = payload.filePath
        setRequestFormField(index, fields)
        break
      }
      case 'removeRequestFormField': {
        const index = rowIndex(draftFormFields, payload)
        if (index >= 0) removeRequestFormField(index)
        break
      }
      case 'addEnvironmentVariable':
        addEnvironmentVariable({
          key: typeof payload?.key === 'string' ? payload.key : '',
          value: typeof payload?.value === 'string' ? payload.value : '',
          enabled: typeof payload?.enabled === 'boolean' ? payload.enabled : true,
          secret: typeof payload?.secret === 'boolean' ? payload.secret : false,
        })
        break
      case 'setEnvironmentVariable': {
        const index = Number(payload?.index)
        if (Number.isNaN(index)) break
        const fields: Partial<domain.Variable> = {}
        if (typeof payload?.key === 'string') fields.key = payload.key
        if (typeof payload?.value === 'string') fields.value = payload.value
        if (typeof payload?.enabled === 'boolean') fields.enabled = payload.enabled
        if (typeof payload?.secret === 'boolean') fields.secret = payload.secret
        setEnvironmentVariable(index, fields)
        break
      }
      case 'removeEnvironmentVariable': {
        const index = rowIndex(environment?.variables ?? [], payload)
        if (index >= 0) removeEnvironmentVariable(index)
        break
      }
      case 'saveEnvironment':
        await saveEnvironment()
        break
    }
  }

  async function openWorkspace() {
    openError = ''
    try {
      const path = await SelectWorkspaceFolder()
      if (!path) return
      await openWorkspaceByPath(path)
    } catch (e) {
      openError = String(e)
    }
  }

  // Shared by the native-picker flow (openWorkspace) and the control
  // API's "openWorkspace" ui:action, which supplies a path directly
  // instead of showing a dialog — the browser-less way to point a
  // running instance at a workspace.
  async function openWorkspaceByPath(path: string) {
    openError = ''
    try {
      const ws = await OpenWorkspace(path)
      await initWorkspace(ws)
    } catch (e) {
      openError = String(e)
    }
  }

  async function initWorkspace(ws: core.WorkspaceInfo) {
    workspace = ws
    if (ws.environments.length) await selectEnvironment(ws.environments[0].id)
    if (ws.collections.length) await selectCollection(ws.collections[0].id)
  }

  async function selectCollection(id: string) {
    collectionId = id
    collection = await GetCollection(id)
    if (collection.items?.length) {
      await selectRequest(collection.items[0])
    } else {
      newRequest()
    }
  }

  async function selectRequest(item: domain.Item) {
    selectedItemId = item.id
    draftName = item.name
    draftMethod = item.method || 'GET'
    draftUrl = item.url || ''
    draftParams = item.params ? item.params.map((p) => ({ ...p })) : []
    draftHeaders = item.headers ? item.headers.map((h) => ({ ...h })) : []
    draftBodyMode = (item.body?.mode as BodyMode) || 'none'
    draftBodyRaw = item.body?.raw || ''
    // { type: 'text', filePath: '', ...f } normalizes rows saved before
    // file fields existed (omitempty means those keys are simply absent,
    // never present-but-undefined, so the defaults only apply then).
    draftFormFields = item.body?.formFields
      ? item.body.formFields.map((f) => ({ type: 'text', filePath: '', ...f }))
      : []
    draftBinaryFilePath = item.body?.binaryFilePath || ''
    sendError = ''
    responseImageUri = null
    // GetCachedResponse rejects with "nothing cached yet" for a request
    // that's never been sent (the common case) just as often as for a
    // real failure — either way, falling back to a blank response pane
    // is the right outcome, not a logged error.
    try {
      response = await GetCachedResponse(item.id)
    } catch {
      response = null
    }
    await refreshResponseImage()
  }

  function newRequest() {
    selectedItemId = null
    draftName = 'New Request'
    draftMethod = 'GET'
    draftUrl = ''
    draftParams = []
    draftHeaders = []
    draftBodyMode = 'none'
    draftBodyRaw = ''
    draftFormFields = []
    draftBinaryFilePath = ''
    response = null
    sendError = ''
    responseImageUri = null
  }

  function addRequestHeader(initial?: Partial<domain.Header>) {
    draftHeaders = [...draftHeaders, { key: '', value: '', enabled: true, ...initial }]
  }

  function setRequestHeader(index: number, fields: Partial<domain.Header>) {
    draftHeaders = draftHeaders.map((h, i) => (i === index ? { ...h, ...fields } : h))
  }

  function removeRequestHeader(index: number) {
    draftHeaders = draftHeaders.filter((_, i) => i !== index)
  }

  function addRequestParam(initial?: Partial<domain.QueryParam>) {
    draftParams = [...draftParams, { key: '', value: '', enabled: true, ...initial }]
  }

  function setRequestParam(index: number, fields: Partial<domain.QueryParam>) {
    draftParams = draftParams.map((p, i) => (i === index ? { ...p, ...fields } : p))
  }

  function removeRequestParam(index: number) {
    draftParams = draftParams.filter((_, i) => i !== index)
  }

  function addRequestFormField(initial?: Partial<domain.FormField>) {
    draftFormFields = [...draftFormFields, { key: '', value: '', enabled: true, type: 'text', filePath: '', ...initial }]
  }

  function setRequestFormField(index: number, fields: Partial<domain.FormField>) {
    draftFormFields = draftFormFields.map((f, i) => (i === index ? { ...f, ...fields } : f))
  }

  function removeRequestFormField(index: number) {
    draftFormFields = draftFormFields.filter((_, i) => i !== index)
  }

  // Opens the native file picker (desktop only) and writes the chosen
  // path into a form-data row. No control-API equivalent needs this
  // function itself — a script sets filePath directly via
  // addRequestFormField/setRequestFormField, the same "no dialog
  // available" split as openWorkspace vs. the folder picker.
  async function pickRequestFormFieldFile(index: number) {
    const path = await SelectFile()
    if (path) setRequestFormField(index, { filePath: path })
  }

  async function pickBinaryFile() {
    const path = await SelectFile()
    if (path) draftBinaryFilePath = path
  }

  async function saveRequest(): Promise<domain.Item> {
    const isFormMode = draftBodyMode === 'form-data' || draftBodyMode === 'x-www-form-urlencoded'
    const item = {
      id: selectedItemId ?? '',
      type: 'request',
      name: draftName || 'Untitled Request',
      method: draftMethod,
      url: draftUrl,
      params: draftParams,
      headers: draftHeaders,
      body: {
        mode: draftBodyMode,
        raw: draftBodyMode === 'raw' ? draftBodyRaw : '',
        formFields: isFormMode ? draftFormFields : [],
        binaryFilePath: draftBodyMode === 'binary' ? draftBinaryFilePath : '',
      },
    } as domain.Item
    const saved = await SaveRequest(collectionId, item)
    selectedItemId = saved.id
    collection = await GetCollection(collectionId)
    return saved
  }

  // Shared by the sidebar's delete button (after a confirm() prompt) and
  // the control API's "deleteRequest" ui:action, which deletes immediately —
  // a script/agent driving the app has already decided, the way clicking
  // "Send" doesn't ask "are you sure?" either.
  async function deleteRequest(id: string) {
    await DeleteRequest(collectionId, id)
    collection = await GetCollection(collectionId)
    if (selectedItemId === id) {
      if (collection.items?.length) {
        await selectRequest(collection.items[0])
      } else {
        newRequest()
      }
    }
  }

  function confirmDeleteRequest(item: domain.Item) {
    if (confirm(`Delete "${item.name}"? This can't be undone from the app.`)) {
      deleteRequest(item.id)
    }
  }

  async function sendRequest() {
    sendError = ''
    sending = true
    responseImageUri = null
    try {
      await saveRequest()
      response = await ExecuteRequest(collectionId, selectedItemId!, environmentId)
      await refreshResponseImage()
    } catch (e) {
      sendError = String(e)
      response = null
    } finally {
      sending = false
    }
  }

  async function openResponseExternally() {
    if (!response?.truncated) return
    try {
      await OpenResponseExternally(response.bodyFile ?? '')
    } catch (e) {
      logEvent(`openResponseExternally failed: ${e}`)
    }
  }

  async function copyResponsePath() {
    if (!response?.truncated) return
    try {
      await navigator.clipboard.writeText(response.bodyFile ?? '')
    } catch (e) {
      logEvent(`copyResponsePath failed: ${e}`)
    }
  }

  async function openResponseInFileExplorer() {
    if (!response?.truncated) return
    try {
      await OpenResponseInFileExplorer(response.bodyFile ?? '')
    } catch (e) {
      logEvent(`openResponseInFileExplorer failed: ${e}`)
    }
  }

  function toggleResponseActionsMenu() {
    showResponseActionsMenu = !showResponseActionsMenu
  }

  // The "..." menu's own Open in external editor/Copy path/Open in File
  // Explorer — the OpenResponse*Externally/InFileExplorer functions
  // above do the same things but only for a truncated response's
  // ephemeral bodyFile; these key off selectedItemId instead, so they
  // work for any response that's ever been cached (see
  // saveResponseCache), truncated or not.
  async function openResponseCacheExternally() {
    showResponseActionsMenu = false
    if (!selectedItemId) return
    try {
      await OpenResponseCacheExternally(selectedItemId)
    } catch (e) {
      logEvent(`openResponseCacheExternally failed: ${e}`)
    }
  }

  async function copyResponseCachePath() {
    showResponseActionsMenu = false
    if (!selectedItemId) return
    try {
      const path = await GetResponseCachePath(selectedItemId)
      await navigator.clipboard.writeText(path)
    } catch (e) {
      logEvent(`copyResponseCachePath failed: ${e}`)
    }
  }

  async function openResponseCacheInFileExplorer() {
    showResponseActionsMenu = false
    if (!selectedItemId) return
    try {
      await OpenResponseCacheInFileExplorer(selectedItemId)
    } catch (e) {
      logEvent(`openResponseCacheInFileExplorer failed: ${e}`)
    }
  }

  // Just this request's cached response — the per-request counterpart to
  // the settings window's whole-workspace clearResponseCache. Clears the
  // pane too, so it matches what a fresh (never-sent) request shows.
  async function clearCachedResponse() {
    showResponseActionsMenu = false
    if (!selectedItemId) return
    try {
      await ClearCachedResponse(selectedItemId)
      response = null
    } catch (e) {
      logEvent(`clearCachedResponse failed: ${e}`)
    }
  }

  let responseCacheCleared = false
  async function clearResponseCache() {
    try {
      await ClearResponseCache()
      responseCacheCleared = true
      setTimeout(() => (responseCacheCleared = false), 2000)
    } catch (e) {
      logEvent(`clearResponseCache failed: ${e}`)
    }
  }

  async function selectEnvironment(id: string) {
    environmentId = id
    environment = await GetEnvironment(id)
  }

  function addEnvironmentVariable(initial?: Partial<domain.Variable>) {
    if (!environment) return
    environment.variables = [...environment.variables, { key: '', value: '', enabled: true, secret: false, ...initial }]
  }

  function setEnvironmentVariable(index: number, fields: Partial<domain.Variable>) {
    if (!environment) return
    environment.variables = environment.variables.map((v, i) => (i === index ? { ...v, ...fields } : v))
  }

  function removeEnvironmentVariable(index: number) {
    if (!environment) return
    environment.variables = environment.variables.filter((_, i) => i !== index)
  }

  async function saveEnvironment() {
    if (!environment) return
    const saved = await SaveEnvironment(environment)
    environmentId = saved.id
    environment = saved
    if (workspace) {
      workspace.environments = workspace.environments.map((e) =>
        e.id === saved.id ? { ...e, name: saved.name } : e,
      )
    }
  }

  function formatDuration(ns: number): string {
    return `${Math.round(ns / 1e6)} ms`
  }

  function formatBytes(n: number): string {
    if (n < 1024) return `${n} bytes`
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
    return `${(n / (1024 * 1024)).toFixed(1)} MB`
  }

  // Go's http.Response.Status (httpengine.Response.status) is already
  // "<code> <reason>", e.g. "200 OK" — this strips the leading code so
  // it isn't shown twice next to statusCode ("200 200 OK").
  function reasonPhrase(status: string): string {
    return status.replace(/^\d+\s*/, '')
  }

  // selectRequestTab (control API) always deterministically switches to
  // and expands the given tab — a script asking for 'body' shouldn't get
  // a collapse instead just because 'body' already happened to be
  // active. The tab buttons' own click handler builds on this: switching
  // tabs behaves identically, but clicking the tab that's *already*
  // active collapses instead — a plain UI gesture with its own dedicated
  // ui:action (toggleRequestPane) for a script to reach the same thing
  // deterministically, without needing to know which tab is current.
  function selectRequestEditorTab(tab: RequestTab) {
    activeTab = tab
    requestPaneCollapsed = false
  }
  function onRequestTabClick(tab: RequestTab) {
    if (activeTab === tab) {
      requestPaneCollapsed = !requestPaneCollapsed
    } else {
      selectRequestEditorTab(tab)
    }
  }

  // Tab badges — count of rows with a key filled in (a blank row the
  // user just added isn't a param/header/field yet), blank at zero.
  const filledCount = (rows: { key: string }[]) => rows.filter((row) => row.key.trim()).length
  $: paramsTabBadge = filledCount(draftParams)
  $: headersTabBadge = filledCount(draftHeaders)
  // A body isn't a list, so its badge shows the field count for the form
  // modes and the mode name for raw/binary — again only once there's
  // actually something there.
  $: bodyTabBadge =
    draftBodyMode === 'form-data' || draftBodyMode === 'x-www-form-urlencoded'
      ? filledCount(draftFormFields)
        ? String(filledCount(draftFormFields))
        : ''
      : draftBodyMode === 'raw'
        ? draftBodyRaw.trim()
          ? 'raw'
          : ''
        : draftBodyMode === 'binary'
          ? draftBinaryFilePath
            ? 'binary'
            : ''
          : ''

  // Standard HTTP status-class semantics, for coloring the status badge.
  function statusTone(code: number): 'success' | 'info' | 'warning' | 'error' {
    if (code >= 200 && code < 300) return 'success'
    if (code >= 300 && code < 400) return 'info'
    if (code >= 400 && code < 500) return 'warning'
    return 'error'
  }

  // Above this the pretty view isn't worth the JSON.parse + regex pass on
  // every render — show raw instead. (Anything over the truncation
  // threshold never reaches the inline view at all; this is a lower cap
  // just for keeping the formatted path snappy.)
  const RESPONSE_PRETTY_MAX = 256 * 1024

  type ResponseKind = 'json' | 'xml' | 'html' | 'image' | 'text'

  function responseHeader(r: httpengine.Response | null, name: string): string {
    if (!r?.headers) return ''
    const key = Object.keys(r.headers).find((k) => k.toLowerCase() === name.toLowerCase())
    return key ? (r.headers[key]?.[0] ?? '') : ''
  }

  // Content-Type first, then a one-character sniff of the body — enough
  // to pick a renderer, not a full content classifier.
  function detectResponseKind(r: httpengine.Response | null): ResponseKind {
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
  function highlightJson(json: string): string {
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
  function prettyXml(xml: string): string {
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
  function highlightXml(xml: string): string {
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
  $: formattedResponse = ((r: httpengine.Response | null, view: 'pretty' | 'raw') => {
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
  })(response, responseView)

  function setResponseView(view: 'pretty' | 'raw') {
    responseView = view
  }

  function setResponseTab(tab: 'body' | 'headers') {
    responseTab = tab
  }

  // The response's headers, flattened (one row per value) and sorted, for
  // the Headers panel.
  $: responseHeaderRows = Object.entries(response?.headers ?? {})
    .flatMap(([name, values]) => (values ?? []).map((value) => ({ name, value })))
    .sort((a, b) => a.name.localeCompare(b.name) || a.value.localeCompare(b.value))

  // Called after a send / on reselect: pulls the image bytes out of the
  // cache as a data URI when the current response is an image, clears it
  // otherwise.
  async function refreshResponseImage() {
    if (!selectedItemId || detectResponseKind(response) !== 'image' || response?.truncated) {
      responseImageUri = null
      return
    }
    try {
      responseImageUri = await GetResponseCacheDataURI(selectedItemId)
    } catch {
      responseImageUri = null
    }
  }

  // Each HTTP method gets one consistent color everywhere it appears
  // (sidebar row accent, method tag, the method select) — a real
  // structural device: the method is the single most important fact
  // about a saved request, so it's the one thing this UI color-codes.
  // POST/PATCH/DELETE deliberately reuse the accent/success/error tokens
  // rather than getting their own hues, so the method system and the
  // status/action system read as one palette, not two.
  const methodColorVar: Record<string, string> = {
    GET: 'var(--fm-method-get)',
    POST: 'var(--fm-accent)',
    PUT: 'var(--fm-method-put)',
    PATCH: 'var(--fm-success)',
    DELETE: 'var(--fm-error)',
  }
  function methodColor(method: string): string {
    return methodColorVar[method] ?? 'var(--fm-method-neutral)'
  }
</script>

<svelte:window on:pointermove={onWindowPointerMove} on:pointerup={onWindowPointerUp} />

<div class="app-shell" class:is-resizing={draggingSplitter !== null}>
{#if !workspace}
  <main class="welcome">
    <h1>Freeman</h1>
    <p class="prose">Pick a folder to use as a workspace for your collections and environments.</p>
    <button class="primary" on:click={openWorkspace}>Choose workspace folder</button>
    {#if openError}<p class="error">{openError}</p>{/if}
  </main>
{:else}
  <div class="layout">
    <aside class="sidebar" style="width: {sidebarWidth}px">
      <div class="sidebar-header">
        <span>{collection?.name ?? ''}</span>
        <button class="icon-btn" title="New request" on:click={newRequest}>+</button>
      </div>
      <ul class="request-list">
        {#each collection?.items ?? [] as item (item.id)}
          <li class:active={item.id === selectedItemId} style="--m: {methodColor(item.method || 'GET')}">
            <button class="request-select" on:click={() => selectRequest(item)}>
              <span class="method-tag">{item.method || 'GET'}</span>
              <span class="request-select-name">{item.name}</span>
            </button>
            <button class="icon-btn" title="Delete request" on:click={() => confirmDeleteRequest(item)}>×</button>
          </li>
        {/each}
      </ul>
    </aside>

    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <div
      class="splitter splitter-vertical"
      class:active={draggingSplitter === 'sidebar'}
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize the request list"
      on:pointerdown={() => onSplitterPointerDown('sidebar')}
    ></div>

    <main class="editor">
      <div class="request-name">
        <input type="text" bind:value={draftName} placeholder="Request name" />
      </div>

      <div class="url-bar">
        <select class="method-select" bind:value={draftMethod} style="--m: {methodColor(draftMethod)}">
          {#each methods as m}<option value={m}>{m}</option>{/each}
        </select>
        <input type="text" bind:value={draftUrl} placeholder="https://api.example.com/{'{'}{'{'}baseUrl{'}'}{'}'}" />
        <button on:click={saveRequest}>Save</button>
        <button class="primary" on:click={sendRequest} disabled={sending}>
          {sending ? 'Sending…' : 'Send'}
        </button>
      </div>

      <div class="tabs">
        <button class:active={activeTab === 'params'} on:click={() => onRequestTabClick('params')}>
          Params{#if paramsTabBadge}<span class="tab-count">{paramsTabBadge}</span>{/if}
          {#if activeTab === 'params'}<span class="tab-chevron">{requestPaneCollapsed ? '▸' : '▾'}</span>{/if}
        </button>
        <button class:active={activeTab === 'headers'} on:click={() => onRequestTabClick('headers')}>
          Headers{#if headersTabBadge}<span class="tab-count">{headersTabBadge}</span>{/if}
          {#if activeTab === 'headers'}<span class="tab-chevron">{requestPaneCollapsed ? '▸' : '▾'}</span>{/if}
        </button>
        <button class:active={activeTab === 'body'} on:click={() => onRequestTabClick('body')}>
          Body{#if bodyTabBadge}<span class="tab-count">{bodyTabBadge}</span>{/if}
          {#if activeTab === 'body'}<span class="tab-chevron">{requestPaneCollapsed ? '▸' : '▾'}</span>{/if}
        </button>
      </div>

      {#if !requestPaneCollapsed}
      {#if activeTab === 'params'}
        <table class="kv-table">
          <thead>
            <tr><th></th><th>Key</th><th>Value</th><th></th></tr>
          </thead>
          <tbody>
            {#each draftParams as p, i}
              <tr>
                <td><input type="checkbox" bind:checked={p.enabled} /></td>
                <td><input type="text" bind:value={p.key} placeholder="param" /></td>
                <td><input type="text" bind:value={p.value} placeholder="value" /></td>
                <td><button class="icon-btn kv-remove-btn" on:click={() => removeRequestParam(i)}>×</button></td>
              </tr>
            {/each}
          </tbody>
        </table>
        <button on:click={() => addRequestParam()}>Add param</button>
      {:else if activeTab === 'headers'}
        <!-- Shared suggestion list of common header names (from the
             backend catalog / headers.yaml). Attached to every key input
             via list="fm-header-names". -->
        <datalist id="fm-header-names">
          {#each headerCatalog as entry}<option value={entry.name}></option>{/each}
        </datalist>

        <table class="kv-table">
          <thead>
            <tr><th></th><th>Key</th><th>Value</th><th></th></tr>
          </thead>
          <tbody>
            {#each draftHeaders as h, i}
              <tr>
                <td><input type="checkbox" bind:checked={h.enabled} /></td>
                <td><input type="text" list="fm-header-names" bind:value={h.key} placeholder="Header-Name" /></td>
                <td>
                  <input
                    type="text"
                    list={headerValues(h.key).length ? `fm-header-values-${i}` : undefined}
                    bind:value={h.value}
                    placeholder="value"
                  />
                  {#if headerValues(h.key).length}
                    <datalist id={`fm-header-values-${i}`}>
                      {#each headerValues(h.key) as v}<option value={v}></option>{/each}
                    </datalist>
                  {/if}
                </td>
                <td><button class="icon-btn kv-remove-btn" on:click={() => removeRequestHeader(i)}>×</button></td>
              </tr>
            {/each}
          </tbody>
        </table>
        <button on:click={() => addRequestHeader()}>Add header</button>
      {:else}
        <div class="body-mode-picker">
          {#each bodyModes as m}
            <label class="body-mode-option">
              <input type="radio" name="body-mode" value={m.value} bind:group={draftBodyMode} />
              {m.label}
            </label>
          {/each}
        </div>

        {#if draftBodyMode === 'raw'}
          <textarea class="body-editor" bind:value={draftBodyRaw} placeholder="Raw request body"></textarea>
        {:else if draftBodyMode === 'form-data' || draftBodyMode === 'x-www-form-urlencoded'}
          <table class="kv-table">
            <thead>
              <tr>
                <th></th>
                <th>Key</th>
                {#if draftBodyMode === 'form-data'}<th class="form-field-type-col">Type</th>{/if}
                <th>Value</th>
                <th></th>
              </tr>
            </thead>
            <tbody>
              {#each draftFormFields as f, i}
                <tr>
                  <td><input type="checkbox" bind:checked={f.enabled} /></td>
                  <td><input type="text" bind:value={f.key} placeholder="key" /></td>
                  {#if draftBodyMode === 'form-data'}
                    <td>
                      <select bind:value={f.type}>
                        <option value="text">text</option>
                        <option value="file">file</option>
                      </select>
                    </td>
                  {/if}
                  <td>
                    {#if draftBodyMode === 'form-data' && f.type === 'file'}
                      <div class="file-field">
                        <input type="text" bind:value={f.filePath} placeholder="path to file" />
                        <button on:click={() => pickRequestFormFieldFile(i)}>Browse…</button>
                      </div>
                    {:else}
                      <input type="text" bind:value={f.value} placeholder="value" />
                    {/if}
                  </td>
                  <td><button class="icon-btn kv-remove-btn" on:click={() => removeRequestFormField(i)}>×</button></td>
                </tr>
              {/each}
            </tbody>
          </table>
          <button on:click={() => addRequestFormField()}>Add field</button>
        {:else if draftBodyMode === 'binary'}
          <div class="file-field">
            <input type="text" bind:value={draftBinaryFilePath} placeholder="Path to file — sent as the entire body" />
            <button on:click={pickBinaryFile}>Browse…</button>
          </div>
        {:else}
          <p class="muted">No body.</p>
        {/if}
      {/if}
      {/if}

      <section class="response">
        {#if sendError}
          <p class="error">{sendError}</p>
        {:else if response}
          <div class="response-header">
            <div class="response-meta">
              <div class="response-stat">
                <span class="response-stat-label">Status</span>
                <span class="status status-{statusTone(response.statusCode)}">
                  {response.statusCode} {reasonPhrase(response.status)}
                </span>
              </div>
              <div class="response-stat">
                <span class="response-stat-label">Time</span>
                <span>{formatDuration(response.durationNs)}</span>
              </div>
              <div class="response-stat">
                <span class="response-stat-label">Size</span>
                <span>{response.sizeBytes} bytes</span>
              </div>
              <div class="response-stat">
                <span class="response-stat-label">Type</span>
                <span>{formattedResponse.kind.toUpperCase()}</span>
              </div>
            </div>
            <div class="response-header-actions">
              <div class="response-segmented">
                <button class:active={responseTab === 'body'} on:click={() => setResponseTab('body')}>Body</button>
                <button class:active={responseTab === 'headers'} on:click={() => setResponseTab('headers')}>
                  Headers{#if responseHeaderRows.length}<span class="tab-count">{responseHeaderRows.length}</span>{/if}
                </button>
              </div>
              {#if responseTab === 'body' && formattedResponse.canPretty}
                <div class="response-segmented">
                  <button class:active={responseView === 'pretty'} on:click={() => setResponseView('pretty')}>Pretty</button>
                  <button class:active={responseView === 'raw'} on:click={() => setResponseView('raw')}>Raw</button>
                </div>
              {/if}
              <div class="response-actions-menu">
              <button class="icon-btn" title="Response actions" on:click={toggleResponseActionsMenu}>⋯</button>
              {#if showResponseActionsMenu}
                <!-- svelte-ignore a11y-no-static-element-interactions -->
                <!-- svelte-ignore a11y-click-events-have-key-events -->
                <div class="menu-backdrop" on:click={() => (showResponseActionsMenu = false)}></div>
                <div class="dropdown-menu">
                  <button on:click={openResponseCacheExternally}>Open in external editor</button>
                  <button on:click={copyResponseCachePath}>Copy path</button>
                  <button on:click={openResponseCacheInFileExplorer}>Open in File Explorer</button>
                  <button on:click={clearCachedResponse}>Clear cached response</button>
                </div>
              {/if}
              </div>
            </div>
          </div>
          {#if responseTab === 'headers'}
            <div class="response-headers">
              {#if responseHeaderRows.length}
                <table class="response-headers-table">
                  <tbody>
                    {#each responseHeaderRows as h}
                      <tr><th>{h.name}</th><td>{h.value}</td></tr>
                    {/each}
                  </tbody>
                </table>
              {:else}
                <p class="muted">No response headers.</p>
              {/if}
            </div>
          {:else if response.truncated}
            <div class="response-truncated">
              <p>
                Response body is {formatBytes(response.sizeBytes)} — too large to show here.
              </p>
              <div class="response-truncated-actions">
                <button on:click={openResponseExternally}>Open in external editor</button>
                <button on:click={copyResponsePath}>Copy path</button>
                <button on:click={openResponseInFileExplorer}>Open in File Explorer</button>
              </div>
            </div>
          {:else if formattedResponse.kind === 'image'}
            {#if responseImageUri}
              <div class="response-image"><img src={responseImageUri} alt="Response body" /></div>
            {:else}
              <p class="muted">Loading image…</p>
            {/if}
          {:else if formattedResponse.canPretty && responseView === 'pretty'}
            <pre class="response-body">{@html formattedResponse.html}</pre>
          {:else}
            <pre class="response-body">{response.body}</pre>
          {/if}
        {:else}
          <p class="muted">Send a request to see the response here.</p>
        {/if}
      </section>
    </main>
  </div>
{/if}

  {#if showControlApiLog}
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <div
      class="splitter splitter-horizontal"
      class:active={draggingSplitter === 'statusBar'}
      role="separator"
      aria-orientation="horizontal"
      aria-label="Resize the control API log"
      on:pointerdown={() => onSplitterPointerDown('statusBar')}
    ></div>
  {/if}
  <footer class="status-bar" style={showControlApiLog ? `height: ${statusBarHeight}px` : ''}>
    <div class="status-bar-header">
      <button
        class="icon-btn"
        title={showControlApiLog ? 'Collapse the log' : 'Expand the log'}
        on:click={toggleControlApiLog}>{showControlApiLog ? '▾' : '▸'}</button
      >
      <span>
        {#if controlApiAddr}
          Control API: <code>http://{controlApiAddr}</code>
        {:else}
          Control API: desktop build only
        {/if}
      </span>
      <span class="status-bar-actions">
        <button class="icon-btn" title="Settings" on:click={() => (showSettings = true)}>⚙</button>
        <button class="icon-btn" title="API help" on:click={() => (showHelp = true)}>?</button>
      </span>
    </div>
    {#if showControlApiLog}
    <div class="status-log" bind:this={statusLogEl}>
      {#if statusLog.length === 0}
        <div class="status-line muted">No control-API events yet.</div>
      {:else}
        {#each statusLog as entry}
          <div class="status-line"><span class="status-time">{entry.time}</span>{entry.text}</div>
        {/each}
      {/if}
    </div>
    {/if}
  </footer>

  {#if showSettings && workspace}
    <div
      class="modal-backdrop"
      role="presentation"
      on:click={() => (showSettings = false)}
      on:keydown={(e) => e.key === 'Escape' && (showSettings = false)}
    >
      <div
        class="modal settings-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="settings-title"
        tabindex="-1"
        on:click|stopPropagation
        on:keydown={(e) => e.key === 'Escape' && (showSettings = false)}
      >
        <div class="modal-header">
          <h2 id="settings-title">Settings</h2>
          <button class="icon-btn" title="Close" on:click={() => (showSettings = false)}>×</button>
        </div>

        <div class="tabs">
          <button class:active={settingsTab === 'workspace'} on:click={() => (settingsTab = 'workspace')}>Workspace</button>
          <button class:active={settingsTab === 'environments'} on:click={() => (settingsTab = 'environments')}
            >Environments</button
          >
        </div>

        {#if settingsTab === 'workspace'}
          <p class="prose">Collections and environments are read from this folder.</p>
          <div class="row">
            <code class="workspace-path">{workspace.root}</code>
            <button on:click={openWorkspace}>Change…</button>
          </div>
          {#if openError}<p class="error">{openError}</p>{/if}

          <p class="prose">
            Every request's last response is cached under this workspace (<code>.cache/responses</code>), so reopening it
            later shows what it last returned.
          </p>
          <div class="row">
            <button on:click={clearResponseCache}>Clear response cache</button>
            {#if responseCacheCleared}<span class="muted">Cleared.</span>{/if}
          </div>
        {:else}
          <div class="row">
            <select bind:value={environmentId} on:change={() => selectEnvironment(environmentId)}>
              {#each workspace.environments as env (env.id)}
                <option value={env.id}>{env.name}</option>
              {/each}
            </select>
          </div>

          {#if environment}
            <table class="kv-table">
              <thead>
                <tr><th></th><th>Key</th><th>Value</th><th>Secret</th><th></th></tr>
              </thead>
              <tbody>
                {#each environment.variables as v, i}
                  <tr>
                    <td><input type="checkbox" bind:checked={v.enabled} /></td>
                    <td><input type="text" bind:value={v.key} placeholder="key" /></td>
                    <td><input type="text" bind:value={v.value} placeholder="value" /></td>
                    <td><input type="checkbox" bind:checked={v.secret} /></td>
                    <td><button class="icon-btn kv-remove-btn" on:click={() => removeEnvironmentVariable(i)}>×</button></td>
                  </tr>
                {/each}
              </tbody>
            </table>
            <div class="row">
              <button on:click={() => addEnvironmentVariable()}>Add variable</button>
              <button class="primary" on:click={saveEnvironment}>Save environment</button>
            </div>
          {/if}
        {/if}
      </div>
    </div>
  {/if}

  {#if showHelp}
    <div
      class="modal-backdrop"
      role="presentation"
      on:click={() => (showHelp = false)}
      on:keydown={(e) => e.key === 'Escape' && (showHelp = false)}
    >
      <div
        class="modal help-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="help-title"
        tabindex="-1"
        on:click|stopPropagation
        on:keydown={(e) => e.key === 'Escape' && (showHelp = false)}
      >
        <div class="modal-header">
          <h2 id="help-title">Control API</h2>
          <button class="icon-btn" title="Close" on:click={() => (showHelp = false)}>×</button>
        </div>
        <p class="hint prose">
          {#if controlApiAddr}
            Base URL: <code>http://{controlApiAddr}</code> — no auth, loopback-only.
          {:else}
            Only available in the desktop build.
          {/if}
        </p>

        <h3>Endpoints</h3>
        <div class="help-list">
          {#each apiEndpoints as e}
            <details class="help-entry">
              <summary>
                <span class="help-method" style="color: {methodColor(e.method)}">{e.method}</span>
                <code>{e.path}</code>
              </summary>
              <p class="prose help-desc">{e.desc}</p>
            </details>
          {/each}
        </div>

        <h3>UI actions (via POST /api/ui/action)</h3>
        <div class="help-list">
          {#each uiActions as a}
            <details class="help-entry">
              <summary>
                <code>{a.action}</code>
                <span class="help-payload">{a.payload}</span>
              </summary>
              <p class="prose help-desc">{a.desc}</p>
            </details>
          {/each}
        </div>
      </div>
    </div>
  {/if}
</div>

<style>
  :global(body) {
    overflow: hidden;
    -webkit-font-smoothing: antialiased;
  }

  /* The one place this monospace-forward interface switches to a
     proportional face — actual sentences, not labels/data. See
     style.css's font-face rules. */
  .prose {
    font-family: 'IBM Plex Sans', -apple-system, 'Segoe UI', sans-serif;
    line-height: 1.55;
  }

  :focus-visible {
    outline: 2px solid var(--fm-accent);
    outline-offset: 1px;
  }

  @media (prefers-reduced-motion: reduce) {
    * {
      transition: none !important;
    }
  }

  .app-shell {
    display: flex;
    flex-direction: column;
    height: 100vh;
  }

  /* While a splitter is being dragged: lock the cursor and stop text
     selection everywhere, not just under the pointer — a fast drag
     easily outruns the splitter's own hit area otherwise. */
  .app-shell.is-resizing {
    cursor: grabbing;
    user-select: none;
  }

  /* A hairline at rest (the same border-subtle token every other
     divider in this app already uses) that picks up the accent color
     under the pointer — discoverable without announcing itself as a
     draggable widget the way a thick gray bar would. The pseudo-element
     widens the actual hit area a few px past what's visible, so grabbing
     it doesn't require pixel-perfect aim. */
  .splitter {
    flex: none;
    position: relative;
    background: var(--fm-border-subtle);
    transition: background-color 0.12s ease;
  }

  .splitter:hover,
  .splitter.active {
    background: var(--fm-accent);
  }

  .splitter-vertical {
    width: 1px;
    cursor: col-resize;
  }

  .splitter-vertical::before {
    content: '';
    position: absolute;
    inset: 0 -3px;
  }

  .splitter-horizontal {
    height: 1px;
    cursor: row-resize;
  }

  .splitter-horizontal::before {
    content: '';
    position: absolute;
    inset: -3px 0;
  }

  .welcome {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 1rem;
  }

  .welcome h1 {
    font-size: 1.75rem;
    font-weight: 600;
    letter-spacing: -0.01em;
    margin: 0;
  }

  .welcome .prose {
    max-width: 42ch;
    text-align: center;
    color: var(--fm-text-muted);
    margin: 0;
  }

  .layout {
    flex: 1;
    min-height: 0;
    display: flex;
  }

  /* No fixed height here — set inline from statusBarHeight while the log
     is expanded (via the splitter above it); collapsed, it's left unset
     so the footer just shrinks to fit .status-bar-header alone. */
  .status-bar {
    flex: none;
    display: flex;
    flex-direction: column;
    border-top: 1px solid var(--fm-border);
    background: var(--fm-bg-panel);
    text-align: left;
  }

  .status-bar-header {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 4px 10px;
    font-size: 0.75rem;
    color: var(--fm-text-muted);
    border-bottom: 1px solid var(--fm-border-subtle);
  }

  .status-bar-actions {
    display: flex;
    gap: 0.25rem;
  }

  .status-bar-header code {
    color: var(--fm-text);
  }

  .status-log {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 4px 10px;
    font-family: 'IBM Plex Mono', 'Cascadia Code', Consolas, monospace;
    font-size: 0.72rem;
  }

  .status-line {
    padding: 1px 0;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .status-line.muted {
    color: var(--fm-text-muted);
  }

  .status-time {
    color: var(--fm-text-muted);
    opacity: 0.8;
    margin-right: 8px;
  }

  .modal-backdrop {
    position: fixed;
    inset: 0;
    background: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 10;
  }

  .modal {
    width: min(720px, 90vw);
    max-height: 80vh;
    overflow-y: auto;
    background: var(--fm-bg);
    border: 1px solid var(--fm-border);
    border-radius: var(--fm-radius-lg);
    padding: 1rem 1.25rem;
    text-align: left;
  }

  /* Wider (more room for Path/Description before either wraps) and a
     taller cap — with the two reference tables below sized to their own
     content instead of fighting the layout, this fits without scrolling
     at any normal window size; overflow-y stays as a safety net only,
     not the expected outcome. */
  .help-modal {
    width: min(960px, 94vw);
    max-height: 94vh;
  }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 0.5rem;
  }

  .modal-header h2 {
    margin: 0;
    font-size: 1.05rem;
    font-weight: 600;
  }

  /* A modal's action/control row (the settings window's workspace-path +
     Change…, environment select, and the Environments tab's Add/Save
     variable buttons). */
  .modal .row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin-top: 0.75rem;
  }

  .workspace-path {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .modal .row select {
    flex: 1;
  }

  /* Sentence case, not tracked-out caps — weight and color carry the
     hierarchy instead. */
  .modal h3 {
    font-size: 0.85rem;
    font-weight: 600;
    color: var(--fm-text-muted);
    margin: 1rem 0 0.5rem;
  }

  .modal table {
    font-size: 0.8rem;
  }

  .modal td,
  .modal th {
    padding: 4px 8px 4px 0;
    text-align: left;
    vertical-align: top;
    border-bottom: 1px solid var(--fm-border-subtle);
  }

  /* The help modal's endpoint / ui:action reference: each row is a
     collapsed <details> showing just the method+path (or action+payload)
     — the prose description is revealed on click, so the whole list
     scans at a glance first. */
  .help-list {
    border: 1px solid var(--fm-border-subtle);
    border-radius: var(--fm-radius);
    overflow: hidden;
  }

  .help-entry + .help-entry {
    border-top: 1px solid var(--fm-border-subtle);
  }

  .help-entry > summary {
    display: flex;
    align-items: baseline;
    gap: 0.6rem;
    padding: 0.4rem 0.6rem;
    font-size: 0.8rem;
    cursor: pointer;
    user-select: none;
    list-style: none;
  }

  .help-entry > summary::-webkit-details-marker {
    display: none;
  }

  /* Same ▸/▾ collapse glyph the tabs and the log panel use. */
  .help-entry > summary::before {
    content: '▸';
    color: var(--fm-text-muted);
    font-size: 0.7em;
  }

  .help-entry[open] > summary::before {
    content: '▾';
  }

  .help-entry > summary:hover {
    background: var(--fm-bg-hover);
  }

  .help-method {
    font-weight: 600;
    min-width: 3.25rem;
  }

  .help-payload {
    color: var(--fm-text-muted);
    overflow-wrap: anywhere;
  }

  .help-desc {
    margin: 0;
    padding: 0.1rem 0.6rem 0.55rem 1.85rem;
    font-size: 0.8rem;
    color: var(--fm-text-muted);
  }

  code {
    font-family: 'IBM Plex Mono', 'Cascadia Code', Consolas, monospace;
    background: var(--fm-bg-elevated);
    padding: 1px 5px;
    border-radius: var(--fm-radius);
  }

  /* No width here — set inline from sidebarWidth (see the splitter next
     to it). */
  .sidebar {
    flex-shrink: 0;
    background: var(--fm-bg-panel);
    display: flex;
    flex-direction: column;
    text-align: left;
  }

  .sidebar-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 0.75rem 1rem;
    font-weight: 600;
    border-bottom: 1px solid var(--fm-border-subtle);
  }

  .request-list {
    list-style: none;
    margin: 0;
    padding: 0;
    flex: 1;
    overflow-y: auto;
  }

  /* --m (set inline per-row from methodColor()) reads the same hue in
     both the left accent bar and the method tag text — one method, one
     color, everywhere it's shown. */
  .request-list li {
    display: flex;
    align-items: center;
    border-left: 2px solid var(--m, transparent);
  }

  .request-list li:hover {
    background: var(--fm-bg-hover);
  }

  /* .request-select: the row's main click target (selects the request).
     A sibling icon-button handles delete — kept out of this button since
     a <button> can't nest another <button>. */
  .request-list li .request-select {
    flex: 1;
    min-width: 0;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    color: inherit;
    padding: 0.5rem 1rem;
    cursor: pointer;
    display: flex;
    gap: 0.5rem;
    align-items: center;
  }

  .request-list li.active .request-select {
    background: var(--fm-bg-hover);
  }

  .request-select-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .request-list li .icon-btn {
    flex-shrink: 0;
    margin-right: 0.5rem;
  }

  .method-tag {
    font-size: 0.7rem;
    font-weight: 600;
    color: var(--m, var(--fm-text-muted));
    width: 3.5rem;
    flex-shrink: 0;
  }

  .editor {
    flex: 1;
    padding: 1rem 1.5rem;
    overflow-y: auto;
    text-align: left;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  .request-name input {
    font-size: 1.15rem;
    font-weight: 600;
    letter-spacing: -0.01em;
    background: none;
    border: none;
    color: inherit;
    width: 100%;
  }

  .url-bar {
    display: flex;
    gap: 0.5rem;
  }

  .url-bar input[type='text'] {
    flex: 1;
  }

  /* --m (set inline from methodColor()) makes the currently selected
     method legible before you've even read the letters — the same
     device as the sidebar's left accent bar. */
  .method-select {
    font-weight: 600;
    color: var(--m);
    border-left: 2px solid var(--m);
  }

  .settings-modal .tabs {
    margin-bottom: 0.75rem;
  }

  .tabs {
    display: flex;
    gap: 0.25rem;
    border-bottom: 1px solid var(--fm-border-subtle);
  }

  .tabs button {
    background: none;
    border: none;
    border-radius: 0;
    color: var(--fm-text-muted);
    padding: 0.5rem 0.75rem;
    cursor: pointer;
    transition: color 0.12s ease;
  }

  .tabs button:hover {
    color: var(--fm-text);
  }

  .tabs button.active {
    color: var(--fm-text);
    border-bottom: 2px solid var(--fm-accent);
  }

  /* Same glyph, same meaning, as the control-API log's own collapse
     toggle — one collapse language reused rather than two. Shown only
     on the active tab: it's what clicking that tab again will do. */
  .tab-chevron {
    margin-left: 0.3em;
    color: var(--fm-text-muted);
  }

  /* How many rows a tab holds, so the ones you're not looking at still
     say whether there's anything in them. */
  .tab-count {
    margin-left: 0.4em;
    padding: 0.05em 0.4em;
    font-size: 0.75em;
    border-radius: 999px;
    background: var(--fm-bg-hover);
    color: var(--fm-text-muted);
  }

  .tabs button.active .tab-count {
    color: var(--fm-text);
  }

  table {
    width: 100%;
    border-collapse: collapse;
  }

  /* .kv-table: the editable key/value grids (env variables, request
     headers) — fixed layout with narrow checkbox/remove-button columns,
     so wide text inputs can't force the layout and overlap each other.
     Scoped to this class rather than a bare `table` selector so it
     doesn't also squash the help modal's plain reference tables, which
     want auto layout with a wide Description column. */
  .kv-table {
    table-layout: fixed;
  }

  .kv-table td,
  .kv-table th {
    padding: 4px 6px;
  }

  .kv-table td:first-child,
  .kv-table th:first-child {
    width: 2.25rem;
    padding-left: 0;
  }

  .kv-table td:last-child,
  .kv-table th:last-child {
    width: 2.25rem;
    padding-right: 0;
  }

  /* The Environments tab's table has a second narrow (checkbox) column —
     Secret — that isn't first or last, so it needs its own rule or it'd
     claim an even share of the remaining width like Key/Value do. */
  .settings-modal .kv-table th:nth-child(4),
  .settings-modal .kv-table td:nth-child(4) {
    width: 4.5rem;
  }

  .kv-table input[type='text'] {
    width: 100%;
  }

  /* Every "enabled"-style checkbox (header/param/form-field/variable rows)
     replaces the OS default with a box built from the same tokens as the
     text input next to it — border, background, radius — sized close to
     that input's own rendered height (see .kv-table's first/last column
     widths, bumped to fit) rather than the native checkbox's small fixed
     size floating in the middle of a much taller row. */
  input[type='checkbox'] {
    appearance: none;
    -webkit-appearance: none;
    width: 1.75rem;
    height: 1.75rem;
    padding: 0;
    margin: 0;
    display: inline-grid;
    place-content: center;
    cursor: pointer;
  }

  input[type='checkbox']:checked {
    background: var(--fm-accent);
    border-color: var(--fm-accent);
  }

  input[type='checkbox']:checked:hover {
    background: color-mix(in srgb, var(--fm-accent) 85%, white);
    border-color: color-mix(in srgb, var(--fm-accent) 85%, white);
  }

  input[type='checkbox']:checked::after {
    content: '';
    width: 0.4rem;
    height: 0.75rem;
    border: solid var(--fm-bg);
    border-width: 0 2px 2px 0;
    transform: rotate(45deg) translate(-1px, -2px);
  }

  /* The X that removes one row from a .kv-table (a header/param/
     form-field/variable) — sized to match the checkbox in the same row
     rather than .icon-btn's smaller default, and tinted toward
     --fm-error on hover so the row it's about to remove is unambiguous.
     A different class from the sidebar's delete/dialog-close ×s, which
     stay at .icon-btn's own size — those close/delete a whole
     request/dialog, not one row in a table. */
  .kv-remove-btn {
    width: 1.75rem;
    height: 1.75rem;
    padding: 0;
    font-size: 1rem;
    line-height: 1;
    color: var(--fm-text-muted);
  }

  .kv-remove-btn:hover {
    color: var(--fm-error);
    background: color-mix(in srgb, var(--fm-error) 14%, transparent);
  }

  .body-editor {
    width: 100%;
    min-height: 140px;
    font-family: 'IBM Plex Mono', 'Cascadia Code', Consolas, monospace;
    resize: vertical;
  }

  .body-mode-picker {
    display: flex;
    gap: 1rem;
  }

  .body-mode-option {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    font-size: 0.8rem;
    font-family: 'IBM Plex Mono', 'Cascadia Code', Consolas, monospace;
    color: var(--fm-text-muted);
    cursor: pointer;
  }

  .body-mode-option:has(input:checked) {
    color: var(--fm-text);
  }

  .body-mode-option input[type='radio'] {
    width: auto;
    padding: 0;
    accent-color: var(--fm-accent);
  }

  /* The form-data table's Type column (text/file) is narrow and doesn't
     need to share the Key/Value split evenly, same reasoning as the
     first/last narrow columns in .kv-table. table-layout: fixed means
     declaring the width once on the header cell is enough for the whole
     column. */
  .form-field-type-col {
    width: 6rem;
  }

  .file-field {
    display: flex;
    gap: 0.5rem;
  }

  .file-field input {
    flex: 1;
    min-width: 0;
  }

  .file-field button {
    flex-shrink: 0;
  }

  .response {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
    border-top: 1px solid var(--fm-border-subtle);
    padding-top: 0.75rem;
  }

  .response-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
  }

  .response-meta {
    display: flex;
    gap: 1.5rem;
    font-size: 0.85rem;
    margin-bottom: 0.5rem;
  }

  .response-header-actions {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-shrink: 0;
  }

  .response-segmented {
    display: flex;
  }

  .response-segmented button {
    padding: 0.15rem 0.5rem;
    font-size: 0.8rem;
    border-radius: 0;
    background: none;
    color: var(--fm-text-muted);
  }

  .response-segmented button:first-child {
    border-radius: var(--fm-radius) 0 0 var(--fm-radius);
  }

  .response-segmented button:last-child {
    border-radius: 0 var(--fm-radius) var(--fm-radius) 0;
    border-left: none;
  }

  .response-segmented button.active {
    color: var(--fm-text);
    background: var(--fm-bg-hover);
  }

  .response-headers {
    flex: 1;
    overflow: auto;
    background: var(--fm-bg-response);
    padding: 0.75rem;
  }

  .response-headers-table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.85rem;
  }

  .response-headers-table th,
  .response-headers-table td {
    text-align: left;
    vertical-align: top;
    padding: 0.2rem 0.75rem 0.2rem 0;
    font-weight: normal;
    word-break: break-word;
  }

  .response-headers-table th {
    white-space: nowrap;
    color: var(--fm-text-muted);
  }

  .response-actions-menu {
    position: relative;
  }

  .menu-backdrop {
    position: fixed;
    inset: 0;
    z-index: 5;
  }

  .dropdown-menu {
    position: absolute;
    top: 100%;
    right: 0;
    z-index: 10;
    display: flex;
    flex-direction: column;
    min-width: 12rem;
    overflow: hidden;
    /* Opaque fill so the menu doesn't read as part of the response body
       it floats over: the base colour with the elevated overlay painted
       on top of it, a touch lighter than the page. */
    background-color: var(--fm-bg);
    background-image: linear-gradient(var(--fm-bg-elevated), var(--fm-bg-elevated));
    border: 1px solid var(--fm-border);
    border-radius: var(--fm-radius);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.3);
  }

  .dropdown-menu button {
    justify-content: flex-start;
    background: none;
    border: none;
    border-radius: 0;
    padding: 0.5rem 0.75rem;
    text-align: left;
  }

  .dropdown-menu button + button {
    border-top: 1px solid var(--fm-border-subtle);
  }

  .dropdown-menu button:hover {
    background: var(--fm-bg-hover);
  }

  .response-stat {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
  }

  .response-stat-label {
    font-size: 0.7rem;
    font-weight: 600;
    color: var(--fm-text-muted);
  }

  .status {
    font-size: 1.05rem;
    font-weight: 600;
    letter-spacing: -0.01em;
  }

  /* Standard HTTP status-class coloring — see statusTone() in the
     script block. */
  .status-success {
    color: var(--fm-success);
  }

  .status-info {
    color: var(--fm-accent);
  }

  .status-warning {
    color: var(--fm-warning);
  }

  .status-error {
    color: var(--fm-error);
  }

  .response-body {
    flex: 1;
    overflow: auto;
    background: var(--fm-bg-response);
    padding: 0.75rem;
    margin: 0;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .response-image {
    flex: 1;
    overflow: auto;
    background: var(--fm-bg-response);
    padding: 0.75rem;
  }

  .response-image img {
    max-width: 100%;
    /* Checkerboard so a transparent PNG's edges are visible against the
       dark response ground. */
    background-image: linear-gradient(45deg, rgba(255, 255, 255, 0.06) 25%, transparent 25%),
      linear-gradient(-45deg, rgba(255, 255, 255, 0.06) 25%, transparent 25%),
      linear-gradient(45deg, transparent 75%, rgba(255, 255, 255, 0.06) 75%),
      linear-gradient(-45deg, transparent 75%, rgba(255, 255, 255, 0.06) 75%);
    background-size: 16px 16px;
    background-position: 0 0, 0 8px, 8px -8px, -8px 0;
  }

  /* JSON syntax colors — deliberately the same hues the rest of the app
     already uses (the accent for keys, the method/status palette for
     values) so a highlighted body reads as part of this UI, not a
     dropped-in editor theme. :global because the spans come from
     {@html}. */
  .response-body :global(.syntax-key) {
    color: var(--fm-accent);
  }

  .response-body :global(.syntax-str) {
    color: var(--fm-success);
  }

  .response-body :global(.syntax-num) {
    color: var(--fm-method-get);
  }

  .response-body :global(.syntax-bool) {
    color: var(--fm-method-put);
  }

  .response-body :global(.syntax-null) {
    color: var(--fm-text-muted);
  }

  .response-truncated {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    justify-content: center;
    gap: 0.75rem;
    background: var(--fm-bg-response);
    padding: 1.5rem;
  }

  .response-truncated-actions {
    display: flex;
    gap: 0.5rem;
  }

  .muted {
    opacity: 0.6;
  }

  .error {
    color: var(--fm-error);
  }

  input,
  select,
  textarea,
  button {
    box-sizing: border-box;
    font-family: inherit;
    font-size: 0.9rem;
    color: inherit;
    background: var(--fm-bg-elevated);
    border: 1px solid var(--fm-border);
    border-radius: var(--fm-radius);
    padding: 0.4rem 0.5rem;
    transition: background-color 0.12s ease, border-color 0.12s ease;
  }

  input:hover,
  select:hover,
  textarea:hover,
  button:hover {
    border-color: var(--fm-text-muted);
  }

  button {
    cursor: pointer;
  }

  button:hover {
    background: var(--fm-bg-hover);
  }

  button:active {
    background: var(--fm-bg-hover);
    transform: translateY(1px);
  }

  button.primary {
    background: var(--fm-accent);
    border-color: var(--fm-accent);
    color: var(--fm-bg);
    font-weight: 600;
  }

  button.primary:hover {
    background: color-mix(in srgb, var(--fm-accent) 85%, white);
    border-color: color-mix(in srgb, var(--fm-accent) 85%, white);
  }

  button:disabled {
    cursor: not-allowed;
    opacity: 0.6;
  }

  button:disabled:hover {
    background: var(--fm-bg-elevated);
  }

  button.primary:disabled:hover {
    background: var(--fm-accent);
  }

  .icon-btn {
    background: none;
    border: none;
    border-radius: var(--fm-radius);
    padding: 0.1rem 0.4rem;
  }

  .icon-btn:hover {
    background: var(--fm-bg-hover);
    border-color: transparent;
  }
</style>
