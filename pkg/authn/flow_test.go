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

	webauthnRows  []db.WebauthnCredential
	passwordRow   *db.PasswordCredential
	totpRow       *db.TotpCredential

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
	f := &flowFake{}
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
	f := &flowFake{}
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
	f := &flowFake{}
	w := &captureWriter{}

	if err := DisableNonWebAuthnFallbacks(context.Background(), f, w, 42); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(w.records) != 3 {
		t.Fatalf("len(records) = %d, want 3", len(w.records))
	}

	want := []struct {
		factor audit.Factor
		event  string
	}{
		{audit.FactorPassword, audit.EventRevoke},
		{audit.FactorTOTP, audit.EventRevoke},
		{audit.FactorRecoveryCode, audit.EventRevoke},
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
	f := &flowFake{}

	if err := DisableNonWebAuthnFallbacks(context.Background(), f, nil, 42); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.deletePasswordCalls != 1 || f.deleteTOTPCalls != 1 || f.deleteRecoveryCalls != 1 {
		t.Errorf("delete call counts = (pw=%d,totp=%d,rec=%d), want all 1",
			f.deletePasswordCalls, f.deleteTOTPCalls, f.deleteRecoveryCalls)
	}
}
