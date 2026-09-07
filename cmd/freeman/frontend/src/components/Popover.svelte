<script lang="ts">
  // A panel that floats above the page, anchored to the control that
  // opened it: the collection picker's menu, the response pane's "..."
  // menu, an InfoTip's explanation.
  //
  // It owns the behaviour every one of those needs and nothing about how
  // they look — the caller puts its own element in the slot. That split
  // is deliberate: Svelte scopes styles to the component that writes the
  // markup, so a menu's item styling has to live with the menu, while
  // placement and dismissal are the same everywhere and belong here.
  //
  // Fixed positioning, measured from the anchor, rather than absolute
  // inside it. An absolutely positioned panel is clipped by any ancestor
  // that scrolls — and overflow-y: auto computes overflow-x to auto with
  // it, so such an ancestor clips on all four sides. That bug reached
  // the Options tab once already; this is the shape that can't have it.
  import { tick } from 'svelte'

  // Bound: the caller owns whether it's open, because that state is
  // often mirrored in GET /api/ui/state and driven by a ui:action.
  export let open = false
  // The control it hangs from. Measured on open, so it can be bound
  // after this component is created.
  export let anchor: HTMLElement | undefined = undefined
  // Which edge of the anchor the panel lines up with when there's room
  // for it. It flips regardless when there isn't.
  export let align: 'start' | 'end' = 'start'
  // Announced to assistive tech: a list of choices or a note.
  export let role: 'menu' | 'note' = 'menu'

  // Clear of the window edges, and clear of the anchor.
  const EDGE = 8
  const GAP = 6

  let panel: HTMLElement
  let x = 0
  let y = 0
  // The panel has to be in the DOM to be measured, so it renders before
  // it has anywhere to be. This keeps it invisible for that one frame.
  let placed = false

  export function close() {
    open = false
  }

  $: if (open) void place()

  async function place() {
    placed = false
    await tick()
    if (!anchor || !panel) return
    const a = anchor.getBoundingClientRect()
    const p = panel.getBoundingClientRect()
    const preferred = align === 'end' ? a.right - p.width : a.left
    x = Math.max(EDGE, Math.min(preferred, window.innerWidth - p.width - EDGE))
    y = a.bottom + GAP
    if (y + p.height > window.innerHeight - EDGE) {
      y = Math.max(EDGE, a.top - p.height - GAP)
    }
    placed = true
  }

  // Any scroll moves the anchor out from under a fixed panel, so close
  // rather than chase it. Capture, because a scroll inside .request-pane
  // or .settings-body doesn't bubble to the window.
  function onScroll() {
    if (open) close()
  }

  function onKeydown(e: KeyboardEvent) {
    if (open && e.key === 'Escape') close()
  }
</script>

<svelte:window on:resize={onScroll} on:scroll|capture={onScroll} on:keydown={onKeydown} />

{#if open}
  <!-- Catches the click that dismisses it, and stops that click reaching
       whatever is underneath. -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <div class="popover-backdrop" on:click|preventDefault|stopPropagation={close}></div>
  <!-- Clicks stop here too: a popover can sit inside a <label>, and a
       click that reached it would activate the field the label is for. -->
  <!-- svelte-ignore a11y-no-static-element-interactions -->
  <!-- svelte-ignore a11y-click-events-have-key-events -->
  <div
    class="popover"
    class:placed
    {role}
    bind:this={panel}
    style="left: {x}px; top: {y}px"
    on:click|stopPropagation
  >
    <slot {close} />
  </div>
{/if}

<style>
  /* Covers the window so the next click anywhere dismisses the panel.
     Below it in the stack, above everything else. */
  .popover-backdrop {
    position: fixed;
    inset: 0;
    z-index: 50;
  }

  .popover {
    position: fixed;
    z-index: 51;
    /* Rendered so it can be measured, shown once it has been placed. */
    opacity: 0;
  }

  .popover.placed {
    opacity: 1;
  }
</style>
