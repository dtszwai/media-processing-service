/**
 * Health API service
 * Handles health check and version info API calls
 */
import { API_BASE, ACTUATOR_BASE } from "../config/env";
import type { HealthResponse, ServiceHealth, VersionInfo } from "../types";

/**
 * Check if API is healthy (basic health check)
 */
export async function checkHealth(): Promise<boolean> {
  try {
    const response = await fetch(`${API_BASE}/health`);
    return response.ok;
  } catch {
    return false;
  }
}

/**
 * Get application version info
 */
export async function getVersionInfo(): Promise<VersionInfo | null> {
  try {
    const response = await fetch(`${ACTUATOR_BASE}/info`);
    if (!response.ok) return null;
    return response.json();
  } catch {
    return null;
  }
}

/**
 * Get detailed service health (API, S3, DynamoDB, SNS)
 */
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
