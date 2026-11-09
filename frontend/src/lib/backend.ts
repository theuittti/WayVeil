import { mock } from './mock'
import type { Api, Events } from './types'

export * from './types'

type Methods = Omit<Api, 'on' | 'PickQRFile'>
type Bound = Record<string, (...args: unknown[]) => Promise<unknown>>

interface WailsWindow {
  go?: {
    gui?: { App?: Bound }
    main?: { Dialogs?: Bound }
  }
  runtime?: { EventsOn(name: string, cb: (data: unknown) => void): () => void }
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
  api.on = <K extends keyof Events>(event: K, cb: (data: Events[K]) => void) =>
    w.runtime!.EventsOn(event, cb as (data: unknown) => void)
  return api as unknown as Api
}

const w = window as unknown as WailsWindow
const useMock = !!import.meta.env.VITE_MOCK || !w.go?.gui?.App

export const api: Api = useMock ? mock : real(w)
export const on = api.on
