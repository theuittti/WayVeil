<script lang="ts">
  let { children, onclick }: { children?: import('svelte').Snippet; onclick?: () => void } = $props()
</script>

{#if onclick}
  <button class="card" {onclick}>{@render children?.()}</button>
{:else}
  <div class="card">{@render children?.()}</div>
{/if}

<style>
  .card {
    flex: 1;
    background: var(--card);
    border: none;
    border-radius: 14px;
    padding: 12px 14px;
    color: var(--text);
    font: inherit;
    text-align: left;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  button.card { cursor: pointer; }
</style>
