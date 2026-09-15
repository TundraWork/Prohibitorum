package oidc

import (
	"context"
	"encoding/base64"
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
		ClientID:         id,
		ClientSecretHash: pgtype.Text{String: mustHash(t, secret), Valid: true},
		ClientAuthMethod: method,
	}
}

func publicClient(id string) db.OidcClient {
	return db.OidcClient{
		ClientID:         id,
		ClientSecretHash: pgtype.Text{Valid: false},
		ClientAuthMethod: "none",
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

// rawBasicAuth sets a Basic Authorization header from an already-assembled
// credentials string, bypassing the encoding that req.SetBasicAuth applies. The
// tests for RFC 6749 §2.3.1 decoding need to control the exact bytes between
// the colon and the base64 layer.
func rawBasicAuth(req *http.Request, credentials string) {
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(credentials)))
}

func TestClientLoadUnknown(t *testing.T) {
	q := fakeClientQueries{clients: map[string]db.OidcClient{}}
	if _, err := loadClient(context.Background(), q, "nope"); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientBasicHappyPath(t *testing.T) {
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret")
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
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret")
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
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	req.SetBasicAuth("cid", "wrong")

	if _, err := authenticateClient(context.Background(), q, req); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientWrongSecretPost(t *testing.T) {
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret")
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

func TestClientMissingSecretForConfidential(t *testing.T) {
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	// Neither channel carries a secret: no Authorization header, no form secret.
	if _, err := authenticateClient(context.Background(), q, postForm("cid", "")); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientBothBasicAndPostRejected(t *testing.T) {
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	req := postForm("cid", "s3cr3t")
	req.SetBasicAuth("cid", "s3cr3t")

	if _, err := authenticateClient(context.Background(), q, req); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientConfidentialAcceptsBasic(t *testing.T) {
	// Both credential channels are equivalent for a confidential client, so
	// Basic is accepted with no per-client registration of the channel.
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret")
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

func TestClientConfidentialAcceptsPost(t *testing.T) {
	// The client_secret_post channel is reachable: this combination used to be
	// rejected because every confidential client was registered as
	// client_secret_basic, which made the discovery document's
	// client_secret_post claim false in practice (PHB-19).
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	// postForm sends client_id + client_secret in the POST body, no Basic header.
	got, err := authenticateClient(context.Background(), q, postForm("cid", "s3cr3t"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ClientID != "cid" {
		t.Fatalf("got client %q", got.ClientID)
	}
}

func TestClientBasicPercentEncodedCredentials(t *testing.T) {
	// RFC 6749 §2.3.1: the two values are form-urlencoded before being joined
	// and base64'd. Some RP libraries encode the unreserved `-` and `_` too, so
	// `my-client` arrives as `my%2Dclient`. Decoding must recover both values.
	const id, secret = "my-client", "se-cr_et"
	c := confidentialClient(t, id, secret, "client_secret")
	q := fakeClientQueries{clients: map[string]db.OidcClient{id: c}}

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	rawBasicAuth(req, "my%2Dclient:se%2Dcr%5Fet")

	got, err := authenticateClient(context.Background(), q, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ClientID != id {
		t.Fatalf("got client %q", got.ClientID)
	}
}

func TestClientBasicPlusDecodesToSpace(t *testing.T) {
	// form-urlencoded semantics, not path semantics: `+` is a space.
	const id, secret = "cid", "two words"
	c := confidentialClient(t, id, secret, "client_secret")
	q := fakeClientQueries{clients: map[string]db.OidcClient{id: c}}

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	rawBasicAuth(req, "cid:two+words")

	if _, err := authenticateClient(context.Background(), q, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientBasicMalformedBase64(t *testing.T) {
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	req.Header.Set("Authorization", "Basic !!!")

	if _, err := authenticateClient(context.Background(), q, req); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientBasicMissingColon(t *testing.T) {
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	rawBasicAuth(req, "justuser")

	if _, err := authenticateClient(context.Background(), q, req); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientBasicInvalidPercentEscape(t *testing.T) {
	// An illegal escape sequence is a hard failure — there is deliberately no
	// fall back to the raw value.
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	rawBasicAuth(req, "a%zz:s3cr3t")

	if _, err := authenticateClient(context.Background(), q, req); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientBasicWithMatchingFormClientID(t *testing.T) {
	// client_id is an identifier, not a credential. Spring Security and several
	// Node/Python RP libraries repeat it in the body while authenticating with
	// Basic; that is one authentication method, not two.
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	req := postForm("cid", "")
	req.SetBasicAuth("cid", "s3cr3t")

	got, err := authenticateClient(context.Background(), q, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ClientID != "cid" {
		t.Fatalf("got client %q", got.ClientID)
	}
}

func TestClientBasicWithMismatchedFormClientID(t *testing.T) {
	// The repetition is tolerated only while the two copies agree.
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	req := postForm("other", "")
	req.SetBasicAuth("cid", "s3cr3t")

	if _, err := authenticateClient(context.Background(), q, req); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientBasicWithFormSecretStillRejected(t *testing.T) {
	// Complements TestClientBothBasicAndPostRejected: a body client_secret with
	// no body client_id is still the dual authentication RFC 6749 §2.3 forbids.
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	form := url.Values{}
	form.Set("client_secret", "s3cr3t")
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("cid", "s3cr3t")

	if _, err := authenticateClient(context.Background(), q, req); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientPublicRejectsFormClientIDWithBasic(t *testing.T) {
	// The relaxation above must not open a Basic channel for public clients:
	// they may not send an Authorization header at all.
	c := publicClient("pub")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"pub": c}}

	req := postForm("pub", "")
	req.SetBasicAuth("pub", "")

	if _, err := authenticateClient(context.Background(), q, req); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
	}
}

func TestClientUnknownAuthMethodRejected(t *testing.T) {
	// Dirty data — e.g. a pre-migration 'client_secret_basic' row — must not be
	// read as "not none, therefore confidential".
	c := confidentialClient(t, "cid", "s3cr3t", "client_secret_basic")
	q := fakeClientQueries{clients: map[string]db.OidcClient{"cid": c}}

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
	req.SetBasicAuth("cid", "s3cr3t")

	if _, err := authenticateClient(context.Background(), q, req); !errors.Is(err, errInvalidClient) {
		t.Fatalf("expected errInvalidClient, got %v", err)
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
	known := confidentialClient(t, "known", knownSecret, "client_secret")
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

	// Case E: an undecodable Basic header never reaches loadClient, so it must
	// not run argon2 either. There is no known-vs-unknown oracle to equalize
	// when no client_id was recovered, and running the dummy verify here would
	// hand a caller an argon2id burn for the cost of three bytes.
	for _, credentials := range []string{
		"!!!",              // not base64 — set raw below
		"justuser",         // no colon
		"a%zz:some-secret", // illegal percent escape
	} {
		calls = nil
		req := httptest.NewRequest(http.MethodPost, "/oauth/token", nil)
		if credentials == "!!!" {
			req.Header.Set("Authorization", "Basic !!!")
		} else {
			rawBasicAuth(req, credentials)
		}
		if _, err := authenticateClient(context.Background(), q, req); !errors.Is(err, errInvalidClient) {
			t.Fatalf("case E (%q): expected errInvalidClient, got %v", credentials, err)
		}
		if len(calls) != 0 {
			t.Fatalf("case E (%q): expected 0 verify calls on an unparsable Basic header, got %d", credentials, len(calls))
		}
	}
}
