import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import PasswordTotpCard from './PasswordTotpCard.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => unknown) => fn(), ensureSudo: vi.fn(), sudoState: { value: { open: false, resolve: null } }, _resolveSudo: vi.fn() }))
vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn(async () => 'data:image/png;base64,AAAA') } }))

const post = vi.mocked(api.post)
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
beforeEach(() => { post.mockReset(); Object.assign(navigator, { clipboard: { writeText: vi.fn(async () => {}) } }) })

function mountCard(props: { passwordSet?: boolean; totpEnrolled?: boolean }) {
  return mount(PasswordTotpCard, { global: { plugins: [i18n()] }, props, attachTo: document.body })
}

describe('PasswordTotpCard', () => {
  it('neither factor set: one combined setup entry, no change/reset', () => {
    const w = mountCard({ passwordSet: false, totpEnrolled: false })
    expect(w.text()).toContain(en.security.passwordTotp.noneDesc)
    expect(w.find('[data-test="setup-both"]').exists()).toBe(true)
    expect(w.find('[data-test="change-password"]').exists()).toBe(false)
    expect(w.find('[data-test="reset-totp"]').exists()).toBe(false)
  })

  it('both factors set: change password and reset authenticator entries', () => {
    const w = mountCard({ passwordSet: true, totpEnrolled: true })
    expect(w.text()).toContain(en.security.passwordTotp.bothDesc)
    expect(w.find('[data-test="change-password"]').exists()).toBe(true)
    expect(w.find('[data-test="reset-totp"]').exists()).toBe(true)
    expect(w.find('[data-test="setup-both"]').exists()).toBe(false)
  })

  it('half set: says the combined path rewrites everything', () => {
    const w = mountCard({ passwordSet: true, totpEnrolled: false })
    expect(w.text()).toContain(en.security.passwordTotp.halfDesc)
    expect(w.find('[data-test="half-note"]').text()).toBe(en.security.passwordTotp.halfReplaces)
    expect(w.find('[data-test="setup-both"]').exists()).toBe(true)
    expect(w.find('[data-test="change-password"]').exists()).toBe(false)
  })

  it('combined setup uses the new endpoints and ends on recovery codes', async () => {
    post.mockImplementation(async (path: string) => {
      if (path.endsWith('/password-totp/begin')) return { secret_base32: 'SECRET', otpauth_uri: 'otpauth://totp/x' }
      if (path.endsWith('/password-totp/verify')) return { recovery_codes: ['c1', 'c2'] }
      throw new Error(`unexpected POST ${path}`)
    })
    const w = mountCard({ passwordSet: false, totpEnrolled: false })
    await w.find('[data-test="setup-both"]').trigger('click')
    await w.find('input[name=new_password]').setValue('longenough1')
    await w.find('input[name=confirm_password]').setValue('longenough1')
    await w.find('form').trigger('submit'); await flushPromises()

    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/password-totp/begin', { password: 'longenough1' })
    expect(w.find('img').exists()).toBe(true)

    await w.find('input[name=code]').setValue('123456')
    await w.find('form').trigger('submit'); await flushPromises()

    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/password-totp/verify', { code: '123456' })
    expect(w.text()).toContain(en.recoveryCodes.heading)
    expect(w.text()).toContain('c1')
    expect(w.emitted('changed')).toBeTruthy()
  })

  it('both set: changing the password posts only /me/password/set', async () => {
    post.mockResolvedValue({})
    const w = mountCard({ passwordSet: true, totpEnrolled: true })
    await w.find('[data-test="change-password"]').trigger('click')
    await w.find('input[name=new_password]').setValue('longenough1')
    await w.find('input[name=confirm_password]').setValue('longenough1')
    await w.find('form').trigger('submit'); await flushPromises()

    expect(post).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/password/set', { password: 'longenough1' })
    expect(w.text()).toContain(en.security.password.saved)
    expect(w.emitted('changed')).toBeTruthy()
  })

  it('both set: resetting the authenticator uses /me/totp/begin + verify', async () => {
    post.mockImplementation(async (path: string) =>
      path.endsWith('/totp/begin') ? { secret_base32: 'SECRET', otpauth_uri: 'otpauth://totp/x' }
      : path.endsWith('/totp/verify') ? { recovery_codes: ['c1'] } : undefined)
    const w = mountCard({ passwordSet: true, totpEnrolled: true })
    await w.find('[data-test="reset-totp"]').trigger('click'); await flushPromises()

    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/totp/begin')

    await w.find('input[name=code]').setValue('123456')
    await w.find('form').trigger('submit'); await flushPromises()

    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/totp/verify', { code: '123456' })
    expect(w.text()).toContain(en.recoveryCodes.heading)
  })
})