package server

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"prohibitorum/pkg/credential/enrollment"
)

// TestEnrollmentAllowedMethods pins the method policy: only the first-admin
// bootstrap is passkey-only; every other intent offers passkey OR
// password+TOTP. The policy keys purely off intent (no role lookup).
func TestEnrollmentAllowedMethods(t *testing.T) {
	cases := []struct {
		intent string
		want   []string
	}{
		{enrollment.IntentBootstrap, []string{enrollMethodPasskey}},
		{enrollment.IntentInvite, []string{enrollMethodPasskey, enrollMethodPasswordTOTP}},
		{enrollment.IntentFederatedRegister, []string{enrollMethodPasskey, enrollMethodPasswordTOTP}},
		{enrollment.IntentReset, []string{enrollMethodPasskey, enrollMethodPasswordTOTP}},
	}
	for _, tc := range cases {
		got := enrollmentAllowedMethods(tc.intent)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("intent %q → %v, want %v", tc.intent, got, tc.want)
		}
	}
}

// TestEnrollPwdTOTPCeremonyKey mirrors the passkey ceremony's WACER-3 hardening:
// the KV key hashes the token (bearer secret never in the keyspace) and uses a
// prefix distinct from the passkey ceremony so both can coexist for one token.
func TestEnrollPwdTOTPCeremonyKey(t *testing.T) {
	token := "super-secret-enrollment-token"

	key := enrollPwdTOTPCeremonyKey(token)
	if strings.Contains(key, token) {
		t.Fatalf("ceremony key %q contains the raw token", key)
	}
	want := "enroll_pwdtotp:" + fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	if key != want {
		t.Fatalf("key = %q, want %q", key, want)
	}
	if enrollPwdTOTPCeremonyKey(token) != key {
		t.Fatal("enrollPwdTOTPCeremonyKey is not deterministic")
	}
	if key == enrollCeremonyKey(token) {
		t.Fatal("password-totp ceremony key collides with the passkey ceremony key")
	}
}
