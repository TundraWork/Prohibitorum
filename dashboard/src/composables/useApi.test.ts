import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent, ref } from 'vue'
import { useApi } from './useApi'
import type { ApiError } from '@/lib/api'

// Mock i18n with a minimal implementation that mirrors the real error-lookup logic.
// We keep te/t returning simple values so we can assert the mapping without real
// message files.  Overrides are set per-test via mockI18n.te / mockI18n.t.
const mockTe = vi.fn((_key: string) => false)
const mockT = vi.fn((key: string) => key)

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: mockT, te: mockTe }),
  createI18n: vi.fn(() => ({ install: vi.fn() })),
}))

// Helper: mount a test component that exposes useApi() state/methods via the
// component instance.  Providing i18n is not strictly needed here because we
// mocked useI18n above, but we pass the plugin for completeness.
function makeWrapper() {
  const TestComp = defineComponent({
    setup() {
      const { busy, error, run, errorText } = useApi()
      return { busy, error, run, errorText }
    },
    template: '<div>{{ errorText }}</div>',
  })

  const i18n = createI18n({ legacy: false, locale: 'en', messages: { en: {} } })
  const wrapper = mount(TestComp, { global: { plugins: [i18n] } })
  return wrapper
}

beforeEach(() => {
  mockTe.mockReset().mockReturnValue(false)
  mockT.mockReset().mockImplementation((key: string) => key)
})

describe('useApi', () => {
  it('starts with busy=false and error=null', () => {
    const w = makeWrapper()
    expect(w.vm.busy).toBe(false)
    expect(w.vm.error).toBeNull()
  })

  it('sets busy=true during run and false after', async () => {
    const w = makeWrapper()
    let resolveFn!: () => void
    const slowFn = () => new Promise<void>((r) => { resolveFn = r })
    const runPromise = w.vm.run(slowFn as unknown as () => Promise<void>)
    await flushPromises()
    expect(w.vm.busy).toBe(true)
    resolveFn()
    await runPromise
    expect(w.vm.busy).toBe(false)
  })

  it('returns undefined and sets error when run throws an ApiError', async () => {
    const w = makeWrapper()
    const err: ApiError = { code: 'not_found', message: 'resource not found' }
    const result = await w.vm.run(() => Promise.reject(err))
    expect(result).toBeUndefined()
    expect(w.vm.error).toEqual(err)
    expect(w.vm.busy).toBe(false)
  })

  it('returns the function result on success', async () => {
    const w = makeWrapper()
    const result = await w.vm.run(() => Promise.resolve(42))
    expect(result).toBe(42)
    expect(w.vm.error).toBeNull()
  })

  it('clears error on the next run call', async () => {
    const w = makeWrapper()
    const err: ApiError = { code: 'server_error', message: 'oops' }
    await w.vm.run(() => Promise.reject(err))
    expect(w.vm.error).not.toBeNull()
    await w.vm.run(() => Promise.resolve('ok'))
    expect(w.vm.error).toBeNull()
  })

  it('does not re-enter if already busy', async () => {
    const w = makeWrapper()
    let resolveFn!: () => void
    const fn = vi.fn(() => new Promise<void>((r) => { resolveFn = r }))
    const first = w.vm.run(fn)
    const second = w.vm.run(fn) // should be a no-op
    expect(fn).toHaveBeenCalledTimes(1)
    resolveFn()
    await Promise.all([first, second])
    expect(w.vm.busy).toBe(false)
  })

  describe('errorText', () => {
    it('is empty when error is null', () => {
      const w = makeWrapper()
      expect(w.vm.errorText).toBe('')
    })

    it('returns the translated key when te() recognises it', async () => {
      const w = makeWrapper()
      const err: ApiError = { code: 'bad_credentials', message: 'raw' }
      mockTe.mockReturnValue(true)
      mockT.mockImplementation((key: string) => key === 'errors.bad_credentials' ? 'Wrong password' : key)
      await w.vm.run(() => Promise.reject(err))
      await flushPromises()
      expect(w.vm.errorText).toBe('Wrong password')
    })

    it('falls back to error.message when te() returns false and message is set', async () => {
      const w = makeWrapper()
      const err: ApiError = { code: 'unknown_code', message: 'Server said nope' }
      mockTe.mockReturnValue(false)
      await w.vm.run(() => Promise.reject(err))
      await flushPromises()
      expect(w.vm.errorText).toBe('Server said nope')
    })

    it('falls back to common.error when te() is false and message is empty', async () => {
      const w = makeWrapper()
      const err: ApiError = { code: 'unknown_code', message: '' }
      mockTe.mockReturnValue(false)
      mockT.mockImplementation((key: string) => key === 'common.error' ? 'An error occurred' : key)
      await w.vm.run(() => Promise.reject(err))
      await flushPromises()
      expect(w.vm.errorText).toBe('An error occurred')
    })
  })
})
