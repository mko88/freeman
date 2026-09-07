<script lang="ts">
  // The sidebar: the open collection's saved requests, colour-coded by
  // method. Selecting and deleting go back to App.svelte — both are also
  // control-API actions, and deleting asks for confirmation there.
  import type { domain } from '../../wailsjs/go/models'
  import { methodColor } from '../lib/format'
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

  export let onNew: () => void
  export let onSelect: (item: domain.Item) => void
  export let onDelete: (item: domain.Item) => void
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
  <ul class="request-list scroll-pane">
    {#each collection?.items ?? [] as item (item.id)}
      <li class:active={item.id === selectedItemId} style="--m: {methodColor(item.method || 'GET')}">
        <button class="request-select" on:click={() => onSelect(item)}>
          <span class="method-tag">{item.method || 'GET'}</span>
          <span class="truncate">{item.name}</span>
        </button>
        <button class="icon-btn" title="Delete request" on:click={() => onDelete(item)}>×</button>
      </li>
    {/each}
  </ul>
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
</style>
