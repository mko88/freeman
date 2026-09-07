<script lang="ts">
  // The sidebar: the open collection's saved requests, colour-coded by
  // method. Selecting and deleting go back to App.svelte — both are also
  // control-API actions, and deleting asks for confirmation there.
  import type { domain } from '../../wailsjs/go/models'
  import { methodColor, methodLabel } from '../lib/format'
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
  export let onDuplicate: (item: domain.Item) => void
  export let onDelete: (item: domain.Item) => void

  // Matched against the name and the method, because "post" is as
  // likely a search as a word in a name. Case-insensitive, no globbing:
  // this is a way to find one row in forty, not a query language.
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

  <ul class="request-list scroll-pane">
    {#each shown as item (item.id)}
      <li class:active={item.id === selectedItemId} style="--m: {methodColor(item.method || 'GET')}">
        <button class="request-select" title="{item.method || 'GET'} {item.name}" on:click={() => onSelect(item)}>
          <span class="method-tag">{methodLabel(item.method)}</span>
          <span class="truncate">{item.name}</span>
        </button>
        <!-- Over the end of the row rather than in it: at rest the name
             gets the whole width, and these two only take space back
             when the row is the one being pointed at. -->
        <span class="row-actions">
          <button class="icon-btn" title="Duplicate request" on:click={() => onDuplicate(item)}>⧉</button>
          <button class="icon-btn" title="Delete request" on:click={() => onDelete(item)}>×</button>
        </span>
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
    position: relative;
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

  /* Hidden until the row is pointed at, keyboard-focused, or selected —
     but still in the accessibility tree and still tabbable, so this is
     opacity rather than display. The fade on the left keeps a long name
     from running into them. */
  .row-actions {
    position: absolute;
    right: 0.35rem;
    top: 50%;
    transform: translateY(-50%);
    display: flex;
    gap: 0.15rem;
    padding-left: 1.25rem;
    background: linear-gradient(to right, transparent, var(--fm-bg-hover) 1.25rem);
    opacity: 0;
  }

  .request-list li:hover .row-actions,
  .request-list li.active .row-actions,
  .row-actions:focus-within {
    opacity: 1;
  }

  /* Every badge the same width, so the names start in a column. 4ch
     plus a little, because methodLabel caps the text at four characters
     (see lib/format.ts). */
  .method-tag {
    font-size: 0.7rem;
    font-weight: 600;
    color: var(--m, var(--fm-text-muted));
    width: 2.6rem;
    flex-shrink: 0;
  }
</style>
