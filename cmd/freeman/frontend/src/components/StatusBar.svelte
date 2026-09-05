<script lang="ts">
  // The strip along the bottom: where the control API is listening, and
  // a collapsible log of the ui:action events it has delivered. App.svelte
  // owns the entries (logEvent appends from all over); this owns the
  // element they render into, and the scrolling that goes with it.
  import { tick } from 'svelte'

  export let controlApiAddr: string
  export let showLog: boolean
  export let height: number
  export let entries: { time: string; text: string }[]
  export let onToggle: () => void

  let logEl: HTMLElement | undefined
  let seen = 0

  // Follow the tail only when something new arrives — not on every
  // update, which would yank the view back down while someone is
  // scrolled up reading.
  $: if (entries.length !== seen) {
    seen = entries.length
    tick().then(() => {
      if (logEl) logEl.scrollTop = logEl.scrollHeight
    })
  }
</script>

<footer class="status-bar" style={showLog ? `height: ${height}px` : ''}>
  <div class="status-bar-header">
    <button class="icon-btn" title={showLog ? 'Collapse the log' : 'Expand the log'} on:click={onToggle}
      >{showLog ? '▾' : '▸'}</button
    >
    <span>
      {#if controlApiAddr}
        Control API: <code>http://{controlApiAddr}</code>
      {:else}
        Control API: desktop build only
      {/if}
    </span>
  </div>
  {#if showLog}
    <div class="status-log" bind:this={logEl}>
      {#if entries.length === 0}
        <div class="status-line muted">No control-API events yet.</div>
      {:else}
        {#each entries as entry}
          <div class="status-line"><span class="status-time">{entry.time}</span>{entry.text}</div>
        {/each}
      {/if}
    </div>
  {/if}
</footer>

<style>
  /* No fixed height here — set inline from statusBarHeight while the log
     is expanded (via the splitter above it); collapsed, it's left unset
     so the footer just shrinks to fit .status-bar-header alone. */
  .status-bar {
    flex: none;
    display: flex;
    flex-direction: column;
    border-top: 1px solid var(--fm-border);
    background: var(--fm-bg-panel);
    text-align: left;
  }

  .status-bar-header {
    flex: none;
    display: flex;
    align-items: center;
    gap: 0.4rem;
    padding: 4px 10px;
    font-size: 0.75rem;
    color: var(--fm-text-muted);
    border-bottom: 1px solid var(--fm-border-subtle);
  }

  .status-bar-header code {
    color: var(--fm-text);
  }

  .status-log {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 4px 10px;
    font-family: 'IBM Plex Mono', 'Cascadia Code', Consolas, monospace;
    font-size: 0.72rem;
  }

  .status-line {
    padding: 1px 0;
    white-space: pre-wrap;
    word-break: break-word;
  }

  .status-line.muted {
    color: var(--fm-text-muted);
  }

  .status-time {
    color: var(--fm-text-muted);
    opacity: 0.8;
    margin-right: 8px;
  }
</style>
