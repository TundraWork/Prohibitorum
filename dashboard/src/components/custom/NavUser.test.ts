import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { createRouter, createMemoryHistory, type Router } from 'vue-router'
import en from '@/locales/en'
import NavUser from './NavUser.vue'
import { SidebarProvider } from '@/components/ui/sidebar'
import { testQueryClient } from '@/testSetup'
import { keys, type SessionView } from '@/queries/resources'
import { api } from '@/lib/api'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn(), put: vi.fn() } }))

if (!window.matchMedia) {
  // @ts-expect-error jsdom lacks matchMedia
  window.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })
}

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const stub = defineComponent({ template: '<div/>' })
function makeRouter(): Router {
  return createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: stub }, { path: '/security', component: stub }, { path: '/admin/accounts', component: stub }, { path: '/logout', component: stub }],
  })
}
const Host = defineComponent({
  components: { SidebarProvider, NavUser },
  template: '<SidebarProvider><NavUser ref="nav" /></SidebarProvider>',
})

beforeEach(() => { document.body.innerHTML = ''; vi.clearAllMocks(); vi.mocked(api.get).mockReset().mockImplementation(async () => testQueryClient.getQueryData(keys.me) ?? null) })

async function mountHost(router: Router) {
  router.push('/security'); await router.isReady()
  const w = mount(Host, { attachTo: document.body, global: { plugins: [router, i18n()] } })
  await flushPromises()
  return w
}

describe('NavUser', () => {
  it('shows a skeleton (no trigger) while the session is loading', async () => {
    const w = await mountHost(makeRouter()) // auth.me is null
    expect(w.find('[data-test="account-trigger"]').exists()).toBe(false)
  })

  it('renders the trigger on the settings shell once the session arrives late', async () => {
    // Regression (PHB-48): the dashboard opened with `auth.me` still null (skeleton);
    // the menu must appear as soon as the shared session query settles, without a remount.
    const w = await mountHost(makeRouter()) // skeleton — no session yet
    expect(w.find('[data-test="account-trigger"]').exists()).toBe(false)

    testQueryClient.setQueryData<SessionView>(keys.me, { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' })
    await flushPromises()
    expect(w.find('[data-test="account-trigger"]').exists()).toBe(true)
    expect(w.text()).toContain('Alex Smith')
  })

  it('recovers to the trigger after a failed session read that arrives late', async () => {
    // A transient /me failure must not leave the skeleton up forever: once a later
    // read succeeds (refetch on remount/focus), the menu renders again.
    vi.mocked(api.get).mockRejectedValueOnce({ code: 'network_error' })
    const w = await mountHost(makeRouter())
    expect(w.find('[data-test="account-trigger"]').exists()).toBe(false)

    testQueryClient.setQueryData<SessionView>(keys.me, { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' })
    await flushPromises()
    expect(w.find('[data-test="account-trigger"]').exists()).toBe(true)
  })

  it('self-heals: a failing session read retries in place and the menu returns when it succeeds', async () => {
    // PHB-48: a transient /me failure left the skeleton up forever (retry: false,
    // no error refetch). The shared session query now retries while mounted, so
    // the account menu must re-appear without any user interaction.
    vi.useFakeTimers()
    try {
      vi.mocked(api.get).mockRejectedValue({ code: 'network_error' })
      const w = await mountHost(makeRouter())
      expect(w.find('[data-test="account-trigger"]').exists()).toBe(false)

      vi.mocked(api.get).mockImplementation(async () => ({ id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }))
      await vi.advanceTimersByTimeAsync(4000 + 1000) // error refetch interval + request settling
      await flushPromises()
      expect(w.find('[data-test="account-trigger"]').exists()).toBe(true)
      expect(w.text()).toContain('Alex Smith')
    } finally {
      vi.useRealTimers()
    }
  })

  it('renders displayName, role, and initials in the trigger when loaded', async () => {

    testQueryClient.setQueryData<SessionView>(keys.me, { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' })
    const w = await mountHost(makeRouter())
    expect(w.find('[data-test="account-trigger"]').exists()).toBe(true)
    expect(w.text()).toContain('Alex Smith')
    expect(w.text()).toContain('user')
    expect(w.text()).toContain('AS')
  })

  it('signOut navigates to /logout', async () => {

    testQueryClient.setQueryData<SessionView>(keys.me, { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' })
    const router = makeRouter()
    const push = vi.spyOn(router, 'push')
    const w = await mountHost(router)
    const nav = (w.vm.$refs as Record<string, { signOut: () => void }>).nav
    nav.signOut()
    expect(push).toHaveBeenCalledWith('/logout')
  })

  it('openEdit opens the edit dialog after nextTick', async () => {

    testQueryClient.setQueryData<SessionView>(keys.me, { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' })
    const w = await mountHost(makeRouter())
    const nav = (w.vm.$refs as Record<string, { openEdit: () => void }>).nav
    nav.openEdit()
    await nextTick(); await flushPromises()
    expect(document.body.querySelector('[data-test="edit-displayname-input"]')).not.toBeNull()
  })


  it('topbar variant mounts WITHOUT a SidebarProvider and navigates to settings/admin', async () => {

    testQueryClient.setQueryData<SessionView>(keys.me, { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'admin' })
    const router = makeRouter()
    router.push('/'); await router.isReady()
    const push = vi.spyOn(router, 'push')
    // No SidebarProvider host here — proves the topbar variant is decoupled from
    // the sidebar primitives (the launcher top bar has no sidebar context).
    const w = mount(NavUser, { props: { variant: 'topbar' }, attachTo: document.body, global: { plugins: [router, i18n()] } })
    await flushPromises()
    expect(w.find('[data-test="account-trigger"]').exists()).toBe(true)
    const nav = w.vm as unknown as { goSettings: () => void; goAdmin: () => void }
    nav.goSettings()
    expect(push).toHaveBeenCalledWith('/security')
    nav.goAdmin()
    expect(push).toHaveBeenCalledWith('/admin/accounts')
  })

})
