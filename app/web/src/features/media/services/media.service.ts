/**
 * Media API service
 * Handles all media-related API calls
 */
import { API_BASE } from "../../../shared/config/env";
import { handleResponse, authenticatedFetch, uploadToPresignedUrl } from "../../../shared/http";
import { RateLimitError, ApiRequestError, AuthenticationError } from "../../../shared/types";
import type {
  Media,
  MediaAsset,
  InitUploadRequest,
  InitUploadResponse,
  UploadResponse,
  MediaType,
  MediaSource,
  PagedResponse,
  CreateAssetRequest,
  AssetDownloadUrlResponse,
} from "../../../shared/types";

export { uploadToPresignedUrl };

/**
 * Get all media with optional pagination
 */
export async function getAllMedia(
  cursor?: string,
  limit?: number,
  mediaType?: MediaType,
  source?: MediaSource,
): Promise<PagedResponse<Media>> {
  const params = new URLSearchParams();
  if (cursor) params.set("cursor", cursor);
  if (limit) params.set("limit", limit.toString());
  if (mediaType) params.set("mediaType", mediaType);
  if (source) params.set("source", source);

  const url = params.toString() ? `${API_BASE}?${params}` : API_BASE;
  const response = await authenticatedFetch(url);
  return handleResponse<PagedResponse<Media>>(response);
}

/**
 * Get media by ID
 */
export async function getMedia(mediaId: string): Promise<Media> {
  const response = await authenticatedFetch(`${API_BASE}/${mediaId}`);
  if (response.status === 404) {
    throw new Error("NOT_FOUND");
  }
  if (response.status === 410) {
    throw new Error("DELETED");
  }
  return handleResponse<Media>(response);
}

/**
 * List assets for a media item
 */
export async function listAssets(mediaId: string): Promise<MediaAsset[]> {
  const response = await authenticatedFetch(`${API_BASE}/${mediaId}/assets`);
  return handleResponse<MediaAsset[]>(response);
}

/**
 * Get asset by ID
 */
export async function getAsset(mediaId: string, assetId: string): Promise<MediaAsset> {
  const response = await authenticatedFetch(`${API_BASE}/${mediaId}/assets/${assetId}`);
  if (response.status === 404) {
    throw new Error("NOT_FOUND");
  }
  return handleResponse<MediaAsset>(response);
}

/**
 * Create derived assets
 */
export async function createAssets(mediaId: string, request: CreateAssetRequest): Promise<MediaAsset[]> {
  const response = await authenticatedFetch(`${API_BASE}/${mediaId}/assets`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });
  return handleResponse<MediaAsset[]>(response);
}

/**
 * Retry processing for a failed asset
 */
export async function retryAsset(mediaId: string, assetId: string): Promise<MediaAsset> {
  const response = await authenticatedFetch(`${API_BASE}/${mediaId}/assets/${assetId}/retry`, {
    method: "POST",
  });
  return handleResponse<MediaAsset>(response);
}

/**
 * Upload media file directly (for files < 5MB)
 */
export async function uploadMedia(
  file: File,
  mediaType?: MediaType,
  idempotencyKey?: string,
): Promise<UploadResponse> {
  const formData = new FormData();
  formData.append("file", file);
  if (mediaType) {
    formData.append("mediaType", mediaType);
  }

  const headers: Record<string, string> = {};
  if (idempotencyKey) {
    headers["Idempotency-Key"] = idempotencyKey;
  }

  const response = await authenticatedFetch(`${API_BASE}/upload`, {
    method: "POST",
    headers,
    body: formData,
  });

  return handleResponse<UploadResponse>(response);
}

/**
 * Initialize presigned upload (for large files)
 */
export async function initPresignedUpload(
  request: InitUploadRequest,
  idempotencyKey?: string,
): Promise<InitUploadResponse> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  if (idempotencyKey) {
    headers["Idempotency-Key"] = idempotencyKey;
  }

  const response = await authenticatedFetch(`${API_BASE}/upload/init`, {
    method: "POST",
    headers,
    body: JSON.stringify(request),
  });

  return handleResponse<InitUploadResponse>(response);
}

/**
 * Refresh presigned upload URL (if previous expired)
 */
export async function refreshPresignedUploadUrl(mediaId: string): Promise<InitUploadResponse> {
  const response = await authenticatedFetch(`${API_BASE}/${mediaId}/upload/refresh`, {
    method: "POST",
  });

  return handleResponse<InitUploadResponse>(response);
}

/**
 * Complete presigned upload after file is uploaded to S3
 */
export async function completePresignedUpload(mediaId: string): Promise<UploadResponse> {
  const response = await authenticatedFetch(`${API_BASE}/${mediaId}/upload/complete`, {
    method: "POST",
  });

  return handleResponse<UploadResponse>(response);
}

/**
 * Delete media by ID
 */
export async function deleteMedia(mediaId: string): Promise<void> {
  const response = await authenticatedFetch(`${API_BASE}/${mediaId}`, {
    method: "DELETE",
  });

  if (response.status === 401) {
    throw new AuthenticationError();
  }

  if (response.status === 429) {
    const retryAfter = parseInt(response.headers.get("X-Rate-Limit-Retry-After-Seconds") || "60", 10);
    throw new RateLimitError(retryAfter);
  }

  if (!response.ok) {
    throw new ApiRequestError("Delete failed", response.status);
  }
}

/**
 * Get download URL for a media asset
 */
export function getAssetDownloadUrl(mediaId: string, assetId: string): string {
  return `${API_BASE}/${mediaId}/assets/${assetId}/download`;
}

async function fetchAssetUrl(
  mediaId: string,
  assetId: string,
  kind: "download" | "preview",
): Promise<string | null> {
  const response = await authenticatedFetch(`${API_BASE}/${mediaId}/assets/${assetId}/${kind}-url`);
  if (response.status === 202) {
    return null;
  }
  if (response.status === 404) {
    throw new Error("NOT_FOUND");
  }
  const payload = await handleResponse<AssetDownloadUrlResponse>(response);
  return payload.url;
}

export function fetchAssetDownloadUrl(mediaId: string, assetId: string): Promise<string | null> {
  return fetchAssetUrl(mediaId, assetId, "download");
}

export function fetchAssetPreviewUrl(mediaId: string, assetId: string): Promise<string | null> {
  return fetchAssetUrl(mediaId, assetId, "preview");
}

/**
 * Generate idempotency key for upload requests
 */
export function generateIdempotencyKey(file: File): string {
  const timestamp = Date.now();
  const data = `${file.name}-${file.size}-${timestamp}`;
  // Simple hash function for browser compatibility
  let hash = 0;
  for (let i = 0; i < data.length; i++) {
    const char = data.charCodeAt(i);
    hash = ((hash << 5) - hash) + char;
    hash = hash & hash;
  }
  return `idem-${Math.abs(hash).toString(36)}-${timestamp.toString(36)}`;
}

/**
 * Poll for asset status until it reaches target statuses
 */
export async function pollForAssetStatus(
  mediaId: string,
  assetId: string,
  targetStatuses: string[],
  onStatusChange?: (status: string) => void,
  interval = 2000,
): Promise<string> {
  let nextDelay = interval;
  while (true) {
    try {
      const assets = await listAssets(mediaId);
      const asset = assets.find((item) => item.assetId === assetId);
      if (!asset) {
        throw new Error("Asset not found");
      }

      const status = asset.status;
      onStatusChange?.(status);

      if (targetStatuses.includes(status)) {
        return status;
      }

      if (status === "ERROR") {
        throw new Error("Processing failed");
      }

      await wait(nextDelay);
      nextDelay = Math.min(nextDelay + 500, 8000);
    } catch (error) {
      if (error instanceof RateLimitError) {
        const retryAfterMs = Math.max(1000, error.retryAfterSeconds * 1000);
        await wait(retryAfterMs);
        nextDelay = Math.max(nextDelay, retryAfterMs);
        continue;
      }
      throw error;
    }
  }
}

/**
 * Poll until all assets are COMPLETE or any ERROR.
 * Uses a single list-assets call per cycle to avoid per-asset polling fanout.
 */
export async function pollForAssets(
  mediaId: string,
  assetIds: string[],
  interval = 2000,
): Promise<void> {
  if (assetIds.length === 0) return;

  let nextDelay = interval;
  while (true) {
    try {
      const assets = await listAssets(mediaId);
      const assetMap = new Map(assets.map((asset) => [asset.assetId, asset]));
      const statuses = assetIds.map((id) => assetMap.get(id)?.status ?? "PENDING");

      if (statuses.every((status) => status === "COMPLETE")) {
        return;
      }
      if (statuses.some((status) => status === "ERROR")) {
        throw new Error("Processing failed");
      }

      const inFlightCount = statuses.filter(
        (status) => status === "PENDING" || status === "PENDING_UPLOAD" || status === "PROCESSING",
      ).length;
      const baseDelay = inFlightCount > 3 ? Math.max(interval, 3500) : interval;
      await wait(nextDelay);
      nextDelay = Math.min(Math.max(baseDelay, nextDelay + 500), 8000);
    } catch (error) {
      if (error instanceof RateLimitError) {
        const retryAfterMs = Math.max(1000, error.retryAfterSeconds * 1000);
        await wait(retryAfterMs);
        nextDelay = Math.max(nextDelay, retryAfterMs);
        continue;
      }
      throw error;
    }
  }
}

function wait(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
