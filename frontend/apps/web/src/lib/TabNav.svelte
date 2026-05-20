<script lang="ts">
  import { navigate } from "../shared/route.svelte";

  type Tab = { id: string; label: string };

  type Props = {
    tabs: (Tab | "divider")[];
    active: string;
  };

  let { tabs, active }: Props = $props();

  function go(id: string) {
    navigate(`/${id}`);
  }
</script>

<nav class="tabs">
  {#each tabs as t, i (i)}
    {#if t === "divider"}
      <span class="divider" aria-hidden="true"></span>
    {:else}
      <button class="tab" class:active={active === t.id} onclick={() => go(t.id)}>
        {t.label}
      </button>
    {/if}
  {/each}
</nav>

<style>
  .tabs {
    display: flex;
    align-items: stretch;
    gap: 2px;
  }

  .tab {
    border: 1px solid transparent;
    border-bottom: none;
    background: transparent;
    color: var(--fg-dim);
    padding: 0 16px;
    height: 40px;
    font-family: var(--font-sans);
    font-size: 14px;
    cursor: pointer;
    transition: color 120ms ease, background 120ms ease;
    position: relative;
    top: 1px;
    text-transform: capitalize;
  }

  .tab:hover {
    color: var(--fg-bright);
    background: var(--bg-panel-hover);
  }

  .tab.active {
    color: var(--accent);
    background: var(--bg-panel);
    border-color: var(--border);
    font-weight: 500;
  }

  .divider {
    width: 1px;
    background: var(--border);
    margin: 8px 10px;
    align-self: stretch;
  }
</style>
