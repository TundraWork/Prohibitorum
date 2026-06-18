import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory, type Router } from 'vue-router'
import en from '@/locales/en'
import NavUser from './NavUser.vue'
import { SidebarProvider } from '@/components/ui/sidebar'
import { useAuthStore } from '@/stores/auth'
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
    routes: [{ path: '/', component: stub }, { path: '/security', component: stub }, { path: '/logout', component: stub }],
  })
}
const Host = defineComponent({
  components: { SidebarProvider, NavUser },
  template: '<SidebarProvider><NavUser ref="nav" /></SidebarProvider>',
})

beforeEach(() => { setActivePinia(createPinia()); document.body.innerHTML = ''; vi.clearAllMocks() })

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

  it('renders displayName, role, and initials in the trigger when loaded', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    const w = await mountHost(makeRouter())
    expect(w.find('[data-test="account-trigger"]').exists()).toBe(true)
    expect(w.text()).toContain('Alex Smith')
    expect(w.text()).toContain('user')
    expect(w.text()).toContain('AS')
  })

  it('signOut navigates to /logout', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    const router = makeRouter()
    const push = vi.spyOn(router, 'push')
    const w = await mountHost(router)
    const nav = (w.vm.$refs as Record<string, { signOut: () => void }>).nav
    nav.signOut()
    expect(push).toHaveBeenCalledWith('/logout')
  })

  it('openEdit opens the edit dialog after nextTick', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    const w = await mountHost(makeRouter())
    const nav = (w.vm.$refs as Record<string, { openEdit: () => void }>).nav
    nav.openEdit()
    await nextTick(); await flushPromises()
    expect(document.body.querySelector('[data-test="edit-displayname-input"]')).not.toBeNull()
  })

  it('renders ThemeToggle inside the account dropdown when the menu is open', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    const w = await mountHost(makeRouter())

    // Open the Reka dropdown via keyboard Enter on the trigger (works in jsdom;
    // click/pointerdown triggers a Teleport insertBefore error in this environment).
    const trigger = w.get('[data-test="account-trigger"]')
    await trigger.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    await nextTick()

    // Reka teleports the DropdownMenuContent to document.body; the ThemeToggle
    // renders a role="radiogroup" — assert it is present in the teleported DOM.
    expect(document.body.querySelector('[role="radiogroup"]')).not.toBeNull()
  })

  describe('pollAvatarUntilSettled', () => {
    beforeEach(() => { vi.useFakeTimers() })
    afterEach(() => { vi.useRealTimers() })

    it('polls /me/avatar/status and reloads /me when pending clears', async () => {
      const auth = useAuthStore()
      // Seed with avatarPending: true
      auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user', avatarPending: true }

      const apiGet = vi.mocked(api.get)
      // First call: status endpoint → settled immediately; second call: reload /me
      apiGet
        .mockResolvedValueOnce({ pending: false })                                      // status → settled
        .mockResolvedValueOnce({ id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }) // reload /me

      await mountHost(makeRouter())

      // Advance past the initial 1500ms delay
      await vi.advanceTimersByTimeAsync(1600)
      await flushPromises()

      expect(apiGet).toHaveBeenCalledWith('/api/prohibitorum/me/avatar/status')
      // reload() calls ensureLoaded() which calls /me
      expect(apiGet).toHaveBeenCalledWith('/api/prohibitorum/me')
    })

    it('keeps polling when status returns pending:true then settles', async () => {
      const auth = useAuthStore()
      auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user', avatarPending: true }

      const apiGet = vi.mocked(api.get)
      apiGet
        .mockResolvedValueOnce({ pending: true })                                       // first poll → still pending
        .mockResolvedValueOnce({ pending: false })                                      // second poll → settled
        .mockResolvedValueOnce({ id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }) // reload /me

      await mountHost(makeRouter())

      // First tick (1500ms) → pending:true, schedules another tick
      await vi.advanceTimersByTimeAsync(1600)
      await flushPromises()

      // Second tick (another 1500ms) → pending:false, reload fires
      await vi.advanceTimersByTimeAsync(1600)
      await flushPromises()

      const statusCalls = apiGet.mock.calls.filter(c => c[0] === '/api/prohibitorum/me/avatar/status')
      expect(statusCalls.length).toBe(2)
      expect(apiGet).toHaveBeenCalledWith('/api/prohibitorum/me')
    })

    it('does not poll when avatarPending is absent', async () => {
      const auth = useAuthStore()
      auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }

      const apiGet = vi.mocked(api.get)

      await mountHost(makeRouter())
      await vi.advanceTimersByTimeAsync(1600)
      await flushPromises()

      const statusCalls = apiGet.mock.calls.filter(c => c[0] === '/api/prohibitorum/me/avatar/status')
      expect(statusCalls.length).toBe(0)
    })

    it('error path clears avatarPending and does not schedule further calls', async () => {
      const auth = useAuthStore()
      auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user', avatarPending: true }

      const apiGet = vi.mocked(api.get)
      apiGet.mockRejectedValueOnce(new Error('network error'))

      await mountHost(makeRouter())

      // Advance past the initial 1500ms delay — the status call rejects
      await vi.advanceTimersByTimeAsync(1600)
      await flushPromises()

      // The spinner flag should be cleared
      expect(auth.me?.avatarPending).toBe(false)

      // Verify no further status calls are scheduled: advance another full interval
      const callsBefore = apiGet.mock.calls.filter(c => c[0] === '/api/prohibitorum/me/avatar/status').length
      await vi.advanceTimersByTimeAsync(3000)
      await flushPromises()
      const callsAfter = apiGet.mock.calls.filter(c => c[0] === '/api/prohibitorum/me/avatar/status').length
      expect(callsAfter).toBe(callsBefore)
    })

    it('unmount cancels the pending poll — status endpoint is never called', async () => {
      const auth = useAuthStore()
      auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user', avatarPending: true }

      const apiGet = vi.mocked(api.get)

      const w = await mountHost(makeRouter())

      // Unmount BEFORE the 1500ms timer fires
      w.unmount()

      // Advance past the timer and flush — the cancelled tick should be a no-op
      await vi.advanceTimersByTimeAsync(1600)
      await flushPromises()

      const statusCalls = apiGet.mock.calls.filter(c => c[0] === '/api/prohibitorum/me/avatar/status')
      expect(statusCalls.length).toBe(0)
    })
  })
})
