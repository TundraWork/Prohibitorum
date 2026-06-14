/**
 * useApi — shared async-flow composable.
 *
 * Provides `busy` and `error` reactive refs plus a `run()` helper that:
 *  - sets busy=true for the duration of the async function
 *  - maps thrown {code,message} ApiErrors into the error ref
 *  - re-sets busy=false on completion (success or error)
 *
 * Pages and components call `run(async () => { ... })` and bind to `busy`/`error`
 * without duplicating the try/catch/finally pattern everywhere.
 *
 * Usage:
 *   const { busy, error, run } = useApi()
 *   await run(() => api.post('/auth/login/begin'))
 */

import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ApiError } from '@/lib/api'

export function useApi() {
  const { t, te } = useI18n()
  const busy = ref(false)
  const error = ref<ApiError | null>(null)

  async function run<T>(fn: () => Promise<T>): Promise<T | undefined> {
    if (busy.value) return undefined
    busy.value = true
    error.value = null
    try {
      return await fn()
    } catch (err: unknown) {
      const apiErr = err as ApiError
      if (apiErr && typeof apiErr.code === 'string') {
        error.value = apiErr
      } else {
        // Network / unexpected error — surface as a generic server_error
        error.value = {
          code: 'server_error',
          message: err instanceof Error ? err.message : 'An unexpected error occurred',
        }
      }
      return undefined
    } finally {
      busy.value = false
    }
  }

  const errorText = computed(() => {
    const e = error.value
    if (!e) return ''
    const key = `errors.${e.code}`
    return te(key) ? t(key) : e.message || t('common.error')
  })

  return { busy, error, run, errorText }
}
