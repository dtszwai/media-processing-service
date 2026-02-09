<script lang="ts">
  import { formatFileSize, formatDateTime } from "../../../shared/utils";
  import {
    fetchAssetDownloadUrl,
    refreshPresignedUploadUrl,
    uploadToPresignedUrl,
    completePresignedUpload,
  } from "../services";
  import { createShortUrl, listShortUrls, revokeShortUrl } from "../../shorturl/services";
  import {
    createMediaListQuery,
    createMediaAssetsQuery,
    createAssetsMutation as buildAssetsMutation,
    createAssetRetryMutation,
  } from "../queries";
  import { invalidateMediaList } from "../../../shared/queries";
  import { currentMediaId, isProcessing } from "../stores";
  import type {
    MediaType,
    OutputFormat,
    MediaAsset,
    ShortUrlResponse,
    CreateAssetRequest,
  } from "../../../shared/types";

  interface Props {
    mediaType?: MediaType;
  }

  interface AssetCollections {
    source: MediaAsset[];
    generated: MediaAsset[];
  }

  interface AssetStatusCounts {
    total: number;
    pendingUpload: number;
    pending: number;
    processing: number;
    complete: number;
    error: number;
  }

  interface WorkflowSummary {
    badgeClass: "pending_upload" | "processing" | "error" | "complete";
    badgeLabel: string;
    headline: string;
    detail: string;
    highlights: string[];
  }

  interface ShortUrlGroup {
    assetId: string;
    asset: MediaAsset | null;
    urls: ShortUrlResponse[];
  }

  const sourceAssetSelectId = "source-asset-select";
  const outputWidthId = "output-width-slider";
  const shareTargetSelectId = "share-target-select";
  const shareAliasInputId = "share-alias-input";
  const shareExpiryInputId = "share-expiry-input";
  const shareLabelInputId = "share-label-input";

  let { mediaType }: Props = $props();

  let width = $state(500);
  let selectedFormats = $state<OutputFormat[]>(["jpeg"]);
  let includePreview = $state(true);
  let includeText = $state(true);
  let selectedSourceAssetId = $state<string | null>(null);
  let isCreatingAssets = $state(false);
  let isRetryingAsset = $state<string | null>(null);

  let isResuming = $state(false);
  let resumeProgress = $state(0);
  let resumeFileInput = $state<HTMLInputElement | null>(null);

  let shortUrls = $state<ShortUrlResponse[]>([]);
  let shortUrlsLoading = $state(false);
  let shortUrlsError = $state<string | null>(null);
  let selectedShareAssetId = $state<string | null>(null);
  let shortUrlAlias = $state("");
  let shortUrlExpiresAt = $state("");
  let shortUrlLabel = $state("");
  let isCreatingShortUrl = $state(false);
  let revokeInProgress = $state<string | null>(null);
  let copiedCode = $state<string | null>(null);
  let showAdvancedShortUrlOptions = $state(false);
  let shortUrlRequestId = 0;
  let shareLinksSectionRef: HTMLDivElement | null = $state(null);

  let assetUrls = $state<Record<string, string>>({});
  let assetUrlErrors = $state<Record<string, string>>({});
  let assetUrlLoading = $state<Set<string>>(new Set());

  const mediaListQuery = createMediaListQuery(undefined, undefined, mediaType);
  let mediaList = $derived(mediaListQuery.data?.items ?? []);
  let filteredList = $derived(
    mediaType ? mediaList.filter((item) => (item.mediaType || "image") === mediaType) : mediaList,
  );
  let currentMedia = $derived(filteredList.find((item) => item.mediaId === $currentMediaId) || null);

  const assetsQuery = $derived(currentMedia ? createMediaAssetsQuery(currentMedia.mediaId) : null);
  let assets = $derived(assetsQuery?.data ?? []);
  let assetCollections = $derived(buildAssetCollections(assets, currentMedia?.originalAssetId ?? null));
  let visibleAssets = $derived([...assetCollections.source, ...assetCollections.generated]);

  let shareableAssets = $derived(visibleAssets.filter((asset) => asset.status === "COMPLETE"));

  let selectedShareAsset = $derived(
    selectedShareAssetId ? visibleAssets.find((asset) => asset.assetId === selectedShareAssetId) || null : null,
  );

  let assetStatusCounts = $derived(countAssetStatuses(visibleAssets));
  let workflowSummary = $derived(buildWorkflowSummary(assetStatusCounts));

  const assetsMutation = buildAssetsMutation();
  const retryAssetMutation = createAssetRetryMutation();

  const formatOptions: { value: OutputFormat; label: string }[] = [
    { value: "jpeg", label: "JPEG" },
    { value: "png", label: "PNG" },
    { value: "webp", label: "WebP" },
  ];

  $effect(() => {
    if (!currentMedia) {
      selectedSourceAssetId = null;
      return;
    }

    const currentSourceStillExists =
      selectedSourceAssetId && visibleAssets.some((asset) => asset.assetId === selectedSourceAssetId);

    if (currentSourceStillExists) return;

    selectedSourceAssetId =
      currentMedia.originalAssetId ||
      assetCollections.source[0]?.assetId ||
      assetCollections.generated[0]?.assetId ||
      null;
  });

  $effect(() => {
    if (!currentMedia) {
      shortUrls = [];
      shortUrlsError = null;
      return;
    }

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
    if (!currentMedia) {
      selectedShareAssetId = null;
      return;
    }

    const stillValid = selectedShareAssetId && shareableAssets.some((asset) => asset.assetId === selectedShareAssetId);
    if (stillValid) return;

    const preferredGenerated = shareableAssets.find(
      (asset) => !isSourceAsset(asset, currentMedia.originalAssetId || null),
    );
    selectedShareAssetId = preferredGenerated?.assetId || shareableAssets[0]?.assetId || null;
  });

  $effect(() => {
    if (!currentMedia) {
      assetUrls = {};
      assetUrlErrors = {};
      assetUrlLoading = new Set();
      return;
    }

    const previewCandidates = visibleAssets
      .filter((asset) => asset.status === "COMPLETE" && isImageAsset(asset))
      .slice(0, 8);

    for (const asset of previewCandidates) {
      if (assetUrls[asset.assetId]) continue;
      if (assetUrlLoading.has(asset.assetId)) continue;
      void ensureAssetUrl(asset);
    }
  });

  let activeShortUrls = $derived(shortUrls.filter((url) => !isInvalid(url)));
  let inactiveShortUrls = $derived(shortUrls.filter((url) => isInvalid(url)));
  let shortUrlCountsByAsset = $derived(countShortUrlsByAsset(activeShortUrls));
  let activeShortUrlGroups = $derived(groupShortUrlsByAsset(activeShortUrls, visibleAssets));
  let inactiveShortUrlGroups = $derived(groupShortUrlsByAsset(inactiveShortUrls, visibleAssets));
  let generatedAssetDisplayTitles = $derived(buildGeneratedAssetDisplayTitles(assetCollections.generated));

  function buildAssetCollections(items: MediaAsset[], originalAssetId: string | null): AssetCollections {
    const sorted = [...items].sort(sortByCreatedAtDesc);
    const source: MediaAsset[] = [];
    const generated: MediaAsset[] = [];

    for (const asset of sorted) {
      if (asset.status === "DELETED") continue;
      if (isSourceAsset(asset, originalAssetId)) {
        source.push(asset);
      } else {
        generated.push(asset);
      }
    }

    return { source, generated };
  }

  function sortByCreatedAtDesc(a: MediaAsset, b: MediaAsset) {
    if (!a.createdAt && !b.createdAt) return 0;
    if (!a.createdAt) return 1;
    if (!b.createdAt) return -1;
    return b.createdAt.localeCompare(a.createdAt);
  }

  function isSourceAsset(asset: MediaAsset, originalAssetId: string | null): boolean {
    if (originalAssetId && asset.assetId === originalAssetId) return true;
    if (asset.type === "ORIGINAL") return true;
    return Boolean(asset.tags?.includes("original"));
  }

  function countAssetStatuses(items: MediaAsset[]): AssetStatusCounts {
    return items.reduce<AssetStatusCounts>(
      (acc, asset) => {
        acc.total += 1;
        if (asset.status === "PENDING_UPLOAD") acc.pendingUpload += 1;
        if (asset.status === "PENDING") acc.pending += 1;
        if (asset.status === "PROCESSING") acc.processing += 1;
        if (asset.status === "COMPLETE") acc.complete += 1;
        if (asset.status === "ERROR") acc.error += 1;
        return acc;
      },
      {
        total: 0,
        pendingUpload: 0,
        pending: 0,
        processing: 0,
        complete: 0,
        error: 0,
      },
    );
  }

  function buildWorkflowSummary(counts: AssetStatusCounts): WorkflowSummary {
    const queued = counts.pendingUpload + counts.pending;
    const running = queued + counts.processing;
    const highlights: string[] = [];

    if (counts.processing > 0) highlights.push(`${counts.processing} processing`);
    if (queued > 0) highlights.push(`${queued} queued`);
    if (counts.error > 0) highlights.push(`${counts.error} failed`);
    if (running === 0 && counts.error === 0 && counts.complete > 0) {
      highlights.push(`${counts.complete} ready`);
    }

    if (counts.pendingUpload > 0) {
      return {
        badgeClass: "pending_upload",
        badgeLabel: "UPLOAD",
        headline: "Upload is still incomplete",
        detail: "Resume the upload to continue processing outputs.",
        highlights,
      };
    }

    if (running > 0) {
      return {
        badgeClass: "processing",
        badgeLabel: "PROCESSING",
        headline: `${running} asset job${running === 1 ? "" : "s"} running`,
        detail: "You can keep browsing. This page refreshes automatically.",
        highlights,
      };
    }

    if (counts.error > 0) {
      return {
        badgeClass: "error",
        badgeLabel: "ATTENTION",
        headline: counts.complete > 0 ? "Some assets failed" : "Processing failed",
        detail: "Retry only the failed assets from the gallery below.",
        highlights,
      };
    }

    return {
      badgeClass: "complete",
      badgeLabel: "READY",
      headline: counts.total > 0 ? `${counts.complete} assets ready` : "No assets yet",
      detail: "Create share links only for outputs you want to publish.",
      highlights,
    };
  }

  function countShortUrlsByAsset(urls: ShortUrlResponse[]) {
    const counts: Record<string, number> = {};
    for (const url of urls) {
      counts[url.assetId] = (counts[url.assetId] || 0) + 1;
    }
    return counts;
  }

  function groupShortUrlsByAsset(urls: ShortUrlResponse[], allAssets: MediaAsset[]): ShortUrlGroup[] {
    const grouped = new Map<string, ShortUrlResponse[]>();

    for (const url of urls) {
      const bucket = grouped.get(url.assetId);
      if (bucket) {
        bucket.push(url);
      } else {
        grouped.set(url.assetId, [url]);
      }
    }

    return Array.from(grouped.entries())
      .map(([assetId, assetUrls]) => {
        const asset = allAssets.find((item) => item.assetId === assetId) || null;
        const sortedUrls = [...assetUrls].sort((a, b) => {
          if (!a.createdAt && !b.createdAt) return 0;
          if (!a.createdAt) return 1;
          if (!b.createdAt) return -1;
          return b.createdAt.localeCompare(a.createdAt);
        });

        return {
          assetId,
          asset,
          urls: sortedUrls,
        };
      })
      .sort((a, b) => {
        const nameA = a.asset ? assetTitle(a.asset) : a.assetId;
        const nameB = b.asset ? assetTitle(b.asset) : b.assetId;
        return nameA.localeCompare(nameB);
      });
  }

  function buildGeneratedAssetDisplayTitles(items: MediaAsset[]) {
    const grouped = new Map<string, MediaAsset[]>();

    for (const asset of items) {
      const base = generatedBaseTitle(asset);
      const bucket = grouped.get(base);
      if (bucket) {
        bucket.push(asset);
      } else {
        grouped.set(base, [asset]);
      }
    }

    const titles: Record<string, string> = {};

    for (const [base, assetsForBase] of grouped.entries()) {
      const sorted = [...assetsForBase].sort(sortByCreatedAtDesc);
      if (sorted.length === 1) {
        titles[sorted[0].assetId] = base;
        continue;
      }

      sorted.forEach((asset, index) => {
        titles[asset.assetId] = `${base} #${index + 1}`;
      });
    }

    return titles;
  }

  function generatedBaseTitle(asset: MediaAsset) {
    if (asset.type === "TEXT" || asset.operation === "document.text") {
      return "Extracted text";
    }

    const format = assetOutputFormat(asset);
    const dimensions = assetDimensions(asset);
    const isPreview =
      asset.type === "PREVIEW" || asset.operation === "image.preview" || asset.operation === "document.preview";

    if (isPreview) {
      if (format && dimensions) return `Preview · ${format} · ${dimensions}`;
      if (format) return `Preview · ${format}`;
      if (dimensions) return `Preview · ${dimensions}`;
      return "Preview";
    }

    if (format && dimensions) return `${format} output · ${dimensions}`;
    if (format) return `${format} output`;
    if (dimensions) return `Output · ${dimensions}`;
    if (asset.operation) return titleCase(asset.operation);
    return "Output";
  }

  function assetKind(asset: MediaAsset): string {
    if (currentMedia && isSourceAsset(asset, currentMedia.originalAssetId || null)) return "Source";
    if (asset.type === "DERIVED") return "Output";
    if (asset.type === "PREVIEW") return "Preview";
    if (asset.type === "TEXT") return "Text";
    return "Asset";
  }

  function assetLabel(asset: MediaAsset) {
    if (currentMedia && isSourceAsset(asset, currentMedia.originalAssetId || null)) {
      return `Source · ${asset.downloadName || "Original file"}`;
    }
    return generatedAssetDisplayTitles[asset.assetId] || generatedBaseTitle(asset);
  }

  function assetTitle(asset: MediaAsset) {
    if (currentMedia && isSourceAsset(asset, currentMedia.originalAssetId || null)) {
      return asset.downloadName || "Original file";
    }
    return generatedAssetDisplayTitles[asset.assetId] || generatedBaseTitle(asset);
  }

  function titleCase(value: string) {
    return value
      .split(/[\s._-]+/)
      .filter(Boolean)
      .map((part) => part.charAt(0).toUpperCase() + part.slice(1).toLowerCase())
      .join(" ");
  }

  function assetOutputFormat(asset: MediaAsset): string | null {
    if (!asset.outputFormat) return null;
    return asset.outputFormat.toUpperCase();
  }

  function assetDimensions(asset: MediaAsset): string | null {
    if (asset.width && asset.height) {
      return `${asset.width}x${asset.height}`;
    }
    if (asset.width) {
      return `${asset.width}px`;
    }
    return null;
  }

  function assetMeta(asset: MediaAsset) {
    const format = assetOutputFormat(asset);
    const dimensions = assetDimensions(asset);
    const size = asset.size ? formatFileSize(asset.size) : null;
    const isSource = currentMedia ? isSourceAsset(asset, currentMedia.originalAssetId || null) : false;

    if (isSource) {
      return [format, dimensions, size].filter(Boolean).join(" · ");
    }

    // Generated titles already include format/dimensions, so keep the support line concise.
    return [size, dimensions, format].filter(Boolean).slice(0, 1).join(" · ");
  }

  function shortUrlTargetLabel(shortUrl: ShortUrlResponse) {
    const asset = visibleAssets.find((item) => item.assetId === shortUrl.assetId);
    if (!asset) return shortUrl.assetId;
    return assetTitle(asset);
  }

  function shortUrlTargetMeta(shortUrl: ShortUrlResponse) {
    const asset = visibleAssets.find((item) => item.assetId === shortUrl.assetId);
    if (!asset) return null;
    return assetMeta(asset) || null;
  }

  function selectAssetForSharing(asset: MediaAsset) {
    if (asset.status !== "COMPLETE") return;
    selectedShareAssetId = asset.assetId;
    shareLinksSectionRef?.scrollIntoView({ behavior: "smooth", block: "start" });
  }

  async function ensureAssetUrl(asset: MediaAsset): Promise<string | null> {
    if (!currentMedia || asset.status !== "COMPLETE") return null;
    if (assetUrls[asset.assetId]) return assetUrls[asset.assetId];
    if (assetUrlLoading.has(asset.assetId)) return null;

    assetUrlLoading = new Set([...assetUrlLoading, asset.assetId]);

    try {
      const url = await fetchAssetDownloadUrl(currentMedia.mediaId, asset.assetId);
      if (!url) return null;
      assetUrls = { ...assetUrls, [asset.assetId]: url };
      return url;
    } catch (error) {
      assetUrlErrors = {
        ...assetUrlErrors,
        [asset.assetId]: error instanceof Error ? error.message : "Failed to fetch URL",
      };
      return null;
    } finally {
      assetUrlLoading = new Set([...assetUrlLoading].filter((id) => id !== asset.assetId));
    }
  }

  async function handleOpenAsset(asset: MediaAsset) {
    const url = await ensureAssetUrl(asset);
    if (url) window.open(url, "_blank");
  }

  function isImageAsset(asset: MediaAsset | null) {
    if (!asset) return false;
    if (asset.mimetype) {
      return asset.mimetype.startsWith("image/");
    }
    return (currentMedia?.mediaType || "image") === "image";
  }

  async function handleCreateAssets() {
    if (!currentMedia || isCreatingAssets || $isProcessing) return;

    if ((currentMedia.mediaType || "image") === "image" && selectedFormats.length === 0) {
      alert("Select at least one output format.");
      return;
    }

    isCreatingAssets = true;
    isProcessing.set(true);

    try {
      const outputs: CreateAssetRequest["outputs"] = [];

      if ((currentMedia.mediaType || "image") === "image") {
        for (const format of selectedFormats) {
          outputs.push({
            operation: "image.process",
            outputFormat: format,
            width,
          });
        }
      } else if (currentMedia.mediaType === "document") {
        if (includePreview) outputs.push({ operation: "document.preview" });
        if (includeText) outputs.push({ operation: "document.text" });
      }

      if (outputs.length === 0) {
        alert("Select at least one output.");
        return;
      }

      const request: CreateAssetRequest = {
        sourceAssetId: selectedSourceAssetId || undefined,
        outputs,
      };

      await assetsMutation.mutateAsync({ mediaId: currentMedia.mediaId, request });
      invalidateMediaList();
      await assetsQuery?.refetch();
    } catch (error) {
      console.error("Create asset error:", error);
      alert("Failed to create assets: " + (error instanceof Error ? error.message : "Unknown error"));
    } finally {
      isCreatingAssets = false;
      isProcessing.set(false);
    }
  }

  async function handleRetryAsset(asset: MediaAsset) {
    if (!currentMedia || isRetryingAsset || $isProcessing) return;

    isRetryingAsset = asset.assetId;
    isProcessing.set(true);

    try {
      await retryAssetMutation.mutateAsync({ mediaId: currentMedia.mediaId, assetId: asset.assetId });
      invalidateMediaList();
      await assetsQuery?.refetch();
    } catch (error) {
      console.error("Retry asset error:", error);
      alert("Retry failed: " + (error instanceof Error ? error.message : "Unknown error"));
    } finally {
      isRetryingAsset = null;
      isProcessing.set(false);
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

    if (file.type !== currentMedia.mimetype) {
      alert(`File type mismatch. Expected ${currentMedia.mimetype}, got ${file.type}`);
      target.value = "";
      return;
    }

    isResuming = true;
    resumeProgress = 0;
    isProcessing.set(true);

    try {
      const uploadInfo = await refreshPresignedUploadUrl(currentMedia.mediaId);
      await uploadToPresignedUrl(uploadInfo.uploadUrl, file, uploadInfo.headers, (progress) => {
        resumeProgress = progress;
      });
      await completePresignedUpload(currentMedia.mediaId);
      invalidateMediaList();
      await assetsQuery?.refetch();
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

  async function handleCreateShortUrl() {
    if (!selectedShareAssetId) {
      shortUrlsError = "Select an asset to share first.";
      return;
    }

    const alias = shortUrlAlias.trim() || undefined;
    const label = shortUrlLabel.trim() || undefined;
    const expiresAt = shortUrlExpiresAt ? new Date(shortUrlExpiresAt).toISOString() : undefined;

    await createShortUrlRequest(selectedShareAssetId, alias, expiresAt, label);
  }

  async function createShortUrlRequest(assetId: string, alias?: string, expiresAt?: string, label?: string) {
    if (!currentMedia || isCreatingShortUrl) return;

    isCreatingShortUrl = true;
    shortUrlsError = null;

    try {
      const created = await createShortUrl({
        mediaId: currentMedia.mediaId,
        assetId,
        alias,
        expiresAt,
        label,
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

  async function handleCreateAnotherLink(assetId: string) {
    selectedShareAssetId = assetId;
    await createShortUrlRequest(assetId, undefined, undefined, undefined);
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
  <div class="card rounded-lg p-6 space-y-4">
    <div class="flex items-center justify-between">
      <div>
        <h2 class="text-base font-semibold text-gray-900">Media Lab</h2>
        <p class="text-xs text-gray-500 mt-0.5">{currentMedia.name}</p>
      </div>
      <span class="status-badge status-{workflowSummary.badgeClass}">{workflowSummary.badgeLabel}</span>
    </div>

    <div class="rounded-xl border border-gray-200 bg-linear-to-r from-slate-50 to-white p-4">
      <p class="text-sm font-semibold text-gray-900">{workflowSummary.headline}</p>
      <p class="text-xs text-gray-500 mt-1">{workflowSummary.detail}</p>
      {#if workflowSummary.highlights.length > 0}
        <div class="mt-3 flex flex-wrap gap-2">
          {#each workflowSummary.highlights as item}
            <span class="text-[11px] px-2.5 py-1 rounded-full bg-white border border-gray-200 text-gray-600">
              {item}
            </span>
          {/each}
        </div>
      {/if}
    </div>

    {#if currentMedia.status === "PENDING_UPLOAD"}
      <div class="p-4 bg-amber-50 border border-amber-200 rounded-lg">
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

    <div class="p-4 bg-gray-50 rounded-lg border border-gray-200">
      <div class="flex items-center justify-between mb-3">
        <h3 class="text-sm font-semibold text-gray-900">Create Outputs</h3>
        <span class="text-xs text-gray-500">{visibleAssets.length} assets</span>
      </div>
      <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
        <div>
          <label for={sourceAssetSelectId} class="text-xs text-gray-500">Source Asset</label>
          <select
            id={sourceAssetSelectId}
            bind:value={selectedSourceAssetId}
            class="mt-1 w-full text-xs bg-white border border-gray-300 rounded px-2 py-2 text-gray-700"
          >
            {#each visibleAssets as asset}
              <option value={asset.assetId}>{assetLabel(asset)}</option>
            {/each}
          </select>
        </div>
        {#if (currentMedia.mediaType || "image") === "image"}
          <div>
            <label for={outputWidthId} class="text-xs text-gray-500">Width</label>
            <input id={outputWidthId} type="range" min="100" max="1024" bind:value={width} class="w-full mt-2" />
            <div class="text-xs text-gray-500 mt-1">{width}px</div>
          </div>
          <fieldset>
            <legend class="text-xs text-gray-500">Formats</legend>
            <div class="mt-2 flex flex-wrap gap-3">
              {#each formatOptions as option}
                <label class="inline-flex items-center gap-2 text-xs text-gray-700">
                  <input
                    type="checkbox"
                    value={option.value}
                    checked={selectedFormats.includes(option.value)}
                    onchange={(e) => {
                      const target = e.currentTarget as HTMLInputElement;
                      const value = target.value as OutputFormat;
                      if (target.checked) {
                        selectedFormats = Array.from(new Set([...selectedFormats, value]));
                      } else {
                        selectedFormats = selectedFormats.filter((format) => format !== value);
                      }
                    }}
                    class="rounded border-gray-300 text-blue-600 focus:ring-blue-500"
                  />
                  {option.label}
                </label>
              {/each}
            </div>
          </fieldset>
        {:else}
          <fieldset>
            <legend class="text-xs text-gray-500">Outputs</legend>
            <div class="mt-2 flex flex-col gap-2">
              <label class="inline-flex items-center gap-2 text-xs text-gray-700">
                <input type="checkbox" bind:checked={includePreview} class="rounded border-gray-300 text-blue-600" />
                Generate preview
              </label>
              <label class="inline-flex items-center gap-2 text-xs text-gray-700">
                <input type="checkbox" bind:checked={includeText} class="rounded border-gray-300 text-blue-600" />
                Extract text
              </label>
            </div>
          </fieldset>
        {/if}
      </div>
      <div class="mt-3">
        <button
          onclick={handleCreateAssets}
          disabled={isCreatingAssets || $isProcessing}
          class="btn-primary px-4 py-2 text-xs font-medium rounded-lg"
        >
          {#if isCreatingAssets}
            Creating...
          {:else}
            Create Outputs
          {/if}
        </button>
      </div>
    </div>

    <div class="space-y-3">
      <div class="flex items-center justify-between">
        <h3 class="text-sm font-semibold text-gray-900">Asset Gallery</h3>
        <span class="text-xs text-gray-400">{visibleAssets.length} assets</span>
      </div>

      {#if visibleAssets.length === 0}
        <div class="text-xs text-gray-500">No assets yet.</div>
      {:else}
        {#if assetCollections.source.length > 0}
          <div>
            <div class="text-[11px] uppercase tracking-widest text-gray-400 mb-2">Source</div>
            <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
              {#each assetCollections.source as asset (asset.assetId)}
                <article
                  class="rounded-xl border p-3 bg-white {selectedShareAssetId === asset.assetId
                    ? 'ring-1 ring-blue-200 border-blue-200'
                    : 'border-gray-200'}"
                >
                  <div class="flex items-start justify-between gap-2">
                    <div>
                      <div class="text-[11px] uppercase tracking-widest text-gray-400">{assetKind(asset)}</div>
                      <h4 class="text-sm font-semibold text-gray-800 truncate">{assetTitle(asset)}</h4>
                      <p class="text-xs text-gray-500">{assetMeta(asset) || "—"}</p>
                    </div>
                    <span class="status-badge status-{asset.status.toLowerCase()}">{asset.status}</span>
                  </div>

                  {#if asset.status === "COMPLETE" && isImageAsset(asset) && assetUrls[asset.assetId]}
                    <img
                      src={assetUrls[asset.assetId]}
                      alt={assetTitle(asset)}
                      class="w-full h-28 object-cover rounded-lg border border-gray-100 mt-3"
                    />
                  {/if}

                  {#if asset.status === "ERROR" && asset.errorMessage}
                    <p class="mt-2 text-xs text-red-600 bg-red-50 border border-red-100 rounded p-2">
                      {asset.errorMessage}
                    </p>
                  {/if}

                  <div class="mt-3 flex flex-wrap gap-2">
                    {#if asset.status === "COMPLETE"}
                      <button
                        onclick={() => handleOpenAsset(asset)}
                        disabled={assetUrlLoading.has(asset.assetId)}
                        class="text-xs px-3 py-1.5 rounded bg-blue-50 text-blue-600 hover:bg-blue-100"
                      >
                        {#if assetUrlLoading.has(asset.assetId)}
                          Preparing...
                        {:else}
                          Open file
                        {/if}
                      </button>
                      <button
                        onclick={() => selectAssetForSharing(asset)}
                        class="text-xs px-3 py-1.5 rounded bg-gray-100 text-gray-600 hover:bg-gray-200"
                      >
                        {selectedShareAssetId === asset.assetId ? "Selected for links" : "Use for links"}
                      </button>
                    {/if}
                    {#if asset.status === "ERROR"}
                      <button
                        onclick={() => handleRetryAsset(asset)}
                        disabled={isRetryingAsset === asset.assetId}
                        class="text-xs px-3 py-1.5 rounded bg-red-50 text-red-600 hover:bg-red-100"
                      >
                        {#if isRetryingAsset === asset.assetId}
                          Retrying...
                        {:else}
                          Retry
                        {/if}
                      </button>
                    {/if}
                  </div>
                </article>
              {/each}
            </div>
          </div>
        {/if}

        {#if assetCollections.generated.length > 0}
          <div>
            <div class="text-[11px] uppercase tracking-widest text-gray-400 mb-2">Generated</div>
            <div class="grid grid-cols-1 md:grid-cols-2 gap-3">
              {#each assetCollections.generated as asset (asset.assetId)}
                <article
                  class="rounded-xl border p-3 bg-white {selectedShareAssetId === asset.assetId
                    ? 'ring-1 ring-blue-200 border-blue-200'
                    : 'border-gray-200'}"
                >
                  <div class="flex items-start justify-between gap-2">
                    <div>
                      <div class="text-[11px] uppercase tracking-widest text-gray-400">{assetKind(asset)}</div>
                      <h4 class="text-sm font-semibold text-gray-800 truncate">{assetTitle(asset)}</h4>
                      <p class="text-xs text-gray-500">{assetMeta(asset) || "—"}</p>
                      {#if shortUrlCountsByAsset[asset.assetId]}
                        <p class="text-[11px] text-gray-400 mt-1">
                          {shortUrlCountsByAsset[asset.assetId]} active link{shortUrlCountsByAsset[asset.assetId] === 1
                            ? ""
                            : "s"}
                        </p>
                      {/if}
                    </div>
                    <span class="status-badge status-{asset.status.toLowerCase()}">{asset.status}</span>
                  </div>

                  {#if asset.status === "COMPLETE" && isImageAsset(asset) && assetUrls[asset.assetId]}
                    <img
                      src={assetUrls[asset.assetId]}
                      alt={assetTitle(asset)}
                      class="w-full h-28 object-cover rounded-lg border border-gray-100 mt-3"
                    />
                  {/if}

                  {#if asset.status === "ERROR" && asset.errorMessage}
                    <p class="mt-2 text-xs text-red-600 bg-red-50 border border-red-100 rounded p-2">
                      {asset.errorMessage}
                    </p>
                  {/if}

                  <div class="mt-3 flex flex-wrap gap-2">
                    {#if asset.status === "COMPLETE"}
                      <button
                        onclick={() => handleOpenAsset(asset)}
                        disabled={assetUrlLoading.has(asset.assetId)}
                        class="text-xs px-3 py-1.5 rounded bg-blue-50 text-blue-600 hover:bg-blue-100"
                      >
                        {#if assetUrlLoading.has(asset.assetId)}
                          Preparing...
                        {:else}
                          Open file
                        {/if}
                      </button>
                      <button
                        onclick={() => selectAssetForSharing(asset)}
                        class="text-xs px-3 py-1.5 rounded bg-gray-100 text-gray-600 hover:bg-gray-200"
                      >
                        {selectedShareAssetId === asset.assetId ? "Selected for links" : "Use for links"}
                      </button>
                    {/if}
                    {#if asset.status === "ERROR"}
                      <button
                        onclick={() => handleRetryAsset(asset)}
                        disabled={isRetryingAsset === asset.assetId}
                        class="text-xs px-3 py-1.5 rounded bg-red-50 text-red-600 hover:bg-red-100"
                      >
                        {#if isRetryingAsset === asset.assetId}
                          Retrying...
                        {:else}
                          Retry
                        {/if}
                      </button>
                    {/if}
                  </div>
                </article>
              {/each}
            </div>
          </div>
        {/if}
      {/if}
    </div>

    <div class="p-4 bg-gray-50 rounded-xl border border-gray-200" bind:this={shareLinksSectionRef}>
      <div class="flex items-start justify-between gap-4 mb-4">
        <div>
          <h3 class="text-sm font-semibold text-gray-900">Share Links</h3>
          <p class="text-xs text-gray-500 mt-1">Create and manage public links per asset from one place.</p>
        </div>
        <span class="text-xs text-gray-400 whitespace-nowrap">{activeShortUrls.length} active</span>
      </div>

      <div class="bg-white border border-gray-200 rounded-lg p-3">
        <div class="grid grid-cols-1 md:grid-cols-[1fr_auto] gap-3">
          <div>
            <label for={shareTargetSelectId} class="text-xs text-gray-500">Asset to share</label>
            <select
              id={shareTargetSelectId}
              bind:value={selectedShareAssetId}
              class="mt-1 w-full text-xs bg-white border border-gray-300 rounded px-2 py-2 text-gray-700"
            >
              {#if shareableAssets.length === 0}
                <option value="">No complete assets available</option>
              {:else}
                {#each shareableAssets as asset}
                  <option value={asset.assetId}>{assetTitle(asset)} ({assetMeta(asset) || "—"})</option>
                {/each}
              {/if}
            </select>
            {#if selectedShareAsset}
              <p class="text-[11px] text-gray-500 mt-1">
                {assetKind(selectedShareAsset)} · {assetMeta(selectedShareAsset) || "No metadata"}
              </p>
            {/if}
          </div>
          <div class="flex items-end gap-2">
            <button
              onclick={() => (showAdvancedShortUrlOptions = !showAdvancedShortUrlOptions)}
              class="text-xs px-3 py-2 rounded bg-gray-100 text-gray-600 hover:bg-gray-200"
            >
              {showAdvancedShortUrlOptions ? "Hide options" : "Advanced options"}
            </button>
            <button
              onclick={handleCreateShortUrl}
              disabled={isCreatingShortUrl || !selectedShareAssetId}
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
              <label for={shareAliasInputId} class="text-xs text-gray-500">Alias (optional)</label>
              <input
                id={shareAliasInputId}
                type="text"
                placeholder="my-link"
                bind:value={shortUrlAlias}
                oninput={(e) => (shortUrlAlias = (e.currentTarget as HTMLInputElement).value.toLowerCase())}
                class="mt-1 w-full text-xs bg-white border border-gray-300 rounded px-2 py-2 text-gray-700"
              />
              <p class="text-[11px] text-gray-400 mt-1">Lowercase, numbers, -, _</p>
            </div>
            <div>
              <label for={shareExpiryInputId} class="text-xs text-gray-500">Expires at (optional)</label>
              <input
                id={shareExpiryInputId}
                type="datetime-local"
                bind:value={shortUrlExpiresAt}
                class="mt-1 w-full text-xs bg-white border border-gray-300 rounded px-2 py-2 text-gray-700"
              />
            </div>
            <div>
              <label for={shareLabelInputId} class="text-xs text-gray-500">Label (optional)</label>
              <input
                id={shareLabelInputId}
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
        {:else if activeShortUrlGroups.length === 0}
          <div class="p-4 text-center text-xs text-gray-500 bg-white rounded-lg border border-gray-200">
            No active links yet. Pick an asset and create one.
          </div>
        {:else}
          <div class="space-y-3">
            {#each activeShortUrlGroups as group (group.assetId)}
              <div class="bg-white rounded-lg border border-gray-200 p-3">
                <div class="flex flex-col md:flex-row md:items-center md:justify-between gap-2">
                  <div>
                    <div class="text-sm font-medium text-gray-800">
                      {group.asset ? assetTitle(group.asset) : group.assetId}
                    </div>
                    <div class="text-xs text-gray-500">
                      {group.asset ? assetMeta(group.asset) || "—" : "Asset unavailable"}
                    </div>
                  </div>
                  <button
                    onclick={() => handleCreateAnotherLink(group.assetId)}
                    disabled={isCreatingShortUrl}
                    class="text-xs px-3 py-1.5 rounded bg-gray-100 text-gray-600 hover:bg-gray-200"
                  >
                    Create another link
                  </button>
                </div>
                <div class="mt-3 space-y-2">
                  {#each group.urls as shortUrl (shortUrl.code)}
                    <div
                      class="flex flex-col md:flex-row md:items-center md:justify-between gap-2 p-2 rounded border border-gray-100"
                    >
                      <div class="min-w-0">
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
              </div>
            {/each}
          </div>
        {/if}

        {#if inactiveShortUrlGroups.length > 0}
          <details class="mt-3 bg-white rounded-lg border border-gray-200 p-3">
            <summary class="cursor-pointer text-xs text-gray-600">
              Inactive links ({inactiveShortUrls.length})
            </summary>
            <div class="mt-3 space-y-3">
              {#each inactiveShortUrlGroups as group (group.assetId)}
                <div>
                  <div class="text-xs font-medium text-gray-700">
                    {group.asset ? assetTitle(group.asset) : group.assetId}
                  </div>
                  <div class="mt-2 space-y-2">
                    {#each group.urls as shortUrl (shortUrl.code)}
                      <div class="p-2 rounded border border-gray-100 opacity-70">
                        <div class="flex items-center gap-2 text-[10px] uppercase tracking-widest text-gray-400">
                          {#if isRevoked(shortUrl)}
                            <span class="text-red-500">Revoked</span>
                          {:else if isExpired(shortUrl)}
                            <span class="text-amber-500">Expired</span>
                          {/if}
                        </div>
                        <a
                          href={resolveShortUrlValue(shortUrl)}
                          target="_blank"
                          rel="noopener noreferrer"
                          class="text-sm font-mono text-blue-700 hover:text-blue-800 truncate block mt-1"
                        >
                          {resolveShortUrlValue(shortUrl)}
                        </a>
                        <div class="text-[11px] text-gray-500 mt-1">
                          {shortUrlTargetLabel(shortUrl)}
                          {#if shortUrlTargetMeta(shortUrl)}
                            · {shortUrlTargetMeta(shortUrl)}
                          {/if}
                        </div>
                      </div>
                    {/each}
                  </div>
                </div>
              {/each}
            </div>
          </details>
        {/if}
      </div>
    </div>

    <div class="p-3 bg-gray-50 rounded-lg">
      <div class="grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
        <div>
          <span class="text-gray-400">Media lifecycle:</span>
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
