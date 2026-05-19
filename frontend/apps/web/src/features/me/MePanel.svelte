<!--
  MePanel — the operator's current-tenant overview.

  Composed from the same LOCAL_ONLY ops reads that power the deeper tabs:
  listJobs, listMedia, queueDepths, and tenant usage. In local single-tenant
  mode every record traces back to the same tenant.
-->
<script lang="ts">
  import { create } from "@bufbuild/protobuf";
  import {
    GetLocalIdentityRequestSchema,
    GetTenantUsageRequestSchema,
    ListJobsRequestSchema,
    ListMediaRequestSchema,
    QueueDepthsRequestSchema,
    type JobSummary,
    type MediaRow,
    type QueueStat,
    type TenantUsageReservoir,
  } from "@media-service/api-client/gen/mediaservice/ops/v1/ops_pb.js";
  import { opsClient } from "../../shared/ops";
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
  // Authoritative tenant for the LOCAL_ONLY console, read from
  // OpsService.GetLocalIdentity so the hero is correct on an empty database.
  let tenantId = $state<string>("…");

  async function loadAll() {
    loading = true;
    lastError = null;
    try {
      const [idRes, jobsRes, mediaRes, queuesRes, usageRes] = await Promise.all([
        opsClient.getLocalIdentity(create(GetLocalIdentityRequestSchema, {})),
        opsClient.listJobs(create(ListJobsRequestSchema, { limit: 200 })),
        opsClient.listMedia(create(ListMediaRequestSchema, { limit: 200, includeDeleted: false })),
        opsClient.queueDepths(create(QueueDepthsRequestSchema, {})),
        opsClient.getTenantUsage(create(GetTenantUsageRequestSchema, {})),
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
  <header class="hero">
    <div class="hero-left">
      <div class="eyebrow">tenant</div>
      <h1 class="tenant-id">{tenantId}</h1>
      <p class="hero-lead">
        Local single-tenant view. Composed live from the same DDB partition the
        other tabs read. Auto-refreshes every 10s — last refresh
        <span class="mono">{fmtAge(refreshedAt)}</span>.
      </p>
    </div>
    <div class="hero-right">
      <button onclick={loadAll} disabled={loading}>{loading ? "…" : "refresh"}</button>
    </div>
  </header>

  {#if lastError}
    <div class="err-bar">err · {lastError}</div>
  {/if}

  <section class="grid">
    <article class="card stat">
      <div class="stat-label">daily cost</div>
      <div class="stat-value cost-value">{fmtMicroUSD(dailyCost?.committed)}</div>
      <div class="stat-detail">
        <span class="qline">held <strong class="tnum">{fmtMicroUSD(dailyCost?.reserved)}</strong></span>
        <span class="qline">remaining <strong class="tnum">{fmtMicroUSD(dailyCost?.available)}</strong></span>
        <span class="qline">cap <strong class="tnum">{fmtMicroUSD(dailyCost?.cap)}</strong></span>
        <span class="cost-period mono">
          {dailyCostPeriod}{dailyCost && !dailyCost.materialized ? " · unopened" : ""}
        </span>
      </div>
    </article>

    <article class="card stat">
      <div class="stat-label">today's jobs</div>
      <div class="stat-value">{jobsToday.length}</div>
      <div class="stat-detail">
        {#each Object.entries(jobsByStatus) as [status, count] (status)}
          <span class="status-row">
            <Pill variant={jobStatusVariant(status)}>{status}</Pill>
            <span class="tnum">{count}</span>
          </span>
        {/each}
      </div>
    </article>

    <article class="card stat">
      <div class="stat-label">in-flight</div>
      <div class="stat-value">{activeJobs}</div>
      <div class="stat-detail dim">
        active = QUEUED + RUNNING + BLOCKED. Watch the
        <a href="#/trace">trace</a> tab for live status.
      </div>
    </article>

    <article class="card stat">
      <div class="stat-label">today's media</div>
      <div class="stat-value">{mediaToday.length}</div>
      <div class="stat-detail">
        {#each Object.entries(mediaByType) as [type, count] (type)}
          <span class="type-row">
            <span class="type-tag">{type}</span>
            <span class="tnum">{count}</span>
          </span>
        {/each}
      </div>
    </article>

    <article class="card stat" class:alert={dlqAlerts.length > 0}>
      <div class="stat-label">queue health</div>
      <div class="stat-value">
        {#if dlqAlerts.length > 0}
          <span class="alert-num">{dlqAlerts.length}</span>
          <span class="alert-sub">dlq with messages</span>
        {:else}
          <span class="ok-mark">✓</span>
          <span class="ok-sub">healthy</span>
        {/if}
      </div>
      <div class="stat-detail">
        <span class="qline">backlog <strong class="tnum">{queuesBacklog}</strong></span>
        <span class="qline">sqs messages in flight <strong class="tnum">{queuesInFlight}</strong></span>
        {#if dlqAlerts.length > 0}
          <a class="alert-link" href="#/queues">view dlq →</a>
        {/if}
      </div>
    </article>
  </section>

  <section class="recent">
    <h2 class="section-h">recently published</h2>
    {#if recentAssets.length === 0}
      <div class="muted-card">no completed generations yet — try the <a href="#/submit">submit</a> tab.</div>
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
                <a class="asset-link" href={`#/trace/${m.jobId}`}>open trace →</a>
              {/if}
            </div>
          </article>
        {/each}
      </div>
    {/if}
  </section>

  <section class="recent">
    <h2 class="section-h">recent activity</h2>
    {#if jobs.length === 0}
      <div class="muted-card">no jobs yet.</div>
    {:else}
      <ul class="activity">
        {#each jobs.slice(0, 8) as j (j.jobId)}
          <li>
            <button type="button" class="activity-row" onclick={() => navigate(`/trace/${j.jobId}`)}>
              <Pill variant={jobStatusVariant(j.status)}>{j.status || "—"}</Pill>
              <code class="row-id" title={j.jobId}>{j.jobId}</code>
              <span class="row-type">{j.outputType || "—"}</span>
              <span class="row-stage mono">{j.currentStage || "—"}</span>
              <span class="row-age tnum">{fmtAge(tsMs(j.createdAt))}</span>
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  </section>
</div>

<style>
  .page {
    max-width: 1080px;
    margin: 0 auto;
    padding: 30px 28px 56px;
    display: flex;
    flex-direction: column;
    gap: 28px;
  }

  .hero {
    display: flex;
    justify-content: space-between;
    align-items: flex-end;
    gap: 24px;
    padding: 20px 24px 22px;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 4px;
    border-left: 3px solid var(--accent);
  }

  .eyebrow {
    font-family: var(--font-sans);
    font-size: 11.5px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.12em;
    margin-bottom: 4px;
    font-weight: 500;
  }

  .tenant-id {
    font-family: var(--font-display);
    font-size: 40px;
    font-weight: 600;
    color: var(--fg-bright);
    margin: 0 0 10px;
    letter-spacing: -0.015em;
    line-height: 1.05;
    font-feature-settings: "ss01";
  }

  .hero-lead {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 14px;
    line-height: 1.55;
    color: var(--fg-default);
    max-width: 540px;
  }

  .mono { font-family: var(--font-mono); }

  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
    gap: 14px;
  }

  /* Below ~760px the stat cards can no longer breathe in a single row, so
     collapse to two columns; below ~480px collapse to a stack. */
  @media (max-width: 760px) {
    .grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }

  @media (max-width: 480px) {
    .grid {
      grid-template-columns: 1fr;
    }
  }

  .card {
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 4px;
    padding: 18px 20px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .stat-label {
    font-family: var(--font-sans);
    font-size: 11.5px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.10em;
    font-weight: 500;
  }

  .stat-value {
    font-family: var(--font-display);
    font-size: 44px;
    font-weight: 600;
    color: var(--fg-bright);
    line-height: 1;
    letter-spacing: -0.02em;
    font-variant-numeric: tabular-nums;
    font-feature-settings: "ss01";
  }

  .cost-value {
    font-size: 34px;
    line-height: 1.08;
  }

  .stat-detail {
    font-family: var(--font-sans);
    font-size: 13px;
    color: var(--fg-default);
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .stat-detail.dim { color: var(--fg-dim); line-height: 1.5; }

  .cost-period {
    color: var(--fg-dim);
    font-size: 11.5px;
    margin-top: 2px;
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
    font-size: 11px;
    font-weight: 600;
    color: var(--accent-strong);
    background: var(--accent-dim);
    padding: 2px 8px;
    border-radius: 2px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  .card.alert {
    border-color: var(--err);
    background: var(--err-dim);
  }

  .alert-num {
    color: var(--err);
    font-size: 44px;
    font-weight: 600;
  }

  .alert-sub {
    display: inline-block;
    margin-left: 10px;
    font-family: var(--font-sans);
    font-size: 14px;
    color: var(--err);
    vertical-align: 0.6em;
  }

  .alert-link {
    color: var(--err);
    font-family: var(--font-sans);
    font-size: 12.5px;
    margin-top: 4px;
  }

  .ok-mark {
    color: var(--accent);
    font-size: 44px;
    font-weight: 600;
  }

  .ok-sub {
    display: inline-block;
    margin-left: 10px;
    font-family: var(--font-sans);
    font-size: 14px;
    color: var(--accent);
    vertical-align: 0.6em;
  }

  .section-h {
    margin: 0 0 14px;
    font-family: var(--font-display);
    font-size: 22px;
    font-weight: 600;
    color: var(--fg-bright);
    letter-spacing: -0.01em;
    font-feature-settings: "ss01";
  }

  .muted-card {
    padding: 28px;
    text-align: center;
    color: var(--fg-dim);
    font-family: var(--font-sans);
    font-size: 14px;
    background: var(--bg-panel);
    border: 1px dashed var(--border);
    border-radius: 4px;
  }

  .muted-card a { color: var(--accent); }

  .recent-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 14px;
  }

  .asset-card {
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 4px;
    overflow: hidden;
    display: flex;
    flex-direction: column;
  }

  .asset-preview-slot {
    aspect-ratio: 4 / 3;
    background: var(--bg-base);
  }

  .asset-meta {
    padding: 12px 14px;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .asset-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .muted-cap {
    font-family: var(--font-mono);
    font-size: 11.5px;
    color: var(--fg-dim);
  }

  .asset-id {
    font-family: var(--font-mono);
    font-size: 12.5px;
    color: var(--fg-default);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .asset-link {
    font-family: var(--font-sans);
    font-size: 12.5px;
    color: var(--accent);
  }

  .activity {
    list-style: none;
    margin: 0;
    padding: 0;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 4px;
    overflow: hidden;
  }

  .activity li {
    border-bottom: 1px solid var(--border);
  }
  .activity li:last-child { border-bottom: none; }

  .activity-row {
    display: grid;
    grid-template-columns: 110px minmax(0, 1fr) 90px minmax(0, 1fr) 110px;
    align-items: center;
    gap: 14px;
    padding: 12px 18px;
    width: 100%;
    background: transparent;
    border: none;
    text-align: left;
    cursor: pointer;
    font-family: inherit;
    font-size: inherit;
    color: inherit;
    transition: background 120ms ease;
  }

  .activity-row:hover { background: var(--bg-panel-hover); }

  .row-id {
    font-family: var(--font-mono);
    font-size: 13px;
    color: var(--fg-bright);
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
    font-size: 12.5px;
    color: var(--fg-dim);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .row-age {
    font-family: var(--font-mono);
    font-size: 12.5px;
    color: var(--fg-dim);
    text-align: right;
  }

  /* MePanel surfaces err-bar inline in a card grid; drop the global
     bottom border and round the corners so it reads as a banner card. */
  .err-bar {
    border-bottom: none;
    border-radius: 3px;
  }
</style>
