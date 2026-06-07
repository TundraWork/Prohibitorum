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
    routes: [{ path: '/', component: stub }, { path: '/security', component: stub }, { path: '/sessions', component: stub }, { path: '/connected', component: stub }, { path: '/devices', component: stub }, { path: '/logout', component: stub }],
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
  it('renders the built Account links and a footer sign-out', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    const router = makeRouter(); router.push('/'); await router.isReady()
    const wrapper = mount(Host, { global: { plugins: [router, makeI18n()], components: { AppSidebar } } })
    const links = wrapper.findAll('a').map((a) => a.attributes('href'))
    expect(links).toContain('/')
    expect(links).toContain('/sessions')
    expect(links).toContain('/logout')
    expect(links).toContain('/connected')
    expect(links).toContain('/devices')
    expect(wrapper.text()).toContain('Alex Smith')
  })

  it('marks only the current route link as active', async () => {
    const auth = useAuthStore()
    auth.me = { id: 1, username: 'alex', displayName: 'Alex Smith', role: 'user' }
    const router = makeRouter(); router.push('/'); await router.isReady()
    const wrapper = mount(Host, { global: { plugins: [router, makeI18n()], components: { AppSidebar } } })
    // Exactly one element should carry data-active="true" — the Profile nav item
    const activeEls = wrapper.findAll('[data-active="true"]')
    expect(activeEls.length).toBe(1)
  })
})
