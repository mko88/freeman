<script lang="ts">
  // A top-bar picker for one of the two things a workspace holds many of:
  // the open collection, and the active environment. Both are the same
  // shape — a named list you pick one of, plus create/rename/delete — so
  // they're one component rather than two that drift.
  //
  // Everything here goes back to App.svelte, which owns the state and the
  // backend calls, so a click and a ui:action take the same path.
  export let label: string
  export let items: { id: string; name: string }[]
  export let selectedId: string
  export let emptyName: string

  // Bound: App.svelte mirrors this in GET /api/ui/state and the control
  // API can toggle it, so writes here have to travel back up.
  export let open: boolean

  export let onSelect: (id: string) => void
  export let onNew: () => void
  export let onRename: () => void
  export let onDelete: () => void

  $: current = items.find((i) => i.id === selectedId)
  // "collection" / "environment" — the menu names the thing it acts on
  // rather than saying "New…", which would leave two identical menus.
  $: noun = label.toLowerCase()
</script>

<div class="switcher">
  <button class="switcher-button" aria-expanded={open} on:click={() => (open = !open)}>
    <!-- The label earns its place here: two adjacent pickers showing
         names the user chose ("My Requests", "Development") are not
         otherwise distinguishable. -->
    <span class="switcher-label">{label}</span>
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
          onNew()
        }}>New {noun}</button
      >
      <button
        on:click={() => {
          open = false
          onRename()
        }}>Rename {noun}</button
      >
      <button
        on:click={() => {
          open = false
          onDelete()
        }}>Delete {noun}</button
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
  }

  .switcher-button:hover {
    background: var(--fm-bg-hover);
    border-color: transparent;
  }

  .switcher-label {
    font-size: 0.72rem;
    color: var(--fm-text-muted);
  }

  .switcher-name {
    font-size: 0.85rem;
  }

  .switcher-caret {
    font-size: 0.7rem;
    color: var(--fm-text-muted);
  }

  /* The open one is marked by weight and the accent rail, not a tick:
     the app already uses a leading rail for "this is the selected row"
     in the request list. */
  .switcher-item.current {
    color: var(--fm-text);
    box-shadow: inset 2px 0 0 var(--fm-accent);
  }

  /* Stronger than the hairline between items, because this break means
     something the others don't: above it you pick one, below it you act
     on the one you picked. */
  .dropdown-divider {
    height: 1px;
    background: var(--fm-border);
  }
</style>
