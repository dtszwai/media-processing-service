<script lang="ts">
  import { create } from "@bufbuild/protobuf";
  import {
    ListJobsRequestSchema,
    type JobSummary,
  } from "@media-service/api-client/gen/mediaservice/ops/v1/ops_pb.js";
  import { opsClient } from "../../shared/ops";
  import { navigate } from "../../shared/route.svelte";
  import { fmtClock, fmtDateTime } from "../../shared/time";
  import { jobStatusVariant } from "../trace/status";
  import Pill from "../../lib/Pill.svelte";
  import JobActions from "./JobActions.svelte";

  let status = $state("");
  let outputType = $state("");
  let rows = $state<JobSummary[]>([]);
  let loading = $state(false);
  let lastError = $state<string | null>(null);
  let lastRefreshAt = $state<number>(Date.now());

  async function load() {
    loading = true;
    lastError = null;
    try {
      const req = create(ListJobsRequestSchema, {
        status,
        outputType,
        limit: 200,
      });
      const res = await opsClient.listJobs(req);
      rows = res.jobs;
      lastRefreshAt = Date.now();
    } catch (err) {
      lastError = err instanceof Error ? err.message : String(err);
      rows = [];
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    status; outputType;
    load();
  });

  $effect(() => {
    const id = setInterval(load, 5000);
    return () => clearInterval(id);
  });

  function onRowClick(row: JobSummary) {
    navigate(`/trace/${row.jobId}`);
  }

</script>

<section>
  <div class="filter-bar">
    <label>
      status
      <select bind:value={status}>
        <option value="">(any)</option>
        <option value="QUEUED">QUEUED</option>
        <option value="RUNNING">RUNNING</option>
        <option value="BLOCKED">BLOCKED</option>
        <option value="COMPLETE">COMPLETE</option>
        <option value="FAILED">FAILED</option>
        <option value="CANCELLED">CANCELLED</option>
      </select>
    </label>
    <label>
      output_type
      <select bind:value={outputType}>
        <option value="">(any)</option>
        <option value="IMAGE">IMAGE</option>
        <option value="AUDIO">AUDIO</option>
      </select>
    </label>
    <span class="meta" style="margin-left:auto">
      {rows.length} rows · auto-refresh 5s · last {fmtClock(lastRefreshAt)}
    </span>
    <button onclick={load} disabled={loading}>{loading ? "…" : "refresh"}</button>
  </div>

  {#if lastError}
    <div class="err-bar">err · {lastError}</div>
  {/if}

  <div class="table-host">
    <table class="dense">
      <thead>
        <tr>
          <th style="width:300px">job_id</th>
          <th style="width:110px">status</th>
          <th style="width:170px">stage</th>
          <th style="width:90px">type</th>
          <th style="width:90px">tier</th>
          <th style="width:120px">model</th>
          <th class="num" style="width:60px">att</th>
          <th class="num" style="width:160px">created</th>
          <th style="width:160px">error</th>
          <th style="width:300px">actions</th>
        </tr>
      </thead>
      <tbody>
        {#if rows.length === 0}
          <tr><td colspan="10" class="empty">{loading ? "loading…" : "no jobs"}</td></tr>
        {:else}
          {#each rows as r (r.jobId)}
            <tr class="clickable" onclick={() => onRowClick(r)}>
              <td title={r.jobId}><code class="id-text">{r.jobId}</code></td>
              <td><Pill variant={jobStatusVariant(r.status)}>{r.status || "—"}</Pill></td>
              <td class="mono">{r.currentStage || "—"}</td>
              <td>{r.outputType || "—"}</td>
              <td>{r.tier || "—"}</td>
              <td class="mono">{r.model || "—"}</td>
              <td class="num">{r.attempts}</td>
              <td class="num mono">{fmtDateTime(r.createdAt)}</td>
              <td class="err-cell">{r.errorCode || ""}</td>
              <td class="actions" onclick={(e) => e.stopPropagation()}>
                <JobActions jobId={r.jobId} status={r.status} onDone={load} />
              </td>
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

  tbody td {
    max-width: 360px;
  }

  td.err-cell { color: var(--err); }
  td.actions :global(.trigger) { margin-right: 6px; }

  .meta { font-size: 12.5px; color: var(--fg-dim); font-family: var(--font-sans); }
</style>
