import { describe, it, expect, beforeEach, vi } from 'vitest'
import { createRouter, createMemoryHistory } from 'vue-router'
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
      { path: '/applications', name: 'applications', component: stub, meta: { requiresAuth: true } },
    ],
  })
  installGuard(r)
  return r
}
beforeEach(() => { get.mockReset() })

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

describe('router guard (requiresAuth)', () => {
  it('allows a regular user into authenticated application routes', async () => {
    get.mockResolvedValue({ id: 1, username: 'u', displayName: 'User', role: 'user' })
    const r = makeRouter()
    await r.push('/applications'); await r.isReady()
    expect(r.currentRoute.value.name).toBe('applications')
  })

  it('redirects an anonymous visitor to login', async () => {
    get.mockRejectedValue({ code: 'no_session' })
    const r = makeRouter()
    await r.push('/applications'); await r.isReady()
    expect(r.currentRoute.value.name).toBe('login')
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
        : Promise.reject({ code: 'no_session' })
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

describe('global group routes', () => {
  it.each([
    ['/admin/groups', 'admin-groups'],
    ['/admin/groups/10', 'admin-group-detail'],
  ] as const)('%s resolves to the admin-only global group route', (path, groupRouteName) => {
    const resolved = realRouter.resolve(path)
    expect(resolved.name).toBe(groupRouteName)
    expect(resolved.meta.requiresAdmin).toBe(true)
  })
})

describe('3c admin routes require admin', () => {
  it.each([
    '/admin/identity-providers',
    '/admin/signing-keys',
    '/admin/audit',
  ])('%s is marked requiresAdmin', (path) => {
    const resolved = realRouter.resolve(path)
    expect(resolved.meta.requiresAdmin).toBe(true)
  })
})

describe('application management routes', () => {
  it.each([
    ['/admin/oidc-applications', 'admin-oidc-applications'],
    ['/admin/oidc-applications/client-id', 'admin-oidc-application-detail'],
    ['/admin/forward-auth-apps', 'admin-forward-auth-apps'],
    ['/admin/forward-auth-apps/client-id', 'admin-forward-auth-app-detail'],
    ['/admin/saml-applications', 'admin-saml-applications'],
    ['/admin/saml-applications/7', 'admin-saml-application-detail'],
  ])('%s is available to authenticated accounts', (path, name) => {
    const resolved = realRouter.resolve(path)
    expect(resolved.name).toBe(name)
    expect(resolved.meta.requiresAuth).toBe(true)
    expect(resolved.meta.requiresAdmin).not.toBe(true)
  })

  it.each(['/manage/applications', '/manage/applications/oidc/client-id'])('%s uses the catch-all redirect', (path) => {
    const resolved = realRouter.resolve(path)
    expect(resolved.redirectedFrom).toBeUndefined()
    expect(resolved.matched.at(-1)?.redirect).toBe('/error')
  })
})

describe('VRChat verification routes', () => {
  it.each([
    ['/federation/flow/flow_abc', 'federation-flow'],
    ['/verify/vrchat/proof_abc', 'vrchat-proof'],
  ])('%s is public and bypasses the auth guard', (path, name) => {
    const resolved = realRouter.resolve(path)
    expect(resolved.name).toBe(name)
    expect(resolved.meta.public).toBe(true)
    expect(resolved.meta.requiresAuth).not.toBe(true)
    expect(resolved.meta.requiresAdmin).not.toBe(true)
  })
})
