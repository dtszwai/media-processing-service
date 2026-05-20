import type { Timestamp } from "@bufbuild/protobuf/wkt";

export type GenerationProviderModels = {
  outputType: string;
  provider: string;
  models: string[];
  defaultModel: string;
};

export type JobSummary = {
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
  createdAt?: Timestamp;
  updatedAt?: Timestamp;
  completedAt?: Timestamp;
};

export type TraceSpan = {
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
  startAt?: Timestamp;
  endAt?: Timestamp;
  durationMs: number;
  pk: string;
  sk: string;
};

export type GateDecision = {
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
  decidedAt?: Timestamp;
};

export type FullJobView = {
  summary?: JobSummary;
  job?: Record<string, unknown>;
  media?: Record<string, unknown>;
  resultAsset?: Record<string, unknown>;
  spans: TraceSpan[];
  gateDecision?: GateDecision;
  relatedKeys: string[];
  firstEventAt?: Timestamp;
  lastEventAt?: Timestamp;
  decryptedPrompt: string;
  decryptedPreparedPrompt: string;
};

export type MediaRow = {
  mediaId: string;
  tenantId: string;
  ownerUserId: string;
  origin: string;
  mediaType: string;
  lifecycle: string;
  originalAssetId?: string;
  createdAt?: Timestamp;
  updatedAt?: Timestamp;
  deletedAt?: Timestamp;
  jobId?: string;
};

export type DdbRow = {
  pk: string;
  sk: string;
  itemType: string;
  attributes: Record<string, unknown>;
};

export type QueueStat = {
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

export type TenantUsageReservoir = {
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
  createdAt?: Timestamp;
  updatedAt?: Timestamp;
  materialized: boolean;
};

export type S3Node = {
  key: string;
  name: string;
  isPrefix: boolean;
  sizeBytes: number;
  etag: string;
  lastModified?: Timestamp;
};

export type LogLine = {
  ts?: Timestamp;
  service: string;
  level: string;
  body: string;
  labels: Record<string, string>;
};

export type PromptDecryptResult = {
  decryptedPrompt: string;
  decryptedPreparedPrompt: string;
};
