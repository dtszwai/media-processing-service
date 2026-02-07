import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { login, register, getCurrentUser, createApiKey, listApiKeys, deleteApiKey } from "./auth.service";
import { AuthenticationError } from "../../../shared/types";

describe("auth.service", () => {
  const mockFetch = vi.fn();

  beforeEach(() => {
    vi.stubGlobal("fetch", mockFetch);
    localStorage.clear();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe("login", () => {
    it("returns auth response on success", async () => {
      const mockResponse = {
        token: "jwt-token",
        refreshToken: "refresh-token",
        tenantId: "t1",
        userId: "u1",
        expiresIn: 3600,
      };
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: () => Promise.resolve(mockResponse),
        headers: new Headers(),
      });

      const result = await login({ email: "admin@example.com", password: "secret123" });

      expect(result).toEqual(mockResponse);
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining("/v1/auth/login"),
        expect.objectContaining({ method: "POST" }),
      );
    });

    it("throws AuthenticationError on 401", async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 401,
        json: () => Promise.resolve({ message: "Invalid credentials", status: 401 }),
        headers: new Headers(),
      });

      await expect(login({ email: "bad@email.com", password: "wrong" })).rejects.toThrow(AuthenticationError);
    });
  });

  describe("register", () => {
    it("returns auth response on success", async () => {
      const mockResponse = {
        token: "jwt-token",
        refreshToken: "refresh-token",
        tenantId: "t1",
        userId: "u1",
        expiresIn: 3600,
      };
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 201,
        json: () => Promise.resolve(mockResponse),
        headers: new Headers(),
      });

      const result = await register({
        tenantName: "Example",
        email: "admin@example.com",
        password: "secret123",
      });

      expect(result).toEqual(mockResponse);
    });
  });

  describe("getCurrentUser", () => {
    it("returns user info with token", async () => {
      const mockUser = {
        tenantId: "t1",
        userId: "u1",
        email: "admin@example.com",
        roles: ["ADMIN"],
      };
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: () => Promise.resolve(mockUser),
        headers: new Headers(),
      });

      const result = await getCurrentUser("jwt-token");

      expect(result).toEqual(mockUser);
      expect(mockFetch).toHaveBeenCalledWith(
        expect.stringContaining("/v1/auth/me"),
        expect.objectContaining({
          headers: { Authorization: "Bearer jwt-token" },
        }),
      );
    });
  });

  describe("createApiKey", () => {
    it("returns api key response", async () => {
      // Set up auth tokens so authenticatedFetch works
      localStorage.setItem("auth_access_token", "jwt-token");
      localStorage.setItem("auth_token_expiry", String(Date.now() + 3600 * 1000));

      const mockKey = {
        keyId: "key-123",
        rawKey: "mps_abc123...",
        name: "CI Pipeline",
        createdAt: "2024-01-01T00:00:00Z",
      };
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 201,
        json: () => Promise.resolve(mockKey),
        headers: new Headers(),
      });

      const result = await createApiKey({ name: "CI Pipeline" });

      expect(result).toEqual(mockKey);
    });
  });

  describe("listApiKeys", () => {
    it("returns list of api keys", async () => {
      localStorage.setItem("auth_access_token", "jwt-token");
      localStorage.setItem("auth_token_expiry", String(Date.now() + 3600 * 1000));

      const mockKeys = [
        { keyId: "key-1", name: "Key 1", createdAt: "2024-01-01T00:00:00Z" },
        { keyId: "key-2", name: "Key 2", createdAt: "2024-01-02T00:00:00Z" },
      ];
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: () => Promise.resolve(mockKeys),
        headers: new Headers(),
      });

      const result = await listApiKeys();

      expect(result).toEqual(mockKeys);
    });
  });

  describe("deleteApiKey", () => {
    it("completes without error on success", async () => {
      localStorage.setItem("auth_access_token", "jwt-token");
      localStorage.setItem("auth_token_expiry", String(Date.now() + 3600 * 1000));

      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 204,
        headers: new Headers(),
      });

      await expect(deleteApiKey("key-123")).resolves.toBeUndefined();
    });
  });
});
