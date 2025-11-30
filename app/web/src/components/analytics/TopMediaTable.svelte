<script lang="ts">
  import type { MediaViewCount } from "../../lib/types";

  interface Props {
    title: string;
    data: MediaViewCount[];
    loading?: boolean;
    onitemclick?: (item: MediaViewCount) => void;
  }

  let { title, data, loading = false, onitemclick }: Props = $props();

  function formatViewCount(count: number): string {
    if (count >= 1000000) return `${(count / 1000000).toFixed(1)}M`;
    if (count >= 1000) return `${(count / 1000).toFixed(1)}K`;
    return count.toString();
  }

  function truncateName(name: string, maxLength = 25): string {
    if (name.length <= maxLength) return name;
    return name.substring(0, maxLength - 3) + "...";
  }

  function handleItemClick(item: MediaViewCount) {
    onitemclick?.(item);
  }

  function handleKeydown(e: KeyboardEvent, item: MediaViewCount) {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      handleItemClick(item);
    }
  }
</script>

<div class="bg-white rounded-lg border border-gray-200">
  <div class="px-4 py-3 border-b border-gray-100">
    <h3 class="text-sm font-medium text-gray-700">{title}</h3>
  </div>

  {#if loading}
    <div class="p-8 text-center">
      <div class="animate-spin rounded-full h-6 w-6 border-b-2 border-gray-400 mx-auto"></div>
      <p class="mt-2 text-sm text-gray-500">Loading...</p>
    </div>
  {:else if data.length === 0}
    <div class="p-8 text-center">
      <svg class="w-12 h-12 text-gray-300 mx-auto" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="1.5"
          d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z"
        ></path>
      </svg>
      <p class="mt-2 text-sm text-gray-500">No data available</p>
    </div>
  {:else}
    <div class="divide-y divide-gray-100">
      {#each data as item}
        <div
          class="px-4 py-3 flex items-center justify-between hover:bg-gray-50 cursor-pointer transition-colors"
          onclick={() => handleItemClick(item)}
          onkeydown={(e) => handleKeydown(e, item)}
          role="button"
          tabindex="0"
        >
          <div class="flex items-center space-x-3">
            <span
              class="w-6 h-6 flex items-center justify-center rounded-full text-xs font-medium
              {item.rank === 1
                ? 'bg-yellow-100 text-yellow-700'
                : item.rank === 2
                  ? 'bg-gray-100 text-gray-600'
                  : item.rank === 3
                    ? 'bg-orange-100 text-orange-700'
                    : 'bg-gray-50 text-gray-500'}"
            >
              {item.rank}
            </span>
            <span class="text-sm text-gray-700" title={item.name}>{truncateName(item.name)}</span>
          </div>
          <div class="flex items-center space-x-2">
            <span class="text-sm font-medium text-gray-900">{formatViewCount(item.viewCount)}</span>
            <svg class="w-4 h-4 text-gray-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"></path>
            </svg>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>
