<script lang="ts">
  type Entry = {
    key: string;
    value: string;
    tone?: "default" | "accent" | "muted" | "err" | "ok" | "warn";
    /** When set, the value renders as an anchor pointing at this href.
     *  The trace panel uses this to deep-link a span's id (and its DDB
     *  pk/sk) directly into the DDB inspector. */
    href?: string;
  };

  type Props = {
    entries: Entry[];
    dense?: boolean;
  };

  let { entries, dense = false }: Props = $props();
</script>

<dl class="kv" class:dense>
  {#each entries as e (e.key)}
    <dt>{e.key}</dt>
    <dd class={e.tone ?? "default"}>
      {#if e.href}
        <a class="kv-link" href={e.href}>{e.value}</a>
      {:else}
        {e.value}
      {/if}
    </dd>
  {/each}
</dl>

<style>
  .kv {
    display: grid;
    grid-template-columns: minmax(140px, max-content) 1fr;
    gap: 8px 22px;
    margin: 0;
    font-size: 14px;
    font-variant-numeric: tabular-nums;
    font-family: var(--font-mono);
  }

  .kv.dense {
    gap: 5px 18px;
    font-size: 13px;
  }

  dt {
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.09em;
    font-size: 11.5px;
    align-self: baseline;
    padding-top: 2px;
    font-family: var(--font-sans);
    font-weight: 500;
  }

  dd {
    margin: 0;
    color: var(--fg-default);
    word-break: break-all;
  }

  dd.accent { color: var(--accent); }
  dd.muted  { color: var(--fg-muted); }
  dd.err    { color: var(--err); }
  dd.ok     { color: var(--ok); }
  dd.warn   { color: var(--warn); }

  /* Inline link styling: monospace, accent-coloured, dotted underline
     that solidifies on hover. We don't want anchors to look like body
     text (so the operator knows they're navigable) nor like buttons. */
  .kv-link {
    color: var(--accent);
    font-family: var(--font-mono);
    text-decoration: none;
    border-bottom: 1px dotted var(--accent);
    transition: border-color 120ms ease, color 120ms ease;
    word-break: break-all;
  }
  .kv-link:hover {
    color: var(--accent-strong);
    border-bottom: 1px solid var(--accent-strong);
  }
</style>
