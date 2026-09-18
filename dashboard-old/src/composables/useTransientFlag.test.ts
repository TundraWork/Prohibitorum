import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { useTransientFlag } from './useTransientFlag'

// Mount a test component that exposes the composable state via the instance.
// onUnmounted requires a component context, so we call useTransientFlag inside
// a mounted component — same pattern as useApi.test.ts.
function makeWrapper(ms?: number) {
  const TestComp = defineComponent({
    setup() {
      const { flag, trigger } = useTransientFlag(ms)
      return { flag, trigger }
    },
    template: '<div></div>',
  })
  return mount(TestComp)
}

beforeEach(() => { vi.useFakeTimers() })
afterEach(() => { vi.useRealTimers() })

describe('useTransientFlag', () => {
  it('starts with flag=false', () => {
    const w = makeWrapper()
    expect(w.vm.flag).toBe(false)
  })

  it('flag is true immediately after trigger()', () => {
    const w = makeWrapper()
    w.vm.trigger()
    expect(w.vm.flag).toBe(true)
  })

  it('flag auto-clears after the default 3000ms', () => {
    const w = makeWrapper()
    w.vm.trigger()
    expect(w.vm.flag).toBe(true)
    vi.advanceTimersByTime(3000)
    expect(w.vm.flag).toBe(false)
  })

  it('flag auto-clears after a custom ms value', () => {
    const w = makeWrapper(1000)
    w.vm.trigger()
    expect(w.vm.flag).toBe(true)
    vi.advanceTimersByTime(999)
    expect(w.vm.flag).toBe(true)
    vi.advanceTimersByTime(1)
    expect(w.vm.flag).toBe(false)
  })

  it('re-triggering resets the timer', () => {
    const w = makeWrapper(1000)
    w.vm.trigger()
    vi.advanceTimersByTime(800)
    // Still true, now re-trigger — should reset the 1s countdown
    w.vm.trigger()
    vi.advanceTimersByTime(800)
    // 800ms into the new 1s window — still true
    expect(w.vm.flag).toBe(true)
    vi.advanceTimersByTime(200)
    // 1000ms after the re-trigger — now clears
    expect(w.vm.flag).toBe(false)
  })

  it('clearTimer is called on unmount (no lingering timeout)', async () => {
    const clearTimeoutSpy = vi.spyOn(globalThis, 'clearTimeout')
    const w = makeWrapper(5000)
    w.vm.trigger()
    w.unmount()
    await flushPromises()
    // The timer should have been cleared on unmount
    expect(clearTimeoutSpy).toHaveBeenCalled()
    clearTimeoutSpy.mockRestore()
  })
})
