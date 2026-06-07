import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import CodeField from './CodeField.vue'

const writeText = vi.fn(async () => {})
beforeEach(() => {
  writeText.mockClear()
  Object.assign(navigator, { clipboard: { writeText } })
})
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

describe('CodeField', () => {
  it('renders the value and copies it', async () => {
    const w = mount(CodeField, { props: { value: 'ABC123' }, global: { plugins: [i18n()] } })
    expect(w.find('code').text()).toBe('ABC123')
    await w.find('button').trigger('click')
    expect(writeText).toHaveBeenCalledWith('ABC123')
  })
})
