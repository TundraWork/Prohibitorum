import { describe, it, expect } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { defineComponent, type ComponentPublicInstance } from 'vue'
import { createI18n } from 'vue-i18n'
import LocaleMenuSection from './LocaleMenuSection.vue'
import { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent } from '@/components/ui/dropdown-menu'

/**
 * DropdownMenuSub requires the surrounding MenuRoot context, so the section is
 * mounted inside a real DropdownMenu host — the same hierarchy NavUser uses.
 * Reka portals the open menus to document.body: open the root menu, then the
 * submenu, then assert there (pointer-capture stubs per the LocaleSwitcher
 * large-target test idiom).
 */
const Host = defineComponent({
  components: { DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, LocaleMenuSection },
  template: `
    <DropdownMenu>
      <DropdownMenuTrigger as-child>
        <button type="button" data-test="menu-trigger">menu</button>
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <LocaleMenuSection />
      </DropdownMenuContent>
    </DropdownMenu>
  `,
})

function makeI18n() {
  return createI18n({
    legacy: false,
    locale: 'en',
    fallbackLocale: 'en',
    messages: {
      en: { common: { language: 'Language' } },
      zh: { common: { language: '语言' } },
    },
  })
}

function mountHost(i18n = makeI18n()) {
  return { i18n, wrapper: mount(Host, { attachTo: document.body, global: { plugins: [i18n] } }) }
}

async function openSubmenus(wrapper: VueWrapper<ComponentPublicInstance>) {
  await wrapper.get('[data-test="menu-trigger"]').trigger('click')
  await flushPromises()
  const subTrigger = document.body.querySelector('[data-test="locale-sub-trigger"]')!
  subTrigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
  await flushPromises()
}

describe('LocaleMenuSection', () => {
  it('lists every available locale in the submenu', async () => {
    const { wrapper } = mountHost()
    await openSubmenus(wrapper)
    const options = Array.from(document.body.querySelectorAll('[data-test="locale-option"]'))
    expect(options).toHaveLength(2)
    expect(options.map((o) => o.textContent?.trim())).toEqual(['English', '中文'])
    wrapper.unmount()
  })

  it('marks the current locale as checked', async () => {
    const { wrapper } = mountHost()
    await openSubmenus(wrapper)
    const options = Array.from(document.body.querySelectorAll('[data-test="locale-option"]'))
    const [en, zh] = options.map((o) => o.getAttribute('aria-checked'))
    expect(en).toBe('true')
    expect(zh).toBe('false')
    wrapper.unmount()
  })

  it('switches the global locale on selection', async () => {
    const { i18n, wrapper } = mountHost()
    await openSubmenus(wrapper)
    const zh = document.body.querySelectorAll('[data-test="locale-option"]')[1] as HTMLElement
    zh.click()
    await flushPromises()
    expect((i18n.global.locale as unknown as { value: string }).value).toBe('zh')
    wrapper.unmount()
  })
})
