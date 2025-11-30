<script lang="ts">
  import { onMount } from "svelte";
  import Header from "./components/Header.svelte";
  import UploadZone from "./components/UploadZone.svelte";
  import ResultSection from "./components/ResultSection.svelte";
  import MediaList from "./components/MediaList.svelte";
  import { getServiceHealth, getAllMedia, getVersionInfo } from "./lib/api";
  import { mediaList, currentMediaId, apiConnected, serviceHealth, versionInfo } from "./lib/stores";

  async function loadAllMedia() {
    try {
      const response = await getAllMedia();
      mediaList.set(response.items);
    } catch (error) {
      console.error("Failed to load media:", error);
    }
  }

  async function fetchVersionInfo() {
    const info = await getVersionInfo();
    versionInfo.set(info);
  }

  async function checkServices(): Promise<boolean> {
    const health = await getServiceHealth();
    serviceHealth.set(health);
    const isHealthy = health.services.api && health.overall === "UP";
    apiConnected.set(isHealthy);
    return isHealthy;
  }

  onMount(() => {
    checkServices().then((isHealthy) => {
      if (isHealthy) {
        loadAllMedia();
        fetchVersionInfo();
      }
    });
    const interval = setInterval(checkServices, 30000);
    return () => clearInterval(interval);
  });
</script>

<div id="app">
  <Header />

  <main class="max-w-5xl mx-auto px-6 py-8">
    <div class="grid lg:grid-cols-3 gap-8">
      <div class="lg:col-span-2 space-y-6">
        <UploadZone />
        {#if $currentMediaId}
          <ResultSection />
        {/if}
      </div>

      <!-- History Sidebar -->
      <div class="lg:col-span-1">
        <MediaList onRefresh={loadAllMedia} />
      </div>
    </div>
  </main>
</div>
