import { describe, it, expect } from 'vitest'
import { buildTitle } from './pageTitle'

describe('buildTitle', () => {
  it('combines page and instance', () => {
    expect(buildTitle('Security', 'Acme SSO')).toBe('Security · Acme SSO')
  })
  it('returns instance alone when no page', () => {
    expect(buildTitle('', 'Acme SSO')).toBe('Acme SSO')
  })
})
