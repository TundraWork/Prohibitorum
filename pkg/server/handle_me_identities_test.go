// Package server — handle_me_identities_test.go
//
// Handler-level tests for the /me/identities surface (Task 8). The list +
// unlink endpoints get a dedicated narrow fake (fakeIdentitiesQueries) so we
// can exercise the last-sign-in-method logic in isolation; the link/begin +
// link/callback endpoints reuse the federation harness from
// handle_federation_test.go since they share the federator's KV state model.

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	fedoidc "prohibitorum/pkg/federation/oidc"
	"prohibitorum/pkg/kv"
	sessstore "prohibitorum/pkg/session"
)

// --- fakeIdentitiesQueries -------------------------------------------------

// fakeIdentitiesQueries satisfies meIdentitiesQueries + authn.FlowQueries.
// Embeds db.Querier so any unexpected dispatch nil-panics — the loudest
// possible signal that the fake is missing a method the handler reached for.
type fakeIdentitiesQueries struct {
	db.Querier

	mu sync.Mutex

	webauthnRows []db.WebauthnCredential
	passwordRow  *db.PasswordCredential
	totpRow      *db.TotpCredential
	identityRows []db.ListAccountIdentitiesByAccountRow

	deletedIdentities []db.DeleteAccountIdentityParams
	events            []db.InsertCredentialEventParams
	sessions          []db.Session

	// lockAcquisitions records every account_id passed to
	// GetAccountByIDForUpdate. callOrder records the dispatch order of
	// querier methods relevant to the unlink-race test. Both are read by
	// TestMeIdentities_Unlink_AcquiresLockBeforeCheck to verify the
	// FOR UPDATE call happens BEFORE any read in the count check.
	lockAcquisitions []int32
	callOrder        []string
}

func newFakeIdentitiesQueries() *fakeIdentitiesQueries {
	return &fakeIdentitiesQueries{}
}

func (f *fakeIdentitiesQueries) GetAccountByIDForUpdate(_ context.Context, id int32) (db.Account, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lockAcquisitions = append(f.lockAcquisitions, id)
	f.callOrder = append(f.callOrder, "GetAccountByIDForUpdate")
	return db.Account{ID: id}, nil
}

func (f *fakeIdentitiesQueries) ListCredentialsByAccount(_ context.Context, accountID int32) ([]db.WebauthnCredential, error) {
	f.mu.Lock()
	f.callOrder = append(f.callOrder, "ListCredentialsByAccount")
	f.mu.Unlock()
	var out []db.WebauthnCredential
	for _, c := range f.webauthnRows {
		if c.AccountID == accountID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeIdentitiesQueries) GetPasswordCredential(_ context.Context, accountID int32) (db.PasswordCredential, error) {
	f.mu.Lock()
	f.callOrder = append(f.callOrder, "GetPasswordCredential")
	f.mu.Unlock()
	if f.passwordRow == nil || f.passwordRow.AccountID != accountID {
		return db.PasswordCredential{}, pgx.ErrNoRows
	}
	return *f.passwordRow, nil
}

func (f *fakeIdentitiesQueries) GetTOTPCredential(_ context.Context, accountID int32) (db.TotpCredential, error) {
	f.mu.Lock()
	f.callOrder = append(f.callOrder, "GetTOTPCredential")
	f.mu.Unlock()
	if f.totpRow == nil || f.totpRow.AccountID != accountID {
		return db.TotpCredential{}, pgx.ErrNoRows
	}
	return *f.totpRow, nil
}

func (f *fakeIdentitiesQueries) DeletePasswordCredential(_ context.Context, _ int32) error { return nil }
func (f *fakeIdentitiesQueries) DeleteTOTPCredential(_ context.Context, _ int32) error     { return nil }
func (f *fakeIdentitiesQueries) DeleteAllRecoveryCodesByAccount(_ context.Context, _ int32) error {
	return nil
}

func (f *fakeIdentitiesQueries) ListAccountIdentitiesByAccount(_ context.Context, accountID int32) ([]db.ListAccountIdentitiesByAccountRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callOrder = append(f.callOrder, "ListAccountIdentitiesByAccount")
	var out []db.ListAccountIdentitiesByAccountRow
	for _, r := range f.identityRows {
		if r.AccountID == accountID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeIdentitiesQueries) DeleteAccountIdentity(_ context.Context, arg db.DeleteAccountIdentityParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callOrder = append(f.callOrder, "DeleteAccountIdentity")
	f.deletedIdentities = append(f.deletedIdentities, arg)
	keep := f.identityRows[:0]
	for _, r := range f.identityRows {
		if r.ID == arg.ID && r.AccountID == arg.AccountID {
			continue
		}
		keep = append(keep, r)
	}
	f.identityRows = keep
	return nil
}

func (f *fakeIdentitiesQueries) InsertCredentialEvent(_ context.Context, arg db.InsertCredentialEventParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, arg)
	return nil
}

func (f *fakeIdentitiesQueries) InsertSession(_ context.Context, arg db.InsertSessionParams) (db.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row := db.Session{ID: arg.ID, AccountID: arg.AccountID, AuthTime: arg.AuthTime, Amr: arg.Amr}
	f.sessions = append(f.sessions, row)
	return row, nil
}

func (f *fakeIdentitiesQueries) RevokeSession(_ context.Context, _ string) error             { return nil }
func (f *fakeIdentitiesQueries) RevokeAllSessionsByAccount(_ context.Context, _ int32) error { return nil }

// Compile-time guards.
var _ meIdentitiesQueries = (*fakeIdentitiesQueries)(nil)
var _ authn.FlowQueries = (*fakeIdentitiesQueries)(nil)

// --- list/unlink scaffold --------------------------------------------------

// newIdentitiesTestServer builds a minimal Server wired for the list+unlink
// handlers: a fake querier that doubles as revokeFlowOverride (the seam
// meIdentitiesQ() probes), a memory KV, and a real session store.
func newIdentitiesTestServer(t *testing.T) (*Server, *fakeIdentitiesQueries) {
	t.Helper()
	q := newFakeIdentitiesQueries()

	cfg := &configx.Config{
		SessionTTL: time.Hour,
		Auth: configx.AuthConfig{
			SudoTTL: 5 * time.Minute,
		},
	}

	kvStore := kv.NewMemoryStore()
	t.Cleanup(func() { _ = kvStore.Close() })

	auditWriter := audit.NewWriter(q)
	sessionStore := sessstore.NewSessionStore(kvStore, q, cfg.SessionTTL)

	s := &Server{
		config:             cfg,
		kvStore:            kvStore,
		sessionStore:       sessionStore,
		rateLimiter:        authn.NewRateLimiter(),
		Audit:              auditWriter,
		revokeFlowOverride: q,
	}
	return s, q
}

func issueIdentitiesTestSession(t *testing.T, s *Server, accountID int32) (string, *authn.Session) {
	t.Helper()
	token, data, err := s.sessionStore.Issue(context.Background(), accountID, "127.0.0.1", "ua/test", []string{"hwk"})
	if err != nil {
		t.Fatalf("sessionStore.Issue: %v", err)
	}
	acct := &db.Account{ID: accountID, Username: "alice"}
	return token, &authn.Session{Account: acct, Token: token, Data: data}
}

func identitiesReq(t *testing.T, sess *authn.Session, method, path, body string) *http.Request {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	r.RemoteAddr = "127.0.0.1:5555"
	r = r.WithContext(authn.WithSession(r.Context(), sess))
	return r
}

func withRouteParam(r *http.Request, k, v string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(k, v)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func seedIdentity(q *fakeIdentitiesQueries, id, idpID int64, accountID int32, slug, name, email string) {
	row := db.ListAccountIdentitiesByAccountRow{
		ID:             id,
		AccountID:      accountID,
		UpstreamIdpID:  idpID,
		UpstreamIss:    "https://op.example.com",
		UpstreamSub:    "sub-" + slug,
		IdpSlug:        slug,
		IdpDisplayName: name,
		LinkedAt:       pgtype.Timestamptz{Time: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), Valid: true},
	}
	if email != "" {
		row.UpstreamEmail = pgtype.Text{String: email, Valid: true}
	}
	q.identityRows = append(q.identityRows, row)
}

// --- list tests ------------------------------------------------------------

func TestMeIdentities_List_Empty(t *testing.T) {
	s, _ := newIdentitiesTestServer(t)
	const accountID int32 = 42
	_, sess := issueIdentitiesTestSession(t, s, accountID)

	r := identitiesReq(t, sess, http.MethodGet, "/api/prohibitorum/me/identities", "")
	w := httptest.NewRecorder()
	s.handleMeIdentitiesListHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != "[]" {
		t.Errorf("body: want [], got %q", got)
	}
}

func TestMeIdentities_List_TwoRows(t *testing.T) {
	s, q := newIdentitiesTestServer(t)
	const accountID int32 = 42
	seedIdentity(q, 1, 100, accountID, "google", "Google", "alice@example.com")
	seedIdentity(q, 2, 101, accountID, "github", "GitHub", "")
	_, sess := issueIdentitiesTestSession(t, s, accountID)

	r := identitiesReq(t, sess, http.MethodGet, "/api/prohibitorum/me/identities", "")
	w := httptest.NewRecorder()
	s.handleMeIdentitiesListHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	var got []identityView
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (body=%s)", err, w.Body.String())
	}
	if len(got) != 2 {
		t.Fatalf("rows: want 2, got %d (body=%s)", len(got), w.Body.String())
	}
	if got[0].IdpSlug != "google" || got[0].IdpDisplayName != "Google" {
		t.Errorf("row[0]: %+v", got[0])
	}
	if got[0].UpstreamEmail == nil || *got[0].UpstreamEmail != "alice@example.com" {
		t.Errorf("row[0] email: want alice@example.com, got %v", got[0].UpstreamEmail)
	}
	if got[1].IdpSlug != "github" {
		t.Errorf("row[1] slug: %s", got[1].IdpSlug)
	}
	if got[1].UpstreamEmail != nil {
		t.Errorf("row[1] email: want nil, got %v", *got[1].UpstreamEmail)
	}
	if got[0].LinkedAt == "" {
		t.Errorf("linkedAt empty")
	}
}

// --- unlink tests ----------------------------------------------------------

func TestMeIdentities_Unlink_SudoGated(t *testing.T) {
	s, q := newIdentitiesTestServer(t)
	const accountID int32 = 42
	seedIdentity(q, 1, 100, accountID, "google", "Google", "alice@example.com")
	// Give the account a webauthn credential so the last-method check would
	// pass — that way a 401 here can only be the sudo gate firing.
	q.webauthnRows = []db.WebauthnCredential{{ID: 1, AccountID: accountID}}
	_, sess := issueIdentitiesTestSession(t, s, accountID)

	r := identitiesReq(t, sess, http.MethodPost, "/api/prohibitorum/me/identities/1/unlink", "")
	r = withRouteParam(r, "id", "1")
	w := httptest.NewRecorder()
	s.handleMeIdentitiesUnlinkHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	if body["code"] != "sudo_required" {
		t.Errorf("code: want sudo_required, got %v", body["code"])
	}
	if len(q.deletedIdentities) != 0 {
		t.Errorf("identity must not be deleted on sudo-gate fail")
	}
}

func TestMeIdentities_Unlink_LastMethod_Rejected(t *testing.T) {
	s, q := newIdentitiesTestServer(t)
	const accountID int32 = 42
	// Single federation identity, no webauthn, no password+TOTP.
	seedIdentity(q, 1, 100, accountID, "google", "Google", "alice@example.com")
	token, sess := issueIdentitiesTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := identitiesReq(t, sess, http.MethodPost, "/api/prohibitorum/me/identities/1/unlink", "")
	r = withRouteParam(r, "id", "1")
	w := httptest.NewRecorder()
	s.handleMeIdentitiesUnlinkHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	if body["code"] != "last_sign_in_method" {
		t.Errorf("code: want last_sign_in_method, got %v", body["code"])
	}
	if len(q.deletedIdentities) != 0 {
		t.Errorf("identity must not be deleted on last-method rejection")
	}
}

func TestMeIdentities_Unlink_WithBackup_204(t *testing.T) {
	s, q := newIdentitiesTestServer(t)
	const accountID int32 = 42
	seedIdentity(q, 1, 100, accountID, "google", "Google", "alice@example.com")
	// Backup factor: a webauthn credential.
	q.webauthnRows = []db.WebauthnCredential{{ID: 1, AccountID: accountID}}
	token, sess := issueIdentitiesTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := identitiesReq(t, sess, http.MethodPost, "/api/prohibitorum/me/identities/1/unlink", "")
	r = withRouteParam(r, "id", "1")
	w := httptest.NewRecorder()
	s.handleMeIdentitiesUnlinkHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status: want 204, got %d (body=%s)", w.Code, w.Body.String())
	}
	if len(q.deletedIdentities) != 1 {
		t.Fatalf("deleted identities: want 1, got %d", len(q.deletedIdentities))
	}
	if q.deletedIdentities[0].ID != 1 || q.deletedIdentities[0].AccountID != accountID {
		t.Errorf("delete params: %+v", q.deletedIdentities[0])
	}
	// Audit row: factor=federation_oidc, event=unlink, detail.identity_id=1.
	found := false
	for _, ev := range q.events {
		if ev.Factor != string(audit.FactorFederationOIDC) || ev.Event != audit.EventUnlink {
			continue
		}
		var detail map[string]any
		_ = json.Unmarshal(ev.Detail, &detail)
		if v, ok := detail["identity_id"]; ok && toInt64(v) == 1 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("audit: missing unlink row with identity_id=1; events=%+v", q.events)
	}
}

func TestMeIdentities_Unlink_TwoIdentities_204(t *testing.T) {
	s, q := newIdentitiesTestServer(t)
	const accountID int32 = 42
	// Two federation identities, no other factors. Unlinking one leaves
	// federation_oidc still in the methods set, so it's allowed.
	seedIdentity(q, 1, 100, accountID, "google", "Google", "alice@example.com")
	seedIdentity(q, 2, 101, accountID, "github", "GitHub", "")
	token, sess := issueIdentitiesTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := identitiesReq(t, sess, http.MethodPost, "/api/prohibitorum/me/identities/1/unlink", "")
	r = withRouteParam(r, "id", "1")
	w := httptest.NewRecorder()
	s.handleMeIdentitiesUnlinkHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status: want 204, got %d (body=%s)", w.Code, w.Body.String())
	}
	if len(q.deletedIdentities) != 1 {
		t.Fatalf("deleted identities: want 1, got %d", len(q.deletedIdentities))
	}
}

func TestMeIdentities_Unlink_BadID(t *testing.T) {
	s, q := newIdentitiesTestServer(t)
	const accountID int32 = 42
	seedIdentity(q, 1, 100, accountID, "google", "Google", "alice@example.com")
	q.webauthnRows = []db.WebauthnCredential{{ID: 1, AccountID: accountID}}
	token, sess := issueIdentitiesTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := identitiesReq(t, sess, http.MethodPost, "/api/prohibitorum/me/identities/abc/unlink", "")
	r = withRouteParam(r, "id", "abc")
	w := httptest.NewRecorder()
	s.handleMeIdentitiesUnlinkHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

// TestMeIdentities_Unlink_AcquiresLockBeforeCheck verifies the race-fix from
// the M3 audit finding: handleMeIdentitiesUnlinkHTTP must acquire a row-level
// lock on the account (via GetAccountByIDForUpdate) BEFORE running the
// last-sign-in-method count check and BEFORE the DeleteAccountIdentity write.
//
// The in-memory fake doesn't truly serialize concurrent requests; the actual
// race-prevention is DB-enforced (Postgres holds the FOR UPDATE row lock for
// the duration of the transaction, blocking concurrent unlinks on the same
// account). What we CAN assert here is dispatch order — the lock-acquire
// query must run first. The smoke-level test exercises real PG concurrency.
func TestMeIdentities_Unlink_AcquiresLockBeforeCheck(t *testing.T) {
	s, q := newIdentitiesTestServer(t)
	const accountID int32 = 42
	seedIdentity(q, 1, 100, accountID, "google", "Google", "alice@example.com")
	// Backup factor so the count check passes and the delete actually fires —
	// otherwise we'd only see the lock acquisition, no delete.
	q.webauthnRows = []db.WebauthnCredential{{ID: 1, AccountID: accountID}}
	token, sess := issueIdentitiesTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := identitiesReq(t, sess, http.MethodPost, "/api/prohibitorum/me/identities/1/unlink", "")
	r = withRouteParam(r, "id", "1")
	w := httptest.NewRecorder()
	s.handleMeIdentitiesUnlinkHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status: want 204, got %d (body=%s)", w.Code, w.Body.String())
	}

	// Lock acquired exactly once, on the right account.
	if len(q.lockAcquisitions) != 1 {
		t.Fatalf("lockAcquisitions: want 1 call, got %d (%v)", len(q.lockAcquisitions), q.lockAcquisitions)
	}
	if q.lockAcquisitions[0] != accountID {
		t.Errorf("lockAcquisitions[0]: want %d, got %d", accountID, q.lockAcquisitions[0])
	}

	// GetAccountByIDForUpdate is the FIRST querier call in the unlink path
	// (i.e., it precedes the count-check reads and the delete write).
	if len(q.callOrder) == 0 || q.callOrder[0] != "GetAccountByIDForUpdate" {
		t.Fatalf("first querier call: want GetAccountByIDForUpdate, got %v", q.callOrder)
	}

	// DeleteAccountIdentity must come AFTER the lock acquisition.
	lockIdx, deleteIdx := -1, -1
	for i, c := range q.callOrder {
		if c == "GetAccountByIDForUpdate" && lockIdx == -1 {
			lockIdx = i
		}
		if c == "DeleteAccountIdentity" && deleteIdx == -1 {
			deleteIdx = i
		}
	}
	if lockIdx < 0 {
		t.Fatalf("GetAccountByIDForUpdate not in callOrder: %v", q.callOrder)
	}
	if deleteIdx < 0 {
		t.Fatalf("DeleteAccountIdentity not in callOrder: %v", q.callOrder)
	}
	if lockIdx >= deleteIdx {
		t.Errorf("lock must be acquired before delete: lockIdx=%d deleteIdx=%d order=%v",
			lockIdx, deleteIdx, q.callOrder)
	}
}

// --- link/begin + link/callback tests --------------------------------------
//
// These reuse the federation harness from handle_federation_test.go: same OP
// mock, same fakeFedQueries (already widened for InsertAccountIdentity), same
// real Federator. The harness mounts both /me/identities/link/* routes
// alongside the public /auth/federation/* routes so the no-follow client can
// step through begin → authorize → callback.

func mountLinkRoutes(h *fedTestHarness) *httptest.Server {
	h.t.Helper()
	r := chi.NewRouter()
	r.Get("/api/prohibitorum/auth/federation/{slug}/login", h.s.handleFederationLoginHTTP)
	r.Get("/api/prohibitorum/auth/federation/{slug}/callback", h.s.handleFederationCallbackHTTP)
	r.Get("/api/prohibitorum/me/identities/link/{slug}/begin", withSessionMW(h.s, h.linkAccountID, h.linkToken, h.s.handleMeIdentitiesLinkBeginHTTP))
	r.Get("/api/prohibitorum/me/identities/link/{slug}/callback", withSessionMW(h.s, h.linkAccountID, h.linkToken, h.s.handleMeIdentitiesLinkCallbackHTTP))
	srv := httptest.NewServer(r)
	h.t.Cleanup(srv.Close)
	// Rewire the federator's publicOrigin to the new server URL so the
	// link-callback redirect_uri targets this httptest origin.
	h.s.federator = fedoidc.NewFederator(h.q, h.s.kvStore, h.s.Audit, fedTestFedCfg(), map[int][]byte{1: fedTestDEK}, srv.URL)
	return srv
}

func fedTestFedCfg() configx.FederationConfig {
	return configx.FederationConfig{
		StateTTL:      5 * time.Minute,
		DefaultScopes: []string{"openid", "profile", "email"},
	}
}

// withSessionMW wraps a handler so the request carries a *authn.Session for
// accountID under sessionToken. The real LoadSession middleware isn't wired
// here; this mirrors what it would do.
func withSessionMW(s *Server, accountID int32, sessionToken string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, _, err := s.sessionStore.Load(r.Context(), accountID, sessionToken, "127.0.0.1", "ua/test")
		if err != nil {
			http.Error(w, "session load: "+err.Error(), http.StatusUnauthorized)
			return
		}
		acct := &db.Account{ID: accountID, Username: "alice"}
		sess := &authn.Session{Account: acct, Token: sessionToken, Data: data}
		r = r.WithContext(authn.WithSession(r.Context(), sess))
		next(w, r)
	}
}

// extendHarness adds session + token fields onto fedTestHarness so the link
// tests can drive a logged-in flow. Setting these is a side-effect of
// newLinkTestHarness.
func newLinkTestHarness(t *testing.T) *fedTestHarness {
	h := newFederationTestServer(t)
	// Plant a session for accountID=900.
	const accountID int32 = 900
	token, _, err := h.s.sessionStore.Issue(context.Background(), accountID, "127.0.0.1", "ua/test", []string{"hwk"})
	if err != nil {
		t.Fatalf("sessionStore.Issue: %v", err)
	}
	h.linkAccountID = accountID
	h.linkToken = token
	// Replace the original test server with one that also mounts the link
	// routes. The fed handlers still work — they hang off the same Server.
	h.srvTS.Close() // ditch the federation-only server.
	h.srvTS = mountLinkRoutes(h)
	h.origin = h.srvTS.URL
	return h
}

// grantSudoOnSession stamps SudoUntil so the link/begin handler accepts the
// session. Mirrors grantFreshSudo but reads the token from the harness.
func (h *fedTestHarness) grantSudoOnSession(t *testing.T) {
	t.Helper()
	current, _, err := h.s.sessionStore.Load(context.Background(), h.linkAccountID, h.linkToken, "127.0.0.1", "ua/test")
	if err != nil {
		t.Fatalf("session load: %v", err)
	}
	current.SudoUntil = time.Now().Add(5 * time.Minute)
	if err := h.s.sessionStore.Save(context.Background(), h.linkAccountID, h.linkToken, current); err != nil {
		t.Fatalf("session save: %v", err)
	}
}

// driveLinkBegin hits /me/identities/link/{slug}/begin. Returns (Location,
// status).
func (h *fedTestHarness) driveLinkBegin(t *testing.T, slug, returnTo string) (string, *http.Response) {
	t.Helper()
	u := h.srvTS.URL + "/api/prohibitorum/me/identities/link/" + slug + "/begin"
	if returnTo != "" {
		u += "?return_to=" + url.QueryEscape(returnTo)
	}
	resp, err := noFollow().Get(u)
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusFound {
		return "", resp
	}
	return resp.Header.Get("Location"), resp
}

// hitLinkCallback hits /me/identities/link/{slug}/callback.
func (h *fedTestHarness) hitLinkCallback(t *testing.T, slug string, q url.Values) *http.Response {
	t.Helper()
	u := h.srvTS.URL + "/api/prohibitorum/me/identities/link/" + slug + "/callback?" + q.Encode()
	resp, err := noFollow().Get(u)
	if err != nil {
		t.Fatalf("GET /link/callback: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestMeIdentities_LinkBegin_SudoGated(t *testing.T) {
	h := newLinkTestHarness(t)
	// No grantSudoOnSession — the gate must fire.

	_, resp := h.driveLinkBegin(t, "mockop", "/me")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", resp.StatusCode)
	}
	body := decodeErrBody(t, resp)
	if body.Code != "sudo_required" {
		t.Errorf("code: want sudo_required, got %q", body.Code)
	}
}

func TestMeIdentities_LinkBegin_HappyPath(t *testing.T) {
	h := newLinkTestHarness(t)
	h.grantSudoOnSession(t)

	loc, resp := h.driveLinkBegin(t, "mockop", "/me")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status: want 302, got %d", resp.StatusCode)
	}
	if !strings.HasPrefix(loc, h.opTS.URL+"/authorize") {
		t.Errorf("Location prefix: want %q, got %q", h.opTS.URL+"/authorize", loc)
	}
	// State should be stashed under LinkKey (not LoginKey) with
	// LinkingAccountID set to our session account.
	u, _ := url.Parse(loc)
	state := u.Query().Get("state")
	if state == "" {
		t.Fatal("state missing from authorize URL")
	}
	if _, err := h.s.kvStore.Get(context.Background(), fedoidc.LoginKey(state)); err == nil {
		t.Errorf("link-flow state must NOT be under LoginKey")
	}
	blob, err := h.s.kvStore.Get(context.Background(), fedoidc.LinkKey(state))
	if err != nil {
		t.Fatalf("state not under LinkKey: %v", err)
	}
	fs, err := fedoidc.DecodeFedState(blob)
	if err != nil {
		t.Fatalf("DecodeFedState: %v", err)
	}
	if fs.LinkingAccountID == nil || *fs.LinkingAccountID != h.linkAccountID {
		t.Errorf("LinkingAccountID: want %d, got %v", h.linkAccountID, fs.LinkingAccountID)
	}
}

func TestMeIdentities_LinkBegin_InvalidReturnTo(t *testing.T) {
	h := newLinkTestHarness(t)
	h.grantSudoOnSession(t)

	_, resp := h.driveLinkBegin(t, "mockop", "https://evil.example.com/")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", resp.StatusCode)
	}
	body := decodeErrBody(t, resp)
	if body.Code != "invalid_return_to" {
		t.Errorf("code: want invalid_return_to, got %q", body.Code)
	}
}

func TestMeIdentities_LinkCallback_HappyPath(t *testing.T) {
	h := newLinkTestHarness(t)
	h.grantSudoOnSession(t)
	// Pre-existing account a (linkAccountID=900) — federator will look up
	// its row by ID to log the link. Pre-seed accountByIDResults.
	h.q.accountByIDResults[h.linkAccountID] = db.Account{ID: h.linkAccountID, Username: "alice"}
	// The harness's session creation has already inserted one db.Session
	// row. Snapshot the count so the post-callback assertion only counts
	// rows added by the link flow itself.
	sessionsBefore := len(h.q.sessions)

	loc, resp := h.driveLinkBegin(t, "mockop", "/me/identities")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("begin: want 302, got %d", resp.StatusCode)
	}
	code, state, iss := driveAuthorize(t, loc)

	q := url.Values{}
	q.Set("code", code)
	q.Set("state", state)
	q.Set("iss", iss)
	resp = h.hitLinkCallback(t, "mockop", q)

	if resp.StatusCode != http.StatusFound {
		body, _ := readAll(resp.Body)
		t.Fatalf("callback: want 302, got %d (body=%s)", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Location"); got != "/me/identities" {
		t.Errorf("Location: want /me/identities, got %q", got)
	}
	// NO session cookie — link callback must not issue a new session.
	for _, c := range resp.Cookies() {
		if c.Name == sessstore.SessionCookieName {
			t.Errorf("link callback must not set session cookie; got %s=%s", c.Name, c.Value)
		}
	}
	// One identity insert for our session account.
	if len(h.q.insertIdentitys) != 1 {
		t.Fatalf("identities inserted: want 1, got %d", len(h.q.insertIdentitys))
	}
	if h.q.insertIdentitys[0].AccountID != h.linkAccountID {
		t.Errorf("inserted identity account: want %d, got %d", h.linkAccountID, h.q.insertIdentitys[0].AccountID)
	}
	// Exactly ONE EventLink audit row (emitted by the federator, not the
	// handler). No duplicates.
	linkRows := 0
	for _, ev := range h.q.events {
		if ev.Factor == string(audit.FactorFederationOIDC) && ev.Event == audit.EventLink {
			linkRows++
		}
	}
	if linkRows != 1 {
		t.Errorf("EventLink audit rows: want 1, got %d (events=%+v)", linkRows, h.q.events)
	}
	// No NEW session insert — link flow never calls sessionStore.Issue.
	if len(h.q.sessions) != sessionsBefore {
		t.Errorf("sessions inserted by link flow: want 0, got %d", len(h.q.sessions)-sessionsBefore)
	}
}

func TestMeIdentities_LinkCallback_SessionSwap(t *testing.T) {
	h := newLinkTestHarness(t)
	h.grantSudoOnSession(t)

	// Start link as accountID=900.
	loc, resp := h.driveLinkBegin(t, "mockop", "/me/identities")
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("begin: want 302, got %d", resp.StatusCode)
	}
	code, state, iss := driveAuthorize(t, loc)

	// Swap the session: tear down the existing routes and remount with a
	// DIFFERENT account on the session, then hit /callback. The federator's
	// FedState carries linking_account_id=900; current session carries 901
	// → state.LinkingAccountID != currentAccountID, federator emits
	// session_swap audit row + ErrFederationStateInvalid.
	const otherAccountID int32 = 901
	otherToken, _, err := h.s.sessionStore.Issue(context.Background(), otherAccountID, "127.0.0.1", "ua/test", []string{"hwk"})
	if err != nil {
		t.Fatalf("session issue (other): %v", err)
	}
	h.srvTS.Close()
	h.linkAccountID = otherAccountID
	h.linkToken = otherToken
	h.srvTS = mountLinkRoutes(h)
	h.origin = h.srvTS.URL

	q := url.Values{}
	q.Set("code", code)
	q.Set("state", state)
	q.Set("iss", iss)
	resp = h.hitLinkCallback(t, "mockop", q)

	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := readAll(resp.Body)
		t.Fatalf("callback: want 401, got %d (body=%s)", resp.StatusCode, body)
	}
	body := decodeErrBody(t, resp)
	if body.Code != "federation_state_invalid" {
		t.Errorf("code: want federation_state_invalid, got %q", body.Code)
	}
	// Federator emits a fail audit row with reason=session_swap.
	found := false
	for _, ev := range h.q.events {
		if ev.Factor != string(audit.FactorFederationOIDC) || ev.Event != audit.EventFail {
			continue
		}
		var detail map[string]any
		_ = json.Unmarshal(ev.Detail, &detail)
		if detail["reason"] == "session_swap" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("audit: missing session_swap fail row; events=%+v", h.q.events)
	}
	// No identity must have been inserted.
	if len(h.q.insertIdentitys) != 0 {
		t.Errorf("identities inserted: want 0 on session swap, got %d", len(h.q.insertIdentitys))
	}
}

func TestMeIdentities_LinkCallback_MissingState(t *testing.T) {
	h := newLinkTestHarness(t)

	q := url.Values{}
	q.Set("state", "")
	q.Set("code", "abc")
	resp := h.hitLinkCallback(t, "mockop", q)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", resp.StatusCode)
	}
	body := decodeErrBody(t, resp)
	if body.Code != "federation_state_invalid" {
		t.Errorf("code: want federation_state_invalid, got %q", body.Code)
	}
}

func TestMeIdentities_LinkCallback_UpstreamError(t *testing.T) {
	h := newLinkTestHarness(t)

	q := url.Values{}
	q.Set("error", "access_denied")
	q.Set("error_description", "user denied consent")
	resp := h.hitLinkCallback(t, "mockop", q)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d", resp.StatusCode)
	}
	body := decodeErrBody(t, resp)
	if body.Code != "upstream_error" {
		t.Errorf("code: want upstream_error, got %q", body.Code)
	}
	// Audit row carries account_id (we have a session).
	found := false
	for _, ev := range h.q.events {
		if ev.Factor != string(audit.FactorFederationOIDC) || ev.Event != audit.EventFail {
			continue
		}
		var detail map[string]any
		_ = json.Unmarshal(ev.Detail, &detail)
		if detail["reason"] == "upstream_error" {
			if ev.AccountID == nil || *ev.AccountID != h.linkAccountID {
				t.Errorf("audit row account_id: want %d, got %v", h.linkAccountID, ev.AccountID)
			}
			found = true
			break
		}
	}
	if !found {
		t.Errorf("audit: missing upstream_error row; events=%+v", h.q.events)
	}
}

// --- helpers ---------------------------------------------------------------

func toInt64(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	}
	return -1
}
