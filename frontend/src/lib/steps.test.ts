import { describe, expect, it } from 'vitest'
import { apply, initial } from './steps'
import type { StepEvent } from './types'

const ev = (id: string, status: string, message = '', at = 1000): StepEvent => ({
  id, status, title: id, message, at,
})
const fold = (evs: StepEvent[], start = initial()) => evs.reduce(apply, start)
const ids = (l: { id: string }[]) => l.map((r) => r.id)
const status = (l: { id: string; status: string }[], id: string) => l.find((r) => r.id === id)?.status

describe('initial', () => {
  it('lists the install steps as pending, in order', () => {
    const l = initial()
    expect(ids(l)).toEqual(['uninstaller', 'authkey', 'deps', 'xray', 'keys', 'config', 'service', 'bbr', 'logrotate', 'verify'])
    expect(l.every((r) => r.status === 'pending')).toBe(true)
  })
})

describe('apply', () => {
  it('start makes a row running, ok finishes it', () => {
    let l = fold([ev('deps', 'start')])
    expect(status(l, 'deps')).toBe('running')
    l = apply(l, ev('deps', 'ok'))
    expect(status(l, 'deps')).toBe('ok')
  })
  it('takes the title from the event', () => {
    const l = fold([{ ...ev('deps', 'start'), title: 'Packages' }])
    expect(l.find((r) => r.id === 'deps')?.title).toBe('Packages')
  })
  it('puts uninstall-* rows first, in uninstall order', () => {
    const l = fold([ev('uninstall-service', 'start'), ev('uninstall-service', 'ok'), ev('uninstall-files', 'start'), ev('uninstaller', 'start')])
    expect(ids(l).slice(0, 2)).toEqual(['uninstall-service', 'uninstall-files'])
    expect(ids(l)[2]).toBe('uninstaller')
  })
  it('keeps install order even when events arrive out of order', () => {
    const l = fold([ev('xray', 'start'), ev('authkey', 'start')], [])
    expect(ids(l)).toEqual(['authkey', 'xray'])
  })
  it('appends unknown ids after the known ones', () => {
    const l = fold([ev('mystery', 'start')])
    expect(ids(l).at(-1)).toBe('mystery')
  })
  it('keeps messages with their times', () => {
    const l = fold([ev('deps', 'start', 'apt update', 5), ev('deps', 'ok', 'done', 9)])
    expect(l.find((r) => r.id === 'deps')?.log).toEqual([
      { at: 5, text: 'apt update' }, { at: 9, text: 'done' },
    ])
  })
  it('warn is shown as warn', () => {
    expect(status(fold([ev('bbr', 'warn', 'not available')]), 'bbr')).toBe('warn')
  })
  it('fail freezes later rows as pending', () => {
    const l = fold([ev('deps', 'ok'), ev('xray', 'fail', 'boom'), ev('keys', 'start'), ev('config', 'ok')])
    expect(status(l, 'xray')).toBe('fail')
    expect(status(l, 'keys')).toBe('pending')
    expect(status(l, 'config')).toBe('pending')
    expect(status(l, 'deps')).toBe('ok')
  })
  it('does not mutate its input', () => {
    const l = initial()
    apply(l, ev('deps', 'start'))
    expect(status(l, 'deps')).toBe('pending')
  })
})
