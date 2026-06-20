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

  it('no-ops on a public route', () => {
    const clearAuth = vi.fn(); const setExpiredFlag = vi.fn()
    const r = fakeRouter('login')
    createUnauthorizedHandler({ router: r, clearAuth, setExpiredFlag })({ method: 'GET' })
    expect(clearAuth).not.toHaveBeenCalled()
    expect((r as never as { replace: ReturnType<typeof vi.fn> }).replace).not.toHaveBeenCalled()
  })

  it('redirects an authenticated GET to /login with return_to + reason', () => {
    const clearAuth = vi.fn(); const setExpiredFlag = vi.fn()
    const r = fakeRouter('sessions', {}, '/sessions')
    createUnauthorizedHandler({ router: r, clearAuth, setExpiredFlag })({ method: 'GET' })
    expect(clearAuth).toHaveBeenCalled()
    const replace = (r as never as { replace: ReturnType<typeof vi.fn> }).replace
    expect(replace).toHaveBeenCalledWith({ name: 'login', query: { return_to: '/sessions', reason: 'session_expired' } })
  })

  it('flags (no navigation) on an authenticated mutation', () => {
    const clearAuth = vi.fn(); const setExpiredFlag = vi.fn()
    const r = fakeRouter('security', {}, '/security')
    createUnauthorizedHandler({ router: r, clearAuth, setExpiredFlag })({ method: 'POST' })
    expect(setExpiredFlag).toHaveBeenCalled()
    expect((r as never as { replace: ReturnType<typeof vi.fn> }).replace).not.toHaveBeenCalled()
  })

  it('is idempotent for concurrent GET triggers', () => {
    const clearAuth = vi.fn(); const setExpiredFlag = vi.fn()
    const r = fakeRouter('sessions')
    const h = createUnauthorizedHandler({ router: r, clearAuth, setExpiredFlag })
    h({ method: 'GET' }); h({ method: 'GET' })
    expect((r as never as { replace: ReturnType<typeof vi.fn> }).replace).toHaveBeenCalledTimes(1)
  })
})
