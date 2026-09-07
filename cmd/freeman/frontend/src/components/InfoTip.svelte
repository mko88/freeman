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
  // Placement, dismissal and staying out of a scrolling ancestor's way
  // are Popover's job — see it for why the panel is fixed rather than
  // absolute.
  //
  // No ui:action for it. It changes nothing about the request or the
  // app — it shows fixed text — so there is nothing for a script to
  // drive or read back, unlike the collapse toggles and menus that do
  // carry state.
  import Popover from './Popover.svelte'

  export let label: string

  let open = false
  let button: HTMLButtonElement
</script>

<span class="info">
  <button
    class="info-btn"
    type="button"
    bind:this={button}
    title={label}
    aria-label={label}
    aria-expanded={open}
    on:click|preventDefault|stopPropagation={() => (open = !open)}>i</button
  >

  <Popover bind:open anchor={button} role="note">
    <div class="info-pop"><slot /></div>
  </Popover>
</span>

<style>
  .info {
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

  /* The same fill, border and shadow as .dropdown-menu — one
     floating-panel look — but sized for a sentence rather than a list of
     choices. */
  .info-pop {
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
  }
</style>
