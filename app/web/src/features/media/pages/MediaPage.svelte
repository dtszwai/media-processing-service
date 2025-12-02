<script lang="ts">
  import UploadZone from "../components/UploadZone.svelte";
  import ResultSection from "../components/ResultSection.svelte";
  import MediaList from "../components/MediaList.svelte";
  import { currentMediaId } from "../stores";
</script>

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
