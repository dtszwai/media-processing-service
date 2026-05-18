// Set VITE_API_URL to point the console at a non-default API host; the
// compose stack defaults to :9000.
export const API_URL = import.meta.env.VITE_API_URL || "http://localhost:9000";
export const GRAFANA_URL = import.meta.env.VITE_GRAFANA_URL || "http://localhost:3000";

export const MAX_DIRECT_UPLOAD_SIZE = 50 * 1024 * 1024;
export const MAX_PRESIGNED_UPLOAD_SIZE = 1024 * 1024 * 1024;
export const PRESIGNED_UPLOAD_THRESHOLD = 5 * 1024 * 1024;
