import { beforeEach, afterEach } from 'vitest'
import { config, enableAutoUnmount } from '@vue/test-utils'
import { VueQueryPlugin } from '@tanstack/vue-query'
import { createQueryClient, queryClient } from '@/queries/client'
enableAutoUnmount(afterEach)
export let testQueryClient = createQueryClient()
function disableQueryRetries(client: ReturnType<typeof createQueryClient>) {
  const defaults = client.getDefaultOptions()
  client.setDefaultOptions({
    ...defaults,
    queries: { ...defaults.queries, retry: false },
  })
}
beforeEach(() => {
  testQueryClient = createQueryClient()
  // Component tests assert one request cycle at a time. Query retry behavior is
  // covered directly in queries/client.test.ts; keep other tests deterministic.
  disableQueryRetries(testQueryClient)
  disableQueryRetries(queryClient)
  queryClient.clear()
  config.global.plugins = [[VueQueryPlugin, { queryClient: testQueryClient }]]
})
afterEach(() => { testQueryClient.clear(); queryClient.clear() })
