<script lang="ts">
  import {
    createAudioOverviewMutation,
    createGenerationMutation,
    createGenerationQuery,
    createGenerationResultQuery,
  } from "../queries";
  import { invalidateMediaList } from "../../../shared/queries";
  import { currentMediaId } from "../stores";
  import { RateLimitError } from "../../../shared/types";
  import type {
    CreateAudioOverviewRequest,
    CreateGenerationRequest,
    GenerationResponse,
    GenerationResultResponse,
    GenerationStatus,
  } from "../../../shared/types";

  interface Props {
    mode: "image" | "audio";
  }

  const AUDIO_PROVIDERS: ReadonlyArray<{ id: "simulated" | "notebooklm"; label: string }> = [
    { id: "simulated", label: "Simulated (sine-wave placeholder)" },
    { id: "notebooklm", label: "NotebookLM (via notebooklm-py)" },
  ];

  let { mode }: Props = $props();

  let prompt = $state("");
  let imageModel = $state("");
  let resolution = $state("1024x1024");
  let seed = $state("");
  let audioProvider = $state<"simulated" | "notebooklm">("simulated");
  let tier = $state<"free" | "paid">("free");
  let webhookUrl = $state("");
  let showAdvanced = $state(false);
  let lastJobId = $state<string | null>(null);
  let lastSubmission = $state<GenerationResponse | null>(null);
  let submitError = $state<string | null>(null);
  let pendingIdempotencyKey = $state<string | null>(null);

  const imageMutation = createGenerationMutation();
  const audioMutation = createAudioOverviewMutation();

  const statusQuery = $derived(lastJobId ? createGenerationQuery(lastJobId) : null);
  const resultQuery = $derived(
    lastJobId
      ? createGenerationResultQuery(
          () => lastJobId!,
          () => statusQuery?.data?.status,
        )
      : null,
  );

  let status = $derived(resultQuery?.data?.status ?? statusQuery?.data?.status ?? lastSubmission?.status ?? null);
  let job = $derived(statusQuery?.data ?? lastSubmission);
  let result = $derived(resultQuery?.data ?? null);
  let isSubmitting = $derived(imageMutation.isPending || audioMutation.isPending);
  let canSubmit = $derived(prompt.trim().length > 0 && !isSubmitting);
  let isRunning = $derived(status === "QUEUED" || status === "RUNNING");
  let title = $derived(mode === "image" ? "Generate Image" : "Audio Overview");
  let promptLabel = $derived(mode === "image" ? "Prompt" : "Topic");
  let placeholder = $derived(
    mode === "image"
      ? "Describe the image to generate"
      : "Paste a document, transcript, or describe the topic for the AI hosts to discuss",
  );
  let showAudioSetup = $state(false);
  let submitLabel = $derived(mode === "image" ? "Generate" : "Create Overview");

  function createIdempotencyKey(prefix: string) {
    const randomId =
      typeof crypto !== "undefined" && "randomUUID" in crypto
        ? crypto.randomUUID()
        : `${Date.now()}-${Math.random().toString(16).slice(2)}`;
    return `${prefix}-${randomId}`;
  }

  function normalizeWebhookUrl() {
    const value = webhookUrl.trim();
    return value.length > 0 ? value : undefined;
  }

  function parsedSeed() {
    const value = seed.trim();
    if (!value) return undefined;
    const numericSeed = Number(value);
    return Number.isFinite(numericSeed) ? numericSeed : undefined;
  }

  async function submitGeneration() {
    if (!canSubmit) return;

    submitError = null;

    // L1: generate idempotency key ONCE per submission attempt and reuse on retry
    if (!pendingIdempotencyKey) {
      const prefix = mode === "image" ? "image-generation" : "audio-overview";
      pendingIdempotencyKey = createIdempotencyKey(prefix);
    }

    try {
      const response = mode === "image" ? await submitImage() : await submitAudioOverview();
      lastSubmission = response;
      lastJobId = response.job_id;
      currentMediaId.set(response.media_id);
      pendingIdempotencyKey = null;
      await invalidateMediaList();
    } catch (err) {
      if (err instanceof RateLimitError) {
        submitError = `Rate limited. Try again in ${err.retryAfterSeconds ?? 60}s.`;
      } else {
        submitError = err instanceof Error ? err.message : "Generation request failed";
      }
    }
  }

  async function submitImage() {
    const request: CreateGenerationRequest = {
      prompt: prompt.trim(),
      model: imageModel.trim() || undefined,
      resolution: resolution || undefined,
      tier,
      seed: parsedSeed(),
      webhook_url: normalizeWebhookUrl(),
    };

    return imageMutation.mutateAsync({
      request,
      idempotencyKey: pendingIdempotencyKey ?? createIdempotencyKey("image-generation"),
    });
  }

  async function submitAudioOverview() {
    const request: CreateAudioOverviewRequest = {
      topic: prompt.trim(),
      tier,
      webhook_url: normalizeWebhookUrl(),
      provider: audioProvider,
    };

    return audioMutation.mutateAsync({
      request,
      idempotencyKey: pendingIdempotencyKey ?? createIdempotencyKey("audio-overview"),
    });
  }

  function statusLabel(value: GenerationStatus | null) {
    if (!value) return "Not submitted";
    if (value === "QUEUED") return "Queued";
    if (value === "RUNNING") return "Running";
    if (value === "BLOCKED") return "Blocked";
    if (value === "FAILED") return "Failed";
    return "Complete";
  }

  function statusClass(value: GenerationStatus | null) {
    if (value === "COMPLETE") return "status-complete";
    if (value === "FAILED" || value === "BLOCKED") return "status-error";
    if (value === "QUEUED") return "status-pending";
    if (value === "RUNNING") return "status-processing";
    return "status-uploading";
  }

  function outputUrl(data: GenerationResultResponse | null) {
    return mode === "image" ? data?.image_url : data?.audio_url;
  }
</script>

<section class="card rounded-lg p-5">
  <div class="flex items-start justify-between gap-4">
    <div>
      <h2 class="text-base font-semibold text-gray-900">{title}</h2>
      {#if job?.job_id}
        <p class="mt-1 text-xs text-gray-500">Job {job.job_id}</p>
      {/if}
    </div>
    <span class="status-badge {statusClass(status)}" aria-live="polite" aria-atomic="true">{statusLabel(status)}</span>
  </div>

  {#if mode === "audio" && audioProvider === "notebooklm"}
    <div class="mt-4 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-900">
      <div class="flex items-start justify-between gap-3">
        <div>
          <span class="font-semibold">NotebookLM bridge:</span>
          uses the captured Google session at
          <code class="rounded bg-amber-100 px-1 py-0.5 font-mono">$HOME/.notebooklm/state.json</code>.
          One-time setup if you have not run the login script yet.
        </div>
        <button
          type="button"
          onclick={() => (showAudioSetup = !showAudioSetup)}
          class="shrink-0 rounded border border-amber-300 px-2 py-0.5 text-[11px] font-medium hover:bg-amber-100"
        >
          {showAudioSetup ? "Hide setup" : "Setup"}
        </button>
      </div>
      {#if showAudioSetup}
        <ol class="mt-3 list-decimal space-y-1 pl-5">
          <li>
            <code class="font-mono">pip install -r scripts/notebooklm/login-requirements.txt</code>
            then <code class="font-mono">playwright install chromium</code>
          </li>
          <li>
            <code class="font-mono">python3 scripts/notebooklm/login.py --out ~/.notebooklm/state.json</code>
            (sign in once with a personal Google account)
          </li>
          <li>
            Submit a job from this panel with provider <strong>NotebookLM</strong> selected. The worker
            spawns <code class="font-mono">scripts/notebooklm/overview.py</code> per job.
          </li>
          <li class="text-amber-800">
            Heads up: <code class="font-mono">notebooklm-py</code> uses an undocumented Google API and a
            personal-account session that expires — re-run the login script when generation starts failing.
          </li>
        </ol>
      {/if}
    </div>
  {/if}

  <div class="mt-4 space-y-4">
    <label class="block">
      <span class="block text-xs font-medium text-gray-600 mb-1">{promptLabel}</span>
      <textarea
        bind:value={prompt}
        rows="4"
        maxlength="4000"
        disabled={isSubmitting}
        placeholder={placeholder}
        class="w-full resize-y rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm text-gray-900 placeholder:text-gray-400 focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900 disabled:opacity-50"
      ></textarea>
    </label>

    {#if mode === "image"}
      <div class="grid gap-3 sm:grid-cols-3">
        <label class="block">
          <span class="block text-xs font-medium text-gray-600 mb-1">Resolution</span>
          <select
            bind:value={resolution}
            disabled={isSubmitting}
            class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900 disabled:opacity-50"
          >
            <option value="1024x1024">1024 x 1024</option>
            <option value="1024x1792">1024 x 1792</option>
            <option value="1792x1024">1792 x 1024</option>
          </select>
        </label>
        <label class="block">
          <span class="block text-xs font-medium text-gray-600 mb-1">Model</span>
          <input
            bind:value={imageModel}
            disabled={isSubmitting}
            placeholder="Default"
            class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900 disabled:opacity-50 placeholder:text-gray-400"
          />
        </label>
        <label class="block">
          <span class="block text-xs font-medium text-gray-600 mb-1">Seed</span>
          <input
            bind:value={seed}
            inputmode="numeric"
            disabled={isSubmitting}
            placeholder="Optional"
            class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900 disabled:opacity-50 placeholder:text-gray-400"
          />
        </label>
      </div>
    {:else}
      <label class="block">
        <span class="block text-xs font-medium text-gray-600 mb-1">Provider</span>
        <div class="flex rounded-lg border border-gray-300 bg-white p-1">
          {#each AUDIO_PROVIDERS as opt (opt.id)}
            <button
              type="button"
              onclick={() => (audioProvider = opt.id)}
              disabled={isSubmitting}
              class="flex-1 rounded-md px-3 py-1.5 text-sm transition-colors {audioProvider === opt.id
                ? 'bg-gray-900 text-white'
                : 'text-gray-600 hover:bg-gray-100'}"
            >
              {opt.label}
            </button>
          {/each}
        </div>
        <p class="mt-1 text-[11px] text-gray-500">
          {#if audioProvider === "notebooklm"}
            Drives notebooklm.google.com through the captured Google session. Slower (2-5 min) and TOS-grey — see the Setup button above.
          {:else}
            In-process sine-wave placeholder for local dev. No external account needed; completes instantly.
          {/if}
        </p>
      </label>
    {/if}

    <div>
      <button
        type="button"
        onclick={() => (showAdvanced = !showAdvanced)}
        class="flex items-center text-xs text-gray-500 hover:text-gray-700"
      >
        <svg
          class="mr-1 h-3 w-3 transition-transform {showAdvanced ? 'rotate-90' : ''}"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
        >
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 5l7 7-7 7"></path>
        </svg>
        Advanced Options
      </button>

      {#if showAdvanced}
        <div class="mt-3 grid gap-3 rounded-lg border border-gray-200 bg-gray-50 p-4 sm:grid-cols-2">
          <label class="block">
            <span class="block text-xs font-medium text-gray-600 mb-1">Tier</span>
            <select
              bind:value={tier}
              disabled={isSubmitting}
              class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900 disabled:opacity-50"
            >
              <option value="free">Free</option>
              <option value="paid">Paid</option>
            </select>
          </label>
          <label class="block">
            <span class="block text-xs font-medium text-gray-600 mb-1">Webhook URL</span>
            <input
              type="url"
              bind:value={webhookUrl}
              disabled={isSubmitting}
              placeholder="https://api.example.com/webhooks/media"
              class="w-full rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm focus:border-gray-900 focus:outline-none focus:ring-1 focus:ring-gray-900 disabled:opacity-50 placeholder:text-gray-400"
            />
          </label>
        </div>
      {/if}
    </div>

    {#if submitError || job?.error_message}
      <p class="rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700">
        {submitError || job?.error_message}
      </p>
    {/if}

    {#if job?.stage || job?.estimated_wait_seconds || statusQuery?.data?.progress !== undefined}
      <div class="rounded-lg border border-gray-200 bg-gray-50 px-3 py-2 text-xs text-gray-600">
        {#if job?.stage}
          <span>{job.stage}</span>
        {/if}
        {#if job?.stage && (job.estimated_wait_seconds || statusQuery?.data?.progress !== undefined)}
          <span> / </span>
        {/if}
        {#if statusQuery?.data?.progress !== undefined}
          <span>{Math.round(statusQuery.data.progress)}%</span>
        {/if}
        {#if statusQuery?.data?.progress !== undefined && job?.estimated_wait_seconds}
          <span> / </span>
        {/if}
        {#if job?.estimated_wait_seconds}
          <span>About {job.estimated_wait_seconds}s remaining</span>
        {/if}
      </div>
    {/if}

    <div class="flex flex-wrap items-center gap-3">
      <button
        type="button"
        onclick={submitGeneration}
        disabled={!canSubmit}
        class="btn-primary rounded-lg px-4 py-2 text-sm font-medium"
      >
        {#if isSubmitting}
          Submitting...
        {:else}
          {submitLabel}
        {/if}
      </button>

      {#if isRunning}
        <span class="text-xs text-gray-500">Status refreshes automatically.</span>
      {/if}

      {#if outputUrl(result)}
        <a
          href={outputUrl(result)}
          target="_blank"
          rel="noreferrer"
          class="rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
        >
          Open Output
        </a>
      {/if}
    </div>
  </div>
</section>
