<script lang="ts">
  import { api, type Done, type HostCheck } from '../lib/backend'

  let { check, onback, onadopted, onreinstall }: {
    check: HostCheck
    onback: () => void
    onadopted: (d: Done) => void
    onreinstall: () => void
  } = $props()

  const ours = $derived(check.state === 'wayveil')
  const title = $derived(ours ? 'Wayveil is already installed on this server' : 'A previous installation did not finish')
  let busy = $state(false)
  let error = $state('')

  async function adopt() {
    if (busy) return
    error = ''
    busy = true
    try {
      onadopted(await api.Adopt())
    } catch (e) {
      error = String(e)
    } finally {
      busy = false
    }
  }
</script>

<section class="wz">
  <div class="top"><button class="back" onclick={onback}>‹ Back</button></div>
  <h2>{title}</h2>
  {#if !ours && check.message && check.message.replace(/\.$/, '') !== title}<p class="mut">{check.message}</p>{/if}
  <div class="grow">
    <p class="warn">Existing connection links will stop working.</p>
    {#if error}<p class="err">{error}</p>{/if}
  </div>
  <div class="btns">
    {#if ours}<button class="primary" disabled={busy} onclick={adopt}>{busy ? 'Working…' : 'Take over'}</button>{/if}
    <button class={ours ? 'ghost' : 'primary'} disabled={busy} onclick={onreinstall}>Reinstall</button>
  </div>
</section>
