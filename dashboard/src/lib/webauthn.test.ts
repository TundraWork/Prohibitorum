import { describe, it, expect, vi, beforeEach } from 'vitest'

const post = vi.fn()
vi.mock('./api', () => ({ api: { post: (...a: unknown[]) => post(...a) } }))

const startRegistration = vi.fn()
vi.mock('@simplewebauthn/browser', () => ({
  startRegistration: (...a: unknown[]) => startRegistration(...a),
}))

import { passkeyRegister, passkeyAddCredential } from './webauthn'

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

describe('passkeyAddCredential', () => {
  it('begins (nickname query), runs startRegistration, completes, returns the credential', async () => {
    post.mockResolvedValueOnce({ challenge: 'abc' })            // begin
    startRegistration.mockResolvedValueOnce({ id: 'cred' })     // attestation
    post.mockResolvedValueOnce({ id: 7, credentialIdSuffix: 'ab12', transports: [] }) // complete
    const cred = await passkeyAddCredential('Laptop')
    expect(post).toHaveBeenNthCalledWith(1, '/api/prohibitorum/me/credentials/register/begin?nickname=Laptop')
    expect(startRegistration).toHaveBeenCalledWith({ optionsJSON: { challenge: 'abc' } })
    expect(post).toHaveBeenNthCalledWith(2, '/api/prohibitorum/me/credentials/register/complete?nickname=Laptop', { id: 'cred' })
    expect(cred.id).toBe(7)
  })
})
