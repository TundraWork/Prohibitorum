package authn

import (
	"errors"
	"fmt"
	"net/http"
)

// AuthError is the canonical error type returned from auth checks and handlers.
// HTTP status, machine-readable code, and human message are all surfaced together;
// the registerOp helper and HTTP error writers project these into wire responses.
type AuthError struct {
	Status  int
	Code    string
	Message string
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

// ErrSudoRequired is returned when a sensitive action needs a fresh
// WebAuthn assertion within the sudo window. Status 401 with a code the
// dashboard can branch on to trigger the sudo flow + retry.
func ErrSudoRequired() *AuthError {
	return newErr(http.StatusUnauthorized, "sudo_required", "敏感操作需要重新验证 Passkey")
}

func ErrSessionNotFound() *AuthError {
	return newErr(http.StatusNotFound, "session_not_found", "会话不存在或已过期")
}

func ErrCannotRevokeCurrentSession() *AuthError {
	return newErr(http.StatusConflict, "cannot_revoke_current_session", "无法从此处撤销当前会话，请使用「退出登录」")
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
