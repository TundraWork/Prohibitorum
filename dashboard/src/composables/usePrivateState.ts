import { onBeforeUnmount } from 'vue'
import { useQueryClient } from '@tanstack/vue-query'
import { keys } from '@/queries/resources'

/** Clear view-held secrets when /me is cleared, and when their view leaves. */
export function usePrivateState(clear: () => void): void {
  const client = useQueryClient()
  const unsubscribe = client.getQueryCache().subscribe(event => {
    if (JSON.stringify(event.query.queryKey) !== JSON.stringify(keys.me)) return
    if (event.type === 'removed' || (event.type === 'updated' && !event.query.state.data)) clear()
  })
  onBeforeUnmount(() => { unsubscribe(); clear() })
}
