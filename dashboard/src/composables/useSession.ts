import { computed, reactive } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { avatarStatusQuery, sessionQuery } from '@/queries/resources'

export type { SessionView } from '@/queries/resources'

export function useSession() {
  const client = useQueryClient()
  const session = useQuery(sessionQuery())
  const avatar = useQuery(computed(() => ({
    ...avatarStatusQuery(client),
    enabled: session.data.value?.avatarPending === true,
    gcTime: 0, staleTime: 0, refetchOnWindowFocus: false,
    refetchInterval: (query: { state: { data?: { pending: boolean }; error: unknown } }) => query.state.error || query.state.data?.pending === false ? false : 1500,
  })))
  return reactive({
    me: computed(() => session.data.value ?? null),
    isAdmin: computed(() => session.data.value?.role === 'admin'),
    avatarBusy: computed(() => session.data.value?.avatarPending === true && !avatar.error.value),
    error: computed(() => session.error.value ?? avatar.error.value),
    retry: () => session.refetch(),
  })
}
