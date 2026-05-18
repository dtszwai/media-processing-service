// Asset preview data layer.
//
// Given a (tenantId, mediaId) tuple, resolve the primary asset under
// `{tenantId}/{mediaId}/assets/` and produce a short-lived presigned
// URL. Composed from the existing ops endpoints (`listS3` +
// `presignDownload`) so the operator console doesn't need a dedicated
// backend RPC yet.
//
// A small in-memory cache de-duplicates concurrent resolutions for the
// same media and reuses presigned URLs within their effective lifetime.
// Presigned URLs out of LocalStack default to ~15 min; we cap at 10 to
// leave headroom for clock skew + paint latency before the asset
// finally renders.

import { create } from "@bufbuild/protobuf";
import {
  ListS3RequestSchema,
  PresignDownloadRequestSchema,
} from "@media-service/api-client/gen/mediaservice/ops/v1/ops_pb.js";
import { opsClient } from "./ops";

export type AssetKind = "image" | "audio" | "other";

export type Asset = {
  key: string;
  url: string;
  kind: AssetKind;
  contentType: string;
  sizeBytes: number;
};

type CacheEntry = { promise: Promise<Asset | null>; ts: number };

const CACHE_TTL_MS = 10 * 60 * 1000;
const cache = new Map<string, CacheEntry>();

export function previewKey(tenantId: string, mediaId: string): string {
  return `${tenantId}/${mediaId}`;
}

export async function fetchPreview(
  tenantId: string,
  mediaId: string,
): Promise<Asset | null> {
  if (!tenantId || !mediaId) return null;
  const k = previewKey(tenantId, mediaId);
  const cached = cache.get(k);
  if (cached && Date.now() - cached.ts < CACHE_TTL_MS) {
    return cached.promise;
  }
  const promise = resolve(tenantId, mediaId);
  cache.set(k, { promise, ts: Date.now() });
  // If resolution fails or returns null, invalidate so a later retry
  // can run a fresh attempt rather than reusing a stale failure.
  promise.then(
    (a) => {
      if (a === null) cache.delete(k);
    },
    () => {
      cache.delete(k);
    },
  );
  return promise;
}

export function invalidatePreview(tenantId: string, mediaId: string): void {
  cache.delete(previewKey(tenantId, mediaId));
}

async function resolve(tenantId: string, mediaId: string): Promise<Asset | null> {
  const list = await opsClient.listS3(
    create(ListS3RequestSchema, {
      prefix: `${tenantId}/${mediaId}/assets/`,
      limit: 10,
    }),
  );
  // Pick the first non-prefix node. If multiple assets exist (image +
  // thumbnail variants) the caller can switch to a richer selector
  // later; for v1 the first one is the primary output.
  const first = list.nodes.find((n) => !n.isPrefix);
  if (!first) return null;
  const presigned = await opsClient.presignDownload(
    create(PresignDownloadRequestSchema, { key: first.key }),
  );
  if (!presigned.url) return null;
  const contentType = contentTypeFromKey(first.key);
  return {
    key: first.key,
    url: presigned.url,
    kind: kindFromContentType(contentType),
    contentType,
    sizeBytes: Number(first.sizeBytes),
  };
}

export function contentTypeFromKey(key: string): string {
  const ext = key.toLowerCase().split(".").pop() ?? "";
  switch (ext) {
    case "png":
    case "webp":
    case "gif":
    case "avif":
    case "bmp":
      return `image/${ext}`;
    case "jpg":
    case "jpeg":
      return "image/jpeg";
    case "svg":
      return "image/svg+xml";
    case "mp3":
      return "audio/mpeg";
    case "wav":
      return "audio/wav";
    case "ogg":
    case "opus":
      return "audio/ogg";
    case "m4a":
    case "aac":
      return "audio/mp4";
    case "flac":
      return "audio/flac";
    default:
      return "application/octet-stream";
  }
}

export function kindFromContentType(ct: string): AssetKind {
  if (ct.startsWith("image/")) return "image";
  if (ct.startsWith("audio/")) return "audio";
  return "other";
}

export function kindFromKey(key: string): AssetKind {
  return kindFromContentType(contentTypeFromKey(key));
}

// Resolve a direct S3 key (used by S3Panel where we already know the
// canonical key and don't want to round-trip through listS3).
export async function presignKey(key: string): Promise<Asset | null> {
  if (!key) return null;
  const cached = cache.get(`key:${key}`);
  if (cached && Date.now() - cached.ts < CACHE_TTL_MS) return cached.promise;
  const promise = (async () => {
    const res = await opsClient.presignDownload(
      create(PresignDownloadRequestSchema, { key }),
    );
    if (!res.url) return null;
    const contentType = contentTypeFromKey(key);
    return {
      key,
      url: res.url,
      kind: kindFromContentType(contentType),
      contentType,
      sizeBytes: 0,
    } satisfies Asset;
  })();
  cache.set(`key:${key}`, { promise, ts: Date.now() });
  promise.then(
    (a) => { if (a === null) cache.delete(`key:${key}`); },
    () => { cache.delete(`key:${key}`); },
  );
  return promise;
}
