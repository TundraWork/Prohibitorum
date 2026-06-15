import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)
const post = vi.mocked(api.post)

const { ensureSudo, hardRedirect } = vi.hoisted(() => ({
  ensureSudo: vi.fn(async () => true),
  hardRedirect: vi.fn(),
}))
vi.mock('@/lib/sudo', () => ({
  ensureSudo,
  withSudo: (fn: () => Promise<unknown>) => fn(),
}))
vi.mock('@/lib/navigate', () => ({ hardRedirect }))

import ConnectedAccountsView from './ConnectedAccountsView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(ConnectedAccountsView, { global: { plugins: [i18n()] }, attachTo: document.body })

const IDENTITIES = [
  { id: 1, idpSlug: 'okta', idpDisplayName: 'Okta', upstreamEmail: 'a@example.com', linkedAt: '2026-01-01T00:00:00Z' },
  { id: 2, idpSlug: 'ad', idpDisplayName: 'Azure AD', upstreamEmail: null, linkedAt: '2026-02-01T00:00:00Z' },
]
const PROVIDERS = [
  { slug: 'okta', displayName: 'Okta' },
  { slug: 'google', displayName: 'Google' },
]

function mockGets(identities = IDENTITIES, providers = PROVIDERS) {
  get.mockImplementation(async (p: string) =>
    p === '/api/prohibitorum/me/identities' ? identities : providers)
}

beforeEach(() => {
  get.mockReset(); post.mockReset(); ensureSudo.mockReset(); hardRedirect.mockReset()
  ensureSudo.mockResolvedValue(true)
})

// ConfirmDialog (reka-ui) teleports to document.body and its confirm button has
// no data-test hook — it's the destructive-variant button carrying the unlink
// label. The row's own unlink button is variant="outline", so filtering by
// data-variant="destructive" uniquely finds the dialog confirm.
function clickConfirm() {
  const confirmBtns = Array.from(document.body.querySelectorAll('button'))
    .filter((b) => b.getAttribute('data-variant') === 'destructive'
      && b.textContent?.includes(en.connected.unlink))
  confirmBtns[confirmBtns.length - 1]!.click()
}

describe('ConnectedAccountsView', () => {
  it('lists linked identities with provider name and upstream email', async () => {
    mockGets()
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain('Okta')
    expect(w.text()).toContain('a@example.com')
    expect(w.text()).toContain('Azure AD')
  })

  it('shows Connected badge (not Linked) and redirect note', async () => {
    mockGets()
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.connected.linked)
    expect(w.text()).toContain(en.connected.linkRedirectNote)
  })

  it('shows empty state when no identities are linked', async () => {
    mockGets([], PROVIDERS)
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.connected.empty)
  })

  it('unlinks an identity (confirm → post → refresh)', async () => {
    mockGets()
    post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="unlink-1"]').trigger('click'); await flushPromises()
    clickConfirm(); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/me/identities/1/unlink')
    expect(get.mock.calls.filter((c) => String(c[0]).includes('/me/identities'))).toHaveLength(2)
  })

  it('surfaces last_sign_in_method on unlink failure', async () => {
    mockGets()
    post.mockRejectedValue({ code: 'last_sign_in_method', message: 'zh' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="unlink-1"]').trigger('click'); await flushPromises()
    clickConfirm(); await flushPromises()
    expect(w.text()).toContain(en.errors.last_sign_in_method)
  })

  it('surfaces credential_not_found on unlink failure', async () => {
    mockGets()
    post.mockRejectedValue({ code: 'credential_not_found', message: 'zh' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="unlink-1"]').trigger('click'); await flushPromises()
    clickConfirm(); await flushPromises()
    expect(w.text()).toContain(en.errors.credential_not_found)
  })

  it('disables already-linked providers in the link picker', async () => {
    mockGets()
    const w = mountView(); await flushPromises()
    expect(w.find('[data-test="link-okta"]').attributes('disabled')).toBeDefined()
    expect(w.find('[data-test="link-google"]').attributes('disabled')).toBeUndefined()
  })

  it('link → ensureSudo then hardRedirect to begin', async () => {
    mockGets()
    const w = mountView(); await flushPromises()
    await w.find('[data-test="link-google"]').trigger('click'); await flushPromises()
    expect(ensureSudo).toHaveBeenCalledOnce()
    expect(hardRedirect).toHaveBeenCalledWith(
      '/api/prohibitorum/me/identities/link/google/begin?return_to=%2Fconnected')
  })

  it('does not redirect when sudo is cancelled', async () => {
    mockGets()
    ensureSudo.mockResolvedValue(false)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="link-google"]').trigger('click'); await flushPromises()
    expect(hardRedirect).not.toHaveBeenCalled()
  })
})
