import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import LocaleSwitcher from './LocaleSwitcher.vue'

/**
 * A two-locale i18n instance so the switch *mechanism* can be exercised even
 * though only `en` ships in Spec 1 (the production instance has one locale).
 */
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

function mountSwitcher(i18n = makeI18n()) {
  return { i18n, wrapper: mount(LocaleSwitcher, { global: { plugins: [i18n] } }) }
}

describe('LocaleSwitcher', () => {
  it('lists every available locale', () => {
    const { wrapper } = mountSwitcher()
    const values = wrapper.findAll('option').map((o) => o.attributes('value'))
    expect(values).toEqual(['en', 'zh'])
  })

  it('shows the current locale as selected', () => {
    const { wrapper } = mountSwitcher()
    expect((wrapper.find('select').element as HTMLSelectElement).value).toBe('en')
  })

  it('switches the global locale on selection', async () => {
    const { i18n, wrapper } = mountSwitcher()
    await wrapper.find('select').setValue('zh')
    // legacy:false → global.locale is a ref.
    expect((i18n.global.locale as unknown as { value: string }).value).toBe('zh')
  })

  it('labels the control for assistive tech', () => {
    const { wrapper } = mountSwitcher()
    expect(wrapper.find('select').attributes('aria-label')).toBe('Language')
  })
})
