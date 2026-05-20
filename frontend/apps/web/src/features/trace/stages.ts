// Human-friendly metadata for the generation-pipeline stages.
//
// The backend pipeline marches each job through a fixed FSM in
// internal/app/generation/workflow_*.go. The local console surfaces
// only stage names + attempt counts; this module supplies the narrative
// — what the stage is for, what it checks, and which span attributes
// the operator most likely wants to see when debugging one.
//
// Adding a stage: append it below, give it a one-line `summary`, a
// paragraph `what`, and any per-kind featured attribute keys. The
// SpanDetail panel reads this map and renders the description plus the
// "Inputs & Outputs" block. Unknown stages still render — they just
// fall back to a generic blurb.

export type StageMeta = {
  /** Short one-liner — appears in the trace row tooltip too. */
  summary: string;
  /** Paragraph-length explanation rendered in the side panel. */
  what: string;
  /** Optional list of common signals to read on this stage. */
  signals?: string[];
};

export const STAGE_META: Record<string, StageMeta> = {
  WORKER_PRECHECK: {
    summary: "Checks whether this worker can run the job before the FSM stage handler starts.",
    what:
      "Resolves the requested provider and validates the worker-side runtime wiring. A failure here means the job never reached the named business stage recorded on the original attempt row; inspect `recorded_stage` to see where the job was parked when the worker rejected it.",
    signals: ["provider", "model", "recorded_stage", "error_code"],
  },
  INPUT_MODERATION: {
    summary: "Screens the user's prompt against safety policy before any cost is incurred.",
    what:
      "Runs the prompt through the safety classifier. ALLOW continues the pipeline; " +
      "BLOCK terminates the job with SAFETY_BLOCKED so disallowed prompts can't drain " +
      "the tenant's daily quota. The classifier verdict is keyed on a prompt+model " +
      "hash so replayed checks return the same outcome.",
    signals: ["verdict", "reason_code", "policy_version"],
  },
  COST_RESERVE: {
    summary: "Locks an atomic slice of the tenant's daily budget for this job.",
    what:
      "Reserves the projected cost on the daily budget row in a single conditional " +
      "update against a precomputed `available` attribute. The reservation succeeds " +
      "or fails as a unit — there is no read-then-write window. If the tenant has " +
      "hit their cap, the stage terminates with BUDGET_EXCEEDED.",
    signals: ["budget_micro_usd", "budget_date"],
  },
  PROMPT_PREPARE: {
    summary: "Normalises and seals the prompt the provider will actually see.",
    what:
      "Applies prompt-policy-v1 normalisation, then envelope-encrypts the prepared " +
      "prompt with KMS. The sealed bytes plus a hash get persisted on the job row so " +
      "subsequent provider attempts read a stable prompt without re-running policy.",
    signals: ["prompt_spec_version", "prepared_prompt_hash"],
  },
  PROVIDER_SUBMIT: {
    summary: "Hands the prepared prompt to the model provider and records the request id.",
    what:
      "Calls the configured provider (codex, notebooklm, simulated, …) using the " +
      "vendor's submit semantics. The vendor's request id, model, and call type are " +
      "captured on a PROVIDER_REQUEST row so the next stage can correlate the result. " +
      "If the call fails transiently, the stage row shows whether automatic redelivery is still expected or overdue.",
    signals: ["provider", "model", "call_type", "vendor_request_id", "provider_job_id"],
  },
  PROVIDER_WAIT: {
    summary: "Polls the provider until the result lands or a deadline expires.",
    what:
      "Polls the provider for the in-flight request, backing off exponentially. " +
      "Transient errors return PROVIDER_WAIT to its own queue with bumped attempts; " +
      "the stage budget caps total wall-clock time.",
    signals: ["provider_status", "poll_count", "vendor_request_id"],
  },
  OUTPUT_MODERATION: {
    summary: "Re-screens the produced asset before it can be persisted.",
    what:
      "Runs the produced bytes through the output safety classifier. A FAIL here " +
      "blocks the asset from reaching durable storage and emits a gate-audit row " +
      "the operator can inspect under AUDIT#GATE#<jobID>.",
    signals: ["verdict", "reason_code", "policy_version"],
  },
  DISCLOSURE_POSTPROCESS: {
    summary: "Stamps the asset with watermark + AI-disclosure metadata.",
    what:
      "Images get a visible watermark plus structured disclosure metadata; audio gets " +
      "the analogous tagging. The post-processed bytes' SHA-256 is captured as the " +
      "fingerprint the gate-decision panel surfaces — that hash is the immutable " +
      "anchor for downstream provenance checks.",
    signals: ["watermark_algo", "watermark_position", "watermark_text", "fingerprint"],
  },
  PUBLISH: {
    summary: "Writes the final asset to S3 and flips the media row to COMPLETE.",
    what:
      "Uploads the final asset to S3 under the tenant-scoped asset key (idempotent on " +
      "that key), then flips the DDB media row to COMPLETE and persists the OUTPUT and " +
      "VARIANT children. After this lands, the asset is visible in the Library tab. " +
      "The actual S3 key lives on the span's `s3_key` attribute.",
    signals: ["s3_key", "size_bytes", "etag", "primary_output_id"],
  },
  TERMINAL: {
    summary: "The job's final outcome — COMPLETE, FAILED, or CANCELLED.",
    what:
      "A write-once terminal marker. COMPLETE means the asset is published and " +
      "discoverable; FAILED carries an error code and reason; CANCELLED was an " +
      "operator action. Audit rows in this partition are immutable.",
    signals: ["status", "error_code", "error_message"],
  },
};

/** Human-friendly summary for a span "kind" when no stage match exists. */
export const KIND_META: Record<string, { summary: string; what: string }> = {
  ATTEMPT: {
    summary: "A single execution attempt within a stage.",
    what:
      "Each attempt row is a discrete try at the parent stage. A stage with three " +
      "attempts means two transient failures preceded a final outcome. TRANSIENT_FAIL on an attempt is past-tense: that attempt is done, and any automatic retry state is shown on the parent stage.",
  },
  PROVIDER_REQUEST: {
    summary: "One call to a model provider.",
    what:
      "The unit that carries the vendor's request id, call type, and final status. " +
      "Authored by PROVIDER_SUBMIT; updated by PROVIDER_WAIT as the provider responds.",
  },
  TERMINAL: {
    summary: "The job's terminal status row.",
    what:
      "Write-once: the outcome and (if FAILED) the error_code/error_message captured " +
      "at the moment the job left the active FSM.",
  },
  TERMINAL_AUDIT: {
    summary: "Immutable audit row for a terminal failure outside the disclosure gate.",
    what:
      "Some terminal failures also write an AUDIT#GATE row so operators have one immutable failure record. This row is not evidence that watermark or disclosure checks ran.",
  },
  GENERATION: {
    summary: "Aggregate parent over all outputs and variants for the job.",
    what:
      "Carries the primary output id and overall mode — the umbrella record the " +
      "Library tab joins against to render the asset's lifecycle.",
  },
  OUTPUT: {
    summary: "A specific output produced for this job.",
    what:
      "A job can produce multiple outputs (e.g. image + thumbnail). Each output row " +
      "carries the canonical asset id once PUBLISH lands.",
  },
  VARIANT: {
    summary: "A variant of an output (e.g. an image size or audio encoding).",
    what:
      "Variants share the parent output's identity but differ in mime / size / " +
      "encoding. The Library tab surfaces variants via the asset registry.",
  },
};

/** Attribute keys we render with special visual weight in SpanDetail.
 *  Anything not on this list falls through to the flat raw-attributes
 *  grid. Order here is rendering order. */
export const FEATURED_ATTRS: { key: string; label: string; kind: AttrKind }[] = [
  // Provider call shape
  { key: "provider", label: "provider", kind: "tag" },
  { key: "model", label: "model", kind: "tag" },
  { key: "call_type", label: "call type", kind: "tag" },
  { key: "vendor_request_id", label: "vendor request id", kind: "id" },
  { key: "provider_job_id", label: "provider job id", kind: "id" },
  { key: "request_hash", label: "request hash", kind: "id" },
  // Cost + budget. Backend stores the reservation in micro-USD on the JOB
  // row, surfaced into the span attribute bag by SpanDetail.svelte when the
  // user selects COST_RESERVE. Other money-shaped fields (available_before,
  // available_after, per-attempt cost) aren't emitted today — add them here
  // when they start being written, not before.
  { key: "budget_micro_usd", label: "reserved", kind: "money" },
  // Safety / verdict
  { key: "verdict", label: "verdict", kind: "verdict" },
  { key: "reason_code", label: "reason", kind: "code" },
  { key: "policy_version", label: "policy version", kind: "code" },
  // Output shape
  { key: "primary_output_id", label: "primary output", kind: "id" },
  { key: "output_id", label: "output id", kind: "id" },
  { key: "variant_id", label: "variant id", kind: "id" },
  { key: "final_asset_id", label: "asset id", kind: "id" },
  { key: "mime", label: "content type", kind: "tag" },
  { key: "s3_key", label: "s3 key", kind: "id" },
  { key: "size_bytes", label: "size", kind: "bytes" },
  // Retry context
  { key: "next_stage", label: "next stage", kind: "stage" },
  { key: "retry_state", label: "retry state", kind: "status" },
  { key: "last_retry_attempt_no", label: "last retry attempt", kind: "code" },
  { key: "recorded_stage", label: "recorded stage", kind: "stage" },
  { key: "resource_class", label: "resource class", kind: "tag" },
  { key: "error_code", label: "error code", kind: "code" },
  { key: "attempt_no", label: "attempt", kind: "code" },
  // Status (mapped to a pill when present)
  { key: "status", label: "status", kind: "status" },
];

export type AttrKind = "tag" | "id" | "money" | "verdict" | "code" | "stage" | "bytes" | "status";

export function stageMeta(stage: string): StageMeta | null {
  if (!stage) return null;
  return STAGE_META[stage] ?? null;
}

export function kindMeta(kind: string): { summary: string; what: string } | null {
  if (!kind) return null;
  return KIND_META[kind] ?? null;
}

// Format a micro-USD integer ($1 = 1_000_000 µUSD). Backend stores costs in
// this unit because image-class jobs are sub-cent. fmtMoneyCents (the old
// helper) rounded micro-USD to cents and produced $0.00 for a $0.004 image
// reservation — the smallest unit we actually charge is below one cent, so
// any cent-based formatter is wrong by construction.
export function fmtMoneyMicroUSD(v: string): string {
  const n = parseInt(v, 10);
  if (!Number.isFinite(n)) return v;
  const dollars = n / 1_000_000;
  if (dollars === 0) return "$0";
  if (Math.abs(dollars) >= 1) return `$${dollars.toFixed(2)}`;
  if (Math.abs(dollars) >= 0.01) return `$${dollars.toFixed(4)}`;
  return `$${dollars.toFixed(6)}`;
}
