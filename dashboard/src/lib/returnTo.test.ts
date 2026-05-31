import { describe, it, expect } from 'vitest'
import { safeReturnTo } from './returnTo'

describe('safeReturnTo', () => {
  it('accepts same-origin absolute + relative', () => {
    expect(safeReturnTo(window.location.origin + '/oauth/authorize?x=1')).toContain('/oauth/authorize')
    expect(safeReturnTo('/oauth/authorize')).toContain('/oauth/authorize')
  })
  it('rejects cross-origin + empty', () => {
    expect(safeReturnTo('https://evil.example/x')).toBeNull()
    expect(safeReturnTo(null)).toBeNull()
    expect(safeReturnTo('')).toBeNull()
  })
  it('rejects protocol-relative and handles whitespace', () => {
    expect(safeReturnTo('//evil.example/x')).toBeNull()
    expect(safeReturnTo(' /oauth/authorize')).toContain('/oauth/authorize')
  })
})
