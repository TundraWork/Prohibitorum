import { afterEach, describe, expect, it, vi } from 'vitest'
import { createTotpEnrollment } from './totp'

afterEach(() => vi.restoreAllMocks())

describe('createTotpEnrollment', () => {
  it('uses 20 cryptographic random bytes and returns unpadded RFC 4648 Base32', () => {
    const source = new TextEncoder().encode('12345678901234567890')
    const random = vi.spyOn(crypto, 'getRandomValues').mockImplementation(array => {
      expect(array).toBeInstanceOf(Uint8Array)
      expect(array.byteLength).toBe(20)
      new Uint8Array(array.buffer, array.byteOffset, array.byteLength).set(source)
      return array
    })

    const enrollment = createTotpEnrollment('alice', {
      issuer: 'Prohibitorum', algorithm: 'SHA1', digits: 6, period: 30,
    })

    expect(random).toHaveBeenCalledOnce()
    expect(enrollment.secretBase32).toBe('GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ')
    expect(enrollment.secretBase32).toMatch(/^[A-Z2-7]{32}$/)
  })

  it('builds the otpauth URI from the account label and public server settings', () => {
    vi.spyOn(crypto, 'getRandomValues').mockImplementation(array => {
      new Uint8Array(array.buffer, array.byteOffset, array.byteLength).fill(0xff)
      return array
    })

    const { secretBase32, otpauthUri } = createTotpEnrollment('alice/team@example.com', {
      issuer: 'Example 例', algorithm: 'SHA256', digits: 8, period: 45,
    })
    const uri = new URL(otpauthUri)

    expect(uri.protocol).toBe('otpauth:')
    expect(uri.hostname).toBe('totp')
    expect(decodeURIComponent(uri.pathname)).toBe('/Example 例:alice/team@example.com')
    expect(Object.fromEntries(uri.searchParams)).toEqual({
      secret: secretBase32,
      issuer: 'Example 例',
      algorithm: 'SHA256',
      digits: '8',
      period: '45',
    })
  })
})
