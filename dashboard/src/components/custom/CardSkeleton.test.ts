import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import CardSkeleton from './CardSkeleton.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

describe('CardSkeleton', () => {
  it('renders title bar + lines skeleton bars with aria-busy', () => {
    const w = mount(CardSkeleton, {
      props: { lines: 3 },
      global: { plugins: [i18n()] },
    })
    expect(w.attributes('role')).toBe('status')
    expect(w.attributes('aria-busy')).toBe('true')
    // 1 title bar + 3 line bars = 4 total
    const bars = w.findAll('[data-slot="skeleton"]')
    expect(bars).toHaveLength(1 + 3)
  })

  it('uses default lines=4 when no props given', () => {
    const w = mount(CardSkeleton, { global: { plugins: [i18n()] } })
    // 1 title + 4 lines = 5
    expect(w.findAll('[data-slot="skeleton"]')).toHaveLength(1 + 4)
  })

  it('includes sr-only loading label', () => {
    const w = mount(CardSkeleton, { global: { plugins: [i18n()] } })
    const sr = w.find('.sr-only')
    expect(sr.exists()).toBe(true)
    expect(sr.text()).toBe(en.common.loading)
  })
})
