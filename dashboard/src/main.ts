import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import { i18n } from './i18n'
import {
  registerUnauthorizedHandler,
  registerMaintenanceHandler,
  registerConnectionErrorHandler,
} from './lib/api'
import { createUnauthorizedHandler } from './lib/sessionExpiry'
import { useAuthStore } from './stores/auth'
import { useBrandingStore } from './stores/branding'
import { useSessionExpiry } from './composables/useSessionExpiry'
import './assets/main.css'

const app = createApp(App)
const pinia = createPinia()
app.use(pinia).use(router).use(i18n)

registerUnauthorizedHandler(
  createUnauthorizedHandler({
    router,
    clearAuth: () => useAuthStore(pinia).clear(),
    setExpiredFlag: () => useSessionExpiry().trigger(),
  }),
)

registerMaintenanceHandler(() => {
  // A 503 maintenance_mode only ever reaches a non-admin (admins are exempt).
  // Set the flag so the banner and guard reflect the new state, then redirect.
  useBrandingStore(pinia).maintenanceMode = true
  void router.push({ name: 'maintenance' })
})

registerConnectionErrorHandler(() => {
  // A dropped/hung server or a 5xx previously surfaced as a global timed
  // toast. Per Task 4, API failures are now persistent and code-driven via
  // ErrorPanel — the connection-error handler no longer pushes a toast.
  // Success/info toasts remain (they are not API failures). The error is
  // surfaced inline by useApi → ErrorPanel wherever the failing request was
  // issued. This handler is retained as a seam for future global connectivity
  // indicators but does not display a transient toast.
})

app.mount('#app')
