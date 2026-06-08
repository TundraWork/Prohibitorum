package oidc

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/credential/password"
	"prohibitorum/pkg/db"
)

// fakeClientQueries implements clientQueries with a canned map of clients.
type fakeClientQueries struct {
	clients map[string]db.OidcClient
}

func (f fakeClientQueries) GetOIDCClient(_ context.Context, clientID string) (db.OidcClient, error) {
	c, ok := f.clients[clientID]
	if !ok {
		return db.OidcClient{}, pgx.ErrNoRows
	}
	return c, nil
}

// errorClientQueries always returns a fixed error from GetOIDCClient.
type errorClientQueries struct{ err error }

func (e errorClientQueries) GetOIDCClient(_ context.Context, _ string) (db.OidcClient, error) {
	return db.OidcClient{}, e.err
}

func mustHash(t *testing.T, secret string) string {
	t.Helper()
	phc, err := password.HashRaw(secret, password.DefaultParams())
	if err != nil {
		t.Fatalf("HashRaw: %v", err)
	}
	return phc
}

func confidentialClient(t *testing.T, id, secret, method string) db.OidcClient {
	t.Helper()
	return db.OidcClient{
		ClientID:                id,
		ClientSecretHash:        pgtype.Text{String: mustHash(t, secret), Valid: true},
		TokenEndpointAuthMethod: method,
	}
}

func publicClient(id string) db.OidcClient {
	return db.OidcClient{
		ClientID:                id,
		ClientSecretHash:        pgtype.Text{Valid: false},
		TokenEndpointAuthMethod: "none",
	}
}

func postForm(id, secret string) *http.Request {
	form := url.Values{}
	form.Set("client_id", id)
	if secret != "" {
		form.Set("client_secret", secret)
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestClientLoadUnknown(t *testing.T) {
	q := fakeClientQueries{clients: map[string]db.OidcClient{}}
	if _, err := loadClient(context.Background(), q, "nope"); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientBasicHappyPath(t *testing.T) {
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret_basic")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	req.SetBasicAuth("cid", "s3cr3t")

	got, err := authenticateClient(context.Background(), q, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ClientID != "cid" {
		t.Fatalf("got client %q", got.ClientID)
	}
}

func TestClientPostHappyPath(t *testing.T) {
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret_post")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	got, err := authenticateClient(context.Background(), q, postForm("cid", "s3cr3t"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ClientID != "cid" {
		t.Fatalf("got client %q", got.ClientID)
	}
}

func TestClientWrongSecretBasic(t *testing.T) {
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret_basic")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	req.SetBasicAuth("cid", "wrong")

	if _, err := authenticateClient(context.Background(), q, req); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientWrongSecretPost(t *testing.T) {
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret_post")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	if _, err := authenticateClient(context.Background(), q, postForm("cid", "wrong")); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientNonePublicHappyPath(t *testing.T) {
	c := publicClient("pub")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"pub": c}}

	got, err := authenticateClient(context.Background(), q, postForm("pub", ""))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ClientID != "pub" {
		t.Fatalf("got client %q", got.ClientID)
	}
}

func TestClientSecretPresentedToPublicViaPost(t *testing.T) {
	c := publicClient("pub")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"pub": c}}

	if _, err := authenticateClient(context.Background(), q, postForm("pub", "snuck-in")); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientSecretPresentedToPublicViaBasic(t *testing.T) {
	c := publicClient("pub")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"pub": c}}

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	req.SetBasicAuth("pub", "snuck-in")

	if _, err := authenticateClient(context.Background(), q, req); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientUnknownClientID(t *testing.T) {
	q := fakeClientQueries{clients: map[string]db.OidcClient{}}

	if _, err := authenticateClient(context.Background(), q, postForm("ghost", "x")); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientDisabledReturnsInvalid(t *testing.T) {
	// The query filters disabled clients, so a disabled client surfaces as
	// pgx.ErrNoRows. The fake simulates that by simply not having the row.
	q := fakeClientQueries{clients: map[string]db.OidcClient{}}

	if _, err := authenticateClient(context.Background(), q, postForm("disabled-cid", "x")); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientMissingSecretForConfidentialBasic(t *testing.T) {
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret_basic")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	// No Authorization header, no form secret.
	if _, err := authenticateClient(context.Background(), q, postForm("cid", "")); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientMissingSecretForConfidentialPost(t *testing.T) {
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret_post")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	if _, err := authenticateClient(context.Background(), q, postForm("cid", "")); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientBothBasicAndPostRejected(t *testing.T) {
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret_basic")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	req := postForm("cid", "s3cr3t")
	req.SetBasicAuth("cid", "s3cr3t")

	if _, err := authenticateClient(context.Background(), q, req); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientWrongAuthMethodPostUsedAsBasic(t *testing.T) {
	// Client requires client_secret_post but caller used Basic.
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret_post")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	req.SetBasicAuth("cid", "s3cr3t")

	if _, err := authenticateClient(context.Background(), q, req); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientPublicNoneBasicEmptyPasswordRejected(t *testing.T) {
	// Channel-binding bypass: Basic header with an empty password must be
	// rejected for a none/public client. r.BasicAuth() returns ("pub", "", true)
	// for "Basic base64(pub:)", which previously slipped past the secret-value
	// check (basicSecret == ""). The fix adds hasBasic to the guard.
	c := publicClient("pub")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"pub": c}}

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	req.SetBasicAuth("pub", "") // empty password — still a Basic header

	if _, err := authenticateClient(context.Background(), q, req); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient for none client with Basic empty-password, got %v", err)
	}
}

func TestClientBasicMethodFormOnlyRejected(t *testing.T) {
	// Regression guard: a client registered for client_secret_basic that
	// presents credentials via the form (no Basic header) must be rejected.
	// This is already enforced by the !hasBasic check in the basic case; this
	// test locks in that behaviour.
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret_basic")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	// postForm sends client_id + client_secret in the POST body, no Basic header.
	if _, err := authenticateClient(context.Background(), q, postForm("cid", "s3cr3t")); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient for basic client authenticated via form only, got %v", err)
	}
}

func TestAuthenticateClientTimingEqualization(t *testing.T) {
	// spy records each (secret, phc) verifyClientSecret call.
	type call struct{ secret, phc string }
	var calls []call

	orig := verifyClientSecret
	verifyClientSecret = func(secret, phc string) bool {
		calls = append(calls, call{secret, phc})
		return orig(secret, phc) // delegate so known-good paths still authenticate
	}
	defer func() { verifyClientSecret = orig }()

	// A known confidential client registered for client_secret_post.
	const knownSecret = "kn0wn-s3cr3t"
	known := confidentialClient(t, "known", knownSecret, "client_secret_post")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"known": known}}

	// Case A: unknown client_id + secret via form → errInvalidClient AND a
	// dummy verify ran (calls[0].phc == dummyClientSecretPHC).
	calls = nil
	_, err := authenticateClient(context.Background(), q, postForm("ghost", "some-secret"))
	if !errors.Is(err, errInvalidClient) {
		t.Fatalf("case A: expected errInvalidClient, got %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("case A: expected 1 verify call (dummy), got %d", len(calls))
	}
	if calls[0].phc != dummyClientSecretPHC {
		t.Fatalf("case A: verify was called against %q, want dummyClientSecretPHC", calls[0].phc)
	}

	// Case A2: unknown client_id + secret via Basic → same dummy-verify behaviour.
	calls = nil
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	req.SetBasicAuth("ghost", "some-secret")
	_, err = authenticateClient(context.Background(), q, req)
	if !errors.Is(err, errInvalidClient) {
		t.Fatalf("case A2: expected errInvalidClient, got %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("case A2: expected 1 verify call (dummy), got %d", len(calls))
	}
	if calls[0].phc != dummyClientSecretPHC {
		t.Fatalf("case A2: verify was called against %q, want dummyClientSecretPHC", calls[0].phc)
	}

	// Case B: unknown client_id + NO secret → errInvalidClient AND no argon2
	// runs (no verify call at all).
	calls = nil
	_, err = authenticateClient(context.Background(), q, postForm("ghost", ""))
	if !errors.Is(err, errInvalidClient) {
		t.Fatalf("case B: expected errInvalidClient, got %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("case B: expected 0 verify calls (no secret), got %d", len(calls))
	}

	// Case C: known confidential client + WRONG secret → errInvalidClient AND
	// a verify ran against the REAL client hash (not dummyClientSecretPHC).
	calls = nil
	_, err = authenticateClient(context.Background(), q, postForm("known", "wrong"))
	if !errors.Is(err, errInvalidClient) {
		t.Fatalf("case C: expected errInvalidClient, got %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("case C: expected 1 verify call (real hash), got %d", len(calls))
	}
	if calls[0].phc == dummyClientSecretPHC {
		t.Fatal("case C: verify was called against the dummy PHC — should use the real client hash")
	}
	if calls[0].phc != known.ClientSecretHash.String {
		t.Fatalf("case C: verify PHC = %q, want real client hash %q", calls[0].phc, known.ClientSecretHash.String)
	}

	// Case D: infra error from loadClient (not errInvalidClient, e.g. DB down) +
	// a presented secret → error is returned AND the dummy verify is NOT run.
	// Burning argon2 on an infra error is wasteful and pointless: no oracle exists
	// between known/unknown client_ids when the DB itself is failing.
	calls = nil
	dbDown := errors.New("db down")
	qErr := errorClientQueries{err: dbDown}
	_, err = authenticateClient(context.Background(), qErr, postForm("any-client", "some-secret"))
	if err == nil {
		t.Fatal("case D: expected non-nil error, got nil")
	}
	if errors.Is(err, errInvalidClient) {
		t.Fatalf("case D: expected raw infra error, got errInvalidClient")
	}
	if len(calls) != 0 {
		t.Fatalf("case D: expected 0 verify calls on infra error, got %d", len(calls))
	}
}
