import { describe, it, expect } from 'vitest'
import { relativeTime, formatDateTime } from './time'

const NOW = Date.parse('2026-06-08T12:00:00Z')

describe('relativeTime', () => {
  it('returns — for null/invalid', () => {
    expect(relativeTime(null, NOW)).toBe('—')
    expect(relativeTime('nonsense', NOW)).toBe('—')
  })
  it('formats recent past', () => {
    expect(relativeTime('2026-06-08T11:59:30Z', NOW)).toBe('just now')
    expect(relativeTime('2026-06-08T11:30:00Z', NOW)).toBe('30m ago')
    expect(relativeTime('2026-06-08T09:00:00Z', NOW)).toBe('3h ago')
    expect(relativeTime('2026-06-05T12:00:00Z', NOW)).toBe('3d ago')
    expect(relativeTime('2025-06-13T12:00:00Z', NOW)).toBe('12mo ago') // 360 days — the boundary the bug missed
    expect(relativeTime('2025-12-08T12:00:00Z', NOW)).toBe('6mo ago')  // ~182 days
    expect(relativeTime('2024-06-08T12:00:00Z', NOW)).toBe('2y ago')   // ~730 days
  })
  it('clamps future timestamps to just now', () => {
    expect(relativeTime('2026-06-09T12:00:00Z', NOW)).toBe('just now')
  })
})
describe('formatDateTime', () => {
  it('returns — for null/invalid', () => {
    expect(formatDateTime(null)).toBe('—')
    expect(formatDateTime('nonsense')).toBe('—')
  })
  it('returns a locale string for a valid date', () => {
    expect(formatDateTime('2026-06-08T12:00:00Z')).toContain('2026')
  })
})
