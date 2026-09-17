package server

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

// TestEnrollCeremonyKeyHashesToken pins the WACER-3 fix: the enrollment WebAuthn
// ceremony KV key is derived from a SHA-256 of the bearer token, so the raw
// enrollment secret never materializes in the KV keyspace (matching the
// add-passkey/sudo ceremony hardening).
func TestEnrollCeremonyKeyHashesToken(t *testing.T) {
	token := "super-secret-enrollment-token"

	key := enrollCeremonyKey(token)
	if strings.Contains(key, token) {
		t.Fatalf("ceremony key %q contains the raw token", key)
	}
	want := "webauthn_ceremony:enroll:" + fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	if key != want {
		t.Fatalf("key = %q, want %q", key, want)
	}
	if enrollCeremonyKey(token) != key {
		t.Fatalf("enrollCeremonyKey is not deterministic")
	}
}

type newAccountPrepQueries struct {
	db.Querier
	existing *db.Account
}

func (q newAccountPrepQueries) GetAccountByUsername(context.Context, string) (db.Account, error) {
	if q.existing != nil {
		return *q.existing, nil
	}
	return db.Account{}, pgx.ErrNoRows
}

func TestPrepareNewEnrollmentAccountPreservesSharedNewAccountPolicy(t *testing.T) {
	body := enrollBeginBody{Username: "new-user", DisplayName: "New User", Nickname: "first key"}
	user, proposal, err := prepareNewEnrollmentAccount(context.Background(), newAccountPrepQueries{}, body, "admin", "test")
	if err != nil {
		t.Fatal(err)
	}
	if user.Account.Username != body.Username || user.Account.DisplayName != body.DisplayName || user.Account.Role != "admin" {
		t.Fatalf("webauthn account = %+v", user.Account)
	}
	if proposal.Username != body.Username || proposal.DisplayName != body.DisplayName || proposal.Nickname != body.Nickname ||
		len(proposal.WebauthnUserHandle) == 0 {
		t.Fatalf("ceremony proposal = %+v", proposal)
	}
	existing := db.Account{ID: 9, Username: body.Username}
	if _, _, err := prepareNewEnrollmentAccount(context.Background(), newAccountPrepQueries{existing: &existing}, body, "user", "test"); err == nil {
		t.Fatal("shared new-account preparation accepted a duplicate username")
	}
}

func TestApplyFixedInvitationUsername(t *testing.T) {
	enrollmentRow := &db.Enrollment{TemplateUsername: pgtype.Text{String: "alice", Valid: true}}

	omitted := enrollBeginBody{}
	if err := applyFixedInvitationUsername(&omitted, enrollmentRow); err != nil {
		t.Fatalf("omitted username: %v", err)
	}
	if omitted.Username != "alice" {
		t.Fatalf("omitted username became %q, want alice", omitted.Username)
	}

	matching := enrollBeginBody{Username: "alice"}
	if err := applyFixedInvitationUsername(&matching, enrollmentRow); err != nil {
		t.Fatalf("matching username: %v", err)
	}

	mismatch := enrollBeginBody{Username: "bob"}
	err := applyFixedInvitationUsername(&mismatch, enrollmentRow)
	if authErr := authn.AsAuthError(err); authErr == nil || authErr.Code != "username_immutable" {
		t.Fatalf("mismatched username error = %v, want username_immutable", err)
	}
}
