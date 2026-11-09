<script lang="ts">
  import { api, type Done } from '../lib/backend'

  // onconnect gets the Connect error ('' on success): Main shows it, so a
  // failed Connect never leaves the user stuck here.
  let { done, onconnect }: { done: Done; onconnect: (error: string) => void } = $props()

  let copied = $state(false)
  let busy = $state(false)
  let error = $state('')

  async function copy() {
    error = ''
    try {
      await api.copyText(done.link)
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
    let failure = ''
    try {
      await api.Connect()
    } catch (e) {
      failure = String(e)
    } finally {
      busy = false
    }
    onconnect(failure)
  }
</script>

<section class="wz">
  <div class="check ok">✓</div>
  <h2>Server is ready</h2>
  <p class="mut c">You are the first device, “Me”. You can add more devices later.</p>
  <div class="grow mid">
    <img src={done.qr} alt="QR code of your connection link" width="160" height="160" />
    <code class="lnk">{done.link}</code>
    {#if error}<p class="err">{error}</p>{/if}
  </div>
  <div class="btns">
    <button class="ghost" onclick={copy}>{copied ? 'Copied' : 'Copy link'}</button>
    <button class="primary" disabled={busy} onclick={connect}>{busy ? 'Connecting…' : 'Connect'}</button>
  </div>
</section>

<style>
  .check { text-align: center; font-size: 40px; margin-top: 16px; }
  h2, .c { text-align: center; }
  .mid { align-items: center; }
  img { background: #fff; padding: 8px; border-radius: 12px; flex: none; }
  .lnk {
    display: block; width: 100%; box-sizing: border-box; padding: 8px 10px; border-radius: 10px;
    background: var(--card); font-family: ui-monospace, monospace; font-size: 11px; line-height: 1.4;
    word-break: break-all; opacity: 0.85; user-select: text; -webkit-user-select: text; cursor: text;
  }
  .btns { margin-top: 0; }
</style>
