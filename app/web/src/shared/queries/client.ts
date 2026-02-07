/**
 * TanStack Query client configuration
 */
import { QueryClient } from "@tanstack/svelte-query";
import { AuthenticationError } from "../types";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30 * 1000, // 30 seconds
      gcTime: 5 * 60 * 1000, // 5 minutes (formerly cacheTime)
      retry: (failureCount, error) => {
        // Never retry auth errors
        if (error instanceof AuthenticationError) return false;
        return failureCount < 1;
      },
      refetchOnWindowFocus: true,
    },
    mutations: {
      retry: false,
    },
  },
});
