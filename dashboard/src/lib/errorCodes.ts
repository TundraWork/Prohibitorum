/**
 * Generated/shared error-code manifest mirroring the Go weberr registry.
 *
 * This is the single frontend source of truth for every backend error code,
 * its declared detail keys, and its recovery hint. It is hand-maintained to
 * match `pkg/weberr/registry.go` + `pkg/authn/errors.go` (the Go init()
 * registrations), and the parity test (`errors.test.ts` → "locale parity")
 * walks this list against both locale files so any drift is caught.
 *
 * Client-synthesized codes (network_error, webauthn_error, not_found,
 * forbidden) live in CLIENT_CODES below; they have no backend registry entry.
 */

export interface ErrorCodeDef {
  /** Stable machine-readable code — matches the Go registry Code field. */
  code: string
  /** Declared public detail keys (from the Go Definition.DetailKeys). */
  details: readonly string[]
  /** Recovery hint from the Go Definition.Recovery (empty = default). */
  recovery: string
}

/**
 * Every code registered in the Go weberr registry (pkg/weberr/registry.go
 * init + pkg/authn/errors.go init). Sorted alphabetically.
 *
 * The count and codes MUST match `weberr.AllDefinitions()` exactly — the
 * parity test asserts this list against a hardcoded snapshot count so a
 * missing or extra code fails the suite.
 */
export const REGISTRY_CODES: readonly ErrorCodeDef[] = [
  { code: 'account_disabled', details: [], recovery: '' },
  { code: 'account_not_found', details: [], recovery: '' },
  { code: 'active_key_no_replacement', details: [], recovery: '' },
  { code: 'admin_cannot_be_disabled', details: [], recovery: '' },
  { code: 'bad_credentials', details: [], recovery: '' },
  { code: 'bad_request', details: [], recovery: '' },
  { code: 'cannot_delete_self', details: [], recovery: '' },
  { code: 'cannot_revoke_current_session', details: [], recovery: '' },
  { code: 'ceremony_expired', details: [], recovery: 'retry' },
  { code: 'ceremony_internal_error', details: [], recovery: 'retry' },
  { code: 'ceremony_missing', details: [], recovery: '' },
  { code: 'ceremony_state_invalid', details: [], recovery: '' },
  { code: 'client_not_found', details: [], recovery: '' },
  { code: 'credential_already_registered', details: [], recovery: '' },
  { code: 'credential_not_found', details: [], recovery: '' },
  { code: 'database_unavailable', details: [], recovery: 'retry' },
  { code: 'email_not_verified', details: [], recovery: '' },
  { code: 'enrollment_consumed', details: [], recovery: '' },
  { code: 'enrollment_expired', details: [], recovery: '' },
  { code: 'enrollment_federation_required', details: [], recovery: '' },
  { code: 'factor_locked', details: ['retryAfterSeconds'], recovery: 'retry' },
  { code: 'federation_state_invalid', details: [], recovery: 'retry' },
  { code: 'group_not_found', details: [], recovery: '' },
  { code: 'group_slug_conflict', details: [], recovery: '' },
  { code: 'invalid_consent_ticket', details: [], recovery: '' },
  { code: 'invalid_display_name', details: [], recovery: '' },
  { code: 'invalid_nickname', details: [], recovery: '' },
  { code: 'invalid_return_to', details: [], recovery: '' },
  { code: 'invalid_role', details: ['allowed'], recovery: '' },
  { code: 'invalid_username', details: [], recovery: '' },
  { code: 'invitation_not_found', details: [], recovery: '' },
  { code: 'invite_required', details: [], recovery: '' },
  { code: 'kv_unavailable', details: [], recovery: 'retry' },
  { code: 'last_admin', details: [], recovery: '' },
  { code: 'last_passkey', details: [], recovery: '' },
  { code: 'last_sign_in_method', details: [], recovery: '' },
  { code: 'link_required', details: [], recovery: '' },
  { code: 'login_account_not_found', details: [], recovery: '' },
  { code: 'login_failed', details: [], recovery: 'retry' },
  { code: 'login_verification_failed', details: [], recovery: '' },
  { code: 'maintenance_mode', details: [], recovery: 'retry' },
  { code: 'no_session', details: [], recovery: 'reauth' },
  { code: 'not_admin', details: [], recovery: '' },
  { code: 'not_bootstrapped', details: [], recovery: '' },
  { code: 'oidc_client_already_exists', details: [], recovery: '' },
  { code: 'pairing_expired', details: [], recovery: '' },
  { code: 'pairing_not_approved', details: [], recovery: '' },
  { code: 'pairing_not_found', details: [], recovery: '' },
  { code: 'pairing_state', details: [], recovery: '' },
  { code: 'partial_session_invalid', details: [], recovery: 'reauth' },
  { code: 'permission_denied', details: [], recovery: '' },
  { code: 'provider_not_ready', details: [], recovery: '' },
  { code: 'rate_limited', details: ['retryAfterSeconds'], recovery: 'retry' },
  { code: 'recovery_session_invalid', details: [], recovery: 'reauth' },
  { code: 'registration_failed', details: [], recovery: '' },
  { code: 'request_too_large', details: [], recovery: 'reduce_payload' },
  { code: 'saml_application_already_exists', details: [], recovery: '' },
  { code: 'server_error', details: [], recovery: 'retry' },
  { code: 'session_not_found', details: [], recovery: '' },
  { code: 'sudo_method_unavailable', details: [], recovery: '' },
  { code: 'sudo_required', details: [], recovery: 'reauth' },
  { code: 'unsupported_media_type', details: [], recovery: 'fix_content_type' },
  { code: 'upstream_error', details: ['upstreamCode'], recovery: '' },
  { code: 'upstream_idp_already_exists', details: [], recovery: '' },
  { code: 'upstream_idp_not_found', details: [], recovery: '' },
  { code: 'username_collision', details: [], recovery: '' },
  { code: 'username_immutable', details: [], recovery: '' },
  { code: 'username_taken', details: [], recovery: '' },
  { code: 'validation_failed', details: ['location', 'reason'], recovery: 'fix_input' },
  { code: 'would_remove_last_factor', details: [], recovery: '' },
] as const

/**
 * Client-synthesized codes that have no backend registry entry but appear in
 * ApiError. The frontend must have locale entries for these too.
 *
 * - network_error: fetch rejection / timeout (api.ts)
 * - webauthn_error: navigator.credentials failure (useWebauthn)
 * - not_found: router 404 (not an API error, but historically in the errors map)
 * - forbidden: router 403 guard
 * - avatar_too_large, avatar_invalid_image, avatar_source_unavailable:
 *   avatar/icon upload validation codes returned by the avatar API endpoints.
 */
export const CLIENT_CODES: readonly ErrorCodeDef[] = [
  { code: 'network_error', details: [], recovery: 'retry' },
  { code: 'webauthn_error', details: [], recovery: 'retry' },
  { code: 'not_found', details: [], recovery: '' },
  { code: 'forbidden', details: [], recovery: '' },
  { code: 'avatar_too_large', details: [], recovery: 'reduce_payload' },
  { code: 'avatar_invalid_image', details: [], recovery: '' },
  { code: 'avatar_source_unavailable', details: [], recovery: '' },
] as const

/** All codes the frontend must have locale entries for. */
export const ALL_CODES: readonly ErrorCodeDef[] = [...REGISTRY_CODES, ...CLIENT_CODES]

/** Every detail key referenced by any code (for locale detail-label parity). */
export const ALL_DETAIL_KEYS: readonly string[] = [
  'allowed',
  'retryAfterSeconds',
  'upstreamCode',
  'location',
  'reason',
] as const

/** Every recovery hint referenced by any code (for locale recovery-label parity). */
export const ALL_RECOVERY_HINTS: readonly string[] = [
  'retry',
  'reauth',
  'reduce_payload',
  'fix_content_type',
  'fix_input',
] as const

/**
 * Lookup map: code → ErrorCodeDef. Includes client codes.
 */
const CODE_MAP: ReadonlyMap<string, ErrorCodeDef> = new Map(
  ALL_CODES.map((d) => [d.code, d]),
)

export function codeDefinition(code: string): ErrorCodeDef | undefined {
  return CODE_MAP.get(code)
}

/**
 * Expected count of registry codes (from weberr.AllDefinitions). The parity
 * test asserts REGISTRY_CODES.length === this value so a code added/removed
 * in the Go registry without updating this manifest fails the suite.
 *
 * Derived from: go test ./pkg/weberr → AllDefinitions() count.
 */
export const EXPECTED_REGISTRY_CODE_COUNT = 70
/**
 * Error codes owned by a GLOBAL handler — a redirect (no_session →
 * sessionExpiry), a full-screen redirect (maintenance_mode), or a connection
 * error (network_error / server_error). Shared between useApi (errorText
 * suppression) and ErrorPanel (message suppression) so both use the same
 * definition.
 */
export const GLOBAL_ERROR_CODES: ReadonlySet<string> = new Set([
  'no_session',
  'maintenance_mode',
  'network_error',
  'server_error',
])
