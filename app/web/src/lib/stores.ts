import { writable } from 'svelte/store';
import type { Media } from './types';

export const mediaList = writable<Media[]>([]);
export const currentMediaId = writable<string | null>(null);
export const isProcessing = writable(false);
export const apiConnected = writable(false);

export function updateMediaStatus(mediaId: string, status: string) {
  mediaList.update((list) =>
    list.map((m) => (m.mediaId === mediaId ? { ...m, status: status as Media['status'] } : m))
  );
}

export function updateMediaWidth(mediaId: string, width: number) {
  mediaList.update((list) =>
    list.map((m) => (m.mediaId === mediaId ? { ...m, width } : m))
  );
}

export function addMedia(media: Media) {
  mediaList.update((list) => [media, ...list]);
}

export function removeMedia(mediaId: string) {
  mediaList.update((list) => list.filter((m) => m.mediaId !== mediaId));
}
