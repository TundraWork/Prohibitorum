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
    get.mockResolvedValue(INVITES)
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/invitations')
    expect(w.text()).toContain('https://x/enroll/tok1')
  })
  it('shows empty state', async () => {
    get.mockResolvedValue([])
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.invitations.empty)
  })
  it('creates an invitation then refreshes', async () => {
    get.mockResolvedValue([]); post.mockResolvedValue({ url: 'https://x/enroll/new', expiresAt: '2026-06-10T00:00:00Z' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')         // open inline form
    await w.find<HTMLSelectElement>('select[name="newRole"]').setValue('admin')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/invitations', { role: 'admin' })
    expect(w.text()).toContain(en.admin.invitations.created)
    expect(get).toHaveBeenCalledTimes(2)
  })
  it('keeps the create form open when create fails', async () => {
    get.mockResolvedValue([])
    post.mockRejectedValue({ code: 'invalid_role', message: 'zh' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(w.find('[data-test="create-confirm"]').exists()).toBe(true)
    expect(w.text()).toContain(en.errors.invalid_role)
  })
  it('revokes an invitation (confirm → post → refresh)', async () => {
    get.mockResolvedValue(INVITES); post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="revoke-tok1"]').trigger('click'); await flushPromises()
    clickConfirm(en.admin.invitations.revoke); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/invitations/revoke', { token: 'tok1' })
    expect(get).toHaveBeenCalledTimes(2)
  })
})
