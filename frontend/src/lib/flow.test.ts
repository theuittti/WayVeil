import { describe, expect, it } from 'vitest'
import { next, type Screen } from './flow'
import type { HostCheck, UIState } from './types'

const ui = (hasProfile: boolean): UIState => ({
  hasProfile, profileName: '', server: '', hasOwner: false, mode: 'smart',
})
const host = (state: string): HostCheck => ({
  fingerprint: '', pinned: false, os: '', arch: '', state, detail: '', message: '',
})

describe('next', () => {
  it('start with a profile goes to main', () => {
    expect(next('paste', { type: 'start', state: ui(true) })).toBe('main')
  })
  it('start without a profile goes to paste', () => {
    expect(next('main', { type: 'start', state: ui(false) })).toBe('paste')
  })
  it('ownServer goes from paste to access', () => {
    expect(next('paste', { type: 'ownServer' })).toBe('access')
  })
  it('openWizard goes from main to access', () => {
    expect(next('main', { type: 'openWizard' })).toBe('access')
  })
  it('replaceLink goes from main to paste', () => {
    expect(next('main', { type: 'replaceLink' })).toBe('paste')
  })
  it('imported goes to main', () => {
    expect(next('paste', { type: 'imported' })).toBe('main')
  })
  it('connected fresh goes to target', () => {
    expect(next('access', { type: 'connected', check: host('fresh') })).toBe('target')
  })
  it('connected wayveil goes to existing', () => {
    expect(next('access', { type: 'connected', check: host('wayveil') })).toBe('existing')
  })
  it('connected partial goes to existing', () => {
    expect(next('access', { type: 'connected', check: host('partial') })).toBe('existing')
  })
  it.each(['foreign', 'unsupported', 'port-busy'])('connected %s goes to refused', (s) => {
    expect(next('access', { type: 'connected', check: host(s) })).toBe('refused')
  })
  it('back from target goes to access', () => {
    expect(next('target', { type: 'back' })).toBe('access')
  })
  it('back from existing and refused goes to access', () => {
    expect(next('existing', { type: 'back' })).toBe('access')
    expect(next('refused', { type: 'back' })).toBe('access')
  })
  it('back from access goes to paste', () => {
    expect(next('access', { type: 'back' })).toBe('paste')
  })
  it('chooseTarget goes from target to installing', () => {
    expect(next('target', { type: 'chooseTarget' })).toBe('installing')
  })
  it('installFinished goes to done', () => {
    expect(next('installing', { type: 'installFinished' })).toBe('done')
  })
  it('installFailed stays on installing (the screen offers a retry)', () => {
    expect(next('installing', { type: 'installFailed' })).toBe('installing')
  })
  it('adopt goes from existing to done', () => {
    expect(next('existing', { type: 'adopt' })).toBe('done')
  })
  it('reinstall goes from existing to installing', () => {
    expect(next('existing', { type: 'reinstall' })).toBe('installing')
  })
  it('connect goes from done to main', () => {
    expect(next('done', { type: 'connect' })).toBe('main')
  })
  it('ignores events that do not apply', () => {
    const s: Screen = 'main'
    expect(next(s, { type: 'chooseTarget' })).toBe('main')
    expect(next('paste', { type: 'connect' })).toBe('paste')
  })
})
