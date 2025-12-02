/**
 * Media-specific Svelte stores
 */
import { writable } from "svelte/store";

/**
 * Currently selected media ID for viewing details
 */
export const currentMediaId = writable<string | null>(null);

/**
 * Whether a processing operation is in progress
 */
export const isProcessing = writable(false);
