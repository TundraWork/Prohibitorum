import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { toasts, pushToast, dismissToast, clearToasts } from './toast'

describe('toast store', () => {
  beforeEach(() => {
    clearToasts()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    clearToasts()
  })

  it('push adds a toast to the array', () => {
    const id = pushToast({ variant: 'error', message: 'Oops', title: 'Heads up' })
    expect(toasts).toHaveLength(1)
    expect(toasts[0]).toMatchObject({ id, variant: 'error', message: 'Oops', title: 'Heads up' })
  })

  it('auto-dismisses after timeoutMs (default 6000)', () => {
    pushToast({ variant: 'info', message: 'Hello' })
    expect(toasts).toHaveLength(1)
    vi.advanceTimersByTime(5999)
    expect(toasts).toHaveLength(1)
    vi.advanceTimersByTime(2)
    expect(toasts).toHaveLength(0)
  })

  it('timeoutMs: 0 makes a sticky toast (no auto-dismiss)', () => {
    pushToast({ variant: 'error', message: 'Sticky', timeoutMs: 0 })
    vi.advanceTimersByTime(60000)
    expect(toasts).toHaveLength(1)
  })

  it('dedup by key returns the same id and does NOT add a second entry', () => {
    const id1 = pushToast({ variant: 'error', message: 'Err', key: 'network' })
    const id2 = pushToast({ variant: 'error', message: 'Err again', key: 'network' })
    expect(id1).toBe(id2)
    expect(toasts).toHaveLength(1)
  })

  it('dedup refreshes the auto-dismiss timer', () => {
    pushToast({ variant: 'error', message: 'Err', key: 'network', timeoutMs: 6000 })
    vi.advanceTimersByTime(5000)
    // Re-push at t=5000 resets the timer to a fresh 6000ms window.
    pushToast({ variant: 'error', message: 'Err', key: 'network', timeoutMs: 6000 })
    vi.advanceTimersByTime(5000) // t=10000 overall, but only 5000 since the refresh
    expect(toasts).toHaveLength(1)
    vi.advanceTimersByTime(1001)
    expect(toasts).toHaveLength(0)
  })

  it('dismiss removes the toast', () => {
    const id = pushToast({ variant: 'success', message: 'Done', timeoutMs: 0 })
    expect(toasts).toHaveLength(1)
    dismissToast(id)
    expect(toasts).toHaveLength(0)
  })

  it('a dismissed key can be pushed again as a fresh toast', () => {
    const id1 = pushToast({ variant: 'error', message: 'Err', key: 'network', timeoutMs: 0 })
    dismissToast(id1)
    const id2 = pushToast({ variant: 'error', message: 'Err', key: 'network', timeoutMs: 0 })
    expect(id2).not.toBe(id1)
    expect(toasts).toHaveLength(1)
  })
})
