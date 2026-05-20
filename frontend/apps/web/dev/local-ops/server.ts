import { createHash } from "node:crypto";
import type { IncomingMessage, ServerResponse } from "node:http";
import path from "node:path";
import { Buffer } from "node:buffer";
import type { Plugin } from "vite";
import {
  DeleteItemCommand,
  DynamoDBClient,
  GetItemCommand,
  PutItemCommand,
  QueryCommand,
  ScanCommand,
  UpdateItemCommand,
  type AttributeValue,
} from "@aws-sdk/client-dynamodb";
import { DecryptCommand, KMSClient } from "@aws-sdk/client-kms";
import {
  DeleteObjectCommand,
  GetObjectCommand,
  HeadObjectCommand,
  ListObjectsV2Command,
  S3Client,
} from "@aws-sdk/client-s3";
import {
  DeleteMessageCommand,
  GetQueueAttributesCommand,
  ListQueuesCommand,
  PurgeQueueCommand,
  QueueAttributeName,
  ReceiveMessageCommand,
  SendMessageCommand,
  SQSClient,
} from "@aws-sdk/client-sqs";
import { getSignedUrl } from "@aws-sdk/s3-request-presigner";
import { marshall, unmarshall } from "@aws-sdk/util-dynamodb";
import { decryptSealedPrompt } from "./prompt-crypto";

type RawRow = Record<string, unknown>;
type DdbItem = Record<string, AttributeValue>;
type Handler = (body: Record<string, unknown>) => Promise<unknown>;
type JsonTimestamp = string | undefined;

type JobSummary = {
  jobId: string;
  tenantId: string;
  mediaId?: string;
  status: string;
  currentStage: string;
  outputType: string;
  tier: string;
  model: string;
  attempts: number;
  errorCode: string;
  createdAt?: JsonTimestamp;
  updatedAt?: JsonTimestamp;
  completedAt?: JsonTimestamp;
};

type TraceSpan = {
  id: string;
  parentId: string;
  kind: string;
  label: string;
  status: string;
  stage: string;
  resourceClass: string;
  attemptNo: number;
  errorCode: string;
  errorMessage: string;
  attributes: Record<string, string>;
  startAt?: JsonTimestamp;
  endAt?: JsonTimestamp;
  durationMs: number;
  pk: string;
  sk: string;
};

type GateDecision = {
  jobId: string;
  tenantId: string;
  gateVersion: string;
  outputType: string;
  provider: string;
  model: string;
  decision: string;
  errorCode: string;
  watermarkPresent: boolean;
  disclosurePresent: boolean;
  safetyPresent: boolean;
  watermarkFingerprint: string;
  watermarkAlgo: string;
  watermarkPosition: string;
  watermarkText: string;
  decidedAt?: JsonTimestamp;
};

type FullJobView = {
  summary?: JobSummary;
  job?: RawRow;
  media?: Record<string, unknown>;
  resultAsset?: Record<string, unknown>;
  spans: TraceSpan[];
  gateDecision?: GateDecision;
  relatedKeys: string[];
  firstEventAt?: JsonTimestamp;
  lastEventAt?: JsonTimestamp;
  decryptedPrompt: string;
  decryptedPreparedPrompt: string;
};

type MediaRow = {
  mediaId: string;
  tenantId: string;
  ownerUserId: string;
  origin: string;
  mediaType: string;
  lifecycle: string;
  originalAssetId?: string;
  createdAt?: JsonTimestamp;
  updatedAt?: JsonTimestamp;
  deletedAt?: JsonTimestamp;
  jobId?: string;
};

type DdbRow = {
  pk: string;
  sk: string;
  itemType: string;
  attributes: Record<string, unknown>;
};

type QueueStat = {
  name: string;
  url: string;
  visible: number;
  inFlight: number;
  delayed: number;
  visibilityTimeoutSeconds: number;
  oldestMessageAgeSeconds: number;
  dlqName: string;
  dlqCount: number;
  tierClass: string;
};

type TenantUsageReservoir = {
  tenantId: string;
  metric: string;
  period: string;
  cap: number;
  available: number;
  reserved: number;
  committed: number;
  released: number;
  state: string;
  policyId: string;
  policyVersion: number;
  createdAt?: JsonTimestamp;
  updatedAt?: JsonTimestamp;
  materialized: boolean;
};

type S3Node = {
  key: string;
  name: string;
  isPrefix: boolean;
  sizeBytes: number;
  etag: string;
  lastModified?: JsonTimestamp;
};

type LogLine = {
  ts?: JsonTimestamp;
  service: string;
  level: string;
  body: string;
  labels: Record<string, string>;
};

const DEFAULT_REGION = "us-east-1";
const DEFAULT_ENDPOINT = "http://localhost:4566";
const DEFAULT_TABLE = "media-v1";
const DEFAULT_BUCKET = "media-service-local";
const DEFAULT_TENANT = "tenant_local";
const DEFAULT_USER = "user_local";
const DEFAULT_LOKI_URL = "http://localhost:3000/api/datasources/proxy/uid/loki/loki";
const COST_MICRO_USD_CAP = 5_000_000;
const MAX_LIST_LIMIT = 200;
const DEFAULT_LIST_LIMIT = 50;
const PROMPT_FIELDS = new Set(["encrypted_prompt", "encrypted_prepared_prompt"]);

type LocalOpsState = {
  region: string;
  table: string;
  bucket: string;
  tenantId: string;
  userId: string;
  endpoint: string;
  lokiURL: string;
  ddb: DynamoDBClient;
  s3: S3Client;
  sqs: SQSClient;
  kms: KMSClient;
};

export function localOpsPlugin(): Plugin {
  const state = createState();
  const handlers = createHandlers(state);
  return {
    name: "media-service-local-ops",
    configureServer(server) {
      server.middlewares.use("/__local_ops", async (req, res) => {
        try {
          if (req.method !== "POST") {
            sendText(res, 405, "method not allowed");
            return;
          }
          const route = routeName(req);
          const handler = handlers[route];
          if (!handler) {
            sendText(res, 404, `unknown local ops route: ${route}`);
            return;
          }
          const body = await readJson(req);
          const result = await handler(body);
          sendJson(res, result ?? {});
        } catch (err) {
          sendText(res, 500, err instanceof Error ? err.message : String(err));
        }
      });
    },
  };
}

function createState(): LocalOpsState {
  const region = process.env.AWS_REGION || process.env.AWS_DEFAULT_REGION || DEFAULT_REGION;
  const endpoint = process.env.AWS_ENDPOINT_URL || DEFAULT_ENDPOINT;
  const credentials = {
    accessKeyId: process.env.AWS_ACCESS_KEY_ID || "test",
    secretAccessKey: process.env.AWS_SECRET_ACCESS_KEY || "test",
  };
  const common = { region, endpoint, credentials };
  return {
    region,
    endpoint,
    table: process.env.DDB_TABLE || process.env.AWS_DDB_TABLE || DEFAULT_TABLE,
    bucket: process.env.S3_BUCKET || process.env.AWS_S3_BUCKET || DEFAULT_BUCKET,
    tenantId: process.env.LOCAL_TENANT_ID || DEFAULT_TENANT,
    userId: process.env.LOCAL_USER_ID || DEFAULT_USER,
    lokiURL: process.env.VITE_LOKI_URL || process.env.LOKI_URL || DEFAULT_LOKI_URL,
    ddb: new DynamoDBClient(common),
    s3: new S3Client({ ...common, forcePathStyle: true }),
    sqs: new SQSClient(common),
    kms: new KMSClient(common),
  };
}

function createHandlers(state: LocalOpsState): Record<string, Handler> {
  return {
    identity: async () => ({ tenantId: state.tenantId, userId: state.userId }),
    "list-jobs": (body) => listJobs(state, body),
    "get-job": (body) => getJob(state, stringBody(body, "jobId")),
    "list-media": (body) => listMedia(state, body),
    "scan-ddb": (body) => scanDdb(state, body),
    "get-ddb-row": (body) => getDdbRow(state, stringBody(body, "pk"), stringBody(body, "sk")),
    "put-ddb-attr": (body) => putDdbAttr(state, body),
    "delete-ddb-row": (body) => deleteDdbRow(state, stringBody(body, "pk"), stringBody(body, "sk")),
    "decrypt-job-prompts": (body) => decryptJobPrompts(state, stringBody(body, "jobId"), optionalString(body, "tenantId")),
    "queue-depths": () => queueDepths(state),
    "purge-queue": (body) => purgeQueue(state, stringBody(body, "queueName")),
    "redrive-dlq": (body) => redriveDlq(state, stringBody(body, "dlqName"), numberBody(body, "limit", 10)),
    "tenant-usage": (body) => tenantUsage(state, optionalString(body, "tenantId")),
    "list-s3": (body) => listS3(state, body),
    "presign-download": (body) => presignDownload(state, stringBody(body, "key")),
    "delete-s3-object": (body) => deleteS3Object(state, stringBody(body, "key")),
    "stream-logs": (body) => streamLogs(state, body),
    "cancel-job": (body) => updateJobTerminal(state, stringBody(body, "jobId"), "CANCELLED", optionalString(body, "reason") || "operator cancel"),
    "force-fail-job": (body) => updateJobTerminal(state, stringBody(body, "jobId"), "FAILED", optionalString(body, "errorMessage") || "manual force-fail", optionalString(body, "errorCode") || "OPERATOR_FORCED_FAIL"),
    "retry-job": (body) => retryJob(state, stringBody(body, "jobId")),
    "replay-outbox": (body) => replayOutbox(state, stringBody(body, "jobId")),
  };
}

async function listJobs(state: LocalOpsState, body: Record<string, unknown>): Promise<{ jobs: JobSummary[]; nextCursor: string }> {
  const status = optionalString(body, "status");
  const outputType = optionalString(body, "outputType");
  const rows = await scanUntilLimit(state, {
    limit: numberBody(body, "limit", DEFAULT_LIST_LIMIT),
    cursor: optionalString(body, "cursor"),
    filterExpression: "item_type = :t",
    values: marshall({ ":t": "GEN" }),
    decode: (row) => {
      const summary = jobSummary(row);
      if (!summary.jobId) return null;
      if (status && summary.status !== status) return null;
      if (outputType && summary.outputType !== outputType) return null;
      return summary;
    },
  });
  rows.items.sort((a, b) => compareDesc(a.createdAt, b.createdAt));
  return { jobs: rows.items, nextCursor: rows.nextCursor };
}

async function listMedia(state: LocalOpsState, body: Record<string, unknown>): Promise<{ items: MediaRow[]; nextCursor: string }> {
  const mediaType = optionalString(body, "mediaType");
  const origin = optionalString(body, "origin");
  const lifecycle = optionalString(body, "lifecycle");
  const includeDeleted = Boolean(body.includeDeleted);
  const rows = await scanUntilLimit(state, {
    limit: numberBody(body, "limit", DEFAULT_LIST_LIMIT),
    cursor: optionalString(body, "cursor"),
    filterExpression: "SK = :sk AND begins_with(PK, :pk)",
    values: marshall({ ":sk": "MEDIA", ":pk": "TENANT#" }),
    decode: (row) => {
      const media = mediaRow(row);
      if (!media.mediaId) return null;
      if (!includeDeleted && media.lifecycle === "DELETED") return null;
      if (mediaType && media.mediaType !== mediaType) return null;
      if (origin && media.origin !== origin) return null;
      if (lifecycle && media.lifecycle !== lifecycle) return null;
      return media;
    },
  });
  rows.items.sort((a, b) => compareDesc(a.createdAt, b.createdAt));
  for (const item of rows.items) {
    if (item.origin === "GENERATED") {
      item.jobId = await findJobForMedia(state, item.tenantId, item.mediaId);
    }
  }
  return { items: rows.items, nextCursor: rows.nextCursor };
}

async function getJob(state: LocalOpsState, jobId: string): Promise<{ view?: FullJobView }> {
  if (!jobId) throw new Error("jobId required");
  const rows = await queryPK(state, `JOB#${jobId}`);
  if (rows.length === 0) return {};

  const view: FullJobView = {
    job: {},
    media: {},
    resultAsset: {},
    spans: [],
    relatedKeys: [],
    decryptedPrompt: "",
    decryptedPreparedPrompt: "",
  };
  const attempts: TraceSpan[] = [];
  for (const row of rows) {
    switch (stringAttr(row, "item_type")) {
      case "GEN":
        view.job = jsonSafeMap(row, false);
        view.summary = jobSummary(row);
        break;
      case "STAGE_ATTEMPT": {
        const span = attemptSpan(row);
        attempts.push(span);
        view.spans.push(span);
        break;
      }
      case "PROVIDER_REQUEST":
        view.spans.push(providerSpan(row));
        break;
      case "TERMINAL":
        view.spans.push(terminalSpan(row));
        break;
      case "OUTPUT":
        view.spans.push(outputSpan(row));
        break;
      case "VARIANT":
        view.spans.push(variantSpan(row));
        break;
    }
  }
  if (!view.summary) return {};

  view.spans.push(...deriveStageSpans(view.spans));
  if (view.summary.status !== "COMPLETE" && view.summary.status !== "FAILED" && view.summary.status !== "CANCELLED") {
    const running = runningStageSpan(view.summary, view.spans);
    if (running) view.spans.push(running);
  }
  const gate = await gateDecision(state, jobId);
  if (gate) {
    view.gateDecision = gate.decision;
    view.spans.push(gate.span);
  }
  linkChildrenToStages(view.spans);
  closeStageEnds(view.spans, view.summary.completedAt || nowISO());

  if (view.summary.tenantId && view.summary.mediaId) {
    const media = await getRawRow(state, mediaPK(view.summary.tenantId, view.summary.mediaId), "MEDIA");
    if (media) view.media = jsonSafeMap(media, false);
    const assetId = stringAttr(view.job ?? {}, "result_asset_id");
    if (assetId) {
      const asset = await getRawRow(state, mediaPK(view.summary.tenantId, view.summary.mediaId), `ASSET#${assetId}`);
      if (asset) {
        view.resultAsset = jsonSafeMap(asset, false);
        if (view.gateDecision) {
          await enrichWatermark(state, asset, view.gateDecision);
        }
      }
    }
  }

  try {
    const prompts = await decryptJobPrompts(state, jobId, view.summary.tenantId);
    view.decryptedPrompt = prompts.decryptedPrompt;
    view.decryptedPreparedPrompt = prompts.decryptedPreparedPrompt;
  } catch {
    view.decryptedPrompt = "";
    view.decryptedPreparedPrompt = "";
  }

  view.spans.sort((a, b) => dateMs(a.startAt) - dateMs(b.startAt));
  if (view.spans.length > 0) {
    view.firstEventAt = view.spans[0].startAt;
    const last = view.spans[view.spans.length - 1];
    view.lastEventAt = last.endAt || last.startAt;
    if (view.summary.completedAt && dateMs(view.lastEventAt) > dateMs(view.summary.completedAt)) {
      view.lastEventAt = view.summary.completedAt;
    }
  }
  view.relatedKeys = relatedKeys(jobId, view);
  return { view };
}

async function scanDdb(state: LocalOpsState, body: Record<string, unknown>): Promise<{ rows: DdbRow[]; nextCursor: string }> {
  const pkPrefix = optionalString(body, "pkPrefix");
  const skPrefix = optionalString(body, "skPrefix");
  const filters: string[] = [];
  const values: Record<string, AttributeValue> = {};
  if (pkPrefix) {
    filters.push("begins_with(PK, :pk)");
    values[":pk"] = { S: pkPrefix };
  }
  if (skPrefix) {
    filters.push("begins_with(SK, :sk)");
    values[":sk"] = { S: skPrefix };
  }
  const out = await state.ddb.send(new ScanCommand({
    TableName: state.table,
    Limit: normalizeLimit(numberBody(body, "limit", DEFAULT_LIST_LIMIT)),
    ExclusiveStartKey: decodeCursor(optionalString(body, "cursor")),
    FilterExpression: filters.length > 0 ? filters.join(" AND ") : undefined,
    ExpressionAttributeValues: Object.keys(values).length > 0 ? values : undefined,
  }));
  return {
    rows: (out.Items ?? []).map((item) => ddbRow(unmarshall(item))),
    nextCursor: encodeCursor(out.LastEvaluatedKey),
  };
}

async function getDdbRow(state: LocalOpsState, pk: string, sk: string): Promise<{ row?: DdbRow }> {
  const row = await getRawRow(state, pk, sk);
  return { row: row ? ddbRow(row) : undefined };
}

async function putDdbAttr(state: LocalOpsState, body: Record<string, unknown>): Promise<void> {
  const pk = stringBody(body, "pk");
  const sk = stringBody(body, "sk");
  const attributeName = stringBody(body, "attributeName");
  if (!pk || !sk || !attributeName) throw new Error("pk, sk, and attributeName are required");
  if (attributeName === "PK" || attributeName === "SK") throw new Error("refusing to mutate PK/SK");
  if (pk.startsWith("AUDIT#")) throw new Error("audit rows are immutable");
  const decoded = JSON.parse(stringBody(body, "valueJson"));
  const marshalled = marshall({ value: decoded }, { removeUndefinedValues: true });
  await state.ddb.send(new UpdateItemCommand({
    TableName: state.table,
    Key: marshall({ PK: pk, SK: sk }),
    UpdateExpression: "SET #n = :v",
    ExpressionAttributeNames: { "#n": attributeName },
    ExpressionAttributeValues: { ":v": marshalled.value },
  }));
}

async function deleteDdbRow(state: LocalOpsState, pk: string, sk: string): Promise<void> {
  if (!pk || !sk) throw new Error("pk and sk are required");
  if (pk.startsWith("AUDIT#")) throw new Error("audit rows are immutable");
  await state.ddb.send(new DeleteItemCommand({
    TableName: state.table,
    Key: marshall({ PK: pk, SK: sk }),
  }));
}

async function decryptJobPrompts(state: LocalOpsState, jobId: string, tenantId = ""): Promise<{ decryptedPrompt: string; decryptedPreparedPrompt: string }> {
  const row = await getRawRow(state, `JOB#${jobId}`, "JOB");
  if (!row) throw new Error(`job ${jobId} not found`);
  const contextTenant = tenantId || stringAttr(row, "tenant_id");
  const context = { tenantId: contextTenant, jobId };
  const decryptDataKey = async (encryptedDataKey: Uint8Array) => {
    const out = await state.kms.send(new DecryptCommand({
      CiphertextBlob: encryptedDataKey,
      EncryptionContext: { tenant_id: context.tenantId, job_id: context.jobId },
    }));
    if (!out.Plaintext) throw new Error("kms decrypt returned empty plaintext");
    return out.Plaintext;
  };
  return {
    decryptedPrompt: await decryptPromptField(row.encrypted_prompt, context, decryptDataKey),
    decryptedPreparedPrompt: await decryptPromptField(row.encrypted_prepared_prompt, context, decryptDataKey),
  };
}

async function queueDepths(state: LocalOpsState): Promise<{ queues: QueueStat[] }> {
  const urls = await listQueueURLs(state);
  const rows = await Promise.all(urls.map(async (url) => queueStat(state, url)));
  const dlqDepth = new Map(rows.filter((q) => isDlq(q.name)).map((q) => [q.name, q.visible]));
  for (const row of rows) {
    if (!isDlq(row.name)) {
      const dlqName = `${row.name}-dlq`;
      if (dlqDepth.has(dlqName)) {
        row.dlqName = dlqName;
        row.dlqCount = dlqDepth.get(dlqName) ?? 0;
      }
    } else {
      row.dlqName = row.name;
    }
  }
  rows.sort((a, b) => {
    if (isDlq(a.name) !== isDlq(b.name)) return isDlq(a.name) ? 1 : -1;
    return a.name.localeCompare(b.name);
  });
  return { queues: rows };
}

async function purgeQueue(state: LocalOpsState, queueName: string): Promise<void> {
  const url = await queueURLByName(state, queueName);
  await state.sqs.send(new PurgeQueueCommand({ QueueUrl: url }));
}

async function redriveDlq(state: LocalOpsState, dlqName: string, limit: number): Promise<{ moved: number; failed: number }> {
  const capped = Math.min(Math.max(Math.floor(limit || 10), 1), 100);
  const dlqURL = await queueURLByName(state, dlqName);
  const sourceURL = await queueURLByName(state, dlqName.replace(/-dlq$/, ""));
  let moved = 0;
  let failed = 0;
  while (moved + failed < capped) {
    const remaining = capped - moved - failed;
    const batchSize = Math.min(10, remaining);
    const received = await state.sqs.send(new ReceiveMessageCommand({
      QueueUrl: dlqURL,
      MaxNumberOfMessages: batchSize,
      WaitTimeSeconds: 0,
      VisibilityTimeout: 30,
      MessageAttributeNames: ["All"],
    }));
    const messages = received.Messages ?? [];
    if (messages.length === 0) break;
    for (const message of messages) {
      try {
        await state.sqs.send(new SendMessageCommand({
          QueueUrl: sourceURL,
          MessageBody: message.Body ?? "",
          MessageAttributes: message.MessageAttributes,
        }));
        await state.sqs.send(new DeleteMessageCommand({
          QueueUrl: dlqURL,
          ReceiptHandle: message.ReceiptHandle,
        }));
        moved += 1;
      } catch {
        failed += 1;
      }
    }
  }
  return { moved, failed };
}

async function tenantUsage(state: LocalOpsState, tenantId = ""): Promise<{ tenantId: string; currentDailyPeriod: string; dailyCost: TenantUsageReservoir; reservoirs: TenantUsageReservoir[] }> {
  const id = tenantId || state.tenantId;
  const period = dailyPeriod(new Date());
  const rows = await scanAll(state, {
    filterExpression: "SK = :sk AND begins_with(PK, :pk)",
    values: marshall({ ":sk": "AGG", ":pk": `RESERVOIR#TENANT#${id}#` }),
  });
  const reservoirs = rows.map(tenantReservoir).filter((row): row is TenantUsageReservoir => !!row);
  reservoirs.sort((a, b) => {
    const rank = usageRank(a.metric) - usageRank(b.metric);
    if (rank !== 0) return rank;
    if (a.period !== b.period) return b.period.localeCompare(a.period);
    return a.metric.localeCompare(b.metric);
  });
  const dailyCost = reservoirs.find((row) => row.metric === "COST_MICRO_USD" && row.period === period) ?? {
    tenantId: id,
    metric: "COST_MICRO_USD",
    period,
    cap: COST_MICRO_USD_CAP,
    available: COST_MICRO_USD_CAP,
    reserved: 0,
    committed: 0,
    released: 0,
    state: "OPEN",
    policyId: "",
    policyVersion: 0,
    materialized: false,
  };
  return { tenantId: id, currentDailyPeriod: period, dailyCost, reservoirs };
}

async function listS3(state: LocalOpsState, body: Record<string, unknown>): Promise<{ nodes: S3Node[] }> {
  const prefix = optionalString(body, "prefix");
  const delimiter = optionalString(body, "delimiter") || "/";
  const out = await state.s3.send(new ListObjectsV2Command({
    Bucket: state.bucket,
    Prefix: prefix,
    Delimiter: delimiter,
    MaxKeys: normalizeLimit(numberBody(body, "limit", 200), 1000),
  }));
  const nodes: S3Node[] = [];
  for (const common of out.CommonPrefixes ?? []) {
    const key = common.Prefix ?? "";
    nodes.push({
      key,
      name: path.posix.basename(key.replace(/\/$/, "")),
      isPrefix: true,
      sizeBytes: 0,
      etag: "",
    });
  }
  for (const obj of out.Contents ?? []) {
    const key = obj.Key ?? "";
    nodes.push({
      key,
      name: path.posix.basename(key),
      isPrefix: false,
      sizeBytes: Number(obj.Size ?? 0),
      etag: (obj.ETag ?? "").replaceAll("\"", ""),
      lastModified: obj.LastModified?.toISOString(),
    });
  }
  nodes.sort((a, b) => {
    if (a.isPrefix !== b.isPrefix) return a.isPrefix ? -1 : 1;
    return a.name.localeCompare(b.name);
  });
  return { nodes };
}

async function presignDownload(state: LocalOpsState, key: string): Promise<{ url: string; expiresAt: string }> {
  if (!key) throw new Error("key required");
  const expiresIn = 15 * 60;
  const url = await getSignedUrl(state.s3, new GetObjectCommand({ Bucket: state.bucket, Key: key }), { expiresIn });
  return { url, expiresAt: new Date(Date.now() + expiresIn * 1000).toISOString() };
}

async function deleteS3Object(state: LocalOpsState, key: string): Promise<void> {
  if (!key) throw new Error("key required");
  await state.s3.send(new DeleteObjectCommand({ Bucket: state.bucket, Key: key }));
}

async function streamLogs(state: LocalOpsState, body: Record<string, unknown>): Promise<{ lines: LogLine[]; nextSince: string }> {
  const end = new Date();
  const since = optionalString(body, "since");
  const tailLines = numberBody(body, "tailLines", 200);
  const lookbackSeconds = numberBody(body, "lookbackSeconds", 60 * 60);
  const direction = since ? "forward" : tailLines > 0 ? "backward" : "forward";
  const start = since ? new Date(since) : new Date(end.getTime() - Math.max(lookbackSeconds, tailLines > 0 ? 24 * 60 * 60 : 0) * 1000);
  const url = lokiQueryURL(state.lokiURL);
  url.searchParams.set("query", buildLokiQuery(body));
  url.searchParams.set("start", String(start.getTime() * 1_000_000));
  url.searchParams.set("end", String(end.getTime() * 1_000_000));
  url.searchParams.set("limit", String(Math.min(Math.max(tailLines || 200, 1), 1000)));
  url.searchParams.set("direction", direction);
  const res = await fetch(url);
  if (!res.ok) throw new Error(`loki ${res.status}: ${await res.text()}`);
  const envelope = await res.json() as {
    data?: { result?: { stream?: Record<string, string>; values?: [string, string][] }[] };
  };
  const lines: LogLine[] = [];
  for (const stream of envelope.data?.result ?? []) {
    const labels = stream.stream ?? {};
    const service = labels.service_name ?? "";
    const level = labels.severity_text || labels.detected_level || "";
    for (const [ns, line] of stream.values ?? []) {
      const ms = Math.floor(Number(ns) / 1_000_000);
      lines.push({ ts: new Date(ms).toISOString(), service, level, body: line, labels });
    }
  }
  lines.sort((a, b) => dateMs(a.ts) - dateMs(b.ts));
  const filtered = since ? lines.filter((line) => dateMs(line.ts) > dateMs(since)) : lines;
  const nextSince = filtered.length > 0 ? String(filtered[filtered.length - 1].ts ?? since) : since;
  return { lines: filtered, nextSince };
}

async function updateJobTerminal(state: LocalOpsState, jobId: string, status: string, message: string, code = ""): Promise<void> {
  if (!jobId) throw new Error("jobId required");
  const now = nowISO();
  await state.ddb.send(new UpdateItemCommand({
    TableName: state.table,
    Key: marshall({ PK: `JOB#${jobId}`, SK: "JOB" }),
    UpdateExpression: "SET #status = :status, updated_at = :now, completed_at = :now, error_message = :msg, error_code = :code, gsi_job_pk = :gpk",
    ExpressionAttributeNames: { "#status": "status" },
    ExpressionAttributeValues: marshall({
      ":status": status,
      ":now": now,
      ":msg": message,
      ":code": code || status,
      ":gpk": `TENANT#${state.tenantId}#STATUS#${status}`,
    }),
  }));
}

async function retryJob(state: LocalOpsState, jobId: string): Promise<void> {
  if (!jobId) throw new Error("jobId required");
  const row = await getRawRow(state, `JOB#${jobId}`, "JOB");
  if (!row) throw new Error(`job ${jobId} not found`);
  const now = nowISO();
  await state.ddb.send(new UpdateItemCommand({
    TableName: state.table,
    Key: marshall({ PK: `JOB#${jobId}`, SK: "JOB" }),
    UpdateExpression: "SET #status = :running, updated_at = :now, gsi_job_pk = :gpk REMOVE completed_at",
    ExpressionAttributeNames: { "#status": "status" },
    ExpressionAttributeValues: marshall({
      ":running": "RUNNING",
      ":now": now,
      ":gpk": `TENANT#${stringAttr(row, "tenant_id")}#STATUS#RUNNING`,
    }),
  }));
  await replayOutbox(state, jobId);
}

async function replayOutbox(state: LocalOpsState, jobId: string): Promise<void> {
  const row = await getRawRow(state, `JOB#${jobId}`, "JOB");
  if (!row) throw new Error(`job ${jobId} not found`);
  const now = new Date();
  const tenantId = stringAttr(row, "tenant_id");
  const stage = stringAttr(row, "current_stage") || "INPUT_MODERATION";
  const tier = stringAttr(row, "tier") || "FREE";
  const body = Buffer.from(JSON.stringify({
    tenant_id: tenantId,
    tenant_lane: tenantLane(tenantId),
    job_id: jobId,
    stage,
    stage_version: numberAttr(row, "stage_version") || 1,
    resource_class: "FAST",
  }));
  const item = outboxJobItem({
    jobId,
    tenantId,
    tenantLane: tenantLane(tenantId),
    tier,
    stage,
    resourceClass: "FAST",
    body,
    partitionTS: now,
  });
  await state.ddb.send(new PutItemCommand({
    TableName: state.table,
    Item: marshall(item),
    ConditionExpression: "attribute_not_exists(PK)",
  }));
}

type ScanUntilOpts<T> = {
  limit: number;
  cursor?: string;
  filterExpression: string;
  values: Record<string, AttributeValue>;
  decode: (row: RawRow) => T | null;
};

async function scanUntilLimit<T>(state: LocalOpsState, opts: ScanUntilOpts<T>): Promise<{ items: T[]; nextCursor: string }> {
  const limit = normalizeLimit(opts.limit);
  let cursor = decodeCursor(opts.cursor);
  let nextCursor = "";
  const items: T[] = [];
  while (items.length < limit) {
    const out = await state.ddb.send(new ScanCommand({
      TableName: state.table,
      Limit: limit * 4,
      ExclusiveStartKey: cursor,
      FilterExpression: opts.filterExpression,
      ExpressionAttributeValues: opts.values,
    }));
    for (const item of out.Items ?? []) {
      const decoded = opts.decode(unmarshall(item));
      if (decoded) items.push(decoded);
      if (items.length >= limit) break;
    }
    if (!out.LastEvaluatedKey) break;
    cursor = out.LastEvaluatedKey;
    nextCursor = encodeCursor(cursor);
  }
  return { items, nextCursor };
}

async function scanAll(state: LocalOpsState, opts: { filterExpression: string; values: Record<string, AttributeValue> }): Promise<RawRow[]> {
  const rows: RawRow[] = [];
  let cursor: DdbItem | undefined;
  do {
    const out = await state.ddb.send(new ScanCommand({
      TableName: state.table,
      ExclusiveStartKey: cursor,
      FilterExpression: opts.filterExpression,
      ExpressionAttributeValues: opts.values,
    }));
    rows.push(...(out.Items ?? []).map((item) => unmarshall(item)));
    cursor = out.LastEvaluatedKey;
  } while (cursor);
  return rows;
}

async function queryPK(state: LocalOpsState, pk: string): Promise<RawRow[]> {
  const rows: RawRow[] = [];
  let cursor: DdbItem | undefined;
  do {
    const out = await state.ddb.send(new QueryCommand({
      TableName: state.table,
      KeyConditionExpression: "PK = :pk",
      ExpressionAttributeValues: marshall({ ":pk": pk }),
      ExclusiveStartKey: cursor,
      ConsistentRead: true,
    }));
    rows.push(...(out.Items ?? []).map((item) => unmarshall(item)));
    cursor = out.LastEvaluatedKey;
  } while (cursor);
  return rows;
}

async function getRawRow(state: LocalOpsState, pk: string, sk: string): Promise<RawRow | null> {
  const out = await state.ddb.send(new GetItemCommand({
    TableName: state.table,
    Key: marshall({ PK: pk, SK: sk }),
    ConsistentRead: true,
  }));
  return out.Item ? unmarshall(out.Item) : null;
}

function jobSummary(row: RawRow): JobSummary {
  return {
    jobId: stringAttr(row, "id"),
    tenantId: stringAttr(row, "tenant_id"),
    mediaId: stringAttr(row, "media_id") || undefined,
    status: stringAttr(row, "status"),
    currentStage: stringAttr(row, "current_stage"),
    outputType: stringAttr(row, "output_type"),
    tier: stringAttr(row, "tier"),
    model: stringAttr(row, "model"),
    attempts: numberAttr(row, "attempts"),
    errorCode: stringAttr(row, "error_code"),
    createdAt: isoAttr(row, "created_at"),
    updatedAt: isoAttr(row, "updated_at"),
    completedAt: isoAttr(row, "completed_at"),
  };
}

function mediaRow(row: RawRow): MediaRow {
  return {
    mediaId: stringAttr(row, "id"),
    tenantId: stringAttr(row, "tenant_id"),
    ownerUserId: stringAttr(row, "owner_user_id"),
    origin: stringAttr(row, "origin"),
    mediaType: stringAttr(row, "media_type"),
    lifecycle: stringAttr(row, "lifecycle"),
    originalAssetId: stringAttr(row, "original_asset_id") || undefined,
    createdAt: isoAttr(row, "created_at"),
    updatedAt: isoAttr(row, "updated_at"),
    deletedAt: isoAttr(row, "deleted_at"),
  };
}

function ddbRow(row: RawRow): DdbRow {
  const safe = jsonSafeMap(row, true);
  return {
    pk: stringAttr(row, "PK"),
    sk: stringAttr(row, "SK"),
    itemType: stringAttr(row, "item_type"),
    attributes: safe,
  };
}

function attemptSpan(row: RawRow): TraceSpan {
  const recordedStage = stringAttr(row, "stage");
  let stage = recordedStage;
  let label = `attempt #${numberAttr(row, "attempt_no")}`;
  const errorCode = stringAttr(row, "error_code");
  let status = "OK";
  const result = stringAttr(row, "result");
  if (result === "TRANSIENT_FAILURE") status = "TRANSIENT_FAIL";
  if (result === "TERMINAL_FAILURE") status = "TERMINAL_FAIL";
  if (errorCode === "PROVIDER_UNAVAILABLE") {
    stage = "WORKER_PRECHECK";
    label = "provider resolution";
  }
  const attrs: Record<string, string> = {
    next_stage: stringAttr(row, "next_stage"),
    resource_class: stringAttr(row, "resource_class"),
    stage_version: String(numberAttr(row, "stage_version")),
    attempt_no: String(numberAttr(row, "attempt_no")),
  };
  if (stage !== recordedStage) attrs.recorded_stage = recordedStage;
  copyStringAttr(row, attrs, "traceparent");
  copyStringAttr(row, attrs, "trace_id");
  return {
    id: `attempt:${stage}:v${numberAttr(row, "stage_version")}:a${numberAttr(row, "attempt_no")}`,
    parentId: "",
    kind: "ATTEMPT",
    label,
    status,
    stage,
    resourceClass: stringAttr(row, "resource_class"),
    attemptNo: numberAttr(row, "attempt_no"),
    errorCode,
    errorMessage: stringAttr(row, "error_message"),
    attributes: attrs,
    startAt: isoAttr(row, "created_at"),
    endAt: isoAttr(row, "created_at"),
    durationMs: 0,
    pk: stringAttr(row, "PK"),
    sk: stringAttr(row, "SK"),
  };
}

function providerSpan(row: RawRow): TraceSpan {
  const created = isoAttr(row, "created_at");
  const completed = isoAttr(row, "completed_at");
  const updated = isoAttr(row, "updated_at");
  const end = completed || updated;
  const providerStatus = stringAttr(row, "status");
  const status = providerStatus === "SUCCEEDED" ? "OK" : providerStatus === "FAILED" ? "TERMINAL_FAIL" : "PENDING";
  return {
    id: `provider:${stringAttr(row, "provider_request_id")}`,
    parentId: "",
    kind: "PROVIDER_REQUEST",
    label: `${stringAttr(row, "provider")} · ${stringAttr(row, "call_type")}`,
    status,
    stage: "PROVIDER_SUBMIT",
    resourceClass: "",
    attemptNo: 0,
    errorCode: stringAttr(row, "error_code"),
    errorMessage: stringAttr(row, "error_message"),
    attributes: {
      provider: stringAttr(row, "provider"),
      model: stringAttr(row, "model"),
      call_type: stringAttr(row, "call_type"),
      request_hash: stringAttr(row, "request_hash"),
      vendor_request_id: stringAttr(row, "vendor_request_id"),
      provider_job_id: stringAttr(row, "provider_job_id"),
      status: providerStatus,
    },
    startAt: created,
    endAt: end,
    durationMs: Math.max(0, dateMs(end) - dateMs(created)),
    pk: stringAttr(row, "PK"),
    sk: stringAttr(row, "SK"),
  };
}

function terminalSpan(row: RawRow): TraceSpan {
  const status = stringAttr(row, "status");
  const mapped = status === "FAILED" || status === "CANCELLED" ? "TERMINAL_FAIL" : "OK";
  const at = isoAttr(row, "created_at");
  return {
    id: "terminal",
    parentId: "",
    kind: "TERMINAL",
    label: `terminal · ${status.toLowerCase()}`,
    status: mapped,
    stage: "TERMINAL",
    resourceClass: "",
    attemptNo: 0,
    errorCode: stringAttr(row, "error_code"),
    errorMessage: stringAttr(row, "error_message"),
    attributes: { status },
    startAt: at,
    endAt: at,
    durationMs: 0,
    pk: stringAttr(row, "PK"),
    sk: stringAttr(row, "SK"),
  };
}

function outputSpan(row: RawRow): TraceSpan {
  const status = stringAttr(row, "status");
  const created = isoAttr(row, "created_at");
  const completed = isoAttr(row, "completed_at");
  const updated = isoAttr(row, "updated_at") || created;
  const final = outputStatus(status) === "PENDING" ? created : completed || updated;
  return {
    id: `output:${stringAttr(row, "output_id")}`,
    parentId: "",
    kind: "OUTPUT",
    label: `output record · ${status}`,
    status: outputStatus(status),
    stage: "PUBLISH",
    resourceClass: "",
    attemptNo: 0,
    errorCode: "",
    errorMessage: "",
    attributes: {
      output_id: stringAttr(row, "output_id"),
      type: stringAttr(row, "type"),
      status,
    },
    startAt: final,
    endAt: final,
    durationMs: 0,
    pk: stringAttr(row, "PK"),
    sk: stringAttr(row, "SK"),
  };
}

function variantSpan(row: RawRow): TraceSpan {
  const created = isoAttr(row, "created_at");
  const updated = isoAttr(row, "updated_at") || created;
  return {
    id: `variant:${stringAttr(row, "variant_id")}`,
    parentId: "",
    kind: "VARIANT",
    label: `variant #${numberAttr(row, "index")}`,
    status: "OK",
    stage: "PUBLISH",
    resourceClass: "",
    attemptNo: 0,
    errorCode: "",
    errorMessage: "",
    attributes: {
      variant_id: stringAttr(row, "variant_id"),
      final_asset_id: stringAttr(row, "final_asset_id"),
      provider: stringAttr(row, "provider"),
      model: stringAttr(row, "model"),
      mime: stringAttr(row, "mime"),
    },
    startAt: created,
    endAt: updated,
    durationMs: Math.max(0, dateMs(updated) - dateMs(created)),
    pk: stringAttr(row, "PK"),
    sk: stringAttr(row, "SK"),
  };
}

function deriveStageSpans(spans: TraceSpan[]): TraceSpan[] {
  const byStage = new Map<string, TraceSpan>();
  const order: string[] = [];
  for (const span of spans) {
    if (span.kind !== "ATTEMPT" || !span.stage) continue;
    let stage = byStage.get(span.stage);
    if (!stage) {
      stage = {
        id: `stage:${span.stage}`,
        parentId: "",
        kind: "STAGE",
        label: span.stage,
        status: span.status,
        stage: span.stage,
        resourceClass: span.resourceClass,
        attemptNo: 0,
        errorCode: "",
        errorMessage: "",
        attributes: {},
        startAt: span.startAt,
        endAt: span.endAt,
        durationMs: 0,
        pk: "",
        sk: "",
      };
      byStage.set(span.stage, stage);
      order.push(span.stage);
    }
    if (dateMs(span.startAt) < dateMs(stage.startAt)) stage.startAt = span.startAt;
    if (dateMs(span.endAt) > dateMs(stage.endAt)) stage.endAt = span.endAt;
    stage.status = worseStatus(stage.status, span.status);
    if (span.errorCode) {
      stage.errorCode = span.errorCode;
      stage.errorMessage = span.errorMessage;
    }
    copyAttrIfPresent(stage.attributes, span.attributes, "trace_id");
    copyAttrIfPresent(stage.attributes, span.attributes, "traceparent");
  }
  return order.map((stageName) => {
    const stage = byStage.get(stageName);
    if (!stage) throw new Error("unreachable missing stage");
    stage.durationMs = Math.max(0, dateMs(stage.endAt) - dateMs(stage.startAt));
    return stage;
  });
}

function runningStageSpan(summary: JobSummary, spans: TraceSpan[]): TraceSpan | null {
  const stage = summary.currentStage;
  if (!stage || stage === "TERMINAL" || spans.some((span) => span.kind === "STAGE" && span.stage === stage)) return null;
  const child = spans
    .filter((span) => span.stage === stage && span.kind !== "ATTEMPT")
    .sort((a, b) => dateMs(a.startAt) - dateMs(b.startAt))[0];
  if (!child) return null;
  return {
    id: `stage:${stage}`,
    parentId: "",
    kind: "STAGE",
    label: stage,
    status: "PENDING",
    stage,
    resourceClass: "",
    attemptNo: 0,
    errorCode: "",
    errorMessage: "",
    attributes: { end_synthesized: "in_flight" },
    startAt: child.startAt,
    endAt: nowISO(),
    durationMs: Math.max(0, Date.now() - dateMs(child.startAt)),
    pk: "",
    sk: "",
  };
}

function closeStageEnds(spans: TraceSpan[], terminalAt: string): void {
  const stages = spans.filter((span) => span.kind === "STAGE").sort((a, b) => dateMs(a.startAt) - dateMs(b.startAt));
  for (const stage of stages) {
    const children = spans.filter((span) => span.stage === stage.stage && span.kind !== "STAGE");
    for (const child of children) {
      if (child.kind !== "ATTEMPT" && dateMs(child.startAt) > 0 && dateMs(child.startAt) < dateMs(stage.startAt)) {
        stage.startAt = child.startAt;
      }
      const childEnd = child.endAt || child.startAt;
      if (dateMs(childEnd) > dateMs(stage.endAt)) {
        stage.endAt = childEnd;
      }
    }
    if (dateMs(stage.endAt) < dateMs(stage.startAt)) stage.endAt = stage.startAt;
    if (terminalAt && dateMs(stage.endAt) > dateMs(terminalAt)) stage.endAt = terminalAt;
    stage.durationMs = Math.max(0, dateMs(stage.endAt) - dateMs(stage.startAt));
  }
}

function linkChildrenToStages(spans: TraceSpan[]): void {
  for (const span of spans) {
    if ((span.kind === "ATTEMPT" || span.kind === "PROVIDER_REQUEST" || span.kind === "OUTPUT" || span.kind === "VARIANT") && span.stage) {
      span.parentId = `stage:${span.stage}`;
    }
  }
}

async function gateDecision(state: LocalOpsState, jobId: string): Promise<{ decision: GateDecision; span: TraceSpan } | null> {
  const rows = await queryPK(state, `AUDIT#GATE#${jobId}`);
  if (rows.length === 0) return null;
  rows.sort((a, b) => stringAttr(b, "created_at").localeCompare(stringAttr(a, "created_at")));
  const row = rows[0];
  const decision: GateDecision = {
    jobId: stringAttr(row, "job_id"),
    tenantId: stringAttr(row, "tenant_id"),
    gateVersion: stringAttr(row, "gate_version"),
    outputType: stringAttr(row, "output_type"),
    provider: stringAttr(row, "provider"),
    model: stringAttr(row, "model"),
    decision: stringAttr(row, "decision"),
    errorCode: stringAttr(row, "error_code"),
    watermarkPresent: boolAttr(row, "watermark_present"),
    disclosurePresent: boolAttr(row, "disclosure_present"),
    safetyPresent: boolAttr(row, "safety_present"),
    watermarkFingerprint: "",
    watermarkAlgo: "",
    watermarkPosition: "",
    watermarkText: "",
    decidedAt: isoAttr(row, "created_at"),
  };
  const disclosureGate = decision.decision === "PASS" || [
    "WATERMARK_FINGERPRINT_MISSING",
    "WATERMARK_ALGO_MISMATCH",
    "WATERMARK_MISSING",
    "WATERMARK_OR_DISCLOSURE_MISSING",
    "AI_DISCLOSURE_MISSING",
    "OUTPUT_SAFETY_MISSING",
  ].includes(decision.errorCode);
  const span: TraceSpan = {
    id: disclosureGate ? "gate" : "terminal-audit",
    parentId: disclosureGate ? "stage:DISCLOSURE_POSTPROCESS" : "",
    kind: disclosureGate ? "GATE_AUDIT" : "TERMINAL_AUDIT",
    label: disclosureGate ? `gate decision · ${decision.decision}` : `failure audit · ${decision.decision}`,
    status: decision.decision === "PASS" ? "OK" : "TERMINAL_FAIL",
    stage: disclosureGate ? "DISCLOSURE_POSTPROCESS" : "TERMINAL",
    resourceClass: "",
    attemptNo: 0,
    errorCode: decision.errorCode,
    errorMessage: "",
    attributes: {
      decision: decision.decision,
      error_code: decision.errorCode,
      watermark_present: String(decision.watermarkPresent),
      disclosure_present: String(decision.disclosurePresent),
      safety_present: String(decision.safetyPresent),
    },
    startAt: decision.decidedAt,
    endAt: decision.decidedAt,
    durationMs: 0,
    pk: stringAttr(row, "PK"),
    sk: stringAttr(row, "SK"),
  };
  return { decision, span };
}

async function enrichWatermark(state: LocalOpsState, asset: RawRow, decision: GateDecision): Promise<void> {
  const key = stringAttr(asset, "storage_key");
  if (!key) return;
  try {
    const out = await state.s3.send(new HeadObjectCommand({ Bucket: state.bucket, Key: key }));
    const meta = out.Metadata ?? {};
    decision.watermarkFingerprint = meta["ai-watermark-fingerprint"] ?? meta["watermark-fingerprint"] ?? "";
    decision.watermarkAlgo = meta["ai-watermark-algo"] ?? meta["watermark-algo"] ?? "";
    decision.watermarkPosition = meta["ai-watermark-position"] ?? meta["watermark-position"] ?? "";
    decision.watermarkText = meta["ai-watermark-text"] ?? meta["watermark-text"] ?? "";
  } catch {
    return;
  }
}

async function findJobForMedia(state: LocalOpsState, tenantId: string, mediaId: string): Promise<string> {
  const rows = await scanAll(state, {
    filterExpression: "item_type = :type AND media_id = :media AND tenant_id = :tenant",
    values: marshall({ ":type": "GEN", ":media": mediaId, ":tenant": tenantId }),
  });
  rows.sort((a, b) => stringAttr(b, "created_at").localeCompare(stringAttr(a, "created_at")));
  return stringAttr(rows[0] ?? {}, "id");
}

function tenantReservoir(row: RawRow): TenantUsageReservoir | null {
  if (stringAttr(row, "scope_type") !== "TENANT") return null;
  const tenantId = stringAttr(row, "scope_id");
  const metric = stringAttr(row, "metric");
  const period = stringAttr(row, "period");
  if (!tenantId || !metric || !period) return null;
  return {
    tenantId,
    metric,
    period,
    cap: numberAttr(row, "cap"),
    available: numberAttr(row, "available"),
    reserved: numberAttr(row, "reserved"),
    committed: numberAttr(row, "committed"),
    released: numberAttr(row, "released"),
    state: stringAttr(row, "state"),
    policyId: stringAttr(row, "policy_id"),
    policyVersion: numberAttr(row, "policy_version"),
    createdAt: isoAttr(row, "created_at"),
    updatedAt: isoAttr(row, "updated_at"),
    materialized: true,
  };
}

async function listQueueURLs(state: LocalOpsState): Promise<string[]> {
  const urls: string[] = [];
  let token: string | undefined;
  do {
    const out = await state.sqs.send(new ListQueuesCommand({ NextToken: token }));
    urls.push(...(out.QueueUrls ?? []));
    token = out.NextToken;
  } while (token);
  return urls;
}

async function queueURLByName(state: LocalOpsState, name: string): Promise<string> {
  const urls = await listQueueURLs(state);
  const url = urls.find((candidate) => queueName(candidate) === name);
  if (!url) throw new Error(`unknown queue ${name}`);
  return url;
}

async function queueStat(state: LocalOpsState, url: string): Promise<QueueStat> {
  const attrs = await state.sqs.send(new GetQueueAttributesCommand({
    QueueUrl: url,
    AttributeNames: [QueueAttributeName.All],
  }));
  const values: Record<string, string | undefined> = attrs.Attributes ?? {};
  const name = queueName(url);
  return {
    name,
    url,
    visible: parseInt(values.ApproximateNumberOfMessages ?? "0", 10) || 0,
    inFlight: parseInt(values.ApproximateNumberOfMessagesNotVisible ?? "0", 10) || 0,
    delayed: parseInt(values.ApproximateNumberOfMessagesDelayed ?? "0", 10) || 0,
    visibilityTimeoutSeconds: parseInt(values.VisibilityTimeout ?? "0", 10) || 0,
    oldestMessageAgeSeconds: parseInt(values["ApproximateAgeOfOldestMessage"] ?? "0", 10) || 0,
    dlqName: "",
    dlqCount: 0,
    tierClass: tierClassFor(name),
  };
}

function outputStatus(status: string): string {
  const upper = status.toUpperCase();
  if (upper === "COMPLETE" || upper === "READY" || upper === "SUCCEEDED") return "OK";
  if (upper === "FAILED" || upper === "CANCELLED") return "TERMINAL_FAIL";
  return "PENDING";
}

function worseStatus(a: string, b: string): string {
  const rank = (status: string) => status === "TERMINAL_FAIL" ? 3 : status === "TRANSIENT_FAIL" ? 2 : status === "PENDING" ? 1 : 0;
  return rank(b) > rank(a) ? b : a;
}

function relatedKeys(jobId: string, view: FullJobView): string[] {
  const keys = [`JOB#${jobId}|JOB`];
  const summary = view.summary;
  if (summary?.tenantId && summary.mediaId) {
    keys.push(`${mediaPK(summary.tenantId, summary.mediaId)}|MEDIA`);
    const assetId = stringAttr(view.job ?? {}, "result_asset_id");
    if (assetId) keys.push(`${mediaPK(summary.tenantId, summary.mediaId)}|ASSET#${assetId}`);
  }
  keys.push(`AUDIT#GATE#${jobId}|`);
  return keys;
}

function jsonSafeMap(row: RawRow, redactPrompts: boolean): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(row)) {
    out[key] = jsonSafeValue(value, key, redactPrompts);
    if (redactPrompts && PROMPT_FIELDS.has(key) && isBytes(value)) {
      out[`${key}_redacted`] = true;
    }
  }
  return out;
}

function jsonSafeValue(value: unknown, key: string, redactPrompts: boolean): unknown {
  if (value === null || value === undefined) return value;
  if (isBytes(value)) {
    return redactPrompts && PROMPT_FIELDS.has(key)
      ? `<encrypted:${value.byteLength}B>`
      : `<bytes:${value.byteLength}B>`;
  }
  if (Array.isArray(value)) return value.map((child) => jsonSafeValue(child, key, redactPrompts));
  if (typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const [childKey, childValue] of Object.entries(value)) {
      out[childKey] = jsonSafeValue(childValue, childKey, redactPrompts);
    }
    return out;
  }
  return value;
}

async function decryptPromptField(value: unknown, context: { tenantId: string; jobId: string }, decryptDataKey: (encryptedDataKey: Uint8Array) => Promise<Uint8Array>): Promise<string> {
  if (!isBytes(value) || value.byteLength === 0) return "";
  return decryptSealedPrompt(value, context, async (encryptedDataKey) => decryptDataKey(encryptedDataKey));
}

function stringBody(body: Record<string, unknown>, key: string): string {
  const value = body[key];
  return typeof value === "string" ? value : "";
}

function optionalString(body: Record<string, unknown>, key: string): string {
  return stringBody(body, key).trim();
}

function numberBody(body: Record<string, unknown>, key: string, fallback: number): number {
  const value = body[key];
  const n = typeof value === "number" ? value : typeof value === "string" ? Number(value) : NaN;
  return Number.isFinite(n) ? n : fallback;
}

function stringAttr(row: RawRow, key: string): string {
  const value = row[key];
  if (typeof value === "string") return value;
  if (typeof value === "number" || typeof value === "bigint" || typeof value === "boolean") return String(value);
  return "";
}

function numberAttr(row: RawRow, key: string): number {
  const value = row[key];
  if (typeof value === "number") return value;
  if (typeof value === "bigint") return Number(value);
  if (typeof value === "string") {
    const n = Number(value);
    return Number.isFinite(n) ? n : 0;
  }
  return 0;
}

function boolAttr(row: RawRow, key: string): boolean {
  return row[key] === true;
}

function isoAttr(row: RawRow, key: string): string | undefined {
  const value = stringAttr(row, key);
  if (!value) return undefined;
  const ms = Date.parse(value);
  return Number.isFinite(ms) ? new Date(ms).toISOString() : value;
}

function dateMs(value: unknown): number {
  if (typeof value !== "string") return 0;
  const n = Date.parse(value);
  return Number.isFinite(n) ? n : 0;
}

function compareDesc(a: unknown, b: unknown): number {
  return dateMs(b) - dateMs(a);
}

function copyStringAttr(row: RawRow, out: Record<string, string>, key: string): void {
  const value = stringAttr(row, key);
  if (value) out[key] = value;
}

function copyAttrIfPresent(dst: Record<string, string>, src: Record<string, string>, key: string): void {
  if (!dst[key] && src[key]) dst[key] = src[key];
}

function isBytes(value: unknown): value is Uint8Array {
  return value instanceof Uint8Array || Buffer.isBuffer(value);
}

function normalizeLimit(limit: number, max = MAX_LIST_LIMIT): number {
  if (!Number.isFinite(limit) || limit <= 0) return DEFAULT_LIST_LIMIT;
  return Math.min(Math.floor(limit), max);
}

function encodeCursor(key: DdbItem | undefined): string {
  if (!key || Object.keys(key).length === 0) return "";
  return Buffer.from(JSON.stringify(key), "utf8").toString("base64url");
}

function decodeCursor(cursor = ""): DdbItem | undefined {
  if (!cursor) return undefined;
  return JSON.parse(Buffer.from(cursor, "base64url").toString("utf8")) as DdbItem;
}

function mediaPK(tenantId: string, mediaId: string): string {
  return `TENANT#${tenantId}#MEDIA#${mediaId}`;
}

function nowISO(): string {
  return new Date().toISOString();
}

function dailyPeriod(date: Date): string {
  const yyyy = date.getUTCFullYear();
  const mm = String(date.getUTCMonth() + 1).padStart(2, "0");
  const dd = String(date.getUTCDate()).padStart(2, "0");
  return `${yyyy}${mm}${dd}`;
}

function usageRank(metric: string): number {
  switch (metric) {
    case "COST_MICRO_USD": return 0;
    case "GENERATED_OUTPUTS": return 1;
    case "REQUESTS": return 2;
    case "STORAGE_BYTES": return 3;
    default: return 9;
  }
}

function queueName(url: string): string {
  return decodeURIComponent(url.split("/").filter(Boolean).pop() ?? url);
}

function isDlq(name: string): boolean {
  return name.endsWith("-dlq");
}

function tierClassFor(name: string): string {
  if (!name.startsWith("generation-jobs-")) return "";
  const rest = name.slice("generation-jobs-".length);
  const splitAt = rest.indexOf("-");
  return splitAt === -1 ? rest : `${rest.slice(0, splitAt)}/${rest.slice(splitAt + 1)}`;
}

function tenantLane(tenantId: string): string {
  return `lane-${createHash("sha256").update(tenantId).digest("hex").slice(0, 4)}`;
}

function outboxJobItem(row: {
  jobId: string;
  tenantId: string;
  tenantLane: string;
  tier: string;
  stage: string;
  resourceClass: string;
  body: Buffer;
  partitionTS: Date;
}): RawRow {
  const pk = `OUTBOX#GEN#${outboxHour(row.partitionTS)}#${outboxShard(row.jobId, 8)}`;
  const sk = `${row.partitionTS.toISOString()}#${row.jobId}#${row.stage}`;
  return {
    PK: pk,
    SK: sk,
    stream: "GEN",
    body: row.body,
    body_sha256: createHash("sha256").update(row.body).digest("hex"),
    status: "PENDING",
    attempts: 0,
    lease_until: 0,
    next_attempt_at: Math.floor(row.partitionTS.getTime() / 1000),
    ttl_epoch: Math.floor(row.partitionTS.getTime() / 1000) + 7 * 24 * 60 * 60,
    job_id: row.jobId,
    tenant_id: row.tenantId,
    tenant_lane: row.tenantLane,
    tier: row.tier,
    stage: row.stage,
    resource_class: row.resourceClass,
  };
}

function outboxHour(date: Date): string {
  return `${date.getUTCFullYear()}${String(date.getUTCMonth() + 1).padStart(2, "0")}${String(date.getUTCDate()).padStart(2, "0")}${String(date.getUTCHours()).padStart(2, "0")}`;
}

function outboxShard(value: string, count: number): number {
  const digest = createHash("sha256").update(value).digest();
  return digest.readUInt32BE(0) % count;
}

function lokiQueryURL(base: string): URL {
  const url = new URL(base);
  const pathname = url.pathname.replace(/\/$/, "");
  url.pathname = pathname.endsWith("/loki") ? `${pathname}/api/v1/query_range` : `${pathname}/loki/api/v1/query_range`;
  return url;
}

function buildLokiQuery(body: Record<string, unknown>): string {
  const matchers: string[] = [];
  const service = optionalString(body, "service");
  const level = optionalString(body, "level");
  if (service) matchers.push(`service_name=~"${escapeLoki(service)}"`);
  if (level) matchers.push(`severity_text=~"(?i)${escapeLoki(level)}"`);
  let query = matchers.length > 0 ? `{${matchers.join(",")}}` : `{service_name=~".+"}`;
  for (const key of ["jobId", "mediaId", "contains"]) {
    const value = optionalString(body, key);
    if (value) query += ` |= "${escapeLoki(value)}"`;
  }
  return query;
}

function escapeLoki(value: string): string {
  return value.replaceAll("\\", "\\\\").replaceAll("\"", "\\\"");
}

function routeName(req: IncomingMessage): string {
  const url = new URL(req.url ?? "/", "http://localhost");
  return url.pathname.replace(/^\/__local_ops\/?/, "").replace(/^\//, "");
}

async function readJson(req: IncomingMessage): Promise<Record<string, unknown>> {
  const chunks: Buffer[] = [];
  for await (const chunk of req) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  }
  if (chunks.length === 0) return {};
  return JSON.parse(Buffer.concat(chunks).toString("utf8")) as Record<string, unknown>;
}

function sendJson(res: ServerResponse, body: unknown): void {
  const raw = JSON.stringify(body);
  res.statusCode = 200;
  res.setHeader("content-type", "application/json");
  res.end(raw);
}

function sendText(res: ServerResponse, status: number, body: string): void {
  res.statusCode = status;
  res.setHeader("content-type", "text/plain; charset=utf-8");
  res.end(body);
}
