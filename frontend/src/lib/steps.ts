import type { StepEvent } from './types'

export type RowStatus = 'pending' | 'running' | 'ok' | 'warn' | 'fail'

export interface StepRow {
  id: string
  title: string
  status: RowStatus
  log: { at: number; text: string }[]
}

// Same order as internal/server scripts.
const INSTALL = ['uninstaller', 'authkey', 'deps', 'xray', 'keys', 'config', 'service', 'bbr', 'logrotate', 'verify']
const UNINSTALL = ['service', 'files', 'sysctl', 'packages', 'user', 'authkey'].map((s) => 'uninstall-' + s)

const TITLES: Record<string, string> = {
  uninstaller: 'Preparing clean removal', authkey: 'App access key', deps: 'Packages',
  xray: 'Xray', keys: 'REALITY keys', config: 'Configuration', service: 'Service',
  bbr: 'BBR', logrotate: 'Log rotation', verify: 'Final check',
}

const BIG = 1000
function rank(id: string): number {
  let i = UNINSTALL.indexOf(id)
  if (i >= 0) return i
  i = INSTALL.indexOf(id)
  if (i >= 0) return 100 + i
  return BIG
}

// The install steps, all pending (uninstall-* rows appear when their events do).
export function initial(): StepRow[] {
  return INSTALL.map((id) => ({ id, title: TITLES[id], status: 'pending' as const, log: [] }))
}

function sorted(list: StepRow[]): StepRow[] {
  return list
    .map((r, n) => ({ r, n }))
    .sort((a, b) => rank(a.r.id) - rank(b.r.id) || a.n - b.n)
    .map((x) => x.r)
}

// Folds a step event into the rows (returns a new list). After a failure,
// events for later rows are ignored so they stay pending.
export function apply(list: StepRow[], ev: StepEvent): StepRow[] {
  const failed = list.find((r) => r.status === 'fail')
  if (failed && failed.id !== ev.id && rank(ev.id) >= rank(failed.id)) return list

  const status: RowStatus = ev.status === 'start' ? 'running' : (ev.status as RowStatus)
  const entry = ev.message ? [{ at: ev.at, text: ev.message }] : []
  const i = list.findIndex((r) => r.id === ev.id)
  if (i < 0) {
    return sorted([...list, { id: ev.id, title: ev.title || ev.id, status, log: entry }])
  }
  return list.map((r, j) =>
    j === i ? { ...r, title: ev.title || r.title, status, log: [...r.log, ...entry] } : r,
  )
}
