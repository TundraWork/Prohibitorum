import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import TagInput from './TagInput.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const mountTag = (props: Record<string, unknown>) =>
  mount(TagInput, { props, global: { plugins: [i18n()] }, attachTo: document.body })

describe('TagInput', () => {
  it('commits a value on Enter', async () => {
    const w = mountTag({ modelValue: [] })
    const input = w.find('input')
    await input.setValue('https://a/cb')
    await input.trigger('keydown.enter')
    expect(w.emitted('update:modelValue')?.[0]).toEqual([['https://a/cb']])
  })

  it('renders existing items as chips and removes one', async () => {
    const w = mountTag({ modelValue: ['one', 'two'] })
    expect(w.find('[data-test="tag-one"]').exists()).toBe(true)
    await w.find('[data-test="tag-remove-one"]').trigger('click')
    expect(w.emitted('update:modelValue')?.[0]).toEqual([['two']])
  })

  it('splits a pasted newline/comma list', async () => {
    const w = mountTag({ modelValue: [] })
    await w.find('input').trigger('paste', {
      clipboardData: { getData: () => 'a\nb,c' },
    })
    expect(w.emitted('update:modelValue')?.[0]).toEqual([['a', 'b', 'c']])
  })

  it('removes the last chip on Backspace with an empty draft', async () => {
    const w = mountTag({ modelValue: ['a', 'b'] })
    await w.find('input').trigger('keydown', { key: 'Backspace' })
    expect(w.emitted('update:modelValue')?.[0]).toEqual([['a']])
  })

  it('flags an invalid item via the validate prop', () => {
    const w = mountTag({
      modelValue: ['ok', 'bad'],
      validate: (s: string) => (s === 'bad' ? 'Not allowed' : null),
    })
    expect(w.text()).toContain('Not allowed')
    expect(w.find('[data-test="tag-bad"]').classes().join(' ')).toContain('text-destructive')
  })

  it('does not add a duplicate', async () => {
    const w = mountTag({ modelValue: ['a'] })
    const input = w.find('input')
    await input.setValue('a')
    await input.trigger('keydown.enter')
    expect(w.emitted('update:modelValue')).toBeUndefined()
  })
})
