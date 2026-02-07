/**
 * Custom error classes for API errors
 */

/**
 * Rate limit exceeded error
 * Includes retry-after information
 */
export class RateLimitError extends Error {
  retryAfterSeconds: number;
  requestId?: string;

  constructor(retryAfterSeconds: number, requestId?: string) {
    super(`Rate limit exceeded. Retry after ${retryAfterSeconds} seconds.`);
    this.name = "RateLimitError";
    this.retryAfterSeconds = retryAfterSeconds;
    this.requestId = requestId;
  }
}

/**
 * API request error
 * Includes status code and optional request ID for debugging
 */
export class ApiRequestError extends Error {
  status: number;
  requestId?: string;

  constructor(message: string, status: number, requestId?: string) {
    super(message);
    this.name = "ApiRequestError";
    this.status = status;
    this.requestId = requestId;
  }
}

/**
 * Authentication error
 * Thrown when a request fails due to missing or invalid credentials
 */
export class AuthenticationError extends Error {
  status: number;

  constructor(message = "Authentication required", status = 401) {
    super(message);
    this.name = "AuthenticationError";
    this.status = status;
  }
}
