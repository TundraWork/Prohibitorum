// Package server — handle_me_totp_test.go
//
// Unit tests for the /me/totp/* endpoints: enrollment begin/verify and
// recovery-code regeneration. Conditional-sudo branches (no row / unconfirmed
// row / confirmed row) and the recovery-code first-mint vs. subsequent-
// verify behaviour are both exercised here.
//
// Reuses fakeSudoQueries from handle_sudo_test.go for the DB surface. The
// fake satisfies meTOTPFlowQueries via its GetTOTPCredential method, so we
// just wire it onto s.meTOTPFlowOverride alongside the sudo override.

package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

// withMeTOTPOverride wires the existing fakeSudoQueries onto
// s.meTOTPFlowOverride so /me/totp/* handlers read from it.
func withMeTOTPOverride(s *Server, f *fakeSudoQueries) {
	s.meTOTPFlowOverride = f
	s.enrollmentTxRunnerOverride = &fakePwdTOTPTxRunner{q: f}
}

func meTOTPBegin(t *testing.T, s *Server, sess *authn.Session) string {
	t.Helper()
	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/totp/begin", "")
	w := httptest.NewRecorder()
	s.handleMeTOTPBeginHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("begin status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	secret, _ := body["secret_base32"].(string)
	if secret == "" {
		t.Fatalf("begin response missing secret_base32: %v", body)
	}
	return secret
}

func meTOTPVerify(t *testing.T, s *Server, sess *authn.Session, code string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"code":%q}`, code)
	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/totp/verify", body)
	w := httptest.NewRecorder()
	s.handleMeTOTPVerifyHTTP(w, r)
	return w
}

func TestMeTOTPBegin_FirstTimeNoSudoRequired(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	_, sess := issueSudoTestSession(t, s, accountID)
	// No TOTP row exists — totpRequiresSudo should fall through without
	// gating, and the handler should mint a new secret.

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/totp/begin", "")
	w := httptest.NewRecorder()
	s.handleMeTOTPBeginHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	if _, ok := body["secret_base32"].(string); !ok {
		t.Errorf("response missing secret_base32: %v", body)
	}
	if uri, _ := body["otpauth_uri"].(string); uri == "" {
		t.Errorf("response missing otpauth_uri")
	}
	if f.totpRow != nil {
		t.Errorf("begin must not insert a totp row, got %+v", f.totpRow)
	}
	if _, err := s.kvStore.Get(context.Background(), meTOTPCeremonyKey(sess)); err != nil {
		t.Errorf("begin did not store the candidate: %v", err)
	}
}

func TestMeTOTPBegin_UnconfirmedNoSudoRequired(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	// Seed an unconfirmed row directly.
	f.totpRow = &db.TotpCredential{AccountID: accountID, ConfirmedAt: pgtype.Timestamptz{}}
	_, sess := issueSudoTestSession(t, s, accountID)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/totp/begin", "")
	w := httptest.NewRecorder()
	s.handleMeTOTPBeginHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestMeTOTPBegin_ConfirmedRequiresSudo(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	_, sess := issueSudoTestSession(t, s, accountID)
	// Backdate IssuedAt so the recent-auth window doesn't apply; no SudoUntil
	// set — re-enroll must fail.
	sess.Data.IssuedAt = time.Now().Add(-(s.config.Auth.SudoTTL + time.Minute))

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/totp/begin", "")
	w := httptest.NewRecorder()
	s.handleMeTOTPBeginHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	if body["code"] != "sudo_required" {
		t.Errorf("code: want sudo_required, got %v", body["code"])
	}
}

func TestMeTOTPBegin_ConfirmedWithSudo(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/totp/begin", "")
	w := httptest.NewRecorder()
	s.handleMeTOTPBeginHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	oldSecret := append([]byte(nil), f.totpRow.SecretEnc...)
	oldRecovery := append([]db.RecoveryCode(nil), f.recoveryRows...)
	meTOTPBegin(t, s, sess)
	if f.totpRow == nil || !f.totpRow.ConfirmedAt.Valid || !slices.Equal(f.totpRow.SecretEnc, oldSecret) {
		t.Errorf("begin changed the confirmed totp credential: %+v", f.totpRow)
	}
	if !slices.Equal(f.recoveryRows, oldRecovery) {
		t.Errorf("begin changed recovery codes")
	}
}

func TestMeTOTPVerify_FirstSuccessReturnsRecoveryCodes(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	_, sess := issueSudoTestSession(t, s, accountID)
	secret := meTOTPBegin(t, s, sess)
	w := meTOTPVerify(t, s, sess, codeForSecret(t, secret))
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	decoded := decodeJSON(t, w.Body.Bytes())
	codes, ok := decoded["recovery_codes"].([]any)
	if !ok {
		t.Fatalf("missing recovery_codes: %v", decoded)
	}
	if len(codes) != 10 {
		t.Errorf("recovery_codes: want 10, got %d", len(codes))
	}
	if f.totpRow == nil || !f.totpRow.ConfirmedAt.Valid {
		t.Errorf("first-verify should confirm the row")
	}
}

func TestMeTOTPVerify_ReplacesExistingFactors(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	oldSecret := append([]byte(nil), decryptTOTPSecret(t, dek, *f.totpRow, accountID)...)
	oldCodes := append([]db.RecoveryCode(nil), f.recoveryRows...)
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	secret := meTOTPBegin(t, s, sess)
	w := meTOTPVerify(t, s, sess, codeForSecret(t, secret))
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	codes, ok := decodeJSON(t, w.Body.Bytes())["recovery_codes"].([]any)
	if !ok || len(codes) != 10 {
		t.Fatalf("recovery_codes: want 10 entries, got %v", codes)
	}
	if got := decryptTOTPSecret(t, dek, *f.totpRow, accountID); slices.Equal(got, oldSecret) {
		t.Error("successful verify did not replace the totp secret")
	}
	if slices.Equal(f.recoveryRows, oldCodes) {
		t.Error("successful verify did not replace recovery codes")
	}
}

func TestMeTOTPVerify_WrongCode(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)
	oldSecret := append([]byte(nil), f.totpRow.SecretEnc...)
	oldCodes := append([]db.RecoveryCode(nil), f.recoveryRows...)
	secret := meTOTPBegin(t, s, sess)

	w := meTOTPVerify(t, s, sess, "000000")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	if body["code"] != "bad_credentials" {
		t.Errorf("code: want bad_credentials, got %v", body["code"])
	}
	if !slices.Equal(f.totpRow.SecretEnc, oldSecret) || !slices.Equal(f.recoveryRows, oldCodes) {
		t.Error("wrong code changed existing credentials")
	}
	if _, err := s.kvStore.Get(context.Background(), meTOTPCeremonyKey(sess)); err != nil {
		t.Fatalf("wrong code consumed the candidate: %v", err)
	}
	w = meTOTPVerify(t, s, sess, codeForSecret(t, secret))
	if w.Code != http.StatusOK {
		t.Fatalf("retry with correct code: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestMeTOTPVerify_EmptyCode(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	// No TOTP row → no sudo required.
	_, sess := issueSudoTestSession(t, s, accountID)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/totp/verify", `{"code":""}`)
	w := httptest.NewRecorder()
	s.handleMeTOTPVerifyHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d", w.Code)
	}
}

func TestMeTOTPVerify_MissingCandidate(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	w := meTOTPVerify(t, s, sess, "000000")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	if body["code"] != "ceremony_expired" {
		t.Errorf("code: want ceremony_expired, got %v", body["code"])
	}
}

func TestMeTOTPVerify_CandidateIsBoundToSession(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	_, ownerSession := issueSudoTestSession(t, s, accountID)
	secret := meTOTPBegin(t, s, ownerSession)
	_, otherSession := issueSudoTestSession(t, s, accountID)

	w := meTOTPVerify(t, s, otherSession, codeForSecret(t, secret))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
	if got := decodeJSON(t, w.Body.Bytes())["code"]; got != "ceremony_expired" {
		t.Errorf("code: want ceremony_expired, got %v", got)
	}
	if _, err := s.kvStore.Get(context.Background(), meTOTPCeremonyKey(ownerSession)); err != nil {
		t.Fatalf("other session consumed the candidate: %v", err)
	}
}

func TestMeTOTPVerify_CommitFailurePreservesExistingFactorsAndCandidate(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	oldTOTP := *f.totpRow
	oldTOTP.SecretEnc = append([]byte(nil), oldTOTP.SecretEnc...)
	oldTOTP.SecretNonce = append([]byte(nil), oldTOTP.SecretNonce...)
	oldCodes := append([]db.RecoveryCode(nil), f.recoveryRows...)
	oldEventCount := len(f.events)

	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)
	secret := meTOTPBegin(t, s, sess)
	runner := s.enrollmentTxRunnerOverride.(*fakePwdTOTPTxRunner)
	runner.commitErr = errors.New("commit failed")

	w := meTOTPVerify(t, s, sess, codeForSecret(t, secret))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status: want 500, got %d (body=%s)", w.Code, w.Body.String())
	}
	if f.totpRow == nil || !slices.Equal(f.totpRow.SecretEnc, oldTOTP.SecretEnc) ||
		!slices.Equal(f.totpRow.SecretNonce, oldTOTP.SecretNonce) || !f.totpRow.ConfirmedAt.Valid {
		t.Errorf("commit failure did not restore totp credential: %+v", f.totpRow)
	}
	if !slices.Equal(f.recoveryRows, oldCodes) {
		t.Error("commit failure did not restore recovery codes")
	}
	if len(f.events) != oldEventCount {
		t.Errorf("commit failure emitted audit events: before=%d after=%d", oldEventCount, len(f.events))
	}
	if _, err := s.kvStore.Get(context.Background(), meTOTPCeremonyKey(sess)); err != nil {
		t.Fatalf("commit failure consumed the candidate: %v", err)
	}
}

type blockingEnrollmentTxRunner struct {
	inner   enrollmentTxRunner
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *blockingEnrollmentTxRunner) BeginEnrollmentTx(ctx context.Context) (enrollmentTx, error) {
	r.once.Do(func() { close(r.started) })
	select {
	case <-r.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return r.inner.BeginEnrollmentTx(ctx)
}

func TestMeTOTPVerify_ConcurrentRequestCannotReuseCandidate(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)
	secret := meTOTPBegin(t, s, sess)
	code := codeForSecret(t, secret)

	blocking := &blockingEnrollmentTxRunner{
		inner: s.enrollmentTxRunnerOverride, started: make(chan struct{}), release: make(chan struct{}),
	}
	s.enrollmentTxRunnerOverride = blocking
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() { firstDone <- meTOTPVerify(t, s, sess, code) }()
	<-blocking.started

	second := meTOTPVerify(t, s, sess, code)
	if second.Code != http.StatusBadRequest {
		t.Fatalf("second status: want 400, got %d (body=%s)", second.Code, second.Body.String())
	}
	if got := decodeJSON(t, second.Body.Bytes())["code"]; got != "ceremony_expired" {
		t.Errorf("second code: want ceremony_expired, got %v", got)
	}

	close(blocking.release)
	first := <-firstDone
	if first.Code != http.StatusOK {
		t.Fatalf("first status: want 200, got %d (body=%s)", first.Code, first.Body.String())
	}
}

func TestMeRegenerateRecoveryCodes_RequiresSudo(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	_ = seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	_, sess := issueSudoTestSession(t, s, accountID)
	// Backdate IssuedAt so the recent-auth window doesn't apply; no SudoUntil
	// set, so the gate must deny with sudo_required.
	sess.Data.IssuedAt = time.Now().Add(-(s.config.Auth.SudoTTL + time.Minute))

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/recovery-codes/regenerate", "")
	w := httptest.NewRecorder()
	s.handleMeRegenerateRecoveryCodesHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: want 401, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	if body["code"] != "sudo_required" {
		t.Errorf("code: want sudo_required, got %v", body["code"])
	}
}

func TestMeRegenerateRecoveryCodes_RequiresConfirmedTOTP(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	// No TOTP row at all. Even with sudo, the precondition rejects.
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/recovery-codes/regenerate", "")
	w := httptest.NewRecorder()
	s.handleMeRegenerateRecoveryCodesHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestMeRegenerateRecoveryCodes_UnconfirmedTOTPRejected(t *testing.T) {
	s, f, _ := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	// Unconfirmed row — same rejection path as missing.
	f.totpRow = &db.TotpCredential{AccountID: accountID}
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/recovery-codes/regenerate", "")
	w := httptest.NewRecorder()
	s.handleMeRegenerateRecoveryCodesHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: want 400, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestMeRegenerateRecoveryCodes_Success(t *testing.T) {
	s, f, dek := newSudoTestServer(t)
	withMeTOTPOverride(s, f)
	const accountID int32 = 42
	originalCodes := seedConfirmedTOTPSudo(t, s, f, dek, accountID)
	token, sess := issueSudoTestSession(t, s, accountID)
	grantFreshSudo(t, s, accountID, token)
	sess.Data.SudoUntil = time.Now().Add(5 * time.Minute)

	r := sudoReq(t, sess, http.MethodPost, "/api/prohibitorum/me/recovery-codes/regenerate", "")
	w := httptest.NewRecorder()
	s.handleMeRegenerateRecoveryCodesHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
	body := decodeJSON(t, w.Body.Bytes())
	codes, ok := body["recovery_codes"].([]any)
	if !ok {
		t.Fatalf("recovery_codes missing: %v", body)
	}
	if len(codes) != 10 {
		t.Errorf("recovery_codes: want 10, got %d", len(codes))
	}
	// New codes must differ from the seed set.
	overlap := 0
	for _, c := range codes {
		got := c.(string)
		for _, o := range originalCodes {
			if got == o {
				overlap++
			}
		}
	}
	if overlap > 0 {
		t.Errorf("regenerated codes overlap with originals (%d matches)", overlap)
	}
	// Storage rowset should now be 10 unused, no leftover used-but-active
	// rows from the prior set.
	live, _ := f.ListRecoveryCodesByAccount(context.Background(), accountID)
	if len(live) != 10 {
		t.Errorf("live recovery rows: want 10, got %d", len(live))
	}
}
