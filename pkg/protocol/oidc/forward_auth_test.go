package oidc

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"prohibitorum/pkg/kv"
)

func TestForwardAuth_PKCE_S256(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := pkceChallengeS256(verifier); got != want {
		t.Fatalf("pkceChallengeS256 = %q, want %q", got, want)
	}
	if !verifyPKCE(verifier, want) {
		t.Fatal("verifyPKCE should accept the matching verifier")
	}
	if verifyPKCE("wrong", want) {
		t.Fatal("verifyPKCE should reject a wrong verifier")
	}
}

func TestForwardAuth_Session_RoundTrip(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	tok, err := mintFASession(ctx, store, faSession{AccountID: 42, ClientID: "svc"}, time.Hour)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	got := loadFASession(ctx, store, tok)
	if got == nil || got.AccountID != 42 || got.ClientID != "svc" {
		t.Fatalf("load = %+v", got)
	}
	if loadFASession(ctx, store, "nonexistent") != nil {
		t.Fatal("missing token should load nil")
	}
}

func TestForwardAuth_State_SingleUse(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	id, err := mintFAState(ctx, store, faState{OriginalURL: "https://app.acme.io/foo", ClientID: "svc", Verifier: "v"}, 5*time.Minute)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	st := popFAState(ctx, store, id)
	if st == nil || st.OriginalURL != "https://app.acme.io/foo" {
		t.Fatalf("pop = %+v", st)
	}
	if popFAState(ctx, store, id) != nil {
		t.Fatal("state must be single-use")
	}
}

func TestForwardAuth_Cookie_HostOnly(t *testing.T) {
	c := faCookie(true, "tok")
	if c.Name != "__Host-"+forwardAuthCookieBase || !c.Secure || !c.HttpOnly || c.Path != "/" || c.Domain != "" {
		t.Fatalf("secure cookie wrong: %+v", c)
	}
	if c2 := faCookie(false, "tok"); c2.Name != forwardAuthCookieBase || c2.Secure {
		t.Fatalf("insecure cookie wrong: %+v", c2)
	}
}

func TestForwardAuth_IdentityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	writeIdentityHeaders(rec, "alice", "Alice A", "alice@example.com", []string{"admins", "staff"})
	h := rec.Header()
	if h.Get("Remote-User") != "alice" || h.Get("Remote-Name") != "Alice A" ||
		h.Get("Remote-Email") != "alice@example.com" || h.Get("Remote-Groups") != "admins,staff" {
		t.Fatalf("headers: %v", h)
	}
	rec2 := httptest.NewRecorder()
	writeIdentityHeaders(rec2, "bob", "Bob", "", nil)
	if _, ok := rec2.Header()["Remote-Email"]; ok {
		t.Fatal("empty email must be omitted")
	}
}
