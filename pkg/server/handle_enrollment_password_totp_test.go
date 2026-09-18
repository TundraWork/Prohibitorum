package server

import (
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
