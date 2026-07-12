import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const get = vi.mocked(api.get); const post = vi.mocked(api.post)
const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
import AdminOidcClientsView from './AdminOidcClientsView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminOidcClientsView, { global: { plugins: [i18n()] }, attachTo: document.body })
const CLIENTS = [
  { clientId: 'web', displayName: 'Web App', redirectUris: ['https://w/cb'], postLogoutRedirectUris: [], allowedScopes: ['openid'], tokenEndpointAuthMethod: 'client_secret_basic', requireConsent: true, disabled: false, createdAt: '2026-01-01T00:00:00Z' },
  { clientId: 'spa', displayName: 'SPA', redirectUris: ['https://s/cb'], postLogoutRedirectUris: [], allowedScopes: ['openid'], tokenEndpointAuthMethod: 'none', requireConsent: false, disabled: false, createdAt: '2026-01-02T00:00:00Z' },
]
beforeEach(() => { get.mockReset(); post.mockReset(); push.mockReset() })

describe('AdminOidcClientsView', () => {
  it('lists clients with type badges', async () => {
    get.mockResolvedValue(CLIENTS)
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/oidc-applications')
    expect(w.text()).toContain('Web App'); expect(w.text()).toContain(en.admin.oidc.confidential); expect(w.text()).toContain(en.admin.oidc.public)
  })
  it('row click navigates to detail', async () => {
    get.mockResolvedValue(CLIENTS)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="client-row-spa"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/admin/oidc-applications/spa')
  })
  it('creates a confidential client and reveals the secret', async () => {
    get.mockResolvedValue([])
    post.mockResolvedValue({ clientId: 'new', secret: 's3cr3t', tokenEndpointAuthMethod: 'client_secret_basic' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('input[name="clientId"]').setValue('new')
    await w.find('input[name="displayName"]').setValue('New')
    await w.find('[data-test="redirectUris-add"]').trigger('click')
    await w.find('[data-test="redirectUris-input-0"]').setValue('https://n/cb')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/oidc-applications', expect.objectContaining({
      clientId: 'new', displayName: 'New', redirectUris: ['https://n/cb'],
    }))
    expect(w.text()).toContain('s3cr3t')
  })
  it('creates a public client (no secret) and shows the created note', async () => {
    get.mockResolvedValue([])
    post.mockResolvedValue({ clientId: 'spa', tokenEndpointAuthMethod: 'none' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('input[name="clientId"]').setValue('spa')
    await w.find('input[name="displayName"]').setValue('SPA')
    await w.find('[data-test="redirectUris-add"]').trigger('click')
    await w.find('[data-test="redirectUris-input-0"]').setValue('https://s/cb')
    await w.find('[data-test="public"]').trigger('click')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(w.text()).toContain(en.admin.oidc.created)
  })
  it('surfaces oidc_client_already_exists', async () => {
    get.mockResolvedValue([])
    post.mockRejectedValue({ code: 'oidc_client_already_exists', message: 'zh' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('input[name="clientId"]').setValue('web')
    await w.find('input[name="displayName"]').setValue('Dup')
    await w.find('[data-test="redirectUris-add"]').trigger('click')
    await w.find('[data-test="redirectUris-input-0"]').setValue('https://w/cb')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(w.text()).toContain(en.errors.codes.oidc_client_already_exists)
  })
})
