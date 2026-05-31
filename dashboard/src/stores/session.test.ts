import { describe, it, expect, vi, beforeEach } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useSessionStore } from './session'

const get = vi.fn()
vi.mock('../lib/api', () => ({ api: { get: (...a: unknown[]) => get(...a) } }))

beforeEach(() => {
  setActivePinia(createPinia())
  get.mockReset()
})

describe('session store', () => {
  it('ensureLoaded fetches once and caches', async () => {
    get.mockResolvedValue({ id: 1, username: 'a', displayName: 'A', role: 'user' })
    const s = useSessionStore()
    await s.ensureLoaded()
    await s.ensureLoaded()
    expect(get).toHaveBeenCalledTimes(1)
    expect(s.me?.username).toBe('a')
  })

  it('treats a rejected fetch as no session', async () => {
    get.mockRejectedValue({ code: 'no_session', message: 'x' })
    const s = useSessionStore()
    await s.ensureLoaded()
    expect(s.me).toBeNull()
  })

  it('isAdmin reflects the role', async () => {
    get.mockResolvedValue({ id: 1, username: 'a', displayName: 'A', role: 'admin' })
    const s = useSessionStore()
    await s.ensureLoaded()
    expect(s.isAdmin).toBe(true)
  })

  it('clear resets state and allows a refetch', async () => {
    get.mockResolvedValue({ id: 1, username: 'a', displayName: 'A', role: 'user' })
    const s = useSessionStore()
    await s.ensureLoaded()
    s.clear()
    expect(s.me).toBeNull()
    await s.ensureLoaded()
    expect(get).toHaveBeenCalledTimes(2)
  })
})
