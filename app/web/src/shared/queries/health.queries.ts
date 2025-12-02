/**
 * Health queries using TanStack Query
 */
import { createQuery } from "@tanstack/svelte-query";
import { queryKeys } from "./keys";
import { getServiceHealth, getVersionInfo } from "../http/health.service";
import { VersionInfoSchema } from "../types";
import type { ServiceHealth, VersionInfo } from "../types";

/**
 * Query for service health status
 */
export function createServiceHealthQuery() {
  return createQuery(() => ({
    queryKey: queryKeys.health.service(),
    queryFn: async (): Promise<ServiceHealth> => {
      return getServiceHealth();
    },
    staleTime: 10 * 1000,
    refetchInterval: 30 * 1000,
  }));
}

/**
 * Query for application version info
 */
export function createVersionInfoQuery() {
  return createQuery(() => ({
    queryKey: queryKeys.health.version(),
    queryFn: async (): Promise<VersionInfo | null> => {
      const data = await getVersionInfo();
      if (!data) return null;
      return VersionInfoSchema.parse(data);
    },
    staleTime: 5 * 60 * 1000, // Version rarely changes
  }));
}
