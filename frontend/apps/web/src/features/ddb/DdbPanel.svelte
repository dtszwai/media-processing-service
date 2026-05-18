<script lang="ts">
  import { create } from "@bufbuild/protobuf";
  import {
    ScanDdbRequestSchema,
    GetDdbRowRequestSchema,
    PutDdbAttrRequestSchema,
    DeleteDdbRowRequestSchema,
    type DdbRow,
  } from "@media-service/api-client/gen/mediaservice/ops/v1/ops_pb.js";
  import { opsClient } from "../../shared/ops";
  import { navigate, route } from "../../shared/route.svelte";
  import EmptyState from "../../lib/EmptyState.svelte";
  import KeyValueGrid from "../../lib/KeyValueGrid.svelte";
  import CodeBox from "../../lib/CodeBox.svelte";
  import Dialog from "../../lib/Dialog.svelte";
  import MutationButton from "../../lib/MutationButton.svelte";
  import Pill from "../../lib/Pill.svelte";

  // Three modes selected by the hash params:
  //   #/ddb              → scan
  //   #/ddb/:pk          → list rows under pk
  //   #/ddb/:pk/:sk      → row detail
  let pkParam = $derived(route.params[0] ? decodeURIComponent(route.params[0]) : "");
  let skParam = $derived(route.params[1] ? decodeURIComponent(route.params[1]) : "");

  let mode = $derived<"scan" | "list" | "detail">(
    skParam ? "detail" : pkParam ? "list" : "scan",
  );

  // Canonical PK prefixes owned by feature keys.go files. There is no top-level
  // MEDIA# partition — media rows live under TENANT#<id>#MEDIA#<id>. OUTBOX is
  // OUTBOX_CHECKPOINT, not OUTBOX. Order: hottest partitions first.
  const PREFIX_HINTS = [
    "JOB#",
    "TENANT#",
    "RESERVOIR#",
    "IDEMPOTENCY#",
    "AUDIT#",
    "OUTBOX_CHECKPOINT#",
    "BUDGET#",
    "USER#",
  ];

  let pkPrefix = $state("");
  let skPrefix = $state("");
  let pageRows = $state<DdbRow[]>([]);
  let cursor = $state("");
  let nextCursor = $state("");
  let cursorHistory = $state<string[]>([]);
  let scanLoading = $state(false);
  let scanError = $state<string | null>(null);

  async function runScan(useCursor = "") {
    scanLoading = true;
    scanError = null;
    try {
      const req = create(ScanDdbRequestSchema, {
        pkPrefix: pkPrefix.trim(),
        skPrefix: skPrefix.trim(),
        limit: 100,
        cursor: useCursor,
      });
      const res = await opsClient.scanDdb(req);
      pageRows = res.rows;
      nextCursor = res.nextCursor;
      cursor = useCursor;
    } catch (err) {
      scanError = err instanceof Error ? err.message : String(err);
      pageRows = [];
      nextCursor = "";
    } finally {
      scanLoading = false;
    }
  }

  function applyPrefix(p: string) {
    pkPrefix = p;
    skPrefix = "";
    cursorHistory = [];
    runScan("");
  }

  function nextPage() {
    if (!nextCursor) return;
    cursorHistory = [...cursorHistory, cursor];
    runScan(nextCursor);
  }

  function prevPage() {
    if (cursorHistory.length === 0) return;
    const prev = cursorHistory[cursorHistory.length - 1];
    cursorHistory = cursorHistory.slice(0, -1);
    runScan(prev);
  }

  function onSubmit(e: Event) {
    e.preventDefault();
    cursorHistory = [];
    runScan("");
  }

  function viewRow(r: DdbRow) {
    navigate(`/ddb/${encodeURIComponent(r.pk)}/${encodeURIComponent(r.sk)}`);
  }

  function viewPk(r: DdbRow) {
    pkPrefix = r.pk;
    skPrefix = "";
    navigate(`/ddb/${encodeURIComponent(r.pk)}`);
  }

  let listRows = $state<DdbRow[]>([]);
  let listLoading = $state(false);
  let listError = $state<string | null>(null);

  async function loadList() {
    if (!pkParam) return;
    listLoading = true;
    listError = null;
    try {
      const req = create(ScanDdbRequestSchema, {
        pkPrefix: pkParam,
        skPrefix: "",
        limit: 200,
        cursor: "",
      });
      const res = await opsClient.scanDdb(req);
      // ScanDdb is prefix-based; tighten to exact-PK so list mode only shows
      // rows in the requested partition.
      listRows = res.rows.filter((r) => r.pk === pkParam);
    } catch (err) {
      listError = err instanceof Error ? err.message : String(err);
      listRows = [];
    } finally {
      listLoading = false;
    }
  }

  let detailRow = $state<DdbRow | null>(null);
  let detailLoading = $state(false);
  let detailError = $state<string | null>(null);

  async function loadDetail() {
    if (!pkParam || !skParam) return;
    detailLoading = true;
    detailError = null;
    try {
      const req = create(GetDdbRowRequestSchema, { pk: pkParam, sk: skParam });
      const res = await opsClient.getDdbRow(req);
      detailRow = res.row ?? null;
    } catch (err) {
      detailError = err instanceof Error ? err.message : String(err);
      detailRow = null;
    } finally {
      detailLoading = false;
    }
  }

  // Plain `let` (not $state) — a one-shot flag for the initial scan. Reading
  // pageRows.length as the auto-load gate would re-trip the effect whenever
  // a prefix scan returned zero rows, hammering the API into 429s.
  let autoScannedOnce = false;

  $effect(() => {
    if (mode === "list") {
      loadList();
    } else if (mode === "detail") {
      loadDetail();
    } else if (mode === "scan" && !autoScannedOnce) {
      autoScannedOnce = true;
      runScan("");
    }
  });

  function isComplex(v: unknown): boolean {
    if (v === null || v === undefined) return false;
    if (typeof v === "object") return true;
    return false;
  }

  function fmtScalar(v: unknown): string {
    if (v === null) return "null";
    if (v === undefined) return "—";
    if (typeof v === "string") return v;
    if (typeof v === "number" || typeof v === "boolean") return String(v);
    if (typeof v === "bigint") return v.toString();
    return String(v);
  }

  let detailScalars = $derived.by(() => {
    if (!detailRow) return [] as { key: string; value: string }[];
    const attrs = detailRow.attributes ?? {};
    return Object.entries(attrs)
      .filter(([, v]) => !isComplex(v))
      .map(([k, v]) => ({ key: k, value: fmtScalar(v) }))
      .sort((a, b) => a.key.localeCompare(b.key));
  });

  let detailComplex = $derived.by(() => {
    if (!detailRow) return [] as { key: string; value: unknown }[];
    const attrs = detailRow.attributes ?? {};
    return Object.entries(attrs)
      .filter(([, v]) => isComplex(v))
      .map(([k, v]) => ({ key: k, value: v }))
      .sort((a, b) => a.key.localeCompare(b.key));
  });

  // Custom dialog rather than MutationButton — needs free-form text inputs.
  let editOpen = $state(false);
  let editName = $state("");
  let editValueJson = $state("");
  let editBusy = $state(false);
  let editError = $state("");

  function openEdit() {
    editName = "";
    editValueJson = "";
    editError = "";
    editOpen = true;
  }

  async function submitEdit() {
    if (editBusy) return;
    const name = editName.trim();
    if (!name) {
      editError = "attribute name is required";
      return;
    }
    // Surface JSON syntax errors in the dialog instead of as an opaque RPC
    // failure.
    try {
      JSON.parse(editValueJson);
    } catch (err) {
      editError = `value_json: ${err instanceof Error ? err.message : String(err)}`;
      return;
    }
    editBusy = true;
    editError = "";
    try {
      await opsClient.putDdbAttr(
        create(PutDdbAttrRequestSchema, {
          pk: pkParam,
          sk: skParam,
          attributeName: name,
          valueJson: editValueJson,
        }),
      );
      editOpen = false;
      await loadDetail();
    } catch (err) {
      editError = err instanceof Error ? err.message : String(err);
    } finally {
      editBusy = false;
    }
  }

  async function deleteRow() {
    await opsClient.deleteDdbRow(
      create(DeleteDdbRowRequestSchema, { pk: pkParam, sk: skParam }),
    );
    navigate("/ddb");
  }
</script>

<section>
  {#if mode === "scan"}
    <form class="filter-bar" onsubmit={onSubmit}>
      <label>
        pk_prefix
        <input
          type="text"
          placeholder="JOB#"
          bind:value={pkPrefix}
          style="width: 200px"
          spellcheck="false"
        />
      </label>
      <label>
        sk_prefix
        <input
          type="text"
          placeholder="(optional)"
          bind:value={skPrefix}
          style="width: 200px"
          spellcheck="false"
        />
      </label>
      <button type="submit" disabled={scanLoading} class="primary">
        {scanLoading ? "…" : "scan"}
      </button>
      <span class="dim" style="margin-left:auto">
        {pageRows.length} rows{nextCursor ? " · more" : ""}
      </span>
    </form>

    <div class="prefix-pills">
      <span class="dim cap">quick prefix</span>
      {#each PREFIX_HINTS as p (p)}
        <button class="pill-btn" class:active={pkPrefix === p} onclick={() => applyPrefix(p)}>
          {p}
        </button>
      {/each}
    </div>

    {#if scanError}
      <div class="err-bar">err · {scanError}</div>
    {/if}

    <div class="table-host">
      {#if pageRows.length === 0 && !scanLoading}
        <EmptyState title="no rows" hint="adjust pk_prefix or pick a quick prefix above." />
      {:else}
        <table class="dense">
          <thead>
            <tr>
              <th style="width: 26%">pk</th>
              <th style="width: 36%">sk</th>
              <th style="width: 22%">item_type</th>
              <th class="num" style="width: 16%"></th>
            </tr>
          </thead>
          <tbody>
            {#each pageRows as r, i (r.pk + "|" + r.sk + "|" + i)}
              <tr class="clickable" onclick={() => viewRow(r)}>
                <td class="pk-cell" onclick={(e) => { e.stopPropagation(); viewPk(r); }} title={r.pk}>
                  <code class="id-text">{r.pk}</code>
                </td>
                <td title={r.sk}><code class="id-text">{r.sk}</code></td>
                <td class="mono dim">{r.itemType || "—"}</td>
                <td class="num"><span class="link">view →</span></td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>

    <div class="pager">
      <button onclick={prevPage} disabled={cursorHistory.length === 0 || scanLoading}>
        ← prev
      </button>
      <button onclick={nextPage} disabled={!nextCursor || scanLoading}>
        next →
      </button>
    </div>
  {:else if mode === "list"}
    <header class="detail-head">
      <div class="crumbs">
        <a href="#/ddb">ddb</a>
        <span class="sep">/</span>
        <code class="pk">{pkParam}</code>
      </div>
      <button onclick={loadList} disabled={listLoading}>{listLoading ? "…" : "refresh"}</button>
    </header>

    {#if listError}
      <div class="err-bar">err · {listError}</div>
    {/if}

    <div class="table-host">
      {#if listRows.length === 0 && !listLoading}
        <EmptyState title="no rows" hint="no items under this partition key." />
      {:else}
        <table class="dense">
          <thead>
            <tr>
              <th style="width: 56%">sk</th>
              <th style="width: 28%">item_type</th>
              <th class="num" style="width: 16%"></th>
            </tr>
          </thead>
          <tbody>
            {#each listRows as r, i (r.sk + "|" + i)}
              <tr class="clickable" onclick={() => viewRow(r)}>
                <td title={r.sk}><code class="id-text">{r.sk}</code></td>
                <td class="mono dim">{r.itemType || "—"}</td>
                <td class="num"><span class="link">view →</span></td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </div>
  {:else if mode === "detail"}
    <header class="detail-head">
      <div class="crumbs">
        <a href="#/ddb">ddb</a>
        <span class="sep">/</span>
        <a href={`#/ddb/${encodeURIComponent(pkParam)}`}><code class="pk">{pkParam}</code></a>
        <span class="sep">/</span>
        <code class="sk">{skParam}</code>
      </div>
      {#if detailRow?.itemType}
        <Pill variant="accent">{detailRow.itemType}</Pill>
      {/if}
      <div class="detail-actions">
        <button onclick={openEdit} disabled={!detailRow}>edit attribute</button>
        <MutationButton
          label="delete row"
          confirmTitle="delete ddb row"
          confirmBody="Permanently delete this row. Audit rows are immutable; deleting one will leave a gap."
          target={`${pkParam}  /  ${skParam}`}
          disabled={!detailRow}
          onConfirm={deleteRow}
        />
        <button onclick={loadDetail} disabled={detailLoading}>
          {detailLoading ? "…" : "refresh"}
        </button>
      </div>
    </header>

    <Dialog
      open={editOpen}
      title="edit attribute"
      onCancel={() => (editOpen = false)}
      onAccept={submitEdit}
      acceptLabel={editBusy ? "…" : "apply"}
      acceptDisabled={editBusy}
      cancelDisabled={editBusy}
      danger
    >
      <p class="dlg-body">
        Sets a single attribute on the row below. <code>value_json</code> is parsed as JSON
        (use <code>"str"</code> for strings, <code>123</code> for numbers, <code>true</code>/<code>false</code>,
        or <code>{`{...}`}</code> for maps).
      </p>
      <div class="dlg-target">{pkParam}  /  {skParam}</div>
      <label class="dlg-field">
        <span>attribute_name</span>
        <input
          type="text"
          bind:value={editName}
          spellcheck="false"
          placeholder="status"
          disabled={editBusy}
        />
      </label>
      <label class="dlg-field">
        <span>value_json</span>
        <textarea
          bind:value={editValueJson}
          spellcheck="false"
          placeholder={`"COMPLETE"`}
          rows="5"
          disabled={editBusy}
        ></textarea>
      </label>
      {#if editError}
        <div class="dlg-error">{editError}</div>
      {/if}
    </Dialog>

    {#if detailError}
      <div class="err-bar">err · {detailError}</div>
    {/if}

    {#if !detailRow && !detailLoading}
      <EmptyState title="row not found" hint={`${pkParam} / ${skParam}`} />
    {:else if detailRow}
      <div class="detail-body">
        {#if detailScalars.length > 0}
          <section class="block">
            <div class="block-label">scalar attributes · {detailScalars.length}</div>
            <KeyValueGrid entries={detailScalars} />
          </section>
        {/if}

        {#each detailComplex as entry (entry.key)}
          <section class="block">
            <CodeBox value={entry.value} label={entry.key} />
          </section>
        {/each}

        {#if detailScalars.length === 0 && detailComplex.length === 0}
          <EmptyState title="no attributes" hint="row exists but carries no attributes." />
        {/if}
      </div>
    {/if}
  {/if}
</section>

<style>
  section {
    display: flex;
    flex-direction: column;
    height: 100%;
  }

  .prefix-pills {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 16px;
    background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
    flex-wrap: wrap;
  }

  .pill-btn {
    padding: 4px 12px;
    font-size: 12.5px;
    border: 1px solid var(--border);
    background: var(--bg-base);
    color: var(--fg-default);
    font-family: var(--font-mono);
    cursor: pointer;
    transition: border-color 120ms ease, background 120ms ease, color 120ms ease;
  }

  .pill-btn:hover {
    background: var(--bg-panel-hover);
    border-color: var(--border-strong);
  }

  .pill-btn.active {
    border-color: var(--accent);
    color: var(--accent-strong);
    background: var(--accent-dim);
    font-weight: 500;
  }

  tbody td {
    max-width: 520px;
  }

  td.pk-cell { cursor: pointer; }
  td.pk-cell:hover .id-text { color: var(--accent); }

  .link { color: var(--accent); font-size: 12.5px; font-family: var(--font-sans); }

  .pager {
    display: flex;
    gap: 10px;
    padding: 12px 16px;
    border-top: 1px solid var(--border);
    background: var(--bg-panel);
  }

  .detail-head {
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
    font-size: 14px;
    font-family: var(--font-mono);
  }

  .crumbs a {
    color: var(--accent);
  }

  .crumbs .sep {
    color: var(--fg-muted);
  }

  .detail-actions {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-left: auto;
  }

  .dlg-body {
    margin: 0 0 12px 0;
    font-size: 14px;
    line-height: 1.6;
    color: var(--fg-default);
    font-family: var(--font-sans);
  }
  .dlg-body code {
    font-family: var(--font-mono);
    font-size: 13px;
    color: var(--fg-bright);
    background: var(--bg-base);
    padding: 1px 4px;
  }
  .dlg-target {
    font-family: var(--font-mono);
    background: var(--bg-base);
    border: 1px solid var(--border);
    padding: 10px 12px;
    font-size: 13px;
    color: var(--fg-bright);
    word-break: break-all;
    margin-bottom: 14px;
    border-radius: 2px;
  }
  .dlg-field {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 14px;
  }
  .dlg-field span {
    font-size: 11.5px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.10em;
    font-family: var(--font-sans);
    font-weight: 500;
  }
  .dlg-field input,
  .dlg-field textarea {
    background: var(--bg-input);
    border: 1px solid var(--border);
    color: var(--fg-bright);
    font-family: var(--font-mono);
    font-size: 13.5px;
    padding: 8px 12px;
    resize: vertical;
  }
  .dlg-field input:focus,
  .dlg-field textarea:focus {
    outline: none;
    border-color: var(--accent);
  }
  .dlg-error {
    margin-top: 6px;
    color: var(--err);
    font-size: 13px;
    border-left: 3px solid var(--err);
    padding-left: 10px;
    font-family: var(--font-sans);
  }

  .pk {
    color: var(--fg-bright);
    font-family: var(--font-mono);
    font-weight: 500;
  }

  .sk {
    color: var(--fg-default);
    font-family: var(--font-mono);
  }

  .detail-body {
    flex: 1;
    overflow: auto;
    padding: 20px;
    display: flex;
    flex-direction: column;
    gap: 18px;
  }

  .block {
    background: var(--bg-panel);
    border: 1px solid var(--border);
    padding: 18px 20px;
    border-radius: 3px;
  }

  .block-label {
    font-size: 11.5px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.10em;
    margin-bottom: 12px;
    font-family: var(--font-sans);
    font-weight: 500;
  }

  .cap {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.10em;
    margin-right: 6px;
    font-family: var(--font-sans);
    font-weight: 500;
  }
</style>
