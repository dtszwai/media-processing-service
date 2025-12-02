<script lang="ts">
  interface Props {
    title: string;
    value: number | string;
    subtitle?: string;
    trend?: "up" | "down" | "neutral";
  }

  let { title, value, subtitle = "", trend = "neutral" }: Props = $props();

  function formatNumber(num: number | string): string {
    if (typeof num === "string") return num;
    if (num >= 1000000) return `${(num / 1000000).toFixed(1)}M`;
    if (num >= 1000) return `${(num / 1000).toFixed(1)}K`;
    return num.toString();
  }
</script>

<div class="bg-white rounded-lg border border-gray-200 p-4">
  <div class="flex items-center justify-between">
    <span class="text-sm text-gray-500">{title}</span>
    {#if trend === "up"}
      <svg class="w-4 h-4 text-green-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 10l7-7m0 0l7 7m-7-7v18"></path>
      </svg>
    {:else if trend === "down"}
      <svg class="w-4 h-4 text-red-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 14l-7 7m0 0l-7-7m7 7V3"></path>
      </svg>
    {/if}
  </div>
  <div class="mt-2">
    <span class="text-2xl font-semibold text-gray-900">{formatNumber(value)}</span>
    {#if subtitle}
      <span class="ml-2 text-sm text-gray-400">{subtitle}</span>
    {/if}
  </div>
</div>
