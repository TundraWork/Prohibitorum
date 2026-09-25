import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";

/**
 * The audit log's vocabulary: what each `factor` and `event` a
 * `credential_event` row can carry is called on screen.
 *
 * The keys follow `pkg/audit/event.go` — and, for the one event the VRChat
 * adapter writes directly, `pkg/federation/providers/vrchat` — so a value the
 * server writes has a name here. A value neither list knows is shown exactly as
 * the server sent it rather than guessed at; the filter offers only these.
 */
export const auditFactors = {
  webauthn: msg({ id: "audit.factor.webauthn", message: "Passkey" }),
  password: msg({ id: "audit.factor.password", message: "Password" }),
  totp: msg({ id: "audit.factor.totp", message: "Authenticator" }),
  recovery_code: msg({
    id: "audit.factor.recovery_code",
    message: "Recovery code",
  }),
  federation_oidc: msg({
    id: "audit.factor.federation_oidc",
    message: "Upstream sign-in",
  }),
  enrollment: msg({ id: "audit.factor.enrollment", message: "Registration" }),
  session: msg({ id: "audit.factor.session", message: "Session" }),
  oidc_client: msg({
    id: "audit.factor.oidc_client",
    message: "OIDC application",
  }),
  saml_sp: msg({ id: "audit.factor.saml_sp", message: "SAML application" }),
  upstream_idp: msg({
    id: "audit.factor.upstream_idp",
    message: "Upstream provider",
  }),
  signing_key: msg({ id: "audit.factor.signing_key", message: "Signing key" }),
  account: msg({ id: "audit.factor.account", message: "Account" }),
  invitation: msg({ id: "audit.factor.invitation", message: "Invitation" }),
  app_policy: msg({
    id: "audit.factor.app_policy",
    message: "Application access",
  }),
  personal_access_token: msg({
    id: "audit.factor.personal_access_token",
    message: "Access token",
  }),
  settings: msg({ id: "audit.factor.settings", message: "Settings" }),
  diagnostic: msg({ id: "audit.factor.diagnostic", message: "Diagnostics" }),
  app_manager: msg({
    id: "audit.factor.app_manager",
    message: "Application manager",
  }),
} satisfies Record<string, MessageDescriptor>;

export const auditEvents = {
  register: msg({ id: "audit.event.register", message: "Created" }),
  use: msg({ id: "audit.event.use", message: "Used" }),
  fail: msg({ id: "audit.event.fail", message: "Failed" }),
  revoke: msg({ id: "audit.event.revoke", message: "Revoked" }),
  clone_warning: msg({
    id: "audit.event.clone_warning",
    message: "Possible clone",
  }),
  link: msg({ id: "audit.event.link", message: "Linked" }),
  unlink: msg({ id: "audit.event.unlink", message: "Unlinked" }),
  enrollment_issued: msg({
    id: "audit.event.enrollment_issued",
    message: "Link issued",
  }),
  enrollment_consumed: msg({
    id: "audit.event.enrollment_consumed",
    message: "Link used",
  }),
  session_start: msg({
    id: "audit.event.session_start",
    message: "Signed in",
  }),
  session_end: msg({ id: "audit.event.session_end", message: "Signed out" }),
  factor_disabled: msg({
    id: "audit.event.factor_disabled",
    message: "Turned off",
  }),
  factor_locked: msg({ id: "audit.event.factor_locked", message: "Locked" }),
  update: msg({ id: "audit.event.update", message: "Changed" }),
  rotate: msg({ id: "audit.event.rotate", message: "Rotated" }),
  access_granted: msg({
    id: "audit.event.access_granted",
    message: "Access allowed",
  }),
  access_revoked: msg({
    id: "audit.event.access_revoked",
    message: "Access cleared",
  }),
  access_restricted_set: msg({
    id: "audit.event.access_restricted_set",
    message: "Restriction changed",
  }),
  access_denied: msg({
    id: "audit.event.access_denied",
    message: "Access denied",
  }),
  app_manager_assigned: msg({
    id: "audit.event.app_manager_assigned",
    message: "Manager assigned",
  }),
  app_manager_removed: msg({
    id: "audit.event.app_manager_removed",
    message: "Manager removed",
  }),
  sudo_granted: msg({
    id: "audit.event.sudo_granted",
    message: "Identity confirmed",
  }),
  sudo_failed: msg({
    id: "audit.event.sudo_failed",
    message: "Identity check failed",
  }),
  diagnostic_lookup: msg({
    id: "audit.event.diagnostic_lookup",
    message: "Diagnostic lookup",
  }),
  vrchat_operator_session_invalidated: msg({
    id: "audit.event.vrchat_operator_session_invalidated",
    message: "VRChat operator signed out",
  }),
} satisfies Record<string, MessageDescriptor>;

export type AuditFactor = keyof typeof auditFactors;
export type AuditEvent = keyof typeof auditEvents;

export const auditFactorKeys = Object.keys(auditFactors) as AuditFactor[];
export const auditEventKeys = Object.keys(auditEvents) as AuditEvent[];

/** Events that record something going wrong, marked so they stand out. */
export const failureEvents: ReadonlySet<string> = new Set<AuditEvent>([
  "fail",
  "sudo_failed",
  "access_denied",
  "factor_locked",
  "clone_warning",
]);

export function isAuditFactor(value: string): value is AuditFactor {
  return Object.hasOwn(auditFactors, value);
}

export function isAuditEvent(value: string): value is AuditEvent {
  return Object.hasOwn(auditEvents, value);
}
