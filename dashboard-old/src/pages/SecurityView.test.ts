import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import SecurityView from './SecurityView.vue'
import PasswordTotpCard from './security/PasswordTotpCard.vue'
import RecoveryCodesCard from './security/RecoveryCodesCard.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(async () => null), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => unknown) => fn(), ensureSudo: vi.fn(), sudoState: { value: { open: false, resolve: null } }, _resolveSudo: vi.fn() }))
vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn(async () => 'data:image/png;base64,AAAA') } }))
const get = vi.mocked(api.get)
const post = vi.mocked(api.post)
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

const FACTORS_SET = {
  passwordSet: true,
  totpEnrolled: true,
  recoveryCodesRemaining: 8,
  passkeyCount: 2,
}

const FACTORS_UNSET = {
  passwordSet: false,
  totpEnrolled: false,
  recoveryCodesRemaining: 0,
  passkeyCount: 1,
}

beforeEach(() => {
  post.mockReset()
  get.mockReset()
  Object.assign(navigator, { clipboard: { writeText: vi.fn(async () => {}) } })
})

describe('SecurityView', () => {
  it('renders the factor cards and the revoke action; revoke opens confirm → posts', async () => {
    get.mockResolvedValue(FACTORS_SET)
    const w = mount(SecurityView, { global: { plugins: [i18n()] }, attachTo: document.body })
    await flushPromises()
    expect(w.text()).toContain(en.security.passkeys.title)
    expect(w.text()).toContain(en.security.passwordTotp.title)
    expect(w.text()).toContain(en.security.recovery.title)
    await w.findAll('button').find((b) => b.text() === en.security.revoke.button)!.trigger('click')
    await flushPromises()
    // Two destructive+label buttons exist: page button + dialog confirm. Take the last one (dialog).
    const allDestructive = Array.from(document.body.querySelectorAll('button')).filter((b) => b.getAttribute('data-variant') === 'destructive' && b.textContent?.includes(en.security.revoke.button))
    const confirmBtn = allDestructive[allDestructive.length - 1]!
    confirmBtn.click(); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/auth/revoke-password-totp')
  })

  it('shows "set" badges when all factors are active', async () => {
    get.mockResolvedValue(FACTORS_SET)
    const w = mount(SecurityView, { global: { plugins: [i18n()] }, attachTo: document.body })
    await flushPromises()
    expect(w.text()).toContain(en.security.factors.passwordSet)
    expect(w.text()).toContain(en.security.factors.totpActive)
    // recoveryRemaining uses {n} substitution — check for the number
    expect(w.text()).toContain('8 codes remaining')
  })

  it('shows "unset" badges when no factors are enrolled', async () => {
    get.mockResolvedValue(FACTORS_UNSET)
    const w = mount(SecurityView, { global: { plugins: [i18n()] }, attachTo: document.body })
    await flushPromises()
    expect(w.text()).toContain(en.security.factors.passwordUnset)
    expect(w.text()).toContain(en.security.factors.totpInactive)
    expect(w.text()).toContain('0 codes remaining')
  })

  it('fetches /me/factors on mount', async () => {
    get.mockResolvedValue(FACTORS_SET)
    mount(SecurityView, { global: { plugins: [i18n()] }, attachTo: document.body })
    await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/me/factors', expect.objectContaining({ signal: expect.any(AbortSignal) }))
  })

  it('shows a non-destructive alert when /me/factors fails', async () => {
    // factors GET for /me/factors rejects; credentials GET returns [] for PasskeysCard
    get.mockImplementation(async (url: string) => {
      if (url === '/api/prohibitorum/me/factors') throw new Error('network')
      return []
    })
    const w = mount(SecurityView, { global: { plugins: [i18n()] }, attachTo: document.body })
    await flushPromises()
    expect(w.text()).toContain(en.security.factorsLoadError)
    // Cards should still render
    expect(w.text()).toContain(en.security.passkeys.title)
  })

  it('passes totpEnabled=false to RecoveryCodesCard when TOTP is not enrolled', async () => {
    get.mockResolvedValue(FACTORS_UNSET) // totpEnrolled: false
    const w = mount(SecurityView, { global: { plugins: [i18n()] }, attachTo: document.body })
    await flushPromises()
    const recoveryCard = w.findComponent(RecoveryCodesCard)
    expect(recoveryCard.props('totpEnabled')).toBe(false)
    // The Regenerate button should be absent — guard prevents the dead-end
    expect(recoveryCard.find('button').exists()).toBe(false)
    expect(recoveryCard.find('[data-test="recovery-no-totp-hint"]').exists()).toBe(true)
  })

  it('passes totpEnabled=true to RecoveryCodesCard when TOTP is enrolled', async () => {
    get.mockResolvedValue(FACTORS_SET) // totpEnrolled: true
    const w = mount(SecurityView, { global: { plugins: [i18n()] }, attachTo: document.body })
    await flushPromises()
    const recoveryCard = w.findComponent(RecoveryCodesCard)
    expect(recoveryCard.props('totpEnabled')).toBe(true)
    expect(recoveryCard.find('button').exists()).toBe(true)
  })

  it('setting up password + authenticator invalidates shared factors and updates the badge', async () => {
    // Initial mount: neither factor is set, so the card offers the combined path.
    get.mockResolvedValue(FACTORS_UNSET)
    const w = mount(SecurityView, { global: { plugins: [i18n()] }, attachTo: document.body })
    await flushPromises()
    expect(w.text()).toContain(en.security.factors.passwordUnset)

    // Count calls to /me/factors specifically (PasskeysCard also calls api.get for credentials)
    const factorsCallsBefore = get.mock.calls.filter((args) => args[0] === '/api/prohibitorum/me/factors').length
    expect(factorsCallsBefore).toBe(1)

    post.mockImplementation(async (path: string) =>
      path.endsWith('/password-totp/begin') ? { secret_base32: 'S', otpauth_uri: 'otpauth://totp/x' }
      : path.endsWith('/password-totp/verify') ? { recovery_codes: ['c1'] } : undefined)
    // After the successful setup the refetch reports both factors set.
    get.mockResolvedValue(FACTORS_SET)

    const card = w.findComponent(PasswordTotpCard)
    await card.find('[data-test="setup-both"]').trigger('click')
    const fields = card.findAll('input')
    await fields[0].setValue('a-new-password-123')
    await fields[1].setValue('a-new-password-123')
    await card.find('form').trigger('submit')
    await flushPromises()
    await card.find('input[name=code]').setValue('123456')
    await card.find('form').trigger('submit')
    await flushPromises()

    const factorsCallsAfter = get.mock.calls.filter((args) => args[0] === '/api/prohibitorum/me/factors').length
    expect(factorsCallsAfter).toBeGreaterThan(1)
    expect(w.text()).toContain(en.security.factors.passwordSet)
  })
})
