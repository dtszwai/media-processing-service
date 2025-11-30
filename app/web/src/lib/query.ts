import { QueryClient } from "@tanstack/svelte-query";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30 * 1000, // 30 seconds
      gcTime: 5 * 60 * 1000, // 5 minutes (formerly cacheTime)
      retry: 1,
      refetchOnWindowFocus: true,
    },
  },
});

// Query keys for consistent cache management
export const queryKeys = {
  media: {
    all: ["media"] as const,
    list: (cursor?: string, limit?: number) => ["media", "list", { cursor, limit }] as const,
    detail: (id: string) => ["media", "detail", id] as const,
    status: (id: string) => ["media", "status", id] as const,
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
} as const;

// Invalidation helpers
export function invalidateMediaList() {
  return queryClient.invalidateQueries({ queryKey: queryKeys.media.all });
}

export function invalidateMediaStatus(mediaId: string) {
  return queryClient.invalidateQueries({ queryKey: queryKeys.media.status(mediaId) });
}

export function invalidateAnalytics() {
  return queryClient.invalidateQueries({ queryKey: queryKeys.analytics.all });
}
