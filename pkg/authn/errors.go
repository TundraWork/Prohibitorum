package authn

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"prohibitorum/pkg/weberr"
)

// init registers every AuthError code with the weberr registry so that the
// public-error envelope ({code, details?, requestId}) can validate detail keys
// and look up HTTP status / diagnostic metadata. Codes here must match the
// Code field in the constructors below — the registry is the single source of
// truth for what may appear in a public response.
func init() {
	defs := []weberr.Definition{
		{Code: "no_session", Status: http.StatusUnauthorized, LocaleKey: "errors.no_session", DiagnosticKind: "auth", Recovery: "reauth"},
		{Code: "not_admin", Status: http.StatusForbidden, LocaleKey: "errors.not_admin", DiagnosticKind: "auth"},
		{Code: "permission_denied", Status: http.StatusForbidden, LocaleKey: "errors.permission_denied", DiagnosticKind: "auth"},
		{Code: "account_disabled", Status: http.StatusForbidden, LocaleKey: "errors.account_disabled", DiagnosticKind: "auth"},
		{Code: "last_admin", Status: http.StatusConflict, LocaleKey: "errors.last_admin", DiagnosticKind: "policy"},
		{Code: "admin_cannot_be_disabled", Status: http.StatusConflict, LocaleKey: "errors.admin_cannot_be_disabled", DiagnosticKind: "policy"},
		{Code: "cannot_delete_self", Status: http.StatusConflict, LocaleKey: "errors.cannot_delete_self", DiagnosticKind: "policy"},
		{Code: "username_taken", Status: http.StatusConflict, LocaleKey: "errors.username_taken", DiagnosticKind: "validation"},
		{Code: "enrollment_expired", Status: http.StatusGone, LocaleKey: "errors.enrollment_expired", DiagnosticKind: "enrollment"},
		{Code: "enrollment_consumed", Status: http.StatusGone, LocaleKey: "errors.enrollment_consumed", DiagnosticKind: "enrollment"},
		{Code: "enrollment_federation_required", Status: http.StatusBadRequest, LocaleKey: "errors.enrollment_federation_required", DiagnosticKind: "enrollment"},
		{Code: "bad_request", Status: http.StatusBadRequest, LocaleKey: "errors.bad_request", DiagnosticKind: "validation"},
		{Code: "invalid_consent_ticket", Status: http.StatusBadRequest, LocaleKey: "errors.invalid_consent_ticket", DiagnosticKind: "validation"},
		{Code: "invalid_role", Status: http.StatusBadRequest, LocaleKey: "errors.invalid_role", DiagnosticKind: "validation", DetailKeys: map[string]struct{}{"allowed": {}}},
		{Code: "invalid_username", Status: http.StatusBadRequest, LocaleKey: "errors.invalid_username", DiagnosticKind: "validation"},
		{Code: "invalid_nickname", Status: http.StatusBadRequest, LocaleKey: "errors.invalid_nickname", DiagnosticKind: "validation"},
		{Code: "invalid_display_name", Status: http.StatusBadRequest, LocaleKey: "errors.invalid_display_name", DiagnosticKind: "validation"},
		{Code: "username_immutable", Status: http.StatusBadRequest, LocaleKey: "errors.username_immutable", DiagnosticKind: "validation"},
		{Code: "last_passkey", Status: http.StatusBadRequest, LocaleKey: "errors.last_passkey", DiagnosticKind: "policy"},
		{Code: "would_remove_last_factor", Status: http.StatusConflict, LocaleKey: "errors.would_remove_last_factor", DiagnosticKind: "policy"},
		{Code: "login_account_not_found", Status: http.StatusUnauthorized, LocaleKey: "errors.login_account_not_found", DiagnosticKind: "auth"},
		{Code: "login_verification_failed", Status: http.StatusUnauthorized, LocaleKey: "errors.login_verification_failed", DiagnosticKind: "auth"},
		{Code: "ceremony_expired", Status: http.StatusBadRequest, LocaleKey: "errors.ceremony_expired", DiagnosticKind: "ceremony", Retryable: true, Recovery: "retry"},
		{Code: "ceremony_missing", Status: http.StatusBadRequest, LocaleKey: "errors.ceremony_missing", DiagnosticKind: "ceremony"},
		{Code: "ceremony_state_invalid", Status: http.StatusInternalServerError, LocaleKey: "errors.ceremony_state_invalid", DiagnosticKind: "ceremony"},
		{Code: "credential_already_registered", Status: http.StatusConflict, LocaleKey: "errors.credential_already_registered", DiagnosticKind: "ceremony"},
		{Code: "registration_failed", Status: http.StatusBadRequest, LocaleKey: "errors.registration_failed", DiagnosticKind: "ceremony"},
		{Code: "login_failed", Status: http.StatusUnauthorized, LocaleKey: "errors.login_failed", DiagnosticKind: "auth", Retryable: true, Recovery: "retry"},
		{Code: "bad_credentials", Status: http.StatusUnauthorized, LocaleKey: "errors.bad_credentials", DiagnosticKind: "auth"},
		{Code: "partial_session_invalid", Status: http.StatusUnauthorized, LocaleKey: "errors.partial_session_invalid", DiagnosticKind: "auth", Recovery: "reauth"},
		{Code: "recovery_session_invalid", Status: http.StatusUnauthorized, LocaleKey: "errors.recovery_session_invalid", DiagnosticKind: "auth", Recovery: "reauth"},
		{Code: "account_not_found", Status: http.StatusNotFound, LocaleKey: "errors.account_not_found", DiagnosticKind: "resource"},
		{Code: "credential_not_found", Status: http.StatusNotFound, LocaleKey: "errors.credential_not_found", DiagnosticKind: "resource"},
		{Code: "invitation_not_found", Status: http.StatusNotFound, LocaleKey: "errors.invitation_not_found", DiagnosticKind: "resource"},
		{Code: "not_bootstrapped", Status: http.StatusServiceUnavailable, LocaleKey: "errors.not_bootstrapped", DiagnosticKind: "system"},
		{Code: "maintenance_mode", Status: http.StatusServiceUnavailable, LocaleKey: "errors.maintenance_mode", DiagnosticKind: "system", Retryable: true, Recovery: "retry"},
		{Code: "pairing_not_found", Status: http.StatusNotFound, LocaleKey: "errors.pairing_not_found", DiagnosticKind: "pairing"},
		{Code: "pairing_state", Status: http.StatusConflict, LocaleKey: "errors.pairing_state", DiagnosticKind: "pairing"},
		{Code: "pairing_expired", Status: http.StatusGone, LocaleKey: "errors.pairing_expired", DiagnosticKind: "pairing"},
		{Code: "pairing_not_approved", Status: http.StatusPreconditionRequired, LocaleKey: "errors.pairing_not_approved", DiagnosticKind: "pairing"},
		{Code: "rate_limited", Status: http.StatusTooManyRequests, LocaleKey: "errors.rate_limited", DiagnosticKind: "throttle", Retryable: true, Recovery: "retry", DetailKeys: map[string]struct{}{"retryAfterSeconds": {}}},
		{Code: "factor_locked", Status: http.StatusTooManyRequests, LocaleKey: "errors.factor_locked", DiagnosticKind: "throttle", Retryable: true, Recovery: "retry", DetailKeys: map[string]struct{}{"retryAfterSeconds": {}}},
		{Code: "sudo_required", Status: http.StatusUnauthorized, LocaleKey: "errors.sudo_required", DiagnosticKind: "auth", Recovery: "reauth"},
		{Code: "sudo_method_unavailable", Status: http.StatusBadRequest, LocaleKey: "errors.sudo_method_unavailable", DiagnosticKind: "auth"},
		{Code: "session_not_found", Status: http.StatusNotFound, LocaleKey: "errors.session_not_found", DiagnosticKind: "resource"},
		{Code: "cannot_revoke_current_session", Status: http.StatusConflict, LocaleKey: "errors.cannot_revoke_current_session", DiagnosticKind: "policy"},
		{Code: "email_not_verified", Status: http.StatusForbidden, LocaleKey: "errors.email_not_verified", DiagnosticKind: "federation"},
		{Code: "username_collision", Status: http.StatusForbidden, LocaleKey: "errors.username_collision", DiagnosticKind: "federation"},
		{Code: "invite_required", Status: http.StatusForbidden, LocaleKey: "errors.invite_required", DiagnosticKind: "federation"},
		{Code: "link_required", Status: http.StatusForbidden, LocaleKey: "errors.link_required", DiagnosticKind: "federation"},
		{Code: "federation_state_invalid", Status: http.StatusUnauthorized, LocaleKey: "errors.federation_state_invalid", DiagnosticKind: "federation", Recovery: "retry"},
		{Code: "last_sign_in_method", Status: http.StatusBadRequest, LocaleKey: "errors.last_sign_in_method", DiagnosticKind: "policy"},
		{Code: "invalid_return_to", Status: http.StatusBadRequest, LocaleKey: "errors.invalid_return_to", DiagnosticKind: "validation"},
		{Code: "upstream_error", Status: http.StatusBadRequest, LocaleKey: "errors.upstream_error", DiagnosticKind: "federation", DetailKeys: map[string]struct{}{"upstreamCode": {}}},
		{Code: "active_key_no_replacement", Status: http.StatusConflict, LocaleKey: "errors.active_key_no_replacement", DiagnosticKind: "policy"},
		{Code: "client_not_found", Status: http.StatusNotFound, LocaleKey: "errors.client_not_found", DiagnosticKind: "resource"},
		{Code: "upstream_idp_not_found", Status: http.StatusNotFound, LocaleKey: "errors.upstream_idp_not_found", DiagnosticKind: "resource"},
		{Code: "provider_not_ready", Status: http.StatusServiceUnavailable, LocaleKey: "errors.provider_not_ready", DiagnosticKind: "federation"},
		{Code: "vrchat_operator_credentials_invalid", Status: http.StatusUnauthorized, LocaleKey: "errors.vrchat_operator_credentials_invalid", DiagnosticKind: "federation"},
		{Code: "vrchat_operator_challenge_invalid", Status: http.StatusBadRequest, LocaleKey: "errors.vrchat_operator_challenge_invalid", DiagnosticKind: "federation"},
		{Code: "vrchat_operator_verification_failed", Status: http.StatusUnauthorized, LocaleKey: "errors.vrchat_operator_verification_failed", DiagnosticKind: "federation", Retryable: true, Recovery: "retry"},
		{Code: "vrchat_upstream_unavailable", Status: http.StatusServiceUnavailable, LocaleKey: "errors.vrchat_upstream_unavailable", DiagnosticKind: "federation", Retryable: true, Recovery: "retry"},
		{Code: "oidc_client_already_exists", Status: http.StatusConflict, LocaleKey: "errors.oidc_client_already_exists", DiagnosticKind: "validation"},
		{Code: "upstream_idp_already_exists", Status: http.StatusConflict, LocaleKey: "errors.upstream_idp_already_exists", DiagnosticKind: "validation"},
		{Code: "saml_application_already_exists", Status: http.StatusConflict, LocaleKey: "errors.saml_application_already_exists", DiagnosticKind: "validation"},
		{Code: "group_not_found", Status: http.StatusNotFound, LocaleKey: "errors.group_not_found", DiagnosticKind: "resource"},
		{Code: "group_slug_conflict", Status: http.StatusConflict, LocaleKey: "errors.group_slug_conflict", DiagnosticKind: "validation"},
	}
	if err := weberr.Register(defs); err != nil {
		panic(fmt.Sprintf("authn: failed to register error definitions: %v", err))
	}
}

// AuthError is the canonical error type returned from auth checks and handlers.
// The Code is the registered weberr code (the single source of truth for
// HTTP status, locale key, and diagnostic kind). Message is vestigial —
// kept for internal logging only and NEVER serialized to the wire. The
// typed Huma path and writeAuthErr both project AuthError into a
// weberr.PublicError via the PublicError() method.
//
// Details carries curated safe detail values (e.g. allowed roles,
// retryAfterSeconds) validated against the registry's DetailKeys.
//
// RetryAfter is optional; when non-zero, HTTP error writers emit a Retry-After
// header with the duration rounded up to whole seconds. Used by 429 responses
// (rate-limit and factor-lockout) to give clients a back-off hint.
type AuthError struct {
	Status     int
	Code       string
	Message    string
	Details    map[string]any
	RetryAfter time.Duration
}

func (e *AuthError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// PublicError projects this AuthError into a weberr.PublicError for the wire
// envelope. Implements weberr.PublicErrorProvider so weberr.AsPublic can
// extract the public error without importing authn. The details map is
// validated against the registry at New time; here we trust it.
func (e *AuthError) PublicError() *weberr.PublicError {
	return &weberr.PublicError{
		Code:    e.Code,
		Details: e.Details,
	}
}

func newErr(status int, code, message string) *AuthError {
	return &AuthError{Status: status, Code: code, Message: message}
}

// Constructors return fresh values so callers can decorate / re-wrap without
// sharing state. Match the catalogue in
// specs/2026-05-23-auth-system/design.md §8.

func ErrNoSession() *AuthError {
	return newErr(http.StatusUnauthorized, "no_session", "请先登录")
}

func ErrNotAdmin() *AuthError {
	return newErr(http.StatusForbidden, "not_admin", "需要管理员权限")
}

func ErrPermissionDenied() *AuthError {
	return newErr(http.StatusForbidden, "permission_denied", "权限不足")
}

func ErrAccountDisabled() *AuthError {
	return newErr(http.StatusForbidden, "account_disabled", "账户已禁用")
}

func ErrLastAdmin() *AuthError {
	return newErr(http.StatusConflict, "last_admin", "无法降级、禁用或删除唯一的管理员")
}

// ErrAdminCannotBeDisabled rejects an update that would leave an account in
// the role=admin AND disabled=true state. Admins must be demoted to 'user'
// before they can be disabled. Keeps the active-admin set cleanly defined.
func ErrAdminCannotBeDisabled() *AuthError {
	return &AuthError{Code: "admin_cannot_be_disabled", Status: 409,
		Message: "无法禁用管理员账户，请先降级为标准用户"}
}

// ErrCannotDeleteSelf rejects an admin's attempt to delete their own account.
// Recoverable but surprising — ask another admin instead.
func ErrCannotDeleteSelf() *AuthError {
	return &AuthError{Code: "cannot_delete_self", Status: 409,
		Message: "无法删除自己的账户，请联系其他管理员"}
}

func ErrUsernameTaken() *AuthError {
	return newErr(http.StatusConflict, "username_taken", "用户名已存在")
}

func ErrEnrollmentExpired() *AuthError {
	return newErr(http.StatusGone, "enrollment_expired", "邀请链接已过期")
}

func ErrEnrollmentConsumed() *AuthError {
	return newErr(http.StatusGone, "enrollment_consumed", "邀请链接已使用")
}

// ErrEnrollmentFederationRequired is returned by the WebAuthn-based
// enrollment endpoints when the invite has been bound to a specific
// upstream IdP (expected_upstream_idp_slug set). The invitee MUST redeem
// via /api/prohibitorum/enrollments/{token}/start-federation; using the
// password+passkey enrollment path would silently override the admin's
// "must federate via this IdP" policy. Closes audit finding M1-int.
func ErrEnrollmentFederationRequired() *AuthError {
	return newErr(http.StatusBadRequest, "enrollment_federation_required",
		"此邀请须通过身份联合注册，请使用联合身份注册链接")
}

// ErrBadRequest is the generic 400 for malformed request bodies / out-of-range
// inputs where a finer-grained code would leak implementation detail or
// doesn't add operational value (e.g., password length bounds — exposing
// "too long" vs "too short" lets an attacker probe the boundary). Handlers
// should prefer a specific code when the failure mode is genuinely
// user-actionable; ErrBadRequest is the catch-all.
func ErrBadRequest() *AuthError {
	return newErr(http.StatusBadRequest, "bad_request", "请求参数无效")
}

// ErrInvalidConsentTicket is returned when a consent ticket is missing, expired,
// already used, or belongs to another account.
func ErrInvalidConsentTicket() *AuthError {
	return newErr(http.StatusBadRequest, "invalid_consent_ticket", "授权请求已失效，请重新发起登录")
}

func ErrInvalidRole() *AuthError {
	return &AuthError{
		Status:  http.StatusBadRequest,
		Code:    "invalid_role",
		Message: "角色无效",
		Details: map[string]any{"allowed": []string{"user", "admin"}},
	}
}

func ErrInvalidUsername() *AuthError {
	return newErr(http.StatusBadRequest, "invalid_username", "用户名必须为 2-32 个小写字母、数字、下划线或连字符")
}

func ErrInvalidNickname() *AuthError {
	return newErr(http.StatusBadRequest, "invalid_nickname", "昵称必须为 1-60 个字符且不含控制字符")
}

func ErrInvalidDisplayName() *AuthError {
	return newErr(http.StatusBadRequest, "invalid_display_name", "显示名长度必须为 1-128 字符且不含控制字符")
}

func ErrUsernameImmutable() *AuthError {
	return newErr(http.StatusBadRequest, "username_immutable", "用户名不可修改")
}

func ErrLastPasskey() *AuthError {
	return newErr(http.StatusBadRequest, "last_passkey", "无法删除最后一把密钥")
}

// ErrWouldRemoveLastFactorAuth is returned by DisableNonWebAuthnFallbacks when
// removing password+TOTP would leave the account with no passkeys and no usable
// federated identity, which would lock the account out entirely. Status 409
// Conflict; the caller must add a passkey or link a federated identity first.
func ErrWouldRemoveLastFactorAuth() *AuthError {
	return newErr(http.StatusConflict, "would_remove_last_factor", "无法撤销密码和 TOTP：账户将无任何可用登录方式，请先添加 Passkey 或联合身份")
}

// ErrLoginAccountNotFound is returned when the WebAuthn library can't resolve
// the credential's user handle to an account row — typically because the
// account was deleted or the credential was force-revoked. Distinct from
// account_disabled (account exists but is locked out).
func ErrLoginAccountNotFound() *AuthError {
	return newErr(http.StatusUnauthorized, "login_account_not_found", "未找到对应账户或凭证")
}

// ErrLoginVerificationFailed is returned when the credential's assertion
// signature, attestation, or other cryptographic verification fails.
func ErrLoginVerificationFailed() *AuthError {
	return newErr(http.StatusUnauthorized, "login_verification_failed", "凭证验证失败，请重试")
}

// ErrCeremonyExpired is returned when the challenge has expired (typically
// the 60s registration / 5min KV stash timeout).
func ErrCeremonyExpired() *AuthError {
	return newErr(http.StatusBadRequest, "ceremony_expired", "操作已超时，请重试")
}

// ErrCeremonyMissing is returned when the ceremony KV stash is gone (user
// hit /complete without a matching /begin, or the server restarted).
func ErrCeremonyMissing() *AuthError {
	return newErr(http.StatusBadRequest, "ceremony_missing", "Passkey 流程已失效，请重新开始")
}

// ErrCeremonyState is returned for unexpected ceremony-state problems
// (KV JSON unmarshal failure, missing bootstrap/invite ceremony struct).
// This is a 500-class — clients can't fix it by retrying.
func ErrCeremonyState() *AuthError {
	return newErr(http.StatusInternalServerError, "ceremony_state_invalid", "Passkey 流程状态异常，请重试")
}

// ErrRegistrationCredentialExists is returned when the user tries to register
// the same authenticator twice — go-webauthn's excludeCredentials check.
func ErrRegistrationCredentialExists() *AuthError {
	return newErr(http.StatusConflict, "credential_already_registered", "此设备的 Passkey 已注册")
}

// ErrRegistrationFailed is the generic friendly-fallback for registration
// ceremony errors not classified above. Server logs the raw detail at WARN.
func ErrRegistrationFailed() *AuthError {
	return newErr(http.StatusBadRequest, "registration_failed", "注册失败，请重试")
}

// ErrLoginFailed is the generic friendly-fallback for login ceremony errors
// not classified above. Server logs the raw detail at WARN.
func ErrLoginFailed() *AuthError {
	return newErr(http.StatusUnauthorized, "login_failed", "登录失败，请重试")
}

// ErrBadCredentials is the username-enumeration-safe response for step-1 of
// the Password+TOTP login: same code/message whether the username doesn't
// exist, the account has no password, or the password is wrong.
func ErrBadCredentials() *AuthError {
	return newErr(http.StatusUnauthorized, "bad_credentials", "用户名或密码错误")
}

// ErrPartialSessionInvalid is returned when a step-2 verify (TOTP or recovery
// code) presents a partial-session token that's missing, expired, or already
// consumed. Collapses all three into one code — no useful signal for an
// attacker probing token validity.
func ErrPartialSessionInvalid() *AuthError {
	return newErr(http.StatusUnauthorized, "partial_session_invalid", "登录会话已过期，请重新登录")
}

// ErrRecoverySessionInvalid is returned by the recovery-ceremony endpoints
// (/auth/recovery/totp/begin and /auth/recovery/totp/verify) when the
// recovery_session_token is missing, expired, or already consumed. Same
// collapse pattern as ErrPartialSessionInvalid: the three failure modes
// share a single code so attackers can't probe token state. The user must
// restart from /auth/password/begin → /auth/recovery-code/verify.
func ErrRecoverySessionInvalid() *AuthError {
	return newErr(http.StatusUnauthorized, "recovery_session_invalid", "恢复流程已失效，请重新使用恢复码")
}

func ErrAccountNotFound() *AuthError {
	return newErr(http.StatusNotFound, "account_not_found", "账户不存在")
}

func ErrCredentialNotFound() *AuthError {
	return newErr(http.StatusNotFound, "credential_not_found", "凭证不存在")
}

// ErrInvitationNotFound differentiates "token doesn't exist as a pending
// invitation" from "token consumed/expired" so the admin UI can show a clean
// message. We do NOT distinguish never-existed vs already-consumed/expired
// (mirrors the LoadEnrollment design — don't leak token-existence to clients).
func ErrInvitationNotFound() *AuthError {
	return &AuthError{Code: "invitation_not_found", Status: 404,
		Message: "邀请不存在"}
}

func ErrNotBootstrapped() *AuthError {
	return newErr(http.StatusServiceUnavailable, "not_bootstrapped", "系统尚未初始化，请在服务器上运行 `prohibitorum enroll-admin`")
}

// ErrMaintenanceMode is returned to non-admin principals while maintenance mode
// is on: login, dashboard self-service, OIDC/SAML SSO, and the forward-auth
// gateway all reject with it. Admins are exempt so they can still manage the
// instance and lift the mode. Status 503 (temporary, not a permanent denial)
// with a code the dashboard maps to the maintenance screen.
func ErrMaintenanceMode() *AuthError {
	return newErr(http.StatusServiceUnavailable, "maintenance_mode", "系统正在维护中，请稍后再试")
}

// Pairing errors — same not-found / state policy as enrollment: collapse
// "never existed", "expired", and "consumed" into one code so an attacker
// can't probe code validity.

func ErrPairingNotFound() *AuthError {
	return newErr(http.StatusNotFound, "pairing_not_found", "配对码无效、已使用或已过期")
}

func ErrPairingState() *AuthError {
	return newErr(http.StatusConflict, "pairing_state", "配对状态不允许此操作")
}

func ErrPairingExpired() *AuthError {
	return newErr(http.StatusGone, "pairing_expired", "配对码已过期，请重新生成")
}

func ErrPairingNotApproved() *AuthError {
	return newErr(http.StatusPreconditionRequired, "pairing_not_approved", "配对尚未在已登录设备上批准")
}

// ErrRateLimited is returned when a per-IP or per-account bucket is full.
// Status 429 lets clients (and proxies) treat it as a back-off signal; the
// handler should also populate Retry-After when possible.
func ErrRateLimited() *AuthError {
	return newErr(http.StatusTooManyRequests, "rate_limited", "请求过于频繁，请稍后再试")
}

// ErrFactorLocked is returned when an auth_throttle lockout window is active
// for a (account, factor) pair. RetryAfter carries the remaining lockout
// duration so the HTTP handler can set a Retry-After header.
func ErrFactorLocked(retryAfter time.Duration) *AuthError {
	secs := int((retryAfter + time.Second - 1) / time.Second)
	if secs < 1 {
		secs = 1
	}
	return &AuthError{
		Status:     http.StatusTooManyRequests,
		Code:       "factor_locked",
		Message:    "尝试次数过多，请稍后再试",
		Details:    map[string]any{"retryAfterSeconds": secs},
		RetryAfter: retryAfter,
	}
}

// ErrSudoRequired is returned when a sensitive action needs a fresh
// WebAuthn assertion within the sudo window. Status 401 with a code the
// dashboard can branch on to trigger the sudo flow + retry.
func ErrSudoRequired() *AuthError {
	return newErr(http.StatusUnauthorized, "sudo_required", "敏感操作需要重新验证 Passkey")
}

// ErrSudoMethodUnavailable is returned by /me/sudo/begin when the caller
// requests an elevation method (webauthn, password_totp, recovery_code) the
// account isn't enrolled in. The dashboard should consult
// /me/sudo/methods first; this error is a safety net.
func ErrSudoMethodUnavailable() *AuthError {
	return newErr(http.StatusBadRequest, "sudo_method_unavailable", "当前账户未启用所请求的二次验证方式")
}

func ErrSessionNotFound() *AuthError {
	return newErr(http.StatusNotFound, "session_not_found", "会话不存在或已过期")
}

func ErrCannotRevokeCurrentSession() *AuthError {
	return newErr(http.StatusConflict, "cannot_revoke_current_session", "无法从此处撤销当前会话，请使用「退出登录」")
}

// ErrEmailNotVerified is returned by federation/oidc auto_provision when the
// upstream IdP did not assert email_verified=true on the ID token and the
// per-IdP require_verified_email flag is set. Defense against an upstream
// that lets a user register with someone else's address.
func ErrEmailNotVerified() *AuthError {
	return newErr(http.StatusForbidden, "email_not_verified", "上游 IdP 未确认电子邮件，禁止自动开户")
}

// ErrUsernameCollision is returned by federation/oidc auto_provision when the
// upstream's preferred_username matches an existing local account. Admin
// intervention required — automatic merging is unsafe because the existing
// account may belong to a different person.
func ErrUsernameCollision() *AuthError {
	return newErr(http.StatusForbidden, "username_collision", "用户名已存在于本地，需管理员介入")
}

// ErrInviteRequired is returned by federation/oidc when an upstream identity
// has no prior account_identity row and the IdP mode is invite_only (or
// equivalent — e.g., domain not in allowed_domains under auto_provision).
// Collapses the two cases under one code so callers don't probe for which.
func ErrInviteRequired() *AuthError {
	return newErr(http.StatusForbidden, "invite_required", "此身份源仅限受邀注册，请联系管理员获取邀请")
}

// ErrLinkRequired is returned by federation/oidc when an upstream identity
// has no prior account_identity row and the IdP mode is link_only. The user
// must first sign in via another method and then link this upstream from
// the "我的身份" page.
func ErrLinkRequired() *AuthError {
	return newErr(http.StatusForbidden, "link_required", "此身份源仅限已绑定账户使用，请先以其他方式登录后在「我的身份」中绑定")
}

// ErrFederationStateInvalid is returned by federation/oidc callback handlers
// when the per-flow KV state is missing, expired, single-use already
// consumed, iss-mismatched, session-swapped, or code-exchange-failed. The
// failure modes are deliberately collapsed under one code: an attacker who
// can probe state validity from outside the flow gains a side channel into
// the federation pipeline. The audit log carries the structured reason for
// operators; the wire response does not.
func ErrFederationStateInvalid() *AuthError {
	return newErr(http.StatusUnauthorized, "federation_state_invalid", "联合登录流程已失效，请重新发起")
}

// ErrLastSignInMethod is returned by /me/identities/{id}/unlink (Task 8) when
// removing the identity would leave the account with zero sign-in methods.
// Callers must add another method (passkey, password+TOTP, or another
// federated identity) before unlinking the last one.
func ErrLastSignInMethod() *AuthError {
	return newErr(http.StatusBadRequest, "last_sign_in_method", "无法移除最后一种登录方式，请先添加其他方式")
}

// ErrInvalidReturnTo is returned by federation HTTP handlers (Task 7) when the
// return_to query param fails origin-allowlist validation. Defense against
// open-redirect via a federation round-trip.
func ErrInvalidReturnTo() *AuthError {
	return newErr(http.StatusBadRequest, "invalid_return_to", "return_to 不在允许的来源内")
}

// ErrUpstreamError is returned by /federation/oidc/{slug}/callback (Task 7)
// when the upstream OP responds with an error= query param. The upstream
// error code is carried in details (upstreamCode) so the frontend can
// display it. The raw description is NOT included in details — it is
// unchecked upstream text that may carry unsafe content. The description
// is logged to the audit log by the handler.
func ErrUpstreamError(upstreamCode, description string) *AuthError {
	details := map[string]any{}
	if upstreamCode != "" {
		details["upstreamCode"] = upstreamCode
	}
	return &AuthError{
		Status:  http.StatusBadRequest,
		Code:    "upstream_error",
		Message: "上游 IdP 拒绝授权",
		Details: details,
	}
}

// ErrActiveKeyNoReplacement is returned when an admin tries to retire the
// active signing key before activating a replacement. Status 409 Conflict.
func ErrActiveKeyNoReplacement() *AuthError {
	return newErr(http.StatusConflict, "active_key_no_replacement", "Activate a replacement key before retiring the active signing key.")
}

// ErrClientNotFound is returned when an OIDC client lookup by client_id yields
// no row. Status 404.
func ErrClientNotFound() *AuthError {
	return newErr(http.StatusNotFound, "client_not_found", "OIDC client not found.")
}

// ErrUpstreamIDPNotFound is returned when an upstream IdP lookup by slug yields
// no row. Status 404.
func ErrUpstreamIDPNotFound() *AuthError {
	return newErr(http.StatusNotFound, "upstream_idp_not_found", "Upstream IdP not found.")
}

func ErrProviderNotReady() *AuthError {
	return newErr(http.StatusServiceUnavailable, "provider_not_ready", "The identity provider is not ready.")
}

func ErrVRChatOperatorCredentialsInvalid() *AuthError {
	return newErr(http.StatusUnauthorized, "vrchat_operator_credentials_invalid", "VRChat operator credentials are invalid.")
}

func ErrVRChatOperatorChallengeInvalid() *AuthError {
	return newErr(http.StatusBadRequest, "vrchat_operator_challenge_invalid", "VRChat operator challenge is invalid.")
}

func ErrVRChatOperatorVerificationFailed() *AuthError {
	return newErr(http.StatusUnauthorized, "vrchat_operator_verification_failed", "VRChat operator verification failed.")
}

func ErrVRChatUpstreamUnavailable() *AuthError {
	return newErr(http.StatusServiceUnavailable, "vrchat_upstream_unavailable", "VRChat is temporarily unavailable.")
}

// ErrClientAlreadyExists is returned when an OIDC client insert violates the
// unique constraint on client_id. Status 409.
func ErrClientAlreadyExists() *AuthError {
	return newErr(http.StatusConflict, "oidc_client_already_exists", "An OIDC client with this client_id already exists.")
}

// ErrUpstreamIDPAlreadyExists is returned when an upstream IdP insert violates
// the unique constraint on slug. Status 409.
func ErrUpstreamIDPAlreadyExists() *AuthError {
	return newErr(http.StatusConflict, "upstream_idp_already_exists", "An upstream IdP with this slug already exists.")
}

// ErrSAMLApplicationAlreadyExists is returned when a SAML SP insert violates the
// unique constraint on entity_id. Status 409.
func ErrSAMLApplicationAlreadyExists() *AuthError {
	return newErr(http.StatusConflict, "saml_application_already_exists", "A SAML application with this entity_id already exists.")
}

// ErrGroupNotFound is returned when a group lookup by ID yields no row.
// Status 404.
func ErrGroupNotFound() *AuthError {
	return newErr(http.StatusNotFound, "group_not_found", "Group not found.")
}

// ErrGroupSlugConflict is returned when a group insert or update violates the
// unique constraint on slug. Status 409.
func ErrGroupSlugConflict() *AuthError {
	return newErr(http.StatusConflict, "group_slug_conflict", "A group with this slug already exists.")
}

// AsAuthError unwraps an error chain and returns the embedded *AuthError if any,
// or nil otherwise. Useful for handler error mapping.
func AsAuthError(err error) *AuthError {
	var a *AuthError
	if errors.As(err, &a) {
		return a
	}
	return nil
}
