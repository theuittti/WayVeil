<script lang="ts">
  import { api, type Done } from '../lib/backend'

  let { done, onconnect }: { done: Done; onconnect: () => void } = $props()

  let copied = $state(false)
  let busy = $state(false)
  let error = $state('')

  async function copy() {
    try {
      await navigator.clipboard.writeText(done.link)
      copied = true
      setTimeout(() => (copied = false), 2000)
    } catch {
      error = 'Could not copy. Select the link and copy it by hand.'
    }
  }

  async function connect() {
    if (busy) return
    error = ''
    busy = true
    try {
      await api.Connect()
      onconnect()
    } catch (e) {
      error = String(e)
    } finally {
      busy = false
    }
  }
</script>

<section class="wz">
  <div class="check ok">✓</div>
  <h2>Server is ready</h2>
  <p class="mut c">You are the first device, “Me”. You can add more devices later.</p>
  <div class="grow mid">
    <img src={done.qr} alt="QR code of your connection link" width="160" height="160" />
    {#if error}<p class="err">{error}</p>{/if}
  </div>
  <div class="btns">
    <button class="ghost" onclick={copy}>{copied ? 'Copied' : 'Copy link'}</button>
    <button class="primary" disabled={busy} onclick={connect}>Connect</button>
  </div>
</section>

<style>
  .check { text-align: center; font-size: 40px; margin-top: 16px; }
  h2, .c { text-align: center; }
  .mid { align-items: center; justify-content: center; }
  img { background: #fff; padding: 8px; border-radius: 12px; }
  .btns { margin-top: 0; }
</style>
