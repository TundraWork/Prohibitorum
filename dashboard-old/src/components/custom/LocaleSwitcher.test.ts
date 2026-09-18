import { describe, it, expect } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
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

  it('can expose 44px trigger and menu-item targets on keyboard-first threshold pages', async () => {
    const i18n = makeI18n()
    const wrapper = mount(LocaleSwitcher, {
      props: { largeTarget: true },
      global: { plugins: [i18n] },
    })

    expect(wrapper.get('[data-test="locale-trigger"]').classes()).toContain('h-11')

    const trigger = wrapper.get('[data-test="locale-trigger"]')
    Object.assign(trigger.element, {
      hasPointerCapture: () => false,
      setPointerCapture: () => {},
      releasePointerCapture: () => {},
    })
    await trigger.trigger('pointerdown', { button: 0, ctrlKey: false, pointerId: 1, pointerType: 'mouse' })
    await flushPromises()
    const options = Array.from(document.body.querySelectorAll('[data-test="locale-option"]'))
    expect(options).toHaveLength(2)
    expect(options.every((option) => option.classList.contains('min-h-11'))).toBe(true)
    wrapper.unmount()
  })
})
