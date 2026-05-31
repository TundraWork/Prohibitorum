import { createRouter, createWebHistory } from 'vue-router'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('./pages/LoginView.vue') },
    { path: '/consent', name: 'consent', component: () => import('./pages/ConsentView.vue') },
    { path: '/logout', name: 'logout', component: () => import('./pages/LogoutView.vue') },
    { path: '/error', name: 'error', component: () => import('./pages/ErrorView.vue') },
    { path: '/:pathMatch(.*)*', redirect: '/login' },
  ],
})
