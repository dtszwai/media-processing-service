import { writable } from "svelte/store";

export type AppView = "upload" | "analytics";

export const currentMediaId = writable<string | null>(null);
export const isProcessing = writable(false);
export const currentView = writable<AppView>("upload");
