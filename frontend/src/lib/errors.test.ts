import { describe, expect, it } from 'vitest'
import { isHostKeyChanged, needsReconnect } from './errors'

describe('needsReconnect', () => {
  it('is true for a lost connection, with or without a step', () => {
    expect(needsReconnect('The connection to the server was lost during “Packages”. Connect again — Wayveil will offer to clean up and install again.')).toBe(true)
    expect(needsReconnect('The connection to the server was lost. Connect again.')).toBe(true)
  })
  it('is true when the session is gone', () => {
    expect(needsReconnect('Not connected to the server. Go back and connect again.')).toBe(true)
  })
  it('is false for other failures', () => {
    expect(needsReconnect('Installation failed at step Packages: apt is locked')).toBe(false)
    expect(needsReconnect('')).toBe(false)
  })
})

describe('isHostKeyChanged', () => {
  it('matches the host key warning only', () => {
    expect(isHostKeyChanged("The server's identity changed since last time. This may be an attack — not continuing.")).toBe(true)
    expect(isHostKeyChanged('Could not log in: check the password or key.')).toBe(false)
  })
})
