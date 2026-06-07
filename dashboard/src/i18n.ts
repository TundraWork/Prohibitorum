import { createI18n } from 'vue-i18n'
import en from './locales/en'

/**
 * English-first i18n instance.
 *
 * - Composition API mode (legacy: false) — use useI18n() in components.
 * - zh locale strings are deferred; the structure is ready for them.
 *   Add: import zh from './locales/zh' and include in messages when ready.
 * - The exported `i18n` is installed on the app in main.ts.
 */
export const i18n = createI18n({
  legacy: false,
  locale: 'en',
  fallbackLocale: 'en',
  messages: {
    en,
    // zh: zh,  // TODO(i18n): add when zh strings are authored
  },
})
