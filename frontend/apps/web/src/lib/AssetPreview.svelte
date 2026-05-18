<!--
  AssetPreview — renders the primary stored asset for a (tenantId,
  mediaId) tuple, or a single S3 key.

  Three sizes:
    - thumb (48-56px square) — for list rows
    - card (240-360px) — for inline trace output / span detail blocks
    - full — claims its container; used inside Lightbox or hero slots

  Lazy by default: defers the listS3 + presignDownload round-trip
  until the element scrolls into view, so a library of 100 rows isn't
  N=100 RPCs on mount.

  Click on an image opens a Lightbox. Audio is inline; unknown
  types defer to "open in new tab".
-->
<script lang="ts">
  import { untrack } from "svelte";
  import {
    fetchPreview,
    presignKey,
    type Asset,
    type AssetKind,
  } from "../shared/asset-preview";
  import Lightbox from "./Lightbox.svelte";

  type Props = {
    tenantId?: string;
    mediaId?: string;
    /** Direct S3 key — bypasses the listS3 lookup. */
    key?: string;
    /** Hint for what kind we expect; lets us render an icon stand-in
     *  while the URL is still resolving. */
    mediaType?: string;
    size?: "thumb" | "card" | "full";
    /** When false, fetch immediately on mount instead of waiting for
     *  intersection. Useful for above-the-fold hero placements. */
    lazy?: boolean;
    /** Whether to enable click-to-lightbox. Default true for images. */
    expandable?: boolean;
    /** Fires when the asset resolution lands. The parent can use this to
     *  hide its chrome (label, padding) when nothing is going to render. */
    onResolve?: (state: "loading" | "found" | "missing") => void;
  };

  let {
    tenantId,
    mediaId,
    key,
    mediaType,
    size = "thumb",
    lazy = true,
    expandable = true,
    onResolve,
  }: Props = $props();

  // Resolution lifecycle:
  //   - undefined  → not yet resolved
  //   - null       → resolved, no asset found
  //   - Asset      → resolved successfully
  let asset = $state<Asset | null | undefined>(undefined);
  let error = $state<string | null>(null);
  let container: HTMLDivElement | null = $state(null);
  let lightboxOpen = $state(false);

  // Capture lazy mode at mount — flipping a row between lazy/eager
  // mid-lifecycle isn't a supported use case. untrack() silences the
  // Svelte 5 "initial value only" hint without changing behaviour.
  const lazyOnMount = untrack(() => lazy);
  let visible = $state(!lazyOnMount);

  let hintKind: AssetKind = $derived.by(() => {
    const mt = (mediaType ?? "").toUpperCase();
    if (mt === "IMAGE") return "image";
    if (mt === "AUDIO") return "audio";
    return "other";
  });

  $effect(() => {
    if (visible || !lazyOnMount || !container) return;
    const io = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          if (e.isIntersecting) {
            visible = true;
            io.disconnect();
            return;
          }
        }
      },
      { rootMargin: "200px" },
    );
    io.observe(container);
    return () => io.disconnect();
  });

  $effect(() => {
    if (!visible) return;
    const k = key;
    const t = tenantId;
    const m = mediaId;
    asset = undefined;
    error = null;
    onResolve?.("loading");
    const p = k ? presignKey(k) : t && m ? fetchPreview(t, m) : Promise.resolve(null);
    p.then(
      (a) => {
        asset = a;
        onResolve?.(a ? "found" : "missing");
      },
      (e) => {
        error = e instanceof Error ? e.message : String(e);
        asset = null;
        onResolve?.("missing");
      },
    );
  });

  function openLightbox(e: MouseEvent) {
    if (!asset || !expandable) return;
    if (asset.kind !== "image") return;
    e.stopPropagation();
    lightboxOpen = true;
  }
</script>

{#snippet glyph(kind: AssetKind, muted = false)}
  <span class="glyph" class:muted aria-hidden="true">
    {#if kind === "image"}
      <svg viewBox="0 0 24 24" width="100%" height="100%">
        <rect x="3.5" y="4.5" width="17" height="15" rx="1.5" stroke="currentColor" stroke-width="1.3" fill="none" />
        <circle cx="9" cy="10" r="1.5" fill="currentColor" />
        <path d="M4 17l4-4 3 3 4-5 5 6" stroke="currentColor" stroke-width="1.3" fill="none" stroke-linejoin="round" />
      </svg>
    {:else if kind === "audio"}
      <svg viewBox="0 0 24 24" width="100%" height="100%">
        <path d="M4 14V10M8 16V8M12 18V6M16 16V8M20 14V10" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" fill="none" />
      </svg>
    {:else}
      <svg viewBox="0 0 24 24" width="100%" height="100%">
        <path d="M7 3.5h7.5L19 8v12.5H7z" stroke="currentColor" stroke-width="1.3" fill="none" stroke-linejoin="round" />
        <path d="M14.5 3.5V8H19" stroke="currentColor" stroke-width="1.3" fill="none" />
      </svg>
    {/if}
  </span>
{/snippet}

<div
  class="preview {size}"
  class:loaded={!!asset}
  class:empty={asset === null}
  bind:this={container}
>
  {#if asset === undefined}
    <div class="skeleton" class:audio={hintKind === "audio"} aria-label="loading preview">
      {#if hintKind !== "image" && hintKind !== "other"}
        {@render glyph(hintKind)}
      {/if}
    </div>
  {:else if asset === null}
    <div class="missing" title={error ?? "no asset found"}>
      {@render glyph(hintKind, true)}
      {#if size !== "thumb"}
        <span class="msg">{error ? "preview failed" : "no asset yet"}</span>
      {/if}
    </div>
  {:else if asset.kind === "image"}
    <button
      type="button"
      class="image-btn"
      onclick={openLightbox}
      aria-label="open larger"
      title={asset.key}
    >
      <img src={asset.url} alt="" loading="lazy" />
      {#if size !== "thumb"}
        <span class="zoom-hint">click to enlarge</span>
      {/if}
    </button>
  {:else if asset.kind === "audio"}
    <div class="audio-wrap" title={asset.key}>
      {@render glyph("audio")}
      <audio src={asset.url} controls preload="metadata"></audio>
    </div>
  {:else}
    <a class="generic" href={asset.url} target="_blank" rel="noopener" title={asset.key}>
      {@render glyph("other")}
      {#if size !== "thumb"}<span class="msg">open ↗</span>{/if}
    </a>
  {/if}
</div>

{#if asset && lightboxOpen}
  <Lightbox
    open={lightboxOpen}
    url={asset.url}
    kind={asset.kind}
    caption={asset.key}
    onClose={() => (lightboxOpen = false)}
  />
{/if}

<style>
  .preview {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--bg-base);
    border: 1px solid var(--border);
    border-radius: 3px;
    overflow: hidden;
  }

  .preview.thumb {
    width: 52px;
    height: 52px;
    flex: 0 0 52px;
  }

  .preview.card {
    width: 100%;
    max-width: 360px;
    min-height: 200px;
    aspect-ratio: 4 / 3;
  }

  .preview.full {
    width: 100%;
    height: 100%;
  }

  .skeleton {
    width: 100%;
    height: 100%;
    background:
      linear-gradient(120deg, var(--bg-base) 0%, var(--bg-panel-hover) 50%, var(--bg-base) 100%);
    background-size: 200% 100%;
    animation: shimmer 1.6s ease-in-out infinite;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--fg-muted);
  }

  .skeleton.audio {
    color: var(--accent);
  }

  @keyframes shimmer {
    0%   { background-position: 100% 0; }
    100% { background-position: -100% 0; }
  }

  .missing {
    width: 100%;
    height: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    color: var(--fg-muted);
    background: var(--bg-base);
    border: 1px dashed var(--border-strong);
    border-radius: 3px;
    padding: 8px;
  }

  .missing .msg,
  .generic .msg {
    font-family: var(--font-sans);
    font-size: 12px;
    color: var(--fg-dim);
  }

  .image-btn {
    width: 100%;
    height: 100%;
    padding: 0;
    border: none;
    background: var(--bg-base);
    cursor: zoom-in;
    position: relative;
    display: block;
    overflow: hidden;
  }

  .image-btn img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
    transition: transform 280ms ease;
  }

  .image-btn:hover img {
    transform: scale(1.04);
  }

  .preview.card .image-btn img,
  .preview.full .image-btn img {
    object-fit: contain;
    background: var(--bg-panel-hover);
  }

  .zoom-hint {
    position: absolute;
    bottom: 8px;
    right: 8px;
    background: rgba(31, 30, 26, 0.78);
    color: #fdfbf3;
    font-family: var(--font-sans);
    font-size: 11px;
    padding: 2px 8px;
    border-radius: 2px;
    opacity: 0;
    transition: opacity 160ms ease;
    pointer-events: none;
    letter-spacing: 0.04em;
  }

  .image-btn:hover .zoom-hint {
    opacity: 1;
  }

  .audio-wrap {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 10px 14px;
    width: 100%;
    background: var(--bg-panel);
    color: var(--accent);
  }

  .preview.thumb .audio-wrap {
    padding: 0;
    justify-content: center;
  }

  .preview.thumb .audio-wrap audio {
    display: none;
  }

  .audio-wrap audio {
    flex: 1;
    height: 36px;
  }

  .generic {
    width: 100%;
    height: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 6px;
    color: var(--accent);
    background: var(--bg-panel);
    cursor: pointer;
    border: none;
    padding: 8px;
    text-decoration: none;
  }

  .generic:hover {
    background: var(--accent-dim);
  }

  .glyph {
    display: inline-flex;
    width: 28px;
    height: 28px;
    color: var(--accent);
  }

  .glyph.muted {
    color: var(--fg-muted);
  }

  .preview.card .glyph,
  .preview.full .glyph {
    width: 48px;
    height: 48px;
  }
</style>
