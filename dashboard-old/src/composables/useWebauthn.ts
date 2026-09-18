/**
 * useWebauthn — WebAuthn ceremony composable.
 *
 * Wraps passkeyGet/passkeyRegister with busy/error state, and handles
 * user-cancel distinctly: a cancelled ceremony resets state without
 * setting an error (no scary banner for a deliberate dismissal).
 *
 * Usage (login):
 *   const { busy, error, authenticate } = useWebauthn()
 *   const assertion = await authenticate(optionsJSON)
 *   if (!assertion) return // cancelled or error (check error ref)
 *
 * Usage (registration):
 *   const { busy, error, register } = useWebauthn()
 *   const credential = await register(optionsJSON)
 */

import { ref } from 'vue'
import { passkeyGet, passkeyRegister, isUserCancel } from '@/lib/webauthn'
import type { ApiError } from '@/lib/api'
import type {
  PublicKeyCredentialRequestOptionsJSON,
  PublicKeyCredentialCreationOptionsJSON,
  AuthenticationResponseJSON,
  RegistrationResponseJSON,
} from '@simplewebauthn/browser'

export function useWebauthn() {
  const busy = ref(false)
  const error = ref<ApiError | null>(null)

  async function authenticate(
    optionsJSON: PublicKeyCredentialRequestOptionsJSON,
  ): Promise<AuthenticationResponseJSON | undefined> {
    if (busy.value) return undefined
    busy.value = true
    error.value = null
    try {
      return await passkeyGet(optionsJSON)
    } catch (err: unknown) {
      if (!isUserCancel(err)) {
        error.value = { code: 'webauthn_error' }
      }
      // User-cancel: reset silently (no error banner)
      return undefined
    } finally {
      busy.value = false
    }
  }

  async function register(
    optionsJSON: PublicKeyCredentialCreationOptionsJSON,
  ): Promise<RegistrationResponseJSON | undefined> {
    if (busy.value) return undefined
    busy.value = true
    error.value = null
    try {
      return await passkeyRegister(optionsJSON)
    } catch (err: unknown) {
      if (!isUserCancel(err)) {
        error.value = { code: 'webauthn_error' }
      }
      return undefined
    } finally {
      busy.value = false
    }
  }

  function clear(): void {
    error.value = null
  }

  return { busy, error, authenticate, register, clear }
}
