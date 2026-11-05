package oidc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"prohibitorum/pkg/audit"
)

// maintenanceSwitch is a maintenance checker the test can turn off midway.
type maintenanceSwitch struct{ on bool }

func (m *maintenanceSwitch) check(context.Context) bool { return m.on }

func setTokenHarnessRole(h *tokenHarness, role string) {
	q := h.p.queries.(*fakeTokenQueries)
	acct := q.accounts[42]
	acct.Role = role
	q.accounts[42] = acct
}

func setEndpointHarnessRole(h *endpointHarness, role string) {
	acct := h.q.byID[7]
	acct.Role = role
	h.q.byID[7] = acct
	h.q.bySubject[testSubject] = acct
}

// assertMaintenanceRefusal checks the 503 temporarily_unavailable answer and
// the matching audit record for account acctID.
func assertMaintenanceRefusal(t *testing.T, rec *httptest.ResponseRecorder, records []audit.Record, acctID int32) {
	t.Helper()
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d (%s)", rec.Code, rec.Body.String())
	}
	if got := decodeError(t, rec); got != errCodeTemporarilyUnavailable {
		t.Fatalf("error = %q, want %q", got, errCodeTemporarilyUnavailable)
	}
	for _, r := range records {
		if r.Detail["reason"] == "maintenance" {
			if r.Event != audit.EventFail || r.AccountID == nil || *r.AccountID != acctID || r.Detail["client_id"] != testClientID {
				t.Fatalf("maintenance audit record = %+v", r)
			}
			return
		}
	}
	t.Fatalf("no maintenance audit record in %+v", records)
}

func TestTokenCodeExchangeMaintenance(t *testing.T) {
	for _, tc := range []struct {
		name, role string
		on         bool
		want       int
	}{
		{"non-admin refused during maintenance", "user", true, http.StatusServiceUnavailable},
		{"admin exempt during maintenance", "admin", true, http.StatusOK},
		{"non-admin allowed when maintenance off", "user", false, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTokenHarness(t)
			setTokenHarnessRole(h, tc.role)
			h.p.SetMaintenanceChecker((&maintenanceSwitch{on: tc.on}).check)

			code := h.mintTestCode(t, baseAuthCode())
			rec := httptest.NewRecorder()
			h.p.HandleToken(rec, tokenReq(codeExchangeForm(code, testVerifier, testRedirect)))

			if tc.want == http.StatusServiceUnavailable {
				assertMaintenanceRefusal(t, rec, h.audit.records, 42)
				return
			}
			if rec.Code != tc.want {
				t.Fatalf("want %d, got %d (%s)", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestTokenRefreshMaintenance(t *testing.T) {
	for _, tc := range []struct {
		name, role string
		on         bool
		want       int
	}{
		{"non-admin refused during maintenance", "user", true, http.StatusServiceUnavailable},
		{"admin exempt during maintenance", "admin", true, http.StatusOK},
		{"non-admin allowed when maintenance off", "user", false, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTokenHarness(t)
			setTokenHarnessRole(h, tc.role)
			h.p.SetMaintenanceChecker((&maintenanceSwitch{on: tc.on}).check)

			presented, _, err := issueRefresh(context.Background(), h.p.kv, grantFamily(), RefreshTokenTTL, RefreshTokenTTL)
			if err != nil {
				t.Fatalf("issueRefresh: %v", err)
			}
			rec := httptest.NewRecorder()
			h.p.HandleToken(rec, tokenReq(refreshForm(presented)))

			if tc.want == http.StatusServiceUnavailable {
				assertMaintenanceRefusal(t, rec, h.audit.records, 42)
				return
			}
			if rec.Code != tc.want {
				t.Fatalf("want %d, got %d (%s)", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestTokenRefreshMaintenanceKeepsFamily: a refused refresh leaves the family
// untouched, so the token the client still holds rotates normally once
// maintenance ends, however long it lasted (no idempotency window to miss).
func TestTokenRefreshMaintenanceKeepsFamily(t *testing.T) {
	h := newTokenHarness(t)
	ctx := context.Background()
	maint := &maintenanceSwitch{on: true}
	h.p.SetMaintenanceChecker(maint.check)

	presented, _, err := issueRefresh(ctx, h.p.kv, grantFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}
	before, err := loadFamily(ctx, h.p.kv, presented)
	if err != nil {
		t.Fatalf("loadFamily: %v", err)
	}
	for range 2 {
		rec := httptest.NewRecorder()
		h.p.HandleToken(rec, tokenReq(refreshForm(presented)))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("want 503, got %d (%s)", rec.Code, rec.Body.String())
		}
	}
	after, err := loadFamily(ctx, h.p.kv, presented)
	if err != nil {
		t.Fatalf("family gone after a maintenance refusal: %v", err)
	}
	if after.Revision != before.Revision || after.CurrentHash != before.CurrentHash {
		t.Fatalf("family rotated during maintenance: revision %d → %d", before.Revision, after.Revision)
	}

	maint.on = false
	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(refreshForm(presented)))
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh after maintenance: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var resp tokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RefreshToken == "" || resp.RefreshToken == presented {
		t.Fatalf("expected a rotated refresh token, got %q", resp.RefreshToken)
	}
}

// TestTokenRefreshMaintenanceStillDetectsReuse: a superseded token presented
// during maintenance is not answered 503; it goes on to reuse detection, which
// revokes the family as it would at any other time.
func TestTokenRefreshMaintenanceStillDetectsReuse(t *testing.T) {
	h := newTokenHarness(t)
	ctx := context.Background()
	maint := &maintenanceSwitch{}
	h.p.SetMaintenanceChecker(maint.check)

	old, _, err := issueRefresh(ctx, h.p.kv, grantFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}
	fid := mustFID(t, old)
	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(refreshForm(old)))
	if rec.Code != http.StatusOK {
		t.Fatalf("first refresh: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	// Push the superseded token out of its idempotency window.
	fam, err := loadFamilyByFID(ctx, h.p.kv, fid)
	if err != nil {
		t.Fatalf("loadFamilyByFID: %v", err)
	}
	fam.PreviousValidUntil = time.Now().Add(-time.Second)
	if err := putFamily(ctx, h.p.kv, fam, RefreshTokenTTL); err != nil {
		t.Fatalf("putFamily: %v", err)
	}

	maint.on = true
	rec = httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(refreshForm(old)))
	if rec.Code != http.StatusBadRequest || decodeError(t, rec) != errCodeInvalidGrant {
		t.Fatalf("superseded token during maintenance: want 400 invalid_grant, got %d (%s)", rec.Code, rec.Body.String())
	}
	if _, err := loadFamilyByFID(ctx, h.p.kv, fid); err == nil {
		t.Fatal("reuse during maintenance left the family alive")
	}
}

// TestTokenRefreshMaintenanceDisabledAccountRevokes: a disabled account's
// family is revoked during maintenance as at any other time, not held for
// after it.
func TestTokenRefreshMaintenanceDisabledAccountRevokes(t *testing.T) {
	h := newTokenHarness(t)
	ctx := context.Background()
	h.p.SetMaintenanceChecker((&maintenanceSwitch{on: true}).check)
	q := h.p.queries.(*fakeTokenQueries)
	acct := q.accounts[42]
	acct.Disabled = true
	q.accounts[42] = acct

	presented, fid, err := issueRefresh(ctx, h.p.kv, grantFamily(), RefreshTokenTTL, RefreshTokenTTL)
	if err != nil {
		t.Fatalf("issueRefresh: %v", err)
	}
	rec := httptest.NewRecorder()
	h.p.HandleToken(rec, tokenReq(refreshForm(presented)))
	if rec.Code != http.StatusBadRequest || decodeError(t, rec) != errCodeInvalidGrant {
		t.Fatalf("want 400 invalid_grant, got %d (%s)", rec.Code, rec.Body.String())
	}
	if _, err := loadFamilyByFID(ctx, h.p.kv, fid); err == nil {
		t.Fatal("disabled account's family survived")
	}
}

func TestUserinfoMaintenance(t *testing.T) {
	for _, tc := range []struct {
		name, role string
		on         bool
		want       int
	}{
		{"non-admin refused during maintenance", "user", true, http.StatusServiceUnavailable},
		{"admin exempt during maintenance", "admin", true, http.StatusOK},
		{"non-admin allowed when maintenance off", "user", false, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newEndpointHarness(t)
			setEndpointHarnessRole(h, tc.role)
			h.p.SetMaintenanceChecker((&maintenanceSwitch{on: tc.on}).check)

			at := h.mintAccessToken(t, testSubject, testClientID, "openid profile", "jti-maint", time.Now().Add(time.Hour))
			rec := httptest.NewRecorder()
			h.p.HandleUserinfo(rec, userinfoReq(at))

			if tc.want == http.StatusServiceUnavailable {
				assertMaintenanceRefusal(t, rec, h.audit.records, 7)
				if rec.Header().Get("WWW-Authenticate") != "" {
					t.Fatalf("maintenance refusal must not challenge the bearer token: %q", rec.Header().Get("WWW-Authenticate"))
				}
				return
			}
			if rec.Code != tc.want {
				t.Fatalf("want %d, got %d (%s)", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestIntrospectMaintenance(t *testing.T) {
	for _, kind := range []string{"access_token", "refresh_token"} {
		for _, tc := range []struct {
			name, role string
			on         bool
			want       int
		}{
			{"non-admin refused during maintenance", "user", true, http.StatusServiceUnavailable},
			{"admin exempt during maintenance", "admin", true, http.StatusOK},
			{"non-admin allowed when maintenance off", "user", false, http.StatusOK},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				h := newEndpointHarness(t)
				setEndpointHarnessRole(h, tc.role)
				h.p.SetMaintenanceChecker((&maintenanceSwitch{on: tc.on}).check)

				var token string
				if kind == "access_token" {
					token = h.mintAccessToken(t, testSubject, testClientID, "openid profile", "jti-maint", time.Now().Add(time.Hour))
				} else {
					rt, _, err := issueRefresh(context.Background(), h.p.kv, refreshFamily{
						ClientID:  testClientID,
						AccountID: 7,
						Scope:     []string{"openid", "offline_access"},
					}, RefreshTokenTTL, RefreshTokenTTL)
					if err != nil {
						t.Fatalf("issueRefresh: %v", err)
					}
					token = rt
				}
				rec := httptest.NewRecorder()
				h.p.HandleIntrospect(rec, introspectReq(token))

				if tc.want == http.StatusServiceUnavailable {
					assertMaintenanceRefusal(t, rec, h.audit.records, 7)
					return
				}
				if rec.Code != tc.want {
					t.Fatalf("want %d, got %d (%s)", tc.want, rec.Code, rec.Body.String())
				}
				if body := decodeIntrospection(t, rec); body["active"] != true || body["token_type"] != kind {
					t.Fatalf("introspection = %v, want an active %s", body, kind)
				}
			})
		}
	}
}
