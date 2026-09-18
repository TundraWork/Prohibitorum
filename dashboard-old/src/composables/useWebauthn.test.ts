import { describe, expect, it, vi, beforeEach } from 'vitest'
import type {
  PublicKeyCredentialCreationOptionsJSON,
  PublicKeyCredentialRequestOptionsJSON,
} from '@simplewebauthn/browser'

vi.mock('@/lib/webauthn', () => ({
  passkeyGet: vi.fn(),
  passkeyRegister: vi.fn(),
  isUserCancel: vi.fn(() => false),
}))

import { passkeyGet, passkeyRegister } from '@/lib/webauthn'
import { useWebauthn } from './useWebauthn'

const requestOptions: PublicKeyCredentialRequestOptionsJSON = {
  challenge: 'request-challenge',
}

const creationOptions: PublicKeyCredentialCreationOptionsJSON = {
  rp: { name: 'Prohibitorum', id: 'example.test' },
  user: { id: 'user-id', name: 'alex', displayName: 'Alex' },
  challenge: 'creation-challenge',
  pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
}

beforeEach(() => {
  vi.mocked(passkeyGet).mockReset()
  vi.mocked(passkeyRegister).mockReset()
})

describe('useWebauthn', () => {
  it('exposes authentication failures by code without browser prose', async () => {
    vi.mocked(passkeyGet).mockRejectedValue(new Error('raw browser authentication failure'))
    const webauthn = useWebauthn()

    await webauthn.authenticate(requestOptions)

    expect(webauthn.error.value).toEqual({ code: 'webauthn_error' })
  })

  it('exposes registration failures by code without browser prose', async () => {
    vi.mocked(passkeyRegister).mockRejectedValue(new Error('raw browser registration failure'))
    const webauthn = useWebauthn()

    await webauthn.register(creationOptions)

    expect(webauthn.error.value).toEqual({ code: 'webauthn_error' })
  })
})
