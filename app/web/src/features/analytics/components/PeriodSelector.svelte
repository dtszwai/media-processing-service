<script lang="ts">
  import type { Period } from "../../../shared/types";

  interface Props {
    selected: Period;
    onchange: (period: Period) => void;
  }

  let { selected, onchange }: Props = $props();

  const periods: { value: Period; label: string }[] = [
    { value: "TODAY", label: "Today" },
    { value: "THIS_WEEK", label: "This Week" },
    { value: "THIS_MONTH", label: "This Month" },
    { value: "ALL_TIME", label: "All Time" },
  ];
</script>

<div class="flex space-x-1 bg-gray-100 rounded-lg p-1">
  {#each periods as period}
    <button
      class="px-3 py-1.5 text-sm font-medium rounded-md transition-colors
        {selected === period.value
        ? 'bg-white text-gray-900 shadow-sm'
        : 'text-gray-500 hover:text-gray-700'}"
      onclick={() => onchange(period.value)}
    >
      {period.label}
    </button>
  {/each}
</div>
