import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { retryProcessing } from "./api";
import { ApiRequestError, RateLimitError } from "./types";

describe("retryProcessing", () => {
  const mockFetch = vi.fn();

  beforeEach(() => {
    vi.stubGlobal("fetch", mockFetch);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("calls retry endpoint and returns mediaId on success", async () => {
    const mockResponse = { mediaId: "test-media-id" };
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () => Promise.resolve(mockResponse),
      headers: new Headers(),
    });

    const result = await retryProcessing("test-media-id");

    expect(mockFetch).toHaveBeenCalledWith("/api/test-media-id/retry", {
      method: "POST",
    });
    expect(result).toEqual(mockResponse);
  });

  it("throws RateLimitError on 429 response", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 429,
      headers: new Headers({
        "X-Rate-Limit-Retry-After-Seconds": "30",
      }),
    });

    await expect(retryProcessing("test-media-id")).rejects.toThrow(RateLimitError);
  });

  it("throws ApiRequestError on 409 conflict response", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 409,
      json: () =>
        Promise.resolve({
          message: "Cannot retry: media is not in PROCESSING or ERROR status",
          status: 409,
        }),
      headers: new Headers(),
    });

    await expect(retryProcessing("test-media-id")).rejects.toThrow(ApiRequestError);
  });

  it("throws ApiRequestError on 404 not found response", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 404,
      json: () =>
        Promise.resolve({
          message: "Not found",
          status: 404,
        }),
      headers: new Headers(),
    });

    await expect(retryProcessing("test-media-id")).rejects.toThrow(ApiRequestError);
  });
});
