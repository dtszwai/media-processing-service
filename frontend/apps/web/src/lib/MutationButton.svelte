<!--
  Confirm-then-execute primitive for every operator mutation. Renders a
  trigger button that opens a Dialog showing the canonical target identifier
  before invoking onConfirm. `danger` defaults to true — operator state
  changes deserve the friction.
-->
<script lang="ts">
  import Dialog from "./Dialog.svelte";

  let {
    label,
    confirmTitle,
    confirmBody,
    target = "",
    danger = true,
    disabled = false,
    onConfirm,
  }: {
    label: string;
    confirmTitle: string;
    confirmBody: string;
    target?: string;
    danger?: boolean;
    disabled?: boolean;
    onConfirm: () => Promise<void>;
  } = $props();

  let open = $state(false);
  let busy = $state(false);
  let error = $state("");

  function cancel() {
    if (busy) return;
    open = false;
  }

  async function execute() {
    if (busy) return;
    busy = true;
    error = "";
    try {
      await onConfirm();
      open = false;
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }
</script>

<button
  type="button"
  class="trigger"
  class:danger
  disabled={disabled || busy}
  onclick={() => (open = true)}
>
  {label}
</button>

<Dialog
  open={open}
  title={confirmTitle}
  onCancel={cancel}
  onAccept={execute}
  acceptLabel={busy ? "..." : label}
  acceptDisabled={busy}
  cancelDisabled={busy}
  {danger}
>
  <p class="body">{confirmBody}</p>
  {#if target}
    <div class="target">{target}</div>
  {/if}
  {#if error}
    <div class="error">{error}</div>
  {/if}
</Dialog>

<style>
  .trigger {
    background: var(--bg-panel);
    color: var(--fg-default);
    border: 1px solid var(--border);
    padding: 5px 12px;
    font-family: var(--font-sans);
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
transition: background 120ms ease, border-color 120ms ease, color 120ms ease;
  }
  .trigger:hover:not(:disabled) {
    border-color: var(--accent);
    color: var(--accent);
    background: var(--accent-dim);
  }
  .trigger.danger:hover:not(:disabled) {
    border-color: var(--err);
    color: var(--err);
    background: var(--err-dim);
  }
  .trigger:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }
  .body {
    margin: 0 0 10px 0;
    color: var(--fg-default);
    font-size: 14px;
    line-height: 1.6;
    font-family: var(--font-sans);
  }
  .target {
    font-family: var(--font-mono);
    background: var(--bg-base);
    border: 1px solid var(--border);
    padding: 8px 12px;
    font-size: 13px;
    color: var(--fg-bright);
    word-break: break-all;
    border-radius: 2px;
  }
  .error {
    margin-top: 10px;
    color: var(--err);
    font-size: 13px;
    border-left: 3px solid var(--err);
    padding-left: 10px;
    font-family: var(--font-sans);
  }
</style>
