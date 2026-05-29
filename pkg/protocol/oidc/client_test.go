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
