<script lang="ts">
  import { onMount } from 'svelte';
  import Header from './components/Header.svelte';
  import UploadZone from './components/UploadZone.svelte';
  import ResultSection from './components/ResultSection.svelte';
  import MediaList from './components/MediaList.svelte';
  import { checkHealth, getAllMedia } from './lib/api';
  import { mediaList, currentMediaId, apiConnected } from './lib/stores';

  async function loadAllMedia() {
    try {
      const data = await getAllMedia();
      mediaList.set(data);
    } catch (error) {
      console.error('Failed to load media:', error);
    }
  }

  onMount(async () => {
    // Check API health
    const healthy = await checkHealth();
    apiConnected.set(healthy);

    if (healthy) {
      await loadAllMedia();
    }
  });
</script>

<div id="app">
  <Header />

  <main class="max-w-5xl mx-auto px-6 py-8">
    <div class="grid lg:grid-cols-3 gap-8">
      <!-- Upload Section -->
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
