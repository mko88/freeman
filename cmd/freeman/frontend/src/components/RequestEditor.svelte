<script lang="ts">
  // The request half of the editor: name, method/URL bar, Save/Send, and
  // the Params | Headers | Auth | Body | Code tabs.
  //
  // `draft` is bound rather than passed down one way — every field in
  // here is edited in place, and App.svelte both mirrors it in
  // GET /api/ui/state and rebuilds a domain.Item out of it on save.
  // Everything that reaches the backend stays in App.svelte and arrives
  // as a callback, so a click and a control-API action take the same
  // path.
  import CodeEditor from './CodeEditor.svelte'
  import { methodColor } from '../lib/format'
  import { resolveBodyLanguage } from '../lib/responseFormat'
  import type { BodyLanguage } from '../lib/responseFormat'
  import { bodyModes, codeFormats, methods } from '../lib/requestDraft'
  import type { CodeFormat, RequestDraft, RequestTab } from '../lib/requestDraft'

  // Common request headers (and, per header, common values) offered as
  // autocomplete in the header editor. Loaded by App.svelte from the
  // desktop backend; [] in the web build, which just means no
  // suggestions.
  type HeaderCatalogEntry = { name: string; values?: string[] }

  export let draft: RequestDraft
  export let activeTab: RequestTab
  export let requestPaneCollapsed: boolean
  export let codeFormat: CodeFormat
  export let bodyLanguage: BodyLanguage
  export let generatedCode: string
  export let codeError: string
  export let headerCatalog: HeaderCatalogEntry[]
  export let sending: boolean

  export let onSave: () => void
  export let onSend: () => void
  export let onCopyCode: () => void

  // Row add/remove and the native file pickers. Grouped into one prop,
  // built once in App.svelte, for the same reason as SettingsModal's
  // `env`: the control API drives these too, so they can't be
  // reimplemented here.
  export let rows: {
    addParam: () => void
    removeParam: (index: number) => void
    addHeader: () => void
    removeHeader: (index: number) => void
    addFormField: () => void
    removeFormField: (index: number) => void
    pickFormFieldFile: (index: number) => void
    pickBinaryFile: () => void
  }

  // Common values for a header the user has typed, matched
  // case-insensitively against the catalog. [] when the header isn't in
  // the catalog or has no typical values — the caller then omits the
  // value dropdown.
  function headerValues(key: string): string[] {
    const norm = key.trim().toLowerCase()
    return headerCatalog.find((e) => e.name.toLowerCase() === norm)?.values ?? []
  }

  // Picking a tab shows it — collapsing is the disclosure toggle's job
  // alone, not a second meaning overloaded onto these buttons. Same
  // split as the response pane's.
  function onRequestTabClick(tab: RequestTab) {
    activeTab = tab
    requestPaneCollapsed = false
  }

  const bodyLanguages: { value: BodyLanguage; label: string }[] = [
    { value: 'auto', label: 'Auto' },
    { value: 'json', label: 'JSON' },
    { value: 'xml', label: 'XML' },
    { value: 'plain', label: 'Plain' },
  ]

  // Auto reads the request's own Content-Type row before falling back to
  // the body's first character — a body that starts as `{` is JSON well
  // before it's valid JSON.
  $: bodyContentType =
    draft.headers.find((h) => h.enabled && h.key.trim().toLowerCase() === 'content-type')?.value ?? ''
  $: resolvedBodyLanguage = resolveBodyLanguage(bodyLanguage, draft.bodyRaw, bodyContentType)

  // No sniffing needed for the Code tab — the format that generated the
  // snippet says what language it is. CodeEditor calls bash 'shell',
  // after the grammar it loads for it, which covers sh and bash alike.
  const codeLanguages = { bash: 'shell', powershell: 'powershell' } as const

  $: codeLanguage = codeLanguages[codeFormat]

  // Tab badges. Every tab that holds state always shows it, including
  // when that state is empty: a silent tab and a tab holding nothing
  // used to look identical, so "no params" was indistinguishable from
  // "haven't looked". Counting only rows with a key filled in, since a
  // blank row the user just added isn't a param or header yet.
  const filledCount = (rows: { key: string }[]) => rows.filter((row) => row.key.trim()).length
  $: paramsTabBadge = filledCount(draft.params)
  $: headersTabBadge = filledCount(draft.headers)
  // Auth and Body are a choice rather than a list, so their badge is the
  // option selected — the same word the tab's own control shows.
  $: authTabBadge = draft.auth.type === 'apikey' ? 'API key' : draft.auth.type
  // Spelled out, x-www-form-urlencoded is wider than the rest of the tab
  // row put together; shortened it still can't be confused with the
  // other form mode next to it.
  $: bodyTabBadge = draft.bodyMode === 'x-www-form-urlencoded' ? 'urlencoded' : draft.bodyMode
</script>

<div class="request-name">
  <input type="text" bind:value={draft.name} placeholder="Request name" />
</div>

<div class="url-bar">
  <select class="method-select" bind:value={draft.method} style="--m: {methodColor(draft.method)}">
    {#each methods as m}<option value={m}>{m}</option>{/each}
  </select>
  <!-- Enter sends, the way a browser's address bar goes. No new
       ui:action for it: this is a second route to onSend, which the
       control API already reaches as `sendRequest`. -->
  <input
    type="text"
    bind:value={draft.url}
    on:keydown={(e) => {
      if (e.key === 'Enter' && !sending) onSend()
    }}
    placeholder="{'{'}{'{'}schema{'}'}{'}'}://{'{'}{'{'}base{'}'}{'}'}/api/{'{'}{'{'}version{'}'}{'}'}/health"
  />
  <button on:click={onSave}>Save</button>
  <button class="primary" on:click={onSend} disabled={sending}>
    {sending ? 'Sending…' : 'Send'}
  </button>
</div>

<div class="tabs">
  <button
    class="disclosure"
    title={requestPaneCollapsed ? 'Expand the request editor' : 'Collapse the request editor'}
    aria-expanded={!requestPaneCollapsed}
    on:click={() => (requestPaneCollapsed = !requestPaneCollapsed)}>{requestPaneCollapsed ? '▸' : '▾'}</button
  >
  <button class:active={activeTab === 'params'} on:click={() => onRequestTabClick('params')}>
    Params<span class="tab-count">{paramsTabBadge}</span>
  </button>
  <button class:active={activeTab === 'headers'} on:click={() => onRequestTabClick('headers')}>
    Headers<span class="tab-count">{headersTabBadge}</span>
  </button>
  <button class:active={activeTab === 'auth'} on:click={() => onRequestTabClick('auth')}>
    Auth<span class="tab-count">{authTabBadge}</span>
  </button>
  <button class:active={activeTab === 'body'} on:click={() => onRequestTabClick('body')}>
    Body<span class="tab-count">{bodyTabBadge}</span>
  </button>
  <button class:active={activeTab === 'code'} on:click={() => onRequestTabClick('code')}>Code</button>
</div>

{#if !requestPaneCollapsed}
{#if activeTab === 'params'}
  <table class="kv-table">
    <thead>
      <tr><th></th><th>Key</th><th>Value</th><th></th></tr>
    </thead>
    <tbody>
      {#each draft.params as p, i}
        <tr>
          <td><input type="checkbox" bind:checked={p.enabled} /></td>
          <td><input type="text" bind:value={p.key} placeholder="param" /></td>
          <td><input type="text" bind:value={p.value} placeholder="value" /></td>
          <td><button class="icon-btn kv-remove-btn" on:click={() => rows.removeParam(i)}>×</button></td>
        </tr>
      {/each}
    </tbody>
  </table>
  <button on:click={() => rows.addParam()}>Add param</button>
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
      {#each draft.headers as h, i}
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
          <td><button class="icon-btn kv-remove-btn" on:click={() => rows.removeHeader(i)}>×</button></td>
        </tr>
      {/each}
    </tbody>
  </table>
  <button on:click={() => rows.addHeader()}>Add header</button>
{:else if activeTab === 'auth'}
  <div class="auth-editor">
    <label class="auth-field">
      <span>Type</span>
      <select bind:value={draft.auth.type}>
        <option value="none">No auth</option>
        <option value="bearer">Bearer token</option>
        <option value="basic">Basic</option>
        <option value="apikey">API key (header)</option>
      </select>
    </label>

    {#if draft.auth.type === 'bearer'}
      <label class="auth-field">
        <span>Token</span>
        <input type="text" bind:value={draft.auth.token} placeholder="token or {'{'}{'{'}var{'}'}{'}'}" />
      </label>
    {:else if draft.auth.type === 'basic'}
      <label class="auth-field">
        <span>Username</span>
        <input type="text" bind:value={draft.auth.username} placeholder="username or {'{'}{'{'}var{'}'}{'}'}" />
      </label>
      <label class="auth-field">
        <span>Password</span>
        <input type="text" bind:value={draft.auth.password} placeholder="password or {'{'}{'{'}var{'}'}{'}'}" />
      </label>
    {:else if draft.auth.type === 'apikey'}
      <label class="auth-field">
        <span>Header</span>
        <input type="text" list="fm-header-names" bind:value={draft.auth.key} placeholder="X-API-Key" />
      </label>
      <label class="auth-field">
        <span>Value</span>
        <input type="text" bind:value={draft.auth.value} placeholder="key or {'{'}{'{'}var{'}'}{'}'}" />
      </label>
    {/if}
  </div>
{:else if activeTab === 'code'}
  <div class="code-tab">
    <div class="code-formats">
      {#each codeFormats as f}
        <button class:active={codeFormat === f.value} on:click={() => (codeFormat = f.value)}>{f.label}</button>
      {/each}
      <button class="code-copy" on:click={onCopyCode} disabled={!generatedCode}>Copy</button>
    </div>
    {#if codeError}
      <p class="error">{codeError}</p>
    {:else}
      <CodeEditor readOnly wrap={false} layout="fill" value={generatedCode} language={codeLanguage} />
    {/if}
  </div>
{:else}
  <div class="body-mode-picker">
    {#each bodyModes as m}
      <label class="body-mode-option">
        <input type="radio" name="body-mode" value={m.value} bind:group={draft.bodyMode} />
        {m.label}
      </label>
    {/each}
  </div>

  {#if draft.bodyMode === 'raw'}
    <div class="option-row body-languages">
      {#each bodyLanguages as l}
        <button class:active={bodyLanguage === l.value} on:click={() => (bodyLanguage = l.value)}>{l.label}</button>
      {/each}
      {#if bodyLanguage === 'auto'}
        <span class="body-language-detected">detected: {resolvedBodyLanguage}</span>
      {/if}
      <span class="body-keys">Tab indents · Esc then Tab leaves</span>
    </div>

    <CodeEditor bind:value={draft.bodyRaw} language={resolvedBodyLanguage} placeholder="Raw request body" />
  {:else if draft.bodyMode === 'form-data' || draft.bodyMode === 'x-www-form-urlencoded'}
    <table class="kv-table">
      <thead>
        <tr>
          <th></th>
          <th>Key</th>
          {#if draft.bodyMode === 'form-data'}<th class="form-field-type-col">Type</th>{/if}
          <th>Value</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        {#each draft.formFields as f, i}
          <tr>
            <td><input type="checkbox" bind:checked={f.enabled} /></td>
            <td><input type="text" bind:value={f.key} placeholder="key" /></td>
            {#if draft.bodyMode === 'form-data'}
              <td>
                <select bind:value={f.type}>
                  <option value="text">text</option>
                  <option value="file">file</option>
                </select>
              </td>
            {/if}
            <td>
              {#if draft.bodyMode === 'form-data' && f.type === 'file'}
                <div class="file-field">
                  <input type="text" bind:value={f.filePath} placeholder="path to file" />
                  <button on:click={() => rows.pickFormFieldFile(i)}>Browse…</button>
                </div>
              {:else}
                <input type="text" bind:value={f.value} placeholder="value" />
              {/if}
            </td>
            <td><button class="icon-btn kv-remove-btn" on:click={() => rows.removeFormField(i)}>×</button></td>
          </tr>
        {/each}
      </tbody>
    </table>
    <button on:click={() => rows.addFormField()}>Add field</button>
  {:else if draft.bodyMode === 'binary'}
    <div class="file-field">
      <input type="text" bind:value={draft.binaryFilePath} placeholder="Path to file — sent as the entire body" />
      <button on:click={rows.pickBinaryFile}>Browse…</button>
    </div>
  {/if}
{/if}
{/if}

<style>
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

  .body-language-detected {
    font-size: 0.72rem;
    color: var(--fm-text-muted);
  }

  /* Tab-indents-instead-of-moving-focus isn't guessable, and neither is
     the way back out. */
  .body-keys {
    margin-left: auto;
    font-size: 0.72rem;
    color: var(--fm-text-muted);
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

  /* The Auth tab: a short stack of labelled fields, each label a fixed
     column so the inputs line up regardless of label length. */
  .auth-editor {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    max-width: 42rem;
  }

  .auth-field {
    display: flex;
    align-items: center;
    gap: 0.6rem;
  }

  .auth-field > span {
    flex-shrink: 0;
    width: 5.5rem;
    font-size: 0.8rem;
    color: var(--fm-text-muted);
  }

  .auth-field > select,
  .auth-field > input {
    flex: 1;
    min-width: 0;
  }

  /* Takes the whole editor pane: App.svelte hides the response while
     this tab is open, so there's nothing below to share the height
     with. */
  .code-tab {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    min-height: 0;
  }

  .code-formats {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem;
  }

  /* Which format is being shown. An accent tint and border rather than
     .primary's filled accent — that's the weight Send carries, and a
     view selector shouldn't shout as loudly as the button that puts a
     request on the wire. */
  .code-formats button.active {
    color: var(--fm-text);
    border-color: var(--fm-accent);
    background: color-mix(in srgb, var(--fm-accent) 14%, transparent);
  }

  /* Without this the base button:hover rule loses on specificity and
     the active button is the one thing in the row that doesn't respond
     to the pointer. */
  .code-formats button.active:hover {
    background: color-mix(in srgb, var(--fm-accent) 22%, transparent);
  }

  .code-formats .code-copy {
    margin-left: auto;
  }

</style>
