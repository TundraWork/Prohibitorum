package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/credential/enrollment"
	"prohibitorum/pkg/db"
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
		{enrollment.IntentReset, []string{enrollMethodPasskey, enrollMethodPasswordTOTP}},
	}
	for _, tc := range cases {
		got := enrollmentAllowedMethods(tc.intent)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("intent %q → %v, want %v", tc.intent, got, tc.want)
		}
	}
}

func passwordTOTPVerifyRequest(token, username, password, secret, code string) *http.Request {
	body := fmt.Sprintf(`{"username":%q,"password":%q,"secret_base32":%q,"code":%q}`, username, password, secret, code)
	req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/enrollments/"+token+"/password-totp/verify", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// TestPasswordTOTPInviteWithoutSnapshotRequiresFederation: a federation-bound
// invite cannot finish with password+TOTP until the upstream identity is on
// the enrollment. The gate is the same one /begin applies to passkeys; without
// it the invite would create an account the operator never verified.
func TestPasswordTOTPInviteWithoutSnapshotRequiresFederation(t *testing.T) {
	e := verifiedInviteEnrollment("pwd-totp-federation-required")
	e.FederatedUpstreamIdpID = pgtype.Int8{}   // snapshot absent
	e.FederatedUpstreamIdpSlug = pgtype.Text{} // ...
	e.FederatedUpstreamIss = pgtype.Text{}     // ...
	e.FederatedUpstreamSub = pgtype.Text{}     // ...
	e.FederatedDisplayName = pgtype.Text{}     // ...
	e.FederatedUpstreamData = nil              // ...
	e.FederatedAvatarUrl = pgtype.Text{}       // ...
	s, q := newAtomicVRChatEnrollmentServer(t, e, nil)
	secret, code := passwordTOTPCandidate(t, s)

	w := httptest.NewRecorder()
	s.handleEnrollmentPasswordTOTPVerifyHTTP(w, passwordTOTPVerifyRequest(e.Token, "snapshot-less", "trustno1pass!", secret, code))
	var public struct {
		Code    string         `json:"code"`
		Details map[string]any `json:"details"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &public); err != nil {
		t.Fatalf("decode response status=%d body=%s: %v", w.Code, w.Body.String(), err)
	}
	if w.Code != http.StatusBadRequest || public.Code != "enrollment_federation_required" {
		t.Fatalf("status=%d code=%q, want enrollment_federation_required", w.Code, public.Code)
	}
	if public.Details["federationName"] != "VRChat" {
		t.Fatalf("details = %#v, want the provider name", public.Details)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.enrollments[e.Token].ConsumedAt.Valid {
		t.Fatal("rejected verify consumed the enrollment")
	}
	if len(q.accounts) != 0 {
		t.Fatal("rejected verify created an account")
	}
}

// TestPasswordTOTPVerifiedInviteCreatesAccountWithIdentity: the happy path.
// With the snapshot present the verify writes account + password + TOTP +
// recovery codes + the federated identity and consumes the invite in one
// transaction.
func TestPasswordTOTPVerifiedInviteCreatesAccountWithIdentity(t *testing.T) {
	e := verifiedInviteEnrollment("pwd-totp-happy")
	s, q := newAtomicVRChatEnrollmentServer(t, e, nil)
	secret, code := passwordTOTPCandidate(t, s)

	w := httptest.NewRecorder()
	s.handleEnrollmentPasswordTOTPVerifyHTTP(w, passwordTOTPVerifyRequest(e.Token, "pwd-totp-user", "trustno1pass!", secret, code))
	if w.Code != http.StatusOK {
		t.Fatalf("verify status = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		RecoveryCodes []string `json:"recoveryCodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.enrollments[e.Token].ConsumedAt.Valid {
		t.Fatal("successful verify did not consume the enrollment")
	}
	if len(q.accounts) != 1 || len(q.identities) != 1 {
		t.Fatalf("accounts=%d identities=%d, want 1/1", len(q.accounts), len(q.identities))
	}
	var accountID int32
	for id := range q.accounts {
		accountID = id
	}
	if _, ok := q.passwords[accountID]; !ok {
		t.Fatal("password credential missing")
	}
	if _, ok := q.totp[accountID]; !ok {
		t.Fatal("totp credential missing")
	}
	if len(q.recovery[accountID]) != len(resp.RecoveryCodes) || len(resp.RecoveryCodes) == 0 {
		t.Fatalf("stored recovery=%d response=%d", len(q.recovery[accountID]), len(resp.RecoveryCodes))
	}
	identity := q.identities[0]
	if identity.AccountID != accountID || identity.UpstreamIdpID != 41 || !identity.ConfirmedAt.Valid {
		t.Fatalf("identity = %+v", identity)
	}
}

// TestPasswordTOTPRollbackKeepsInviteUnconsumed: when the identity write
// fails, the whole transaction unwinds — no account, no credentials, and the
// invitation stays usable rather than half-consumed.
func TestPasswordTOTPRollbackKeepsInviteUnconsumed(t *testing.T) {
	e := verifiedInviteEnrollment("pwd-totp-rollback")
	s, q := newAtomicVRChatEnrollmentServer(t, e, nil)
	q.mu.Lock()
	q.insertIdentityErr = fmt.Errorf("injected identity write failure")
	q.mu.Unlock()
	secret, code := passwordTOTPCandidate(t, s)

	w := httptest.NewRecorder()
	s.handleEnrollmentPasswordTOTPVerifyHTTP(w, passwordTOTPVerifyRequest(e.Token, "rollback-user", "trustno1pass!", secret, code))
	if w.Code == http.StatusOK {
		t.Fatalf("injected failure still returned %d", w.Code)
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if q.enrollments[e.Token].ConsumedAt.Valid {
		t.Fatal("failed verify consumed the enrollment")
	}
	if len(q.accounts) != 0 || len(q.passwords) != 0 || len(q.totp) != 0 || len(q.recovery) != 0 {
		t.Fatalf("rollback left accounts=%d passwords=%d totp=%d recovery=%d",
			len(q.accounts), len(q.passwords), len(q.totp), len(q.recovery))
	}
}

var _ = db.Enrollment{} // keep db import when helpers change shape
