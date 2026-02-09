<script lang="ts">
  import { formatFileSize, formatRelativeTime } from "../../../shared/utils";
  import { createMediaListQuery, createDeleteMutation } from "../queries";
  import { currentMediaId } from "../stores";
  import type { MediaType } from "../../../shared/types";

  interface Props {
    mediaType?: MediaType;
  }

  let { mediaType }: Props = $props();

  const mediaListQuery = createMediaListQuery(undefined, undefined, mediaType);
  const deleteMutation = createDeleteMutation();

  // Track items being deleted locally for optimistic UI
  let deletingIds = $state<Set<string>>(new Set());

  let mediaList = $derived(mediaListQuery.data?.items ?? []);
  let filteredList = $derived(
    mediaType ? mediaList.filter((item) => (item.mediaType || "image") === mediaType) : mediaList,
  );
  let listTitle = $derived(getListTitle(mediaType));

  function getListTitle(type?: MediaType): string {
    if (type === "image") return "Images";
    if (type === "document") return "Documents";
    return "All Media";
  }

  async function handleDelete(mediaId: string) {
    const item = filteredList.find((m) => m.mediaId === mediaId);
    if (!item) return;
    if (deletingIds.has(mediaId)) return;
    // Don't allow deleting an item that is currently being processed
    if (item.status === "PROCESSING" || item.status === "PENDING" || item.status === "PENDING_UPLOAD") return;

    if (!confirm("Are you sure you want to delete this media?")) return;

    try {
      deletingIds = new Set([...deletingIds, mediaId]);

      await deleteMutation.mutateAsync(mediaId);

      if ($currentMediaId === mediaId) {
        currentMediaId.set(null);
      }

      // Refetch the list after successful deletion
      mediaListQuery.refetch();
    } catch (error) {
      console.error("Delete error:", error);
      alert("Delete failed: " + (error instanceof Error ? error.message : "Unknown error"));
    } finally {
      deletingIds = new Set([...deletingIds].filter((id) => id !== mediaId));
    }
  }

  function handleView(mediaId: string) {
    const item = filteredList.find((m) => m.mediaId === mediaId);
    if (!item || deletingIds.has(mediaId)) return;

    currentMediaId.set(mediaId);
  }

  function handleCardKeydown(event: KeyboardEvent, mediaId: string) {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      handleView(mediaId);
    }
  }

  function handleRefresh() {
    mediaListQuery.refetch();
  }

  function isDeleting(mediaId: string): boolean {
    return deletingIds.has(mediaId);
  }

  function statusBadgeLabel(status: string): string {
    switch (status) {
      case "COMPLETE":
        return "READY";
      case "PENDING_UPLOAD":
        return "UPLOAD";
      case "PENDING":
        return "QUEUED";
      default:
        return status;
    }
  }

  function statusHint(status: string): string | null {
    if (status === "PENDING" || status === "PROCESSING") {
      return "Asset jobs are still running";
    }
    return null;
  }
</script>

<div class="card rounded-lg p-5 sticky top-6">
  <div class="flex items-center justify-between mb-4">
    <h2 class="text-base font-semibold text-gray-900">{listTitle}</h2>
    <div class="flex items-center space-x-2">
      <span class="text-xs text-gray-400">
        {filteredList.length}
        {filteredList.length === 1 ? "item" : "items"}
      </span>
      <button
        onclick={handleRefresh}
        class="text-gray-400 hover:text-gray-600"
        title="Refresh"
        disabled={mediaListQuery.isFetching}
      >
        <svg
          class="w-4 h-4 {mediaListQuery.isFetching ? 'animate-spin' : ''}"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="2"
            d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
          ></path>
        </svg>
      </button>
    </div>
  </div>

  <div class="space-y-2 max-h-80 overflow-y-auto">
    {#if mediaListQuery.isLoading}
      <div class="flex items-center justify-center py-6">
        <div class="animate-spin rounded-full h-6 w-6 border-b-2 border-gray-600"></div>
      </div>
    {:else if mediaListQuery.isError}
      <p class="text-sm text-red-500 text-center py-6">Failed to load media</p>
    {:else if filteredList.length === 0}
      <p class="text-sm text-gray-400 text-center py-6">No uploads yet</p>
    {:else}
      {#each filteredList as item (item.mediaId)}
        <div
          class="p-3 rounded-lg border transition-colors {$currentMediaId === item.mediaId
            ? 'bg-blue-50 border-blue-200 shadow-sm ring-1 ring-blue-100'
            : 'bg-gray-50 border-transparent hover:bg-white hover:border-gray-200'}"
          class:opacity-50={isDeleting(item.mediaId)}
          class:cursor-pointer={!isDeleting(item.mediaId)}
          role="button"
          tabindex="0"
          onclick={() => handleView(item.mediaId)}
          onkeydown={(e) => handleCardKeydown(e, item.mediaId)}
        >
          <div class="flex items-center justify-between mb-1">
            <div class="flex-1 min-w-0 mr-3">
              <p class="text-sm text-gray-800 truncate font-medium">{item.name}</p>
            </div>
            <div class="flex items-center space-x-2">
              <span class="status-badge status-{item.status.toLowerCase()}">{statusBadgeLabel(item.status)}</span>
              {#if !isDeleting(item.mediaId)}
                <button
                  onclick={(e) => {
                    e.stopPropagation();
                    handleDelete(item.mediaId);
                  }}
                  class="text-gray-400 hover:text-red-500 p-1"
                  title="Delete"
                >
                  <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path
                      stroke-linecap="round"
                      stroke-linejoin="round"
                      stroke-width="2"
                      d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                    ></path>
                  </svg>
                </button>
              {/if}
            </div>
          </div>
          <div class="text-xs text-gray-400 space-y-0.5">
            <p>{item.size ? formatFileSize(item.size) : "N/A"} · {item.mediaType || "image"}</p>
            {#if item.createdAt}
              <p>{formatRelativeTime(item.createdAt)}</p>
            {/if}
            {#if statusHint(item.status)}
              <p>{statusHint(item.status)}</p>
            {/if}
          </div>
        </div>
      {/each}
    {/if}
  </div>
</div>
