<script lang="ts">
  // The settings window: a Workspace tab (which folder is open, and the
  // response cache that lives inside it) and an Environments tab.
  //
  // Presentation only. Every mutation is a callback into App.svelte,
  // because the control API drives the same operations through
  // dispatchUIAction — the state and the functions that change it have
  // to stay in one place, or a scripted selectEnvironment and a clicked
  // one would take different paths.
  import InfoTip from './InfoTip.svelte'
  import type { core, domain, settings as settings_ } from '../../wailsjs/go/models'

  export let workspace: core.WorkspaceInfo
  export let openError: string
  export let responseCacheCleared: boolean

  // Bound, not one-way: the tab strip and the environment picker are
  // edited here, and the variable rows below write straight into
  // `environment` — App.svelte's reportUIState mirrors all three, so the
  // changes have to travel back up.
  export let settingsTab: 'workspace' | 'collections' | 'environments'
  export let environmentId: string
  export let environment: domain.Environment | null
  // Only the id: the list names every collection from
  // workspace.collections, and uses this to mark which one the sidebar
  // is showing.
  export let collectionId: string

  export let onClose: () => void
  export let onOpenWorkspace: () => void
  export let onClearResponseCache: () => void

  // Bound, like `environment` above: App.svelte owns the value, mirrors
  // it in GET /api/ui/state, and saves it — the two byte fields are
  // edited in MiB here because nobody wants to type 67108864.
  export let settings: settings_.Settings
  export let onSaveSettings: () => void

  const MIB = 1024 * 1024
  // Derived one way only — reactive statements in both directions would
  // chase each other. The boxes are read from the settings; saveSizes
  // writes them back when one is edited.
  $: inlineMiB = Math.max(1, Math.round(settings.inlineResponseBytes / MIB))
  $: maxMiB = Math.max(1, Math.round(settings.maxResponseBytes / MIB))

  function saveSizes() {
    settings.inlineResponseBytes = inlineMiB * MIB
    settings.maxResponseBytes = maxMiB * MIB
    onSaveSettings()
  }

  // The environment mutations, grouped rather than passed as seven
  // separate props. App.svelte holds one stable object so this doesn't
  // see a new prop value on every render.
  export let env: {
    expand: (id: string) => void
    create: () => void
    rename: (id: string, value: string) => void
    commit: () => void
    confirmDelete: (summary: core.EnvironmentSummary) => void
    addVariable: () => void
    removeVariable: (index: number) => void
  }

  export let coll: {
    create: () => void
    rename: (id: string, value: string) => void
    confirmDelete: (summary: core.CollectionSummary) => void
  }
</script>

<!-- svelte-ignore a11y-click-events-have-key-events -->
<div
  class="modal-backdrop"
  role="presentation"
  on:click={onClose}
  on:keydown={(e) => e.key === 'Escape' && onClose()}
>
  <div
    class="modal settings-modal"
    role="dialog"
    aria-modal="true"
    aria-labelledby="settings-title"
    tabindex="-1"
    on:click|stopPropagation
    on:keydown={(e) => e.key === 'Escape' && onClose()}
  >
    <div class="modal-header">
      <h2 id="settings-title">Settings</h2>
      <button class="icon-btn" title="Close" on:click={onClose}>×</button>
    </div>

    <div class="tabs">
      <button class:active={settingsTab === 'workspace'} on:click={() => (settingsTab = 'workspace')}>Workspace</button>
      <button class:active={settingsTab === 'collections'} on:click={() => (settingsTab = 'collections')}
        >Collections</button
      >
      <button class:active={settingsTab === 'environments'} on:click={() => (settingsTab = 'environments')}
        >Environments</button
      >
    </div>

    <div class="settings-body scroll-pane">
      {#if settingsTab === 'workspace'}
        <!-- Setting, value, action — the same row rhythm as the two
             lists beside it, rather than paragraphs with buttons after
             them. -->
        <ul class="settings-list">
          <li class="settings-setting">
            <div class="settings-setting-text">
              <span class="settings-setting-name">Folder</span>
              <code class="workspace-path truncate">{workspace.root}</code>
            </div>
            <button on:click={onOpenWorkspace}>Change…</button>
          </li>
          <li class="settings-setting">
            <div class="settings-setting-text">
              <span class="settings-setting-name"
                >Response cache
                <InfoTip label="About the response cache">
                  Each request's last response, kept in <code>.cache/responses</code> so reopening it shows what it
                  returned.
                </InfoTip>
              </span>
            </div>
            <button on:click={onClearResponseCache}>{responseCacheCleared ? 'Cleared' : 'Clear'}</button>
          </li>
        </ul>

        <!-- The four values that used to be constants in
             internal/httpengine. They live in the workspace's
             settings.yaml, so a workspace shared through git carries
             them. -->
        <h3 class="settings-group">Defaults for new requests</h3>
        <ul class="settings-list">
          <li class="settings-setting">
            <span class="settings-setting-name"
              >Request timeout
              <InfoTip label="About the request timeout">
                How long a request may take before it gives up, unless it sets its own in the Options tab.
                0 waits forever.
              </InfoTip>
            </span>
            <span class="settings-value">
              <input type="number" min="0" step="500" bind:value={settings.requestTimeoutMs} on:change={onSaveSettings} />
              <span class="settings-unit">ms</span>
            </span>
          </li>
          <li class="settings-setting">
            <span class="settings-setting-name"
              >Maximum redirects
              <InfoTip label="About the redirect limit">
                How many redirects a request follows, unless it sets its own. 0 follows without limit —
                only the timeout ends a redirect loop.
              </InfoTip>
            </span>
            <span class="settings-value">
              <input type="number" min="0" bind:value={settings.maxRedirects} on:change={onSaveSettings} />
            </span>
          </li>
        </ul>

        <h3 class="settings-group">Response limits</h3>
        <ul class="settings-list">
          <li class="settings-setting">
            <span class="settings-setting-name"
              >Show inline up to
              <InfoTip label="About the inline limit">
                Bodies bigger than this aren't rendered in the response pane. They're still cached in
                full, and the ⋯ menu opens the file.
              </InfoTip>
            </span>
            <span class="settings-value">
              <input type="number" min="1" bind:value={inlineMiB} on:change={saveSizes} />
              <span class="settings-unit">MiB</span>
            </span>
          </li>
          <li class="settings-setting">
            <span class="settings-setting-name"
              >Read at most
              <InfoTip label="About the response ceiling">
                The hard ceiling on how much of a response is read at all. Past it the body comes back
                truncated rather than filling memory — a compressed payload can expand a long way.
              </InfoTip>
            </span>
            <span class="settings-value">
              <input type="number" min="1" bind:value={maxMiB} on:change={saveSizes} />
              <span class="settings-unit">MiB</span>
            </span>
          </li>
        </ul>
        {#if openError}<p class="error">{openError}</p>{/if}
      {:else if settingsTab === 'collections'}
        <!-- A row per collection, not a picker and a form: this tab
             manages the set, and every name is editable where it sits.
             The rail marks the one the sidebar is showing — read-only
             here, since the top bar is what changes it. -->
        <ul class="settings-list">
          {#each workspace.collections as c (c.id)}
            <li class:current={c.id === collectionId}>
              <!-- on:change, not on:input: renaming moves the
                   collection's folder, so it commits when you leave the
                   field or press Enter, not once per keystroke. -->
              <input
                class="settings-list-name"
                type="text"
                value={c.name}
                on:change={(e) => coll.rename(c.id, e.currentTarget.value)}
                aria-label="Collection name"
              />
              <span class="settings-list-count"
                >{c.itemCount || 'empty'}{c.itemCount ? ` request${c.itemCount === 1 ? '' : 's'}` : ''}</span
              >
              <button
                class="icon-btn"
                title="Delete collection"
                disabled={workspace.collections.length <= 1}
                on:click={() => coll.confirmDelete(c)}>×</button
              >
            </li>
          {/each}
        </ul>
        <button class="settings-list-add" on:click={coll.create}>+ New collection</button>
      {:else}
        <!-- The same list, with each environment's variables opening
             under the row they belong to. Opening one is not selecting
             it: the rail still marks whichever the top bar has active. -->
        <ul class="settings-list">
          {#each workspace.environments as e (e.id)}
            <li class:current={e.id === environmentId} class:open={environment?.id === e.id}>
              <!-- The same disclosure the request editor, the response
                   pane and the control API log use. Expanding is its own
                   control, so the name stays a name — editable whether
                   the row is open or not, exactly like a collection's. -->
              <button
                class="disclosure"
                title={environment?.id === e.id ? 'Hide variables' : 'Show variables'}
                aria-expanded={environment?.id === e.id}
                on:click={() => env.expand(e.id)}>{environment?.id === e.id ? '▾' : '▸'}</button
              >
              <input
                class="settings-list-name"
                type="text"
                value={e.name}
                on:change={(ev) => env.rename(e.id, ev.currentTarget.value)}
                aria-label="Environment name"
              />
              <span class="settings-list-count">{e.variableCount || 'empty'}{e.variableCount ? ' variables' : ''}</span>
              <button
                class="icon-btn"
                title="Delete environment"
                disabled={workspace.environments.length <= 1}
                on:click={() => env.confirmDelete(e)}>×</button
              >
            </li>

            {#if environment?.id === e.id}
              <li class="settings-list-detail">
                <table class="kv-table">
                  <thead>
                    <tr><th></th><th>Key</th><th>Value</th><th>Secret</th><th></th></tr>
                  </thead>
                  <tbody>
                    {#each environment.variables as v, i}
                      <tr>
                        <td><input type="checkbox" bind:checked={v.enabled} on:change={env.commit} /></td>
                        <td><input type="text" bind:value={v.key} placeholder="key" on:change={env.commit} /></td>
                        <td><input type="text" bind:value={v.value} placeholder="value" on:change={env.commit} /></td>
                        <td><input type="checkbox" bind:checked={v.secret} on:change={env.commit} /></td>
                        <td>
                          <button class="icon-btn kv-remove-btn" on:click={() => env.removeVariable(i)}>×</button>
                        </td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
                <button class="settings-list-add" on:click={env.addVariable}>+ Add variable</button>
              </li>
            {/if}
          {/each}
        </ul>
        <button class="settings-list-add" on:click={env.create}>+ New environment</button>
      {/if}
    </div>
  </div>
</div>

<style>
  /* Same footprint as the help modal, but a fixed height so it doesn't
     jump around between tabs, and a flex column so the header and tab
     bar stay put while only .settings-body scrolls. */
  .settings-modal {
    width: min(960px, 94vw);
    height: min(680px, 90vh);
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .settings-modal > .modal-header,
  .settings-modal > .tabs {
    flex: none;
  }

  .settings-modal .tabs {
    margin-bottom: 0.75rem;
  }

  /* overflow-y makes this a scroll container, and a scroll container
     clips at its own edges — including horizontally, since overflow-x
     computes to auto alongside it. Anything flush against the left edge
     then loses part of its focus ring, which sits 3px outside the box
     (a 2px outline at 1px offset). The padding gives the ring room and
     the matching negative margin puts the content back where it was;
     both fit inside .modal's own 1.25rem padding. */
  .settings-body {
    flex: 1;
    min-height: 0;
  }

  .workspace-path {
    flex: 1;
  }

  /* One row per thing, across all three tabs: what it is on the left,
     what's in it in the middle, what you can do to it on the right. The
     tabs then differ in content rather than in shape. */
  .settings-list {
    list-style: none;
    margin: 0;
    padding: 0;
    border: 1px solid var(--fm-border-subtle);
    border-radius: var(--fm-radius);
  }

  /* The ends are rounded by rounding the end rows rather than clipping
     the list: overflow: hidden here would also clip anything a row opens
     outside itself, like an InfoTip's popup. */
  .settings-list > li:first-child {
    border-start-start-radius: var(--fm-radius);
    border-start-end-radius: var(--fm-radius);
  }

  .settings-list > li:last-child {
    border-end-start-radius: var(--fm-radius);
    border-end-end-radius: var(--fm-radius);
  }

  .settings-list > li {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.4rem 0.6rem;
  }

  .settings-list > li + li {
    border-top: 1px solid var(--fm-border-subtle);
  }

  /* The leading rail marks what's active — the same mark the request
     list and the switcher menu use. It's shown, not set: the top bar is
     what changes it, and having two controls for one choice was the
     reason this tab was rebuilt. */
  .settings-list > li.current {
    box-shadow: inset 2px 0 0 var(--fm-accent);
  }

  .settings-list > li.open {
    background: var(--fm-bg-hover);
  }

  /* Reads as text until focused, so a list of names looks like a list of
     names rather than a stack of form fields. */
  .settings-list-name {
    flex: 1;
    min-width: 0;
    background: none;
    border-color: transparent;
    text-align: left;
  }

  .settings-list-name:hover {
    border-color: var(--fm-border);
  }

  .settings-list-name:focus {
    background: var(--fm-bg-elevated);
    border-color: var(--fm-accent);
  }

  .settings-list-count {
    flex: none;
    font-size: 0.78rem;
    color: var(--fm-text-muted);
  }

  .settings-list-add {
    margin-top: 0.5rem;
    background: none;
    border-color: transparent;
    color: var(--fm-text-muted);
    font-size: 0.8rem;
  }

  .settings-list-add:hover {
    color: var(--fm-text);
    border-color: var(--fm-border);
  }

  /* An environment's variables, under the environment they belong to.
     Indented to the name column — the row's own padding, plus the
     chevron and the gap after it — so the table starts where the name
     above it does. */
  .settings-list-detail {
    display: block;
    padding: 0 0.6rem 0.6rem 2.85rem;
    background: var(--fm-bg-hover);
  }

  /* A setting rather than a named thing: its explanation sits under its
     name instead of a count sitting beside it. */
  .settings-setting-text {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    font-size: 0.8rem;
  }

  .settings-setting-name {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    font-size: 0.85rem;
  }

  /* A heading between lists, not above the first one — the Folder and
     Response cache rows need no label to be understood. */
  .settings-group {
    margin: 1.1rem 0 0.4rem;
    font-size: 0.78rem;
    font-weight: 600;
    color: var(--fm-text-muted);
  }

  .settings-value {
    display: flex;
    align-items: center;
    gap: 0.35rem;
    margin-left: auto;
  }

  .settings-value input {
    width: 7rem;
    text-align: right;
  }

  .settings-unit {
    font-size: 0.78rem;
    color: var(--fm-text-muted);
  }

  /* The Environments tab's table has a second narrow (checkbox) column —
     Secret — that isn't first or last, so it needs its own rule or it'd
     claim an even share of the remaining width like Key/Value do. */
  .settings-modal .kv-table th:nth-child(4),
  .settings-modal .kv-table td:nth-child(4) {
    width: 4.5rem;
  }
</style>
