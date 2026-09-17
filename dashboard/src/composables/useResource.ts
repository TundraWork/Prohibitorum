import { computed, ref, watch } from 'vue'
import { useQuery, type UseQueryOptions, type QueryKey } from '@tanstack/vue-query'
import type { ApiError } from '@/lib/api'
import { isRequestCancelled } from '@/lib/cancellation'

/** Presentation state only; server data remains owned by the query observer. */
export function useResource<T, K extends QueryKey>(options: UseQueryOptions<T, Error, T, T, K>) {
  const query = useQuery(options)
  const dismissed = ref<unknown>(null)
  const error = computed<ApiError | null>(() => {
    const failure = query.error.value
    if (!failure || failure === dismissed.value || isRequestCancelled(failure)) return null
    return typeof (failure as unknown as ApiError).code === 'string' ? failure as unknown as ApiError : { code: 'network_error' }
  })
  watch(query.error, value => { if (!value) dismissed.value = null })
  return { ...query, error, busy: query.isFetching, clear: () => { dismissed.value = query.error.value } }
}
