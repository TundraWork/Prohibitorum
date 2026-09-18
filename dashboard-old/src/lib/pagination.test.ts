import { describe, it, expect } from 'vitest'
import { type Page, unwrap, emptyPage, buildPagePath } from './pagination'

describe('Page<T>', () => {
  it('unwrap returns items and nextCursor from a page envelope', () => {
    const page: Page<number> = { items: [1, 2, 3], nextCursor: 'abc' }
    expect(unwrap(page)).toEqual({ items: [1, 2, 3], nextCursor: 'abc' })
  })

  it('unwrap on a missing page returns empty items and empty cursor', () => {
    expect(unwrap(undefined)).toEqual({ items: [], nextCursor: '' })
  })

  it('emptyPage produces a zero-item page with empty cursor', () => {
    const p = emptyPage<string>()
    expect(p.items).toEqual([])
    expect(p.nextCursor).toBe('')
  })

  it('buildPagePath appends cursor and limit params', () => {
    const path = buildPagePath('/api/prohibitorum/accounts', { cursor: 'xyz', limit: 10 })
    expect(path).toContain('cursor=xyz')
    expect(path).toContain('limit=10')
  })

  it('buildPagePath omits cursor when empty', () => {
    const path = buildPagePath('/api/prohibitorum/accounts', { cursor: '', limit: 10 })
    expect(path).not.toContain('cursor=')
    expect(path).toContain('limit=10')
  })

  it('buildPagePath omits limit when undefined', () => {
    const path = buildPagePath('/api/prohibitorum/accounts', {})
    expect(path).toBe('/api/prohibitorum/accounts')
  })

  it('buildPagePath preserves existing query params', () => {
    const path = buildPagePath('/api/prohibitorum/audit-events?factor=webauthn', { cursor: 'c1' })
    expect(path).toContain('factor=webauthn')
    expect(path).toContain('cursor=c1')
  })

  it('buildPagePath safely encodes account filter params and omits empty values', () => {
    const path = buildPagePath('/api/prohibitorum/accounts', {
      q: 'Alice & Bob',
      provider: 'vrchat',
      field: 'displayName',
      value: 'A&B / 星',
      match: 'contains',
      cursor: '',
    })
    expect(path).toBe(
      '/api/prohibitorum/accounts?q=Alice+%26+Bob&provider=vrchat&field=displayName&value=A%26B+%2F+%E6%98%9F&match=contains',
    )
  })
})
