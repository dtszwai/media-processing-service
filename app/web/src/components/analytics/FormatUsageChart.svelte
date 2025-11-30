<script lang="ts">
  interface Props {
    data: Record<string, number>;
    loading?: boolean;
  }

  let { data, loading = false }: Props = $props();

  const formatColors: Record<string, string> = {
    JPEG: "bg-blue-500",
    PNG: "bg-green-500",
    WEBP: "bg-purple-500",
  };

  let total = $derived(Object.values(data).reduce((sum, val) => sum + val, 0));
  let sortedFormats = $derived(Object.entries(data).sort(([, a], [, b]) => b - a));
</script>

<div class="bg-white rounded-lg border border-gray-200">
  <div class="px-4 py-3 border-b border-gray-100">
    <h3 class="text-sm font-medium text-gray-700">Format Usage</h3>
  </div>

  {#if loading}
    <div class="p-8 text-center">
      <div class="animate-spin rounded-full h-6 w-6 border-b-2 border-gray-400 mx-auto"></div>
      <p class="mt-2 text-sm text-gray-500">Loading...</p>
    </div>
  {:else if total === 0}
    <div class="p-8 text-center">
      <svg class="w-12 h-12 text-gray-300 mx-auto" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="1.5"
          d="M11 3.055A9.001 9.001 0 1020.945 13H11V3.055z"
        ></path>
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="1.5"
          d="M20.488 9H15V3.512A9.025 9.025 0 0120.488 9z"
        ></path>
      </svg>
      <p class="mt-2 text-sm text-gray-500">No format data available</p>
    </div>
  {:else}
    <div class="p-4">
      <!-- Bar chart visualization -->
      <div class="space-y-3">
        {#each sortedFormats as [format, count]}
          {@const percentage = total > 0 ? (count / total) * 100 : 0}
          <div>
            <div class="flex items-center justify-between mb-1">
              <span class="text-sm text-gray-600">{format}</span>
              <span class="text-sm text-gray-500">{count} ({percentage.toFixed(1)}%)</span>
            </div>
            <div class="h-2 bg-gray-100 rounded-full overflow-hidden">
              <div
                class="h-full rounded-full transition-all duration-300 {formatColors[format] || 'bg-gray-400'}"
                style="width: {percentage}%"
              ></div>
            </div>
          </div>
        {/each}
      </div>

      <!-- Total -->
      <div class="mt-4 pt-3 border-t border-gray-100">
        <div class="flex items-center justify-between">
          <span class="text-sm font-medium text-gray-700">Total</span>
          <span class="text-sm font-medium text-gray-900">{total}</span>
        </div>
      </div>
    </div>
  {/if}
</div>
