import { onBeforeUnmount } from 'vue'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { invalidateResource, type MutationResource } from '@/queries/invalidation'

/** Explicit actions never retry, retain results, or deliver into a closed view. */
export function useAction(resource?: MutationResource) {
  const client = useQueryClient()
  type Action = { invoke: (signal: AbortSignal) => Promise<unknown>; controller: AbortController }
  let active: Action | undefined
  const mutation = useMutation({
    mutationFn: async (action: Action) => {
      action.controller.signal.throwIfAborted()
      const result = await action.invoke(action.controller.signal)
      action.controller.signal.throwIfAborted()
      return result
    },
    retry: false, gcTime: 0,
    onSuccess: async (result, action) => {
      if (resource && result !== undefined && !action.controller.signal.aborted) await invalidateResource(client, resource)
    },
  })
  // QueryClient.clear's mutation removal is also a cancellation boundary for
  // view-local action results. This uses the action's AbortSignal, not identity keys.
  const unsubscribe = client.getMutationCache().subscribe(event => {
    if (event.type === 'removed' && event.mutation.state.variables === active) active?.controller.abort()
  })
  onBeforeUnmount(() => { active?.controller.abort(); mutation.reset(); unsubscribe() })
  async function execute<T>(invoke: (signal: AbortSignal) => Promise<T>): Promise<T> {
    const action = { invoke, controller: new AbortController() }
    active = action
    try {
      const result = await mutation.mutateAsync(action)
      action.controller.signal.throwIfAborted()
      return result as T
    } finally {
      if (active === action) active = undefined
      mutation.reset()
    }
  }
  return { execute }
}
