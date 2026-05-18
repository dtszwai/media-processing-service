import type { TraceSpan } from "@media-service/api-client/gen/mediaservice/ops/v1/ops_pb.js";
import { fmtDateTime, fmtDuration } from "../../shared/time";

type Entry = {
  key: string;
  value: string;
  tone?: "default" | "accent" | "muted" | "err" | "ok" | "warn";
  /** When present, the value is rendered as an anchor pointing at this
   *  hash-route — used to deep-link span ids straight into the DDB
   *  inspector for the row that backs them. */
  href?: string;
};

function ddbHref(pk: string, sk: string): string {
  return `#/ddb/${encodeURIComponent(pk)}/${encodeURIComponent(sk)}`;
}

export const waterfallHelpers = {
  baseRows(span: TraceSpan): Entry[] {
    const rows: Entry[] = [];
    // Skip the synthetic `stage:<NAME>` id — the panel title already
    // encodes both the kind ("STAGE") and the stage name. We keep the id
    // for everything else (attempts, provider requests, output rows) where
    // it's a real cross-reference key worth surfacing.
    const autoStageId = span.kind === "STAGE" && span.id === `stage:${span.stage}`;
    if (!autoStageId && span.id) {
      // When the span is backed by a real DDB row, link the id straight
      // into the inspector. Synthetic spans (STAGE rollups) carry no
      // pk/sk and stay as plain text.
      const href = span.pk && span.sk ? ddbHref(span.pk, span.sk) : undefined;
      rows.push({ key: "id", value: span.id, href });
    }
    if (span.parentId) rows.push({ key: "parent_id", value: span.parentId });
    // For STAGE rows the title already says the stage; rendering it again
    // in the grid wastes a row. Non-STAGE spans (attempts, provider
    // requests) still need it because their title is the kind, not the
    // stage they belong to.
    if (span.stage && span.kind !== "STAGE") {
      rows.push({ key: "stage", value: span.stage });
    }
    if (span.resourceClass) rows.push({ key: "resource_class", value: span.resourceClass });
    if (span.attemptNo) rows.push({ key: "attempt_no", value: String(span.attemptNo) });
    rows.push({ key: "start_at", value: fmtDateTime(span.startAt) });
    rows.push({ key: "end_at", value: fmtDateTime(span.endAt) });
    rows.push({ key: "duration", value: fmtDuration(span.durationMs) });
    if (span.errorCode) rows.push({ key: "error_code", value: span.errorCode, tone: "err" });
    // The DDB pk / sk pair, rendered as their own rows so operators who
    // ignored the linked `id` still get a one-click deep link. Both
    // values share the same href so clicking either lands on the row.
    if (span.pk && span.sk) {
      const href = ddbHref(span.pk, span.sk);
      rows.push({ key: "pk", value: span.pk, href });
      rows.push({ key: "sk", value: span.sk, href });
    }
    return rows;
  },
};
