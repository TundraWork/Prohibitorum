import { createI18n } from 'vue-i18n'
import en from './locales/en'
import zh from './locales/zh'

/**
 * English-first i18n instance. en is the source of truth; zh is key-parallel.
 * Composition API mode (legacy: false) — use useI18n() in components.
 */
export const i18n = createI18n({
  legacy: false,
  locale: 'en',
  fallbackLocale: 'en',
  messages: {
    en,
    zh,
  },
})
