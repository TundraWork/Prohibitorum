import { describe, it, expect, vi, beforeEach } from 'vitest'
import { withSudo, ensureSudo, sudoState, _resolveSudo } from './sudo'

beforeEach(() => { sudoState.value = { open: false, resolve: null } })

describe('withSudo', () => {
  it('returns the result when fn succeeds (no step-up)', async () => {
    const r = await withSudo(async () => 'ok')
    expect(r).toBe('ok')
    expect(sudoState.value.open).toBe(false)
  })

  it('on sudo_required opens the modal, then retries once after success', async () => {
    let calls = 0
    const p = withSudo(async () => {
      calls++
      if (calls === 1) throw { code: 'sudo_required', message: 'x' }
      return 'done'
    })
    await Promise.resolve()
    expect(sudoState.value.open).toBe(true)
    _resolveSudo(true)
    expect(await p).toBe('done')
    expect(calls).toBe(2)
  })

  it('rethrows the original error if the ceremony is cancelled', async () => {
    const p = withSudo(async () => { throw { code: 'sudo_required', message: 'x' } })
    await Promise.resolve()
    _resolveSudo(false)
    await expect(p).rejects.toMatchObject({ code: 'sudo_required' })
  })

  it('does not intercept non-sudo errors', async () => {
    await expect(withSudo(async () => { throw { code: 'boom' } })).rejects.toMatchObject({ code: 'boom' })
    expect(sudoState.value.open).toBe(false)
  })

  it('ensureSudo resolves true/false from one modal run', async () => {
    const p = ensureSudo()
    await Promise.resolve()
    expect(sudoState.value.open).toBe(true)
    _resolveSudo(true)
    expect(await p).toBe(true)

    const p2 = ensureSudo()
    await Promise.resolve()
    _resolveSudo(false)
    expect(await p2).toBe(false)
  })
})
