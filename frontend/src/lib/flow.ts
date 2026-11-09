import type { HostCheck, UIState } from './types'

export type Screen =
  | 'paste' | 'main' | 'access' | 'target' | 'installing' | 'done' | 'existing' | 'refused'

export type FlowEvent =
  | { type: 'start'; state: UIState }
  | { type: 'ownServer' }
  | { type: 'back'; hasProfile?: boolean }
  | { type: 'connected'; check: HostCheck }
  | { type: 'chooseTarget' }
  | { type: 'installFinished' }
  | { type: 'installFailed' }
  | { type: 'adopt' }
  | { type: 'reinstall' }
  | { type: 'imported' }
  | { type: 'openWizard' }
  | { type: 'replaceLink' }
  | { type: 'connect' }

// Pure screen-flow reducer; events that do not apply leave the screen as is.
export function next(s: Screen, ev: FlowEvent): Screen {
  switch (ev.type) {
    case 'start':
      return ev.state.hasProfile ? 'main' : 'paste'
    case 'ownServer':
      return s === 'paste' ? 'access' : s
    case 'openWizard':
      return s === 'main' ? 'access' : s
    case 'replaceLink':
      return s === 'main' ? 'paste' : s
    case 'imported':
      return s === 'paste' ? 'main' : s
    case 'connected':
      if (s !== 'access') return s
      switch (ev.check.state) {
        case 'fresh': return 'target'
        case 'wayveil':
        case 'partial': return 'existing'
        default: return 'refused' // foreign, unsupported, port-busy
      }
    case 'back':
      if (s === 'target' || s === 'existing' || s === 'refused') return 'access'
      if (s !== 'access') return s
      return ev.hasProfile ? 'main' : 'paste'
    case 'chooseTarget':
      return s === 'target' ? 'installing' : s
    case 'installFinished':
      return s === 'installing' ? 'done' : s
    case 'installFailed':
      return s // stay: the installing screen shows the failure and a retry
    case 'adopt':
      return s === 'existing' ? 'done' : s
    case 'reinstall':
      return s === 'existing' || s === 'installing' ? 'installing' : s
    case 'connect':
      return s === 'done' ? 'main' : s
  }
}
