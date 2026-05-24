package auth

import (
	"context"
	"strings"

	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/logx"
)

// MapLoginCeremonyError classifies an error returned by go-webauthn's
// FinishPasskeyLogin into a friendly typed AuthError. Raw library details are
// logged at WARN for operators; the returned error has a user-facing message
// only. Pass ctx so the log carries request-scoped fields.
func MapLoginCeremonyError(ctx context.Context, err error) *AuthError {
	if err == nil {
		return nil
	}
	msg := err.Error()
	mapped := classifyLogin(msg)
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event":          "auth.webauthn_error",
		"operation":      "login",
		"mapped_code":    mapped.Code,
		"library_detail": msg,
	}).Warn("auth")
	return mapped
}

// MapRegisterCeremonyError is the same but for go-webauthn's CreateCredential
// (registration). Pattern-matches different cases.
func MapRegisterCeremonyError(ctx context.Context, err error) *AuthError {
	if err == nil {
		return nil
	}
	msg := err.Error()
	mapped := classifyRegister(msg)
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event":          "auth.webauthn_error",
		"operation":      "register",
		"mapped_code":    mapped.Code,
		"library_detail": msg,
	}).Warn("auth")
	return mapped
}

func classifyLogin(msg string) *AuthError {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "unknown user handle"),
		strings.Contains(lower, "failed to lookup"),
		strings.Contains(lower, "could not find"):
		return ErrLoginAccountNotFound()
	case strings.Contains(lower, "signature"),
		strings.Contains(lower, "verification"),
		strings.Contains(lower, "verify"):
		return ErrLoginVerificationFailed()
	case strings.Contains(lower, "challenge"),
		strings.Contains(lower, "expired"),
		strings.Contains(lower, "timeout"):
		return ErrCeremonyExpired()
	default:
		return ErrLoginFailed()
	}
}

func classifyRegister(msg string) *AuthError {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "exclude"),
		strings.Contains(lower, "credential already"),
		strings.Contains(lower, "duplicate"):
		return ErrRegistrationCredentialExists()
	case strings.Contains(lower, "challenge"),
		strings.Contains(lower, "expired"),
		strings.Contains(lower, "timeout"):
		return ErrCeremonyExpired()
	default:
		return ErrRegistrationFailed()
	}
}
