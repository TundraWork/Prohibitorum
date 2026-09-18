import { describe, it, expect, beforeEach } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, nextTick, type ComponentPublicInstance } from 'vue'
import { createI18n } from 'vue-i18n'
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent } from '@/components/ui/dropdown-menu'
import en from '@/locales/en'
import ThemeMenuSection from './ThemeMenuSection.vue'

if (!window.matchMedia) {
  // @ts-expect-error jsdom lacks matchMedia; useColorMode reads it for prefers-color-scheme
  window.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })
}

/**
 * DropdownMenuSub requires the surrounding MenuRoot context, so the section is
 * mounted inside a real DropdownMenu host — the same hierarchy NavUser uses.
 * Reka portals the open menus to document.body: open the root menu, then the
 * submenu, then assert there (pointer-capture stubs per the LocaleSwitcher
 * large-target test idiom).
 */
const Host = defineComponent({
  components: { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, ThemeMenuSection },
  template: `
    <DropdownMenu>
      <DropdownMenuTrigger as-child>
        <button type="button" data-test="menu-trigger">menu</button>
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <ThemeMenuSection />
      </DropdownMenuContent>
    </DropdownMenu>
  `,
})
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountHost = () => mount(Host, { attachTo: document.body, global: { plugins: [i18n()] } })

async function openSubmenus(wrapper: VueWrapper<ComponentPublicInstance>) {
  await wrapper.get('[data-test="menu-trigger"]').trigger('click')
  await flushPromises()
  const subTrigger = document.body.querySelector('[data-test="theme-sub-trigger"]')!
  subTrigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
  await flushPromises()
}

describe('ThemeMenuSection', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.className = ''
    document.body.innerHTML = ''
  })

  it('renders the three theme options in the submenu', async () => {
    const w = mountHost()
    await openSubmenus(w)
    const options = Array.from(document.body.querySelectorAll('[role="menuitemradio"]'))
    expect(options.map((o) => o.getAttribute('data-test'))).toEqual(['theme-light', 'theme-system', 'theme-dark'])
    w.unmount()
  })

  it('selecting Dark applies the dark theme, persists it, and checks only that option', async () => {
    const w = mountHost()
    await openSubmenus(w)
    // Baseline: before any click, System ('auto' default) is the checked one.
    expect(Array.from(document.body.querySelectorAll('[role="menuitemradio"]'))
      .map((o) => o.getAttribute('aria-checked'))).toEqual(['false', 'true', 'false'])
    const dark = document.body.querySelector('[data-test="theme-dark"]') as HTMLElement
    dark.click()
    await flushPromises()
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(localStorage.getItem('theme')).toBe('dark')
    w.unmount()
  })

  it('selecting System persists auto and checks that option', async () => {
    const w = mountHost()
    await openSubmenus(w)
    // Clicking an item closes the menu, so read the checked state BEFORE the click.
    const checkedBefore = Array.from(document.body.querySelectorAll('[role="menuitemradio"]'))
      .map((o) => o.getAttribute('aria-checked'))
    expect(checkedBefore).toEqual(['false', 'true', 'false'])
    const system = document.body.querySelector('[data-test="theme-system"]') as HTMLElement
    system.click()
    await flushPromises()
    expect(localStorage.getItem('theme')).toBe('auto')
    w.unmount()
  })
})
