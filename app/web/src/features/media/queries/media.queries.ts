/**
 * Media queries and mutations using TanStack Query
 */
import { createQuery, createMutation } from "@tanstack/svelte-query";
import { queryClient, queryKeys } from "../../../shared/queries";
import {
  getAllMedia,
  getMedia,
  listAssets,
  createAssets,
  retryAsset,
  uploadMedia,
  initPresignedUpload,
  uploadToPresignedUrl,
  completePresignedUpload,
  deleteMedia,
  generateIdempotencyKey,
} from "../services";
import {
  MediaSchema,
  MediaAssetSchema,
  PagedMediaResponseSchema,
} from "../../../shared/types";
import type {
  Media,
  MediaAsset,
  MediaType,
  InitUploadRequest,
  PagedMediaResponse,
  CreateAssetRequest,
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
 * Query for assets of a media item
 */
export function createMediaAssetsQuery(mediaId: string, enabled = true) {
  return createQuery(() => ({
    queryKey: queryKeys.media.assets(mediaId),
    queryFn: async (): Promise<MediaAsset[]> => {
      const data = await listAssets(mediaId);
      return MediaAssetSchema.array().parse(data);
    },
    enabled,
    refetchInterval: (query) => {
      const assets = query.state.data || [];
      const isProcessing = assets.some(
        (asset) => asset.status === "PENDING" || asset.status === "PROCESSING",
      );
      return isProcessing ? 2000 : false;
    },
  }));
}

/**
 * Mutation for direct file upload
 */
export function createUploadMutation() {
  return createMutation(() => ({
    mutationFn: async ({ file, mediaType }: { file: File; mediaType?: MediaType }) => {
      const idempotencyKey = generateIdempotencyKey(file);
      return uploadMedia(file, mediaType, idempotencyKey);
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
      mediaType,
      webhookUrl,
      onProgress,
    }: {
      file: File;
      mediaType?: MediaType;
      webhookUrl?: string;
      onProgress?: (progress: number) => void;
    }) => {
      const request: InitUploadRequest = {
        fileName: file.name,
        fileSize: file.size,
        contentType: file.type,
        mediaType,
        webhookUrl,
      };

      const idempotencyKey = generateIdempotencyKey(file);
      const initResponse = await initPresignedUpload(request, idempotencyKey);
      await uploadToPresignedUrl(initResponse.uploadUrl, file, initResponse.headers, onProgress);
      return completePresignedUpload(initResponse.mediaId);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.media.all });
    },
  }));
}

/**
 * Mutation for creating assets
 */
export function createAssetsMutation() {
  return createMutation(() => ({
    mutationFn: async ({ mediaId, request }: { mediaId: string; request: CreateAssetRequest }) => {
      return createAssets(mediaId, request);
    },
    onSuccess: (_result, variables) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.media.list() });
      queryClient.invalidateQueries({ queryKey: queryKeys.media.all });
      queryClient.invalidateQueries({ queryKey: queryKeys.media.assets(variables.mediaId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.media.detail(variables.mediaId) });
    },
  }));
}

/**
 * Mutation for retrying a failed asset
 */
export function createAssetRetryMutation() {
  return createMutation(() => ({
    mutationFn: async ({ mediaId, assetId }: { mediaId: string; assetId: string }) => {
      return retryAsset(mediaId, assetId);
    },
    onSuccess: (_result, variables) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.media.list() });
      queryClient.invalidateQueries({ queryKey: queryKeys.media.all });
      queryClient.invalidateQueries({ queryKey: queryKeys.media.assets(variables.mediaId) });
      queryClient.invalidateQueries({ queryKey: queryKeys.media.detail(variables.mediaId) });
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
