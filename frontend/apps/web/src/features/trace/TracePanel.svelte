<script lang="ts">
  import { localOpsClient } from "../../shared/local-ops/client";
  import type { FullJobView, TraceSpan } from "../../shared/local-ops/types";
  import { fmtClock, fmtDateTime, fmtDuration, tsToMs } from "../../shared/time";
  import Pill from "../../lib/Pill.svelte";
  import EmptyState from "../../lib/EmptyState.svelte";
  import AssetPreview from "../../lib/AssetPreview.svelte";
  import JobActions from "../jobs/JobActions.svelte";
  import SpanDetail from "./SpanDetail.svelte";
  import JobsPanel from "../jobs/JobsPanel.svelte";
  import { navigate, route } from "../../shared/route.svelte";
  import {
    buildNodes,
    computeWindow,
    barRect,
    isGateSpan,
    type SpanNode,
  } from "./waterfall";
  import {
    jobStatusVariant,
    isJobTerminal,
    traceStatusLabel,
    traceStatusVariant,
  } from "./status";
  import {
    barKindClass,
    chooseFailureSource,
    endLabelLeft,
    gapStartPct,
    gapWidthPct,
    isDerivedRow,
    isOkRow,
    parseMediaTuple,
    rowKind,
    rowLabel,
    rowSummary,
    showDurationLabel,
    showEdgeGapLabel,
    showGapSegment,
    showInlineGapLabel,
    type MediaTuple,
  } from "./trace-panel-helpers";

  type Props = { jobId?: string };

  let { jobId }: Props = $props();

  let view = $state<FullJobView | undefined>(undefined);
  let lastError = $state<string | null>(null);
  let loading = $state(false);
  let nowMs = $state(Date.now());
  let polling = $state(false);
  let lastSuccessfulFetchAt = $state<number | null>(null);

  // The URL is the source of truth for the selected span. Hash format:
  // #/trace/<jobId>/<encodedSpanId>. When the user clicks a row we push a
  // new hash; when they hit Back the browser pops it and the panel
  // restores. Synthetic stage rollups carry a `:` in their id which
  // survives URL encoding, so decoding here matches whatever navigate
  // pushed.
  let urlSpanId = $derived.by<string>(() => {
    if (route.tab !== "trace") return "";
    const raw = route.params[1];
    if (!raw) return "";
    try {
      return decodeURIComponent(raw);
    } catch {
      return raw;
    }
  });

  let summary = $derived(view?.summary);
  let terminal = $derived(summary ? isJobTerminal(summary.status) : false);

  // For terminal jobs, freeze the "now" anchor to the job's last event.
  // Otherwise `nowMs` keeps ticking forward every second (it drives the
  // live elapsed counter), and that growth leaks into spanEndMs's
  // fallback path AND into computeWindow's upper bound — so any stage
  // that lacks a written endAt, or just the trace window itself,
  // appears to keep extending after the job has already completed.
  // Live (non-terminal) jobs still use the moving nowMs so an
  // in-flight bar correctly streams to the right edge.
  let effectiveNow = $derived.by(() => {
    if (terminal) {
      const last = tsToMs(view?.lastEventAt);
      if (last !== undefined) return last;
    }
    return nowMs;
  });

  let window_ = $derived(view ? computeWindow(view.spans, effectiveNow) : { start: 0, end: 1, span: 1 });
  // Seed buildNodes with the window's left edge so the first row reports a
  // real leading gap when the trace anchors on a pre-stage event (eager
  // OUTPUT row insertion, etc.). Without this, the 2s of intake latency
  // before the first STAGE renders as silent dead air.
  let nodes = $derived(view ? buildNodes(view.spans, effectiveNow, terminal, {}, window_.start) : []);
  // Auto-pick: the contextually most relevant span when the URL doesn't
  // pin one. Failure source > gate decision > first FSM stage > first
  // span. The user's URL pick always overrides this; auto-pick only
  // surfaces when they haven't selected anything yet (fresh entry,
  // bookmarked /trace/<jobId> without a span suffix).
  let autoSelectedSpanId = $derived.by<string>(() => {
    if (!view || view.spans.length === 0) return "";
    if (failureSource) return failureSource.id;
    if (view.gateDecision) {
      const gate = view.spans.find((s: TraceSpan) => isGateSpan(s));
      if (gate) return gate.id;
    }
    const firstStage = view.spans.find((s: TraceSpan) => s.kind === "STAGE");
    return (firstStage ?? view.spans[0]).id;
  });

  // The right-hand detail panel must always have something to show once
  // the trace has spans. URL pin wins when it resolves; auto-pick is the
  // contextual fallback; nodes[0] is the last-resort safety net so the
  // panel never flickers empty mid-poll if a node id momentarily slips
  // out of the visible set.
  let selectedSpan = $derived.by<TraceSpan | undefined>(() => {
    if (nodes.length === 0) return undefined;
    if (urlSpanId) {
      const fromUrl = nodes.find((n) => n.span.id === urlSpanId);
      if (fromUrl) return fromUrl.span;
    }
    if (autoSelectedSpanId) {
      const fromAuto = nodes.find((n) => n.span.id === autoSelectedSpanId);
      if (fromAuto) return fromAuto.span;
    }
    return nodes[0].span;
  });
  let selectedId = $derived(selectedSpan?.id ?? "");
  let firstEventMs = $derived(tsToMs(view?.firstEventAt));
  let axisTicks = $derived.by(() => {
    if (!window_ || window_.span <= 0) return [];
    const count = 4;
    return Array.from({ length: count + 1 }, (_, i) => {
      const left = (i / count) * 100;
      return { left, label: fmtDuration(window_.span * (i / count)) };
    });
  });

  let elapsedMs = $derived.by(() => {
    const first = firstEventMs;
    if (!first) return 0;
    const last = tsToMs(view?.lastEventAt) ?? 0;
    if (summary && isJobTerminal(summary.status) && last >= first) return last - first;
    return Math.max(0, nowMs - first);
  });

  // Total time the FSM spent actually doing stage work, vs. idle gaps
  // between stages (e.g. provider polling, queue handoffs). Without this
  // split a long job reads as "16m21s elapsed" with no hint of where the
  // time actually went — most of it is usually idle waiting.
  let activeMs = $derived(
    nodes
      .filter((n) => n.span.kind === "STAGE" && !n.skipped)
      .reduce((acc, n) => acc + n.durationMs, 0),
  );
  let idleMs = $derived(Math.max(0, elapsedMs - activeMs));
  // Only surface the breakdown when idle is non-trivial — a fast job
  // with 50ms of overhead doesn't need its own pill.
  let showIdleBreakdown = $derived(
    elapsedMs > 5_000 && idleMs > Math.max(1_000, elapsedMs * 0.20),
  );

  // The slowest active stage when one is clearly dominant. Heuristic:
  // the stage must be ≥1.5× the runner-up AND own ≥25% of elapsed.
  // Otherwise no one stage is "the bottleneck" and the chip would be
  // misleading.
  let slowestStage = $derived.by<SpanNode | null>(() => {
    const stages = nodes.filter(
      (n) => n.span.kind === "STAGE" && !n.skipped && n.durationMs > 0,
    );
    if (stages.length < 2) return null;
    const sorted = [...stages].sort((a, b) => b.durationMs - a.durationMs);
    const [top, second] = sorted;
    if (top.durationMs < (second?.durationMs ?? 0) * 1.5) return null;
    if (elapsedMs > 0 && top.durationMs / elapsedMs < 0.25) return null;
    return top;
  });
  let slowestPct = $derived(
    slowestStage && elapsedMs > 0
      ? Math.round((slowestStage.durationMs / elapsedMs) * 100)
      : 0,
  );

  let failureSource = $derived.by(() => (view ? chooseFailureSource(view.spans) : undefined));
  let failureSourceRecordedStage = $derived(failureSource?.attributes?.recorded_stage ?? "");

  async function fetchJob() {
    if (!jobId) return;
    polling = true;
    try {
      const res = await localOpsClient.getJob({ jobId });
      const fetchedAt = Date.now();
      view = res.view;
      lastError = null;
      lastSuccessfulFetchAt = fetchedAt;
      nowMs = fetchedAt;
    } catch (err) {
      lastError = err instanceof Error ? err.message : String(err);
    } finally {
      polling = false;
    }
  }

  $effect(() => {
    if (!jobId) return;
    loading = true;
    fetchJob().finally(() => (loading = false));
  });

  $effect(() => {
    if (!jobId) return;
    const id = setInterval(() => {
      if (!view?.summary || !isJobTerminal(view.summary.status)) {
        void fetchJob();
      }
      nowMs = Date.now();
    }, 2000);
    return () => clearInterval(id);
  });

  // Drive the elapsed counter between poll cycles.
  $effect(() => {
    const id = setInterval(() => (nowMs = Date.now()), 1000);
    return () => clearInterval(id);
  });

  function onSpanClick(span: TraceSpan) {
    // Selection is URL-driven: pushing a new hash both restores the
    // span on Back navigation and makes the link shareable. The route
    // store listens to `hashchange`, so the derived `selectedSpan`
    // re-runs without any local state to keep in sync. Clicking the
    // same row writes the same hash, which is a no-op — the panel
    // never toggles off.
    if (!jobId) return;
    navigate(`/trace/${jobId}/${encodeURIComponent(span.id)}`);
  }

  function onFailureSourceClick() {
    if (!failureSource) return;
    onSpanClick(failureSource);
  }

  function onSlowestClick() {
    if (!slowestStage) return;
    onSpanClick(slowestStage.span);
  }

  // Derive the storage tuple the output preview needs. The summary is the
  // authority; relatedKeys is the same server-owned DDB locator rendered in
  // the header, so it keeps the preview available while a partial view loads.
  let mediaTuple = $derived.by<MediaTuple | null>(() => {
    if (summary?.tenantId && summary.mediaId) {
      return { tenantId: summary.tenantId, mediaId: summary.mediaId };
    }
    if (!view) return null;
    for (const k of view.relatedKeys) {
      const tuple = parseMediaTuple(k);
      if (tuple) return tuple;
    }
    return null;
  });

  let showOutput = $derived(
    summary?.status?.toUpperCase() === "COMPLETE" && !!mediaTuple,
  );
</script>

{#if !jobId}
  <JobsPanel />
{:else if loading && !view}
  <EmptyState title="loading" hint={jobId} />
{:else if lastError && !view}
  <div class="err-panel">err · {lastError}</div>
{:else if !view}
  <EmptyState title="job not found" hint={jobId} />
{:else}
  <div class="trace-grid" class:has-detail={!!selectedSpan}>
    <div class="header-strip">
      <div class="header-main">
      <div class="id-block">
        <button type="button" class="back-btn" onclick={() => navigate("/trace")} aria-label="back to job list">
          <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true">
            <path d="M10 3l-5 5 5 5" stroke="currentColor" stroke-width="1.6" fill="none" stroke-linecap="round" stroke-linejoin="round" />
          </svg>
          <span>back to list</span>
        </button>
        <span class="sep" aria-hidden="true">·</span>
        <span class="muted-cap">job</span>
        <code class="job-id">{summary?.jobId}</code>
        {#if polling}
          <span class="spin" aria-label="refreshing"></span>
        {/if}
        {#if mediaTuple}
          <span class="sep" aria-hidden="true">·</span>
          <span class="muted-cap">media</span>
          <button
            type="button"
            class="media-link"
            onclick={() => navigate(`/library/${encodeURIComponent(mediaTuple.mediaId)}`)}
            title={`open ${mediaTuple.mediaId} in library`}
            aria-label={`open media ${mediaTuple.mediaId} in library`}
          >
            <code>{mediaTuple.mediaId}</code>
            <svg viewBox="0 0 16 16" width="11" height="11" aria-hidden="true">
              <path d="M6 3.5h6.5V10" stroke="currentColor" stroke-width="1.3" fill="none" stroke-linecap="round" />
              <path d="M12.5 3.5L6.5 9.5" stroke="currentColor" stroke-width="1.3" fill="none" stroke-linecap="round" />
              <path d="M11.5 8v4.5h-8v-8H8" stroke="currentColor" stroke-width="1.3" fill="none" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>
        {/if}
      </div>
      <div class="pills">
        <span class="meta-chip">
          <span class="meta-chip-k">type</span>
          <span class="meta-chip-v">{summary?.outputType || "—"}</span>
        </span>
        <span class="meta-chip">
          <span class="meta-chip-k">tier</span>
          <span class="meta-chip-v">{summary?.tier || "—"}</span>
        </span>
        <span class="meta-chip meta-chip-wide">
          <span class="meta-chip-k">model</span>
          <span class="meta-chip-v meta-chip-v-mono">{summary?.model || "—"}</span>
        </span>
        <span class="meta-pill"><span class="muted-cap">stage</span>
          <Pill variant="accent">{summary?.currentStage || "—"}</Pill>
        </span>
        <span class="meta-pill"><span class="muted-cap">status</span>
          <Pill variant={jobStatusVariant(summary?.status ?? "")}>{summary?.status || "—"}</Pill>
        </span>
        <span class="meta-pill">
          <span class="muted-cap">elapsed</span>
          <span class="elapsed">{fmtDuration(elapsedMs)}</span>
          {#if showIdleBreakdown}
            <span class="elapsed-split" aria-label={`${fmtDuration(activeMs)} active, ${fmtDuration(idleMs)} idle`}>
              <span class="split-num">{fmtDuration(activeMs)}</span><span class="split-cap">active</span>
              <span class="split-sep" aria-hidden="true">·</span>
              <span class="split-num">{fmtDuration(idleMs)}</span><span class="split-cap">idle</span>
            </span>
          {/if}
        </span>
        <span class="toolbar">
          <JobActions jobId={summary?.jobId ?? jobId} status={summary?.status} onDone={fetchJob} />
        </span>
      </div>
      </div>
      {#if showOutput && mediaTuple}
        <aside class="output-card" title="click to enlarge">
          <span class="output-cap">output</span>
          <AssetPreview
            tenantId={mediaTuple.tenantId}
            mediaId={mediaTuple.mediaId}
            mediaType={summary?.outputType}
            size="thumb"
            lazy={false}
          />
        </aside>
      {/if}
    </div>

    <div class="waterfall-pane">
      <div class="panel-header">
        <span>waterfall · {nodes.length} spans</span>
        <span class="dim">
          {fmtDateTime(view.firstEventAt)} → {fmtDateTime(view.lastEventAt)}
          {#if lastSuccessfulFetchAt}
            <span class="refresh-age">last refreshed {fmtClock(lastSuccessfulFetchAt)}</span>
          {/if}
        </span>
      </div>
      {#if lastError}
        <div class="refresh-error" role="status" aria-live="polite">refresh failed · {lastError}</div>
      {/if}
      {#if failureSource}
        <button
          type="button"
          class="source-summary"
          onclick={onFailureSourceClick}
          aria-label={`failure source ${failureSource.errorCode}: ${failureSource.errorMessage || rowLabel(failureSource)}`}
        >
          <span class="source-label">failure source</span>
          <code>{failureSource.errorCode}</code>
          <span class="source-message">{failureSource.errorMessage || rowLabel(failureSource)}</span>
          {#if failureSourceRecordedStage}
            <span class="source-stage">recorded at {failureSourceRecordedStage}</span>
          {/if}
        </button>
      {/if}
      {#if slowestStage && !failureSource}
        <button
          type="button"
          class="hotspot-summary"
          onclick={onSlowestClick}
          aria-label={`slowest stage ${slowestStage.span.stage} took ${fmtDuration(slowestStage.durationMs)}, ${slowestPct} percent of elapsed time`}
        >
          <span class="hotspot-label">slowest stage</span>
          <code>{slowestStage.span.stage}</code>
          <span class="hotspot-time">{fmtDuration(slowestStage.durationMs)}</span>
          <span class="hotspot-pct">{slowestPct}% of elapsed</span>
        </button>
      {/if}

      {#if nodes.length === 0}
        <EmptyState title="no spans yet" hint="waiting for first stage row to land…" />
      {:else}
        <div class="trace-legend" aria-label="trace row legend">
          <span><i class="legend-mark legend-event"></i>event</span>
          <span><i class="legend-mark legend-stage"></i>stage</span>
          <span><i class="legend-mark legend-wait"></i>wait</span>
        </div>
        <div class="rows">
          <div class="axis" aria-hidden="true">
            <span class="axis-title">span</span>
            <span class="axis-track">
              {#each axisTicks as tick (tick.left)}
                <span class="axis-tick" style={`left:${tick.left}%`}>
                  <span class="axis-line"></span>
                  <span class="axis-label">{tick.label}</span>
                </span>
              {/each}
            </span>
            <span class="axis-status">status</span>
          </div>
          {#each nodes as n (n.span.id)}
            {@const rect = barRect(n, window_)}
            <button
              class="row"
              class:selected={selectedId === n.span.id}
              class:child={n.depth > 0}
              class:failed={n.span.status === "TERMINAL_FAIL"}
              class:pending={n.span.status === "PENDING"}
              class:source={failureSource?.id === n.span.id}
              class:derived={isDerivedRow(n.span)}
              class:skipped={n.skipped}
              onclick={() => onSpanClick(n.span)}
              title={rowSummary(n)}
              aria-label={rowSummary(n)}
            >
              <span class="label mono" style={`padding-left: ${n.depth * 14}px`}>
                {#if n.depth > 0}<span class="branch">└─</span>{/if}
                {#if failureSource?.id === n.span.id}<span class="source-token">source</span>{/if}
                <span class="lbl-text">{rowLabel(n.span)}</span>
                {#if rowKind(n.span)}<span class="kind muted">{rowKind(n.span)}</span>{/if}
              </span>
              <span class="track">
                {#if showGapSegment(n, rect, window_)}
                  {@const gapL = gapStartPct(n, window_)}
                  {@const gapW = gapWidthPct(n, rect, window_)}
                  <span class="gap-segment" style={`left:${gapL}%; width:${gapW}%`} aria-hidden="true">
                    {#if showInlineGapLabel(n, rect, window_)}
                      <span class="gap-label-inline">waiting · {fmtDuration(n.gapFromPreviousEndMs)}</span>
                    {/if}
                  </span>
                  {#if showEdgeGapLabel(n, rect, window_)}
                    <span class="gap-label-edge" style={`left:${rect.left}%`}>· {fmtDuration(n.gapFromPreviousEndMs)}</span>
                  {/if}
                {/if}
                {#if !n.skipped}
                  <span
                    class={`bar bar-${barKindClass(n.span)}`}
                    style={`left:${rect.left}%; width:${rect.width}%`}
                  ></span>
                  {#if showDurationLabel(n, rect)}
                    <span class="duration-label" style={`left:${endLabelLeft(rect)}%`}>{fmtDuration(n.durationMs)}</span>
                  {/if}
                {/if}
              </span>
              <span class="status-cell">
                {#if isOkRow(n.span)}
                  <span class="status-ok" aria-label="ok">ok</span>
                {:else}
                  <Pill variant={traceStatusVariant(n.span)}>{traceStatusLabel(n.span)}</Pill>
                {/if}
              </span>
            </button>
          {/each}
        </div>
      {/if}
    </div>

    {#if selectedSpan}
      <SpanDetail
        span={selectedSpan}
        gate={view.gateDecision}
        job={view.job}
        tenantId={summary?.tenantId}
        mediaId={summary?.mediaId}
        prompt={view.decryptedPrompt}
        preparedPrompt={view.decryptedPreparedPrompt}
      />
    {/if}
  </div>
{/if}

<style>
  .trace-grid {
    display: grid;
    grid-template-columns: 1fr;
    grid-template-rows: auto 1fr;
    grid-template-areas:
      "head"
      "main";
    height: 100%;
    overflow: hidden;
  }

  .trace-grid.has-detail {
    grid-template-columns: minmax(0, 1fr) minmax(360px, 38%);
    grid-template-areas:
      "head head"
      "main side";
  }

  .header-strip {
    grid-area: head;
    background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
    padding: 14px 20px 12px;
    /* Two-column layout keeps the OUTPUT preview beside the summary data
       without stretching the header into the main workspace. */
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 12px 20px;
    align-items: start;
  }

  .header-main {
    display: flex;
    flex-direction: column;
    gap: 12px;
    min-width: 0;
  }

  .id-block {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-wrap: wrap;
  }

  .back-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 6px 12px 6px 10px;
    background: var(--bg-base);
    border: 1px solid var(--border);
    color: var(--fg-default);
    font-family: var(--font-sans);
    font-size: 12.5px;
    font-weight: 500;
    cursor: pointer;
    border-radius: 3px;
    transition: border-color 120ms ease, background 120ms ease, color 120ms ease;
  }

  .back-btn:hover {
    border-color: var(--accent);
    color: var(--accent);
    background: var(--accent-dim);
  }

  .back-btn svg {
    flex: 0 0 auto;
  }

  .sep {
    color: var(--fg-muted);
    font-size: 16px;
  }

  .job-id {
    font-size: 20px;
    color: var(--accent-strong);
    font-family: var(--font-mono);
    letter-spacing: -0.005em;
    font-weight: 500;
  }

  .pills {
    display: flex;
    flex-wrap: wrap;
    gap: 20px;
    align-items: center;
  }

  .meta-pill {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    font-size: 13.5px;
    color: var(--fg-default);
    font-family: var(--font-mono);
  }

  /* Read-only descriptor chips for type/tier/model. A two-tone segmented
     pill keeps each label visually separate from its value. */
  .meta-chip {
    display: inline-flex;
    align-items: stretch;
    border: 1px solid var(--border);
    border-radius: 3px;
    overflow: hidden;
    line-height: 1;
    background: var(--bg-base);
  }

  .meta-chip-k {
    display: inline-flex;
    align-items: center;
    background: var(--bg-panel-hover);
    color: var(--fg-dim);
    font-family: var(--font-sans);
    font-size: 10.5px;
    font-weight: 600;
padding: 5px 9px;
    border-right: 1px solid var(--border);
  }

  .meta-chip-v {
    display: inline-flex;
    align-items: center;
    color: var(--fg-bright);
    font-family: var(--font-sans);
    font-size: 12.5px;
    font-weight: 600;
    padding: 5px 11px;
}

  .meta-chip-v-mono {
    font-family: var(--font-mono);
    font-weight: 500;
    letter-spacing: 0;
    color: var(--accent-strong);
  }

  .meta-chip-wide .meta-chip-v {
    /* model strings can run long (e.g. notebooklm-default-v2) — let the
       chip grow rather than truncate. */
    white-space: nowrap;
  }

  .muted-cap {
    font-size: 11.5px;
    color: var(--fg-dim);
font-family: var(--font-sans);
    font-weight: 500;
  }

  .elapsed { color: var(--accent-strong); font-variant-numeric: tabular-nums; font-weight: 500; }

  /* Elapsed gets a "(active / idle)" split when idle is non-trivial.
     Three small chunks rather than one cramped line so the eye can pick
     out the two durations at a glance. */
  .elapsed-split {
    display: inline-flex;
    align-items: baseline;
    gap: 5px;
    margin-left: 6px;
    padding-left: 8px;
    border-left: 1px solid var(--border);
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg-default);
    font-variant-numeric: tabular-nums;
  }

  .elapsed-split .split-num {
    color: var(--fg-bright);
  }

  .elapsed-split .split-cap {
    font-family: var(--font-sans);
    font-size: 10.5px;
    color: var(--fg-dim);
margin-left: 1px;
  }

  .elapsed-split .split-sep {
    color: var(--border-strong);
    padding: 0 2px;
  }

  .toolbar {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin-left: auto;
  }

  /* The completion preview card. Stripped down to just the AssetPreview
     thumbnail with a tiny "output" caption above it — the media-id link
     lives in the id-block now, so this slot's only job is "click to see
     the artefact at full size". AssetPreview's thumb is already wired
     to open the Lightbox on click. */
  .output-card {
    display: inline-flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    padding: 4px 6px;
    border-radius: 3px;
    min-width: 0;
  }

  .output-cap {
    font-family: var(--font-sans);
    font-size: 10px;
    color: var(--fg-dim);
font-weight: 500;
    line-height: 1;
  }

  /* The "back to library" link sitting next to the job id. Reads as a
     plain id by default; on hover the accent + arrow signal it's
     navigable. */
  .media-link {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    border: none;
    background: transparent;
    padding: 0;
    cursor: pointer;
    color: var(--fg-default);
    font-family: var(--font-mono);
    font-size: 12.5px;
    transition: color 120ms ease;
  }

  .media-link code {
    color: inherit;
    max-width: 200px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .media-link:hover,
  .media-link:focus-visible {
    color: var(--accent);
    text-decoration: underline;
    outline: none;
  }

  .media-link svg {
    flex: 0 0 auto;
    opacity: 0.55;
    transition: opacity 120ms ease;
  }

  .media-link:hover svg,
  .media-link:focus-visible svg {
    opacity: 1;
  }

  .waterfall-pane {
    grid-area: main;
    overflow: auto;
    background: var(--bg-base);
    display: flex;
    flex-direction: column;
  }

  .waterfall-pane > :global(.panel-header) {
    background: var(--bg-panel);
  }

  .refresh-age {
    margin-left: 14px;
    color: var(--fg-muted);
    text-transform: none;
    letter-spacing: 0;
  }

  .refresh-error {
    border-bottom: 1px solid var(--border);
    background: var(--warn-dim);
    color: var(--warn);
    padding: 8px 16px;
    font-family: var(--font-sans);
    font-size: 13px;
  }

  .source-summary {
    display: grid;
    grid-template-columns: 96px max-content minmax(0, 1fr) max-content;
    gap: 12px;
    align-items: center;
    width: 100%;
    border: 0;
    border-bottom: 1px solid var(--border);
    border-left: 4px solid var(--err);
    border-radius: 0;
    background: var(--err-dim);
    color: var(--fg-default);
    padding: 9px 16px 9px 12px;
    text-align: left;
    font-family: var(--font-sans);
  }

  .source-summary:hover {
    background: var(--err-dim);
    border-bottom-color: var(--border-strong);
  }

  .source-summary .source-label {
    color: var(--err);
    font-size: 11.5px;
    font-weight: 700;
}

  .source-summary code {
    color: var(--err);
    font-family: var(--font-mono);
    font-size: 12.5px;
    font-weight: 600;
  }

  .source-message {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--fg-bright);
    font-size: 13px;
  }

  .source-stage {
    color: var(--fg-dim);
    font-family: var(--font-mono);
    font-size: 12px;
    white-space: nowrap;
  }

  /* The hotspot banner is the slowest-stage cousin of the failure-source
     banner. Same shape — single click-through chip pinned above the
     waterfall — but accent-toned instead of error-toned. It only shows
     when no failure source exists; failures always dominate the alley. */
  .hotspot-summary {
    display: grid;
    grid-template-columns: 110px max-content minmax(0, 1fr) max-content;
    gap: 12px;
    align-items: center;
    width: 100%;
    border: 0;
    border-bottom: 1px solid var(--border);
    border-left: 4px solid var(--accent);
    border-radius: 0;
    background: var(--accent-dim);
    color: var(--fg-default);
    padding: 8px 16px 8px 12px;
    text-align: left;
    font-family: var(--font-sans);
  }

  .hotspot-summary:hover {
    background: var(--accent-dim);
    border-bottom-color: var(--border-strong);
  }

  .hotspot-summary .hotspot-label {
    color: var(--accent-strong);
    font-size: 11.5px;
    font-weight: 700;
}

  .hotspot-summary code {
    color: var(--accent-strong);
    font-family: var(--font-mono);
    font-size: 12.5px;
    font-weight: 600;
  }

  .hotspot-time {
    color: var(--fg-bright);
    font-family: var(--font-mono);
    font-size: 13px;
    font-variant-numeric: tabular-nums;
    font-weight: 600;
  }

  .hotspot-pct {
    color: var(--fg-dim);
    font-family: var(--font-mono);
    font-size: 12px;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .trace-legend {
    display: flex;
    gap: 14px;
    align-items: center;
    padding: 7px 18px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-panel);
    color: var(--fg-dim);
    font-family: var(--font-sans);
    font-size: 11.5px;
  }

  .trace-legend span {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }

  .legend-mark {
    display: inline-block;
    width: 18px;
    height: 8px;
    border-radius: 2px;
    background: var(--accent);
  }

  .legend-event {
    width: 3px;
    background: var(--accent);
  }

  .legend-wait {
    background:
      repeating-linear-gradient(
        135deg,
        transparent 0 4px,
        rgba(141, 130, 109, 0.3) 4px 5px
      ),
      color-mix(in srgb, var(--fg-muted) 18%, transparent);
    border-top: 1px dashed var(--border-strong);
    border-bottom: 1px dashed var(--border-strong);
  }

  .rows {
    flex: 1;
    overflow: auto;
    background: var(--bg-panel);
  }

  .row,
  .axis {
    display: grid;
    grid-template-columns: minmax(260px, 390px) minmax(360px, 1fr) 120px;
    column-gap: 16px;
    width: 100%;
  }

  .axis {
    position: sticky;
    top: 0;
    z-index: 2;
    height: 28px;
    align-items: stretch;
    padding: 0 18px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-panel);
  }

  .axis-title,
  .axis-status {
    align-self: center;
    color: var(--fg-dim);
    font-family: var(--font-sans);
    font-size: 11.5px;
    font-weight: 600;
}

  .axis-status {
    text-align: right;
  }

  .axis-track {
    position: relative;
    min-width: 0;
  }

  .axis-tick {
    position: absolute;
    top: 0;
    bottom: 0;
    width: 1px;
  }

  .axis-line {
    display: block;
    width: 1px;
    height: 100%;
    background: var(--border);
  }

  .axis-label {
    position: absolute;
    top: 6px;
    left: 6px;
    color: var(--fg-muted);
    font-family: var(--font-mono);
    font-size: 11px;
    line-height: 1;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  }

  .row {
    align-items: center;
    padding: 6px 18px;
    height: var(--row-h);
    border: none;
    border-bottom: 1px solid var(--border);
    background: transparent;
    color: var(--fg-default);
    cursor: pointer;
    text-align: left;
    font-family: var(--font-mono);
  }

  .row:hover {
    background: var(--bg-panel-hover);
  }

  .row.selected {
    background: var(--accent-dim);
  }

  .row.failed {
    border-left: 3px solid var(--err);
    padding-left: 15px;
  }

  .row.source {
    background: var(--err-dim);
    border-left-width: 4px;
    padding-left: 14px;
  }

  .row.source.selected {
    background: var(--err-dim);
  }

  .row.source .lbl-text {
    color: var(--err);
    font-weight: 600;
  }

  .row.derived.failed {
    border-left: none;
    padding-left: 18px;
  }

  .row.derived {
    color: var(--fg-dim);
  }

  .row.pending {
    color: var(--fg-dim);
  }

  /* Child spans get a soft ink wash so the hierarchy reads without
     resorting to icons. */
  .row.child {
    background: rgba(31, 63, 191, 0.018);
  }

  .row.child.selected { background: var(--accent-dim); }

  .label {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13.5px;
    color: var(--fg-default);
    white-space: nowrap;
    overflow: hidden;
    min-width: 0;
  }

  .lbl-text {
    color: var(--fg-bright);
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .source-token {
    flex: 0 0 auto;
    color: var(--err);
    background: var(--err-dim);
    border: 1px solid var(--err);
    border-radius: 2px;
    padding: 0 6px;
    font-family: var(--font-sans);
    font-size: 10.5px;
    font-weight: 700;
}
  .kind {
    font-size: 11.5px;
font-family: var(--font-sans);
    color: var(--fg-dim);
  }
  .branch { color: var(--fg-muted); margin-right: 4px; }

  .track {
    position: relative;
    height: 22px;
    min-width: 0;
    background:
      linear-gradient(var(--border), var(--border)) 0 50% / 100% 1px no-repeat,
      var(--bg-base);
    border-left: 1px solid var(--border);
    border-right: 1px solid var(--border);
  }

  .bar {
    position: absolute;
    top: 5px;
    bottom: 5px;
    background: var(--accent);
    transition: left 220ms ease, width 220ms ease, background 200ms ease;
    min-width: 2px;
  }

  /* Bar fills are keyed off span kind so the operator can tell where time
     went without reading every label. STAGE is the canonical accent;
     ATTEMPT is a washed variant so an expanded retry chain is legibly
     "smaller" than its parent; PROVIDER is banded to read as external
     work; record markers (OUTPUT/VARIANT) and audits are neutral. Failure
     states still take precedence, in their own colour family. */
  .bar.bar-stage    { background: var(--accent); }
  /* Synthesized stage end now means an open tail, usually an in-flight stage
     whose exact end is not known yet. Completed handoffs render as wait rows. */
  .bar.bar-stage-synthesized {
    background:
      repeating-linear-gradient(
        135deg,
        var(--accent) 0 6px,
        color-mix(in srgb, var(--accent) 35%, transparent) 6px 9px
      );
  }
  .bar.bar-wait {
    background:
      repeating-linear-gradient(
        135deg,
        transparent 0 4px,
        rgba(141, 130, 109, 0.26) 4px 5px
      ),
      color-mix(in srgb, var(--fg-muted) 20%, transparent);
    border-top: 1px dashed var(--border-strong);
    border-bottom: 1px dashed var(--border-strong);
  }
  .bar.bar-attempt  { background: color-mix(in srgb, var(--accent) 55%, transparent); }
  .bar.bar-provider {
    background: var(--accent);
    background-image: linear-gradient(180deg, var(--accent-strong) 0 35%, transparent 35%);
  }
  .bar.bar-record {
    background: color-mix(in srgb, var(--accent) 30%, var(--border-strong));
    box-shadow: inset 0 1px 0 var(--accent);
  }
  .bar.bar-audit {
    background: var(--border-strong);
    opacity: 0.6;
  }
  .bar.bar-fail      { background: var(--err); }
  .bar.bar-transient { background: var(--warn); }
  .bar.bar-pending   { background: var(--fg-muted); }
  .bar.bar-skipped   { background: transparent; }

  /* Gap segment: a hatched stripe sitting where time was spent waiting
     rather than working. The pattern + dashed boundary read as "this
     isn't active work" without competing with the bar's colour. */
  .gap-segment {
    position: absolute;
    top: 7px;
    bottom: 7px;
    background:
      repeating-linear-gradient(
        135deg,
        transparent 0 4px,
        rgba(141, 130, 109, 0.22) 4px 5px
      );
    border-top: 1px dashed var(--border-strong);
    border-bottom: 1px dashed var(--border-strong);
    border-radius: 1px;
    pointer-events: none;
  }

  /* Inline label rides centred inside the gap segment when there's room
     for it; we draw a paper-coloured chip behind it so the diagonal
     hatching doesn't make the text hard to read. */
  .gap-label-inline {
    position: absolute;
    top: 50%;
    left: 50%;
    transform: translate(-50%, -50%);
    font-family: var(--font-sans);
    font-style: italic;
    font-size: 11px;
    color: var(--fg-dim);
    background: var(--bg-panel);
    padding: 1px 8px;
    border-radius: 2px;
white-space: nowrap;
    pointer-events: none;
  }

  /* When the gap is narrow, the inline label won't fit. Surface a small
     italic note just outside the bar's left edge instead — visually
     distinct from the duration-label that sits on the bar's right. */
  .gap-label-edge {
    position: absolute;
    top: 4px;
    transform: translateX(calc(-100% - 8px));
    font-family: var(--font-sans);
    font-style: italic;
    font-size: 11px;
    color: var(--fg-muted);
    white-space: nowrap;
    pointer-events: none;
  }

  .duration-label {
    position: absolute;
    top: 4px;
    transform: translateX(8px);
    color: var(--fg-default);
    font-family: var(--font-mono);
    font-size: 12.5px;
    font-weight: 500;
    line-height: 1;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    pointer-events: none;
  }

  .row.skipped {
    color: var(--fg-dim);
  }

  .row.skipped .lbl-text {
    color: var(--fg-dim);
    text-decoration: line-through;
    text-decoration-thickness: 1px;
    text-decoration-color: var(--fg-muted);
  }

  .row.skipped .track {
    visibility: hidden;
  }

  .status-cell {
    display: flex;
    justify-content: flex-end;
    align-items: center;
  }

  /* The "ok" rendering for the all-OK common case. Sans-serif lowercase
     in a muted tone — present enough to confirm "yes, this step ran
     fine" but quiet enough that the eye scans straight past a stack of
     them to land on whatever is actually FAILED / RETRY / PENDING. */
  .status-ok {
    font-family: var(--font-sans);
    font-size: 11px;
    font-weight: 500;
    color: var(--fg-muted);
font-variant-numeric: tabular-nums;
  }

  .err-panel {
    padding: 24px;
    color: var(--err);
    background: var(--err-dim);
    border-bottom: 1px solid var(--border);
    font-size: 14px;
    font-family: var(--font-sans);
  }

  .spin {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--accent);
    animation: spinpulse 1.2s ease-in-out infinite;
  }

  @keyframes spinpulse {
    0%, 100% { opacity: 0.4; transform: scale(0.85); }
    50% { opacity: 1; transform: scale(1.15); }
  }

  .dim { color: var(--fg-dim); }
  .mono { font-family: var(--font-mono); }
</style>
