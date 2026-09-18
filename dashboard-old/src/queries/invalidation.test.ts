import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { useResource } from '@/composables/useResource'
import { memberQuery, keys } from './resources'
import { invalidateResource, removeDetail } from './invalidation'
import { testQueryClient } from '@/testSetup'
import { api } from '@/lib/api'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn() } }))
describe('resource dependencies', () => {
  it('updates all mounted app consumers once and leaves unrelated data fresh', async () => {
    vi.mocked(api.get).mockResolvedValue(['Before'])
    const Host = defineComponent({ setup: () => ({ result: useResource(memberQuery<string[]>('apps')) }), template: '<div>{{ result.data.value }}</div>' })
    const first = mount(Host); const second = mount(Host); await flushPromises()
    testQueryClient.setQueryData(keys.collection('audit-events'), { items: [] })
    vi.mocked(api.get).mockResolvedValue(['After'])
    await invalidateResource(testQueryClient, 'oidc-applications'); await flushPromises()
    expect(first.text()).toContain('After'); expect(second.text()).toContain('After')
    expect(api.get).toHaveBeenCalledTimes(2)
    expect(testQueryClient.getQueryState(keys.collection('audit-events'))?.isInvalidated).toBe(false)
  })
  it('cancels and removes deleted details and nested sections without retaining a late response', async () => {
    const queryKey = [...keys.detail('accounts', 7), 'sessions']
    let finish!: (value: string[]) => void
    let signal!: AbortSignal
    const pending = testQueryClient.fetchQuery({ queryKey, queryFn: context => { signal = context.signal; return new Promise<string[]>(resolve => { finish = resolve }) } }).catch(() => null)
    testQueryClient.setQueryData(keys.detail('accounts', 8), { id: 8 })
    await removeDetail(testQueryClient, 'accounts', 7)
    expect(signal.aborted).toBe(true)
    finish(['old']); await pending
    expect(testQueryClient.getQueryData(queryKey)).toBeUndefined()
    expect(testQueryClient.getQueryData(keys.detail('accounts', 8))).toEqual({ id: 8 })
  })
})
