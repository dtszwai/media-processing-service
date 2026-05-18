<script lang="ts">
  import type { GateDecision } from "@media-service/api-client/gen/mediaservice/ops/v1/ops_pb.js";
  import Pill from "../../lib/Pill.svelte";
  import { fmtDateTime } from "../../shared/time";

  type Props = { decision: GateDecision };

  let { decision }: Props = $props();

  let verdict = $derived(decision.decision.toUpperCase() || "UNKNOWN");
  let pass = $derived(verdict === "PASS");
  let fail = $derived(verdict === "FAIL");

  function gateVariant(v: string): "ok" | "err" | "warn" {
    if (v === "PASS") return "ok";
    if (v === "FAIL") return "err";
    return "warn";
  }

  const checks = $derived([
    { key: "watermark_present", ok: decision.watermarkPresent },
    { key: "disclosure_present", ok: decision.disclosurePresent },
    { key: "safety_present", ok: decision.safetyPresent },
  ]);

  // Bundle the supporting evidence into a single grid. Previously these
  // lived in two extra blocks (METADATA and WATERMARK) sitting alongside
  // the decision card — three separately-framed sections describing one
  // event. Folding them inside the same card makes the panel read as one
  // verdict with its receipts attached.
  let evidence = $derived(
    [
      { key: "output type", value: decision.outputType || "—", tone: "tag" as const },
      {
        key: "decided by",
        value:
          [decision.provider, decision.model].filter(Boolean).join(" · ") || "—",
        tone: "plain" as const,
      },
      { key: "policy", value: decision.gateVersion || "—", tone: "plain" as const },
      { key: "decided at", value: fmtDateTime(decision.decidedAt), tone: "plain" as const },
      {
        key: "watermark",
        value:
          [decision.watermarkAlgo, decision.watermarkPosition]
            .filter(Boolean)
            .join(" · ") || "—",
        tone: "plain" as const,
      },
      ...(decision.watermarkText
        ? [{ key: "watermark text", value: `"${decision.watermarkText}"`, tone: "quote" as const }]
        : []),
    ].filter((row) => row.value !== "—"),
  );
</script>

<div class="gate-card" class:pass class:fail class:warn={!pass && !fail}>
  <header class="gate-head">
    <span class="gate-eyebrow">gate decision</span>
    <Pill variant={gateVariant(verdict)}>{verdict}</Pill>
    {#if !pass && decision.errorCode}
      <code class="err-code">{decision.errorCode}</code>
    {/if}
  </header>

  <ul class="gate-checks">
    {#each checks as c (c.key)}
      <li class="check" class:ok={c.ok} class:bad={!c.ok}>
        <span class="mark" aria-hidden="true">{c.ok ? "✓" : "✗"}</span>
        <span class="ckey">{c.key}</span>
      </li>
    {/each}
  </ul>

  {#if evidence.length > 0}
    <dl class="gate-evidence">
      {#each evidence as row (row.key)}
        <div class="ev">
          <dt>{row.key}</dt>
          <dd class="ev-value" data-tone={row.tone}>
            {#if row.tone === "tag"}
              <span class="tag">{row.value}</span>
            {:else}
              {row.value}
            {/if}
          </dd>
        </div>
      {/each}
    </dl>
  {/if}

  {#if decision.watermarkFingerprint}
    <footer class="gate-fingerprint">
      <span class="fp-label">fingerprint · sha-256</span>
      <code class="fp-value">{decision.watermarkFingerprint}</code>
    </footer>
  {/if}
</div>

<style>
  /* One bordered card carrying the verdict plus everything that supports
     it. The left border picks up the verdict colour so the eye can read
     PASS / FAIL at a glance before reading any text. */
  .gate-card {
    display: flex;
    flex-direction: column;
    border: 1px solid var(--border);
    border-left-width: 3px;
    background: var(--bg-base);
    border-radius: 3px;
  }

  .gate-card.pass { border-color: var(--accent); border-left-color: var(--accent); background: var(--accent-dim); }
  .gate-card.fail { border-color: var(--err); border-left-color: var(--err); background: var(--err-dim); }
  .gate-card.warn { border-color: var(--warn); border-left-color: var(--warn); background: var(--warn-dim); }

  .gate-head {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 14px 10px;
  }

  .gate-eyebrow {
    font-size: 11.5px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.10em;
    font-family: var(--font-sans);
    font-weight: 500;
  }

  .err-code {
    margin-left: auto;
    color: var(--err);
    font-size: 13px;
    font-family: var(--font-mono);
    font-weight: 500;
  }

  .gate-checks {
    list-style: none;
    margin: 0;
    padding: 0 14px 12px;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .check {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 13px;
    line-height: 1.4;
  }

  .check .mark {
    width: 14px;
    flex: 0 0 14px;
    font-size: 14px;
    line-height: 1;
    font-weight: 700;
    text-align: center;
  }

  .check.ok .mark { color: var(--accent); }
  .check.bad .mark { color: var(--err); }

  .check .ckey {
    color: var(--fg-bright);
    font-family: var(--font-mono);
  }

  .check.bad .ckey {
    color: var(--err);
    font-weight: 600;
  }

  /* Evidence is supporting context — visually subordinate to the verdict
     above and the receipts above that. A single hairline divider from
     the body of the card. Two-column grid keeps the key/value pairs
     dense without feeling like a separate block. */
  .gate-evidence {
    margin: 0;
    padding: 12px 14px 10px;
    border-top: 1px solid var(--border-strong);
    display: grid;
    grid-template-columns: minmax(110px, max-content) minmax(0, 1fr);
    column-gap: 16px;
    row-gap: 6px;
    align-items: baseline;
  }

  .ev {
    display: contents;
  }

  .ev dt {
    font-family: var(--font-sans);
    font-size: 11.5px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.09em;
    font-weight: 500;
  }

  .ev dd {
    margin: 0;
    font-family: var(--font-mono);
    font-size: 13px;
    color: var(--fg-default);
    min-width: 0;
    overflow-wrap: anywhere;
  }

  .ev-value[data-tone="quote"] {
    color: var(--fg-bright);
  }

  .tag {
    display: inline-block;
    font-family: var(--font-sans);
    font-size: 12px;
    font-weight: 500;
    color: var(--fg-bright);
    background: var(--bg-base);
    border: 1px solid var(--border-strong);
    padding: 2px 10px;
    border-radius: 2px;
    letter-spacing: 0.02em;
  }

  /* The fingerprint is the immutable provenance anchor — it earns its own
     row at the foot of the card. Long monospace value, label above it,
     selectable on click. */
  .gate-fingerprint {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 10px 14px 12px;
    border-top: 1px solid var(--border-strong);
  }

  .fp-label {
    font-family: var(--font-sans);
    font-size: 11.5px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.09em;
    font-weight: 500;
  }

  .fp-value {
    font-family: var(--font-mono);
    font-size: 12.5px;
    color: var(--fg-bright);
    background: var(--bg-elevated);
    border: 1px solid var(--border);
    padding: 8px 10px;
    border-radius: 2px;
    word-break: break-all;
    user-select: all;
    cursor: text;
    line-height: 1.5;
  }
</style>
