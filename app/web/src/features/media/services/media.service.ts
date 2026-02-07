/**
 * Media API service
 * Handles all media-related API calls
 */
import { API_BASE } from "../../../shared/config/env";
import { handleResponse, authenticatedFetch, uploadToPresignedUrl } from "../../../shared/http";
import { RateLimitError, ApiRequestError, AuthenticationError } from "../../../shared/types";
import type {
  Media,
  InitUploadRequest,
  InitUploadResponse,
  StatusResponse,
  UploadResponse,
  ResizeRequest,
  OutputFormat,
  PagedResponse,
} from "../../../shared/types";

export { uploadToPresignedUrl };

/**
 * Get all media with optional pagination
 */
export async function getAllMedia(cursor?: string, limit?: number): Promise<PagedResponse<Media>> {
  const params = new URLSearchParams();
  if (cursor) params.set("cursor", cursor);
  if (limit) params.set("limit", limit.toString());

  const url = params.toString() ? `${API_BASE}?${params}` : API_BASE;
  const response = await authenticatedFetch(url);
  return handleResponse<PagedResponse<Media>>(response);
}

/**
 * Get media by ID
 * @throws Error with message "NOT_FOUND" if media doesn't exist
 * @throws Error with message "DELETED" if media was deleted
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
 * Get media status by ID
 * @throws Error with message "NOT_FOUND" if media doesn't exist
 * @throws Error with message "DELETED" if media was deleted
 */
export async function getMediaStatus(mediaId: string): Promise<StatusResponse> {
  const response = await authenticatedFetch(`${API_BASE}/${mediaId}/status`);
  if (response.status === 404) {
    throw new Error("NOT_FOUND");
  }
  if (response.status === 410) {
    throw new Error("DELETED");
  }
  return handleResponse<StatusResponse>(response);
}

/**
 * Upload media file directly (for files < 5MB)
 * @param file - File to upload
 * @param width - Target width
 * @param outputFormat - Output format
 * @param idempotencyKey - Optional idempotency key for retry safety
 */
export async function uploadMedia(
  file: File,
  width: number,
  outputFormat: OutputFormat = "jpeg",
  idempotencyKey?: string,
): Promise<UploadResponse> {
  const formData = new FormData();
  formData.append("file", file);
  formData.append("width", width.toString());
  formData.append("outputFormat", outputFormat);

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
 * @param request - Upload initialization request
 * @param idempotencyKey - Optional idempotency key for retry safety
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
 * Resize existing media
 */
export async function resizeMedia(mediaId: string, request: ResizeRequest): Promise<void> {
  const response = await authenticatedFetch(`${API_BASE}/${mediaId}/resize`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });

  if (response.status === 401) {
    throw new AuthenticationError();
  }

  if (response.status === 429) {
    const retryAfter = parseInt(response.headers.get("X-Rate-Limit-Retry-After-Seconds") || "60", 10);
    throw new RateLimitError(retryAfter);
  }

  if (!response.ok) {
    const error = await response.json();
    throw new ApiRequestError(error.message || "Resize failed", response.status, error.requestId);
  }
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
 * Retry processing for failed media
 */
export async function retryProcessing(mediaId: string): Promise<UploadResponse> {
  const response = await authenticatedFetch(`${API_BASE}/${mediaId}/retry`, {
    method: "POST",
  });

  return handleResponse<UploadResponse>(response);
}

/**
 * Get download URL for processed media
 */
export function getDownloadUrl(mediaId: string): string {
  return `${API_BASE}/${mediaId}/download`;
}

/**
 * Get original file URL (redirects to presigned S3 URL)
 */
export function getOriginalUrl(mediaId: string): string {
  return `${API_BASE}/${mediaId}/original`;
}

/**
 * Get preview URL for processed media (via CDN in production)
 */
export function getPreviewUrl(mediaId: string): string {
  return `${API_BASE}/${mediaId}/preview`;
}

/**
 * Generate idempotency key for upload requests
 * Uses file name, size, and timestamp to create unique key
 */
export function generateIdempotencyKey(file: File): string {
  const timestamp = Date.now();
  const data = `${file.name}-${file.size}-${timestamp}`;
  // Simple hash function for browser compatibility
  let hash = 0;
  for (let i = 0; i < data.length; i++) {
    const char = data.charCodeAt(i);
    hash = ((hash << 5) - hash) + char;
    hash = hash & hash; // Convert to 32-bit integer
  }
  return `idem-${Math.abs(hash).toString(36)}-${timestamp.toString(36)}`;
}

/**
 * Poll for media status until it reaches target status
 */
export async function pollForStatus(
  mediaId: string,
  targetStatuses: string[],
  onStatusChange?: (status: string) => void,
  interval = 2000,
): Promise<string> {
  while (true) {
    try {
      const { status } = await getMediaStatus(mediaId);
      onStatusChange?.(status);

      if (targetStatuses.includes(status)) {
        return status;
      }

      if (status === "ERROR") {
        throw new Error("Processing failed");
      }
    } catch (error) {
      if (error instanceof Error && (error.message === "NOT_FOUND" || error.message === "DELETED")) {
        return "DELETED";
      }
      throw error;
    }

    await new Promise((resolve) => setTimeout(resolve, interval));
  }
}
