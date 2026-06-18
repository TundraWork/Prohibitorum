import { describe, it, expect, beforeEach } from 'vitest'
import { nextTick } from 'vue'
import { useTheme } from './useTheme'

describe('useTheme', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.className = ''
  })

  it('applies the dark class when set to dark', async () => {
    const { setMode } = useTheme()
    setMode('dark')
    await nextTick()
    expect(document.documentElement.classList.contains('dark')).toBe(true)
    expect(localStorage.getItem('theme')).toBe('dark')
  })

  it('removes the dark class when set to light', async () => {
    const { setMode } = useTheme()
    setMode('dark'); await nextTick()
    setMode('light'); await nextTick()
    expect(document.documentElement.classList.contains('dark')).toBe(false)
    expect(localStorage.getItem('theme')).toBe('light')
  })

  it('persists the auto selection and surfaces it via stored', async () => {
    // The actual OS-preference resolution for 'auto' is driven by a
    // prefers-color-scheme media query, which jsdom does not fire — so we
    // verify the SELECTION is stored/surfaced, not the resolved class here.
    const { stored, setMode } = useTheme()
    setMode('auto')
    await nextTick()
    expect(stored.value).toBe('auto')
    expect(localStorage.getItem('theme')).toBe('auto')
  })
})
