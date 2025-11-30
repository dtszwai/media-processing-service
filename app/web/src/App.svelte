<script lang="ts">
  import { QueryClientProvider } from "@tanstack/svelte-query";
  import { queryClient } from "./lib/query";
  import Header from "./components/Header.svelte";
  import UploadZone from "./components/UploadZone.svelte";
  import ResultSection from "./components/ResultSection.svelte";
  import MediaList from "./components/MediaList.svelte";
  import AnalyticsDashboard from "./components/analytics/AnalyticsDashboard.svelte";
  import { currentMediaId, currentView } from "./lib/stores";
</script>

<QueryClientProvider client={queryClient}>
  <div id="app">
    <Header />

    <main class="max-w-5xl mx-auto px-6 py-8">
      {#if $currentView === "upload"}
        <div class="grid lg:grid-cols-3 gap-8">
          <div class="lg:col-span-2 space-y-6">
            <UploadZone />
            {#if $currentMediaId}
              <ResultSection />
            {/if}
          </div>

          <!-- History Sidebar -->
          <div class="lg:col-span-1">
            <MediaList />
          </div>
        </div>
      {:else if $currentView === "analytics"}
        <AnalyticsDashboard />
      {/if}
    </main>
  </div>
</QueryClientProvider>
