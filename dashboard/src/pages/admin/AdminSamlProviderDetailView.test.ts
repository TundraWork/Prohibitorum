import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const get = vi.mocked(api.get); const post = vi.mocked(api.post); const put = vi.mocked(api.put)
const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }), useRoute: () => ({ params: { id: '5' } }) }))
import AdminSamlProviderDetailView from './AdminSamlProviderDetailView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminSamlProviderDetailView, { global: { plugins: [i18n()], stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } } }, attachTo: document.body })
const SP = { id: 5, entityId: 'https://sp/meta', displayName: 'GHES', nameIdFormat: 'persistent', attributeMap: [{ name: 'USERNAME', name_format: 'urn:oasis:names:tc:SAML:2.0:attrname-format:basic', source: 'username', multi: false }], requireSignedAuthnRequest: false, allowIdpInitiated: true, disabled: false, sessionLifetimeSecs: 3600, acs: [{ binding: 'urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST', location: 'https://sp/acs', index: 0, isDefault: true }], keys: [{ use: 'signing', notAfter: '2027-01-01T00:00:00Z' }], createdAt: '2026-01-01T00:00:00Z' }
function clickConfirm(label: string) { const b = Array.from(document.body.querySelectorAll('button')).filter((x) => x.getAttribute('data-variant') === 'destructive' && x.textContent?.includes(label)); b[b.length - 1]!.click() }
beforeEach(() => { get.mockReset(); post.mockReset(); put.mockReset(); push.mockReset() })

describe('AdminSamlProviderDetailView', () => {
  it('loads the SP, shows ACS, saves flags via PUT', async () => {
    get.mockResolvedValue(SP); put.mockResolvedValue({ ...SP, displayName: 'GHES 2' })
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/saml-applications/5')
    expect(w.text()).toContain('https://sp/acs')
    await w.find('input[name="displayName"]').setValue('GHES 2')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/saml-applications/5', expect.objectContaining({ displayName: 'GHES 2', allowIdpInitiated: true }))
    expect(w.text()).toContain(en.admin.saml.saved)
  })
  it('not found', async () => {
    get.mockRejectedValue({ code: 'credential_not_found', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.saml.notFound)
  })
  it('re-ingests metadata', async () => {
    get.mockResolvedValue(SP); post.mockResolvedValue(SP)
    const w = mountView(); await flushPromises()
    await w.find('textarea[name="reingestXml"]').setValue('<xml2/>')
    await w.find('[data-test="reingest"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/saml-applications/5/reingest-metadata', { metadataXml: '<xml2/>' })
    expect(w.text()).toContain(en.admin.saml.reingestDone)
  })
  it('deletes and navigates to the list', async () => {
    get.mockResolvedValue(SP); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="delete"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.saml.delete); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/saml-applications/delete', { id: 5 })
    expect(push).toHaveBeenCalledWith('/admin/saml-applications')
  })
  it('does not navigate when delete fails', async () => {
    get.mockResolvedValue(SP)
    post.mockRejectedValue({ code: 'credential_not_found', message: 'zh' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="delete"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.saml.delete); await flushPromises()
    expect(push).not.toHaveBeenCalled()
    expect(w.text()).toContain(en.errors.credential_not_found)
  })
  it('disables the provider via set-disabled and flips the badge', async () => {
    get.mockResolvedValue(SP); post.mockResolvedValue({ ...SP, disabled: true })
    const w = mountView(); await flushPromises()
    expect(w.find('[data-test="status-badge"]').text()).toBe(en.admin.saml.active)
    await w.find('[data-test="disable-toggle"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/saml-applications/set-disabled', { id: 5, disabled: true })
    expect(w.find('[data-test="status-badge"]').text()).toBe(en.admin.saml.disabled)
  })
  it('seeds attributeMap rows from the loaded provider', async () => {
    get.mockResolvedValue(SP)
    const w = mountView(); await flushPromises()
    // Row editor renders one row for the single entry in SP.attributeMap
    expect(w.find('[data-test="attr-row-0"]').exists()).toBe(true)
    const nameInput = w.find<HTMLInputElement>('[data-test="attr-name-0"]')
    expect(nameInput.element.value).toBe('USERNAME')
    const sourceInput = w.find<HTMLInputElement>('[data-test="attr-source-0"]')
    expect(sourceInput.element.value).toBe('username')
  })
  it('sends attributeMap as an array in PUT body alongside other fields', async () => {
    get.mockResolvedValue(SP); put.mockResolvedValue({ ...SP, attributeMap: SP.attributeMap })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/saml-applications/5', expect.objectContaining({
      attributeMap: SP.attributeMap,
      displayName: 'GHES',
      allowIdpInitiated: true,
    }))
    expect(w.text()).toContain(en.admin.saml.saved)
  })
  it('removes an attributeMap row and sends the shorter array on save', async () => {
    get.mockResolvedValue(SP); put.mockResolvedValue({ ...SP, attributeMap: [] })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="attr-remove-0"]').trigger('click')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/saml-applications/5', expect.objectContaining({
      attributeMap: [],
    }))
  })
})
