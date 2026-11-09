<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type DomainCheck, type TargetEvent } from '../lib/backend'
  import { russian, visible } from '../lib/targets'

  let { targets, scanning, scanStart, selected = $bindable(), onback, onnext }: {
    targets: TargetEvent[]
    scanning: boolean
    scanStart: number
    selected: string
    onback: () => void
    onnext: () => void
  } = $props()

  let custom = $state('')
  let checking = $state(false)
  let check = $state<DomainCheck | null>(null)
  let error = $state('')
  let now = $state(Date.now())
  let warnEl: HTMLDivElement | undefined = $state()

  $effect(() => {
    if (check && warnEl) warnEl.scrollIntoView({ block: 'nearest' })
  })

  onMount(() => {
    const t = setInterval(() => (now = Date.now()), 1000)
    return () => clearInterval(t)
  })

  const secs = $derived(Math.max(0, Math.round((now - scanStart) / 1000)))
  const ru = $derived(russian(targets))

  async function useCustom() {
    const d = custom.trim()
    if (!d || checking) return
    error = ''
    check = null
    checking = true
    try {
      const c = await api.CheckDomain(d)
      if (c.ok) {
        selected = d
      } else {
        check = c
      }
    } catch (e) {
      error = String(e)
    } finally {
      checking = false
    }
  }

  function pick(d: string) {
    selected = d
    check = null
  }
</script>

<section class="wz">
  <div class="top"><button class="back" onclick={onback}>‹ Back</button></div>
  <div class="step">Step 2 of 3</div>
  <h2>Which site should the traffic look like?</h2>
  <p class="mut">To your internet provider, your traffic will look like a normal visit to this site.</p>

  <div class="grow">
    {#if scanning}
      <p class="link scan">◌ Looking for sites near your server… {secs} s</p>
    {/if}
    {#each visible(targets) as t (t.domain)}
      <button class="card item" class:sel={selected === t.domain} onclick={() => pick(t.domain)}>
        <span class="dom">{t.domain}</span>
        {#if t.recommended}<span class="ok badge">recommended</span>{/if}
      </button>
    {/each}
    {#if ru.length}
      <p class="mut small">
        Also possible: {ru.map((t) => t.domain).join(', ')} — Russian sites; on a foreign server the disguise may be easier to spot.
      </p>
    {/if}
    <input bind:value={custom} placeholder="Your own domain…" spellcheck="false" autocomplete="off"
      onkeydown={(e) => e.key === 'Enter' && useCustom()} />
    {#if custom.trim() && !check}
      <button class="ghost" disabled={checking} onclick={useCustom}>{checking ? 'Checking…' : 'Check and use'}</button>
    {/if}
    {#if error}<p class="err">{error}</p>{/if}
    {#if check}
      <div class="card warnbox" bind:this={warnEl}>
        {#each check.problems as p}<p class="warn">{p}</p>{/each}
        {#if check.alternatives.length}
          <p class="mut small">Try instead:</p>
          {#each check.alternatives as a}
            <button class="alt link" onclick={() => { custom = a; check = null; pick(a) }}>{a}</button>
          {/each}
        {/if}
        <button class="ghost" onclick={() => { selected = custom.trim(); check = null }}>Use anyway</button>
      </div>
    {/if}
  </div>

  <p class="mut small sel-line">Selected: <b>{selected}</b></p>
  <button class="primary" onclick={onnext}>Next</button>
</section>

<style>
  .scan { margin: 0; font-size: 14px; }
  .item {
    display: flex; justify-content: space-between; align-items: center; gap: 8px;
    border: 1px solid transparent; color: var(--text); font: inherit; text-align: left; cursor: pointer;
  }
  .item.sel { border-color: var(--link); background: rgba(59, 130, 246, 0.2); }
  .dom { overflow-wrap: anywhere; }
  .badge { font-size: 12px; white-space: nowrap; }
  .warnbox { display: flex; flex-direction: column; gap: 6px; }
  .warnbox p { margin: 0; }
  .alt { background: none; border: none; font: inherit; text-align: left; padding: 2px 0; cursor: pointer; }
  .sel-line { margin: 0; }
</style>
