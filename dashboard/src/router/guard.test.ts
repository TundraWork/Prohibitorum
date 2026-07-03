import { describe, it, expect, beforeEach, vi } from 'vitest'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent } from 'vue'
import { installGuard } from './index'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)

const stub = defineComponent({ template: '<div/>' })
function makeRouter() {
  const r = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', name: 'login', component: stub, meta: { public: true } },
      { path: '/error', name: 'error', component: stub, meta: { public: true } },
      { path: '/admin', name: 'test-admin', component: stub, meta: { requiresAuth: true, requiresAdmin: true } },
    ],
  })
  installGuard(r)
  return r
}
beforeEach(() => { setActivePinia(createPinia()); get.mockReset() })

describe('router guard (requiresAdmin)', () => {
  it('redirects a non-admin to error?error=forbidden', async () => {
    get.mockResolvedValue({ id: 1, username: 'u', displayName: 'U', role: 'user' })
    const r = makeRouter()
    await r.push('/admin'); await r.isReady()
    expect(r.currentRoute.value.name).toBe('error')
    expect(r.currentRoute.value.query.error).toBe('forbidden')
  })
  it('lets an admin through', async () => {
    get.mockResolvedValue({ id: 1, username: 'a', displayName: 'A', role: 'admin' })
    const r = makeRouter()
    await r.push('/admin'); await r.isReady()
    expect(r.currentRoute.value.name).toBe('test-admin')
  })
})

function makeMaintRouter() {
  const r = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', name: 'login', component: stub, meta: { public: true } },
      { path: '/maintenance', name: 'maintenance', component: stub, meta: { public: true } },
      { path: '/logout', name: 'logout', component: stub, meta: { public: true } },
      { path: '/app', name: 'test-app', component: stub, meta: { requiresAuth: true } },
    ],
  })
  installGuard(r)
  return r
}

// Route api.get by path: config carries maintenanceMode; me is the session (or 401).
function mockApi(maintenance: boolean, me: { role: string } | null) {
  get.mockImplementation(((path: string) => {
    if (path === '/api/prohibitorum/config') {
      return Promise.resolve({ maintenanceMode: maintenance, maintenanceMessage: '' })
    }
    if (path === '/api/prohibitorum/me') {
      return me
        ? Promise.resolve({ id: 1, username: 'u', displayName: 'U', role: me.role })
        : Promise.reject({ code: 'unauthorized' })
    }
    return Promise.resolve({})
  }) as any)
}

describe('router guard (maintenance mode)', () => {
  it('redirects an unauthenticated visitor from /login to /maintenance', async () => {
    mockApi(true, null)
    const r = makeMaintRouter()
    await r.push('/login'); await r.isReady()
    expect(r.currentRoute.value.name).toBe('maintenance')
  })

  it('allows /login?admin=1 through during maintenance', async () => {
    mockApi(true, null)
    const r = makeMaintRouter()
    await r.push('/login?admin=1'); await r.isReady()
    expect(r.currentRoute.value.name).toBe('login')
    expect(r.currentRoute.value.query.admin).toBe('1')
  })

  it('allows /login?admin (bare, no value) through during maintenance', async () => {
    mockApi(true, null)
    const r = makeMaintRouter()
    await r.push('/login?admin'); await r.isReady()
    expect(r.currentRoute.value.name).toBe('login')
  })

  it('redirects an authenticated non-admin from an app route to /maintenance', async () => {
    mockApi(true, { role: 'user' })
    const r = makeMaintRouter()
    await r.push('/app'); await r.isReady()
    expect(r.currentRoute.value.name).toBe('maintenance')
  })

  it('lets an admin through during maintenance', async () => {
    mockApi(true, { role: 'admin' })
    const r = makeMaintRouter()
    await r.push('/app'); await r.isReady()
    expect(r.currentRoute.value.name).toBe('test-app')
  })

  it('redirects off /maintenance to /login when maintenance is off', async () => {
    mockApi(false, null)
    const r = makeMaintRouter()
    await r.push('/maintenance'); await r.isReady()
    expect(r.currentRoute.value.name).toBe('login')
  })
})

import realRouter from './index'

describe('3c admin routes require admin', () => {
  it.each([
    '/admin/identity-providers',
    '/admin/signing-keys',
    '/admin/audit',
    '/admin/forward-auth-apps',
    '/admin/forward-auth-apps/some-client',
  ])('%s is marked requiresAdmin', (path) => {
    const resolved = realRouter.resolve(path)
    expect(resolved.meta.requiresAdmin).toBe(true)
  })
})
