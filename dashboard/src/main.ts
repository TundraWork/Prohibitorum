import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ui from '@nuxt/ui/vue-plugin'
import App from './App.vue'
import { router } from './router'
import { i18n } from './i18n'
import './assets/main.css'

createApp(App).use(createPinia()).use(router).use(i18n).use(ui).mount('#app')
