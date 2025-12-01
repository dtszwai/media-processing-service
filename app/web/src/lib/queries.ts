import { createQuery, createMutation } from "@tanstack/svelte-query";
import type { Query } from "@tanstack/svelte-query";
import { queryKeys, queryClient as defaultQueryClient } from "./query";
import {
  getAllMedia,
  getMediaStatus,
  uploadMedia,
  initPresignedUpload,
  uploadToPresignedUrl,
  completePresignedUpload,
  resizeMedia,
  deleteMedia,
  getServiceHealth,
  getVersionInfo,
  getAnalyticsSummary,
  getTopMedia,
  getMediaViews,
  getFormatUsage,
  getDownloadStats,
  PRESIGNED_UPLOAD_THRESHOLD,
} from "./api";
import {
  PagedMediaResponseSchema,
  StatusResponseSchema,
  VersionInfoSchema,
  AnalyticsSummarySchema,
  EntityViewCountSchema,
  ViewStatsSchema,
  FormatUsageStatsSchema,
  DownloadStatsSchema,
} from "./schemas";
import type {
  OutputFormat,
  Period,
  InitUploadRequest,
  ResizeRequest,
  PagedMediaResponse,
  StatusResponse,
  VersionInfo,
  AnalyticsSummary,
  EntityViewCount,
  ViewStats,
  FormatUsageStats,
  DownloadStats,
} from "./schemas";
import type { ServiceHealth } from "./types";
import { z } from "zod";

// Helper to validate response with Zod schema
function validateResponse<T>(schema: z.ZodType<T>, data: unknown): T {
  return schema.parse(data);
}

// ============= Media Queries =============

export function createMediaListQuery(cursor?: string, limit?: number) {
  return createQuery(() => ({
    queryKey: queryKeys.media.list(cursor, limit),
    queryFn: async (): Promise<PagedMediaResponse> => {
      const data = await getAllMedia(cursor, limit);
      return validateResponse(PagedMediaResponseSchema, data);
    },
    staleTime: 30 * 1000,
  }));
}

export function createMediaStatusQuery(mediaId: string, enabled = true) {
  return createQuery(() => ({
    queryKey: queryKeys.media.status(mediaId),
    queryFn: async (): Promise<StatusResponse> => {
      const data = await getMediaStatus(mediaId);
      return validateResponse(StatusResponseSchema, data);
    },
    enabled,
    refetchInterval: (query: Query<StatusResponse>) => {
      const status = query.state.data?.status;
      // Keep polling while processing
      if (status === "PENDING" || status === "PROCESSING" || status === "DELETING") {
        return 2000;
      }
      return false;
    },
  }));
}

// ============= Media Mutations =============

export function createUploadMutation() {
  return createMutation(() => ({
    mutationFn: async ({
      file,
      width,
      outputFormat = "jpeg",
    }: {
      file: File;
      width: number;
      outputFormat?: OutputFormat;
    }) => {
      return uploadMedia(file, width, outputFormat);
    },
    onSuccess: () => {
      defaultQueryClient.invalidateQueries({ queryKey: queryKeys.media.all });
    },
  }));
}

export function createPresignedUploadMutation() {
  return createMutation(() => ({
    mutationFn: async ({
      file,
      width,
      outputFormat = "jpeg",
      onProgress,
    }: {
      file: File;
      width: number;
      outputFormat?: OutputFormat;
      onProgress?: (progress: number) => void;
    }) => {
      const request: InitUploadRequest = {
        fileName: file.name,
        fileSize: file.size,
        contentType: file.type,
        width,
        outputFormat,
      };

      // Step 1: Initialize presigned upload
      const initResponse = await initPresignedUpload(request);

      // Step 2: Upload to presigned URL
      await uploadToPresignedUrl(initResponse.uploadUrl, file, initResponse.headers, onProgress);

      // Step 3: Complete the upload
      return completePresignedUpload(initResponse.mediaId);
    },
    onSuccess: () => {
      defaultQueryClient.invalidateQueries({ queryKey: queryKeys.media.all });
    },
  }));
}

export function createResizeMutation() {
  return createMutation(() => ({
    mutationFn: async ({ mediaId, request }: { mediaId: string; request: ResizeRequest }) => {
      await resizeMedia(mediaId, request);
      return { mediaId, ...request };
    },
    onSuccess: (
      _result: { mediaId: string; width: number; outputFormat?: OutputFormat },
      variables: { mediaId: string; request: ResizeRequest },
    ) => {
      defaultQueryClient.invalidateQueries({
        queryKey: queryKeys.media.status(variables.mediaId),
      });
      defaultQueryClient.invalidateQueries({ queryKey: queryKeys.media.all });
    },
  }));
}

export function createDeleteMutation() {
  return createMutation(() => ({
    mutationFn: async (mediaId: string) => {
      await deleteMedia(mediaId);
      return mediaId;
    },
    onSuccess: () => {
      defaultQueryClient.invalidateQueries({ queryKey: queryKeys.media.all });
    },
  }));
}

// ============= Health Queries =============

export function createServiceHealthQuery() {
  return createQuery(() => ({
    queryKey: queryKeys.health.service(),
    queryFn: async (): Promise<ServiceHealth> => {
      return getServiceHealth();
    },
    staleTime: 10 * 1000,
    refetchInterval: 30 * 1000,
  }));
}

export function createVersionInfoQuery() {
  return createQuery(() => ({
    queryKey: queryKeys.health.version(),
    queryFn: async (): Promise<VersionInfo | null> => {
      const data = await getVersionInfo();
      if (!data) return null;
      return validateResponse(VersionInfoSchema, data);
    },
    staleTime: 5 * 60 * 1000, // Version rarely changes
  }));
}

// ============= Analytics Queries =============

export function createAnalyticsSummaryQuery(enabled = true) {
  return createQuery(() => ({
    queryKey: queryKeys.analytics.summary(),
    queryFn: async (): Promise<AnalyticsSummary> => {
      const data = await getAnalyticsSummary();
      return validateResponse(AnalyticsSummarySchema, data);
    },
    enabled,
    staleTime: 30 * 1000,
  }));
}

export function createTopMediaQuery(period: Period = "TODAY", limit = 10) {
  return createQuery(() => ({
    queryKey: queryKeys.analytics.topMedia(period, limit),
    queryFn: async (): Promise<EntityViewCount[]> => {
      const data = await getTopMedia(period, limit);
      return z.array(EntityViewCountSchema).parse(data);
    },
    staleTime: 30 * 1000,
  }));
}

export function createMediaViewsQuery(mediaId: string, enabled = true) {
  return createQuery(() => ({
    queryKey: queryKeys.analytics.mediaViews(mediaId),
    queryFn: async (): Promise<ViewStats> => {
      const data = await getMediaViews(mediaId);
      return validateResponse(ViewStatsSchema, data);
    },
    enabled,
  }));
}

export function createFormatUsageQuery(period: Period = "TODAY") {
  return createQuery(() => ({
    queryKey: queryKeys.analytics.formatUsage(period),
    queryFn: async (): Promise<FormatUsageStats> => {
      const data = await getFormatUsage(period);
      return validateResponse(FormatUsageStatsSchema, data);
    },
    staleTime: 30 * 1000,
  }));
}

export function createDownloadStatsQuery(period: Period = "TODAY") {
  return createQuery(() => ({
    queryKey: queryKeys.analytics.downloadStats(period),
    queryFn: async (): Promise<DownloadStats> => {
      const data = await getDownloadStats(period);
      return validateResponse(DownloadStatsSchema, data);
    },
    staleTime: 30 * 1000,
  }));
}

// Export the threshold for components
export { PRESIGNED_UPLOAD_THRESHOLD };
