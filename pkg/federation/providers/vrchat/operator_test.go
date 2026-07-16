package vrchat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
	"prohibitorum/pkg/kv"
)

const testUserID = "usr_12345678-1234-1234-1234-123456789abc"

type operatorFakeClient struct {
	mu                                   sync.Mutex
	authUser                             CurrentUser
	authCookies                          []http.Cookie
	authErr                              error
	currentUser                          CurrentUser
	currentCookies                       []http.Cookie
	currentErr                           error
	verifyErr                            error
	currentHook, verifyHook              func()
	authCalls, currentCalls, verifyCalls int
	lastMethod, lastCode                 string
}

func (f *operatorFakeClient) Authenticate(context.Context, string, string) (CurrentUser, []http.Cookie, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authCalls++
	return f.authUser, cloneOperatorCookies(f.authCookies), f.authErr
}
func (f *operatorFakeClient) CurrentUser(context.Context, []http.Cookie) (CurrentUser, []http.Cookie, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.currentCalls++
	if f.currentHook != nil {
		f.currentHook()
	}
	return f.currentUser, cloneOperatorCookies(f.currentCookies), f.currentErr
}
func (f *operatorFakeClient) VerifyTwoFactor(_ context.Context, method, code string, cookies []http.Cookie) ([]http.Cookie, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.verifyCalls++
	f.lastMethod, f.lastCode = method, code
	if f.verifyHook != nil {
		f.verifyHook()
	}
	return cloneOperatorCookies(cookies), f.verifyErr
}
func (f *operatorFakeClient) EncodeCookies(c []http.Cookie) ([]byte, error) { return encodeCookies(c) }
func (f *operatorFakeClient) DecodeCookies(b []byte, now time.Time) ([]http.Cookie, error) {
	return decodeCookies(b, now)
}

func cloneOperatorCookies(in []http.Cookie) []http.Cookie { return append([]http.Cookie(nil), in...) }
func operatorCookie(name, value string, expires time.Time) http.Cookie {
	return http.Cookie{Name: name, Value: value, Path: "/", Expires: expires, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode}
}

type operatorFakeQueries struct {
	mu                           sync.Mutex
	row                          db.UpstreamIdp
	getErr, healthErr, secretErr error
	secretUpdates, healthUpdates int
}

func (q *operatorFakeQueries) GetUpstreamIDPBySlugAny(context.Context, string) (db.UpstreamIdp, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.getErr != nil {
		return db.UpstreamIdp{}, q.getErr
	}
	return q.row, nil
}
func (q *operatorFakeQueries) UpdateVRChatOperatorSecret(_ context.Context, p db.UpdateVRChatOperatorSecretParams) (db.UpstreamIdp, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.secretErr != nil {
		return db.UpstreamIdp{}, q.secretErr
	}
	q.secretUpdates++
	if q.row.ID != p.ID || q.row.Slug != p.Slug || q.row.Protocol != "vrchat" {
		return db.UpstreamIdp{}, pgx.ErrNoRows
	}
	q.row.SecretEnc, q.row.SecretNonce, q.row.KeyVersion = append([]byte(nil), p.SecretEnc...), append([]byte(nil), p.SecretNonce...), p.KeyVersion
	q.row.SecretStatus, q.row.SecretValidatedAt = "valid", p.SecretValidatedAt
	return q.row, nil
}
func (q *operatorFakeQueries) RefreshVRChatOperatorSecret(_ context.Context, p db.RefreshVRChatOperatorSecretParams) (db.UpstreamIdp, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.secretErr != nil {
		return db.UpstreamIdp{}, q.secretErr
	}
	if q.row.ID != p.ID || q.row.Slug != p.Slug || q.row.Protocol != "vrchat" ||
		!bytes.Equal(q.row.SecretEnc, p.ExpectedSecretEnc) ||
		!bytes.Equal(q.row.SecretNonce, p.ExpectedSecretNonce) ||
		q.row.KeyVersion != p.ExpectedKeyVersion {
		return db.UpstreamIdp{}, pgx.ErrNoRows
	}
	q.secretUpdates++
	q.row.SecretEnc, q.row.SecretNonce, q.row.KeyVersion = append([]byte(nil), p.NewSecretEnc...), append([]byte(nil), p.NewSecretNonce...), p.NewKeyVersion
	q.row.SecretStatus, q.row.SecretValidatedAt = "valid", p.SecretValidatedAt
	return q.row, nil
}
func (q *operatorFakeQueries) InvalidateVRChatOperatorSecret(_ context.Context, p db.InvalidateVRChatOperatorSecretParams) (db.UpstreamIdp, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.healthUpdates++
	if q.healthErr != nil {
		return db.UpstreamIdp{}, q.healthErr
	}
	if q.row.ID != p.ID || q.row.Slug != p.Slug || q.row.Protocol != "vrchat" ||
		!bytes.Equal(q.row.SecretEnc, p.ExpectedSecretEnc) ||
		!bytes.Equal(q.row.SecretNonce, p.ExpectedSecretNonce) ||
		q.row.KeyVersion != p.ExpectedKeyVersion {
		return db.UpstreamIdp{}, pgx.ErrNoRows
	}
	q.row.SecretStatus = "invalid"
	return q.row, nil
}

func operatorProvider() db.UpstreamIdp {
	return db.UpstreamIdp{ID: 42, Slug: "social", DisplayName: "VRChat", Protocol: "vrchat", Mode: "link_only", ProviderConfig: []byte(`{}`), SecretStatus: "unconfigured", Disabled: true}
}
func operatorService(t *testing.T, client *operatorFakeClient, q *operatorFakeQueries, now time.Time) (*OperatorService, kv.Store) {
	t.Helper()
	store := kv.NewMemoryStore()
	service := NewOperatorService(client, q, store, federation.NewSecretStore(map[int][]byte{3: make([]byte, 32)}), 3)
	service.now = func() time.Time { return now }
	service.random = &repeatingReader{value: 0x5a}
	return service, store
}

type repeatingReader struct{ value byte }

func (r *repeatingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.value
	}
	return len(p), nil
}

type restoreFailKV struct {
	kv.Store
	calls       int
	failAt      int
	transitions []string
}

func (s *restoreFailKV) CompareAndSwap(ctx context.Context, key, oldValue, newValue string, ttl time.Duration) (bool, error) {
	s.calls++
	var oldState, newState struct {
		State string `json:"state"`
	}
	_ = json.Unmarshal([]byte(oldValue), &oldState)
	_ = json.Unmarshal([]byte(newValue), &newState)
	s.transitions = append(s.transitions, oldState.State+"->"+newState.State)
	failAt := s.failAt
	if failAt == 0 {
		failAt = 2
	}
	if s.calls == failAt {
		return false, errors.New("kv unavailable")
	}
	return s.Store.CompareAndSwap(ctx, key, oldValue, newValue, ttl)
}

type canceledContextKV struct {
	kv.Store
	calls              int
	allowCanceledUntil int
	recoveryTTL        time.Duration
}

func (s *canceledContextKV) CompareAndSwap(ctx context.Context, key, oldValue, newValue string, ttl time.Duration) (bool, error) {
	s.calls++
	if s.calls > s.allowCanceledUntil {
		s.recoveryTTL = ttl
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
	}
	return s.Store.CompareAndSwap(ctx, key, oldValue, newValue, ttl)
}

func TestOperatorStartPersistsFullSessionWithoutChallenge(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.FixedZone("offset", 3600))
	client := &operatorFakeClient{authUser: CurrentUser{ID: testUserID, DisplayName: "Operator"}, authCookies: []http.Cookie{operatorCookie("auth", "secret-cookie", now.Add(24*time.Hour))}}
	q := &operatorFakeQueries{row: operatorProvider()}
	service, _ := operatorService(t, client, q, now)
	result, err := service.Start(context.Background(), "social", 7, "session-a", "private-user", "private-password")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != OperatorStatusValid || result.Provider == nil || result.Challenge != "" || result.Methods != nil || result.ExpiresAt != nil {
		t.Fatalf("result leaked challenge fields: %+v", result)
	}
	if client.authCalls != 1 || client.currentCalls != 0 || q.secretUpdates != 1 {
		t.Fatalf("calls auth=%d current=%d updates=%d", client.authCalls, client.currentCalls, q.secretUpdates)
	}
	if q.row.SecretStatus != "valid" || !q.row.SecretValidatedAt.Valid || !q.row.SecretValidatedAt.Time.Equal(now.UTC()) {
		t.Fatalf("health=%s at=%v", q.row.SecretStatus, q.row.SecretValidatedAt)
	}
	opened, err := federation.NewSecretStore(map[int][]byte{3: make([]byte, 32)}).OpenProviderSecret(federation.SealedSecret{Ciphertext: q.row.SecretEnc, Nonce: q.row.SecretNonce, KeyVersion: q.row.KeyVersion.Int32}, 42)
	if err != nil || string(opened) == "secret-cookie" {
		t.Fatalf("persisted envelope invalid/plaintext: %q %v", opened, err)
	}
}

func TestOperatorStartStoresSealedChallengeForEverySupportedMethod(t *testing.T) {
	for _, methods := range [][]string{{"totp"}, {"emailOtp"}, {"otp"}, {"totp", "emailOtp", "otp"}} {
		t.Run(methods[0], func(t *testing.T) {
			now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
			client := &operatorFakeClient{authUser: CurrentUser{RequiresTwoFactorAuth: methods}, authCookies: []http.Cookie{operatorCookie("auth", "temporary-secret", now.Add(24*time.Hour))}}
			q := &operatorFakeQueries{row: operatorProvider()}
			service, store := operatorService(t, client, q, now)
			result, err := service.Start(context.Background(), "social", 7, "session-a", "user", "pass")
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != OperatorStatusChallenge || len(result.Challenge) != 43 || result.Provider != nil || result.ExpiresAt == nil || !result.ExpiresAt.Equal(now.Add(10*time.Minute)) {
				t.Fatalf("result=%+v", result)
			}
			raw, err := store.Get(context.Background(), operatorChallengeKey(result.Challenge))
			if err != nil {
				t.Fatal(err)
			}
			if containsAny(raw, "temporary-secret", "user", "pass") {
				t.Fatalf("KV contains plaintext secret: %q", raw)
			}
			state, err := decodeOperatorChallenge(raw)
			if err != nil {
				t.Fatal(err)
			}
			if state.AccountID != 7 || state.SessionID != "session-a" || state.ProviderID != 42 || state.ProviderSlug != "social" || state.Protocol != "vrchat" {
				t.Fatalf("bindings=%+v", state)
			}
			secrets := federation.NewSecretStore(map[int][]byte{3: make([]byte, 32)})
			if _, err := secrets.OpenTemporary(state.Secret, 42, result.Challenge); err != nil {
				t.Fatalf("temporary AAD: %v", err)
			}
			if _, err := secrets.OpenTemporary(state.Secret, 43, result.Challenge); err == nil {
				t.Fatal("temporary secret opened with wrong provider ID")
			}
		})
	}
}
func containsAny(value string, needles ...string) bool {
	for _, n := range needles {
		if n != "" && len(value) >= len(n) {
			for i := 0; i+len(n) <= len(value); i++ {
				if value[i:i+len(n)] == n {
					return true
				}
			}
		}
	}
	return false
}

func TestOperatorVerifyWrongCodeRetryThenSuccessAndReplayFails(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	client := &operatorFakeClient{authUser: CurrentUser{RequiresTwoFactorAuth: []string{"totp"}}, authCookies: []http.Cookie{operatorCookie("auth", "temporary", now.Add(24*time.Hour))}}
	q := &operatorFakeQueries{row: operatorProvider()}
	service, _ := operatorService(t, client, q, now)
	start, err := service.Start(context.Background(), "social", 7, "session-a", "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	client.verifyErr = &VerificationError{}
	if _, err = service.Verify(context.Background(), "social", 7, "session-a", start.Challenge, "totp", "wrong"); OperatorErrorCategory(err) != OperatorCategoryCodeInvalid {
		t.Fatalf("wrong-code category=%v", err)
	}
	client.verifyErr = nil
	client.currentUser = CurrentUser{ID: testUserID, DisplayName: "Operator"}
	client.currentCookies = []http.Cookie{operatorCookie("auth", "final", now.Add(24*time.Hour)), operatorCookie("twoFactorAuth", "factor", now.Add(24*time.Hour))}
	got, err := service.Verify(context.Background(), "social", 7, "session-a", start.Challenge, "totp", "right")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != OperatorStatusValid || q.secretUpdates != 1 {
		t.Fatalf("result=%+v updates=%d", got, q.secretUpdates)
	}
	if _, err = service.Verify(context.Background(), "social", 7, "session-a", start.Challenge, "totp", "right"); OperatorErrorCategory(err) != OperatorCategoryChallengeInvalid {
		t.Fatalf("replay=%v", err)
	}
}

func TestOperatorVerifyReportsKVFailureWhenRetryStateCannotBeRestored(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	client := &operatorFakeClient{
		authUser:    CurrentUser{RequiresTwoFactorAuth: []string{"totp"}},
		authCookies: []http.Cookie{operatorCookie("auth", "temporary", now.Add(24*time.Hour))},
		verifyErr:   &VerificationError{},
	}
	q := &operatorFakeQueries{row: operatorProvider()}
	service, store := operatorService(t, client, q, now)
	start, err := service.Start(context.Background(), "social", 7, "session-a", "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	service.kv = &restoreFailKV{Store: store}
	_, err = service.Verify(context.Background(), "social", 7, "session-a", start.Challenge, "totp", "wrong")
	if OperatorErrorCategory(err) != OperatorCategoryKVUnavailable {
		t.Fatalf("category = %q, want kv_unavailable", OperatorErrorCategory(err))
	}
}

func TestOperatorVerifyFinalizesChallengeBeforeDatabaseUpdate(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	client := &operatorFakeClient{
		authUser:       CurrentUser{RequiresTwoFactorAuth: []string{"totp"}},
		authCookies:    []http.Cookie{operatorCookie("auth", "temporary", now.Add(24*time.Hour))},
		currentUser:    CurrentUser{ID: testUserID, DisplayName: "Operator"},
		currentCookies: []http.Cookie{operatorCookie("auth", "final", now.Add(24*time.Hour))},
	}
	q := &operatorFakeQueries{row: operatorProvider()}
	service, store := operatorService(t, client, q, now)
	start, err := service.Start(context.Background(), "social", 7, "session-a", "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	recording := &restoreFailKV{Store: store, failAt: 2}
	service.kv = recording
	_, err = service.Verify(context.Background(), "social", 7, "session-a", start.Challenge, "totp", "code")
	if OperatorErrorCategory(err) != OperatorCategoryKVUnavailable || q.secretUpdates != 0 {
		t.Fatalf("err=%v database_updates=%d", err, q.secretUpdates)
	}
	if !reflect.DeepEqual(recording.transitions, []string{"ready->verifying", "verifying->consumed"}) {
		t.Fatalf("transitions=%v", recording.transitions)
	}
}

func TestOperatorVerifyExpiryBeforeFinalizationSkipsDatabaseUpdate(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	currentNow := now
	client := &operatorFakeClient{
		authUser:       CurrentUser{RequiresTwoFactorAuth: []string{"totp"}},
		authCookies:    []http.Cookie{operatorCookie("auth", "temporary", now.Add(24*time.Hour))},
		currentUser:    CurrentUser{ID: testUserID, DisplayName: "Operator"},
		currentCookies: []http.Cookie{operatorCookie("auth", "final", now.Add(24*time.Hour))},
		currentHook:    func() { currentNow = now.Add(11 * time.Minute) },
	}
	q := &operatorFakeQueries{row: operatorProvider()}
	service, _ := operatorService(t, client, q, now)
	service.now = func() time.Time { return currentNow }
	start, err := service.Start(context.Background(), "social", 7, "session-a", "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Verify(context.Background(), "social", 7, "session-a", start.Challenge, "totp", "code")
	if OperatorErrorCategory(err) != OperatorCategoryChallengeInvalid || q.secretUpdates != 0 {
		t.Fatalf("err=%v database_updates=%d", err, q.secretUpdates)
	}
}

func TestOperatorVerifyDatabaseFailureRestoresConsumedChallenge(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	client := &operatorFakeClient{
		authUser:       CurrentUser{RequiresTwoFactorAuth: []string{"totp"}},
		authCookies:    []http.Cookie{operatorCookie("auth", "temporary", now.Add(24*time.Hour))},
		currentUser:    CurrentUser{ID: testUserID, DisplayName: "Operator"},
		currentCookies: []http.Cookie{operatorCookie("auth", "final", now.Add(24*time.Hour))},
	}
	q := &operatorFakeQueries{row: operatorProvider(), secretErr: errors.New("database unavailable")}
	service, store := operatorService(t, client, q, now)
	start, err := service.Start(context.Background(), "social", 7, "session-a", "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	recording := &restoreFailKV{Store: store, failAt: 99}
	service.kv = recording
	_, err = service.Verify(context.Background(), "social", 7, "session-a", start.Challenge, "totp", "code")
	if OperatorErrorCategory(err) != OperatorCategoryDatabaseUnavailable || q.secretUpdates != 0 {
		t.Fatalf("err=%v database_updates=%d", err, q.secretUpdates)
	}
	if !reflect.DeepEqual(recording.transitions, []string{"ready->verifying", "verifying->consumed", "consumed->ready"}) {
		t.Fatalf("transitions=%v", recording.transitions)
	}
	raw, err := store.Get(context.Background(), operatorChallengeKey(start.Challenge))
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeOperatorChallenge(raw)
	if err != nil || state.State != "ready" {
		t.Fatalf("restored state=%+v err=%v", state, err)
	}
	q.secretErr = nil
	if _, err = service.Verify(context.Background(), "social", 7, "session-a", start.Challenge, "totp", "code"); err != nil {
		t.Fatalf("restored challenge did not retry: %v", err)
	}
	if q.secretUpdates != 1 {
		t.Fatalf("database_updates=%d", q.secretUpdates)
	}
}

func TestOperatorVerifyDatabaseFailureRestoreFailureIsKVUnavailable(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	client := &operatorFakeClient{
		authUser:       CurrentUser{RequiresTwoFactorAuth: []string{"totp"}},
		authCookies:    []http.Cookie{operatorCookie("auth", "temporary", now.Add(24*time.Hour))},
		currentUser:    CurrentUser{ID: testUserID, DisplayName: "Operator"},
		currentCookies: []http.Cookie{operatorCookie("auth", "final", now.Add(24*time.Hour))},
	}
	q := &operatorFakeQueries{row: operatorProvider(), secretErr: errors.New("database unavailable")}
	service, store := operatorService(t, client, q, now)
	start, err := service.Start(context.Background(), "social", 7, "session-a", "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	service.kv = &restoreFailKV{Store: store, failAt: 3}
	_, err = service.Verify(context.Background(), "social", 7, "session-a", start.Challenge, "totp", "code")
	if OperatorErrorCategory(err) != OperatorCategoryKVUnavailable || q.secretUpdates != 0 {
		t.Fatalf("err=%v database_updates=%d", err, q.secretUpdates)
	}
}

func TestOperatorVerifyWrongCodeRecoverySurvivesCanceledRequest(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	client := &operatorFakeClient{
		authUser:    CurrentUser{RequiresTwoFactorAuth: []string{"totp"}},
		authCookies: []http.Cookie{operatorCookie("auth", "temporary", now.Add(24*time.Hour))},
		verifyErr:   &VerificationError{},
		verifyHook:  cancel,
	}
	q := &operatorFakeQueries{row: operatorProvider()}
	service, store := operatorService(t, client, q, now)
	start, err := service.Start(context.Background(), "social", 7, "session-a", "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	recoveryKV := &canceledContextKV{Store: store, allowCanceledUntil: 1}
	service.kv = recoveryKV
	_, err = service.Verify(ctx, "social", 7, "session-a", start.Challenge, "totp", "wrong")
	if OperatorErrorCategory(err) != OperatorCategoryCodeInvalid {
		t.Fatalf("category=%q", OperatorErrorCategory(err))
	}
	if recoveryKV.recoveryTTL <= 0 || recoveryKV.recoveryTTL > operatorChallengeTTL {
		t.Fatalf("recovery TTL=%v", recoveryKV.recoveryTTL)
	}
	raw, err := store.Get(context.Background(), operatorChallengeKey(start.Challenge))
	if err != nil {
		t.Fatal(err)
	}
	state, err := decodeOperatorChallenge(raw)
	if err != nil || state.State != "ready" {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestOperatorVerifyDatabaseRecoverySurvivesCanceledRequest(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	client := &operatorFakeClient{
		authUser:       CurrentUser{RequiresTwoFactorAuth: []string{"totp"}},
		authCookies:    []http.Cookie{operatorCookie("auth", "temporary", now.Add(24*time.Hour))},
		currentUser:    CurrentUser{ID: testUserID, DisplayName: "Operator"},
		currentCookies: []http.Cookie{operatorCookie("auth", "final", now.Add(24*time.Hour))},
		currentHook:    cancel,
	}
	q := &operatorFakeQueries{row: operatorProvider(), secretErr: errors.New("database unavailable")}
	service, store := operatorService(t, client, q, now)
	start, err := service.Start(context.Background(), "social", 7, "session-a", "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	recoveryKV := &canceledContextKV{Store: store, allowCanceledUntil: 2}
	service.kv = recoveryKV
	_, err = service.Verify(ctx, "social", 7, "session-a", start.Challenge, "totp", "code")
	if OperatorErrorCategory(err) != OperatorCategoryDatabaseUnavailable {
		t.Fatalf("category=%q", OperatorErrorCategory(err))
	}
	if recoveryKV.recoveryTTL <= 0 || recoveryKV.recoveryTTL > operatorChallengeTTL {
		t.Fatalf("recovery TTL=%v", recoveryKV.recoveryTTL)
	}
}

func TestOperatorVerifyRequiresFullAuthenticatedCurrentUser(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	client := &operatorFakeClient{
		authUser:    CurrentUser{RequiresTwoFactorAuth: []string{"totp"}},
		authCookies: []http.Cookie{operatorCookie("auth", "temporary", now.Add(24*time.Hour))},
		currentUser: CurrentUser{RequiresTwoFactorAuth: []string{"totp"}},
	}
	q := &operatorFakeQueries{row: operatorProvider()}
	service, _ := operatorService(t, client, q, now)
	start, err := service.Start(context.Background(), "social", 7, "session-a", "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Verify(context.Background(), "social", 7, "session-a", start.Challenge, "totp", "code"); OperatorErrorCategory(err) != OperatorCategoryUpstreamTemporarilyUnavailable {
		t.Fatalf("challenge-shaped CurrentUser category = %q", OperatorErrorCategory(err))
	}
	if q.secretUpdates != 0 {
		t.Fatalf("persisted challenge-shaped CurrentUser")
	}
	client.currentUser = CurrentUser{ID: testUserID, DisplayName: "Operator"}
	client.currentCookies = []http.Cookie{operatorCookie("auth", "final", now.Add(24*time.Hour))}
	if _, err = service.Verify(context.Background(), "social", 7, "session-a", start.Challenge, "totp", "code"); err != nil {
		t.Fatalf("challenge was not restored for retry: %v", err)
	}
}

func TestOperatorVerifyRejectsMethodNotOfferedWithoutConsumingChallenge(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	client := &operatorFakeClient{
		authUser:       CurrentUser{RequiresTwoFactorAuth: []string{"totp"}},
		authCookies:    []http.Cookie{operatorCookie("auth", "temporary", now.Add(24*time.Hour))},
		currentUser:    CurrentUser{ID: testUserID, DisplayName: "Operator"},
		currentCookies: []http.Cookie{operatorCookie("auth", "final", now.Add(24*time.Hour))},
	}
	q := &operatorFakeQueries{row: operatorProvider()}
	service, _ := operatorService(t, client, q, now)
	start, err := service.Start(context.Background(), "social", 7, "session-a", "u", "p")
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Verify(context.Background(), "social", 7, "session-a", start.Challenge, "emailOtp", "code")
	if OperatorErrorCategory(err) != OperatorCategoryChallengeInvalid || client.verifyCalls != 0 {
		t.Fatalf("err=%v verify_calls=%d", err, client.verifyCalls)
	}
	if _, err = service.Verify(context.Background(), "social", 7, "session-a", start.Challenge, "totp", "code"); err != nil {
		t.Fatalf("offered method failed after rejection: %v", err)
	}
}

func TestOperatorVerifyClassifiesCodeAndPostVerifyAuthenticationFailures(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name       string
		verifyErr  error
		currentErr error
		want       OperatorCategory
	}{
		{name: "oversize code", verifyErr: &OversizeError{Category: "verification code"}, want: OperatorCategoryBadRequest},
		{name: "current user unauthorized", currentErr: &HTTPError{Status: http.StatusUnauthorized, Category: "authentication"}, want: OperatorCategoryCodeInvalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &operatorFakeClient{
				authUser:    CurrentUser{RequiresTwoFactorAuth: []string{"totp"}},
				authCookies: []http.Cookie{operatorCookie("auth", "temporary", now.Add(24*time.Hour))},
				verifyErr:   test.verifyErr,
				currentErr:  test.currentErr,
			}
			q := &operatorFakeQueries{row: operatorProvider()}
			service, _ := operatorService(t, client, q, now)
			start, err := service.Start(context.Background(), "social", 7, "session-a", "u", "p")
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.Verify(context.Background(), "social", 7, "session-a", start.Challenge, "totp", "code")
			if OperatorErrorCategory(err) != test.want {
				t.Fatalf("category = %q, want %q", OperatorErrorCategory(err), test.want)
			}
		})
	}
}

func TestOperatorVerifyRejectsBindingExpiryAADAndConcurrentUse(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	newChallenge := func(t *testing.T) (*OperatorService, *operatorFakeClient, *operatorFakeQueries, OperatorSessionResult) {
		c := &operatorFakeClient{authUser: CurrentUser{RequiresTwoFactorAuth: []string{"totp"}}, authCookies: []http.Cookie{operatorCookie("auth", "temporary", now.Add(24*time.Hour))}, currentUser: CurrentUser{ID: testUserID, DisplayName: "Operator"}, currentCookies: []http.Cookie{operatorCookie("auth", "final", now.Add(24*time.Hour))}}
		q := &operatorFakeQueries{row: operatorProvider()}
		s, _ := operatorService(t, c, q, now)
		r, e := s.Start(context.Background(), "social", 7, "session-a", "u", "p")
		if e != nil {
			t.Fatal(e)
		}
		return s, c, q, r
	}
	for name, mutate := range map[string]func(*string, *int32, *string){"provider": func(slug *string, _ *int32, _ *string) { *slug = "other" }, "account": func(_ *string, a *int32, _ *string) { *a = 8 }, "session": func(_ *string, _ *int32, s *string) { *s = "session-b" }} {
		t.Run(name, func(t *testing.T) {
			svc, c, _, r := newChallenge(t)
			slug, account, session := "social", int32(7), "session-a"
			mutate(&slug, &account, &session)
			_, err := svc.Verify(context.Background(), slug, account, session, r.Challenge, "totp", "x")
			if OperatorErrorCategory(err) != OperatorCategoryChallengeInvalid || c.verifyCalls != 0 {
				t.Fatalf("err=%v calls=%d", err, c.verifyCalls)
			}
		})
	}
	t.Run("expired", func(t *testing.T) {
		svc, c, _, r := newChallenge(t)
		svc.now = func() time.Time { return now.Add(11 * time.Minute) }
		_, err := svc.Verify(context.Background(), "social", 7, "session-a", r.Challenge, "totp", "x")
		if OperatorErrorCategory(err) != OperatorCategoryChallengeInvalid || c.verifyCalls != 0 {
			t.Fatalf("err=%v calls=%d", err, c.verifyCalls)
		}
	})
	t.Run("provider-id-metadata-mismatch", func(t *testing.T) {
		svc, c, _, r := newChallenge(t)
		raw, _ := svc.kv.Get(context.Background(), operatorChallengeKey(r.Challenge))
		state, _ := decodeOperatorChallenge(raw)
		state.ProviderID = 43
		tampered, _ := encodeOperatorChallenge(state)
		_ = svc.kv.SetEx(context.Background(), operatorChallengeKey(r.Challenge), tampered, time.Minute)
		_, err := svc.Verify(context.Background(), "social", 7, "session-a", r.Challenge, "totp", "x")
		if OperatorErrorCategory(err) != OperatorCategoryChallengeInvalid || c.verifyCalls != 0 {
			t.Fatalf("err=%v calls=%d", err, c.verifyCalls)
		}
	})
	t.Run("ciphertext", func(t *testing.T) {
		svc, c, _, r := newChallenge(t)
		raw, _ := svc.kv.Get(context.Background(), operatorChallengeKey(r.Challenge))
		state, _ := decodeOperatorChallenge(raw)
		state.Secret.Ciphertext[0] ^= 0xff
		tampered, _ := encodeOperatorChallenge(state)
		_ = svc.kv.SetEx(context.Background(), operatorChallengeKey(r.Challenge), tampered, time.Minute)
		_, err := svc.Verify(context.Background(), "social", 7, "session-a", r.Challenge, "totp", "x")
		if OperatorErrorCategory(err) != OperatorCategoryChallengeInvalid || c.verifyCalls != 0 {
			t.Fatalf("err=%v calls=%d", err, c.verifyCalls)
		}
	})
	t.Run("provider-recreated", func(t *testing.T) {
		svc, c, q, r := newChallenge(t)
		q.row.ID++
		_, err := svc.Verify(context.Background(), "social", 7, "session-a", r.Challenge, "totp", "x")
		if OperatorErrorCategory(err) != OperatorCategoryChallengeInvalid || c.verifyCalls != 0 {
			t.Fatalf("err=%v calls=%d", err, c.verifyCalls)
		}
	})
	t.Run("protocol-swapped", func(t *testing.T) {
		svc, c, q, r := newChallenge(t)
		q.row.Protocol = "oidc"
		_, err := svc.Verify(context.Background(), "social", 7, "session-a", r.Challenge, "totp", "x")
		if OperatorErrorCategory(err) != OperatorCategoryChallengeInvalid || c.verifyCalls != 0 {
			t.Fatalf("err=%v calls=%d", err, c.verifyCalls)
		}
	})
	t.Run("concurrent", func(t *testing.T) {
		svc, _, q, r := newChallenge(t)
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, e := svc.Verify(context.Background(), "social", 7, "session-a", r.Challenge, "totp", "x")
				results <- e
			}()
		}
		wg.Wait()
		close(results)
		success := 0
		for e := range results {
			if e == nil {
				success++
			}
		}
		if success != 1 || q.secretUpdates != 1 {
			t.Fatalf("success=%d updates=%d", success, q.secretUpdates)
		}
	})
}

func TestOperatorValidateRefreshInvalidationAndTransientErrors(t *testing.T) {
	now := time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)
	makeValid := func(t *testing.T) (*OperatorService, *operatorFakeClient, *operatorFakeQueries) {
		c := &operatorFakeClient{authUser: CurrentUser{ID: testUserID, DisplayName: "Operator"}, authCookies: []http.Cookie{operatorCookie("auth", "old", now.Add(24*time.Hour))}}
		q := &operatorFakeQueries{row: operatorProvider()}
		s, _ := operatorService(t, c, q, now)
		if _, e := s.Start(context.Background(), "social", 7, "sid", "u", "p"); e != nil {
			t.Fatal(e)
		}
		q.secretUpdates = 0
		return s, c, q
	}
	t.Run("refresh", func(t *testing.T) {
		s, c, q := makeValid(t)
		c.currentUser = CurrentUser{ID: testUserID, DisplayName: "Operator"}
		c.currentCookies = []http.Cookie{operatorCookie("auth", "new", now.Add(24*time.Hour))}
		got, e := s.Validate(context.Background(), "social")
		if e != nil || got.Status != OperatorStatusValid || q.secretUpdates != 1 {
			t.Fatalf("got=%+v err=%v updates=%d", got, e, q.secretUpdates)
		}
	})
	t.Run("stale-success-does-not-overwrite-replacement", func(t *testing.T) {
		s, c, q := makeValid(t)
		replacementValidatedAt := now.Add(time.Minute)
		c.currentUser = CurrentUser{ID: testUserID, DisplayName: "Operator"}
		c.currentCookies = []http.Cookie{operatorCookie("auth", "stale-refresh", now.Add(24*time.Hour))}
		c.currentHook = func() {
			q.row.SecretEnc = []byte{9}
			q.row.SecretNonce = []byte{8}
			q.row.KeyVersion = pgtype.Int4{Int32: 4, Valid: true}
			q.row.SecretStatus = "valid"
			q.row.SecretValidatedAt = pgtype.Timestamptz{Time: replacementValidatedAt, Valid: true}
		}
		result, err := s.Validate(context.Background(), "social")
		if err != nil {
			t.Fatal(err)
		}
		if result.Provider == nil || !bytes.Equal(q.row.SecretEnc, []byte{9}) || q.row.KeyVersion.Int32 != 4 || !q.row.SecretValidatedAt.Time.Equal(replacementValidatedAt) {
			t.Fatalf("stale validation overwrote replacement: result=%+v row=%+v", result, q.row)
		}
	})
	t.Run("stale-unauthorized-does-not-invalidate-replacement", func(t *testing.T) {
		s, c, q := makeValid(t)
		replacementValidatedAt := now.Add(time.Minute)
		c.currentErr = &HTTPError{Status: http.StatusUnauthorized, Category: "authentication"}
		c.currentHook = func() {
			q.row.SecretEnc = []byte{9}
			q.row.SecretNonce = []byte{8}
			q.row.KeyVersion = pgtype.Int4{Int32: 4, Valid: true}
			q.row.SecretStatus = "valid"
			q.row.SecretValidatedAt = pgtype.Timestamptz{Time: replacementValidatedAt, Valid: true}
		}
		_, err := s.Validate(context.Background(), "social")
		if OperatorErrorCategory(err) != OperatorCategoryCredentialsInvalid || !bytes.Equal(q.row.SecretEnc, []byte{9}) || q.row.SecretStatus != "valid" || !q.row.SecretValidatedAt.Time.Equal(replacementValidatedAt) {
			t.Fatalf("stale unauthorized invalidated replacement: err=%v row=%+v", err, q.row)
		}
	})
	t.Run("challenge-shaped-current-user", func(t *testing.T) {
		s, c, q := makeValid(t)
		c.currentUser = CurrentUser{RequiresTwoFactorAuth: []string{"totp"}}
		_, err := s.Validate(context.Background(), "social")
		if OperatorErrorCategory(err) != OperatorCategoryUpstreamTemporarilyUnavailable || q.secretUpdates != 0 || q.healthUpdates != 0 {
			t.Fatalf("err=%v secret_updates=%d health_updates=%d", err, q.secretUpdates, q.healthUpdates)
		}
	})
	for _, status := range []int{401, 403} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			s, c, q := makeValid(t)
			original := append([]byte(nil), q.row.SecretEnc...)
			originalValidatedAt := q.row.SecretValidatedAt
			c.currentErr = &HTTPError{Status: status, Category: "authentication"}
			_, e := s.Validate(context.Background(), "social")
			if OperatorErrorCategory(e) != OperatorCategoryCredentialsInvalid || q.healthUpdates != 1 || q.row.SecretStatus != "invalid" || string(original) != string(q.row.SecretEnc) || q.row.SecretValidatedAt.Valid != originalValidatedAt.Valid || !q.row.SecretValidatedAt.Time.Equal(originalValidatedAt.Time) {
				t.Fatalf("err=%v health=%s updates=%d validated_at=%v", e, q.row.SecretStatus, q.healthUpdates, q.row.SecretValidatedAt)
			}
		})
	}
	for name, upstreamErr := range map[string]error{"rate": &HTTPError{Status: 429, Category: "rate_limited", RetryAfter: 17 * time.Second}, "5xx": &HTTPError{Status: 503, Category: "upstream"}, "network": &RequestError{}, "decode": &DecodeError{Category: "current-user"}} {
		t.Run(name, func(t *testing.T) {
			s, c, q := makeValid(t)
			c.currentErr = upstreamErr
			_, _ = s.Validate(context.Background(), "social")
			if q.healthUpdates != 0 {
				t.Fatalf("transient error changed health: %d", q.healthUpdates)
			}
		})
	}
	t.Run("local-cookie-invalid", func(t *testing.T) {
		s, _, q := makeValid(t)
		originalValidatedAt := q.row.SecretValidatedAt
		q.row.SecretEnc = []byte("bad ciphertext")
		_, e := s.Validate(context.Background(), "social")
		if e == nil || q.healthUpdates != 1 || q.row.SecretStatus != "invalid" || q.row.SecretValidatedAt.Valid != originalValidatedAt.Valid || !q.row.SecretValidatedAt.Time.Equal(originalValidatedAt.Time) {
			t.Fatalf("err=%v updates=%d health=%s validated_at=%v", e, q.healthUpdates, q.row.SecretStatus, q.row.SecretValidatedAt)
		}
	})
	t.Run("locally-expired-cookie", func(t *testing.T) {
		s, _, q := makeValid(t)
		originalValidatedAt := q.row.SecretValidatedAt
		s.now = func() time.Time { return now.Add(25 * time.Hour) }
		_, err := s.Validate(context.Background(), "social")
		if OperatorErrorCategory(err) != OperatorCategoryCredentialsInvalid || q.healthUpdates != 1 || q.row.SecretStatus != "invalid" || q.row.SecretValidatedAt.Valid != originalValidatedAt.Valid || !q.row.SecretValidatedAt.Time.Equal(originalValidatedAt.Time) {
			t.Fatalf("err=%v updates=%d health=%s validated_at=%v", err, q.healthUpdates, q.row.SecretStatus, q.row.SecretValidatedAt)
		}
	})
	t.Run("invalidation-database-failure", func(t *testing.T) {
		s, c, q := makeValid(t)
		q.healthErr = errors.New("database unavailable")
		c.currentErr = &HTTPError{Status: http.StatusUnauthorized, Category: "authentication"}
		_, err := s.Validate(context.Background(), "social")
		if OperatorErrorCategory(err) != OperatorCategoryDatabaseUnavailable {
			t.Fatalf("category = %q, want database_unavailable", OperatorErrorCategory(err))
		}
	})
}

func TestOperatorStartClassifiesLookupAndUpstreamFailures(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name            string
		getErr, errorIn error
		want            OperatorCategory
		retry           time.Duration
	}{{"missing", pgx.ErrNoRows, nil, OperatorCategoryProviderNotReady, 0}, {"database", errors.New("db down"), nil, OperatorCategoryDatabaseUnavailable, 0}, {"credentials", nil, &HTTPError{Status: 401, Category: "authentication"}, OperatorCategoryCredentialsInvalid, 0}, {"rate", nil, &HTTPError{Status: 429, Category: "rate_limited", RetryAfter: 9 * time.Second}, OperatorCategoryUpstreamRateLimited, 9 * time.Second}, {"upstream", nil, &HTTPError{Status: 500, Category: "upstream"}, OperatorCategoryUpstreamTemporarilyUnavailable, 0}, {"network", nil, &RequestError{}, OperatorCategoryUpstreamTemporarilyUnavailable, 0}, {"decode", nil, &DecodeError{Category: "current-user"}, OperatorCategoryUpstreamTemporarilyUnavailable, 0}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &operatorFakeClient{authErr: tc.errorIn}
			q := &operatorFakeQueries{row: operatorProvider(), getErr: tc.getErr}
			s, _ := operatorService(t, c, q, now)
			_, e := s.Start(context.Background(), "social", 7, "sid", "u", "p")
			oe := AsOperatorError(e)
			if oe == nil || oe.Category != tc.want || oe.RetryAfter != tc.retry {
				t.Fatalf("error=%#v", e)
			}
		})
	}
}

var _ = pgtype.Int4{}
