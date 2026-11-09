import type { TargetEvent } from './types'

const FALLBACK = 'www.microsoft.com'

function rank(e: TargetEvent): number {
  if (e.recommended) return 0
  switch (e.kind) {
    case 'nearby': return 1
    case 'country': return 2
    case 'russian': return 4
    default: return 3
  }
}

// Adds a suggestion: dedupe by domain (a recommended one replaces a plain one,
// never the other way round), ordered recommended, nearby, country, russian;
// arrival order is kept inside a group. Returns a new list.
export function merge(list: TargetEvent[], ev: TargetEvent): TargetEvent[] {
  const i = list.findIndex((e) => e.domain === ev.domain)
  let out: TargetEvent[]
  if (i < 0) out = [...list, ev]
  else if (ev.recommended && !list[i].recommended) out = list.map((e, j) => (j === i ? ev : e))
  else return list
  return out
    .map((e, n) => ({ e, n }))
    .sort((a, b) => rank(a.e) - rank(b.e) || a.n - b.n)
    .map((x) => x.e)
}

// The main list: Russian sites are shown only as a caption line.
export const visible = (list: TargetEvent[]): TargetEvent[] => list.filter((e) => e.kind !== 'russian')

export const russian = (list: TargetEvent[]): TargetEvent[] => list.filter((e) => e.kind === 'russian')

// Pre-selected domain: first recommended, else first country one, else a safe default.
export function defaultTarget(list: TargetEvent[]): string {
  return (
    list.find((e) => e.recommended)?.domain ??
    list.find((e) => e.kind === 'country')?.domain ??
    FALLBACK
  )
}
