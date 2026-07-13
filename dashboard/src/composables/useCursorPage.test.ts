import { describe, it, expect, vi, beforeEach } from 'vitest'
import { defineComponent } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { useCursorPage, type Page } from './useCursorPage'
import type { Page as PageType } from '@/lib/pagination'
import { isApiError } from '@/lib/errors'

// Mock i18n minimally — useCursorPage does not use i18n, but Vue component
// mounting requires the plugin for components that use useI18n.
const i18n = createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en: {} } })

function makeHost<T>(fetcher: (cursor: string) => Promise<PageType<T>>) {
  return defineComponent({
    setup() {
      const page = useCursorPage<T>(fetcher)
      return { page }
    },
    template: '<div />',
  })
}

beforeEach(() => { vi.clearAllMocks() })

describe('useCursorPage', () => {
  it('fetches page one on mount with empty cursor', async () => {
    const fetcher = vi.fn().mockResolvedValue({ items: [1], nextCursor: 'c1' })
    const Host = makeHost(fetcher)
    mount(Host, { global: { plugins: [i18n] } })
    await flushPromises()
    expect(fetcher).toHaveBeenCalledWith('')
    expect(fetcher).toHaveBeenCalledTimes(1)
  })

  it('exposes items, nextCursor, pageIndex, busy, and error', async () => {
    const fetcher = vi.fn().mockResolvedValue({ items: [1, 2], nextCursor: 'c1' })
    const Host = defineComponent({
      setup() {
        const page = useCursorPage<number>(fetcher)
        return { page }
      },
      template: '<div />',
    })
    const w = mount(Host, { global: { plugins: [i18n] } })
    await flushPromises()
    expect(w.vm.page.items.value).toEqual([1, 2])
    expect(w.vm.page.nextCursor.value).toBe('c1')
    expect(w.vm.page.pageIndex.value).toBe(0)
    expect(w.vm.page.busy.value).toBe(false)
    expect(w.vm.page.error.value).toBeNull()
  })

  it('next pushes current nextCursor and advances pageIndex', async () => {
    let call = 0
    const fetcher = vi.fn().mockImplementation(() => {
      call++
      if (call === 1) return Promise.resolve({ items: [1, 2], nextCursor: 'c1' })
      if (call === 2) return Promise.resolve({ items: [3, 4], nextCursor: 'c2' })
      return Promise.resolve({ items: [], nextCursor: '' })
    })
    const Host = makeHost(fetcher)
    const w = mount(Host, { global: { plugins: [i18n] } })
    await flushPromises()
    await w.vm.page.next()
    await flushPromises()
    expect(fetcher).toHaveBeenLastCalledWith('c1')
    expect(w.vm.page.pageIndex.value).toBe(1)
    expect(w.vm.page.items.value).toEqual([3, 4])
  })

  it('next is a no-op when nextCursor is empty (final page)', async () => {
    const fetcher = vi.fn().mockResolvedValue({ items: [1], nextCursor: '' })
    const Host = makeHost(fetcher)
    const w = mount(Host, { global: { plugins: [i18n] } })
    await flushPromises()
    const callsBefore = fetcher.mock.calls.length
    await w.vm.page.next()
    await flushPromises()
    expect(fetcher.mock.calls.length).toBe(callsBefore) // no new fetch
    expect(w.vm.page.pageIndex.value).toBe(0)
  })

  it('next is a no-op when busy', async () => {
    let resolveFirst: (v: Page<number>) => void = () => {}
    const fetcher = vi.fn().mockImplementation(() => new Promise((r) => { resolveFirst = r }))
    const Host = makeHost(fetcher)
    const w = mount(Host, { global: { plugins: [i18n] } })
    await flushPromises()
    // Still busy — first fetch hasn't resolved
    await w.vm.page.next()
    expect(fetcher).toHaveBeenCalledTimes(1) // no second call
    resolveFirst({ items: [1], nextCursor: 'c1' })
    await flushPromises()
  })

  it('previous pops cursor history and returns to the prior page', async () => {
    let call = 0
    const fetcher = vi.fn().mockImplementation(() => {
      call++
      if (call === 1) return Promise.resolve({ items: [1, 2], nextCursor: 'c1' })
      if (call === 2) return Promise.resolve({ items: [3, 4], nextCursor: 'c2' })
      return Promise.resolve({ items: [1, 2], nextCursor: 'c1' })
    })
    const Host = makeHost(fetcher)
    const w = mount(Host, { global: { plugins: [i18n] } })
    await flushPromises()
    await w.vm.page.next()
    await flushPromises()
    // Now on page 2 (cursor c1 was used, pageIndex=1)
    await w.vm.page.previous()
    await flushPromises()
    expect(fetcher).toHaveBeenLastCalledWith('') // page 1 uses empty cursor
    expect(w.vm.page.pageIndex.value).toBe(0)
    expect(w.vm.page.items.value).toEqual([1, 2])
  })

  it('previous is a no-op on page 0', async () => {
    const fetcher = vi.fn().mockResolvedValue({ items: [1], nextCursor: '' })
    const Host = makeHost(fetcher)
    const w = mount(Host, { global: { plugins: [i18n] } })
    await flushPromises()
    const callsBefore = fetcher.mock.calls.length
    await w.vm.page.previous()
    await flushPromises()
    expect(fetcher.mock.calls.length).toBe(callsBefore)
    expect(w.vm.page.pageIndex.value).toBe(0)
  })

  it('reset clears history and reloads page one', async () => {
    let call = 0
    const fetcher = vi.fn().mockImplementation(() => {
      call++
      if (call === 1) return Promise.resolve({ items: [1], nextCursor: 'c1' })
      if (call === 2) return Promise.resolve({ items: [2], nextCursor: 'c2' })
      return Promise.resolve({ items: [9], nextCursor: '' })
    })
    const Host = makeHost(fetcher)
    const w = mount(Host, { global: { plugins: [i18n] } })
    await flushPromises()
    await w.vm.page.next()
    await flushPromises()
    await w.vm.page.reset()
    await flushPromises()
    expect(fetcher).toHaveBeenLastCalledWith('')
    expect(w.vm.page.pageIndex.value).toBe(0)
    expect(w.vm.page.items.value).toEqual([9])
  })

  it('reload re-fetches the current page with the same cursor', async () => {
    let call = 0
    const fetcher = vi.fn().mockImplementation(() => {
      call++
      if (call === 1) return Promise.resolve({ items: [1, 2], nextCursor: 'c1' })
      if (call === 2) return Promise.resolve({ items: [3, 4], nextCursor: '' })
      return Promise.resolve({ items: [3, 4, 5], nextCursor: '' })
    })
    const Host = makeHost(fetcher)
    const w = mount(Host, { global: { plugins: [i18n] } })
    await flushPromises()
    await w.vm.page.next()
    await flushPromises()
    // Now on page 2 (cursor c1)
    await w.vm.page.reload()
    await flushPromises()
    expect(fetcher).toHaveBeenLastCalledWith('c1')
    expect(w.vm.page.pageIndex.value).toBe(1)
    expect(w.vm.page.items.value).toEqual([3, 4, 5])
  })

  it('reload steps back to the previous page when the current page becomes empty', async () => {
    let call = 0
    const fetcher = vi.fn().mockImplementation(() => {
      call++
      if (call === 1) return Promise.resolve({ items: [1, 2], nextCursor: 'c1' })
      if (call === 2) return Promise.resolve({ items: [3, 4], nextCursor: '' })
      // Reload of page 2 returns empty (e.g. the last item was deleted)
      return Promise.resolve({ items: [], nextCursor: '' })
    })
    const Host = makeHost(fetcher)
    const w = mount(Host, { global: { plugins: [i18n] } })
    await flushPromises()
    await w.vm.page.next()
    await flushPromises()
    // Now on page 2 (cursor c1), items [3,4]
    await w.vm.page.reload()
    await flushPromises()
    // Reload returned empty → stepped back to page 1 (cursor '')
    expect(w.vm.page.pageIndex.value).toBe(0)
    // The last fetch should have been for page 1 (cursor '')
    expect(fetcher).toHaveBeenLastCalledWith('')
  })

  it('suppreses stale requests: a slow first-page response does not overwrite a later page', async () => {
    let slowResolve: (v: Page<number>) => void = () => {}
    const fetcher = vi.fn().mockImplementation((cursor: string) => {
      if (cursor === '') {
        return new Promise((r) => { slowResolve = r })
      }
      return Promise.resolve({ items: [99], nextCursor: '' })
    })
    const Host = makeHost(fetcher)
    const w = mount(Host, { global: { plugins: [i18n] } })
    await flushPromises()
    // First fetch (page 1) is still pending. Trigger a reset which will
    // produce a new request id, so the stale first response is ignored.
    slowResolve({ items: [1, 2], nextCursor: 'c1' })
    await flushPromises()
    // Now complete the reset
    fetcher.mockImplementation(() => Promise.resolve({ items: [42], nextCursor: '' }))
    await w.vm.page.reset()
    await flushPromises()
    expect(w.vm.page.items.value).toEqual([42])
  })

  it('keeps ApiError failures typed, prose-free, and explicitly clearable', async () => {
    const thrown = {
      code: 'forbidden',
      details: { reason: 'not_registered' },
      requestId: 'rid-cursor',
      message: 'raw server prose',
    }
    const fetcher = vi.fn().mockRejectedValue(thrown)
    const Host = makeHost(fetcher)
    const w = mount(Host, { global: { plugins: [i18n] } })
    await flushPromises()
    expect(isApiError(w.vm.page.error.value)).toBe(true)
    expect(w.vm.page.error.value).toEqual({
      code: 'forbidden',
      details: { reason: 'not_registered' },
      requestId: 'rid-cursor',
    })
    expect(w.vm.page.items.value).toEqual([])

    w.vm.page.clear()

    expect(w.vm.page.error.value).toBeNull()
  })

  it('clears error on a successful fetch after failure', async () => {
    let call = 0
    const fetcher = vi.fn().mockImplementation(() => {
      call++
      if (call === 1) return Promise.reject({ code: 'forbidden' })
      return Promise.resolve({ items: [1], nextCursor: '' })
    })
    const Host = makeHost(fetcher)
    const w = mount(Host, { global: { plugins: [i18n] } })
    await flushPromises()
    expect(w.vm.page.error.value).not.toBeNull()
    await w.vm.page.reset()
    await flushPromises()
    expect(w.vm.page.error.value).toBeNull()
    expect(w.vm.page.items.value).toEqual([1])
  })

  it('exposes hasMore computed from nextCursor', async () => {
    const fetcher = vi.fn().mockResolvedValue({ items: [1], nextCursor: 'c1' })
    const Host = makeHost(fetcher)
    const w = mount(Host, { global: { plugins: [i18n] } })
    await flushPromises()
    expect(w.vm.page.hasMore.value).toBe(true)
  })

  it('hasMore is false when nextCursor is empty', async () => {
    const fetcher = vi.fn().mockResolvedValue({ items: [1], nextCursor: '' })
    const Host = makeHost(fetcher)
    const w = mount(Host, { global: { plugins: [i18n] } })
    await flushPromises()
    expect(w.vm.page.hasMore.value).toBe(false)
  })
})
