import { describe, it, expect, vi } from 'vitest'
import { withSudo, sudoState, _resolveSudo } from './sudo'

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
