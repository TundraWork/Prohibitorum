import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import LocaleSwitcher from './LocaleSwitcher.vue'
import { Select } from '@/components/ui/select'

/**
 * A two-locale i18n instance so the switch *mechanism* can be exercised even
 * though only `en` ships by default. The control is the vendored Reka Select,
 * so we drive it via the root Select's v-model (its menu is portaled and only
 * renders when open — see reka primitive idioms).
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
    const vm = wrapper.vm as unknown as { options: { value: string }[] }
    expect(vm.options.map((o) => o.value)).toEqual(['en', 'zh'])
  })

  it('binds the current locale to the Select', () => {
    const { wrapper } = mountSwitcher()
    expect(wrapper.findComponent(Select).props('modelValue')).toBe('en')
  })

  it('switches the global locale on selection', async () => {
    const { i18n, wrapper } = mountSwitcher()
    wrapper.findComponent(Select).vm.$emit('update:modelValue', 'zh')
    await wrapper.vm.$nextTick()
    // legacy:false → global.locale is a ref.
    expect((i18n.global.locale as unknown as { value: string }).value).toBe('zh')
  })

  it('labels the control for assistive tech', () => {
    const { wrapper } = mountSwitcher()
    expect(wrapper.find('[data-test="locale-trigger"]').attributes('aria-label')).toBe('Language')
  })

  it('can expose a 44px trigger on keyboard-first threshold pages', () => {
    const i18n = makeI18n()
    const wrapper = mount(LocaleSwitcher, {
      props: { largeTarget: true },
      global: { plugins: [i18n] },
    })

    expect(wrapper.get('[data-test="locale-trigger"]').classes()).toContain('h-11')
  })
})
