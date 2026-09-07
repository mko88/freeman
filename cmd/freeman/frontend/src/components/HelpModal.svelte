<script lang="ts">
  // The in-app reference for the control API (see CLAUDE.md's standing
  // rule). The two tables it renders live in lib/controlApiCatalog.ts,
  // because App.svelte reports them to the Go side for GET /api/agent
  // and consistency.py parses them as source — this component is only
  // one of three readers now.
  import { methodColor } from '../lib/format'
  import { apiEndpoints, uiActions } from '../lib/controlApiCatalog'

  export let controlApiAddr: string
  export let onClose: () => void
</script>

<!-- svelte-ignore a11y-click-events-have-key-events -->
<div
  class="modal-backdrop"
  role="presentation"
  on:click={() => onClose()}
  on:keydown={(e) => e.key === 'Escape' && onClose()}
>
  <div
    class="modal help-modal"
    role="dialog"
    aria-modal="true"
    aria-labelledby="help-title"
    tabindex="-1"
    on:click|stopPropagation
    on:keydown={(e) => e.key === 'Escape' && onClose()}
  >
    <div class="modal-header">
      <h2 id="help-title">Control API</h2>
      <button class="icon-btn" title="Close" on:click={() => onClose()}>×</button>
    </div>
    <p class="hint prose">
      {#if controlApiAddr}
        Base URL: <code>http://{controlApiAddr}</code> — no auth, loopback-only. Requests with a body must
        send <code>Content-Type: application/json</code>, and anything a browser marks as coming from
        another site is refused — that's what stops a web page you have open from driving this.
      {:else}
        Only available in the desktop build.
      {/if}
    </p>

    <h3>Endpoints</h3>
    <div class="help-list">
      {#each apiEndpoints as e}
        <details class="help-entry">
          <summary>
            <span class="help-method" style="color: {methodColor(e.method)}">{e.method}</span>
            <code>{e.path}</code>
          </summary>
          <p class="prose help-desc">{e.desc}</p>
        </details>
      {/each}
    </div>

    <h3>UI actions (via POST /api/ui/action)</h3>
    <div class="help-list">
      {#each uiActions as a}
        <details class="help-entry">
          <summary>
            <code>{a.action}</code>
            <span class="help-payload">{a.payload}</span>
          </summary>
          <p class="prose help-desc">{a.desc}</p>
        </details>
      {/each}
    </div>
  </div>
</div>

<style>
  /* Wider (more room for Path/Description before either wraps) and a
     taller cap — with the two reference tables below sized to their own
     content instead of fighting the layout, this fits without scrolling
     at any normal window size; overflow-y stays as a safety net only,
     not the expected outcome. */
  .help-modal {
    width: min(960px, 94vw);
    max-height: 94vh;
  }

  /* The help modal's endpoint / ui:action reference: each row is a
     collapsed <details> showing just the method+path (or action+payload)
     — the prose description is revealed on click, so the whole list
     scans at a glance first. */
  .help-list {
    border: 1px solid var(--fm-border-subtle);
    border-radius: var(--fm-radius);
    overflow: hidden;
  }

  .help-entry + .help-entry {
    border-top: 1px solid var(--fm-border-subtle);
  }

  .help-entry > summary {
    display: flex;
    align-items: baseline;
    gap: 0.6rem;
    padding: 0.4rem 0.6rem;
    font-size: 0.8rem;
    cursor: pointer;
    user-select: none;
    list-style: none;
  }

  .help-entry > summary::-webkit-details-marker {
    display: none;
  }

  /* Same ▸/▾ collapse glyph the tabs and the log panel use. */
  .help-entry > summary::before {
    content: '▸';
    color: var(--fm-text-muted);
    font-size: 0.7em;
  }

  .help-entry[open] > summary::before {
    content: '▾';
  }

  .help-entry > summary:hover {
    background: var(--fm-bg-hover);
  }

  .help-method {
    font-weight: 600;
    min-width: 3.25rem;
  }

  .help-payload {
    color: var(--fm-text-muted);
    overflow-wrap: anywhere;
  }

  .help-desc {
    margin: 0;
    padding: 0.1rem 0.6rem 0.55rem 1.85rem;
    font-size: 0.8rem;
    color: var(--fm-text-muted);
  }
</style>
