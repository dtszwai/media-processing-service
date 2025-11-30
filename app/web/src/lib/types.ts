export type MediaStatus = "PENDING_UPLOAD" | "PENDING" | "PROCESSING" | "COMPLETE" | "ERROR" | "DELETING";

export type OutputFormat = "jpeg" | "png" | "webp";

export interface Media {
  mediaId: string;
  name: string;
  size: number;
  mimetype: string;
  status: MediaStatus;
  width: number;
  outputFormat?: OutputFormat;
  createdAt?: string;
  updatedAt?: string;
}

export interface InitUploadRequest {
  fileName: string;
  fileSize: number;
  contentType: string;
  width?: number;
  outputFormat?: OutputFormat;
}

export interface InitUploadResponse {
  mediaId: string;
  uploadUrl: string;
  expiresIn: number;
  method: string;
  headers: Record<string, string>;
}

export interface StatusResponse {
  status: MediaStatus;
}

export interface UploadResponse {
  mediaId: string;
}

export interface ResizeRequest {
  width: number;
  outputFormat?: OutputFormat;
}

export interface PagedResponse<T> {
  items: T[];
  nextCursor: string | null;
  hasMore: boolean;
}

export interface ApiError {
  message: string;
  status: number;
  requestId?: string;
  timestamp?: string;
}

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

export type HealthStatus = "UP" | "DOWN" | "UNKNOWN";

export interface BuildInfo {
  artifact: string;
  name: string;
  version: string;
  time: string;
  group: string;
}

export interface VersionInfo {
  build?: BuildInfo;
}

export interface ComponentHealth {
  status: HealthStatus;
  details?: Record<string, unknown>;
}

export interface HealthResponse {
  status: HealthStatus;
  components?: {
    s3?: ComponentHealth;
    dynamoDb?: ComponentHealth;
    sns?: ComponentHealth;
    [key: string]: ComponentHealth | undefined;
  };
}

export interface ServiceHealth {
  overall: HealthStatus;
  services: {
    api: boolean;
    s3: HealthStatus;
    dynamoDb: HealthStatus;
    sns: HealthStatus;
  };
}
