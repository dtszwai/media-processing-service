<script lang="ts">
  import { create } from "@bufbuild/protobuf";
  import {
    CreateGenerationRequestSchema,
    CreateAudioOverviewRequestSchema,
    OutputType,
  } from "@media-service/api-client/gen/mediaservice/generation/v1/generation_pb.js";
  import { generationClient } from "../../shared/api";
  import { localOpsClient } from "../../shared/local-ops/client";
  import type { GenerationProviderModels } from "../../shared/local-ops/types";
  import { navigate } from "../../shared/route.svelte";
  import { idempotencyKey } from "../../shared/time";
  import Pill from "../../lib/Pill.svelte";

  type Kind = "image" | "audio";

  let kind = $state<Kind>("image");
  let prompt = $state("");
  let tier = $state("free");
  let provider = $state("");
  let providerScope = $state("");
  let model = $state("");
  let busy = $state(false);
  let lastError = $state<string | null>(null);

  // Provider catalog grouped by output_type ("image" / "audio"). Empty until
  // the mount-time ListGenerationModels call lands. Kept off the page on error
  // so the form falls back to a placeholder model display.
  let catalog = $state<Record<string, GenerationProviderModels[]>>({});

  const kinds: { id: Kind; label: string; hint: string }[] = [
    { id: "image", label: "image", hint: "text → image (gen)" },
    { id: "audio", label: "audio", hint: "text → audio overview" },
  ];

  let providers = $derived.by(() => {
    return catalog[kind] ?? [];
  });
  let activeProvider = $derived.by(() => {
    return providers.find((p) => p.provider === provider) ?? providers[0];
  });
  let providerName = $derived(activeProvider?.provider ?? "");

  async function loadCatalog() {
    try {
      const res = await localOpsClient.listGenerationModels();
      const next: Record<string, GenerationProviderModels[]> = {};
      for (const p of res.providers) {
        next[p.outputType] ??= [];
        next[p.outputType].push(p);
      }
      catalog = next;
    } catch (err) {
      // Non-fatal: the submit form keeps working with an empty model field.
      console.warn("listGenerationModels failed", err);
    }
  }

  $effect(() => {
    loadCatalog();
  });

  // When kind or catalog changes, snap `provider` to a valid provider for the
  // selected kind.
  $effect(() => {
    const scope = `${kind}:${providers.map((p) => p.provider).join("|")}`;
    if (providers.length === 0) {
      provider = "";
      providerScope = scope;
      return;
    }
    if (providerScope !== scope || !providers.some((p) => p.provider === provider)) {
      provider = providers[0].provider;
    }
    providerScope = scope;
  });

  // Model is not user-selectable in this console — each provider currently
  // exposes exactly one model. Snap to the active provider's default so the
  // submit payload still carries a stable identifier the backend can record.
  $effect(() => {
    model = activeProvider?.defaultModel ?? "";
  });

  async function onSubmit(e: SubmitEvent) {
    e.preventDefault();
    lastError = null;

    if (!prompt.trim()) {
      lastError = "prompt is required";
      return;
    }

    busy = true;
    try {
      if (kind === "image") {
        const req = create(CreateGenerationRequestSchema, {
          prompt,
          tier: tier || undefined,
          model: model || undefined,
          provider: providerName || undefined,
          idempotencyKey: idempotencyKey("gen"),
          outputType: OutputType.IMAGE,
        });
        const res = await generationClient.createGeneration(req);
        const jobId = res.generation?.jobId;
        if (jobId) navigate(`/trace/${jobId}`);
        else lastError = "no job_id in response";
      } else {
        const req = create(CreateAudioOverviewRequestSchema, {
          topic: prompt,
          tier: tier || undefined,
          model: model || undefined,
          provider: providerName || undefined,
          idempotencyKey: idempotencyKey("aud"),
        });
        const res = await generationClient.createAudioOverview(req);
        const jobId = res.generation?.jobId;
        if (jobId) navigate(`/trace/${jobId}`);
        else lastError = "no job_id in response";
      }
    } catch (err) {
      lastError = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }
</script>

<section class="submit">
  <div class="submit-header">
    <h2 class="title">Submit generation job</h2>
    <span class="meta">Enqueue a text-to-image or audio overview generation</span>
  </div>
  <form onsubmit={onSubmit}>
    <fieldset>
      <legend>kind</legend>
      <div class="radio-row">
        {#each kinds as k (k.id)}
          <label class="radio">
            <input
              type="radio"
              name="kind"
              value={k.id}
              checked={kind === k.id}
              onchange={() => (kind = k.id)}
              disabled={busy}
            />
            <span class="lbl">{k.label}</span>
            <span class="hint">{k.hint}</span>
          </label>
        {/each}
      </div>
    </fieldset>

    <fieldset>
      <legend>prompt</legend>
      <textarea
        bind:value={prompt}
        placeholder={kind === "audio"
          ? "describe the audio overview you want"
          : "describe the image"}
        disabled={busy}
      ></textarea>
    </fieldset>

    <fieldset>
      <legend>tier</legend>
      <div class="radio-row">
        {#each ["free", "paid"] as t (t)}
          <label class="radio">
            <input
              type="radio"
              name="tier"
              value={t}
              checked={tier === t}
              onchange={() => (tier = t)}
              disabled={busy}
            />
            <span class="lbl">{t}</span>
          </label>
        {/each}
      </div>
    </fieldset>

    <fieldset>
      <legend>provider</legend>
      {#if providers.length > 0}
        <div class="radio-row">
          {#each providers as opt (opt.provider)}
            <label class="radio">
              <input
                type="radio"
                name="provider"
                value={opt.provider}
                checked={provider === opt.provider}
                onchange={() => (provider = opt.provider)}
                disabled={busy}
              />
              <span class="lbl">{opt.provider}</span>
            </label>
          {/each}
        </div>
      {:else}
        <div class="fixed-model mono dim" title="not applicable for this kind">
          —
        </div>
      {/if}
    </fieldset>

    <div class="actions">
      <button class="primary" type="submit" disabled={busy || !model}>
        {busy ? "submitting…" : "submit"}
      </button>
      {#if lastError}
        <span class="err"><Pill variant="err">err</Pill> {lastError}</span>
      {/if}
    </div>
  </form>
</section>

<style>
  .submit {
    max-width: 780px;
    margin: 32px auto;
    background: var(--bg-panel);
    border: 1px solid var(--border);
    border-radius: 6px;
  }

  .submit-header {
    padding: 24px 26px 0;
    font-family: var(--font-sans);
  }

  .title {
    margin: 0 0 4px;
    font-size: 18px;
    font-weight: 600;
    color: var(--fg-bright);
  }

  .meta {
    font-size: 13px;
    color: var(--fg-dim);
  }

  form {
    padding: 22px 26px 26px;
    display: flex;
    flex-direction: column;
    gap: 22px;
  }

  fieldset {
    border: none;
    padding: 0;
    margin: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }

  legend {
    padding: 0;
    font-size: 12px;
    color: var(--fg-dim);
font-family: var(--font-sans);
    font-weight: 500;
  }

  .radio-row {
    display: grid;
    grid-template-columns: repeat(2, 1fr);
    gap: 8px 10px;
  }

  .radio {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    border: 1px solid var(--border);
    cursor: pointer;
    background: var(--bg-panel);
    border-radius: 3px;
    transition: border-color 120ms ease, background 120ms ease;
  }

  .radio:hover {
    background: var(--bg-panel-hover);
    border-color: var(--border-strong);
  }

  .radio:has(input:checked) {
    border-color: var(--accent);
    background: var(--accent-dim);
  }

  .radio input {
    accent-color: var(--accent);
  }

  .lbl { color: var(--fg-bright); font-weight: 500; }
  .hint {
    color: var(--fg-dim);
    font-size: 12.5px;
    margin-left: auto;
    font-family: var(--font-sans);
  }

  .actions {
    display: flex;
    align-items: center;
    gap: 14px;
    margin-top: 8px;
  }

  .actions button.primary {
    padding: 10px 22px;
    font-size: 14px;
  }

  .err {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    color: var(--err);
    font-size: 13px;
    font-family: var(--font-sans);
  }

  .dim { color: var(--fg-dim); }

  /* Read-only model display when the provider exposes exactly one model. */
  .fixed-model {
    background: var(--bg-base);
    border: 1px solid var(--border);
    padding: 8px 12px;
    color: var(--fg-bright);
    font-size: 13.5px;
    border-radius: 2px;
    user-select: all;
  }
  .fixed-model.dim { color: var(--fg-dim); }
  .mono { font-family: var(--font-mono); }
</style>
