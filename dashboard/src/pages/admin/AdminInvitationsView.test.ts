import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const get = vi.mocked(api.get); const post = vi.mocked(api.post)
import AdminInvitationsView from './AdminInvitationsView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminInvitationsView, { global: { plugins: [i18n()] }, attachTo: document.body })
const IDPS = [{ slug: 'okta', displayName: 'Okta', disabled: false }]
const INVITES = [
  { token: 'tok1', url: 'https://x/enroll/tok1', role: 'user', createdAt: '2026-06-01T00:00:00Z', expiresAt: '2026-06-09T00:00:00Z' },
]
function clickConfirm(label: string) {
  const btns = Array.from(document.body.querySelectorAll('button'))
    .filter((b) => b.getAttribute('data-variant') === 'destructive' && b.textContent?.includes(label))
  btns[btns.length - 1]!.click()
}
beforeEach(() => { get.mockReset(); post.mockReset() })

describe('AdminInvitationsView', () => {
  it('lists outstanding invitations with their URL', async () => {
    get.mockImplementation(async (p: string) => p.includes('/upstream-idps') ? IDPS : INVITES)
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/invitations')
    expect(w.text()).toContain('https://x/enroll/tok1')
  })
  it('shows empty state', async () => {
    get.mockImplementation(async (p: string) => p.includes('/upstream-idps') ? IDPS : [])
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.invitations.empty)
  })
  it('creates an invitation then refreshes', async () => {
    get.mockImplementation(async (p: string) => p.includes('/upstream-idps') ? IDPS : [])
    post.mockResolvedValue({ url: 'https://x/enroll/new', expiresAt: '2026-06-10T00:00:00Z' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click'); await flushPromises()
    await w.find<HTMLSelectElement>('select[name="newRole"]').setValue('admin')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/invitations', { role: 'admin' })
    expect(w.text()).toContain(en.admin.invitations.created)
    expect(get).toHaveBeenCalledTimes(4) // initial load (invitations + upstream-idps) + refresh (same two)
  })
  it('keeps the create form open when create fails', async () => {
    get.mockImplementation(async (p: string) => p.includes('/upstream-idps') ? IDPS : [])
    post.mockRejectedValue({ code: 'invalid_role', message: 'zh' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(w.find('[data-test="create-confirm"]').exists()).toBe(true)
    expect(w.text()).toContain(en.errors.invalid_role)
  })
  it('revokes an invitation (confirm → post → refresh)', async () => {
    get.mockImplementation(async (p: string) => p.includes('/upstream-idps') ? IDPS : INVITES)
    post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="revoke-tok1"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.invitations.revoke); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/invitations/revoke', { token: 'tok1' })
    expect(get).toHaveBeenCalledTimes(4) // initial (invitations + upstream-idps) + refresh (same two)
  })
  it('creates a federation-bound invitation when an IdP is chosen', async () => {
    get.mockImplementation(async (p: string) => p.includes('/upstream-idps') ? [{ slug: 'okta', displayName: 'Okta', disabled: false }] : [])
    post.mockResolvedValue({ url: 'https://x/enroll/n', expiresAt: '2026-06-10T00:00:00Z' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click'); await flushPromises()
    await w.find<HTMLSelectElement>('select[name="idp"]').setValue('okta')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/invitations', { role: 'user', expectedUpstreamIdpSlug: 'okta' })
  })
  it('still loads invitations when the upstream-idps fetch fails', async () => {
    get.mockImplementation(async (p: string) => { if (p.includes('/upstream-idps')) throw new Error('forbidden'); return INVITES })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain('https://x/enroll/tok1')
    expect(w.find('[data-test="create"]').exists()).toBe(true)
  })
  it('shows the bound IdP displayName in the Method column', async () => {
    const bound = [{ token: 'tokf', url: 'https://x/enroll/tokf', role: 'user', createdAt: '2026-06-01T00:00:00Z', expiresAt: '2026-06-09T00:00:00Z', expectedUpstreamIdpSlug: 'okta' }]
    get.mockImplementation(async (p: string) => p.includes('/upstream-idps') ? [{ slug: 'okta', displayName: 'Okta', disabled: false }] : bound)
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain('Okta')
  })
  it('filters disabled IdPs out of the picker', async () => {
    get.mockImplementation(async (p: string) => p.includes('/upstream-idps')
      ? [{ slug: 'okta', displayName: 'Okta', disabled: false }, { slug: 'old', displayName: 'Old IdP', disabled: true }]
      : [])
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    const opts = w.findAll('select[name="idp"] option').map((o) => o.text())
    expect(opts).toContain('Okta')
    expect(opts).not.toContain('Old IdP')
  })
})
