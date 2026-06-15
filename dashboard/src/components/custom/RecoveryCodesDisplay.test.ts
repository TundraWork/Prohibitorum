import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import RecoveryCodesDisplay from './RecoveryCodesDisplay.vue'

const writeText = vi.fn(async () => {})
beforeEach(() => {
  writeText.mockClear()
  Object.assign(navigator, { clipboard: { writeText } })
  // @ts-expect-error test stub
  URL.createObjectURL = vi.fn(() => 'blob:x')
  // @ts-expect-error test stub
  URL.revokeObjectURL = vi.fn()
})
const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })
const CODES = ['aaaa-bbbb', 'cccc-dddd']

describe('RecoveryCodesDisplay', () => {
  it('renders codes and copies all', async () => {
    const w = mount(RecoveryCodesDisplay, { props: { codes: CODES }, global: { plugins: [i18n()] } })
    expect(w.findAll('li').map((l) => l.text())).toEqual(CODES)
    await w.findAll('button')[0].trigger('click')
    expect(writeText).toHaveBeenCalledWith('aaaa-bbbb\ncccc-dddd')
  })

  it('gates the Done emit behind the saved checkbox', async () => {
    const w = mount(RecoveryCodesDisplay, { props: { codes: CODES }, global: { plugins: [i18n()] } })
    const done = w.find('[data-test=done]')
    expect((done.element as HTMLButtonElement).disabled).toBe(true)
    await w.find('[data-test=saved]').trigger('click')
    expect((done.element as HTMLButtonElement).disabled).toBe(false)
    await done.trigger('click')
    expect(w.emitted('confirmed')).toBeTruthy()
  })

  it('shows the regenerated warning when flagged', () => {
    const w = mount(RecoveryCodesDisplay, { props: { codes: CODES, regenerated: true }, global: { plugins: [i18n()] } })
    expect(w.text()).toContain(en.recoveryCodes.regeneratedWarning)
  })

  it('shows copy-failed message when clipboard rejects', async () => {
    writeText.mockRejectedValueOnce(new Error('not allowed'))
    const w = mount(RecoveryCodesDisplay, { props: { codes: CODES }, global: { plugins: [i18n()] } })
    expect(w.find('[role="alert"]').exists()).toBe(false)
    await w.findAll('button')[0].trigger('click')
    await new Promise(r => setTimeout(r, 0)) // flush microtasks
    expect(w.find('[role="alert"]').text()).toContain(en.recoveryCodes.copyFailed)
  })

  it('clears copy-failed on next copy attempt', async () => {
    writeText.mockRejectedValueOnce(new Error('not allowed')).mockResolvedValueOnce(undefined)
    const w = mount(RecoveryCodesDisplay, { props: { codes: CODES }, global: { plugins: [i18n()] } })
    await w.findAll('button')[0].trigger('click')
    await new Promise(r => setTimeout(r, 0))
    expect(w.find('[role="alert"]').exists()).toBe(true)
    await w.findAll('button')[0].trigger('click')
    await new Promise(r => setTimeout(r, 0))
    expect(w.find('[role="alert"]').exists()).toBe(false)
  })
})
