/**
 * Analytics queries using TanStack Query
 */
import { createQuery } from "@tanstack/svelte-query";
import { queryKeys } from "../../../shared/queries";
import { getAnalyticsSummary, getTopMedia, getMediaViews, getFormatUsage, getDownloadStats } from "../services";
import {
  AnalyticsSummarySchema,
  EntityViewCountSchema,
  ViewStatsSchema,
  FormatUsageStatsSchema,
  DownloadStatsSchema,
} from "../../../shared/types";
import type { Period, AnalyticsSummary, EntityViewCount, ViewStats, FormatUsageStats, DownloadStats } from "../../../shared/types";
import { z } from "zod";

/**
 * Query for analytics summary (all key metrics)
 */
export function createAnalyticsSummaryQuery(enabled = true) {
  return createQuery(() => ({
    queryKey: queryKeys.analytics.summary(),
    queryFn: async (): Promise<AnalyticsSummary> => {
      const data = await getAnalyticsSummary();
      return AnalyticsSummarySchema.parse(data);
    },
    enabled,
    staleTime: 30 * 1000,
  }));
}

/**
 * Query for top media by views
 */
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

/**
 * Query for specific media view statistics
 */
export function createMediaViewsQuery(mediaId: string, enabled = true) {
  return createQuery(() => ({
    queryKey: queryKeys.analytics.mediaViews(mediaId),
    queryFn: async (): Promise<ViewStats> => {
      const data = await getMediaViews(mediaId);
      return ViewStatsSchema.parse(data);
    },
    enabled,
  }));
}

/**
 * Query for format usage statistics
 */
export function createFormatUsageQuery(period: Period = "TODAY") {
  return createQuery(() => ({
    queryKey: queryKeys.analytics.formatUsage(period),
    queryFn: async (): Promise<FormatUsageStats> => {
      const data = await getFormatUsage(period);
      return FormatUsageStatsSchema.parse(data);
    },
    staleTime: 30 * 1000,
  }));
}

/**
 * Query for download statistics
 */
export function createDownloadStatsQuery(period: Period = "TODAY") {
  return createQuery(() => ({
    queryKey: queryKeys.analytics.downloadStats(period),
    queryFn: async (): Promise<DownloadStats> => {
      const data = await getDownloadStats(period);
      return DownloadStatsSchema.parse(data);
    },
    staleTime: 30 * 1000,
  }));
}
