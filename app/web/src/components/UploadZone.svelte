<script lang="ts">
  import { formatFileSize } from '../lib/utils';
  import {
    uploadMedia,
    initPresignedUpload,
    uploadToPresignedUrl,
    completePresignedUpload,
    pollForStatus,
    PRESIGNED_UPLOAD_THRESHOLD,
  } from '../lib/api';
  import {
    isProcessing,
    currentMediaId,
    addMedia,
    updateMediaStatus,
  } from '../lib/stores';
  import type { Media, OutputFormat } from '../lib/types';

  let selectedFile: File | null = $state(null);
  let previewUrl: string | null = $state(null);
  let dragover = $state(false);
  let width = $state(500);
  let outputFormat = $state<OutputFormat>('jpeg');
  let uploadProgress = $state(0);
  let uploadMethod = $state<'direct' | 'presigned' | null>(null);

  let fileInput: HTMLInputElement;

  const formatOptions: { value: OutputFormat; label: string }[] = [
    { value: 'jpeg', label: 'JPEG' },
    { value: 'png', label: 'PNG' },
    { value: 'webp', label: 'WebP' },
  ];

  function handleDragOver(e: DragEvent) {
    e.preventDefault();
    if (!$isProcessing) dragover = true;
  }

  function handleDragLeave() {
    dragover = false;
  }

  function handleDrop(e: DragEvent) {
    e.preventDefault();
    dragover = false;
    if ($isProcessing) return;

    const files = e.dataTransfer?.files;
    if (files?.length) handleFile(files[0]);
  }

  function triggerFileSelect() {
    if ($isProcessing) return;
    fileInput?.click();
  }

  function handleFileSelect(e: Event) {
    const target = e.target as HTMLInputElement;
    const file = target.files?.[0];
    if (file) handleFile(file);
  }

  function handleFile(file: File) {
    if ($isProcessing) return;
    if (!file.type.startsWith('image/')) {
      alert('Please select an image file');
      return;
    }

    selectedFile = file;

    const reader = new FileReader();
    reader.onload = (e) => {
      previewUrl = e.target?.result as string;
    };
    reader.readAsDataURL(file);

    // Determine upload method based on file size
    uploadMethod = file.size > PRESIGNED_UPLOAD_THRESHOLD ? 'presigned' : 'direct';
  }

  function clearFile() {
    selectedFile = null;
    previewUrl = null;
    uploadProgress = 0;
    uploadMethod = null;
    if (fileInput) fileInput.value = '';
  }

  function createMediaEntry(mediaId: string, status: Media['status']): Media {
    return {
      mediaId,
      name: selectedFile!.name,
      size: selectedFile!.size,
      mimetype: selectedFile!.type,
      status,
      width,
      outputFormat,
    };
  }

  async function handleUpload() {
    if (!selectedFile || $isProcessing) return;

    isProcessing.set(true);
    uploadProgress = 0;

    try {
      const mediaId = selectedFile.size > PRESIGNED_UPLOAD_THRESHOLD
        ? await uploadWithPresignedUrl()
        : await uploadDirect();

      await pollForStatus(mediaId, ['COMPLETE', 'ERROR'], (status) => updateMediaStatus(mediaId, status));
      currentMediaId.set(mediaId);
      clearFile();
    } catch (error) {
      console.error('Upload error:', error);
      alert('Upload failed: ' + (error instanceof Error ? error.message : 'Unknown error'));
    } finally {
      isProcessing.set(false);
    }
  }

  async function uploadDirect(): Promise<string> {
    const response = await uploadMedia(selectedFile!, width, outputFormat);
    addMedia(createMediaEntry(response.mediaId, 'PENDING'));
    return response.mediaId;
  }

  async function uploadWithPresignedUrl(): Promise<string> {
    const initResponse = await initPresignedUpload({
      fileName: selectedFile!.name,
      fileSize: selectedFile!.size,
      contentType: selectedFile!.type,
      width,
      outputFormat,
    });

    addMedia(createMediaEntry(initResponse.mediaId, 'PENDING_UPLOAD'));

    await uploadToPresignedUrl(initResponse.uploadUrl, selectedFile!, initResponse.headers, (progress) => {
      uploadProgress = progress;
    });

    updateMediaStatus(initResponse.mediaId, 'PENDING');
    await completePresignedUpload(initResponse.mediaId);
    return initResponse.mediaId;
  }
</script>

<div class="card rounded-lg p-6">
  <h2 class="text-base font-semibold text-gray-900 mb-4">Upload Image</h2>

  <!-- Upload Zone -->
  <div
    class="upload-zone rounded-lg p-8 text-center cursor-pointer"
    class:dragover
    class:has-file={selectedFile !== null}
    class:opacity-50={$isProcessing}
    class:pointer-events-none={$isProcessing}
    role="button"
    tabindex="0"
    ondragover={handleDragOver}
    ondragleave={handleDragLeave}
    ondrop={handleDrop}
    onclick={triggerFileSelect}
    onkeydown={(e) => e.key === 'Enter' && triggerFileSelect()}
  >
    <input
      type="file"
      accept="image/*"
      class="hidden"
      bind:this={fileInput}
      onchange={handleFileSelect}
    />

    {#if !selectedFile}
      <svg class="w-10 h-10 mx-auto text-gray-400 mb-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
        <path
          stroke-linecap="round"
          stroke-linejoin="round"
          stroke-width="1.5"
          d="M4 16l4.586-4.586a2 2 0 012.828 0L16 16m-2-2l1.586-1.586a2 2 0 012.828 0L20 14m-6-6h.01M6 20h12a2 2 0 002-2V6a2 2 0 00-2-2H6a2 2 0 00-2 2v12a2 2 0 002 2z"
        ></path>
      </svg>
      <p class="text-gray-600 text-sm">Drop image here or click to browse</p>
      <p class="text-gray-400 text-xs mt-1">JPG, PNG, GIF, WebP supported (up to 5GB)</p>
    {:else}
      <img src={previewUrl} alt="Preview" class="max-h-24 mx-auto rounded mb-2" />
      <p class="text-gray-800 text-sm font-medium">{selectedFile.name}</p>
      <p class="text-gray-400 text-xs">{formatFileSize(selectedFile.size)}</p>
      {#if uploadMethod === 'presigned'}
        <p class="text-blue-600 text-xs mt-1">Large file - will use direct S3 upload</p>
      {/if}
    {/if}
  </div>

  <!-- Progress Bar (for presigned uploads) -->
  {#if $isProcessing && uploadMethod === 'presigned' && uploadProgress > 0}
    <div class="mt-4">
      <div class="flex justify-between text-xs text-gray-500 mb-1">
        <span>Uploading to S3...</span>
        <span>{uploadProgress.toFixed(0)}%</span>
      </div>
      <div class="progress-bar h-2">
        <div class="progress-bar-fill" style="width: {uploadProgress}%"></div>
      </div>
    </div>
  {/if}

  <!-- Options -->
  <div class="flex flex-wrap items-end gap-4 mt-4">
    <div class="flex-1 min-w-48">
      <label for="widthSlider" class="block text-xs font-medium text-gray-600 mb-2">Target Width</label>
      <div class="flex items-center space-x-3">
        <input
          type="range"
          id="widthSlider"
          min="100"
          max="1024"
          bind:value={width}
          disabled={$isProcessing}
          class="flex-1 h-1.5 bg-gray-200 rounded-lg appearance-none cursor-pointer disabled:opacity-50"
        />
        <span class="text-xs font-mono text-gray-600 bg-gray-100 px-2 py-1 rounded">{width}px</span>
      </div>
    </div>
    <div class="min-w-32">
      <label for="formatSelect" class="block text-xs font-medium text-gray-600 mb-2">Output Format</label>
      <select
        id="formatSelect"
        bind:value={outputFormat}
        disabled={$isProcessing}
        class="w-full px-3 py-1.5 text-sm border border-gray-300 rounded-lg bg-white focus:ring-2 focus:ring-blue-500 focus:border-blue-500 disabled:opacity-50"
      >
        {#each formatOptions as option}
          <option value={option.value}>{option.label}</option>
        {/each}
      </select>
    </div>
    <button
      onclick={handleUpload}
      disabled={!selectedFile || $isProcessing}
      class="btn-primary px-5 py-2 text-sm font-medium rounded-lg"
    >
      {#if $isProcessing}
        <svg class="animate-spin -ml-1 mr-2 h-4 w-4 text-white inline-block" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
        </svg>
        Processing...
      {:else}
        Process Image
      {/if}
    </button>
  </div>
</div>
