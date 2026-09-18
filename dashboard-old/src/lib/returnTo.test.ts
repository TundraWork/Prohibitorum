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

  // The server bounces unauthenticated users to /login with an ABSOLUTE
  // same-origin return_to (e.g. the OIDC /oauth/authorize URL). Accept it,
  // normalised to a relative path, preserving the (nested) query string.
  it('accepts a same-origin absolute URL, returning its relative path', () =>
    expect(safeReturnTo(window.location.origin + '/oauth/authorize?client_id=x&redirect_uri=http%3A%2F%2Frp%2Fcb'))
      .toBe('/oauth/authorize?client_id=x&redirect_uri=http%3A%2F%2Frp%2Fcb'))

  it('rejects a same-origin path that resolves protocol-relative (backslash trick)', () =>
    expect(safeReturnTo('/\\evil.test')).toBe('/'))
})
