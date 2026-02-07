import { describe, it, expect, beforeEach, vi } from "vitest";
import {
  saveAuthTokens,
  getAccessToken,
  getRefreshToken,
  getTokenExpiry,
  clearAuthTokens,
  isTokenExpired,
  shouldRefreshToken,
  saveUserInfo,
  getUserInfo,
} from "./storage";

describe("auth storage", () => {
  beforeEach(() => {
    localStorage.clear();
  });

  describe("saveAuthTokens / getters", () => {
    it("saves and retrieves access token", () => {
      saveAuthTokens("access-123", "refresh-456", 3600);
      expect(getAccessToken()).toBe("access-123");
    });

    it("saves and retrieves refresh token", () => {
      saveAuthTokens("access-123", "refresh-456", 3600);
      expect(getRefreshToken()).toBe("refresh-456");
    });

    it("computes token expiry from expiresIn (seconds)", () => {
      const before = Date.now();
      saveAuthTokens("a", "r", 3600);
      const expiry = getTokenExpiry();
      const after = Date.now();

      expect(expiry).toBeGreaterThanOrEqual(before + 3600 * 1000);
      expect(expiry).toBeLessThanOrEqual(after + 3600 * 1000);
    });
  });

  describe("clearAuthTokens", () => {
    it("removes all auth data from localStorage", () => {
      saveAuthTokens("a", "r", 3600);
      saveUserInfo({ tenantId: "t1", userId: "u1", email: "a@b.com", roles: ["USER"] });

      clearAuthTokens();

      expect(getAccessToken()).toBeNull();
      expect(getRefreshToken()).toBeNull();
      expect(getTokenExpiry()).toBe(0);
      expect(getUserInfo()).toBeNull();
    });
  });

  describe("isTokenExpired", () => {
    it("returns true when no token stored", () => {
      expect(isTokenExpired()).toBe(true);
    });

    it("returns false for a token that expires in the future", () => {
      saveAuthTokens("a", "r", 3600);
      expect(isTokenExpired()).toBe(false);
    });

    it("returns true for an expired token", () => {
      // Set expiry in the past
      vi.spyOn(Date, "now").mockReturnValue(1000);
      saveAuthTokens("a", "r", 1);
      vi.spyOn(Date, "now").mockReturnValue(1000 + 2000);

      expect(isTokenExpired()).toBe(true);
      vi.restoreAllMocks();
    });
  });

  describe("shouldRefreshToken", () => {
    it("returns false when no token stored", () => {
      expect(shouldRefreshToken()).toBe(false);
    });

    it("returns false when token is far from expiry", () => {
      saveAuthTokens("a", "r", 3600); // expires in 1 hour
      expect(shouldRefreshToken()).toBe(false);
    });

    it("returns true when token is within 5 minutes of expiry", () => {
      saveAuthTokens("a", "r", 200); // expires in ~3.3 minutes (< 5 min buffer)
      expect(shouldRefreshToken()).toBe(true);
    });
  });

  describe("saveUserInfo / getUserInfo", () => {
    it("saves and retrieves user info", () => {
      const user = { tenantId: "t1", userId: "u1", email: "admin@example.com", roles: ["ADMIN"] };
      saveUserInfo(user);
      expect(getUserInfo()).toEqual(user);
    });

    it("returns null when no user info stored", () => {
      expect(getUserInfo()).toBeNull();
    });

    it("returns null for corrupted data", () => {
      localStorage.setItem("auth_user_info", "not-json");
      expect(getUserInfo()).toBeNull();
    });
  });
});
