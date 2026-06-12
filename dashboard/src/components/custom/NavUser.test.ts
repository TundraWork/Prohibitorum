import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory, type Router } from 'vue-router'
import en from '@/locales/en'
import NavUser from './NavUser.vue'
import { SidebarProvider } from '@/components/ui/sidebar'
import { useAuthStore } from '@/stores/auth'

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

beforeEach(() => { setActivePinia(createPinia()); document.body.innerHTML = '' })

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
})
