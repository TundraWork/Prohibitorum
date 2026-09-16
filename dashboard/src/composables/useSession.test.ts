import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { useSession } from './useSession'
import { testQueryClient } from '@/testSetup'
import { keys } from '@/queries/resources'
import { clearSessionQueries } from '@/queries/client'
import { api } from '@/lib/api'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))
const Host = defineComponent({ setup: () => ({ session: useSession() }), template: '<div>{{ session.me?.displayName }}|{{ session.avatarBusy }}|{{ !!session.error }}</div>' })
const user = { id: 1, username: 'alex', displayName: 'Alex', role: 'user' }
const get = vi.mocked(api.get)
beforeEach(() => { get.mockReset(); vi.useFakeTimers() })
afterEach(() => vi.useRealTimers())
describe('shared session', () => {
  it('deduplicates mounted consumers and clears their rendered private state', async () => {
    get.mockResolvedValue(user)
    const first = mount(Host); const second = mount(Host)
    await flushPromises()
    expect(get).toHaveBeenCalledTimes(1)
    expect(first.text()).toContain('Alex')
    await clearSessionQueries(testQueryClient)
    await flushPromises()
    expect(first.text()).not.toContain('Alex')
    expect(second.text()).not.toContain('Alex')
    expect(testQueryClient.getQueryData(keys.me)).toBeUndefined()
  })
  it('polls once for multiple consumers, refreshes the settled avatar and supports a new cycle', async () => {
    let pending = true
    let polls = 0
    get.mockImplementation(async (url) => {
      if (url.endsWith('/avatar/status')) { polls++; pending = polls === 1; return { pending } }
      return { ...user, avatarPending: pending }
    })
    mount(Host); mount(Host)
    await flushPromises()
    expect(polls).toBe(1)
    await vi.advanceTimersByTimeAsync(1500); await flushPromises()
    expect(polls).toBe(2)
    const reads = get.mock.calls.filter(([url]) => url.endsWith('/me')).length
    expect(reads).toBe(2) // initial session + one shared completion refresh
    await vi.advanceTimersByTimeAsync(3000)
    expect(polls).toBe(2)
    pending = true
    testQueryClient.setQueryData(keys.me, { ...user, avatarPending: true })
    await flushPromises()
    expect(polls).toBe(3)
  })
  it('stops polling on error without pretending the server finished, and stops on unmount', async () => {
    get.mockImplementation(async (url) => {
      if (url.endsWith('/avatar/status')) throw { code: 'network_error' }
      return { ...user, avatarPending: true }
    })
    const host = mount(Host); await flushPromises()
    expect(host.text()).toBe('Alex|false|true')
    expect(testQueryClient.getQueryData(keys.me)).toMatchObject({ avatarPending: true })
    const count = get.mock.calls.length
    await vi.advanceTimersByTimeAsync(4500)
    expect(get).toHaveBeenCalledTimes(count)
    host.unmount()
    await vi.advanceTimersByTimeAsync(3000)
    expect(get).toHaveBeenCalledTimes(count)
  })
  it('cancels a pending avatar request when its last consumer leaves', async () => {
    let signal: AbortSignal | undefined
    get.mockImplementation(async (url, options) => {
      if (url.endsWith('/avatar/status')) { signal = options?.signal; return new Promise(() => {}) }
      return { ...user, avatarPending: true }
    })
    const host = mount(Host); await flushPromises()
    expect(signal?.aborted).toBe(false)
    host.unmount()
    expect(signal?.aborted).toBe(true)
  })
})
