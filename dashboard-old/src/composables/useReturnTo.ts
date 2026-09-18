/**
 * useReturnTo — return-URL composable.
 *
 * Reads `return_to` from the current route query, applies `safeReturnTo`
 * to reject cross-origin values, and provides `goReturnTo()` to navigate
 * to the guarded destination after a successful auth flow.
 *
 * Uses window.location.assign (full-page navigation) so the browser issues
 * a new GET and the session cookie is sent on the next request — same pattern
 * as the old dashboard and required for the OIDC consent redirect.
 *
 * Usage:
 *   const { returnTo, rawReturnTo, goReturnTo } = useReturnTo()
 *   // Login ceremony: forward the RAW value to the server (the authoritative
 *   // validator) and hardRedirect ONLY the server's blessed response — never
 *   // hardRedirect(rawReturnTo.value) directly (that would be an open redirect):
 *   const res = await api.post(`/auth/login/complete?return_to=${encodeURIComponent(rawReturnTo.value)}`, …)
 *   hardRedirect(res.redirect)
 *   // Fallback when there is no server redirect (recovery / already signed in):
 *   goReturnTo()   // client-side safeReturnTo guard
 */

import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { safeReturnTo } from '@/lib/returnTo'

export function useReturnTo() {
  const route = useRoute()

  const returnTo = computed<string>(() => {
    const raw = route.query.return_to
    return safeReturnTo(typeof raw === 'string' ? raw : undefined)
  })

  // rawReturnTo is the unsanitized query value forwarded to the server, which is
  // the authoritative return_to validator (validateReturnTo); use returnTo/goReturnTo
  // for client-side navigation.
  const rawReturnTo = computed<string>(() => {
    const raw = route.query.return_to
    return typeof raw === 'string' ? raw : ''
  })

  function goReturnTo(): void {
    window.location.assign(returnTo.value)
  }

  return { returnTo, rawReturnTo, goReturnTo }
}
