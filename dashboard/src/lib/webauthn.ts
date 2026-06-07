/**
 * WebAuthn ceremony wrappers.
 *
 * Delegates ALL base64url encoding/decoding and navigator.credentials calls
 * to @simplewebauthn/browser (v13). We pass the server-returned publicKey
 * options JSON through verbatim via the { optionsJSON } argument shape that
 * v13 expects.
 *
 * `isUserCancel` surfaces a distinct cancel signal so callers can reset state
 * quietly (no scary error banner) when the user dismisses the browser dialog.
 *
 * v13 API confirmed from:
 *   node_modules/@simplewebauthn/browser/esm/methods/startAuthentication.d.ts
 *   node_modules/@simplewebauthn/browser/esm/methods/startRegistration.d.ts
 *
 * Both functions accept { optionsJSON: PublicKeyCredential*OptionsJSON }.
 * Rejection on user-cancel is a DOMException with name 'NotAllowedError'.
 */

import { startAuthentication, startRegistration } from '@simplewebauthn/browser'
import type {
  PublicKeyCredentialRequestOptionsJSON,
  PublicKeyCredentialCreationOptionsJSON,
  AuthenticationResponseJSON,
  RegistrationResponseJSON,
} from '@simplewebauthn/browser'

/**
 * Run the WebAuthn authentication ceremony (login with passkey).
 * Throws on failure; callers should check `isUserCancel(err)` before showing
 * an error banner.
 */
export async function passkeyGet(
  optionsJSON: PublicKeyCredentialRequestOptionsJSON,
): Promise<AuthenticationResponseJSON> {
  return startAuthentication({ optionsJSON })
}

/**
 * Run the WebAuthn registration ceremony (enroll a passkey).
 * Throws on failure; callers should check `isUserCancel(err)` before showing
 * an error banner.
 */
export async function passkeyRegister(
  optionsJSON: PublicKeyCredentialCreationOptionsJSON,
): Promise<RegistrationResponseJSON> {
  return startRegistration({ optionsJSON })
}

/**
 * Returns true if the error is a user-cancellation (NotAllowedError DOMException).
 * Used by composables to silently reset state without showing a scary banner.
 */
export function isUserCancel(e: unknown): boolean {
  return e instanceof Error && e.name === 'NotAllowedError'
}
