import { describe, expect, it } from 'vitest'
import { defaultTarget, merge, russian, visible } from './targets'
import type { TargetEvent } from './types'

const t = (domain: string, kind: string, recommended = false): TargetEvent => ({
  domain, kind, recommended, handshakeMs: 50,
})
const fold = (evs: TargetEvent[]) => evs.reduce(merge, [] as TargetEvent[])
const names = (l: TargetEvent[]) => l.map((e) => e.domain)

describe('merge', () => {
  it('dedupes by domain', () => {
    const l = fold([t('a.com', 'nearby'), t('a.com', 'nearby')])
    expect(names(l)).toEqual(['a.com'])
  })
  it('orders recommended, nearby, country, russian', () => {
    const l = fold([t('ru.ru', 'russian'), t('c.com', 'country'), t('n.com', 'nearby'), t('r.com', 'nearby', true)])
    expect(names(l)).toEqual(['r.com', 'n.com', 'c.com', 'ru.ru'])
  })
  it('keeps arrival order within a group', () => {
    const l = fold([t('b.com', 'nearby'), t('a.com', 'nearby')])
    expect(names(l)).toEqual(['b.com', 'a.com'])
  })
  it('a late recommended replaces an earlier plain entry', () => {
    const l = fold([t('a.com', 'nearby'), t('b.com', 'nearby'), t('b.com', 'nearby', true)])
    expect(names(l)).toEqual(['b.com', 'a.com'])
    expect(l[0].recommended).toBe(true)
  })
  it('a later plain duplicate does not downgrade a recommended one', () => {
    const l = fold([t('a.com', 'nearby', true), t('a.com', 'nearby')])
    expect(l).toHaveLength(1)
    expect(l[0].recommended).toBe(true)
  })
  it('does not mutate its input', () => {
    const l = fold([t('a.com', 'nearby')])
    merge(l, t('b.com', 'country'))
    expect(l).toHaveLength(1)
  })
})

describe('visible / russian', () => {
  const l = fold([t('n.com', 'nearby'), t('ru.ru', 'russian'), t('vk.com', 'russian')])
  it('visible leaves russian ones out', () => expect(names(visible(l))).toEqual(['n.com']))
  it('russian returns only russian ones', () => expect(names(russian(l))).toEqual(['ru.ru', 'vk.com']))
})

describe('defaultTarget', () => {
  it('prefers the recommended one', () => {
    expect(defaultTarget(fold([t('c.com', 'country'), t('r.com', 'nearby', true)]))).toBe('r.com')
  })
  it('then the first country one', () => {
    expect(defaultTarget(fold([t('n.com', 'nearby'), t('c.com', 'country')]))).toBe('c.com')
  })
  it('falls back to www.microsoft.com', () => {
    expect(defaultTarget(fold([t('n.com', 'nearby'), t('ru.ru', 'russian')]))).toBe('www.microsoft.com')
    expect(defaultTarget([])).toBe('www.microsoft.com')
  })
})
