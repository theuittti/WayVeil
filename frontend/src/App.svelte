<script lang="ts">
  import { onMount } from 'svelte'
  import { api, type UIState } from './lib/backend'
  import { next, type FlowEvent, type Screen } from './lib/flow'
  import Paste from './screens/Paste.svelte'
  import Main from './screens/Main.svelte'

  let screen = $state<Screen | null>(null)
  let ui = $state<UIState>({ hasProfile: false, profileName: '', server: '', hasOwner: false, mode: 'smart' })

  function send(ev: FlowEvent) {
    screen = next(screen ?? 'paste', ev)
  }

  async function refresh() {
    ui = await api.State()
  }

  onMount(async () => {
    await refresh()
    send({ type: 'start', state: ui })
  })

  // Task 7 replaces the placeholders below with the real wizard screens.
  const placeholder: Partial<Record<Screen, { next: FlowEvent; label: string }>> = {
    access: { next: { type: 'connected', check: { fingerprint: '', pinned: false, os: '', arch: '', state: 'fresh', detail: '', message: '' } }, label: 'Connect to the server' },
    target: { next: { type: 'chooseTarget' }, label: 'Choose a target' },
    installing: { next: { type: 'installFinished' }, label: 'Finish installing' },
    done: { next: { type: 'connect' }, label: 'Connect' },
    existing: { next: { type: 'reinstall' }, label: 'Install again' },
    refused: { next: { type: 'back' }, label: 'Back' },
  }
</script>

<main>
  {#if screen === 'paste'}
    <Paste
      onimported={async () => { await refresh(); send({ type: 'imported' }) }}
      onownserver={() => send({ type: 'ownServer' })}
    />
  {:else if screen === 'main'}
    <Main {ui} onopenwizard={() => send({ type: 'openWizard' })} onreplacelink={() => send({ type: 'replaceLink' })} />
  {:else if screen && placeholder[screen]}
    <div class="ph">
      <p>{screen}</p>
      <button onclick={() => send(placeholder[screen!]!.next)}>{placeholder[screen]!.label}</button>
      <button onclick={() => send({ type: 'back' })}>Back</button>
    </div>
  {/if}
</main>

<style>
  main { max-width: 380px; margin: 0 auto; min-height: 100vh; }
  .ph { padding: 80px 24px; display: flex; flex-direction: column; gap: 12px; align-items: center; }
</style>
