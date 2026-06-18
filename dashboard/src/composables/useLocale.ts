import { watch } from 'vue'
import { i18n } from '@/i18n'

const STORAGE_KEY = 'locale'

/**
 * Single source of truth for the app locale. Persists the user's choice to
 * localStorage['locale']; on first load with no stored choice, detects the
 * browser language (zh* -> 'zh', else 'en'). Call once at app start (App.vue)
 * so it applies on every route, including pre-login pages. LocaleSwitcher binds
 * the global locale directly; the watcher here persists whatever it sets.
 *
 * CSP is `script-src 'self'`, so the locale is applied during Vue bootstrap
 * (no inline pre-paint script); text reflow is instant.
 */
function resolveInitial(): string {
  const available = i18n.global.availableLocales as string[]
  const stored = localStorage.getItem(STORAGE_KEY)
  if (stored && available.includes(stored)) return stored
  const nav = (navigator.language ?? '').toLowerCase()
  if (nav.startsWith('zh')) return 'zh'
  return 'en'
}

// vue-i18n types locale as WritableComputedRef<'en' | 'zh'> (the literal union of
// registered message keys). Assignments from string-typed helpers need a cast;
// resolveInitial() only returns registered locales so the cast is safe.
type Locale = (typeof i18n.global.locale)['value']

export function useLocale() {
  const locale = i18n.global.locale
  const initial = resolveInitial() as Locale
  if (locale.value !== initial) locale.value = initial
  watch(locale, (v) => {
    try { localStorage.setItem(STORAGE_KEY, v) } catch { /* storage unavailable */ }
  }, { flush: 'sync' })
  function setLocale(l: string): void { locale.value = l as Locale }
  return { locale, setLocale }
}
