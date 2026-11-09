import { mock } from './mock'
import type { Api, Events } from './types'

export * from './types'

type Methods = Omit<Api, 'on' | 'PickQRFile' | 'copyText' | 'readClipboard'>
type Bound = Record<string, (...args: unknown[]) => Promise<unknown>>

interface WailsWindow {
  go?: {
    gui?: { App?: Bound }
    main?: { Dialogs?: Bound }
  }
  runtime?: {
    EventsOn(name: string, cb: (data: unknown) => void): () => void
    ClipboardSetText?(text: string): Promise<boolean>
    ClipboardGetText?(): Promise<string>
  }
}

// Clipboard: the Wails runtime when present (navigator.clipboard is not
// reliable in WebKitGTK), else the browser API (mock / `make ui`).
export async function copyText(text: string): Promise<void> {
  const rt = (window as unknown as WailsWindow).runtime
  if (rt?.ClipboardSetText) {
    if (!(await rt.ClipboardSetText(text))) throw new Error('clipboard')
    return
  }
  await navigator.clipboard.writeText(text)
}

export async function readClipboard(): Promise<string> {
  const rt = (window as unknown as WailsWindow).runtime
  if (rt?.ClipboardGetText) return (await rt.ClipboardGetText()) ?? ''
  return navigator.clipboard.readText()
}

function real(w: WailsWindow): Api {
  const app = w.go!.gui!.App!
  const call = (name: string) => (...args: unknown[]) => app[name](...args)
  const names: (keyof Methods)[] = [
    'State', 'ImportLink', 'ImportQRFile', 'Connect', 'Disconnect', 'Status', 'SetMode',
    'ServerConnect', 'ServerDisconnect', 'CheckDomain', 'Install', 'Adopt', 'Reinstall',
  ]
  const api: Record<string, unknown> = {}
  for (const n of names) api[n] = call(n)
  api.PickQRFile = async () => {
    const pick = w.go?.main?.Dialogs?.PickQRFile
    return pick ? ((await pick()) as string) : ''
  }
  api.copyText = copyText
  api.readClipboard = readClipboard
  api.on = <K extends keyof Events>(event: K, cb: (data: Events[K]) => void) =>
    w.runtime!.EventsOn(event, cb as (data: unknown) => void)
  return api as unknown as Api
}

// Every call rejects; App shows the fatal screen instead of calling it.
const missing = new Proxy({} as Api, {
  get: (_, name) => {
    if (name === 'then') return undefined // not a thenable
    return name === 'on' ? () => () => {} : () => Promise.reject(backendError)
  },
})

const w = window as unknown as WailsWindow
const useMock = !!import.meta.env.VITE_MOCK || import.meta.env.DEV
const hasBackend = !!w.go?.gui?.App

export const backendError = 'Wayveil could not start its backend.'
// False in a production build without the Go backend: App shows backendError.
export const backendOK = useMock || hasBackend

export const api: Api = useMock ? mock : hasBackend ? real(w) : missing
export const on = api.on
