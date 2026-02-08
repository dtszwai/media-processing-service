<script lang="ts">
  import { createServiceHealthQuery, createVersionInfoQuery } from "../queries";
  import { authStore } from "../../features/auth/stores/auth.store";
  import { queryClient } from "../queries";
  import type { HealthStatus, UserInfo } from "../types";

  interface Props {
    currentPath: string;
    navigate: (path: string) => void;
    user: UserInfo | null;
  }

  let { currentPath, navigate, user }: Props = $props();

  let showDetails = $state(false);
  let showUserMenu = $state(false);
  let dropdownRef: HTMLDivElement;
  let userMenuRef: HTMLDivElement;

  const healthQuery = createServiceHealthQuery();
  const versionQuery = createVersionInfoQuery();

  let isAdmin = $derived(user?.roles?.includes("ADMIN") ?? false);
  let isImagesRoute = $derived(
    currentPath === "/" || currentPath === "/media" || currentPath.startsWith("/media/images"),
  );
  let isDocumentsRoute = $derived(currentPath.startsWith("/media/documents"));

  function handleClickOutside(event: MouseEvent) {
    if (showDetails && dropdownRef && !dropdownRef.contains(event.target as Node)) {
      showDetails = false;
    }
    if (showUserMenu && userMenuRef && !userMenuRef.contains(event.target as Node)) {
      showUserMenu = false;
    }
  }

  function handleNavClick(e: MouseEvent, path: string) {
    e.preventDefault();
    navigate(path);
  }

  function handleLogout() {
    authStore.logout();
    queryClient.clear();
    navigate("/login");
  }

  function getStatusColor(status: HealthStatus | boolean): string {
    if (typeof status === "boolean") return status ? "bg-green-500" : "bg-red-500";
    switch (status) {
      case "UP":
        return "bg-green-500";
      case "DOWN":
        return "bg-red-500";
      default:
        return "bg-gray-400";
    }
  }

  function getStatusText(status: HealthStatus | boolean): string {
    if (typeof status === "boolean") {
      return status ? "UP" : "DOWN";
    }
    return status;
  }

  let serviceHealth = $derived(
    healthQuery.data ?? {
      overall: "UNKNOWN" as const,
      services: {
        api: false,
        s3: "UNKNOWN" as const,
        dynamoDb: "UNKNOWN" as const,
        sns: "UNKNOWN" as const,
      },
    },
  );
  let versionInfo = $derived(versionQuery.data);
  let apiConnected = $derived(serviceHealth.services.api && serviceHealth.overall === "UP");
  let overallStatus = $derived(serviceHealth.overall);
</script>

<svelte:window onclick={handleClickOutside} />

<header class="bg-white border-b border-gray-200">
  <div class="max-w-5xl mx-auto px-6 py-4">
    <div class="flex items-center justify-between">
      <div class="flex items-center space-x-3">
        <svg class="w-8 h-8 text-gray-800" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="1.5"
            d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z"
          ></path>
        </svg>
        <span class="text-lg font-semibold text-gray-900">Media Processing Service</span>
      </div>

      <div class="flex items-center space-x-6">
        <!-- Navigation -->
        <nav class="flex items-center space-x-4">
          <a
            href="/media/images"
            onclick={(e) => handleNavClick(e, "/media/images")}
            class="text-sm font-medium transition-colors {isImagesRoute
              ? 'text-gray-900'
              : 'text-gray-500 hover:text-gray-700'}"
          >
            Images
          </a>
          <a
            href="/media/documents"
            onclick={(e) => handleNavClick(e, "/media/documents")}
            class="text-sm font-medium transition-colors {isDocumentsRoute
              ? 'text-gray-900'
              : 'text-gray-500 hover:text-gray-700'}"
          >
            Documents
          </a>
          <a
            href="/analytics"
            onclick={(e) => handleNavClick(e, "/analytics")}
            class="text-sm font-medium transition-colors {currentPath === '/analytics'
              ? 'text-gray-900'
              : 'text-gray-500 hover:text-gray-700'}"
          >
            Analytics
          </a>
          {#if isAdmin}
            <a
              href="/admin/dlq"
              onclick={(e) => handleNavClick(e, "/admin/dlq")}
              class="text-sm font-medium transition-colors {currentPath === '/admin/dlq'
                ? 'text-gray-900'
                : 'text-gray-500 hover:text-gray-700'}"
            >
              Admin
            </a>
          {/if}
        </nav>

        <!-- Health Status -->
        <div class="relative" bind:this={dropdownRef}>
          <button
            class="flex items-center space-x-2 text-sm text-gray-500 hover:text-gray-700 focus:outline-none"
            onclick={() => (showDetails = !showDetails)}
          >
            {#if healthQuery.isLoading}
              <!-- Loading state -->
              <svg class="w-3 h-3 animate-spin text-gray-400" fill="none" viewBox="0 0 24 24">
                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
              </svg>
              <span class="text-gray-400">Checking...</span>
            {:else if apiConnected && overallStatus === "UP"}
              <span class="w-2 h-2 rounded-full bg-green-500"></span>
              <span>All Systems Operational</span>
            {:else if serviceHealth.services.api}
              <span class="w-2 h-2 rounded-full bg-yellow-500"></span>
              <span class="text-yellow-600">Degraded</span>
            {:else}
              <span class="w-2 h-2 rounded-full bg-red-500"></span>
              <span class="text-red-600">Disconnected</span>
            {/if}
            <svg
              class="w-4 h-4 transition-transform {showDetails ? 'rotate-180' : ''}"
              fill="none"
              stroke="currentColor"
              viewBox="0 0 24 24"
            >
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7"></path>
            </svg>
          </button>

          {#if showDetails}
            <div class="absolute right-0 mt-2 w-56 bg-white rounded-lg shadow-lg border border-gray-200 py-2 z-50">
              <div class="px-4 py-2 border-b border-gray-100">
                <span class="text-xs font-medium text-gray-500 uppercase">Service Status</span>
              </div>
              <div class="px-4 py-2 space-y-2">
                <div class="flex items-center justify-between">
                  <span class="text-sm text-gray-600">API</span>
                  <div class="flex items-center space-x-1">
                    <span class="w-2 h-2 rounded-full {getStatusColor(serviceHealth.services.api)}"></span>
                    <span class="text-xs text-gray-500">{getStatusText(serviceHealth.services.api)}</span>
                  </div>
                </div>
                <div class="flex items-center justify-between">
                  <span class="text-sm text-gray-600">S3 Storage</span>
                  <div class="flex items-center space-x-1">
                    <span class="w-2 h-2 rounded-full {getStatusColor(serviceHealth.services.s3)}"></span>
                    <span class="text-xs text-gray-500">{getStatusText(serviceHealth.services.s3)}</span>
                  </div>
                </div>
                <div class="flex items-center justify-between">
                  <span class="text-sm text-gray-600">DynamoDB</span>
                  <div class="flex items-center space-x-1">
                    <span class="w-2 h-2 rounded-full {getStatusColor(serviceHealth.services.dynamoDb)}"></span>
                    <span class="text-xs text-gray-500">{getStatusText(serviceHealth.services.dynamoDb)}</span>
                  </div>
                </div>
                <div class="flex items-center justify-between">
                  <span class="text-sm text-gray-600">SNS Events</span>
                  <div class="flex items-center space-x-1">
                    <span class="w-2 h-2 rounded-full {getStatusColor(serviceHealth.services.sns)}"></span>
                    <span class="text-xs text-gray-500">{getStatusText(serviceHealth.services.sns)}</span>
                  </div>
                </div>
              </div>
              {#if versionInfo?.build?.version}
                <div class="px-4 py-2 border-t border-gray-100">
                  <div class="flex items-center justify-between">
                    <span class="text-xs text-gray-400">Version</span>
                    <span class="text-xs text-gray-500">v{versionInfo.build.version}</span>
                  </div>
                </div>
              {/if}
            </div>
          {/if}
        </div>

        <!-- User Menu -->
        {#if user}
          <div class="relative" bind:this={userMenuRef}>
            <button
              class="flex items-center space-x-2 focus:outline-none"
              onclick={() => (showUserMenu = !showUserMenu)}
            >
              <div
                class="w-8 h-8 rounded-full bg-gray-200 flex items-center justify-center text-sm font-medium text-gray-600"
              >
                {user.email.charAt(0).toUpperCase()}
              </div>
            </button>

            {#if showUserMenu}
              <div class="absolute right-0 mt-2 w-56 bg-white rounded-lg shadow-lg border border-gray-200 py-1 z-50">
                <div class="px-4 py-2 border-b border-gray-100">
                  <p class="text-sm font-medium text-gray-900 truncate">{user.email}</p>
                  <p class="text-xs text-gray-500">{user.roles.join(", ")}</p>
                </div>
                <button
                  type="button"
                  class="w-full text-left px-4 py-2 text-sm text-gray-700 hover:bg-gray-50"
                  onclick={() => {
                    showUserMenu = false;
                    navigate("/settings/api-keys");
                  }}
                >
                  API Keys
                </button>
                <div class="border-t border-gray-100">
                  <button
                    type="button"
                    class="w-full text-left px-4 py-2 text-sm text-red-600 hover:bg-gray-50"
                    onclick={handleLogout}
                  >
                    Sign out
                  </button>
                </div>
              </div>
            {/if}
          </div>
        {/if}
      </div>
    </div>
  </div>
</header>
