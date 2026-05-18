// Shared mapping from backend status strings to Pill variants. Keeping the
// mapping in one place lets the jobs table and the trace header agree on
// what colour each status renders as.

import type { TraceSpan } from "@media-service/api-client/gen/mediaservice/ops/v1/ops_pb.js";

type Variant = "ok" | "warn" | "err" | "pending" | "neutral" | "accent";

export function jobStatusVariant(status: string): Variant {
  const s = status.toUpperCase();
  if (s === "COMPLETE" || s === "READY" || s === "OK") return "ok";
  if (s === "FAILED" || s === "TERMINAL_FAIL" || s === "CANCELLED" || s === "STUCK") return "err";
  if (s === "RUNNING") return "accent";
  if (s === "TRANSIENT_FAIL" || s === "RETRY" || s === "AWAITING_REDELIVERY" || s === "AWAITING REDELIVERY") return "warn";
  if (s === "PENDING" || s === "HELD" || s === "RETRYING") return "pending";
  return "neutral";
}

export function traceStatusLabel(span: TraceSpan): string {
  if (span.status === "SKIPPED") return "SKIPPED";
  if (span.kind === "TERMINAL") {
    const status = span.attributes?.status?.toUpperCase();
    if (status === "FAILED") return "JOB_FAILED";
    if (status === "COMPLETE") return "JOB_COMPLETE";
    if (status === "CANCELLED") return "JOB_CANCELLED";
    return "JOB_DONE";
  }
  if (span.kind === "TERMINAL_AUDIT") return "RECORDED";
  if (span.kind === "GATE_AUDIT") {
    const decision = span.attributes?.decision?.toUpperCase();
    if (decision === "PASS") return "GATE_PASS";
    if (decision === "FAIL") return "GATE_FAIL";
    return "GATE_RECORDED";
  }
  if (span.kind === "OUTPUT") {
    const status = span.attributes?.status?.toUpperCase();
    if (status === "FAILED") return "FAILED";
    if (status === "CANCELLED") return "CANCELLED";
  }
  const status = span.status.toUpperCase();
  if (span.kind === "STAGE" && status === "TRANSIENT_FAIL") {
    const retryState = span.attributes?.retry_state?.toLowerCase();
    if (retryState === "retrying") return "RETRYING";
    if (retryState === "stuck") return "STUCK";
    return "AWAITING REDELIVERY";
  }
  if (status === "TERMINAL_FAIL") return "FAILED";
  if (status === "TRANSIENT_FAIL") return "TRANSIENT_FAIL";
  if (status === "PENDING" || status === "") return "PENDING";
  return status;
}

export function traceStatusVariant(span: TraceSpan): Variant {
  if (span.status === "SKIPPED" || span.kind === "TERMINAL" || span.kind === "TERMINAL_AUDIT") {
    return "neutral";
  }
  if (span.kind === "GATE_AUDIT") return span.status === "OK" ? "ok" : "warn";
  return jobStatusVariant(traceStatusLabel(span));
}

export function isJobTerminal(status: string): boolean {
  const s = status.toUpperCase();
  return s === "COMPLETE" || s === "FAILED" || s === "CANCELLED";
}

// Color the time-bar fill on a span.
export function spanBarColor(status: string): string {
  const s = status.toUpperCase();
  if (s === "TRANSIENT_FAIL") return "var(--warn)";
  if (s === "TERMINAL_FAIL") return "var(--err)";
  if (s === "PENDING" || s === "" || s === "HELD") return "var(--fg-muted)";
  return "var(--accent)";
}
