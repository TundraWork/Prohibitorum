import type { Router } from 'vue-router'

// Threshold/public routes where a no_session is the expected, normal state —
// the handler must never redirect or flag on these (prevents /login loops and
// preserves ConsentView/WelcomeView/boot-time /me handling).
const PUBLIC_ROUTE_NAMES = new Set(['login', 'error', 'welcome', 'consent', 'enroll', 'pair', 'logout'])

export interface SessionExpiryDeps {
  router: Router
  clearAuth: () => void | Promise<void>
  setExpiredFlag: () => void
}

let handling = false
/** test-only reset of the idempotency latch */
export function __resetHandlingForTest(): void { handling = false }

/**
 * Build the 401-no_session handler. Route-aware (no navigation on public routes),
 * idempotent (one redirect per expiry), and read-vs-mutation aware: GET
 * navigations redirect to /login; mutations flag a non-destructive banner so
 * unsaved form input is not silently discarded.
 */
export function createUnauthorizedHandler(deps: SessionExpiryDeps) {
  return async ({ method }: { method: string }): Promise<void> => {
    if (handling) return
    handling = true
    const cur = deps.router.currentRoute.value
    const isPublic = cur.meta?.public === true || PUBLIC_ROUTE_NAMES.has(String(cur.name ?? ''))
    try {
      // Public routes suppress navigation, but must never retain a previous account.
      await deps.clearAuth()
      if (isPublic) return
      if (method === 'GET') {
        await deps.router.replace({ name: 'login', query: { return_to: cur.fullPath, reason: 'session_expired' } })
      } else {
        deps.setExpiredFlag()
      }
    } finally {
      handling = false
    }
  }
}
