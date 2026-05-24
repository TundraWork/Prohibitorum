package webauthn

import (
	"fmt"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
)

// NewWebAuthn constructs the project's WebAuthn library handle from a
// WebAuthnConfig. Fails fast if RP origins or RP ID are misconfigured —
// better to crash at startup than emit ceremonies the browser rejects.
func NewWebAuthn(cfg configx.WebAuthnConfig) (*webauthn.WebAuthn, error) {
	if len(cfg.RPOrigins) == 0 {
		return nil, fmt.Errorf("webauthn: PROHIBITORUM_PUBLIC_ORIGIN must be set (comma-separated list of origins)")
	}
	if cfg.RPID == "" {
		return nil, fmt.Errorf("webauthn: PROHIBITORUM_WEBAUTHN_RP_ID must be set (or derivable from PUBLIC_ORIGIN)")
	}
	return webauthn.New(&webauthn.Config{
		RPID:                  cfg.RPID,
		RPDisplayName:         cfg.RPDisplayName,
		RPOrigins:             cfg.RPOrigins,
		AttestationPreference: protocol.PreferNoAttestation,
		Timeouts: webauthn.TimeoutsConfig{
			Login: webauthn.TimeoutConfig{
				Enforce: true,
				Timeout: 60 * time.Second,
			},
			Registration: webauthn.TimeoutConfig{
				Enforce: true,
				Timeout: 120 * time.Second,
			},
		},
	})
}

// WebAuthnAccount adapts a db.Account plus its credentials so the go-webauthn
// library can produce protocol-correct user metadata. The library's User
// interface is small — ID (opaque handle), Name, DisplayName, and the list of
// credentials registered to this user.
type WebAuthnAccount struct {
	Account     *db.Account
	Credentials []db.WebauthnCredential
}

// WebAuthnID returns the opaque user handle (NOT account.id, per the WebAuthn spec).
func (w *WebAuthnAccount) WebAuthnID() []byte { return w.Account.WebauthnUserHandle }

// WebAuthnName is the username — used by authenticators to disambiguate.
func (w *WebAuthnAccount) WebAuthnName() string { return w.Account.Username }

// WebAuthnDisplayName is the human-friendly name shown in the authenticator UI.
func (w *WebAuthnAccount) WebAuthnDisplayName() string { return w.Account.DisplayName }

// WebAuthnCredentials projects each persisted credential into the library's
// Credential shape so ceremonies can verify against them.
func (w *WebAuthnAccount) WebAuthnCredentials() []webauthn.Credential {
	out := make([]webauthn.Credential, 0, len(w.Credentials))
	for _, c := range w.Credentials {
		transports := make([]protocol.AuthenticatorTransport, 0, len(c.Transports))
		for _, t := range c.Transports {
			transports = append(transports, protocol.AuthenticatorTransport(t))
		}
		// backup_eligible and backup_state are nullable in the new schema;
		// default to false if not set (legacy rows or unset on older authenticators).
		backupEligible := c.BackupEligible.Valid && c.BackupEligible.Bool
		backupState := c.BackupState.Valid && c.BackupState.Bool
		// attestation_type is nullable; default to empty string if not set.
		attType := ""
		if c.AttestationType.Valid {
			attType = c.AttestationType.String
		}
		out = append(out, webauthn.Credential{
			ID:              c.CredentialID,
			PublicKey:       c.PublicKey,
			AttestationType: attType,
			Transport:       transports,
			Flags: webauthn.CredentialFlags{
				BackupEligible: backupEligible,
				BackupState:    backupState,
			},
			Authenticator: webauthn.Authenticator{
				AAGUID:    c.Aaguid,
				SignCount: uint32(c.SignCount),
			},
		})
	}
	return out
}

// RegistrationOptions are the per-ceremony options used for BeginRegistration.
// Policy split per design.md §5:
//   - UV=Required at registration to force UV-capable authenticators (the
//     credential carries the UV flag forever after).
//   - ResidentKey=Required so credentials are discoverable, enabling the
//     username-less login flow.
//
// `exclude` lists existing credentials to prevent the same authenticator from
// double-registering (used by /me/credentials/register/begin when the caller
// already has passkeys).
func RegistrationOptions(exclude []protocol.CredentialDescriptor) []webauthn.RegistrationOption {
	opts := []webauthn.RegistrationOption{
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:      protocol.ResidentKeyRequirementRequired,
			UserVerification: protocol.VerificationRequired,
		}),
	}
	if len(exclude) > 0 {
		opts = append(opts, webauthn.WithExclusions(exclude))
	}
	return opts
}

// LoginOptions are the per-ceremony options used for BeginDiscoverableLogin.
// UV=Preferred for smooth platform-authenticator UX (the credential already
// carries the UV flag from registration). No allowCredentials → discoverable.
func LoginOptions() []webauthn.LoginOption {
	return []webauthn.LoginOption{
		webauthn.WithUserVerification(protocol.VerificationPreferred),
	}
}
