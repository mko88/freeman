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
  export let label: string
  // Which edge the popup hangs from. An InfoTip near the right edge of
  // its container would otherwise open off it and be clipped by the
  // nearest scroll container.
  export let align: 'left' | 'right' = 'left'

  let open = false
</script>

<span class="info">
  <button
    class="info-btn"
    type="button"
    title={label}
    aria-label={label}
    aria-expanded={open}
    on:keydown={(e) => e.key === 'Escape' && (open = false)}
    on:click|preventDefault|stopPropagation={() => (open = !open)}>i</button
  >

  {#if open}
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <!-- svelte-ignore a11y-click-events-have-key-events -->
    <div class="menu-backdrop" on:click|preventDefault|stopPropagation={() => (open = false)}></div>
    <!-- Clicks stop here: an InfoTip can sit inside a <label>, and a
         click that reached it would activate the field the label is
         for. Escape closes it from the button, which still has focus. -->
    <!-- svelte-ignore a11y-no-static-element-interactions -->
    <!-- svelte-ignore a11y-click-events-have-key-events -->
    <div class="info-pop" class:right={align === 'right'} on:click|stopPropagation><slot /></div>
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

  /* Same fill, border and shadow as .dropdown-menu — one floating-panel
     look — but sized for a sentence rather than a list of choices. */
  .info-pop {
    position: absolute;
    top: calc(100% + 0.35rem);
    left: 0;
    z-index: 10;
    width: max-content;
    max-width: 22rem;
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
  }

  .info-pop.right {
    left: auto;
    right: 0;
  }
</style>
