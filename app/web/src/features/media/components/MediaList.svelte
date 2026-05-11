<script lang="ts">
  import { formatFileSize, formatRelativeTime } from "../../../shared/utils";
  import {
    createMediaListQuery,
    createDeleteMutation,
    createUploadMutation,
    createPresignedUploadMutation,
    createAssetsMutation as buildAssetsMutation,
    PRESIGNED_UPLOAD_THRESHOLD,
  } from "../queries";
  import { invalidateMediaList } from "../../../shared/queries";
  import { currentMediaId, isProcessing } from "../stores";
  import type { MediaType, MediaSource, OutputFormat, CreateAssetRequest } from "../../../shared/types";

  const SOURCE_FILTERS: ReadonlyArray<{ value: MediaSource | undefined; label: string }> = [
    { value: undefined, label: "All" },
    { value: "upload", label: "Uploaded" },
    { value: "generated", label: "Generated" },
  ];

  interface Props {
    mediaType?: MediaType;
    source?: MediaSource;
    layout?: "list" | "grid";
  }

  let { mediaType, source: initialSource = undefined, layout = "list" }: Props = $props();

  let sourceFilter = $state<MediaSource | undefined>(initialSource);
  const mediaListQuery = $derived(createMediaListQuery({ mediaType, source: sourceFilter }));
  const deleteMutation = createDeleteMutation();
  const uploadMutation = createUploadMutation();
  const presignedUploadMutation = createPresignedUploadMutation();
  const assetsMutation = buildAssetsMutation();

  let deletingIds = $state<Set<string>>(new Set());
  let isDragging = $state(false);
  let isUploading = $state(false);
  let dragCounter = 0;
  let fileInput = $state<HTMLInputElement | undefined>(undefined);

  let mediaList = $derived(mediaListQuery.data?.items ?? []);
  let filteredList = $derived(
    mediaType ? mediaList.filter((item) => (item.mediaType || "image") === mediaType) : mediaList,
  );
  let listTitle = $derived(getListTitle(mediaType));

  function getListTitle(type?: MediaType): string {
    if (type === "image") return "Images";
    if (type === "document") return "Documents";
    if (type === "audio") return "Audio Overviews";
    return "All Media";
  }

  function lazyLoad(node: HTMLImageElement) {
    if (!("IntersectionObserver" in window)) return;
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) {
            const img = entry.target as HTMLImageElement;
            const src = img.dataset.src;
            if (src) {
              img.src = src;
              img.removeAttribute("data-src");
            }
            observer.unobserve(img);
          }
        }
      },
      { rootMargin: "200px" },
    );
    observer.observe(node);
    return { destroy: () => observer.disconnect() };
  }

  // Upload handlers (grid mode)
  async function handleUploadFile(file: File) {
    if (isUploading || $isProcessing) return;
    if (!file.type.startsWith("image/")) {
      alert("Please select an image file");
      return;
    }

    isUploading = true;
    isProcessing.set(true);
    try {
      let mediaId: string;
      if (file.size > PRESIGNED_UPLOAD_THRESHOLD) {
        const result = await presignedUploadMutation.mutateAsync({ file, mediaType: "image" });
        mediaId = result.mediaId;
      } else {
        const result = await uploadMutation.mutateAsync({ file, mediaType: "image" });
        mediaId = result.mediaId;
      }

      // Create a default JPEG output at 800px
      const outputs: CreateAssetRequest["outputs"] = [
        { operation: "image.process", outputFormat: "jpeg" as OutputFormat, width: 800 },
      ];
      await assetsMutation.mutateAsync({ mediaId, request: { outputs } });

      invalidateMediaList();
      currentMediaId.set(mediaId);
    } catch (error) {
      console.error("Upload error:", error);
      alert("Upload failed: " + (error instanceof Error ? error.message : "Unknown error"));
    } finally {
      isUploading = false;
      isProcessing.set(false);
      if (fileInput) fileInput.value = "";
    }
  }

  function handleFileSelect(e: Event) {
    const target = e.target as HTMLInputElement;
    const file = target.files?.[0];
    if (file) handleUploadFile(file);
  }

  // Drag & drop handlers (grid mode)
  function handleDragEnter(e: DragEvent) {
    e.preventDefault();
    e.stopPropagation();
    dragCounter += 1;
    if (e.dataTransfer?.items && e.dataTransfer.items.length > 0) isDragging = true;
  }

  function handleDragLeave(e: DragEvent) {
    e.preventDefault();
    e.stopPropagation();
    dragCounter -= 1;
    if (dragCounter === 0) isDragging = false;
  }

  function handleDragOver(e: DragEvent) {
    e.preventDefault();
    e.stopPropagation();
  }

  async function handleDrop(e: DragEvent) {
    e.preventDefault();
    e.stopPropagation();
    isDragging = false;
    dragCounter = 0;
    if (e.dataTransfer?.files && e.dataTransfer.files.length > 0) {
      await handleUploadFile(e.dataTransfer.files[0]);
    }
  }

  // Delete / view handlers
  async function handleDelete(mediaId: string) {
    const item = filteredList.find((m) => m.mediaId === mediaId);
    if (!item) return;
    if (deletingIds.has(mediaId)) return;
    if (item.status === "PROCESSING" || item.status === "PENDING" || item.status === "PENDING_UPLOAD") return;
    if (!confirm("Are you sure you want to delete this media?")) return;

    try {
      deletingIds = new Set([...deletingIds, mediaId]);
      await deleteMutation.mutateAsync(mediaId);
      if ($currentMediaId === mediaId) currentMediaId.set(null);
      mediaListQuery.refetch();
    } catch (error) {
      console.error("Delete error:", error);
      alert("Delete failed: " + (error instanceof Error ? error.message : "Unknown error"));
    } finally {
      deletingIds = new Set([...deletingIds].filter((id) => id !== mediaId));
    }
  }

  function handleView(mediaId: string) {
    const item = filteredList.find((m) => m.mediaId === mediaId);
    if (!item || deletingIds.has(mediaId)) return;
    currentMediaId.set(mediaId);
  }

  function handleCardKeydown(event: KeyboardEvent, mediaId: string) {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      handleView(mediaId);
    }
  }

  function handleRefresh() {
    mediaListQuery.refetch();
  }

  function isDeleting(mediaId: string): boolean {
    return deletingIds.has(mediaId);
  }

  function statusBadgeLabel(status: string): string {
    switch (status) {
      case "COMPLETE":
        return "READY";
      case "PENDING_UPLOAD":
        return "UPLOAD";
      case "PENDING":
        return "QUEUED";
      default:
        return status;
    }
  }

  function statusHint(status: string): string | null {
    if (status === "PENDING" || status === "PROCESSING") return "Asset jobs are still running";
    return null;
  }
</script>

{#if layout === "grid"}
  <!-- Grid Library View (lumina-style) -->
  <div
    role="region"
    class="flex-1 flex flex-col gap-3"
    ondragenter={handleDragEnter}
    ondragleave={handleDragLeave}
    ondragover={handleDragOver}
    ondrop={handleDrop}
  >
    {#if isDragging}
      <div
        class="absolute inset-0 z-50 bg-blue-50/90 backdrop-blur-sm border-2 border-dashed border-blue-500 rounded-xl flex flex-col items-center justify-center pointer-events-none"
      >
        <svg class="w-12 h-12 text-blue-500 mb-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path
            stroke-linecap="round"
            stroke-linejoin="round"
            stroke-width="1.5"
            d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12"
          />
        </svg>
        <h3 class="text-xl font-bold text-blue-900">Drop file to upload</h3>
      </div>
    {/if}

    <!-- Library Header -->
    <div class="flex items-center justify-between">
      <h2 class="text-2xl font-bold text-gray-900 tracking-tight">Library</h2>
      <div class="flex items-center gap-3">
        <div class="flex rounded-lg border border-gray-300 bg-white p-1" role="group" aria-label="Filter by source">
          {#each SOURCE_FILTERS as option (option.label)}
            <button
              type="button"
              onclick={() => (sourceFilter = option.value)}
              class="rounded-md px-3 py-1 text-xs font-medium transition-colors {sourceFilter === option.value
                ? 'bg-gray-900 text-white'
                : 'text-gray-600 hover:bg-gray-100'}"
              aria-pressed={sourceFilter === option.value}
            >
              {option.label}
            </button>
          {/each}
        </div>
        <button
          onclick={handleRefresh}
          disabled={mediaListQuery.isFetching}
          class="p-2 text-gray-400 hover:text-gray-600 hover:bg-gray-100 rounded-lg transition-colors"
          title="Refresh"
        >
          <svg
            class="w-4 h-4 {mediaListQuery.isFetching ? 'animate-spin' : ''}"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
            ></path>
          </svg>
        </button>
        <button
          onclick={() => fileInput?.click()}
          disabled={isUploading || $isProcessing}
          class="flex items-center gap-2 bg-gray-900 hover:bg-gray-800 text-white px-4 py-2 rounded-lg text-sm font-medium shadow-sm transition-colors disabled:opacity-50 cursor-pointer disabled:cursor-not-allowed"
        >
          {#if isUploading}
            <svg class="animate-spin h-4 w-4" fill="none" viewBox="0 0 24 24">
              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
              <path
                class="opacity-75"
                fill="currentColor"
                d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
              ></path>
            </svg>
            Uploading...
          {:else}
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
            </svg>
            Upload Image
          {/if}
        </button>
        <input bind:this={fileInput} type="file" class="hidden" accept="image/*" onchange={handleFileSelect} />
      </div>
    </div>

    <!-- Grid Container -->
    <div class="flex-1 bg-white rounded-lg border border-gray-200 shadow-sm overflow-hidden flex flex-col relative">
      {#if mediaListQuery.isLoading}
        <div class="absolute inset-0 flex items-center justify-center">
          <div class="animate-spin rounded-full h-8 w-8 border-b-2 border-gray-600"></div>
        </div>
      {:else if mediaListQuery.isError}
        <div class="absolute inset-0 flex items-center justify-center">
          <p class="text-sm text-red-500">Failed to load media</p>
        </div>
      {:else if filteredList.length === 0}
        <div
          class="absolute inset-0 flex flex-col items-center justify-center bg-gray-50/50 cursor-pointer"
          onclick={() => fileInput?.click()}
          role="button"
          tabindex="0"
          onkeydown={(e) => e.key === "Enter" && fileInput?.click()}
        >
          <div class="text-center opacity-60 hover:opacity-100 transition-opacity">
            <svg class="w-12 h-12 mx-auto text-gray-300 mb-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="1.5"
                d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z"
              />
            </svg>
            <p class="text-gray-500 text-sm">No images yet</p>
            <p class="text-gray-400 text-xs mt-1">Upload something or drag & drop!</p>
          </div>
        </div>
      {:else}
        <div
          class="p-3 overflow-y-auto grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-3 content-start h-full"
        >
          {#each filteredList as item (item.mediaId)}
            {@const isSelected = $currentMediaId === item.mediaId}
            <div
              class="group cursor-pointer rounded-lg border bg-white overflow-hidden transition-all duration-200 hover:shadow-lg relative {isSelected
                ? 'ring-2 ring-blue-600 border-transparent shadow-md'
                : 'border-gray-200'}"
              class:opacity-50={isDeleting(item.mediaId)}
              role="button"
              tabindex="0"
              onclick={() => handleView(item.mediaId)}
              onkeydown={(e) => handleCardKeydown(e, item.mediaId)}
            >
              <!-- Thumbnail -->
              <div class="aspect-4/3 bg-gray-100 relative overflow-hidden">
                {#if item.thumbnailUrl}
                  <img
                    data-src={item.thumbnailUrl}
                    use:lazyLoad
                    class="w-full h-full object-contain"
                    alt={item.name}
                    loading="lazy"
                  />
                {:else if item.status === "ERROR"}
                  <div class="w-full h-full flex items-center justify-center bg-gray-50">
                    <svg class="w-8 h-8 text-red-300" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path
                        stroke-linecap="round"
                        stroke-linejoin="round"
                        stroke-width="2"
                        d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
                      />
                    </svg>
                  </div>
                {:else if item.status === "PROCESSING" || item.status === "PENDING"}
                  <div class="w-full h-full flex items-center justify-center bg-gray-50">
                    <div class="text-center">
                      <svg class="w-8 h-8 text-blue-300 animate-spin mx-auto mb-2" fill="none" viewBox="0 0 24 24">
                        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"
                        ></circle>
                        <path
                          class="opacity-75"
                          fill="currentColor"
                          d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
                        ></path>
                      </svg>
                      <span class="text-xs text-blue-400 font-medium">Processing</span>
                    </div>
                  </div>
                {:else}
                  <div class="w-full h-full flex items-center justify-center bg-gray-50">
                    <svg class="w-8 h-8 text-gray-300" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path
                        stroke-linecap="round"
                        stroke-linejoin="round"
                        stroke-width="1.5"
                        d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z"
                      />
                    </svg>
                  </div>
                {/if}

                <!-- Status Badge Dot -->
                {#if item.status === "COMPLETE"}
                  <div
                    class="absolute top-2 right-2 w-2.5 h-2.5 bg-green-500 rounded-full shadow-sm border border-white"
                  ></div>
                {:else if item.status === "PROCESSING" || item.status === "PENDING"}
                  <div
                    class="absolute top-2 right-2 w-2.5 h-2.5 bg-blue-500 rounded-full shadow-sm border border-white animate-pulse"
                  ></div>
                {:else if item.status === "ERROR"}
                  <div
                    class="absolute top-2 right-2 w-2.5 h-2.5 bg-red-500 rounded-full shadow-sm border border-white"
                  ></div>
                {:else if item.status === "PENDING_UPLOAD"}
                  <div
                    class="absolute top-2 right-2 w-2.5 h-2.5 bg-purple-500 rounded-full shadow-sm border border-white"
                  ></div>
                {/if}

                <!-- Delete button on hover -->
                {#if !isDeleting(item.mediaId) && item.status !== "PROCESSING" && item.status !== "PENDING"}
                  <button
                    onclick={(e) => {
                      e.stopPropagation();
                      handleDelete(item.mediaId);
                    }}
                    class="absolute top-2 left-2 p-1 bg-white/80 backdrop-blur rounded-md text-gray-400 hover:text-red-500 opacity-0 group-hover:opacity-100 transition-opacity"
                    title="Delete"
                  >
                    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path
                        stroke-linecap="round"
                        stroke-linejoin="round"
                        stroke-width="2"
                        d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                      ></path>
                    </svg>
                  </button>
                {/if}
              </div>

              <!-- Card Info -->
              <div class="p-3">
                <p class="text-sm font-semibold text-gray-900 truncate">{item.name}</p>
                <p class="text-xs text-gray-500 mt-1 flex justify-between">
                  <span>{item.size ? formatFileSize(item.size) : "N/A"}</span>
                  <span class="uppercase">{item.mimetype?.split("/")[1] || "FILE"}</span>
                </p>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  </div>
{:else}
  <!-- Original List View (sidebar) -->
  <div class="card rounded-lg p-5 sticky top-6">
    <div class="flex items-center justify-between mb-4">
      <h2 class="text-base font-semibold text-gray-900">{listTitle}</h2>
      <div class="flex items-center space-x-2">
        <span class="text-xs text-gray-400">
          {filteredList.length}
          {filteredList.length === 1 ? "item" : "items"}
        </span>
        <button
          onclick={handleRefresh}
          class="text-gray-400 hover:text-gray-600"
          title="Refresh"
          disabled={mediaListQuery.isFetching}
        >
          <svg
            class="w-4 h-4 {mediaListQuery.isFetching ? 'animate-spin' : ''}"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
            ></path>
          </svg>
        </button>
      </div>
    </div>

    <div class="mb-3 flex rounded-lg border border-gray-300 bg-white p-1" role="group" aria-label="Filter by source">
      {#each SOURCE_FILTERS as option (option.label)}
        <button
          type="button"
          onclick={() => (sourceFilter = option.value)}
          class="flex-1 rounded-md px-2 py-1 text-xs font-medium transition-colors {sourceFilter === option.value
            ? 'bg-gray-900 text-white'
            : 'text-gray-600 hover:bg-gray-100'}"
          aria-pressed={sourceFilter === option.value}
        >
          {option.label}
        </button>
      {/each}
    </div>

    <div class="space-y-2 max-h-80 overflow-y-auto">
      {#if mediaListQuery.isLoading}
        <div class="flex items-center justify-center py-6">
          <div class="animate-spin rounded-full h-6 w-6 border-b-2 border-gray-600"></div>
        </div>
      {:else if mediaListQuery.isError}
        <p class="text-sm text-red-500 text-center py-6">Failed to load media</p>
      {:else if filteredList.length === 0}
        <p class="text-sm text-gray-400 text-center py-6">No uploads yet</p>
      {:else}
        {#each filteredList as item (item.mediaId)}
          <div
            class="p-3 rounded-lg border transition-colors {$currentMediaId === item.mediaId
              ? 'bg-blue-50 border-blue-200 shadow-sm ring-1 ring-blue-100'
              : 'bg-gray-50 border-transparent hover:bg-white hover:border-gray-200'}"
            class:opacity-50={isDeleting(item.mediaId)}
            class:cursor-pointer={!isDeleting(item.mediaId)}
            role="button"
            tabindex="0"
            onclick={() => handleView(item.mediaId)}
            onkeydown={(e) => handleCardKeydown(e, item.mediaId)}
          >
            <div class="flex items-center justify-between mb-1">
              <div class="flex-1 min-w-0 mr-3">
                <p class="text-sm text-gray-800 truncate font-medium">{item.name}</p>
              </div>
              <div class="flex items-center space-x-2">
                <span class="status-badge status-{item.status.toLowerCase()}">{statusBadgeLabel(item.status)}</span>
                {#if !isDeleting(item.mediaId)}
                  <button
                    onclick={(e) => {
                      e.stopPropagation();
                      handleDelete(item.mediaId);
                    }}
                    class="text-gray-400 hover:text-red-500 p-1"
                    title="Delete"
                  >
                    <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                      <path
                        stroke-linecap="round"
                        stroke-linejoin="round"
                        stroke-width="2"
                        d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
                      ></path>
                    </svg>
                  </button>
                {/if}
              </div>
            </div>
            <div class="text-xs text-gray-400 space-y-0.5">
              <p>{item.size ? formatFileSize(item.size) : "N/A"} · {item.mediaType || "image"}</p>
              {#if item.createdAt}
                <p>{formatRelativeTime(item.createdAt)}</p>
              {/if}
              {#if statusHint(item.status)}
                <p>{statusHint(item.status)}</p>
              {/if}
            </div>
          </div>
        {/each}
      {/if}
    </div>
  </div>
{/if}
