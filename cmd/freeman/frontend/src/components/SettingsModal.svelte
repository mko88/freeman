<script lang="ts">
  // The settings window: a Workspace tab (which folder is open, and the
  // response cache that lives inside it) and an Environments tab.
  //
  // Presentation only. Every mutation is a callback into App.svelte,
  // because the control API drives the same operations through
  // dispatchUIAction — the state and the functions that change it have
  // to stay in one place, or a scripted selectEnvironment and a clicked
  // one would take different paths.
  import type { core, domain } from '../../wailsjs/go/models'

  export let workspace: core.WorkspaceInfo
  export let openError: string
  export let responseCacheCleared: boolean

  // Bound, not one-way: the tab strip and the environment picker are
  // edited here, and the variable rows below write straight into
  // `environment` — App.svelte's reportUIState mirrors all three, so the
  // changes have to travel back up.
  export let settingsTab: 'workspace' | 'environments'
  export let environmentId: string
  export let environment: domain.Environment | null

  export let onClose: () => void
  export let onOpenWorkspace: () => void
  export let onClearResponseCache: () => void

  // The environment mutations, grouped rather than passed as seven
  // separate props. App.svelte holds one stable object so this doesn't
  // see a new prop value on every render.
  export let env: {
    select: (id: string) => void
    create: () => void
    setName: (value: string) => void
    confirmDelete: () => void
    addVariable: () => void
    removeVariable: (index: number) => void
    save: () => void
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
      <button class:active={settingsTab === 'environments'} on:click={() => (settingsTab = 'environments')}
        >Environments</button
      >
    </div>

    <div class="settings-body">
      {#if settingsTab === 'workspace'}
        <p class="prose">Collections and environments are read from this folder.</p>
        <div class="row">
          <code class="workspace-path">{workspace.root}</code>
          <button on:click={onOpenWorkspace}>Change…</button>
        </div>
        {#if openError}<p class="error">{openError}</p>{/if}

        <p class="prose">
          Every request's last response is cached under this workspace (<code>.cache/responses</code>), so reopening it
          later shows what it last returned.
        </p>
        <div class="row">
          <button on:click={onClearResponseCache}>Clear response cache</button>
          {#if responseCacheCleared}<span class="muted">Cleared.</span>{/if}
        </div>
      {:else}
        <div class="row env-fields">
          <select bind:value={environmentId} on:change={() => env.select(environmentId)}>
            {#each workspace.environments as e (e.id)}
              <option value={e.id}>{e.name}</option>
            {/each}
          </select>
          <input
            class="env-name"
            type="text"
            value={environment ? environment.name : ''}
            on:input={(e) => env.setName(e.currentTarget.value)}
            placeholder="Environment name"
            disabled={!environment}
          />
        </div>

        <div class="row env-actions">
          <button on:click={env.create}>New</button>
          <button on:click={env.confirmDelete} disabled={workspace.environments.length <= 1}>Delete</button>
          <button class="env-actions-split" on:click={env.addVariable} disabled={!environment}>Add variable</button>
          <button class="primary" on:click={env.save} disabled={!environment}>Save environment</button>
        </div>

        {#if environment}
          <table class="kv-table">
            <thead>
              <tr><th></th><th>Key</th><th>Value</th><th>Secret</th><th></th></tr>
            </thead>
            <tbody>
              {#each environment.variables as v, i}
                <tr>
                  <td><input type="checkbox" bind:checked={v.enabled} /></td>
                  <td><input type="text" bind:value={v.key} placeholder="key" /></td>
                  <td><input type="text" bind:value={v.value} placeholder="value" /></td>
                  <td><input type="checkbox" bind:checked={v.secret} /></td>
                  <td><button class="icon-btn kv-remove-btn" on:click={() => env.removeVariable(i)}>×</button></td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
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

  .settings-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
  }

  .workspace-path {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  /* The two fields at the top of the Environments tab — the active-env
     picker and its rename box — share the row evenly. */
  .env-fields > select,
  .env-name {
    flex: 1;
    min-width: 0;
  }

  /* One toolbar for both environment-level (New/Delete) and
     variable-level (Add/Save) actions; the split pushes the
     variable pair to the right edge. */
  .env-actions {
    flex-wrap: wrap;
  }

  .env-actions-split {
    margin-left: auto;
  }

  /* The Environments tab's table has a second narrow (checkbox) column —
     Secret — that isn't first or last, so it needs its own rule or it'd
     claim an even share of the remaining width like Key/Value do. */
  .settings-modal .kv-table th:nth-child(4),
  .settings-modal .kv-table td:nth-child(4) {
    width: 4.5rem;
  }
</style>
