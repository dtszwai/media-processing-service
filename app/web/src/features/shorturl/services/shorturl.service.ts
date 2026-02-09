/**
 * Short URL API service
 * Handles short URL management calls
 */
import { SHORT_URL_BASE } from "../../../shared/config/env";
import { authenticatedFetch, handleResponse } from "../../../shared/http";
import type { CreateShortUrlRequest, ShortUrlResponse } from "../../../shared/types";

export async function createShortUrl(request: CreateShortUrlRequest): Promise<ShortUrlResponse> {
  const response = await authenticatedFetch(SHORT_URL_BASE, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });
  return handleResponse<ShortUrlResponse>(response);
}

export async function listShortUrls(mediaId: string, limit?: number): Promise<ShortUrlResponse[]> {
  const params = new URLSearchParams({ mediaId });
  if (limit) params.set("limit", limit.toString());
  const response = await authenticatedFetch(`${SHORT_URL_BASE}?${params.toString()}`);
  return handleResponse<ShortUrlResponse[]>(response);
}

export async function revokeShortUrl(code: string): Promise<void> {
  const response = await authenticatedFetch(`${SHORT_URL_BASE}/${code}`, { method: "DELETE" });
  if (response.status === 204) {
    return;
  }
  await handleResponse<unknown>(response);
}
