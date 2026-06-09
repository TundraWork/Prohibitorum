import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import ListInput from './ListInput.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountList = (props: Record<string, unknown>) =>
  mount(ListInput, {
    props: { name: 'uri', addLabel: 'Add URI', ...props },
    global: { plugins: [i18n()] },
    attachTo: document.body,
  })

describe('ListInput', () => {
  it('renders a full-width input per existing value', () => {
    const w = mountList({ modelValue: ['https://a/cb', 'https://b/cb'] })
    expect(w.find('[data-test="uri-input-0"]').exists()).toBe(true)
    expect((w.find('[data-test="uri-input-1"]').element as HTMLInputElement).value).toBe('https://b/cb')
  })

  it('adds a row and emits the typed value', async () => {
    const w = mountList({ modelValue: [] })
    await w.find('[data-test="uri-add"]').trigger('click')
    await w.find('[data-test="uri-input-0"]').setValue('https://n/cb')
    expect(w.emitted('update:modelValue')?.at(-1)).toEqual([['https://n/cb']])
  })

  it('removes a row', async () => {
    const w = mountList({ modelValue: ['a', 'b'] })
    await w.find('[data-test="uri-remove-0"]').trigger('click')
    expect(w.emitted('update:modelValue')?.at(-1)).toEqual([['b']])
  })

  it('flags an invalid row via validate and omits empty rows from the emitted value', async () => {
    const w = mountList({ modelValue: ['ok'], validate: (s: string) => (s === 'bad' ? 'Nope' : null) })
    await w.find('[data-test="uri-add"]').trigger('click')
    await w.find('[data-test="uri-input-1"]').setValue('bad')
    expect(w.text()).toContain('Nope')
    expect(w.find('[data-test="uri-input-1"]').attributes('aria-invalid')).toBe('true')
    // empty rows are not emitted; invalid-but-present values are
    expect(w.emitted('update:modelValue')?.at(-1)).toEqual([['ok', 'bad']])
  })
})
