import { QueryClient } from '@tanstack/vue-query'

export function createQueryClient(): QueryClient {
  return new QueryClient({ defaultOptions: {
    queries: { staleTime: 30_000, gcTime: 300_000, retry: false, refetchOnWindowFocus: true, refetchOnReconnect: true },
    mutations: { retry: false, gcTime: 0 },
  } })
}

export const queryClient = createQueryClient()

/** Cancel before removing private state so late responses cannot repopulate it. */
export async function clearSessionQueries(client: QueryClient = queryClient): Promise<void> {
  await client.cancelQueries({ queryKey: ['session'] })
  // Reset active observers as well: removal alone leaves their last result visible.
  for (const query of client.getQueryCache().findAll({ queryKey: ['session'] })) query.reset()
  client.removeQueries({ queryKey: ['session'] })
  client.getMutationCache().clear()
}
