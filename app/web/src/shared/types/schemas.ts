/**
 * Zod schemas for API response validation
 * Types are inferred from schemas for type safety
 */
import { z } from "zod";

// ============= Media Schemas =============

export const MediaStatusSchema = z.enum(["PENDING_UPLOAD", "PENDING", "PROCESSING", "COMPLETE", "ERROR", "DELETED"]);

export const MediaTypeSchema = z.enum(["image", "document", "video", "audio", "other"]);

export const OutputFormatSchema = z.enum(["jpeg", "png", "webp"]);

export const MediaSchema = z.object({
  mediaId: z.string(),
  name: z.string(),
  size: z.number(),
  mimetype: z.string(),
  mediaType: MediaTypeSchema.optional(),
  status: MediaStatusSchema,
  width: z.number().optional(),
  outputFormat: OutputFormatSchema.optional(),
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
});

export const InitUploadRequestSchema = z.object({
  fileName: z.string(),
  fileSize: z.number(),
  contentType: z.string(),
  mediaType: MediaTypeSchema.optional(),
  width: z.number().optional(),
  outputFormat: OutputFormatSchema.optional(),
  webhookUrl: z.string().url().optional(),
});

export const InitUploadResponseSchema = z.object({
  mediaId: z.string(),
  uploadUrl: z.string(),
  expiresIn: z.number(),
  method: z.string(),
  headers: z.record(z.string(), z.string()),
});

export const StatusResponseSchema = z.object({
  status: MediaStatusSchema,
});

export const UploadResponseSchema = z.object({
  mediaId: z.string(),
});

export const ResizeRequestSchema = z.object({
  width: z.number(),
  outputFormat: OutputFormatSchema.optional(),
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

export const ShortUrlVariantSchema = z.enum(["preview", "download", "original"]);

export const CreateShortUrlRequestSchema = z.object({
  mediaId: z.string(),
  variant: ShortUrlVariantSchema,
  alias: z.string().optional(),
  expiresAt: z.string().datetime().optional(),
  label: z.string().optional(),
});

export const ShortUrlResponseSchema = z
  .object({
    code: z.string(),
    shortUrl: z.string().optional().nullable(),
    mediaId: z.string(),
    variant: ShortUrlVariantSchema,
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
export type OutputFormat = z.infer<typeof OutputFormatSchema>;
export type Media = z.infer<typeof MediaSchema>;
export type InitUploadRequest = z.infer<typeof InitUploadRequestSchema>;
export type InitUploadResponse = z.infer<typeof InitUploadResponseSchema>;
export type StatusResponse = z.infer<typeof StatusResponseSchema>;
export type UploadResponse = z.infer<typeof UploadResponseSchema>;
export type ResizeRequest = z.infer<typeof ResizeRequestSchema>;
export type DocumentText = z.infer<typeof DocumentTextSchema>;
export type PagedMediaResponse = z.infer<typeof PagedMediaResponseSchema>;
export type ApiError = z.infer<typeof ApiErrorSchema>;
export type ShortUrlVariant = z.infer<typeof ShortUrlVariantSchema>;
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
