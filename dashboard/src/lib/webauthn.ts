import { startAuthentication, startRegistration } from '@simplewebauthn/browser'
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

// Identity fields for the enrollment begin body. Bootstrap & invite intents
// REQUIRE username + displayName (server validates); reset ignores them (empty
// body). nickname is the optional first-passkey label. See
// pkg/server/handle_enrollment.go (enrollBeginBody).
export interface EnrollFields {
  username?: string
  displayName?: string
  nickname?: string
}

// Drives the WebAuthn registration ceremony for an enrollment token.
// /begin returns flat PublicKeyCredentialCreationOptions (probe .publicKey for
// forward-compat, mirroring passkeyLogin). /complete sets the session cookie and
// returns { session: SessionView, newCredentialId } — we return the session.
export async function passkeyRegister(token: string, fields: EnrollFields): Promise<SessionView> {
  const base = `/api/prohibitorum/enrollments/${encodeURIComponent(token)}`
  const options = await api.post<any>(`${base}/register/begin`, fields)
  const attestation = await startRegistration({ optionsJSON: options.publicKey ?? options })
  const result = await api.post<{ session: SessionView }>(`${base}/register/complete`, attestation)
  return result.session
}
