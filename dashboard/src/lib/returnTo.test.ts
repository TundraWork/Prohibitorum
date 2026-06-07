import { describe, it, expect } from 'vitest'
import { safeReturnTo } from './returnTo'

describe('safeReturnTo', () => {
  it('keeps a same-origin relative path', () =>
    expect(safeReturnTo('/me/security')).toBe('/me/security'))

  it('defaults empty string to /', () =>
    expect(safeReturnTo('')).toBe('/'))

  it('defaults undefined to /', () =>
    expect(safeReturnTo(undefined)).toBe('/'))

  it('rejects absolute cross-origin', () =>
    expect(safeReturnTo('https://evil.test/x')).toBe('/'))

  it('rejects protocol-relative //', () =>
    expect(safeReturnTo('//evil.test')).toBe('/'))

  it('rejects javascript: scheme', () =>
    expect(safeReturnTo('javascript:alert(1)')).toBe('/'))

  it('keeps a relative path with query string', () =>
    expect(safeReturnTo('/consent?ticket=abc')).toBe('/consent?ticket=abc'))

  it('rejects a bare scheme with colon', () =>
    expect(safeReturnTo('data:text/html,x')).toBe('/'))
})
