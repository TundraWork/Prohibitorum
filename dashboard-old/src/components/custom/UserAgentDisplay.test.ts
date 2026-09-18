import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import UserAgentDisplay from './UserAgentDisplay.vue'

const REAL_UA =
  'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36'

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

function mountComp(ua?: string) {
  return mount(UserAgentDisplay, {
    props: { ua },
    global: { plugins: [makeI18n()] },
  })
}

describe('UserAgentDisplay', () => {
  it('renders the humanized label and hides raw UA by default (collapsed)', () => {
    const w = mountComp(REAL_UA)
    expect(w.text()).toContain('Chrome on macOS')
    expect(w.text()).not.toContain(REAL_UA)
  })

  it('shows a toggle button with aria-expanded=false when ua is provided', () => {
    const w = mountComp(REAL_UA)
    const btn = w.find('button')
    expect(btn.exists()).toBe(true)
    expect(btn.attributes('aria-expanded')).toBe('false')
  })

  it('reveals the full raw UA after clicking the toggle button', async () => {
    const w = mountComp(REAL_UA)
    await w.find('button').trigger('click')
    expect(w.text()).toContain(REAL_UA)
  })

  it('flips aria-expanded to true after expanding', async () => {
    const w = mountComp(REAL_UA)
    const btn = w.find('button')
    await btn.trigger('click')
    expect(btn.attributes('aria-expanded')).toBe('true')
  })

  it('collapses again on a second click', async () => {
    const w = mountComp(REAL_UA)
    const btn = w.find('button')
    await btn.trigger('click')
    await btn.trigger('click')
    expect(w.text()).not.toContain(REAL_UA)
    expect(btn.attributes('aria-expanded')).toBe('false')
  })

  it('renders the fallback label and no toggle when ua is empty/undefined', () => {
    const w = mountComp(undefined)
    expect(w.text()).toContain('Unknown device')
    expect(w.find('button').exists()).toBe(false)
  })

  it('renders the fallback label and no toggle when ua is an empty string', () => {
    const w = mountComp('')
    expect(w.text()).toContain('Unknown device')
    expect(w.find('button').exists()).toBe(false)
  })

  it('toggle button aria-label uses the i18n toggle key when collapsed', () => {
    const w = mountComp(REAL_UA)
    expect(w.find('button').attributes('aria-label')).toBe(en.userAgent.toggle)
  })

  it('toggle button aria-label uses the i18n hide key when expanded', async () => {
    const w = mountComp(REAL_UA)
    await w.find('button').trigger('click')
    expect(w.find('button').attributes('aria-label')).toBe(en.userAgent.hide)
  })
})
