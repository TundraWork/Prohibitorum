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
import AdminUpstreamIdpsView from './AdminUpstreamIdpsView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminUpstreamIdpsView, { global: { plugins: [i18n()] }, attachTo: document.body })
const IDPS = [
  { slug: 'okta', displayName: 'Okta', issuerUrl: 'https://okta/', clientId: 'c1', scopes: ['openid'], mode: 'auto_provision', allowedDomains: [], usernameClaim: 'preferred_username', displayNameClaim: 'name', emailClaim: 'email', pictureClaim: 'picture', requireVerifiedEmail: false, disabled: false, createdAt: '2026-01-01T00:00:00Z' },
  { slug: 'entra', displayName: 'Entra', issuerUrl: 'https://entra/', clientId: 'c2', scopes: ['openid'], mode: 'invite_only', allowedDomains: [], usernameClaim: 'preferred_username', displayNameClaim: 'name', emailClaim: 'email', pictureClaim: 'picture', requireVerifiedEmail: true, disabled: true, createdAt: '2026-01-02T00:00:00Z' },
]
beforeEach(() => { get.mockReset(); post.mockReset(); push.mockReset() })

describe('AdminUpstreamIdpsView', () => {
  it('lists providers with mode + state', async () => {
    get.mockResolvedValue(IDPS)
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/identity-providers')
    expect(w.text()).toContain('Okta'); expect(w.text()).toContain(en.admin.upstream.modeInviteOnly)
  })
  it('has a dedicated Slug column header and shows slug in its own cell', async () => {
    get.mockResolvedValue(IDPS)
    const w = mountView(); await flushPromises()
    const headers = w.findAll('th')
    const headerTexts = headers.map((h) => h.text())
    expect(headerTexts).toContain(en.admin.upstream.colSlug)
    // slug column is between Name (index 0) and Mode (index 2)
    const slugHeaderIdx = headerTexts.indexOf(en.admin.upstream.colSlug)
    expect(slugHeaderIdx).toBe(1)
    // okta row: its slug 'okta' appears as a standalone cell (not a sub-line of the name cell)
    const row = w.find('[data-test="idp-row-okta"]')
    const cells = row.findAll('td')
    // Name cell should only contain the display name, not the slug
    expect(cells[0].text()).toBe('Okta')
    expect(cells[0].text()).not.toContain('okta')
    // Slug cell (index 1) should contain the slug
    expect(cells[1].text()).toBe('okta')
  })
  it('row click navigates to detail', async () => {
    get.mockResolvedValue(IDPS)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="idp-row-okta"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/admin/identity-providers/okta')
  })
  it('creates a provider with mode + secret via withSudo', async () => {
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
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/identity-providers', expect.objectContaining({
      slug: 'new', displayName: 'New', issuerUrl: 'https://new/', clientId: 'cid', clientSecret: 'sek', mode: 'link_only',
    }))
    expect(w.text()).toContain(en.admin.upstream.created)
  })
  it('includes pictureClaim in create payload and renders the input', async () => {
    get.mockResolvedValue([])
    post.mockResolvedValue({ slug: 'new', displayName: 'New', mode: 'auto_provision', pictureClaim: 'avatar' })
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
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/identity-providers', expect.objectContaining({ pictureClaim: 'avatar' }))
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
    expect(w.text()).toContain(en.errors.upstream_idp_already_exists)
  })
  it('hides the empty-state while the create form is open', async () => {
    get.mockResolvedValue([])
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.upstream.empty)
    await w.find('[data-test="create"]').trigger('click'); await flushPromises()
    expect(w.text()).not.toContain(en.admin.upstream.empty)
  })
})
