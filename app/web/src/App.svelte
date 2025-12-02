<script lang="ts">
  import { QueryClientProvider } from "@tanstack/svelte-query";
  import { queryClient } from "./shared/queries";
  import Header from "./shared/components/Header.svelte";
  import MediaPage from "./features/media/pages/MediaPage.svelte";
  import AnalyticsPage from "./features/analytics/pages/AnalyticsPage.svelte";

  let currentPath = $state(window.location.pathname);

  // Handle navigation
  function navigate(path: string) {
    window.history.pushState({}, "", path);
    currentPath = path;
  }

  // Expose navigate function globally for the Header links
  if (typeof window !== "undefined") {
    (window as unknown as { navigate: typeof navigate }).navigate = navigate;
  }

  // Listen for popstate (back/forward buttons)
  $effect(() => {
    function handlePopState() {
      currentPath = window.location.pathname;
    }

    window.addEventListener("popstate", handlePopState);

    return () => {
      window.removeEventListener("popstate", handlePopState);
    };
  });
</script>

<QueryClientProvider client={queryClient}>
  <div class="min-h-screen bg-gray-50">
    <Header {currentPath} {navigate} />
    <main class="max-w-5xl mx-auto px-6 py-8">
      {#if currentPath === "/" || currentPath === ""}
        <MediaPage />
      {:else if currentPath === "/analytics"}
        <AnalyticsPage />
      {:else}
        <MediaPage />
      {/if}
    </main>
  </div>
</QueryClientProvider>
