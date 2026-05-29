package authn

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// AuthError is the canonical error type returned from auth checks and handlers.
// HTTP status, machine-readable code, and human message are all surfaced together;
// the registerOp helper and HTTP error writers project these into wire responses.
//
// RetryAfter is optional; when non-zero, HTTP error writers emit a Retry-After
// header with the duration rounded up to whole seconds. Used by 429 responses
// (rate-limit and factor-lockout) to give clients a back-off hint.
type AuthError struct {
	Status     int
	Code       string
	Message    string
	RetryAfter time.Duration
}

func (e *AuthError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

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

func ErrInvalidRole() *AuthError {
	return newErr(http.StatusBadRequest, "invalid_role", "角色无效")
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
	return &AuthError{
		Status:     http.StatusTooManyRequests,
		Code:       "factor_locked",
		Message:    "尝试次数过多，请稍后再试",
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
// when the upstream OP responds with an error= query param. Embeds the
// upstream code + description in the user-facing message; the raw values are
// also emitted to the audit log by the handler.
func ErrUpstreamError(upstreamCode, description string) *AuthError {
	msg := "上游 IdP 拒绝授权"
	switch {
	case upstreamCode != "" && description != "":
		msg = fmt.Sprintf("上游 IdP 拒绝授权 (%s)：%s", upstreamCode, description)
	case upstreamCode != "":
		msg = fmt.Sprintf("上游 IdP 拒绝授权：%s", upstreamCode)
	case description != "":
		msg = fmt.Sprintf("上游 IdP 拒绝授权：%s", description)
	}
	return newErr(http.StatusBadRequest, "upstream_error", msg)
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
