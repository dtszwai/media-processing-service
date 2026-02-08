<script lang="ts">
  import UploadZone from "../components/UploadZone.svelte";
  import ResultSection from "../components/ResultSection.svelte";
  import MediaList from "../components/MediaList.svelte";
  import { currentMediaId } from "../stores";
  import type { MediaType } from "../../../shared/types";

  interface Props {
    mediaType?: MediaType;
  }

  let { mediaType = "image" }: Props = $props();
</script>

<div class="grid lg:grid-cols-3 gap-8">
  <div class="lg:col-span-2 space-y-6">
    <UploadZone {mediaType} />
    {#if $currentMediaId}
      <ResultSection {mediaType} />
    {/if}
  </div>

  <!-- History Sidebar -->
  <div class="lg:col-span-1">
    <MediaList {mediaType} />
  </div>
</div>
