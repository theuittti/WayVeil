import { describe, expect, it } from 'vitest'
import { bytes, duration } from './format'

describe('bytes', () => {
  it('formats units', () => {
    expect(bytes(0)).toBe('0 B')
    expect(bytes(512)).toBe('512 B')
    expect(bytes(1536)).toBe('1.5 KB')
    expect(bytes(2.3 * 1024 ** 3)).toBe('2.3 GB')
  })
  it('handles large values', () => {
    expect(bytes(5 * 1024 ** 4)).toBe('5.0 TB')
    expect(bytes(5000 * 1024 ** 4)).toBe('5000 TB')
  })
  it('treats negative or NaN as 0', () => {
    expect(bytes(-5)).toBe('0 B')
    expect(bytes(NaN)).toBe('0 B')
  })
})

describe('duration', () => {
  it('formats m:ss', () => {
    expect(duration(0)).toBe('0:00')
    expect(duration(41_000)).toBe('0:41')
    expect(duration(62_000)).toBe('1:02')
  })
  it('formats h:mm:ss', () => {
    expect(duration(3_723_000)).toBe('1:02:03')
    expect(duration(100 * 3600_000)).toBe('100:00:00')
  })
  it('clamps negatives', () => {
    expect(duration(-1000)).toBe('0:00')
  })
})
