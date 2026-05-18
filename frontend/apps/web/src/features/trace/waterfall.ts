// Pure functions that fold a flat list of TraceSpans into the nested rows
// the waterfall renders. Kept out of the .svelte component so they are easy
// to reason about and (eventually) test.

import { create } from "@bufbuild/protobuf";
import {
  TraceSpanSchema,
  type TraceSpan,
} from "@media-service/api-client/gen/mediaservice/ops/v1/ops_pb.js";
import { tsToMs } from "../../shared/time";

export type SpanNode = {
  span: TraceSpan;
  depth: number;
  startMs: number;
  endMs: number;
  durationMs: number;
  gapFromPreviousEndMs?: number;
  /** End time of the previous visible non-skipped node. Used by the
   *  template to position the gap segment within the same track as the
   *  current bar. Undefined for the first row. */
  previousEndMs?: number;
  skipped?: boolean;
};

export type Window = {
  start: number;
  end: number;
  span: number;
};

export type BuildOpts = {
  /** Drop the single OK ATTEMPT child under an OK STAGE — its bar duplicates
   *  the stage row, which adds noise without information. Multi-attempt
   *  retries and any failure path still surface every attempt. */
  collapseSoloAttempts?: boolean;
  /** Drop the synthetic TERMINAL kind row at the tail. The job's exit code
   *  is already displayed in the header strip and on TERMINAL_AUDIT rows
   *  when present, so this row has no information to add. */
  hideTerminalRow?: boolean;
};

const STAGE_ORDER = [
  "WORKER_PRECHECK",
  "INPUT_MODERATION",
  "COST_RESERVE",
  "PROMPT_PREPARE",
  "PROVIDER_SUBMIT",
  "PROVIDER_WAIT",
  "OUTPUT_MODERATION",
  "DISCLOSURE_POSTPROCESS",
  "PUBLISH",
  "TERMINAL",
];

const EXECUTABLE_STAGES = STAGE_ORDER.filter(
  (stage) => stage !== "WORKER_PRECHECK" && stage !== "TERMINAL",
);

const stageRank = new Map<string, number>();
STAGE_ORDER.forEach((k, i) => stageRank.set(k, i));

const executableStageRank = new Map<string, number>();
EXECUTABLE_STAGES.forEach((k, i) => executableStageRank.set(k, i));

function spanStartMs(span: TraceSpan): number {
  const s = tsToMs(span.startAt);
  if (s !== undefined) return s;
  const e = tsToMs(span.endAt);
  if (e !== undefined) return e - Number(span.durationMs ?? 0);
  return 0;
}

function spanEndMs(span: TraceSpan, fallbackNow: number): number {
  const e = tsToMs(span.endAt);
  if (e !== undefined) return e;
  const s = tsToMs(span.startAt);
  if (s !== undefined && span.durationMs && Number(span.durationMs) > 0) {
    return s + Number(span.durationMs);
  }
  // Open span — render to "now" so a streaming bar grows.
  return fallbackNow;
}

export function computeWindow(spans: TraceSpan[], now: number): Window {
  if (spans.length === 0) {
    return { start: now, end: now, span: 1 };
  }
  let start = Infinity;
  let end = -Infinity;
  for (const s of spans) {
    const a = spanStartMs(s);
    const b = spanEndMs(s, now);
    if (a < start) start = a;
    if (b > end) end = b;
  }
  if (!Number.isFinite(start)) start = now;
  if (!Number.isFinite(end)) end = now;
  if (end <= start) end = start + 1;
  return { start, end, span: end - start };
}

// Build a depth-ordered flat list. Stage rows first (depth 0, sorted by
// FSM order then by start_at), then their children indented by 1.
//
// `windowStart`, when provided, anchors the first row's leading gap to the
// trace's left edge. Without it the first row never renders a `waiting · …`
// segment even when something existed before it (e.g. an eagerly-created
// OUTPUT slot inserted at job intake — the 2s pickup latency that otherwise
// reads as dead air to the operator).
export function buildNodes(
  spans: TraceSpan[],
  now: number,
  terminal = false,
  opts: BuildOpts = {},
  windowStart?: number,
): SpanNode[] {
  const collapseSoloAttempts = opts.collapseSoloAttempts ?? true;
  const hideTerminalRow = opts.hideTerminalRow ?? true;
  const stages = spans
    .filter((s) => s.kind === "STAGE")
    .slice()
    .sort((a, b) => {
      const ra = stageRank.get(a.stage) ?? 999;
      const rb = stageRank.get(b.stage) ?? 999;
      if (ra !== rb) return ra - rb;
      return spanStartMs(a) - spanStartMs(b);
    });

  const presentStages = new Set(stages.map((s) => s.stage));
  const stageEndByStage = new Map<string, number>();
  for (const stage of stages) {
    stageEndByStage.set(stage.stage, spanEndMs(stage, now));
  }

  const skippedStageAnchors = collectSkippedStages(spans, stageEndByStage, now, terminal);
  for (const [stage, anchorMs] of skippedStageAnchors) {
    if (presentStages.has(stage)) continue;
    stages.push(createSkippedStage(stage, anchorMs));
  }

  stages.sort((a, b) => {
    const ra = stageRank.get(a.stage) ?? 999;
    const rb = stageRank.get(b.stage) ?? 999;
    if (ra !== rb) return ra - rb;
    return spanStartMs(a) - spanStartMs(b);
  });

  const byParent = new Map<string, TraceSpan[]>();
  for (const s of spans) {
    if (s.kind === "STAGE") continue;
    if (!s.parentId) continue;
    const arr = byParent.get(s.parentId) ?? [];
    arr.push(s);
    byParent.set(s.parentId, arr);
  }
  // Also bucket children that link by stage name when parent_id is missing.
  const stageById = new Map<string, TraceSpan>();
  for (const s of stages) {
    if (s.id) stageById.set(s.id, s);
  }

  const out: SpanNode[] = [];
  // Tracks every span that has a known home — its parent stage exists and
  // has claimed it as a child. The orphan pass uses this rather than the
  // `out` array so that *hidden* children (e.g. solo OK attempts we chose
  // not to render) don't get re-picked-up as orphans and dumped at the
  // bottom of the waterfall.
  const homed = new Set<string>();

  for (const stage of stages) {
    homed.add(stage.id);
    const skipped = stage.status === "SKIPPED";
    const startMs = skipped ? skippedAnchorMs(stage) : spanStartMs(stage);
    const endMs = skipped ? startMs : spanEndMs(stage, now);
    out.push({
      span: stage,
      depth: 0,
      startMs,
      endMs,
      durationMs: Math.max(0, endMs - startMs),
      skipped,
    });

    if (skipped) continue;

    const children = (byParent.get(stage.id) ?? []).slice().sort(
      (a, b) => spanStartMs(a) - spanStartMs(b),
    );
    // Every child belongs to this stage, even when we hide it. Mark
    // them homed first so the orphan pass below leaves them alone.
    for (const c of children) homed.add(c.id);

    const visibleChildren = collapseSoloAttempts
      ? filterSoloAttempt(stage, children)
      : children;
    for (const c of visibleChildren) {
      const window = childWindow(c, startMs, endMs, now);
      out.push({
        span: c,
        depth: 1,
        startMs: window.start,
        endMs: window.end,
        durationMs: Math.max(0, window.end - window.start),
      });
    }
  }

  // Orphan spans (no parent stage) appear after, depth 0, sorted by start.
  const orphans = spans
    .filter((s) => !homed.has(s.id))
    .sort((a, b) => spanStartMs(a) - spanStartMs(b));
  for (const o of orphans) {
    const os = spanStartMs(o);
    const oe = spanEndMs(o, now);
    out.push({
      span: o,
      depth: 0,
      startMs: os,
      endMs: oe,
      durationMs: Math.max(0, oe - os),
    });
  }

  const filtered = hideTerminalRow ? out.filter((n) => n.span.kind !== "TERMINAL") : out;
  return withPreviousEndGaps(filtered, windowStart);
}

// A solo OK ATTEMPT under an OK STAGE is just a re-drawing of the parent's
// bar — same window, same status, no retry context. Drop it so the row
// budget is spent on rows that carry information. Failed paths and
// multi-attempt retries keep every attempt.
//
// Exception: when the stage's wall-clock is much larger than its single
// attempt (e.g. a stage that finished compute in a second but the FSM
// only marked it complete 30 minutes later due to a delayed audit write
// or async confirmation), keep the attempt visible. The attempt's tight
// bar inside the long stage bar is what tells the operator *where*
// the actual work happened.
function filterSoloAttempt(stage: TraceSpan, children: TraceSpan[]): TraceSpan[] {
  if (stage.status !== "OK") return children;
  const attempts = children.filter((c) => c.kind === "ATTEMPT");
  if (attempts.length !== 1) return children;
  if (attempts[0].status !== "OK") return children;
  const stageMs = Number(stage.durationMs ?? 0);
  const attemptMs = Number(attempts[0].durationMs ?? 0);
  if (stageMs > 1_000 && attemptMs > 0 && attemptMs < stageMs * 0.5) {
    return children;
  }
  return children.filter((c) => c.kind !== "ATTEMPT");
}

function childWindow(
  span: TraceSpan,
  parentStartMs: number,
  parentEndMs: number,
  now: number,
): { start: number; end: number } {
  const rawStart = spanStartMs(span);
  const anchor = isRecordSpan(span) ? parentEndMs : parentStartMs;
  const start = Math.max(rawStart, anchor);
  const end = Math.max(start, spanEndMs(span, now));
  return { start, end };
}

function isRecordSpan(span: TraceSpan): boolean {
  return span.kind === "OUTPUT" || span.kind === "VARIANT";
}

// Returns the left + width of the time-bar as CSS percentages.
export function barRect(node: SpanNode, window: Window): { left: number; width: number } {
  if (window.span <= 0) return { left: 0, width: 100 };
  const left = ((node.startMs - window.start) / window.span) * 100;
  const width = (node.durationMs / window.span) * 100;
  return {
    left: Math.max(0, Math.min(100, left)),
    width: Math.max(0.4, Math.min(100 - left, width)),
  };
}

export function timePointLeft(ms: number, window: Window): number {
  if (window.span <= 0) return 0;
  const left = ((ms - window.start) / window.span) * 100;
  return Math.max(0, Math.min(100, left));
}

function collectSkippedStages(
  spans: TraceSpan[],
  stageEndByStage: Map<string, number>,
  now: number,
  terminal: boolean,
): Map<string, number> {
  const skipped = new Map<string, number>();
  for (const span of spans) {
    if (span.kind !== "ATTEMPT") continue;
    const nextStage = span.attributes?.next_stage;
    if (!nextStage) continue;

    const anchorMs = stageEndByStage.get(span.stage) ?? spanEndMs(span, now);
    if (span.stage === "WORKER_PRECHECK" && nextStage === "TERMINAL" && terminal) {
      addSkippedRange(skipped, 0, EXECUTABLE_STAGES.length, anchorMs);
      continue;
    }

    const fromRank = executableStageRank.get(span.stage);
    if (fromRank === undefined) continue;

    if (nextStage === "TERMINAL") {
      if (terminal) addSkippedRange(skipped, fromRank + 1, EXECUTABLE_STAGES.length, anchorMs);
      continue;
    }

    const toRank = executableStageRank.get(nextStage);
    if (toRank === undefined || toRank <= fromRank + 1) continue;
    addSkippedRange(skipped, fromRank + 1, toRank, anchorMs);
  }
  return skipped;
}

function addSkippedRange(skipped: Map<string, number>, start: number, end: number, anchorMs: number) {
  for (let i = start; i < end; i++) {
    const stage = EXECUTABLE_STAGES[i];
    if (!stage || skipped.has(stage)) continue;
    skipped.set(stage, anchorMs);
  }
}

function createSkippedStage(stage: string, anchorMs: number): TraceSpan {
  return create(TraceSpanSchema, {
    id: `skipped:${stage}`,
    kind: "STAGE",
    label: stage,
    status: "SKIPPED",
    stage,
    attributes: {
      skipped: "true",
      reason: "stage was bypassed by the recorded FSM transition",
      anchor_ms: String(Math.round(anchorMs)),
    },
  });
}

function skippedAnchorMs(span: TraceSpan): number {
  const raw = span.attributes?.anchor_ms;
  const parsed = raw ? Number.parseInt(raw, 10) : NaN;
  return Number.isFinite(parsed) ? parsed : 0;
}

// `leadingAnchorMs`, when set, seeds the running `previousEndMs` so the
// first non-skipped row reports the gap from the trace's left edge to its
// own start. This surfaces job-intake latency (worker pickup, lease, KMS,
// FSM setup) instead of leaving it as silent left-padding.
function withPreviousEndGaps(nodes: SpanNode[], leadingAnchorMs?: number): SpanNode[] {
  let previousEndMs: number | undefined = leadingAnchorMs;
  return nodes.map((node) => {
    if (node.skipped) return node;
    const previousEnd = previousEndMs;
    const gapFromPreviousEndMs =
      previousEnd === undefined ? 0 : Math.max(0, node.startMs - previousEnd);
    previousEndMs = node.endMs;
    return { ...node, gapFromPreviousEndMs, previousEndMs: previousEnd };
  });
}

export function isGateSpan(span: TraceSpan): boolean {
  if (span.kind === "GATE_AUDIT") return true;
  const s = span.stage.toUpperCase();
  return s === "POSTPROCESS_GATE" || s === "DISCLOSURE_POSTPROCESS";
}
