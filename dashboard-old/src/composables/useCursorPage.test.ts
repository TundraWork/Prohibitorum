import { describe, it, expect, vi, beforeEach } from 'vitest'
import { defineComponent, ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { useCursorPage } from './useCursorPage'
vi.mock('@/lib/api', () => ({ api: { get: vi.fn() }, isRequestCancelled: (e: unknown) => e instanceof DOMException && e.name === 'AbortError' }))
import { api } from '@/lib/api'
const get = vi.mocked(api.get)
function host() {
  return mount(defineComponent({ setup() {
    const filters = ref({ q: 'first' })
    return { filters, page: useCursorPage<number>('accounts', filters) }
  }, template: '<div />' }))
}
beforeEach(() => get.mockReset())
describe('cursor queries', () => {
  it('reads each cursor, reuses previous page, reloads and steps back from an empty page', async () => {
    get.mockResolvedValueOnce({ items: [1], nextCursor: 'next' }).mockResolvedValueOnce({ items: [2], nextCursor: '' })
    const w = host(); await flushPromises()
    expect(w.vm.page.items.value).toEqual([1])
    await w.vm.page.next(); await flushPromises(); expect(w.vm.page.items.value).toEqual([2])
    expect(get.mock.calls[1][0]).toContain('cursor=next')
    await w.vm.page.previous(); await flushPromises(); expect(w.vm.page.items.value).toEqual([1]); expect(get).toHaveBeenCalledTimes(2)
    await w.vm.page.next(); await flushPromises()
    get.mockResolvedValueOnce({ items: [], nextCursor: '' })
    await w.vm.page.reload(); await flushPromises()
    expect(w.vm.page.pageIndex.value).toBe(0); expect(w.vm.page.items.value).toEqual([1]); w.unmount()
  })
  it('resets cursor atomically with filters and cancels obsolete requests', async () => {
    let finish!: (value: unknown) => void
    get.mockResolvedValueOnce({ items: [1], nextCursor: 'old-cursor' }).mockImplementationOnce(() => new Promise(resolve => { finish = resolve })).mockResolvedValueOnce({ items: [9], nextCursor: '' })
    const w = host(); await flushPromises(); await w.vm.page.next(); await flushPromises()
    const oldSignal = get.mock.calls[1][1]?.signal
    w.vm.filters = { q: 'new' }; await flushPromises()
    expect(get.mock.calls[2][0]).toBe('/api/prohibitorum/accounts?q=new')
    expect(oldSignal?.aborted).toBe(true)
    finish({ items: [2], nextCursor: 'stale' }); await flushPromises()
    expect(w.vm.page.items.value).toEqual([9]); expect(w.vm.page.pageIndex.value).toBe(0); w.unmount()
  })
  it('preserves prior data on failure, dismisses explicitly, and clears on successful retry', async () => {
    get.mockResolvedValueOnce({ items: [1], nextCursor: '' }).mockRejectedValueOnce({ code: 'network_error' })
    const w = host(); await flushPromises(); await w.vm.page.reload(); await flushPromises()
    expect(w.vm.page.items.value).toEqual([1]); expect(w.vm.page.error.value?.code).toBe('network_error')
    w.vm.page.clear(); expect(w.vm.page.error.value).toBeNull()
    get.mockResolvedValueOnce({ items: [2], nextCursor: '' }); await w.vm.page.reset(); await flushPromises()
    expect(w.vm.page.items.value).toEqual([2]); expect(w.vm.page.error.value).toBeNull(); w.unmount()
  })
})
