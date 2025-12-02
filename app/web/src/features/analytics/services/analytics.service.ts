/**
 * Analytics API service
 * Handles all analytics-related API calls
 */
import { ANALYTICS_BASE } from "../../../shared/config/env";
import { handleResponse } from "../../../shared/http";
import type { Period, EntityViewCount, ViewStats, FormatUsageStats, DownloadStats, AnalyticsSummary } from "../../../shared/types";

/**
 * Get top media by views for a given period
 */
export async function getTopMedia(period: Period = "TODAY", limit = 10): Promise<EntityViewCount[]> {
  const params = new URLSearchParams();
  params.set("period", period);
  params.set("limit", limit.toString());

  const response = await fetch(`${ANALYTICS_BASE}/top-media?${params}`);
  return handleResponse<EntityViewCount[]>(response);
}

/**
 * Get view statistics for a specific media
 */
export async function getMediaViews(mediaId: string): Promise<ViewStats> {
  const response = await fetch(`${ANALYTICS_BASE}/media/${mediaId}/views`);
  return handleResponse<ViewStats>(response);
}

/**
 * Get format usage statistics
 */
export async function getFormatUsage(period: Period = "TODAY"): Promise<FormatUsageStats> {
  const response = await fetch(`${ANALYTICS_BASE}/formats?period=${period}`);
  return handleResponse<FormatUsageStats>(response);
}

/**
 * Get download statistics
 */
export async function getDownloadStats(period: Period = "TODAY"): Promise<DownloadStats> {
  const response = await fetch(`${ANALYTICS_BASE}/downloads?period=${period}`);
  return handleResponse<DownloadStats>(response);
}

/**
 * Get analytics summary (all key metrics)
 */
export async function getAnalyticsSummary(): Promise<AnalyticsSummary> {
  const response = await fetch(`${ANALYTICS_BASE}/summary`);
  return handleResponse<AnalyticsSummary>(response);
}
