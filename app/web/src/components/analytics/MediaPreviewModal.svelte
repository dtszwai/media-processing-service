<script lang="ts">
  import { getDownloadUrl } from "../../lib/api";
  import { createMediaViewsQuery } from "../../lib/queries";
  import { currentMediaId, currentView } from "../../lib/stores";
  import type { EntityViewCount } from "../../lib/types";

  interface Props {
    media: EntityViewCount | null;
    onclose: () => void;
  }

  let { media, onclose }: Props = $props();

  // Query for detailed view stats when modal opens (skip for deleted media)
  const viewsQuery = $derived(media && !media.deleted ? createMediaViewsQuery(media.entityId) : null);

  function handleBackdropClick(e: MouseEvent) {
    if (e.target === e.currentTarget) {
      onclose();
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === "Escape") {
      onclose();
    }
  }

  function handleViewDetails() {
    if (media && !media.deleted) {
      currentMediaId.set(media.entityId);
      currentView.set("upload");
      onclose();
    }
  }

  function handleDownload() {
    if (media && !media.deleted) {
      window.open(getDownloadUrl(media.entityId), "_blank");
    }
  }

  function formatNumber(num: number): string {
    return num.toLocaleString();
  }

  function formatDeletedAt(deletedAt?: string): string {
    if (!deletedAt) return "";
    try {
      const date = new Date(deletedAt);
      return date.toLocaleDateString(undefined, {
        year: "numeric",
        month: "long",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      });
    } catch {
      return "";
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if media}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- Backdrop - keyboard handled via svelte:window -->
  <div
    class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4"
    onclick={handleBackdropClick}
    role="dialog"
    aria-modal="true"
    aria-labelledby="modal-title"
    tabindex="-1"
  >
    <!-- Modal -->
    <div class="bg-white rounded-xl shadow-2xl max-w-2xl w-full max-h-[90vh] overflow-hidden">
      <!-- Header -->
      <div class="flex items-center justify-between px-6 py-4 border-b border-gray-200">
        <div class="flex items-center space-x-3 min-w-0 pr-4">
          <h2 id="modal-title" class="text-lg font-semibold text-gray-900 truncate" title={media.name}>
            {media.name}
          </h2>
          {#if media.deleted}
            <span class="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-red-100 text-red-700 flex-shrink-0">
              Deleted
            </span>
          {/if}
        </div>
        <button
          onclick={onclose}
          class="text-gray-400 hover:text-gray-600 p-1 rounded-lg hover:bg-gray-100 transition-colors flex-shrink-0"
          aria-label="Close modal"
        >
          <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path>
          </svg>
        </button>
      </div>

      <!-- Content -->
      <div class="p-6 overflow-y-auto max-h-[calc(90vh-140px)]">
        <!-- Image Preview or Deleted Placeholder -->
        {#if media.deleted}
          <div class="bg-gray-100 rounded-lg overflow-hidden mb-6 flex flex-col items-center justify-center py-12">
            <svg class="w-16 h-16 text-gray-300 mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"></path>
            </svg>
            <p class="text-gray-500 font-medium">This media has been deleted</p>
            {#if media.deletedAt}
              <p class="text-gray-400 text-sm mt-1">Deleted on {formatDeletedAt(media.deletedAt)}</p>
            {/if}
            <p class="text-gray-400 text-xs mt-3">The file is no longer available for download</p>
          </div>
        {:else}
          <div class="bg-gray-100 rounded-lg overflow-hidden mb-6">
            <img
              src="{getDownloadUrl(media.entityId)}?t={Date.now()}"
              alt={media.name}
              class="w-full h-auto max-h-80 object-contain"
            />
          </div>
        {/if}

        <!-- View Statistics -->
        <div class="grid grid-cols-2 sm:grid-cols-4 gap-4 mb-6">
          <div class="bg-gray-50 rounded-lg p-4 text-center">
            <p class="text-2xl font-bold text-gray-900">{formatNumber(media.viewCount)}</p>
            <p class="text-xs text-gray-500 mt-1">Total Views</p>
          </div>
          {#if viewsQuery}
            {#if viewsQuery.isLoading}
              <div class="bg-gray-50 rounded-lg p-4 text-center animate-pulse">
                <div class="h-8 bg-gray-200 rounded w-16 mx-auto"></div>
                <p class="text-xs text-gray-500 mt-1">Today</p>
              </div>
              <div class="bg-gray-50 rounded-lg p-4 text-center animate-pulse">
                <div class="h-8 bg-gray-200 rounded w-16 mx-auto"></div>
                <p class="text-xs text-gray-500 mt-1">This Week</p>
              </div>
              <div class="bg-gray-50 rounded-lg p-4 text-center animate-pulse">
                <div class="h-8 bg-gray-200 rounded w-16 mx-auto"></div>
                <p class="text-xs text-gray-500 mt-1">This Month</p>
              </div>
            {:else if viewsQuery.data}
              <div class="bg-blue-50 rounded-lg p-4 text-center">
                <p class="text-2xl font-bold text-blue-600">{formatNumber(viewsQuery.data.today)}</p>
                <p class="text-xs text-gray-500 mt-1">Today</p>
              </div>
              <div class="bg-green-50 rounded-lg p-4 text-center">
                <p class="text-2xl font-bold text-green-600">{formatNumber(viewsQuery.data.thisWeek)}</p>
                <p class="text-xs text-gray-500 mt-1">This Week</p>
              </div>
              <div class="bg-purple-50 rounded-lg p-4 text-center">
                <p class="text-2xl font-bold text-purple-600">{formatNumber(viewsQuery.data.thisMonth)}</p>
                <p class="text-xs text-gray-500 mt-1">This Month</p>
              </div>
            {:else}
              <div class="bg-gray-50 rounded-lg p-4 text-center col-span-3">
                <p class="text-sm text-gray-400">Stats unavailable</p>
              </div>
            {/if}
          {/if}
        </div>

        <!-- Rank Badge -->
        <div class="flex items-center justify-center mb-6">
          <div
            class="inline-flex items-center space-x-2 px-4 py-2 rounded-full
            {media.rank === 1
              ? 'bg-yellow-100 text-yellow-700'
              : media.rank === 2
                ? 'bg-gray-100 text-gray-600'
                : media.rank === 3
                  ? 'bg-orange-100 text-orange-700'
                  : 'bg-gray-50 text-gray-500'}"
          >
            <span class="text-lg font-bold">#{media.rank}</span>
            <span class="text-sm">
              {media.rank === 1 ? "Top Performer" : media.rank <= 3 ? "Top 3" : "Ranked"}
            </span>
          </div>
        </div>
      </div>

      <!-- Footer Actions -->
      <div class="flex items-center justify-end gap-3 px-6 py-4 border-t border-gray-200 bg-gray-50">
        {#if media.deleted}
          <p class="text-sm text-gray-400 mr-auto">Historical analytics preserved</p>
          <button
            onclick={onclose}
            class="px-4 py-2 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 transition-colors"
          >
            Close
          </button>
        {:else}
          <button
            onclick={handleViewDetails}
            class="px-4 py-2 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-lg hover:bg-gray-50 transition-colors"
          >
            View Details
          </button>
          <button
            onclick={handleDownload}
            class="px-4 py-2 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 transition-colors flex items-center space-x-2"
          >
            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"
              ></path>
            </svg>
            <span>Download</span>
          </button>
        {/if}
      </div>
    </div>
  </div>
{/if}
