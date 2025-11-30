import { writable } from "svelte/store";
import type { Media, OutputFormat, ServiceHealth, VersionInfo } from "./types";

export type AppView = "upload" | "analytics";

export const mediaList = writable<Media[]>([]);
export const currentMediaId = writable<string | null>(null);
export const isProcessing = writable(false);
export const apiConnected = writable(false);
export const versionInfo = writable<VersionInfo | null>(null);
export const currentView = writable<AppView>("upload");
export const serviceHealth = writable<ServiceHealth>({
  overall: "UNKNOWN",
  services: {
    api: false,
    s3: "UNKNOWN",
    dynamoDb: "UNKNOWN",
    sns: "UNKNOWN",
  },
});

export function updateMediaStatus(mediaId: string, status: string) {
  mediaList.update((list) =>
    list.map((m) => (m.mediaId === mediaId ? { ...m, status: status as Media["status"] } : m)),
  );
}

export function updateMediaWidth(mediaId: string, width: number, outputFormat?: OutputFormat) {
  mediaList.update((list) =>
    list.map((m) => (m.mediaId === mediaId ? { ...m, width, ...(outputFormat && { outputFormat }) } : m)),
  );
}

export function addMedia(media: Media) {
  mediaList.update((list) => [media, ...list]);
}

export function removeMedia(mediaId: string) {
  mediaList.update((list) => list.filter((m) => m.mediaId !== mediaId));
}
