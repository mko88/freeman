<script lang="ts">
  // The sidebar: the open collection's saved requests, colour-coded by
  // method. Selecting and deleting go back to App.svelte — both are also
  // control-API actions, and deleting asks for confirmation there.
  import type { domain } from '../../wailsjs/go/models'
  import { methodColor, methodLabel } from '../lib/format'
  import { tick } from 'svelte'
  import Switcher from './Switcher.svelte'

  export let collection: domain.Collection | null
  export let selectedItemId: string | null
  export let width: number

  // The collection picker sits above the list it fills, in place of the
  // name that used to be there — the same choice, one control instead of
  // a label in the top bar and a heading here saying the same thing.
  export let collections: { id: string; name: string }[] = []
  export let collectionId = ''
  // Bound: App.svelte mirrors it in GET /api/ui/state and
  // toggleCollectionMenu drives it.
  export let collectionMenuOpen = false
  export let onSelectCollection: (id: string) => void
  export let onEditCollections: () => void

  // Bound: App.svelte mirrors it in GET /api/ui/state and
  // filterRequests drives it. Not persisted — a filter is where you are
  // right now, not a setting.
  export let filter = ''

  export let onNew: () => void
  export let onSelect: (item: domain.Item) => void

  // Matched against the name and the method, because "post" is as
  // likely a search as a word in a name. Case-insensitive, no globbing:
  // this is a way to find one row in forty, not a query language.
  // Bring the selected row into view when the selection changes from
  // somewhere other than a click on it — selectRequest over the control
  // API, or the app reopening on a request further down the list. A
  // click needs no help, and 'nearest' is what makes this a no-op when
  // the row is already showing rather than a jump that moves the list
  // under the pointer.
  let listEl: HTMLElement
  $: void scrollSelectedIntoView(selectedItemId, listEl)
  async function scrollSelectedIntoView(id: string | null, el: HTMLElement | undefined) {
    if (!id || !el) return
    await tick()
    el.querySelector(`[data-item-id="${CSS.escape(id)}"]`)?.scrollIntoView({ block: 'nearest' })
  }

  $: needle = filter.trim().toLowerCase()
  $: shown = (collection?.items ?? []).filter(
    (i) => !needle || `${i.method || 'GET'} ${i.name}`.toLowerCase().includes(needle),
  )
</script>

<aside class="sidebar" style="width: {width}px">
  <div class="sidebar-header">
    <Switcher
      label="Collection"
      items={collections}
      selectedId={collectionId}
      emptyName="No collection"
      align="left"
      block
      bind:open={collectionMenuOpen}
      onSelect={onSelectCollection}
      onEdit={onEditCollections}
    />
    <button class="icon-btn sidebar-new" title="New request" on:click={onNew}>+</button>
  </div>

  <div class="sidebar-filter">
    <input type="search" bind:value={filter} placeholder="Filter requests" aria-label="Filter requests" />
  </div>

  <ul class="request-list scroll-pane" bind:this={listEl}>
    {#each shown as item (item.id)}
      <li
        data-item-id={item.id}
        class:active={item.id === selectedItemId}
        style="--m: {methodColor(item.method || 'GET')}"
      >
        <!-- The whole row selects. Duplicating and deleting live in the
             editor, beside the name they act on, so this list is a list
             of names and nothing else. -->
        <button class="request-select" title="{item.method || 'GET'} {item.name}" on:click={() => onSelect(item)}>
          <span class="method-tag">{methodLabel(item.method)}</span>
          <span class="request-name-text">{item.name}</span>
        </button>
      </li>
    {/each}
  </ul>
  <!-- An empty list means one of two different things, and saying which
       saves a hunt for a request that was never there. -->
  {#if shown.length === 0 && needle}
    <p class="sidebar-empty muted">Nothing matches “{filter}”.</p>
  {/if}
</aside>

<style>
  /* No width here — set inline from sidebarWidth (see the splitter next
     to it). */
  .sidebar {
    flex-shrink: 0;
    background: var(--fm-bg-panel);
    display: flex;
    flex-direction: column;
    text-align: left;
  }

  /* The picker takes the width and the + stays at the right edge. Less
     left padding than before: the switcher is a button with its own
     hover fill, and indenting it would leave that fill floating away
     from the panel edge. */
  .sidebar-header {
    display: flex;
    align-items: center;
    gap: 0.25rem;
    padding: 0.5rem 0.6rem 0.5rem 1rem;
    font-weight: 600;
    border-bottom: 1px solid var(--fm-border-subtle);
  }

  .sidebar-new {
    flex: none;
  }

  .sidebar-filter {
    flex: none;
    padding: 0.5rem 0.6rem;
  }

  .sidebar-filter input {
    width: 100%;
    font-size: 0.8rem;
  }

  .sidebar-empty {
    flex: none;
    margin: 0;
    padding: 0.25rem 1rem 0.75rem;
    font-size: 0.8rem;
  }

  /* .scroll-pane (style.css) supplies the overflow, the padding and the
     margin — including the ul reset the padding would otherwise need,
     since it sets both. Redeclaring either here would win on specificity
     and quietly switch the utility off. */
  .request-list {
    list-style: none;
    flex: 1;
  }

  /* --m (set inline per-row from methodColor()) reads the same hue in
     both the left accent bar and the method tag text — one method, one
     color, everywhere it's shown. */
  .request-list li {
    display: flex;
    align-items: center;
    border-left: 2px solid var(--m, transparent);
    border-bottom: 1px solid var(--fm-border-subtle);
  }

  /* No rule under the last row: it would read as the start of another
     one that isn't there. */
  .request-list li:last-child {
    border-bottom: none;
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
    padding: 0.45rem 0.6rem 0.45rem 2px;
    cursor: pointer;
    display: flex;
    gap: 0.5rem;
    align-items: center;
  }

  /* Wraps rather than truncating: a name is what tells two requests
     apart, and the end of it is often the part that does. overflow-wrap
     so a long unbroken one — a pasted URL, say — breaks instead of
     forcing the row wider than the sidebar. */
  .request-name-text {
    min-width: 0;
    text-align: left;
    overflow-wrap: anywhere;
  }

  .request-list li.active .request-select {
    background: var(--fm-bg-hover);
  }

  /* Turned on its side, so the method costs one line of text across the
     row instead of a whole column — which is most of why the name has
     room. The four-character cap in methodLabel (lib/format.ts) is what
     bounds the height: a longer word here would make every row taller.
     grid + place-items centres it regardless of the writing mode, which
     swaps the axes the usual alignment properties refer to. */
  .method-tag {
    writing-mode: vertical-rl;
    font-size: 0.6rem;
    font-weight: 600;
    letter-spacing: 0.08em;
    color: var(--m, var(--fm-text-muted));
    flex: none;
    height: 2.9rem;
    display: grid;
    place-items: center;
  }
</style>
