<script lang="ts">
  import { api, type AccessFields, type HostCheck } from '../lib/backend'

  let { fields = $bindable(), onback, onconnected }: {
    fields: AccessFields
    onback: () => void
    onconnected: (check: HostCheck) => void
  } = $props()

  let auth = $state<'password' | 'key'>('password')
  let password = $state('')
  let privateKey = $state('')
  let busy = $state(false)
  let error = $state('')
  let pending = $state<HostCheck | null>(null)

  async function connect() {
    if (busy) return
    error = ''
    busy = true
    try {
      const check = await api.ServerConnect({
        host: fields.host,
        port: Number(fields.port) || 22,
        user: fields.user,
        password: auth === 'password' ? password : '',
        privateKey: auth === 'key' ? privateKey : '',
      })
      if (check.pinned) onconnected(check)
      else pending = check
    } catch (e) {
      error = String(e)
    } finally {
      password = '' // never kept after the call
      busy = false
    }
  }

  async function reject() {
    pending = null
    try {
      await api.ServerDisconnect()
    } catch (e) {
      error = String(e)
    }
  }
</script>

<section class="wz">
  <div class="top"><button class="back" onclick={onback}>‹ Back</button></div>
  <div class="step">Step 1 of 3</div>
  <h2>Your server</h2>
  <p class="mut">The details from your hosting provider's email. The password is used once and never saved.</p>

  {#if pending}
    <div class="card fp">
      <div class="small mut">Server fingerprint</div>
      <code>{pending.fingerprint}</code>
    </div>
    <p>Is this your server?</p>
    <div class="btns">
      <button class="primary" onclick={() => { const c = pending!; pending = null; onconnected(c) }}>Yes, continue</button>
      <button class="ghost" onclick={reject}>No</button>
    </div>
  {:else}
    <div class="grow">
      <label><span>IP or address</span>
        <input bind:value={fields.host} spellcheck="false" autocomplete="off" placeholder="203.0.113.7" />
      </label>
      <div class="row">
        <label style="flex: 1"><span>Port</span>
          <input type="text" inputmode="numeric" bind:value={fields.port} />
        </label>
        <label style="flex: 2"><span>User</span>
          <input bind:value={fields.user} spellcheck="false" autocomplete="off" />
        </label>
      </div>
      <div class="seg">
        <button class:on={auth === 'password'} onclick={() => (auth = 'password')}>Password</button>
        <button class:on={auth === 'key'} onclick={() => (auth = 'key')}>Key</button>
      </div>
      {#if auth === 'password'}
        <input type="password" bind:value={password} autocomplete="off" aria-label="Password"
          onkeydown={(e) => e.key === 'Enter' && connect()} />
      {:else}
        <textarea bind:value={privateKey} spellcheck="false" aria-label="Private key"
          placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"></textarea>
      {/if}
      {#if error}<p class="err">{error}</p>{/if}
    </div>
    <button class="primary" disabled={busy} onclick={connect}>{busy ? 'Connecting…' : 'Connect'}</button>
  {/if}
</section>

<style>
  .fp code { display: block; margin-top: 6px; font-size: 12px; word-break: break-all; color: var(--link); }
</style>
