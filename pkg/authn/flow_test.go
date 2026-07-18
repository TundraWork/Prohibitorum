package authn

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/db"
)

type flowFake struct {
	db.Querier

	webauthnRows []db.WebauthnCredential
	passwordRow  *db.PasswordCredential
	totpRow      *db.TotpCredential
	usableFed    int64 // returned by CountUsableSignInFederation

	deletePasswordCalls int
	deleteTOTPCalls     int
	deleteRecoveryCalls int

	deletePasswordErr error
	deleteTOTPErr     error
	deleteRecoveryErr error
}

func (f *flowFake) ListCredentialsByAccount(_ context.Context, _ int32) ([]db.WebauthnCredential, error) {
	return f.webauthnRows, nil
}

func (f *flowFake) GetPasswordCredential(_ context.Context, _ int32) (db.PasswordCredential, error) {
	if f.passwordRow == nil {
		return db.PasswordCredential{}, pgx.ErrNoRows
	}
	return *f.passwordRow, nil
}

func (f *flowFake) GetTOTPCredential(_ context.Context, _ int32) (db.TotpCredential, error) {
	if f.totpRow == nil {
		return db.TotpCredential{}, pgx.ErrNoRows
	}
	return *f.totpRow, nil
}

func (f *flowFake) DeletePasswordCredential(_ context.Context, _ int32) error {
	f.deletePasswordCalls++
	return f.deletePasswordErr
}

func (f *flowFake) DeleteTOTPCredential(_ context.Context, _ int32) error {
	f.deleteTOTPCalls++
	return f.deleteTOTPErr
}

func (f *flowFake) DeleteAllRecoveryCodesByAccount(_ context.Context, _ int32) error {
	f.deleteRecoveryCalls++
	return f.deleteRecoveryErr
}

func (f *flowFake) CountUsableSignInFederation(_ context.Context, _ int32) (int64, error) {
	return f.usableFed, nil
}

func confirmed() *db.TotpCredential {
	return &db.TotpCredential{
		ConfirmedAt: pgtype.Timestamptz{Valid: true},
	}
}

func unconfirmed() *db.TotpCredential {
	return &db.TotpCredential{
		ConfirmedAt: pgtype.Timestamptz{Valid: false},
	}
}

func TestAvailableMethods_WebAuthnOnly(t *testing.T) {
	f := &flowFake{
		webauthnRows: []db.WebauthnCredential{{ID: 1}},
	}
	methods, err := AvailableMethods(context.Background(), f, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 1 || methods[0] != MethodWebAuthn {
		t.Errorf("methods = %v, want [%v]", methods, MethodWebAuthn)
	}
}

func TestAvailableMethods_PasswordTOTPOnly(t *testing.T) {
	f := &flowFake{
		passwordRow: &db.PasswordCredential{},
		totpRow:     confirmed(),
	}
	methods, err := AvailableMethods(context.Background(), f, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 1 || methods[0] != MethodPasswordTOTP {
		t.Errorf("methods = %v, want [%v]", methods, MethodPasswordTOTP)
	}
}

func TestAvailableMethods_Both(t *testing.T) {
	f := &flowFake{
		webauthnRows: []db.WebauthnCredential{{ID: 1}},
		passwordRow:  &db.PasswordCredential{},
		totpRow:      confirmed(),
	}
	methods, err := AvailableMethods(context.Background(), f, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 2 || methods[0] != MethodWebAuthn || methods[1] != MethodPasswordTOTP {
		t.Errorf("methods = %v, want [%v, %v]", methods, MethodWebAuthn, MethodPasswordTOTP)
	}
}

func TestAvailableMethods_FederationOnly(t *testing.T) {
	f := &flowFake{
		usableFed: 1,
	}
	methods, err := AvailableMethods(context.Background(), f, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 1 || methods[0] != MethodFederationOIDC {
		t.Errorf("methods = %v, want [%v]", methods, MethodFederationOIDC)
	}
}

func TestAvailableMethods_VRChatIdentityOnly(t *testing.T) {
	// CountUsableSignInFederation excludes VRChat identities because link-only
	// providers are not direct local sign-in methods.
	f := &flowFake{usableFed: 0}
	methods, err := AvailableMethods(context.Background(), f, 1)
	if !errors.Is(err, ErrNoUsableMethod) {
		t.Fatalf("error = %v, want ErrNoUsableMethod", err)
	}
	if len(methods) != 0 {
		t.Errorf("methods = %v, want no direct sign-in methods", methods)
	}
}

func TestAvailableMethods_WebAuthnAndFederation(t *testing.T) {
	f := &flowFake{
		webauthnRows: []db.WebauthnCredential{{ID: 1}},
		usableFed:    1,
	}
	methods, err := AvailableMethods(context.Background(), f, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 2 || methods[0] != MethodWebAuthn || methods[1] != MethodFederationOIDC {
		t.Errorf("methods = %v, want [%v, %v]", methods, MethodWebAuthn, MethodFederationOIDC)
	}
}

func TestAvailableMethods_PasswordTOTPAndFederation(t *testing.T) {
	f := &flowFake{
		passwordRow: &db.PasswordCredential{},
		totpRow:     confirmed(),
		usableFed:   1,
	}
	methods, err := AvailableMethods(context.Background(), f, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 2 || methods[0] != MethodPasswordTOTP || methods[1] != MethodFederationOIDC {
		t.Errorf("methods = %v, want [%v, %v]", methods, MethodPasswordTOTP, MethodFederationOIDC)
	}
}

func TestAvailableMethods_UnconfirmedTOTPDoesNotCount(t *testing.T) {
	f := &flowFake{
		passwordRow: &db.PasswordCredential{},
		totpRow:     unconfirmed(),
	}
	methods, err := AvailableMethods(context.Background(), f, 1)
	if !errors.Is(err, ErrNoUsableMethod) {
		t.Errorf("err = %v, want ErrNoUsableMethod", err)
	}
	if methods != nil {
		t.Errorf("methods = %v, want nil", methods)
	}
}

func TestAvailableMethods_PasswordOnlyDoesNotCount(t *testing.T) {
	f := &flowFake{
		passwordRow: &db.PasswordCredential{},
	}
	methods, err := AvailableMethods(context.Background(), f, 1)
	if !errors.Is(err, ErrNoUsableMethod) {
		t.Errorf("err = %v, want ErrNoUsableMethod", err)
	}
	if methods != nil {
		t.Errorf("methods = %v, want nil", methods)
	}
}

func TestAvailableMethods_TOTPOnlyDoesNotCount(t *testing.T) {
	f := &flowFake{
		totpRow: confirmed(),
	}
	methods, err := AvailableMethods(context.Background(), f, 1)
	if !errors.Is(err, ErrNoUsableMethod) {
		t.Errorf("err = %v, want ErrNoUsableMethod", err)
	}
	if methods != nil {
		t.Errorf("methods = %v, want nil", methods)
	}
}

func TestAvailableMethods_EmptyReturnsErrNoUsableMethod(t *testing.T) {
	f := &flowFake{}
	methods, err := AvailableMethods(context.Background(), f, 1)
	if !errors.Is(err, ErrNoUsableMethod) {
		t.Errorf("err = %v, want ErrNoUsableMethod", err)
	}
	if methods != nil {
		t.Errorf("methods = %v, want nil", methods)
	}
}

type captureWriter struct {
	records []audit.Record
	err     error
}

func (c *captureWriter) Record(_ context.Context, r audit.Record) error {
	c.records = append(c.records, r)
	return c.err
}

func TestDisableNonWebAuthnFallbacks_DeletesAllThree(t *testing.T) {
	f := &flowFake{usableFed: 1}
	w := &captureWriter{}

	if err := DisableNonWebAuthnFallbacks(context.Background(), f, w, 42); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.deletePasswordCalls != 1 {
		t.Errorf("deletePasswordCalls = %d, want 1", f.deletePasswordCalls)
	}
	if f.deleteTOTPCalls != 1 {
		t.Errorf("deleteTOTPCalls = %d, want 1", f.deleteTOTPCalls)
	}
	if f.deleteRecoveryCalls != 1 {
		t.Errorf("deleteRecoveryCalls = %d, want 1", f.deleteRecoveryCalls)
	}
}

func TestDisableNonWebAuthnFallbacks_Idempotent(t *testing.T) {
	f := &flowFake{usableFed: 1}
	w := &captureWriter{}

	if err := DisableNonWebAuthnFallbacks(context.Background(), f, w, 42); err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if err := DisableNonWebAuthnFallbacks(context.Background(), f, w, 42); err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if f.deletePasswordCalls != 2 || f.deleteTOTPCalls != 2 || f.deleteRecoveryCalls != 2 {
		t.Errorf("delete call counts = (pw=%d,totp=%d,rec=%d), want all 2",
			f.deletePasswordCalls, f.deleteTOTPCalls, f.deleteRecoveryCalls)
	}
}

func TestDisableNonWebAuthnFallbacks_EmitsAuditEvents(t *testing.T) {
	f := &flowFake{usableFed: 1}
	w := &captureWriter{}

	if err := DisableNonWebAuthnFallbacks(context.Background(), f, w, 42); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(w.records) != 3 {
		t.Fatalf("len(records) = %d, want 3", len(w.records))
	}

	// Revoke order is recovery → TOTP → password (Bundle 1 / Fix 5):
	// safer under partial failure than the pre-bundle password-first order,
	// because an orphan after a partial failure keeps the *stronger*
	// factor (TOTP+authenticator) intact rather than leaving the weaker
	// single-use recovery codes alive.
	want := []struct {
		factor audit.Factor
		event  string
	}{
		{audit.FactorRecoveryCode, audit.EventRevoke},
		{audit.FactorTOTP, audit.EventRevoke},
		{audit.FactorPassword, audit.EventRevoke},
	}
	for i, exp := range want {
		got := w.records[i]
		if got.Factor != exp.factor {
			t.Errorf("records[%d].Factor = %q, want %q", i, got.Factor, exp.factor)
		}
		if got.Event != exp.event {
			t.Errorf("records[%d].Event = %q, want %q", i, got.Event, exp.event)
		}
		if got.AccountID == nil || *got.AccountID != 42 {
			t.Errorf("records[%d].AccountID = %v, want *42", i, got.AccountID)
		}
	}
}

func TestDisableNonWebAuthnFallbacks_NilAuditOK(t *testing.T) {
	f := &flowFake{usableFed: 1}

	if err := DisableNonWebAuthnFallbacks(context.Background(), f, nil, 42); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.deletePasswordCalls != 1 || f.deleteTOTPCalls != 1 || f.deleteRecoveryCalls != 1 {
		t.Errorf("delete call counts = (pw=%d,totp=%d,rec=%d), want all 1",
			f.deletePasswordCalls, f.deleteTOTPCalls, f.deleteRecoveryCalls)
	}
}

// Bundle 1 / Fix 5: delete order is recovery → TOTP → password. The early
// returns below reflect that order so partial failures stop at the safer
// state (e.g. recovery delete fails → TOTP and password untouched, so the
// strong factor stays whole rather than being half-destroyed).

func TestDisableNonWebAuthnFallbacks_RecoveryDeleteErrorStops(t *testing.T) {
	errSim := errors.New("sim")
	f := &flowFake{deleteRecoveryErr: errSim, usableFed: 1}
	w := &captureWriter{}

	err := DisableNonWebAuthnFallbacks(context.Background(), f, w, 42)
	if !errors.Is(err, errSim) {
		t.Fatalf("err = %v, want wraps errSim", err)
	}
	if f.deleteRecoveryCalls != 1 {
		t.Errorf("deleteRecoveryCalls = %d, want 1", f.deleteRecoveryCalls)
	}
	if f.deleteTOTPCalls != 0 {
		t.Errorf("deleteTOTPCalls = %d, want 0 (early return on first failure)", f.deleteTOTPCalls)
	}
	if f.deletePasswordCalls != 0 {
		t.Errorf("deletePasswordCalls = %d, want 0 (early return on first failure)", f.deletePasswordCalls)
	}
	if len(w.records) != 0 {
		t.Errorf("len(records) = %d, want 0 (no audit on partial failure)", len(w.records))
	}
}

func TestDisableNonWebAuthnFallbacks_TOTPDeleteErrorStops(t *testing.T) {
	errSim := errors.New("sim")
	f := &flowFake{deleteTOTPErr: errSim, usableFed: 1}
	w := &captureWriter{}

	err := DisableNonWebAuthnFallbacks(context.Background(), f, w, 42)
	if !errors.Is(err, errSim) {
		t.Fatalf("err = %v, want wraps errSim", err)
	}
	if f.deleteRecoveryCalls != 1 {
		t.Errorf("deleteRecoveryCalls = %d, want 1", f.deleteRecoveryCalls)
	}
	if f.deleteTOTPCalls != 1 {
		t.Errorf("deleteTOTPCalls = %d, want 1", f.deleteTOTPCalls)
	}
	if f.deletePasswordCalls != 0 {
		t.Errorf("deletePasswordCalls = %d, want 0 (early return on TOTP failure)", f.deletePasswordCalls)
	}
	if len(w.records) != 0 {
		t.Errorf("len(records) = %d, want 0 (no audit on partial failure)", len(w.records))
	}
}

func TestDisableNonWebAuthnFallbacks_PasswordDeleteErrorStops(t *testing.T) {
	errSim := errors.New("sim")
	f := &flowFake{deletePasswordErr: errSim, usableFed: 1}
	w := &captureWriter{}

	err := DisableNonWebAuthnFallbacks(context.Background(), f, w, 42)
	if !errors.Is(err, errSim) {
		t.Fatalf("err = %v, want wraps errSim", err)
	}
	if f.deleteRecoveryCalls != 1 {
		t.Errorf("deleteRecoveryCalls = %d, want 1", f.deleteRecoveryCalls)
	}
	if f.deleteTOTPCalls != 1 {
		t.Errorf("deleteTOTPCalls = %d, want 1", f.deleteTOTPCalls)
	}
	if f.deletePasswordCalls != 1 {
		t.Errorf("deletePasswordCalls = %d, want 1", f.deletePasswordCalls)
	}
	if len(w.records) != 0 {
		t.Errorf("len(records) = %d, want 0 (no audit on partial failure)", len(w.records))
	}
}

// ---- Lockout guard tests --------------------------------------------------

// TestDisableNonWebAuthnFallbacks_Guard_NoPasskeyNoFed: account has zero
// passkeys and zero usable federation — the guard must return the 409 sentinel
// before making any deletes.
func TestDisableNonWebAuthnFallbacks_Guard_NoPasskeyNoFed(t *testing.T) {
	f := &flowFake{} // no webauthnRows, usableFed defaults to 0
	w := &captureWriter{}

	err := DisableNonWebAuthnFallbacks(context.Background(), f, w, 42)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	ae, ok := err.(*AuthError)
	if !ok {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
	if ae.Code != "would_remove_last_factor" {
		t.Errorf("code = %q, want would_remove_last_factor", ae.Code)
	}
	if ae.Status != 409 {
		t.Errorf("status = %d, want 409", ae.Status)
	}
	// No deletes must have been attempted.
	if f.deleteRecoveryCalls != 0 {
		t.Errorf("deleteRecoveryCalls = %d, want 0 (guard must abort before deletes)", f.deleteRecoveryCalls)
	}
	if f.deleteTOTPCalls != 0 {
		t.Errorf("deleteTOTPCalls = %d, want 0 (guard must abort before deletes)", f.deleteTOTPCalls)
	}
	if f.deletePasswordCalls != 0 {
		t.Errorf("deletePasswordCalls = %d, want 0 (guard must abort before deletes)", f.deletePasswordCalls)
	}
}

// TestDisableNonWebAuthnFallbacks_Guard_PasskeyPresent: account has >=1 passkey
// and no federation — guard passes, deletes proceed.
func TestDisableNonWebAuthnFallbacks_Guard_PasskeyPresent(t *testing.T) {
	f := &flowFake{
		webauthnRows: []db.WebauthnCredential{{ID: 1}},
		// usableFed is 0
	}
	w := &captureWriter{}

	if err := DisableNonWebAuthnFallbacks(context.Background(), f, w, 42); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.deleteRecoveryCalls != 1 || f.deleteTOTPCalls != 1 || f.deletePasswordCalls != 1 {
		t.Errorf("delete call counts = (rec=%d,totp=%d,pw=%d), want all 1",
			f.deleteRecoveryCalls, f.deleteTOTPCalls, f.deletePasswordCalls)
	}
}

// TestDisableNonWebAuthnFallbacks_Guard_UsableFedPresent: account has no
// passkeys but >=1 usable federation identity — guard passes, deletes proceed.
func TestDisableNonWebAuthnFallbacks_Guard_UsableFedPresent(t *testing.T) {
	f := &flowFake{usableFed: 1} // no webauthnRows
	w := &captureWriter{}

	if err := DisableNonWebAuthnFallbacks(context.Background(), f, w, 42); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.deleteRecoveryCalls != 1 || f.deleteTOTPCalls != 1 || f.deletePasswordCalls != 1 {
		t.Errorf("delete call counts = (rec=%d,totp=%d,pw=%d), want all 1",
			f.deleteRecoveryCalls, f.deleteTOTPCalls, f.deletePasswordCalls)
	}
}

// ---- AvailableMethods usable-federation tests ----------------------------

// TestAvailableMethods_UsableFedZero_NoFederationMethod: usableFed=0 even
// when a webauthn credential is present (disabled upstream IdP) — federation
// must not appear in the method list.
func TestAvailableMethods_UsableFedZero_NoFederationMethod(t *testing.T) {
	f := &flowFake{
		webauthnRows: []db.WebauthnCredential{{ID: 1}},
		// usableFed = 0 (disabled upstream)
	}
	methods, err := AvailableMethods(context.Background(), f, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, m := range methods {
		if m == MethodFederationOIDC {
			t.Errorf("MethodFederationOIDC should not appear when usableFed=0 (disabled upstream)")
		}
	}
}

// TestAvailableMethods_UsableFedNonZero_FederationMethod: usableFed>0 — the
// federation method appears.
func TestAvailableMethods_UsableFedNonZero_FederationMethod(t *testing.T) {
	f := &flowFake{usableFed: 2} // two enabled IdPs
	methods, err := AvailableMethods(context.Background(), f, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, m := range methods {
		if m == MethodFederationOIDC {
			found = true
		}
	}
	if !found {
		t.Errorf("MethodFederationOIDC not found in methods=%v; expected it when usableFed>0", methods)
	}
}
