<!--
  Lightbox — full-bleed modal for inspecting a generated asset.

  Used by AssetPreview when an image thumb is clicked. Audio doesn't
  need a lightbox — the player is inline. Stays intentionally minimal:
  ESC or backdrop click closes; the canvas centres on a single asset
  and exposes a "open in new tab" affordance for the underlying URL.
-->
<script lang="ts">
  type Props = {
    open: boolean;
    url: string;
    kind: "image" | "audio" | "other";
    /** Caption shown beneath the asset — usually the S3 key. */
    caption?: string;
    onClose: () => void;
  };

  let { open, url, kind, caption, onClose }: Props = $props();

  function onKey(e: KeyboardEvent) {
    if (!open) return;
    if (e.key === "Escape") onClose();
  }
</script>

<svelte:window onkeydown={onKey} />

{#if open}
  <div
    class="backdrop"
    onclick={onClose}
    onkeydown={(e) => { if (e.key === "Enter" || e.key === " ") onClose(); }}
    role="presentation"
  >
    <div
      class="frame"
      onclick={(e) => e.stopPropagation()}
      onkeydown={(e) => e.stopPropagation()}
      role="dialog"
      tabindex="-1"
      aria-modal="true"
      aria-label="asset preview"
    >
      <button class="close" type="button" onclick={onClose} aria-label="close">
        <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
          <path d="M3 3l10 10M13 3L3 13" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
        </svg>
      </button>

      <div class="canvas">
        {#if kind === "image"}
          <img src={url} alt="generated asset" />
        {:else if kind === "audio"}
          <audio src={url} controls autoplay></audio>
        {:else}
          <div class="other">
            <p>preview unavailable for this asset type</p>
            <a href={url} target="_blank" rel="noopener">open in new tab</a>
          </div>
        {/if}
      </div>

      {#if caption}
        <footer class="caption">
          <code class="key">{caption}</code>
          <a href={url} target="_blank" rel="noopener" class="external">open ↗</a>
        </footer>
      {/if}
    </div>
  </div>
{/if}

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    background: rgba(42, 36, 29, 0.55);
    backdrop-filter: blur(4px);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 200;
    padding: 40px;
    animation: fadein 160ms ease;
  }

  .frame {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 14px;
    max-width: min(1200px, 92vw);
    max-height: 90vh;
    animation: rise 200ms cubic-bezier(0.2, 0.7, 0.2, 1);
  }

  .close {
    position: absolute;
    top: -14px;
    right: -14px;
    width: 36px;
    height: 36px;
    border-radius: 50%;
    background: var(--bg-panel);
    border: 1px solid var(--border-strong);
    color: var(--fg-default);
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    box-shadow: var(--shadow-paper);
    padding: 0;
    z-index: 1;
    transition: background 120ms ease, color 120ms ease, border-color 120ms ease;
  }

  .close:hover {
    background: var(--accent);
    color: #fdfbf3;
    border-color: var(--accent);
  }

  .canvas {
    flex: 1;
    min-height: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-panel);
    border: 1px solid var(--border-strong);
    border-radius: 6px;
    overflow: hidden;
    box-shadow: var(--shadow-paper);
    padding: 8px;
  }

  .canvas img {
    max-width: 100%;
    max-height: calc(90vh - 100px);
    object-fit: contain;
    display: block;
  }

  .canvas audio {
    width: min(640px, 80vw);
  }

  .other {
    padding: 60px 40px;
    text-align: center;
    font-family: var(--font-sans);
  }

  .other p {
    color: var(--fg-dim);
    margin: 0 0 12px;
  }

  .caption {
    display: flex;
    align-items: center;
    gap: 16px;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    padding: 8px 14px;
    border-radius: 4px;
  }

  .key {
    flex: 1;
    font-family: var(--font-mono);
    font-size: 12.5px;
    color: var(--fg-default);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .external {
    font-family: var(--font-sans);
    font-size: 12.5px;
    color: var(--accent);
    white-space: nowrap;
  }

  @keyframes fadein {
    from { opacity: 0; }
    to   { opacity: 1; }
  }

  @keyframes rise {
    from { opacity: 0; transform: scale(0.96); }
    to   { opacity: 1; transform: scale(1); }
  }
</style>
