<script lang="ts">
  import type { GateDecision, TraceSpan } from "@media-service/api-client/gen/mediaservice/ops/v1/ops_pb.js";
  import KeyValueGrid from "../../lib/KeyValueGrid.svelte";
  import Pill from "../../lib/Pill.svelte";
  import AssetPreview from "../../lib/AssetPreview.svelte";
  import Dialog from "../../lib/Dialog.svelte";
  import GateDecisionPanel from "./GateDecisionPanel.svelte";
  import { isGateSpan } from "./waterfall";
  import { waterfallHelpers } from "./detail-helpers";
  import {
    stageMeta,
    kindMeta,
    FEATURED_ATTRS,
    fmtMoneyMicroUSD,
    type AttrKind,
  } from "./stages";
  import { fmtBytes } from "../../shared/time";
  import { GRAFANA_URL } from "../../shared/config/env";
  import { jobStatusVariant, traceStatusLabel, traceStatusVariant } from "./status";

  type Props = {
    span: TraceSpan;
    gate?: GateDecision;
    /** The decoded JOB row (`google.protobuf.Struct`). Used to surface
     *  job-wide signals like the budget reservation that live on the
     *  job row rather than on per-attempt span attributes. */
    job?: Record<string, unknown> | { fields?: unknown };
    /** Tenant + media identifiers from the job summary. Used to render
     *  the published asset preview on OUTPUT / VARIANT / GENERATION spans. */
    tenantId?: string;
    mediaId?: string;
    /** The customer's plaintext prompt and the post-policy prepared
     *  variant. Decrypted server-side from the JOB row. Empty when the
     *  prompt is not yet sealed or the sealer is offline. */
    prompt?: string;
    preparedPrompt?: string;
  };

  let { span, gate, job, tenantId, mediaId, prompt = "", preparedPrompt = "" }: Props = $props();

  // Long prompts can run hundreds of chars — preview to a few lines and
  // open the full text in a Dialog on click. The threshold is generous
  // (220 chars / ~3 lines at 14px) so short prompts render inline
  // without a "read more" affordance.
  const PROMPT_PREVIEW_CHARS = 220;
  let promptDialogOpen = $state(false);
  let preparedPromptDialogOpen = $state(false);
  let promptCopied = $state(false);
  let preparedCopied = $state(false);

  // Asset resolution state, fed back from AssetPreview's onResolve so we
  // can drop the entire "published asset" section when the lookup confirms
  // there is nothing to render — avoids planting a giant dashed-empty
  // placeholder on every span detail for in-progress jobs.
  let assetState = $state<"loading" | "found" | "missing">("loading");

  async function copyText(text: string, flag: "prompt" | "prepared") {
    try {
      await navigator.clipboard.writeText(text);
      if (flag === "prompt") {
        promptCopied = true;
        setTimeout(() => (promptCopied = false), 1400);
      } else {
        preparedCopied = true;
        setTimeout(() => (preparedCopied = false), 1400);
      }
    } catch {
      // clipboard access denied — silently swallow; user can still select
      // text manually.
    }
  }

  let promptPreview = $derived.by(() => {
    if (!prompt) return "";
    if (prompt.length <= PROMPT_PREVIEW_CHARS) return prompt;
    return prompt.slice(0, PROMPT_PREVIEW_CHARS).trimEnd() + "…";
  });

  let preparedDiffers = $derived(
    !!preparedPrompt && !!prompt && preparedPrompt !== prompt,
  );

  let promptIsTruncated = $derived(prompt.length > PROMPT_PREVIEW_CHARS);

  // Only render the prompt where it is actually load-bearing. Showing it
  // on every span (e.g. DISCLOSURE_POSTPROCESS, which operates on bytes,
  // not text) was scope creep — the prompt is a JOB-level signal that
  // happens to be relevant to three stages and the provider request.
  let showPrompt = $derived.by(() => {
    if (!prompt) return false;
    const promptStages = new Set([
      "INPUT_MODERATION",
      "PROMPT_PREPARE",
      "PROVIDER_SUBMIT",
    ]);
    if (span.kind === "STAGE" || span.kind === "ATTEMPT") {
      return promptStages.has(span.stage);
    }
    return span.kind === "PROVIDER_REQUEST";
  });

  // Connect-ES decodes google.protobuf.Struct into a plain object whose
  // keys map directly to attribute names. Older codegen returns a
  // `{ fields }` envelope; both shapes are normalised here.
  let jobRow = $derived.by<Record<string, unknown>>(() => {
    if (!job) return {};
    if (typeof job === "object" && "fields" in job && (job as { fields: unknown }).fields) {
      return ((job as { fields: Record<string, unknown> }).fields ?? {}) as Record<string, unknown>;
    }
    return job as Record<string, unknown>;
  });

  let attrs = $derived(span.attributes ?? {});
  let traceId = $derived(attrs.trace_id || traceIdFromTraceparent(attrs.traceparent ?? ""));
  let traceHref = $derived(traceId ? grafanaExploreTraceUrl(traceId) : "");

  let featured = $derived.by(() => {
    const out: { key: string; label: string; kind: AttrKind; value: string }[] = [];
    for (const f of FEATURED_ATTRS) {
      const v = attrs[f.key];
      if (v !== undefined && v !== "") {
        out.push({ key: f.key, label: f.label, kind: f.kind, value: v });
      }
    }

    // Job-level budget reservation. Stored in micro-USD on the JOB row, not
    // on the attempt span, so the COST_RESERVE detail panel surfaces it
    // here. Pass micro-USD straight through; fmtMoneyMicroUSD picks the
    // right precision (image reservations are sub-cent and would round to
    // $0.00 if we converted to cents up front).
    if (span.stage === "COST_RESERVE") {
      const microUSD = numericField(jobRow, "budget_micro_usd");
      if (microUSD !== null) {
        out.unshift({ key: "budget_micro_usd", label: "reserved budget", kind: "money", value: String(microUSD) });
      }
      const budgetDate = stringField(jobRow, "budget_date");
      if (budgetDate) {
        out.push({ key: "_budget_date", label: "ledger date", kind: "tag", value: budgetDate });
      }
    }
    return out;
  });

  let rawAttrs = $derived(
    Object.entries(attrs)
      .filter(([k, v]) => v !== "" && !FEATURED_ATTRS.some((f) => f.key === k))
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([key, value]) => ({ key, value: String(value) })),
  );

  let baseMeta = $derived(waterfallHelpers.baseRows(span));

  let showGate = $derived(isGateSpan(span) && !!gate);

  let stageInfo = $derived(stageMeta(span.stage));
  let kindInfo = $derived(kindMeta(span.kind));
  let about = $derived(span.kind === "STAGE" ? stageInfo ?? kindInfo : kindInfo ?? stageInfo);
  let isFailureSource = $derived(span.stage === "WORKER_PRECHECK" && span.status === "TERMINAL_FAIL");
  let detailKind = $derived(span.kind);
  let detailLabel = $derived(cleanDetailLabel(span));
  let showDetailLabel = $derived(normalizedLabel(detailLabel) !== normalizedLabel(detailKind));
  let retryNote = $derived(retryContext(span, attrs));

  // Asset-preview block: we render the published output when the
  // selected span is the one that represents the output itself
  // (OUTPUT/VARIANT) or the aggregate parent (GENERATION) once the job
  // is far enough along to have a media id. We additionally hide the
  // whole section once the lookup resolves to "missing" so a span that
  // semantically *could* carry an asset but doesn't yet (in-flight job)
  // doesn't ship an oversized dashed empty box.
  let assetEligible = $derived(
    !!tenantId &&
    !!mediaId &&
    (span.kind === "OUTPUT" || span.kind === "VARIANT" || span.kind === "GENERATION"),
  );
  let showAssetPreview = $derived(assetEligible && assetState !== "missing");

  // Reset the asset-resolution state whenever the selection target
  // changes — otherwise a previous "missing" verdict would keep the next
  // span's section hidden until its own lookup completed. $effect.pre
  // runs before the DOM commit so we don't flash the previous verdict.
  $effect.pre(() => {
    span.id;
    tenantId;
    mediaId;
    assetState = "loading";
  });

  function verdictVariant(v: string): "ok" | "warn" | "err" | "neutral" {
    const u = v.toUpperCase();
    if (u === "ALLOW" || u === "PASS") return "ok";
    if (u === "REVIEW") return "warn";
    if (u === "BLOCK" || u === "FAIL") return "err";
    return "neutral";
  }

  function cleanDetailLabel(s: TraceSpan): string {
    if (s.kind === "STAGE" && s.stage === "WORKER_PRECHECK" && s.status === "TERMINAL_FAIL") return "worker precheck";
    if (s.status === "SKIPPED") return s.stage.toLowerCase().replaceAll("_", " ");
    if (s.kind === "TERMINAL") return "terminal";
    if (s.kind === "TERMINAL_AUDIT") return "failure audit";
    if (s.kind === "GATE_AUDIT") return "gate audit";
    if (s.kind === "OUTPUT") return "output record";
    if (s.kind === "VARIANT") return "variant record";
    return s.label || s.stage || s.id;
  }

  function normalizedLabel(value: string): string {
    return value.toLowerCase().replaceAll("_", " ").replace(/\s+/g, " ").trim();
  }

  function numericField(obj: Record<string, unknown>, key: string): number | null {
    const v = obj[key];
    if (v === null || v === undefined) return null;
    if (typeof v === "number") return v;
    if (typeof v === "bigint") return Number(v);
    if (typeof v === "string") {
      const n = Number(v);
      return Number.isFinite(n) ? n : null;
    }
    return null;
  }

  function stringField(obj: Record<string, unknown>, key: string): string {
    const v = obj[key];
    if (v === null || v === undefined) return "";
    return String(v);
  }

  function traceIdFromTraceparent(traceparent: string): string {
    const parts = traceparent.trim().split("-");
    const candidate = parts.length >= 4 ? parts[1] : "";
    return /^[0-9a-f]{32}$/i.test(candidate) && !/^0+$/.test(candidate) ? candidate.toLowerCase() : "";
  }

  function grafanaExploreTraceUrl(traceId: string): string {
    const panes = {
      trace: {
        datasource: "tempo",
        queries: [{
          refId: "A",
          datasource: { type: "tempo", uid: "tempo" },
          queryType: "traceql",
          query: traceId,
        }],
        range: { from: "now-6h", to: "now" },
      },
    };
    return `${GRAFANA_URL.replace(/\/+$/, "")}/explore?panes=${encodeURIComponent(JSON.stringify(panes))}&schemaVersion=1&orgId=1`;
  }

  function retryContext(s: TraceSpan, a: Record<string, string>): string {
    if (s.kind === "ATTEMPT" && s.status === "TRANSIENT_FAIL") {
      return "This attempt ended with a retryable failure. The worker retries automatically.";
    }
    if (s.kind !== "STAGE" || s.status !== "TRANSIENT_FAIL") return "";
    const state = a.retry_state?.toLowerCase();
    if (state === "retrying") {
      return "A new provider call started after the failed attempt — a retry is in flight. The next ATTEMPT row will land once it completes.";
    }
    if (state === "stuck") {
      return "No new attempt has surfaced after the redelivery window. The SQS message may have reached its DLQ, or the consumer is down — check the Queues tab.";
    }
    return "The last attempt failed with a retryable error. Waiting for SQS to redeliver the message. If no new attempt appears within a minute, the worker may not be consuming this queue — check the Queues tab.";
  }
</script>

<aside class="detail">
  <header>
    <div class="row1">
      <span class="kind" class:sourceKind={isFailureSource}>{detailKind}</span>
      <Pill variant={traceStatusVariant(span)}>{traceStatusLabel(span)}</Pill>
    </div>
    {#if showDetailLabel}
      <div class="label" class:sourceLabel={isFailureSource}>{detailLabel}</div>
    {/if}
    {#if about}
      <p class="about-what">{about.what}</p>
    {/if}
    {#if retryNote}
      <p class="retry-note">{retryNote}</p>
    {/if}
  </header>

  <section class="block">
    <KeyValueGrid entries={baseMeta} dense />
  </section>

  {#if traceHref}
    <section class="block">
      <div class="trace-link-row">
        <span class="block-label">trace</span>
        <a class="trace-link" href={traceHref} target="_blank" rel="noreferrer">
          <code>{traceId}</code>
          <span>open in Grafana ↗</span>
        </a>
      </div>
    </section>
  {/if}

  {#if showPrompt}
    <section class="block">
      <div class="prompt-head">
        <span class="block-label">prompt</span>
        {#if preparedDiffers}
          <button type="button" class="prepared-link" onclick={() => (preparedPromptDialogOpen = true)}>
            view prepared variant ↗
          </button>
        {/if}
      </div>
      <button
        type="button"
        class="prompt-preview"
        class:truncated={promptIsTruncated}
        onclick={() => (promptDialogOpen = true)}
        aria-label="open full prompt"
      >
        <p class="prompt-body">{promptPreview}</p>
        {#if promptIsTruncated}
          <span class="prompt-more">read full prompt · {prompt.length} chars ↗</span>
        {/if}
      </button>
    </section>
  {/if}

  {#if featured.length > 0}
    <section class="block">
      <div class="block-label">inputs &amp; outputs</div>
      <div class="featured-grid">
        {#each featured as f (f.key)}
          <div class="feat">
            <div class="feat-k">{f.label}</div>
            <div class="feat-v" data-kind={f.kind}>
              {#if f.kind === "money"}
                <span class="money">{fmtMoneyMicroUSD(f.value)}</span>
              {:else if f.kind === "bytes"}
                <span class="bytes">{fmtBytes(f.value)}</span>
              {:else if f.kind === "tag"}
                <span class="tag">{f.value}</span>
              {:else if f.kind === "stage"}
                <span class="stage-tag">{f.value}</span>
              {:else if f.kind === "verdict"}
                <Pill variant={verdictVariant(f.value)}>{f.value}</Pill>
              {:else if f.kind === "status"}
                <Pill variant={jobStatusVariant(f.value)}>{f.value}</Pill>
              {:else if f.kind === "id"}
                <code class="id-text" title={f.value}>{f.value}</code>
              {:else}
                <code class="code">{f.value}</code>
              {/if}
            </div>
          </div>
        {/each}
      </div>
    </section>
  {/if}

  {#if showAssetPreview && tenantId && mediaId}
    <section class="block">
      <div class="block-label">published asset</div>
      <div class="asset-slot">
        <AssetPreview
          tenantId={tenantId}
          mediaId={mediaId}
          size="card"
          lazy={false}
          onResolve={(s) => (assetState = s)}
        />
      </div>
    </section>
  {/if}

  {#if showGate && gate}
    <section class="block">
      <GateDecisionPanel decision={gate} />
    </section>
  {/if}

  {#if rawAttrs.length > 0}
    <section class="block">
      <div class="block-label">raw attributes ({rawAttrs.length})</div>
      <KeyValueGrid entries={rawAttrs} dense />
    </section>
  {/if}

  {#if span.errorMessage}
    <section class="block">
      <div class="block-label err-label">error</div>
      <div class="err">{span.errorMessage}</div>
    </section>
  {/if}
</aside>

{#snippet copyButton(copied: boolean, ariaLabel: string, onclick: () => void)}
  <button
    type="button"
    class="copy-btn"
    class:copied
    {onclick}
    aria-label={ariaLabel}
  >
    {#if copied}
      <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
        <path d="M3.5 8.5l3 3 6-7" stroke="currentColor" stroke-width="1.6" fill="none" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
      <span>copied</span>
    {:else}
      <svg viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
        <rect x="5" y="3.5" width="8" height="9.5" rx="1" stroke="currentColor" stroke-width="1.3" fill="none" />
        <path d="M3.5 11.5V3.5A1 1 0 0 1 4.5 2.5h6" stroke="currentColor" stroke-width="1.3" fill="none" stroke-linecap="round" />
      </svg>
      <span>copy</span>
    {/if}
  </button>
{/snippet}

<Dialog
  open={promptDialogOpen}
  title="prompt"
  onCancel={() => (promptDialogOpen = false)}
>
  <div class="prompt-dialog-inner">
    <div class="prompt-toolbar">
      <span class="prompt-len">{prompt.length} chars</span>
      {@render copyButton(promptCopied, "copy prompt to clipboard", () => copyText(prompt, "prompt"))}
    </div>
    <pre class="prompt-full">{prompt}</pre>
  </div>
</Dialog>

<Dialog
  open={preparedPromptDialogOpen}
  title="prepared prompt"
  onCancel={() => (preparedPromptDialogOpen = false)}
>
  <p class="prepared-note">
    Post-policy normalised variant the provider was actually called with. Differs from the raw prompt when the policy applied transformations.
  </p>
  <div class="prompt-dialog-inner">
    <div class="prompt-toolbar">
      <span class="prompt-len">{preparedPrompt.length} chars</span>
      {@render copyButton(preparedCopied, "copy prepared prompt to clipboard", () => copyText(preparedPrompt, "prepared"))}
    </div>
    <pre class="prompt-full">{preparedPrompt}</pre>
  </div>
</Dialog>

<style>
  .detail {
    display: flex;
    flex-direction: column;
    gap: 20px;
    padding: 20px 22px;
    background: var(--bg-panel);
    border-left: 1px solid var(--border);
    height: 100%;
    overflow: auto;
  }

  header {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding-bottom: 18px;
    border-bottom: 1px solid var(--border);
  }

  .row1 {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .kind {
    font-size: 11.5px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.10em;
    font-family: var(--font-sans);
    font-weight: 500;
  }

  .kind.sourceKind {
    color: var(--err);
    font-weight: 700;
  }

  .label {
    font-family: var(--font-mono);
    color: var(--accent-strong);
    font-size: 15.5px;
    word-break: break-all;
    font-weight: 500;
    line-height: 1.4;
  }

  .label.sourceLabel {
    color: var(--err);
  }

  .about-what {
    margin: 4px 0 0;
    font-family: var(--font-sans);
    font-size: 13px;
    line-height: 1.65;
    color: var(--fg-default);
  }

  .retry-note {
    margin: 2px 0 0;
    padding: 8px 10px;
    border-left: 3px solid var(--warn);
    background: var(--warn-dim);
    color: var(--fg-default);
    font-family: var(--font-sans);
    font-size: 12.5px;
    line-height: 1.5;
  }

  .block-label {
    font-size: 11.5px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.10em;
    margin-bottom: 10px;
    font-family: var(--font-sans);
    font-weight: 500;
  }

  .trace-link-row {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .trace-link-row .block-label {
    margin-bottom: 0;
    flex: 0 0 auto;
  }

  .trace-link {
    min-width: 0;
    display: inline-flex;
    align-items: center;
    gap: 8px;
    color: var(--accent);
    font-family: var(--font-sans);
    font-size: 12.5px;
    text-decoration: none;
  }

  .trace-link:hover {
    text-decoration: underline;
  }

  .trace-link code {
    min-width: 0;
    max-width: 220px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--fg-bright);
    font-family: var(--font-mono);
    font-size: 12.5px;
  }

  .featured-grid {
    display: grid;
    grid-template-columns: minmax(140px, max-content) 1fr;
    gap: 8px 22px;
    align-items: baseline;
  }

  .feat {
    display: contents;
  }

  .feat-k {
    font-family: var(--font-sans);
    font-size: 11.5px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.09em;
    font-weight: 500;
    padding-top: 2px;
  }

  .feat-v {
    font-family: var(--font-mono);
    font-size: 13.5px;
    color: var(--fg-default);
    word-break: break-all;
  }

  .money {
    font-family: var(--font-mono);
    font-variant-numeric: tabular-nums;
    color: var(--accent-strong);
    font-weight: 600;
    font-size: 14.5px;
  }

  .bytes {
    font-family: var(--font-mono);
    font-variant-numeric: tabular-nums;
    color: var(--fg-bright);
  }

  .tag {
    display: inline-block;
    font-family: var(--font-sans);
    font-size: 12px;
    font-weight: 500;
    color: var(--fg-bright);
    background: var(--bg-base);
    border: 1px solid var(--border);
    padding: 2px 10px;
    border-radius: 2px;
    letter-spacing: 0.02em;
  }

  .stage-tag {
    display: inline-block;
    font-family: var(--font-mono);
    font-size: 12px;
    color: var(--accent-strong);
    background: var(--accent-dim);
    border: 1px solid var(--accent);
    padding: 1px 8px;
    border-radius: 2px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    font-weight: 600;
  }

  .code {
    font-family: var(--font-mono);
    font-size: 13px;
    color: var(--fg-bright);
  }

  .asset-slot {
    max-width: 100%;
  }

  /* Prompt block — preview that opens into a dialog. Designed to look
     like a quote: a left-stripe + soft paper background. The preview
     button covers the whole preview body so clicking anywhere expands. */
  .prompt-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 10px;
  }

  .prompt-head .block-label {
    margin-bottom: 0;
  }

  .prepared-link {
    background: transparent;
    border: none;
    color: var(--accent);
    font-family: var(--font-sans);
    font-size: 12px;
    padding: 0;
    cursor: pointer;
  }

  .prepared-link:hover {
    text-decoration: underline;
  }

  .prompt-preview {
    display: block;
    width: 100%;
    text-align: left;
    border: 1px solid var(--border);
    border-left: 3px solid var(--accent);
    background: var(--bg-base);
    padding: 12px 14px;
    border-radius: 2px;
    cursor: pointer;
    transition: background 120ms ease, border-color 120ms ease;
    font-family: inherit;
  }

  .prompt-preview:hover {
    background: var(--accent-dim);
  }

  .prompt-body {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 14px;
    line-height: 1.6;
    color: var(--fg-bright);
    white-space: pre-wrap;
    word-break: break-word;
  }

  .prompt-more {
    display: inline-block;
    margin-top: 8px;
    font-family: var(--font-sans);
    font-size: 12px;
    color: var(--accent);
    font-weight: 500;
  }

  .prompt-preview.truncated .prompt-body {
    /* Soft visual cue that more text continues below the fold. */
    position: relative;
  }

  .prompt-full {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 14.5px;
    line-height: 1.65;
    color: var(--fg-bright);
    white-space: pre-wrap;
    word-break: break-word;
    max-height: 60vh;
    overflow: auto;
    background: var(--bg-base);
    padding: 14px 16px;
    border-left: 3px solid var(--accent);
    border-radius: 2px;
  }

  .prepared-note {
    margin: 0 0 12px;
    font-family: var(--font-sans);
    font-size: 13px;
    color: var(--fg-dim);
    line-height: 1.55;
  }

  .prompt-dialog-inner {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  .prompt-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .prompt-len {
    font-family: var(--font-mono);
    font-size: 11.5px;
    color: var(--fg-dim);
    letter-spacing: 0.04em;
    font-variant-numeric: tabular-nums;
  }

  .copy-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 5px 11px;
    background: var(--bg-base);
    border: 1px solid var(--border);
    color: var(--fg-default);
    font-family: var(--font-sans);
    font-size: 12px;
    font-weight: 500;
    cursor: pointer;
    border-radius: 3px;
    transition: border-color 120ms ease, background 120ms ease, color 120ms ease;
  }

  .copy-btn:hover {
    border-color: var(--accent);
    color: var(--accent);
    background: var(--accent-dim);
  }

  .copy-btn.copied {
    border-color: var(--accent);
    color: var(--accent-strong);
    background: var(--accent-dim);
  }

  .copy-btn svg {
    flex: 0 0 auto;
  }

  .err-label { color: var(--err); }
  .err {
    color: var(--err);
    font-family: var(--font-mono);
    font-size: 13.5px;
    white-space: pre-wrap;
    word-break: break-all;
    background: var(--err-dim);
    padding: 12px 14px;
    border-left: 3px solid var(--err);
    line-height: 1.55;
  }
</style>
