// Package server — handle_sudo_test.go
//
// Unit-level tests for the v0.2 three-method sudo flow:
//   GET  /me/sudo/methods           — discovery
//   POST /me/sudo/begin             — method intent stash
//   POST /me/sudo/complete          — verify + stamp SudoUntil
//
// The webauthn path is exercised only at the routing/stash level. End-to-
// end webauthn ceremonies live in cmd/smoke (Task 8) because they need a
// concrete *db.Queries for ListCredentialsByAccount + the WebAuthn library
// state machine, neither of which the unit scaffolding here supplies.
//
// Tests share a fake DB shim (fakeSudoQueries) that satisfies db.Querier
// for the surfaces touched: webauthn-credential listing, password
// credentials, TOTP credentials, recovery codes, throttle, audit, and
// session inserts. All paths through password.Store / totp.Store flow
// through this fake.

package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/credential/password"
	"prohibitorum/pkg/credential/totp"
	"prohibitorum/pkg/db"
	fedoidc "prohibitorum/pkg/federation/oidc"
	"prohibitorum/pkg/kv"
	sessstore "prohibitorum/pkg/session"
)

// fakeSudoFederator is a seedable stand-in for *fedoidc.Federator used by the
// federation_oidc sudo branch tests. It records its call args and returns the
// seeded values.
type fakeSudoFederator struct {
	beginReq    *fedoidc.LoginRequest
	beginErr    error
	beginArgs   struct{ accountID int32; slug, returnTo string }
	beginCalled bool

	cbReturnTo string
	cbErr      error
	cbArgs     struct {
		state, code, iss, browserToken string
		accountID                      int32
	}
	cbCalled bool
}

func (f *fakeSudoFederator) SudoBegin(_ context.Context, accountID int32, idpSlug, returnTo string) (*fedoidc.LoginRequest, error) {
	f.beginCalled = true
	f.beginArgs.accountID = accountID
	f.beginArgs.slug = idpSlug
	f.beginArgs.returnTo = returnTo
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	return f.beginReq, nil
}

func (f *fakeSudoFederator) SudoCallback(_ context.Context, stateToken, code, issParam, browserToken string, currentAccountID int32) (string, error) {
	f.cbCalled = true
	f.cbArgs.state = stateToken
	f.cbArgs.code = code
	f.cbArgs.iss = issParam
	f.cbArgs.browserToken = browserToken
	f.cbArgs.accountID = currentAccountID
	if f.cbErr != nil {
		return "", f.cbErr
	}
	return f.cbReturnTo, nil
}

// failingSaveKV wraps a kv.Store but errors on SetEx, simulating a transient KV
// write failure during the sudo one-shot clear (audit SESS-1).
type failingSaveKV struct{ kv.Store }

func (failingSaveKV) SetEx(context.Context, string, string, time.Duration) error {
	return fmt.Errorf("kv unavailable")
}

// TestConsumeFreshSudoFailsClosedOnSaveError: when the cleared SudoUntil cannot
// be persisted, consumeFreshSudo must DENY (return false). Returning true would
// leave the future SudoUntil live in KV — and since every request re-reads it,
// the one-shot grant would silently become "every gated action for the rest of
// the TTL window" (audit SESS-1).
func TestConsumeFreshSudoFailsClosedOnSaveError(t *testing.T) {
	store := sessstore.NewSessionStore(failingSaveKV{kv.NewMemoryStore()}, nil, time.Hour)
	s := &Server{config: &configx.Config{SessionTTL: time.Hour}, sessionStore: store}
	sess := &authn.Session{
		Token:   "tok",
		Account: &db.Account{ID: 1},
		Data: &authn.SessionData{
			SessionID: "sid",
			ExpiresAt: time.Now().Add(time.Hour),
			SudoUntil: time.Now().Add(2 * time.Minute),
		},
	}
	if s.consumeFreshSudo(context.Background(), sess) {
		t.Fatal("consumeFreshSudo returned true despite Save failure; want false (fail closed)")
	}
}

// fakeSudoQueries satisfies db.Querier for every surface the sudo handlers
// (and the password.Store / totp.Store / session.SessionStore they reach
// through) touch.
type fakeSudoQueries struct {
	db.Querier

	webauthnRows []db.WebauthnCredential
	passwordRow  *db.PasswordCredential
	totpRow      *db.TotpCredential
	recoveryRows []db.RecoveryCode

	// linkedIdPs seeds ListLinkedEnabledIdPs and, when len > 0, CountUsableSignInFederation
	// returns a positive count so AvailableMethods surfaces federation_oidc.
	linkedIdPs []db.ListLinkedEnabledIdPsRow

	nextRecID int32
	throttle  map[string]db.AuthThrottle
	events    []db.InsertCredentialEventParams
	sessions  []db.Session
}

func newFakeSudoQueries() *fakeSudoQueries {
	return &fakeSudoQueries{
		throttle:  map[string]db.AuthThrottle{},
		nextRecID: 1,
	}
}

func (f *fakeSudoQueries) ListCredentialsByAccount(_ context.Context, accountID int32) ([]db.WebauthnCredential, error) {
	var out []db.WebauthnCredential
	for _, c := range f.webauthnRows {
		if c.AccountID == accountID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeSudoQueries) GetPasswordCredential(_ context.Context, accountID int32) (db.PasswordCredential, error) {
	if f.passwordRow == nil || f.passwordRow.AccountID != accountID {
		return db.PasswordCredential{}, pgx.ErrNoRows
	}
	return *f.passwordRow, nil
}

func (f *fakeSudoQueries) UpsertPasswordCredential(_ context.Context, arg db.UpsertPasswordCredentialParams) error {
	row := db.PasswordCredential{AccountID: arg.AccountID, Hash: arg.Hash}
	f.passwordRow = &row
	return nil
}

func (f *fakeSudoQueries) UpdatePasswordHashOnly(_ context.Context, arg db.UpdatePasswordHashOnlyParams) error {
	if f.passwordRow != nil && f.passwordRow.AccountID == arg.AccountID {
		f.passwordRow.Hash = arg.Hash
	}
	return nil
}

func (f *fakeSudoQueries) DeletePasswordCredential(_ context.Context, accountID int32) error {
	if f.passwordRow != nil && f.passwordRow.AccountID == accountID {
		f.passwordRow = nil
	}
	return nil
}

func (f *fakeSudoQueries) GetTOTPCredential(_ context.Context, accountID int32) (db.TotpCredential, error) {
	if f.totpRow == nil || f.totpRow.AccountID != accountID {
		return db.TotpCredential{}, pgx.ErrNoRows
	}
	return *f.totpRow, nil
}

func (f *fakeSudoQueries) InsertTOTPCredential(_ context.Context, arg db.InsertTOTPCredentialParams) (db.TotpCredential, error) {
	row := db.TotpCredential{
		AccountID:   arg.AccountID,
		SecretEnc:   arg.SecretEnc,
		SecretNonce: arg.SecretNonce,
		KeyVersion:  arg.KeyVersion,
		Period:      arg.Period,
		Digits:      arg.Digits,
		Algorithm:   arg.Algorithm,
	}
	f.totpRow = &row
	return row, nil
}

func (f *fakeSudoQueries) DeleteTOTPCredential(_ context.Context, accountID int32) error {
	if f.totpRow != nil && f.totpRow.AccountID == accountID {
		f.totpRow = nil
	}
	return nil
}

func (f *fakeSudoQueries) ConfirmTOTPCredential(_ context.Context, accountID int32) error {
	if f.totpRow != nil && f.totpRow.AccountID == accountID {
		f.totpRow.ConfirmedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	}
	return nil
}

func (f *fakeSudoQueries) UpdateTOTPLastStep(_ context.Context, arg db.UpdateTOTPLastStepParams) (int64, error) {
	if f.totpRow != nil && f.totpRow.AccountID == arg.AccountID && arg.LastStep > f.totpRow.LastStep {
		f.totpRow.LastStep = arg.LastStep
		return arg.LastStep, nil
	}
	return 0, pgx.ErrNoRows
}

func (f *fakeSudoQueries) ListRecoveryCodesByAccount(_ context.Context, accountID int32) ([]db.RecoveryCode, error) {
	var out []db.RecoveryCode
	for _, r := range f.recoveryRows {
		if r.AccountID == accountID && !r.UsedAt.Valid {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeSudoQueries) InsertRecoveryCode(_ context.Context, arg db.InsertRecoveryCodeParams) (db.RecoveryCode, error) {
	row := db.RecoveryCode{
		ID:        f.nextRecID,
		AccountID: arg.AccountID,
		Hash:      arg.Hash,
	}
	f.nextRecID++
	f.recoveryRows = append(f.recoveryRows, row)
	return row, nil
}

func (f *fakeSudoQueries) ConsumeRecoveryCode(_ context.Context, arg db.ConsumeRecoveryCodeParams) (db.RecoveryCode, error) {
	for i := range f.recoveryRows {
		if f.recoveryRows[i].ID == arg.ID && !f.recoveryRows[i].UsedAt.Valid {
			f.recoveryRows[i].UsedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
			f.recoveryRows[i].UsedSessionID = arg.UsedSessionID
			f.recoveryRows[i].UsedIp = arg.UsedIp
			return f.recoveryRows[i], nil
		}
	}
	return db.RecoveryCode{}, pgx.ErrNoRows
}

// ListAccountIdentitiesByAccount returns no rows in the sudo tests — none of
// these scenarios seed federation identities, so the sudo handler sees the
// account as webauthn/password+TOTP only. Required by authn.FlowQueries (v0.3).
func (f *fakeSudoQueries) ListAccountIdentitiesByAccount(_ context.Context, _ int32) ([]db.ListAccountIdentitiesByAccountRow, error) {
	return nil, nil
}

// CountUsableSignInFederation returns the count of seeded linkedIdPs so
// AvailableMethods surfaces federation_oidc when linkedIdPs is non-empty.
func (f *fakeSudoQueries) CountUsableSignInFederation(_ context.Context, _ int32) (int64, error) {
	return int64(len(f.linkedIdPs)), nil
}

// ListLinkedEnabledIdPs returns the seeded linkedIdPs slice.
func (f *fakeSudoQueries) ListLinkedEnabledIdPs(_ context.Context, _ int32) ([]db.ListLinkedEnabledIdPsRow, error) {
	if f.linkedIdPs == nil {
		return []db.ListLinkedEnabledIdPsRow{}, nil
	}
	return f.linkedIdPs, nil
}

func (f *fakeSudoQueries) DeleteAllRecoveryCodesByAccount(_ context.Context, accountID int32) error {
	keep := f.recoveryRows[:0]
	for _, r := range f.recoveryRows {
		if r.AccountID != accountID {
			keep = append(keep, r)
		}
	}
	f.recoveryRows = keep
	return nil
}

func (f *fakeSudoQueries) throttleKey(accountID int32, factor string) string {
	return fmt.Sprintf("%d:%s", accountID, factor)
}

func (f *fakeSudoQueries) GetAuthThrottle(_ context.Context, arg db.GetAuthThrottleParams) (db.AuthThrottle, error) {
	row, ok := f.throttle[f.throttleKey(arg.AccountID, arg.Factor)]
	if !ok {
		return db.AuthThrottle{}, pgx.ErrNoRows
	}
	return row, nil
}

func (f *fakeSudoQueries) BumpAuthThrottle(_ context.Context, arg db.BumpAuthThrottleParams) (db.BumpAuthThrottleRow, error) {
	key := f.throttleKey(arg.AccountID, arg.Factor)
	now := time.Now()
	cur, ok := f.throttle[key]
	if !ok {
		cur = db.AuthThrottle{
			AccountID:   arg.AccountID,
			Factor:      arg.Factor,
			WindowStart: pgtype.Timestamptz{Time: now, Valid: true},
		}
	}
	cur.FailedAttempts++
	idx := int(cur.FailedAttempts) - 1
	if idx >= len(arg.ScheduleMicros) {
		idx = len(arg.ScheduleMicros) - 1
	}
	if idx < 0 || arg.ScheduleMicros[idx] <= 0 {
		cur.LockedUntil = pgtype.Timestamptz{Valid: false}
	} else {
		d := time.Duration(arg.ScheduleMicros[idx]) * time.Microsecond
		cur.LockedUntil = pgtype.Timestamptz{Time: now.Add(d), Valid: true}
	}
	f.throttle[key] = cur
	return db.BumpAuthThrottleRow{FailedAttempts: cur.FailedAttempts, LockedUntil: cur.LockedUntil}, nil
}

func (f *fakeSudoQueries) ResetAuthThrottle(_ context.Context, arg db.ResetAuthThrottleParams) error {
	delete(f.throttle, f.throttleKey(arg.AccountID, arg.Factor))
	return nil
}

func (f *fakeSudoQueries) InsertCredentialEvent(_ context.Context, arg db.InsertCredentialEventParams) error {
	f.events = append(f.events, arg)
	return nil
}

func (f *fakeSudoQueries) InsertSession(_ context.Context, arg db.InsertSessionParams) (db.Session, error) {
	row := db.Session{
		ID:        arg.ID,
		AccountID: arg.AccountID,
		AuthTime:  arg.AuthTime,
		Amr:       arg.Amr,
	}
	f.sessions = append(f.sessions, row)
	return row, nil
}

func (f *fakeSudoQueries) RevokeSession(_ context.Context, _ string) error { return nil }

func (f *fakeSudoQueries) RevokeAllSessionsByAccount(_ context.Context, _ int32) error { return nil }

// --- Server scaffolding ----------------------------------------------------

// newSudoTestServer builds a Server with enough wiring to exercise the v0.2
// three-method sudo flow without spinning up the production constructor.
// queries (the concrete *db.Queries) is left nil; tests that need DB reads
// go through s.sudoFlowOverride (the narrow methods-interface) and through
// password.Store / totp.Store / sessstore.SessionStore — all of which take
// fake interfaces.
func newSudoTestServer(t *testing.T) (*Server, *fakeSudoQueries, []byte) {
	t.Helper()
	f := newFakeSudoQueries()

	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		t.Fatal(err)
	}
	deks := map[int][]byte{1: dek}

	totpCfg := configx.TOTPConfig{
		DefaultPeriod:     30,
		DefaultDigits:     6,
		DefaultAlgorithm:  "SHA1",
		DriftSteps:        1,
		RecoveryCodeCount: 10,
		Issuer:            "Prohibitorum",
	}
	throttleSchedule := []time.Duration{0, 0, time.Second, 2 * time.Second}
	authCfg := configx.AuthConfig{
		ThrottleSchedule:  throttleSchedule,
		PartialSessionTTL: 5 * time.Minute,
		SudoTTL:           5 * time.Minute,
	}
	// Modest argon2id params — these tests hash on every password verify, so
	// the production memory/iter budget would blow out the test runtime.
	pwParams := configx.PasswordHashParams{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1}
	cfg := &configx.Config{
		SessionTTL:         time.Hour,
		TOTP:               totpCfg,
		Auth:               authCfg,
		PasswordHashParams: pwParams,
	}

	auditWriter := audit.NewWriter(f)
	throttle := authn.NewThrottle(f, throttleSchedule, auditWriter)
	pwStore := password.NewStore(f, pwParams, throttle, auditWriter)
	totpStore := totp.NewStore(f, &totpTestTxRunner{q: f}, deks, totpCfg, throttle, auditWriter)

	kvStore := kv.NewMemoryStore()
	sessionStore := sessstore.NewSessionStore(kvStore, f, cfg.SessionTTL)

	s := &Server{
		config:           cfg,
		kvStore:          kvStore,
		sessionStore:     sessionStore,
		rateLimiter:      authn.NewRateLimiter(),
		passwordStore:    pwStore,
		totpStore:        totpStore,
		throttle:         throttle,
		Audit:            auditWriter,
		sudoFlowOverride: f,
	}
	return s, f, dek
}

// issueSudoTestSession installs a real session in KV via SessionStore.Issue
// so /me/sudo/* handlers find a *authn.Session on the request context and
// stampSudoUntil can Load+Save it back.
func issueSudoTestSession(t *testing.T, s *Server, accountID int32) (token string, sess *authn.Session) {
	t.Helper()
	token, data, err := s.sessionStore.Issue(context.Background(), accountID, "127.0.0.1", "ua/test", []string{"hwk"}, nil)
	if err != nil {
		t.Fatalf("sessionStore.Issue: %v", err)
	}
	acct := &db.Account{ID: accountID, Username: "alice"}
	return token, &authn.Session{Account: acct, Token: token, Data: data}
}

// sudoReq constructs an *http.Request with the session pre-attached on the
// context, since the LoadSession middleware isn't wired in unit tests.
func sudoReq(t *testing.T, sess *authn.Session, method, path, body string) *http.Request {
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

func seedConfirmedTOTPSudo(t *testing.T, s *Server, f *fakeSudoQueries, dek []byte, accountID int32) []string {
	t.Helper()
	ctx := context.Background()
	if _, err := s.totpStore.Begin(ctx, accountID, "alice"); err != nil {
		t.Fatalf("totpStore.Begin: %v", err)
	}
	row := *f.totpRow
	code := totp.ComputeCodeForTesting(decryptTOTPSecret(t, dek, row, accountID), time.Now().Unix(), int(row.Digits))
	codes, err := s.totpStore.Verify(ctx, accountID, code)
	if err != nil {
		t.Fatalf("totpStore.Verify (confirm): %v", err)
	}
	if len(codes) != 10 {
		t.Fatalf("recovery codes: want 10, got %d", len(codes))
	}
	return codes
}

func seedPassword(t *testing.T, s *Server, accountID int32, plain string) {
	t.Helper()
	if err := s.passwordStore.Set(context.Background(), accountID, plain); err != nil {
		t.Fatalf("passwordStore.Set: %v", err)
	}
}

func decodeJSON(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("decode body: %v (raw=%s)", err, string(body))
	}
	return m
}

func methodsFromBody(t *testing.T, body []byte) []string {
	t.Helper()
	m := decodeJSON(t, body)
	raw, ok := m["methods"].([]any)
	if !ok {
		t.Fatalf("methods key missing or wrong type in %s", string(body))
	}
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		out = append(out, x.(string))
	}
	return out
}

// --- /me/sudo/methods tests ------------------------------------------------
//
// Note: there is no direct nil-session test here — the handlers trust the
// sessionReq middleware (matching the convention used by handleGetMe et al.)
// to guarantee a non-nil session before dispatch.

func TestSudoMethods_OnlyWebauthn(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	const accountID int32 = 42
	f.webauthnRows = []db.WebauthnCredential{{ID: 1, AccountID: accountID, Nickname: pgtype.Text{String: "yk1", Valid: true}}}
	_, sess := issueSudoTestSession(t, s, accountID)

	r := sudoReq(t, sess, http.MethodGet, "/api/prohibitorum/me/sudo/methods", "")
	w := httptest.NewRecorder()
	s.handleSudoMethodsHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	got := methodsFromBody(t, w.Body.Bytes())
	want := []string{"webauthn"}
	if !equalStrings(got, want) {
		t.Errorf("methods: want %v, got %v", want, got)
	}
}

func TestSudoMethods_OnlyPasswordTOTP(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	const accountID int32 = 42
	seedPassword(t, s, accountID, "correct-horse")
	// Seed confirmed TOTP, then wipe the recovery codes so the slice doesn't
	// list "recovery_code".
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	f.recoveryRows = nil
	_, sess := issueSudoTestSession(t, s, accountID)

	r := sudoReq(t, sess, http.MethodGet, "/api/prohibitorum/me/sudo/methods", "")
	w := httptest.NewRecorder()
	s.handleSudoMethodsHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	got := methodsFromBody(t, w.Body.Bytes())
	want := []string{"password_totp"}
	if !equalStrings(got, want) {
		t.Errorf("methods: want %v, got %v", want, got)
	}
}

// TestSudoMethods_RecoveryCodesNotSurfaced: post 2026-05-28 hardening,
// recovery codes must NOT appear in the sudo methods list even when the
// account has them — the recovery code path runs through the ceremony
// endpoints, not sudo. See pkg/server/handle_sudo.go package doc.
func TestSudoMethods_RecoveryCodesNotSurfaced(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	const accountID int32 = 42
	seedPassword(t, s, accountID, "correct-horse")
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	_, sess := issueSudoTestSession(t, s, accountID)

	r := sudoReq(t, sess, http.MethodGet, "/api/prohibitorum/me/sudo/methods", "")
	w := httptest.NewRecorder()
	s.handleSudoMethodsHTTP(w, r)
	got := methodsFromBody(t, w.Body.Bytes())
	if slices.Contains(got, "recovery_code") {
		t.Errorf("recovery_code must not be a sudo method, got %v", got)
	}
}

func TestSudoMethods_WebAuthnAndPasswordTOTP(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	const accountID int32 = 42
	f.webauthnRows = []db.WebauthnCredential{{ID: 1, AccountID: accountID, Nickname: pgtype.Text{String: "yk1", Valid: true}}}
	seedPassword(t, s, accountID, "correct-horse")
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	_, sess := issueSudoTestSession(t, s, accountID)

	r := sudoReq(t, sess, http.MethodGet, "/api/prohibitorum/me/sudo/methods", "")
	w := httptest.NewRecorder()
	s.handleSudoMethodsHTTP(w, r)
	got := methodsFromBody(t, w.Body.Bytes())
	want := []string{"webauthn", "password_totp"}
	if !equalStrings(got, want) {
		t.Errorf("methods order: want %v, got %v", want, got)
	}
}

func TestSudoMethods_None(t *testing.T) {
	s, _, _ := newSudoTestServer(t)
	const accountID int32 = 42
	_, sess := issueSudoTestSession(t, s, accountID)
	r := sudoReq(t, sess, http.MethodGet, "/api/prohibitorum/me/sudo/methods", "")
	w := httptest.NewRecorder()
	s.handleSudoMethodsHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", w.Code)
	}
	got := methodsFromBody(t, w.Body.Bytes())
	if len(got) != 0 {
		t.Errorf("methods: want empty, got %v", got)
	}
}

// --- /me/sudo/begin tests --------------------------------------------------

func TestSudoBegin_UnavailableMethodReturns400(t *testing.T) {
	s, _, _ := newSudoTestServer(t)
	const accountID int32 = 42
	// No password, no TOTP, no webauthn — every method should be rejected.
	_, sess := issueSudoTestSession(t, s, accountID)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/sudo/begin", `{"method":"password_totp"}`)
	w := httptest.NewRecorder()
	s.handleSudoBeginHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	if body["code"] != "sudo_method_unavailable" {
		t.Errorf("code: want sudo_method_unavailable, got %v", body["code"])
	}
}

func TestSudoBegin_PasswordTOTPReturns204AndStashesIntent(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	const accountID int32 = 42
	seedPassword(t, s, accountID, "correct-horse")
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	_, sess := issueSudoTestSession(t, s, accountID)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/sudo/begin", `{"method":"password_totp"}`)
	w := httptest.NewRecorder()
	s.handleSudoBeginHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status: want 204, got %d (body=%s)", w.Code, w.Body.String())
	}

	raw, err := s.kvStore.Get(context.Background(), sudoIntentKey(sess.Data.SessionID))
	if err != nil {
		t.Fatalf("intent not stashed: %v", err)
	}
	var intent sudoIntent
	if err := json.Unmarshal([]byte(raw), &intent); err != nil {
		t.Fatalf("decode intent: %v", err)
	}
	if intent.Method != "password_totp" {
		t.Errorf("intent.Method: want password_totp, got %s", intent.Method)
	}
}

// TestSudoBegin_RecoveryCodeRejected: post 2026-05-28 hardening, requesting
// the recovery_code method at /me/sudo/begin must be rejected even when the
// account has recovery codes.
func TestSudoBegin_RecoveryCodeRejected(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	const accountID int32 = 42
	seedPassword(t, s, accountID, "correct-horse")
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	_, sess := issueSudoTestSession(t, s, accountID)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/sudo/begin", `{"method":"recovery_code"}`)
	w := httptest.NewRecorder()
	s.handleSudoBeginHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400 (sudo_method_unavailable), got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	if body["code"] != "sudo_method_unavailable" {
		t.Errorf("code: want sudo_method_unavailable, got %v", body["code"])
	}
}

// --- /me/sudo/complete tests -----------------------------------------------

func stashIntent(t *testing.T, s *Server, sessionID, method string) {
	t.Helper()
	payload, _ := json.Marshal(sudoIntent{Method: method, IssuedAt: time.Now().UTC()})
	if err := s.kvStore.SetEx(context.Background(), sudoIntentKey(sessionID), string(payload), 5*time.Minute); err != nil {
		t.Fatal(err)
	}
}

func TestSudoComplete_PasswordTOTPSuccess(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	const accountID int32 = 42
	seedPassword(t, s, accountID, "correct-horse")
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	_, sess := issueSudoTestSession(t, s, accountID)
	stashIntent(t, s, sess.Data.SessionID, "password_totp")

	// Step forward past the seed Verify's consumed step.
	at := time.Now().Add(31 * time.Second)
	code := totp.ComputeCodeForTesting(decryptTOTPSecret(t, dek, *f.totpRow, accountID), at.Unix(), 6)

	body := fmt.Sprintf(`{"current_password":"correct-horse","totp_code":%q}`, code)
	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/sudo/complete", body)
	w := httptest.NewRecorder()
	s.handleSudoCompleteHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status: want 204, got %d (body=%s)", w.Code, w.Body.String())
	}

	// Session should now carry SudoUntil > now.
	current, _, err := s.sessionStore.Load(context.Background(), accountID, sess.Token, "127.0.0.1", "ua/test")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !current.HasFreshSudo() {
		t.Errorf("SudoUntil not stamped: %v (now=%v)", current.SudoUntil, time.Now())
	}

	// Audit event should be present with method=password_totp.
	found := false
	for _, ev := range f.events {
		if ev.Factor == "session" && ev.Event == "sudo_granted" {
			var detail map[string]any
			_ = json.Unmarshal(ev.Detail, &detail)
			if detail["method"] == "password_totp" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("audit: missing sudo_granted event with method=password_totp; events=%+v", f.events)
	}

	// Intent should have been cleared.
	if _, err := s.kvStore.Get(context.Background(), sudoIntentKey(sess.Data.SessionID)); err == nil {
		t.Error("intent should be cleared on success")
	}
}

func TestSudoComplete_PasswordTOTPWrongPassword(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	const accountID int32 = 42
	seedPassword(t, s, accountID, "correct-horse")
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	_, sess := issueSudoTestSession(t, s, accountID)
	stashIntent(t, s, sess.Data.SessionID, "password_totp")

	at := time.Now().Add(31 * time.Second)
	code := totp.ComputeCodeForTesting(decryptTOTPSecret(t, dek, *f.totpRow, accountID), at.Unix(), 6)

	body := fmt.Sprintf(`{"current_password":"wrong","totp_code":%q}`, code)
	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/sudo/complete", body)
	w := httptest.NewRecorder()
	s.handleSudoCompleteHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	// SudoUntil must NOT be stamped.
	current, _, err := s.sessionStore.Load(context.Background(), accountID, sess.Token, "127.0.0.1", "ua/test")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if current.HasFreshSudo() {
		t.Error("SudoUntil should not be stamped on failure")
	}
	// TOTP throttle must be clean — password failure short-circuits TOTP.
	if _, ok := f.throttle["42:totp"]; ok {
		t.Error("totp throttle should not be touched when password fails")
	}
}

func TestSudoComplete_PasswordTOTPWrongCode(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	const accountID int32 = 42
	seedPassword(t, s, accountID, "correct-horse")
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	_, sess := issueSudoTestSession(t, s, accountID)
	stashIntent(t, s, sess.Data.SessionID, "password_totp")

	body := `{"current_password":"correct-horse","totp_code":"000000"}`
	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/sudo/complete", body)
	w := httptest.NewRecorder()
	s.handleSudoCompleteHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	current, _, err := s.sessionStore.Load(context.Background(), accountID, sess.Token, "127.0.0.1", "ua/test")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if current.HasFreshSudo() {
		t.Error("SudoUntil should not be stamped on TOTP failure")
	}
}

// TestSudoComplete_RecoveryCodeRejected guards against any future drift
// that re-adds recovery_code as a complete-time dispatch case. Even if the
// intent KV is hand-stashed with method=recovery_code, /complete must
// reject the dispatch (the switch falls through to sudo_method_unavailable
// since the case was removed).
func TestSudoComplete_RecoveryCodeRejected(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	const accountID int32 = 42
	codes := seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	_, sess := issueSudoTestSession(t, s, accountID)
	stashIntent(t, s, sess.Data.SessionID, "recovery_code")

	body := fmt.Sprintf(`{"recovery_code":%q}`, codes[0])
	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/sudo/complete", body)
	w := httptest.NewRecorder()
	s.handleSudoCompleteHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400 (sudo_method_unavailable), got %d (body=%s)", w.Code, w.Body.String())
	}
	bj := decodeJSON(t, w.Body.Bytes())
	if bj["code"] != "sudo_method_unavailable" {
		t.Errorf("code: want sudo_method_unavailable, got %v", bj["code"])
	}
	// Recovery code MUST NOT have been consumed.
	remaining, _ := f.ListRecoveryCodesByAccount(context.Background(), accountID)
	if len(remaining) != 10 {
		t.Errorf("recovery codes must not be consumed by rejected sudo dispatch: have %d (want 10)", len(remaining))
	}
}

func TestSudoComplete_MissingIntent(t *testing.T) {
	s, _, _ := newSudoTestServer(t)
	const accountID int32 = 42
	_, sess := issueSudoTestSession(t, s, accountID)
	// No stashIntent call.

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/sudo/complete", `{"recovery_code":"x"}`)
	w := httptest.NewRecorder()
	s.handleSudoCompleteHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400 (ceremony_expired), got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	if body["code"] != "ceremony_expired" {
		t.Errorf("code: want ceremony_expired, got %v", body["code"])
	}
}

// --- /me/sudo/methods federation tests --------------------------------------

// federationProvidersFromBody decodes the federationProviders key from the
// /me/sudo/methods JSON response.
func federationProvidersFromBody(t *testing.T, body []byte) []map[string]any {
	t.Helper()
	m := decodeJSON(t, body)
	raw, ok := m["federationProviders"]
	if !ok {
		t.Fatalf("federationProviders key missing in %s", string(body))
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("federationProviders is not an array in %s", string(body))
	}
	out := make([]map[string]any, 0, len(arr))
	for _, x := range arr {
		entry, ok := x.(map[string]any)
		if !ok {
			t.Fatalf("federationProviders entry is not an object: %v", x)
		}
		out = append(out, entry)
	}
	return out
}

// TestSudoMethods_FederationOIDCAndProviders: when the account has a linked
// enabled upstream IdP, /me/sudo/methods must include "federation_oidc" in
// methods and return the provider slug+displayName in federationProviders.
func TestSudoMethods_FederationOIDCAndProviders(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	const accountID int32 = 42
	// Seed a WebAuthn credential so we confirm other methods still appear.
	f.webauthnRows = []db.WebauthnCredential{{ID: 1, AccountID: accountID, Nickname: pgtype.Text{String: "yk1", Valid: true}}}
	// Seed a linked enabled upstream IdP.
	f.linkedIdPs = []db.ListLinkedEnabledIdPsRow{
		{Slug: "google", DisplayName: "Google"},
	}
	_, sess := issueSudoTestSession(t, s, accountID)

	r := sudoReq(t, sess, http.MethodGet, "/api/prohibitorum/me/sudo/methods", "")
	w := httptest.NewRecorder()
	s.handleSudoMethodsHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}

	got := methodsFromBody(t, w.Body.Bytes())
	if !slices.Contains(got, "federation_oidc") {
		t.Errorf("methods: want federation_oidc present, got %v", got)
	}
	if !slices.Contains(got, "webauthn") {
		t.Errorf("methods: want webauthn present, got %v", got)
	}

	providers := federationProvidersFromBody(t, w.Body.Bytes())
	if len(providers) != 1 {
		t.Fatalf("federationProviders: want 1, got %d (%v)", len(providers), providers)
	}
	if providers[0]["slug"] != "google" {
		t.Errorf("federationProviders[0].slug: want google, got %v", providers[0]["slug"])
	}
	if providers[0]["displayName"] != "Google" {
		t.Errorf("federationProviders[0].displayName: want Google, got %v", providers[0]["displayName"])
	}
}

// TestSudoMethods_NoFederationEmptyProviders: when the account has no linked
// federation identity, federationProviders must be present and empty (not null).
func TestSudoMethods_NoFederationEmptyProviders(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	const accountID int32 = 42
	f.webauthnRows = []db.WebauthnCredential{{ID: 1, AccountID: accountID, Nickname: pgtype.Text{String: "yk1", Valid: true}}}
	// No linkedIdPs seeded.
	_, sess := issueSudoTestSession(t, s, accountID)

	r := sudoReq(t, sess, http.MethodGet, "/api/prohibitorum/me/sudo/methods", "")
	w := httptest.NewRecorder()
	s.handleSudoMethodsHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}

	got := methodsFromBody(t, w.Body.Bytes())
	if slices.Contains(got, "federation_oidc") {
		t.Errorf("methods: federation_oidc must be absent when no linked IdP, got %v", got)
	}

	providers := federationProvidersFromBody(t, w.Body.Bytes())
	if len(providers) != 0 {
		t.Errorf("federationProviders: want empty slice, got %v", providers)
	}
	// Verify the key is present and not null (JSON null would not decode to []any).
	rawBody := decodeJSON(t, w.Body.Bytes())
	if rawBody["federationProviders"] == nil {
		t.Error("federationProviders must not be null; want []")
	}
}

// --- helpers ---------------------------------------------------------------

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ----- federation_oidc sudo branch ---------------------------------------

// newFederationSudoServer wires a sudo test server whose account has a linked
// upstream IdP (so federation_oidc is an available method) and a fake
// federator injected via sudoFederatorOverride.
func newFederationSudoServer(t *testing.T) (*Server, *fakeSudoQueries, *fakeSudoFederator, int32) {
	t.Helper()
	s, f, _ := newSudoTestServer(t)
	// Seed one linked enabled IdP so AvailableMethods surfaces federation_oidc.
	f.linkedIdPs = []db.ListLinkedEnabledIdPsRow{{Slug: "okta", DisplayName: "Okta"}}
	fed := &fakeSudoFederator{}
	s.sudoFederatorOverride = fed
	return s, f, fed, 7
}

func TestSudoBegin_FederationReturnsRedirectAndCookie(t *testing.T) {
	s, _, fed, accountID := newFederationSudoServer(t)
	_, sess := issueSudoTestSession(t, s, accountID)
	fed.beginReq = &fedoidc.LoginRequest{
		AuthorizeURL:     "https://okta.example/authorize?x=1",
		StateKey:         "state-key",
		AntiForgeryToken: "anti-forgery-123",
	}

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/sudo/begin",
		`{"method":"federation_oidc","slug":"okta","returnTo":"/security"}`)
	rec := httptest.NewRecorder()
	s.handleSudoBeginHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec.Body.Bytes())
	if got := body["redirect"]; got != "https://okta.example/authorize?x=1" {
		t.Errorf("redirect = %v, want the AuthorizeURL", got)
	}
	if !fed.beginCalled {
		t.Fatal("SudoBegin was not called")
	}
	if fed.beginArgs.accountID != accountID || fed.beginArgs.slug != "okta" || fed.beginArgs.returnTo != "/security" {
		t.Errorf("SudoBegin args = %+v, want acct=%d slug=okta returnTo=/security", fed.beginArgs, accountID)
	}

	// fed-state cookie set.
	var found bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessstore.FedStateCookieName {
			found = true
			if c.Value != "anti-forgery-123" {
				t.Errorf("cookie value = %q, want anti-forgery-123", c.Value)
			}
		}
	}
	if !found {
		t.Errorf("no %s cookie set", sessstore.FedStateCookieName)
	}

	// sudo_intent must NOT be stashed for the federation method.
	if _, err := s.kvStore.Get(context.Background(), sudoIntentKey(sess.Data.SessionID)); err == nil {
		t.Error("sudo_intent was stashed for federation_oidc; it must not be (only /complete consumes it)")
	}
}

func TestSudoBegin_FederationBeginErrorWritesAuthErr(t *testing.T) {
	s, _, fed, accountID := newFederationSudoServer(t)
	_, sess := issueSudoTestSession(t, s, accountID)
	fed.beginErr = authn.ErrFederationStateInvalid()

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/sudo/begin",
		`{"method":"federation_oidc","slug":"okta","returnTo":"/security"}`)
	rec := httptest.NewRecorder()
	s.handleSudoBeginHTTP(rec, r)

	if rec.Code == http.StatusOK {
		t.Fatalf("status = 200, want an error status (body=%s)", rec.Body.String())
	}
}

func TestSudoFederationCallback_SuccessStampsAndRedirects(t *testing.T) {
	s, _, fed, accountID := newFederationSudoServer(t)
	token, sess := issueSudoTestSession(t, s, accountID)
	fed.cbReturnTo = "/security"

	r := sudoReq(t, sess, http.MethodGet,
		"/api/prohibitorum/me/sudo/federation/callback?state=st&code=cd&iss=https://okta", "")
	r.AddCookie(&http.Cookie{Name: sessstore.FedStateCookieName, Value: "browser-tok"})
	rec := httptest.NewRecorder()
	s.handleSudoFederationCallbackHTTP(rec, r)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (body=%s)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/security" {
		t.Errorf("Location = %q, want /security", loc)
	}
	if !fed.cbCalled {
		t.Fatal("SudoCallback not called")
	}
	if fed.cbArgs.state != "st" || fed.cbArgs.code != "cd" || fed.cbArgs.iss != "https://okta" ||
		fed.cbArgs.browserToken != "browser-tok" || fed.cbArgs.accountID != accountID {
		t.Errorf("SudoCallback args = %+v", fed.cbArgs)
	}

	current, _, err := s.sessionStore.Load(context.Background(), accountID, token, "127.0.0.1", "ua/test")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !current.SudoUntil.After(time.Now()) {
		t.Errorf("SudoUntil = %v, want in the future", current.SudoUntil)
	}
}

func TestSudoFederationCallback_FailureRedirectsAndDoesNotStamp(t *testing.T) {
	s, _, fed, accountID := newFederationSudoServer(t)
	token, sess := issueSudoTestSession(t, s, accountID)
	fed.cbErr = authn.ErrSudoReauthStale()

	r := sudoReq(t, sess, http.MethodGet,
		"/api/prohibitorum/me/sudo/federation/callback?state=st&code=cd&iss=https://okta", "")
	r.AddCookie(&http.Cookie{Name: sessstore.FedStateCookieName, Value: "browser-tok"})
	rec := httptest.NewRecorder()
	s.handleSudoFederationCallbackHTTP(rec, r)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (body=%s)", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/?sudo=failed" {
		t.Errorf("Location = %q, want /?sudo=failed", loc)
	}

	current, _, err := s.sessionStore.Load(context.Background(), accountID, token, "127.0.0.1", "ua/test")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if current.SudoUntil.After(time.Now()) {
		t.Errorf("SudoUntil = %v, want NOT stamped on failure", current.SudoUntil)
	}
}
