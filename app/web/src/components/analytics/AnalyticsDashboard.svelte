<script lang="ts">
  import { onMount } from "svelte";
  import type { Period, AnalyticsSummary, MediaViewCount } from "../../lib/types";
  import { getAnalyticsSummary, getTopMedia } from "../../lib/api";
  import StatCard from "./StatCard.svelte";
  import PeriodSelector from "./PeriodSelector.svelte";
  import TopMediaTable from "./TopMediaTable.svelte";
  import FormatUsageChart from "./FormatUsageChart.svelte";

  let loading = $state(true);
  let error = $state<string | null>(null);
  let selectedPeriod = $state<Period>("TODAY");

  let summary = $state<AnalyticsSummary | null>(null);
  let topMediaByPeriod = $state<MediaViewCount[]>([]);
  let topMediaLoading = $state(false);

  async function loadSummary() {
    try {
      loading = true;
      error = null;
      summary = await getAnalyticsSummary();
    } catch (e) {
      error = e instanceof Error ? e.message : "Failed to load analytics";
      console.error("Failed to load analytics:", e);
    } finally {
      loading = false;
    }
  }

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

  onMount(() => {
    loadSummary();
    loadTopMediaByPeriod(selectedPeriod);

    // Refresh every 30 seconds
    const interval = setInterval(() => {
      loadSummary();
      loadTopMediaByPeriod(selectedPeriod);
    }, 30000);

    return () => clearInterval(interval);
  });
</script>

<div class="space-y-6">
  <!-- Header -->
  <div class="flex items-center justify-between">
    <h2 class="text-lg font-semibold text-gray-900">Analytics Dashboard</h2>
    <button
      class="text-sm text-gray-500 hover:text-gray-700 flex items-center space-x-1"
      onclick={loadSummary}
      disabled={loading}
    >
      <svg
        class="w-4 h-4 {loading ? 'animate-spin' : ''}"
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

  {#if error}
    <div class="bg-red-50 border border-red-200 rounded-lg p-4 text-sm text-red-700">
      {error}
    </div>
  {/if}

  {#if loading && !summary}
    <div class="flex items-center justify-center py-12">
      <div class="animate-spin rounded-full h-8 w-8 border-b-2 border-gray-600"></div>
    </div>
  {:else if summary}
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
      />

      <!-- Format Usage -->
      <FormatUsageChart data={summary.formatUsage} loading={loading} />
    </div>

    <!-- All Time Top Media -->
    <div class="grid md:grid-cols-2 gap-6">
      <TopMediaTable title="Top Media Today" data={summary.topMediaToday} />
      <TopMediaTable title="Top Media All Time" data={summary.topMediaAllTime} />
    </div>
  {/if}
</div>
