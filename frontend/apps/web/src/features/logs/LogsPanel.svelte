<script lang="ts">
  import { create } from "@bufbuild/protobuf";
  import {
    StreamLogsRequestSchema,
    type LogLine,
  } from "@media-service/api-client/gen/mediaservice/ops/v1/ops_pb.js";
  import { opsClient } from "../../shared/ops";
  import { fmtTimeMs } from "../../shared/time";
  import Pill from "../../lib/Pill.svelte";
  import EmptyState from "../../lib/EmptyState.svelte";

  let service = $state("");
  let level = $state("");
  let jobId = $state("");
  let mediaId = $state("");
  let contains = $state("");
  let tailLines = $state(200);
  let lookbackSeconds = $state(6 * 60 * 60);

  // Bounded ring buffer — trim the head past BUFFER_CAP so the DOM stays
  // small without pulling in a virtual-scroll dependency.
  const BUFFER_CAP = 2000;
  const MAX_TAIL_LINES = 1000;
  const TAIL_BACKFILL_SECONDS = 24 * 60 * 60;
  const lookbackOptions = [
    { value: 60 * 60, label: "1h" },
    { value: 6 * 60 * 60, label: "6h" },
    { value: 24 * 60 * 60, label: "24h" },
    { value: 7 * 24 * 60 * 60, label: "7d" },
  ];

  let effectiveWindowLabel = $derived(
    formatDuration(tailLines > 0 ? Math.max(lookbackSeconds, TAIL_BACKFILL_SECONDS) : lookbackSeconds),
  );
  let lines = $state<LogLine[]>([]);
  let expanded = $state<Set<number>>(new Set());
  let lineSeq = 0;
  // Stable monotonic ID per LogLine so #each keys don't collide when the
  // head trims and array indices shift.
  let lineIds = new WeakMap<LogLine, number>();

  let streaming = $state(false);
  let streamError = $state<string | null>(null);
  let totalReceived = $state(0);

  let viewport = $state<HTMLDivElement | null>(null);
  let pinnedToBottom = $state(true);

  function genId(line: LogLine): number {
    const id = ++lineSeq;
    lineIds.set(line, id);
    return id;
  }

  function lineKey(line: LogLine): number {
    return lineIds.get(line) ?? genId(line);
  }

  let abort: AbortController | null = null;
  let debounceTimer: ReturnType<typeof setTimeout> | null = null;

  async function runStream() {
    abort?.abort();
    const controller = new AbortController();
    abort = controller;
    streamError = null;
    streaming = true;
    lines = [];
    expanded = new Set();
    lineIds = new WeakMap();
    totalReceived = 0;

    try {
      const req = create(StreamLogsRequestSchema, {
        service: service === "all" ? "" : service,
        jobId,
        mediaId,
        level: level === "all" ? "" : level,
        contains,
        tailLines: clampTailLines(tailLines),
        lookbackSeconds,
      });
      const stream = opsClient.streamLogs(req, { signal: controller.signal });
      for await (const msg of stream) {
        if (controller.signal.aborted) return;
        const line = msg.line;
        if (!line) continue;
        genId(line);
        // Replace the array (not push) so $state tracks the change.
        const next = lines.length >= BUFFER_CAP
          ? [...lines.slice(lines.length - BUFFER_CAP + 1), line]
          : [...lines, line];
        lines = next;
        totalReceived = totalReceived + 1;
      }
    } catch (err) {
      if (controller.signal.aborted) return;
      streamError = err instanceof Error ? err.message : String(err);
    } finally {
      if (abort === controller) {
        streaming = false;
        abort = null;
      }
    }
  }

  function restart() {
    if (debounceTimer !== null) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      debounceTimer = null;
      void runStream();
    }, 250);
  }

  // Debounced so typing in contains/job_id/media_id doesn't open one stream
  // per keystroke.
  $effect(() => {
    service; level; jobId; mediaId; contains; tailLines; lookbackSeconds;
    restart();
    return () => {
      if (debounceTimer !== null) clearTimeout(debounceTimer);
      abort?.abort();
    };
  });

  $effect(() => {
    lines.length;
    if (!pinnedToBottom || !viewport) return;
    queueMicrotask(() => {
      if (viewport) viewport.scrollTop = viewport.scrollHeight;
    });
  });

  function onScroll() {
    if (!viewport) return;
    const distance = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight;
    pinnedToBottom = distance < 24;
  }

  function jumpToTail() {
    if (!viewport) return;
    viewport.scrollTop = viewport.scrollHeight;
    pinnedToBottom = true;
  }

  function levelVariant(l: string): "ok" | "warn" | "err" | "pending" | "neutral" {
    const v = (l || "").toUpperCase();
    if (v === "ERROR" || v === "FATAL") return "err";
    if (v === "WARN" || v === "WARNING") return "warn";
    if (v === "DEBUG" || v === "TRACE") return "pending";
    return "neutral";
  }

  function shortService(s: string): string {
    return s.replace(/^msg-/, "");
  }

  function toggleExpanded(id: number) {
    const next = new Set(expanded);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    expanded = next;
  }

  function labelEntries(labels: { [key: string]: string }): { key: string; value: string }[] {
    return Object.entries(labels)
      .map(([key, value]) => ({ key, value }))
      .sort((a, b) => a.key.localeCompare(b.key));
  }

  function clearBuffer() {
    lines = [];
    expanded = new Set();
    lineIds = new WeakMap();
  }

  function clampTailLines(value: number): number {
    if (!Number.isFinite(value) || value <= 0) return 0;
    return Math.min(Math.floor(value), MAX_TAIL_LINES);
  }

  function formatDuration(seconds: number): string {
    if (seconds >= 7 * 24 * 60 * 60) return `${seconds / (7 * 24 * 60 * 60)}w`;
    if (seconds >= 24 * 60 * 60) return `${seconds / (24 * 60 * 60)}d`;
    if (seconds >= 60 * 60) return `${seconds / (60 * 60)}h`;
    return `${seconds / 60}m`;
  }
</script>

<section>
  <div class="filter-bar">
    <label>
      service
      <select bind:value={service}>
        <option value="">(all)</option>
        <option value="media-service-api">media-service-api</option>
        <option value="outbox-relay">outbox-relay</option>
        <option value="generation-worker">generation-worker</option>
      </select>
    </label>
    <label>
      level
      <select bind:value={level}>
        <option value="">(all)</option>
        <option value="INFO">INFO</option>
        <option value="WARN">WARN</option>
        <option value="ERROR">ERROR</option>
        <option value="DEBUG">DEBUG</option>
      </select>
    </label>
    <label>
      job_id
      <input type="text" bind:value={jobId} placeholder="(optional)" style="width: 160px" spellcheck="false" />
    </label>
    <label>
      media_id
      <input type="text" bind:value={mediaId} placeholder="(optional)" style="width: 160px" spellcheck="false" />
    </label>
    <label>
      contains
      <input type="text" bind:value={contains} placeholder="substring" style="width: 180px" spellcheck="false" />
    </label>
    <label>
      tail
      <input
        type="number"
        bind:value={tailLines}
        min="0"
        max="1000"
        step="50"
        style="width: 70px"
      />
    </label>
    <label>
      lookback
      <select bind:value={lookbackSeconds}>
        {#each lookbackOptions as option}
          <option value={option.value}>{option.label}</option>
        {/each}
      </select>
    </label>

    <div class="status-cluster">
      <span class="live" class:on={streaming} aria-label={streaming ? "streaming" : "stopped"}></span>
      <span class="dim">{streaming ? "live" : "idle"} · {lines.length}/{BUFFER_CAP} · rx {totalReceived}</span>
      {#if !pinnedToBottom}
        <button onclick={jumpToTail} class="tail-btn">tail ↓</button>
      {/if}
      <button onclick={clearBuffer} disabled={lines.length === 0}>clear</button>
    </div>
  </div>

  {#if streamError}
    <div class="err-bar">stream err · {streamError} <button class="retry" onclick={restart}>retry</button></div>
  {/if}

  <div
    class="viewport"
    bind:this={viewport}
    onscroll={onScroll}
  >
    {#if lines.length === 0}
      {#if streaming}
        <EmptyState title={`no logs in last ${effectiveWindowLabel}`} hint="the live stream is still open for new lines." />
      {:else}
        <EmptyState title="no logs" hint="the stream is idle. Adjust filters and the tail will resume." />
      {/if}
    {:else}
      <ol class="lines">
        {#each lines as line (lineKey(line))}
          {@const id = lineKey(line)}
          {@const tone = levelVariant(line.level)}
          {@const hasLabels = Object.keys(line.labels).length > 0}
          {@const isOpen = expanded.has(id)}
          <li
            class="line"
            class:expanded={isOpen}
            class:err={tone === "err"}
            class:warn={tone === "warn"}
          >
            <button
              class="line-row"
              class:has-labels={hasLabels}
              onclick={hasLabels ? () => toggleExpanded(id) : undefined}
              disabled={!hasLabels}
            >
              <span class="ts">{fmtTimeMs(line.ts)}</span>
              <span class="lv">
                <Pill variant={tone}>{line.level || "—"}</Pill>
              </span>
              <span class="svc">{shortService(line.service) || "—"}</span>
              <span class="body">{line.body}</span>
            </button>

            {#if isOpen && hasLabels}
              <div class="labels">
                {#each labelEntries(line.labels) as l (l.key)}
                  <span class="lbl" title={l.value}>
                    <span class="lbl-k">{l.key}</span>
                    <span class="lbl-v">{l.value}</span>
                  </span>
                {/each}
              </div>
            {/if}
          </li>
        {/each}
      </ol>
    {/if}
  </div>
</section>

<style>
  section {
    display: flex;
    flex-direction: column;
    height: 100%;
  }

  .filter-bar {
    display: flex;
    flex-wrap: wrap;
    gap: 14px;
    padding: 12px 16px;
    border-bottom: 1px solid var(--border);
    background: var(--bg-panel);
    align-items: center;
  }

  .filter-bar input,
  .filter-bar select {
    height: 32px;
    padding: 4px 10px;
    font-size: 13.5px;
  }

  .filter-bar label {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.10em;
    font-family: var(--font-sans);
    font-weight: 500;
  }

  .status-cluster {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-left: auto;
    font-size: 13px;
    font-family: var(--font-sans);
  }

  .live {
    width: 9px;
    height: 9px;
    border-radius: 50%;
    background: var(--fg-muted);
  }

  .live.on {
    background: var(--accent);
    animation: pulse 1.4s ease-in-out infinite;
  }

  @keyframes pulse {
    0%, 100% { opacity: 0.45; transform: scale(0.85); }
    50% { opacity: 1; transform: scale(1.15); }
  }

  .tail-btn {
    border-color: var(--accent);
    color: var(--accent);
  }

  .viewport {
    flex: 1;
    overflow: auto;
    background: var(--bg-panel);
  }

  .lines {
    list-style: none;
    margin: 0;
    padding: 0;
  }

  .line {
    border-bottom: 1px solid var(--border);
  }

  /* Subtle level-tinted left rail. The strip itself sits on cream and
     reads as a margin rule rather than a colour flood. */
  .line.err { border-left: 3px solid var(--err); }
  .line.warn { border-left: 3px solid var(--warn); }

  .line-row {
    display: grid;
    grid-template-columns: 130px 86px 160px minmax(0, 1fr);
    column-gap: 16px;
    align-items: baseline;
    width: 100%;
    padding: 6px 16px;
    border: none;
    background: transparent;
    color: var(--fg-default);
    text-align: left;
    font-family: var(--font-mono);
    font-size: 13.5px;
    line-height: 1.55;
  }

  .line-row.has-labels { cursor: pointer; }
  .line-row.has-labels:hover { background: var(--bg-panel-hover); }
  .line-row:disabled { cursor: default; }

  .line.expanded > .line-row {
    background: var(--bg-panel-hover);
  }

  .ts {
    color: var(--fg-dim);
    font-size: 13px;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }

  .lv { display: inline-flex; }

  .svc {
    color: var(--fg-dim);
    font-size: 11.5px;
    text-transform: uppercase;
    letter-spacing: 0.09em;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-family: var(--font-sans);
    font-weight: 500;
  }

  .body {
    color: var(--fg-default);
    word-break: break-word;
    white-space: pre-wrap;
  }

  .labels {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    padding: 6px 16px 12px calc(130px + 86px + 160px + 48px + 16px);
    background: var(--bg-panel-hover);
  }

  .lbl {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    font-family: var(--font-mono);
    border: 1px solid var(--border);
    background: var(--bg-panel);
    padding: 2px 8px;
    border-radius: 2px;
  }

  .lbl-k {
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.09em;
    font-size: 11px;
    font-family: var(--font-sans);
    font-weight: 500;
  }

  .lbl-v {
    color: var(--fg-default);
  }

  /* Override of the global .err-bar so the inline retry button sits flush
     against the right edge of the strip. */
  .err-bar {
    display: flex;
    align-items: center;
    gap: 14px;
  }

  .retry {
    margin-left: auto;
    border-color: var(--err);
    color: var(--err);
  }

  .dim { color: var(--fg-dim); }
</style>
