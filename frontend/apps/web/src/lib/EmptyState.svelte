<script lang="ts">
  type Props = {
    title: string;
    hint?: string;
  };

  let { title, hint }: Props = $props();
</script>

<div class="empty">
  <div class="title">{title}</div>
  {#if hint}
    <div class="hint">{hint}</div>
  {/if}
</div>

<style>
  .empty {
    padding: 72px 28px;
    text-align: center;
    color: var(--fg-dim);
    font-family: var(--font-sans);
  }

  .title {
    font-family: var(--font-display);
    font-size: 22px;
    color: var(--fg-bright);
    margin-bottom: 10px;
    letter-spacing: -0.01em;
    font-weight: 500;
  }

  .hint {
    font-size: 14px;
    color: var(--fg-dim);
    line-height: 1.6;
    max-width: 480px;
    margin: 0 auto;
  }
</style>
