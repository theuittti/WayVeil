<script lang="ts">
  import { onMount } from 'svelte'
  import { api, on, type Done } from '../lib/backend'
  import { duration } from '../lib/format'
  import { apply, initial, type StepRow } from '../lib/steps'

  let { sni, action, ondone }: {
    sni: string
    action: 'install' | 'reinstall'
    ondone: (d: Done) => void
  } = $props()

  let rows = $state<StepRow[]>(initial())
  let error = $state('')
  let running = $state(false)
  let showLog = $state(false)
  let started = $state(Date.now())
  let now = $state(Date.now())
  let first = true

  async function run(act: 'install' | 'reinstall') {
    if (running) return
    rows = initial()
    error = ''
    started = Date.now()
    running = true
    try {
      const d = await (act === 'install' ? api.Install(sni) : api.Reinstall(sni))
      ondone(d)
    } catch (e) {
      error = String(e)
    } finally {
      running = false
    }
  }

  onMount(() => {
    const off = on('step', (ev) => (rows = apply(rows, ev)))
    const t = setInterval(() => (now = Date.now()), 1000)
    if (first) {
      first = false
      run(action)
    }
    return () => {
      off()
      clearInterval(t)
    }
  })

  const icon = (s: string) => (s === 'ok' ? '✓' : s === 'warn' ? '!' : '✗')
  const time = (at: number) => new Date(at).toLocaleTimeString([], { hour12: false })
</script>

<section class="wz">
  <div class="step">Step 3 of 3</div>
  <h2>Installing</h2>

  <div class="grow">
    <ul>
      {#each rows as r (r.id)}
        <li class="st-{r.status}">
          <span class="ic">{#if r.status === 'running'}<i class="spin"></i>{:else if r.status === 'pending'}<i class="idle"></i>{:else}{icon(r.status)}{/if}</span>
          <span class="t">{r.title}</span>
          {#if showLog && r.log.length}
            <ul class="log">
              {#each r.log as l}<li><span class="at">{time(l.at)}</span> {l.text}</li>{/each}
            </ul>
          {/if}
        </li>
      {/each}
    </ul>
    <button class="linkbtn" onclick={() => (showLog = !showLog)}>
      {showLog ? '▾ Hide detailed log' : '▸ Show detailed log'}
    </button>
    {#if error}
      <div class="card"><p class="err">{error}</p></div>
    {/if}
  </div>

  {#if error}
    <button class="primary" disabled={running} onclick={() => run('reinstall')}>Clean up and install again</button>
  {:else}
    <p class="mut timer">{duration(now - started)}</p>
  {/if}
</section>

<style>
  ul { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 7px; }
  li { display: grid; grid-template-columns: 24px 1fr; align-items: start; }
  li.st-pending { opacity: 0.5; }
  li.st-ok .ic { color: var(--ok); }
  li.st-warn .ic, li.st-fail .ic { color: var(--warn); }
  li.st-running .ic { color: var(--link); }
  .ic { display: flex; align-items: center; justify-content: center; width: 16px; height: 22px; }
  .idle, .spin { display: block; width: 10px; height: 10px; border-radius: 50%; box-sizing: border-box; border: 1.5px solid currentColor; }
  .spin { border-color: var(--link) transparent var(--link) var(--link); animation: turn 0.9s linear infinite; }
  @keyframes turn { to { transform: rotate(360deg); } }
  li.st-fail .t { color: var(--warn); }
  .log { grid-column: 2; margin-top: 3px; gap: 2px; font-size: 12px; opacity: 0.7; }
  .log li { display: block; word-break: break-word; }
  .at { opacity: 0.6; font-variant-numeric: tabular-nums; }
  .linkbtn { background: none; border: none; color: var(--link); font: inherit; text-align: left; padding: 4px 0; cursor: pointer; }
  .timer { text-align: left; font-variant-numeric: tabular-nums; }
</style>
