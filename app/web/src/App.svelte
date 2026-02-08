<script lang="ts">
  import { QueryClientProvider } from "@tanstack/svelte-query";
  import { queryClient } from "./shared/queries";
  import { authStore } from "./features/auth/stores/auth.store";
  import Header from "./shared/components/Header.svelte";
  import MediaPage from "./features/media/pages/MediaPage.svelte";
  import AnalyticsPage from "./features/analytics/pages/AnalyticsPage.svelte";
  import DlqPage from "./features/admin/pages/DlqPage.svelte";
  import LoginPage from "./features/auth/pages/LoginPage.svelte";
  import RegisterPage from "./features/auth/pages/RegisterPage.svelte";
  import ApiKeysPage from "./features/auth/pages/ApiKeysPage.svelte";

  let currentPath = $state(window.location.pathname);

  const PUBLIC_ROUTES = ["/login", "/register"];
  const DEFAULT_MEDIA_PATH = "/media/images";

  // Restore auth from localStorage on mount
  authStore.restore();

  // Handle navigation
  function navigate(path: string) {
    window.history.pushState({}, "", path);
    currentPath = window.location.pathname;
    window.dispatchEvent(new CustomEvent("app:navigate"));
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

  // Route guard
  $effect(() => {
    const isPublicRoute = PUBLIC_ROUTES.includes(currentPath);
    const auth = authState;

    if (auth.isLoading) return;

    if (!auth.isAuthenticated && !isPublicRoute) {
      navigate("/login");
    } else if (auth.isAuthenticated && isPublicRoute) {
      navigate(DEFAULT_MEDIA_PATH);
    } else if (auth.isAuthenticated && (currentPath === "/" || currentPath === "/media")) {
      navigate(DEFAULT_MEDIA_PATH);
    }
  });

  let authState = $derived($authStore);
  let isPublicPage = $derived(PUBLIC_ROUTES.includes(currentPath));
</script>

<QueryClientProvider client={queryClient}>
  <div class="min-h-screen bg-gray-50">
    {#if authState.isLoading}
      <!-- Loading state while restoring auth -->
    {:else if isPublicPage}
      <main class="max-w-5xl mx-auto px-6 py-8">
        {#if currentPath === "/register"}
          <RegisterPage {navigate} />
        {:else}
          <LoginPage {navigate} />
        {/if}
      </main>
    {:else}
      <Header {currentPath} {navigate} user={authState.user} />
      <main class="max-w-5xl mx-auto px-6 py-8">
        {#if currentPath === "/analytics"}
          <AnalyticsPage />
        {:else if currentPath === "/admin/dlq"}
          <DlqPage />
        {:else if currentPath === "/settings/api-keys"}
          <ApiKeysPage />
        {:else if currentPath === "/media/documents"}
          <MediaPage mediaType="document" />
        {:else}
          <MediaPage mediaType="image" />
        {/if}
      </main>
    {/if}
  </div>
</QueryClientProvider>
