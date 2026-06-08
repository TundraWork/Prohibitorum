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

import realRouter from './index'

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
