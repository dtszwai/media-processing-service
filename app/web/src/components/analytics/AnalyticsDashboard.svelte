<script lang="ts">
  import type { Period, MediaViewCount } from "../../lib/types";
  import { getTopMedia } from "../../lib/api";
  import { createAnalyticsSummaryQuery } from "../../lib/queries";
  import StatCard from "./StatCard.svelte";
  import PeriodSelector from "./PeriodSelector.svelte";
  import TopMediaTable from "./TopMediaTable.svelte";
  import FormatUsageChart from "./FormatUsageChart.svelte";
  import MediaPreviewModal from "./MediaPreviewModal.svelte";

  let selectedPeriod = $state<Period>("TODAY");
  let topMediaByPeriod = $state<MediaViewCount[]>([]);
  let topMediaLoading = $state(false);
  let selectedMedia = $state<MediaViewCount | null>(null);

  const summaryQuery = createAnalyticsSummaryQuery();

  async function loadTopMediaByPeriod(period: Period) {
    try {
      topMediaLoading = true;
      topMediaByPeriod = await getTopMedia(period, 10);
    } catch (e) {
      console.error("Failed to load top media:", e);
      topMediaByPeriod = [];
    } finally {
      topMediaLoading = false;
    }
  }

  function handlePeriodChange(period: Period) {
    selectedPeriod = period;
    loadTopMediaByPeriod(period);
  }

  function handleRefresh() {
    summaryQuery.refetch();
    loadTopMediaByPeriod(selectedPeriod);
  }

  function handleMediaClick(media: MediaViewCount) {
    selectedMedia = media;
  }

  function handleModalClose() {
    selectedMedia = null;
  }

  // Load initial top media data
  $effect(() => {
    loadTopMediaByPeriod(selectedPeriod);
  });
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
        data={topMediaByPeriod}
        loading={topMediaLoading}
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
