import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import TableSkeleton from './TableSkeleton.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

describe('TableSkeleton', () => {
  it('renders rows * cols skeleton bars with aria-busy', () => {
    const w = mount(TableSkeleton, {
      props: { rows: 3, cols: 4 },
      global: { plugins: [i18n()] },
    })
    expect(w.attributes('role')).toBe('status')
    expect(w.attributes('aria-busy')).toBe('true')
    const bars = w.findAll('[data-slot="skeleton"]')
    expect(bars).toHaveLength(3 * 4)
  })

  it('uses default rows=5 cols=4 when no props given', () => {
    const w = mount(TableSkeleton, { global: { plugins: [i18n()] } })
    expect(w.findAll('[data-slot="skeleton"]')).toHaveLength(5 * 4)
  })

  it('includes sr-only loading label', () => {
    const w = mount(TableSkeleton, { global: { plugins: [i18n()] } })
    const sr = w.find('.sr-only')
    expect(sr.exists()).toBe(true)
    expect(sr.text()).toBe(en.common.loading)
  })
})
