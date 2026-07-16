package vrchat

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
	"prohibitorum/pkg/kv"
)

const (
	OperatorStatusChallenge = "challenge"
	OperatorStatusValid     = "valid"
	operatorChallengeTTL    = 10 * time.Minute
	operatorChallengePrefix = "federation:vrchat:operator:"
	operatorRecoveryTimeout = 5 * time.Second
)

type OperatorCategory string

const (
	OperatorCategoryCredentialsInvalid             OperatorCategory = "credentials_invalid"
	OperatorCategoryChallengeInvalid               OperatorCategory = "challenge_invalid"
	OperatorCategoryCodeInvalid                    OperatorCategory = "code_invalid"
	OperatorCategoryUpstreamRateLimited            OperatorCategory = "upstream_rate_limited"
	OperatorCategoryUpstreamTemporarilyUnavailable OperatorCategory = "upstream_temporarily_unavailable"
	OperatorCategoryProviderNotReady               OperatorCategory = "provider_not_ready"
	OperatorCategoryBadRequest                     OperatorCategory = "bad_request"
	OperatorCategoryDatabaseUnavailable            OperatorCategory = "database_unavailable"
	OperatorCategoryKVUnavailable                  OperatorCategory = "kv_unavailable"
	OperatorCategoryServerError                    OperatorCategory = "server_error"
)

type OperatorError struct {
	Category           OperatorCategory
	RetryAfter         time.Duration
	SessionInvalidated bool
}

func (e *OperatorError) Error() string { return "vrchat: operator session operation failed" }
func AsOperatorError(err error) *OperatorError {
	var target *OperatorError
	if errors.As(err, &target) {
		return target
	}
	return nil
}
func OperatorErrorCategory(err error) OperatorCategory {
	if e := AsOperatorError(err); e != nil {
		return e.Category
	}
	return ""
}
func operatorErr(category OperatorCategory) error { return &OperatorError{Category: category} }

type OperatorSessionResult struct {
	Status    string
	Challenge string
	Methods   []string
	ExpiresAt *time.Time
	Provider  *db.UpstreamIdp
}

type OperatorClient interface {
	Authenticate(context.Context, string, string) (CurrentUser, []http.Cookie, error)
	VerifyTwoFactor(context.Context, string, string, []http.Cookie) ([]http.Cookie, error)
	CurrentUser(context.Context, []http.Cookie) (CurrentUser, []http.Cookie, error)
	EncodeCookies([]http.Cookie) ([]byte, error)
	DecodeCookies([]byte, time.Time) ([]http.Cookie, error)
}
type OperatorQueries interface {
	GetUpstreamIDPBySlugAny(context.Context, string) (db.UpstreamIdp, error)
	UpdateVRChatOperatorSecret(context.Context, db.UpdateVRChatOperatorSecretParams) (db.UpstreamIdp, error)
	RefreshVRChatOperatorSecret(context.Context, db.RefreshVRChatOperatorSecretParams) (db.UpstreamIdp, error)
	InvalidateVRChatOperatorSecret(context.Context, db.InvalidateVRChatOperatorSecretParams) (db.UpstreamIdp, error)
}
type OperatorSecretStore interface {
	SealProviderSecret([]byte, int64, int32) (*federation.SealedSecret, error)
	OpenProviderSecret(federation.SealedSecret, int64) ([]byte, error)
	SealTemporary([]byte, int64, int32, string) (*federation.SealedSecret, error)
	OpenTemporary(federation.SealedSecret, int64, string) ([]byte, error)
}

type OperatorService struct {
	client     OperatorClient
	queries    OperatorQueries
	kv         kv.Store
	secrets    OperatorSecretStore
	keyVersion int32
	now        func() time.Time
	random     io.Reader
}

func NewOperatorService(client OperatorClient, queries OperatorQueries, store kv.Store, secrets OperatorSecretStore, keyVersion int32) *OperatorService {
	return &OperatorService{client: client, queries: queries, kv: store, secrets: secrets, keyVersion: keyVersion, now: time.Now, random: rand.Reader}
}

type operatorChallenge struct {
	State        string                  `json:"state"`
	ProviderID   int64                   `json:"provider_id"`
	ProviderSlug string                  `json:"provider_slug"`
	Protocol     string                  `json:"protocol"`
	AccountID    int32                   `json:"account_id"`
	SessionID    string                  `json:"session_id"`
	Methods      []string                `json:"methods"`
	ExpiresAt    time.Time               `json:"expires_at"`
	Secret       federation.SealedSecret `json:"secret"`
}

func operatorChallengeKey(challenge string) string { return operatorChallengePrefix + challenge }
func encodeOperatorChallenge(state operatorChallenge) (string, error) {
	b, e := json.Marshal(state)
	return string(b), e
}
func decodeOperatorChallenge(raw string) (operatorChallenge, error) {
	dec := json.NewDecoder(bytes.NewBufferString(raw))
	dec.DisallowUnknownFields()
	var state operatorChallenge
	if err := dec.Decode(&state); err != nil {
		return state, err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return state, errors.New("trailing challenge state")
	}
	if state.ProviderID <= 0 || state.ProviderSlug == "" || state.Protocol != "vrchat" || state.AccountID <= 0 || state.SessionID == "" || state.ExpiresAt.IsZero() || state.State == "" || len(state.Secret.Ciphertext) == 0 || len(state.Secret.Nonce) == 0 || state.Secret.KeyVersion <= 0 {
		return state, errors.New("invalid challenge state")
	}
	return state, nil
}

func (s *OperatorService) Start(ctx context.Context, slug string, accountID int32, sessionID, username, password string) (OperatorSessionResult, error) {
	if slug == "" || accountID <= 0 || sessionID == "" || username == "" || password == "" {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryBadRequest)
	}
	provider, err := s.provider(ctx, slug)
	if err != nil {
		return OperatorSessionResult{}, err
	}
	user, cookies, err := s.client.Authenticate(ctx, username, password)
	if err != nil {
		return OperatorSessionResult{}, classifyOperatorUpstream(err, true)
	}
	if fullCurrentUser(user) {
		return s.persist(ctx, provider, cookies)
	}
	methods := supportedOperatorMethods(user.RequiresTwoFactorAuth)
	if len(methods) == 0 {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryUpstreamTemporarilyUnavailable)
	}
	if !usableOperatorAuthCookie(cookies, s.now()) {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryUpstreamTemporarilyUnavailable)
	}
	challengeBytes := make([]byte, 32)
	if _, err = io.ReadFull(s.random, challengeBytes); err != nil {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryServerError)
	}
	challenge := base64.RawURLEncoding.EncodeToString(challengeBytes)
	encoded, err := s.client.EncodeCookies(cookies)
	if err != nil {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryUpstreamTemporarilyUnavailable)
	}
	sealed, err := s.secrets.SealTemporary(encoded, provider.ID, s.keyVersion, challenge)
	clearOperatorPlaintext(encoded)
	if err != nil {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryServerError)
	}
	expires := s.now().UTC().Add(operatorChallengeTTL)
	state := operatorChallenge{State: "ready", ProviderID: provider.ID, ProviderSlug: provider.Slug, Protocol: provider.Protocol, AccountID: accountID, SessionID: sessionID, Methods: methods, ExpiresAt: expires, Secret: *sealed}
	raw, err := encodeOperatorChallenge(state)
	if err != nil {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryServerError)
	}
	if err = s.kv.SetEx(ctx, operatorChallengeKey(challenge), raw, operatorChallengeTTL); err != nil {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryKVUnavailable)
	}
	return OperatorSessionResult{Status: OperatorStatusChallenge, Challenge: challenge, Methods: append([]string(nil), methods...), ExpiresAt: &expires}, nil
}

func (s *OperatorService) Verify(ctx context.Context, slug string, accountID int32, sessionID, challenge, method, code string) (OperatorSessionResult, error) {
	if slug == "" || accountID <= 0 || sessionID == "" || challenge == "" || method == "" || code == "" {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryBadRequest)
	}
	raw, err := s.kv.Get(ctx, operatorChallengeKey(challenge))
	if err != nil {
		if errors.Is(err, kv.ErrKeyNotFound) {
			return OperatorSessionResult{}, operatorErr(OperatorCategoryChallengeInvalid)
		}
		return OperatorSessionResult{}, operatorErr(OperatorCategoryKVUnavailable)
	}
	state, err := decodeOperatorChallenge(raw)
	if err != nil || state.State != "ready" || state.ProviderSlug != slug || state.AccountID != accountID || state.SessionID != sessionID || !state.ExpiresAt.After(s.now()) || !containsMethod(state.Methods, method) {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryChallengeInvalid)
	}
	provider, err := s.provider(ctx, slug)
	if err != nil {
		if OperatorErrorCategory(err) == OperatorCategoryProviderNotReady {
			return OperatorSessionResult{}, operatorErr(OperatorCategoryChallengeInvalid)
		}
		return OperatorSessionResult{}, err
	}
	if provider.ID != state.ProviderID || provider.Protocol != state.Protocol {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryChallengeInvalid)
	}
	remaining := state.ExpiresAt.Sub(s.now())
	if remaining <= 0 {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryChallengeInvalid)
	}
	verifying := state
	verifying.State = "verifying"
	verifyingRaw, err := encodeOperatorChallenge(verifying)
	if err != nil {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryServerError)
	}
	swapped, err := s.kv.CompareAndSwap(ctx, operatorChallengeKey(challenge), raw, verifyingRaw, remaining)
	if err != nil {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryKVUnavailable)
	}
	if !swapped {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryChallengeInvalid)
	}
	restore := func(failure error) error {
		retry := state.ExpiresAt.Sub(s.now())
		if retry <= 0 {
			return failure
		}
		recoveryCtx, cancelRecovery := context.WithTimeout(context.WithoutCancel(ctx), operatorRecoveryTimeout)
		defer cancelRecovery()
		swapped, restoreErr := s.kv.CompareAndSwap(recoveryCtx, operatorChallengeKey(challenge), verifyingRaw, raw, retry)
		if restoreErr != nil || !swapped {
			return operatorErr(OperatorCategoryKVUnavailable)
		}
		return failure
	}
	encoded, err := s.secrets.OpenTemporary(state.Secret, state.ProviderID, challenge)
	if err != nil {
		return OperatorSessionResult{}, restore(operatorErr(OperatorCategoryChallengeInvalid))
	}
	cookies, err := s.client.DecodeCookies(encoded, s.now())
	clearOperatorPlaintext(encoded)
	if err != nil {
		return OperatorSessionResult{}, restore(operatorErr(OperatorCategoryChallengeInvalid))
	}
	cookies, err = s.client.VerifyTwoFactor(ctx, method, code, cookies)
	if err != nil {
		return OperatorSessionResult{}, restore(classifyVerifyError(err))
	}
	user, cookies, err := s.client.CurrentUser(ctx, cookies)
	if err != nil {
		return OperatorSessionResult{}, restore(classifyOperatorUpstream(err, false))
	}
	if !fullCurrentUser(user) {
		return OperatorSessionResult{}, restore(operatorErr(OperatorCategoryUpstreamTemporarilyUnavailable))
	}
	if !usableOperatorAuthCookie(cookies, s.now()) {
		return OperatorSessionResult{}, restore(operatorErr(OperatorCategoryUpstreamTemporarilyUnavailable))
	}
	consumed := verifying
	consumed.State = "consumed"
	consumed.Secret = federation.SealedSecret{}
	consumedRaw, err := encodeOperatorChallenge(consumed)
	if err != nil {
		return OperatorSessionResult{}, restore(operatorErr(OperatorCategoryServerError))
	}
	finalizeTTL := state.ExpiresAt.Sub(s.now())
	if finalizeTTL <= 0 {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryChallengeInvalid)
	}
	finalizeCtx, cancelFinalize := context.WithTimeout(context.WithoutCancel(ctx), operatorRecoveryTimeout)
	swapped, err = s.kv.CompareAndSwap(finalizeCtx, operatorChallengeKey(challenge), verifyingRaw, consumedRaw, finalizeTTL)
	cancelFinalize()
	if err != nil {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryKVUnavailable)
	}
	if !swapped {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryChallengeInvalid)
	}
	result, err := s.persist(ctx, provider, cookies)
	if err == nil {
		return result, nil
	}
	restoreTTL := state.ExpiresAt.Sub(s.now())
	if restoreTTL <= 0 {
		return OperatorSessionResult{}, err
	}
	recoveryCtx, cancelRecovery := context.WithTimeout(context.WithoutCancel(ctx), operatorRecoveryTimeout)
	defer cancelRecovery()
	restored, restoreErr := s.kv.CompareAndSwap(recoveryCtx, operatorChallengeKey(challenge), consumedRaw, raw, restoreTTL)
	if restoreErr != nil || !restored {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryKVUnavailable)
	}
	return OperatorSessionResult{}, err
}

func (s *OperatorService) Validate(ctx context.Context, slug string) (OperatorSessionResult, error) {
	if slug == "" {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryBadRequest)
	}
	provider, err := s.provider(ctx, slug)
	if err != nil {
		return OperatorSessionResult{}, err
	}
	if !provider.KeyVersion.Valid || len(provider.SecretEnc) == 0 || len(provider.SecretNonce) == 0 {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryProviderNotReady)
	}
	sealed := federation.SealedSecret{Ciphertext: provider.SecretEnc, Nonce: provider.SecretNonce, KeyVersion: provider.KeyVersion.Int32}
	encoded, err := s.secrets.OpenProviderSecret(sealed, provider.ID)
	if err != nil {
		return s.invalidCredentials(ctx, provider)
	}
	cookies, err := s.client.DecodeCookies(encoded, s.now())
	clearOperatorPlaintext(encoded)
	if err != nil {
		return s.invalidCredentials(ctx, provider)
	}
	user, cookies, err := s.client.CurrentUser(ctx, cookies)
	if err != nil {
		var httpErr *HTTPError
		if errors.As(err, &httpErr) && (httpErr.Status == http.StatusUnauthorized || httpErr.Status == http.StatusForbidden) {
			return s.invalidCredentials(ctx, provider)
		}
		return OperatorSessionResult{}, classifyOperatorUpstream(err, true)
	}
	if !fullCurrentUser(user) {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryUpstreamTemporarilyUnavailable)
	}
	if !usableOperatorAuthCookie(cookies, s.now()) {
		return s.invalidCredentials(ctx, provider)
	}
	return s.persistValidation(ctx, provider, cookies)
}

func (s *OperatorService) provider(ctx context.Context, slug string) (db.UpstreamIdp, error) {
	row, err := s.queries.GetUpstreamIDPBySlugAny(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return row, operatorErr(OperatorCategoryProviderNotReady)
		}
		return row, operatorErr(OperatorCategoryDatabaseUnavailable)
	}
	if row.Slug != slug || row.Protocol != "vrchat" {
		return db.UpstreamIdp{}, operatorErr(OperatorCategoryProviderNotReady)
	}
	return row, nil
}
func (s *OperatorService) persist(ctx context.Context, provider db.UpstreamIdp, cookies []http.Cookie) (OperatorSessionResult, error) {
	if !usableOperatorAuthCookie(cookies, s.now()) {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryUpstreamTemporarilyUnavailable)
	}
	encoded, err := s.client.EncodeCookies(cookies)
	if err != nil {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryUpstreamTemporarilyUnavailable)
	}
	sealed, err := s.secrets.SealProviderSecret(encoded, provider.ID, s.keyVersion)
	clearOperatorPlaintext(encoded)
	if err != nil {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryServerError)
	}
	row, err := s.queries.UpdateVRChatOperatorSecret(ctx, db.UpdateVRChatOperatorSecretParams{SecretEnc: sealed.Ciphertext, SecretNonce: sealed.Nonce, KeyVersion: pgtype.Int4{Int32: sealed.KeyVersion, Valid: true}, SecretValidatedAt: pgtype.Timestamptz{Time: s.now().UTC(), Valid: true}, ID: provider.ID, Slug: provider.Slug})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OperatorSessionResult{}, operatorErr(OperatorCategoryProviderNotReady)
		}
		return OperatorSessionResult{}, operatorErr(OperatorCategoryDatabaseUnavailable)
	}
	return OperatorSessionResult{Status: OperatorStatusValid, Provider: &row}, nil
}
func (s *OperatorService) persistValidation(ctx context.Context, provider db.UpstreamIdp, cookies []http.Cookie) (OperatorSessionResult, error) {
	if !usableOperatorAuthCookie(cookies, s.now()) {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryUpstreamTemporarilyUnavailable)
	}
	encoded, err := s.client.EncodeCookies(cookies)
	if err != nil {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryUpstreamTemporarilyUnavailable)
	}
	sealed, err := s.secrets.SealProviderSecret(encoded, provider.ID, s.keyVersion)
	clearOperatorPlaintext(encoded)
	if err != nil {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryServerError)
	}
	row, err := s.queries.RefreshVRChatOperatorSecret(ctx, db.RefreshVRChatOperatorSecretParams{
		NewSecretEnc: sealed.Ciphertext, NewSecretNonce: sealed.Nonce,
		NewKeyVersion:     pgtype.Int4{Int32: sealed.KeyVersion, Valid: true},
		SecretValidatedAt: pgtype.Timestamptz{Time: s.now().UTC(), Valid: true},
		ID:                provider.ID, Slug: provider.Slug,
		ExpectedSecretEnc: provider.SecretEnc, ExpectedSecretNonce: provider.SecretNonce,
		ExpectedKeyVersion: provider.KeyVersion,
	})
	if err == nil {
		return OperatorSessionResult{Status: OperatorStatusValid, Provider: &row}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryDatabaseUnavailable)
	}
	current, reloadErr := s.currentProviderAfterStale(ctx, provider)
	if reloadErr != nil {
		return OperatorSessionResult{}, reloadErr
	}
	if !current.KeyVersion.Valid || len(current.SecretEnc) == 0 || len(current.SecretNonce) == 0 || current.SecretStatus != "valid" {
		return OperatorSessionResult{}, operatorErr(OperatorCategoryProviderNotReady)
	}
	return OperatorSessionResult{Status: OperatorStatusValid, Provider: &current}, nil
}

func (s *OperatorService) invalidCredentials(ctx context.Context, provider db.UpstreamIdp) (OperatorSessionResult, error) {
	invalidated, err := s.invalidate(ctx, provider)
	if err != nil {
		return OperatorSessionResult{}, err
	}
	return OperatorSessionResult{}, &OperatorError{Category: OperatorCategoryCredentialsInvalid, SessionInvalidated: invalidated}
}

func (s *OperatorService) invalidate(ctx context.Context, provider db.UpstreamIdp) (bool, error) {
	_, err := s.queries.InvalidateVRChatOperatorSecret(ctx, db.InvalidateVRChatOperatorSecretParams{
		ID: provider.ID, Slug: provider.Slug,
		ExpectedSecretEnc: provider.SecretEnc, ExpectedSecretNonce: provider.SecretNonce,
		ExpectedKeyVersion: provider.KeyVersion,
	})
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, operatorErr(OperatorCategoryDatabaseUnavailable)
	}
	if _, reloadErr := s.currentProviderAfterStale(ctx, provider); reloadErr != nil {
		return false, reloadErr
	}
	return false, nil
}

func (s *OperatorService) currentProviderAfterStale(ctx context.Context, expected db.UpstreamIdp) (db.UpstreamIdp, error) {
	current, err := s.queries.GetUpstreamIDPBySlugAny(ctx, expected.Slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.UpstreamIdp{}, operatorErr(OperatorCategoryProviderNotReady)
		}
		return db.UpstreamIdp{}, operatorErr(OperatorCategoryDatabaseUnavailable)
	}
	if current.ID != expected.ID || current.Slug != expected.Slug || current.Protocol != "vrchat" {
		return db.UpstreamIdp{}, operatorErr(OperatorCategoryProviderNotReady)
	}
	return current, nil
}
func fullCurrentUser(user CurrentUser) bool {
	return user.ID != "" && user.DisplayName != "" && len(user.RequiresTwoFactorAuth) == 0
}
func usableOperatorAuthCookie(cookies []http.Cookie, now time.Time) bool {
	authCookies := 0
	for i := range cookies {
		if cookies[i].Name != "auth" {
			continue
		}
		if cookies[i].Value == "" || !validAuthenticationCookie(&cookies[i], now) {
			return false
		}
		authCookies++
	}
	return authCookies == 1
}
func supportedOperatorMethods(methods []string) []string {
	out := make([]string, 0, len(methods))
	seen := map[string]bool{}
	for _, m := range methods {
		if (m == "totp" || m == "emailOtp" || m == "otp") && !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}
func containsMethod(methods []string, method string) bool {
	for _, candidate := range methods {
		if candidate == method {
			return true
		}
	}
	return false
}
func classifyVerifyError(err error) error {
	var verification *VerificationError
	if errors.As(err, &verification) {
		return operatorErr(OperatorCategoryCodeInvalid)
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) && (httpErr.Status == http.StatusUnauthorized || httpErr.Status == http.StatusForbidden) {
		return operatorErr(OperatorCategoryCodeInvalid)
	}
	var oversize *OversizeError
	var validation *ValidationError
	if errors.As(err, &oversize) || errors.As(err, &validation) {
		return operatorErr(OperatorCategoryBadRequest)
	}
	return classifyOperatorUpstream(err, false)
}
func classifyOperatorUpstream(err error, credentials bool) error {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		switch {
		case httpErr.Status == http.StatusUnauthorized || httpErr.Status == http.StatusForbidden:
			if credentials {
				return operatorErr(OperatorCategoryCredentialsInvalid)
			}
			return operatorErr(OperatorCategoryCodeInvalid)
		case httpErr.Status == http.StatusTooManyRequests:
			return &OperatorError{Category: OperatorCategoryUpstreamRateLimited, RetryAfter: httpErr.RetryAfter}
		case httpErr.Status >= 500:
			return operatorErr(OperatorCategoryUpstreamTemporarilyUnavailable)
		}
	}
	var request *RequestError
	var decode *DecodeError
	var oversize *OversizeError
	if errors.As(err, &request) || errors.As(err, &decode) || errors.As(err, &oversize) {
		return operatorErr(OperatorCategoryUpstreamTemporarilyUnavailable)
	}
	var validation *ValidationError
	if errors.As(err, &validation) {
		return operatorErr(OperatorCategoryBadRequest)
	}
	return operatorErr(OperatorCategoryUpstreamTemporarilyUnavailable)
}

// clearOperatorPlaintext erases mutable cookie-envelope bytes as soon as they
// have crossed the crypto/decoder boundary. Go strings (including KV API
// values) are immutable and cannot be reliably cleared; those values contain
// metadata and ciphertext only.
func clearOperatorPlaintext(value []byte) {
	clear(value)
}
