<script lang="ts">
  type Props = {
    open: boolean;
    title: string;
    onAccept?: () => void;
    onCancel: () => void;
    acceptLabel?: string;
    cancelLabel?: string;
    acceptDisabled?: boolean;
    cancelDisabled?: boolean;
    danger?: boolean;
    children: import("svelte").Snippet;
  };

  let {
    open,
    title,
    onAccept,
    onCancel,
    acceptLabel = "Accept",
    cancelLabel = "Cancel",
    acceptDisabled = false,
    cancelDisabled = false,
    danger = false,
    children,
  }: Props = $props();

  function cancel() {
    if (cancelDisabled) return;
    onCancel();
  }

  function onKey(e: KeyboardEvent) {
    if (!open) return;
    if (e.key === "Escape") cancel();
  }
</script>

<svelte:window onkeydown={onKey} />

{#if open}
  <div
    class="backdrop"
    onclick={cancel}
    onkeydown={(e) => { if (e.key === "Enter" || e.key === " ") cancel(); }}
    role="presentation"
  >
    <div
      class="dialog"
      onclick={(e) => e.stopPropagation()}
      onkeydown={(e) => e.stopPropagation()}
      role="dialog"
      tabindex="-1"
      aria-modal="true"
      aria-label={title}
    >
      <div class="head">
        <span>{title}</span>
      </div>
      <div class="body">
        {@render children()}
      </div>
      <div class="foot">
        <button type="button" onclick={cancel} disabled={cancelDisabled}>{cancelLabel}</button>
        {#if onAccept}
          <button type="button" class={danger ? "danger" : "primary"} onclick={onAccept} disabled={acceptDisabled}>{acceptLabel}</button>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(42, 36, 29, 0.35);
    backdrop-filter: blur(2px);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
    animation: fadein 140ms ease;
  }

  .dialog {
    background: var(--bg-panel);
    border: 1px solid var(--border-strong);
    min-width: 460px;
    max-width: 680px;
    box-shadow: var(--shadow-paper);
    border-radius: 4px;
    animation: rise 180ms cubic-bezier(0.2, 0.7, 0.2, 1);
  }

  .head {
    padding: 12px 18px;
    border-bottom: 1px solid var(--border);
    font-size: 12.5px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.10em;
    font-family: var(--font-sans);
    font-weight: 500;
  }

  .body {
    padding: 18px;
    font-size: 14px;
    color: var(--fg-default);
    line-height: 1.6;
  }

  .foot {
    border-top: 1px solid var(--border);
    padding: 14px 18px;
    display: flex;
    gap: 10px;
    justify-content: flex-end;
    background: var(--bg-base);
    border-radius: 0 0 4px 4px;
  }

  @keyframes fadein {
    from { opacity: 0; }
    to   { opacity: 1; }
  }

  @keyframes rise {
    from { opacity: 0; transform: translateY(8px); }
    to   { opacity: 1; transform: translateY(0); }
  }
</style>
