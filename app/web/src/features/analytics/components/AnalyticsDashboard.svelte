<script lang="ts">
  import type { Period, EntityViewCount } from "../../../shared/types";
  import { createQuery } from "@tanstack/svelte-query";
  import { createAnalyticsSummaryQuery } from "../queries";
  import { queryKeys } from "../../../shared/queries";
  import { getTopMedia } from "../services";
  import { EntityViewCountSchema } from "../../../shared/types";
  import { z } from "zod";
  import StatCard from "./StatCard.svelte";
  import PeriodSelector from "./PeriodSelector.svelte";
  import TopMediaTable from "./TopMediaTable.svelte";
  import FormatUsageChart from "./FormatUsageChart.svelte";
  import MediaPreviewModal from "./MediaPreviewModal.svelte";

  let selectedPeriod = $state<Period>("TODAY");
  let selectedMedia = $state<EntityViewCount | null>(null);

  const summaryQuery = createAnalyticsSummaryQuery();

  // Reactive query that updates when selectedPeriod changes
  const topMediaQuery = createQuery(() => ({
    queryKey: queryKeys.analytics.topMedia(selectedPeriod, 10),
    queryFn: async (): Promise<EntityViewCount[]> => {
      const data = await getTopMedia(selectedPeriod, 10);
      return z.array(EntityViewCountSchema).parse(data);
    },
    staleTime: 30 * 1000,
  }));

  function handlePeriodChange(period: Period) {
    selectedPeriod = period;
  }

  function handleRefresh() {
    summaryQuery.refetch();
    topMediaQuery.refetch();
  }

  function handleMediaClick(media: EntityViewCount) {
    selectedMedia = media;
  }

  function handleModalClose() {
    selectedMedia = null;
  }
</script>

<div class="space-y-6">
  <!-- Header -->
  <div class="flex items-center justify-between">
    <h2 class="text-lg font-semibold text-gray-900">Analytics Dashboard</h2>
    <button
      class="text-sm text-gray-500 hover:text-gray-700 flex items-center space-x-1"
      onclick={handleRefresh}
      disabled={summaryQuery.isFetching}
    >
      <svg
        class="w-4 h-4 {summaryQuery.isFetching ? 'animate-spin' : ''}"
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
      <span>Refresh</span>
    </button>
  </div>

  {#if summaryQuery.isError}
    <div class="bg-red-50 border border-red-200 rounded-lg p-4 text-sm text-red-700">
      {summaryQuery.error instanceof Error ? summaryQuery.error.message : "Failed to load analytics"}
    </div>
  {/if}

  {#if summaryQuery.isLoading && !summaryQuery.data}
    <div class="flex items-center justify-center py-12">
      <div class="animate-spin rounded-full h-8 w-8 border-b-2 border-gray-600"></div>
    </div>
  {:else if summaryQuery.data}
    {@const summary = summaryQuery.data}
    <!-- Stats Overview -->
    <div class="grid grid-cols-2 md:grid-cols-4 gap-4">
      <StatCard title="Total Views" value={summary.totalViews} />
      <StatCard title="Views Today" value={summary.viewsToday} />
      <StatCard title="Total Downloads" value={summary.totalDownloads} />
      <StatCard title="Downloads Today" value={summary.downloadsToday} />
    </div>

    <!-- Period Selector -->
    <div class="flex justify-center">
      <PeriodSelector selected={selectedPeriod} onchange={handlePeriodChange} />
    </div>

    <!-- Charts Grid -->
    <div class="grid md:grid-cols-2 gap-6">
      <!-- Top Media by Selected Period -->
      <TopMediaTable
        title="Top Media ({selectedPeriod === 'TODAY'
          ? 'Today'
          : selectedPeriod === 'THIS_WEEK'
            ? 'This Week'
            : selectedPeriod === 'THIS_MONTH'
              ? 'This Month'
              : 'All Time'})"
        data={topMediaQuery.data ?? []}
        loading={topMediaQuery.isLoading}
        onitemclick={handleMediaClick}
      />

      <!-- Format Usage -->
      <FormatUsageChart data={summary.formatUsage} loading={summaryQuery.isFetching} />
    </div>

    <!-- All Time Top Media -->
    <div class="grid md:grid-cols-2 gap-6">
      <TopMediaTable title="Top Media Today" data={summary.topMediaToday} onitemclick={handleMediaClick} />
      <TopMediaTable title="Top Media All Time" data={summary.topMediaAllTime} onitemclick={handleMediaClick} />
    </div>
  {/if}
</div>

<!-- Media Preview Modal -->
<MediaPreviewModal media={selectedMedia} onclose={handleModalClose} />
