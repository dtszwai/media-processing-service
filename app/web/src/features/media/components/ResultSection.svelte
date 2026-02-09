<script lang="ts">
  import { formatFileSize, formatDateTime } from "../../../shared/utils";
  import {
    getDownloadUrl,
    getOriginalUrl,
    getPreviewUrl,
    pollForStatus,
    refreshPresignedUploadUrl,
    uploadToPresignedUrl,
    completePresignedUpload,
  } from "../services";
  import { createShortUrl, listShortUrls, revokeShortUrl } from "../../shorturl/services";
  import { createMediaListQuery, createResizeMutation, createRetryMutation } from "../queries";
  import { invalidateMediaList } from "../../../shared/queries";
  import { currentMediaId, isProcessing } from "../stores";
  import type { OutputFormat, MediaType, ShortUrlResponse, ShortUrlVariant } from "../../../shared/types";

  interface Props {
    mediaType?: MediaType;
  }

  let { mediaType }: Props = $props();

  let resizeWidth = $state(500);
  let resizeFormat = $state<OutputFormat>("jpeg");
  let isResizing = $state(false);
  let isResuming = $state(false);
  let isRetrying = $state(false);
  let resumeProgress = $state(0);
  let resumeFileInput: HTMLInputElement;
  let shortUrls = $state<ShortUrlResponse[]>([]);
  let shortUrlsLoading = $state(false);
  let shortUrlsError = $state<string | null>(null);
  let shortUrlVariant = $state<ShortUrlVariant>("preview");
  let shortUrlAlias = $state("");
  let shortUrlExpiresAt = $state("");
  let shortUrlLabel = $state("");
  let isCreatingShortUrl = $state(false);
  let revokeInProgress = $state<string | null>(null);
  let copiedCode = $state<string | null>(null);
  let shortUrlRequestId = 0;
  let showAdvancedShortUrlOptions = $state(false);
  let showAllActiveShortUrls = $state(false);
  let showAllInvalidShortUrls = $state(false);
  const MAX_VISIBLE_SHORT_URLS = 4;

  const mediaListQuery = createMediaListQuery(undefined, undefined, mediaType);
  const resizeMutation = createResizeMutation();
  const retryMutation = createRetryMutation();

  const formatOptions: { value: OutputFormat; label: string }[] = [
    { value: "jpeg", label: "JPEG" },
    { value: "png", label: "PNG" },
    { value: "webp", label: "WebP" },
  ];

  let mediaList = $derived(mediaListQuery.data?.items ?? []);
  let filteredList = $derived(
    mediaType
      ? mediaList.filter((item) => (item.mediaType || "image") === mediaType)
      : mediaList,
  );
  let currentMedia = $derived(filteredList.find((m) => m.mediaId === $currentMediaId) || null);
  let validShortUrls = $derived(shortUrls.filter((url) => !isInvalid(url)));
  let invalidShortUrls = $derived(shortUrls.filter((url) => isInvalid(url)));
  let visibleActiveShortUrls = $derived(
    showAllActiveShortUrls ? validShortUrls : validShortUrls.slice(0, MAX_VISIBLE_SHORT_URLS),
  );
  let visibleInvalidShortUrls = $derived(
    showAllInvalidShortUrls ? invalidShortUrls : invalidShortUrls.slice(0, MAX_VISIBLE_SHORT_URLS),
  );
  let availableVariants = $derived(
    currentMedia?.mediaType === "document"
      ? (["download", "original"] as ShortUrlVariant[])
      : (["preview", "download", "original"] as ShortUrlVariant[]),
  );

  $effect(() => {
    if (currentMedia && (currentMedia.mediaType === "image" || !currentMedia.mediaType)) {
      resizeWidth = currentMedia.width || 500;
      resizeFormat = currentMedia.outputFormat || "jpeg";
    }
  });

  $effect(() => {
    if (!currentMedia) {
      shortUrls = [];
      shortUrlsError = null;
      return;
    }
    if (!availableVariants.includes(shortUrlVariant)) {
      shortUrlVariant = availableVariants[0];
    }
  });

  $effect(() => {
    if (!currentMedia) return;
    const requestId = ++shortUrlRequestId;
    shortUrlsLoading = true;
    shortUrlsError = null;

    listShortUrls(currentMedia.mediaId)
      .then((items) => {
        if (requestId !== shortUrlRequestId) return;
        shortUrls = items;
      })
      .catch((error) => {
        if (requestId !== shortUrlRequestId) return;
        shortUrlsError = error instanceof Error ? error.message : "Failed to load short URLs";
        shortUrls = [];
      })
      .finally(() => {
        if (requestId !== shortUrlRequestId) return;
        shortUrlsLoading = false;
      });
  });

  $effect(() => {
    if (validShortUrls.length <= MAX_VISIBLE_SHORT_URLS) {
      showAllActiveShortUrls = false;
    }
    if (invalidShortUrls.length <= MAX_VISIBLE_SHORT_URLS) {
      showAllInvalidShortUrls = false;
    }
  });

  async function handleResize() {
    if (!currentMedia || currentMedia.mediaType === "document" || isResizing || $isProcessing) return;

    isResizing = true;

    try {
      await resizeMutation.mutateAsync({
        mediaId: currentMedia.mediaId,
        request: { width: resizeWidth, outputFormat: resizeFormat },
      });

      // Poll for completion and invalidate cache
      await pollForStatus(currentMedia.mediaId, ["COMPLETE", "ERROR"], () => {
        invalidateMediaList();
      });
    } catch (error) {
      console.error("Resize error:", error);
      alert("Resize failed: " + (error instanceof Error ? error.message : "Unknown error"));
    } finally {
      isResizing = false;
    }
  }

  function triggerResumeFileSelect() {
    if (isResuming || $isProcessing) return;
    resumeFileInput?.click();
  }

  async function handleResumeUpload(e: Event) {
    const target = e.target as HTMLInputElement;
    const file = target.files?.[0];
    if (!file || !currentMedia || isResuming) return;

    // Validate file type matches original
    if (file.type !== currentMedia.mimetype) {
      alert(`File type mismatch. Expected ${currentMedia.mimetype}, got ${file.type}`);
      target.value = "";
      return;
    }

    isResuming = true;
    resumeProgress = 0;
    isProcessing.set(true);

    try {
      // Get fresh presigned URL
      const uploadInfo = await refreshPresignedUploadUrl(currentMedia.mediaId);

      // Upload to S3
      await uploadToPresignedUrl(uploadInfo.uploadUrl, file, uploadInfo.headers, (progress) => {
        resumeProgress = progress;
      });

      // Complete the upload
      await completePresignedUpload(currentMedia.mediaId);

      // Poll for completion
      await pollForStatus(currentMedia.mediaId, ["COMPLETE", "ERROR"], () => {
        invalidateMediaList();
      });
    } catch (error) {
      console.error("Resume upload error:", error);
      alert("Resume upload failed: " + (error instanceof Error ? error.message : "Unknown error"));
    } finally {
      isResuming = false;
      resumeProgress = 0;
      isProcessing.set(false);
      target.value = "";
    }
  }

  async function handleRetry() {
    if (!currentMedia || isRetrying || $isProcessing) return;

    isRetrying = true;
    isProcessing.set(true);

    try {
      await retryMutation.mutateAsync(currentMedia.mediaId);

      // Poll for completion
      await pollForStatus(currentMedia.mediaId, ["COMPLETE", "ERROR"], () => {
        invalidateMediaList();
      });
    } catch (error) {
      console.error("Retry error:", error);
      alert("Retry failed: " + (error instanceof Error ? error.message : "Unknown error"));
    } finally {
      isRetrying = false;
      isProcessing.set(false);
    }
  }

  async function handleCreateShortUrl() {
    if (!currentMedia || isCreatingShortUrl) return;
    isCreatingShortUrl = true;
    shortUrlsError = null;

    const trimmedAlias = shortUrlAlias.trim();
    const trimmedLabel = shortUrlLabel.trim();
    const expiresAt = shortUrlExpiresAt ? new Date(shortUrlExpiresAt).toISOString() : undefined;

    try {
      const created = await createShortUrl({
        mediaId: currentMedia.mediaId,
        variant: shortUrlVariant,
        alias: trimmedAlias ? trimmedAlias : undefined,
        expiresAt,
        label: trimmedLabel ? trimmedLabel : undefined,
      });
      shortUrls = [created, ...shortUrls.filter((item) => item.code !== created.code)];
      shortUrlAlias = "";
      shortUrlLabel = "";
      shortUrlExpiresAt = "";
      showAdvancedShortUrlOptions = false;
    } catch (error) {
      shortUrlsError = error instanceof Error ? error.message : "Failed to create short URL";
    } finally {
      isCreatingShortUrl = false;
    }
  }

  async function handleRevokeShortUrl(code: string) {
    if (revokeInProgress) return;
    revokeInProgress = code;
    try {
      await revokeShortUrl(code);
      const revokedAt = new Date().toISOString();
      shortUrls = shortUrls.map((item) => (item.code === code ? { ...item, revokedAt } : item));
    } catch (error) {
      shortUrlsError = error instanceof Error ? error.message : "Failed to revoke short URL";
    } finally {
      revokeInProgress = null;
    }
  }

  async function handleCopyShortUrl(shortUrl: ShortUrlResponse) {
    const url = resolveShortUrlValue(shortUrl);
    try {
      await navigator.clipboard.writeText(url);
      copiedCode = shortUrl.code;
      setTimeout(() => {
        if (copiedCode === shortUrl.code) copiedCode = null;
      }, 1500);
    } catch (error) {
      console.warn("Failed to copy short URL", error);
      shortUrlsError = "Failed to copy short URL";
    }
  }

  function resolveShortUrlValue(shortUrl: ShortUrlResponse) {
    if (shortUrl.shortUrl) return shortUrl.shortUrl;
    if (typeof window !== "undefined") {
      return `${window.location.origin}/s/${shortUrl.code}`;
    }
    return `/s/${shortUrl.code}`;
  }

  function isRevoked(url: ShortUrlResponse) {
    return Boolean(url.revokedAt);
  }

  function isExpired(url: ShortUrlResponse) {
    if (!url.expiresAt) return false;
    const expiresAt = new Date(url.expiresAt).getTime();
    return !Number.isNaN(expiresAt) && expiresAt <= Date.now();
  }

  function isInvalid(url: ShortUrlResponse) {
    return isRevoked(url) || isExpired(url);
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
    {#if currentMedia.status === "COMPLETE" && currentMedia.mediaType !== "document"}
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

    <!-- Resume Upload Controls for PENDING_UPLOAD -->
    {#if currentMedia.status === "PENDING_UPLOAD"}
      <div class="mb-4 p-4 bg-amber-50 border border-amber-200 rounded-lg">
        <input
          type="file"
          accept={currentMedia.mimetype}
          class="hidden"
          bind:this={resumeFileInput}
          onchange={handleResumeUpload}
        />
        <div class="flex items-center justify-between">
          <div>
            <p class="text-sm font-medium text-amber-800">Upload incomplete</p>
            <p class="text-xs text-amber-600 mt-1">Select the same file to resume upload</p>
          </div>
          <button
            onclick={triggerResumeFileSelect}
            disabled={isResuming}
            class="btn-primary px-4 py-2 text-sm font-medium rounded-lg"
          >
            {#if isResuming}
              <svg
                class="animate-spin -ml-1 mr-2 h-4 w-4 text-white inline-block"
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
              Uploading...
            {:else}
              Resume Upload
            {/if}
          </button>
        </div>
        {#if isResuming && resumeProgress > 0}
          <div class="mt-3">
            <div class="flex justify-between text-xs text-amber-600 mb-1">
              <span>Uploading to S3...</span>
              <span>{resumeProgress.toFixed(0)}%</span>
            </div>
            <div class="h-2 bg-amber-200 rounded-full overflow-hidden">
              <div class="h-full bg-amber-500 transition-all duration-300" style="width: {resumeProgress}%"></div>
            </div>
          </div>
        {/if}
      </div>
    {/if}

    <!-- Retry Controls for PROCESSING or ERROR status -->
    {#if currentMedia.status === "PROCESSING" || currentMedia.status === "ERROR"}
      <div class="mb-4 p-4 bg-red-50 border border-red-200 rounded-lg">
        <div class="flex items-center justify-between">
          <div>
            <p class="text-sm font-medium text-red-800">
              {currentMedia.status === "ERROR" ? "Processing failed" : "Stuck in processing"}
            </p>
            <p class="text-xs text-red-600 mt-1">
              {currentMedia.status === "ERROR"
                ? "Click retry to reprocess this media"
                : "Processing is taking longer than expected. Try again?"}
            </p>
          </div>
          <button
            onclick={handleRetry}
            disabled={isRetrying}
            class="btn-primary px-4 py-2 text-sm font-medium rounded-lg"
          >
            {#if isRetrying}
              <svg
                class="animate-spin -ml-1 mr-2 h-4 w-4 text-white inline-block"
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
              Retrying...
            {:else}
              Retry
            {/if}
          </button>
        </div>
      </div>
    {/if}

    <!-- Media Preview -->
    {#if currentMedia.mediaType === "document"}
      <div class="mb-4">
        <p class="text-xs font-medium text-gray-500 mb-2">PDF Preview</p>
        {#if currentMedia.status === "PENDING_UPLOAD"}
          <div class="h-[240px] flex items-center justify-center text-gray-400 text-sm">
            <span>Awaiting upload...</span>
          </div>
        {:else if currentMedia.status === "COMPLETE"}
          <iframe
            title="PDF preview"
            src={getOriginalUrl(currentMedia.mediaId)}
            class="w-full h-[400px] border rounded"
          ></iframe>
        {:else}
          <div class="h-[240px] flex items-center justify-center text-gray-400 text-sm">
            {#if currentMedia.status === "PROCESSING"}
              <span class="pulse">Processing...</span>
            {:else if currentMedia.status === "PENDING"}
              <span>Pending...</span>
            {:else}
              <span>{currentMedia.status}</span>
            {/if}
          </div>
        {/if}
      </div>
    {:else}
      <div class="grid grid-cols-2 gap-4 mb-4">
        <div class="image-box">
          <p class="text-xs font-medium text-gray-500 mb-2">Original</p>
          {#if currentMedia.status === "PENDING_UPLOAD"}
            <div class="h-[180px] flex items-center justify-center text-gray-400 text-sm">
              <span>Awaiting upload...</span>
            </div>
          {:else}
            <img src={getOriginalUrl(currentMedia.mediaId)} alt="Original" />
          {/if}
          <div class="mt-2 space-y-0.5">
            <p class="text-xs text-gray-500">Size: {formatFileSize(currentMedia.size)}</p>
          </div>
        </div>
        <div class="image-box">
          <div class="flex items-center justify-between mb-2">
            <p class="text-xs font-medium text-gray-500">Processed</p>
            {#if currentMedia.status === "COMPLETE"}
              <span class="text-xs text-cyan-600 bg-cyan-50 px-2 py-0.5 rounded-full">CDN Preview</span>
            {/if}
          </div>
          {#if currentMedia.status === "COMPLETE"}
            <img src="{getPreviewUrl(currentMedia.mediaId)}?t={Date.now()}" alt="Processed" />
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
    {/if}

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
          <span class="text-gray-400">Media Type:</span>
          <p class="text-gray-600">{currentMedia.mediaType || "image"}</p>
        </div>
        {#if currentMedia.mediaType === "image"}
          <div>
            <span class="text-gray-400">Width:</span>
            <p class="text-gray-600">{currentMedia.width}px</p>
          </div>
          <div>
            <span class="text-gray-400">Output Format:</span>
            <p class="text-gray-600 uppercase">{currentMedia.outputFormat || "jpeg"}</p>
          </div>
        {/if}
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

    <!-- Short URLs -->
    <div class="mt-4 p-4 bg-gray-50 rounded-lg">
      <div class="flex items-start justify-between gap-4 mb-4">
        <div>
          <h3 class="text-sm font-semibold text-gray-900">Short URLs</h3>
          <p class="text-xs text-gray-500 mt-1">Create public links for this media and share instantly.</p>
        </div>
        <span class="text-xs text-gray-400 whitespace-nowrap">{shortUrls.length} links</span>
      </div>

      <div class="bg-white rounded-lg p-3 shadow-sm">
        <div class="flex flex-col md:flex-row md:items-end gap-3">
          <div class="flex-1">
            <label class="text-xs text-gray-500">Target</label>
            <select
              bind:value={shortUrlVariant}
              class="mt-1 w-full text-xs bg-white border border-gray-300 rounded px-2 py-2 text-gray-700"
            >
              {#each availableVariants as variant}
                <option value={variant}>{variant}</option>
              {/each}
            </select>
          </div>
          <div class="flex items-center gap-2 md:justify-end">
            <button
              onclick={() => (showAdvancedShortUrlOptions = !showAdvancedShortUrlOptions)}
              class="text-xs px-3 py-2 rounded bg-gray-100 text-gray-600 hover:bg-gray-200"
            >
              {showAdvancedShortUrlOptions ? "Hide options" : "Advanced options"}
            </button>
            <button
              onclick={handleCreateShortUrl}
              disabled={isCreatingShortUrl}
              class="btn-primary px-4 py-2 text-xs font-medium rounded-lg"
            >
              {#if isCreatingShortUrl}
                Creating...
              {:else}
                Create link
              {/if}
            </button>
          </div>
        </div>

        {#if showAdvancedShortUrlOptions}
          <div class="grid grid-cols-1 md:grid-cols-3 gap-3 mt-3">
            <div>
              <label class="text-xs text-gray-500">Alias (optional)</label>
              <input
                type="text"
                placeholder="my-link"
                bind:value={shortUrlAlias}
                oninput={(e) => (shortUrlAlias = (e.currentTarget as HTMLInputElement).value.toLowerCase())}
                class="mt-1 w-full text-xs bg-white border border-gray-300 rounded px-2 py-2 text-gray-700"
              />
              <p class="text-[11px] text-gray-400 mt-1">Lowercase, numbers, -, _</p>
            </div>
            <div>
              <label class="text-xs text-gray-500">Expires at (optional)</label>
              <input
                type="datetime-local"
                bind:value={shortUrlExpiresAt}
                class="mt-1 w-full text-xs bg-white border border-gray-300 rounded px-2 py-2 text-gray-700"
              />
            </div>
            <div>
              <label class="text-xs text-gray-500">Label (optional)</label>
              <input
                type="text"
                placeholder="Campaign name"
                bind:value={shortUrlLabel}
                class="mt-1 w-full text-xs bg-white border border-gray-300 rounded px-2 py-2 text-gray-700"
              />
            </div>
          </div>
        {/if}

        {#if shortUrlsError}
          <p class="text-xs text-red-500 mt-2">{shortUrlsError}</p>
        {/if}
      </div>

      <div class="mt-3">
        {#if shortUrlsLoading}
          <p class="text-xs text-gray-500">Loading short URLs...</p>
        {:else if shortUrls.length === 0}
          <div class="p-4 text-center text-xs text-gray-500 bg-white rounded-lg shadow-sm">
            No short URLs yet. Create one above.
          </div>
        {:else}
          {#if validShortUrls.length > 0}
            <div class="flex items-center justify-between mb-2">
              <div class="text-[11px] uppercase tracking-widest text-gray-400">Active</div>
              {#if validShortUrls.length > MAX_VISIBLE_SHORT_URLS}
                <button
                  onclick={() => (showAllActiveShortUrls = !showAllActiveShortUrls)}
                  class="text-[11px] text-gray-500 hover:text-gray-700"
                >
                  {#if showAllActiveShortUrls}
                    Show less
                  {:else}
                    Show all ({validShortUrls.length})
                  {/if}
                </button>
              {/if}
            </div>
            <div class="space-y-2 mb-4">
              {#each visibleActiveShortUrls as shortUrl (shortUrl.code)}
                <div class="flex flex-col md:flex-row md:items-center md:justify-between gap-3 p-3 bg-white rounded-lg shadow-sm">
                  <div class="min-w-0">
                    <div class="flex items-center gap-2">
                      <span class="text-[10px] uppercase tracking-widest text-gray-400">{shortUrl.variant}</span>
                    </div>
                    <a
                      href={resolveShortUrlValue(shortUrl)}
                      target="_blank"
                      rel="noopener noreferrer"
                      class="text-sm font-mono text-blue-700 hover:text-blue-800 truncate block"
                    >
                      {resolveShortUrlValue(shortUrl)}
                    </a>
                    <div class="text-[11px] text-gray-500 mt-1 space-x-2">
                      {#if shortUrl.label}
                        <span>Label: {shortUrl.label}</span>
                      {/if}
                      {#if shortUrl.expiresAt}
                        <span>Expires: {formatDateTime(shortUrl.expiresAt)}</span>
                      {/if}
                    </div>
                  </div>
                  <div class="flex items-center gap-2 md:justify-end">
                    <button
                      onclick={() => handleCopyShortUrl(shortUrl)}
                      class="text-xs px-2 py-1.5 rounded bg-gray-100 text-gray-600 hover:bg-gray-200"
                    >
                      {#if copiedCode === shortUrl.code}
                        Copied
                      {:else}
                        Copy
                      {/if}
                    </button>
                    <button
                      onclick={() => handleRevokeShortUrl(shortUrl.code)}
                      disabled={revokeInProgress === shortUrl.code}
                      class="text-xs px-2 py-1.5 rounded bg-red-50 text-red-600 hover:bg-red-100 disabled:opacity-50"
                    >
                      {#if revokeInProgress === shortUrl.code}
                        Revoking...
                      {:else}
                        Revoke
                      {/if}
                    </button>
                  </div>
                </div>
              {/each}
            </div>
          {/if}

          {#if invalidShortUrls.length > 0}
            <div class="flex items-center justify-between mb-2">
              <div class="text-[11px] uppercase tracking-widest text-gray-400">Expired or Revoked</div>
              {#if invalidShortUrls.length > MAX_VISIBLE_SHORT_URLS}
                <button
                  onclick={() => (showAllInvalidShortUrls = !showAllInvalidShortUrls)}
                  class="text-[11px] text-gray-500 hover:text-gray-700"
                >
                  {#if showAllInvalidShortUrls}
                    Show less
                  {:else}
                    Show all ({invalidShortUrls.length})
                  {/if}
                </button>
              {/if}
            </div>
            <div class="space-y-2">
              {#each visibleInvalidShortUrls as shortUrl (shortUrl.code)}
                <div
                  class="flex flex-col md:flex-row md:items-center md:justify-between gap-3 p-3 bg-white rounded-lg shadow-sm opacity-60"
                >
                <div class="min-w-0">
                  <div class="flex items-center gap-2">
                    <span class="text-[10px] uppercase tracking-widest text-gray-400">{shortUrl.variant}</span>
                    {#if isRevoked(shortUrl)}
                      <span class="text-[10px] uppercase tracking-widest text-red-500">Revoked</span>
                    {:else if isExpired(shortUrl)}
                      <span class="text-[10px] uppercase tracking-widest text-amber-500">Expired</span>
                    {/if}
                  </div>
                  <a
                    href={resolveShortUrlValue(shortUrl)}
                    target="_blank"
                    rel="noopener noreferrer"
                    class="text-sm font-mono text-blue-700 hover:text-blue-800 truncate block"
                  >
                    {resolveShortUrlValue(shortUrl)}
                  </a>
                  <div class="text-[11px] text-gray-500 mt-1 space-x-2">
                    {#if shortUrl.label}
                      <span>Label: {shortUrl.label}</span>
                    {/if}
                    {#if shortUrl.expiresAt}
                      <span>Expires: {formatDateTime(shortUrl.expiresAt)}</span>
                    {/if}
                  </div>
                </div>
                <div class="flex items-center gap-2 md:justify-end">
                  <button
                    onclick={() => handleCopyShortUrl(shortUrl)}
                    disabled={isInvalid(shortUrl)}
                    class="text-xs px-2 py-1.5 rounded bg-gray-100 text-gray-600 hover:bg-gray-200 disabled:opacity-50"
                  >
                    {#if copiedCode === shortUrl.code}
                      Copied
                    {:else}
                      Copy
                    {/if}
                  </button>
                  <button
                    onclick={() => handleRevokeShortUrl(shortUrl.code)}
                    disabled={isInvalid(shortUrl) || revokeInProgress === shortUrl.code}
                    class="text-xs px-2 py-1.5 rounded bg-red-50 text-red-600 hover:bg-red-100 disabled:opacity-50"
                  >
                    {#if revokeInProgress === shortUrl.code}
                      Revoking...
                    {:else}
                      Revoke
                    {/if}
                  </button>
                </div>
              </div>
            {/each}
          </div>
          {/if}
        {/if}
      </div>
    </div>
  </div>
{/if}
