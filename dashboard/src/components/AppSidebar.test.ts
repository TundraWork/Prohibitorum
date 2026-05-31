import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import zh from '../locales/zh'
import en from '../locales/en'
import { useSessionStore } from '../stores/session'
import AppSidebar from './AppSidebar.vue'

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'zh', messages: { zh, en } })
}
function makeRouter() {
  return createRouter({ history: createMemoryHistory(), routes: [
    { path: '/', component: { template: '<div/>' } },
    { path: '/sessions', component: { template: '<div/>' } },
    { path: '/credentials', component: { template: '<div/>' } },
    { path: '/admin/accounts', component: { template: '<div/>' } },
    { path: '/admin/invitations', component: { template: '<div/>' } },
  ] })
}

beforeEach(() => setActivePinia(createPinia()))

describe('AppSidebar', () => {
  it('hides the admin group for non-admins', async () => {
    const s = useSessionStore()
    s.me = { id: 1, username: 'u', displayName: 'U', role: 'user' }
    const wrapper = mount(AppSidebar, { global: { plugins: [makeI18n(), makeRouter()] } })
    expect(wrapper.text()).toContain(en.nav.profile)
    expect(wrapper.text()).not.toContain(en.nav.accounts)
  })

  it('shows the admin group for admins', async () => {
    const s = useSessionStore()
    s.me = { id: 1, username: 'a', displayName: 'A', role: 'admin' }
    const wrapper = mount(AppSidebar, { global: { plugins: [makeI18n(), makeRouter()] } })
    expect(wrapper.text()).toContain(en.nav.accounts)
    expect(wrapper.text()).toContain(en.nav.invitations)
  })
})
