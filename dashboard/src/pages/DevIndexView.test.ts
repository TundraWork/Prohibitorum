import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { useSessionStore } from '../stores/session'
import DevIndexView from './DevIndexView.vue'

const get = vi.fn()
const post = vi.fn()
vi.mock('../lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a), post: (...a: unknown[]) => post(...a) } }))

// jsdom serves on http://localhost, so isDevMode() is true in tests.
function makeRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div/>' } },
      { path: '/dev', component: DevIndexView },
      { path: '/login', component: { template: '<div/>' } },
      { path: '/logout', component: { template: '<div/>' } },
      { path: '/sessions', component: { template: '<div/>' } },
      { path: '/credentials', component: { template: '<div/>' } },
      { path: '/admin/accounts', component: { template: '<div/>' } },
      { path: '/admin/invitations', component: { template: '<div/>' } },
      { path: '/error', component: { template: '<div/>' } },
      { path: '/enroll/:token', component: { template: '<div/>' } },
    ],
  })
}

function mountView() {
  const router = makeRouter()
  return mount(DevIndexView, { global: { plugins: [router] } })
}

beforeEach(() => { setActivePinia(createPinia()); get.mockReset(); post.mockReset() })

describe('DevIndexView', () => {
  it('lists routes to every page and the API endpoints', async () => {
    get.mockResolvedValueOnce(null) // /me → no session
    const wrapper = mountView()
    await flushPromises()
    const hrefs = wrapper.findAll('a')
    const text = wrapper.text()
    expect(text).toContain('Login')
    expect(text).toContain('Sessions')
    expect(text).toContain('Accounts')
    // raw API links present
    expect(wrapper.html()).toContain('/.well-known/openid-configuration')
    expect(wrapper.html()).toContain('/oauth/jwks')
    // not signed in shown
    expect(text).toContain('not signed in')
    // mint-invite action hidden for non-admins
    expect(text).not.toContain('Mint invitation')
    expect(hrefs.length).toBeGreaterThan(0)
  })

  it('lets an admin mint an invitation and surfaces the enroll link', async () => {
    get.mockResolvedValueOnce({ id: 1, username: 'admin', displayName: 'Admin', role: 'admin' })
    const wrapper = mountView()
    await flushPromises()
    const s = useSessionStore()
    expect(s.isAdmin).toBe(true)
    post.mockResolvedValueOnce({ url: 'http://localhost:8080/enroll/tok123', expiresAt: '2026-07-01T00:00:00Z' })
    const mintBtn = wrapper.findAll('button').find((b) => b.text().includes('Mint invitation'))
    expect(mintBtn).toBeTruthy()
    await mintBtn!.trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/prohibitorum/invitations', { role: 'user' })
    // the enroll URL + in-app path are shown
    expect(wrapper.text()).toContain('http://localhost:8080/enroll/tok123')
    expect(wrapper.html()).toContain('/enroll/tok123')
  })
})
