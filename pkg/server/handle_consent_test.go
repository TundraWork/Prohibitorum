// Package server — handle_consent_test.go
//
// Unit tests for the pure helper unionScopes in handle_consent.go, the
// cfg() helper shared with returnto_test.go, and the OIDC consent context
// endpoint (handleConsentContextHTTP). Open-redirect cases previously covered
// by TestSameOriginAsIssuer are now covered by TestValidateReturnTo in
// returnto_test.go (sameOriginAsIssuer was deleted; validateReturnTo is the
// shared successor).

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

// --- fake DB for OIDC consent context -----------------------------------------

type fakeOIDCConsentQ struct {
	client        db.OidcClient
	clientErr     error
	grantedScopes []string // the stored grant GetConsent returns (when noGrant is false)
	noGrant       bool     // when true, GetConsent returns pgx.ErrNoRows (no existing grant)
}

func (f *fakeOIDCConsentQ) GetOIDCClient(_ context.Context, _ string) (db.OidcClient, error) {
	return f.client, f.clientErr
}

func (f *fakeOIDCConsentQ) GetConsent(_ context.Context, _ db.GetConsentParams) ([]string, error) {
	if f.noGrant {
		return nil, pgx.ErrNoRows
	}
	return f.grantedScopes, nil
}

// --- test helpers for OIDC consent context ------------------------------------

func newOIDCConsentTestServer(t *testing.T, fq *fakeOIDCConsentQ) (*Server, kv.Store) {
	t.Helper()
	kvStore := kv.NewMemoryStore()
	s := &Server{
		kvStore:            kvStore,
		oidcConsentOverride: fq,
	}
	return s, kvStore
}

func mintOIDCTicket(t *testing.T, kvStore kv.Store, ticket authn.ConsentTicket) string {
	t.Helper()
	nonce, err := authn.DemandConsent(context.Background(), kvStore, ticket)
	if err != nil {
		t.Fatalf("DemandConsent: %v", err)
	}
	return nonce
}

func oidcConsentGETReq(t *testing.T, sess *authn.Session, ticketParam string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/prohibitorum/consent?ticket=%s", ticketParam), nil)
	r = r.WithContext(authn.WithSession(r.Context(), sess))
	return r
}

func newOIDCConsentSession(accountID int32) *authn.Session {
	acct := &db.Account{ID: accountID, Username: "alice", DisplayName: "Alice Liddell"}
	data := &authn.SessionData{AccountID: accountID}
	return &authn.Session{Account: acct, Data: data}
}

func cfg(issuer string) *configx.Config {
	return &configx.Config{OIDC: configx.OIDCConfig{Issuer: issuer}}
}

// --- OIDC consent context (GET) tests -----------------------------------------

// TestConsentContext_Incremental: existing grant of ["openid"] + request for
// ["openid","email"] → Scopes == both, AlreadyGranted == ["openid"].
func TestConsentContext_Incremental(t *testing.T) {
	const accountID int32 = 7
	const clientID = "rp1"

	fq := &fakeOIDCConsentQ{
		client: db.OidcClient{ClientID: clientID, DisplayName: "My App"},
		grantedScopes: []string{"openid"},
	}
	s, kvStore := newOIDCConsentTestServer(t, fq)

	nonce := mintOIDCTicket(t, kvStore, authn.ConsentTicket{
		AccountID: accountID, ClientID: clientID,
		Scopes: []string{"openid", "email"}, RedirectURI: "https://rp/cb",
	})

	sess := newOIDCConsentSession(accountID)
	r := oidcConsentGETReq(t, sess, nonce)
	w := httptest.NewRecorder()
	s.handleConsentContextHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var out contract.ConsentContext
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !reflect.DeepEqual(out.Scopes, []string{"openid", "email"}) {
		t.Errorf("Scopes: want [openid email], got %v", out.Scopes)
	}
	if !reflect.DeepEqual(out.AlreadyGranted, []string{"openid"}) {
		t.Errorf("AlreadyGranted: want [openid], got %v", out.AlreadyGranted)
	}
}

// TestConsentContext_FirstTime: no existing grant (GetConsent → ErrNoRows) →
// AlreadyGranted is empty/nil.
func TestConsentContext_FirstTime(t *testing.T) {
	const accountID int32 = 7
	const clientID = "rp1"

	fq := &fakeOIDCConsentQ{
		client:  db.OidcClient{ClientID: clientID, DisplayName: "My App"},
		noGrant: true,
	}
	s, kvStore := newOIDCConsentTestServer(t, fq)

	nonce := mintOIDCTicket(t, kvStore, authn.ConsentTicket{
		AccountID: accountID, ClientID: clientID,
		Scopes: []string{"openid", "email"}, RedirectURI: "https://rp/cb",
	})

	sess := newOIDCConsentSession(accountID)
	r := oidcConsentGETReq(t, sess, nonce)
	w := httptest.NewRecorder()
	s.handleConsentContextHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var out contract.ConsentContext
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out.AlreadyGranted) != 0 {
		t.Errorf("AlreadyGranted: want empty, got %v", out.AlreadyGranted)
	}
}


func TestUnionScopes(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want []string
	}{
		{
			name: "dedup overlapping element",
			a:    []string{"openid"},
			b:    []string{"openid", "profile"},
			want: []string{"openid", "profile"},
		},
		{
			name: "nil first slice",
			a:    nil,
			b:    []string{"openid"},
			want: []string{"openid"},
		},
		{
			name: "partial overlap preserves order",
			a:    []string{"a", "b"},
			b:    []string{"b", "c"},
			want: []string{"a", "b", "c"},
		},
		{
			name: "nil second slice",
			a:    []string{"openid"},
			b:    nil,
			want: []string{"openid"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := unionScopes(tc.a, tc.b)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("unionScopes(%v, %v) = %v; want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
