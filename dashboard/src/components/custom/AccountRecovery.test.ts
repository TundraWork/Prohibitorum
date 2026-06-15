import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn(async () => 'data:image/png;base64,AAAA') } }))
const post = vi.mocked(api.post)
import AccountRecovery from './AccountRecovery.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountC = () => mount(AccountRecovery, { props: { partialToken: 'pt_1' }, global: { plugins: [i18n()] }, attachTo: document.body })
beforeEach(() => { post.mockReset() })

describe('AccountRecovery', () => {
  it('verifies a recovery code, re-enrolls TOTP, shows new codes, emits success', async () => {
    post.mockImplementation(async (p: string) => {
      if (p.endsWith('/recovery-code/verify')) return { recovery_session_token: 'rs_1' }
      if (p.endsWith('/recovery/totp/begin')) return { secret_base32: 'ABCD', otpauth_uri: 'otpauth://x' }
      if (p.endsWith('/recovery/totp/verify')) return { recovery_codes: ['c1', 'c2'] }
      return undefined
    })
    const w = mountC()
    await w.find('input[name="recovery-code"]').setValue('backup-1')
    await w.find('[data-test="verify-code"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/recovery-code/verify', { partial_session_token: 'pt_1', code: 'backup-1' })
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/recovery/totp/begin', { recovery_session_token: 'rs_1' })
    expect(w.text()).toContain('ABCD') // secret shown
    await w.find('input[name="reenroll-code"]').setValue('123456')
    await w.find('[data-test="confirm-reenroll"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/auth/recovery/totp/verify', { recovery_session_token: 'rs_1', code: '123456' })
    expect(w.text()).toContain(en.recoveryCodes.heading) // RecoveryCodesDisplay heading
    await w.find('[data-test="saved"]').trigger('click')
    await w.find('[data-test="done"]').trigger('click'); await flushPromises()
    expect(w.emitted('success')).toBeTruthy()
  })
  it('shows codeWarning and reenrollHeadsUp notes in the code phase', () => {
    const w = mountC()
    expect(w.text()).toContain(en.recovery.codeWarning)
    expect(w.text()).toContain(en.recovery.reenrollHeadsUp)
  })
  it('emits restart when the recovery code is rejected', async () => {
    post.mockRejectedValue({ code: 'bad_credentials', message: 'zh' })
    const w = mountC()
    await w.find('input[name="recovery-code"]').setValue('wrong')
    await w.find('[data-test="verify-code"]').trigger('click'); await flushPromises()
    expect(w.emitted('restart')).toBeTruthy()
  })
  it('shows a retry when totp/begin fails, then recovers on retry', async () => {
    let beginCalls = 0
    post.mockImplementation(async (p: string) => {
      if (p.endsWith('/recovery-code/verify')) return { recovery_session_token: 'rs_1' }
      if (p.endsWith('/recovery/totp/begin')) { beginCalls++; if (beginCalls === 1) throw { code: 'server_error', message: 'boom' }; return { secret_base32: 'ABCD', otpauth_uri: 'otpauth://x' } }
      return undefined
    })
    const w = mountC()
    await w.find('input[name="recovery-code"]').setValue('backup-1')
    await w.find('[data-test="verify-code"]').trigger('click'); await flushPromises()
    expect(w.find('[data-test="reenroll-retry"]').exists()).toBe(true) // stranded → retry offered
    await w.find('[data-test="reenroll-retry"]').trigger('click'); await flushPromises()
    expect(w.text()).toContain('ABCD') // QR/secret now loaded
  })
  it('stays in reenroll when the TOTP code is wrong', async () => {
    post.mockImplementation(async (p: string) => {
      if (p.endsWith('/recovery-code/verify')) return { recovery_session_token: 'rs_1' }
      if (p.endsWith('/recovery/totp/begin')) return { secret_base32: 'ABCD', otpauth_uri: 'otpauth://x' }
      if (p.endsWith('/recovery/totp/verify')) throw { code: 'bad_credentials', message: 'zh' }
      return undefined
    })
    const w = mountC()
    await w.find('input[name="recovery-code"]').setValue('backup-1')
    await w.find('[data-test="verify-code"]').trigger('click'); await flushPromises()
    await w.find('input[name="reenroll-code"]').setValue('000000')
    await w.find('[data-test="confirm-reenroll"]').trigger('click'); await flushPromises()
    expect(w.find('input[name="reenroll-code"]').exists()).toBe(true) // still in reenroll
    expect(w.text()).toContain(en.errors.bad_credentials)
  })
})
