import { describe, it, expect, vi, beforeEach } from 'vitest'

const post = vi.fn()
vi.mock('./api', () => ({ api: { post: (...a: unknown[]) => post(...a) } }))

const startRegistration = vi.fn()
vi.mock('@simplewebauthn/browser', () => ({
  startRegistration: (...a: unknown[]) => startRegistration(...a),
}))

import { passkeyRegister } from './webauthn'

beforeEach(() => {
  post.mockReset()
  startRegistration.mockReset()
})

describe('passkeyRegister', () => {
  it('drives begin → ceremony → complete and returns the session', async () => {
    post.mockResolvedValueOnce({ challenge: 'abc' }) // begin (flat options)
    startRegistration.mockResolvedValueOnce({ id: 'cred' }) // attestation
    post.mockResolvedValueOnce({ session: { id: 1, username: 'a', displayName: 'A', role: 'admin' }, newCredentialId: 9 }) // complete

    const session = await passkeyRegister('tok', { username: 'a', displayName: 'A' })

    expect(post).toHaveBeenNthCalledWith(1, '/api/prohibitorum/enrollments/tok/register/begin', { username: 'a', displayName: 'A' })
    expect(startRegistration).toHaveBeenCalledWith({ optionsJSON: { challenge: 'abc' } })
    expect(post).toHaveBeenNthCalledWith(2, '/api/prohibitorum/enrollments/tok/register/complete', { id: 'cred' })
    expect(session.username).toBe('a')
  })
})
