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
import AdminGroupsView from './AdminGroupsView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminGroupsView, { global: { plugins: [i18n()] }, attachTo: document.body })

const GROUPS = [
  { id: 1, slug: 'engineering', displayName: 'Engineering', description: 'Eng team', exposedToDownstream: true, memberCount: 5, createdAt: '2026-01-01T00:00:00Z' },
  { id: 2, slug: 'support', displayName: 'Support', exposedToDownstream: false, memberCount: 2, createdAt: '2026-01-02T00:00:00Z' },
]

beforeEach(() => { get.mockReset(); post.mockReset(); push.mockReset() })

describe('AdminGroupsView', () => {
  it('lists groups with stacked name/slug cells and exposed badge', async () => {
    get.mockResolvedValue(GROUPS)
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/groups')
    expect(w.text()).toContain('Engineering')
    expect(w.text()).toContain('engineering')
    expect(w.text()).toContain('Support')
    expect(w.text()).toContain(en.admin.groups.exposedYes)
    expect(w.text()).toContain(en.admin.groups.exposedNo)
  })

  it('row click navigates to detail', async () => {
    get.mockResolvedValue(GROUPS)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="group-row-1"]').trigger('click')
    expect(push).toHaveBeenCalledWith('/admin/groups/1')
  })

  it('row is keyboard-activatable (Enter)', async () => {
    get.mockResolvedValue(GROUPS)
    const w = mountView(); await flushPromises()
    await w.find('[data-test="group-row-2"]').trigger('keydown.enter')
    expect(push).toHaveBeenCalledWith('/admin/groups/2')
  })

  it('shows empty state when no groups', async () => {
    get.mockResolvedValue([])
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.groups.empty)
  })

  it('creates a group and shows created note', async () => {
    get.mockResolvedValue([])
    post.mockResolvedValue({ id: 3, slug: 'admins', displayName: 'Admins', exposedToDownstream: false, memberCount: 0, createdAt: '2026-01-03T00:00:00Z' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('input[name="slug"]').setValue('admins')
    await w.find('input[name="displayName"]').setValue('Admins')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/groups', expect.objectContaining({
      slug: 'admins', displayName: 'Admins',
    }))
    expect(w.text()).toContain(en.admin.groups.created)
  })

  it('rejects invalid slug client-side', async () => {
    get.mockResolvedValue([])
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('input[name="slug"]').setValue('BAD SLUG!')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(post).not.toHaveBeenCalled()
    expect(w.text()).toContain(en.admin.groups.slugInvalid)
  })

  it('surfaces group_slug_conflict error', async () => {
    get.mockResolvedValue([])
    post.mockRejectedValue({ code: 'group_slug_conflict', message: 'zh' })
    const w = mountView(); await flushPromises()
    await w.find('[data-test="create"]').trigger('click')
    await w.find('input[name="slug"]').setValue('dupe')
    await w.find('input[name="displayName"]').setValue('Dupe')
    await w.find('[data-test="create-confirm"]').trigger('click'); await flushPromises()
    expect(w.text()).toContain(en.errors.codes.group_slug_conflict)
  })

  it('surfaces an app load error inline', async () => {
    // App 4xx codes still render inline; connectivity/5xx (server_error) are now
    // suppressed here and surfaced via the global toast instead.
    get.mockRejectedValue({ code: 'forbidden', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.errors.codes.forbidden)
  })

  it('does NOT render server_error inline (global toast owns it)', async () => {
    get.mockRejectedValue({ code: 'server_error', message: 'boom' })
    const w = mountView(); await flushPromises()
    expect(w.text()).not.toContain(en.errors.codes.server_error)
  })
})
