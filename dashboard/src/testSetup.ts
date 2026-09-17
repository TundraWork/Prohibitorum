import { beforeEach, afterEach } from 'vitest'
import { config, enableAutoUnmount } from '@vue/test-utils'
import { VueQueryPlugin } from '@tanstack/vue-query'
import { createQueryClient, queryClient } from '@/queries/client'
enableAutoUnmount(afterEach)
export let testQueryClient = createQueryClient()
beforeEach(() => {
  testQueryClient = createQueryClient()
  queryClient.clear()
  config.global.plugins = [[VueQueryPlugin, { queryClient: testQueryClient }]]
})
afterEach(() => { testQueryClient.clear(); queryClient.clear() })
