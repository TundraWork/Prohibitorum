import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
vi.mock('./api', () => ({ api: { get: vi.fn() } }))
import { api } from './api'
import { ensureSudo, withSudo, sudoState, _resolveSudo } from './sudo'

beforeEach(() => { vi.mocked(api.get).mockReset(); _resolveSudo(false) })

describe('ensureSudo', () => {
  it('reuses the server grant for repeated actions without prompting', async () => {
    vi.mocked(api.get).mockResolvedValue({ fresh: true })
    expect(await ensureSudo()).toBe(true)
    expect(await ensureSudo()).toBe(true)
    expect(api.get).toHaveBeenCalledTimes(2)
    expect(sudoState.value.open).toBe(false)
  })

  it('rechecks expiry instead of trusting a prior successful check', async () => {
    vi.mocked(api.get).mockResolvedValueOnce({ fresh: true }).mockResolvedValueOnce({ fresh: false })
    expect(await ensureSudo()).toBe(true)
    const pending = ensureSudo('link')
    await flushPromises()
    expect(sudoState.value.open).toBe(true)
    expect(sudoState.value.reason).toBe('link')
    _resolveSudo(true)
    expect(await pending).toBe(true)
  })

  it('does not proceed when the user cancels or the preflight fails', async () => {
    vi.mocked(api.get).mockResolvedValueOnce({ fresh: false })
    const pending = ensureSudo()
    await flushPromises()
    _resolveSudo(false)
    expect(await pending).toBe(false)
    const err = { code: 'network_error' }
    vi.mocked(api.get).mockRejectedValueOnce(err)
    await expect(ensureSudo()).rejects.toBe(err)
    expect(sudoState.value.open).toBe(false)
  })
})

describe('withSudo', () => {
  it('passes through on success without opening the modal', async () => {
    const fn = vi.fn(async () => 'ok')
    expect(await withSudo(fn)).toBe('ok')
    expect(sudoState.value.open).toBe(false)
    expect(fn).toHaveBeenCalledOnce()
  })

  it('steps up and retries once on sudo_required', async () => {
    const fn = vi.fn()
      .mockRejectedValueOnce({ code: 'sudo_required' })
      .mockResolvedValueOnce('done')
    const p = withSudo(fn as () => Promise<string>)
    await Promise.resolve()
    expect(sudoState.value.open).toBe(true)
    _resolveSudo(true)
    expect(await p).toBe('done')
    expect(fn).toHaveBeenCalledTimes(2)
  })

  it('rethrows when the user cancels the step-up', async () => {
    const err = { code: 'sudo_required' }
    const fn = vi.fn().mockRejectedValue(err)
    const p = withSudo(fn as () => Promise<unknown>)
    await Promise.resolve()
    _resolveSudo(false)
    await expect(p).rejects.toBe(err)
    expect(fn).toHaveBeenCalledOnce()
  })

  it('rethrows non-sudo errors immediately', async () => {
    const err = { code: 'bad_request' }
    const fn = vi.fn().mockRejectedValue(err)
    await expect(withSudo(fn as () => Promise<unknown>)).rejects.toBe(err)
    expect(sudoState.value.open).toBe(false)
  })
})
