import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import en from '@/locales/en'
import AppSidebar from './AppSidebar.vue'
import { SidebarProvider } from '@/components/ui/sidebar'
import { useAuthStore } from '@/stores/auth'

if (!window.matchMedia) {
  // @ts-expect-error jsdom lacks matchMedia
  window.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })
}

const stub = defineComponent({ template: '<div/>' })
function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: stub }, { path: '/security', component: stub }, { path: '/sessions', component: stub }, { path: '/connected', component: stub }, { path: '/devices', component: stub }, { path: '/logout', component: stub }, { path: '/admin/accounts', component: stub }, { path: '/admin/invitations', component: stub }, { path: '/admin/groups', component: stub }, { path: '/admin/oidc-applications', component: stub }, { path: '/admin/saml-applications', component: stub }, { path: '/admin/identity-providers', component: stub }, { path: '/admin/signing-keys', component: stub }, { path: '/admin/audit', component: stub }],
  })
}
function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}
// Host wraps AppSidebar in the provider the primitive requires.
const Host = defineComponent({ components: { SidebarProvider, AppSidebar },
  template: '<SidebarProvider><AppSidebar /></SidebarProvider>' })

beforeEach(() => setActivePinia(createPinia()))

describe('AppSidebar', () => {
  it('renders the built Account links and the account control (no Profile, no footer sign-out link)', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    const router = makeRouter(); router.push('/security'); await router.isReady()
    const wrapper = mount(Host, { global: { plugins: [router, makeI18n()], components: { AppSidebar } } })
    const links = wrapper.findAll('a').map((a) => a.attributes('href'))
    expect(links).toContain('/security')
    expect(links).toContain('/sessions')
    expect(links).toContain('/connected')
    expect(links).toContain('/devices')
    expect(links).not.toContain('/')        // Profile link removed
    expect(links).not.toContain('/logout')  // sign-out is now inside the account menu
    expect(wrapper.text()).toContain('Alex Smith') // NavUser trigger shows displayName
  })

  it('marks only the current route link as active', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    const router = makeRouter(); router.push('/security'); await router.isReady()
    const wrapper = mount(Host, { global: { plugins: [router, makeI18n()], components: { AppSidebar } } })
    // Exactly one element should carry data-active="true" - the Security nav item
    const activeEls = wrapper.findAll('[data-active="true"]')
    expect(activeEls.length).toBe(1)
  })

  it('renders the admin group only for admins', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'admin' }
    const router = makeRouter(); router.push('/'); await router.isReady()
    const wrapper = mount(Host, { global: { plugins: [router, makeI18n()], components: { AppSidebar } } })
    const links = wrapper.findAll('a').map((a) => a.attributes('href'))
    expect(links).toContain('/admin/accounts')
    expect(links).toContain('/admin/invitations')
    expect(links).toContain('/admin/groups')
    expect(links).toContain('/admin/oidc-applications')
    expect(links).toContain('/admin/saml-applications')
    expect(links).toContain('/admin/identity-providers')
  })

  it('hides the admin group for non-admins', async () => {
    const auth = useAuthStore()
    auth.me = { id: 2, username: 'bob', displayName: 'Bob Lee', role: 'user' }
    const router = makeRouter(); router.push('/'); await router.isReady()
    const wrapper = mount(Host, { global: { plugins: [router, makeI18n()], components: { AppSidebar } } })
    const links = wrapper.findAll('a').map((a) => a.attributes('href'))
    expect(links).not.toContain('/admin/accounts')
  })

  it('renders the language switcher and theme toggle as standalone footer controls', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    const router = makeRouter(); router.push('/security'); await router.isReady()
    const wrapper = mount(Host, { global: { plugins: [router, makeI18n()], components: { AppSidebar } } })
    // LocaleSwitcher renders the vendored Select trigger; ThemeToggle a role="radiogroup".
    expect(wrapper.find('[data-test="locale-trigger"]').exists()).toBe(true)
    expect(wrapper.find('[role="radiogroup"]').exists()).toBe(true)
  })
})
