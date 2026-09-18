import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import ConfirmDialog from './ConfirmDialog.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

describe('ConfirmDialog', () => {
  it('emits confirm and cancel; confirm button is destructive', async () => {
    const w = mount(ConfirmDialog, {
      props: { open: true, title: 'Remove X', confirmLabel: 'Remove X' },
      slots: { default: 'This cannot be undone.' },
      attachTo: document.body,
      global: { plugins: [i18n()] },
    })
    await flushPromises()
    const buttons = Array.from(document.body.querySelectorAll('button'))
    const confirm = buttons.find((b) => b.textContent?.includes('Remove X'))!
    const cancel = buttons.find((b) => b.textContent?.includes(en.confirm.cancel))!
    expect(confirm.getAttribute('data-variant')).toBe('destructive')
    confirm.click(); await flushPromises()
    expect(w.emitted('confirm')).toBeTruthy()
    cancel.click(); await flushPromises()
    expect(w.emitted('cancel')).toBeTruthy()
  })
})
