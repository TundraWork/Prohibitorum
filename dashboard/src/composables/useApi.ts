/**
 * useApi — shared async-flow composable.
 *
 * Provides `busy`, `error`, `run`, `clear`, and `errorText` reactive refs.
 *
 * - `run(fn)` sets busy=true for the duration of the async function and maps
 *   thrown `ApiError`s into the `error` ref. On SUCCESS, the error is cleared
 *   (a successful retry is the explicit dismissal for errors tied to a form
 *   action). On FAILURE, the error PERSISTS — it is never auto-dismissed.
 * - `clear()` explicitly clears the error (ErrorPanel dismiss button).
 * - `errorText` is the localized message computed from `error.code` via
 *   `errorTranslationKey`. It returns '' when there is no error, when the code
 *   is globally handled (redirect/toast), or when the locale lacks the key
 *   (the ErrorPanel renders the unknown fallback in that case).
 *
 * Pages call `run(async () => { ... })` and bind `error` to `<ErrorPanel>`.
 */

import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ApiError } from '@/lib/api'
import { errorTranslationKey } from '@/lib/errors'
import { GLOBAL_ERROR_CODES } from '@/lib/errorCodes'

export function useApi() {
  const { t, te } = useI18n()
  const busy = ref(false)
  const error = ref<ApiError | null>(null)

  async function run<T>(fn: () => Promise<T>): Promise<T | undefined> {
    if (busy.value) return undefined
    busy.value = true
    // Do NOT clear error on entry — the error persists until the run succeeds
    // or clear() is called. This matches the contract: errors persist until
    // explicit dismissal or successful retry.
    try {
      const result = await fn()
      // Success → clear any prior error (successful retry dismisses).
      error.value = null
      return result
    } catch (err: unknown) {
      const apiErr = err as ApiError
      if (apiErr && typeof apiErr.code === 'string') {
        error.value = apiErr
      } else {
        // Network / unexpected error — surface as a generic network_error
        // (no message field — the frontend maps from the code).
        error.value = { code: 'network_error' }
      }
      return undefined
    } finally {
      busy.value = false
    }
  }

  /** Explicitly clear the error (ErrorPanel dismiss button). */
  function clear(): void {
    error.value = null
  }

  const errorText = computed(() => {
    const e = error.value
    if (!e) return ''
    // Globally-handled codes (redirect / maintenance / connection toast) must
    // NOT leak an inline message — the global handler owns the UX for these.
    // ErrorPanel will still render (showing details/copy), just without the
    // summary text duplication.
    if (GLOBAL_ERROR_CODES.has(e.code)) return ''
    const key = errorTranslationKey(e.code)
    return te(key) ? t(key) : ''
  })

  return { busy, error, run, clear, errorText }
}
