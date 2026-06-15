import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))
import { api } from '@/lib/api'
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => Promise<unknown>) => fn() }))
const get = vi.mocked(api.get); const post = vi.mocked(api.post); const put = vi.mocked(api.put)
const { push } = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({
  useRouter: () => ({ push }),
  useRoute: () => ({ params: { id: '7' } }),
}))
import AdminGroupDetailView from './AdminGroupDetailView.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountView = () => mount(AdminGroupDetailView, {
  global: {
    plugins: [i18n()],
    stubs: { RouterLink: { props: ['to'], template: '<a :href="to"><slot/></a>' } },
  },
  attachTo: document.body,
})

/** Click the last destructive confirm button in the portal (matches ConfirmDialog pattern) */
function clickConfirm(label: string) {
  const b = Array.from(document.body.querySelectorAll('button')).filter(
    (x) => x.getAttribute('data-variant') === 'destructive' && x.textContent?.includes(label),
  )
  b[b.length - 1]!.click()
}

const GROUP = {
  id: 7,
  slug: 'engineering',
  displayName: 'Engineering',
  description: 'The eng team',
  exposedToDownstream: true,
  memberCount: 2,
  createdAt: '2026-01-01T00:00:00Z',
}

const MEMBERS: { id: number; username: string; displayName: string }[] = [
  { id: 10, username: 'alice', displayName: 'Alice' },
]

const ACCOUNTS = [
  { id: 10, username: 'alice', displayName: 'Alice', role: 'user', disabled: false },
  { id: 20, username: 'bob', displayName: 'Bob', role: 'user', disabled: false },
  { id: 30, username: 'carol', displayName: 'Carol', role: 'admin', disabled: false },
]

/** Wire get() to dispatch by URL so concurrent calls resolve independently */
function mockGetByUrl(map: Record<string, unknown>) {
  get.mockImplementation((url: string) => {
    if (url in map) return Promise.resolve(map[url])
    return Promise.reject({ code: 'server_error', message: `unmocked GET ${url}` })
  })
}

beforeEach(() => { get.mockReset(); post.mockReset(); put.mockReset(); push.mockReset() })

describe('AdminGroupDetailView', () => {
  it('group load populates the edit fields (displayName, slug, description, exposed)', async () => {
    mockGetByUrl({
      '/api/prohibitorum/groups/7': GROUP,
      '/api/prohibitorum/groups/7/members': MEMBERS,
      '/api/prohibitorum/accounts': ACCOUNTS,
    })
    const w = mountView(); await flushPromises()
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/groups/7')
    expect((w.find('[data-test="slug"]').element as HTMLInputElement).value).toBe('engineering')
    expect((w.find('[data-test="displayName"]').element as HTMLInputElement).value).toBe('Engineering')
    expect((w.find('[data-test="description"]').element as HTMLInputElement).value).toBe('The eng team')
    // exposed switch exists and the group page title is rendered
    expect(w.find('[data-test="exposed"]').exists()).toBe(true)
    expect(w.text()).toContain('Engineering')
  })

  it('add-member picker is populated — GET /accounts actually fires (Fix 1: useApi busy-guard race)', async () => {
    // alice (id=10) is already a member; bob + carol should appear as addable options.
    // Before the fix: loadAccounts() shared memberApi with loadMembers(); the concurrent
    // Promise.all([loadMembers(), loadAccounts()]) caused loadAccounts() to see busy=true
    // and return undefined — allAccounts stayed [], addableAccounts stayed [].
    // After the fix: accountsApi is a separate useApi() instance, so both fire concurrently.
    mockGetByUrl({
      '/api/prohibitorum/groups/7': GROUP,
      '/api/prohibitorum/groups/7/members': MEMBERS,   // alice only
      '/api/prohibitorum/accounts': ACCOUNTS,           // alice + bob + carol
    })
    const w = mountView(); await flushPromises()

    // The key assertion: GET /accounts must have been called (not skipped by the busy guard)
    expect(get).toHaveBeenCalledWith('/api/prohibitorum/accounts')

    // Verify allAccounts and addableAccounts via the component's reactive state.
    // vue-test-utils exposes <script setup> variables on the vm instance.
    // allAccounts should have 3 entries (alice + bob + carol)
    // addableAccounts filters out alice (already a member), leaving bob + carol
    const vm = w.vm as unknown as {
      allAccounts: Array<{ id: number; username: string; displayName: string }>
      addableAccounts: Array<{ id: number; username: string; displayName: string }>
    }
    expect(vm.allAccounts).toHaveLength(3)
    expect(vm.addableAccounts).toHaveLength(2)
    expect(vm.addableAccounts.map((a) => a.username)).toContain('bob')
    expect(vm.addableAccounts.map((a) => a.username)).toContain('carol')
    expect(vm.addableAccounts.map((a) => a.username)).not.toContain('alice')
  })

  it('404: a group_not_found error from the group load shows the not-found state', async () => {
    get.mockRejectedValue({ code: 'group_not_found', message: 'zh' })
    const w = mountView(); await flushPromises()
    expect(w.text()).toContain(en.admin.groups.notFound)
    // The form must NOT be rendered
    expect(w.find('[data-test="save"]').exists()).toBe(false)
  })

  it('delete confirm navigates away and calls the delete endpoint', async () => {
    mockGetByUrl({
      '/api/prohibitorum/groups/7': GROUP,
      '/api/prohibitorum/groups/7/members': MEMBERS,
      '/api/prohibitorum/accounts': ACCOUNTS,
    })
    post.mockResolvedValue(undefined)
    const w = mountView(); await flushPromises()
    // Open the confirmation dialog
    await w.find('[data-test="delete"]').trigger('click'); await flushPromises()
    // Click the destructive confirm button rendered in the portal
    clickConfirm(en.admin.groups.delete); await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/groups/delete', { id: 7 })
    expect(push).toHaveBeenCalledWith('/admin/groups')
  })

  it('save calls PUT with updated fields and shows the Saved notice', async () => {
    mockGetByUrl({
      '/api/prohibitorum/groups/7': GROUP,
      '/api/prohibitorum/groups/7/members': MEMBERS,
      '/api/prohibitorum/accounts': ACCOUNTS,
    })
    put.mockResolvedValue({ ...GROUP, displayName: 'Engineering Renamed' })
    const w = mountView(); await flushPromises()
    await w.find('input[name="displayName"]').setValue('Engineering Renamed')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).toHaveBeenCalledWith('/api/prohibitorum/groups/7', expect.objectContaining({
      displayName: 'Engineering Renamed',
    }))
    expect(w.text()).toContain(en.admin.groups.saved)
  })

  it('rejects an invalid slug client-side without calling the API', async () => {
    mockGetByUrl({
      '/api/prohibitorum/groups/7': GROUP,
      '/api/prohibitorum/groups/7/members': MEMBERS,
      '/api/prohibitorum/accounts': ACCOUNTS,
    })
    const w = mountView(); await flushPromises()
    await w.find('input[name="slug"]').setValue('BAD SLUG!')
    await w.find('[data-test="slug"]').trigger('input')
    await w.find('[data-test="save"]').trigger('click'); await flushPromises()
    expect(put).not.toHaveBeenCalled()
    expect(w.text()).toContain(en.admin.groups.slugInvalid)
  })
})
