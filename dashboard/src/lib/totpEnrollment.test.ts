import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api } from '@/lib/api'
import { generateTotpEnrollment } from './totpEnrollment'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))

describe('generateTotpEnrollment', () => {
  beforeEach(() => vi.mocked(api.get).mockReset())

  it('loads public settings through the shared query and uses the account label', async () => {
    vi.mocked(api.get).mockResolvedValue({
      instanceName: 'Example',
      totp: { issuer: 'Example Auth', algorithm: 'SHA1', digits: 6, period: 30 },
    })
    vi.spyOn(crypto, 'getRandomValues').mockImplementation(array => {
      new Uint8Array(array.buffer, array.byteOffset, array.byteLength).fill(0)
      return array
    })

    const result = await generateTotpEnrollment('alice')

    expect(api.get).toHaveBeenCalledWith('/api/prohibitorum/config', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(decodeURIComponent(new URL(result.otpauthUri).pathname)).toBe('/Example Auth:alice')
  })
})
