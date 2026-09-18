import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
const { session } = vi.hoisted(() => ({ session: { isAdmin: true } }))
vi.mock('@/composables/useSession', () => ({ useSession: () => session }))
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const get = vi.mocked(api.get); const post = vi.mocked(api.post)
const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
import AdminSamlProvidersView from './AdminSamlProvidersView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = (isAdmin = true) => { session.isAdmin = isAdmin; return mount(AdminSamlProvidersView, { global: { plugins: [i18n()] }, attachTo: document.body }) }
const SPS = [{ id: 1, entityId: 'https://sp/meta', displayName: 'GHES', nameIdFormat: 'persistent', requireSignedAuthnRequest: false, allowIdpInitiated: true, disabled: false, acs: [], keys: [], createdAt: '2026-01-01T00:00:00Z' }]
beforeEach(() => { session.isAdmin = true; get.mockReset(); post.mockReset(); push.mockReset() })

describe('AdminSamlProvidersView', () => {
  it('hides creation from non-admin users while keeping the assigned list available', async () => {
    get.mockResolvedValue({ items: [], nextCursor: '' })
    const w = mountView(false); await flushPromises()
    expect(w.find('[data-test="create"]').exists()).toBe(false)
    expect(w.text()).toContain(en.admin.saml.empty)
  })

  it('submits restricted access and resets it when reopening creation', async () => {
    vi.mocked(api.get).mockResolvedValue({ items: [], nextCursor: '' })
    vi.mocked(api.post).mockResolvedValue({ clientId: 'new', id: 2 })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    expect(w.find('[data-test="access-restricted"]').attributes('aria-checked')).toBe('false')
    await w.find('[data-test="access-restricted"]').trigger('click')
    expect(w.text()).toContain(en.admin.access.createRestrictedHint)
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(api.post).toHaveBeenLastCalledWith('/api/prohibitorum/saml-applications', expect.objectContaining({ accessRestricted: true }))
    await w.find('[data-test="create"]').trigger('click')
    expect(w.find('[data-test="access-restricted"]').attributes('aria-checked')).toBe('false')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(api.post).toHaveBeenLastCalledWith('/api/prohibitorum/saml-applications', expect.objectContaining({ accessRestricted: false }))
    w.unmount()
  })

  it('lists providers', async () => {
    get.mockResolvedValue({ items: SPS, nextCursor: '' })
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/saml-applications', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(w.text()).toContain('GHES'); expect(w.text()).toContain('https://sp/meta')
  })
  it('shows the active status badge for an enabled provider', async () => {
    get.mockResolvedValue({ items: SPS, nextCursor: '' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.saml.active)
  })
  it('shows the disabled status badge for a disabled provider', async () => {
    get.mockResolvedValue({ items: [{ ...SPS[0], disabled: true }], nextCursor: '' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.saml.disabled)
  })
  it('row click navigates to detail', async () => {
    get.mockResolvedValue({ items: SPS, nextCursor: '' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="sp-row-1"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/admin/saml-applications/1')
  })
  it('creates via metadata paste', async () => {
    get.mockResolvedValue({ items: [], nextCursor: '' }); post.mockResolvedValue({ id: 2 })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    // metadata mode is default
    await w.find('textarea[name="metadataXml"]').setValue('<xml/>')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/saml-applications', expect.objectContaining({ metadataXml: '<xml/>' }))
  })
  it('creates via manual ACS', async () => {
    get.mockResolvedValue({ items: [], nextCursor: '' }); post.mockResolvedValue({ id: 3 })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    // reka Tabs activates on mousedown (not click).
    await w.find('[data-test="mode-manual"]').trigger('mousedown')
    await flushPromises()
    await w.find('[data-test="access-restricted"]').trigger('click')
    await w.find('input[name="entityId"]').setValue('https://manual/sp')
    await w.find('input[name="displayName"]').setValue('Manual SP')
    await w.find('[data-test="acs-add"]').trigger('click')
    await w.find('input[name="acs-location-0"]').setValue('https://manual/acs')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/saml-applications', expect.objectContaining({
      entityId: 'https://manual/sp', displayName: 'Manual SP', accessRestricted: true,
      acs: [expect.objectContaining({ binding: 'urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST', location: 'https://manual/acs', isDefault: true })],
    }))
  })
  it('surfaces saml_application_already_exists', async () => {
    get.mockResolvedValue({ items: [], nextCursor: '' }); post.mockRejectedValue({ code: 'saml_application_already_exists', message: 'zh' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('textarea[name="metadataXml"]').setValue('<xml/>')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(w.text()).toContain(en.errors.codes.saml_application_already_exists)
  })
})
