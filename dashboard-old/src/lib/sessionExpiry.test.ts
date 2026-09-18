import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createUnauthorizedHandler, __resetHandlingForTest } from './sessionExpiry'

function fakeRouter(name: string, meta: Record<string, unknown> = {}, fullPath = '/sessions') {
  return {
    currentRoute: { value: { name, meta, fullPath } },
    replace: vi.fn().mockResolvedValue(undefined),
  } as never
}

describe('createUnauthorizedHandler', () => {
  beforeEach(() => __resetHandlingForTest())

  it('clears private state without navigation on a public route', async () => {
    const clearAuth = vi.fn(); const setExpiredFlag = vi.fn()
    const r = fakeRouter('login')
    await createUnauthorizedHandler({ router: r, clearAuth, setExpiredFlag })({ method: 'GET' })
    expect(clearAuth).toHaveBeenCalledTimes(1)
    expect((r as never as { replace: ReturnType<typeof vi.fn> }).replace).not.toHaveBeenCalled()
  })

  it('redirects an authenticated GET to /login with return_to + reason', async () => {
    const clearAuth = vi.fn(); const setExpiredFlag = vi.fn()
    const r = fakeRouter('sessions', {}, '/sessions')
    await createUnauthorizedHandler({ router: r, clearAuth, setExpiredFlag })({ method: 'GET' })
    expect(clearAuth).toHaveBeenCalled()
    const replace = (r as never as { replace: ReturnType<typeof vi.fn> }).replace
    expect(replace).toHaveBeenCalledWith({ name: 'login', query: { return_to: '/sessions', reason: 'session_expired' } })
  })

  it('flags (no navigation) on an authenticated mutation', async () => {
    const clearAuth = vi.fn(); const setExpiredFlag = vi.fn()
    const r = fakeRouter('security', {}, '/security')
    await createUnauthorizedHandler({ router: r, clearAuth, setExpiredFlag })({ method: 'POST' })
    expect(setExpiredFlag).toHaveBeenCalled()
    expect((r as never as { replace: ReturnType<typeof vi.fn> }).replace).not.toHaveBeenCalled()
  })

  it('is idempotent for concurrent GET triggers', async () => {
    const clearAuth = vi.fn(); const setExpiredFlag = vi.fn()
    const r = fakeRouter('sessions')
    const h = createUnauthorizedHandler({ router: r, clearAuth, setExpiredFlag })
    await Promise.all([h({ method: 'GET' }), h({ method: 'GET' })])
    expect((r as never as { replace: ReturnType<typeof vi.fn> }).replace).toHaveBeenCalledTimes(1)
  })
})
