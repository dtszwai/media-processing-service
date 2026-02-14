<script lang="ts">
import UploadZone from "../components/UploadZone.svelte";
import ResultSection from "../components/ResultSection.svelte";
import MediaList from "../components/MediaList.svelte";
import { createMediaQuery } from "../queries";
import { currentMediaId } from "../stores";
import type { MediaType } from "../../../shared/types";

  interface Props {
    mediaType?: MediaType;
  }

  let { mediaType = "image" }: Props = $props();

  const MEDIA_ID_QUERY_PARAM = "mediaId";
  let selectionContextLabel = $state("Source");

  function syncMediaIdFromUrl() {
    if (typeof window === "undefined") return;
    const params = new URLSearchParams(window.location.search);
    currentMediaId.set(params.get(MEDIA_ID_QUERY_PARAM));
  }

  function updateUrlFromMediaId(mediaId: string | null) {
    if (typeof window === "undefined") return;
    const url = new URL(window.location.href);
    if (mediaId) {
      url.searchParams.set(MEDIA_ID_QUERY_PARAM, mediaId);
    } else {
      url.searchParams.delete(MEDIA_ID_QUERY_PARAM);
    }
    const nextUrl = `${url.pathname}${url.search}${url.hash}`;
    const currentUrl = `${window.location.pathname}${window.location.search}${window.location.hash}`;
    if (nextUrl !== currentUrl) {
      window.history.replaceState({}, "", nextUrl);
    }
  }

  $effect(() => {
    if (typeof window === "undefined") return;
    const handleNavigation = () => {
      syncMediaIdFromUrl();
    };

    handleNavigation();
    window.addEventListener("popstate", handleNavigation);
    window.addEventListener("app:navigate", handleNavigation);

    return () => {
      window.removeEventListener("popstate", handleNavigation);
      window.removeEventListener("app:navigate", handleNavigation);
    };
  });

  $effect(() => {
    updateUrlFromMediaId($currentMediaId);
  });

  const currentMediaQuery = $derived($currentMediaId ? createMediaQuery($currentMediaId) : null);
  let currentMedia = $derived(currentMediaQuery?.data ?? null);

  $effect(() => {
    if (!$currentMediaId) selectionContextLabel = "Source";
  });

  function closeWorkstation() {
    currentMediaId.set(null);
  }

  function handleSelectionContextChange(label: string) {
    selectionContextLabel = label;
  }
</script>

{#if mediaType === "image"}
  <!-- Lumina-style split-screen image library -->
  <div class="flex gap-6 h-full relative">
    <!-- Library Area -->
    <div class="flex-1 flex flex-col gap-5 transition-all duration-300 ease-in-out min-w-0 {$currentMediaId ? 'mr-[440px]' : ''}">
      <MediaList {mediaType} layout="grid" />
    </div>

    <!-- Workstation Slide-over Panel -->
    <div
      class="fixed right-0 top-16 bottom-0 w-[440px] bg-white border-l border-gray-200 shadow-2xl transform transition-transform duration-300 z-20 flex flex-col {$currentMediaId ? 'translate-x-0' : 'translate-x-full'}"
    >
      {#if $currentMediaId}
        <!-- Workstation Header -->
        <div class="h-14 border-b border-gray-100 flex items-center justify-between px-5 bg-gray-50/50 flex-shrink-0">
          <div class="min-w-0">
            <h3 class="text-sm font-bold text-gray-800 truncate">{currentMedia?.name || "Media"}</h3>
            <p class="text-[11px] text-gray-500 truncate">{selectionContextLabel}</p>
          </div>
          <button
            onclick={closeWorkstation}
            aria-label="Close workstation"
            class="text-gray-400 hover:text-gray-600 p-1 rounded-md hover:bg-gray-200 transition-colors"
          >
            <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <!-- Workstation Content -->
        <div class="flex-1 overflow-y-auto">
          <ResultSection layout="panel" onSelectionContextChange={handleSelectionContextChange} />
        </div>
      {/if}
    </div>
  </div>
{:else}
  <!-- Standard document layout -->
  <div class="grid lg:grid-cols-3 gap-8">
    <div class="lg:col-span-2 space-y-6">
      <UploadZone {mediaType} />
      {#if $currentMediaId}
        <ResultSection />
      {/if}
    </div>

    <!-- History Sidebar -->
    <div class="lg:col-span-1">
      <MediaList {mediaType} />
    </div>
  </div>
{/if}
