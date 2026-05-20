<script lang="ts">
  import { localOpsClient } from "../../shared/local-ops/client";
  import type { QueueStat } from "../../shared/local-ops/types";
  import { fmtClock } from "../../shared/time";
  import EmptyState from "../../lib/EmptyState.svelte";
  import MutationButton from "../../lib/MutationButton.svelte";

  type SortKey = "name" | "visible" | "inFlight" | "oldest" | "dlq";
  type SortDir = "asc" | "desc";

  let rows = $state<QueueStat[]>([]);
  let loading = $state(false);
  let lastError = $state<string | null>(null);
  let lastRefreshAt = $state<number>(Date.now());
  let redriveResult = $state<Record<string, { moved: number; failed: number }>>({});

  let primarySortKey = $state<SortKey>("visible");
  let primarySortDir = $state<SortDir>("desc");
  let dlqSortKey = $state<SortKey>("visible");
  let dlqSortDir = $state<SortDir>("desc");
  let activeOnly = $state(false);

  function isDlq(name: string): boolean {
    return name.endsWith("-dlq");
  }

  function dlqParent(name: string): string {
    return name.replace(/-dlq$/, "");
  }

  function isActive(q: QueueStat): boolean {
    return q.visible + q.inFlight + q.dlqCount > 0;
  }

  // Compact seconds → "12s" / "2m" / "2m05s" / "1h12m". The console-wide
  // fmtDuration takes ms and renders too verbosely for at-a-glance columns.
  function fmtSecs(n: number): string {
    if (n <= 0) return "";
    if (n < 60) return `${n}s`;
    if (n < 3600) {
      const m = Math.floor(n / 60);
      const s = n % 60;
      return s > 0 ? `${m}m${s.toString().padStart(2, "0")}s` : `${m}m`;
    }
    const h = Math.floor(n / 3600);
    const m = Math.floor((n % 3600) / 60);
    return m > 0 ? `${h}h${m.toString().padStart(2, "0")}m` : `${h}h`;
  }

  function keyVal(q: QueueStat, k: SortKey): number | string {
    switch (k) {
      case "name": return q.name;
      case "visible": return q.visible;
      case "inFlight": return q.inFlight;
      case "oldest": return q.oldestMessageAgeSeconds;
      case "dlq": return q.dlqCount;
    }
  }

  function sortRows(qs: QueueStat[], key: SortKey, dir: SortDir): QueueStat[] {
    const sign = dir === "asc" ? 1 : -1;
    return [...qs].sort((a, b) => {
      const av = keyVal(a, key);
      const bv = keyVal(b, key);
      if (typeof av === "string" && typeof bv === "string") {
        return av.localeCompare(bv) * sign;
      }
      return ((av as number) - (bv as number)) * sign;
    });
  }

  function clickPrimarySort(k: SortKey) {
    if (primarySortKey === k) {
      primarySortDir = primarySortDir === "asc" ? "desc" : "asc";
    } else {
      primarySortKey = k;
      primarySortDir = k === "name" ? "asc" : "desc";
    }
  }

  function clickDlqSort(k: SortKey) {
    if (dlqSortKey === k) {
      dlqSortDir = dlqSortDir === "asc" ? "desc" : "asc";
    } else {
      dlqSortKey = k;
      dlqSortDir = k === "name" ? "asc" : "desc";
    }
  }

  async function load() {
    loading = true;
    lastError = null;
    try {
      const res = await localOpsClient.queueDepths();
      rows = res.queues;
      lastRefreshAt = Date.now();
    } catch (err) {
      lastError = err instanceof Error ? err.message : String(err);
      rows = [];
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    load();
  });

  $effect(() => {
    const id = setInterval(load, 2000);
    return () => clearInterval(id);
  });

  let primary = $derived(rows.filter((q) => !isDlq(q.name)));
  let dlqs = $derived(rows.filter((q) => isDlq(q.name)));

  let totalVisible = $derived(primary.reduce((a, q) => a + q.visible, 0));
  let totalInFlight = $derived(primary.reduce((a, q) => a + q.inFlight, 0));
  let totalDlqDepth = $derived(dlqs.reduce((a, q) => a + q.visible, 0));
  let stuckDlqCount = $derived(dlqs.filter((q) => q.visible > 0).length);
  let backloggedCount = $derived(primary.filter((q) => q.visible + q.inFlight > 0).length);

  let oldest = $derived.by(() => {
    let winner: QueueStat | null = null;
    for (const q of primary) {
      if (q.oldestMessageAgeSeconds <= 0) continue;
      if (!winner || q.oldestMessageAgeSeconds > winner.oldestMessageAgeSeconds) {
        winner = q;
      }
    }
    return winner;
  });

  let primaryVisibleRows = $derived.by(() => {
    const base = activeOnly ? primary.filter(isActive) : primary;
    return sortRows(base, primarySortKey, primarySortDir);
  });

  let dlqVisibleRows = $derived.by(() => {
    const base = activeOnly ? dlqs.filter((q) => q.visible > 0) : dlqs;
    return sortRows(base, dlqSortKey, dlqSortDir);
  });

  let hiddenPrimaryCount = $derived(primary.length - primaryVisibleRows.length);
  let hiddenDlqCount = $derived(dlqs.length - dlqVisibleRows.length);

  async function doPurge(name: string) {
    await localOpsClient.purgeQueue({ queueName: name });
    await load();
  }

  async function doRedrive(name: string) {
    const res = await localOpsClient.redriveDlq({ dlqName: name, limit: 10 });
    redriveResult = { ...redriveResult, [name]: { moved: res.moved, failed: res.failed } };
    await load();
  }
</script>

<section>
  <header class="panel-header">
    <span>queues</span>
    <span class="hdr-meta">
      <button
        class="toggle"
        class:on={activeOnly}
        onclick={() => (activeOnly = !activeOnly)}
        title="hide queues with zero traffic"
      >
        <span class="dot"></span> active only
      </button>
      <span class="sep">·</span>
      <span>auto-refresh 2s · last {fmtClock(lastRefreshAt)}</span>
      <button onclick={load} disabled={loading} class="hdr-btn">
        {loading ? "…" : "refresh"}
      </button>
    </span>
  </header>

  {#if lastError}
    <div class="err-bar">err · {lastError}</div>
  {/if}

  {#if rows.length === 0 && !loading}
    <div class="table-host">
      <EmptyState
        title="no queues"
        hint="the topology hasn't been provisioned yet — run `make tf-up`."
      />
    </div>
  {:else}
    <div class="stats">
      <div class="tile">
        <div class="tile-label">in queue</div>
        <div class="tile-value">{totalVisible}</div>
        <div class="tile-sub">{backloggedCount}/{primary.length} q backlogged</div>
      </div>
      <div class="tile">
        <div class="tile-label">in flight</div>
        <div class="tile-value" class:hi-warn={totalInFlight > 0}>{totalInFlight}</div>
        <div class="tile-sub">workers processing</div>
      </div>
      <div class="tile" class:tile-err={totalDlqDepth > 0}>
        <div class="tile-label">dead-letter</div>
        <div class="tile-value" class:hi-err={totalDlqDepth > 0}>{totalDlqDepth}</div>
        <div class="tile-sub">
          {#if stuckDlqCount > 0}
            {stuckDlqCount} of {dlqs.length} dlq stuck
          {:else}
            {dlqs.length} dlq drained
          {/if}
        </div>
      </div>
      <div class="tile">
        <div class="tile-label">oldest</div>
        <div class="tile-value">
          {#if oldest}{fmtSecs(oldest.oldestMessageAgeSeconds)}{:else}—{/if}
        </div>
        <div class="tile-sub mono" title={oldest?.name ?? ""}>
          {#if oldest}
            {oldest.tierClass || oldest.name}
          {:else}
            nothing stuck
          {/if}
        </div>
      </div>
    </div>

    <div class="scroll-host">
      <div class="section-label">
        <span class="section-title">primary · {primary.length}</span>
        <span class="section-note dim">
          {#if activeOnly && hiddenPrimaryCount > 0}
            {hiddenPrimaryCount} idle hidden
          {:else}
            queues consumed by workers
          {/if}
        </span>
      </div>

      {#if primaryVisibleRows.length === 0}
        <div class="section-empty">
          {activeOnly ? "all primary queues are idle." : "no primary queues."}
        </div>
      {:else}
        <table class="dense">
          <thead>
            <tr>
              <th>
                <button class="sort-th" onclick={() => clickPrimarySort("name")}>
                  name
                  <span class="caret" class:on={primarySortKey === "name"}>
                    {primarySortKey === "name" && primarySortDir === "asc" ? "↑" : "↓"}
                  </span>
                </button>
              </th>
              <th>tier / class</th>
              <th class="num">
                <button class="sort-th" onclick={() => clickPrimarySort("visible")}>
                  visible
                  <span class="caret" class:on={primarySortKey === "visible"}>
                    {primarySortKey === "visible" && primarySortDir === "asc" ? "↑" : "↓"}
                  </span>
                </button>
              </th>
              <th class="num">
                <button class="sort-th" onclick={() => clickPrimarySort("inFlight")}>
                  in&nbsp;flight
                  <span class="caret" class:on={primarySortKey === "inFlight"}>
                    {primarySortKey === "inFlight" && primarySortDir === "asc" ? "↑" : "↓"}
                  </span>
                </button>
              </th>
              <th class="num">
                <button class="sort-th" onclick={() => clickPrimarySort("oldest")}>
                  oldest
                  <span class="caret" class:on={primarySortKey === "oldest"}>
                    {primarySortKey === "oldest" && primarySortDir === "asc" ? "↑" : "↓"}
                  </span>
                </button>
              </th>
              <th class="num">
                <button class="sort-th" onclick={() => clickPrimarySort("dlq")}>
                  dlq
                  <span class="caret" class:on={primarySortKey === "dlq"}>
                    {primarySortKey === "dlq" && primarySortDir === "asc" ? "↑" : "↓"}
                  </span>
                </button>
              </th>
              <th class="action-col">action</th>
            </tr>
          </thead>
          <tbody>
            {#each primaryVisibleRows as q (q.name)}
              <tr class:active={isActive(q)}>
                <td class="q-name">{q.name}</td>
                <td>
                  {#if q.tierClass}
                    <span class="chip">{q.tierClass}</span>
                  {:else}
                    <span class="muted">—</span>
                  {/if}
                </td>
                <td class="num" class:hi-ok={q.visible > 0}>{q.visible}</td>
                <td class="num" class:hi-warn={q.inFlight > 0}>{q.inFlight}</td>
                <td class="num">
                  {#if q.oldestMessageAgeSeconds > 0}
                    {fmtSecs(q.oldestMessageAgeSeconds)}
                  {:else}
                    <span class="muted">—</span>
                  {/if}
                </td>
                <td class="num" class:hi-err={q.dlqCount > 0}>{q.dlqCount}</td>
                <td class="action-col">
                  <MutationButton
                    label="purge"
                    confirmTitle="purge queue"
                    confirmBody="Drop every visible + in-flight message on this queue. Cannot be undone."
                    target={q.name}
                    onConfirm={() => doPurge(q.name)}
                  />
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}

      <div class="section-label dlq-section-label">
        <span class="section-title">
          dead-letter · {dlqs.length}
          {#if stuckDlqCount > 0}
            <span class="badge-err">{stuckDlqCount} stuck</span>
          {/if}
        </span>
        <span class="section-note dim">
          {#if activeOnly && hiddenDlqCount > 0}
            {hiddenDlqCount} empty hidden
          {:else}
            terminal-failure parking lots
          {/if}
        </span>
      </div>

      {#if dlqVisibleRows.length === 0}
        <div class="section-empty">
          {activeOnly ? "all dlqs are empty." : "no dlqs provisioned."}
        </div>
      {:else}
        <table class="dense">
          <thead>
            <tr>
              <th>
                <button class="sort-th" onclick={() => clickDlqSort("name")}>
                  name
                  <span class="caret" class:on={dlqSortKey === "name"}>
                    {dlqSortKey === "name" && dlqSortDir === "asc" ? "↑" : "↓"}
                  </span>
                </button>
              </th>
              <th>source</th>
              <th class="num">
                <button class="sort-th" onclick={() => clickDlqSort("visible")}>
                  depth
                  <span class="caret" class:on={dlqSortKey === "visible"}>
                    {dlqSortKey === "visible" && dlqSortDir === "asc" ? "↑" : "↓"}
                  </span>
                </button>
              </th>
              <th class="num">
                <button class="sort-th" onclick={() => clickDlqSort("oldest")}>
                  oldest
                  <span class="caret" class:on={dlqSortKey === "oldest"}>
                    {dlqSortKey === "oldest" && dlqSortDir === "asc" ? "↑" : "↓"}
                  </span>
                </button>
              </th>
              <th class="action-col">action</th>
            </tr>
          </thead>
          <tbody>
            {#each dlqVisibleRows as q (q.name)}
              {@const result = redriveResult[q.name]}
              <tr class:stuck={q.visible > 0} class:active={q.visible > 0}>
                <td class="q-name">{q.name}</td>
                <td class="mono">{dlqParent(q.name)}</td>
                <td class="num" class:hi-err={q.visible > 0}>{q.visible}</td>
                <td class="num">
                  {#if q.oldestMessageAgeSeconds > 0}
                    {fmtSecs(q.oldestMessageAgeSeconds)}
                  {:else}
                    <span class="muted">—</span>
                  {/if}
                </td>
                <td class="action-col">
                  <MutationButton
                    label="redrive"
                    confirmTitle="redrive dlq"
                    confirmBody="Move up to 10 messages from this DLQ back to the parent queue."
                    target={q.name}
                    danger={false}
                    onConfirm={() => doRedrive(q.name)}
                  />
                  {#if result}
                    <span class="redrive-out">
                      <span class:hi-ok={result.moved > 0}>{result.moved} moved</span>
                      <span class="sep">·</span>
                      <span class:hi-err={result.failed > 0}>{result.failed} failed</span>
                    </span>
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
  {/if}
</section>

<style>
  section {
    display: flex;
    flex-direction: column;
    height: 100%;
  }

  .hdr-meta {
    display: flex;
    align-items: center;
    gap: 10px;
    text-transform: none;
    letter-spacing: 0;
    font-family: var(--font-sans);
    font-size: 12.5px;
    color: var(--fg-dim);
  }

  .hdr-meta .sep {
    color: var(--fg-muted);
  }

  .hdr-btn {
    padding: 4px 10px;
    font-size: 12px;
  }

  /* compact toggle pill — sits in the header strip; "on" state borrows accent */
  .toggle {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 3px 10px 3px 8px;
    background: var(--bg-panel);
    color: var(--fg-dim);
    border: 1px solid var(--border);
    font-family: var(--font-sans);
    font-size: 11.5px;
cursor: pointer;
    border-radius: 2px;
  }
  .toggle .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--border-strong);
    transition: background 120ms ease;
  }
  .toggle:hover { border-color: var(--border-strong); color: var(--fg-default); }
  .toggle.on {
    background: var(--accent-dim);
    border-color: var(--accent);
    color: var(--accent-strong);
  }
  .toggle.on .dot {
    background: var(--accent);
    box-shadow: 0 0 0 3px rgba(31, 63, 191, 0.18);
  }

  /* ── stat tiles ───────────────────────────────────────────────────── */

  .stats {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 1px;
    background: var(--border);
    border-bottom: 1px solid var(--border);
  }

  .tile {
    background: var(--bg-panel);
    padding: 10px 16px;
    display: grid;
    grid-template-columns: 1fr auto;
    grid-template-rows: auto auto;
    column-gap: 14px;
    row-gap: 2px;
    align-items: baseline;
    min-width: 0;
  }

  .tile-err {
    background: var(--err-dim);
  }

  .tile-label {
    grid-column: 1;
    grid-row: 1;
    font-size: 10.5px;
color: var(--fg-dim);
    font-family: var(--font-sans);
    font-weight: 500;
  }

  .tile-value {
    grid-column: 2;
    grid-row: 1 / span 2;
    font-family: var(--font-mono);
    font-size: 22px;
    line-height: 1;
    color: var(--fg-bright);
    font-weight: 500;
    letter-spacing: -0.01em;
    font-variant-numeric: tabular-nums;
    align-self: center;
  }

  .tile-sub {
    grid-column: 1;
    grid-row: 2;
    font-size: 12px;
    color: var(--fg-default);
    font-family: var(--font-sans);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tile-sub.mono {
    font-family: var(--font-mono);
    font-size: 12.5px;
    color: var(--fg-default);
  }

  /* ── sectioned tables ─────────────────────────────────────────────── */

  .scroll-host {
    flex: 1;
    overflow: auto;
    background: var(--bg-panel);
  }

  .section-label {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
    padding: 14px 16px 8px;
    font-family: var(--font-sans);
    color: var(--fg-default);
    background: var(--bg-panel);
    position: sticky;
    top: 0;
    z-index: 2;
    border-bottom: 1px solid var(--border);
  }

  .section-title {
    font-size: 11.5px;
font-weight: 500;
  }

  .section-note {
    font-size: 12px;
  }

  .dlq-section-label {
    margin-top: 8px;
    border-top: 1px solid var(--border);
  }

  .badge-err {
    display: inline-block;
    margin-left: 8px;
    padding: 1px 7px;
    font-size: 10.5px;
    border: 1px solid var(--err);
    color: var(--err);
    background: var(--err-dim);
border-radius: 2px;
    font-weight: 600;
  }

  .section-empty {
    padding: 18px 16px 22px;
    color: var(--fg-muted);
    font-size: 13px;
    font-family: var(--font-sans);
  }

  /* table thead resets — sticky lives on .section-label here, not the th */
  .scroll-host :global(table.dense thead th) {
    position: static;
    background: var(--bg-panel);
  }

  /* clickable header buttons — render like a heading, not like a button */
  .sort-th {
    background: none;
    border: 0;
    padding: 0;
    margin: 0;
    color: inherit;
    font: inherit;
    text-transform: inherit;
    letter-spacing: inherit;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .sort-th:hover { color: var(--accent); background: none; }
  .sort-th .caret {
    color: var(--fg-muted);
    font-size: 10px;
    opacity: 0;
    transition: opacity 120ms ease;
  }
  .sort-th .caret.on {
    opacity: 1;
    color: var(--accent);
  }
  .sort-th:hover .caret { opacity: 1; }

  /* row hover — quiet, non-clickable feel; left accent stripe marks active rows */
  tbody tr:hover td {
    background: var(--bg-panel-hover);
  }

  tbody tr.active td:first-child {
    box-shadow: inset 2px 0 0 0 var(--accent);
  }

  tbody tr.stuck td {
    background: rgba(176, 51, 37, 0.035);
  }
  tbody tr.stuck.active td:first-child {
    box-shadow: inset 2px 0 0 0 var(--err);
  }
  tbody tr.stuck:hover td {
    background: rgba(176, 51, 37, 0.07);
  }

  /* tier/class chip — visual chunking without leaning on the heavy Pill component */
  .chip {
    display: inline-block;
    padding: 1px 8px;
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--fg-bright);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    border-radius: 2px;
    letter-spacing: -0.005em;
  }

  .action-col {
    width: 240px;
    white-space: nowrap;
    overflow: visible;
  }

  .redrive-out {
    margin-left: 10px;
    font-size: 12.5px;
    color: var(--fg-dim);
    font-family: var(--font-sans);
    font-variant-numeric: tabular-nums;
  }

  .redrive-out .sep {
    margin: 0 4px;
    color: var(--fg-muted);
  }

  td.q-name {
    font-family: var(--font-mono);
    font-size: 14px;
    font-weight: 500;
    color: var(--fg-bright);
    letter-spacing: -0.005em;
  }

  .hi-ok { color: var(--accent); font-weight: 500; }
  .hi-warn { color: var(--warn); font-weight: 500; }
  .hi-err { color: var(--err); font-weight: 500; }

  .muted { color: var(--fg-muted); }
</style>
