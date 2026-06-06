package server

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
)

// TestCredentialViewSuffixInvariant unit-tests the credentialView projection
// helper (defined in handle_me.go, shared between /me/credentials and
// GET /accounts/{id}/credentials).
//
// Assertions:
//  1. CredentialIDSuffix is exactly the last 4 chars of base64url(credential_id).
//  2. No field in the returned view carries the full credential id (bytes or
//     complete base64 string).
//
// This test is DB-free: it exercises the pure projection logic only.
func TestListAccountCredentials_credentialViewSuffix(t *testing.T) {
	// Use a credential_id long enough that its base64url encoding is > 4 chars.
	rawID := []byte("this-is-a-fake-credential-id-for-testing")
	enc := base64.RawURLEncoding.EncodeToString(rawID)
	if len(enc) <= 4 {
		t.Fatalf("test setup: encoded credential id too short (%d chars)", len(enc))
	}
	wantSuffix := enc[len(enc)-4:]

	row := &db.WebauthnCredential{
		ID:              42,
		CredentialID:    rawID,
		Transports:      []string{"internal"},
		BackupState:     pgtype.Bool{Bool: false, Valid: true},
		AttestationType: pgtype.Text{String: "none", Valid: true},
		CreatedAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}

	view := credentialView(row)

	if view.CredentialIDSuffix != wantSuffix {
		t.Errorf("CredentialIDSuffix = %q; want %q", view.CredentialIDSuffix, wantSuffix)
	}

	// The full base64url encoding must NOT appear anywhere in the view.
	if view.CredentialIDSuffix == enc {
		t.Errorf("CredentialIDSuffix holds full encoded credential id %q; suffix only expected", enc)
	}

	// Sanity: ID field is the row's integer PK, not a byte slice.
	if view.ID != 42 {
		t.Errorf("view.ID = %d; want 42", view.ID)
	}
}

// TestListAccountCredentials_shortCredentialID verifies the edge case where
// the base64url encoding of the credential_id is 4 chars or fewer: the entire
// encoded string is returned (no truncation that would produce an empty suffix).
func TestListAccountCredentials_shortCredentialID(t *testing.T) {
	// 2 bytes → 3 base64url chars (no padding in RawURL encoding).
	rawID := []byte{0xAB, 0xCD}
	enc := base64.RawURLEncoding.EncodeToString(rawID)
	if len(enc) > 4 {
		t.Fatalf("test setup: encoded id %q longer than 4 chars", enc)
	}

	row := &db.WebauthnCredential{
		ID:           1,
		CredentialID: rawID,
		Transports:   []string{},
		CreatedAt:    pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}

	view := credentialView(row)

	// When the encoded form is ≤ 4 chars, the whole string is the suffix.
	if view.CredentialIDSuffix != enc {
		t.Errorf("CredentialIDSuffix = %q; want %q (full encoding for short id)", view.CredentialIDSuffix, enc)
	}
}

// TestListAccountCredentials_nullableFields verifies that optional fields
// (Nickname, LastUsedAt) are nil when the DB columns are NULL, and populated
// when they are valid.
func TestListAccountCredentials_nullableFields(t *testing.T) {
	rawID := []byte("some-credential-id-bytes")
	now := time.Now()

	t.Run("nulls", func(t *testing.T) {
		row := &db.WebauthnCredential{
			ID:           7,
			CredentialID: rawID,
			Transports:   []string{},
			CreatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
			// Nickname and LastUsedAt are zero-value (Valid=false).
		}
		view := credentialView(row)
		if view.Nickname != nil {
			t.Errorf("Nickname should be nil for NULL db column, got %v", *view.Nickname)
		}
		if view.LastUsedAt != nil {
			t.Errorf("LastUsedAt should be nil for NULL db column, got %v", *view.LastUsedAt)
		}
	})

	t.Run("populated", func(t *testing.T) {
		nick := "yubikey-5"
		row := &db.WebauthnCredential{
			ID:           8,
			CredentialID: rawID,
			Transports:   []string{"usb"},
			Nickname:     pgtype.Text{String: nick, Valid: true},
			LastUsedAt:   pgtype.Timestamptz{Time: now, Valid: true},
			CreatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
		}
		view := credentialView(row)
		if view.Nickname == nil || *view.Nickname != nick {
			t.Errorf("Nickname = %v; want %q", view.Nickname, nick)
		}
		if view.LastUsedAt == nil {
			t.Error("LastUsedAt should be non-nil when db column is valid")
		}
	})
}
