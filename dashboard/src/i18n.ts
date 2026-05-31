import { createI18n } from 'vue-i18n'
import zh from './locales/zh'
import en from './locales/en'

const stored = localStorage.getItem('locale')
export const i18n = createI18n({
  legacy: false,
  locale: stored ?? (navigator.language.startsWith('zh') ? 'zh' : 'en'),
  fallbackLocale: 'zh',
  messages: { zh, en },
})
