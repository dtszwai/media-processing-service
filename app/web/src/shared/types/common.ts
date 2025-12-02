/**
 * Common types used across the application
 */
import type { HealthStatus } from "./schemas";

/**
 * Generic paginated response
 */
export interface PagedResponse<T> {
  items: T[];
  nextCursor: string | null;
  hasMore: boolean;
}

/**
 * Service health status for all backend services
 */
export interface ServiceHealth {
  overall: HealthStatus;
  services: {
    api: boolean;
    s3: HealthStatus;
    dynamoDb: HealthStatus;
    sns: HealthStatus;
    redis?: HealthStatus;
  };
}

/**
 * Application view types
 */
export type AppView = "upload" | "analytics";
