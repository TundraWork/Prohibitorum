import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import SecurityView from './SecurityView.vue'
import PasswordCard from './security/PasswordCard.vue'

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
  it('renders the four cards and the revoke action; revoke opens confirm → posts', async () => {
    get.mockResolvedValue(FACTORS_SET)
    const w = mount(SecurityView, { global: { plugins: [i18n()] }, attachTo: document.body })
    await flushPromises()
    expect(w.text()).toContain(en.security.passkeys.title)
    expect(w.text()).toContain(en.security.password.title)
    expect(w.text()).toContain(en.security.totp.title)
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
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/me/factors')
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

  it('@changed from PasswordCard triggers re-fetch and updates badge', async () => {
    // Initial mount: password is not set
    get.mockResolvedValue(FACTORS_UNSET)
    const w = mount(SecurityView, { global: { plugins: [i18n()] }, attachTo: document.body })
    await flushPromises()
    expect(w.text()).toContain(en.security.factors.passwordUnset)

    // Count calls to /me/factors specifically (PasskeysCard also calls api.get for credentials)
    const factorsCallsBefore = get.mock.calls.filter((args) => args[0] === '/api/prohibitorum/me/factors').length
    expect(factorsCallsBefore).toBe(1)

    // After the card emits "changed", the GET will return passwordSet: true
    get.mockResolvedValue({ ...FACTORS_UNSET, passwordSet: true })
    await w.findComponent(PasswordCard).vm.$emit('changed')
    await flushPromises()

    const factorsCallsAfter = get.mock.calls.filter((args) => args[0] === '/api/prohibitorum/me/factors').length
    expect(factorsCallsAfter).toBe(2)
    expect(w.text()).toContain(en.security.factors.passwordSet)
  })
})
