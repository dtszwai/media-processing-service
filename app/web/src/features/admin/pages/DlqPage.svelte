<script lang="ts">
  import {
    createDlqStatusQuery,
    createDlqMessagesQuery,
    createReplayDlqMessageMutation,
    createDeleteDlqMessageMutation,
    createPurgeDlqMutation,
  } from "../queries";
  import type { DlqMessage } from "../../../shared/types";

  const statusQuery = createDlqStatusQuery();
  const messagesQuery = createDlqMessagesQuery(10);
  const replayMutation = createReplayDlqMessageMutation();
  const deleteMutation = createDeleteDlqMessageMutation();
  const purgeMutation = createPurgeDlqMutation();

  let selectedMessage = $state<DlqMessage | null>(null);
  let showPurgeConfirm = $state(false);

  function formatTimestamp(timestamp?: string): string {
    if (!timestamp) return "Unknown";
    try {
      return new Date(timestamp).toLocaleString();
    } catch {
      return timestamp;
    }
  }

  function parseMessageBody(body: string): object | string {
    try {
      return JSON.parse(body);
    } catch {
      return body;
    }
  }

  async function handleReplay(message: DlqMessage) {
    try {
      await replayMutation.mutateAsync(message.receiptHandle);
    } catch (error) {
      console.error("Replay failed:", error);
    }
  }

  async function handleDelete(message: DlqMessage) {
    try {
      await deleteMutation.mutateAsync(message.receiptHandle);
      if (selectedMessage?.messageId === message.messageId) {
        selectedMessage = null;
      }
    } catch (error) {
      console.error("Delete failed:", error);
    }
  }

  async function handlePurge() {
    try {
      await purgeMutation.mutateAsync();
      showPurgeConfirm = false;
      selectedMessage = null;
    } catch (error) {
      console.error("Purge failed:", error);
    }
  }
</script>

<div class="space-y-6">
  <!-- Header -->
  <div class="flex items-center justify-between">
    <div>
      <h1 class="text-2xl font-bold text-gray-900">Dead Letter Queue</h1>
      <p class="text-sm text-gray-500 mt-1">Manage failed message processing</p>
    </div>
    {#if statusQuery.data?.configured}
      <button
        onclick={() => (showPurgeConfirm = true)}
        disabled={purgeMutation.isPending || !messagesQuery.data?.length}
        class="px-4 py-2 text-sm font-medium text-red-600 bg-red-50 rounded-lg hover:bg-red-100 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
      >
        Purge All
      </button>
    {/if}
  </div>

  <!-- Status Card -->
  <div class="bg-white rounded-xl shadow-sm border border-gray-200 p-6">
    <div class="flex items-center justify-between">
      <div class="flex items-center space-x-3">
        {#if statusQuery.isLoading}
          <div class="w-3 h-3 rounded-full bg-gray-300 animate-pulse"></div>
          <span class="text-gray-500">Loading...</span>
        {:else if statusQuery.error}
          <div class="w-3 h-3 rounded-full bg-red-500"></div>
          <span class="text-red-600">Error loading status</span>
        {:else if statusQuery.data?.configured}
          <div class="w-3 h-3 rounded-full bg-green-500"></div>
          <span class="text-gray-700">DLQ Configured</span>
        {:else}
          <div class="w-3 h-3 rounded-full bg-yellow-500"></div>
          <span class="text-yellow-700">DLQ Not Configured</span>
        {/if}
      </div>
      {#if statusQuery.data?.approximateMessageCount !== undefined}
        <div class="text-right">
          <p class="text-2xl font-bold text-gray-900">{statusQuery.data.approximateMessageCount}</p>
          <p class="text-xs text-gray-500">Messages in Queue</p>
        </div>
      {/if}
    </div>
  </div>

  {#if !statusQuery.data?.configured}
    <div class="bg-yellow-50 border border-yellow-200 rounded-xl p-6 text-center">
      <svg class="w-12 h-12 text-yellow-400 mx-auto mb-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"></path>
      </svg>
      <h3 class="text-lg font-medium text-yellow-800">DLQ Not Available</h3>
      <p class="text-sm text-yellow-600 mt-1">The Dead Letter Queue is not configured in this environment.</p>
    </div>
  {:else}
    <!-- Messages List -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
      <!-- Message List -->
      <div class="bg-white rounded-xl shadow-sm border border-gray-200 overflow-hidden">
        <div class="px-4 py-3 border-b border-gray-200 bg-gray-50">
          <h2 class="text-sm font-medium text-gray-700">Messages</h2>
        </div>
        <div class="divide-y divide-gray-100 max-h-[500px] overflow-y-auto">
          {#if messagesQuery.isLoading}
            <div class="p-8 text-center">
              <div class="w-6 h-6 border-2 border-gray-300 border-t-gray-600 rounded-full animate-spin mx-auto"></div>
              <p class="text-sm text-gray-500 mt-2">Loading messages...</p>
            </div>
          {:else if messagesQuery.error}
            <div class="p-8 text-center">
              <p class="text-sm text-red-600">Failed to load messages</p>
            </div>
          {:else if !messagesQuery.data?.length}
            <div class="p-8 text-center">
              <svg class="w-12 h-12 text-gray-300 mx-auto mb-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4"></path>
              </svg>
              <p class="text-sm text-gray-500">No messages in queue</p>
            </div>
          {:else}
            {#each messagesQuery.data as message}
              <button
                class="w-full px-4 py-3 text-left hover:bg-gray-50 transition-colors {selectedMessage?.messageId === message.messageId ? 'bg-blue-50' : ''}"
                onclick={() => (selectedMessage = message)}
              >
                <div class="flex items-center justify-between">
                  <span class="text-sm font-mono text-gray-700 truncate">{message.messageId.slice(0, 20)}...</span>
                  <span class="text-xs text-gray-400">{message.approximateReceiveCount ?? 0}x</span>
                </div>
                <p class="text-xs text-gray-500 mt-1">{formatTimestamp(message.sentTimestamp)}</p>
              </button>
            {/each}
          {/if}
        </div>
      </div>

      <!-- Message Detail -->
      <div class="bg-white rounded-xl shadow-sm border border-gray-200 overflow-hidden">
        <div class="px-4 py-3 border-b border-gray-200 bg-gray-50">
          <h2 class="text-sm font-medium text-gray-700">Message Detail</h2>
        </div>
        {#if selectedMessage}
          <div class="p-4 space-y-4">
            <div>
              <span class="text-xs font-medium text-gray-500 uppercase block">Message ID</span>
              <p class="text-sm font-mono text-gray-700 break-all">{selectedMessage.messageId}</p>
            </div>
            <div>
              <span class="text-xs font-medium text-gray-500 uppercase block">Sent At</span>
              <p class="text-sm text-gray-700">{formatTimestamp(selectedMessage.sentTimestamp)}</p>
            </div>
            <div>
              <span class="text-xs font-medium text-gray-500 uppercase block">Receive Count</span>
              <p class="text-sm text-gray-700">{selectedMessage.approximateReceiveCount ?? 0}</p>
            </div>
            <div>
              <span class="text-xs font-medium text-gray-500 uppercase block">Body</span>
              <pre class="mt-1 p-3 bg-gray-50 rounded-lg text-xs text-gray-700 overflow-x-auto max-h-48">{JSON.stringify(parseMessageBody(selectedMessage.body), null, 2)}</pre>
            </div>
            {#if selectedMessage.attributes && Object.keys(selectedMessage.attributes).length > 0}
              <div>
                <span class="text-xs font-medium text-gray-500 uppercase block">Attributes</span>
                <pre class="mt-1 p-3 bg-gray-50 rounded-lg text-xs text-gray-700 overflow-x-auto">{JSON.stringify(selectedMessage.attributes, null, 2)}</pre>
              </div>
            {/if}
            <div class="flex space-x-3 pt-4 border-t border-gray-200">
              <button
                onclick={() => handleReplay(selectedMessage!)}
                disabled={replayMutation.isPending}
                class="flex-1 px-4 py-2 text-sm font-medium text-white bg-blue-600 rounded-lg hover:bg-blue-700 disabled:opacity-50 transition-colors"
              >
                {replayMutation.isPending ? "Replaying..." : "Replay"}
              </button>
              <button
                onclick={() => handleDelete(selectedMessage!)}
                disabled={deleteMutation.isPending}
                class="flex-1 px-4 py-2 text-sm font-medium text-red-600 bg-red-50 rounded-lg hover:bg-red-100 disabled:opacity-50 transition-colors"
              >
                {deleteMutation.isPending ? "Deleting..." : "Delete"}
              </button>
            </div>
          </div>
        {:else}
          <div class="p-8 text-center">
            <svg class="w-12 h-12 text-gray-300 mx-auto mb-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M15 15l-2 5L9 9l11 4-5 2zm0 0l5 5M7.188 2.239l.777 2.897M5.136 7.965l-2.898-.777M13.95 4.05l-2.122 2.122m-5.657 5.656l-2.12 2.122"></path>
            </svg>
            <p class="text-sm text-gray-500">Select a message to view details</p>
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>

<!-- Purge Confirmation Modal -->
{#if showPurgeConfirm}
  <div class="fixed inset-0 bg-black/50 z-50 flex items-center justify-center p-4">
    <div class="bg-white rounded-xl shadow-2xl max-w-md w-full p-6">
      <div class="text-center">
        <svg class="w-12 h-12 text-red-500 mx-auto mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"></path>
        </svg>
        <h3 class="text-lg font-semibold text-gray-900">Purge All Messages?</h3>
        <p class="text-sm text-gray-500 mt-2">This will permanently delete all messages from the Dead Letter Queue. This action cannot be undone.</p>
      </div>
      <div class="flex space-x-3 mt-6">
        <button
          onclick={() => (showPurgeConfirm = false)}
          class="flex-1 px-4 py-2 text-sm font-medium text-gray-700 bg-gray-100 rounded-lg hover:bg-gray-200 transition-colors"
        >
          Cancel
        </button>
        <button
          onclick={handlePurge}
          disabled={purgeMutation.isPending}
          class="flex-1 px-4 py-2 text-sm font-medium text-white bg-red-600 rounded-lg hover:bg-red-700 disabled:opacity-50 transition-colors"
        >
          {purgeMutation.isPending ? "Purging..." : "Purge All"}
        </button>
      </div>
    </div>
  </div>
{/if}
