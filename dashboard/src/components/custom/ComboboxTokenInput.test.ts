import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import ComboboxTokenInput from './ComboboxTokenInput.vue'

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

const SUGGESTIONS = [
  { value: 'openid', description: 'Required. Returns the subject identifier.' },
  { value: 'profile', description: 'Basic profile: name, picture, locale.' },
  { value: 'email', description: 'Email address and its verified flag.' },
  { value: 'offline_access', description: 'Issues a refresh token for offline access.' },
]

const mountCmp = (props: Record<string, unknown>) =>
  mount(ComboboxTokenInput, {
    props: { suggestions: SUGGESTIONS, ...props },
    global: { plugins: [i18n()] },
    attachTo: document.body,
  })

describe('ComboboxTokenInput', () => {
  it('renders existing modelValue as chips', () => {
    const w = mountCmp({ modelValue: ['openid', 'email'] })
    expect(w.find('[data-test="tag-openid"]').exists()).toBe(true)
    expect(w.find('[data-test="tag-email"]').exists()).toBe(true)
  })

  it('has combobox semantics on the input', () => {
    const w = mountCmp({ modelValue: [] })
    const input = w.find('input')
    expect(input.attributes('role')).toBe('combobox')
    expect(input.attributes('aria-expanded')).toBeDefined()
  })

  it('filters the suggestion list as the draft is typed and shows descriptions', async () => {
    const w = mountCmp({ modelValue: [] })
    const input = w.find('input')
    await input.trigger('focus')
    await input.setValue('off')
    // Only the offline_access suggestion matches "off".
    expect(w.find('[data-test="scope-option-offline_access"]').exists()).toBe(true)
    expect(w.find('[data-test="scope-option-openid"]').exists()).toBe(false)
    expect(w.find('[data-test="scope-option-email"]').exists()).toBe(false)
    // Description renders alongside the value.
    expect(w.find('[data-test="scope-option-offline_access"]').text()).toContain('refresh token')
  })

  it('adds a suggestion as a chip on click and clears the draft', async () => {
    const w = mountCmp({ modelValue: [] })
    const input = w.find('input')
    await input.trigger('focus')
    await input.setValue('open')
    await w.find('[data-test="scope-option-openid"]').trigger('click')
    expect(w.emitted('update:modelValue')?.[0]).toEqual([['openid']])
    expect((input.element as HTMLInputElement).value).toBe('')
  })

  it('adds a CUSTOM value not in suggestions on Enter', async () => {
    const w = mountCmp({ modelValue: [] })
    const input = w.find('input')
    await input.setValue('custom_scope')
    await input.trigger('keydown.enter')
    expect(w.emitted('update:modelValue')?.[0]).toEqual([['custom_scope']])
  })

  it('adds a custom value on comma', async () => {
    const w = mountCmp({ modelValue: [] })
    const input = w.find('input')
    await input.setValue('foo')
    await input.trigger('keydown', { key: ',' })
    expect(w.emitted('update:modelValue')?.[0]).toEqual([['foo']])
  })

  it('excludes already-selected suggestions from the dropdown', async () => {
    const w = mountCmp({ modelValue: ['openid'] })
    const input = w.find('input')
    await input.trigger('focus')
    await input.setValue('')
    expect(w.find('[data-test="scope-option-openid"]').exists()).toBe(false)
    expect(w.find('[data-test="scope-option-email"]').exists()).toBe(true)
  })

  it('removes a chip via the remove button', async () => {
    const w = mountCmp({ modelValue: ['openid', 'email'] })
    await w.find('[data-test="tag-remove-openid"]').trigger('click')
    expect(w.emitted('update:modelValue')?.[0]).toEqual([['email']])
  })

  it('removes the last chip on Backspace with an empty draft', async () => {
    const w = mountCmp({ modelValue: ['openid', 'email'] })
    await w.find('input').trigger('keydown', { key: 'Backspace' })
    expect(w.emitted('update:modelValue')?.[0]).toEqual([['openid']])
  })

  it('splits a pasted newline/comma list', async () => {
    const w = mountCmp({ modelValue: [] })
    await w.find('input').trigger('paste', { clipboardData: { getData: () => 'a\nb,c' } })
    expect(w.emitted('update:modelValue')?.[0]).toEqual([['a', 'b', 'c']])
  })

  it('does not add a duplicate', async () => {
    const w = mountCmp({ modelValue: ['openid'] })
    const input = w.find('input')
    await input.setValue('openid')
    await input.trigger('keydown.enter')
    expect(w.emitted('update:modelValue')).toBeUndefined()
  })

  it('flags an invalid item via the validate prop', () => {
    const w = mountCmp({
      modelValue: ['openid', 'bad scope'],
      validate: (s: string) => (s.includes(' ') ? 'No spaces' : null),
    })
    expect(w.text()).toContain('No spaces')
    expect(w.find('[data-test="tag-bad scope"]').classes().join(' ')).toContain('text-destructive')
  })

  it('ArrowDown then Enter selects the active suggestion (not the draft)', async () => {
    const w = mountCmp({ modelValue: [] })
    const input = w.find('input')
    await input.trigger('focus')                       // opens the list (all 4 suggestions)
    await input.trigger('keydown', { key: 'ArrowDown' }) // activeIndex 0 -> openid
    await input.trigger('keydown.enter')
    expect(w.emitted('update:modelValue')?.[0]).toEqual([['openid']])
  })

  it('updates aria-activedescendant as ArrowDown moves the active option', async () => {
    const w = mountCmp({ modelValue: [], inputId: 'scopes' })
    const input = w.find('input')
    await input.trigger('focus')
    expect(input.attributes('aria-activedescendant')).toBeUndefined() // nothing active yet
    await input.trigger('keydown', { key: 'ArrowDown' })
    expect(input.attributes('aria-activedescendant')).toBe('scopes-listbox-openid')
  })

  it('closes the list on Escape (aria-expanded flips to false)', async () => {
    const w = mountCmp({ modelValue: [] })
    const input = w.find('input')
    await input.trigger('focus')
    expect(input.attributes('aria-expanded')).toBe('true')
    await input.trigger('keydown', { key: 'Escape' })
    expect(input.attributes('aria-expanded')).toBe('false')
  })
})
