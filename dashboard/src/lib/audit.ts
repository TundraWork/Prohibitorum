/**
 * audit.ts — mirrors the factor / event constants from pkg/audit/event.go.
 * Keep in sync when new constants are added to the Go source.
 */

export const AUDIT_FACTORS = [
  'webauthn',
  'password',
  'totp',
  'recovery_code',
  'federation_oidc',
  'enrollment',
  'session',
  'oidc_client',
  'saml_sp',
  'upstream_idp',
  'signing_key',
  'account',
  'invitation',
  'settings',
] as const

export type AuditFactor = (typeof AUDIT_FACTORS)[number]

export const AUDIT_EVENTS = [
  'register',
  'use',
  'fail',
  'revoke',
  'clone_warning',
  'link',
  'unlink',
  'enrollment_issued',
  'enrollment_consumed',
  'session_start',
  'session_end',
  'factor_disabled',
  'factor_locked',
  'update',
  'rotate',
  'sudo_granted',
  'sudo_failed',
] as const

export type AuditEvent = (typeof AUDIT_EVENTS)[number]
