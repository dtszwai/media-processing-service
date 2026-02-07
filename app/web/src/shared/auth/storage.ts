/**
 * Token storage utilities
 * Persists auth tokens in localStorage
 */
import type { UserInfo } from "../types";

const KEYS = {
  ACCESS_TOKEN: "auth_access_token",
  REFRESH_TOKEN: "auth_refresh_token",
  TOKEN_EXPIRY: "auth_token_expiry",
  USER_INFO: "auth_user_info",
} as const;

const REFRESH_BUFFER_MS = 5 * 60 * 1000; // 5 minutes

export function saveAuthTokens(accessToken: string, refreshToken: string, expiresIn: number): void {
  localStorage.setItem(KEYS.ACCESS_TOKEN, accessToken);
  localStorage.setItem(KEYS.REFRESH_TOKEN, refreshToken);
  localStorage.setItem(KEYS.TOKEN_EXPIRY, String(Date.now() + expiresIn * 1000));
}

export function getAccessToken(): string | null {
  return localStorage.getItem(KEYS.ACCESS_TOKEN);
}

export function getRefreshToken(): string | null {
  return localStorage.getItem(KEYS.REFRESH_TOKEN);
}

export function getTokenExpiry(): number {
  const expiry = localStorage.getItem(KEYS.TOKEN_EXPIRY);
  return expiry ? parseInt(expiry, 10) : 0;
}

export function clearAuthTokens(): void {
  localStorage.removeItem(KEYS.ACCESS_TOKEN);
  localStorage.removeItem(KEYS.REFRESH_TOKEN);
  localStorage.removeItem(KEYS.TOKEN_EXPIRY);
  localStorage.removeItem(KEYS.USER_INFO);
}

export function isTokenExpired(): boolean {
  return Date.now() >= getTokenExpiry();
}

export function shouldRefreshToken(): boolean {
  const expiry = getTokenExpiry();
  return expiry > 0 && Date.now() >= expiry - REFRESH_BUFFER_MS;
}

export function saveUserInfo(userInfo: UserInfo): void {
  localStorage.setItem(KEYS.USER_INFO, JSON.stringify(userInfo));
}

export function getUserInfo(): UserInfo | null {
  const raw = localStorage.getItem(KEYS.USER_INFO);
  if (!raw) return null;
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}
