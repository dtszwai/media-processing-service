import { z } from "zod";

// Enums
export const MediaStatusSchema = z.enum(["PENDING_UPLOAD", "PENDING", "PROCESSING", "COMPLETE", "ERROR", "DELETING"]);

export const OutputFormatSchema = z.enum(["jpeg", "png", "webp"]);

export const HealthStatusSchema = z.enum(["UP", "DOWN", "UNKNOWN"]);

export const PeriodSchema = z.enum(["TODAY", "THIS_WEEK", "THIS_MONTH", "ALL_TIME"]);

// Media
export const MediaSchema = z.object({
  mediaId: z.string(),
  name: z.string(),
  size: z.number(),
  mimetype: z.string(),
  status: MediaStatusSchema,
  width: z.number(),
  outputFormat: OutputFormatSchema.optional(),
  createdAt: z.string().optional(),
  updatedAt: z.string().optional(),
});

// Upload
export const InitUploadRequestSchema = z.object({
  fileName: z.string(),
  fileSize: z.number(),
  contentType: z.string(),
  width: z.number().optional(),
  outputFormat: OutputFormatSchema.optional(),
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

// Pagination
export const PagedResponseSchema = <T extends z.ZodTypeAny>(itemSchema: T) =>
  z.object({
    items: z.array(itemSchema),
    nextCursor: z.string().nullish(),
    hasMore: z.boolean(),
  });

export const PagedMediaResponseSchema = PagedResponseSchema(MediaSchema);

// API Error
export const ApiErrorSchema = z.object({
  message: z.string(),
  status: z.number(),
  requestId: z.string().optional(),
  timestamp: z.string().optional(),
});

// Health
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

// Analytics
export const MediaViewCountSchema = z.object({
  mediaId: z.string(),
  name: z.string(),
  viewCount: z.number(),
  rank: z.number(),
});

export const ViewStatsSchema = z.object({
  mediaId: z.string(),
  total: z.number(),
  today: z.number(),
  thisWeek: z.number(),
  thisMonth: z.number(),
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
  topMediaToday: z.array(MediaViewCountSchema),
  topMediaAllTime: z.array(MediaViewCountSchema),
  formatUsage: z.record(z.string(), z.number()),
});

// Type exports (inferred from schemas)
export type Media = z.infer<typeof MediaSchema>;
export type MediaStatus = z.infer<typeof MediaStatusSchema>;
export type OutputFormat = z.infer<typeof OutputFormatSchema>;
export type InitUploadRequest = z.infer<typeof InitUploadRequestSchema>;
export type InitUploadResponse = z.infer<typeof InitUploadResponseSchema>;
export type StatusResponse = z.infer<typeof StatusResponseSchema>;
export type UploadResponse = z.infer<typeof UploadResponseSchema>;
export type ResizeRequest = z.infer<typeof ResizeRequestSchema>;
export type PagedMediaResponse = z.infer<typeof PagedMediaResponseSchema>;
export type HealthStatus = z.infer<typeof HealthStatusSchema>;
export type BuildInfo = z.infer<typeof BuildInfoSchema>;
export type VersionInfo = z.infer<typeof VersionInfoSchema>;
export type ComponentHealth = z.infer<typeof ComponentHealthSchema>;
export type HealthResponse = z.infer<typeof HealthResponseSchema>;
export type Period = z.infer<typeof PeriodSchema>;
export type MediaViewCount = z.infer<typeof MediaViewCountSchema>;
export type ViewStats = z.infer<typeof ViewStatsSchema>;
export type FormatUsageStats = z.infer<typeof FormatUsageStatsSchema>;
export type DownloadStats = z.infer<typeof DownloadStatsSchema>;
export type AnalyticsSummary = z.infer<typeof AnalyticsSummarySchema>;
