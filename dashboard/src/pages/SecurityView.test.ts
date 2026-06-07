import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import SecurityView from './SecurityView.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(async () => []), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => unknown) => fn(), ensureSudo: vi.fn(), sudoState: { value: { open: false, resolve: null } }, _resolveSudo: vi.fn() }))
vi.mock('qrcode', () => ({ default: { toDataURL: vi.fn(async () => 'data:image/png;base64,AAAA') } }))
const post = vi.mocked(api.post)
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
beforeEach(() => { post.mockReset(); Object.assign(navigator, { clipboard: { writeText: vi.fn(async () => {}) } }) })

describe('SecurityView', () => {
  it('renders the four cards and the revoke action; revoke opens confirm → posts', async () => {
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
})
