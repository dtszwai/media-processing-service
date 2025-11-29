<script lang="ts">
  import { apiConnected } from '../lib/stores';
</script>

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
      <div class="flex items-center space-x-2 text-sm text-gray-500">
        {#if $apiConnected}
          <span class="w-2 h-2 bg-green-500 rounded-full"></span>
          <span>Connected</span>
        {:else}
          <span class="w-2 h-2 bg-red-500 rounded-full"></span>
          <span>Disconnected</span>
        {/if}
      </div>
    </div>
  </div>
</header>
