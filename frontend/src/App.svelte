<script lang="ts">
  import { onMount } from 'svelte'
  import { api, on, type AccessFields, type Done, type HostCheck, type TargetEvent, type UIState } from './lib/backend'
  import { next, type FlowEvent, type Screen } from './lib/flow'
  import { defaultTarget, merge } from './lib/targets'
  import Paste from './screens/Paste.svelte'
  import Main from './screens/Main.svelte'
  import Access from './screens/Access.svelte'
  import Target from './screens/Target.svelte'
  import Installing from './screens/Installing.svelte'
  import DoneScreen from './screens/Done.svelte'
  import Existing from './screens/Existing.svelte'
  import Refused from './screens/Refused.svelte'

  const startState: UIState = { hasProfile: false, profileName: '', server: '', hasOwner: false, mode: 'smart' }

  let screen = $state<Screen | null>(null)
  let ui = $state<UIState>(startState)

  let fields = $state<AccessFields>({ host: '', port: 22, user: 'root' })
  let check = $state<HostCheck | null>(null)
  let targets = $state<TargetEvent[]>([])
  let scanning = $state(false)
  let scanStart = $state(0)
  let picked = $state<string | null>(null)
  let sni = $state('')
  let action = $state<'install' | 'reinstall'>('install')
  let done = $state<Done | null>(null)

  function send(ev: FlowEvent) {
    screen = next(screen ?? 'paste', ev)
  }

  // Never leave the window blank or Main stale: a failed refresh keeps the last state.
  async function refresh() {
    try {
      ui = await api.State()
    } catch {
      /* keep the previous state */
    }
  }

  async function goMain(ev: FlowEvent) {
    await refresh()
    send(ev)
  }

  onMount(() => {
    const offTarget = on('target', (ev) => (targets = merge(targets, ev)))
    const offDone = on('target-done', () => (scanning = false))
    api.State().then(
      (s) => { ui = s; send({ type: 'start', state: s }) },
      () => send({ type: 'start', state: startState }),
    )
    return () => { offTarget(); offDone() }
  })

  // A cancelled scan never emits target-done, so the indicator is stopped here.
  const stopScan = () => { scanning = false }

  function connected(c: HostCheck) {
    check = c
    targets = []
    picked = null
    scanning = c.state === 'fresh' || c.state === 'wayveil' || c.state === 'partial'
    scanStart = Date.now()
    send({ type: 'connected', check: c })
  }

  async function disconnectAndBack() {
    stopScan()
    try {
      await api.ServerDisconnect()
    } catch {
      /* nothing to clean up */
    }
    send({ type: 'back' })
  }

  function install() {
    stopScan()
    sni = picked ?? defaultTarget(targets)
    action = 'install'
    send({ type: 'chooseTarget' })
  }

  function reinstall() {
    stopScan()
    sni = defaultTarget(targets)
    action = 'reinstall'
    send({ type: 'reinstall' })
  }

  function finished(d: Done) {
    done = d
    send({ type: 'installFinished' })
  }

  function adopted(d: Done) {
    stopScan()
    done = d
    send({ type: 'adopt' })
  }
</script>

<main>
  {#if screen === 'paste'}
    <Paste
      onimported={() => goMain({ type: 'imported' })}
      onownserver={() => send({ type: 'ownServer' })}
    />
  {:else if screen === 'main'}
    <Main {ui} onopenwizard={() => send({ type: 'openWizard' })} onreplacelink={() => send({ type: 'replaceLink' })} />
  {:else if screen === 'access'}
    <Access bind:fields onback={() => goMain({ type: 'back', hasProfile: ui.hasProfile })} onconnected={connected} />
  {:else if screen === 'target'}
    <Target
      {targets}
      {scanning}
      {scanStart}
      bind:selected={() => picked ?? defaultTarget(targets), (v) => (picked = v)}
      onback={disconnectAndBack}
      onnext={install}
    />
  {:else if screen === 'installing'}
    <Installing {sni} {action} ondone={finished} />
  {:else if screen === 'done' && done}
    <DoneScreen {done} onconnect={() => goMain({ type: 'connect' })} />
  {:else if screen === 'existing' && check}
    <Existing {check} onback={disconnectAndBack} onadopted={adopted} onreinstall={reinstall} />
  {:else if screen === 'refused' && check}
    <Refused {check} onback={disconnectAndBack} />
  {/if}
</main>

<style>
  main { max-width: 380px; margin: 0 auto; min-height: 100vh; }
</style>
