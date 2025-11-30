<script lang="ts">
  import { formatFileSize, formatDateTime } from "../lib/utils";
  import { getDownloadUrl, getOriginalUrl, resizeMedia, pollForStatus } from "../lib/api";
  import { mediaList, currentMediaId, isProcessing, updateMediaStatus, updateMediaWidth } from "../lib/stores";
  import type { Media, OutputFormat } from "../lib/types";

  let resizeWidth = $state(500);
  let resizeFormat = $state<OutputFormat>("jpeg");
  let isResizing = $state(false);

  const formatOptions: { value: OutputFormat; label: string }[] = [
    { value: "jpeg", label: "JPEG" },
    { value: "png", label: "PNG" },
    { value: "webp", label: "WebP" },
  ];

  let currentMedia = $derived($mediaList.find((m) => m.mediaId === $currentMediaId) || null);

  $effect(() => {
    if (currentMedia) {
      resizeWidth = currentMedia.width;
      resizeFormat = currentMedia.outputFormat || "jpeg";
    }
  });

  async function handleResize() {
    if (!currentMedia || isResizing || $isProcessing) return;

    isResizing = true;

    try {
      await resizeMedia(currentMedia.mediaId, { width: resizeWidth, outputFormat: resizeFormat });
      updateMediaStatus(currentMedia.mediaId, "PENDING");

      await pollForStatus(currentMedia.mediaId, ["COMPLETE", "ERROR"], (status) =>
        updateMediaStatus(currentMedia.mediaId, status),
      );

      updateMediaWidth(currentMedia.mediaId, resizeWidth, resizeFormat);
    } catch (error) {
      console.error("Resize error:", error);
      alert("Resize failed: " + (error instanceof Error ? error.message : "Unknown error"));
    } finally {
      isResizing = false;
    }
  }
</script>

{#if currentMedia}
  <div class="card rounded-lg p-6">
    <!-- Media Info Header -->
    <div class="flex items-center justify-between mb-4">
      <div>
        <h2 class="text-base font-semibold text-gray-900">Result</h2>
        <p class="text-xs text-gray-500 mt-0.5">{currentMedia.name}</p>
      </div>
      <div class="flex items-center space-x-3">
        {#if currentMedia.status === "COMPLETE"}
          <a
            href={getDownloadUrl(currentMedia.mediaId)}
            target="_blank"
            rel="noopener noreferrer"
            class="text-sm text-blue-600 hover:text-blue-700 font-medium flex items-center space-x-1"
          >
            <span>Download</span>
            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"
              ></path>
            </svg>
          </a>
        {/if}
      </div>
    </div>

    <!-- Resize Controls -->
    {#if currentMedia.status === "COMPLETE"}
      <div class="flex items-center gap-4 mb-4 p-3 bg-gray-50 rounded-lg">
        <label for="resizeSlider" class="text-xs font-medium text-gray-600">Resize to:</label>
        <input
          type="range"
          id="resizeSlider"
          min="100"
          max="1024"
          bind:value={resizeWidth}
          disabled={isResizing}
          class="flex-1 h-1.5 bg-gray-200 rounded-lg appearance-none cursor-pointer disabled:opacity-50"
        />
        <span class="text-xs font-mono text-gray-600 bg-white px-2 py-1 rounded border">{resizeWidth}px</span>
        <select
          bind:value={resizeFormat}
          disabled={isResizing}
          class="text-xs bg-white border border-gray-300 rounded px-2 py-1.5 text-gray-600 focus:outline-none focus:ring-1 focus:ring-blue-500 disabled:opacity-50"
        >
          {#each formatOptions as option}
            <option value={option.value}>{option.label}</option>
          {/each}
        </select>
        <button
          onclick={handleResize}
          disabled={isResizing}
          class="btn-primary px-4 py-1.5 text-xs font-medium rounded-lg"
        >
          {#if isResizing}
            <svg
              class="animate-spin -ml-1 mr-2 h-3 w-3 text-white inline-block"
              xmlns="http://www.w3.org/2000/svg"
              fill="none"
              viewBox="0 0 24 24"
            >
              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
              <path
                class="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              ></path>
            </svg>
            Resizing...
          {:else}
            Resize
          {/if}
        </button>
      </div>
    {/if}

    <!-- Image Comparison -->
    <div class="grid grid-cols-2 gap-4 mb-4">
      <div class="image-box">
        <p class="text-xs font-medium text-gray-500 mb-2">Original</p>
        <img src={getOriginalUrl(currentMedia.mediaId, currentMedia.name)} alt="Original" />
        <div class="mt-2 space-y-0.5">
          <p class="text-xs text-gray-500">Size: {formatFileSize(currentMedia.size)}</p>
        </div>
      </div>
      <div class="image-box">
        <p class="text-xs font-medium text-gray-500 mb-2">Processed</p>
        {#if currentMedia.status === "COMPLETE"}
          <img src="{getDownloadUrl(currentMedia.mediaId)}?t={Date.now()}" alt="Processed" />
          <div class="mt-2 space-y-0.5">
            <p class="text-xs text-gray-500">Width: {currentMedia.width}px</p>
            <p class="text-xs text-gray-500">
              Format: <span class="uppercase">{currentMedia.outputFormat || "jpeg"}</span>
            </p>
          </div>
        {:else}
          <div class="h-[180px] flex items-center justify-center text-gray-400 text-sm">
            {#if currentMedia.status === "PROCESSING"}
              <span class="pulse">Processing...</span>
            {:else if currentMedia.status === "PENDING" || currentMedia.status === "PENDING_UPLOAD"}
              <span>Pending...</span>
            {:else}
              <span>{currentMedia.status}</span>
            {/if}
          </div>
        {/if}
      </div>
    </div>

    <!-- Media Metadata -->
    <div class="p-3 bg-gray-50 rounded-lg">
      <div class="grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
        <div>
          <span class="text-gray-400">Media ID:</span>
          <p class="font-mono text-gray-600 truncate" title={currentMedia.mediaId}>{currentMedia.mediaId}</p>
        </div>
        <div>
          <span class="text-gray-400">Status:</span>
          <p class="text-gray-600">{currentMedia.status}</p>
        </div>
        <div>
          <span class="text-gray-400">MIME Type:</span>
          <p class="text-gray-600">{currentMedia.mimetype}</p>
        </div>
        <div>
          <span class="text-gray-400">Original Size:</span>
          <p class="text-gray-600">{formatFileSize(currentMedia.size)}</p>
        </div>
        <div>
          <span class="text-gray-400">Width:</span>
          <p class="text-gray-600">{currentMedia.width}px</p>
        </div>
        <div>
          <span class="text-gray-400">Output Format:</span>
          <p class="text-gray-600 uppercase">{currentMedia.outputFormat || "jpeg"}</p>
        </div>
        <div>
          <span class="text-gray-400">Created:</span>
          <p class="text-gray-600">{formatDateTime(currentMedia.createdAt)}</p>
        </div>
        <div>
          <span class="text-gray-400">Updated:</span>
          <p class="text-gray-600">{formatDateTime(currentMedia.updatedAt)}</p>
        </div>
      </div>
    </div>
  </div>
{/if}
