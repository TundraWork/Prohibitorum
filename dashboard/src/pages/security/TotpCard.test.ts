import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import TotpCard from './TotpCard.vue'

const post = vi.fn()
vi.mock('../../lib/api', () => ({ api: { post: (...a: unknown[]) => post(...a) } }))
vi.mock('../../lib/sudo', () => ({ withSudo: (fn: any) => fn() }))
beforeEach(() => post.mockReset())

describe('TotpCard', () => {
  it('begins enrollment and shows the secret', async () => {
    post.mockResolvedValueOnce({ secret_base32: 'JBSWY3DP', otpauth_uri: 'otpauth://totp/x' })
    const w = mount(TotpCard)
    await w.find('[data-test="totp-begin"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/totp/begin')
    expect(w.text()).toContain('JBSWY3DP')
  })
  it('verifies and shows recovery codes when returned', async () => {
    post.mockResolvedValueOnce({ secret_base32: 'JBSWY3DP', otpauth_uri: 'otpauth://totp/x' }) // begin
    post.mockResolvedValueOnce({ recovery_codes: ['aaaa-bbbb', 'cccc-dddd'] }) // verify
    const w = mount(TotpCard)
    await w.find('[data-test="totp-begin"]').trigger('click'); await flushPromises()
    await w.find('[data-test="totp-code"]').setValue('123456')
    await w.find('[data-test="totp-verify"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenLastCalledWith('/api/prohibitorum/me/totp/verify', { code: '123456' })
    expect(w.text()).toContain('aaaa-bbbb')
  })
})
