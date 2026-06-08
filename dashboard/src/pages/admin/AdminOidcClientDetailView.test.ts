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
