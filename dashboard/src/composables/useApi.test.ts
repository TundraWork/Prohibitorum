import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { defineComponent } from 'vue'
import { useApi } from './useApi'
import type { ApiError } from '@/lib/api'

// Mock i18n with a minimal implementation that mirrors the real error-lookup logic.
const mockTe = vi.fn((_key: string) => false)
const mockT = vi.fn((key: string) => key)

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: mockT, te: mockTe }),
  createI18n: vi.fn(() => ({ install: vi.fn() })),
}))

function makeWrapper() {
  const TestComp = defineComponent({
    setup() {
      const { busy, error, run, clear, errorText } = useApi()
      return { busy, error, run, clear, errorText }
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
    const err: ApiError = { code: 'not_found', requestId: 'rid' }
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

  it('clears error on a successful retry (run succeeds after failure)', async () => {
    const w = makeWrapper()
    const err: ApiError = { code: 'server_error' }
    await w.vm.run(() => Promise.reject(err))
    expect(w.vm.error).not.toBeNull()
    await w.vm.run(() => Promise.resolve('ok'))
    expect(w.vm.error).toBeNull()
  })

  it('does NOT clear error on entry — error persists until success or clear', async () => {
    const w = makeWrapper()
    const err: ApiError = { code: 'bad_request' }
    await w.vm.run(() => Promise.reject(err))
    expect(w.vm.error).not.toBeNull()
    // Starting another run does NOT clear the error until it succeeds
    let resolveFn!: () => void
    const slowFn = () => new Promise<void>((r) => { resolveFn = r })
    const p = w.vm.run(slowFn as unknown as () => Promise<void>)
    await flushPromises()
    expect(w.vm.error).not.toBeNull() // still there during the run
    resolveFn()
    await p
    expect(w.vm.error).toBeNull() // cleared on success
  })

  it('clear() explicitly clears the error', async () => {
    const w = makeWrapper()
    const err: ApiError = { code: 'bad_request' }
    await w.vm.run(() => Promise.reject(err))
    expect(w.vm.error).not.toBeNull()
    w.vm.clear()
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

  it('maps non-ApiError throws to network_error (no message)', async () => {
    const w = makeWrapper()
    await w.vm.run(() => Promise.reject(new Error('boom')))
    expect(w.vm.error).not.toBeNull()
    expect(w.vm.error!.code).toBe('network_error')
    expect((w.vm.error as Record<string, unknown>).message).toBeUndefined()
  })
})

describe('errorText', () => {
  it('is empty when error is null', () => {
    const w = makeWrapper()
    expect(w.vm.errorText).toBe('')
  })

  it('returns the translated key when te() recognises it', async () => {
    const w = makeWrapper()
    const err: ApiError = { code: 'bad_credentials' }
    mockTe.mockReturnValue(true)
    mockT.mockImplementation((key: string) => key === 'errors.codes.bad_credentials' ? 'Wrong password' : key)
    await w.vm.run(() => Promise.reject(err))
    await flushPromises()
    expect(w.vm.errorText).toBe('Wrong password')
  })

  it('returns empty string for unknown codes (ErrorPanel renders the fallback)', async () => {
    const w = makeWrapper()
    const err: ApiError = { code: 'unknown_code_xyz' }
    mockTe.mockReturnValue(false)
    await w.vm.run(() => Promise.reject(err))
    await flushPromises()
    expect(w.vm.errorText).toBe('')
  })

  it('returns empty for globally-handled codes (redirect/toast)', async () => {
    const w = makeWrapper()
    const err: ApiError = { code: 'server_error' }
    mockTe.mockReturnValue(true)
    mockT.mockImplementation((key: string) => key === 'errors.codes.server_error' ? 'Server error' : key)
    await w.vm.run(() => Promise.reject(err))
    await flushPromises()
    expect(w.vm.errorText).toBe('')
  })
})
