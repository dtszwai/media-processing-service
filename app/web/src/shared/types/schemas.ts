/**
 * Zod schemas for API response validation
 * Types are inferred from schemas for type safety
 */
import { z } from "zod";

// ============= Media Schemas =============

export const MediaStatusSchema = z.enum(["PENDING_UPLOAD", "PENDING", "PROCESSING", "COMPLETE", "ERROR", "DELETED"]);

export const MediaTypeSchema = z.enum(["image", "document", "video", "audio", "other"]);

export const MediaSourceSchema = z.enum(["upload", "generated"]);

export const OutputFormatSchema = z.enum(["jpeg", "png", "webp"]);

export const AssetStatusSchema = z.enum(["PENDING_UPLOAD", "PENDING", "PROCESSING", "COMPLETE", "ERROR", "DELETED"]);
export const AssetTypeSchema = z.enum(["ORIGINAL", "DERIVED", "THUMBNAIL", "TEXT"]);
export const AssetOperationSchema = z.enum(["image.process", "image.thumbnail", "document.preview", "document.text"]);

export const MediaSchema = z.object({
  mediaId: z.string(),
  name: z.string(),
  size: z.number(),
  mimetype: z.string(),
  mediaType: MediaTypeSchema.optional(),
  source: MediaSourceSchema.default("upload"),
  status: MediaStatusSchema,
  originalAssetId: z.string().optional(),
  createdAt: z.string().optional(),
  updatedAt: z.string().optional(),
  deletedAt: z.string().optional(),
  documentPageCount: z.number().optional(),
  documentTitle: z.string().optional(),
  documentAuthor: z.string().optional(),
  documentSubject: z.string().optional(),
  documentCreator: z.string().optional(),
  documentProducer: z.string().optional(),
  documentCreationDate: z.string().optional(),
  documentModifiedDate: z.string().optional(),
  documentTextLength: z.number().optional(),
  documentTextTruncated: z.boolean().optional(),
  thumbnailUrl: z.string().optional(),
});

export const InitUploadRequestSchema = z.object({
  fileName: z.string(),
  fileSize: z.number(),
  contentType: z.string(),
  mediaType: MediaTypeSchema.optional(),
  webhookUrl: z.string().url().optional(),
});

export const InitUploadResponseSchema = z.object({
  mediaId: z.string(),
  assetId: z.string().optional(),
  uploadUrl: z.string(),
  expiresIn: z.number(),
  method: z.string(),
  headers: z.record(z.string(), z.string()),
});

export const UploadResponseSchema = z.object({
  mediaId: z.string(),
});

export const MediaAssetSchema = z.object({
  assetId: z.string(),
  mediaId: z.string(),
  sourceAssetId: z.string().optional().nullable(),
  type: AssetTypeSchema,
  tags: z.array(z.string()).optional(),
  status: AssetStatusSchema,
  outputFormat: z.string().optional(),
  mimetype: z.string().optional(),
  size: z.number().optional(),
  width: z.number().optional(),
  height: z.number().optional(),
  downloadName: z.string().optional(),
  operation: AssetOperationSchema.optional(),
  createdAt: z.string().optional(),
  updatedAt: z.string().optional(),
  errorMessage: z.string().optional(),
});

export const CreateAssetOutputSchema = z.object({
  operation: AssetOperationSchema,
  outputFormat: z.string().optional(),
  width: z.number().optional(),
  downloadName: z.string().optional(),
  tags: z.array(z.string()).optional(),
});

export const CreateAssetRequestSchema = z.object({
  sourceAssetId: z.string().optional(),
  outputs: z.array(CreateAssetOutputSchema),
});

export const AssetDownloadUrlResponseSchema = z.object({
  url: z.string(),
});

// ============= Generation Schemas =============

export const GenerationStatusSchema = z.enum(["QUEUED", "RUNNING", "BLOCKED", "FAILED", "COMPLETE"]);

export const GenerationStageSchema = z.enum([
  "ADMISSION",
  "PREPROCESS",
  "INFERENCE",
  "INFERENCE_POLL",
  "POSTPROCESS",
  "DELIVERY",
]);

export const CreateGenerationRequestSchema = z.object({
  prompt: z.string().min(1).max(4000),
  model: z.string().optional(),
  resolution: z.string().optional(),
  tier: z.enum(["free", "paid"]).optional(),
  seed: z.number().optional(),
  webhook_url: z.string().url().optional(),
});

export const CreateAudioOverviewRequestSchema = z.object({
  topic: z.string().min(1).max(4000),
  tier: z.enum(["free", "paid"]).optional(),
  webhook_url: z.string().url().optional(),
  provider: z.enum(["simulated", "notebooklm"]).optional(),
});

export const AcceptedConfigSchema = z
  .object({
    resolution: z.string().optional(),
    enhancement: z.boolean().optional(),
  })
  .passthrough();

export const GenerationResponseSchema = z.object({
  job_id: z.string(),
  media_id: z.string(),
  status: GenerationStatusSchema,
  stage: GenerationStageSchema.optional(),
  estimated_wait_seconds: z.number().optional(),
  accepted_config: AcceptedConfigSchema.optional(),
  admission: z.record(z.string(), z.unknown()).optional(),
  created_at: z.string().optional(),
  updated_at: z.string().optional(),
  error_code: z.string().optional().nullable(),
  error_message: z.string().optional().nullable(),
  is_ai_generated: z.boolean().optional(),
});

export const GenerationStatusResponseSchema = GenerationResponseSchema.extend({
  progress: z.number().min(0).max(100).optional(),
  stage: z.string().optional(),
});

export const GenerationResultResponseSchema = z.object({
  job_id: z.string(),
  media_id: z.string(),
  status: GenerationStatusSchema,
  image_url: z.string().optional(),
  audio_url: z.string().optional(),
  expires_at: z.string().optional(),
  // TODO: structure variants as objects with metadata once API stabilizes
  variants: z.array(z.string()).optional(),
  is_ai_generated: z.boolean().optional(),
});

// ============= Document Text Schemas =============

export const DocumentTextPageSchema = z.object({
  page: z.number(),
  text: z.string(),
});

export const DocumentTextSchema = z.object({
  mediaId: z.string(),
  pageCount: z.number(),
  extractedAt: z.string(),
  truncated: z.boolean(),
  pages: z.array(DocumentTextPageSchema),
});

// ============= Short URL Schemas =============

export const CreateShortUrlRequestSchema = z.object({
  mediaId: z.string(),
  assetId: z.string(),
  alias: z.string().optional(),
  expiresAt: z.string().datetime().optional(),
  label: z.string().optional(),
});

export const ShortUrlResponseSchema = z
  .object({
    code: z.string(),
    shortUrl: z.string().optional().nullable(),
    mediaId: z.string(),
    assetId: z.string(),
    isPublic: z.boolean().optional(),
    public: z.boolean().optional(),
    createdAt: z.string().optional(),
    createdBy: z.string().optional(),
    expiresAt: z.string().optional().nullable(),
    revokedAt: z.string().optional().nullable(),
    label: z.string().optional(),
  })
  .passthrough();

// ============= Pagination Schemas =============

export const PagedResponseSchema = <T extends z.ZodTypeAny>(itemSchema: T) =>
  z.object({
    items: z.array(itemSchema),
    nextCursor: z.string().nullish(),
    hasMore: z.boolean(),
  });

export const PagedMediaResponseSchema = PagedResponseSchema(MediaSchema);

// ============= Error Schemas =============

export const ApiErrorSchema = z.object({
  message: z.string(),
  status: z.number(),
  requestId: z.string().optional(),
  timestamp: z.string().optional(),
});

// ============= Health Schemas =============

export const HealthStatusSchema = z.enum(["UP", "DOWN", "UNKNOWN"]);

export const BuildInfoSchema = z.object({
  artifact: z.string(),
  name: z.string(),
  version: z.string(),
  time: z.string(),
  group: z.string(),
});

export const VersionInfoSchema = z.object({
  build: BuildInfoSchema.optional(),
});

export const ComponentHealthSchema = z.object({
  status: HealthStatusSchema,
  details: z.record(z.string(), z.unknown()).optional(),
});

export const HealthResponseSchema = z.object({
  status: HealthStatusSchema,
  components: z.record(z.string(), ComponentHealthSchema.optional()).optional(),
});

// ============= Analytics Schemas =============

export const PeriodSchema = z.enum(["TODAY", "THIS_WEEK", "THIS_MONTH", "THIS_YEAR", "ALL_TIME"]);

export const EntityTypeSchema = z.enum(["MEDIA", "THREAD", "COMMENT", "USER"]);

export const EntityViewCountSchema = z.object({
  entityType: EntityTypeSchema,
  entityId: z.string(),
  name: z.string(),
  viewCount: z.number(),
  rank: z.number(),
  deleted: z.boolean().optional().default(false),
  deletedAt: z.string().optional(),
});

export const ViewStatsSchema = z.object({
  entityType: EntityTypeSchema,
  entityId: z.string(),
  total: z.number(),
  today: z.number(),
  thisWeek: z.number(),
  thisMonth: z.number(),
  thisYear: z.number(),
});

export const FormatUsageStatsSchema = z.object({
  period: PeriodSchema,
  usage: z.record(z.string(), z.number()),
  total: z.number(),
});

export const DownloadStatsSchema = z.object({
  period: PeriodSchema,
  totalDownloads: z.number(),
  byFormat: z.record(z.string(), z.number()),
  byDay: z.record(z.string(), z.number()),
});

export const AnalyticsSummarySchema = z.object({
  totalViews: z.number(),
  totalDownloads: z.number(),
  viewsToday: z.number(),
  downloadsToday: z.number(),
  topMediaToday: z.array(EntityViewCountSchema),
  topMediaAllTime: z.array(EntityViewCountSchema),
  formatUsage: z.record(z.string(), z.number()),
});

// ============= Admin DLQ Schemas =============

export const DlqMessageSchema = z.object({
  messageId: z.string(),
  receiptHandle: z.string(),
  body: z.string(),
  attributes: z.record(z.string(), z.string()).optional(),
  sentTimestamp: z.string().optional(),
  approximateReceiveCount: z.number().optional(),
});

export const DlqStatusSchema = z.object({
  configured: z.boolean(),
  queueUrl: z.string().optional(),
  approximateMessageCount: z.number().optional(),
});

// ============= Auth Schemas =============

export const AuthResponseSchema = z.object({
  token: z.string(),
  refreshToken: z.string(),
  tenantId: z.string(),
  userId: z.string(),
  expiresIn: z.number(),
});

export const UserInfoSchema = z.object({
  tenantId: z.string(),
  userId: z.string(),
  email: z.string(),
  roles: z.array(z.string()),
});

export const ApiKeyResponseSchema = z.object({
  keyId: z.string(),
  rawKey: z.string().optional(),
  name: z.string(),
  createdAt: z.string(),
});

// ============= Inferred Types =============

// Media types
export type MediaStatus = z.infer<typeof MediaStatusSchema>;
export type MediaType = z.infer<typeof MediaTypeSchema>;
export type MediaSource = z.infer<typeof MediaSourceSchema>;
export type OutputFormat = z.infer<typeof OutputFormatSchema>;
export type AssetStatus = z.infer<typeof AssetStatusSchema>;
export type AssetType = z.infer<typeof AssetTypeSchema>;
export type AssetOperation = z.infer<typeof AssetOperationSchema>;
export type Media = z.infer<typeof MediaSchema>;
export type InitUploadRequest = z.infer<typeof InitUploadRequestSchema>;
export type InitUploadResponse = z.infer<typeof InitUploadResponseSchema>;
export type UploadResponse = z.infer<typeof UploadResponseSchema>;
export type MediaAsset = z.infer<typeof MediaAssetSchema>;
export type CreateAssetOutput = z.infer<typeof CreateAssetOutputSchema>;
export type CreateAssetRequest = z.infer<typeof CreateAssetRequestSchema>;
export type AssetDownloadUrlResponse = z.infer<typeof AssetDownloadUrlResponseSchema>;
export type GenerationStatus = z.infer<typeof GenerationStatusSchema>;
export type GenerationStage = z.infer<typeof GenerationStageSchema>;
export type CreateGenerationRequest = z.infer<typeof CreateGenerationRequestSchema>;
export type CreateAudioOverviewRequest = z.infer<typeof CreateAudioOverviewRequestSchema>;
export type GenerationResponse = z.infer<typeof GenerationResponseSchema>;
export type GenerationStatusResponse = z.infer<typeof GenerationStatusResponseSchema>;
export type GenerationResultResponse = z.infer<typeof GenerationResultResponseSchema>;
export type AcceptedConfig = z.infer<typeof AcceptedConfigSchema>;
export type DocumentText = z.infer<typeof DocumentTextSchema>;
export type PagedMediaResponse = z.infer<typeof PagedMediaResponseSchema>;
export type ApiError = z.infer<typeof ApiErrorSchema>;
export type CreateShortUrlRequest = z.infer<typeof CreateShortUrlRequestSchema>;
export type ShortUrlResponse = z.infer<typeof ShortUrlResponseSchema>;

// Health types
export type HealthStatus = z.infer<typeof HealthStatusSchema>;
export type BuildInfo = z.infer<typeof BuildInfoSchema>;
export type VersionInfo = z.infer<typeof VersionInfoSchema>;
export type ComponentHealth = z.infer<typeof ComponentHealthSchema>;
export type HealthResponse = z.infer<typeof HealthResponseSchema>;

// Analytics types
export type Period = z.infer<typeof PeriodSchema>;
export type EntityType = z.infer<typeof EntityTypeSchema>;
export type EntityViewCount = z.infer<typeof EntityViewCountSchema>;
export type ViewStats = z.infer<typeof ViewStatsSchema>;
export type FormatUsageStats = z.infer<typeof FormatUsageStatsSchema>;
export type DownloadStats = z.infer<typeof DownloadStatsSchema>;
export type AnalyticsSummary = z.infer<typeof AnalyticsSummarySchema>;

// DLQ types
export type DlqMessage = z.infer<typeof DlqMessageSchema>;
export type DlqStatus = z.infer<typeof DlqStatusSchema>;

// Auth types
export type AuthResponse = z.infer<typeof AuthResponseSchema>;
export type UserInfo = z.infer<typeof UserInfoSchema>;
export type ApiKeyResponse = z.infer<typeof ApiKeyResponseSchema>;
