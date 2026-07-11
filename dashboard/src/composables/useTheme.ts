import { useColorMode } from '@vueuse/core'

export type ThemeMode = 'light' | 'dark' | 'auto'

/**
 * Single source of truth for the app theme. Persists the user's choice to
 * localStorage['theme'] and toggles `class="dark"` on <html>. `auto` follows
 * the OS prefers-color-scheme. Call once at app start (App.vue) so the class
 * applies on every route, including pre-login threshold pages.
 *
 * Returns:
 * - `mode`    the useColorMode ref; `mode.value` is the RESOLVED applied value
 *             ('light' | 'dark') that drives the <html> class.
 * - `stored`  the persisted SELECTION ref; `stored.value` is 'light' | 'dark'
 *             | 'auto'. Use this to know whether the user picked 'auto'
 *             (e.g. to highlight the System option in the toggle).
 * - `setMode` set the selection (writes localStorage + updates the class).
 *
 * Tailwind v4 dark mode keys off the `.dark` class only; `modes.light` maps to
 * '' so no stray `.light` class is added to <html>.
 *
 * Note: CSP is `script-src 'self'`, so there is no inline FOUC-prevention
 * script; the class is applied by this composable during app bootstrap.
 */
export function useTheme() {
  const mode = useColorMode({
    storageKey: 'theme',
    attribute: 'class',
    selector: 'html',
    modes: { light: '', dark: 'dark' },
    initialValue: 'auto',
    disableTransition: false,
  })
  function setMode(m: ThemeMode): void {
    mode.store.value = m
  }
  return { mode, stored: mode.store, setMode }
}
