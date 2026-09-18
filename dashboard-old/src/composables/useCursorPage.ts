import { computed, nextTick, ref, watch, type MaybeRefOrGetter, toValue } from 'vue'
import { useResource } from '@/composables/useResource'
import { collectionQuery, type Collection, type PageFilters } from '@/queries/resources'
export type { Page } from '@/lib/pagination'

/** Cursor history is UI state; each filter/cursor combination owns a query. */
export function useCursorPage<T>(resource: Collection, filters: MaybeRefOrGetter<PageFilters> = {}) {
  const cursors = ref([''])
  const pageIndex = ref(0)
  const parameters = computed(() => toValue(filters))
  watch(parameters, () => { cursors.value = ['']; pageIndex.value = 0 }, { flush: 'sync' })
  const query = useResource(computed(() => {
    const options = collectionQuery<T>(resource, { ...parameters.value, cursor: cursors.value[pageIndex.value] ?? '' })
    return { queryKey: options.queryKey, queryFn: options.queryFn }
  }))
  const items = computed(() => query.data.value?.items ?? [])
  const nextCursor = computed(() => query.data.value?.nextCursor ?? '')
  async function next(): Promise<void> {
    if (query.busy.value || !nextCursor.value) return
    cursors.value = [...cursors.value.slice(0, pageIndex.value + 1), nextCursor.value]
    pageIndex.value++
    await nextTick()
  }
  async function previous(): Promise<void> {
    if (query.busy.value || pageIndex.value === 0) return
    pageIndex.value--
    await nextTick()
  }
  async function reset(): Promise<void> {
    cursors.value = ['']; pageIndex.value = 0
    await nextTick(); await query.refetch()
  }
  // Mutation invalidation has already refreshed the active page. Step back only
  // after a successful empty response, never while changing filters/loading.
  watch([query.data, query.isFetching], ([data, fetching]) => {
    if (!fetching && query.isSuccess.value && pageIndex.value > 0 && data && !data.items.length) {
      pageIndex.value--; cursors.value = cursors.value.slice(0, pageIndex.value + 1)
    }
  })
  async function reload(): Promise<void> { await query.refetch(); await nextTick() }

  return { items, nextCursor, pageIndex, hasMore: computed(() => !!nextCursor.value), busy: query.busy, error: query.error, clear: query.clear, next, previous, reset, reload }
}
