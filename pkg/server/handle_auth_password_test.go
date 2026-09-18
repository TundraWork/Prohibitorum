// Package server — handle_auth_password_test.go
//
// Unit-level tests for the Password+TOTP login handlers. These exercise
// the partial-session token mechanics (KV stash, single-use consumption,
// success-path session issuance) without spinning up the full Server
// constructor. Tests against a real DB live in cmd/smoke (Task 8).
//
// Scope: TOTP-verify and recovery-code-verify success paths, the
// partial-session-token-missing 401, and the consume-on-failure
// guarantee. The /auth/password/begin handler is covered by the
// smoke test because it reaches into *db.Queries directly and
// stubbing the sqlc-generated concrete type from here would require
// invasive refactoring of unrelated handlers.

package server

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/clientip"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/credential/totp"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	sessstore "prohibitorum/pkg/session"
)

// directStore is a minimal clientip.Store that always returns the "direct"
// strategy, used to provide a non-nil clientIP resolver in unit tests that
// build *Server manually without the full constructor.
type directStore struct{}

func (directStore) Get(_ context.Context) (clientip.Stored, error) {
	return clientip.Stored{Strategy: "direct"}, nil
}
func (directStore) Set(_ context.Context, _ clientip.Stored) error { return nil }

// newDirectResolver returns a *clientip.Resolver wired to a direct-strategy
// in-memory store, suitable for tests that don't need real IP extraction.
func newDirectResolver() *clientip.Resolver { return clientip.NewResolver(directStore{}) }

// TestPasswordBeginRateLimitsByIP verifies the per-IP fixed-window cap added in
// front of /auth/password/begin (audit AUTHZ-1): a flood from one IP gets a 429
// after pwdBeginIPLimit requests, bounding the unauthenticated argon2id DoS
// surface. Bodies are empty so each allowed request bails at the decode guard
// before any DB/argon2 work — proving the limiter sits ahead of everything.
func TestPasswordBeginRateLimitsByIP(t *testing.T) {
	s := &Server{config: &configx.Config{}, rateLimiter: authn.NewRateLimiter(), clientIP: newDirectResolver()}

	for i := 0; i < pwdBeginIPLimit; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/auth/password/begin", strings.NewReader("{}"))
		s.handlePasswordBeginHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d was rate-limited early (got 429); limit is %d", i+1, pwdBeginIPLimit)
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/password/begin", strings.NewReader("{}"))
	s.handlePasswordBeginHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request %d: want 429, got %d", pwdBeginIPLimit+1, rec.Code)
	}
}

// TestPasswordBeginRejectsOversizePassword verifies the length cap added to
// /auth/password/begin (audit AUTHZ-1): an over-cap password is rejected with
// 401 BEFORE any account lookup or argon2id hash. s.queries is nil here, so if
// the cap did not short-circuit, the handler would reach s.queries and panic —
// the clean 401 proves the cap fires first.
func TestPasswordBeginRejectsOversizePassword(t *testing.T) {
	s := &Server{config: &configx.Config{}, rateLimiter: authn.NewRateLimiter(), clientIP: newDirectResolver()}

	body := fmt.Sprintf(`{"username":"alice","password":%q}`, strings.Repeat("a", maxPasswordBytes+1))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/password/begin", strings.NewReader(body))
	s.handlePasswordBeginHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("oversize password: want 401, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// TestLoginCompleteRateLimitsByIP verifies the per-IP cap added to
// /auth/login/complete (audit SESS-3). With no ceremony cookie each allowed
// request bails at the cookie guard (401) before any KV/webauthn work; after
// loginIPLimit requests the IP is throttled with 429.
func TestLoginCompleteRateLimitsByIP(t *testing.T) {
	s := &Server{config: &configx.Config{}, rateLimiter: authn.NewRateLimiter(), clientIP: newDirectResolver()}

	for i := 0; i < loginIPLimit; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/auth/login/complete", strings.NewReader("{}"))
		s.handleLoginCompleteHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d was rate-limited early (got 429); limit is %d", i+1, loginIPLimit)
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/login/complete", strings.NewReader("{}"))
	s.handleLoginCompleteHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request %d: want 429, got %d", loginIPLimit+1, rec.Code)
	}
}

// fakeAuthQueries satisfies totp.TOTPQueries, authn.ThrottleQueries, the
// audit InsertCredentialEvent call, and session.SessionQueries — every
// query surface the verify handlers reach through. Mirrors the pattern in
// pkg/credential/totp/totp_test.go.
type fakeAuthQueries struct {
	db.Querier

	totpRow            *db.TotpCredential
	recoveryRows       []db.RecoveryCode
	nextRecID          int32
	consumeRecoveryErr error

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

func (f *fakeAuthQueries) GetAccountByIDForUpdate(ctx context.Context, id int32) (db.Account, error) {
	return f.GetAccountByID(ctx, id)
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
	if f.consumeRecoveryErr != nil {
		return db.RecoveryCode{}, f.consumeRecoveryErr
	}
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

type fakeAuthEnrollmentTxRunner struct {
	q         *fakeAuthQueries
	commitErr error
}

func (r *fakeAuthEnrollmentTxRunner) BeginEnrollmentTx(context.Context) (enrollmentTx, error) {
	var totpRow *db.TotpCredential
	if r.q.totpRow != nil {
		row := *r.q.totpRow
		row.SecretEnc = append([]byte(nil), row.SecretEnc...)
		row.SecretNonce = append([]byte(nil), row.SecretNonce...)
		totpRow = &row
	}
	return &fakeAuthEnrollmentTx{
		q: r.q, commitErr: r.commitErr, totpRow: totpRow,
		recoveryRows: append([]db.RecoveryCode(nil), r.q.recoveryRows...), nextRecID: r.q.nextRecID,
	}, nil
}

type fakeAuthEnrollmentTx struct {
	q            *fakeAuthQueries
	commitErr    error
	committed    bool
	totpRow      *db.TotpCredential
	recoveryRows []db.RecoveryCode
	nextRecID    int32
}

func (tx *fakeAuthEnrollmentTx) Queries() db.Querier { return tx.q }
func (tx *fakeAuthEnrollmentTx) Commit(context.Context) error {
	if tx.commitErr != nil {
		return tx.commitErr
	}
	tx.committed = true
	return nil
}
func (tx *fakeAuthEnrollmentTx) Rollback(context.Context) error {
	if tx.committed {
		return nil
	}
	tx.q.totpRow = tx.totpRow
	tx.q.recoveryRows = append([]db.RecoveryCode(nil), tx.recoveryRows...)
	tx.q.nextRecID = tx.nextRecID
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
		config:                     cfg,
		kvStore:                    kvStore,
		sessionStore:               sessionStore,
		rateLimiter:                authn.NewRateLimiter(),
		totpStore:                  totpStore,
		throttle:                   throttle,
		Audit:                      auditWriter,
		accountLookup:              f, // Fix 4: step-2 disabled re-check
		clientIP:                   newDirectResolver(),
		enrollmentTxRunnerOverride: &fakeAuthEnrollmentTxRunner{q: f},
	}
	return s, f, dek
}

// seedConfirmedTOTP installs a confirmed totp_credential row and mints 10
// recovery codes via the store's normal path. Returns the plaintext
// recovery codes (10 of them).
func seedConfirmedTOTP(t *testing.T, s *Server, f *fakeAuthQueries, dek []byte, accountID int32) []string {
	t.Helper()
	ctx := context.Background()
	secret := browserTOTPSecret(t)
	code := codeForSecret(t, secret)
	step, ok := s.totpStore.VerifyCandidateSecret(secret, code)
	if !ok {
		t.Fatal("candidate TOTP rejected")
	}
	codes, err := s.totpStore.EnrollConfirmedForTx(ctx, f, accountID, secret, step)
	if err != nil {
		t.Fatalf("seed confirmed TOTP: %v", err)
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

// TestTOTPVerify_RejectsWrongCompletedFactor: step-2 must reject a partial
// session whose recorded first factor isn't "password", so the MFA state machine
// self-validates rather than trusting that the sole writer always set it
// correctly (audit WACER-2). A token carrying a non-password factor is rejected
// with partial_session_invalid BEFORE any TOTP verification.
func TestTOTPVerify_RejectsWrongCompletedFactor(t *testing.T) {
	s, _, _ := newTestServer(t)
	token := mustToken(t)
	payload, _ := json.Marshal(partialSession{AccountID: 42, FactorCompleted: "", IssuedAt: time.Now().UTC()})
	if err := s.kvStore.SetEx(context.Background(), partialSessionKey(token), string(payload), s.config.Auth.PartialSessionTTL); err != nil {
		t.Fatal(err)
	}

	body := fmt.Sprintf(`{"partial_session_token":%q,"code":"123456"}`, token)
	req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/totp/verify", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()

	s.handleTOTPVerifyHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["code"] != "partial_session_invalid" {
		t.Errorf("code: want partial_session_invalid, got %v", resp["code"])
	}
}

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

	// Center the code 15s into the step AFTER the one the seed's confirm-Verify
	// consumed: always within the handler's ±1-step drift window and never on a
	// 30s boundary (a +31s offset can cross two steps at the tail of a window,
	// landing outside drift → a flaky 401).
	at := (time.Now().Unix()/30+1)*30 + 15
	code := totp.ComputeCodeForTesting(decryptTOTPSecret(t, dek, *f.totpRow, accountID), at, 6)

	bodyJSON := fmt.Sprintf(`{"partial_session_token":%q,"code":%q}`, token, code)
	req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/totp/verify",
		strings.NewReader(bodyJSON))
	req.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()

	s.handleTOTPVerifyHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}

	// Body must be a LoginResult with redirect == "/" (no return_to supplied).
	var result struct {
		Redirect string `json:"redirect"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode LoginResult: %v", err)
	}
	if result.Redirect != "/" {
		t.Errorf("redirect: want %q, got %q", "/", result.Redirect)
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

// TestTOTPVerify_RedirectValidation proves that validateReturnTo runs at
// completion: a same-origin relative path is kept verbatim, and a
// cross-origin URL is collapsed to "/". Cases are issuer-independent so
// they do not depend on the test config's OIDC issuer. Each case spins up
// its own Server so that each has independent TOTP state (LastStep) and
// computes its code one step after its own seed step (see the timing note
// below) without hitting replay rejection.
func TestTOTPVerify_RedirectValidation(t *testing.T) {
	cases := []struct {
		name         string
		returnTo     string
		wantRedirect string
	}{
		{
			name:         "same-origin relative kept",
			returnTo:     "/me/security",
			wantRedirect: "/me/security",
		},
		{
			name:         "cross-origin rejected to safe default",
			returnTo:     "https://evil.test/x",
			wantRedirect: "/",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, f, dek := newTestServer(t)
			const accountID int32 = 99
			_ = seedConfirmedTOTP(t, s, f, dek, accountID)

			token := mustToken(t)
			stashPartialSession(t, s, token, accountID)

			// Center the code 15s into the step AFTER the one the seed's confirm-Verify
			// consumed: always within the handler's ±1-step drift window and never on a
			// 30s boundary (a +31s offset can cross two steps at the tail of a window,
			// landing outside drift → a flaky 401).
			at := (time.Now().Unix()/30+1)*30 + 15
			code := totp.ComputeCodeForTesting(decryptTOTPSecret(t, dek, *f.totpRow, accountID), at, 6)

			bodyJSON := fmt.Sprintf(`{"partial_session_token":%q,"code":%q}`, token, code)
			target := "/api/prohibitorum/auth/totp/verify?return_to=" + url.QueryEscape(tc.returnTo)
			req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(bodyJSON))
			req.RemoteAddr = "127.0.0.1:5555"
			w := httptest.NewRecorder()

			s.handleTOTPVerifyHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
			}
			var result struct {
				Redirect string `json:"redirect"`
			}
			if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
				t.Fatalf("decode LoginResult: %v", err)
			}
			if result.Redirect != tc.wantRedirect {
				t.Errorf("redirect: want %q, got %q", tc.wantRedirect, result.Redirect)
			}
		})
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

func recoveryCodeVerify(t *testing.T, s *Server, token, code string, reset bool, secret, totpCode string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"partial_session_token":%q,"code":%q,"reset_authenticator":%t`, token, code, reset)
	if secret != "" {
		body += fmt.Sprintf(`,"totp_secret_base32":%q`, secret)
	}
	if totpCode != "" {
		body += fmt.Sprintf(`,"totp_code":%q`, totpCode)
	}
	body += "}"
	req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/auth/recovery-code/verify?return_to=%2Fme%2Fsecurity", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:5555"
	w := httptest.NewRecorder()
	s.handleRecoveryCodeVerifyHTTP(w, req)
	return w
}

func TestRecoveryCodeVerify_WithoutResetConsumesOnlyOneCode(t *testing.T) {
	s, f, dek := newTestServer(t)
	codes := seedConfirmedTOTP(t, s, f, dek, 42)
	oldTOTP := *f.totpRow
	token := mustToken(t)
	stashPartialSession(t, s, token, 42)
	w := recoveryCodeVerify(t, s, token, codes[0], false, "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var result struct {
		Redirect string `json:"redirect"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Redirect != "/me/security" {
		t.Fatalf("response = %+v, err = %v", result, err)
	}
	if len(f.sessions) != 1 || !equalAmr(f.sessions[0].Amr, []string{"pwd", "recovery_code", "mfa"}) {
		t.Fatalf("sessions = %+v", f.sessions)
	}
	if !f.recoveryRows[0].UsedAt.Valid {
		t.Fatal("submitted recovery code was not consumed")
	}
	for _, row := range f.recoveryRows[1:] {
		if row.UsedAt.Valid {
			t.Fatal("another recovery code was consumed")
		}
	}
	if !slices.Equal(f.totpRow.SecretEnc, oldTOTP.SecretEnc) {
		t.Fatal("TOTP changed without reset")
	}
}

func TestRecoveryCodeVerify_WithResetReplacesAuthenticatorAtomically(t *testing.T) {
	s, f, dek := newTestServer(t)
	codes := seedConfirmedTOTP(t, s, f, dek, 42)
	oldSecret := append([]byte(nil), decryptTOTPSecret(t, dek, *f.totpRow, 42)...)
	token := mustToken(t)
	stashPartialSession(t, s, token, 42)
	candidate, candidateCode := passwordTOTPCandidate(t, s)
	w := recoveryCodeVerify(t, s, token, codes[0], true, candidate, candidateCode)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var result struct {
		Redirect      string   `json:"redirect"`
		RecoveryCodes []string `json:"recovery_codes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Redirect != "/me/security" || len(result.RecoveryCodes) != 10 {
		t.Fatalf("response = %+v", result)
	}
	if slices.Equal(decryptTOTPSecret(t, dek, *f.totpRow, 42), oldSecret) || f.totpRow.LastStep <= 0 {
		t.Fatal("TOTP was not replaced with replay step")
	}
	if len(f.recoveryRows) != 10 {
		t.Fatalf("recovery rows = %d, want 10", len(f.recoveryRows))
	}
	if len(f.sessions) != 1 || !equalAmr(f.sessions[0].Amr, []string{"pwd", "otp", "mfa"}) {
		t.Fatalf("sessions = %+v", f.sessions)
	}
}

func TestRecoveryCodeVerify_ResetFieldsMustMatchFlag(t *testing.T) {
	for _, tc := range []struct {
		name         string
		reset        bool
		secret, code string
	}{
		{"reset missing replacement", true, "", ""},
		{"reset missing code", true, "ABCDEFGHIJKLMNOPQRSTUVWX23456789", ""},
		{"replacement without reset", false, "ABCDEFGHIJKLMNOPQRSTUVWX23456789", "123456"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f, dek := newTestServer(t)
			codes := seedConfirmedTOTP(t, s, f, dek, 42)
			token := mustToken(t)
			stashPartialSession(t, s, token, 42)
			w := recoveryCodeVerify(t, s, token, codes[0], tc.reset, tc.secret, tc.code)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", w.Code, w.Body.String())
			}
			if _, err := s.kvStore.Get(context.Background(), partialSessionKey(token)); err != nil {
				t.Fatal("invalid request shape consumed partial token")
			}
		})
	}
}

func TestRecoveryCodeVerify_InvalidCredentialsConsumePartialAndPreserveFactors(t *testing.T) {
	for _, tc := range []struct {
		name               string
		recoveryOK, totpOK bool
	}{
		{"recovery code", false, true},
		{"candidate TOTP", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f, dek := newTestServer(t)
			codes := seedConfirmedTOTP(t, s, f, dek, 42)
			oldTOTP := *f.totpRow
			oldCodes := append([]db.RecoveryCode(nil), f.recoveryRows...)
			token := mustToken(t)
			stashPartialSession(t, s, token, 42)
			candidate, candidateCode := passwordTOTPCandidate(t, s)
			recovery := "WRONG-CODE-NOT-VALID"
			if tc.recoveryOK {
				recovery = codes[0]
			}
			if !tc.totpOK {
				candidateCode = "000000"
			}
			w := recoveryCodeVerify(t, s, token, recovery, true, candidate, candidateCode)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401: %s", w.Code, w.Body.String())
			}
			if _, err := s.kvStore.Get(context.Background(), partialSessionKey(token)); err == nil {
				t.Fatal("failed attempt did not consume partial token")
			}
			if !slices.Equal(f.totpRow.SecretEnc, oldTOTP.SecretEnc) || !slices.Equal(f.recoveryRows, oldCodes) {
				t.Fatal("failed attempt changed credentials")
			}
		})
	}
}

func TestRecoveryCodeVerify_CommitFailureRollsBack(t *testing.T) {
	s, f, dek := newTestServer(t)
	codes := seedConfirmedTOTP(t, s, f, dek, 42)
	oldTOTP := *f.totpRow
	oldTOTP.SecretEnc = append([]byte(nil), oldTOTP.SecretEnc...)
	oldTOTP.SecretNonce = append([]byte(nil), oldTOTP.SecretNonce...)
	oldCodes := append([]db.RecoveryCode(nil), f.recoveryRows...)
	token := mustToken(t)
	stashPartialSession(t, s, token, 42)
	candidate, candidateCode := passwordTOTPCandidate(t, s)
	s.enrollmentTxRunnerOverride.(*fakeAuthEnrollmentTxRunner).commitErr = errors.New("commit failed")
	w := recoveryCodeVerify(t, s, token, codes[0], true, candidate, candidateCode)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", w.Code, w.Body.String())
	}
	if !slices.Equal(f.totpRow.SecretEnc, oldTOTP.SecretEnc) || !slices.Equal(f.totpRow.SecretNonce, oldTOTP.SecretNonce) || !slices.Equal(f.recoveryRows, oldCodes) {
		t.Fatal("commit failure changed credentials")
	}
	if len(f.sessions) != 0 {
		t.Fatal("commit failure issued a session")
	}
}

func TestRecoveryCodeVerify_ConsumeInfrastructureFailureDoesNotCountAsBadCredentials(t *testing.T) {
	s, f, dek := newTestServer(t)
	codes := seedConfirmedTOTP(t, s, f, dek, 42)
	oldCodes := append([]db.RecoveryCode(nil), f.recoveryRows...)
	token := mustToken(t)
	stashPartialSession(t, s, token, 42)
	f.consumeRecoveryErr = errors.New("database unavailable")

	w := recoveryCodeVerify(t, s, token, codes[0], false, "", "")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500: %s", w.Code, w.Body.String())
	}
	if _, ok := f.throttle[f.throttleKey(42, "recovery_code")]; ok {
		t.Fatal("infrastructure failure incremented recovery-code throttle")
	}
	for _, event := range f.events {
		if event.Event == string(audit.EventFail) && event.Factor == string(audit.FactorRecoveryCode) {
			t.Fatal("infrastructure failure emitted a recovery-code failure event")
		}
	}
	if !slices.Equal(f.recoveryRows, oldCodes) {
		t.Fatal("infrastructure failure changed recovery codes")
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
