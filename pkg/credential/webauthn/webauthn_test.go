package webauthn

import (
	"testing"

	"github.com/go-webauthn/webauthn/protocol"
)

// TestLoginOptionsRequireUserVerification pins the login ceremony to
// UserVerification=Required. Credentials are registered UV=Required, so every
// passkey is UV-capable; requesting Required at assertion makes go-webauthn
// verify the asserted UV flag (shouldVerifyUser) and reject a presence-only
// (UV=0) assertion — closing the sudo/login UV-downgrade (audit WACER-1).
func TestLoginOptionsRequireUserVerification(t *testing.T) {
	opts := &protocol.PublicKeyCredentialRequestOptions{}
	for _, apply := range LoginOptions() {
		apply(opts)
	}
	if opts.UserVerification != protocol.VerificationRequired {
		t.Fatalf("LoginOptions UserVerification = %q, want %q", opts.UserVerification, protocol.VerificationRequired)
	}
}

// TestRegistrationOptionsRequireUserVerification documents the registration
// side of the invariant: every credential is minted UV-capable.
func TestRegistrationOptionsRequireUserVerification(t *testing.T) {
	cc := &protocol.PublicKeyCredentialCreationOptions{}
	for _, apply := range RegistrationOptions(nil) {
		apply(cc)
	}
	if cc.AuthenticatorSelection.UserVerification != protocol.VerificationRequired {
		t.Fatalf("RegistrationOptions UserVerification = %q, want %q", cc.AuthenticatorSelection.UserVerification, protocol.VerificationRequired)
	}
}
