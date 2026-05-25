// Package authn — flow.go
//
// AvailableMethods resolves "which sign-in methods does this account have?"
// Used by the /me/sudo/methods endpoint (Task 6) and the login UI (v0.6) to
// render the credential management surface.
//
// DisableNonWebAuthnFallbacks clears password + TOTP + recovery codes for an
// account. Called by /me/auth/revoke-password-totp (Task 7).

package authn

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
)

var ErrNoUsableMethod = errors.New("authn: account has no usable sign-in method; admin recovery required")

type Method string

const (
	MethodWebAuthn       Method = "webauthn"
	MethodPasswordTOTP   Method = "password_totp"
	MethodFederationOIDC Method = "federation_oidc"
)

// FlowQueries is the subset of db.Querier this package's flow helpers need.
type FlowQueries interface {
	ListCredentialsByAccount(ctx context.Context, accountID int32) ([]db.WebauthnCredential, error)
	GetPasswordCredential(ctx context.Context, accountID int32) (db.PasswordCredential, error)
	GetTOTPCredential(ctx context.Context, accountID int32) (db.TotpCredential, error)
	DeletePasswordCredential(ctx context.Context, accountID int32) error
	DeleteTOTPCredential(ctx context.Context, accountID int32) error
	DeleteAllRecoveryCodesByAccount(ctx context.Context, accountID int32) error
}

func AvailableMethods(ctx context.Context, q FlowQueries, accountID int32) ([]Method, error) {
	var methods []Method

	creds, err := q.ListCredentialsByAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("AvailableMethods: list webauthn: %w", err)
	}
	if len(creds) > 0 {
		methods = append(methods, MethodWebAuthn)
	}

	hasPassword := false
	if _, err := q.GetPasswordCredential(ctx, accountID); err == nil {
		hasPassword = true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("AvailableMethods: get password: %w", err)
	}

	hasConfirmedTOTP := false
	if totpRow, err := q.GetTOTPCredential(ctx, accountID); err == nil {
		if totpRow.ConfirmedAt.Valid {
			hasConfirmedTOTP = true
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("AvailableMethods: get totp: %w", err)
	}

	if hasPassword && hasConfirmedTOTP {
		methods = append(methods, MethodPasswordTOTP)
	}

	if len(methods) == 0 {
		return nil, ErrNoUsableMethod
	}
	return methods, nil
}

// DisableNonWebAuthnFallbacks deletes password + TOTP + recovery rows for the
// account. Each delete is independent — a missing row is a no-op at the SQL
// level. Returns the first error encountered, wrapped with context.
//
// v0.2 does NOT wrap these in a Postgres transaction; partial failure is
// recoverable by retrying the endpoint. Hardening is deferred to v0.3+.
func DisableNonWebAuthnFallbacks(ctx context.Context, q FlowQueries, w audit.Writer, accountID int32) error {
	if err := q.DeletePasswordCredential(ctx, accountID); err != nil {
		return fmt.Errorf("DisableNonWebAuthnFallbacks: delete password: %w", err)
	}
	if err := q.DeleteTOTPCredential(ctx, accountID); err != nil {
		return fmt.Errorf("DisableNonWebAuthnFallbacks: delete totp: %w", err)
	}
	if err := q.DeleteAllRecoveryCodesByAccount(ctx, accountID); err != nil {
		return fmt.Errorf("DisableNonWebAuthnFallbacks: delete recovery: %w", err)
	}
	if w != nil {
		for _, factor := range []audit.Factor{
			audit.FactorPassword,
			audit.FactorTOTP,
			audit.FactorRecoveryCode,
		} {
			_ = w.Record(ctx, audit.Record{
				AccountID: &accountID,
				Factor:    factor,
				Event:     audit.EventRevoke,
			})
		}
	}
	return nil
}
