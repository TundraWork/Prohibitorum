package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/credential/enrollment"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
	federationvrchat "prohibitorum/pkg/federation/providers/vrchat"
	"prohibitorum/pkg/kv"
	sessstore "prohibitorum/pkg/session"
)

type enrollmentAuditCapture struct {
	records []audit.Record
}

func (c *enrollmentAuditCapture) Record(_ context.Context, record audit.Record) error {
	c.records = append(c.records, record)
	return nil
}

type failingSessionRevocationStore struct {
	kv.Store
	scanErr error
}

func (s *failingSessionRevocationStore) ScanEntries(ctx context.Context, pattern string, cursor uint64, count int64) (kv.ScanEntriesResult, error) {
	if s.scanErr != nil && strings.HasPrefix(pattern, "session:") {
		return kv.ScanEntriesResult{}, s.scanErr
	}
	return s.Store.ScanEntries(ctx, pattern, cursor, count)
}

type vrchatEnrollmentQueries struct {
	db.Querier
	mu                sync.Mutex
	enrollments       map[string]db.Enrollment
	accounts          map[int32]db.Account
	providers         map[int64]db.UpstreamIdp
	providerErr       error
	credentials       map[int32][]db.WebauthnCredential
	identities        []db.AccountIdentity
	sessions          []db.Session
	nextAccountID     int32
	nextCredentialID  int32
	nextIdentityID    int64
	insertAccountErr  error
	insertIdentityErr error
	revokeCalls       int
	sessionOps        []string
}

func (q *vrchatEnrollmentQueries) GetEnrollmentByToken(_ context.Context, token string) (db.Enrollment, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	e, ok := q.enrollments[token]
	if !ok {
		return db.Enrollment{}, pgx.ErrNoRows
	}
	return e, nil
}

func (q *vrchatEnrollmentQueries) GetAccountByID(_ context.Context, id int32) (db.Account, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	a, ok := q.accounts[id]
	if !ok {
		return db.Account{}, pgx.ErrNoRows
	}
	return a, nil
}

func (q *vrchatEnrollmentQueries) GetAccountByUsername(_ context.Context, username string) (db.Account, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, account := range q.accounts {
		if account.Username == username {
			return account, nil
		}
	}
	return db.Account{}, pgx.ErrNoRows
}

func (q *vrchatEnrollmentQueries) ListCredentialsByAccount(_ context.Context, accountID int32) ([]db.WebauthnCredential, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]db.WebauthnCredential(nil), q.credentials[accountID]...), nil
}

func (q *vrchatEnrollmentQueries) GetUpstreamIDPByIDAny(_ context.Context, id int64) (db.UpstreamIdp, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.providerErr != nil {
		return db.UpstreamIdp{}, q.providerErr
	}
	provider, ok := q.providers[id]
	if !ok {
		return db.UpstreamIdp{}, pgx.ErrNoRows
	}
	return provider, nil
}

func (q *vrchatEnrollmentQueries) GetUpstreamIDPBySlugAny(_ context.Context, slug string) (db.UpstreamIdp, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, provider := range q.providers {
		if provider.Slug == slug {
			return provider, nil
		}
	}
	return db.UpstreamIdp{}, pgx.ErrNoRows
}

func (q *vrchatEnrollmentQueries) ConsumeEnrollment(_ context.Context, token string) (db.Enrollment, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	e, ok := q.enrollments[token]
	if !ok || e.ConsumedAt.Valid || !e.ExpiresAt.Valid || !e.ExpiresAt.Time.After(time.Now()) {
		return db.Enrollment{}, pgx.ErrNoRows
	}
	e.ConsumedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	q.enrollments[token] = e
	return e, nil
}

func (q *vrchatEnrollmentQueries) InsertAccount(_ context.Context, p db.InsertAccountParams) (db.Account, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.insertAccountErr != nil {
		return db.Account{}, q.insertAccountErr
	}
	for _, existing := range q.accounts {
		if existing.Username == p.Username {
			return db.Account{}, &pgconn.PgError{Code: "23505", ConstraintName: "account_username_key"}
		}
	}
	q.nextAccountID++
	if q.nextAccountID == 1 {
		q.nextAccountID = 100
	}
	account := db.Account{
		ID:                 q.nextAccountID,
		Username:           p.Username,
		DisplayName:        p.DisplayName,
		WebauthnUserHandle: append([]byte(nil), p.WebauthnUserHandle...),
		Role:               p.Role,
		Attributes:         append([]byte(nil), p.Attributes...),
		Disabled:           p.Disabled,
	}
	q.accounts[account.ID] = account
	return account, nil
}

func (q *vrchatEnrollmentQueries) InsertCredential(_ context.Context, p db.InsertCredentialParams) (db.WebauthnCredential, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.nextCredentialID++
	credential := db.WebauthnCredential{
		ID:              q.nextCredentialID,
		AccountID:       p.AccountID,
		CredentialID:    append([]byte(nil), p.CredentialID...),
		PublicKey:       append([]byte(nil), p.PublicKey...),
		CoseAlg:         p.CoseAlg,
		UserHandle:      append([]byte(nil), p.UserHandle...),
		SignCount:       p.SignCount,
		Transports:      append([]string(nil), p.Transports...),
		Aaguid:          append([]byte(nil), p.Aaguid...),
		AttestationType: p.AttestationType,
		BackupEligible:  p.BackupEligible,
		BackupState:     p.BackupState,
		UvInitialized:   p.UvInitialized,
		Nickname:        p.Nickname,
	}
	q.credentials[p.AccountID] = append(q.credentials[p.AccountID], credential)
	return credential, nil
}

func (q *vrchatEnrollmentQueries) DeleteAllCredentialsForAccount(_ context.Context, accountID int32) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.credentials, accountID)
	return nil
}

func (q *vrchatEnrollmentQueries) GetAccountIdentityByIssuerSub(_ context.Context, p db.GetAccountIdentityByIssuerSubParams) (db.AccountIdentity, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, identity := range q.identities {
		if identity.UpstreamIss == p.UpstreamIss && identity.UpstreamSub == p.UpstreamSub {
			return identity, nil
		}
	}
	return db.AccountIdentity{}, pgx.ErrNoRows
}

func (q *vrchatEnrollmentQueries) InsertAccountIdentity(_ context.Context, p db.InsertAccountIdentityParams) (db.AccountIdentity, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.insertIdentityErr != nil {
		return db.AccountIdentity{}, q.insertIdentityErr
	}
	for _, existing := range q.identities {
		if existing.UpstreamIss == p.UpstreamIss && existing.UpstreamSub == p.UpstreamSub {
			return db.AccountIdentity{}, &pgconn.PgError{Code: "23505", ConstraintName: "account_identity_upstream_iss_upstream_sub_key"}
		}
	}
	q.nextIdentityID++
	identity := db.AccountIdentity{
		ID:            q.nextIdentityID,
		AccountID:     p.AccountID,
		UpstreamIdpID: p.UpstreamIdpID,
		UpstreamIss:   p.UpstreamIss,
		UpstreamSub:   p.UpstreamSub,
		UpstreamEmail: p.UpstreamEmail,
		UpstreamData:  append([]byte(nil), p.UpstreamData...),
	}
	q.identities = append(q.identities, identity)
	return identity, nil
}

func (q *vrchatEnrollmentQueries) ConfirmAccountIdentity(_ context.Context, id int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.identities {
		if q.identities[i].ID == id {
			q.identities[i].ConfirmedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
			return nil
		}
	}
	return pgx.ErrNoRows
}

func (q *vrchatEnrollmentQueries) InsertSession(_ context.Context, p db.InsertSessionParams) (db.Session, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	session := db.Session{ID: p.ID, AccountID: p.AccountID, AuthTime: p.AuthTime, Amr: append([]string(nil), p.Amr...), Acr: p.Acr, UpstreamIdpID: p.UpstreamIdpID}
	q.sessions = append(q.sessions, session)
	q.sessionOps = append(q.sessionOps, "issue")
	return session, nil
}

func (q *vrchatEnrollmentQueries) RevokeSession(_ context.Context, id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.sessions {
		if q.sessions[i].ID == id {
			q.sessions[i].RevokedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
		}
	}
	return nil
}

func (q *vrchatEnrollmentQueries) RevokeAllSessionsByAccount(_ context.Context, accountID int32) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.revokeCalls++
	q.sessionOps = append(q.sessionOps, "revoke")
	for i := range q.sessions {
		if q.sessions[i].AccountID == accountID && !q.sessions[i].RevokedAt.Valid {
			q.sessions[i].RevokedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
		}
	}
	return nil
}

func cloneEnrollmentQueries(q *vrchatEnrollmentQueries) *vrchatEnrollmentQueries {
	clone := &vrchatEnrollmentQueries{
		enrollments:       make(map[string]db.Enrollment, len(q.enrollments)),
		accounts:          make(map[int32]db.Account, len(q.accounts)),
		providers:         make(map[int64]db.UpstreamIdp, len(q.providers)),
		providerErr:       q.providerErr,
		credentials:       make(map[int32][]db.WebauthnCredential, len(q.credentials)),
		identities:        append([]db.AccountIdentity(nil), q.identities...),
		sessions:          append([]db.Session(nil), q.sessions...),
		nextAccountID:     q.nextAccountID,
		nextCredentialID:  q.nextCredentialID,
		nextIdentityID:    q.nextIdentityID,
		insertAccountErr:  q.insertAccountErr,
		insertIdentityErr: q.insertIdentityErr,
		revokeCalls:       q.revokeCalls,
	}
	for token, e := range q.enrollments {
		clone.enrollments[token] = e
	}
	for id, account := range q.accounts {
		clone.accounts[id] = account
	}
	for id, provider := range q.providers {
		clone.providers[id] = provider
	}
	for accountID, credentials := range q.credentials {
		clone.credentials[accountID] = append([]db.WebauthnCredential(nil), credentials...)
	}
	return clone
}

type memoryEnrollmentTxRunner struct{ root *vrchatEnrollmentQueries }

func (r *memoryEnrollmentTxRunner) BeginEnrollmentTx(context.Context) (enrollmentTx, error) {
	r.root.mu.Lock()
	return &memoryEnrollmentTx{root: r.root, q: cloneEnrollmentQueries(r.root), active: true}, nil
}

type memoryEnrollmentTx struct {
	root   *vrchatEnrollmentQueries
	q      *vrchatEnrollmentQueries
	active bool
}

func (tx *memoryEnrollmentTx) Queries() db.Querier { return tx.q }

func (tx *memoryEnrollmentTx) Commit(context.Context) error {
	if !tx.active {
		return errors.New("transaction closed")
	}
	tx.root.enrollments = tx.q.enrollments
	tx.root.accounts = tx.q.accounts
	tx.root.providers = tx.q.providers
	tx.root.credentials = tx.q.credentials
	tx.root.identities = tx.q.identities
	tx.root.nextAccountID = tx.q.nextAccountID
	tx.root.nextCredentialID = tx.q.nextCredentialID
	tx.root.nextIdentityID = tx.q.nextIdentityID
	tx.active = false
	tx.root.mu.Unlock()
	return nil
}

func (tx *memoryEnrollmentTx) Rollback(context.Context) error {
	if tx.active {
		tx.active = false
		tx.root.mu.Unlock()
	}
	return nil
}

type recordingEnrollmentWebAuthn struct {
	user       webauthn.User
	options    protocol.PublicKeyCredentialCreationOptions
	credential *webauthn.Credential
	createErr  error
}

func (f *recordingEnrollmentWebAuthn) BeginRegistration(user webauthn.User, opts ...webauthn.RegistrationOption) (*protocol.CredentialCreation, *webauthn.SessionData, error) {
	f.user = user
	for _, opt := range opts {
		opt(&f.options)
	}
	return &protocol.CredentialCreation{Response: f.options}, &webauthn.SessionData{UserID: append([]byte(nil), user.WebAuthnID()...)}, nil
}

func (f *recordingEnrollmentWebAuthn) CreateCredential(webauthn.User, webauthn.SessionData, *protocol.ParsedCredentialCreationData) (*webauthn.Credential, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.credential != nil {
		return f.credential, nil
	}
	return nil, errors.New("unexpected credential completion")
}

func beginEnrollmentRequest(token, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/enrollments/"+token+"/register/begin", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func validVRChatEnrollmentProvider() db.UpstreamIdp {
	return db.UpstreamIdp{
		ID:             41,
		Slug:           "vrchat-main",
		DisplayName:    "VRChat",
		Protocol:       "vrchat",
		Mode:           federation.ModeLinkOnly,
		ProviderConfig: []byte(`{}`),
		SecretEnc:      []byte{1},
		SecretNonce:    []byte{2},
		KeyVersion:     pgtype.Int4{Int32: 1, Valid: true},
		SecretStatus:   "valid",
	}
}

func vrchatEnrollmentRegistry(t *testing.T) *federation.Registry {
	t.Helper()
	registry := federation.NewRegistry()
	if err := registry.RegisterDefinition(federationvrchat.Definition{}); err != nil {
		t.Fatal(err)
	}
	return registry
}

func pendingEnrollment(token, intent string) db.Enrollment {
	return db.Enrollment{
		Token:     token,
		Intent:    intent,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}
}

func TestEnrollmentPreviewFederatedRegistrationReturnsOnlySafeSuggestion(t *testing.T) {
	provider := validVRChatEnrollmentProvider()
	e := pendingEnrollment("federated", enrollment.IntentFederatedRegister)
	e.FederatedUpstreamIdpID = pgtype.Int8{Int64: provider.ID, Valid: true}
	e.FederatedUpstreamIdpSlug = pgtype.Text{String: provider.Slug, Valid: true}
	e.FederatedUpstreamIss = pgtype.Text{String: "https://secret-issuer.example", Valid: true}
	e.FederatedUpstreamSub = pgtype.Text{String: "secret-subject", Valid: true}
	e.FederatedDisplayName = pgtype.Text{String: "Suggested VRChat Name", Valid: true}
	e.FederatedUpstreamData = []byte(`{"userId":"secret-metadata"}`)
	e.FederatedAvatarUrl = pgtype.Text{String: "https://secret-avatar.example/image.png", Valid: true}
	q := &vrchatEnrollmentQueries{
		enrollments: map[string]db.Enrollment{e.Token: e},
		providers:   map[int64]db.UpstreamIdp{provider.ID: provider},
	}
	s := &Server{enrollmentQueriesOverride: q, federationRegistry: vrchatEnrollmentRegistry(t)}

	out, err := s.handlePreviewEnrollment(context.Background(), &previewIn{Token: e.Token})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if out.Body.Intent != enrollment.IntentFederatedRegister || out.Body.ExpiresAt != e.ExpiresAt.Time || out.Body.SuggestedDisplayName != e.FederatedDisplayName.String {
		t.Fatalf("preview = %+v", out.Body)
	}
	if out.Body.Target != nil {
		t.Fatalf("federated preview exposed target %+v", out.Body.Target)
	}
	raw, err := json.Marshal(out.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-issuer", "secret-subject", "secret-metadata", "secret-avatar", provider.Slug, `providerId`, `providerSlug`, `issuer`, `subject`, `metadata`, `avatarUrl`} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("preview leaked %q: %s", secret, raw)
		}
	}
}

func TestEnrollmentPreviewProviderRecoverySuppressesTargetButAdminResetKeepsIt(t *testing.T) {
	account := db.Account{ID: 73, Username: "private-user", DisplayName: "Private Name"}
	providerRecovery := pendingEnrollment("provider-reset", enrollment.IntentReset)
	providerRecovery.TargetAccountID = pgtype.Int4{Int32: account.ID, Valid: true}
	providerRecovery.RecoverySourceUpstreamIdpID = pgtype.Int8{Int64: 41, Valid: true}
	adminReset := pendingEnrollment("admin-reset", enrollment.IntentReset)
	adminReset.TargetAccountID = providerRecovery.TargetAccountID
	q := &vrchatEnrollmentQueries{
		enrollments: map[string]db.Enrollment{providerRecovery.Token: providerRecovery, adminReset.Token: adminReset},
		accounts:    map[int32]db.Account{account.ID: account},
	}
	s := &Server{enrollmentQueriesOverride: q}

	providerOut, err := s.handlePreviewEnrollment(context.Background(), &previewIn{Token: providerRecovery.Token})
	if err != nil {
		t.Fatal(err)
	}
	if providerOut.Body.Target != nil {
		t.Fatalf("provider recovery exposed target %+v", providerOut.Body.Target)
	}
	adminOut, err := s.handlePreviewEnrollment(context.Background(), &previewIn{Token: adminReset.Token})
	if err != nil {
		t.Fatal(err)
	}
	if adminOut.Body.Target == nil || adminOut.Body.Target.Username != account.Username || adminOut.Body.Target.DisplayName != account.DisplayName {
		t.Fatalf("admin reset target = %+v", adminOut.Body.Target)
	}
}

func TestEnrollmentBeginFederatedAndProviderRecoveryRejectChangedProvider(t *testing.T) {
	baseProvider := validVRChatEnrollmentProvider()
	cases := map[string]struct {
		mutate  func(*db.UpstreamIdp)
		missing bool
		err     error
	}{
		"disabled":       {mutate: func(p *db.UpstreamIdp) { p.Disabled = true }},
		"deleted":        {missing: true},
		"unready":        {mutate: func(p *db.UpstreamIdp) { p.SecretStatus = "invalid" }},
		"wrong protocol": {mutate: func(p *db.UpstreamIdp) { p.Protocol = "oidc" }},
		"wrong mode":     {mutate: func(p *db.UpstreamIdp) { p.Mode = federation.ModeAutoProvision }},
		"lookup failure": {err: errors.New("database unavailable")},
	}
	for _, intent := range []string{enrollment.IntentFederatedRegister, enrollment.IntentReset} {
		for name, tc := range cases {
			t.Run(intent+"/"+name, func(t *testing.T) {
				provider := baseProvider
				if tc.mutate != nil {
					tc.mutate(&provider)
				}
				e := pendingEnrollment("token", intent)
				if intent == enrollment.IntentFederatedRegister {
					e.FederatedUpstreamIdpID = pgtype.Int8{Int64: baseProvider.ID, Valid: true}
					e.FederatedDisplayName = pgtype.Text{String: "VRChat User", Valid: true}
				} else {
					e.TargetAccountID = pgtype.Int4{Int32: 73, Valid: true}
					e.RecoverySourceUpstreamIdpID = pgtype.Int8{Int64: baseProvider.ID, Valid: true}
				}
				providers := map[int64]db.UpstreamIdp{provider.ID: provider}
				if tc.missing {
					providers = map[int64]db.UpstreamIdp{}
				}
				q := &vrchatEnrollmentQueries{
					enrollments: map[string]db.Enrollment{e.Token: e},
					accounts:    map[int32]db.Account{73: {ID: 73, WebauthnUserHandle: []byte("opaque-handle")}},
					providers:   providers,
					providerErr: tc.err,
				}
				s := &Server{enrollmentQueriesOverride: q, federationRegistry: vrchatEnrollmentRegistry(t)}
				body := strings.NewReader(`{"username":"local-user","displayName":"Local Name"}`)
				req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/enrollments/token/register/begin", body)
				rctx := chi.NewRouteContext()
				rctx.URLParams.Add("token", e.Token)
				req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
				w := httptest.NewRecorder()

				s.handleEnrollmentBeginHTTP(w, req)

				if w.Code != http.StatusServiceUnavailable {
					t.Fatalf("status = %d body=%s, want 503", w.Code, w.Body.String())
				}
				var public struct {
					Code string `json:"code"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &public); err != nil {
					t.Fatal(err)
				}
				if public.Code != authn.ErrProviderNotReady().Code {
					t.Fatalf("code = %q, want provider_not_ready", public.Code)
				}
			})
		}
	}
}

func TestEnrollmentBeginFederatedAndProviderRecoveryUsePublicSafeWebAuthnOptions(t *testing.T) {
	provider := validVRChatEnrollmentProvider()
	account := db.Account{
		ID:                 73,
		Username:           "private-user",
		DisplayName:        "Private Display",
		WebauthnUserHandle: []byte("opaque-account-handle"),
	}
	federated := pendingEnrollment("federated-begin", enrollment.IntentFederatedRegister)
	federated.FederatedUpstreamIdpID = pgtype.Int8{Int64: provider.ID, Valid: true}
	federated.FederatedUpstreamIss = pgtype.Text{String: "https://secret-issuer.example", Valid: true}
	federated.FederatedUpstreamSub = pgtype.Text{String: "secret-subject", Valid: true}
	federated.FederatedUpstreamData = []byte(`{"userId":"secret-metadata"}`)
	federated.FederatedAvatarUrl = pgtype.Text{String: "https://secret-avatar.example/image.png", Valid: true}
	providerReset := pendingEnrollment("provider-reset-begin", enrollment.IntentReset)
	providerReset.TargetAccountID = pgtype.Int4{Int32: account.ID, Valid: true}
	providerReset.RecoverySourceUpstreamIdpID = pgtype.Int8{Int64: provider.ID, Valid: true}
	adminReset := pendingEnrollment("admin-reset-begin", enrollment.IntentReset)
	adminReset.TargetAccountID = providerReset.TargetAccountID
	q := &vrchatEnrollmentQueries{
		enrollments: map[string]db.Enrollment{
			federated.Token:     federated,
			providerReset.Token: providerReset,
			adminReset.Token:    adminReset,
		},
		accounts:  map[int32]db.Account{account.ID: account},
		providers: map[int64]db.UpstreamIdp{provider.ID: provider},
		credentials: map[int32][]db.WebauthnCredential{
			account.ID: {{CredentialID: []byte("existing-credential-id")}},
		},
	}
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })
	recorder := &recordingEnrollmentWebAuthn{}
	s := &Server{
		enrollmentQueriesOverride:  q,
		enrollmentWebAuthnOverride: recorder,
		federationRegistry:         vrchatEnrollmentRegistry(t),
		kvStore:                    store,
	}

	w := httptest.NewRecorder()
	s.handleEnrollmentBeginHTTP(w, beginEnrollmentRequest(federated.Token, `{"username":"chosen-user","displayName":"Edited Local Name","nickname":"key"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("federated begin status = %d body=%s", w.Code, w.Body.String())
	}
	if recorder.user.WebAuthnName() != "chosen-user" || recorder.user.WebAuthnDisplayName() != "Edited Local Name" {
		t.Fatalf("federated public labels = (%q, %q)", recorder.user.WebAuthnName(), recorder.user.WebAuthnDisplayName())
	}
	if len(recorder.options.CredentialExcludeList) != 0 {
		t.Fatalf("federated exclusions = %v", recorder.options.CredentialExcludeList)
	}
	raw, err := store.Get(context.Background(), enrollCeremonyKey(federated.Token))
	if err != nil {
		t.Fatal(err)
	}
	for _, identitySecret := range []string{"secret-issuer", "secret-subject", "secret-metadata", "secret-avatar"} {
		if strings.Contains(raw, identitySecret) {
			t.Fatalf("ceremony stash leaked %q: %s", identitySecret, raw)
		}
	}
	var stash enrollCeremonyStash
	if err := json.Unmarshal([]byte(raw), &stash); err != nil {
		t.Fatal(err)
	}
	if stash.Federated == nil || stash.Federated.Username != "chosen-user" || stash.Federated.DisplayName != "Edited Local Name" ||
		string(stash.Federated.WebauthnUserHandle) == "" || stash.Federated.Nickname != "key" {
		t.Fatalf("federated stash = %+v", stash.Federated)
	}

	recorder.options = protocol.PublicKeyCredentialCreationOptions{}
	w = httptest.NewRecorder()
	s.handleEnrollmentBeginHTTP(w, beginEnrollmentRequest(providerReset.Token, `{}`))
	if w.Code != http.StatusOK {
		t.Fatalf("provider recovery begin status = %d body=%s", w.Code, w.Body.String())
	}
	if recorder.user.WebAuthnName() != "account" || recorder.user.WebAuthnDisplayName() != "account" ||
		!bytes.Equal(recorder.user.WebAuthnID(), account.WebauthnUserHandle) {
		t.Fatalf("provider recovery user labels/id = (%q, %q, %q)", recorder.user.WebAuthnName(), recorder.user.WebAuthnDisplayName(), recorder.user.WebAuthnID())
	}
	if len(recorder.options.CredentialExcludeList) != 0 {
		t.Fatalf("provider recovery exposed credential descriptors: %v", recorder.options.CredentialExcludeList)
	}

	recorder.options = protocol.PublicKeyCredentialCreationOptions{}
	w = httptest.NewRecorder()
	s.handleEnrollmentBeginHTTP(w, beginEnrollmentRequest(adminReset.Token, `{}`))
	if w.Code != http.StatusOK {
		t.Fatalf("admin reset begin status = %d body=%s", w.Code, w.Body.String())
	}
	if recorder.user.WebAuthnName() != account.Username || recorder.user.WebAuthnDisplayName() != account.DisplayName {
		t.Fatalf("admin reset labels changed = (%q, %q)", recorder.user.WebAuthnName(), recorder.user.WebAuthnDisplayName())
	}
	if len(recorder.options.CredentialExcludeList) != 1 ||
		!bytes.Equal(recorder.options.CredentialExcludeList[0].CredentialID, []byte("existing-credential-id")) {
		t.Fatalf("admin reset exclusions = %v", recorder.options.CredentialExcludeList)
	}
}

const validCredentialCreationJSON = `{
	"id":"6xrtBhJQW6QU4tOaB4rrHaS2Ks0yDDL_q8jDC16DEjZ-VLVf4kCRkvl2xp2D71sTPYns-exsHQHTy3G-zJRK8g",
	"rawId":"6xrtBhJQW6QU4tOaB4rrHaS2Ks0yDDL_q8jDC16DEjZ-VLVf4kCRkvl2xp2D71sTPYns-exsHQHTy3G-zJRK8g",
	"type":"public-key",
	"response":{
		"attestationObject":"o2NmbXRkbm9uZWdhdHRTdG10oGhhdXRoRGF0YVjEdKbqkhPJnC90siSSsyDPQCYqlMGpUKA5fyklC2CEHvBBAAAAAAAAAAAAAAAAAAAAAAAAAAAAQOsa7QYSUFukFOLTmgeK6x2ktirNMgwy_6vIwwtegxI2flS1X-JAkZL5dsadg-9bEz2J7PnsbB0B08txvsyUSvKlAQIDJiABIVggLKF5xS0_BntttUIrm2Z2tgZ4uQDwllbdIfrrBMABCNciWCDHwin8Zdkr56iSIh0MrB5qZiEzYLQpEOREhMUkY6q4Vw",
		"clientDataJSON":"eyJjaGFsbGVuZ2UiOiJXOEd6RlU4cEdqaG9SYldyTERsYW1BZnFfeTRTMUNaRzFWdW9lUkxBUnJFIiwib3JpZ2luIjoiaHR0cHM6Ly93ZWJhdXRobi5pbyIsInR5cGUiOiJ3ZWJhdXRobi5jcmVhdGUifQ"
	}
}`

func completeEnrollmentRequest(token string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/prohibitorum/enrollments/"+token+"/register/complete", strings.NewReader(validCredentialCreationJSON))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func newAtomicVRChatEnrollmentServer(t *testing.T, e db.Enrollment, account *db.Account) (*Server, *vrchatEnrollmentQueries) {
	t.Helper()
	provider := validVRChatEnrollmentProvider()
	accounts := make(map[int32]db.Account)
	if account != nil {
		accounts[account.ID] = *account
	}
	q := &vrchatEnrollmentQueries{
		enrollments: map[string]db.Enrollment{e.Token: e},
		accounts:    accounts,
		providers:   map[int64]db.UpstreamIdp{provider.ID: provider},
		credentials: make(map[int32][]db.WebauthnCredential),
	}
	store := kv.NewMemoryStore()
	t.Cleanup(func() { _ = store.Close() })
	cfg := &configx.Config{SessionTTL: time.Hour}
	credential := &webauthn.Credential{
		ID:        []byte("new-credential"),
		PublicKey: []byte{0xa0},
		Transport: []protocol.AuthenticatorTransport{protocol.USB},
		Flags:     webauthn.CredentialFlags{UserVerified: true},
	}
	s := &Server{
		config:                     cfg,
		kvStore:                    store,
		sessionStore:               sessstore.NewSessionStore(store, q, cfg.SessionTTL),
		clientIP:                   newDirectResolver(),
		Audit:                      noopAuditWriter{},
		enrollmentQueriesOverride:  q,
		enrollmentTxRunnerOverride: &memoryEnrollmentTxRunner{root: q},
		enrollmentWebAuthnOverride: &recordingEnrollmentWebAuthn{credential: credential},
		federationRegistry:         vrchatEnrollmentRegistry(t),
	}
	return s, q
}

func federatedRegistrationEnrollment(token string) db.Enrollment {
	e := pendingEnrollment(token, enrollment.IntentFederatedRegister)
	e.FederatedUpstreamIdpID = pgtype.Int8{Int64: 41, Valid: true}
	e.FederatedUpstreamIdpSlug = pgtype.Text{String: "vrchat-main", Valid: true}
	e.FederatedUpstreamIss = pgtype.Text{String: "https://api.vrchat.cloud/api/1", Valid: true}
	e.FederatedUpstreamSub = pgtype.Text{String: "usr_123", Valid: true}
	e.FederatedDisplayName = pgtype.Text{String: "VRChat Suggested", Valid: true}
	e.FederatedUpstreamData = []byte(`{"displayName":"VRChat Suggested","profileUrl":"https://vrchat.com/home/user/usr_123","userId":"usr_123"}`)
	e.FederatedAvatarUrl = pgtype.Text{String: "https://api.vrchat.cloud/api/1/file/avatar", Valid: true}
	return e
}

func beginFederatedRegistration(t *testing.T, s *Server, e db.Enrollment, username, displayName string) {
	t.Helper()
	w := httptest.NewRecorder()
	s.handleEnrollmentBeginHTTP(w, beginEnrollmentRequest(e.Token, `{"username":"`+username+`","displayName":"`+displayName+`"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("begin status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestFederatedRegistrationCompletionCommitsAccountCredentialIdentityAndSession(t *testing.T) {
	e := federatedRegistrationEnrollment("complete-registration")
	s, q := newAtomicVRChatEnrollmentServer(t, e, nil)
	beginFederatedRegistration(t, s, e, "local-choice", "Edited Local Display")

	w := httptest.NewRecorder()
	s.handleEnrollmentCompleteHTTP(w, completeEnrollmentRequest(e.Token))
	if w.Code != http.StatusOK {
		t.Fatalf("complete status = %d body=%s", w.Code, w.Body.String())
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.accounts) != 1 {
		t.Fatalf("accounts = %d, want 1", len(q.accounts))
	}
	var account db.Account
	for _, account = range q.accounts {
	}
	if account.Username != "local-choice" || account.DisplayName != "Edited Local Display" || account.Role != "user" ||
		string(account.Attributes) != "{}" || account.Disabled {
		t.Fatalf("account = %+v", account)
	}
	if credentials := q.credentials[account.ID]; len(credentials) != 1 || !bytes.Equal(credentials[0].CredentialID, []byte("new-credential")) {
		t.Fatalf("credentials = %+v", credentials)
	}
	if len(q.identities) != 1 {
		t.Fatalf("identities = %d, want 1", len(q.identities))
	}
	identity := q.identities[0]
	if identity.AccountID != account.ID || identity.UpstreamIdpID != e.FederatedUpstreamIdpID.Int64 ||
		identity.UpstreamIss != e.FederatedUpstreamIss.String || identity.UpstreamSub != e.FederatedUpstreamSub.String ||
		!bytes.Equal(identity.UpstreamData, e.FederatedUpstreamData) || !identity.ConfirmedAt.Valid {
		t.Fatalf("identity = %+v", identity)
	}
	if !q.enrollments[e.Token].ConsumedAt.Valid {
		t.Fatal("enrollment was not consumed")
	}
	activeSessions := 0
	for _, session := range q.sessions {
		if !session.RevokedAt.Valid {
			activeSessions++
		}
	}
	if activeSessions != 1 {
		t.Fatalf("active sessions = %d, want 1", activeSessions)
	}
}

func TestFederatedRegistrationCompletionRollsBackConflictsProviderChangesAndBadStash(t *testing.T) {
	tests := map[string]struct {
		mutate   func(*testing.T, *Server, *vrchatEnrollmentQueries, db.Enrollment)
		wantCode string
	}{
		"username race": {
			mutate: func(_ *testing.T, _ *Server, q *vrchatEnrollmentQueries, _ db.Enrollment) {
				q.mu.Lock()
				q.accounts[9] = db.Account{ID: 9, Username: "racing-user"}
				q.mu.Unlock()
			},
			wantCode: "username_taken",
		},
		"identity uniqueness race": {
			mutate: func(_ *testing.T, _ *Server, q *vrchatEnrollmentQueries, _ db.Enrollment) {
				q.mu.Lock()
				q.insertIdentityErr = &pgconn.PgError{Code: "23505", ConstraintName: "account_identity_upstream_iss_upstream_sub_key"}
				q.mu.Unlock()
			},
			wantCode: "federation_identity_conflict",
		},
		"provider disabled after begin": {
			mutate: func(_ *testing.T, _ *Server, q *vrchatEnrollmentQueries, _ db.Enrollment) {
				q.mu.Lock()
				provider := q.providers[41]
				provider.Disabled = true
				q.providers[41] = provider
				q.mu.Unlock()
			},
			wantCode: "provider_not_ready",
		},
		"provider mode changed after begin": {
			mutate: func(_ *testing.T, _ *Server, q *vrchatEnrollmentQueries, _ db.Enrollment) {
				q.mu.Lock()
				provider := q.providers[41]
				provider.Mode = federation.ModeAutoProvision
				q.providers[41] = provider
				q.mu.Unlock()
			},
			wantCode: "provider_not_ready",
		},
		"provider became unready after begin": {
			mutate: func(_ *testing.T, _ *Server, q *vrchatEnrollmentQueries, _ db.Enrollment) {
				q.mu.Lock()
				provider := q.providers[41]
				provider.SecretStatus = "invalid"
				q.providers[41] = provider
				q.mu.Unlock()
			},
			wantCode: "provider_not_ready",
		},
		"provider protocol changed after begin": {
			mutate: func(_ *testing.T, _ *Server, q *vrchatEnrollmentQueries, _ db.Enrollment) {
				q.mu.Lock()
				provider := q.providers[41]
				provider.Protocol = "oidc"
				q.providers[41] = provider
				q.mu.Unlock()
			},
			wantCode: "provider_not_ready",
		},
		"provider deleted after begin": {
			mutate: func(_ *testing.T, _ *Server, q *vrchatEnrollmentQueries, _ db.Enrollment) {
				q.mu.Lock()
				delete(q.providers, 41)
				q.mu.Unlock()
			},
			wantCode: "provider_not_ready",
		},
		"ceremony stash missing proposal": {
			mutate: func(t *testing.T, s *Server, _ *vrchatEnrollmentQueries, e db.Enrollment) {
				t.Helper()
				raw, err := json.Marshal(enrollCeremonyStash{Data: webauthn.SessionData{UserID: []byte("substitute")}})
				if err != nil {
					t.Fatal(err)
				}
				if err := s.kvStore.SetEx(context.Background(), enrollCeremonyKey(e.Token), string(raw), time.Minute); err != nil {
					t.Fatal(err)
				}
			},
			wantCode: "ceremony_state_invalid",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			e := federatedRegistrationEnrollment("rollback-" + strings.ReplaceAll(name, " ", "-"))
			s, q := newAtomicVRChatEnrollmentServer(t, e, nil)
			username := "available-user"
			if name == "username race" {
				username = "racing-user"
			}
			beginFederatedRegistration(t, s, e, username, "Local Display")
			test.mutate(t, s, q, e)

			w := httptest.NewRecorder()
			s.handleEnrollmentCompleteHTTP(w, completeEnrollmentRequest(e.Token))
			var public struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &public); err != nil {
				t.Fatalf("decode response status=%d body=%s: %v", w.Code, w.Body.String(), err)
			}
			if public.Code != test.wantCode {
				t.Fatalf("code = %q status=%d body=%s, want %q", public.Code, w.Code, w.Body.String(), test.wantCode)
			}

			q.mu.Lock()
			defer q.mu.Unlock()
			if q.enrollments[e.Token].ConsumedAt.Valid {
				t.Fatal("failed completion consumed enrollment")
			}
			createdAccounts := 0
			for _, account := range q.accounts {
				if account.ID != 9 {
					createdAccounts++
				}
			}
			if createdAccounts != 0 || len(q.credentials) != 0 || len(q.identities) != 0 {
				t.Fatalf("rollback left accounts=%d credentials=%d identities=%d", createdAccounts, len(q.credentials), len(q.identities))
			}
		})
	}
}

func TestProviderRecoveryCompletionAtomicallyReplacesCredentialsAndSessions(t *testing.T) {
	account := db.Account{ID: 73, Username: "private-user", DisplayName: "Private Name", WebauthnUserHandle: []byte("opaque-handle")}
	e := pendingEnrollment("provider-recovery-complete", enrollment.IntentReset)
	e.TargetAccountID = pgtype.Int4{Int32: account.ID, Valid: true}
	e.RecoverySourceUpstreamIdpID = pgtype.Int8{Int64: 41, Valid: true}
	s, q := newAtomicVRChatEnrollmentServer(t, e, &account)
	audits := &enrollmentAuditCapture{}
	s.Audit = audits
	q.credentials[account.ID] = []db.WebauthnCredential{{ID: 8, AccountID: account.ID, CredentialID: []byte("old-credential")}}
	if _, _, err := s.sessionStore.Issue(context.Background(), account.ID, "127.0.0.1", "old-one", []string{"hwk"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.sessionStore.Issue(context.Background(), account.ID, "127.0.0.1", "old-two", []string{"hwk"}, nil); err != nil {
		t.Fatal(err)
	}
	q.mu.Lock()
	q.sessionOps = nil
	q.mu.Unlock()
	w := httptest.NewRecorder()
	s.handleEnrollmentBeginHTTP(w, beginEnrollmentRequest(e.Token, `{}`))
	if w.Code != http.StatusOK {
		t.Fatalf("begin status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	s.handleEnrollmentCompleteHTTP(w, completeEnrollmentRequest(e.Token))
	if w.Code != http.StatusOK {
		t.Fatalf("complete status=%d body=%s", w.Code, w.Body.String())
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if credentials := q.credentials[account.ID]; len(credentials) != 1 || !bytes.Equal(credentials[0].CredentialID, []byte("new-credential")) {
		t.Fatalf("credentials = %+v", credentials)
	}
	if q.revokeCalls != 1 {
		t.Fatalf("revoke calls = %d, want 1", q.revokeCalls)
	}
	if got := strings.Join(q.sessionOps, ","); got != "revoke,issue" {
		t.Fatalf("session operations = %q, want revoke,issue", got)
	}
	active := 0
	for _, session := range q.sessions {
		if !session.RevokedAt.Valid {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("active sessions = %d, want exactly fresh session", active)
	}
	var consumedAudit *audit.Record
	for i := range audits.records {
		if audits.records[i].Event == audit.EventEnrollmentConsumed {
			consumedAudit = &audits.records[i]
		}
	}
	if consumedAudit == nil || len(consumedAudit.Detail) != 2 ||
		consumedAudit.Detail["intent"] != enrollment.IntentReset || consumedAudit.Detail["source"] != "vrchat" {
		t.Fatalf("provider recovery audit = %+v", consumedAudit)
	}
}

func TestProviderRecoveryCompletionFailsClosedWhenSessionRevocationFails(t *testing.T) {
	account := db.Account{ID: 74, Username: "private-user", DisplayName: "Private Name", WebauthnUserHandle: []byte("opaque-handle")}
	e := pendingEnrollment("provider-recovery-revocation-failure", enrollment.IntentReset)
	e.TargetAccountID = pgtype.Int4{Int32: account.ID, Valid: true}
	e.RecoverySourceUpstreamIdpID = pgtype.Int8{Int64: 41, Valid: true}
	s, q := newAtomicVRChatEnrollmentServer(t, e, &account)
	failingStore := &failingSessionRevocationStore{Store: s.kvStore}
	s.sessionStore = sessstore.NewSessionStore(failingStore, q, s.config.SessionTTL)
	q.credentials[account.ID] = []db.WebauthnCredential{{ID: 8, AccountID: account.ID, CredentialID: []byte("old-credential")}}
	if _, _, err := s.sessionStore.Issue(context.Background(), account.ID, "127.0.0.1", "old-session", []string{"hwk"}, nil); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	s.handleEnrollmentBeginHTTP(w, beginEnrollmentRequest(e.Token, `{}`))
	if w.Code != http.StatusOK {
		t.Fatalf("begin status=%d body=%s", w.Code, w.Body.String())
	}
	failingStore.scanErr = errors.New("revocation backend secret provider=41 account=74 token=compromised")

	w = httptest.NewRecorder()
	s.handleEnrollmentCompleteHTTP(w, completeEnrollmentRequest(e.Token))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("complete status=%d body=%s, want 500", w.Code, w.Body.String())
	}
	var public map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &public); err != nil {
		t.Fatalf("decode safe response: %v; body=%s", err, w.Body.String())
	}
	if len(public) != 2 || public["code"] != "server_error" {
		t.Fatalf("public response = %#v, want safe server_error envelope", public)
	}
	if strings.Contains(w.Body.String(), "provider") || strings.Contains(w.Body.String(), "account") ||
		strings.Contains(w.Body.String(), "token") || strings.Contains(w.Body.String(), account.Username) {
		t.Fatalf("public response leaked revocation context: %s", w.Body.String())
	}
	if cookies := w.Header().Values("Set-Cookie"); len(cookies) != 0 {
		t.Fatalf("failure issued session cookie: %v", cookies)
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.sessions) != 1 {
		t.Fatalf("session rows = %d, want no fresh issuance after revocation failure", len(q.sessions))
	}
	if !q.enrollments[e.Token].ConsumedAt.Valid {
		t.Fatal("response concealed that committed enrollment consumption already occurred")
	}
	if credentials := q.credentials[account.ID]; len(credentials) != 1 || !bytes.Equal(credentials[0].CredentialID, []byte("new-credential")) {
		t.Fatalf("committed credential replacement = %+v", credentials)
	}
}

func TestProviderRecoveryCompletionRejectsChangedProviderOrDisabledTargetBeforeDeletion(t *testing.T) {
	tests := map[string]struct {
		mutate   func(*vrchatEnrollmentQueries, int32)
		wantCode string
	}{
		"provider changed": {
			mutate: func(q *vrchatEnrollmentQueries, _ int32) {
				provider := q.providers[41]
				provider.Mode = federation.ModeAutoProvision
				q.providers[41] = provider
			},
			wantCode: "provider_not_ready",
		},
		"target disabled": {
			mutate: func(q *vrchatEnrollmentQueries, accountID int32) {
				account := q.accounts[accountID]
				account.Disabled = true
				q.accounts[accountID] = account
			},
			wantCode: "enrollment_consumed",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			account := db.Account{ID: 73, Username: "private-user", DisplayName: "Private Name", WebauthnUserHandle: []byte("opaque-handle")}
			e := pendingEnrollment("provider-recovery-reject-"+strings.ReplaceAll(name, " ", "-"), enrollment.IntentReset)
			e.TargetAccountID = pgtype.Int4{Int32: account.ID, Valid: true}
			e.RecoverySourceUpstreamIdpID = pgtype.Int8{Int64: 41, Valid: true}
			s, q := newAtomicVRChatEnrollmentServer(t, e, &account)
			q.credentials[account.ID] = []db.WebauthnCredential{{ID: 8, AccountID: account.ID, CredentialID: []byte("old-credential")}}
			w := httptest.NewRecorder()
			s.handleEnrollmentBeginHTTP(w, beginEnrollmentRequest(e.Token, `{}`))
			if w.Code != http.StatusOK {
				t.Fatalf("begin status=%d body=%s", w.Code, w.Body.String())
			}
			q.mu.Lock()
			test.mutate(q, account.ID)
			q.mu.Unlock()

			w = httptest.NewRecorder()
			s.handleEnrollmentCompleteHTTP(w, completeEnrollmentRequest(e.Token))
			var public struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &public); err != nil {
				t.Fatal(err)
			}
			if public.Code != test.wantCode {
				t.Fatalf("code=%q status=%d body=%s, want %q", public.Code, w.Code, w.Body.String(), test.wantCode)
			}
			q.mu.Lock()
			defer q.mu.Unlock()
			if q.enrollments[e.Token].ConsumedAt.Valid {
				t.Fatal("failed recovery consumed enrollment")
			}
			credentials := q.credentials[account.ID]
			if len(credentials) != 1 || !bytes.Equal(credentials[0].CredentialID, []byte("old-credential")) {
				t.Fatalf("failed recovery changed credentials: %+v", credentials)
			}
		})
	}
}

func TestFederatedRegistrationConcurrentCompletionHasOneWinner(t *testing.T) {
	e := federatedRegistrationEnrollment("concurrent-registration")
	s, q := newAtomicVRChatEnrollmentServer(t, e, nil)
	beginFederatedRegistration(t, s, e, "concurrent-user", "Concurrent User")

	start := make(chan struct{})
	statuses := make(chan int, 2)
	for range 2 {
		go func() {
			<-start
			w := httptest.NewRecorder()
			s.handleEnrollmentCompleteHTTP(w, completeEnrollmentRequest(e.Token))
			statuses <- w.Code
		}()
	}
	close(start)
	first, second := <-statuses, <-statuses
	successes := 0
	for _, status := range []int{first, second} {
		if status == http.StatusOK {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("statuses = %d, %d; want one winner", first, second)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.accounts) != 1 || len(q.identities) != 1 {
		t.Fatalf("accounts=%d identities=%d", len(q.accounts), len(q.identities))
	}
}

func TestFederatedRegistrationAvatarFailureAfterCommitDoesNotAlterSuccess(t *testing.T) {
	e := federatedRegistrationEnrollment("avatar-failure-registration")
	s, q := newAtomicVRChatEnrollmentServer(t, e, nil)
	calls := 0
	s.enrollmentAvatarOverride = func(accountID int32, provider federation.Provider, delivery federation.AvatarDelivery) error {
		calls++
		if accountID == 0 || provider.ID != e.FederatedUpstreamIdpID.Int64 || delivery.URL != e.FederatedAvatarUrl.String {
			t.Fatalf("avatar inheritance args = account=%d provider=%+v delivery=%+v", accountID, provider, delivery)
		}
		q.mu.Lock()
		committed := q.enrollments[e.Token].ConsumedAt.Valid && len(q.accounts) == 1
		q.mu.Unlock()
		if !committed {
			t.Fatal("avatar inheritance ran before account transaction commit")
		}
		return errors.New("avatar fetch failed")
	}
	beginFederatedRegistration(t, s, e, "avatar-user", "Avatar User")

	w := httptest.NewRecorder()
	s.handleEnrollmentCompleteHTTP(w, completeEnrollmentRequest(e.Token))
	if w.Code != http.StatusOK {
		t.Fatalf("complete status=%d body=%s", w.Code, w.Body.String())
	}
	if calls != 1 {
		t.Fatalf("avatar calls = %d, want 1", calls)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.accounts) != 1 || !q.enrollments[e.Token].ConsumedAt.Valid {
		t.Fatalf("avatar failure changed committed result: accounts=%d consumed=%v", len(q.accounts), q.enrollments[e.Token].ConsumedAt.Valid)
	}
}
