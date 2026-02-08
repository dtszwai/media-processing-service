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

  const MEDIA_ID_QUERY_PARAM = "mediaId";

  function syncMediaIdFromUrl() {
    if (typeof window === "undefined") return;
    const params = new URLSearchParams(window.location.search);
    currentMediaId.set(params.get(MEDIA_ID_QUERY_PARAM));
  }

  function updateUrlFromMediaId(mediaId: string | null) {
    if (typeof window === "undefined") return;
    const url = new URL(window.location.href);
    if (mediaId) {
      url.searchParams.set(MEDIA_ID_QUERY_PARAM, mediaId);
    } else {
      url.searchParams.delete(MEDIA_ID_QUERY_PARAM);
    }
    const nextUrl = `${url.pathname}${url.search}${url.hash}`;
    const currentUrl = `${window.location.pathname}${window.location.search}${window.location.hash}`;
    if (nextUrl !== currentUrl) {
      window.history.replaceState({}, "", nextUrl);
    }
  }

  $effect(() => {
    if (typeof window === "undefined") return;
    const handleNavigation = () => {
      syncMediaIdFromUrl();
    };

    handleNavigation();
    window.addEventListener("popstate", handleNavigation);
    window.addEventListener("app:navigate", handleNavigation);

    return () => {
      window.removeEventListener("popstate", handleNavigation);
      window.removeEventListener("app:navigate", handleNavigation);
    };
  });

  $effect(() => {
    updateUrlFromMediaId($currentMediaId);
  });
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
