/**
 * Router — threshold page routes + guard scaffold.
 *
 * Routes: /login /consent /logout /error /enroll/:token + catch-all → /error.
 * Pages are lazy-imported. For Tasks 6–9 they are stub placeholder components;
 * each task replaces the stub with the real implementation.
 *
 * `installGuard` — sets up the navigation guard for requiresAuth/requiresAdmin
 * meta (reserved for Spec 2/3 authenticated routes). Threshold routes (login,
 * consent, logout, error, enroll) are intentionally public — the guard is
 * a no-op on them. The scaffold is in place so adding auth requirements later
 * requires only a route `meta` field, not touching the guard logic.
 */

import { createRouter, createWebHistory, type Router, type RouteRecordRaw } from 'vue-router'
import { defineComponent, h } from 'vue'

// ---------------------------------------------------------------------------
// Extend vue-router's RouteMeta with our custom guard meta fields.
// Placed here (in a module file that imports vue-router) to ensure this is
// treated as a module augmentation, not an ambient declaration — the latter
// breaks named exports from the vue-router package.
// ---------------------------------------------------------------------------
declare module 'vue-router' {
  interface RouteMeta {
    /** Threshold / public route — the guard skips auth checks. */
    public?: boolean
    /** Requires an authenticated session (Spec 2/3 dashboard routes). */
    requiresAuth?: boolean
    /** Requires admin role (Spec 3 admin routes). */
    requiresAdmin?: boolean
  }
}

// ---------------------------------------------------------------------------
// Stub page component — used for any route whose real page hasn't been built
// yet. Tasks 6–9 replace these with real implementations.
// ---------------------------------------------------------------------------
function makePlaceholder(name: string) {
  return defineComponent({
    name,
    render() {
      return h(
        'div',
        {
          style:
            'display:flex;align-items:center;justify-content:center;min-height:100vh;font-family:sans-serif;color:#555',
        },
        `Prohibitorum — ${name} (coming soon)`,
      )
    },
  })
}

// ---------------------------------------------------------------------------
// Route table
//
// Each entry uses () => Promise<Component> (the lazy-import pattern) so
// Tasks 6–9 can drop in the real page with a one-line edit:
//   component: () => import('../pages/LoginView.vue')
// For now, all resolve synchronously via a resolved promise so the build passes.
// ---------------------------------------------------------------------------
const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'login',
    component: () => import('../pages/LoginView.vue'),
    meta: { public: true },
  },
  {
    path: '/consent',
    name: 'consent',
    component: () => import('../pages/ConsentView.vue'),
    meta: { public: true },
  },
  {
    path: '/logout',
    name: 'logout',
    component: () => Promise.resolve(makePlaceholder('LogoutView')),
    meta: { public: true },
  },
  {
    path: '/error',
    name: 'error',
    component: () => Promise.resolve(makePlaceholder('ErrorView')),
    meta: { public: true },
  },
  {
    path: '/enroll/:token',
    name: 'enroll',
    component: () => Promise.resolve(makePlaceholder('EnrollView')),
    meta: { public: true },
  },
  // Catch-all → /error
  {
    path: '/:pathMatch(.*)*',
    redirect: '/error',
  },
]

// ---------------------------------------------------------------------------
// Navigation guard scaffold
//
// requiresAuth / requiresAdmin are reserved for Spec 2/3 routes — no
// threshold route uses them. When those meta fields are present on a future
// route, the guard enforces authentication/authorisation before allowing
// navigation. The auth store check is deferred until needed (import lazily
// to avoid import cycles between router ↔ store during bootstrap).
// ---------------------------------------------------------------------------
export function installGuard(router: Router): void {
  router.beforeEach(async (to) => {
    // All threshold routes and any route marked public: skip guard.
    if (to.meta.public) return true

    // requiresAuth: ensure the user is logged in.
    if (to.meta.requiresAuth) {
      // Lazy import to avoid circular dependency at module init time.
      const { useAuthStore } = await import('@/stores/auth')
      const { getActivePinia } = await import('pinia')
      const pinia = getActivePinia()
      if (!pinia) return { name: 'login', query: { return_to: to.fullPath } }
      const auth = useAuthStore()
      await auth.ensureLoaded()
      if (!auth.me) {
        return { name: 'login', query: { return_to: to.fullPath } }
      }
    }

    // requiresAdmin: ensure the user has admin role.
    if (to.meta.requiresAdmin) {
      const { useAuthStore } = await import('@/stores/auth')
      const { getActivePinia } = await import('pinia')
      const pinia = getActivePinia()
      if (!pinia) return { name: 'error', query: { error: 'forbidden' } }
      const auth = useAuthStore()
      await auth.ensureLoaded()
      if (!auth.isAdmin) {
        return { name: 'error', query: { error: 'forbidden' } }
      }
    }

    return true
  })
}

const router = createRouter({
  history: createWebHistory(),
  routes,
})

installGuard(router)

export default router
