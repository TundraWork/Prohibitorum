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
import { pushToast } from './lib/toast'
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

registerConnectionErrorHandler((err) => {
  // A dropped/hung server or a 5xx surfaces as a global, non-blocking toast.
  // Dedup by code so a burst of failing requests collapses into one toast.
  const t = i18n.global.t
  const code = err.code === 'network_error' ? 'errors.network_error' : 'errors.server_error'
  pushToast({ variant: 'error', message: t(code), key: err.code })
})

app.mount('#app')
