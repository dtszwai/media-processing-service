<script lang="ts">
  import { createApiKeysQuery, createApiKeyMutation, createDeleteApiKeyMutation } from "../queries";
  import type { ApiKeyResponse } from "../../../shared/types";

  let keyName = $state("");
  let newlyCreatedKey = $state<ApiKeyResponse | null>(null);
  let copied = $state(false);
  let deleteConfirmId = $state<string | null>(null);

  const apiKeysQuery = createApiKeysQuery();
  const createKeyMutation = createApiKeyMutation();
  const deleteKeyMutation = createDeleteApiKeyMutation();

  function handleCreate(e: Event) {
    e.preventDefault();
    if (!keyName.trim()) return;

    createKeyMutation.mutate(
      { name: keyName.trim() },
      {
        onSuccess: (data: ApiKeyResponse) => {
          newlyCreatedKey = data;
          keyName = "";
          copied = false;
        },
      },
    );
  }

  function handleDelete(keyId: string) {
    deleteKeyMutation.mutate(keyId, {
      onSuccess: () => {
        deleteConfirmId = null;
      },
    });
  }

  async function copyKey(key: string) {
    await navigator.clipboard.writeText(key);
    copied = true;
    setTimeout(() => (copied = false), 2000);
  }

  function formatDate(dateStr: string): string {
    return new Date(dateStr).toLocaleDateString("en-US", {
      year: "numeric",
      month: "short",
      day: "numeric",
    });
  }
</script>

<div class="max-w-2xl mx-auto space-y-6">
  <div>
    <h1 class="text-xl font-semibold text-gray-900">API Keys</h1>
    <p class="text-sm text-gray-500 mt-1">Manage API keys for programmatic access.</p>
  </div>

  <!-- Create key form -->
  <div class="bg-white rounded-lg border border-gray-200 p-6">
    <h2 class="text-sm font-medium text-gray-900 mb-4">Create new key</h2>
    <form onsubmit={handleCreate} class="flex items-end gap-3">
      <div class="flex-1">
        <label for="keyName" class="block text-sm text-gray-600 mb-1">Name</label>
        <input
          id="keyName"
          type="text"
          bind:value={keyName}
          required
          class="w-full px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-gray-400 focus:border-transparent"
          placeholder="e.g. CI/CD Pipeline"
        />
      </div>
      <button
        type="submit"
        disabled={!keyName.trim() || createKeyMutation.isPending}
        class="px-4 py-2 bg-gray-900 text-white text-sm font-medium rounded-md hover:bg-gray-800 disabled:opacity-50 disabled:cursor-not-allowed whitespace-nowrap"
      >
        {createKeyMutation.isPending ? "Creating..." : "Create key"}
      </button>
    </form>
  </div>

  <!-- Newly created key banner -->
  {#if newlyCreatedKey?.rawKey}
    <div class="bg-yellow-50 border border-yellow-200 rounded-lg p-4">
      <div class="flex items-start gap-3">
        <svg class="w-5 h-5 text-yellow-600 mt-0.5 shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-2.5L13.732 4c-.77-.833-1.964-.833-2.732 0L4.082 16.5c-.77.833.192 2.5 1.732 2.5z"></path>
        </svg>
        <div class="flex-1 min-w-0">
          <p class="text-sm font-medium text-yellow-800">Copy your API key now</p>
          <p class="text-xs text-yellow-700 mt-0.5">This key won't be shown again.</p>
          <div class="mt-2 flex items-center gap-2">
            <code class="flex-1 bg-white px-3 py-1.5 rounded border border-yellow-300 text-xs font-mono text-gray-800 truncate">
              {newlyCreatedKey.rawKey}
            </code>
            <button
              type="button"
              onclick={() => copyKey(newlyCreatedKey!.rawKey!)}
              class="px-3 py-1.5 text-xs font-medium rounded-md border {copied
                ? 'bg-green-50 border-green-300 text-green-700'
                : 'bg-white border-yellow-300 text-yellow-800 hover:bg-yellow-100'}"
            >
              {copied ? "Copied" : "Copy"}
            </button>
          </div>
        </div>
        <button
          type="button"
          aria-label="Dismiss"
          onclick={() => (newlyCreatedKey = null)}
          class="text-yellow-600 hover:text-yellow-800"
        >
          <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12"></path>
          </svg>
        </button>
      </div>
    </div>
  {/if}

  <!-- Keys list -->
  <div class="bg-white rounded-lg border border-gray-200">
    {#if apiKeysQuery.isLoading}
      <div class="p-6 text-center text-sm text-gray-500">Loading keys...</div>
    {:else if apiKeysQuery.data && apiKeysQuery.data.length > 0}
      <table class="w-full">
        <thead>
          <tr class="border-b border-gray-100">
            <th class="text-left text-xs font-medium text-gray-500 uppercase px-6 py-3">Name</th>
            <th class="text-left text-xs font-medium text-gray-500 uppercase px-6 py-3">Created</th>
            <th class="text-right text-xs font-medium text-gray-500 uppercase px-6 py-3">Actions</th>
          </tr>
        </thead>
        <tbody>
          {#each apiKeysQuery.data as key (key.keyId)}
            <tr class="border-b border-gray-50 last:border-b-0">
              <td class="px-6 py-3">
                <span class="text-sm text-gray-900">{key.name}</span>
                <span class="text-xs text-gray-400 ml-2 font-mono">{key.keyId.slice(0, 8)}...</span>
              </td>
              <td class="px-6 py-3 text-sm text-gray-500">{formatDate(key.createdAt)}</td>
              <td class="px-6 py-3 text-right">
                {#if deleteConfirmId === key.keyId}
                  <span class="text-xs text-gray-500 mr-2">Revoke?</span>
                  <button
                    type="button"
                    onclick={() => handleDelete(key.keyId)}
                    disabled={deleteKeyMutation.isPending}
                    class="text-xs text-red-600 hover:text-red-800 font-medium mr-2"
                  >
                    Yes
                  </button>
                  <button
                    type="button"
                    onclick={() => (deleteConfirmId = null)}
                    class="text-xs text-gray-500 hover:text-gray-700"
                  >
                    No
                  </button>
                {:else}
                  <button
                    type="button"
                    onclick={() => (deleteConfirmId = key.keyId)}
                    class="text-xs text-red-500 hover:text-red-700"
                  >
                    Revoke
                  </button>
                {/if}
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    {:else}
      <div class="p-6 text-center text-sm text-gray-500">No API keys yet. Create one above.</div>
    {/if}
  </div>
</div>
