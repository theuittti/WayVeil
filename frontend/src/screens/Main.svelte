<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type ConnStatus, type UIState } from '../lib/backend'
  import { bytes, duration } from '../lib/format'
  import RoundButton from '../components/RoundButton.svelte'
  import Card from '../components/Card.svelte'

  let { ui, onopenwizard, onreplacelink }: {
    ui: UIState
    onopenwizard: () => void
    onreplacelink: () => void
  } = $props()

  let status = $state<ConnStatus>({ state: 'disconnected', since: 0, up: 0, down: 0, error: '' })
  let mode = $state('smart')
  let now = $state(Date.now())
  let error = $state('') // from user actions
  let pollError = $state('') // from the last Status poll
  let menu = $state(false)
  let pending = $state(false)

  async function poll() {
    try {
      status = await api.Status()
      pollError = ''
    } catch (e) {
      pollError = String(e)
    }
    now = Date.now()
  }

  onMount(() => {
    mode = ui.mode || 'smart'
    poll()
    const t = setInterval(poll, 1000)
    return () => clearInterval(t)
  })

  const connected = $derived(status.state === 'connected')
  const connecting = $derived(status.state === 'connecting' || pending)
  const label = $derived(
    connecting ? 'Connecting…' : connected ? 'Connected' : status.state === 'failed' ? 'Connection failed' : 'Disconnected',
  )
  const detail = $derived(error || pollError || (status.state === 'failed' ? status.error : ''))

  async function toggle() {
    error = ''
    pending = true
    try {
      status = connected || status.state === 'connecting' ? await api.Disconnect() : await api.Connect()
    } catch (e) {
      error = String(e)
    } finally {
      pending = false
    }
  }

  async function toggleMode() {
    const m = mode === 'smart' ? 'all' : 'smart'
    error = ''
    try {
      status = await api.SetMode(m)
      mode = m
    } catch (e) {
      error = String(e)
    }
  }
</script>

<svelte:window onclick={() => (menu = false)} />

<section>
  <header>
    <span class="name">{ui.profileName || 'Wayveil'}</span>
    <div class="menuwrap">
      <button class="dots" aria-label="Menu" onclick={(e) => { e.stopPropagation(); menu = !menu }}>⋯</button>
      {#if menu}
        <div class="menu">
          <button onclick={onopenwizard}>Set up my own server</button>
          <button onclick={onreplacelink}>Replace link</button>
        </div>
      {/if}
    </div>
  </header>

  <div class="center">
    <RoundButton glow={connected} disabled={pending && !connected} label={connected ? 'Disconnect' : 'Connect'} onclick={toggle}>
      <svg width="64" height="64" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
        <path d="M12 3v9" /><path d="M6.4 6.6a8 8 0 1 0 11.2 0" />
      </svg>
    </RoundButton>
    <p class="state" class:ok={connected}>{label}</p>
    {#if connected && status.since}<p class="timer">{duration(now - status.since)}</p>{/if}
    {#if detail}<p class="err">{detail}</p>{/if}
  </div>

  <div class="cards">
    <Card><span class="k">Downloaded</span><span class="v">{bytes(status.down)}</span></Card>
    <Card onclick={toggleMode}>
      <span class="k">Mode</span><span class="v">{mode === 'smart' ? 'Smart' : 'All traffic'}</span>
    </Card>
  </div>
</section>

<style>
  section {
    box-sizing: border-box;
    min-height: 100vh;
    padding: 16px 20px 24px;
    display: flex;
    flex-direction: column;
  }
  header { display: flex; justify-content: space-between; align-items: center; }
  .name { font-weight: 600; letter-spacing: 0.04em; }
  .menuwrap { position: relative; }
  .dots {
    background: none; border: none; color: var(--text); font-size: 24px; cursor: pointer; padding: 0 8px;
  }
  .menu {
    position: absolute; right: 0; top: 34px; z-index: 1; min-width: 190px;
    background: #1a2238; border-radius: 12px; padding: 6px;
    display: flex; flex-direction: column; box-shadow: 0 6px 20px rgba(0, 0, 0, 0.4);
  }
  .menu button {
    background: none; border: none; color: var(--text); font: inherit; text-align: left;
    padding: 10px 12px; border-radius: 8px; cursor: pointer;
  }
  .menu button:hover { background: var(--card); }
  .center { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 4px; }
  .state { margin: 18px 0 0; font-size: 20px; }
  .state.ok { color: var(--ok); }
  .timer { margin: 0; opacity: 0.8; font-variant-numeric: tabular-nums; }
  .err { margin: 8px 0 0; color: var(--warn); font-size: 14px; text-align: center; }
  .cards { display: flex; gap: 10px; }
  .cards :global(.k) { font-size: 13px; opacity: 0.7; }
  .cards :global(.v) { font-size: 17px; }
</style>
