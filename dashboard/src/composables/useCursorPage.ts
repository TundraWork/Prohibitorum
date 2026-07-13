/**
 * useCursorPage — shared cursor-pagination composable for admin collections.
 *
 * Wraps a fetcher that takes a cursor and returns a Page<T> envelope. Provides:
 *   - items, nextCursor, pageIndex, hasMore, busy, error (reactive refs)
 *   - next(): advance to the next page (pushes current cursor onto history)
 *   - previous(): go back one page (pops history, re-fetches with prior cursor)
 *   - reset(): clear history, reload page one (use after filter/sort changes)
 *   - reload(): re-fetch the current page (use after mutations)
 *
 * Cursor history is a stack of cursors used to fetch each page. pageCursors[0]
 * is always '' (the first page). previous() pops the stack and re-fetches with
 * the prior cursor — no reverse cursors are fabricated.
 *
 * Stale-request suppression: every fetch gets a monotonically increasing
 * request ID. If a response arrives with an outdated ID, it is silently
 * discarded. This prevents a slow first-page response from overwriting a
 * newer page that was fetched in the meantime (e.g. after a filter change).
 *
 * Empty-page step-back: if reload() returns an empty page and we're past
 * page 0, we step back to the previous page. This handles the case where a
 * mutation (e.g. delete) empties the current page.
 */

import { ref, computed, onMounted, type Ref } from 'vue'
import type { Page } from '@/lib/pagination'
import { isApiError, type ApiError } from '@/lib/errors'

export type { Page }

export interface CursorPage<T> {
  items: Ref<T[]>
  nextCursor: Ref<string>
  pageIndex: Ref<number>
  hasMore: Ref<boolean>
  busy: Ref<boolean>
  error: Ref<ApiError | null>
  next: () => Promise<void>
  previous: () => Promise<void>
  reset: () => Promise<void>
  reload: () => Promise<void>
  clear: () => void
}

export type CursorFetcher<T> = (cursor: string) => Promise<Page<T>>

export function useCursorPage<T>(fetcher: CursorFetcher<T>): CursorPage<T> {
  const items = ref<T[]>([]) as Ref<T[]>
  const nextCursor = ref('')
  const pageIndex = ref(0)
  const busy = ref(false)
  const error: Ref<ApiError | null> = ref(null)

  // Cursor history: pageCursors[i] = the cursor used to fetch page i.
  // pageCursors[0] = '' (first page, no cursor).
  const pageCursors = ref<string[]>([''])
  let requestId = 0

  const hasMore = computed(() => nextCursor.value !== '')

  async function fetchPage(cursor: string, stepBackOnEmpty = false): Promise<void> {
    const myId = ++requestId
    busy.value = true
    // Do not clear error on entry — it persists until a successful fetch.
    try {
      const page = await fetcher(cursor)
      // Stale suppression: discard if a newer request has started.
      if (myId !== requestId) return
      items.value = page.items ?? []
      nextCursor.value = page.nextCursor ?? ''
      error.value = null
      // Step-back: if the page is empty and we're past page 0, go back.
      if (stepBackOnEmpty && items.value.length === 0 && pageIndex.value > 0) {
        pageCursors.value = pageCursors.value.slice(0, pageIndex.value)
        pageIndex.value = pageIndex.value - 1
        const prevCursor = pageCursors.value[pageIndex.value] ?? ''
        await fetchPage(prevCursor, false)
        return
      }
    } catch (err: unknown) {
      if (myId !== requestId) return
      if (isApiError(err)) {
        const apiError: ApiError = { code: err.code }
        if (err.details !== undefined) apiError.details = err.details
        if (err.requestId !== undefined) apiError.requestId = err.requestId
        error.value = apiError
      } else {
        error.value = { code: 'network_error' }
      }
    } finally {
      if (myId === requestId) busy.value = false
    }
  }

  async function next(): Promise<void> {
    if (busy.value || nextCursor.value === '') return
    const cursor = nextCursor.value
    pageCursors.value = [...pageCursors.value.slice(0, pageIndex.value + 1), cursor]
    pageIndex.value = pageIndex.value + 1
    await fetchPage(cursor)
  }

  async function previous(): Promise<void> {
    if (busy.value || pageIndex.value <= 0) return
    pageIndex.value = pageIndex.value - 1
    const cursor = pageCursors.value[pageIndex.value] ?? ''
    await fetchPage(cursor)
  }

  async function reset(): Promise<void> {
    if (busy.value) {
      // Allow reset even when busy — it supersedes the current request.
    }
    pageCursors.value = ['']
    pageIndex.value = 0
    await fetchPage('')
  }

  async function reload(): Promise<void> {
    const cursor = pageCursors.value[pageIndex.value] ?? ''
    await fetchPage(cursor, true)
  }

  function clear(): void {
    error.value = null
  }

  onMounted(() => { void fetchPage('') })

  return { items, nextCursor, pageIndex, hasMore, busy, error, next, previous, reset, reload, clear }
}
