import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { fetchAssetDownloadUrl } from "./media.service";
import { RateLimitError } from "../../../shared/types";

describe("fetchAssetDownloadUrl", () => {
  const mockFetch = vi.fn();

  beforeEach(() => {
    vi.stubGlobal("fetch", mockFetch);
    localStorage.setItem("auth_access_token", "test-token");
    localStorage.setItem("auth_token_expiry", String(Date.now() + 3600 * 1000));
  });

  afterEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
  });

  it("returns url when available", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: () => Promise.resolve({ url: "https://s3.example.com/file" }),
      headers: new Headers(),
    });

    const result = await fetchAssetDownloadUrl("media-123", "asset-123");

    expect(mockFetch).toHaveBeenCalledWith(
      "http://localhost:9000/v1/media/media-123/assets/asset-123/download-url",
      expect.any(Object),
    );
    expect(result).toEqual("https://s3.example.com/file");
  });

  it("returns null when asset is still processing", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      status: 202,
      headers: new Headers(),
      json: () => Promise.resolve({}),
    });

    const result = await fetchAssetDownloadUrl("media-123", "asset-123");

    expect(result).toBeNull();
  });

  it("throws RateLimitError on 429 response", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 429,
      headers: new Headers({
        "X-Rate-Limit-Retry-After-Seconds": "30",
      }),
      json: () => Promise.resolve({ message: "Rate limited", status: 429 }),
    });

    await expect(fetchAssetDownloadUrl("media-123", "asset-123")).rejects.toThrow(RateLimitError);
  });
});
