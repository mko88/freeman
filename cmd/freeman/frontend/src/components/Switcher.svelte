<script lang="ts">
  import Popover from './Popover.svelte'

  // A picker for one of the two things a workspace holds many of: the
  // open collection, above the request list it fills, and the active
  // environment, in the top bar. Both are the same shape — a named list
  // you pick one of — so they're one component rather than two that
  // drift.
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

  // Which edge the menu hangs from, and whether the button fills its
  // container. The top bar wants a button sized to its name with the
  // menu hanging right; the sidebar wants the full column width with
  // the menu under the left edge.
  export let align: 'left' | 'right' = 'right'
  export let block = false

  let button: HTMLButtonElement

  $: current = items.find((i) => i.id === selectedId)
</script>

<div class="switcher" class:block>
  <!-- The name alone, no label beside it: two of these sit together and
       the tooltip says which is which, rather than spending bar width on
       a word that never changes. -->
  <button
    class="switcher-button"
    bind:this={button}
    title={label}
    aria-label={label}
    aria-expanded={open}
    on:click={() => (open = !open)}
  >
    <span class="switcher-name truncate">{current?.name ?? emptyName}</span>
    <span class="switcher-caret">▾</span>
  </button>

  <Popover bind:open anchor={button} align={align === 'left' ? 'start' : 'end'}>
    <div class="dropdown-menu">
      <!-- The way out, before the list: it leaves this menu for the
           settings window rather than doing something to it, so it reads
           first and is fenced off below. -->
      <button
        class="switcher-escape"
        on:click={() => {
          open = false
          onEdit()
        }}>Edit {label.toLowerCase()}s…</button
      >
      <div class="dropdown-divider"></div>
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
    </div>
  </Popover>
</div>

<style>
  .switcher {
    display: inline-flex;
  }

  /* Fills its container rather than sizing to the name — the sidebar
     gives it a whole column, and a button floating at the left of an
     empty header reads as unrelated to the list under it. */
  .switcher.block,
  .switcher.block .switcher-button {
    width: 100%;
  }

  .switcher.block .switcher-button {
    justify-content: space-between;
    padding-left: 0;
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
    flex: none;
    font-size: 0.7rem;
    color: var(--fm-text-muted);
  }

  /* The open one is marked by the accent rail, not a tick: the request
     list already uses a leading rail for "this is the selected row". */
  .switcher-item.current {
    color: var(--fm-text);
    box-shadow: inset 2px 0 0 var(--fm-accent);
  }

  /* The shortcut out of the menu. Set apart from the names below it by
     weight rather than colour — the accent is spent on which item is
     current, and two accents in a menu this small would compete. */
  .switcher-escape {
    color: var(--fm-text-muted);
    font-size: 0.8rem;
  }

  .switcher-escape:hover {
    color: var(--fm-text);
  }

  /* A rule with air around it, not just another hairline: consecutive
     items already have one between each pair, so a third identical line
     would read as one more item boundary. The margin is transparent, so
     the menu's own background shows through it. */
  .dropdown-divider {
    height: 1px;
    margin: 0.3rem 0;
    background: var(--fm-border);
  }
</style>
