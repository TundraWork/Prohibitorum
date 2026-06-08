/**
 * Router — threshold page routes + guard scaffold.
 *
 * Routes: /login /consent /logout /error /enroll/:token + catch-all → /error.
 * Pages are lazy-imported (one chunk per threshold page).
 *
 * `installGuard` — sets up the navigation guard for requiresAuth/requiresAdmin
 * meta (reserved for Spec 2/3 authenticated routes). Threshold routes (login,
 * consent, logout, error, enroll) are intentionally public — the guard is
 * a no-op on them. The scaffold is in place so adding auth requirements later
 * requires only a route `meta` field, not touching the guard logic.
 */

import { createRouter, createWebHistory, type Router, type RouteRecordRaw } from 'vue-router'

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
// Route table — each threshold page is lazy-imported into its own chunk.
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
    component: () => import('../pages/LogoutView.vue'),
    meta: { public: true },
  },
  {
    path: '/error',
    name: 'error',
    component: () => import('../pages/ErrorView.vue'),
    meta: { public: true },
  },
  {
    path: '/enroll/:token',
    name: 'enroll',
    component: () => import('../pages/EnrollView.vue'),
    meta: { public: true },
  },
  {
    path: '/pair',
    name: 'pair',
    component: () => import('../pages/PairDeviceView.vue'),
    meta: { public: true },
  },
  // Authenticated dashboard shell (Spec 2a). requiresAuth → installGuard
  // redirects to /login?return_to= when not signed in.
  {
    path: '/',
    component: () => import('../pages/DashboardLayout.vue'),
    meta: { requiresAuth: true },
    children: [
      { path: '', name: 'profile', component: () => import('../pages/ProfileView.vue') },
      { path: 'sessions', name: 'sessions', component: () => import('../pages/SessionsView.vue') },
      { path: 'security', name: 'security', component: () => import('../pages/SecurityView.vue') },
      { path: 'connected', name: 'connected', component: () => import('../pages/ConnectedAccountsView.vue') },
      { path: 'devices', name: 'devices', component: () => import('../pages/DevicesView.vue') },
      { path: 'admin/accounts', name: 'admin-accounts', component: () => import('../pages/admin/AdminAccountsView.vue'), meta: { requiresAdmin: true } },
      { path: 'admin/accounts/:id', name: 'admin-account-detail', component: () => import('../pages/admin/AdminAccountDetailView.vue'), meta: { requiresAdmin: true } },
      { path: 'admin/invitations', name: 'admin-invitations', component: () => import('../pages/admin/AdminInvitationsView.vue'), meta: { requiresAdmin: true } },
      { path: 'admin/oidc-clients', name: 'admin-oidc-clients', component: () => import('../pages/admin/AdminOidcClientsView.vue'), meta: { requiresAdmin: true } },
      { path: 'admin/oidc-clients/:clientId', name: 'admin-oidc-client-detail', component: () => import('../pages/admin/AdminOidcClientDetailView.vue'), meta: { requiresAdmin: true } },
      { path: 'admin/saml-providers', name: 'admin-saml-providers', component: () => import('../pages/admin/AdminSamlProvidersView.vue'), meta: { requiresAdmin: true } },
      { path: 'admin/saml-providers/:id', name: 'admin-saml-provider-detail', component: () => import('../pages/admin/AdminSamlProviderDetailView.vue'), meta: { requiresAdmin: true } },
    ],
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
