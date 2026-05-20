<script lang="ts">
  import { localOpsClient } from "../../shared/local-ops/client";
  import type { MediaRow } from "../../shared/local-ops/types";
  import { navigate, route } from "../../shared/route.svelte";
  import { fmtDateTime } from "../../shared/time";
  import AssetPreview from "../../lib/AssetPreview.svelte";
  import Pill from "../../lib/Pill.svelte";

  type PillVariant = "ok" | "warn" | "err" | "pending" | "neutral" | "accent";

  let mediaType = $state("");
  let origin = $state("");
  let lifecycle = $state("");
  let includeDeleted = $state(false);
  let rows = $state<MediaRow[]>([]);
  let loading = $state(false);
  let lastError = $state<string | null>(null);

  // /library/<mediaId> lets other panels (e.g. the trace header's
  // media-link) deep-link to a specific row. We narrow the visible list
  // client-side; the backend ListMedia RPC has no per-id predicate, but
  // a single-row filter on a typical 100-row page is cheap enough.
  let mediaIdFilter = $derived(
    route.tab === "library" && route.params[0] ? decodeURIComponent(route.params[0]) : "",
  );

  let visibleRows = $derived(
    mediaIdFilter
      ? rows.filter((r) => r.mediaId.includes(mediaIdFilter))
      : rows,
  );

  function clearFilter() {
    navigate("/library");
  }

  async function load() {
    loading = true;
    lastError = null;
    try {
      const res = await localOpsClient.listMedia({
        mediaType,
        origin,
        lifecycle,
        includeDeleted,
        limit: 100,
      });
      rows = res.items;
    } catch (err) {
      lastError = err instanceof Error ? err.message : String(err);
      rows = [];
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    mediaType; origin; lifecycle; includeDeleted;
    load();
  });

  function onRowClick(row: MediaRow) {
    if (row.origin === "GENERATED" && row.jobId) {
      navigate(`/trace/${row.jobId}`);
    }
  }

  function lifecycleVariant(l: string): PillVariant {
    const u = (l || "").toUpperCase();
    if (u === "COMPLETE") return "ok";
    if (u === "FAILED") return "err";
    if (u === "RUNNING") return "accent";
    if (u === "DELETED") return "pending";
    return "neutral";
  }
</script>

<section>
  <div class="filter-bar">
    <label>
      type
      <select bind:value={mediaType}>
        <option value="">(any)</option>
        <option value="IMAGE">IMAGE</option>
        <option value="AUDIO">AUDIO</option>
      </select>
    </label>
    <label>
      origin
      <select bind:value={origin}>
        <option value="">(any)</option>
        <option value="UPLOAD">UPLOAD</option>
        <option value="GENERATED">GENERATED</option>
      </select>
    </label>
    <label>
      lifecycle
      <select bind:value={lifecycle}>
        <option value="">(any)</option>
        <option value="PENDING">PENDING</option>
        <option value="RUNNING">RUNNING</option>
        <option value="COMPLETE">COMPLETE</option>
        <option value="FAILED">FAILED</option>
        <option value="DELETED">DELETED</option>
      </select>
    </label>
    <label>
      <input type="checkbox" bind:checked={includeDeleted} />
      include deleted
    </label>
    <button onclick={load} disabled={loading} style="margin-left:auto">
      {loading ? "…" : "refresh"}
    </button>
  </div>

  {#if mediaIdFilter}
    <div class="filter-active">
      <span class="muted-cap">filter</span>
      <span>media_id</span>
      <code class="id-text">{mediaIdFilter}</code>
      <button type="button" class="filter-clear" onclick={clearFilter} aria-label="clear filter">clear ×</button>
    </div>
  {/if}

  {#if lastError}
    <div class="err-bar">err · {lastError}</div>
  {/if}

  <div class="table-host">
    <table class="dense lib">
      <thead>
        <tr>
          <th class="thumb-col"></th>
          <th style="width: 280px">media_id</th>
          <th style="width: 200px">tenant</th>
          <th style="width: 110px">origin</th>
          <th style="width: 100px">type</th>
          <th style="width: 130px">lifecycle</th>
          <th style="width: 240px">job_id</th>
          <th class="num" style="width: 170px">created</th>
        </tr>
      </thead>
      <tbody>
        {#if visibleRows.length === 0}
          <tr>
            <td colspan="8" class="empty">
              {#if loading}
                loading…
              {:else if mediaIdFilter && rows.length > 0}
                no rows match media_id "{mediaIdFilter}"
              {:else}
                no rows
              {/if}
            </td>
          </tr>
        {:else}
          {#each visibleRows as r (r.mediaId)}
            <tr class:clickable={r.origin === "GENERATED" && r.jobId} onclick={() => onRowClick(r)}>
              <td class="thumb-col" onclick={(e) => e.stopPropagation()}>
                <AssetPreview
                  tenantId={r.tenantId}
                  mediaId={r.mediaId}
                  mediaType={r.mediaType}
                  size="thumb"
                />
              </td>
              <td title={r.mediaId}><code class="id-text">{r.mediaId}</code></td>
              <td title={r.tenantId}><code class="id-text">{r.tenantId}</code></td>
              <td><Pill variant={r.origin === "GENERATED" ? "accent" : "neutral"}>{r.origin}</Pill></td>
              <td class="mono">{r.mediaType}</td>
              <td><Pill variant={lifecycleVariant(r.lifecycle)}>{r.lifecycle}</Pill></td>
              <td title={r.jobId || ""}>
                {#if r.jobId}
                  <code class="id-text">{r.jobId}</code>
                {:else}
                  <span class="dim mono">—</span>
                {/if}
              </td>
              <td class="num mono">{fmtDateTime(r.createdAt)}</td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>
</section>

<style>
  section {
    display: flex;
    flex-direction: column;
    height: 100%;
  }

  table.lib {
    table-layout: auto;
  }

  table.lib thead th.thumb-col { width: 72px; padding-left: 18px; }

  table.lib tbody td {
    padding: 10px 14px;
    height: 68px;
    vertical-align: middle;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  table.lib tbody td.thumb-col {
    padding: 8px 6px 8px 18px;
    width: 72px;
  }

  .filter-active {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 18px;
    background: var(--accent-dim);
    border-bottom: 1px solid var(--accent);
    font-family: var(--font-sans);
    font-size: 12.5px;
    color: var(--fg-default);
  }

  .filter-active .id-text {
    color: var(--accent-strong);
    font-weight: 500;
  }

  .filter-clear {
    margin-left: auto;
    border: 1px solid var(--accent);
    background: var(--bg-base);
    color: var(--accent);
    font-family: var(--font-sans);
    font-size: 11.5px;
    font-weight: 500;
    padding: 3px 10px;
    cursor: pointer;
    border-radius: 2px;
}

  .filter-clear:hover {
    background: var(--accent);
    color: var(--bg-base);
  }
</style>
