<script lang="ts">
  // One small dialog for every "what should this be called?" — new
  // collection, rename collection, new environment, rename environment.
  // The four differ only in their title and the word on the confirm
  // button, so they're one component.
  import { tick } from 'svelte'

  export let title: string
  export let confirmLabel: string
  export let value: string
  export let onConfirm: (name: string) => void
  export let onCancel: () => void

  let input: HTMLInputElement | undefined

  // Focused and selected on open: the dialog exists to take one string,
  // so typing should be the next thing that happens. Selected rather
  // than just focused because a rename starts with the old name in the
  // field, and replacing it is the common case.
  $: if (input) {
    tick().then(() => {
      input?.focus()
      input?.select()
    })
  }

  $: trimmed = value.trim()

  function confirm() {
    if (trimmed) onConfirm(trimmed)
  }
</script>

<!-- svelte-ignore a11y-click-events-have-key-events -->
<div class="modal-backdrop" role="presentation" on:click={onCancel}>
  <div
    class="modal name-dialog"
    role="dialog"
    aria-modal="true"
    aria-labelledby="name-dialog-title"
    tabindex="-1"
    on:click|stopPropagation
    on:keydown={(e) => e.key === 'Escape' && onCancel()}
  >
    <div class="modal-header">
      <h2 id="name-dialog-title">{title}</h2>
      <button class="icon-btn" title="Close" on:click={onCancel}>×</button>
    </div>

    <div class="name-dialog-body">
      <!-- Enter confirms, so the dialog can be finished without reaching
           for the mouse — the same as the URL field's send. -->
      <input
        bind:this={input}
        type="text"
        bind:value
        placeholder="Name"
        on:keydown={(e) => e.key === 'Enter' && confirm()}
      />
      <div class="name-dialog-actions">
        <button on:click={onCancel}>Cancel</button>
        <button class="primary" disabled={!trimmed} on:click={confirm}>{confirmLabel}</button>
      </div>
    </div>
  </div>
</div>

<style>
  .name-dialog {
    width: min(24rem, 90vw);
  }

  .name-dialog-body {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    padding: 1rem;
  }

  .name-dialog-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.5rem;
  }
</style>
