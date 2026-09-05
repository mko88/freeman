<script lang="ts">
  // The in-app reference for the control API (see CLAUDE.md's standing
  // rule). The two tables below are the documentation — every route and
  // every ui:action the app answers to — and are read as source by
  // scripts/control_api/checks/consistency.py, which diffs them against
  // dispatchUIAction's cases and against what the suite actually drives.
  // Keep them here, next to nothing else, so that parse stays simple.
  import { methodColor } from '../lib/format'

  export let controlApiAddr: string
  export let onClose: () => void

  const apiEndpoints = [
    { method: 'GET', path: '/api/health', desc: 'Health check.' },
    { method: 'GET', path: '/api/workspace', desc: 'Current workspace (collection/environment summaries).' },
    { method: 'GET', path: '/api/collections', desc: 'List collections.' },
    { method: 'GET', path: '/api/collections/{id}', desc: 'Get a collection, including its requests.' },
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
        'Render a request as a runnable command. Body: {item: domain.Item, environmentId, format}. ' +
        "format is 'curl', 'shell', 'powershell', or 'powershell-script'. Returns {code}.",
    },
    { method: 'GET', path: '/api/theme', desc: 'Resolved color palette.' },
    { method: 'GET', path: '/api/headers', desc: 'Common request-header names/values for editor autocomplete (from headers.yaml).' },
    { method: 'POST', path: '/api/ui/action', desc: 'Drive the GUI itself (desktop only, see below). Body: {action, payload}.' },
    {
      method: 'GET',
      path: '/api/ui/state',
      desc:
        'Current editor state — workspaceRoot/name/method/url/bodyMode/bodyRaw/binaryFilePath/params/headers/auth/' +
        'formFields/codeFormat/code (the Code tab’s rendered command, when that tab is open)/' +
        'tab/requestPaneCollapsed/selected ids/the open environment/the last response ' +
        '(truncated/bodyFile in place of body when it was too large — bodyFile is its path in the ' +
        'per-request on-disk cache, see clearResponseCache; capped when the response outgrew the ' +
        'in-memory ceiling and body holds only what was read); responseTab (body/headers), ' +
        'responseView (pretty/raw) and responseKind (json/xml/html/image/text, lightly autodetected))/' +
        'showResponseActionsMenu/showSettings/settingsTab/showHelp/sidebarWidth/statusBarHeight/showControlApiLog — so a script ' +
        "can read what the UI shows instead of screenshotting it (desktop only).",
    },
  ]

  const uiActions = [
    { action: 'toggleSettings', payload: '—', desc: 'Open/close the settings window (workspace folder, environments).' },
    {
      action: 'selectSettingsTab',
      payload: '{ tab }',
      desc: "Switch the settings window tab. tab is 'workspace' or 'environments'.",
    },
    { action: 'selectEnvironment', payload: '{ id }', desc: 'Switch the active environment.' },
    { action: 'newEnvironment', payload: '—', desc: 'Create a new environment and switch to it.' },
    {
      action: 'setEnvironmentField',
      payload: "{ field: 'name', value }",
      desc: 'Rename the environment currently in the editor (persisted by saveEnvironment).',
    },
    {
      action: 'deleteEnvironment',
      payload: '{ id? }',
      desc: 'Delete an environment by id (default: the one in the editor). Refused for the last one; no confirmation.',
    },
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
      desc: "Switch the request editor tab. tab is 'params', 'headers', 'auth', 'body' or 'code'.",
    },
    {
      action: 'selectCodeFormat',
      payload: '{ format }',
      desc:
        "Switch the Code tab's output. format is 'curl', 'shell' (curl as a bash script), 'powershell', " +
        "or 'powershell-script'. Read the rendered command from GET /api/ui/state's code field.",
    },
    {
      action: 'copyRequestCode',
      payload: '—',
      desc: "Copy the Code tab's rendered command to the clipboard.",
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
</script>

<!-- svelte-ignore a11y-click-events-have-key-events -->
<div
  class="modal-backdrop"
  role="presentation"
  on:click={() => onClose()}
  on:keydown={(e) => e.key === 'Escape' && onClose()}
>
  <div
    class="modal help-modal"
    role="dialog"
    aria-modal="true"
    aria-labelledby="help-title"
    tabindex="-1"
    on:click|stopPropagation
    on:keydown={(e) => e.key === 'Escape' && onClose()}
  >
    <div class="modal-header">
      <h2 id="help-title">Control API</h2>
      <button class="icon-btn" title="Close" on:click={() => onClose()}>×</button>
    </div>
    <p class="hint prose">
      {#if controlApiAddr}
        Base URL: <code>http://{controlApiAddr}</code> — no auth, loopback-only. Requests with a body must
        send <code>Content-Type: application/json</code>, and anything a browser marks as coming from
        another site is refused — that's what stops a web page you have open from driving this.
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

<style>
  /* Wider (more room for Path/Description before either wraps) and a
     taller cap — with the two reference tables below sized to their own
     content instead of fighting the layout, this fits without scrolling
     at any normal window size; overflow-y stays as a safety net only,
     not the expected outcome. */
  .help-modal {
    width: min(960px, 94vw);
    max-height: 94vh;
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
</style>
