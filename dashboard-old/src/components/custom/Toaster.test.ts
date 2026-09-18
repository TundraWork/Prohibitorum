import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { nextTick } from 'vue'
import en from '@/locales/en'
import Toaster from './Toaster.vue'
import { pushToast, clearToasts, toasts } from '@/lib/toast'

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
}

describe('Toaster', () => {
  beforeEach(() => clearToasts())
  afterEach(() => clearToasts())

  it('renders a toast message when one is pushed', async () => {
    const w = mount(Toaster, { global: { plugins: [makeI18n()] } })
    pushToast({ variant: 'error', message: 'Server error', timeoutMs: 0 })
    await nextTick()
    expect(w.text()).toContain('Server error')
  })

  it('renders the title when provided', async () => {
    const w = mount(Toaster, { global: { plugins: [makeI18n()] } })
    pushToast({ variant: 'info', message: 'Body', title: 'A Title', timeoutMs: 0 })
    await nextTick()
    expect(w.text()).toContain('A Title')
    expect(w.text()).toContain('Body')
  })

  it('clicking the dismiss button removes the toast', async () => {
    const w = mount(Toaster, { global: { plugins: [makeI18n()] } })
    pushToast({ variant: 'info', message: 'Info msg', timeoutMs: 0 })
    await nextTick()
    expect(w.text()).toContain('Info msg')
    await w.find('button').trigger('click')
    await nextTick()
    expect(toasts).toHaveLength(0)
    expect(w.text()).not.toContain('Info msg')
  })

  it('an error variant toast has role=alert', async () => {
    const w = mount(Toaster, { global: { plugins: [makeI18n()] } })
    pushToast({ variant: 'error', message: 'Error!', timeoutMs: 0 })
    await nextTick()
    expect(w.find('[role="alert"]').exists()).toBe(true)
  })

  it('an info variant toast does not have role=alert', async () => {
    const w = mount(Toaster, { global: { plugins: [makeI18n()] } })
    pushToast({ variant: 'info', message: 'Hello', timeoutMs: 0 })
    await nextTick()
    expect(w.find('[role="alert"]').exists()).toBe(false)
  })

  it('the dismiss button carries the localized aria-label', async () => {
    const w = mount(Toaster, { global: { plugins: [makeI18n()] } })
    pushToast({ variant: 'success', message: 'Saved', timeoutMs: 0 })
    await nextTick()
    expect(w.find('button').attributes('aria-label')).toBe(en.common.dismiss)
  })
})
