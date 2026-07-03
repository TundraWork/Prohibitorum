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
import { buildTitle } from '@/lib/pageTitle'

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
    /** i18n key for the page title (title.*). Absent on the root redirect. */
    titleKey?: string
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
    meta: { public: true, titleKey: 'title.login' },
  },
  {
    path: '/consent',
    name: 'consent',
    component: () => import('../pages/ConsentView.vue'),
    meta: { public: true, titleKey: 'title.consent' },
  },
  {
    path: '/saml-consent',
    name: 'saml-consent',
    component: () => import('../pages/SamlConsentView.vue'),
    meta: { public: true, titleKey: 'title.samlConsent' },
  },
  {
    path: '/logout',
    name: 'logout',
    component: () => import('../pages/LogoutView.vue'),
    meta: { public: true, titleKey: 'title.logout' },
  },
  {
    path: '/error',
    name: 'error',
    component: () => import('../pages/ErrorView.vue'),
    meta: { public: true, titleKey: 'title.error' },
  },
  {
    path: '/enroll/:token',
    name: 'enroll',
    component: () => import('../pages/EnrollView.vue'),
    meta: { public: true, titleKey: 'title.enroll' },
  },
  {
    path: '/pair',
    name: 'pair',
    component: () => import('../pages/PairDeviceView.vue'),
    meta: { public: true, titleKey: 'title.pair' },
  },
  {
    path: '/welcome',
    name: 'welcome',
    component: () => import('../pages/WelcomeView.vue'),
    meta: { public: true, titleKey: 'title.welcome' },
  },
  {
    path: '/maintenance',
    name: 'maintenance',
    component: () => import('../pages/MaintenanceView.vue'),
    meta: { public: true, titleKey: 'title.maintenance' },
  },
  // Launcher shell — the end-user home.
  {
    path: '/',
    component: () => import('../pages/LauncherLayout.vue'),
    meta: { requiresAuth: true },
    children: [
      { path: '', name: 'my-apps', component: () => import('../pages/MyAppsView.vue'), meta: { titleKey: 'title.myApps' } },
    ],
  },
  // Settings/admin shell — absolute child paths keep existing URLs.
  {
    path: '/account', // internal parent; never navigated to directly
    component: () => import('../pages/DashboardLayout.vue'),
    meta: { requiresAuth: true },
    children: [
      { path: '/sessions', name: 'sessions', component: () => import('../pages/SessionsView.vue'), meta: { titleKey: 'title.sessions' } },
      { path: '/tokens', name: 'tokens', component: () => import('../pages/TokensView.vue'), meta: { titleKey: 'title.tokens' } },
      { path: '/security', name: 'security', component: () => import('../pages/SecurityView.vue'), meta: { titleKey: 'title.security' } },
      { path: '/connected', name: 'connected', component: () => import('../pages/ConnectedAccountsView.vue'), meta: { titleKey: 'title.connected' } },
      { path: '/devices', name: 'devices', component: () => import('../pages/DevicesView.vue'), meta: { titleKey: 'title.devices' } },
      { path: '/app-access', name: 'app-access', component: () => import('../pages/AppAccessView.vue'), meta: { titleKey: 'title.appAccess' } },
      { path: '/admin/accounts', name: 'admin-accounts', component: () => import('../pages/admin/AdminAccountsView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminAccounts' } },
      { path: '/admin/accounts/:id', name: 'admin-account-detail', component: () => import('../pages/admin/AdminAccountDetailView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminAccountDetail' } },
      { path: '/admin/invitations', name: 'admin-invitations', component: () => import('../pages/admin/AdminInvitationsView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminInvitations' } },
      { path: '/admin/oidc-applications', name: 'admin-oidc-applications', component: () => import('../pages/admin/AdminOidcClientsView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminOidcApplications' } },
      { path: '/admin/oidc-applications/:clientId', name: 'admin-oidc-application-detail', component: () => import('../pages/admin/AdminOidcClientDetailView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminOidcApplicationDetail' } },
      { path: '/admin/forward-auth-apps', name: 'admin-forward-auth-apps', component: () => import('../pages/admin/AdminForwardAuthAppsView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminForwardAuthApps' } },
      { path: '/admin/forward-auth-apps/:clientId', name: 'admin-forward-auth-app-detail', component: () => import('../pages/admin/AdminForwardAuthAppDetailView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminForwardAuthAppDetail' } },
      { path: '/admin/saml-applications', name: 'admin-saml-applications', component: () => import('../pages/admin/AdminSamlProvidersView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminSamlApplications' } },
      { path: '/admin/saml-applications/:id', name: 'admin-saml-application-detail', component: () => import('../pages/admin/AdminSamlProviderDetailView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminSamlApplicationDetail' } },
      { path: '/admin/identity-providers', name: 'admin-identity-providers', component: () => import('../pages/admin/AdminUpstreamIdpsView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminIdentityProviders' } },
      { path: '/admin/identity-providers/:slug', name: 'admin-identity-provider-detail', component: () => import('../pages/admin/AdminUpstreamIdpDetailView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminIdentityProviderDetail' } },
      { path: '/admin/signing-keys', name: 'admin-signing-keys', component: () => import('../pages/admin/AdminSigningKeysView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminSigningKeys' } },
      { path: '/admin/audit', name: 'admin-audit', component: () => import('../pages/admin/AdminAuditView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminAudit' } },
      { path: '/admin/settings', name: 'admin-settings', component: () => import('../pages/admin/SettingsView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminSettings' } },
      { path: '/admin/groups', name: 'admin-groups', component: () => import('../pages/admin/AdminGroupsView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminGroups' } },
      { path: '/admin/groups/:id', name: 'admin-group-detail', component: () => import('../pages/admin/AdminGroupDetailView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminGroupDetail' } },
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
    // ---------------------------------------------------------------------------
    // Maintenance gate — evaluated before auth so non-admin users are redirected
    // even on routes they would otherwise be allowed to visit.
    // ---------------------------------------------------------------------------
    {
      const { useBrandingStore: useBranding } = await import('@/stores/branding')
      const { getActivePinia } = await import('pinia')
      const pinia = getActivePinia()
      if (pinia) {
        const branding = useBranding(pinia)
        await branding.ensureLoaded()

        if (branding.maintenanceMode) {
          // Admins bypass maintenance entirely; everyone else — unauthenticated
          // visitors AND authenticated non-admins — is confined to the notice
          // page, sign-out, and the deliberate admin-login entry (/login?admin=1).
          const { useAuthStore: useAuth } = await import('@/stores/auth')
          const auth = useAuth(pinia)
          // GET /me is allowlisted by the backend during maintenance.
          try { await auth.ensureLoaded() } catch { /* treat as unauthenticated */ }

          if (!(auth.me && auth.isAdmin)) {
            const allowed =
              to.name === 'maintenance' ||
              to.name === 'logout' ||
              (to.name === 'login' && to.query.admin !== undefined)
            if (!allowed) return { name: 'maintenance' }
            return true
          }
          // Admin: fall through to the normal flow below.
        } else {
          // Maintenance is off — don't strand anyone on the maintenance page.
          if (to.name === 'maintenance') return { name: 'login' }
        }
      }
    }

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

router.afterEach((to) => {
  void (async () => {
    const { useBrandingStore } = await import('@/stores/branding')
    const { i18n } = await import('@/i18n')
    const { getActivePinia } = await import('pinia')
    const pinia = getActivePinia()
    const name = pinia ? useBrandingStore(pinia).instanceName : 'Prohibitorum'
    const key = to.meta.titleKey
    const page = key ? i18n.global.t(key as string) : ''
    document.title = buildTitle(page, name)
  })()
})

export default router
