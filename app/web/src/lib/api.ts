import type {
  Media,
  InitUploadRequest,
  InitUploadResponse,
  StatusResponse,
  UploadResponse,
  ResizeRequest,
  OutputFormat,
  ApiError,
  HealthResponse,
  ServiceHealth,
  PagedResponse,
  VersionInfo,
  Period,
  EntityViewCount,
  ViewStats,
  FormatUsageStats,
  DownloadStats,
  AnalyticsSummary,
} from "./types";
import { RateLimitError, ApiRequestError } from "./types";

const API_BASE = "/api";
const ACTUATOR_BASE = "/actuator";

// Threshold for using presigned URL upload (5MB)
export const PRESIGNED_UPLOAD_THRESHOLD = 5 * 1024 * 1024;

async function handleResponse<T>(response: Response): Promise<T> {
  const requestId = response.headers.get("X-Request-ID") || undefined;

  if (response.status === 429) {
    const retryAfter = parseInt(response.headers.get("X-Rate-Limit-Retry-After-Seconds") || "60", 10);
    throw new RateLimitError(retryAfter, requestId);
  }

  if (!response.ok) {
    let error: ApiError;
    try {
      error = await response.json();
    } catch {
      error = { message: "Request failed", status: response.status };
    }
    throw new ApiRequestError(error.message || "Request failed", response.status, error.requestId || requestId);
  }

  return response.json();
}

export async function checkHealth(): Promise<boolean> {
  try {
    const response = await fetch(`${API_BASE}/health`);
    return response.ok;
  } catch {
    return false;
  }
}

export async function getVersionInfo(): Promise<VersionInfo | null> {
  try {
    const response = await fetch(`${ACTUATOR_BASE}/info`);
    if (!response.ok) return null;
    return response.json();
  } catch {
    return null;
  }
}

export async function getServiceHealth(): Promise<ServiceHealth> {
  const defaultHealth: ServiceHealth = {
    overall: "DOWN",
    services: {
      api: false,
      s3: "UNKNOWN",
      dynamoDb: "UNKNOWN",
      sns: "UNKNOWN",
    },
  };

  try {
    // First check if API is reachable
    const apiHealthy = await checkHealth();
    if (!apiHealthy) {
      return defaultHealth;
    }

    // Then check readiness (includes S3, DynamoDB, SNS)
    const response = await fetch(`${ACTUATOR_BASE}/health/readiness`);
    if (!response.ok) {
      return {
        ...defaultHealth,
        services: { ...defaultHealth.services, api: true },
      };
    }

    const health: HealthResponse = await response.json();

    return {
      overall: health.status,
      services: {
        api: true,
        s3: health.components?.s3?.status ?? "UNKNOWN",
        dynamoDb: health.components?.dynamoDb?.status ?? "UNKNOWN",
        sns: health.components?.sns?.status ?? "UNKNOWN",
      },
    };
  } catch {
    return defaultHealth;
  }
}

export async function getAllMedia(cursor?: string, limit?: number): Promise<PagedResponse<Media>> {
  const params = new URLSearchParams();
  if (cursor) params.set("cursor", cursor);
  if (limit) params.set("limit", limit.toString());

  const url = params.toString() ? `${API_BASE}?${params}` : API_BASE;
  const response = await fetch(url);
  return handleResponse<PagedResponse<Media>>(response);
}

export async function getMediaStatus(mediaId: string): Promise<StatusResponse> {
  const response = await fetch(`${API_BASE}/${mediaId}/status`);
  if (response.status === 404) {
    throw new Error("NOT_FOUND");
  }
  return handleResponse<StatusResponse>(response);
}

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

export async function initPresignedUpload(request: InitUploadRequest): Promise<InitUploadResponse> {
  const response = await fetch(`${API_BASE}/upload/init`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });

  return handleResponse<InitUploadResponse>(response);
}

export async function uploadToPresignedUrl(
  url: string,
  file: File,
  headers: Record<string, string>,
  onProgress?: (progress: number) => void,
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("PUT", url);

    Object.entries(headers).forEach(([key, value]) => {
      xhr.setRequestHeader(key, value);
    });

    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable && onProgress) {
        const progress = (event.loaded / event.total) * 100;
        onProgress(progress);
      }
    };

    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve();
      } else {
        reject(new Error(`Upload failed with status ${xhr.status}`));
      }
    };

    xhr.onerror = () => reject(new Error("Upload failed"));
    xhr.send(file);
  });
}

export async function completePresignedUpload(mediaId: string): Promise<UploadResponse> {
  const response = await fetch(`${API_BASE}/${mediaId}/upload/complete`, {
    method: "POST",
  });

  return handleResponse<UploadResponse>(response);
}

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

export function getDownloadUrl(mediaId: string): string {
  return `${API_BASE}/${mediaId}/download`;
}

export function getOriginalUrl(mediaId: string, fileName: string): string {
  // S3 key format: {mediaId}/original.{ext}
  const extension = fileName.includes(".") ? fileName.substring(fileName.lastIndexOf(".")) : "";
  return `http://127.0.0.1:4566/media-bucket/${mediaId}/original${extension}`;
}

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
      if (error instanceof Error && error.message === "NOT_FOUND") {
        return "DELETED";
      }
      throw error;
    }

    await new Promise((resolve) => setTimeout(resolve, interval));
  }
}

// Analytics API functions
const ANALYTICS_BASE = "/api/analytics";

export async function getTopMedia(period: Period = "TODAY", limit = 10): Promise<EntityViewCount[]> {
  const params = new URLSearchParams();
  params.set("period", period);
  params.set("limit", limit.toString());

  const response = await fetch(`${ANALYTICS_BASE}/top-media?${params}`);
  return handleResponse<EntityViewCount[]>(response);
}

export async function getMediaViews(mediaId: string): Promise<ViewStats> {
  const response = await fetch(`${ANALYTICS_BASE}/media/${mediaId}/views`);
  return handleResponse<ViewStats>(response);
}

export async function getFormatUsage(period: Period = "TODAY"): Promise<FormatUsageStats> {
  const response = await fetch(`${ANALYTICS_BASE}/formats?period=${period}`);
  return handleResponse<FormatUsageStats>(response);
}

export async function getDownloadStats(period: Period = "TODAY"): Promise<DownloadStats> {
  const response = await fetch(`${ANALYTICS_BASE}/downloads?period=${period}`);
  return handleResponse<DownloadStats>(response);
}

export async function getAnalyticsSummary(): Promise<AnalyticsSummary> {
  const response = await fetch(`${ANALYTICS_BASE}/summary`);
  return handleResponse<AnalyticsSummary>(response);
}
