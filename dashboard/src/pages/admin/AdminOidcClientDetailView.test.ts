import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const get = vi.mocked(api.get); const post = vi.mocked(api.post); const put = vi.mocked(api.put)
const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }), useRoute: () => ({ params: { clientId: 'web' } }) }))
import AdminOidcClientDetailView from './AdminOidcClientDetailView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminOidcClientDetailView, { global: { plugins: [i18n()], stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } } }, attachTo: document.body })
const CLIENT = { clientId: 'web', displayName: 'Web App', redirectUris: ['https://w/cb'], postLogoutRedirectUris: [], allowedScopes: ['openid', 'profile'], tokenEndpointAuthMethod: 'client_secret_basic', requireConsent: true, disabled: false, createdAt: '2026-01-01T00:00:00Z' }
function clickConfirm(label: string) {
  const b = Array.from(document.body.querySelectorAll('button')).filter((x) => x.getAttribute('data-variant') === 'destructive' && x.textContent?.includes(label))
  b[b.length - 1]!.click()
}
beforeEach(() => { get.mockReset(); post.mockReset(); put.mockReset(); push.mockReset() })

describe('AdminOidcClientDetailView', () => {
  it('loads the client and saves config via PUT (allowedScopes)', async () => {
    get.mockResolvedValue(CLIENT); put.mockResolvedValue({ ...CLIENT, displayName: 'Renamed' })
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/oidc-applications/web')
    await w.find('input[name="displayName"]').setValue('Renamed')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/oidc-applications/web', expect.objectContaining({
      displayName: 'Renamed', allowedScopes: ['openid', 'profile'], requireConsent: true, disabled: false,
    }))
    expect(w.text()).toContain(en.admin.oidc.saved)
  })
  it('shows the client_id as a read-only field (immutable identifier, not editable)', async () => {
    get.mockResolvedValue(CLIENT)
    const w = mountView(); await flushPromises()
    const idEl = w.find('[data-test="oidc-client-id"]')
    expect(idEl.exists()).toBe(true)
    expect(idEl.text()).toBe('web')
    // No input has name="clientId" (client_id cannot be changed)
    expect(w.find('input[name="clientId"]').exists()).toBe(false)
  })
  it('groups the disable button, rotate-secret and delete in the Danger zone card', async () => {
    get.mockResolvedValue(CLIENT)
    const w = mountView(); await flushPromises()
    const cards = w.findAll('[data-slot="card"]')
    // The Danger zone is the card holding Delete; it now also holds disable + rotate.
    const dangerCard = cards.find((c) => c.find('[data-test="delete"]').exists())
    expect(dangerCard).toBeTruthy()
    expect(dangerCard!.find('[data-test="disable-toggle"]').exists()).toBe(true)
    expect(dangerCard!.find('[data-test="rotate"]').exists()).toBe(true)
    // The disable control must NOT be in the Config card (the one holding Save).
    const configCard = cards.find((c) => c.find('[data-test="save"]').exists())
    expect(configCard!.find('[data-test="disable-toggle"]').exists()).toBe(false)
  })
  it('disables independently via the dedicated set-disabled endpoint (no config PUT)', async () => {
    get.mockResolvedValue(CLIENT) // disabled: false → button reads "Disable"
    post.mockResolvedValue({ ...CLIENT, disabled: true })
    const w = mountView(); await flushPromises()
    const btn = w.find('[data-test="disable-toggle"]')
    expect(btn.text()).toBe(en.admin.oidc.disable)
    expect(w.find('[data-test="status-badge"]').text()).toBe(en.admin.oidc.active) // green "Active"
    await btn.trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/oidc-applications/set-disabled', { clientId: 'web', disabled: true })
    expect(put).not.toHaveBeenCalled() // independent of the config Save
    expect(w.find('[data-test="disable-toggle"]').text()).toBe(en.admin.oidc.enable) // flipped to "Enable"
    expect(w.find('[data-test="status-badge"]').text()).toBe(en.admin.oidc.disabled) // now "Disabled"
  })
  it('not found', async () => {
    get.mockRejectedValue({ code: 'client_not_found', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.oidc.notFound)
  })
  it('rotates the secret and reveals it', async () => {
    get.mockResolvedValue(CLIENT); post.mockResolvedValue({ clientId: 'web', secret: 'newsecret' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="rotate"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.oidc.rotate); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/oidc-applications/rotate-secret', { clientId: 'web' })
    expect(w.text()).toContain('newsecret')
  })
  it('deletes and navigates to the list', async () => {
    get.mockResolvedValue(CLIENT); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="delete"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.oidc.delete); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/oidc-applications/delete', { clientId: 'web' })
    expect(push).toHaveBeenCalledWith('/admin/oidc-applications')
  })
  it('clears the revealed secret after a subsequent save', async () => {
    get.mockResolvedValue(CLIENT)
    post.mockResolvedValue({ clientId: 'web', secret: 'rotated123' })
    put.mockResolvedValue({ ...CLIENT })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="rotate"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.oidc.rotate); await flushPromises()
    expect(w.text()).toContain('rotated123')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(w.text()).not.toContain('rotated123')
  })
  it('does not navigate when delete fails', async () => {
    get.mockResolvedValue(CLIENT)
    post.mockRejectedValue({ code: 'client_not_found', message: 'zh' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="delete"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.oidc.delete); await flushPromises()
    expect(push).not.toHaveBeenCalled()
    expect(w.text()).toContain(en.errors.client_not_found)
  })
})
