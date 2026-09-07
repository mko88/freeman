<script lang="ts">
  // A small "i" beside a label, opening the explanation that would
  // otherwise sit under every field. One control wherever the app has
  // something to explain, so a form reads as a list of settings and the
  // prose is there when it's wanted.
  //
  // Deliberately not a hover tooltip: these explain what a setting does
  // to your requests, which is worth being able to read at your own
  // pace, select, and dismiss on purpose.
  //
  // No ui:action for it. It changes nothing about the request or the
  // app — it shows fixed text — so there is nothing for a script to
  // drive or read back, unlike the collapse toggles and menus that do
  // carry state.
  import { tick } from 'svelte'

  export let label: string

  let open = false
  // Positioned in viewport coordinates rather than relative to the
  // button, because the popup is `position: fixed` — see the note on
  // .info-pop. `placed` keeps it invisible for the frame between being
  // rendered (so it can be measured) and being put somewhere.
  let button: HTMLButtonElement
  let popup: HTMLElement
  let x = 0
  let y = 0
  let placed = false

  // Keeps the popup off the window edges, and off the button.
  const EDGE = 8
  const GAP = 6

  async function toggle() {
    open = !open
    if (!open) return
    placed = false
    await tick()
    place()
    placed = true
  }

  function place() {
    if (!button || !popup) return
    const b = button.getBoundingClientRect()
    const p = popup.getBoundingClientRect()
    // Left edges aligned, flipping to stay inside the window rather than
    // taking an `align` prop — the caller shouldn't have to know which
    // side of the screen its field ended up on.
    x = Math.max(EDGE, Math.min(b.left, window.innerWidth - p.width - EDGE))
    y = b.bottom + GAP
    if (y + p.height > window.innerHeight - EDGE) {
      y = Math.max(EDGE, b.top - p.height - GAP)
    }
  }

  // Any scroll moves the button out from under a fixed popup, so close
  // rather than chase it. Capture, because a scroll inside .request-pane
  // or .settings-body doesn't bubble to the window.
  function close() {
    open = false
  }
</script>

<svelte:window on:resize={close} on:scroll|capture={close} />

<span class="info">
  <button
    class="info-btn"
    type="button"
    bind:this={button}
    title={label}
    aria-label={label}
    aria-expanded={open}
    on:keydown={(e) => e.key === 'Escape' && close()}
    on:click|preventDefault|stopPropagation={toggle}>i</button
  >

  {#if open}
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <!-- svelte-ignore a11y-click-events-have-key-events -->
    <div class="menu-backdrop" on:click|preventDefault|stopPropagation={close}></div>
    <!-- Clicks stop here: an InfoTip can sit inside a <label>, and a
         click that reached it would activate the field the label is
         for. Escape closes it from the button, which still has focus. -->
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <!-- svelte-ignore a11y-click-events-have-key-events -->
    <div
      class="info-pop"
      class:placed
      bind:this={popup}
      style="left: {x}px; top: {y}px"
      on:click|stopPropagation
    >
      <slot />
    </div>
  {/if}
</span>

<style>
  .info {
    position: relative;
    display: inline-flex;
    vertical-align: middle;
  }

  /* Round rather than the app's usual square control, so it reads as a
     marker on the label rather than another button in the row. */
  .info-btn {
    width: 1.05rem;
    height: 1.05rem;
    padding: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 50%;
    border: 1px solid var(--fm-border);
    background: none;
    color: var(--fm-text-muted);
    font-size: 0.65rem;
    font-style: italic;
    line-height: 1;
  }

  .info-btn:hover {
    color: var(--fm-text);
    border-color: var(--fm-text-muted);
    background: none;
  }

  /* Fixed, not absolute: every place an InfoTip is used sits inside a
     scroll container (.request-pane, .settings-body), and an absolutely
     positioned popup is clipped by one — overflow-y: auto computes
     overflow-x to auto with it, so it clips on all four sides. Fixed
     positioning escapes the ancestor entirely; place() supplies the
     coordinates that would otherwise come for free.
     Same fill, border and shadow as .dropdown-menu — one floating-panel
     look — but sized for a sentence rather than a list of choices. */
  .info-pop {
    position: fixed;
    z-index: 60;
    width: max-content;
    max-width: min(22rem, calc(100vw - 2rem));
    padding: 0.5rem 0.6rem;
    font-size: 0.78rem;
    font-style: normal;
    line-height: 1.5;
    text-align: left;
    color: var(--fm-text);
    background-color: var(--fm-bg);
    background-image: linear-gradient(var(--fm-bg-elevated), var(--fm-bg-elevated));
    border: 1px solid var(--fm-border);
    border-radius: var(--fm-radius);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.3);
    /* Rendered so it can be measured, shown once it has been placed. */
    opacity: 0;
  }

  .info-pop.placed {
    opacity: 1;
  }
</style>
