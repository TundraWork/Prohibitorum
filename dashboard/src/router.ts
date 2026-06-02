import { createRouter, createWebHistory, type Router } from 'vue-router'
import { useSessionStore } from './stores/session'
import { isDevMode } from './lib/devMode'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    // In-layout dashboard routes (sidebar shell).
    {
      path: '/',
      component: () => import('./pages/DashboardLayout.vue'),
      children: [
        { path: '', name: 'profile', component: () => import('./pages/ProfileView.vue'), meta: { requiresAuth: true } },
        { path: 'security', name: 'security', component: () => import('./pages/SecurityView.vue'), meta: { requiresAuth: true } },
        { path: 'sessions', name: 'sessions', component: () => import('./pages/SessionsView.vue'), meta: { requiresAuth: true } },
        { path: 'connected', name: 'connected', component: () => import('./pages/ConnectedAccountsView.vue'), meta: { requiresAuth: true } },
        { path: 'devices', name: 'devices', component: () => import('./pages/DevicesView.vue'), meta: { requiresAuth: true } },
        { path: 'admin/accounts', name: 'admin-accounts', component: () => import('./pages/AccountsView.vue'), meta: { requiresAuth: true, requiresAdmin: true } },
        { path: 'admin/accounts/:id', name: 'admin-account-detail', component: () => import('./pages/AccountDetailView.vue'), meta: { requiresAuth: true, requiresAdmin: true } },
        { path: 'admin/invitations', name: 'admin-invitations', component: () => import('./pages/InvitationsView.vue'), meta: { requiresAuth: true, requiresAdmin: true } },
        { path: 'admin/oidc-clients', component: () => import('./pages/PlaceholderView.vue'), meta: { requiresAuth: true, requiresAdmin: true, title: 'OIDC clients', summary: 'Register and manage downstream OIDC relying parties (client IDs, secrets, redirect URIs, rotation).' } },
        { path: 'admin/saml-providers', component: () => import('./pages/PlaceholderView.vue'), meta: { requiresAuth: true, requiresAdmin: true, title: 'SAML service providers', summary: 'Register SAML SPs, manage metadata, ACS URLs, and signing certificates.' } },
        { path: 'admin/signing-keys', component: () => import('./pages/PlaceholderView.vue'), meta: { requiresAuth: true, requiresAdmin: true, title: 'Signing keys', summary: 'View, generate, and rotate the OIDC/SAML signing keys.' } },
        { path: 'admin/audit', component: () => import('./pages/PlaceholderView.vue'), meta: { requiresAuth: true, requiresAdmin: true, title: 'Audit log', summary: 'Browse and export credential and security events.' } },
        { path: 'admin/settings', component: () => import('./pages/PlaceholderView.vue'), meta: { requiresAuth: true, requiresAdmin: true, title: 'Settings', summary: 'Issuer, WebAuthn RP, TOTP issuer, allowed origins (read-only for now).' } },
        { path: 'credentials', redirect: '/security' },
      ],
    },
    // Public auth pages (centered card chrome). Pathless parent + absolute child
    // paths is the canonical vue-router layout pattern (no '/' collision).
    {
      path: '',
      component: () => import('./pages/CenteredLayout.vue'),
      children: [
        { path: '/login', name: 'login', component: () => import('./pages/LoginView.vue') },
        { path: '/consent', name: 'consent', component: () => import('./pages/ConsentView.vue') },
        { path: '/logout', name: 'logout', component: () => import('./pages/LogoutView.vue') },
        { path: '/error', name: 'error', component: () => import('./pages/ErrorView.vue') },
      ],
    },
    // Public, no layout.
    { path: '/enroll/:token', name: 'enroll', component: () => import('./pages/EnrollView.vue') },
    // Dev-only console (lazy chunk; guarded off in real deployments — see installGuard).
    { path: '/dev', name: 'dev', component: () => import('./pages/DevIndexView.vue') },
    // Unknown paths fall back to the dashboard; the guard bounces to /login if unauthenticated.
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

// Installable guard (exported for tests). requiresAuth → ensure a session;
// requiresAdmin → also require role==='admin'.
export function installGuard(r: Router) {
  r.beforeEach(async (to) => {
    // Dev console is unreachable outside dev (loopback host / vite dev server).
    if (to.path === '/dev' && !isDevMode()) return { path: '/' }
    if (!to.meta.requiresAuth) return true
    const session = useSessionStore()
    await session.ensureLoaded()
    if (!session.me) return { path: '/login', query: { return_to: to.fullPath } }
    if (to.meta.requiresAdmin && !session.isAdmin) return { path: '/' }
    return true
  })
}

installGuard(router)
