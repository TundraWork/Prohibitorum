import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import zh from '../locales/zh'
import en from '../locales/en'
import CopyableUrl from './CopyableUrl.vue'

const writeText = vi.fn().mockResolvedValue(undefined)
beforeEach(() => {
  writeText.mockClear()
  Object.assign(navigator, { clipboard: { writeText } })
})

function makeI18n() {
  return createI18n({ legacy: false, locale: 'en', fallbackLocale: 'zh', messages: { zh, en } })
}

describe('CopyableUrl', () => {
  it('copies the url to the clipboard', async () => {
    const wrapper = mount(CopyableUrl, { props: { url: 'http://x/enroll/t' }, global: { plugins: [makeI18n()] } })
    await wrapper.find('button').trigger('click')
    expect(writeText).toHaveBeenCalledWith('http://x/enroll/t')
  })
})
