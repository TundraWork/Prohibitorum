import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import TotpCard from './TotpCard.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => unknown) => fn(), ensureSudo: vi.fn(), sudoState: { value: { open: false, resolve: null } }, _resolveSudo: vi.fn() }))
vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn(async () => 'data:image/png;base64,AAAA') } }))
const post = vi.mocked(api.post)
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
beforeEach(() => { post.mockReset(); Object.assign(navigator, { clipboard: { writeText: vi.fn(async () => {}) } }) })
const mountCard = () => mount(TotpCard, { global: { plugins: [i18n()] }, attachTo: document.body })

describe('TotpCard', () => {
  it('enrolls: setup shows QR+secret, verify shows recovery codes', async () => {
    post.mockImplementation(async (p: string) =>
      p.endsWith('/totp/begin') ? { secret_base32: 'SECRET', otpauth_uri: 'otpauth://totp/x' }
      : p.endsWith('/totp/verify') ? { recovery_codes: ['c1', 'c2'] } : undefined)
    const w = mountCard()
    await w.findAll('button').find((b) => b.text().includes(en.security.totp.setup))!.trigger('click')
    await flushPromises()
    expect(w.find('img').exists()).toBe(true)
    expect(w.text()).toContain('SECRET')
    await w.find('input[name=code]').setValue('123456')
    await w.find('form').trigger('submit'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/totp/verify', { code: '123456' })
    expect(w.text()).toContain(en.recoveryCodes.heading)
  })

  it('cancel during setup returns to idle state', async () => {
    post.mockResolvedValue({ secret_base32: 'SECRET', otpauth_uri: 'otpauth://totp/x' })
    const w = mountCard()
    // Start setup
    await w.findAll('button').find((b) => b.text().includes(en.security.totp.setup))!.trigger('click')
    await flushPromises()
    // QR/secret should be shown
    expect(w.find('img').exists()).toBe(true)
    // Click cancel
    const cancelBtn = w.findAll('button').find((b) => b.text() === en.security.totp.cancelSetup)!
    await cancelBtn.trigger('click')
    await flushPromises()
    // Should be back to idle: setup button visible, no QR
    expect(w.find('img').exists()).toBe(false)
    expect(w.findAll('button').some((b) => b.text().includes(en.security.totp.setup))).toBe(true)
  })

  it('verify does NOT call withSudo — sudo is hoisted to begin', async () => {
    // withSudo mock calls fn() directly. We track that begin called withSudo but verify does not.
    const withSudoMod = await import('@/lib/sudo')
    const withSudoSpy = vi.spyOn(withSudoMod, 'withSudo').mockImplementation((fn) => fn() as ReturnType<typeof fn>)

    post.mockImplementation(async (p: string) =>
      p.endsWith('/totp/begin') ? { secret_base32: 'S', otpauth_uri: 'otpauth://totp/x' }
      : p.endsWith('/totp/verify') ? { recovery_codes: [] } : undefined)

    const w = mountCard()
    // Trigger setup (begin)
    await w.findAll('button').find((b) => b.text().includes(en.security.totp.setup))!.trigger('click')
    await flushPromises()

    const callsAfterBegin = withSudoSpy.mock.calls.length
    expect(callsAfterBegin).toBe(1) // withSudo called once for begin

    // Trigger verify
    await w.find('input[name=code]').setValue('000000')
    await w.find('form').trigger('submit'); await flushPromises()

    // withSudo should NOT have been called again for verify
    expect(withSudoSpy.mock.calls.length).toBe(1)

    withSudoSpy.mockRestore()
  })
})
