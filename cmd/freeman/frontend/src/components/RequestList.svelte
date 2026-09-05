<script lang="ts">
  // The sidebar: the open collection's saved requests, colour-coded by
  // method. Selecting and deleting go back to App.svelte — both are also
  // control-API actions, and deleting asks for confirmation there.
  import type { domain } from '../../wailsjs/go/models'
  import { methodColor } from '../lib/format'

  export let collection: domain.Collection | null
  export let selectedItemId: string | null
  export let width: number

  export let onNew: () => void
  export let onSelect: (item: domain.Item) => void
  export let onDelete: (item: domain.Item) => void
</script>

<aside class="sidebar" style="width: {width}px">
  <div class="sidebar-header">
    <span>{collection?.name ?? ''}</span>
    <button class="icon-btn" title="New request" on:click={onNew}>+</button>
  </div>
  <ul class="request-list">
    {#each collection?.items ?? [] as item (item.id)}
      <li class:active={item.id === selectedItemId} style="--m: {methodColor(item.method || 'GET')}">
        <button class="request-select" on:click={() => onSelect(item)}>
          <span class="method-tag">{item.method || 'GET'}</span>
          <span class="request-select-name">{item.name}</span>
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
</style>
