import { startAuthentication } from '@simplewebauthn/browser'
import { api } from './api'
import type { SessionView } from '../stores/session'

// Drives the WebAuthn login ceremony.
//
// Both endpoints are POST (see pkg/server/server.go). /begin sets the ceremony
// cookie and returns the PublicKeyCredentialRequestOptions. Verified against the
// Go handler (handleLoginBeginHTTP): it writes `assertion.Response`, i.e. the
// go-webauthn protocol.PublicKeyCredentialRequestOptions *directly* (flat), not
// wrapped in {publicKey: ...}. We still probe `options.publicKey` first so this
// keeps working if the wire shape ever switches to the nested CredentialAssertion
// envelope. /complete returns the SessionView.
export async function passkeyLogin(): Promise<SessionView> {
  const options = await api.post<any>('/api/prohibitorum/auth/login/begin')
  const assertion = await startAuthentication({ optionsJSON: options.publicKey ?? options })
  return await api.post<SessionView>('/api/prohibitorum/auth/login/complete', assertion)
}
