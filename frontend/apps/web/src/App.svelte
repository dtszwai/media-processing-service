<script lang="ts">
  import TabNav from "./lib/TabNav.svelte";
  import SubmitPanel from "./features/submit/SubmitPanel.svelte";
  import LibraryPanel from "./features/library/LibraryPanel.svelte";
  import TracePanel from "./features/trace/TracePanel.svelte";
  import QueuesPanel from "./features/queues/QueuesPanel.svelte";
  import DdbPanel from "./features/ddb/DdbPanel.svelte";
  import S3Panel from "./features/s3/S3Panel.svelte";
  import LogsPanel from "./features/logs/LogsPanel.svelte";
  import MePanel from "./features/me/MePanel.svelte";
  import { route } from "./shared/route.svelte";

  // Trace owns both the jobs list (its empty state) and the per-job
  // detail view; there is no separate "jobs" tab.
  const TABS = [
    { id: "me", label: "me" },
    "divider" as const,
    { id: "submit", label: "submit" },
    { id: "library", label: "library" },
    "divider" as const,
    { id: "trace", label: "trace" },
    { id: "queues", label: "queues" },
    { id: "ddb", label: "ddb" },
    { id: "s3", label: "s3" },
    { id: "logs", label: "logs" },
  ];

  let activeTab = $derived(route.tab);
  let traceJobId = $derived(activeTab === "trace" ? route.params[0] : undefined);

  // Browser-tab title tracks the active route so multiple windows pinned to
  // different tabs stay distinguishable in the OS tab strip.
  let pageTitle = $derived.by(() => {
    if (activeTab === "trace" && traceJobId) {
      return `trace/${traceJobId} · Media Processing Service`;
    }
    return `${activeTab} · Media Processing Service`;
  });

  $effect(() => {
    document.title = pageTitle;
  });
</script>

<header class="app-header">
  <div class="brand">
    <span class="logo" aria-hidden="true"></span>
    <span class="name">Media Processing Service</span>
  </div>
</header>

<div class="tab-row">
  <TabNav tabs={TABS} active={activeTab} />
</div>

<main>
  {#if activeTab === "me"}
    <MePanel />
  {:else if activeTab === "submit"}
    <SubmitPanel />
  {:else if activeTab === "library"}
    <LibraryPanel />
  {:else if activeTab === "trace"}
    <TracePanel jobId={traceJobId} />
  {:else if activeTab === "queues"}
    <QueuesPanel />
  {:else if activeTab === "ddb"}
    <DdbPanel />
  {:else if activeTab === "s3"}
    <S3Panel />
  {:else if activeTab === "logs"}
    <LogsPanel />
  {:else}
    <MePanel />
  {/if}
</main>

<style>
  .app-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 22px 12px;
    background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
    position: relative;
  }

  /* A 2px ink-cobalt rule under the header gives the otherwise calm
     paper canvas a single confident edge — the brand stripe. */
  .app-header::after {
    content: "";
    position: absolute;
    left: 0;
    right: 0;
    bottom: -1px;
    height: 1px;
    background: linear-gradient(90deg, var(--accent) 0, var(--accent) 88px, var(--border) 88px, var(--border) 100%);
  }

  .brand {
    display: flex;
    align-items: baseline;
    gap: 14px;
  }

  .logo {
    width: 14px;
    height: 14px;
    background: var(--accent);
    display: inline-block;
    align-self: center;
    box-shadow:
      2px 2px 0 0 var(--bg-base),
      2px 2px 0 1px var(--accent);
  }

  .name {
    font-family: var(--font-display);
    color: var(--fg-bright);
    font-size: 22px;
    line-height: 1;
    letter-spacing: -0.01em;
    font-weight: 600;
    font-feature-settings: "ss01";
  }

  .name em {
    font-style: normal;
    color: var(--accent);
    margin: 0 1px;
  }

  .sub {
    color: var(--fg-dim);
    font-size: 12px;
    text-transform: uppercase;
    letter-spacing: 0.12em;
    font-family: var(--font-sans);
  }

  .tab-row {
    background: var(--bg-base);
    border-bottom: 1px solid var(--border);
    padding: 0 14px;
  }

  main {
    flex: 1;
    min-height: 0;
    overflow: auto;
    display: flex;
    flex-direction: column;
    background: var(--bg-base);
  }

  main :global(> *) {
    flex: 1;
    min-height: 0;
  }
</style>
