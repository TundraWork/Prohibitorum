import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import CopyButton from './CopyButton.vue'

const writeText = vi.fn(async () => {})
beforeEach(() => {
  writeText.mockClear()
  Object.assign(navigator, { clipboard: { writeText } })
})
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

const mountButton = () =>
  mount(CopyButton, {
    props: { value: 'UUID-1234', label: 'Copy OIDC subject' },
    global: { plugins: [i18n()] },
  })

describe('CopyButton', () => {
  it('writes the value to the clipboard when clicked', async () => {
    const w = mountButton()
    await w.find('[data-test="copy-button"]').trigger('click')
    expect(writeText).toHaveBeenCalledWith('UUID-1234')
  })

  it('announces an observable failure when clipboard access is blocked', async () => {
    writeText.mockRejectedValueOnce(new Error('blocked'))
    const w = mountButton()

    await w.find('[data-test="copy-button"]').trigger('click')
    await flushPromises()

    expect(w.find('[role="status"]').text()).toBe(en.common.copyFailed)
    expect(w.find('[role="status"]').classes()).not.toContain('sr-only')
  })

  it('keeps a later copy failure visible after an earlier success timer expires', async () => {
    vi.useFakeTimers()
    const w = mountButton()

    await w.find('[data-test="copy-button"]').trigger('click')
    await flushPromises()
    writeText.mockRejectedValueOnce(new Error('blocked'))
    await w.find('[data-test="copy-button"]').trigger('click')
    await flushPromises()
    await vi.advanceTimersByTimeAsync(1500)

    expect(w.find('[role="status"]').text()).toBe(en.common.copyFailed)
    vi.useRealTimers()
  })

  it('names the button for assistive tech with the given label', () => {
    const w = mountButton()
    expect(w.find('[data-test="copy-button"]').attributes('aria-label')).toBe('Copy OIDC subject')
  })
})
