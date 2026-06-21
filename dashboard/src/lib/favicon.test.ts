import { describe, it, expect, beforeEach } from 'vitest'
import { setFavicon } from './favicon'

describe('setFavicon', () => {
  beforeEach(() => {
    document.head.querySelectorAll('link[rel="icon"]').forEach((el) => el.remove())
  })

  it('creates a single icon link with the given href', () => {
    setFavicon('/branding/icon?v=abc12345')
    const links = document.head.querySelectorAll('link[rel="icon"]')
    expect(links).toHaveLength(1)
    expect(links[0].getAttribute('href')).toBe('/branding/icon?v=abc12345')
    expect(links[0].getAttribute('type')).toBe('image/png')
  })

  it('replaces the previous icon link instead of stacking duplicates (forces refetch)', () => {
    setFavicon('/branding/icon?v=default')
    setFavicon('/branding/icon?v=newetag1')
    const links = document.head.querySelectorAll('link[rel="icon"]')
    expect(links).toHaveLength(1)
    expect(links[0].getAttribute('href')).toBe('/branding/icon?v=newetag1')
  })
})
