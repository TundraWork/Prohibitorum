import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
const { withSudo } = vi.hoisted(() => ({
  withSudo: vi.fn((fn: () => Promise<unknown>) => fn()),
}))
vi.mock('@/lib/sudo', () => ({ withSudo }))
const get = vi.mocked(api.get); const post = vi.mocked(api.post)
const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push }) }))
import AdminUpstreamIdpsView from './AdminUpstreamIdpsView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminUpstreamIdpsView, { global: { plugins: [i18n()] }, attachTo: document.body })
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}
const OIDC_CONFIG = {
  issuerUrl: 'https://okta/',
  clientId: 'c1',
  scopes: ['openid'],
  allowedDomains: [],
  usernameClaim: 'preferred_username',
  displayNameClaim: 'name',
  emailClaim: 'email',
  pictureClaim: 'picture',
  requireVerifiedEmail: false,
  allowPrivateNetwork: false,
}
const IDPS = [
  { slug: 'okta', displayName: 'Okta', protocol: 'oidc', mode: 'auto_provision', config: OIDC_CONFIG, disabled: false, secretConfigured: true, secretStatus: 'valid', secretValidatedAt: null, ready: true, supportsOperator: false, searchFields: [], createdAt: '2026-01-01T00:00:00Z' },
  { slug: 'entra', displayName: 'Entra', protocol: 'oidc', mode: 'invite_only', config: { ...OIDC_CONFIG, issuerUrl: 'https://entra/', clientId: 'c2', requireVerifiedEmail: true }, disabled: true, secretConfigured: true, secretStatus: 'configured', secretValidatedAt: null, ready: false, supportsOperator: false, searchFields: [], createdAt: '2026-01-02T00:00:00Z' },
]
beforeEach(() => {
  get.mockReset(); post.mockReset(); push.mockReset(); withSudo.mockClear()
})

describe('AdminUpstreamIdpsView', () => {
  it('lists providers with mode + state', async () => {
    get.mockResolvedValue({ items: IDPS, nextCursor: '' })
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/identity-providers')
    expect(w.text()).toContain('Okta'); expect(w.text()).toContain(en.admin.upstream.modeInviteOnly)
  })
  it('shows name + slug stacked in the first cell, with both named in its header', async () => {
    get.mockResolvedValue({ items: IDPS, nextCursor: '' })
    const w = mountView(); await flushPromises()
    // The first column header names both lines (Provider · Slug).
    const firstHeader = w.findAll('th')[0].text()
    expect(firstHeader).toContain(en.admin.upstream.colName)
    expect(firstHeader).toContain(en.admin.upstream.colSlug)
    // okta row: the first cell stacks the display name AND the slug (two-line).
    const cells = w.find('[data-test="idp-row-okta"]').findAll('td')
    expect(cells[0].text()).toContain('Okta')
    expect(cells[0].text()).toContain('okta')
    // There is no longer a standalone slug column: 3 columns (Name·Slug / Mode / State).
    expect(cells.length).toBe(3)
  })
  it('row click navigates to detail', async () => {
    get.mockResolvedValue({ items: IDPS, nextCursor: '' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="idp-row-okta"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/admin/identity-providers/okta')
  })
  it('creates an OIDC provider with adapter config and a generic secret', async () => {
    get.mockResolvedValue([])
    post.mockResolvedValue({ slug: 'new', displayName: 'New', mode: 'link_only' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('input[name="slug"]').setValue('new')
    await w.find('input[name="displayName"]').setValue('New')
    await w.find('input[name="issuerUrl"]').setValue('https://new/')
    await w.find('input[name="clientId"]').setValue('cid')
    await w.find('input[name="clientSecret"]').setValue('sek')
    await w.find('[data-test="radio-card-link_only"]').trigger('click'); await flushPromises()
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/identity-providers', {
      slug: 'new',
      displayName: 'New',
      protocol: 'oidc',
      mode: 'link_only',
      config: {
        issuerUrl: 'https://new/',
        clientId: 'cid',
        scopes: ['openid', 'profile', 'email'],
        allowedDomains: [],
        usernameClaim: 'preferred_username',
        displayNameClaim: 'name',
        emailClaim: 'email',
        pictureClaim: 'picture',
        requireVerifiedEmail: false,
        allowPrivateNetwork: false,
      },
      secret: 'sek',
    })
    expect(w.text()).toContain(en.admin.upstream.created)
  })
  it('creates a Steam provider with empty adapter config and a generic secret', async () => {
    get.mockResolvedValue([])
    post.mockResolvedValue({ slug: 'steam', displayName: 'Steam', protocol: 'steam', mode: 'auto_provision', config: {} })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('[data-test="radio-card-steam"]').trigger('click'); await flushPromises()
    await w.find('input[name="slug"]').setValue('steam')
    await w.find('input[name="displayName"]').setValue('Steam')
    await w.find('input[name="apiKey"]').setValue('api-key')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/identity-providers', {
      slug: 'steam',
      displayName: 'Steam',
      protocol: 'steam',
      mode: 'auto_provision',
      config: {},
      secret: 'api-key',
    })
  })
  it('locks VRChat creation to link only and explains the local credential flow', async () => {
    get.mockResolvedValue([])
    post.mockResolvedValue({
      slug: 'vrchat',
      displayName: 'VRChat moderation',
      protocol: 'vrchat',
      mode: 'link_only',
      config: {},
    })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    expect(w.text()).toContain(en.admin.upstream.protocolVrchat)

    await w.find('[data-test="radio-card-invite_only"]').trigger('click')
    await w.find('[data-test="radio-card-vrchat"]').trigger('click')
    await flushPromises()

    expect(w.text()).toContain(en.admin.upstream.vrchatCreateWarning)
    expect(
      w.get('[data-test="vrchat-create-warning"]').get('[data-slot="alert-description"]').classes(),
    ).toContain('max-w-[75ch]')
    expect(w.get('[data-test="vrchat-create-warning"]').attributes('role')).toBe('note')
    const fixedMode = w.get('[data-test="vrchat-fixed-mode"]')
    expect(fixedMode.text()).toContain(en.admin.upstream.modeLinkOnly)
    expect(fixedMode.text()).toContain(en.admin.upstream.vrchatLinkOnlyDescription)
    expect(w.find('[data-test="radio-card-auto_provision"]').exists()).toBe(false)
    expect(w.find('[data-test="radio-card-invite_only"]').exists()).toBe(false)
    expect(w.find('[data-test="radio-card-link_only"]').exists()).toBe(false)
    expect(w.find('input[name="issuerUrl"]').exists()).toBe(false)
    expect(w.find('input[name="clientId"]').exists()).toBe(false)
    expect(w.find('input[name="clientSecret"]').exists()).toBe(false)
    expect(w.find('input[name="apiKey"]').exists()).toBe(false)
    expect(w.find('input[name="usernameClaim"]').exists()).toBe(false)
    expect(w.find('input[name="allowedDomains"]').exists()).toBe(false)

    await w.find('input[name="slug"]').setValue('vrchat')
    await w.find('input[name="displayName"]').setValue('VRChat moderation')
    await w.find('[data-test="create-confirm"]').trigger('click')
    await flushPromises()

    expect(withSudo).toHaveBeenCalledOnce()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/identity-providers', {
      slug: 'vrchat',
      displayName: 'VRChat moderation',
      protocol: 'vrchat',
      mode: 'link_only',
      config: {},
    })
    expect(push).toHaveBeenCalledWith('/admin/identity-providers/vrchat')
  })

  it('restores the selected provisioning mode after switching back from VRChat', async () => {
    get.mockResolvedValue([])
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('[data-test="radio-card-invite_only"]').trigger('click')
    await w.find('[data-test="radio-card-vrchat"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-test="radio-card-invite_only"]').exists()).toBe(false)

    await w.find('[data-test="radio-card-steam"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-test="radio-card-invite_only"]').attributes('data-state')).toBe('checked')
  })
  it('routes from the authoritative create response after the form changes', async () => {
    get.mockResolvedValue([])
    const response = deferred<unknown>()
    post.mockReturnValue(response.promise)
    const w = mountView()
    await flushPromises()
    await w.get('[data-test="create"]').trigger('click')
    await w.get('[data-test="radio-card-vrchat"]').trigger('click')
    await w.get('input[name="slug"]').setValue('requested-slug')
    await w.get('input[name="displayName"]').setValue('Requested provider')
    await w.get('[data-test="create-confirm"]').trigger('click')

    await w.get('[data-test="radio-card-oidc"]').trigger('click')
    await w.get('input[name="slug"]').setValue('mutated-slug')
    response.resolve({
      slug: 'returned-vrchat',
      displayName: 'Returned VRChat',
      protocol: 'vrchat',
      mode: 'auto_provision',
      config: {},
    })
    await flushPromises()

    expect(push).toHaveBeenCalledWith('/admin/identity-providers/returned-vrchat')
  })
  it('includes pictureClaim in create payload and renders the input', async () => {
    get.mockResolvedValue([])
    post.mockResolvedValue({ slug: 'new', displayName: 'New', mode: 'auto_provision', config: { ...OIDC_CONFIG, pictureClaim: 'avatar' } })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    expect(w.find('input[name="pictureClaim"]').exists()).toBe(true)
    await w.find('input[name="slug"]').setValue('new')
    await w.find('input[name="displayName"]').setValue('New')
    await w.find('input[name="issuerUrl"]').setValue('https://new/')
    await w.find('input[name="clientId"]').setValue('cid')
    await w.find('input[name="clientSecret"]').setValue('sek')
    await w.find('input[name="pictureClaim"]').setValue('avatar')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/identity-providers', expect.objectContaining({
      config: expect.objectContaining({ pictureClaim: 'avatar' }),
    }))
  })
  it('renders claim inputs as a compact grid with default-value placeholders and pre-filled defaults', async () => {
    get.mockResolvedValue([])
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    expect(w.find('[data-test="claim-username"]').attributes('placeholder')).toBe('preferred_username')
    expect(w.find('[data-test="claim-displayName"]').attributes('placeholder')).toBe('name')
    expect(w.find('[data-test="claim-email"]').attributes('placeholder')).toBe('email')
    expect(w.find('[data-test="claim-avatar"]').attributes('placeholder')).toBe('picture')
    // Create form pre-fills the schema defaults
    expect((w.find('input[name="usernameClaim"]').element as HTMLInputElement).value).toBe('preferred_username')
    expect((w.find('input[name="displayNameClaim"]').element as HTMLInputElement).value).toBe('name')
    expect((w.find('input[name="emailClaim"]').element as HTMLInputElement).value).toBe('email')
    expect((w.find('input[name="pictureClaim"]').element as HTMLInputElement).value).toBe('picture')
  })
  it('surfaces upstream_idp_already_exists', async () => {
    get.mockResolvedValue([])
    post.mockRejectedValue({ code: 'upstream_idp_already_exists', message: 'zh' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('input[name="slug"]').setValue('okta')
    await w.find('input[name="displayName"]').setValue('Dup')
    await w.find('input[name="issuerUrl"]').setValue('https://x/')
    await w.find('input[name="clientId"]').setValue('c')
    await w.find('input[name="clientSecret"]').setValue('s')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(w.text()).toContain(en.errors.codes.upstream_idp_already_exists)
  })
  it('hides the empty-state while the create form is open', async () => {
    get.mockResolvedValue([])
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.upstream.empty)
    await w.find('[data-test="create"]').trigger('click'); await flushPromises()
    expect(w.text()).not.toContain(en.admin.upstream.empty)
  })
})
