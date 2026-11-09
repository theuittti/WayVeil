// JSON shapes of internal/gui (field names match the Go json tags).

export interface UIState {
  hasProfile: boolean
  profileName: string
  server: string
  hasOwner: boolean
  mode: string // "smart" | "all"
}

export interface ConnStatus {
  state: string // "disconnected" | "connecting" | "connected" | "failed"
  since: number // unix ms, 0 when not connected
  up: number
  down: number
  error: string
}

export interface AccessForm {
  host: string
  port: number
  user: string
  password: string
  privateKey: string
}

// The non-secret part of the access form, kept across screens.
export interface AccessFields {
  host: string
  port: number
  user: string
}

export interface HostCheck {
  fingerprint: string
  pinned: boolean
  os: string
  arch: string
  state: string // "fresh" | "wayveil" | "partial" | "foreign" | "unsupported" | "port-busy"
  detail: string
  message: string // human text for partial and refused states, else ""
}

export interface DomainCheck {
  ok: boolean
  problems: string[]
  alternatives: string[]
}

export interface Done {
  link: string
  qr: string // data:image/png;base64,…
}

export interface TargetEvent {
  domain: string
  kind: string // "nearby" | "country" | "russian"
  recommended: boolean
  handshakeMs: number
}

export interface StepEvent {
  id: string
  status: string // "start" | "ok" | "warn" | "fail"
  title: string
  message: string
  at: number // unix ms
}

export interface Events {
  target: TargetEvent
  'target-done': Record<string, never>
  step: StepEvent
}

export interface Api {
  State(): Promise<UIState>
  ImportLink(link: string): Promise<UIState>
  ImportQRFile(path: string): Promise<UIState>
  PickQRFile(): Promise<string> // "" when the dialog is cancelled
  Connect(): Promise<ConnStatus>
  Disconnect(): Promise<ConnStatus>
  Status(): Promise<ConnStatus>
  SetMode(mode: string): Promise<ConnStatus>
  ServerConnect(form: AccessForm): Promise<HostCheck>
  ServerDisconnect(): Promise<void>
  CheckDomain(domain: string): Promise<DomainCheck>
  Install(sni: string): Promise<Done>
  Adopt(): Promise<Done>
  Reinstall(sni: string): Promise<Done>
  on<K extends keyof Events>(event: K, cb: (data: Events[K]) => void): () => void
}
