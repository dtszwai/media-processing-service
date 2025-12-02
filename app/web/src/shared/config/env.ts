/**
 * Environment configuration
 *
 * API_URL is configurable via environment variable:
 * - Development: defaults to http://localhost:9000
 * - Production: set VITE_API_URL to your backend URL
 */
export const API_URL = import.meta.env.VITE_API_URL || "http://localhost:9000";

// API endpoint bases
export const API_BASE = `${API_URL}/v1/media`;
export const ANALYTICS_BASE = `${API_URL}/v1/analytics`;
export const ACTUATOR_BASE = `${API_URL}/actuator`;

// File size limits
export const MAX_DIRECT_UPLOAD_SIZE = 50 * 1024 * 1024; // 50MB for direct upload
export const MAX_PRESIGNED_UPLOAD_SIZE = 1024 * 1024 * 1024; // 1GB for presigned upload
export const PRESIGNED_UPLOAD_THRESHOLD = 5 * 1024 * 1024; // Use presigned for files > 5MB
