import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { LOCAL_MODEL_CATALOG } from "./model-catalog";
import type {
  DdbRow,
  FullJobView,
  GateDecision,
  GenerationProviderModels,
  JobSummary,
  LogLine,
  MediaRow,
  PromptDecryptResult,
  QueueStat,
  S3Node,
  TenantUsageReservoir,
  TraceSpan,
} from "./types";

type RequestOptions = { signal?: AbortSignal };
type StreamLogResponse = { line?: LogLine };

const BASE = "/__local_ops";

function timestampFromISO(value: unknown): Timestamp | undefined {
  if (typeof value !== "string" || value === "") return undefined;
  const ms = Date.parse(value);
  if (!Number.isFinite(ms)) return undefined;
  return timestampFromMs(ms);
}

async function request<T>(path: string, body: unknown = {}, opts: RequestOptions = {}): Promise<T> {
  const res = await fetch(`${BASE}/${path}`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
    signal: opts.signal,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `${res.status} ${res.statusText}`);
  }
  return (await res.json()) as T;
}

function reviveJobSummary(raw: Partial<JobSummary> & Record<string, unknown>): JobSummary {
  return {
    jobId: String(raw.jobId ?? ""),
    tenantId: String(raw.tenantId ?? ""),
    mediaId: raw.mediaId ? String(raw.mediaId) : undefined,
    status: String(raw.status ?? ""),
    currentStage: String(raw.currentStage ?? ""),
    outputType: String(raw.outputType ?? ""),
    tier: String(raw.tier ?? ""),
    model: String(raw.model ?? ""),
    attempts: Number(raw.attempts ?? 0),
    errorCode: String(raw.errorCode ?? ""),
    createdAt: timestampFromISO(raw.createdAt),
    updatedAt: timestampFromISO(raw.updatedAt),
    completedAt: timestampFromISO(raw.completedAt),
  };
}

function reviveTraceSpan(raw: Partial<TraceSpan> & Record<string, unknown>): TraceSpan {
  return {
    id: String(raw.id ?? ""),
    parentId: String(raw.parentId ?? ""),
    kind: String(raw.kind ?? ""),
    label: String(raw.label ?? ""),
    status: String(raw.status ?? ""),
    stage: String(raw.stage ?? ""),
    resourceClass: String(raw.resourceClass ?? ""),
    attemptNo: Number(raw.attemptNo ?? 0),
    errorCode: String(raw.errorCode ?? ""),
    errorMessage: String(raw.errorMessage ?? ""),
    attributes: (raw.attributes ?? {}) as Record<string, string>,
    startAt: timestampFromISO(raw.startAt),
    endAt: timestampFromISO(raw.endAt),
    durationMs: Number(raw.durationMs ?? 0),
    pk: String(raw.pk ?? ""),
    sk: String(raw.sk ?? ""),
  };
}

function reviveGateDecision(raw: Partial<GateDecision> & Record<string, unknown>): GateDecision {
  return {
    jobId: String(raw.jobId ?? ""),
    tenantId: String(raw.tenantId ?? ""),
    gateVersion: String(raw.gateVersion ?? ""),
    outputType: String(raw.outputType ?? ""),
    provider: String(raw.provider ?? ""),
    model: String(raw.model ?? ""),
    decision: String(raw.decision ?? ""),
    errorCode: String(raw.errorCode ?? ""),
    watermarkPresent: Boolean(raw.watermarkPresent),
    disclosurePresent: Boolean(raw.disclosurePresent),
    safetyPresent: Boolean(raw.safetyPresent),
    watermarkFingerprint: String(raw.watermarkFingerprint ?? ""),
    watermarkAlgo: String(raw.watermarkAlgo ?? ""),
    watermarkPosition: String(raw.watermarkPosition ?? ""),
    watermarkText: String(raw.watermarkText ?? ""),
    decidedAt: timestampFromISO(raw.decidedAt),
  };
}

function reviveMediaRow(raw: Partial<MediaRow> & Record<string, unknown>): MediaRow {
  return {
    mediaId: String(raw.mediaId ?? ""),
    tenantId: String(raw.tenantId ?? ""),
    ownerUserId: String(raw.ownerUserId ?? ""),
    origin: String(raw.origin ?? ""),
    mediaType: String(raw.mediaType ?? ""),
    lifecycle: String(raw.lifecycle ?? ""),
    originalAssetId: raw.originalAssetId ? String(raw.originalAssetId) : undefined,
    createdAt: timestampFromISO(raw.createdAt),
    updatedAt: timestampFromISO(raw.updatedAt),
    deletedAt: timestampFromISO(raw.deletedAt),
    jobId: raw.jobId ? String(raw.jobId) : undefined,
  };
}

function reviveDdbRow(raw: DdbRow): DdbRow {
  return {
    pk: raw.pk,
    sk: raw.sk,
    itemType: raw.itemType,
    attributes: raw.attributes ?? {},
  };
}

function reviveQueueStat(raw: Partial<QueueStat> & Record<string, unknown>): QueueStat {
  return {
    name: String(raw.name ?? ""),
    url: String(raw.url ?? ""),
    visible: Number(raw.visible ?? 0),
    inFlight: Number(raw.inFlight ?? 0),
    delayed: Number(raw.delayed ?? 0),
    visibilityTimeoutSeconds: Number(raw.visibilityTimeoutSeconds ?? 0),
    oldestMessageAgeSeconds: Number(raw.oldestMessageAgeSeconds ?? 0),
    dlqName: String(raw.dlqName ?? ""),
    dlqCount: Number(raw.dlqCount ?? 0),
    tierClass: String(raw.tierClass ?? ""),
  };
}

function reviveReservoir(raw: Partial<TenantUsageReservoir> & Record<string, unknown>): TenantUsageReservoir {
  return {
    tenantId: String(raw.tenantId ?? ""),
    metric: String(raw.metric ?? ""),
    period: String(raw.period ?? ""),
    cap: Number(raw.cap ?? 0),
    available: Number(raw.available ?? 0),
    reserved: Number(raw.reserved ?? 0),
    committed: Number(raw.committed ?? 0),
    released: Number(raw.released ?? 0),
    state: String(raw.state ?? ""),
    policyId: String(raw.policyId ?? ""),
    policyVersion: Number(raw.policyVersion ?? 0),
    createdAt: timestampFromISO(raw.createdAt),
    updatedAt: timestampFromISO(raw.updatedAt),
    materialized: Boolean(raw.materialized),
  };
}

function reviveS3Node(raw: Partial<S3Node> & Record<string, unknown>): S3Node {
  return {
    key: String(raw.key ?? ""),
    name: String(raw.name ?? ""),
    isPrefix: Boolean(raw.isPrefix),
    sizeBytes: Number(raw.sizeBytes ?? 0),
    etag: String(raw.etag ?? ""),
    lastModified: timestampFromISO(raw.lastModified),
  };
}

function reviveLogLine(raw: Partial<LogLine> & Record<string, unknown>): LogLine {
  return {
    ts: timestampFromISO(raw.ts),
    service: String(raw.service ?? ""),
    level: String(raw.level ?? ""),
    body: String(raw.body ?? ""),
    labels: (raw.labels ?? {}) as Record<string, string>,
  };
}

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  if (signal?.aborted) return Promise.resolve();
  return new Promise((resolve) => {
    const id = setTimeout(resolve, ms);
    signal?.addEventListener("abort", () => {
      clearTimeout(id);
      resolve();
    }, { once: true });
  });
}

export const localOpsClient = {
  async listGenerationModels(): Promise<{ providers: GenerationProviderModels[] }> {
    return { providers: LOCAL_MODEL_CATALOG };
  },

  async getLocalIdentity(): Promise<{ tenantId: string; userId: string }> {
    return request("identity");
  },

  async listJobs(req: { status?: string; outputType?: string; limit?: number; cursor?: string }): Promise<{ jobs: JobSummary[]; nextCursor: string }> {
    const res = await request<{ jobs: JobSummary[]; nextCursor: string }>("list-jobs", req);
    return { jobs: res.jobs.map(reviveJobSummary), nextCursor: res.nextCursor };
  },

  async getJob(req: { jobId: string }): Promise<{ view?: FullJobView }> {
    const res = await request<{ view?: FullJobView }>("get-job", req);
    if (!res.view) return {};
    return {
      view: {
        ...res.view,
        summary: res.view.summary ? reviveJobSummary(res.view.summary) : undefined,
        spans: (res.view.spans ?? []).map(reviveTraceSpan),
        gateDecision: res.view.gateDecision ? reviveGateDecision(res.view.gateDecision) : undefined,
        firstEventAt: timestampFromISO(res.view.firstEventAt),
        lastEventAt: timestampFromISO(res.view.lastEventAt),
      },
    };
  },

  async listMedia(req: { mediaType?: string; origin?: string; lifecycle?: string; includeDeleted?: boolean; limit?: number; cursor?: string }): Promise<{ items: MediaRow[]; nextCursor: string }> {
    const res = await request<{ items: MediaRow[]; nextCursor: string }>("list-media", req);
    return { items: res.items.map(reviveMediaRow), nextCursor: res.nextCursor };
  },

  async scanDdb(req: { pkPrefix?: string; skPrefix?: string; limit?: number; cursor?: string }): Promise<{ rows: DdbRow[]; nextCursor: string }> {
    const res = await request<{ rows: DdbRow[]; nextCursor: string }>("scan-ddb", req);
    return { rows: res.rows.map(reviveDdbRow), nextCursor: res.nextCursor };
  },

  async getDdbRow(req: { pk: string; sk: string }): Promise<{ row?: DdbRow }> {
    const res = await request<{ row?: DdbRow }>("get-ddb-row", req);
    return { row: res.row ? reviveDdbRow(res.row) : undefined };
  },

  async putDdbAttr(req: { pk: string; sk: string; attributeName: string; valueJson: string }): Promise<void> {
    await request("put-ddb-attr", req);
  },

  async deleteDdbRow(req: { pk: string; sk: string }): Promise<void> {
    await request("delete-ddb-row", req);
  },

  async decryptJobPrompts(req: { jobId: string; tenantId?: string }): Promise<PromptDecryptResult> {
    return request("decrypt-job-prompts", req);
  },

  async queueDepths(): Promise<{ queues: QueueStat[] }> {
    const res = await request<{ queues: QueueStat[] }>("queue-depths");
    return { queues: res.queues.map(reviveQueueStat) };
  },

  async purgeQueue(req: { queueName: string }): Promise<void> {
    await request("purge-queue", req);
  },

  async redriveDlq(req: { dlqName: string; limit?: number }): Promise<{ moved: number; failed: number }> {
    return request("redrive-dlq", req);
  },

  async getTenantUsage(req: { tenantId?: string }): Promise<{ tenantId: string; currentDailyPeriod: string; dailyCost?: TenantUsageReservoir; reservoirs: TenantUsageReservoir[] }> {
    const res = await request<{ tenantId: string; currentDailyPeriod: string; dailyCost?: TenantUsageReservoir; reservoirs: TenantUsageReservoir[] }>("tenant-usage", req);
    return {
      tenantId: res.tenantId,
      currentDailyPeriod: res.currentDailyPeriod,
      dailyCost: res.dailyCost ? reviveReservoir(res.dailyCost) : undefined,
      reservoirs: (res.reservoirs ?? []).map(reviveReservoir),
    };
  },

  async listS3(req: { prefix?: string; delimiter?: string; limit?: number }): Promise<{ nodes: S3Node[] }> {
    const res = await request<{ nodes: S3Node[] }>("list-s3", req);
    return { nodes: res.nodes.map(reviveS3Node) };
  },

  async presignDownload(req: { key: string }): Promise<{ url: string; expiresAt?: Timestamp }> {
    const res = await request<{ url: string; expiresAt?: string }>("presign-download", req);
    return { url: res.url, expiresAt: timestampFromISO(res.expiresAt) };
  },

  async deleteS3Object(req: { key: string }): Promise<void> {
    await request("delete-s3-object", req);
  },

  async cancelJob(req: { jobId: string; reason?: string }): Promise<void> {
    await request("cancel-job", req);
  },

  async retryJob(req: { jobId: string }): Promise<void> {
    await request("retry-job", req);
  },

  async forceFailJob(req: { jobId: string; errorCode?: string; errorMessage?: string }): Promise<void> {
    await request("force-fail-job", req);
  },

  async replayOutbox(req: { jobId: string }): Promise<void> {
    await request("replay-outbox", req);
  },

  async *streamLogs(req: {
    service?: string;
    jobId?: string;
    mediaId?: string;
    level?: string;
    contains?: string;
    tailLines?: number;
    lookbackSeconds?: number;
  }, opts: RequestOptions = {}): AsyncGenerator<StreamLogResponse> {
    let since = "";
    while (!opts.signal?.aborted) {
      const res = await request<{ lines: LogLine[]; nextSince: string }>(
        "stream-logs",
        { ...req, since },
        opts,
      );
      since = res.nextSince || since;
      for (const line of res.lines.map(reviveLogLine)) {
        if (opts.signal?.aborted) return;
        yield { line };
      }
      await sleep(2000, opts.signal);
    }
  },
};
