import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { describe, expect, it } from "vitest";
import type { TraceSpan } from "../../shared/local-ops/types";
import { buildNodes } from "./waterfall";

function traceSpan(init: Partial<TraceSpan>): TraceSpan {
  return {
    id: "",
    parentId: "",
    kind: "",
    label: "",
    status: "",
    stage: "",
    resourceClass: "",
    attemptNo: 0,
    errorCode: "",
    errorMessage: "",
    attributes: {},
    durationMs: 0,
    pk: "",
    sk: "",
    ...init,
  };
}

function at(ms: number) {
  return timestampFromMs(ms);
}

describe("buildNodes", () => {
  it("computes row gaps from the previous visible span end", () => {
    const nodes = buildNodes([
      traceSpan({
        id: "stage:INPUT_MODERATION",
        kind: "STAGE",
        label: "INPUT_MODERATION",
        status: "OK",
        stage: "INPUT_MODERATION",
        startAt: at(1_000),
        endAt: at(1_100),
      }),
      traceSpan({
        id: "stage:COST_RESERVE",
        kind: "STAGE",
        label: "COST_RESERVE",
        status: "OK",
        stage: "COST_RESERVE",
        startAt: at(1_250),
        endAt: at(1_300),
      }),
    ], 1_300);

    expect(nodes[0].gapFromPreviousEndMs).toBe(0);
    expect(nodes[1].gapFromPreviousEndMs).toBe(150);
  });

  it("adds a skipped stage when a recorded transition jumps over it", () => {
    const nodes = buildNodes([
      traceSpan({
        id: "stage:PROVIDER_SUBMIT",
        kind: "STAGE",
        label: "PROVIDER_SUBMIT",
        status: "OK",
        stage: "PROVIDER_SUBMIT",
        startAt: at(1_000),
        endAt: at(1_010),
      }),
      traceSpan({
        id: "attempt:PROVIDER_SUBMIT:v1:a1",
        parentId: "stage:PROVIDER_SUBMIT",
        kind: "ATTEMPT",
        label: "attempt #1",
        status: "OK",
        stage: "PROVIDER_SUBMIT",
        startAt: at(1_010),
        endAt: at(1_010),
        attributes: { next_stage: "OUTPUT_MODERATION" },
      }),
      traceSpan({
        id: "stage:OUTPUT_MODERATION",
        kind: "STAGE",
        label: "OUTPUT_MODERATION",
        status: "OK",
        stage: "OUTPUT_MODERATION",
        startAt: at(1_020),
        endAt: at(1_030),
      }),
    ], 1_030);

    const skipped = nodes.find((n) => n.span.stage === "PROVIDER_WAIT");
    expect(skipped?.skipped).toBe(true);
    expect(skipped?.span.status).toBe("SKIPPED");
  });

  it("interleaves a handoff wait before skipped stages and the destination stage", () => {
    const nodes = buildNodes([
      traceSpan({
        id: "stage:PROVIDER_SUBMIT",
        kind: "STAGE",
        label: "PROVIDER_SUBMIT",
        status: "OK",
        stage: "PROVIDER_SUBMIT",
        startAt: at(1_000),
        endAt: at(1_010),
      }),
      traceSpan({
        id: "attempt:PROVIDER_SUBMIT:v1:a1",
        parentId: "stage:PROVIDER_SUBMIT",
        kind: "ATTEMPT",
        label: "attempt #1",
        status: "OK",
        stage: "PROVIDER_SUBMIT",
        startAt: at(1_010),
        endAt: at(1_010),
        attributes: { next_stage: "OUTPUT_MODERATION" },
      }),
      traceSpan({
        id: "handoff:PROVIDER_SUBMIT:v1:a1:to:OUTPUT_MODERATION",
        kind: "HANDOFF_WAIT",
        label: "handoff wait",
        status: "OK",
        stage: "OUTPUT_MODERATION",
        startAt: at(1_010),
        endAt: at(2_000),
        attributes: {
          from_stage: "PROVIDER_SUBMIT",
          to_stage: "OUTPUT_MODERATION",
          reason: "normal_handoff",
          confidence: "observed",
        },
      }),
      traceSpan({
        id: "stage:OUTPUT_MODERATION",
        kind: "STAGE",
        label: "OUTPUT_MODERATION",
        status: "OK",
        stage: "OUTPUT_MODERATION",
        startAt: at(2_000),
        endAt: at(2_100),
      }),
    ], 2_100);

    expect(nodes.map((n) => n.span.id)).toEqual([
      "stage:PROVIDER_SUBMIT",
      "handoff:PROVIDER_SUBMIT:v1:a1:to:OUTPUT_MODERATION",
      "skipped:PROVIDER_WAIT",
      "stage:OUTPUT_MODERATION",
    ]);
    expect(nodes[1].depth).toBe(0);
    expect(nodes[3].gapFromPreviousEndMs).toBe(0);
  });

  it("places an initial handoff wait before input moderation", () => {
    const nodes = buildNodes([
      traceSpan({
        id: "handoff:JOB_CREATED:to:INPUT_MODERATION",
        kind: "HANDOFF_WAIT",
        label: "handoff wait",
        status: "OK",
        stage: "INPUT_MODERATION",
        startAt: at(1_000),
        endAt: at(2_000),
        attributes: {
          from_stage: "JOB_CREATED",
          to_stage: "INPUT_MODERATION",
          reason: "initial_dispatch",
          confidence: "observed",
        },
      }),
      traceSpan({
        id: "stage:INPUT_MODERATION",
        kind: "STAGE",
        label: "INPUT_MODERATION",
        status: "OK",
        stage: "INPUT_MODERATION",
        startAt: at(2_000),
        endAt: at(2_100),
      }),
    ], 2_100);

    expect(nodes.map((n) => n.span.id)).toEqual([
      "handoff:JOB_CREATED:to:INPUT_MODERATION",
      "stage:INPUT_MODERATION",
    ]);
    expect(nodes[1].gapFromPreviousEndMs).toBe(0);
  });

  it("marks business stages skipped when worker precheck terminates before the FSM handler", () => {
    const nodes = buildNodes([
      traceSpan({
        id: "stage:WORKER_PRECHECK",
        kind: "STAGE",
        label: "WORKER_PRECHECK",
        status: "TERMINAL_FAIL",
        stage: "WORKER_PRECHECK",
        startAt: at(1_000),
        endAt: at(1_000),
      }),
      traceSpan({
        id: "attempt:WORKER_PRECHECK:v1:a1",
        parentId: "stage:WORKER_PRECHECK",
        kind: "ATTEMPT",
        label: "provider resolution",
        status: "TERMINAL_FAIL",
        stage: "WORKER_PRECHECK",
        startAt: at(1_000),
        endAt: at(1_000),
        attributes: { next_stage: "TERMINAL", recorded_stage: "INPUT_MODERATION" },
      }),
    ], 1_000, true);

    expect(nodes.find((n) => n.span.stage === "INPUT_MODERATION")?.skipped).toBe(true);
    expect(nodes.find((n) => n.span.stage === "PUBLISH")?.skipped).toBe(true);
  });

  it("draws record spans after their parent stage", () => {
    const nodes = buildNodes([
      traceSpan({
        id: "stage:PUBLISH",
        kind: "STAGE",
        label: "PUBLISH",
        status: "OK",
        stage: "PUBLISH",
        startAt: at(2_000),
        endAt: at(2_050),
      }),
      traceSpan({
        id: "output:out-1",
        parentId: "stage:PUBLISH",
        kind: "OUTPUT",
        label: "output record · COMPLETE",
        status: "OK",
        stage: "PUBLISH",
        startAt: at(1_000),
        endAt: at(1_500),
      }),
    ], 2_050);

    const output = nodes.find((n) => n.span.id === "output:out-1");
    expect(output?.startMs).toBe(2_050);
    expect(output?.endMs).toBe(2_050);
  });

  it("hides solo OK attempts under OK stages without orphaning them", () => {
    const nodes = buildNodes([
      traceSpan({
        id: "stage:INPUT_MODERATION",
        kind: "STAGE",
        label: "INPUT_MODERATION",
        status: "OK",
        stage: "INPUT_MODERATION",
        startAt: at(1_000),
        endAt: at(1_100),
      }),
      traceSpan({
        id: "attempt:INPUT_MODERATION:a1",
        parentId: "stage:INPUT_MODERATION",
        kind: "ATTEMPT",
        label: "attempt #1",
        status: "OK",
        stage: "INPUT_MODERATION",
        startAt: at(1_000),
        endAt: at(1_100),
      }),
      traceSpan({
        id: "stage:PUBLISH",
        kind: "STAGE",
        label: "PUBLISH",
        status: "OK",
        stage: "PUBLISH",
        startAt: at(1_200),
        endAt: at(1_300),
      }),
      traceSpan({
        id: "attempt:PUBLISH:a1",
        parentId: "stage:PUBLISH",
        kind: "ATTEMPT",
        label: "attempt #1",
        status: "OK",
        stage: "PUBLISH",
        startAt: at(1_200),
        endAt: at(1_300),
      }),
    ], 1_300);

    // Both solo OK attempts should be hidden — neither should appear in the
    // node list, including at the orphan tail.
    expect(nodes.map((n) => n.span.id)).toEqual([
      "stage:INPUT_MODERATION",
      "stage:PUBLISH",
    ]);
  });

  it("keeps multi-attempt children visible", () => {
    const nodes = buildNodes([
      traceSpan({
        id: "stage:PROVIDER_SUBMIT",
        kind: "STAGE",
        label: "PROVIDER_SUBMIT",
        status: "OK",
        stage: "PROVIDER_SUBMIT",
        startAt: at(1_000),
        endAt: at(1_400),
      }),
      traceSpan({
        id: "attempt:PROVIDER_SUBMIT:a1",
        parentId: "stage:PROVIDER_SUBMIT",
        kind: "ATTEMPT",
        label: "attempt #1",
        status: "TRANSIENT_FAIL",
        stage: "PROVIDER_SUBMIT",
        startAt: at(1_000),
        endAt: at(1_200),
      }),
      traceSpan({
        id: "attempt:PROVIDER_SUBMIT:a2",
        parentId: "stage:PROVIDER_SUBMIT",
        kind: "ATTEMPT",
        label: "attempt #2",
        status: "OK",
        stage: "PROVIDER_SUBMIT",
        startAt: at(1_200),
        endAt: at(1_400),
      }),
    ], 1_400);

    expect(nodes.map((n) => n.span.id)).toContain("attempt:PROVIDER_SUBMIT:a1");
    expect(nodes.map((n) => n.span.id)).toContain("attempt:PROVIDER_SUBMIT:a2");
    expect(nodes.find((n) => n.span.id === "attempt:PROVIDER_SUBMIT:a1")?.depth).toBe(1);
  });

  it("groups the disclosure gate audit under postprocess before publish", () => {
    const nodes = buildNodes([
      traceSpan({
        id: "stage:DISCLOSURE_POSTPROCESS",
        kind: "STAGE",
        label: "DISCLOSURE_POSTPROCESS",
        status: "OK",
        stage: "DISCLOSURE_POSTPROCESS",
        startAt: at(1_000),
        endAt: at(1_100),
      }),
      traceSpan({
        id: "gate",
        parentId: "stage:DISCLOSURE_POSTPROCESS",
        kind: "GATE_AUDIT",
        label: "gate decision · PASS",
        status: "OK",
        stage: "DISCLOSURE_POSTPROCESS",
        startAt: at(1_090),
        endAt: at(1_090),
      }),
      traceSpan({
        id: "stage:PUBLISH",
        kind: "STAGE",
        label: "PUBLISH",
        status: "OK",
        stage: "PUBLISH",
        startAt: at(1_120),
        endAt: at(1_140),
      }),
    ], 1_140);

    expect(nodes.map((n) => n.span.id)).toEqual([
      "stage:DISCLOSURE_POSTPROCESS",
      "gate",
      "stage:PUBLISH",
    ]);
    expect(nodes[1].depth).toBe(1);
  });

  // When the window's left edge anchors on a pre-stage event (e.g. an OUTPUT
  // row eagerly inserted at job intake), the first STAGE row should surface
  // the leading 2s of intake/lease/warmup as an explicit gap, not as silent
  // left-padding the operator has to discover by squinting at the axis.
  it("reports a leading gap from windowStart to the first row", () => {
    const nodes = buildNodes(
      [
        traceSpan({
          id: "stage:INPUT_MODERATION",
          kind: "STAGE",
          label: "INPUT_MODERATION",
          status: "OK",
          stage: "INPUT_MODERATION",
          startAt: at(3_000),
          endAt: at(3_100),
        }),
      ],
      3_100,
      false,
      {},
      1_000,
    );

    expect(nodes[0].gapFromPreviousEndMs).toBe(2_000);
    expect(nodes[0].previousEndMs).toBe(1_000);
  });

  it("omits the leading gap when windowStart equals the first row's start", () => {
    const nodes = buildNodes(
      [
        traceSpan({
          id: "stage:INPUT_MODERATION",
          kind: "STAGE",
          label: "INPUT_MODERATION",
          status: "OK",
          stage: "INPUT_MODERATION",
          startAt: at(1_000),
          endAt: at(1_100),
        }),
      ],
      1_100,
      false,
      {},
      1_000,
    );

    expect(nodes[0].gapFromPreviousEndMs).toBe(0);
  });
});
