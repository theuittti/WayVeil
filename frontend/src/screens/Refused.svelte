<script lang="ts">
  import type { HostCheck } from '../lib/backend'

  let { check, onback }: { check: HostCheck; onback: () => void } = $props()
</script>

<section class="wz">
  <div class="top"></div>
  <div class="grow">
    <div class="card"><p class="warn msg">{check.message || check.detail}</p></div>
  </div>
  <button class="primary" onclick={onback}>Back</button>
</section>

<style>
  .msg { margin: 0; line-height: 1.45; }
</style>
