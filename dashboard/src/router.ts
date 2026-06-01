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
        { path: 'sessions', name: 'sessions', component: () => import('./pages/SessionsView.vue'), meta: { requiresAuth: true } },
        { path: 'credentials', name: 'credentials', component: () => import('./pages/CredentialsView.vue'), meta: { requiresAuth: true } },
        { path: 'admin/accounts', name: 'admin-accounts', component: () => import('./pages/AccountsView.vue'), meta: { requiresAuth: true, requiresAdmin: true } },
        { path: 'admin/invitations', name: 'admin-invitations', component: () => import('./pages/InvitationsView.vue'), meta: { requiresAuth: true, requiresAdmin: true } },
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
