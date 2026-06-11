package server

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
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
