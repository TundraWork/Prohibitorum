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

/**
 * Error codes owned by a GLOBAL handler — a redirect (no_session →
 * sessionExpiry), a full-screen redirect (maintenance_mode), or a toast
 * (network_error / server_error from api.ts). For these, `errorText` returns ''
 * so pages do NOT also render a redundant (and often misleading, e.g. a stale
 * "Please sign in to continue." dead-end) inline message. Only genuine app 4xx
 * codes — validation, conflicts — surface inline.
 */
const GLOBAL_CODES = new Set(['no_session', 'maintenance_mode', 'network_error', 'server_error'])

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
    // Globally-handled codes (toast / redirect / maintenance screen) must NOT
    // leak an inline message — the global handler owns the UX for these.
    if (GLOBAL_CODES.has(e.code)) return ''
    const key = `errors.${e.code}`
    return te(key) ? t(key) : e.message || t('common.error')
  })

  return { busy, error, run, errorText }
}
