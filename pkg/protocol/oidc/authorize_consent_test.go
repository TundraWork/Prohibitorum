package oidc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/authn"
)

// (1) RequireConsent + a stored grant covering every requested scope → proceeds
// (no /consent bounce); the handler issues a code to the RP.
func TestAuthorize_Consent_StoredGrantCovers_Proceeds(t *testing.T) {
	c := validClient()
	c.RequireConsent = true
	q := &fakeAuthzQueries{
		client:  c,
		session: validSession(),
		granted: []string{"openid", "profile"}, // covers the requested scope
	}
	p := newProvider(q, &recordingAudit{})

	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(baseParams()))

	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	loc := rec.Result().Header.Get("Location")
	if strings.Contains(loc, "/consent?ticket=") {
		t.Fatalf("covered grant must not bounce to /consent, got %q", loc)
	}
	if !strings.HasPrefix(loc, "https://rp.example.com/callback?") {
		t.Fatalf("want redirect to the RP callback, got %q", loc)
	}
	u, _ := http.NewRequest("GET", loc, nil)
	if u.URL.Query().Get("code") == "" {
		t.Fatalf("expected a code in the RP redirect, got %q", loc)
	}
}

// (2) RequireConsent + missing grant (pgx.ErrNoRows) → 302 bounce to
// <issuer>/consent?ticket=...
func TestAuthorize_Consent_MissingGrant_BouncesToConsent(t *testing.T) {
	c := validClient()
	c.RequireConsent = true
	q := &fakeAuthzQueries{
		client:     c,
		session:    validSession(),
		grantedErr: pgx.ErrNoRows,
	}
	p := newProvider(q, &recordingAudit{})

	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(baseParams()))

	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	loc := rec.Result().Header.Get("Location")
	wantPrefix := testIssuer + "/consent?ticket="
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Fatalf("missing grant must bounce to %q, got %q", wantPrefix, loc)
	}
	// The bounce must carry a return_to that is issuer-origin and free of any
	// reauth nonce (none present here, but the strip must not break the param).
	cu, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse consent Location %q: %v", loc, err)
	}
	returnTo := cu.Query().Get("return_to")
	if returnTo == "" {
		t.Fatalf("consent bounce must carry a return_to; got %q", loc)
	}
	if !strings.HasPrefix(returnTo, testIssuer) {
		t.Fatalf("return_to must be issuer-origin; got %q", returnTo)
	}
	if strings.Contains(returnTo, "reauth") {
		t.Fatalf("return_to must not carry a reauth nonce; got %q", returnTo)
	}
}

// (2c) The consent bounce's return_to must strip any reauth nonce that rode in
// on the request — a re-auth demand (prompt=login) is satisfied by a single-use
// nonce consumed before consent, so echoing it would be dead state. We reach the
// consent bounce WITH prompt=login by satisfying re-auth first (the same way the
// package's re-auth tests do: DemandReauth → fresh auth_time → pass reauth=nonce),
// then forcing consent via a missing grant on a RequireConsent client.
func TestAuthorize_Consent_StripsStaleReauthFromReturnTo(t *testing.T) {
	c := validClient()
	c.RequireConsent = true
	p := newProvider(&fakeAuthzQueries{client: c}, &recordingAudit{})

	// Satisfy the re-auth demand: a marker written now + an auth_time strictly
	// after it makes the consumed nonce valid, so the flow falls through to the
	// consent check rather than re-bouncing to /login.
	nonce, err := authn.DemandReauth(context.Background(), p.kv, "oidc:reauth:", 42)
	if err != nil {
		t.Fatalf("DemandReauth: %v", err)
	}
	freshAuth := time.Now().Add(time.Second)
	p.queries = &fakeAuthzQueries{
		client:     c,
		session:    sessionWithAuthTime(freshAuth),
		grantedErr: pgx.ErrNoRows, // no grant → consent needed
	}

	v := baseParams()
	v.Set("prompt", "login")
	v.Set("reauth", nonce)
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	loc := rec.Result().Header.Get("Location")
	if !strings.HasPrefix(loc, testIssuer+"/consent?ticket=") {
		t.Fatalf("prompt=login + missing grant must bounce to /consent, got %q", loc)
	}
	cu, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse consent Location %q: %v", loc, err)
	}
	returnTo := cu.Query().Get("return_to")
	if returnTo == "" {
		t.Fatalf("consent bounce must carry a return_to; got %q", loc)
	}
	if !strings.HasPrefix(returnTo, testIssuer) {
		t.Fatalf("return_to must be issuer-origin; got %q", returnTo)
	}
	if strings.Contains(returnTo, "reauth") {
		t.Fatalf("return_to must strip the stale reauth nonce; got %q", returnTo)
	}
}

// (2b) RequireConsent + an insufficient grant (covers some, not all requested
// scopes) → 302 bounce to /consent.
func TestAuthorize_Consent_InsufficientGrant_BouncesToConsent(t *testing.T) {
	c := validClient()
	c.RequireConsent = true
	q := &fakeAuthzQueries{
		client:  c,
		session: validSession(),
		granted: []string{"openid"}, // missing "profile" from the request
	}
	p := newProvider(q, &recordingAudit{})

	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(baseParams()))

	loc := rec.Result().Header.Get("Location")
	wantPrefix := testIssuer + "/consent?ticket="
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Fatalf("insufficient grant must bounce to %q, got %q", wantPrefix, loc)
	}
}

// (3) prompt=consent forces the bounce even when a full grant exists.
func TestAuthorize_Consent_PromptConsent_ForcesBounce(t *testing.T) {
	c := validClient()
	c.RequireConsent = true
	q := &fakeAuthzQueries{
		client:  c,
		session: validSession(),
		granted: []string{"openid", "profile"}, // fully covers, but...
	}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Set("prompt", "consent")
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	loc := rec.Result().Header.Get("Location")
	wantPrefix := testIssuer + "/consent?ticket="
	if !strings.HasPrefix(loc, wantPrefix) {
		t.Fatalf("prompt=consent must force a /consent bounce, got %q", loc)
	}
}

// (4) prompt=none + consent needed → consent_required redirected to the RP.
func TestAuthorize_Consent_PromptNone_ConsentRequired(t *testing.T) {
	c := validClient()
	c.RequireConsent = true
	q := &fakeAuthzQueries{
		client:     c,
		session:    validSession(),
		grantedErr: pgx.ErrNoRows,
	}
	p := newProvider(q, &recordingAudit{})

	v := baseParams()
	v.Set("prompt", "none")
	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(v))

	gotQ := redirectQuery(t, rec)
	if got := gotQ.Get("error"); got != errCodeConsentRequired {
		t.Fatalf("want error=%s, got %q", errCodeConsentRequired, got)
	}
	loc := rec.Result().Header.Get("Location")
	if !strings.HasPrefix(loc, "https://rp.example.com/callback") {
		t.Fatalf("consent_required must go to the RP redirect_uri, got %q", loc)
	}
	if strings.Contains(loc, "/consent?ticket=") {
		t.Fatalf("prompt=none must not bounce to /consent, got %q", loc)
	}
}

// (5) RequireConsent=false → trusted-client skip; GetConsent is never consulted
// and the flow proceeds to code issuance.
func TestAuthorize_Consent_TrustedClient_Skips(t *testing.T) {
	c := validClient()
	c.RequireConsent = false
	// grantedErr would surface if GetConsent were (wrongly) consulted.
	q := &fakeAuthzQueries{
		client:     c,
		session:    validSession(),
		grantedErr: pgx.ErrNoRows,
	}
	p := newProvider(q, &recordingAudit{})

	rec := httptest.NewRecorder()
	p.HandleAuthorize(rec, authedReq(baseParams()))

	if rec.Code != http.StatusFound {
		t.Fatalf("want 302, got %d", rec.Code)
	}
	loc := rec.Result().Header.Get("Location")
	if strings.Contains(loc, "/consent?ticket=") {
		t.Fatalf("trusted client must not bounce to /consent, got %q", loc)
	}
	if !strings.HasPrefix(loc, "https://rp.example.com/callback?") {
		t.Fatalf("trusted client should proceed to the RP callback, got %q", loc)
	}
}
