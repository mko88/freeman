<script lang="ts">
  // A top-bar picker for one of the two things a workspace holds many of:
  // the open collection, and the active environment. Both are the same
  // shape — a named list you pick one of — so they're one component
  // rather than two that drift.
  //
  // Picking only. Creating, renaming and deleting live in the settings
  // window, which this links to: those are occasional, and putting them
  // in a menu you open several times an hour puts Delete one slip away
  // from Switch.
  //
  // Everything goes back to App.svelte, which owns the state and the
  // backend calls, so a click and a ui:action take the same path.
  export let label: string
  export let items: { id: string; name: string }[]
  export let selectedId: string
  export let emptyName: string

  // Bound: App.svelte mirrors this in GET /api/ui/state and the control
  // API can toggle it, so writes here have to travel back up.
  export let open: boolean

  export let onSelect: (id: string) => void
  export let onEdit: () => void

  $: current = items.find((i) => i.id === selectedId)
</script>

<div class="switcher">
  <!-- The name alone, no label beside it: two of these sit together and
       the tooltip says which is which, rather than spending bar width on
       a word that never changes. -->
  <button class="switcher-button" title={label} aria-label={label} aria-expanded={open} on:click={() => (open = !open)}>
    <span class="switcher-name">{current?.name ?? emptyName}</span>
    <span class="switcher-caret">▾</span>
  </button>

  {#if open}
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <!-- svelte-ignore a11y-click-events-have-key-events -->
    <div class="menu-backdrop" on:click={() => (open = false)}></div>
    <div class="dropdown-menu">
      {#each items as item (item.id)}
        <button
          class="switcher-item"
          class:current={item.id === selectedId}
          on:click={() => {
            open = false
            onSelect(item.id)
          }}
        >
          {item.name}
        </button>
      {/each}
      <div class="dropdown-divider"></div>
      <button
        on:click={() => {
          open = false
          onEdit()
        }}>Edit {label.toLowerCase()}s…</button
      >
    </div>
  {/if}
</div>

<style>
  .switcher {
    position: relative;
  }

  .switcher-button {
    display: flex;
    align-items: baseline;
    gap: 0.5rem;
    background: none;
    border-color: transparent;
    padding: 0.25rem 0.5rem;
    font-size: 0.85rem;
  }

  .switcher-button:hover {
    background: var(--fm-bg-hover);
    border-color: transparent;
  }

  .switcher-caret {
    font-size: 0.7rem;
    color: var(--fm-text-muted);
  }

  /* The open one is marked by the accent rail, not a tick: the request
     list already uses a leading rail for "this is the selected row". */
  .switcher-item.current {
    color: var(--fm-text);
    box-shadow: inset 2px 0 0 var(--fm-accent);
  }

  /* Stronger than the hairline between items, because this break means
     something the others don't: above it you pick one, below it you
     leave for the window that changes them. */
  .dropdown-divider {
    height: 1px;
    background: var(--fm-border);
  }
</style>
