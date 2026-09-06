<script lang="ts">
  // Everything below the request editor: the status/time/size/type
  // strip, the Body|Headers switch, and whichever body view the
  // response calls for — headers table, too-large callout, image,
  // syntax-highlighted pretty print, or raw text.
  //
  // Read-only about the response itself. The cache actions in the "..."
  // menu act on files keyed by the selected request's id, which this
  // component has no business knowing, so they come in as callbacks.
  import type { httpengine } from '../../wailsjs/go/models'
  import CodeEditor from './CodeEditor.svelte'
  import { formatBytes, formatDuration, reasonPhrase, statusTone } from '../lib/format'
  import { hexDump } from '../lib/responseFormat'
  import type { ResponseKind, ResponseView } from '../lib/responseFormat'

  export let response: httpengine.Response | null
  export let sendError: string
  export let formatted: { kind: ResponseKind; hasPretty: boolean; text: string }
  // The cached body as a data: URI, loaded on demand — the bytes as
  // they arrived, which response.body can't carry across the Wails
  // bridge. Rendered as the picture for an image, and the source the
  // hex dump was built from.
  export let dataUri: string | null

  // Bound: these are view preferences App.svelte mirrors in
  // GET /api/ui/state and the control API can set, so writes here have
  // to travel back up.
  export let tab: 'body' | 'headers'
  export let view: ResponseView
  export let collapsed: boolean
  export let showActionsMenu: boolean
  // Set by the splitter above; ignored while collapsed or filling.
  export let height: number
  // Take the whole editor pane rather than the dragged height — set when
  // the request editor above is collapsed, so its freed space goes here.
  export let fill = false

  // Picking a panel shows it — collapsing is the disclosure toggle's job
  // alone, not a second meaning overloaded onto these buttons.
  function onResponseTabClick(next: 'body' | 'headers') {
    tab = next
    collapsed = false
  }

  export let cache: {
    openExternally: () => void
    copyPath: () => void
    openInFileExplorer: () => void
    clearCached: () => void
  }

  // detectResponseKind has five values; the editor has three. html is
  // close enough to xml to share a grammar, and image never reaches the
  // editor as text except in raw/hex, where it isn't a grammar anyway.
  //
  // Only the pretty view gets a grammar: raw means the payload as it
  // arrived and hex means the bytes, and colouring either as if it were
  // JSON would be inventing structure the view exists to strip away.
  $: editorLanguage = ((): 'json' | 'xml' | 'plain' => {
    if (view !== 'pretty') return 'plain'
    if (formatted.kind === 'json') return 'json'
    if (formatted.kind === 'xml' || formatted.kind === 'html') return 'xml'
    return 'plain'
  })()

  // The response's headers, flattened (one row per value) and sorted,
  // for the Headers panel.
  $: headerRows = Object.entries(response?.headers ?? {})
    .flatMap(([name, values]) => (values ?? []).map((value) => ({ name, value })))
    .sort((a, b) => a.name.localeCompare(b.name) || a.value.localeCompare(b.value))

  // The headers as they'd read on the wire. No status line above them:
  // Response doesn't carry the protocol version, and writing "HTTP/1.1"
  // would be inventing one.
  $: headerText = headerRows.map((h) => `${h.name}: ${h.value}`).join('\n')

  // The bytes of exactly what the raw view above shows. Not a
  // reconstruction of the CRLF-delimited block from the wire: Response
  // keeps headers as a parsed map, so those bytes are gone, and putting
  // them back would be inventing them the way a status line would.
  $: headerHex = hexDump(new TextEncoder().encode(headerText))

  // Which views this panel offers. Every panel with content has its
  // text and the bytes of that text; a reading distinct from them —
  // the table, the reindent, the picture — only sometimes. A truncated
  // body has none of the three, because that panel is the callout and
  // its buttons instead. Fewer than two hides the switch rather than
  // showing a control that does nothing.
  $: views = ((): ResponseView[] => {
    if (tab === 'headers') return ['pretty', 'raw', 'hex']
    if (response?.truncated) return []
    return formatted.hasPretty ? ['pretty', 'raw', 'hex'] : ['raw', 'hex']
  })()

  $: activeView = views.includes(view) ? view : 'raw'

  const viewLabels: Record<ResponseView, string> = { pretty: 'Pretty', raw: 'Raw', hex: 'Hex' }
</script>

<section class="response" class:collapsed class:fill style:height="{height}px">
  {#if sendError}
    <p class="error">{sendError}</p>
  {:else if response}
    <div class="response-header">
      <div class="response-lead">
        <!-- Same glyphs, same leading position, same wording as the
             control API log's own toggle at the foot of the window —
             the app has one collapse language, not two. -->
        <button
          class="icon-btn"
          title={collapsed ? 'Expand the response' : 'Collapse the response'}
          aria-expanded={!collapsed}
          on:click={() => (collapsed = !collapsed)}>{collapsed ? '▸' : '▾'}</button
        >
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
            <span
              title={response.capped
                ? 'The response exceeded the size Freeman will hold in memory, so it was cut off at this point.'
                : undefined}
            >
              {response.sizeBytes} bytes{#if response.capped}<span class="size-capped">capped</span>{/if}
            </span>
          </div>
          <div class="response-stat">
            <span class="response-stat-label">Type</span>
            <span>{formatted.kind.toUpperCase()}</span>
          </div>
        </div>
      </div>
      <div class="response-header-actions">
        <div class="response-segmented">
          <button class:active={tab === 'body'} on:click={() => onResponseTabClick('body')}>Body</button>
          <button class:active={tab === 'headers'} on:click={() => onResponseTabClick('headers')}>
            Headers{#if headerRows.length}<span class="tab-count">{headerRows.length}</span>{/if}
          </button>
        </div>
        {#if !collapsed && views.length > 1}
          <div class="response-segmented">
            {#each views as v}
              <button class:active={activeView === v} on:click={() => (view = v)}>{viewLabels[v]}</button>
            {/each}
          </div>
        {/if}
        <div class="response-actions-menu">
          <button class="icon-btn" title="Response actions" on:click={() => (showActionsMenu = !showActionsMenu)}
            >⋯</button
          >
          {#if showActionsMenu}
            <!-- svelte-ignore a11y-no-static-element-interactions -->
            <!-- svelte-ignore a11y-click-events-have-key-events -->
            <div class="menu-backdrop" on:click={() => (showActionsMenu = false)}></div>
            <div class="dropdown-menu">
              <button on:click={cache.openExternally}>Open in external editor</button>
              <button on:click={cache.copyPath}>Copy path</button>
              <button on:click={cache.openInFileExplorer}>Open in File Explorer</button>
              <button on:click={cache.clearCached}>Clear cached response</button>
            </div>
          {/if}
        </div>
      </div>
    </div>
    {#if collapsed}
      <!-- Nothing: the meta strip above stays, and its chevron is what
           brings the panel back. -->
    {:else if tab === 'headers'}
      {#if !headerRows.length}
        <p class="muted">No response headers.</p>
      {:else if activeView === 'pretty'}
        <div class="response-headers">
          <table class="response-headers-table">
            <tbody>
              {#each headerRows as h}
                <tr><th>{h.name}</th><td>{h.value}</td></tr>
              {/each}
            </tbody>
          </table>
        </div>
      {:else}
        <CodeEditor readOnly layout="fill" value={activeView === 'hex' ? headerHex : headerText} language="plain" />
      {/if}
    {:else if response.truncated}
      <div class="response-truncated">
        <p>
          Response body is {formatBytes(response.sizeBytes)} — too large to show here.
          {#if response.capped}The server sent more than that; this is where Freeman stopped reading.{/if}
        </p>
        <div class="response-truncated-actions">
          <button on:click={cache.openExternally}>Open in external editor</button>
          <button on:click={cache.copyPath}>Copy path</button>
          <button on:click={cache.openInFileExplorer}>Open in File Explorer</button>
        </div>
      </div>
    {:else if formatted.kind === 'image' && view === 'pretty'}
      {#if dataUri}
        <div class="response-image"><img src={dataUri} alt="Response body" /></div>
      {:else}
        <p class="muted">Loading image…</p>
      {/if}
    {:else}
      <!-- Everything else is text by the time it gets here: a pretty
           print, the payload as it arrived, an image's data: URI, or a
           hex dump. formatResponse decided which. -->
      <CodeEditor readOnly layout="fill" value={formatted.text} language={editorLanguage} />
    {/if}
  {:else}
    <p class="muted">Send a request to see the response here.</p>
  {/if}
</section>

<style>
  /* Height comes from the splitter, as an inline style. shrink 1 (rather
     than a hard height) means a window too short for both halves takes
     it out of the response instead of overflowing the column. */
  .response {
    flex: 0 1 auto;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }

  /* Collapsed it's just the meta strip: no splitter above it any more,
     so it brings back the hairline the splitter was providing, and drops
     the dragged height so the request editor takes that space. */
  .response.collapsed {
    flex: none;
    height: auto !important;
    border-top: 1px solid var(--fm-border-subtle);
    padding-top: 0.75rem;
  }

  /* The request editor above is collapsed, so the dragged height no
     longer applies — this takes what's left instead. Same hairline as
     the collapsed case, for the same reason: no splitter to divide. */
  .response.fill {
    flex: 1 1 auto;
    height: auto !important;
    border-top: 1px solid var(--fm-border-subtle);
    padding-top: 0.75rem;
  }

  /* The spacing below the strip lives here rather than on .response-meta,
     so the disclosure button beside it can centre against the stats
     instead of being pushed up by their margin. */
  .response-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    margin-bottom: 0.5rem;
  }

  /* Collapsed, the strip is the whole pane — nothing below to space off. */
  .response.collapsed .response-header {
    margin-bottom: 0;
  }

  .response-lead {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    min-width: 0;
  }

  .response-meta {
    display: flex;
    gap: 1.5rem;
    font-size: 0.85rem;
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

  /* Marks a body that hit httpengine.MaxResponseBytes — the number next
     to it is what was kept, not what the server sent. */
  .size-capped {
    margin-left: 0.35rem;
    padding: 0.1rem 0.3rem;
    border-radius: 3px;
    font-size: 0.65rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--fm-warning);
    background: color-mix(in srgb, var(--fm-warning) 16%, transparent);
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
</style>
