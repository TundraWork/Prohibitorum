import { describe, it, expect, beforeEach } from 'vitest'
import { nextTick } from 'vue'
import { i18n } from '@/i18n'
import { useLocale } from './useLocale'

function setNavLanguage(value: string) {
  Object.defineProperty(navigator, 'language', { value, configurable: true })
}

describe('useLocale', () => {
  beforeEach(() => {
    i18n.global.locale.value = 'en'
    localStorage.clear()
    document.documentElement.lang = 'en'
  })

  it('uses a valid stored locale', () => {
    localStorage.setItem('locale', 'zh')
    useLocale()
    expect(i18n.global.locale.value).toBe('zh')
    expect(document.documentElement.lang).toBe('zh')
  })

  it('detects zh from navigator.language when nothing stored', () => {
    setNavLanguage('zh-CN')
    useLocale()
    expect(i18n.global.locale.value).toBe('zh')
  })

  it('falls back to en for a non-zh browser', () => {
    setNavLanguage('en-US')
    useLocale()
    expect(i18n.global.locale.value).toBe('en')
  })

  it('ignores an unregistered stored value', () => {
    localStorage.setItem('locale', 'fr')
    setNavLanguage('en-US')
    useLocale()
    expect(i18n.global.locale.value).toBe('en')
  })

  it('persists a locale change', async () => {
    const { setLocale } = useLocale()
    setLocale('zh')
    await nextTick()
    expect(localStorage.getItem('locale')).toBe('zh')
    expect(document.documentElement.lang).toBe('zh')

    setLocale('en')
    await nextTick()
    expect(document.documentElement.lang).toBe('en')
  })
})
