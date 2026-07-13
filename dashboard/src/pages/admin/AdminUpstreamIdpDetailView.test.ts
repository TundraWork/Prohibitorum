import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const get = vi.mocked(api.get); const post = vi.mocked(api.post); const put = vi.mocked(api.put)
const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { slug: 'okta' } }), useRouter: () => ({ push }), RouterLink: { template: '<a><slot/></a>' } }))
import AdminUpstreamIdpDetailView from './AdminUpstreamIdpDetailView.vue'

const IDP = { slug: 'okta', displayName: 'Okta', issuerUrl: 'https://okta/', clientId: 'c1', scopes: ['openid','email'], mode: 'auto_provision', allowedDomains: ['ex.com'], usernameClaim: 'preferred_username', displayNameClaim: 'name', emailClaim: 'email', pictureClaim: 'picture', requireVerifiedEmail: false, disabled: false, createdAt: '2026-01-01T00:00:00Z', allowPrivateNetwork: false }
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminUpstreamIdpDetailView, { global: { plugins: [i18n()], stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } } }, attachTo: document.body })
const clickConfirm = async (_w: ReturnType<typeof mountView>, label: string) => {
  const btns = Array.from(document.body.querySelectorAll('button')).filter((b) => b.textContent?.trim() === label && b.classList.contains('bg-destructive'))
  btns[btns.length - 1]!.click(); await flushPromises()
}
beforeEach(() => { get.mockReset(); post.mockReset(); put.mockReset(); push.mockReset() })

describe('AdminUpstreamIdpDetailView', () => {
  it('shows not-found on upstream_idp_not_found', async () => {
    get.mockRejectedValue({ code: 'upstream_idp_not_found', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.upstream.notFound)
  })
  it('shows the slug as a read-only field (not an editable input)', async () => {
    get.mockResolvedValue(IDP)
    const w = mountView(); await flushPromises()
    // The read-only slug element exists and shows the slug value
    const slugEl = w.find('[data-test="idp-slug"]')
    expect(slugEl.exists()).toBe(true)
    expect(slugEl.text()).toBe('okta')
    // No input has name="slug" (slug is the immutable identifier, not editable)
    expect(w.find('input[name="slug"]').exists()).toBe(false)
  })
  it('saves config via PUT without clientSecret', async () => {
    get.mockResolvedValue(IDP); put.mockResolvedValue({ ...IDP, displayName: 'Okta 2' })
    const w = mountView(); await flushPromises()
    await w.find('input[name="displayName"]').setValue('Okta 2')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    const body = put.mock.calls[0][1] as Record<string, unknown>
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/identity-providers/okta', expect.objectContaining({ displayName: 'Okta 2', disabled: false }))
    expect(body).not.toHaveProperty('clientSecret')
    expect(w.text()).toContain(en.admin.upstream.saved)
  })
  it('groups the disable button, rotate-secret and delete in the Danger zone card', async () => {
    get.mockResolvedValue(IDP)
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
    get.mockResolvedValue(IDP) // disabled: false → button reads "Disable"
    post.mockResolvedValue({ ...IDP, disabled: true })
    const w = mountView(); await flushPromises()
    const btn = w.find('[data-test="disable-toggle"]')
    expect(btn.text()).toBe(en.admin.upstream.disable)
    expect(w.find('[data-test="status-badge"]').text()).toBe(en.admin.upstream.active) // green "Active"
    await btn.trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/identity-providers/set-disabled', { slug: 'okta', disabled: true })
    expect(put).not.toHaveBeenCalled() // independent of the config Save
    expect(w.find('[data-test="disable-toggle"]').text()).toBe(en.admin.upstream.enable) // flipped to "Enable"
    expect(w.find('[data-test="status-badge"]').text()).toBe(en.admin.upstream.disabled) // now "Disabled"
  })
  it('includes pictureClaim in save payload and renders the input', async () => {
    get.mockResolvedValue(IDP); put.mockResolvedValue({ ...IDP, pictureClaim: 'avatar_url' })
    const w = mountView(); await flushPromises()
    expect(w.find('input[name="pictureClaim"]').exists()).toBe(true)
    await w.find('input[name="pictureClaim"]').setValue('avatar_url')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/identity-providers/okta', expect.objectContaining({ pictureClaim: 'avatar_url' }))
  })
  it('renders claim inputs as a compact grid with default-value placeholders', async () => {
    get.mockResolvedValue(IDP)
    const w = mountView(); await flushPromises()
    expect(w.find('[data-test="claim-username"]').attributes('placeholder')).toBe('preferred_username')
    expect(w.find('[data-test="claim-displayName"]').attributes('placeholder')).toBe('name')
    expect(w.find('[data-test="claim-email"]').attributes('placeholder')).toBe('email')
    expect(w.find('[data-test="claim-avatar"]').attributes('placeholder')).toBe('picture')
    // All four inputs still bound to correct v-models (values round-trip from IDP fixture)
    expect((w.find('input[name="usernameClaim"]').element as HTMLInputElement).value).toBe('preferred_username')
    expect((w.find('input[name="emailClaim"]').element as HTMLInputElement).value).toBe('email')
  })
  it('rotates the secret without revealing a value', async () => {
    get.mockResolvedValue(IDP); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('input[name="newSecret"]').setValue('newsek')
    await w.find('[data-test="rotate"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/identity-providers/rotate-secret', { slug: 'okta', clientSecret: 'newsek' })
    expect(w.text()).toContain(en.admin.upstream.rotated)
  })
  it('deletes and navigates back to the list', async () => {
    get.mockResolvedValue(IDP); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="delete"]').trigger('click'); await flushPromises()
    await clickConfirm(w, en.admin.upstream.delete)
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/identity-providers/delete', { slug: 'okta' })
    expect(push).toHaveBeenCalledWith('/admin/identity-providers')
  })
  it('surfaces a generic app load error in the Alert without showing not-found', async () => {
    // A non-not-found app 4xx still renders inline; connectivity/5xx
    // (server_error) is now suppressed here and shown via the global toast.
    get.mockRejectedValue({ code: 'forbidden', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.errors.codes.forbidden)
    expect(w.text()).not.toContain(en.admin.upstream.notFound)
  })
  it('does not navigate when delete fails', async () => {
    get.mockResolvedValue(IDP); post.mockRejectedValue({ code: 'server_error', message: 'boom' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="delete"]').trigger('click'); await flushPromises()
    await clickConfirm(w, en.admin.upstream.delete)
    expect(push).not.toHaveBeenCalled()
  })
  it('renders the private network toggle with a security warning', async () => {
    get.mockResolvedValue(IDP)
    const w = mountView(); await flushPromises()
    expect(w.find('[data-test="allowPrivateNetwork"]').exists()).toBe(true)
    const warning = w.get('[data-test="private-network-warning"]')
    expect(warning.attributes('data-slot')).toBe('alert')
    expect(warning.get('[data-slot="alert-description"]').text()).toContain(
      en.admin.upstream.allowPrivateNetworkWarning,
    )
  })
  it('includes allowPrivateNetwork in the save payload', async () => {
    get.mockResolvedValue({ ...IDP, allowPrivateNetwork: false })
    put.mockResolvedValue({ ...IDP, allowPrivateNetwork: true })
    const w = mountView(); await flushPromises()
    // Toggle the switch on
    await w.find('[data-test="allowPrivateNetwork"]').trigger('click')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    const body = put.mock.calls[0][1] as Record<string, unknown>
    expect(body).toHaveProperty('allowPrivateNetwork', true)
  })
})
