import { describe, it, expect, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import en from '@/locales/en'
import OfflineBanner from './OfflineBanner.vue'

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

function setOnLine(value: boolean): void {
  Object.defineProperty(window.navigator, 'onLine', { configurable: true, value })
}

describe('OfflineBanner', () => {
  const original = window.navigator.onLine

  afterEach(() => {
    setOnLine(original)
  })

  it('renders the banner with the offline message when offline', () => {
    setOnLine(false)
    const w = mount(OfflineBanner, { global: { plugins: [makeI18n()] } })
    expect(w.find('[role="status"]').exists()).toBe(true)
    expect(w.text()).toContain(en.offline.message)
  })

  it('is hidden when online', () => {
    setOnLine(true)
    const w = mount(OfflineBanner, { global: { plugins: [makeI18n()] } })
    expect(w.find('[role="status"]').exists()).toBe(false)
  })

  it('shows on the offline event and hides on the online event', async () => {
    setOnLine(true)
    const w = mount(OfflineBanner, { global: { plugins: [makeI18n()] } })
    expect(w.find('[role="status"]').exists()).toBe(false)

    setOnLine(false)
    window.dispatchEvent(new Event('offline'))
    await nextTick()
    expect(w.find('[role="status"]').exists()).toBe(true)

    setOnLine(true)
    window.dispatchEvent(new Event('online'))
    await nextTick()
    expect(w.find('[role="status"]').exists()).toBe(false)
  })
})
