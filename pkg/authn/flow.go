package authn

import (
	"context"
	"errors"

	"prohibitorum/pkg/db"
)

var ErrNoUsableMethod = errors.New("authn: account has no usable sign-in method; admin recovery required")

type Method string

const (
	MethodWebAuthn       Method = "webauthn"
	MethodPasswordTOTP   Method = "password_totp"
	MethodFederationOIDC Method = "federation_oidc"
)

// AvailableMethods returns the sign-in methods enrolled for an account in
// preference order: WebAuthn > password+TOTP > federation suggestion.
// TODO(v0.2+): query webauthn_credential / password_credential / totp_credential
// / account_identity rows and return the available methods.
func AvailableMethods(ctx context.Context, q db.Querier, accountID int32) ([]Method, error) {
	return nil, ErrNoUsableMethod
}

// DisableNonWebAuthnFallbacks transactionally deletes password_credential,
// totp_credential, and recovery_code rows for an account. Called after a
// successful WebAuthn enrollment when the user opts in to "disable backup."
// TODO(v0.2): tx { DeletePasswordCredential, DeleteTOTPCredential,
// DeleteAllRecoveryCodesByAccount } + audit event.
func DisableNonWebAuthnFallbacks(ctx context.Context, q db.Querier, accountID int32) error {
	return nil
}
