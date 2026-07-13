// Package server — handle_sudo_test.go
//
// Unit-level tests for the three-method sudo flow:
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
	"prohibitorum/pkg/clientip"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/credential/password"
	"prohibitorum/pkg/credential/totp"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	sessstore "prohibitorum/pkg/session"
)

// fakeSudoQueries satisfies db.Querier for every surface the sudo handlers
// (and the password.Store / totp.Store / session.SessionStore they reach
// through) touch.
type fakeSudoQueries struct {
	db.Querier

	webauthnRows []db.WebauthnCredential
	passwordRow  *db.PasswordCredential
	totpRow      *db.TotpCredential
	recoveryRows []db.RecoveryCode

	// fedCount is the value returned by CountUsableSignInFederation.
	// Defaults to 0 (no federation identities); set to a positive value in
	// tests that want AvailableMethods to include federation_oidc so we can
	// verify that availableSudoMethods still filters it out.
	fedCount int64

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
// account as webauthn/password+TOTP only. Required by authn.FlowQueries.
func (f *fakeSudoQueries) ListAccountIdentitiesByAccount(_ context.Context, _ int32) ([]db.ListAccountIdentitiesByAccountRow, error) {
	return nil, nil
}

// CountUsableSignInFederation returns f.fedCount (default 0). Tests that want
// AvailableMethods to surface federation_oidc can set fedCount > 0 to
// exercise the availableSudoMethods filter.
func (f *fakeSudoQueries) CountUsableSignInFederation(_ context.Context, _ int32) (int64, error) {
	return f.fedCount, nil
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

// newSudoTestServer builds a Server with enough wiring to exercise the
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
		clientIP:         clientip.NewResolver(directStore{}),
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

// TestSudoMethods_FederationExcluded: even when AvailableMethods would include
// federation_oidc (because the account has a usable upstream identity),
// /me/sudo/methods must NOT list federation_oidc — sudo accepts local factors
// only (webauthn, password_totp). Upstream re-authentication happens on the
// login screen, not in the step-up modal.
func TestSudoMethods_FederationExcluded(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	const accountID int32 = 42

	// Give the account a passkey AND a usable federation identity.
	// AvailableMethods will return [webauthn, federation_oidc].
	f.webauthnRows = []db.WebauthnCredential{{ID: 1, AccountID: accountID, Nickname: pgtype.Text{String: "yk1", Valid: true}}}
	f.fedCount = 1

	_, sess := issueSudoTestSession(t, s, accountID)
	r := sudoReq(t, sess, http.MethodGet, "/api/prohibitorum/me/sudo/methods", "")
	w := httptest.NewRecorder()
	s.handleSudoMethodsHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	got := methodsFromBody(t, w.Body.Bytes())
	if slices.Contains(got, "federation_oidc") {
		t.Errorf("federation_oidc must not appear in sudo methods, got %v", got)
	}
	if !slices.Contains(got, "webauthn") {
		t.Errorf("webauthn should still appear in sudo methods, got %v", got)
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

	// Step one TOTP counter PAST the seed Verify's consumed step. The seed
	// verify ran against now_step = now.Unix()/30 and recorded it as the
	// credential's LastStep; we compute the code for the *immediately next*
	// counter (LastStep+1) by using its period-aligned start timestamp.
	//
	// This replaces the old time.Now().Add(31s) computation, which when the
	// wall clock sat near the end of a 30s period (now.Unix()%30 >= 29) landed
	// two counters ahead of the seed step while the verifier only accepts ±1
	// drift — producing an intermittent 401 bad_credentials failure. Anchoring
	// on LastStep (rather than the clock) makes the consumed-step/replay
	// intent exact and wall-clock-independent: the code is always exactly one
	// step ahead, within drift, and strictly greater than LastStep.
	period := int64(f.totpRow.Period) // 30s (established test period)
	at := time.Unix((f.totpRow.LastStep+1)*period, 0)
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

	// Audit event should be present: factor=totp (the verified elevator),
	// event=sudo_granted, detail.method=password_totp.
	found := false
	for _, ev := range f.events {
		if ev.Factor == string(audit.FactorTOTP) && ev.Event == audit.EventSudoGranted {
			var detail map[string]any
			_ = json.Unmarshal(ev.Detail, &detail)
			if detail["method"] == "password_totp" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("audit: missing sudo_granted(totp) event with method=password_totp; events=%+v", f.events)
	}

	// Intent should have been cleared.
	if _, err := s.kvStore.Get(context.Background(), sudoIntentKey(sess.Data.SessionID)); err == nil {
		t.Error("intent should be cleared on success")
	}
}

// TestSudoComplete_PasswordTOTPSuccessAuditFactor verifies that the grant
// record carries Factor=totp (not "session") and Event=sudo_granted.
// The success path is already covered by TestSudoComplete_PasswordTOTPSuccess;
// this test isolates the factor/event shape.
func TestSudoComplete_PasswordTOTPFailAudit(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	const accountID int32 = 42
	seedPassword(t, s, accountID, "correct-horse")
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	_, sess := issueSudoTestSession(t, s, accountID)
	stashIntent(t, s, sess.Data.SessionID, "password_totp")

	// Send wrong password — should produce a sudo_failed audit record.
	at := time.Now().Add(31 * time.Second)
	code := totp.ComputeCodeForTesting(decryptTOTPSecret(t, dek, *f.totpRow, accountID), at.Unix(), 6)
	body := fmt.Sprintf(`{"current_password":"wrong","totp_code":%q}`, code)
	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/sudo/complete", body)
	w := httptest.NewRecorder()
	s.handleSudoCompleteHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d (body=%s)", w.Code, w.Body.String())
	}

	// Expect a sudo_failed audit record with factor=password.
	found := false
	for _, ev := range f.events {
		if ev.Factor == string(audit.FactorPassword) && ev.Event == audit.EventSudoFailed {
			var detail map[string]any
			_ = json.Unmarshal(ev.Detail, &detail)
			if detail["reason"] == "bad_credentials" && detail["method"] == "password_totp" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("audit: missing sudo_failed(password) event; events=%+v", f.events)
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

func TestHasFreshSudo_RecentAuthWindow(t *testing.T) {
	s := &Server{config: &configx.Config{}}
	s.config.Auth.SudoTTL = 15 * time.Minute

	// Recently issued, no explicit SudoUntil → elevated by the window.
	fresh := &authn.Session{Data: &authn.SessionData{IssuedAt: time.Now()}}
	if !s.hasFreshSudo(fresh) {
		t.Fatal("recently-issued session should satisfy the gate (recent-auth window)")
	}

	// Issued longer ago than the window, zero SudoUntil → denied.
	stale := &authn.Session{Data: &authn.SessionData{IssuedAt: time.Now().Add(-30 * time.Minute)}}
	if s.hasFreshSudo(stale) {
		t.Fatal("stale session with no step-up should NOT satisfy the gate")
	}

	// Stale issue but an explicit step-up still in its window → elevated.
	stepped := &authn.Session{Data: &authn.SessionData{
		IssuedAt:  time.Now().Add(-30 * time.Minute),
		SudoUntil: time.Now().Add(5 * time.Minute),
	}}
	if !s.hasFreshSudo(stepped) {
		t.Fatal("explicit step-up window should satisfy the gate")
	}

	// Zero-config (TTL=0): window inert, falls back to SudoUntil only.
	zero := &Server{config: &configx.Config{}}
	if zero.hasFreshSudo(fresh) {
		t.Fatal("with SudoTTL=0 the recent-auth window must be inert")
	}
}
