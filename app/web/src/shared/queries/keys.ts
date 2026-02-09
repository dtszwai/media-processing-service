/**
 * Query keys for consistent cache management
 */
import { queryClient } from "./client";

export const queryKeys = {
  media: {
    all: ["media"] as const,
    list: (cursor?: string, limit?: number, mediaType?: string) =>
      ["media", "list", { cursor, limit, mediaType }] as const,
    detail: (id: string) => ["media", "detail", id] as const,
    assets: (id: string) => ["media", "assets", id] as const,
  },
  health: {
    all: ["health"] as const,
    service: () => ["health", "service"] as const,
    version: () => ["health", "version"] as const,
  },
  analytics: {
    all: ["analytics"] as const,
    summary: () => ["analytics", "summary"] as const,
    topMedia: (period: string, limit?: number) => ["analytics", "topMedia", { period, limit }] as const,
    mediaViews: (id: string) => ["analytics", "mediaViews", id] as const,
    formatUsage: (period: string) => ["analytics", "formatUsage", period] as const,
    downloadStats: (period: string) => ["analytics", "downloadStats", period] as const,
  },
  admin: {
    all: ["admin"] as const,
    dlq: {
      all: ["admin", "dlq"] as const,
      status: () => ["admin", "dlq", "status"] as const,
      messages: (limit?: number) => ["admin", "dlq", "messages", { limit }] as const,
    },
  },
  auth: {
    all: ["auth"] as const,
    user: () => ["auth", "user"] as const,
    apiKeys: () => ["auth", "apiKeys"] as const,
  },
} as const;

/**
 * Invalidation helpers
 */
export function invalidateMediaList() {
  return queryClient.invalidateQueries({ queryKey: queryKeys.media.all });
}

export function invalidateMediaStatus(mediaId: string) {
  return queryClient.invalidateQueries({ queryKey: queryKeys.media.assets(mediaId) });
}

export function invalidateAnalytics() {
  return queryClient.invalidateQueries({ queryKey: queryKeys.analytics.all });
}
