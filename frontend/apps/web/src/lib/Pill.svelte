<script lang="ts">
  type Variant = "ok" | "warn" | "err" | "pending" | "neutral" | "accent";

  type Props = {
    variant?: Variant;
    children: import("svelte").Snippet;
  };

  let { variant = "neutral", children }: Props = $props();
</script>

<span class="pill {variant}">{@render children()}</span>

<style>
  .pill {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 2px 10px;
    font-size: 11.5px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.09em;
    line-height: 18px;
    border: 1px solid var(--border);
    background: var(--bg-panel);
    color: var(--fg-default);
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
    font-family: var(--font-sans);
    border-radius: 2px;
  }

  .ok {
    border-color: var(--accent);
    color: var(--accent-strong);
    background: var(--accent-dim);
  }

  .accent {
    border-color: var(--accent);
    color: var(--accent-strong);
    background: var(--accent-dim);
  }

  .warn {
    border-color: var(--warn);
    color: var(--warn);
    background: var(--warn-dim);
  }

  .err {
    border-color: var(--err);
    color: var(--err);
    background: var(--err-dim);
  }

  .pending {
    border-color: var(--border-strong);
    color: var(--fg-dim);
    background: var(--pending-dim);
  }

  .neutral {
    border-color: var(--border);
    color: var(--fg-default);
    background: var(--bg-base);
  }
</style>
