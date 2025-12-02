/**
 * Media API service
 * Handles all media-related API calls
 */
import { API_BASE } from "../../../shared/config/env";
import { handleResponse, uploadToPresignedUrl } from "../../../shared/http";
import { RateLimitError, ApiRequestError } from "../../../shared/types";
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
  const response = await fetch(url);
  return handleResponse<PagedResponse<Media>>(response);
}

/**
 * Get media status by ID
 * @throws Error with message "NOT_FOUND" if media doesn't exist
 * @throws Error with message "DELETED" if media was deleted
 */
export async function getMediaStatus(mediaId: string): Promise<StatusResponse> {
  const response = await fetch(`${API_BASE}/${mediaId}/status`);
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
 */
export async function uploadMedia(
  file: File,
  width: number,
  outputFormat: OutputFormat = "jpeg",
): Promise<UploadResponse> {
  const formData = new FormData();
  formData.append("file", file);
  formData.append("width", width.toString());
  formData.append("outputFormat", outputFormat);

  const response = await fetch(`${API_BASE}/upload`, {
    method: "POST",
    body: formData,
  });

  return handleResponse<UploadResponse>(response);
}

/**
 * Initialize presigned upload (for large files)
 */
export async function initPresignedUpload(request: InitUploadRequest): Promise<InitUploadResponse> {
  const response = await fetch(`${API_BASE}/upload/init`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });

  return handleResponse<InitUploadResponse>(response);
}

/**
 * Refresh presigned upload URL (if previous expired)
 */
export async function refreshPresignedUploadUrl(mediaId: string): Promise<InitUploadResponse> {
  const response = await fetch(`${API_BASE}/${mediaId}/upload/refresh`, {
    method: "POST",
  });

  return handleResponse<InitUploadResponse>(response);
}

/**
 * Complete presigned upload after file is uploaded to S3
 */
export async function completePresignedUpload(mediaId: string): Promise<UploadResponse> {
  const response = await fetch(`${API_BASE}/${mediaId}/upload/complete`, {
    method: "POST",
  });

  return handleResponse<UploadResponse>(response);
}

/**
 * Resize existing media
 */
export async function resizeMedia(mediaId: string, request: ResizeRequest): Promise<void> {
  const response = await fetch(`${API_BASE}/${mediaId}/resize`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });

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
  const response = await fetch(`${API_BASE}/${mediaId}`, {
    method: "DELETE",
  });

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
  const response = await fetch(`${API_BASE}/${mediaId}/retry`, {
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
 * Get direct S3 URL for original file (LocalStack only)
 */
export function getOriginalUrl(mediaId: string, fileName: string): string {
  const extension = fileName.includes(".") ? fileName.substring(fileName.lastIndexOf(".")) : "";
  return `http://127.0.0.1:4566/media-bucket/${mediaId}/original${extension}`;
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
