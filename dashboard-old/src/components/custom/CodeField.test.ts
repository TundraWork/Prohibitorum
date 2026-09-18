import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
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

  it('announces an observable failure when clipboard access is blocked', async () => {
    writeText.mockRejectedValueOnce(new Error('blocked'))
    const w = mount(CodeField, {
      props: { value: 'ABC123', copyLabel: 'Copy one-time proof URL' },
      global: { plugins: [i18n()] },
    })

    await w.find('button').trigger('click')
    await flushPromises()

    expect(w.find('[role="status"]').text()).toBe('Copy failed. Select and copy the value manually.')
    expect(w.find('code').text()).toBe('ABC123')
    expect(w.find('button').attributes('aria-label')).toBe('Copy one-time proof URL')
  })

  it('keeps a later copy failure visible after an earlier success timer expires', async () => {
    vi.useFakeTimers()
    const w = mount(CodeField, {
      props: { value: 'ABC123' },
      global: { plugins: [i18n()] },
    })

    await w.find('button').trigger('click')
    await flushPromises()
    writeText.mockRejectedValueOnce(new Error('blocked'))
    await w.find('button').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1500)

    expect(w.find('[role="status"]').text()).toBe('Copy failed. Select and copy the value manually.')
    vi.useRealTimers()
  })
})
