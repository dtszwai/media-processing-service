<script lang="ts">
  import { formatFileSize, formatRelativeTime } from '../lib/utils';
  import { deleteMedia, pollForStatus } from '../lib/api';
  import { mediaList, currentMediaId, updateMediaStatus, removeMedia } from '../lib/stores';

  interface Props {
    onRefresh: () => void;
  }

  let { onRefresh }: Props = $props();

  async function handleDelete(mediaId: string) {
    const item = $mediaList.find((m) => m.mediaId === mediaId);
    if (item?.status === 'DELETING') return;
    // Don't allow deleting an item that is currently being processed
    if (item?.status === 'PROCESSING' || item?.status === 'PENDING' || item?.status === 'PENDING_UPLOAD') return;

    if (!confirm('Are you sure you want to delete this media?')) return;

    try {
      await deleteMedia(mediaId);
      updateMediaStatus(mediaId, 'DELETING');

      if ($currentMediaId === mediaId) {
        currentMediaId.set(null);
      }

      // Poll until deleted
      const status = await pollForStatus(mediaId, [], undefined, 3000);
      if (status === 'DELETED') {
        removeMedia(mediaId);
      }
    } catch (error) {
      console.error('Delete error:', error);
      alert('Delete failed: ' + (error instanceof Error ? error.message : 'Unknown error'));
    }
  }

  function handleView(mediaId: string) {
    const item = $mediaList.find((m) => m.mediaId === mediaId);
    if (!item || item.status === 'DELETING') return;

    currentMediaId.set(mediaId);
  }
</script>

<div class="card rounded-lg p-5 sticky top-6">
  <div class="flex items-center justify-between mb-4">
    <h2 class="text-base font-semibold text-gray-900">All Media</h2>
    <div class="flex items-center space-x-2">
      <span class="text-xs text-gray-400">
        {$mediaList.length} {$mediaList.length === 1 ? 'item' : 'items'}
      </span>
      <button onclick={onRefresh} class="text-gray-400 hover:text-gray-600" title="Refresh">
        <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
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
    {#if $mediaList.length === 0}
      <p class="text-sm text-gray-400 text-center py-6">No uploads yet</p>
    {:else}
      {#each $mediaList as item (item.mediaId)}
        <div
          class="p-3 rounded-lg bg-gray-50 hover:bg-gray-100 transition-colors"
          class:opacity-50={item.status === 'DELETING'}
        >
          <div class="flex items-center justify-between mb-1">
            <div
              class="flex-1 min-w-0 mr-3"
              class:cursor-pointer={item.status !== 'DELETING'}
              role="button"
              tabindex="0"
              onclick={() => handleView(item.mediaId)}
              onkeydown={(e) => e.key === 'Enter' && handleView(item.mediaId)}
            >
              <p class="text-sm text-gray-800 truncate font-medium">{item.name}</p>
            </div>
            <div class="flex items-center space-x-2">
              <span class="status-badge status-{item.status.toLowerCase()}">{item.status}</span>
              {#if item.status !== 'DELETING'}
                <button
                  onclick={() => handleDelete(item.mediaId)}
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
            <p>{item.width}px · {item.size ? formatFileSize(item.size) : 'N/A'}</p>
            {#if item.createdAt}
              <p>{formatRelativeTime(item.createdAt)}</p>
            {/if}
          </div>
        </div>
      {/each}
    {/if}
  </div>
</div>
