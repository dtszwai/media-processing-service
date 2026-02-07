/**
 * HTTP client utilities for API requests
 */
import type { ApiError } from "../types";
import { RateLimitError, ApiRequestError, AuthenticationError } from "../types";
import { getAccessToken, shouldRefreshToken } from "../auth/storage";
import { refreshAuthToken } from "../auth/token";

/**
 * Handle API response and parse JSON
 * Throws appropriate errors for rate limiting, auth, and API errors
 */
export async function handleResponse<T>(response: Response): Promise<T> {
  const requestId = response.headers.get("X-Request-ID") || undefined;

  if (response.status === 401) {
    throw new AuthenticationError("Authentication required", 401);
  }

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

/**
 * Fetch with automatic auth header injection and proactive token refresh.
 * Use this for all authenticated API calls.
 */
export async function authenticatedFetch(url: string, options: RequestInit = {}): Promise<Response> {
  if (shouldRefreshToken()) {
    await refreshAuthToken();
  }

  const token = getAccessToken();
  const headers = new Headers(options.headers);
  if (token) {
    headers.set("Authorization", `Bearer ${token}`);
  }

  return fetch(url, { ...options, headers });
}

/**
 * Upload file to presigned URL with progress tracking
 */
export function uploadToPresignedUrl(
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
