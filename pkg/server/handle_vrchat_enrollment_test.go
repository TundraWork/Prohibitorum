package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
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
	"prohibitorum/pkg/credential/password"
	"prohibitorum/pkg/credential/totp"
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

// foreignKeyAuditCapture stands in for the real database: an audit row that
// references an account the caller's writer cannot see fails the same way the
// credential_event.account_id foreign key does. insertInviteIdentityTx must
// therefore pass the transaction's writer, not the server's shared one.
type foreignKeyAuditCapture struct {
	enrollmentAuditCapture
	// txVisible is the transaction writer's view; the shared writer (the
	// capture itself) never sees the transaction's inserts.
	txVisible map[int32]bool
}

func (c *foreignKeyAuditCapture) Record(ctx context.Context, record audit.Record) error {
	if record.AccountID != nil {
		return fmt.Errorf("pg mock: credential_event_account_id_fkey violated for account %d", *record.AccountID)
	}
	return c.enrollmentAuditCapture.Record(ctx, record)
}

// txWriter is what enrollmentTx.AuditWriter returns: the same record log, but
// with the transaction's own view of which accounts exist.
type txWriter struct {
	c       *foreignKeyAuditCapture
	visible map[int32]bool
}

func (w txWriter) Record(ctx context.Context, record audit.Record) error {
	if record.AccountID != nil && !w.visible[*record.AccountID] {
		return fmt.Errorf("pg mock: credential_event_account_id_fkey violated for account %d", *record.AccountID)
	}
	return w.c.enrollmentAuditCapture.Record(ctx, record)
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
	mu                  sync.Mutex
	enrollments         map[string]db.Enrollment
	accounts            map[int32]db.Account
	providers           map[int64]db.UpstreamIdp
	providerErr         error
	providerLockedCalls int
	accountLockedCalls  int
	credentials         map[int32][]db.WebauthnCredential
	identities          []db.AccountIdentity
	sessions            []db.Session
	passwords           map[int32]string
	totp                map[int32]db.TotpCredential
	recovery            map[int32][]db.RecoveryCode
	nextAccountID       int32
	nextCredentialID    int32
	nextIdentityID      int64
	// insertHook fires after InsertAccount so the FK-aware audit fake can mark
	// the new account visible to this transaction's writer.
	insertHook        func(id int32)
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

func (q *vrchatEnrollmentQueries) GetAccountByIDForUpdate(_ context.Context, id int32) (db.Account, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.accountLockedCalls++
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

func (q *vrchatEnrollmentQueries) GetUpstreamIDPByIDForUpdate(_ context.Context, id int64) (db.UpstreamIdp, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.providerLockedCalls++
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

func (q *vrchatEnrollmentQueries) GetUpstreamIDPBySlug(ctx context.Context, slug string) (db.UpstreamIdp, error) {
	return q.GetUpstreamIDPBySlugAny(ctx, slug)
}

func (q *vrchatEnrollmentQueries) ListUpstreamIDPs(_ context.Context) ([]db.UpstreamIdp, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]db.UpstreamIdp, 0, len(q.providers))
	for _, provider := range q.providers {
		out = append(out, provider)
	}
	return out, nil
}

func (q *vrchatEnrollmentQueries) ListEntityIconEtags(_ context.Context, _ string) ([]db.ListEntityIconEtagsRow, error) {
	return nil, nil
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
	if q.insertHook != nil {
		q.insertHook(account.ID)
	}
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

func (q *vrchatEnrollmentQueries) UpsertPasswordCredential(_ context.Context, p db.UpsertPasswordCredentialParams) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.passwords == nil {
		q.passwords = map[int32]string{}
	}
	q.passwords[p.AccountID] = p.Hash
	return nil
}

func (q *vrchatEnrollmentQueries) GetPasswordCredential(_ context.Context, accountID int32) (db.PasswordCredential, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if hash, ok := q.passwords[accountID]; ok {
		return db.PasswordCredential{AccountID: accountID, Hash: hash}, nil
	}
	return db.PasswordCredential{}, pgx.ErrNoRows
}

func (q *vrchatEnrollmentQueries) DeleteTOTPCredential(_ context.Context, accountID int32) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.totp, accountID)
	return nil
}

func (q *vrchatEnrollmentQueries) InsertTOTPCredential(_ context.Context, p db.InsertTOTPCredentialParams) (db.TotpCredential, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.totp == nil {
		q.totp = map[int32]db.TotpCredential{}
	}
	row := db.TotpCredential{AccountID: p.AccountID, SecretEnc: p.SecretEnc, SecretNonce: p.SecretNonce, KeyVersion: p.KeyVersion, Period: p.Period, Digits: p.Digits, Algorithm: p.Algorithm}
	q.totp[p.AccountID] = row
	return row, nil
}

func (q *vrchatEnrollmentQueries) ConfirmTOTPCredential(_ context.Context, accountID int32) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if row, ok := q.totp[accountID]; ok {
		row.ConfirmedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
		q.totp[accountID] = row
	}
	return nil
}

func (q *vrchatEnrollmentQueries) UpdateTOTPLastStep(_ context.Context, p db.UpdateTOTPLastStepParams) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if row, ok := q.totp[p.AccountID]; ok {
		row.LastStep = p.LastStep
		q.totp[p.AccountID] = row
	}
	return 1, nil
}

func (q *vrchatEnrollmentQueries) DeleteAllRecoveryCodesByAccount(_ context.Context, accountID int32) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.recovery, accountID)
	return nil
}

func (q *vrchatEnrollmentQueries) InsertRecoveryCode(_ context.Context, p db.InsertRecoveryCodeParams) (db.RecoveryCode, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.recovery == nil {
		q.recovery = map[int32][]db.RecoveryCode{}
	}
	row := db.RecoveryCode{ID: int32(len(q.recovery[p.AccountID]) + 1), AccountID: p.AccountID, Hash: p.Hash}
	q.recovery[p.AccountID] = append(q.recovery[p.AccountID], row)
	return row, nil
}

func cloneEnrollmentQueries(q *vrchatEnrollmentQueries) *vrchatEnrollmentQueries {
	clone := &vrchatEnrollmentQueries{
		enrollments:         make(map[string]db.Enrollment, len(q.enrollments)),
		providers:           make(map[int64]db.UpstreamIdp, len(q.providers)),
		accounts:            make(map[int32]db.Account, len(q.accounts)),
		credentials:         make(map[int32][]db.WebauthnCredential, len(q.credentials)),
		providerErr:         q.providerErr,
		providerLockedCalls: q.providerLockedCalls,
		accountLockedCalls:  q.accountLockedCalls,
		identities:          append([]db.AccountIdentity(nil), q.identities...),
		sessions:            append([]db.Session(nil), q.sessions...),
		passwords:           cloneStringMap(q.passwords),
		totp:                cloneTotpMap(q.totp),
		recovery:            cloneRecoveryMap(q.recovery),
		nextAccountID:       q.nextAccountID,
		nextCredentialID:    q.nextCredentialID,
		nextIdentityID:      q.nextIdentityID,
		insertHook:          q.insertHook,
		insertAccountErr:    q.insertAccountErr,
		insertIdentityErr:   q.insertIdentityErr,
	}
	for token, row := range q.enrollments {
		clone.enrollments[token] = row
	}
	for id, row := range q.accounts {
		clone.accounts[id] = row
	}
	for id, row := range q.providers {
		clone.providers[id] = row
	}
	for id, rows := range q.credentials {
		clone.credentials[id] = append([]db.WebauthnCredential(nil), rows...)
	}
	return clone
}

func cloneStringMap(m map[int32]string) map[int32]string {
	if m == nil {
		return nil
	}
	out := make(map[int32]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneTotpMap(m map[int32]db.TotpCredential) map[int32]db.TotpCredential {
	if m == nil {
		return nil
	}
	out := make(map[int32]db.TotpCredential, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneRecoveryMap(m map[int32][]db.RecoveryCode) map[int32][]db.RecoveryCode {
	if m == nil {
		return nil
	}
	out := make(map[int32][]db.RecoveryCode, len(m))
	for k, v := range m {
		out[k] = append([]db.RecoveryCode(nil), v...)
	}
	return out
}

type memoryEnrollmentTxRunner struct {
	root  *vrchatEnrollmentQueries
	audit audit.Writer
}

func (r *memoryEnrollmentTxRunner) BeginEnrollmentTx(context.Context) (enrollmentTx, error) {
	r.root.mu.Lock()
	tx := &memoryEnrollmentTx{root: r.root, q: cloneEnrollmentQueries(r.root), audit: r.audit, active: true}
	if _, ok := r.audit.(*foreignKeyAuditCapture); ok {
		// The transaction's writer sees every account this transaction inserts.
		tx.fkView = map[int32]bool{}
		tx.q.insertHook = func(id int32) { tx.fkView[id] = true }
	}
	return tx, nil
}

type memoryEnrollmentTx struct {
	root   *vrchatEnrollmentQueries
	q      *vrchatEnrollmentQueries
	audit  audit.Writer
	fkView map[int32]bool
	active bool
}

func (tx *memoryEnrollmentTx) Queries() db.Querier { return tx.q }

func (tx *memoryEnrollmentTx) AuditWriter() audit.Writer {
	if tx.fkView != nil {
		if fk, ok := tx.audit.(*foreignKeyAuditCapture); ok {
			return txWriter{c: fk, visible: tx.fkView}
		}
	}
	return tx.audit
}

func (tx *memoryEnrollmentTx) Commit(context.Context) error {
	if !tx.active {
		return errors.New("transaction closed")
	}
	tx.root.enrollments = tx.q.enrollments
	tx.root.accounts = tx.q.accounts
	tx.root.providers = tx.q.providers
	tx.root.providerLockedCalls = tx.q.providerLockedCalls
	tx.root.accountLockedCalls = tx.q.accountLockedCalls
	tx.root.credentials = tx.q.credentials
	tx.root.identities = tx.q.identities
	tx.root.passwords = tx.q.passwords
	tx.root.totp = tx.q.totp
	tx.root.recovery = tx.q.recovery
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
		Mode:           federation.ModeInviteOnly,
		AllowSignIn:    false,
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

func TestEnrollmentPreviewInviteWithSnapshotReturnsVerifiedIdentity(t *testing.T) {
	provider := validVRChatEnrollmentProvider()
	e := pendingEnrollment("verified-invite", enrollment.IntentInvite)
	e.ExpectedUpstreamIdpSlug = pgtype.Text{String: provider.Slug, Valid: true}
	e.RequireLocalCredential = true
	e.FederatedUpstreamIdpID = pgtype.Int8{Int64: provider.ID, Valid: true}
	e.FederatedUpstreamIdpSlug = pgtype.Text{String: provider.Slug, Valid: true}
	e.FederatedUpstreamIss = pgtype.Text{String: "https://secret-issuer.example", Valid: true}
	e.FederatedUpstreamSub = pgtype.Text{String: "secret-subject", Valid: true}
	e.FederatedDisplayName = pgtype.Text{String: "VRChat Suggested", Valid: true}
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
	if out.Body.Intent != enrollment.IntentInvite || out.Body.ExpiresAt != e.ExpiresAt.Time || !out.Body.RequireLocalCredential {
		t.Fatalf("preview = %+v", out.Body)
	}
	if out.Body.VerifiedIdentity == nil ||
		out.Body.VerifiedIdentity.ProviderSlug != provider.Slug ||
		out.Body.VerifiedIdentity.ProviderDisplayName != provider.DisplayName ||
		out.Body.VerifiedIdentity.DisplayName != "VRChat Suggested" ||
		out.Body.VerifiedIdentity.AvatarURL == nil || *out.Body.VerifiedIdentity.AvatarURL != "https://secret-avatar.example/image.png" {
		t.Fatalf("verified identity = %+v", out.Body.VerifiedIdentity)
	}
	if out.Body.Target != nil {
		t.Fatalf("invite preview exposed target %+v", out.Body.Target)
	}
	raw, err := json.Marshal(out.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret-issuer", "secret-subject", "secret-metadata"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("preview leaked %q: %s", secret, raw)
		}
	}
}

func TestEnrollmentPreviewInviteSnapshotOmittedWhenProviderUnusable(t *testing.T) {
	provider := validVRChatEnrollmentProvider()
	provider.Disabled = true
	e := pendingEnrollment("stale-snapshot-invite", enrollment.IntentInvite)
	e.ExpectedUpstreamIdpSlug = pgtype.Text{String: provider.Slug, Valid: true}
	e.FederatedUpstreamIdpID = pgtype.Int8{Int64: provider.ID, Valid: true}
	e.FederatedUpstreamIdpSlug = pgtype.Text{String: provider.Slug, Valid: true}
	e.FederatedUpstreamIss = pgtype.Text{String: "https://secret-issuer.example", Valid: true}
	e.FederatedUpstreamSub = pgtype.Text{String: "secret-subject", Valid: true}
	e.FederatedDisplayName = pgtype.Text{String: "Suggested", Valid: true}
	e.FederatedUpstreamData = []byte(`{"userId":"secret-metadata"}`)
	q := &vrchatEnrollmentQueries{
		enrollments: map[string]db.Enrollment{e.Token: e},
		providers:   map[int64]db.UpstreamIdp{provider.ID: provider},
	}
	s := &Server{enrollmentQueriesOverride: q, federationRegistry: vrchatEnrollmentRegistry(t)}

	out, err := s.handlePreviewEnrollment(context.Background(), &previewIn{Token: e.Token})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if out.Body.VerifiedIdentity != nil {
		t.Fatalf("stale snapshot exposed verified identity: %+v", out.Body.VerifiedIdentity)
	}
}

func TestEnrollmentPreviewResetShowsTarget(t *testing.T) {
	account := db.Account{ID: 73, Username: "private-user", DisplayName: "Private Name"}
	adminReset := pendingEnrollment("admin-reset", enrollment.IntentReset)
	adminReset.TargetAccountID = pgtype.Int4{Int32: account.ID, Valid: true}
	q := &vrchatEnrollmentQueries{
		enrollments: map[string]db.Enrollment{adminReset.Token: adminReset},
		accounts:    map[int32]db.Account{account.ID: account},
	}
	s := &Server{enrollmentQueriesOverride: q}

	out, err := s.handlePreviewEnrollment(context.Background(), &previewIn{Token: adminReset.Token})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if out.Body.Target == nil || out.Body.Target.Username != account.Username || out.Body.Target.DisplayName != account.DisplayName {
		t.Fatalf("target = %+v", out.Body.Target)
	}
}

func TestEnrollmentPreviewResetFailsWithoutReadableTarget(t *testing.T) {
	missing := pendingEnrollment("missing-target", enrollment.IntentReset)
	missing.TargetAccountID = pgtype.Int4{Int32: 404, Valid: true}
	unset := pendingEnrollment("unset-target", enrollment.IntentReset)
	q := &vrchatEnrollmentQueries{
		enrollments: map[string]db.Enrollment{missing.Token: missing, unset.Token: unset},
		accounts:    map[int32]db.Account{},
	}
	s := &Server{enrollmentQueriesOverride: q}

	for _, token := range []string{missing.Token, unset.Token} {
		out, err := s.handlePreviewEnrollment(context.Background(), &previewIn{Token: token})
		if err == nil {
			t.Fatalf("%s preview succeeded without a target: %+v", token, out.Body)
		}
		if authn.AsAuthError(err) != nil {
			t.Fatalf("%s preview error = %v, want an internal failure", token, err)
		}
	}
}

func TestEnrollmentPreviewInviteSnapshotRejectsUnusableProvider(t *testing.T) {
	baseProvider := validVRChatEnrollmentProvider()
	cases := map[string]struct {
		mutate  func(*db.UpstreamIdp)
		missing bool
	}{
		"disabled":    {mutate: func(p *db.UpstreamIdp) { p.Disabled = true }},
		"deleted":     {missing: true},
		"wrong mode":  {mutate: func(p *db.UpstreamIdp) { p.Mode = federation.ModeLinkOnly }},
		"slug change": {mutate: func(p *db.UpstreamIdp) { p.Slug = "renamed" }},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			provider := baseProvider
			if tc.mutate != nil {
				tc.mutate(&provider)
			}
			e := pendingEnrollment("token", enrollment.IntentInvite)
			e.ExpectedUpstreamIdpSlug = pgtype.Text{String: baseProvider.Slug, Valid: true}
			e.FederatedUpstreamIdpID = pgtype.Int8{Int64: baseProvider.ID, Valid: true}
			e.FederatedUpstreamIdpSlug = pgtype.Text{String: baseProvider.Slug, Valid: true}
			e.FederatedUpstreamIss = pgtype.Text{String: "https://issuer.example", Valid: true}
			e.FederatedUpstreamSub = pgtype.Text{String: "subject", Valid: true}
			e.FederatedDisplayName = pgtype.Text{String: "VRChat User", Valid: true}
			e.FederatedUpstreamData = []byte(`{"userId":"subject"}`)
			providers := map[int64]db.UpstreamIdp{provider.ID: provider}
			if tc.missing {
				providers = map[int64]db.UpstreamIdp{}
			}
			q := &vrchatEnrollmentQueries{
				enrollments: map[string]db.Enrollment{e.Token: e},
				accounts:    map[int32]db.Account{},
				providers:   providers,
			}
			s := &Server{enrollmentQueriesOverride: q, federationRegistry: vrchatEnrollmentRegistry(t)}

			out, err := s.handlePreviewEnrollment(context.Background(), &previewIn{Token: e.Token})
			if err != nil {
				t.Fatalf("preview: %v", err)
			}
			if out.Body.VerifiedIdentity != nil {
				t.Fatalf("unusable provider exposed verified identity: %+v", out.Body.VerifiedIdentity)
			}
		})
	}
}

func TestEnrollmentBeginVerifiedInviteAndAdminResetUsePublicSafeWebAuthnOptions(t *testing.T) {
	provider := validVRChatEnrollmentProvider()
	account := db.Account{
		ID:                 73,
		Username:           "private-user",
		DisplayName:        "Private Display",
		WebauthnUserHandle: []byte("opaque-account-handle"),
	}
	verifiedInvite := pendingEnrollment("verified-invite-begin", enrollment.IntentInvite)
	verifiedInvite.ExpectedUpstreamIdpSlug = pgtype.Text{String: provider.Slug, Valid: true}
	verifiedInvite.FederatedUpstreamIdpID = pgtype.Int8{Int64: provider.ID, Valid: true}
	verifiedInvite.FederatedUpstreamIdpSlug = pgtype.Text{String: provider.Slug, Valid: true}
	verifiedInvite.FederatedUpstreamIss = pgtype.Text{String: "https://secret-issuer.example", Valid: true}
	verifiedInvite.FederatedUpstreamSub = pgtype.Text{String: "secret-subject", Valid: true}
	verifiedInvite.FederatedDisplayName = pgtype.Text{String: "Suggested Display", Valid: true}
	verifiedInvite.FederatedUpstreamData = []byte(`{"userId":"secret-metadata"}`)
	verifiedInvite.FederatedAvatarUrl = pgtype.Text{String: "https://secret-avatar.example/image.png", Valid: true}
	adminReset := pendingEnrollment("admin-reset-begin", enrollment.IntentReset)
	adminReset.TargetAccountID = pgtype.Int4{Int32: account.ID, Valid: true}
	q := &vrchatEnrollmentQueries{
		enrollments: map[string]db.Enrollment{
			verifiedInvite.Token: verifiedInvite,
			adminReset.Token:     adminReset,
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
	s.handleEnrollmentBeginHTTP(w, beginEnrollmentRequest(verifiedInvite.Token, `{"username":"chosen-user","displayName":"Edited Local Name","nickname":"key"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("verified invite begin status = %d body=%s", w.Code, w.Body.String())
	}
	if recorder.user.WebAuthnName() != "chosen-user" || recorder.user.WebAuthnDisplayName() != "Edited Local Name" {
		t.Fatalf("verified invite public labels = (%q, %q)", recorder.user.WebAuthnName(), recorder.user.WebAuthnDisplayName())
	}
	if len(recorder.options.CredentialExcludeList) != 0 {
		t.Fatalf("verified invite exclusions = %v", recorder.options.CredentialExcludeList)
	}
	raw, err := store.Get(context.Background(), enrollCeremonyKey(verifiedInvite.Token))
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
	if stash.Invite == nil || stash.Invite.Username != "chosen-user" || stash.Invite.DisplayName != "Edited Local Name" ||
		string(stash.Invite.WebauthnUserHandle) == "" || stash.Invite.Nickname != "key" {
		t.Fatalf("verified invite stash = %+v", stash.Invite)
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
	return newAtomicVRChatEnrollmentServerWithAudit(t, e, account, noopAuditWriter{})
}

func newAtomicVRChatEnrollmentServerWithAudit(t *testing.T, e db.Enrollment, account *db.Account, writer audit.Writer) (*Server, *vrchatEnrollmentQueries) {
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
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		t.Fatal(err)
	}
	// The tx-scoped writer is the caller's writer itself; the FK-aware fake
	// and the plain capture both flow through unchanged.
	capture := writer
	if capture == nil {
		capture = noopAuditWriter{}
	}
	// Password/TOTP stores so the password-totp verify path runs end to end
	// against the same fake; argon2 params stay minimal for test speed.
	cfg.TOTP = configx.TOTPConfig{DefaultPeriod: 30, DefaultDigits: 6, DefaultAlgorithm: "SHA1", DriftSteps: 1, RecoveryCodeCount: 10}
	cfg.PasswordHashParams = configx.PasswordHashParams{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1}
	auditWriter := audit.NewWriter(q)
	s := &Server{
		config:                     cfg,
		kvStore:                    store,
		sessionStore:               sessstore.NewSessionStore(store, q, cfg.SessionTTL, cfg.SessionTTL),
		clientIP:                   newDirectResolver(),
		Audit:                      writer,
		passwordStore:              password.NewStore(q, cfg.PasswordHashParams, nil, auditWriter),
		totpStore:                  totp.NewStore(q, &totpTestTxRunner{q: q}, map[int][]byte{1: dek}, cfg.TOTP, nil, auditWriter),
		enrollmentQueriesOverride:  q,
		enrollmentTxRunnerOverride: &memoryEnrollmentTxRunner{root: q, audit: capture},
		enrollmentWebAuthnOverride: &recordingEnrollmentWebAuthn{credential: credential},
		federationRegistry:         vrchatEnrollmentRegistry(t),
		federationService:          federation.NewService(vrchatEnrollmentRegistry(t), federation.NewProviderStore(q), nil, nil, federation.ServiceConfig{}),
	}
	return s, q
}

func verifiedInviteEnrollment(token string) db.Enrollment {
	e := pendingEnrollment(token, enrollment.IntentInvite)
	e.ExpectedUpstreamIdpSlug = pgtype.Text{String: "vrchat-main", Valid: true}
	e.RequireLocalCredential = true
	e.FederatedUpstreamIdpID = pgtype.Int8{Int64: 41, Valid: true}
	e.FederatedUpstreamIdpSlug = pgtype.Text{String: "vrchat-main", Valid: true}
	e.FederatedUpstreamIss = pgtype.Text{String: "https://api.vrchat.cloud/api/1", Valid: true}
	e.FederatedUpstreamSub = pgtype.Text{String: "usr_123", Valid: true}
	e.FederatedDisplayName = pgtype.Text{String: "VRChat Suggested", Valid: true}
	e.FederatedUpstreamData = []byte(`{"displayName":"VRChat Suggested","profileUrl":"https://vrchat.com/home/user/usr_123","userId":"usr_123"}`)
	e.FederatedAvatarUrl = pgtype.Text{String: "https://api.vrchat.cloud/api/1/file/avatar", Valid: true}
	return e
}

func beginVerifiedInvite(t *testing.T, s *Server, e db.Enrollment, username, displayName string) {
	t.Helper()
	body := `{"username":"` + username + `","displayName":"` + displayName + `"}`
	if displayName == "" {
		body = `{"username":"` + username + `"}`
	}
	w := httptest.NewRecorder()
	s.handleEnrollmentBeginHTTP(w, beginEnrollmentRequest(e.Token, body))
	if w.Code != http.StatusOK {
		t.Fatalf("begin status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestVerifiedInviteCompletionCommitsAccountCredentialIdentityAndSession(t *testing.T) {
	e := verifiedInviteEnrollment("complete-registration")
	audits := &enrollmentAuditCapture{}
	s, q := newAtomicVRChatEnrollmentServerWithAudit(t, e, nil, audits)
	beginVerifiedInvite(t, s, e, "local-choice", "Edited Local Display")

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
	// The identity link audit row must exist on the tx-scoped writer — the
	// shared writer would drop it against the uncommitted account row.
	var linkRow *audit.Record
	for i := range audits.records {
		if audits.records[i].Event == audit.EventLink && audits.records[i].Factor == audit.FactorEnrollment {
			linkRow = &audits.records[i]
		}
	}
	if linkRow == nil {
		t.Fatalf("audit records = %+v, want an enrollment/link row", audits.records)
	}
	if linkRow.AccountID == nil || *linkRow.AccountID != account.ID ||
		linkRow.Detail["idp_slug"] != "vrchat-main" || linkRow.Detail["upstream_sub"] != e.FederatedUpstreamSub.String {
		t.Fatalf("identity link audit row = %+v", linkRow)
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

// The identity link audit row must go through the transaction's writer: the
// shared writer cannot see the uncommitted account and the row would fail the
// credential_event.account_id foreign key and be dropped, as the real database
// showed in the smoke review.
func TestVerifiedInviteCompletionWritesLinkAuditInsideTransaction(t *testing.T) {
	e := verifiedInviteEnrollment("complete-registration-fk")
	audits := &foreignKeyAuditCapture{}
	s, q := newAtomicVRChatEnrollmentServerWithAudit(t, e, nil, audits)
	beginVerifiedInvite(t, s, e, "local-choice", "Edited Local Display")

	w := httptest.NewRecorder()
	s.handleEnrollmentCompleteHTTP(w, completeEnrollmentRequest(e.Token))
	if w.Code != http.StatusOK {
		t.Fatalf("complete status = %d body=%s", w.Code, w.Body.String())
	}

	var linkRow *audit.Record
	for i := range audits.records {
		if audits.records[i].Event == audit.EventLink && audits.records[i].Factor == audit.FactorEnrollment {
			linkRow = &audits.records[i]
		}
	}
	if linkRow == nil {
		t.Fatal("identity link audit row missing: the writer that cannot see the account dropped it")
	}
	var account db.Account
	q.mu.Lock()
	for _, account = range q.accounts {
	}
	q.mu.Unlock()
	if linkRow.AccountID == nil || *linkRow.AccountID != account.ID {
		t.Fatalf("identity link audit row = %+v, want account %d", linkRow, account.ID)
	}
}

// Enrollment has no "keep me signed in" choice, so the session it issues
// lives only as long as the browser does.
func TestEnrollmentCompletionIssuesBrowserSessionCookie(t *testing.T) {
	e := verifiedInviteEnrollment("complete-session-cookie")
	s, _ := newAtomicVRChatEnrollmentServer(t, e, nil)
	beginVerifiedInvite(t, s, e, "local-cookie", "Local Cookie")

	w := httptest.NewRecorder()
	s.handleEnrollmentCompleteHTTP(w, completeEnrollmentRequest(e.Token))
	if w.Code != http.StatusOK {
		t.Fatalf("complete status = %d body=%s", w.Code, w.Body.String())
	}
	var session *http.Cookie
	for _, c := range w.Result().Cookies() {
		if strings.HasSuffix(c.Name, "prohibitorum_session") {
			session = c
		}
	}
	if session == nil {
		t.Fatalf("no session cookie in %q", w.Header().Values("Set-Cookie"))
	}
	if session.MaxAge != 0 || strings.Contains(session.Raw, "Max-Age") {
		t.Fatalf("enrollment session cookie = %q, want no Max-Age", session.Raw)
	}
}

func TestVerifiedInviteCompletionRollsBackConflictsProviderChangesAndBadStash(t *testing.T) {
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
				provider.Mode = federation.ModeLinkOnly
				q.providers[41] = provider
				q.mu.Unlock()
			},
			wantCode: "provider_not_ready",
		},
		"provider slug renamed after begin": {
			mutate: func(_ *testing.T, _ *Server, q *vrchatEnrollmentQueries, _ db.Enrollment) {
				q.mu.Lock()
				provider := q.providers[41]
				provider.Slug = "renamed"
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
			e := verifiedInviteEnrollment("rollback-" + strings.ReplaceAll(name, " ", "-"))
			s, q := newAtomicVRChatEnrollmentServer(t, e, nil)
			username := "available-user"
			if name == "username race" {
				username = "racing-user"
			}
			beginVerifiedInvite(t, s, e, username, "Local Display")
			test.mutate(t, s, q, e)

			w := httptest.NewRecorder()
			s.handleEnrollmentCompleteHTTP(w, completeEnrollmentRequest(e.Token))
			var public struct {
				Code    string         `json:"code"`
				Details map[string]any `json:"details"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &public); err != nil {
				t.Fatalf("decode response status=%d body=%s: %v", w.Code, w.Body.String(), err)
			}
			if public.Code != test.wantCode {
				t.Fatalf("code = %q status=%d body=%s, want %q", public.Code, w.Code, w.Body.String(), test.wantCode)
			}
			// The loaded provider names an identity conflict; the collapsed
			// provider_not_ready says nothing about which provider changed.
			wantDetails := 0
			if test.wantCode == "federation_identity_conflict" {
				wantDetails = 1
				if public.Details["federationName"] != "VRChat" {
					t.Fatalf("details = %#v, want the provider name", public.Details)
				}
			}
			if len(public.Details) != wantDetails {
				t.Fatalf("details = %#v", public.Details)
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

func TestAdminResetCompletionReplacesCredentialsAndRevokesSessions(t *testing.T) {
	account := db.Account{ID: 73, Username: "private-user", DisplayName: "Private Name", WebauthnUserHandle: []byte("opaque-handle")}
	e := pendingEnrollment("admin-reset-complete", enrollment.IntentReset)
	e.TargetAccountID = pgtype.Int4{Int32: account.ID, Valid: true}
	s, q := newAtomicVRChatEnrollmentServer(t, e, &account)
	q.credentials[account.ID] = []db.WebauthnCredential{{ID: 8, AccountID: account.ID, CredentialID: []byte("old-credential")}}
	if _, _, err := s.sessionStore.Issue(context.Background(), account.ID, "127.0.0.1", "old-one", []string{"hwk"}, nil, false); err != nil {
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
}

func TestAdminResetCompletionRejectsDisabledTargetBeforeDeletion(t *testing.T) {
	account := db.Account{ID: 73, Username: "private-user", DisplayName: "Private Name", WebauthnUserHandle: []byte("opaque-handle")}
	e := pendingEnrollment("admin-reset-disabled-target", enrollment.IntentReset)
	e.TargetAccountID = pgtype.Int4{Int32: account.ID, Valid: true}
	s, q := newAtomicVRChatEnrollmentServer(t, e, &account)
	q.credentials[account.ID] = []db.WebauthnCredential{{ID: 8, AccountID: account.ID, CredentialID: []byte("old-credential")}}
	w := httptest.NewRecorder()
	s.handleEnrollmentBeginHTTP(w, beginEnrollmentRequest(e.Token, `{}`))
	if w.Code != http.StatusOK {
		t.Fatalf("begin status=%d body=%s", w.Code, w.Body.String())
	}
	q.mu.Lock()
	disabled := q.accounts[account.ID]
	disabled.Disabled = true
	q.accounts[account.ID] = disabled
	q.mu.Unlock()

	w = httptest.NewRecorder()
	s.handleEnrollmentCompleteHTTP(w, completeEnrollmentRequest(e.Token))
	var public struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &public); err != nil {
		t.Fatal(err)
	}
	if public.Code != "enrollment_consumed" {
		t.Fatalf("code=%q status=%d, want enrollment_consumed", public.Code, w.Code)
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.enrollments[e.Token].ConsumedAt.Valid {
		t.Fatal("failed reset consumed enrollment")
	}
	credentials := q.credentials[account.ID]
	if len(credentials) != 1 || !bytes.Equal(credentials[0].CredentialID, []byte("old-credential")) {
		t.Fatalf("failed reset changed credentials: %+v", credentials)
	}
}

func TestVerifiedInviteConcurrentCompletionHasOneWinner(t *testing.T) {
	e := verifiedInviteEnrollment("concurrent-registration")
	s, q := newAtomicVRChatEnrollmentServer(t, e, nil)
	beginVerifiedInvite(t, s, e, "concurrent-user", "Concurrent User")

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

func TestVerifiedInviteRefreshesAvatarAfterCommit(t *testing.T) {
	e := verifiedInviteEnrollment("avatar-refresh-registration")
	s, q := newAtomicVRChatEnrollmentServer(t, e, nil)
	s.vrchatUserAgent = "Prohibitorum/test https://login.example.com"
	calls := 0
	s.enrollmentAvatarRefresh = func(ctx context.Context, accountID int32, provider federation.Provider, delivery federation.AvatarDelivery) {
		calls++
		if ctx == nil {
			t.Fatal("avatar refresh got no request context")
		}
		if accountID == 0 || provider.ID != e.FederatedUpstreamIdpID.Int64 || delivery.URL != e.FederatedAvatarUrl.String || delivery.UserAgent != s.vrchatUserAgent {
			t.Fatalf("avatar inheritance args = account=%d provider=%+v delivery=%+v", accountID, provider, delivery)
		}
		q.mu.Lock()
		committed := q.enrollments[e.Token].ConsumedAt.Valid && len(q.accounts) == 1
		q.mu.Unlock()
		if !committed {
			t.Fatal("avatar inheritance ran before account transaction commit")
		}
	}
	beginVerifiedInvite(t, s, e, "avatar-user", "Avatar User")

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
		t.Fatalf("avatar refresh changed committed result: accounts=%d consumed=%v", len(q.accounts), q.enrollments[e.Token].ConsumedAt.Valid)
	}
}

func TestFederatedAvatarDeliveryIdentifiesOnlyVRChat(t *testing.T) {
	s := &Server{vrchatUserAgent: "Prohibitorum/test https://login.example.com"}
	vrchat := s.federatedAvatarDelivery(federation.Provider{Protocol: "vrchat"}, "https://api.vrchat.cloud/api/1/file/icon/1/file")
	if vrchat.URL != "https://api.vrchat.cloud/api/1/file/icon/1/file" || vrchat.UserAgent != s.vrchatUserAgent {
		t.Fatalf("vrchat delivery = %+v", vrchat)
	}
	if oidc := s.federatedAvatarDelivery(federation.Provider{Protocol: "oidc"}, "https://idp.example.com/a.png"); oidc.UserAgent != "" {
		t.Fatalf("oidc delivery carries a User-Agent: %+v", oidc)
	}
}

func TestEnrollmentBeginFederationBoundInviteNamesEnabledProvider(t *testing.T) {
	enabled := db.UpstreamIdp{ID: 51, Slug: "corp", DisplayName: "Corporate SSO", Protocol: "oidc", Mode: federation.ModeInviteOnly}
	disabled := db.UpstreamIdp{ID: 52, Slug: "retired", DisplayName: "Retired SSO", Protocol: "oidc", Mode: federation.ModeInviteOnly, Disabled: true}
	tests := []struct {
		slug     string
		wantName string
	}{
		{enabled.Slug, enabled.DisplayName},
		{disabled.Slug, ""},
		{"no-such-idp", ""},
	}
	for _, test := range tests {
		t.Run(test.slug, func(t *testing.T) {
			e := pendingEnrollment("bound-invite", enrollment.IntentInvite)
			e.ExpectedUpstreamIdpSlug = pgtype.Text{String: test.slug, Valid: true}
			q := &vrchatEnrollmentQueries{
				enrollments: map[string]db.Enrollment{e.Token: e},
				providers:   map[int64]db.UpstreamIdp{enabled.ID: enabled, disabled.ID: disabled},
			}
			s := &Server{
				enrollmentQueriesOverride: q,
				Audit:                     noopAuditWriter{},
				federationService:         federation.NewService(federation.NewRegistry(), federation.NewProviderStore(q), nil, nil, federation.ServiceConfig{}),
			}

			w := httptest.NewRecorder()
			s.handleEnrollmentBeginHTTP(w, beginEnrollmentRequest(e.Token, `{"username":"invitee","displayName":"Invitee"}`))
			var public struct {
				Code    string         `json:"code"`
				Details map[string]any `json:"details"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &public); err != nil {
				t.Fatalf("decode response status=%d body=%s: %v", w.Code, w.Body.String(), err)
			}
			if w.Code != http.StatusBadRequest || public.Code != "enrollment_federation_required" {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if test.wantName == "" {
				if len(public.Details) != 0 {
					t.Fatalf("details = %#v, want none", public.Details)
				}
				return
			}
			if len(public.Details) != 1 || public.Details["federationName"] != test.wantName {
				t.Fatalf("details = %#v, want federationName %q", public.Details, test.wantName)
			}
		})
	}
}
