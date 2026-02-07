/**
 * Token refresh utility
 * Isolated from auth.service.ts to avoid circular dependency with client.ts
 */
import { AUTH_BASE } from "../config/env";
import { AuthResponseSchema } from "../types";
import { getRefreshToken, saveAuthTokens, clearAuthTokens } from "./storage";

/**
 * Refresh the access token using the stored refresh token.
 * Uses raw fetch() to avoid circular dependency with authenticatedFetch.
 * Returns true if refresh succeeded, false otherwise.
 */
export async function refreshAuthToken(): Promise<boolean> {
  const refreshToken = getRefreshToken();
  if (!refreshToken) {
    clearAuthTokens();
    return false;
  }

  try {
    const response = await fetch(`${AUTH_BASE}/refresh`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refreshToken }),
    });

    if (!response.ok) {
      clearAuthTokens();
      return false;
    }

    const data = AuthResponseSchema.parse(await response.json());
    saveAuthTokens(data.token, data.refreshToken, data.expiresIn);
    return true;
  } catch {
    clearAuthTokens();
    return false;
  }
}
