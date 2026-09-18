import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import CodeBlock from './CodeBlock.vue'

const writeText = vi.fn(async () => {})
beforeEach(() => {
  writeText.mockClear()
  Object.assign(navigator, { clipboard: { writeText } })
})
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

describe('CodeBlock', () => {
  it('renders the value inside the block', () => {
    const w = mount(CodeBlock, { props: { value: 'hello\nworld' }, global: { plugins: [i18n()] } })
    expect(w.find('pre').text()).toBe('hello\nworld')
  })

  it('clicking copy calls clipboard.writeText with the value', async () => {
    const w = mount(CodeBlock, { props: { value: 'hello\nworld' }, global: { plugins: [i18n()] } })
    await w.find('button').trigger('click')
    expect(writeText).toHaveBeenCalledWith('hello\nworld')
  })
})
