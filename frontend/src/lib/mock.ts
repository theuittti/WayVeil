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
  ['ssh', 'Connecting to the server'],
  ['download', 'Downloading Xray'],
  ['install', 'Installing Xray'],
  ['config', 'Writing the configuration'],
  ['firewall', 'Opening the firewall'],
  ['start', 'Starting the service'],
  ['verify', 'Checking that it works'],
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

async function runInstall(): Promise<Done> {
  stopScan()
  for (const [id, title] of steps) {
    const ev = (status: string): StepEvent => ({ id, status, title, message: '', at: Date.now() })
    emit('step', ev('start'))
    await sleep(400)
    emit('step', ev('ok'))
  }
  state = { ...state, hasOwner: true }
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
    const base: HostCheck = {
      fingerprint: 'SHA256:mockFingerprintmockFingerprintmockFingerprint0', pinned: false,
      os: 'Ubuntu 24.04', arch: 'amd64', state: 'fresh', detail: 'Ubuntu 24.04', message: '',
    }
    if (host.includes('wayveil')) return { ...base, state: 'wayveil' }
    if (host.includes('partial')) return { ...base, state: 'partial', message: 'A previous installation did not finish.' }
    if (host.includes('busy')) return { ...base, state: 'port-busy', detail: 'nginx', message: 'Port 443 is used by nginx. Free it and try again.' }
    stopScan()
    targets.forEach((t, i) => timers.push(setTimeout(() => emit('target', t), 500 + i * 550)))
    timers.push(setTimeout(() => emit('target-done', {}), 500 + targets.length * 550))
    return base
  },
  async ServerDisconnect() { stopScan() },
  async CheckDomain(d) {
    await sleep(300)
    return d.trim() ? { ok: true, problems: [], alternatives: [] } : { ok: false, problems: ['Enter a website domain.'], alternatives: [] }
  },
  async Install() { return runInstall() },
  async Adopt() { await sleep(500); return link(host) },
  async Reinstall() { return runInstall() },
  on(event, cb) {
    let set = listeners.get(event)
    if (!set) listeners.set(event, (set = new Set()))
    set.add(cb as (d: never) => void)
    return () => { set.delete(cb as (d: never) => void) }
  },
}
