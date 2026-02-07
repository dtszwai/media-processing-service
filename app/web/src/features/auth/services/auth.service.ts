/**
 * Auth API service
 * Handles registration, login, token refresh, and API key management
 */
import { AUTH_BASE } from "../../../shared/config/env";
import { handleResponse, authenticatedFetch } from "../../../shared/http";
import { AuthResponseSchema, UserInfoSchema } from "../../../shared/types";
import type { AuthResponse, UserInfo, ApiKeyResponse } from "../../../shared/types";

export { refreshAuthToken } from "../../../shared/auth/token";

export interface LoginRequest {
  email: string;
  password: string;
}

export interface RegisterRequest {
  tenantName: string;
  email: string;
  password: string;
}

export interface CreateApiKeyRequest {
  name: string;
}

/**
 * Register a new tenant and admin user
 */
export async function register(request: RegisterRequest): Promise<AuthResponse> {
  const response = await fetch(`${AUTH_BASE}/register`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });
  const data = await handleResponse<AuthResponse>(response);
  return AuthResponseSchema.parse(data);
}

/**
 * Login with email and password
 */
export async function login(request: LoginRequest): Promise<AuthResponse> {
  const response = await fetch(`${AUTH_BASE}/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });
  const data = await handleResponse<AuthResponse>(response);
  return AuthResponseSchema.parse(data);
}

/**
 * Get current user info (requires auth)
 */
export async function getCurrentUser(token?: string): Promise<UserInfo> {
  const headers: Record<string, string> = {};
  if (token) {
    headers["Authorization"] = `Bearer ${token}`;
  }

  const response = token
    ? await fetch(`${AUTH_BASE}/me`, { headers })
    : await authenticatedFetch(`${AUTH_BASE}/me`);

  const data = await handleResponse<UserInfo>(response);
  return UserInfoSchema.parse(data);
}

/**
 * Create a new API key
 */
export async function createApiKey(request: CreateApiKeyRequest): Promise<ApiKeyResponse> {
  const response = await authenticatedFetch(`${AUTH_BASE}/api-keys`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });
  return handleResponse<ApiKeyResponse>(response);
}

/**
 * List all API keys for the current tenant
 */
export async function listApiKeys(): Promise<ApiKeyResponse[]> {
  const response = await authenticatedFetch(`${AUTH_BASE}/api-keys`);
  return handleResponse<ApiKeyResponse[]>(response);
}

/**
 * Delete (revoke) an API key
 */
export async function deleteApiKey(keyId: string): Promise<void> {
  const response = await authenticatedFetch(`${AUTH_BASE}/api-keys/${keyId}`, {
    method: "DELETE",
  });
  if (!response.ok) {
    await handleResponse(response);
  }
}
