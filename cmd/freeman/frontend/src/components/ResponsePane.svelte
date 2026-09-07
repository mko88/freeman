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
  import { durationMillis, exactBytes, formatBytes, formatDuration, reasonPhrase, statusTone } from '../lib/format'
  import { hexDump } from '../lib/responseFormat'
  import type { ResponseKind, ResponseView } from '../lib/responseFormat'

  export let response: httpengine.Response | null
  export let sendError: string
  export let formatted: { kind: ResponseKind; text: string }
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

  // All three, always, on both panels — the switch keeps one shape
  // rather than gaining and losing buttons with the shape of the body,
  // which is worst exactly when you're comparing a failure against a
  // success. Pretty falls back to the plain text when there's nothing
  // to reindent; see formatResponse.
  //
  // The one exception is a truncated body, where the panel is the
  // "too large" callout and its buttons: there's no rendered content
  // for a view to apply to.
  $: views = ((): ResponseView[] =>
    tab === 'body' && response?.truncated ? [] : ['pretty', 'raw', 'hex'])()

  $: activeView = views.includes(view) ? view : 'raw'

  const viewLabels: Record<ResponseView, string> = { pretty: 'Pretty', raw: 'Raw', hex: 'Hex' }
</script>

<section class="response" class:collapsed class:fill style:height="{height}px">
  {#if sendError}
    <p class="error">{sendError}</p>
  {:else if response}
    <!-- One bar, then the content. The request editor needs two rows
         because its options differ per tab; the response's don't, so
         everything fits beside the tabs and the pane keeps the height a
         second row would have cost.
--
         The bar carries the rule under the whole width; .tabs inside it
         gives up its own. Keeping the tabs in their own child is what
         stops .tabs button restyling the view buttons next to them. -->
    <div class="response-bar">
      <div class="tabs">
        <button
          class="disclosure"
          title={collapsed ? 'Expand the response' : 'Collapse the response'}
          aria-expanded={!collapsed}
          on:click={() => (collapsed = !collapsed)}>{collapsed ? '▸' : '▾'}</button
        >
        <button class:active={tab === 'body'} on:click={() => onResponseTabClick('body')}>Body</button>
        <button class:active={tab === 'headers'} on:click={() => onResponseTabClick('headers')}>
          Headers<span class="tab-count">{headerRows.length}</span>
        </button>
      </div>

      <!-- No labels: each value says what it is by its own shape — a
           bare number coloured by class, a duration with a unit, a size
           with a unit, a type name. The labels were repeating that in
           words. Where a value is rounded for reading, the exact figure
           is a tooltip. -->
      <div class="response-meta">
        <span
          class="response-stat status status-{statusTone(response.statusCode)}"
          title={reasonPhrase(response.status) || undefined}
        >
          {response.statusCode}
        </span>
        <span class="response-stat" title={durationMillis(response.durationNs)}>
          {formatDuration(response.durationNs)}
        </span>
        <span
          class="response-stat"
          title={response.capped
            ? `${exactBytes(response.sizeBytes)} — the response exceeded the size Freeman will hold in memory, so it was cut off at this point.`
            : exactBytes(response.sizeBytes)}
        >
          {formatBytes(response.sizeBytes)}{#if response.capped}<span class="size-capped">capped</span>{/if}
        </span>
        <span class="response-stat">{formatted.kind.toUpperCase()}</span>
      </div>

      {#if !collapsed}
        <div class="option-row response-views">
          {#each views as v}
            <button class:active={activeView === v} on:click={() => (view = v)}>{viewLabels[v]}</button>
          {/each}
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
      {/if}
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

  /* Tabs, stats and view buttons on one line. The rule belongs to the
     bar so it runs the full width; .tabs would only draw it under the
     tabs themselves. */
  .response-bar {
    display: flex;
    align-items: stretch;
    border-bottom: 1px solid var(--fm-border-subtle);
  }

  .response-bar .tabs {
    border-bottom: none;
  }

  /* Rides the right end of the bar: these describe the response as a
     whole, not whichever panel is open, so they sit apart from the tabs
     rather than among them. Allowed to shrink and clip on a narrow
     window — the tabs are what must stay clickable.
--
     The explicit height is what makes the hairlines between the stats
     line up: they're borders on stretched children, so they're only
     equal if the row they stretch to is. Shorter than the tab row, so
     the rules read as separators inside the group rather than reaching
     for its edges. */
  .response-meta {
    display: flex;
    align-items: stretch;
    align-self: center;
    height: 1.4rem;
    margin-left: auto;
    padding-left: 1rem;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    font-size: 0.85rem;
  }

  .response-stat {
    display: flex;
    align-items: center;
    padding: 0 0.75rem;
  }

  /* A hairline, not a middle dot: the same 1px divider the tab row, the
     splitter and the status bar already use, so the app separates things
     one way. */
  .response-stat + .response-stat {
    border-left: 1px solid var(--fm-border-subtle);
  }

  .response-stat:last-child {
    padding-right: 0;
  }

  /* Last in the bar, after the stats. .option-row's bottom margin is for
     a row of its own; in here the bar's height sets the spacing. */
  .response-views {
    align-self: center;
    margin: 0;
    padding-left: 1rem;
  }

  /* A touch more air before the menu than between the view buttons —
     it's a different kind of control, not a fourth view. */
  .response-views .response-actions-menu {
    margin-left: 0.35rem;
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

  /* The one thing in the row that gets to be bigger than the rest: it's
     the first question anyone asks of a response. Its tone colour does
     the rest, so nothing else here is coloured. */
  .status {
    font-size: 1rem;
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
