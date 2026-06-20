import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import router from './router'
import { i18n } from './i18n'
import { registerUnauthorizedHandler } from './lib/api'
import { createUnauthorizedHandler } from './lib/sessionExpiry'
import { useAuthStore } from './stores/auth'
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

app.mount('#app')
