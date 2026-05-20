<!--
  MePanel — the operator's current-tenant overview.

  Composed from the same frontend-local ops reads that power the deeper tabs:
  listJobs, listMedia, queueDepths, and tenant usage. In local single-tenant
  mode every record traces back to the same tenant.
-->
<script lang="ts">
  import { localOpsClient } from "../../shared/local-ops/client";
  import type {
    JobSummary,
    MediaRow,
    QueueStat,
    TenantUsageReservoir,
  } from "../../shared/local-ops/types";
  import { navigate } from "../../shared/route.svelte";
  import { fmtAge, fmtDateTime } from "../../shared/time";
  import Pill from "../../lib/Pill.svelte";
  import AssetPreview from "../../lib/AssetPreview.svelte";
  import { jobStatusVariant } from "../trace/status";

  let jobs = $state<JobSummary[]>([]);
  let media = $state<MediaRow[]>([]);
  let queues = $state<QueueStat[]>([]);
  let dailyCost = $state<TenantUsageReservoir | null>(null);
  let usagePeriod = $state("");
  let loading = $state(true);
  let lastError = $state<string | null>(null);
  let refreshedAt = $state<number>(Date.now());
  // Authoritative tenant for the local playground, read from the Vite
  // adapter so the hero is correct on an empty database.
  let tenantId = $state<string>("…");

  async function loadAll() {
    loading = true;
    lastError = null;
    try {
      const [idRes, jobsRes, mediaRes, queuesRes, usageRes] = await Promise.all([
        localOpsClient.getLocalIdentity(),
        localOpsClient.listJobs({ limit: 200 }),
        localOpsClient.listMedia({ limit: 200, includeDeleted: false }),
        localOpsClient.queueDepths(),
        localOpsClient.getTenantUsage({}),
      ]);
      tenantId = idRes.tenantId;
      jobs = jobsRes.jobs;
      media = mediaRes.items;
      queues = queuesRes.queues;
      dailyCost = usageRes.dailyCost ?? null;
      usagePeriod = usageRes.currentDailyPeriod;
      refreshedAt = Date.now();
    } catch (err) {
      lastError = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  $effect(() => { void loadAll(); });

  $effect(() => {
    const id = setInterval(loadAll, 10_000);
    return () => clearInterval(id);
  });

  // Today = local midnight forward. listJobs/listMedia return recent
  // rows in reverse-chronological order; we filter client-side on the
  // createdAt timestamp.
  let todayStart = $derived.by(() => {
    const d = new Date();
    d.setHours(0, 0, 0, 0);
    return d.getTime();
  });

  function tsMs(ts?: { seconds?: bigint } | null): number {
    if (!ts || !ts.seconds) return 0;
    return Number(ts.seconds) * 1000;
  }

  function int64(v?: bigint | number | string | null): number {
    if (typeof v === "bigint") return Number(v);
    if (typeof v === "number") return v;
    if (typeof v === "string") return Number(v) || 0;
    return 0;
  }

  function fmtMicroUSD(v?: bigint | number | string | null): string {
    const usd = int64(v) / 1_000_000;
    const decimals = usd > 0 && usd < 1 ? 4 : 2;
    return `$${usd.toFixed(decimals)}`;
  }

  let jobsToday = $derived(jobs.filter((j) => tsMs(j.createdAt) >= todayStart));
  let mediaToday = $derived(media.filter((m) => tsMs(m.createdAt) >= todayStart));
  let dailyCostPeriod = $derived(dailyCost?.period || usagePeriod || "—");

  let jobsByStatus = $derived.by(() => {
    const out: Record<string, number> = {};
    for (const j of jobs) {
      const s = (j.status || "—").toUpperCase();
      out[s] = (out[s] ?? 0) + 1;
    }
    return out;
  });

  let mediaByType = $derived.by(() => {
    const out: Record<string, number> = {};
    for (const m of media) {
      const t = (m.mediaType || "—").toUpperCase();
      out[t] = (out[t] ?? 0) + 1;
    }
    return out;
  });

  let activeJobs = $derived(
    jobs.filter((j) => {
      const s = (j.status || "").toUpperCase();
      return s === "QUEUED" || s === "RUNNING" || s === "BLOCKED";
    }).length,
  );

  let dlqAlerts = $derived(queues.filter((q) => q.name.endsWith("-dlq") && q.visible > 0));

  let queuesPrimary = $derived(queues.filter((q) => !q.name.endsWith("-dlq")));
  let queuesBacklog = $derived(queuesPrimary.reduce((a, q) => a + q.visible, 0));
  let queuesInFlight = $derived(queuesPrimary.reduce((a, q) => a + q.inFlight, 0));

  // Recent generated media — the visual hero of the panel. The first
  // few image/audio results render as cards so the operator sees the
  // last things the tenant actually shipped.
  let recentAssets = $derived(
    media
      .filter((m) => m.origin === "GENERATED" && m.lifecycle === "COMPLETE")
      .slice(0, 6),
  );

</script>

<div class="page">
  <header class="page-header">
    <div class="header-left">
      <h1 class="page-title">{tenantId}</h1>
      <p class="page-meta">
        Single-tenant view. Auto-refreshes every 10s (last <span class="mono">{fmtAge(refreshedAt)}</span>).
      </p>
    </div>
    <div class="header-right">
      <button class="primary" onclick={loadAll} disabled={loading}>{loading ? "…" : "Refresh"}</button>
    </div>
  </header>

  {#if lastError}
    <div class="err-bar">err · {lastError}</div>
  {/if}

  <section class="grid">
    <article class="card stat">
      <div class="stat-header">Daily cost</div>
      <div class="stat-value">{fmtMicroUSD(dailyCost?.committed)}</div>
      <div class="stat-detail">
        <div class="qline"><span>Held</span> <strong class="mono">{fmtMicroUSD(dailyCost?.reserved)}</strong></div>
        <div class="qline"><span>Remaining</span> <strong class="mono">{fmtMicroUSD(dailyCost?.available)}</strong></div>
        <div class="qline"><span>Cap</span> <strong class="mono">{fmtMicroUSD(dailyCost?.cap)}</strong></div>
        <div class="cost-period mono">
          {dailyCostPeriod}{dailyCost && !dailyCost.materialized ? " (unopened)" : ""}
        </div>
      </div>
    </article>

    <article class="card stat">
      <div class="stat-header">Jobs today</div>
      <div class="stat-value">{jobsToday.length}</div>
      <div class="stat-detail">
        {#each Object.entries(jobsByStatus) as [status, count] (status)}
          <div class="status-row">
            <Pill variant={jobStatusVariant(status)}>{status}</Pill>
            <span class="mono">{count}</span>
          </div>
        {/each}
      </div>
    </article>

    <article class="card stat">
      <div class="stat-header">In-flight</div>
      <div class="stat-value">{activeJobs}</div>
      <div class="stat-detail dim">
        QUEUED + RUNNING + BLOCKED.
      </div>
    </article>

    <article class="card stat">
      <div class="stat-header">Media today</div>
      <div class="stat-value">{mediaToday.length}</div>
      <div class="stat-detail">
        {#each Object.entries(mediaByType) as [type, count] (type)}
          <div class="type-row">
            <span class="type-tag">{type}</span>
            <span class="mono">{count}</span>
          </div>
        {/each}
      </div>
    </article>

    <article class="card stat" class:alert={dlqAlerts.length > 0}>
      <div class="stat-header">Queue health</div>
      <div class="stat-value">
        {#if dlqAlerts.length > 0}
          <span class="alert-num">{dlqAlerts.length}</span>
          <span class="alert-sub">DLQ stuck</span>
        {:else}
          <span class="ok-mark">Healthy</span>
        {/if}
      </div>
      <div class="stat-detail">
        <div class="qline"><span>Backlog</span> <strong class="mono">{queuesBacklog}</strong></div>
        <div class="qline"><span>In flight</span> <strong class="mono">{queuesInFlight}</strong></div>
        {#if dlqAlerts.length > 0}
          <a class="alert-link" href="#/queues">View DLQ →</a>
        {/if}
      </div>
    </article>
  </section>

  <div class="layout-main">
    <section class="recent">
      <h2 class="section-h">Recently published</h2>
      {#if recentAssets.length === 0}
        <div class="muted-card">No completed generations yet.</div>
      {:else}
        <div class="recent-grid">
          {#each recentAssets as m (m.mediaId)}
            <article class="asset-card">
              <div class="asset-preview-slot">
                <AssetPreview
                  tenantId={m.tenantId}
                  mediaId={m.mediaId}
                  mediaType={m.mediaType}
                  size="card"
                  lazy={true}
                />
              </div>
              <div class="asset-meta">
                <div class="asset-row">
                  <span class="type-tag">{m.mediaType}</span>
                  <span class="muted-cap mono">{fmtAge(tsMs(m.createdAt))}</span>
                </div>
                <code class="asset-id" title={m.mediaId}>{m.mediaId}</code>
                {#if m.jobId}
                  <a class="asset-link" href={`#/trace/${m.jobId}`}>View Trace</a>
                {/if}
              </div>
            </article>
          {/each}
        </div>
      {/if}
    </section>

    <section class="recent">
      <h2 class="section-h">Recent activity</h2>
      {#if jobs.length === 0}
        <div class="muted-card">No jobs yet.</div>
      {:else}
        <div class="activity">
          {#each jobs.slice(0, 8) as j (j.jobId)}
            <button type="button" class="activity-row" onclick={() => navigate(`/trace/${j.jobId}`)}>
              <div class="activity-left">
                <Pill variant={jobStatusVariant(j.status)}>{j.status || "—"}</Pill>
                <code class="row-id" title={j.jobId}>{j.jobId}</code>
                <span class="row-type">{j.outputType || "—"}</span>
              </div>
              <div class="activity-right">
                <span class="row-stage mono">{j.currentStage || "—"}</span>
                <span class="row-age mono">{fmtAge(tsMs(j.createdAt))}</span>
              </div>
            </button>
          {/each}
        </div>
      {/if}
    </section>
  </div>
</div>

<style>
  .page {
    max-width: 1200px;
    margin: 0 auto;
    padding: 32px 32px 64px;
    display: flex;
    flex-direction: column;
    gap: 32px;
  }

  .page-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    padding-bottom: 24px;
    border-bottom: 1px solid var(--border);
  }

  .page-title {
    font-family: var(--font-sans);
    font-size: 20px;
    font-weight: 600;
    color: var(--fg-bright);
    margin: 0 0 8px;
    letter-spacing: -0.01em;
  }

  .page-meta {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 14px;
    color: var(--fg-dim);
  }

  .mono { font-family: var(--font-mono); }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 16px;
  }

  .card {
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 20px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .stat-header {
    font-family: var(--font-sans);
    font-size: 13px;
    color: var(--fg-dim);
    font-weight: 500;
  }

  .stat-value {
    font-family: var(--font-sans);
    font-size: 28px;
    font-weight: 600;
    color: var(--fg-bright);
    line-height: 1;
    letter-spacing: -0.01em;
  }

  .stat-detail {
    font-family: var(--font-sans);
    font-size: 13px;
    color: var(--fg-default);
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-top: 4px;
  }

  .stat-detail.dim { color: var(--fg-dim); line-height: 1.5; }

  .cost-period {
    color: var(--fg-muted);
    font-size: 12px;
    margin-top: 4px;
  }

  .status-row,
  .type-row,
  .qline {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .type-tag {
    display: inline-block;
    font-family: var(--font-sans);
    font-size: 12px;
    color: var(--fg-default);
  }

  .card.alert {
    border-color: var(--err);
    background: var(--err-dim);
  }

  .alert-num {
    color: var(--err);
    font-size: 28px;
    font-weight: 600;
  }

  .alert-sub {
    display: inline-block;
    margin-left: 8px;
    font-family: var(--font-sans);
    font-size: 13px;
    color: var(--err);
    font-weight: 500;
    vertical-align: 4px;
  }

  .alert-link {
    color: var(--err);
    font-family: var(--font-sans);
    font-size: 13px;
    margin-top: 4px;
  }

  .ok-mark {
    color: var(--ok);
    font-size: 28px;
    font-weight: 600;
  }

  .layout-main {
    display: grid;
    grid-template-columns: 2fr 1fr;
    gap: 32px;
    align-items: start;
  }

  @media (max-width: 900px) {
    .layout-main {
      grid-template-columns: 1fr;
    }
  }

  .section-h {
    margin: 0 0 16px;
    font-family: var(--font-sans);
    font-size: 16px;
    font-weight: 600;
    color: var(--fg-bright);
  }

  .muted-card {
    padding: 24px;
    color: var(--fg-dim);
    font-family: var(--font-sans);
    font-size: 14px;
    background: var(--bg-panel-hover);
    border: 1px solid var(--border);
    border-radius: 6px;
  }

  .muted-card a { color: var(--accent); }

  .recent-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
    gap: 16px;
  }

  .asset-card {
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 6px;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .asset-preview-slot {
    aspect-ratio: 4 / 3;
    background: var(--bg-panel-hover);
    border-bottom: 1px solid var(--border);
  }

  .asset-meta {
    padding: 14px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .asset-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .muted-cap {
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg-dim);
  }

  .asset-id {
    font-family: var(--font-mono);
    font-size: 13px;
    color: var(--fg-default);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .asset-link {
    font-family: var(--font-sans);
    font-size: 13px;
    color: var(--accent);
    font-weight: 500;
  }

  .activity {
    display: flex;
    flex-direction: column;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 6px;
    overflow: hidden;
  }

  .activity-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 12px 16px;
    width: 100%;
    background: transparent;
    border: none;
    border-bottom: 1px solid var(--border);
    text-align: left;
    cursor: pointer;
    font-family: inherit;
    transition: background 120ms ease;
  }

  .activity-row:last-child {
    border-bottom: none;
  }

  .activity-row:hover { background: var(--bg-panel-hover); }

  .activity-left {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
  }

  .activity-right {
    display: flex;
    align-items: center;
    gap: 16px;
    flex-shrink: 0;
  }

  .row-id {
    font-family: var(--font-mono);
    font-size: 13px;
    color: var(--fg-bright);
    max-width: 140px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .row-type {
    font-family: var(--font-sans);
    font-size: 13px;
    color: var(--fg-default);
  }

  .row-stage {
    font-size: 12px;
    color: var(--fg-dim);
  }

  .row-age {
    font-size: 13px;
    color: var(--fg-dim);
    text-align: right;
    min-width: 50px;
  }

  .err-bar {
    border-radius: 4px;
    background: var(--err-dim);
    color: var(--err);
    padding: 12px 16px;
    border: 1px solid var(--err);
    font-family: var(--font-sans);
    font-size: 14px;
  }
</style>
