import type {
  AccessForm, Api, ConnStatus, Done, Events, HostCheck, StepEvent, TargetEvent, UIState,
} from './types'

// Scripted backend for browser preview (`make ui`): no Go, no network.

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms))

const listeners = new Map<string, Set<(d: never) => void>>()
function emit<K extends keyof Events>(event: K, data: Events[K]) {
  listeners.get(event)?.forEach((cb) => (cb as (d: Events[K]) => void)(data))
}

let state: UIState = { hasProfile: false, profileName: '', server: '', hasOwner: false, mode: 'smart' }
let conn: ConnStatus = { state: 'disconnected', since: 0, up: 0, down: 0, error: '' }
let connectGen = 0
let timers: ReturnType<typeof setTimeout>[] = []

function stopScan() {
  timers.forEach(clearTimeout)
  timers = []
}

const targets: TargetEvent[] = [
  { domain: 'www.microsoft.com', kind: 'nearby', recommended: true, handshakeMs: 38 },
  { domain: 'www.apple.com', kind: 'nearby', recommended: false, handshakeMs: 52 },
  { domain: 'www.samsung.com', kind: 'country', recommended: false, handshakeMs: 71 },
  { domain: 'www.nvidia.com', kind: 'country', recommended: false, handshakeMs: 88 },
  { domain: 'yandex.ru', kind: 'russian', recommended: false, handshakeMs: 120 },
]

const steps: [string, string][] = [
  ['uninstaller', 'Preparing clean removal'], ['authkey', 'App access key'], ['deps', 'Packages'],
  ['xray', 'Xray'], ['keys', 'REALITY keys'], ['config', 'Configuration'], ['service', 'Service'],
  ['bbr', 'BBR'], ['logrotate', 'Log rotation'], ['verify', 'Final check'],
]

function link(host: string): Done {
  return {
    link: `vless://00000000-0000-4000-8000-000000000000@${host}:443?security=reality&sni=www.microsoft.com#Me`,
    qr: 'data:image/svg+xml;utf8,' + encodeURIComponent(
      '<svg xmlns="http://www.w3.org/2000/svg" width="160" height="160"><rect width="160" height="160" fill="#fff"/>' +
      '<rect x="16" y="16" width="40" height="40"/><rect x="104" y="16" width="40" height="40"/>' +
      '<rect x="16" y="104" width="40" height="40"/><rect x="72" y="72" width="16" height="16"/></svg>'),
  }
}

let host = '203.0.113.7'
let clip = ''

// A host containing "fail" makes the first install lose the connection halfway,
// "aptfail" makes it fail at a step; Reinstall then works. "noperm": Connect fails.
let failNext: '' | 'lost' | 'apt' = ''
let connectFails = false

async function runInstall(): Promise<Done> {
  stopScan()
  const mode = failNext
  const failAt = mode ? 4 : -1
  failNext = ''
  for (const [n, [id, title]] of steps.entries()) {
    const ev = (status: string, message = ''): StepEvent => ({ id, status, title, message, at: Date.now() })
    emit('step', ev('start', 'running…'))
    await sleep(400)
    if (n === failAt) {
      if (mode === 'apt') {
        emit('step', ev('fail', 'E: Could not get lock /var/lib/dpkg/lock-frontend'))
        throw 'Installation failed at step ' + title + ': apt is locked by another process'
      }
      emit('step', ev('fail', 'connection lost'))
      throw 'The connection to the server was lost during “' + title + '”. Connect again — Wayveil will offer to clean up and install again.'
    }
    emit('step', ev('ok'))
  }
  return finish()
}

// Like App.finish: the "Me" link becomes the connection profile.
function finish(): Done {
  state = { ...state, hasOwner: true, hasProfile: true, profileName: 'Me', server: host + ':443' }
  return link(host)
}

function setState(s: Partial<UIState>): UIState {
  state = { ...state, ...s }
  return state
}

export const mock: Api = {
  async State() { return state },
  async ImportLink(l) {
    const t = l.trim()
    if (!t.startsWith('vless://')) throw 'This is not a vless:// connection link'
    return setState({ hasProfile: true, profileName: 'Wayveil', server: '203.0.113.7:443' })
  },
  async ImportQRFile(path) {
    if (!path) throw 'Pick an image first'
    return setState({ hasProfile: true, profileName: 'Wayveil', server: '203.0.113.7:443' })
  },
  async PickQRFile() { return '/tmp/mock-qr.png' },
  async Connect() {
    if (connectFails) { await sleep(300); throw 'Permission was not granted.' }
    const gen = ++connectGen
    conn = { state: 'connecting', since: 0, up: 0, down: 0, error: '' }
    setTimeout(() => {
      if (gen === connectGen) conn = { state: 'connected', since: Date.now(), up: 0, down: 0, error: '' }
    }, 1000)
    return conn
  },
  async Disconnect() {
    connectGen++
    conn = { state: 'disconnected', since: 0, up: 0, down: 0, error: '' }
    return conn
  },
  async Status() {
    if (conn.state === 'connected') {
      conn = { ...conn, up: conn.up + 2_000, down: conn.down + 40_000 }
    }
    return conn
  },
  async SetMode(mode) {
    setState({ mode })
    return conn
  },
  async ServerConnect(f: AccessForm): Promise<HostCheck> {
    await sleep(600)
    host = f.host.trim() || host
    failNext = host.includes('aptfail') ? 'apt' : host.includes('fail') ? 'lost' : ''
    connectFails = host.includes('noperm')
    const base: HostCheck = {
      fingerprint: 'SHA256:mockFingerprintmockFingerprintmockFingerprint0', pinned: false,
      os: 'Ubuntu 24.04', arch: 'amd64', state: 'fresh', detail: 'Ubuntu 24.04', message: '',
    }
    if (host.includes('changed')) throw "The server's identity changed since last time. This may be an attack — not continuing."
    if (host.includes('busy')) return { ...base, state: 'port-busy', detail: 'nginx', message: 'Port 443 is used by nginx. Free it and try again.' }
    stopScan() // fresh, wayveil and partial servers are scanned in the background
    targets.forEach((t, i) => timers.push(setTimeout(() => emit('target', t), 500 + i * 550)))
    timers.push(setTimeout(() => emit('target-done', {}), 500 + targets.length * 550))
    if (host.includes('wayveil')) return { ...base, state: 'wayveil' }
    if (host.includes('partial')) return { ...base, state: 'partial', message: 'A previous installation did not finish.' }
    return base
  },
  async ServerDisconnect() { stopScan() },
  async CheckDomain(d) {
    await sleep(300)
    if (d.includes('bad')) {
      return {
        ok: false,
        problems: ['This site does not support TLS 1.3.', 'It is far from your server (310 ms).'],
        alternatives: ['www.microsoft.com', 'www.apple.com'],
      }
    }
    return d.trim() ? { ok: true, problems: [], alternatives: [] } : { ok: false, problems: ['Enter a website domain.'], alternatives: [] }
  },
  async Install() { return runInstall() },
  async Adopt() { await sleep(500); return finish() },
  async Reinstall() { return runInstall() },
  // The browser clipboard; an in-memory one where it is not allowed (headless preview).
  async copyText(text) {
    clip = text
    try { await navigator.clipboard.writeText(text) } catch { /* keep the in-memory copy */ }
  },
  async readClipboard() {
    try { return await navigator.clipboard.readText() } catch { return clip }
  },
  on(event, cb) {
    let set = listeners.get(event)
    if (!set) listeners.set(event, (set = new Set()))
    set.add(cb as (d: never) => void)
    return () => { set.delete(cb as (d: never) => void) }
  },
}
