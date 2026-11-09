<script lang="ts">
  import { api } from '../lib/backend'

  // onback is set when a profile already exists (Main → Replace link).
  let { onimported, onownserver, onback }: {
    onimported: () => void
    onownserver: () => void
    onback?: () => void
  } = $props()

  let link = $state('')
  let error = $state('')
  let busy = $state(false)
  let input: HTMLInputElement | undefined = $state()

  async function run(f: () => Promise<unknown>) {
    error = ''
    busy = true
    try {
      await f()
      onimported()
    } catch (e) {
      error = String(e)
    } finally {
      busy = false
    }
  }

  const submit = () => run(() => api.ImportLink(link))

  async function fromClipboard() {
    try {
      link = await api.readClipboard()
    } catch {
      input?.focus()
      return
    }
    if (link.trim()) await submit()
    else input?.focus()
  }

  async function fromQR() {
    error = ''
    let path = ''
    try {
      path = await api.PickQRFile()
    } catch (e) {
      error = String(e)
      return
    }
    if (path) await run(() => api.ImportQRFile(path))
  }
</script>

<section class:hasback={!!onback}>
  {#if onback}<button class="back" onclick={onback}>‹ Back</button>{/if}
  <h1>Wayveil</h1>
  <p class="sub">Paste a connection link</p>
  <input
    bind:this={input}
    bind:value={link}
    placeholder="vless://…"
    spellcheck="false"
    autocomplete="off"
    onkeydown={(e) => e.key === 'Enter' && !busy && submit()}
  />
  {#if error}<p class="err">{error}</p>{/if}
  <div class="row">
    <button class="primary" disabled={busy} onclick={fromClipboard}>From clipboard</button>
    <button class="ghost" disabled={busy} onclick={fromQR}>QR image</button>
  </div>
  {#if link.trim()}
    <button class="primary wide" disabled={busy} onclick={submit}>Connect</button>
  {/if}
  <button class="linkbtn" onclick={onownserver}>I have my own server → set up</button>
</section>

<style>
  section {
    box-sizing: border-box;
    min-height: 100vh;
    padding: 56px 24px 24px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  section.hasback { padding-top: 18px; }
  .back {
    align-self: flex-start; min-height: 22px; margin-bottom: 2px;
    background: none; border: none; color: var(--link); font: inherit; cursor: pointer; padding: 0;
  }
  h1 { margin: 0; font-size: 28px; font-weight: 600; letter-spacing: 0.04em; text-align: center; }
  .sub { margin: 0 0 12px; text-align: center; opacity: 0.8; }
  input {
    padding: 12px 14px;
    border-radius: 12px;
    border: 1px solid rgba(255, 255, 255, 0.12);
    background: var(--card);
    color: var(--text);
    font: inherit;
  }
  input:focus { outline: 1px solid var(--link); }
  .err { margin: 0; color: var(--warn); font-size: 14px; }
  .row { display: flex; gap: 10px; }
  .row button { flex: 1; }
  button.primary, button.ghost {
    padding: 12px;
    border-radius: 12px;
    border: none;
    color: var(--text);
    font: inherit;
    cursor: pointer;
  }
  button.primary { background: var(--btn); box-shadow: var(--glow); }
  button.ghost { background: var(--card); }
  button:disabled { opacity: 0.6; cursor: default; }
  .linkbtn {
    margin-top: auto;
    background: none;
    border: none;
    color: var(--link);
    font: inherit;
    cursor: pointer;
    padding: 8px;
  }
</style>
