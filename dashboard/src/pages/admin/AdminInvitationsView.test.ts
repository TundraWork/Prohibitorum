import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const get = vi.mocked(api.get); const post = vi.mocked(api.post)
import AdminInvitationsView from './AdminInvitationsView.vue'
import { Select } from '@/components/ui/select'
import { Checkbox } from '@/components/ui/checkbox'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminInvitationsView, { global: { plugins: [i18n()] }, attachTo: document.body })
const IDPS = [{ slug: 'okta', displayName: 'Okta', disabled: false, mode: 'auto_provision' }]
const INVITES = [
  { token: 'tok1', url: 'https://x/enroll/tok1', role: 'user', groupIds: [], groups: [], createdAt: '2026-06-01T00:00:00Z', expiresAt: '2026-06-09T00:00:00Z' },
]
function clickConfirm(label: string) {
  const btns = Array.from(document.body.querySelectorAll('button'))
    .filter((b) => b.getAttribute('data-variant') === 'destructive' && b.textContent?.includes(label))
  btns[btns.length - 1]!.click()
}
beforeEach(() => { get.mockReset(); post.mockReset() })

describe('AdminInvitationsView', () => {
  it('lists outstanding invitations with their URL', async () => {
    get.mockImplementation(async (p: string) => p.includes('/identity-providers') ? { items: IDPS, nextCursor: '' } : { items: INVITES, nextCursor: '' })
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/invitations', expect.objectContaining({ signal: expect.any(AbortSignal) }))
    expect(w.text()).toContain('https://x/enroll/tok1')
  })
  it('shows empty state', async () => {
    get.mockImplementation(async (p: string) => p.includes('/identity-providers') ? { items: IDPS, nextCursor: '' } : { items: [], nextCursor: '' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.invitations.empty)
  })
  it('creates an invitation then refreshes', async () => {
    get.mockImplementation(async (p: string) => p.includes('/identity-providers') ? { items: IDPS, nextCursor: '' } : { items: [], nextCursor: '' })
    post.mockResolvedValue({ url: 'https://x/enroll/new', expiresAt: '2026-06-10T00:00:00Z' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click'); await flushPromises()
    await w.find('[data-test="segment-admin"]').trigger('click'); await flushPromises()
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/invitations', { role: 'admin' })
    expect(w.text()).toContain(en.admin.invitations.created)
    expect(get).toHaveBeenCalledTimes(4) // initial resources + group picker + invitation reload
  })
  it('offers app manager invitations', async () => {
    get.mockImplementation(async (p: string) => p.includes('/identity-providers') ? { items: IDPS, nextCursor: '' } : { items: [], nextCursor: '' })
    post.mockResolvedValue({ url: 'https://x/enroll/manager', expiresAt: '2026-06-10T00:00:00Z' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click'); await flushPromises()
    await w.find('[data-test="segment-app_manager"]').trigger('click'); await flushPromises()
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/invitations', { role: 'app_manager' })
  })
  it('keeps the create form open when create fails', async () => {
    get.mockImplementation(async (p: string) => p.includes('/identity-providers') ? { items: IDPS, nextCursor: '' } : { items: [], nextCursor: '' })
    post.mockRejectedValue({ code: 'invalid_role', message: 'zh' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(w.find('[data-test="create-confirm"]').exists()).toBe(true)
    expect(w.text()).toContain(en.errors.codes.invalid_role)
  })
  it('revokes an invitation (confirm → post → refresh)', async () => {
    get.mockImplementation(async (p: string) => p.includes('/identity-providers') ? { items: IDPS, nextCursor: '' } : { items: INVITES, nextCursor: '' })
    post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="revoke-tok1"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.invitations.revoke); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/invitations/revoke', { token: 'tok1' })
    expect(get).toHaveBeenCalledTimes(3) // initial (invitations + upstream-idps) + refresh (invitations only)
  })
  it('creates a federation-bound invitation when an IdP is chosen', async () => {
    get.mockImplementation(async (p: string) => p.includes('/identity-providers') ? { items: [{ slug: 'okta', displayName: 'Okta', disabled: false, mode: 'invite_only' }], nextCursor: '' } : { items: [], nextCursor: '' })
    post.mockResolvedValue({ url: 'https://x/enroll/n', expiresAt: '2026-06-10T00:00:00Z' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click'); await flushPromises()
    const selects = w.findAllComponents(Select)  // [0]=idp (role is now SegmentedControl)
    await selects[0].vm.$emit('update:modelValue', 'okta'); await flushPromises()
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/invitations', { role: 'user', expectedUpstreamIdpSlug: 'okta' })
  })
  it('creates an invitation with a trimmed username and selected manual groups', async () => {
    const groups = [{ id: 12, slug: 'engineering', displayName: 'Engineering' }]
    get.mockImplementation(async (p: string) => {
      if (p.includes('/identity-providers')) return { items: IDPS, nextCursor: '' }
      if (p.includes('/groups?')) return { items: groups, nextCursor: '' }
      return { items: [], nextCursor: '' }
    })
    post.mockResolvedValue({ url: 'https://x/enroll/n', expiresAt: '2026-06-10T00:00:00Z' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click'); await flushPromises()
    await w.get('#newUsername').setValue('  alice  ')
    const checkbox = w.getComponent(Checkbox)
    checkbox.vm.$emit('update:modelValue', true)
    await flushPromises()
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/invitations', {
      role: 'user', username: 'alice', groupIds: [12],
    })
  })
  it('shows fixed usernames, resolved groups, and unavailable saved IDs', async () => {
    const invitation = [{
      token: 'tok2', url: 'https://x/enroll/tok2', role: 'user', username: 'alice',
      groupIds: [12, 404], groups: [{ id: 12, slug: 'engineering', displayName: 'Engineering' }],
      createdAt: '2026-06-01T00:00:00Z', expiresAt: '2026-06-09T00:00:00Z',
    }]
    get.mockImplementation(async (p: string) => p.includes('/identity-providers') ? { items: IDPS, nextCursor: '' } : { items: invitation, nextCursor: '' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain('alice')
    expect(w.text()).toContain('Engineering')
    expect(w.text()).toContain('Group #404 unavailable')
  })
  it('still loads invitations when the upstream-idps fetch fails', async () => {
    get.mockImplementation(async (p: string) => { if (p.includes('/identity-providers')) throw new Error('forbidden'); return { items: INVITES, nextCursor: '' } })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain('https://x/enroll/tok1')
    expect(w.find('[data-test="create"]').exists()).toBe(true)
  })
  it('shows the bound IdP displayName in the Method column', async () => {
    const bound = [{ token: 'tokf', url: 'https://x/enroll/tokf', role: 'user', groupIds: [], groups: [], createdAt: '2026-06-01T00:00:00Z', expiresAt: '2026-06-09T00:00:00Z', expectedUpstreamIdpSlug: 'okta' }]
    get.mockImplementation(async (p: string) => p.includes('/identity-providers') ? { items: [{ slug: 'okta', displayName: 'Okta', disabled: false, mode: 'invite_only' }], nextCursor: '' } : { items: bound, nextCursor: '' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain('Okta')
  })
  it('filters disabled IdPs out of the picker', async () => {
    get.mockImplementation(async (p: string) => p.includes('/identity-providers')
      ? { items: [{ slug: 'okta', displayName: 'Okta', disabled: false, mode: 'invite_only' }, { slug: 'old', displayName: 'Old IdP', disabled: true }], nextCursor: '' }
      : { items: [], nextCursor: '' })
    const w = mountView(); await flushPromises()
    // Assert the component's idp list (which feeds the Select items) only contains enabled IdPs
    const vm = w.vm as unknown as { idps: Array<{ slug: string; displayName: string; disabled: boolean }> }
    expect(vm.idps.map((i) => i.displayName)).toContain('Okta')
    expect(vm.idps.map((i) => i.displayName)).not.toContain('Old IdP')
  })

  it('filters link_only IdPs out of the picker', async () => {
    get.mockImplementation(async (p: string) => p.includes('/identity-providers')
      ? { items: [
          { slug: 'okta', displayName: 'Okta', disabled: false, mode: 'auto_provision' },
          { slug: 'bound', displayName: 'Bound Co', disabled: false, mode: 'invite_only' },
          { slug: 'vrchat', displayName: 'VRChat', disabled: false, mode: 'link_only' },
        ], nextCursor: '' }
      : { items: [], nextCursor: '' })
    const w = mountView(); await flushPromises()
    const vm = w.vm as unknown as { idps: Array<{ slug: string; displayName: string }> }
    const slugs = vm.idps.map((i) => i.slug)
    expect(slugs).toContain('okta')
    expect(slugs).toContain('bound')
    expect(slugs).not.toContain('vrchat')
  })
})
