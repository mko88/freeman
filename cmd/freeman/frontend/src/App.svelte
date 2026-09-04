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
  } from '$backend'
  import type { domain, httpengine, core } from '../wailsjs/go/models'
  import { EventsOn } from '../wailsjs/runtime/runtime'
  import { ControlAPIAddr, GetHeaderCatalog } from '../wailsjs/go/wailsapp/App.js'

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

  // Mirrors domain.BodyMode's string constants — Wails' binding generator
  // doesn't emit a type for a named string type, only struct classes, so
  // this is redefined here (domain.Body.mode itself is typed as a plain
  // string in models.ts).
  type BodyMode = 'none' | 'raw' | 'form-data' | 'x-www-form-urlencoded'

  let draftName = 'New Request'
  let draftMethod = 'GET'
  let draftUrl = ''
  let draftHeaders: domain.Header[] = []
  let draftBodyMode: BodyMode = 'none'
  let draftBodyRaw = ''
  let draftBodyContentType = 'application/json'
  let draftFormFields: domain.FormField[] = []

  const bodyModes: { value: BodyMode; label: string }[] = [
    { value: 'none', label: 'none' },
    { value: 'raw', label: 'raw' },
    { value: 'form-data', label: 'form-data' },
    { value: 'x-www-form-urlencoded', label: 'x-www-form-urlencoded' },
  ]

  let environmentId = ''
  let environment: domain.Environment | null = null
  let showEnvEditor = false

  let response: httpengine.Response | null = null
  let sending = false
  let sendError = ''
  let activeTab: 'headers' | 'body' = 'headers'

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
    { method: 'GET', path: '/api/theme', desc: 'Resolved color palette.' },
    { method: 'GET', path: '/api/headers', desc: 'Common request-header names/values for editor autocomplete (from headers.yaml).' },
    { method: 'POST', path: '/api/ui/action', desc: 'Drive the GUI itself (desktop only, see below). Body: {action, payload}.' },
  ]

  // Action names spell out their target explicitly — Request or
  // Environment — wherever one applies, rather than a bare verb (e.g.
  // addRequestHeader/addEnvironmentVariable, not addHeader/addVariable),
  // so the list reads unambiguously on its own.
  const uiActions = [
    { action: 'toggleEnvironmentEditor', payload: '—', desc: 'Open/close the environment editor window.' },
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
    { action: 'openWorkspace', payload: '{ path }', desc: 'Open a workspace by path (no folder dialog).' },
    { action: 'toggleHelp', payload: '—', desc: 'Open/close this help panel.' },
    { action: 'selectRequestTab', payload: "{ tab }  // 'headers' | 'body'", desc: 'Switch the request editor tab.' },
    {
      action: 'setRequestField',
      payload:
        "{ field, value }  // field: 'name'|'method'|'url'|'bodyRaw'|'bodyMode'\n" +
        "  // bodyMode value: 'none'|'raw'|'form-data'|'x-www-form-urlencoded'",
      desc: "Set a field in the request currently in the editor (before saving).",
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
      action: 'addRequestFormField',
      payload: '{ key?, value?, enabled? }',
      desc: "Add a form-data / URL-encoded body field row, optionally pre-filled.",
    },
    {
      action: 'setRequestFormField',
      payload: '{ index, key?, value?, enabled? }',
      desc: 'Populate an existing form field row by its position.',
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
      case 'toggleEnvironmentEditor':
        showEnvEditor = !showEnvEditor
        break
      case 'selectEnvironment':
        if (payload?.id) await selectEnvironment(String(payload.id))
        break
      case 'selectCollection':
        if (payload?.id) await selectCollection(String(payload.id))
        break
      case 'selectRequest': {
        const item = collection?.items.find((i) => i.id === payload?.id)
        if (item) selectRequest(item)
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
        if (tab === 'headers' || tab === 'body') activeTab = tab
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
          // No 'bodyContentType' case: retired 2026-09-04 along with the
          // Headers tab's old Content-Type field — Content-Type is set
          // via a regular header row now (addRequestHeader/setRequestHeader),
          // same as any other header. draftBodyContentType itself still
          // exists as saveRequest's fallback default (see below); it's
          // just no longer independently settable here.
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
      case 'addRequestFormField':
        addRequestFormField({
          key: typeof payload?.key === 'string' ? payload.key : '',
          value: typeof payload?.value === 'string' ? payload.value : '',
          enabled: typeof payload?.enabled === 'boolean' ? payload.enabled : true,
        })
        break
      case 'setRequestFormField': {
        const index = Number(payload?.index)
        if (Number.isNaN(index)) break
        const fields: Partial<domain.FormField> = {}
        if (typeof payload?.key === 'string') fields.key = payload.key
        if (typeof payload?.value === 'string') fields.value = payload.value
        if (typeof payload?.enabled === 'boolean') fields.enabled = payload.enabled
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
    if (collection.items.length) {
      selectRequest(collection.items[0])
    } else {
      newRequest()
    }
  }

  function selectRequest(item: domain.Item) {
    selectedItemId = item.id
    draftName = item.name
    draftMethod = item.method || 'GET'
    draftUrl = item.url || ''
    draftHeaders = item.headers ? item.headers.map((h) => ({ ...h })) : []
    draftBodyMode = (item.body?.mode as BodyMode) || 'none'
    draftBodyRaw = item.body?.raw || ''
    draftBodyContentType = item.body?.rawContentType || 'application/json'
    draftFormFields = item.body?.formFields ? item.body.formFields.map((f) => ({ ...f })) : []
    response = null
    sendError = ''
  }

  function newRequest() {
    selectedItemId = null
    draftName = 'New Request'
    draftMethod = 'GET'
    draftUrl = ''
    draftHeaders = []
    draftBodyMode = 'none'
    draftBodyRaw = ''
    draftBodyContentType = 'application/json'
    draftFormFields = []
    response = null
    sendError = ''
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

  function addRequestFormField(initial?: Partial<domain.FormField>) {
    draftFormFields = [...draftFormFields, { key: '', value: '', enabled: true, ...initial }]
  }

  function setRequestFormField(index: number, fields: Partial<domain.FormField>) {
    draftFormFields = draftFormFields.map((f, i) => (i === index ? { ...f, ...fields } : f))
  }

  function removeRequestFormField(index: number) {
    draftFormFields = draftFormFields.filter((_, i) => i !== index)
  }

  async function saveRequest(): Promise<domain.Item> {
    const isFormMode = draftBodyMode === 'form-data' || draftBodyMode === 'x-www-form-urlencoded'
    const item = {
      id: selectedItemId ?? '',
      type: 'request',
      name: draftName || 'Untitled Request',
      method: draftMethod,
      url: draftUrl,
      headers: draftHeaders,
      body: {
        mode: draftBodyMode,
        raw: draftBodyMode === 'raw' ? draftBodyRaw : '',
        rawContentType: draftBodyContentType,
        formFields: isFormMode ? draftFormFields : [],
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
      if (collection.items.length) {
        selectRequest(collection.items[0])
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
    try {
      await saveRequest()
      response = await ExecuteRequest(collectionId, selectedItemId!, environmentId)
    } catch (e) {
      sendError = String(e)
      response = null
    } finally {
      sending = false
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

  // Go's http.Response.Status (httpengine.Response.status) is already
  // "<code> <reason>", e.g. "200 OK" — this strips the leading code so
  // it isn't shown twice next to statusCode ("200 200 OK").
  function reasonPhrase(status: string): string {
    return status.replace(/^\d+\s*/, '')
  }

  // Standard HTTP status-class semantics, for coloring the status badge.
  function statusTone(code: number): 'success' | 'info' | 'warning' | 'error' {
    if (code >= 200 && code < 300) return 'success'
    if (code >= 300 && code < 400) return 'info'
    if (code >= 400 && code < 500) return 'warning'
    return 'error'
  }
</script>

<div class="app-shell">
{#if !workspace}
  <main class="welcome">
    <h1>Freeman</h1>
    <p>Pick a folder to use as a workspace for your collections and environments.</p>
    <button class="primary" on:click={openWorkspace}>Choose workspace folder</button>
    {#if openError}<p class="error">{openError}</p>{/if}
  </main>
{:else}
  <div class="layout">
    <aside class="sidebar">
      <div class="sidebar-header">
        <span>{collection?.name ?? ''}</span>
        <button class="icon-btn" title="New request" on:click={newRequest}>+</button>
      </div>
      <ul class="request-list">
        {#each collection?.items ?? [] as item (item.id)}
          <li class:active={item.id === selectedItemId}>
            <button class="request-select" on:click={() => selectRequest(item)}>
              <span class="method-tag">{item.method || 'GET'}</span>
              <span class="request-select-name">{item.name}</span>
            </button>
            <button class="icon-btn" title="Delete request" on:click={() => confirmDeleteRequest(item)}>×</button>
          </li>
        {/each}
      </ul>

      <div class="env-picker">
        <select bind:value={environmentId} on:change={() => selectEnvironment(environmentId)}>
          {#each workspace.environments as env (env.id)}
            <option value={env.id}>{env.name}</option>
          {/each}
        </select>
        <button class="icon-btn" title="Edit environment" on:click={() => (showEnvEditor = !showEnvEditor)}
          >⚙</button
        >
      </div>
    </aside>

    <main class="editor">
      <div class="request-name">
        <input type="text" bind:value={draftName} placeholder="Request name" />
      </div>

      <div class="url-bar">
        <select bind:value={draftMethod}>
          {#each methods as m}<option value={m}>{m}</option>{/each}
        </select>
        <input type="text" bind:value={draftUrl} placeholder="https://api.example.com/{'{'}{'{'}baseUrl{'}'}{'}'}" />
        <button on:click={saveRequest}>Save</button>
        <button class="primary" on:click={sendRequest} disabled={sending}>
          {sending ? 'Sending…' : 'Send'}
        </button>
      </div>

      <div class="tabs">
        <button class:active={activeTab === 'headers'} on:click={() => (activeTab = 'headers')}>Headers</button>
        <button class:active={activeTab === 'body'} on:click={() => (activeTab = 'body')}>Body</button>
      </div>

      {#if activeTab === 'headers'}
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
                <td><button class="icon-btn" on:click={() => removeRequestHeader(i)}>×</button></td>
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
              <tr><th></th><th>Key</th><th>Value</th><th></th></tr>
            </thead>
            <tbody>
              {#each draftFormFields as f, i}
                <tr>
                  <td><input type="checkbox" bind:checked={f.enabled} /></td>
                  <td><input type="text" bind:value={f.key} placeholder="key" /></td>
                  <td><input type="text" bind:value={f.value} placeholder="value" /></td>
                  <td><button class="icon-btn" on:click={() => removeRequestFormField(i)}>×</button></td>
                </tr>
              {/each}
            </tbody>
          </table>
          <button on:click={() => addRequestFormField()}>Add field</button>
        {:else}
          <p class="muted">No body.</p>
        {/if}
      {/if}

      <section class="response">
        {#if sendError}
          <p class="error">{sendError}</p>
        {:else if response}
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
          </div>
          <pre class="response-body">{response.body}</pre>
        {:else}
          <p class="muted">Send a request to see the response here.</p>
        {/if}
      </section>
    </main>
  </div>
{/if}

  <footer class="status-bar">
    <div class="status-bar-header">
      <span>
        {#if controlApiAddr}
          Control API: <code>http://{controlApiAddr}</code>
        {:else}
          Control API: desktop build only
        {/if}
      </span>
      <button class="icon-btn" title="API help" on:click={() => (showHelp = true)}>?</button>
    </div>
    <div class="status-log" bind:this={statusLogEl}>
      {#if statusLog.length === 0}
        <div class="status-line muted">No control-API events yet.</div>
      {:else}
        {#each statusLog as entry}
          <div class="status-line"><span class="status-time">{entry.time}</span>{entry.text}</div>
        {/each}
      {/if}
    </div>
  </footer>

  {#if showEnvEditor && environment}
    <div
      class="modal-backdrop"
      role="presentation"
      on:click={() => (showEnvEditor = false)}
      on:keydown={(e) => e.key === 'Escape' && (showEnvEditor = false)}
    >
      <div
        class="modal env-editor"
        role="dialog"
        aria-modal="true"
        aria-labelledby="env-title"
        tabindex="-1"
        on:click|stopPropagation
        on:keydown={(e) => e.key === 'Escape' && (showEnvEditor = false)}
      >
        <div class="modal-header">
          <h2 id="env-title">Environment: {environment.name}</h2>
          <button class="icon-btn" title="Close" on:click={() => (showEnvEditor = false)}>×</button>
        </div>
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
                <td><button class="icon-btn" on:click={() => removeEnvironmentVariable(i)}>×</button></td>
              </tr>
            {/each}
          </tbody>
        </table>
        <div class="row">
          <button on:click={() => addEnvironmentVariable()}>Add variable</button>
          <button class="primary" on:click={saveEnvironment}>Save environment</button>
        </div>
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
        class="modal"
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
        <p class="hint">
          {#if controlApiAddr}
            Base URL: <code>http://{controlApiAddr}</code> — no auth, loopback-only.
          {:else}
            Only available in the desktop build.
          {/if}
        </p>

        <h3>Endpoints</h3>
        <table>
          <thead><tr><th>Method</th><th>Path</th><th>Description</th></tr></thead>
          <tbody>
            {#each apiEndpoints as e}
              <tr><td>{e.method}</td><td><code>{e.path}</code></td><td>{e.desc}</td></tr>
            {/each}
          </tbody>
        </table>

        <h3>UI actions (via POST /api/ui/action)</h3>
        <table>
          <thead><tr><th>action</th><th>payload</th><th>Description</th></tr></thead>
          <tbody>
            {#each uiActions as a}
              <tr><td><code>{a.action}</code></td><td>{a.payload}</td><td>{a.desc}</td></tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>
  {/if}
</div>

<style>
  :global(body) {
    overflow: hidden;
  }

  .app-shell {
    display: flex;
    flex-direction: column;
    height: 100vh;
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

  .layout {
    flex: 1;
    min-height: 0;
    display: flex;
  }

  .status-bar {
    flex: none;
    display: flex;
    flex-direction: column;
    height: 118px;
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

  .status-bar-header code {
    color: var(--fm-text);
  }

  .status-log {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 4px 10px;
    font-family: 'Cascadia Code', Consolas, monospace;
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
    border-radius: 8px;
    padding: 1rem 1.25rem;
    text-align: left;
  }

  .modal-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 0.5rem;
  }

  .modal-header h2 {
    margin: 0;
    font-size: 1.1rem;
  }

  /* Action row at the bottom of the env-editor modal. */
  .env-editor .row {
    display: flex;
    gap: 0.5rem;
    margin-top: 0.75rem;
  }

  .modal h3 {
    font-size: 0.85rem;
    text-transform: uppercase;
    letter-spacing: 0.03em;
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

  code {
    font-family: 'Cascadia Code', Consolas, monospace;
    background: var(--fm-bg-elevated);
    padding: 1px 5px;
    border-radius: 3px;
  }

  .sidebar {
    width: 260px;
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

  .request-list li {
    display: flex;
    align-items: center;
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
    opacity: 0.7;
    width: 3.5rem;
    flex-shrink: 0;
  }

  .env-picker {
    display: flex;
    gap: 0.5rem;
    padding: 0.75rem 1rem;
    border-top: 1px solid var(--fm-border-subtle);
  }

  .env-picker select {
    flex: 1;
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
    font-size: 1.1rem;
    font-weight: 600;
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

  .tabs {
    display: flex;
    gap: 0.25rem;
    border-bottom: 1px solid var(--fm-border-subtle);
  }

  .tabs button {
    background: none;
    border: none;
    color: inherit;
    opacity: 0.6;
    padding: 0.5rem 0.75rem;
    cursor: pointer;
  }

  .tabs button.active {
    opacity: 1;
    border-bottom: 2px solid var(--fm-accent);
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
    width: 1.75rem;
    padding-left: 0;
  }

  .kv-table td:last-child,
  .kv-table th:last-child {
    width: 1.75rem;
    padding-right: 0;
  }

  /* The env-editor's table has a second narrow (checkbox) column —
     Secret — that isn't first or last, so it needs its own rule or it'd
     claim an even share of the remaining width like Key/Value do. */
  .env-editor .kv-table th:nth-child(4),
  .env-editor .kv-table td:nth-child(4) {
    width: 4.5rem;
  }

  .kv-table input[type='text'] {
    width: 100%;
  }

  .body-editor {
    width: 100%;
    min-height: 140px;
    font-family: 'Cascadia Code', Consolas, monospace;
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
    font-family: 'Cascadia Code', Consolas, monospace;
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

  .response {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
    border-top: 1px solid var(--fm-border-subtle);
    padding-top: 0.75rem;
  }

  .response-meta {
    display: flex;
    gap: 1.5rem;
    font-size: 0.85rem;
    margin-bottom: 0.5rem;
  }

  .response-stat {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
  }

  .response-stat-label {
    font-size: 0.68rem;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--fm-text-muted);
  }

  .status {
    font-weight: 700;
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
    font-family: 'Cascadia Code', Consolas, monospace;
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
    border-radius: 4px;
    padding: 0.4rem 0.5rem;
  }

  button {
    cursor: pointer;
  }

  button.primary {
    background: var(--fm-accent);
    border-color: var(--fm-accent);
  }

  .icon-btn {
    background: none;
    border: none;
    padding: 0.1rem 0.4rem;
  }
</style>
