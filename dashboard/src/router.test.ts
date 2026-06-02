import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { installGuard } from './router'
import { createRouter, createMemoryHistory } from 'vue-router'
import { useSessionStore } from './stores/session'

const get = vi.fn()
vi.mock('./lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a) } }))

const devMode = vi.fn(() => true)
vi.mock('./lib/devMode', () => ({ isDevMode: () => devMode() }))

function buildRouter() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div/>' }, meta: { requiresAuth: true } },
      { path: '/admin/accounts', component: { template: '<div/>' }, meta: { requiresAuth: true, requiresAdmin: true } },
      { path: '/security', component: { template: '<div/>' }, meta: { requiresAuth: true } },
      { path: '/admin/oidc-clients', component: { template: '<div/>' }, meta: { requiresAuth: true, requiresAdmin: true } },
      { path: '/login', component: { template: '<div/>' } },
      { path: '/enroll/:token', component: { template: '<div/>' } },
      { path: '/dev', component: { template: '<div/>' } },
    ],
  })
  installGuard(router)
  return router
}

beforeEach(() => { setActivePinia(createPinia()); get.mockReset(); devMode.mockReturnValue(true) })

describe('router guard', () => {
  it('redirects unauthenticated users to /login with return_to', async () => {
    get.mockRejectedValue({ code: 'no_session', message: 'x' })
    const router = buildRouter()
    await router.push('/')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.return_to).toBe('/')
  })

  it('redirects non-admins away from admin routes', async () => {
    get.mockResolvedValue({ id: 1, username: 'u', displayName: 'U', role: 'user' })
    const router = buildRouter()
    await router.push('/admin/accounts')
    expect(router.currentRoute.value.path).toBe('/')
  })

  it('lets admins into admin routes', async () => {
    get.mockResolvedValue({ id: 1, username: 'a', displayName: 'A', role: 'admin' })
    const router = buildRouter()
    await router.push('/admin/accounts')
    expect(router.currentRoute.value.path).toBe('/admin/accounts')
  })

  it('allows public routes without a session', async () => {
    get.mockRejectedValue({ code: 'no_session', message: 'x' })
    const router = buildRouter()
    await router.push('/enroll/tok')
    expect(router.currentRoute.value.path).toBe('/enroll/tok')
  })

  it('allows /dev in dev mode', async () => {
    devMode.mockReturnValue(true)
    const router = buildRouter()
    await router.push('/dev')
    expect(router.currentRoute.value.path).toBe('/dev')
  })

  it('redirects /dev to / outside dev mode', async () => {
    devMode.mockReturnValue(false)
    get.mockResolvedValue({ id: 1, username: 'a', displayName: 'A', role: 'admin' }) // so the / target (requiresAuth) resolves
    const router = buildRouter()
    await router.push('/dev')
    expect(router.currentRoute.value.path).toBe('/')
  })

  it('bounces non-admin from a planned admin route', async () => {
    get.mockResolvedValue({ id: 1, username: 'u', displayName: 'U', role: 'user' })
    const router = buildRouter()
    await router.push('/admin/oidc-clients')
    expect(router.currentRoute.value.path).toBe('/')
  })
})
