<script lang="ts">
  import { onMount } from 'svelte'
  import {
    CurrentWorkspace,
    OpenWorkspace,
    SelectWorkspaceFolder,
    GetCollection,
    CreateCollection,
    RenameCollection,
    DeleteCollection,
    SaveRequest,
    DeleteRequest,
    GetEnvironment,
    SaveEnvironment,
    DeleteEnvironment,
    ExecuteRequest,
    GenerateRequestCode,
    GetVersion,
    GetSettings,
    SaveSettings,
    GetCachedResponse,
    ClearResponseCache,
    ClearCachedResponse,
    OpenResponseCacheExternally,
    GetResponseCachePath,
    GetResponseCacheDataURI,
    OpenResponseCacheInFileExplorer,
  } from '$backend'
  import type { domain, httpengine, core, settings } from '../wailsjs/go/models'
  import { EventsOn } from '../wailsjs/runtime/runtime'
  import HelpModal from './components/HelpModal.svelte'
  import RequestList from './components/RequestList.svelte'
  import StatusBar from './components/StatusBar.svelte'
  import RequestEditor from './components/RequestEditor.svelte'
  import ResponsePane from './components/ResponsePane.svelte'
  import SettingsModal from './components/SettingsModal.svelte'
  import Switcher from './components/Switcher.svelte'
  import { formatBytes, formatDuration, methodColor, reasonPhrase, statusTone } from './lib/format'
  import { detectResponseKind, formatResponse } from './lib/responseFormat'
  import type { BodyLanguage, ResponseView } from './lib/responseFormat'
  import {
    bodyModes,
    changedOptionCount,
    codeFormats,
    defaultOptions,
    emptyAuth,
    emptyDraft,
    methods,
  } from './lib/requestDraft'
  import type { AuthType, BodyMode, CodeFormat, FormFieldType, RequestDraft, RequestTab } from './lib/requestDraft'

  // The request currently in the editor, before it's saved. RequestEditor
  // takes it as one bound prop, and reportUIState spreads it — its keys
  // are deliberately the names GET /api/ui/state reports and
  // setRequestField accepts, so the mirror can't drift from what it
  // mirrors. See lib/requestDraft.ts.
  let draft: RequestDraft = emptyDraft()
  import {
    ControlAPIAddr,
    GetHeaderCatalog,
    SelectFile,
    ReportUIState,
    ReportControlAPIDocs,
  } from '../wailsjs/go/wailsapp/App.js'
  import { apiEndpoints, uiActions } from './lib/controlApiCatalog'

  // Common request headers (and, per header, common values) offered as
  // autocomplete in the header editor. Loaded from the desktop backend on
  // mount (see internal/wailsapp.GetHeaderCatalog / internal/headercatalog,
  // configurable via headers.yaml); stays [] in the web build, which just
  // means no suggestions.
  type HeaderCatalogEntry = { name: string; values?: string[] }
  let headerCatalog: HeaderCatalogEntry[] = []

  let workspace: core.WorkspaceInfo | null = null
  let openError = ''

  // Which build this is — shown on the welcome screen and in the help
  // modal, and reported by GET /api/version. Loaded once on mount; it
  // can't change while the app runs.
  let appVersion = ''

  // The workspace's app-wide defaults (internal/settings): how long a
  // request may take, how many redirects it follows, and the two
  // response-size limits. Loaded when a workspace opens, edited in the
  // settings window, and the first two seed every new request's Options.
  let workspaceSettings: settings.Settings = {
    requestTimeoutMs: 30_000,
    maxRedirects: 10,
    inlineResponseBytes: 1 << 20,
    maxResponseBytes: 64 << 20,
  }
  $: workspaceDefaults = {
    maxRedirects: workspaceSettings.maxRedirects,
    requestTimeoutMs: workspaceSettings.requestTimeoutMs,
  }

  let collectionId = ''
  let collection: domain.Collection | null = null
  let selectedItemId: string | null = null

  // The two pickers' open state — the collection's lives above the
  // request list, the environment's in the top bar.
  let showCollectionMenu = false
  let showEnvironmentMenu = false

  let environmentId = ''
  let environment: domain.Environment | null = null
  let showSettings = false
  // The settings window's tabs: the workspace folder, and the two things
  // it holds many of. Collections and environments are managed there
  // rather than from the pickers, which only pick one — creating and
  // deleting are occasional, and don't belong a slip away from a control
  // used several times an hour.
  type SettingsTab = 'workspace' | 'collections' | 'environments'
  let settingsTab: SettingsTab = 'workspace'

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
  // 'pretty' reindents a JSON/XML body and colours it (or renders an
  // image); 'raw' shows exactly what came back, uncoloured as well as
  // unformatted; 'hex' dumps the bytes. Sticky across requests — a view
  // preference, not per-response state. See formattedResponse below for
  // the (lightweight) detection of whether pretty is even possible.
  let responseView: ResponseView = 'pretty'
  // Which of the response's two panels is showing — the body, or its
  // headers (the response's meta info the meta stats don't cover).
  // Sticky across requests, same as responseView.
  let responseTab: 'body' | 'headers' = 'body'
  // The cached response body as a data: URI (see
  // GetResponseCacheDataURI) — the bytes as they arrived, which
  // response.body can't carry across the JSON bridge. Wanted by the
  // image view, which renders it, and the hex view, which dumps it.
  // Loaded on demand rather than held for every response: it's a base64
  // copy of the whole body. null until loaded / when nothing needs it.
  let responseDataUri: string | null = null
  let activeTab: RequestTab = 'headers'
  // Collapsed by the disclosure toggle at the head of the tab row: the
  // tab content hides, the name/URL/tab rows stay, and the response
  // takes the freed height (see .request-pane.collapsed).
  let requestPaneCollapsed = false

  // The same gesture on the response's own Body/Headers switch: the meta
  // strip stays, so there's always something to click to get it back.
  let responsePaneCollapsed = false

  // The Code tab renders the draft request as a runnable command (see
  // internal/codegen). codeFormat is sticky like responseView — a view
  // preference, not per-request state. generatedCode is recomputed by the
  // backend whenever the tab is open and any input changes.
  let codeFormat: CodeFormat = 'bash'
  let generatedCode = ''
  let codeError = ''

  // How the raw-body editor colours what's being typed. Sticky like
  // codeFormat and responseView — a view preference, not part of the
  // request. 'auto' reads the Content-Type row and the first character.
  let bodyLanguage: BodyLanguage = 'auto'

  // Sidebar width, the log panel's height, and whether the log panel is
  // collapsed are pure layout comfort — remembered per-browser-profile
  // via localStorage (silently no-op if unavailable, e.g. a locked-down
  // profile) rather than round-tripped through the workspace like real
  // request/environment data.
  const LAYOUT_PREFS_KEY = 'freeman.layoutPrefs'
  function loadLayoutPrefs(): {
    sidebarWidth?: number
    statusBarHeight?: number
    responseHeight?: number
    showControlApiLog?: boolean
  } {
    try {
      return JSON.parse(localStorage.getItem(LAYOUT_PREFS_KEY) ?? '{}')
    } catch {
      return {}
    }
  }
  function saveLayoutPrefs() {
    try {
      localStorage.setItem(
        LAYOUT_PREFS_KEY,
        JSON.stringify({ sidebarWidth, statusBarHeight, responseHeight, showControlApiLog }),
      )
    } catch {
      // ignore — comfort setting only, not worth surfacing an error for
    }
  }
  const layoutPrefs = loadLayoutPrefs()
  const SIDEBAR_WIDTH_RANGE = [180, 560] as const
  const STATUS_BAR_HEIGHT_RANGE = [60, 500] as const
  const RESPONSE_HEIGHT_RANGE = [120, 1200] as const
  let sidebarWidth = clamp(layoutPrefs.sidebarWidth ?? 260, SIDEBAR_WIDTH_RANGE)
  let statusBarHeight = clamp(layoutPrefs.statusBarHeight ?? 118, STATUS_BAR_HEIGHT_RANGE)
  let responseHeight = clamp(layoutPrefs.responseHeight ?? 360, RESPONSE_HEIGHT_RANGE)
  let showControlApiLog = layoutPrefs.showControlApiLog ?? true
  function clamp(value: number, [min, max]: readonly [number, number]): number {
    return Math.min(max, Math.max(min, value))
  }

  // Drag-resize for the two splitters (sidebar | editor, and above the
  // status bar). One pair of window-level pointer listeners handles
  // whichever splitter is currently being dragged, rather than each
  // splitter wiring its own — there's only ever one drag in flight.
  let draggingSplitter: 'sidebar' | 'statusBar' | 'response' | null = null
  // The editor column, needed to turn a pointer position into a response
  // height: the response's bottom edge is this element's bottom, not the
  // window's — the status bar sits below it.
  let editorEl: HTMLElement | undefined
  function onSplitterPointerDown(which: typeof draggingSplitter) {
    draggingSplitter = which
  }
  function onWindowPointerMove(e: PointerEvent) {
    if (draggingSplitter === 'sidebar') {
      sidebarWidth = clamp(e.clientX, SIDEBAR_WIDTH_RANGE)
    } else if (draggingSplitter === 'statusBar') {
      statusBarHeight = clamp(window.innerHeight - e.clientY, STATUS_BAR_HEIGHT_RANGE)
    } else if (draggingSplitter === 'response' && editorEl) {
      responseHeight = clamp(editorEl.getBoundingClientRect().bottom - e.clientY, RESPONSE_HEIGHT_RANGE)
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

  // Reference shown in the help modal — must match internal/httpapi's
  // registered routes (handler.go) and main.go's POST /api/ui/action, and
  // the action names handled in dispatchUIAction below. Kept as plain
  // data here rather than generated, so keep both sides in sync by hand.

  // Action names spell out their target explicitly — Request or
  // Environment — wherever one applies, rather than a bare verb (e.g.
  // addRequestHeader/addEnvironmentVariable, not addHeader/addVariable),
  // so the list reads unambiguously on its own.

  // Log of ui:action events received from the control API (see
  // internal/wailsapp.DispatchUIAction) — rendered in the status bar so
  // it's visible when an external script or agent is driving the app.
  // Regular in-app clicks don't go through this event bus, so they don't
  // appear here; capped so a long-running session doesn't grow forever.
  let statusLog: { time: string; text: string }[] = []
  let controlApiAddr = ''
  let showHelp = false

  function logEvent(text: string) {
    const time = new Date().toLocaleTimeString()
    statusLog = [...statusLog.slice(-49), { time, text }]
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
      collections: workspace?.collections ?? [],
      collectionName: collection?.name ?? '',
      showCollectionMenu,
      showEnvironmentMenu,
      collectionId,
      environmentId,
      selectedItemId,
      tab: activeTab,
      requestPaneCollapsed,
      // name/method/url/params/headers/auth/bodyMode/bodyRaw/
      // formFields/binaryFilePath — the draft's keys are the mirror's
      // keys on purpose, so this can't fall out of step with it.
      ...draft,
      codeFormat,
      code: generatedCode,
      bodyLanguage,
      environment,
      showSettings,
      settingsTab,
      // The workspace's app-wide defaults, so setWorkspaceSetting has a
      // getter to read back — same rule as every other settable field.
      workspaceSettings,
      showHelp,
      sending,
      sendError,
      response,
      responseTab,
      responseView,
      responsePaneCollapsed,
      showResponseActionsMenu,
      sidebarWidth,
      statusBarHeight,
      responseHeight,
      showControlApiLog,
    }
    // Encoded here and passed as a string, not the plain object — a
    // Wails-bound method taking an object argument from the frontend
    // didn't reliably reach the Go side in testing (ReportUIState kept
    // storing whatever was first reported, never a later update); Go
    // just writes this string straight back out for GET /api/ui/state.
    ReportUIState(JSON.stringify(state)).catch((e) => logEvent(`reportUIState failed: ${e}`))
  }

  // Reloaded whenever a workspace opens: they live in its settings.yaml,
  // so a different workspace is a different set.
  async function loadSettings() {
    try {
      workspaceSettings = await GetSettings()
    } catch {
      // No workspace open yet, or an unreadable file — the fallbacks
      // above are the same values Go would have used anyway.
    }
  }

  // Saved on every field change rather than behind a button: these are
  // four numbers, and Go clamps whatever arrives, so the stored answer
  // comes back and replaces what was typed if it was out of range.
  async function saveWorkspaceSettings() {
    workspaceSettings = await SaveSettings(workspaceSettings)
  }

  onMount(async () => {
    // Before the workspace: it needs none, and the welcome screen shows
    // it while there's nothing else on screen.
    try {
      // The version string alone: `git describe` already carries the
      // commit whenever the build isn't sitting on a tag, so showing
      // both reads as "v0.1.0-4-g94b2835 (94b2835)". GET /api/version
      // returns the commit and build date separately for a bug report.
      appVersion = (await GetVersion()).version ?? ''
    } catch {
      // A build without it is not worth an error banner.
    }

    const ws = await CurrentWorkspace()
    if (ws) await initWorkspace(ws)

    // 'runtime' only exists on window inside the Wails-hosted desktop
    // build — the web build (cmd/freeman-server) has no Wails bridge, so
    // this control-API-driven UI dispatch is desktop-only by design (see
    // internal/wailsapp.DispatchUIAction).
    if ('runtime' in window) {
      controlApiAddr = await ControlAPIAddr()
      // Hand Go the route/action catalogue so GET /api/agent can serve
      // it. Reported once, since it's a constant — unlike the UI state
      // beside it, which is re-reported on every change.
      ReportControlAPIDocs(JSON.stringify({ apiEndpoints, uiActions })).catch((e) =>
        logEvent(`reportControlAPIDocs failed: ${e}`),
      )
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
        if (tab === 'workspace' || tab === 'collections' || tab === 'environments') settingsTab = tab
        break
      }
      case 'selectEnvironment':
        if (payload?.id) await selectEnvironment(String(payload.id))
        break
      case 'newEnvironment':
        await newEnvironment()
        break
      case 'setWorkspaceSetting': {
        const field = String(payload?.field ?? '')
        const value = Number(payload?.value)
        if (field in workspaceSettings && Number.isFinite(value)) {
          workspaceSettings = { ...workspaceSettings, [field]: value }
          await saveWorkspaceSettings()
        }
        break
      }
      case 'setEnvironmentField':
        if (payload?.field === 'name' && typeof payload?.value === 'string') {
          setEnvironmentField('name', payload.value)
        }
        break
      case 'expandEnvironment':
        await expandEnvironment(payload?.id ? String(payload.id) : '')
        break
      case 'renameEnvironment': {
        const name = String(payload?.name ?? '').trim()
        const id = payload?.id ? String(payload.id) : environmentId
        if (name && id) await renameEnvironmentById(id, name)
        break
      }
      case 'deleteEnvironment': {
        const id = payload?.id ? String(payload.id) : environmentId
        if (id) await deleteEnvironment(id)
        break
      }
      case 'selectCollection':
        if (payload?.id) await selectCollection(String(payload.id))
        break
      case 'newCollection': {
        const name = String(payload?.name ?? '').trim()
        if (name) await newCollection(name)
        break
      }
      case 'renameCollection': {
        const name = String(payload?.name ?? '').trim()
        if (name) await renameCollection(name, payload?.id ? String(payload.id) : collectionId)
        break
      }
      case 'deleteCollection':
        await deleteCollection(payload?.id ? String(payload.id) : collectionId)
        break
      case 'toggleCollectionMenu':
        showCollectionMenu = !showCollectionMenu
        break
      case 'toggleEnvironmentMenu':
        showEnvironmentMenu = !showEnvironmentMenu
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
      case 'toggleResponseActionsMenu':
        toggleResponseActionsMenu()
        break
      case 'setResponseView': {
        const view = payload?.view
        if (view === 'pretty' || view === 'raw' || view === 'hex') setResponseView(view)
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
        if (
          tab === 'params' ||
          tab === 'headers' ||
          tab === 'auth' ||
          tab === 'body' ||
          tab === 'options' ||
          tab === 'code'
        ) {
          selectRequestEditorTab(tab)
        }
        break
      }
      case 'selectCodeFormat': {
        // Validated against the tab row itself, so a format added there
        // is drivable without a second list to remember.
        const f = codeFormats.find((c) => c.value === payload?.format)
        if (f) codeFormat = f.value
        break
      }
      case 'selectBodyLanguage': {
        const l = payload?.language
        if (l === 'auto' || l === 'json' || l === 'xml' || l === 'plain') bodyLanguage = l
        break
      }
      case 'copyRequestCode':
        await copyRequestCode()
        break
      case 'toggleRequestPane':
        requestPaneCollapsed = !requestPaneCollapsed
        break
      case 'toggleResponsePane':
        responsePaneCollapsed = !responsePaneCollapsed
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
      case 'setResponseHeight': {
        const px = Number(payload?.px)
        if (!Number.isNaN(px)) {
          responseHeight = clamp(px, RESPONSE_HEIGHT_RANGE)
          saveLayoutPrefs()
        }
        break
      }
      case 'setRequestOption': {
        const field = payload?.field
        if (field === 'followRedirects' || field === 'storeCookies' || field === 'skipTlsVerify') {
          if (typeof payload?.value === 'boolean') draft.options[field] = payload.value
        } else if (field === 'maxRedirects' || field === 'timeoutMs') {
          const n = Number(payload?.value)
          if (!Number.isNaN(n)) draft.options[field] = Math.max(0, Math.trunc(n))
        } else if (field === 'clientCertFile' || field === 'clientCertKeyFile') {
          if (typeof payload?.value === 'string') draft.options[field] = payload.value
        }
        draft = draft
        break
      }
      case 'setRequestField': {
        const value = payload?.value
        if (typeof value !== 'string') break
        switch (payload?.field) {
          case 'name':
            draft.name = value
            break
          case 'method':
            draft.method = value
            break
          case 'url':
            draft.url = value
            break
          case 'bodyRaw':
            draft.bodyRaw = value
            break
          case 'bodyMode':
            if (bodyModes.some((m) => m.value === value)) draft.bodyMode = value as BodyMode
            break
          case 'binaryFilePath':
            draft.binaryFilePath = value
            break
        }
        break
      }
      case 'setRequestAuth': {
        const field = payload?.field
        const value = payload?.value
        if (field === 'type') {
          if (
            value === 'none' ||
            value === 'bearer' ||
            value === 'basic' ||
            value === 'apikey' ||
            value === 'oauth2'
          ) {
            draft.auth = { ...draft.auth, type: value }
          }
        } else if (
          (field === 'token' ||
            field === 'username' ||
            field === 'password' ||
            field === 'key' ||
            field === 'value' ||
            field === 'tokenUrl' ||
            field === 'clientId' ||
            field === 'clientSecret' ||
            field === 'scope') &&
          typeof value === 'string'
        ) {
          draft.auth = { ...draft.auth, [field]: value }
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
        const index = rowIndex(draft.headers, payload)
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
        const index = rowIndex(draft.params, payload)
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
        const index = rowIndex(draft.formFields, payload)
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
    await loadSettings()
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

  // --- collections -----------------------------------------------------
  //
  // The three that change the workspace's shape rather than its contents.
  // Each refreshes the workspace afterwards, since the switcher's list
  // comes from it.

  async function newCollection(name: string) {
    const created = await CreateCollection(name)
    await refreshWorkspace()
    await selectCollection(created.id)
  }

  async function renameCollection(name: string, id = collectionId) {
    await RenameCollection(id, name)
    await refreshWorkspace()
    if (id === collectionId) collection = await GetCollection(id)
  }

  // Deleting the open collection leaves nothing selected, so it moves to
  // whichever is left rather than showing an empty sidebar for a
  // collection that no longer exists.
  async function deleteCollection(id = collectionId) {
    await DeleteCollection(id)
    const ws = await refreshWorkspace()
    if (id === collectionId && ws.collections.length) {
      await selectCollection(ws.collections[0].id)
    }
  }

  async function refreshWorkspace(): Promise<core.WorkspaceInfo> {
    workspace = await CurrentWorkspace()
    return workspace
  }

  // The switchers' "Edit …" item: open the settings window on the tab
  // that manages that kind of thing.
  function openSettingsTab(tab: SettingsTab) {
    settingsTab = tab
    showSettings = true
  }

  // A backend call from a click, with its error surfaced rather than
  // left as an unhandled rejection. The control API path reports through
  // dispatchUIAction's own handling instead.
  async function guard(action: () => Promise<void>) {
    try {
      await action()
    } catch (e) {
      openError = String(e)
    }
  }

  // Deleting asks first, the way deleting a request does — it takes every
  // request in it with it.
  // Named and counted in the prompt, because the row you clicked isn't
  // necessarily the collection you're working in — the settings list
  // deletes any of them.
  function confirmDeleteCollection(summary: core.CollectionSummary) {
    const detail = summary.itemCount
      ? ` and its ${summary.itemCount} request${summary.itemCount === 1 ? '' : 's'}`
      : ''
    if (confirm(`Delete "${summary.name}"${detail}? This can't be undone from the app.`)) {
      void guard(() => deleteCollection(summary.id))
    }
  }

  async function selectRequest(item: domain.Item) {
    selectedItemId = item.id
    draft = {
      name: item.name,
      method: item.method || 'GET',
      url: item.url || '',
      params: item.params ? item.params.map((p) => ({ ...p })) : [],
      headers: item.headers ? item.headers.map((h) => ({ ...h })) : [],
      auth: item.auth
        ? { ...emptyAuth(), ...item.auth, type: (item.auth.type as AuthType) || 'none' }
        : emptyAuth(),
      bodyMode: (item.body?.mode as BodyMode) || 'none',
      bodyRaw: item.body?.raw || '',
      // { type: 'text', filePath: '', ...f } normalizes rows saved before
      // file fields existed (omitempty means those keys are simply absent,
      // never present-but-undefined, so the defaults only apply then).
      formFields: item.body?.formFields
        ? item.body.formFields.map((f) => ({ type: 'text', filePath: '', ...f }))
        : [],
      binaryFilePath: item.body?.binaryFilePath || '',
      // Absent for anything saved before options existed, and for
      // anything nobody has changed — the same defaults httpengine
      // applies in that case.
      options: item.options ? { ...defaultOptions(workspaceDefaults), ...item.options } : defaultOptions(workspaceDefaults),
    }
    sendError = ''
    responseDataUri = null
    // GetCachedResponse rejects with "nothing cached yet" for a request
    // that's never been sent (the common case) just as often as for a
    // real failure — either way, falling back to a blank response pane
    // is the right outcome, not a logged error.
    try {
      response = await GetCachedResponse(item.id)
    } catch {
      response = null
    }
    await refreshResponseData()
  }

  function newRequest() {
    selectedItemId = null
    draft = emptyDraft(workspaceDefaults)
    response = null
    sendError = ''
    responseDataUri = null
  }

  function addRequestHeader(initial?: Partial<domain.Header>) {
    draft.headers = [...draft.headers, { key: '', value: '', enabled: true, ...initial }]
  }

  function setRequestHeader(index: number, fields: Partial<domain.Header>) {
    draft.headers = draft.headers.map((h, i) => (i === index ? { ...h, ...fields } : h))
  }

  function removeRequestHeader(index: number) {
    draft.headers = draft.headers.filter((_, i) => i !== index)
  }

  function addRequestParam(initial?: Partial<domain.QueryParam>) {
    draft.params = [...draft.params, { key: '', value: '', enabled: true, ...initial }]
  }

  function setRequestParam(index: number, fields: Partial<domain.QueryParam>) {
    draft.params = draft.params.map((p, i) => (i === index ? { ...p, ...fields } : p))
  }

  function removeRequestParam(index: number) {
    draft.params = draft.params.filter((_, i) => i !== index)
  }

  function addRequestFormField(initial?: Partial<domain.FormField>) {
    draft.formFields = [...draft.formFields, { key: '', value: '', enabled: true, type: 'text', filePath: '', ...initial }]
  }

  function setRequestFormField(index: number, fields: Partial<domain.FormField>) {
    draft.formFields = draft.formFields.map((f, i) => (i === index ? { ...f, ...fields } : f))
  }

  function removeRequestFormField(index: number) {
    draft.formFields = draft.formFields.filter((_, i) => i !== index)
  }

  // Handed to RequestEditor as one prop, built once (see
  // environmentActions). These stay here because dispatchUIAction drives
  // the same operations, and the file pickers are a desktop-only Wails
  // call the editor shouldn't reach for itself.
  const requestRowActions = {
    addParam: () => addRequestParam(),
    removeParam: removeRequestParam,
    addHeader: () => addRequestHeader(),
    removeHeader: removeRequestHeader,
    addFormField: () => addRequestFormField(),
    removeFormField: removeRequestFormField,
    pickFormFieldFile: pickRequestFormFieldFile,
    pickBinaryFile,
    pickClientCert,
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
    if (path) draft.binaryFilePath = path
  }

  async function pickClientCert(which: 'cert' | 'key') {
    const path = await SelectFile()
    if (!path) return
    if (which === 'cert') draft.options.clientCertFile = path
    else draft.options.clientCertKeyFile = path
    draft = draft
  }

  // The draft editor state as a domain.Item — shared by saveRequest and
  // the Code tab's generator so both see exactly the same request.
  function buildDraftItem(): domain.Item {
    const isFormMode = draft.bodyMode === 'form-data' || draft.bodyMode === 'x-www-form-urlencoded'
    return {
      id: selectedItemId ?? '',
      type: 'request',
      name: draft.name || 'Untitled Request',
      method: draft.method,
      url: draft.url,
      params: draft.params,
      headers: draft.headers,
      auth: draft.auth.type === 'none' ? undefined : { ...draft.auth },
      body: {
        mode: draft.bodyMode,
        raw: draft.bodyMode === 'raw' ? draft.bodyRaw : '',
        formFields: isFormMode ? draft.formFields : [],
        binaryFilePath: draft.bodyMode === 'binary' ? draft.binaryFilePath : '',
      },
      // Omitted while everything is default, so an untouched request
      // doesn't grow an options block in its collection.json — and so a
      // future change of default reaches requests nobody has customised.
      options: changedOptionCount(draft.options, workspaceDefaults) ? { ...draft.options } : undefined,
    } as unknown as domain.Item
  }

  async function saveRequest(): Promise<domain.Item> {
    const saved = await SaveRequest(collectionId, buildDraftItem())
    selectedItemId = saved.id
    collection = await GetCollection(collectionId)
    return saved
  }

  // Recomputed by the backend (internal/codegen) whenever the Code tab is
  // open and any input changes — see the reactive block below. codeGen
  // guards against an earlier request's response landing after a later
  // one (each keystroke fires a fresh call).
  let codeGen = 0
  async function regenerateCode() {
    const seq = ++codeGen
    try {
      const out = await GenerateRequestCode(buildDraftItem(), environmentId, codeFormat)
      if (seq !== codeGen) return
      generatedCode = out
      codeError = ''
    } catch (e) {
      if (seq !== codeGen) return
      generatedCode = ''
      codeError = String(e)
    }
  }

  async function copyRequestCode() {
    if (!generatedCode) return
    try {
      await navigator.clipboard.writeText(generatedCode)
    } catch (e) {
      logEvent(`copyRequestCode failed: ${e}`)
    }
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
    responseDataUri = null
    try {
      await saveRequest()
      response = await ExecuteRequest(collectionId, selectedItemId!, environmentId)
      await refreshResponseData()
    } catch (e) {
      sendError = String(e)
      response = null
    } finally {
      sending = false
    }
  }

  function toggleResponseActionsMenu() {
    showResponseActionsMenu = !showResponseActionsMenu
  }

  // The "..." menu's actions (and the too-large callout's) — keyed by
  // selectedItemId, they act on the request's cache file (see
  // saveResponseCache), which exists for any response that's been sent.
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

  // Handed to ResponsePane as one prop, built once (see
  // environmentActions for why). These stay here because they're keyed
  // by selectedItemId — which request's cache file to act on — and the
  // pane has no business knowing that.
  const responseCacheActions = {
    openExternally: openResponseCacheExternally,
    copyPath: copyResponseCachePath,
    openInFileExplorer: openResponseCacheInFileExplorer,
    clearCached: clearCachedResponse,
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

  // The active environment: the one {{vars}} resolve against, picked in
  // the top bar and sent to ExecuteRequest. It also opens in the
  // settings list, since selecting one is a reason to want to look at
  // it.
  async function selectEnvironment(id: string) {
    environmentId = id
    environment = await GetEnvironment(id)
  }

  // Open an environment's variables in the settings list *without*
  // making it the active one — the whole point of the list is that
  // reading an environment and using it are different acts. `environment`
  // is therefore "the one open in settings", which is why every
  // environment ui:action operates on it. No id collapses whatever's
  // open.
  async function expandEnvironment(id: string) {
    if (!id || environment?.id === id) {
      environment = null
      return
    }
    environment = await GetEnvironment(id)
  }

  // Renames any row in the settings list, open or not — the same reach
  // coll.rename has. It loads the environment rather than editing
  // `environment`, because that only holds the open one; every edit
  // commits on blur, so there's never unsaved state to clobber.
  async function renameEnvironmentById(id: string, name: string) {
    const loaded = await GetEnvironment(id)
    const saved = await SaveEnvironment({ ...loaded, name } as domain.Environment)
    if (workspace) {
      workspace.environments = workspace.environments.map((e) => (e.id === id ? { ...e, name: saved.name } : e))
    }
    if (environment?.id === id) environment = saved
  }

  // Edits commit when you leave the field, not on every keystroke — one
  // rule across both settings lists, and the only one a collection
  // rename could use anyway, since it moves a directory.
  async function commitEnvironment() {
    if (!environment) return
    const saved = await SaveEnvironment(environment)
    environment = saved
    if (workspace) {
      workspace.environments = workspace.environments.map((e) =>
        e.id === saved.id ? { ...e, name: saved.name, variableCount: saved.variables.length } : e,
      )
    }
  }

  async function newEnvironment(name = 'New environment') {
    const draft = { formatVersion: '1', id: '', name, variables: [] }
    const saved = await SaveEnvironment(draft as unknown as domain.Environment)
    if (workspace) {
      workspace.environments = [
        ...workspace.environments,
        { id: saved.id, name: saved.name, variableCount: saved.variables?.length ?? 0 },
      ]
    }
    await selectEnvironment(saved.id)
  }

  function setEnvironmentField(field: 'name', value: string) {
    if (!environment) return
    if (field === 'name') {
      environment.name = value
      environment = environment
    }
  }

  async function deleteEnvironment(id: string) {
    try {
      await DeleteEnvironment(id)
    } catch (e) {
      logEvent(`deleteEnvironment failed: ${e}`)
      return
    }
    if (workspace) workspace.environments = workspace.environments.filter((e) => e.id !== id)
    if (environmentId === id && workspace?.environments.length) {
      await selectEnvironment(workspace.environments[0].id)
    }
  }

  function confirmDeleteEnvironment(summary: core.EnvironmentSummary) {
    if (confirm(`Delete "${summary.name}"? This can't be undone from the app.`)) {
      void guard(() => deleteEnvironment(summary.id))
    }
  }

  // Handed to SettingsModal as one prop. Built once rather than inline
  // in the markup so the component doesn't see a new object — and so
  // re-render — every time anything else on this page changes. The
  // functions themselves stay here: dispatchUIAction drives the same
  // ones, so a scripted selectEnvironment and a clicked one take
  // exactly the same path.
  // Both lists take the same shape: act on the row you're pointing at,
  // identified by id, rather than on whatever a picker elsewhere in the
  // dialog happens to hold.
  const environmentActions = {
    expand: (id: string) => guard(() => expandEnvironment(id)),
    create: () => guard(async () => void (await newEnvironment())),
    rename: (id: string, value: string) => guard(() => renameEnvironmentById(id, value)),
    commit: () => guard(commitEnvironment),
    confirmDelete: confirmDeleteEnvironment,
    addVariable: () => addEnvironmentVariable(),
    removeVariable: (index: number) =>
      guard(async () => {
        removeEnvironmentVariable(index)
        await commitEnvironment()
      }),
  }

  const collectionActions = {
    create: () => guard(() => newCollection('New collection')),
    rename: (id: string, value: string) => guard(() => renameCollection(value, id)),
    confirmDelete: confirmDeleteCollection,
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

  // Regenerate the Code tab whenever it's open and anything the snippet
  // depends on changes. codeKey stringifies exactly those inputs so the
  // reactive re-runs on any of them (Svelte only tracks variables named
  // in the statement itself, not ones read inside regenerateCode).
  $: codeKey =
    activeTab === 'code'
      ? JSON.stringify([
          codeFormat,
          draft.name,
          draft.method,
          draft.url,
          draft.bodyMode,
          draft.bodyRaw,
          draft.binaryFilePath,
          draft.params,
          draft.headers,
          draft.formFields,
          draft.auth,
          // The Options tab feeds the generated script too — the
          // redirect, timeout, TLS and cookie-jar flags. Leaving it out
          // is invisible by hand (you have to leave the Code tab to
          // change an option, and coming back regenerates) but not to a
          // script: setRequestOption would leave state.code stale.
          draft.options,
          environmentId,
        ])
      : ''
  $: if (codeKey) regenerateCode()

  // The response pane's derived view model: what kind of body came
  // back, whether a pretty view is even possible, and the highlighted
  // HTML when it's the pretty view's turn to render.
  $: formattedResponse = formatResponse(response, responseView, responseDataUri)

  function setResponseView(view: ResponseView) {
    responseView = view
  }

  // Always switches and expands, for the same reason
  // selectRequestEditorTab does — a script asking for 'headers'
  // shouldn't get a collapse just because 'headers' was already showing.
  // toggleResponsePane is the deterministic way to reach the collapse.
  function setResponseTab(tab: 'body' | 'headers') {
    responseTab = tab
    responsePaneCollapsed = false
  }

  // Called after a send / on reselect, and whenever the view changes:
  // pulls the cached body out as a data URI when something on screen
  // needs the real bytes, clears it otherwise. A truncated response has
  // no inline body to speak for, and its cache file is the thing the
  // "..." menu opens instead.
  async function refreshResponseData() {
    const wanted = detectResponseKind(response) === 'image' || responseView === 'hex'
    if (!selectedItemId || !wanted || !response || response.truncated) {
      responseDataUri = null
      return
    }
    try {
      responseDataUri = await GetResponseCacheDataURI(selectedItemId)
    } catch {
      responseDataUri = null
    }
  }

  // Switching to hex is the one case that needs the bytes without a send
  // or a reselect having happened.
  $: responseView, void refreshResponseData()
</script>

<svelte:window on:pointermove={onWindowPointerMove} on:pointerup={onWindowPointerUp} />

<div class="app-shell" class:is-resizing={draggingSplitter !== null}>
  <header class="top-bar">
    <span class="top-bar-title">Freeman</span>
    <!-- What {{vars}} resolve against. It belongs here rather than in
         Settings: reaching it through a preferences window meant opening
         a dialog to change what you're looking at. The collection picker
         is in the sidebar instead, above the requests it fills. -->
    <!-- Everything that isn't the app's name is grouped right, so the
         picker sits with the buttons rather than floating between them —
         .top-bar is space-between, which would otherwise spread the
         children across the whole width. -->
    <div class="top-bar-right">
      {#if workspace}
        <Switcher
          label="Environment"
          items={workspace.environments}
          selectedId={environmentId}
          emptyName="No environment"
          bind:open={showEnvironmentMenu}
          onSelect={(id) => void guard(() => selectEnvironment(id))}
          onEdit={() => openSettingsTab('environments')}
        />
      {/if}
      <span class="top-bar-actions">
        {#if workspace}
          <button class="icon-btn top-bar-btn" title="Settings" on:click={() => (showSettings = true)}>⚙</button>
        {/if}
        <button class="icon-btn top-bar-btn" title="Control API help" on:click={() => (showHelp = true)}>?</button>
      </span>
    </div>
  </header>

{#if !workspace}
  <main class="welcome">
    <h1>Freeman</h1>
    {#if appVersion}<p class="welcome-version">{appVersion}</p>{/if}
    <p class="prose">Pick a folder to use as a workspace for your collections and environments.</p>
    <button class="primary" on:click={openWorkspace}>Choose workspace folder</button>
    {#if openError}<p class="error">{openError}</p>{/if}
  </main>
{:else}
  <div class="layout">
    <RequestList
      {collection}
      {selectedItemId}
      width={sidebarWidth}
      collections={workspace.collections}
      {collectionId}
      bind:collectionMenuOpen={showCollectionMenu}
      onSelectCollection={(id) => void guard(() => selectCollection(id))}
      onEditCollections={() => openSettingsTab('collections')}
      onNew={newRequest}
      onSelect={selectRequest}
      onDelete={confirmDeleteRequest}
    />

    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <div
      class="splitter splitter-vertical"
      class:active={draggingSplitter === 'sidebar'}
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize the request list"
      on:pointerdown={() => onSplitterPointerDown('sidebar')}
    ></div>

    <main class="editor" bind:this={editorEl}>
      <div class="request-pane scroll-pane" class:collapsed={requestPaneCollapsed}>
        <RequestEditor
          bind:draft
          bind:activeTab
          bind:requestPaneCollapsed
          bind:codeFormat
          bind:bodyLanguage
          {generatedCode}
          {codeError}
          {headerCatalog}
          {sending}
          onSave={saveRequest}
          onSend={sendRequest}
          onCopyCode={copyRequestCode}
          rows={requestRowActions}
        />
      </div>

      <!-- The Code tab is about the command, not the last reply — so it
           gets the whole pane rather than sharing it with a response the
           reader isn't looking at. -->
      {#if activeTab !== 'code'}
        <!-- The splitter only means something while both halves are
             showing. Collapse either one and the other takes the whole
             pane, so there's no boundary left to drag. -->
        {#if !responsePaneCollapsed && !requestPaneCollapsed}
          <!-- svelte-ignore a11y-no-static-element-interactions -->
          <div
            class="splitter splitter-horizontal"
            class:active={draggingSplitter === 'response'}
            role="separator"
            aria-orientation="horizontal"
            aria-label="Resize the response pane"
            on:pointerdown={() => onSplitterPointerDown('response')}
          ></div>
        {/if}
        <ResponsePane
          height={responseHeight}
          fill={requestPaneCollapsed && !responsePaneCollapsed}
          {response}
          {sendError}
          formatted={formattedResponse}
          dataUri={responseDataUri}
          bind:tab={responseTab}
          bind:view={responseView}
          bind:collapsed={responsePaneCollapsed}
          bind:showActionsMenu={showResponseActionsMenu}
          cache={responseCacheActions}
        />
      {/if}
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
  <StatusBar
    {controlApiAddr}
    showLog={showControlApiLog}
    height={statusBarHeight}
    entries={statusLog}
    onToggle={toggleControlApiLog}
  />

  {#if showSettings && workspace}
    <SettingsModal
      {workspace}
      {openError}
      {responseCacheCleared}
      bind:settingsTab
      bind:environmentId
      bind:environment
      bind:collectionId
      env={environmentActions}
      coll={collectionActions}
      onClose={() => (showSettings = false)}
      onOpenWorkspace={openWorkspace}
      onClearResponseCache={clearResponseCache}
      bind:settings={workspaceSettings}
      onSaveSettings={saveWorkspaceSettings}
    />
  {/if}

  {#if showHelp}
    <HelpModal {controlApiAddr} {appVersion} onClose={() => (showHelp = false)} />
  {/if}
</div>

<style>
  .app-shell {
    display: flex;
    flex-direction: column;
    height: 100vh;
  }

  .top-bar {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.3rem 0.9rem;
    background: var(--fm-bg-panel);
    border-bottom: 1px solid var(--fm-border);
  }

  .top-bar-title {
    font-weight: 600;
    letter-spacing: -0.01em;
  }

  .top-bar-right {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }

  .top-bar-actions {
    display: flex;
    gap: 0.35rem;
  }

  .top-bar-btn {
    font-size: 1.2rem;
    line-height: 1;
    padding: 0.25rem 0.55rem;
    color: var(--fm-text-muted);
  }

  .top-bar-btn:hover {
    color: var(--fm-text);
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

  /* Sits under the title, inside the flex gap that separates it from
     the prose below — so it reads as part of the heading rather than a
     third paragraph. */
  .welcome-version {
    margin: -0.85rem 0 0;
    font-size: 0.78rem;
    color: var(--fm-text-muted);
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

  /* A split pane: the request editor on top, the response below it, and
     a draggable hairline between. The column itself doesn't scroll —
     each half scrolls its own content, so dragging the splitter moves a
     real boundary rather than sliding one long page. */
  .editor {
    flex: 1;
    padding: 1rem 1.5rem;
    overflow: hidden;
    text-align: left;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  /* flex-basis 0 so this takes the height the response isn't using —
     including all of it when the response is collapsed or the Code tab
     has hidden it. min-height keeps a dragged splitter from squeezing
     the editor away entirely. */
  .request-pane {
    flex: 1 1 0;
    min-height: 8rem;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
  }

  /* Collapsed it's only the name, URL and tab rows, so it shrinks to
     them and the response takes everything below (see ResponsePane's
     .fill) — the mirror of what collapsing the response does. */
  .request-pane.collapsed {
    flex: none;
    min-height: 0;
    overflow: visible;
  }
</style>