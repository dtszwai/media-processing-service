/**
 * Media queries and mutations using TanStack Query
 */
import { createQuery, createMutation } from "@tanstack/svelte-query";
import type { Query } from "@tanstack/svelte-query";
import { queryClient, queryKeys } from "../../../shared/queries";
import {
  getAllMedia,
  getMedia,
  getMediaStatus,
  uploadMedia,
  initPresignedUpload,
  uploadToPresignedUrl,
  completePresignedUpload,
  resizeMedia,
  deleteMedia,
  retryProcessing,
  generateIdempotencyKey,
} from "../services";
import { MediaSchema, PagedMediaResponseSchema, StatusResponseSchema } from "../../../shared/types";
import type {
  Media,
  OutputFormat,
  MediaType,
  InitUploadRequest,
  ResizeRequest,
  PagedMediaResponse,
  StatusResponse,
} from "../../../shared/types";
import {
  PRESIGNED_UPLOAD_THRESHOLD,
  MAX_DIRECT_UPLOAD_SIZE,
  MAX_PRESIGNED_UPLOAD_SIZE,
} from "../../../shared/config/env";

// Re-export size constants for components
export { PRESIGNED_UPLOAD_THRESHOLD, MAX_DIRECT_UPLOAD_SIZE, MAX_PRESIGNED_UPLOAD_SIZE };

/**
 * Query for paginated media list
 */
export function createMediaListQuery(cursor?: string, limit?: number, mediaType?: MediaType) {
  return createQuery(() => ({
    queryKey: queryKeys.media.list(cursor, limit, mediaType),
    queryFn: async (): Promise<PagedMediaResponse> => {
      const data = await getAllMedia(cursor, limit, mediaType);
      return PagedMediaResponseSchema.parse(data);
    },
    staleTime: 30 * 1000,
  }));
}

/**
 * Query for single media by ID
 */
export function createMediaQuery(mediaId: string, enabled = true) {
  return createQuery(() => ({
    queryKey: queryKeys.media.detail(mediaId),
    queryFn: async (): Promise<Media> => {
      const data = await getMedia(mediaId);
      return MediaSchema.parse(data);
    },
    enabled,
  }));
}

/**
 * Query for media status with auto-polling while processing
 */
export function createMediaStatusQuery(mediaId: string, enabled = true) {
  return createQuery(() => ({
    queryKey: queryKeys.media.status(mediaId),
    queryFn: async (): Promise<StatusResponse> => {
      const data = await getMediaStatus(mediaId);
      return StatusResponseSchema.parse(data);
    },
    enabled,
    refetchInterval: (query: Query<StatusResponse>) => {
      const status = query.state.data?.status;
      // Keep polling while processing
      if (status === "PENDING" || status === "PROCESSING") {
        return 2000;
      }
      return false;
    },
  }));
}

/**
 * Mutation for direct file upload
 */
export function createUploadMutation() {
  return createMutation(() => ({
    mutationFn: async ({
      file,
      width,
      outputFormat,
      mediaType,
    }: {
      file: File;
      width?: number;
      outputFormat?: OutputFormat;
      mediaType?: MediaType;
    }) => {
      const idempotencyKey = generateIdempotencyKey(file);
      const resolvedFormat =
        mediaType === "image" || !mediaType ? outputFormat || "jpeg" : undefined;
      return uploadMedia(file, width, resolvedFormat, mediaType, idempotencyKey);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.media.all });
    },
  }));
}

/**
 * Mutation for presigned URL upload (large files)
 * Supports optional webhookUrl for processing completion notification
 */
export function createPresignedUploadMutation() {
  return createMutation(() => ({
    mutationFn: async ({
      file,
      width,
      outputFormat,
      mediaType,
      webhookUrl,
      onProgress,
    }: {
      file: File;
      width?: number;
      outputFormat?: OutputFormat;
      mediaType?: MediaType;
      webhookUrl?: string;
      onProgress?: (progress: number) => void;
    }) => {
      const request: InitUploadRequest = {
        fileName: file.name,
        fileSize: file.size,
        contentType: file.type,
        mediaType,
        width,
        outputFormat: mediaType === "image" || !mediaType ? outputFormat || "jpeg" : undefined,
        webhookUrl,
      };

      // Generate idempotency key for retry safety
      const idempotencyKey = generateIdempotencyKey(file);

      // Step 1: Initialize presigned upload with idempotency
      const initResponse = await initPresignedUpload(request, idempotencyKey);

      // Step 2: Upload to presigned URL
      await uploadToPresignedUrl(initResponse.uploadUrl, file, initResponse.headers, onProgress);

      // Step 3: Complete the upload
      return completePresignedUpload(initResponse.mediaId);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.media.all });
    },
  }));
}

/**
 * Mutation for resizing media
 */
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
      queryClient.invalidateQueries({
        queryKey: queryKeys.media.status(variables.mediaId),
      });
      queryClient.invalidateQueries({ queryKey: queryKeys.media.all });
    },
  }));
}

/**
 * Mutation for deleting media
 */
export function createDeleteMutation() {
  return createMutation(() => ({
    mutationFn: async (mediaId: string) => {
      await deleteMedia(mediaId);
      return mediaId;
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.media.all });
      queryClient.invalidateQueries({ queryKey: queryKeys.analytics.all });
    },
  }));
}

/**
 * Mutation for retrying failed processing
 */
export function createRetryMutation() {
  return createMutation(() => ({
    mutationFn: async (mediaId: string) => {
      await retryProcessing(mediaId);
      return mediaId;
    },
    onSuccess: (_result: string, mediaId: string) => {
      queryClient.invalidateQueries({
        queryKey: queryKeys.media.status(mediaId),
      });
      queryClient.invalidateQueries({ queryKey: queryKeys.media.all });
    },
  }));
}
