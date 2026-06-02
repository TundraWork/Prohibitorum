import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { useSessionStore } from '../stores/session'
import AppSidebar from './AppSidebar.vue'

function makeRouter() {
  return createRouter({ history: createMemoryHistory(), routes: [
    { path: '/', component: { template: '<div/>' } },
    { path: '/security', component: { template: '<div/>' } },
    { path: '/sessions', component: { template: '<div/>' } },
    { path: '/connected', component: { template: '<div/>' } },
    { path: '/devices', component: { template: '<div/>' } },
    { path: '/admin/accounts', component: { template: '<div/>' } },
    { path: '/admin/invitations', component: { template: '<div/>' } },
    { path: '/admin/oidc-clients', component: { template: '<div/>' } },
    { path: '/admin/saml-providers', component: { template: '<div/>' } },
    { path: '/admin/signing-keys', component: { template: '<div/>' } },
    { path: '/admin/audit', component: { template: '<div/>' } },
    { path: '/admin/settings', component: { template: '<div/>' } },
  ] })
}

beforeEach(() => setActivePinia(createPinia()))

describe('AppSidebar', () => {
  it('hides the admin group for non-admins', async () => {
    const s = useSessionStore()
    s.me = { id: 1, username: 'u', displayName: 'U', role: 'user' }
    const wrapper = mount(AppSidebar, { global: { plugins: [makeRouter()] } })
    expect(wrapper.text()).toContain('Profile')
    expect(wrapper.text()).not.toContain('Accounts')
  })

  it('shows the admin group for admins', async () => {
    const s = useSessionStore()
    s.me = { id: 1, username: 'a', displayName: 'A', role: 'admin' }
    const wrapper = mount(AppSidebar, { global: { plugins: [makeRouter()] } })
    expect(wrapper.text()).toContain('Accounts')
    expect(wrapper.text()).toContain('Invitations')
  })
})
