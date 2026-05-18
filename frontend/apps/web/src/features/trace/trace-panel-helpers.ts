import type { TraceSpan } from "@media-service/api-client/gen/mediaservice/ops/v1/ops_pb.js";
import { fmtDuration, tsToMs } from "../../shared/time";
import { traceStatusLabel } from "./status";
import { timePointLeft, type SpanNode, type Window } from "./waterfall";

export type MediaTuple = {
  tenantId: string;
  mediaId: string;
};

const absoluteFormatter = new Intl.DateTimeFormat(undefined, {
  year: "numeric",
  month: "2-digit",
  day: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
  hour12: false,
});

export function fmtAbsoluteMs(ms: number): string {
  return absoluteFormatter.format(new Date(ms));
}

export function rowLabel(span: TraceSpan): string {
  if (span.kind === "STAGE" && span.stage === "WORKER_PRECHECK") return "worker precheck";
  if (span.status === "SKIPPED") return lowerStageLabel(span.stage);
  if (span.kind === "TERMINAL") return "terminal";
  if (span.kind === "TERMINAL_AUDIT") return "failure audit";
  if (span.kind === "GATE_AUDIT") return "gate audit";
  if (span.kind === "OUTPUT") return "output record";
  if (span.kind === "VARIANT") return "variant record";
  return span.label || span.stage || span.kind;
}

export function rowKind(span: TraceSpan): string {
  const label = rowLabel(span);
  const kind = rawRowKind(span);
  if (isDuplicateLabelKind(label, kind)) return "";
  return kind;
}

function rawRowKind(span: TraceSpan): string {
  if (span.status === "SKIPPED") return "";
  if (span.kind === "STAGE") {
    return span.stage === "WORKER_PRECHECK" ? "precheck" : "";
  }
  if (span.kind === "TERMINAL_AUDIT") return "audit";
  return span.kind.toLowerCase();
}

function isDuplicateLabelKind(label: string, kind: string): boolean {
  const normalizedLabel = normalizedRowText(label);
  const normalizedKind = normalizedRowText(kind);
  if (!normalizedKind) return true;
  return normalizedLabel === normalizedKind || normalizedLabel.includes(normalizedKind) || normalizedKind.includes(normalizedLabel);
}

function normalizedRowText(value: string): string {
  return value.toLowerCase().replaceAll("_", " ").replace(/\s+/g, " ").trim();
}

export function rowSummary(n: SpanNode): string {
  const parts = [
    rowLabel(n.span),
    `status ${traceStatusLabel(n.span)}`,
    `start ${fmtAbsoluteMs(n.startMs)}`,
    `duration ${fmtDuration(n.durationMs)}`,
  ];
  if (n.durationMs > 0) parts.push(`end ${fmtAbsoluteMs(n.endMs)}`);
  if (isSynthesizedStageEnd(n.span)) {
    parts.push("end synthesized from next stage — bar includes queue handoff, not just observed work");
  }
  if (n.span.errorCode) parts.push(`error ${n.span.errorCode}`);
  return parts.join("; ");
}

// A stage carries end_synthesized when closeStageEnds had no observable
// child event (PROVIDER_REQUEST.completed_at, OUTPUT/VARIANT.updated_at) to
// pin the real end-of-work, and fell back to projecting the bar out to the
// next stage's StartAt. Operators need this distinction: a 2-minute solid
// bar means 2 minutes of observed work; a 2-minute hatched bar means the
// FSM was *nominally* in this stage for 2 minutes but most of it was idle
// queue handoff to a different worker.
export function isSynthesizedStageEnd(span: TraceSpan): boolean {
  return span.kind === "STAGE" && !!span.attributes?.end_synthesized;
}

function lowerStageLabel(stage: string): string {
  return stage.toLowerCase().replaceAll("_", " ");
}

export function chooseFailureSource(spans: TraceSpan[]): TraceSpan | undefined {
  return spans
    .filter((span) => span.status === "TERMINAL_FAIL" && !!span.errorCode)
    .slice()
    .sort((a, b) => sourcePriority(a) - sourcePriority(b) || (tsToMs(a.startAt) ?? 0) - (tsToMs(b.startAt) ?? 0))[0];
}

function sourcePriority(span: TraceSpan): number {
  if (span.kind === "STAGE" && span.stage === "WORKER_PRECHECK") return 0;
  if (span.stage === "WORKER_PRECHECK") return 1;
  if (span.kind === "ATTEMPT") return 2;
  if (span.kind === "STAGE") return 3;
  if (span.kind === "PROVIDER_REQUEST") return 4;
  if (span.kind === "GATE_AUDIT") return 5;
  if (span.kind === "TERMINAL") return 6;
  return 7;
}

export function isDerivedRow(span: TraceSpan): boolean {
  return span.kind === "TERMINAL" || span.kind === "TERMINAL_AUDIT";
}

export function barKindClass(span: TraceSpan): string {
  if (span.status === "SKIPPED") return "skipped";
  if (span.status === "TERMINAL_FAIL") return "fail";
  if (span.status === "TRANSIENT_FAIL") return "transient";
  if (span.status === "PENDING") return "pending";
  if (span.kind === "PROVIDER_REQUEST") return "provider";
  if (span.kind === "ATTEMPT") return "attempt";
  if (span.kind === "OUTPUT" || span.kind === "VARIANT") return "record";
  if (span.kind === "GATE_AUDIT" || isDerivedRow(span)) return "audit";
  if (isSynthesizedStageEnd(span)) return "stage-synthesized";
  return "stage";
}

export function isOkRow(span: TraceSpan): boolean {
  if (span.status !== "OK") return false;
  if (span.kind === "GATE_AUDIT" || span.kind === "TERMINAL_AUDIT") return false;
  return traceStatusLabel(span) === "OK";
}

export function gapStartPct(n: SpanNode, window: Window): number | null {
  if (n.skipped) return null;
  if (n.previousEndMs === undefined) return null;
  if (!n.gapFromPreviousEndMs || n.gapFromPreviousEndMs <= 0) return null;
  return timePointLeft(n.previousEndMs, window);
}

export function gapWidthPct(n: SpanNode, rect: { left: number; width: number }, window: Window): number {
  const start = gapStartPct(n, window);
  if (start === null) return 0;
  return Math.max(0, rect.left - start);
}

export function showGapSegment(n: SpanNode, rect: { left: number; width: number }, window: Window): boolean {
  if ((n.gapFromPreviousEndMs ?? 0) < 500) return false;
  return gapWidthPct(n, rect, window) >= 0.6;
}

export function showInlineGapLabel(n: SpanNode, rect: { left: number; width: number }, window: Window): boolean {
  return showGapSegment(n, rect, window) && gapWidthPct(n, rect, window) >= 6;
}

export function showEdgeGapLabel(n: SpanNode, rect: { left: number; width: number }, window: Window): boolean {
  return showGapSegment(n, rect, window) && gapWidthPct(n, rect, window) < 6;
}

export function showDurationLabel(n: SpanNode, rect: { left: number; width: number }): boolean {
  return !n.skipped && n.durationMs > 0 && rect.left + rect.width < 93;
}

export function endLabelLeft(rect: { left: number; width: number }): number {
  return Math.min(96, Math.max(0, rect.left + rect.width));
}

export function parseMediaTuple(keyRef: string): MediaTuple | null {
  const [pk] = keyRef.split("|", 1);
  const parts = pk.split("#");
  if (parts.length !== 4) return null;
  if (parts[0] !== "TENANT" || parts[2] !== "MEDIA") return null;
  if (!parts[1] || !parts[3]) return null;
  return { tenantId: parts[1], mediaId: parts[3] };
}
