<script lang="ts">
  import type { DownloadStats } from "../../../shared/types";

  interface Props {
    data: DownloadStats | null;
    loading?: boolean;
  }

  let { data, loading = false }: Props = $props();

  // Derived values computed once per data change
  let sortedFormats = $derived(
    data?.byFormat
      ? Object.entries(data.byFormat)
          .map(([format, count]) => ({ format, count }))
          .sort((a, b) => b.count - a.count)
      : [],
  );

  let maxByFormat = $derived(
    sortedFormats.length > 0 ? Math.max(...sortedFormats.map((f) => f.count), 1) : 1,
  );

  function formatNumber(num: number): string {
    return num.toLocaleString();
  }

  function getFormatColor(format: string): string {
    switch (format.toLowerCase()) {
      case "jpeg":
        return "bg-blue-500";
      case "png":
        return "bg-green-500";
      case "webp":
        return "bg-purple-500";
      default:
        return "bg-gray-500";
    }
  }
</script>

<div class="bg-white rounded-xl border border-gray-200 p-4">
  <div class="flex items-center justify-between mb-4">
    <h3 class="text-sm font-medium text-gray-700">Downloads by Format</h3>
    {#if data}
      <span class="text-xs text-gray-500">{data.period.replace("_", " ")}</span>
    {/if}
  </div>

  {#if loading}
    <div class="space-y-3 animate-pulse">
      <div class="h-6 bg-gray-200 rounded w-full"></div>
      <div class="h-6 bg-gray-200 rounded w-3/4"></div>
      <div class="h-6 bg-gray-200 rounded w-1/2"></div>
    </div>
  {:else if !data || sortedFormats.length === 0}
    <div class="text-center py-6">
      <svg class="w-10 h-10 text-gray-300 mx-auto mb-2" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="1.5"
          d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"
        ></path>
      </svg>
      <p class="text-sm text-gray-400">No download data</p>
    </div>
  {:else}
    <div class="space-y-3">
      {#each sortedFormats as { format, count }}
        <div>
          <div class="flex items-center justify-between text-sm mb-1">
            <span class="font-medium text-gray-700 uppercase">{format}</span>
            <span class="text-gray-500">{formatNumber(count)}</span>
          </div>
          <div class="h-2 bg-gray-100 rounded-full overflow-hidden">
            <div
              class="{getFormatColor(format)} h-full rounded-full transition-all duration-300"
              style="width: {(count / maxByFormat) * 100}%"
            ></div>
          </div>
        </div>
      {/each}
    </div>
    <div class="mt-4 pt-4 border-t border-gray-100 flex items-center justify-between">
      <span class="text-sm text-gray-500">Total Downloads</span>
      <span class="text-lg font-semibold text-gray-900">{formatNumber(data.totalDownloads)}</span>
    </div>
  {/if}
</div>
