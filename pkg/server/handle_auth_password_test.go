// Package server — handle_auth_password_test.go
//
// Unit-level tests for the Password+TOTP login handlers. These exercise
// the partial-session token mechanics (KV stash, single-use consumption,
// success-path session issuance) without spinning up the full Server
// constructor. Tests against a real DB live in cmd/smoke (Task 8).
//
// Scope: TOTP-verify and recovery-code-verify success paths, the
// partial-session-token-missing 401, and the consume-on-failure
// guarantee. The /auth/password/begin handler is covered by the v0.2
// smoke test because step 1 reaches into *db.Queries directly and
// stubbing the sqlc-generated concrete type from here would require
// invasive refactoring of unrelated handlers.

package server

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/credential/totp"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	sessstore "prohibitorum/pkg/session"
)

// fakeAuthQueries satisfies totp.TOTPQueries, authn.ThrottleQueries, the
// audit InsertCredentialEvent call, and session.SessionQueries — every
// query surface the verify handlers reach through. Mirrors the pattern in
// pkg/credential/totp/totp_test.go.
type fakeAuthQueries struct {
	db.Querier

	totpRow      *db.TotpCredential
	recoveryRows []db.RecoveryCode
	nextRecID    int32

	throttle map[string]db.AuthThrottle
	events   []db.InsertCredentialEventParams
	sessions []db.Session
	revokes  []string

	// accounts indexed by ID — used by the post-partial-session disabled
	// re-check in handleTOTPVerifyHTTP / handleRecoveryCodeVerifyHTTP
	// (Bundle 1 / Fix 4). Tests that don't seed an account default to a
	// synthetic enabled row so the legacy code paths keep working.
	accounts map[int32]db.Account
}

func newFakeAuthQueries() *fakeAuthQueries {
	return &fakeAuthQueries{
		throttle:  map[string]db.AuthThrottle{},
		nextRecID: 1,
		accounts:  map[int32]db.Account{},
	}
}

// GetAccountByID satisfies accountLookupQueries. Returns the seeded row;
// when none was seeded, falls back to a synthetic enabled account so the
// step-2 disabled re-check passes for tests that predate Fix 4.
func (f *fakeAuthQueries) GetAccountByID(_ context.Context, id int32) (db.Account, error) {
	if a, ok := f.accounts[id]; ok {
		return a, nil
	}
	return db.Account{ID: id, Username: "alice"}, nil
}

func (f *fakeAuthQueries) GetTOTPCredential(_ context.Context, accountID int32) (db.TotpCredential, error) {
	if f.totpRow == nil || f.totpRow.AccountID != accountID {
		return db.TotpCredential{}, pgx.ErrNoRows
	}
	return *f.totpRow, nil
}

func (f *fakeAuthQueries) InsertTOTPCredential(_ context.Context, arg db.InsertTOTPCredentialParams) (db.TotpCredential, error) {
	// ConfirmedAt is intentionally left unset so the first Verify will both
	// confirm and mint recovery codes (matching the production semantics).
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

func (f *fakeAuthQueries) DeleteTOTPCredential(_ context.Context, accountID int32) error {
	if f.totpRow != nil && f.totpRow.AccountID == accountID {
		f.totpRow = nil
	}
	return nil
}

func (f *fakeAuthQueries) ConfirmTOTPCredential(_ context.Context, accountID int32) error {
	if f.totpRow != nil && f.totpRow.AccountID == accountID {
		f.totpRow.ConfirmedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	}
	return nil
}

func (f *fakeAuthQueries) UpdateTOTPLastStep(_ context.Context, arg db.UpdateTOTPLastStepParams) (int64, error) {
	if f.totpRow != nil && f.totpRow.AccountID == arg.AccountID && arg.LastStep > f.totpRow.LastStep {
		f.totpRow.LastStep = arg.LastStep
		return arg.LastStep, nil
	}
	return 0, pgx.ErrNoRows
}

func (f *fakeAuthQueries) ListRecoveryCodesByAccount(_ context.Context, accountID int32) ([]db.RecoveryCode, error) {
	var out []db.RecoveryCode
	for _, r := range f.recoveryRows {
		if r.AccountID == accountID && !r.UsedAt.Valid {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeAuthQueries) InsertRecoveryCode(_ context.Context, arg db.InsertRecoveryCodeParams) (db.RecoveryCode, error) {
	row := db.RecoveryCode{
		ID:        f.nextRecID,
		AccountID: arg.AccountID,
		Hash:      arg.Hash,
	}
	f.nextRecID++
	f.recoveryRows = append(f.recoveryRows, row)
	return row, nil
}

func (f *fakeAuthQueries) ConsumeRecoveryCode(_ context.Context, arg db.ConsumeRecoveryCodeParams) (db.RecoveryCode, error) {
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

func (f *fakeAuthQueries) DeleteAllRecoveryCodesByAccount(_ context.Context, accountID int32) error {
	keep := f.recoveryRows[:0]
	for _, r := range f.recoveryRows {
		if r.AccountID != accountID {
			keep = append(keep, r)
		}
	}
	f.recoveryRows = keep
	return nil
}

func (f *fakeAuthQueries) throttleKey(accountID int32, factor string) string {
	return fmt.Sprintf("%d:%s", accountID, factor)
}

func (f *fakeAuthQueries) GetAuthThrottle(_ context.Context, arg db.GetAuthThrottleParams) (db.AuthThrottle, error) {
	row, ok := f.throttle[f.throttleKey(arg.AccountID, arg.Factor)]
	if !ok {
		return db.AuthThrottle{}, pgx.ErrNoRows
	}
	return row, nil
}

func (f *fakeAuthQueries) BumpAuthThrottle(_ context.Context, arg db.BumpAuthThrottleParams) (db.BumpAuthThrottleRow, error) {
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

func (f *fakeAuthQueries) ResetAuthThrottle(_ context.Context, arg db.ResetAuthThrottleParams) error {
	delete(f.throttle, f.throttleKey(arg.AccountID, arg.Factor))
	return nil
}

func (f *fakeAuthQueries) InsertCredentialEvent(_ context.Context, arg db.InsertCredentialEventParams) error {
	f.events = append(f.events, arg)
	return nil
}

func (f *fakeAuthQueries) InsertSession(_ context.Context, arg db.InsertSessionParams) (db.Session, error) {
	row := db.Session{
		ID:        arg.ID,
		AccountID: arg.AccountID,
		AuthTime:  arg.AuthTime,
		Amr:       arg.Amr,
	}
	f.sessions = append(f.sessions, row)
	return row, nil
}

func (f *fakeAuthQueries) RevokeSession(_ context.Context, id string) error {
	f.revokes = append(f.revokes, id)
	return nil
}

func (f *fakeAuthQueries) RevokeAllSessionsByAccount(_ context.Context, _ int32) error {
	return nil
}

// --- Server scaffolding ----------------------------------------------------

// newTestServer builds a Server with the minimum wiring needed to exercise
// /auth/totp/verify and /auth/recovery-code/verify. queries is left nil
// because those handlers never touch *db.Queries directly — they go
// through totpStore and sessionStore, both of which take fake interfaces.
func newTestServer(t *testing.T) (*Server, *fakeAuthQueries, []byte) {
	t.Helper()
	f := newFakeAuthQueries()

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
	cfg := &configx.Config{
		SessionTTL: time.Hour,
		TOTP:       totpCfg,
		Auth:       authCfg,
	}

	auditWriter := audit.NewWriter(f)
	throttle := authn.NewThrottle(f, throttleSchedule, auditWriter)
	totpStore := totp.NewStore(f, &totpTestTxRunner{q: f}, deks, totpCfg, throttle, auditWriter)

	kvStore := kv.NewMemoryStore()
	sessionStore := sessstore.NewSessionStore(kvStore, f, cfg.SessionTTL)

	s := &Server{
		config:        cfg,
		kvStore:       kvStore,
		sessionStore:  sessionStore,
		rateLimiter:   authn.NewRateLimiter(),
		totpStore:     totpStore,
		throttle:      throttle,
		Audit:         auditWriter,
		accountLookup: f, // Fix 4: step-2 disabled re-check
	}
	return s, f, dek
}

// seedConfirmedTOTP installs a confirmed totp_credential row and mints 10
// recovery codes via the store's normal path. Returns the plaintext
// recovery codes (10 of them).
func seedConfirmedTOTP(t *testing.T, s *Server, f *fakeAuthQueries, dek []byte, accountID int32) []string {
	t.Helper()
	ctx := context.Background()
	if _, err := s.totpStore.Begin(ctx, accountID, "alice"); err != nil {
		t.Fatalf("totpStore.Begin: %v", err)
	}
	row := *f.totpRow
	// First verify confirms enrollment and mints recovery codes.
	code := totpCodeFor(t, dek, row, accountID, time.Now(), 0)
	codes, err := s.totpStore.Verify(ctx, accountID, code)
	if err != nil {
		t.Fatalf("totpStore.Verify (confirm): %v", err)
	}
	if len(codes) != 10 {
		t.Fatalf("recovery codes: want 10, got %d", len(codes))
	}
	return codes
}

func totpCodeFor(t *testing.T, dek []byte, row db.TotpCredential, accountID int32, at time.Time, _ int64) string {
	t.Helper()
	plaintext := decryptTOTPSecret(t, dek, row, accountID)
	return totp.ComputeCodeForTesting(plaintext, at.Unix(), int(row.Digits))
}

// decryptTOTPSecret mirrors the AAD construction in pkg/credential/totp/aead.go
// so we can compute codes externally without exposing the unexported helper.
// AAD layout: "totp:<accountID>:<keyVersion>".
func decryptTOTPSecret(t *testing.T, dek []byte, row db.TotpCredential, accountID int32) []byte {
	t.Helper()
	aad := []byte("totp:" + strconv.Itoa(int(accountID)) + ":" + strconv.Itoa(int(row.KeyVersion)))
	block, err := aes.NewCipher(dek)
	if err != nil {
		t.Fatalf("aes.NewCipher: %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("cipher.NewGCM: %v", err)
	}
	plaintext, err := aead.Open(nil, row.SecretNonce, row.SecretEnc, aad)
	if err != nil {
		t.Fatalf("decrypt totp secret: %v", err)
	}
	return plaintext
}

// --- Tests -----------------------------------------------------------------

func TestTOTPVerify_MissingTokenReturns401(t *testing.T) {
	s, _, _ := newTestServer(t)

	body := strings.NewReader(`{"partial_session_token":"bogus","code":"123456"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/totp/verify", body)
	req.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()

	s.handleTOTPVerifyHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp["code"] != "partial_session_invalid" {
		t.Errorf("code: want partial_session_invalid, got %v", resp["code"])
	}
}

func TestTOTPVerify_EmptyBodyReturns401(t *testing.T) {
	s, _, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/totp/verify", strings.NewReader(`{}`))
	req.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()

	s.handleTOTPVerifyHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", w.Code)
	}
}

func TestTOTPVerify_Success(t *testing.T) {
	s, f, dek := newTestServer(t)
	const accountID int32 = 42
	_ = seedConfirmedTOTP(t, s, f, dek, accountID)

	// Manually stash a partial-session token in KV so we don't depend on
	// the begin handler (which needs *db.Queries — out of scope here).
	token := mustToken(t)
	stashPartialSession(t, s, token, accountID)

	// Advance time enough to be in a fresh step (the seed Verify consumed
	// the current step). 31 seconds is enough.
	at := time.Now().Add(31 * time.Second)
	code := totp.ComputeCodeForTesting(decryptTOTPSecret(t, dek, *f.totpRow, accountID), at.Unix(), 6)

	bodyJSON := fmt.Sprintf(`{"partial_session_token":%q,"code":%q}`, token, code)
	req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/totp/verify",
		strings.NewReader(bodyJSON))
	req.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()

	s.handleTOTPVerifyHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status: want 204, got %d (body=%s)", w.Code, w.Body.String())
	}

	// Session cookie should be present.
	cookies := w.Result().Cookies()
	var sessCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == sessstore.SessionCookieName {
			sessCookie = c
			break
		}
	}
	if sessCookie == nil {
		t.Fatalf("no %s cookie in response", sessstore.SessionCookieName)
	}
	if sessCookie.Value == "" {
		t.Fatalf("session cookie value empty")
	}

	// PG session row should have been inserted with the right amr.
	if len(f.sessions) != 1 {
		t.Fatalf("sessions: want 1, got %d", len(f.sessions))
	}
	wantAmr := []string{"pwd", "otp", "mfa"}
	if !equalAmr(f.sessions[0].Amr, wantAmr) {
		t.Errorf("amr: want %v, got %v", wantAmr, f.sessions[0].Amr)
	}

	// Partial-session token must be gone from KV.
	if _, err := s.kvStore.Get(context.Background(), partialSessionKey(token)); err == nil {
		t.Error("partial-session token should be consumed on success")
	}
}

func TestTOTPVerify_ConsumesTokenOnFailure(t *testing.T) {
	s, f, dek := newTestServer(t)
	const accountID int32 = 42
	_ = seedConfirmedTOTP(t, s, f, dek, accountID)

	token := mustToken(t)
	stashPartialSession(t, s, token, accountID)

	// First attempt with a wrong code — should fail but consume the token.
	body1 := fmt.Sprintf(`{"partial_session_token":%q,"code":"000000"}`, token)
	req1 := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/totp/verify",
		strings.NewReader(body1))
	req1.RemoteAddr = "127.0.0.1:5555"
	w1 := httptest.NewRecorder()
	s.handleTOTPVerifyHTTP(w1, req1)

	if w1.Code != http.StatusUnauthorized {
		t.Fatalf("first attempt: want 401, got %d (body=%s)", w1.Code, w1.Body.String())
	}

	// Token should be gone — second attempt (even with a correct code) is rejected.
	at := time.Now().Add(31 * time.Second)
	correct := totp.ComputeCodeForTesting(decryptTOTPSecret(t, dek, *f.totpRow, accountID), at.Unix(), 6)
	body2 := fmt.Sprintf(`{"partial_session_token":%q,"code":%q}`, token, correct)
	req2 := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/totp/verify",
		strings.NewReader(body2))
	req2.RemoteAddr = "127.0.0.1:5555"
	w2 := httptest.NewRecorder()
	s.handleTOTPVerifyHTTP(w2, req2)

	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("second attempt: want 401, got %d (body=%s)", w2.Code, w2.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w2.Body.Bytes(), &resp)
	if resp["code"] != "partial_session_invalid" {
		t.Errorf("second attempt code: want partial_session_invalid, got %v", resp["code"])
	}
}

func TestRecoveryCodeVerify_Success(t *testing.T) {
	s, f, dek := newTestServer(t)
	const accountID int32 = 42
	codes := seedConfirmedTOTP(t, s, f, dek, accountID)

	token := mustToken(t)
	stashPartialSession(t, s, token, accountID)

	bodyJSON := fmt.Sprintf(`{"partial_session_token":%q,"code":%q}`, token, codes[0])
	req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/recovery-code/verify",
		strings.NewReader(bodyJSON))
	req.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()

	s.handleRecoveryCodeVerifyHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status: want 204, got %d (body=%s)", w.Code, w.Body.String())
	}

	if len(f.sessions) != 1 {
		t.Fatalf("sessions: want 1, got %d", len(f.sessions))
	}
	wantAmr := []string{"pwd", "recovery_code", "mfa"}
	if !equalAmr(f.sessions[0].Amr, wantAmr) {
		t.Errorf("amr: want %v, got %v", wantAmr, f.sessions[0].Amr)
	}
}

func TestRecoveryCodeVerify_InvalidCodeConsumesToken(t *testing.T) {
	s, f, dek := newTestServer(t)
	const accountID int32 = 42
	_ = seedConfirmedTOTP(t, s, f, dek, accountID)

	token := mustToken(t)
	stashPartialSession(t, s, token, accountID)

	body := fmt.Sprintf(`{"partial_session_token":%q,"code":"WRONG-CODE-NOT-VALID"}`, token)
	req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/recovery-code/verify",
		strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()
	s.handleRecoveryCodeVerifyHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", w.Code)
	}
	// Token consumed.
	if _, err := s.kvStore.Get(context.Background(), partialSessionKey(token)); err == nil {
		t.Error("partial-session token should be consumed even on recovery-code failure")
	}
}

func TestPartialSessionTTL_ExpiredTokenRejected(t *testing.T) {
	s, _, _ := newTestServer(t)

	// Stash with a 1ms TTL, sleep past it, then try verify.
	token := mustToken(t)
	payload, _ := json.Marshal(partialSession{
		AccountID:       42,
		FactorCompleted: "password",
		IssuedAt:        time.Now().UTC(),
	})
	if err := s.kvStore.SetEx(context.Background(), partialSessionKey(token), string(payload), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)

	body := fmt.Sprintf(`{"partial_session_token":%q,"code":"123456"}`, token)
	req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/totp/verify",
		strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()
	s.handleTOTPVerifyHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"] != "partial_session_invalid" {
		t.Errorf("code: want partial_session_invalid, got %v", resp["code"])
	}
}

// --- helpers ---------------------------------------------------------------

func mustToken(t *testing.T) string {
	t.Helper()
	tok, err := newCeremonyToken()
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func stashPartialSession(t *testing.T, s *Server, token string, accountID int32) {
	t.Helper()
	payload, _ := json.Marshal(partialSession{
		AccountID:       accountID,
		FactorCompleted: "password",
		IssuedAt:        time.Now().UTC(),
	})
	if err := s.kvStore.SetEx(context.Background(), partialSessionKey(token), string(payload), s.config.Auth.PartialSessionTTL); err != nil {
		t.Fatal(err)
	}
}

func equalAmr(a, b []string) bool {
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

