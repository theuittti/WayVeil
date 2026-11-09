<script lang="ts">
  import { onMount } from 'svelte'
  import { api, backendError, backendOK, on, type AccessFields, type Done, type HostCheck, type TargetEvent, type UIState } from './lib/backend'
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
  let notice = $state('') // error for Main to show on arrival

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
    if (!backendOK) return
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
    if (screen !== 'access') return // a late result after Back
    check = c
    action = 'install'
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

  // Back on target: during a reinstall to the existing-install screen (the
  // session stays), otherwise to Access.
  function targetBack() {
    if (action === 'reinstall') send({ type: 'back', reinstall: true })
    else disconnectAndBack()
  }

  // Next on target: Install, or Reinstall when coming from the existing screen.
  function chooseTarget() {
    stopScan()
    sni = picked ?? defaultTarget(targets)
    send({ type: 'chooseTarget' })
  }

  function reinstall() {
    action = 'reinstall'
    picked = null
    send({ type: 'reinstall' })
  }

  function connectedFromDone(error: string) {
    notice = error
    goMain({ type: 'connect' })
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
  {#if !backendOK}
    <section class="wz fatal"><p class="err">{backendError}</p></section>
  {:else if screen === 'paste'}
    <Paste
      onimported={() => goMain({ type: 'imported' })}
      onownserver={() => send({ type: 'ownServer' })}
      onback={ui.hasProfile ? () => goMain({ type: 'back', hasProfile: true }) : undefined}
    />
  {:else if screen === 'main'}
    <Main
      {ui}
      {notice}
      onopenwizard={() => { notice = ''; send({ type: 'openWizard' }) }}
      onreplacelink={() => { notice = ''; send({ type: 'replaceLink' }) }}
    />
  {:else if screen === 'access'}
    <Access bind:fields onback={() => goMain({ type: 'back', hasProfile: ui.hasProfile })} onconnected={connected} />
  {:else if screen === 'target'}
    <Target
      {targets}
      {scanning}
      {scanStart}
      bind:selected={() => picked ?? defaultTarget(targets), (v) => (picked = v)}
      onback={targetBack}
      onnext={chooseTarget}
    />
  {:else if screen === 'installing'}
    <Installing {sni} {action} ondone={finished} onback={disconnectAndBack} />
  {:else if screen === 'done' && done}
    <DoneScreen {done} onconnect={connectedFromDone} />
  {:else if screen === 'existing' && check}
    <Existing {check} onback={disconnectAndBack} onadopted={adopted} onreinstall={reinstall} />
  {:else if screen === 'refused' && check}
    <Refused {check} onback={disconnectAndBack} />
  {/if}
</main>

<style>
  main { max-width: 380px; margin: 0 auto; min-height: 100vh; }
  .fatal { justify-content: center; text-align: center; }
</style>
