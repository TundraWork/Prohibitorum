import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import ThemeToggle from './ThemeToggle.vue'

if (!window.matchMedia) {
  // @ts-expect-error jsdom lacks matchMedia; useColorMode reads it for prefers-color-scheme
  window.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} })
}

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountToggle = () => mount(ThemeToggle, { global: { plugins: [i18n()] } })

describe('ThemeToggle', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.className = ''
  })

  it('renders three radio options', () => {
    const w = mountToggle()
    expect(w.findAll('[role="radio"]')).toHaveLength(3)
  })

  it('selecting Dark applies the dark theme and checks only that option', async () => {
    const w = mountToggle()
    await w.get('[data-test="theme-dark"]').trigger('click')
    await nextTick()
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(w.get('[data-test="theme-dark"]').attributes('aria-checked')).toBe('true')
    expect(w.get('[data-test="theme-light"]').attributes('aria-checked')).toBe('false')
    expect(w.get('[data-test="theme-system"]').attributes('aria-checked')).toBe('false')
  })

  it('selecting System checks System and persists auto', async () => {
    const w = mountToggle()
    await w.get('[data-test="theme-system"]').trigger('click')
    await nextTick()
    expect(w.get('[data-test="theme-system"]').attributes('aria-checked')).toBe('true')
    expect(localStorage.getItem('theme')).toBe('auto')
  })

  it('switching back to Light removes the dark class', async () => {
    const w = mountToggle()
    await w.get('[data-test="theme-dark"]').trigger('click'); await nextTick()
    await w.get('[data-test="theme-light"]').trigger('click'); await nextTick()
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(w.get('[data-test="theme-light"]').attributes('aria-checked')).toBe('true')
  })

  it('arrow keys move the selection (roving radiogroup)', async () => {
    const w = mountToggle()
    // initial stored = 'auto' (System, index 1); ArrowRight -> index 2 (Dark)
    await w.get('[role="radiogroup"]').trigger('keydown', { key: 'ArrowRight' })
    await nextTick()
    expect(localStorage.getItem('theme')).toBe('dark')
    expect(w.get('[data-test="theme-dark"]').attributes('aria-checked')).toBe('true')
  })
})
