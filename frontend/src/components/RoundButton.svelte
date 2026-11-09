<script lang="ts">
  let { glow = false, disabled = false, label, onclick, children }: {
    glow?: boolean
    disabled?: boolean
    label: string
    onclick: () => void
    children?: import('svelte').Snippet
  } = $props()
</script>

<button class="round" class:glow {disabled} aria-label={label} {onclick}>
  {@render children?.()}
</button>

<style>
  .round {
    width: 160px;
    height: 160px;
    border-radius: 50%;
    border: none;
    background: var(--btn);
    color: var(--text);
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    box-shadow: 0 4px 14px rgba(0, 0, 0, 0.35);
    transition: box-shadow 0.3s, transform 0.1s;
  }
  .round.glow { box-shadow: var(--glow), 0 0 42px rgba(99, 102, 241, 0.35); }
  .round:active:not(:disabled) { transform: scale(0.97); }
  .round:disabled { opacity: 0.7; cursor: default; }
</style>
