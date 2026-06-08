// Package server — handle_me_test.go
//
// Unit tests for PUT /me (handleUpdateMe) and GET /me/factors (handleGetMyFactors).
// Handlers are huma-style (receive context.Context + typed input); we call them
// directly, injecting an authn.Session via authn.WithSession and stubbing DB
// reads/writes through the override seams.

package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/errorx"
)

// fakeUpdateMeQ is a minimal stub satisfying the updateMeQueries interface.
type fakeUpdateMeQ struct {
	calls []db.UpdateAccountDisplayNameParams
	err   error
}

func (f *fakeUpdateMeQ) UpdateAccountDisplayName(_ context.Context, arg db.UpdateAccountDisplayNameParams) error {
	f.calls = append(f.calls, arg)
	return f.err
}

// newUpdateMeServer builds the smallest Server that can run handleUpdateMe.
func newUpdateMeServer(q *fakeUpdateMeQ) *Server {
	return &Server{
		updateMeOverride: q,
	}
}

// updateMeCtx returns a context with a minimal session attached.
func updateMeCtx(accountID int32, displayName string) context.Context {
	acct := &db.Account{ID: accountID, Username: "alice", DisplayName: displayName}
	sess := &authn.Session{Account: acct}
	return authn.WithSession(context.Background(), sess)
}

// codeFromErr extracts the errorx code from an error returned by authErrToHuma.
func codeFromErr(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if e, ok := err.(*errorx.Error); ok {
		return e.Code
	}
	t.Fatalf("expected *errorx.Error, got %T: %v", err, err)
	return ""
}

// TestHandleUpdateMe_ValidatesAndUpdatesDisplayNameOnly — happy path: valid
// displayName is stored and the returned SessionView reflects the new name.
func TestHandleUpdateMe_ValidatesAndUpdatesDisplayNameOnly(t *testing.T) {
	q := &fakeUpdateMeQ{}
	s := newUpdateMeServer(q)
	ctx := updateMeCtx(42, "Old Name")

	in := &updateMeIn{}
	in.Body.DisplayName = "New Name"

	out, err := s.handleUpdateMe(ctx, in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Body.DisplayName != "New Name" {
		t.Errorf("DisplayName: want %q, got %q", "New Name", out.Body.DisplayName)
	}
	if len(q.calls) != 1 {
		t.Fatalf("UpdateAccountDisplayName call count: want 1, got %d", len(q.calls))
	}
	if q.calls[0].ID != 42 {
		t.Errorf("ID param: want 42, got %d", q.calls[0].ID)
	}
	if q.calls[0].DisplayName != "New Name" {
		t.Errorf("DisplayName param: want %q, got %q", "New Name", q.calls[0].DisplayName)
	}
}

// TestHandleUpdateMe_InvalidDisplayName_Empty — empty string must fail with
// invalid_display_name and must NOT call UpdateAccountDisplayName.
func TestHandleUpdateMe_InvalidDisplayName_Empty(t *testing.T) {
	q := &fakeUpdateMeQ{}
	s := newUpdateMeServer(q)
	ctx := updateMeCtx(42, "Old Name")

	in := &updateMeIn{}
	in.Body.DisplayName = ""

	_, err := s.handleUpdateMe(ctx, in)
	if code := codeFromErr(t, err); code != "invalid_display_name" {
		t.Errorf("code: want invalid_display_name, got %s", code)
	}
	if len(q.calls) != 0 {
		t.Errorf("UpdateAccountDisplayName must not be called on invalid input; got %d call(s)", len(q.calls))
	}
}

// TestHandleUpdateMe_InvalidDisplayName_TooLong — >128 bytes must fail with
// invalid_display_name and must NOT call UpdateAccountDisplayName.
func TestHandleUpdateMe_InvalidDisplayName_TooLong(t *testing.T) {
	q := &fakeUpdateMeQ{}
	s := newUpdateMeServer(q)
	ctx := updateMeCtx(42, "Old Name")

	in := &updateMeIn{}
	in.Body.DisplayName = strings.Repeat("x", 129)

	_, err := s.handleUpdateMe(ctx, in)
	if code := codeFromErr(t, err); code != "invalid_display_name" {
		t.Errorf("code: want invalid_display_name, got %s", code)
	}
	if len(q.calls) != 0 {
		t.Errorf("UpdateAccountDisplayName must not be called on invalid input; got %d call(s)", len(q.calls))
	}
}

// TestHandleUpdateMe_InvalidDisplayName_ControlChar — newline (control char)
// must fail with invalid_display_name.
func TestHandleUpdateMe_InvalidDisplayName_ControlChar(t *testing.T) {
	q := &fakeUpdateMeQ{}
	s := newUpdateMeServer(q)
	ctx := updateMeCtx(42, "Old Name")

	in := &updateMeIn{}
	in.Body.DisplayName = "hello\nworld"

	_, err := s.handleUpdateMe(ctx, in)
	if code := codeFromErr(t, err); code != "invalid_display_name" {
		t.Errorf("code: want invalid_display_name, got %s", code)
	}
	if len(q.calls) != 0 {
		t.Errorf("UpdateAccountDisplayName must not be called on invalid input; got %d call(s)", len(q.calls))
	}
}

// ---------------------------------------------------------------------------
// GET /me/factors tests
// ---------------------------------------------------------------------------

// fakeGetMyFactorsQ is a minimal stub satisfying the getMyFactorsQueries interface.
// All four results are configurable; zero values yield "nothing enrolled".
type fakeGetMyFactorsQ struct {
	// password
	pwErr error // pgx.ErrNoRows => not set; nil => set; other => internal error

	// TOTP
	totpRow db.TotpCredential
	totpErr error // pgx.ErrNoRows => not set; nil => row returned; other => error

	// recovery codes (unused slice)
	codes []db.RecoveryCode
	rcErr error

	// passkey count
	credCount int64
	credErr   error
}

func (f *fakeGetMyFactorsQ) GetPasswordCredential(_ context.Context, _ int32) (db.PasswordCredential, error) {
	return db.PasswordCredential{}, f.pwErr
}

func (f *fakeGetMyFactorsQ) GetTOTPCredential(_ context.Context, _ int32) (db.TotpCredential, error) {
	return f.totpRow, f.totpErr
}

func (f *fakeGetMyFactorsQ) ListRecoveryCodesByAccount(_ context.Context, _ int32) ([]db.RecoveryCode, error) {
	return f.codes, f.rcErr
}

func (f *fakeGetMyFactorsQ) CountCredentialsByAccount(_ context.Context, _ int32) (int64, error) {
	return f.credCount, f.credErr
}

// newGetMyFactorsServer builds the smallest Server that can run handleGetMyFactors.
func newGetMyFactorsServer(q *fakeGetMyFactorsQ) *Server {
	return &Server{
		getMyFactorsOverride: q,
	}
}

// getMyFactorsCtx returns a context with a minimal session for accountID.
func getMyFactorsCtx(accountID int32) context.Context {
	acct := &db.Account{ID: accountID, Username: "bob", DisplayName: "Bob"}
	sess := &authn.Session{Account: acct}
	return authn.WithSession(context.Background(), sess)
}

// confirmedTOTPRow builds a TotpCredential whose ConfirmedAt is set (valid).
func confirmedTOTPRow() db.TotpCredential {
	return db.TotpCredential{
		ConfirmedAt: pgtype.Timestamptz{Valid: true},
	}
}

// unconfirmedTOTPRow builds a TotpCredential whose ConfirmedAt is NULL (not yet confirmed).
func unconfirmedTOTPRow() db.TotpCredential {
	return db.TotpCredential{
		ConfirmedAt: pgtype.Timestamptz{Valid: false},
	}
}

// makeRecoveryCodes returns n stub RecoveryCode rows (content is irrelevant for counting).
func makeRecoveryCodes(n int) []db.RecoveryCode {
	out := make([]db.RecoveryCode, n)
	for i := range out {
		out[i] = db.RecoveryCode{ID: int32(i + 1)}
	}
	return out
}

// TestHandleGetMyFactors_FullyEnrolled — password present, TOTP confirmed,
// 3 unused recovery codes, 2 passkeys → {true,true,3,2}.
func TestHandleGetMyFactors_FullyEnrolled(t *testing.T) {
	q := &fakeGetMyFactorsQ{
		pwErr:     nil, // password present
		totpRow:   confirmedTOTPRow(),
		totpErr:   nil,
		codes:     makeRecoveryCodes(3),
		credCount: 2,
	}
	s := newGetMyFactorsServer(q)
	ctx := getMyFactorsCtx(7)

	out, err := s.handleGetMyFactors(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v := out.Body
	if !v.PasswordSet {
		t.Error("PasswordSet: want true")
	}
	if !v.TOTPEnrolled {
		t.Error("TOTPEnrolled: want true")
	}
	if v.RecoveryCodesRemaining != 3 {
		t.Errorf("RecoveryCodesRemaining: want 3, got %d", v.RecoveryCodesRemaining)
	}
	if v.PasskeyCount != 2 {
		t.Errorf("PasskeyCount: want 2, got %d", v.PasskeyCount)
	}
}

// TestHandleGetMyFactors_NothingEnrolled — ErrNoRows for password+TOTP,
// 0 codes, 0 creds → {false,false,0,0}.
func TestHandleGetMyFactors_NothingEnrolled(t *testing.T) {
	q := &fakeGetMyFactorsQ{
		pwErr:     pgx.ErrNoRows,
		totpErr:   pgx.ErrNoRows,
		codes:     nil,
		credCount: 0,
	}
	s := newGetMyFactorsServer(q)
	ctx := getMyFactorsCtx(8)

	out, err := s.handleGetMyFactors(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v := out.Body
	if v.PasswordSet {
		t.Error("PasswordSet: want false")
	}
	if v.TOTPEnrolled {
		t.Error("TOTPEnrolled: want false")
	}
	if v.RecoveryCodesRemaining != 0 {
		t.Errorf("RecoveryCodesRemaining: want 0, got %d", v.RecoveryCodesRemaining)
	}
	if v.PasskeyCount != 0 {
		t.Errorf("PasskeyCount: want 0, got %d", v.PasskeyCount)
	}
}

// TestHandleGetMyFactors_TOTPRowExistsButNotConfirmed — TOTP row exists but
// ConfirmedAt is NULL → totpEnrolled false.
func TestHandleGetMyFactors_TOTPRowExistsButNotConfirmed(t *testing.T) {
	q := &fakeGetMyFactorsQ{
		pwErr:     nil, // password present
		totpRow:   unconfirmedTOTPRow(),
		totpErr:   nil, // row returned, but ConfirmedAt is not valid
		codes:     makeRecoveryCodes(1),
		credCount: 1,
	}
	s := newGetMyFactorsServer(q)
	ctx := getMyFactorsCtx(9)

	out, err := s.handleGetMyFactors(ctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v := out.Body
	if !v.PasswordSet {
		t.Error("PasswordSet: want true")
	}
	if v.TOTPEnrolled {
		t.Error("TOTPEnrolled: want false (ConfirmedAt not set)")
	}
	if v.RecoveryCodesRemaining != 1 {
		t.Errorf("RecoveryCodesRemaining: want 1, got %d", v.RecoveryCodesRemaining)
	}
	if v.PasskeyCount != 1 {
		t.Errorf("PasskeyCount: want 1, got %d", v.PasskeyCount)
	}
}

// TestHandleGetMyFactors_RecoveryDBError — a non-ErrNoRows error from
// ListRecoveryCodesByAccount must be propagated as a non-nil handler error.
func TestHandleGetMyFactors_RecoveryDBError(t *testing.T) {
	q := &fakeGetMyFactorsQ{
		pwErr:   pgx.ErrNoRows,
		totpErr: pgx.ErrNoRows,
		rcErr:   errors.New("injected"),
	}
	s := newGetMyFactorsServer(q)
	ctx := getMyFactorsCtx(10)

	_, err := s.handleGetMyFactors(ctx, nil)
	if err == nil {
		t.Fatal("expected non-nil error from recovery-codes DB failure, got nil")
	}
}

// TestHandleGetMyFactors_PasskeyCountDBError — a non-ErrNoRows error from
// CountCredentialsByAccount must be propagated as a non-nil handler error.
func TestHandleGetMyFactors_PasskeyCountDBError(t *testing.T) {
	q := &fakeGetMyFactorsQ{
		pwErr:   pgx.ErrNoRows,
		totpErr: pgx.ErrNoRows,
		credErr: errors.New("injected"),
	}
	s := newGetMyFactorsServer(q)
	ctx := getMyFactorsCtx(11)

	_, err := s.handleGetMyFactors(ctx, nil)
	if err == nil {
		t.Fatal("expected non-nil error from passkey-count DB failure, got nil")
	}
}
