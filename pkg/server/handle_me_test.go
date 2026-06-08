// Package server — handle_me_test.go
//
// Unit tests for PUT /me (handleUpdateMe). The handler is huma-style
// (receives context.Context + typed input); we call it directly, injecting
// an authn.Session via authn.WithSession and stubbing DB writes through the
// updateMeOverride seam.

package server

import (
	"context"
	"strings"
	"testing"

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
