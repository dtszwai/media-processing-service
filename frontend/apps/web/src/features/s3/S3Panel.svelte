<script lang="ts">
  import { localOpsClient } from "../../shared/local-ops/client";
  import type { S3Node } from "../../shared/local-ops/types";
  import { navigate, route } from "../../shared/route.svelte";
  import { fmtBytes, fmtDateTime } from "../../shared/time";
  import EmptyState from "../../lib/EmptyState.svelte";
  import MutationButton from "../../lib/MutationButton.svelte";
  import AssetPreview from "../../lib/AssetPreview.svelte";
  import { kindFromKey } from "../../shared/asset-preview";

  type Props = {
    prefix?: string;
  };

  let { prefix: prefixOverride }: Props = $props();

  // Hash routing splits on "/", so an S3 key like "tenant/media/asset"
  // arrives as multiple params. Rejoin to recover the original prefix.
  let routePrefix = $derived(route.params.map((p) => decodeURIComponent(p)).join("/"));
  let prefix = $derived(prefixOverride ?? routePrefix);
  // S3 ListObjectsV2 with delimiter="/" treats prefix as a literal byte match,
  // not a folder. Without a trailing slash, listing inside `tenant_local`
  // collapses every `tenant_local/...` key into a single `tenant_local/`
  // common prefix instead of revealing the children. URLs keep the cleaner
  // no-trailing-slash form; only the request gets the slash.
  let s3Prefix = $derived(prefix ? `${prefix}/` : "");

  let nodes = $state<S3Node[]>([]);
  let loading = $state(false);
  let lastError = $state<string | null>(null);
  let presigning = $state<string | null>(null);

  async function load() {
    loading = true;
    lastError = null;
    try {
      const res = await localOpsClient.listS3({
        prefix: s3Prefix,
        delimiter: "/",
        limit: 200,
      });
      nodes = res.nodes;
    } catch (err) {
      lastError = err instanceof Error ? err.message : String(err);
      nodes = [];
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    prefix;
    load();
  });

  let crumbs = $derived.by(() => {
    const parts = prefix.split("/").filter(Boolean);
    const acc: { label: string; href: string }[] = [];
    let cur = "";
    for (const p of parts) {
      cur = cur ? `${cur}/${p}` : p;
      acc.push({ label: p, href: `#/s3/${cur.split("/").map(encodeURIComponent).join("/")}` });
    }
    return acc;
  });


  function isPreviewable(key: string): boolean {
    const k = kindFromKey(key);
    return k === "image" || k === "audio";
  }

  async function presignAndOpen(key: string) {
    presigning = key;
    try {
      const res = await localOpsClient.presignDownload({ key });
      if (res.url) window.open(res.url, "_blank", "noopener");
    } catch (err) {
      lastError = err instanceof Error ? err.message : String(err);
    } finally {
      presigning = null;
    }
  }

  async function onRowClick(n: S3Node) {
    if (n.isPrefix) {
      // S3 prefix keys always end in "/"; strip it so the URL doesn't grow
      // an empty trailing segment.
      const next = n.key.replace(/\/$/, "");
      navigate(`/s3/${next.split("/").map(encodeURIComponent).join("/")}`);
      return;
    }
    await presignAndOpen(n.key);
  }

  let prefixCount = $derived(nodes.filter((n) => n.isPrefix).length);
  let objectCount = $derived(nodes.filter((n) => !n.isPrefix).length);

  async function doDelete(key: string) {
    await localOpsClient.deleteS3Object({ key });
    await load();
  }
</script>

<section>
  <header class="head">
    <div class="crumbs">
      <a href="#/s3" class:root={crumbs.length === 0}>bucket</a>
      {#each crumbs as c, i (c.href)}
        <span class="sep">/</span>
        <a href={c.href} class:current={i === crumbs.length - 1}>{c.label}</a>
      {/each}
    </div>
    <div class="meta">
      <span class="dim">{prefixCount} prefix · {objectCount} object</span>
      <button onclick={load} disabled={loading}>{loading ? "…" : "refresh"}</button>
    </div>
  </header>

  {#if lastError}
    <div class="err-bar">err · {lastError}</div>
  {/if}

  <div class="table-host">
    {#if nodes.length === 0 && !loading}
      <EmptyState
        title={prefix ? "empty prefix" : "empty bucket"}
        hint={prefix ? "no objects or sub-prefixes under this path." : "no objects yet — submit a job to seed the bucket."}
      />
    {:else}
      <table class="dense">
        <thead>
          <tr>
            <th>name</th>
            <th class="num" style="width: 110px">size</th>
            <th style="width: 160px">etag</th>
            <th class="num" style="width: 180px">last_modified</th>
            <th style="width: 180px">actions</th>
          </tr>
        </thead>
        <tbody>
          {#each nodes as n (n.key)}
            {@const previewable = isPreviewable(n.key)}
            <tr class="clickable" class:prefix={n.isPrefix} class:with-thumb={!n.isPrefix && previewable} onclick={() => onRowClick(n)}>
              <td class="name-cell">
                {#if n.isPrefix}
                  <span class="icon" aria-hidden="true">▸</span>
                  <span class="name-text mono">{n.name}/</span>
                {:else}
                  {#if previewable}
                    <div
                      class="thumb-slot"
                      role="presentation"
                      onclick={(e) => e.stopPropagation()}
                      onkeydown={(e) => e.stopPropagation()}
                    >
                      <AssetPreview key={n.key} size="thumb" />
                    </div>
                  {:else}
                    <span class="icon obj" aria-hidden="true">·</span>
                  {/if}
                  <span class="name-text mono">{n.name}</span>
                  {#if presigning === n.key}
                    <span class="dim presigning">presigning…</span>
                  {/if}
                {/if}
              </td>
              <td class="num" class:dim={n.isPrefix}>
                {n.isPrefix ? "—" : fmtBytes(n.sizeBytes)}
              </td>
              <td title={n.isPrefix ? "" : n.etag}>
                {#if n.isPrefix}
                  <span class="dim mono">—</span>
                {:else}
                  <code class="id-text">{n.etag}</code>
                {/if}
              </td>
              <td class="num mono dim">{n.isPrefix ? "—" : fmtDateTime(n.lastModified)}</td>
              <td class="actions" onclick={(e) => e.stopPropagation()}>
                {#if !n.isPrefix}
                  <button
                    class="dl-btn"
                    onclick={() => presignAndOpen(n.key)}
                    disabled={presigning === n.key}
                  >
                    {presigning === n.key ? "…" : "download"}
                  </button>
                  <MutationButton
                    label="delete"
                    confirmTitle="delete s3 object"
                    confirmBody="Permanently delete this S3 object. The DDB asset row is not touched — the operator is responsible for any reconciliation."
                    target={n.key}
                    onConfirm={() => doDelete(n.key)}
                  />
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
</section>

<style>
  section {
    display: flex;
    flex-direction: column;
    height: 100%;
  }

  .head {
    display: flex;
    align-items: center;
    gap: 16px;
    padding: 14px 20px;
    background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
  }

  .crumbs {
    display: flex;
    align-items: center;
    gap: 10px;
    flex: 1;
    flex-wrap: wrap;
    font-size: 14px;
    font-family: var(--font-mono);
  }

  .crumbs a {
    color: var(--accent);
  }

  .crumbs a.root {
    color: var(--fg-bright);
    font-weight: 500;
  }

  .crumbs a.current {
    color: var(--fg-bright);
    cursor: default;
    text-decoration: none;
    font-weight: 500;
  }

  .crumbs .sep {
    color: var(--fg-muted);
  }

  .meta {
    display: flex;
    align-items: center;
    gap: 14px;
    font-size: 13px;
    font-family: var(--font-sans);
  }

  tbody tr.prefix td:first-child { color: var(--accent); font-weight: 500; }

  tbody td {
    max-width: 560px;
    vertical-align: middle;
  }

  tbody tr.with-thumb td {
    height: 68px;
    padding: 8px 14px;
  }

  .name-cell {
    display: flex;
    align-items: center;
    gap: 12px;
    max-width: 100%;
  }

  .thumb-slot {
    flex: 0 0 auto;
    display: inline-flex;
  }

  .name-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }

  .presigning {
    margin-left: 8px;
    font-family: var(--font-sans);
    font-size: 12px;
  }

  td.actions :global(.trigger) { margin-left: 6px; }

  .dl-btn {
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
  .dl-btn:hover:not(:disabled) {
    border-color: var(--accent);
    color: var(--accent);
    background: var(--accent-dim);
  }
  .dl-btn:disabled { opacity: 0.45; cursor: not-allowed; }

  .icon {
    display: inline-block;
    width: 16px;
    color: var(--accent);
    text-align: center;
  }

  .icon.obj {
    color: var(--fg-muted);
  }

</style>
