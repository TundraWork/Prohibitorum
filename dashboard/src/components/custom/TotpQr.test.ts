import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

const toDataURL = vi.fn(async () => 'data:image/png;base64,AAAA')
vi.mock('qrcode', () => ({ default: { toDataURL: (...a: unknown[]) => toDataURL(...a) } }))
import TotpQr from './TotpQr.vue'

describe('TotpQr', () => {
  it('renders an img with a data: src and the provided alt', async () => {
    toDataURL.mockResolvedValueOnce('data:image/png;base64,AAAA')
    const w = mount(TotpQr, { props: { uri: 'otpauth://totp/x', alt: 'Scan to add' } })
    await flushPromises()
    const img = w.find('img')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toContain('data:image/png')
    expect(img.attributes('alt')).toBe('Scan to add')
  })

  it('renders nothing when QR generation fails', async () => {
    toDataURL.mockRejectedValueOnce(new Error('boom'))
    const w = mount(TotpQr, { props: { uri: 'otpauth://totp/x', alt: 'Scan to add' } })
    await flushPromises()
    expect(w.find('img').exists()).toBe(false)
  })
})
