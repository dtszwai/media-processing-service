<script lang="ts">
  type Props = {
    value: unknown;
    label?: string;
    maxHeight?: string;
    wrap?: boolean;
  };

  let { value, label, maxHeight, wrap = true }: Props = $props();

  function stringify(v: unknown): string {
    if (typeof v === "string") return v;
    try {
      return JSON.stringify(v, replacer, 2);
    } catch {
      return String(v);
    }
  }

  function replacer(_key: string, val: unknown): unknown {
    if (typeof val === "bigint") return val.toString();
    return val;
  }
</script>

{#if label}
  <div class="codebox-label">{label}</div>
{/if}
<pre class:wrap style={maxHeight ? `max-height: ${maxHeight}` : undefined}><code>{stringify(value)}</code></pre>

<style>
  pre {
    margin: 0;
    padding: 12px 14px;
    background: var(--bg-input);
    border: 1px solid var(--border);
    color: var(--fg-default);
    font-family: var(--font-mono);
    font-size: 13px;
    line-height: 1.6;
    overflow: auto;
    white-space: pre;
    border-radius: 2px;
  }

  pre.wrap {
    white-space: pre-wrap;
    word-break: break-all;
  }

  .codebox-label {
    font-size: 11.5px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.09em;
    margin-bottom: 6px;
    font-family: var(--font-sans);
    font-weight: 500;
  }
</style>
