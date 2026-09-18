import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import AccountRecovery from './AccountRecovery.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'

const { generateTotpEnrollment } = vi.hoisted(() => ({
  generateTotpEnrollment: vi.fn(async () => ({ secretBase32: 'ABCD', otpauthUri: 'otpauth://totp/x' })),
}))
vi.mock('@/lib/totpEnrollment', () => ({ generateTotpEnrollment }))
vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn(async () => 'data:image/png;base64,AAAA') } }))

const post = vi.mocked(api.post)
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountC = () => mount(AccountRecovery, {
  props: { partialToken: 'pt_1', username: 'alex', returnTo: '/me' },
  global: { plugins: [i18n()] },
  attachTo: document.body,
})

beforeEach(() => {
  post.mockReset()
  generateTotpEnrollment.mockClear()
  Object.assign(navigator, { clipboard: { writeText: vi.fn(async () => {}) } })
})

describe('AccountRecovery', () => {
  it('uses a recovery code without replacing the authenticator', async () => {
    post.mockResolvedValue({ redirect: '/me' })
    const w = mountC()
    await w.find('input[name="recovery-code"]').setValue('backup-1')
    await w.find('[data-test="verify-code"]').trigger('click')
    await flushPromises()

    expect(generateTotpEnrollment).not.toHaveBeenCalled()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/recovery-code/verify?return_to=%2Fme', {
      partial_session_token: 'pt_1', code: 'backup-1', reset_authenticator: false,
    })
    expect(w.emitted('success')).toEqual([['/me']])
  })

  it('optionally submits the recovery code and replacement authenticator together', async () => {
    post.mockResolvedValue({ redirect: '/me', recovery_codes: ['c1', 'c2'] })
    const w = mountC()
    await w.find('input[name="recovery-code"]').setValue('backup-1')
    await w.find('[data-test="reset-authenticator"]').trigger('click')
    await flushPromises()

    expect(generateTotpEnrollment).toHaveBeenCalledWith('alex')
    expect(w.text()).toContain('ABCD')
    expect(post).not.toHaveBeenCalled()

    await w.find('input[name="reenroll-code"]').setValue('123456')
    await w.find('[data-test="verify-code"]').trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/recovery-code/verify?return_to=%2Fme', {
      partial_session_token: 'pt_1', code: 'backup-1', reset_authenticator: true,
      totp_secret_base32: 'ABCD', totp_code: '123456',
    })
    expect(w.text()).toContain(en.recoveryCodes.heading)
    await w.find('[data-test="saved"]').trigger('click')
    await w.find('[data-test="done"]').trigger('click')
    expect(w.emitted('success')).toEqual([['/me']])
  })

  it('explains that the recovery code is single-use and offers the reset choice', () => {
    const w = mountC()
    expect(w.text()).toContain(en.recovery.codeWarning)
    expect(w.text()).toContain(en.recovery.resetAuthenticator)
  })

  it('emits restart when the single recovery submission fails', async () => {
    post.mockRejectedValue({ code: 'bad_credentials' })
    const w = mountC()
    await w.find('input[name="recovery-code"]').setValue('wrong')
    await w.find('[data-test="verify-code"]').trigger('click')
    await flushPromises()
    expect(w.emitted('restart')).toBeTruthy()
  })

  it('does not consume the recovery token when local candidate generation fails', async () => {
    generateTotpEnrollment.mockRejectedValueOnce({ code: 'network_error' })
    const w = mountC()
    await w.find('[data-test="reset-authenticator"]').trigger('click')
    await flushPromises()
    expect(post).not.toHaveBeenCalled()
    expect(w.find('[data-test="replacement-authenticator"]').exists()).toBe(false)
    expect(w.get('[data-test="error-summary"]').text()).toBe(en.errors.unknown)
  })
})
