package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/account"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	webauthnauth "prohibitorum/pkg/credential/webauthn"
	"prohibitorum/pkg/logx"
	sessstore "prohibitorum/pkg/session"
)

// ----- GET /me ------------------------------------------------------------

type meOut struct {
	Body contract.SessionView
}

func (s *Server) handleGetMe(ctx context.Context, _ *struct{}) (*meOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		// LoadSession middleware should have attached one; if not, registerOp's
		// AuthSession requirement would have already rejected. Defensive.
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	return &meOut{Body: sessionView(sess.Account)}, nil
}

// ----- PUT /me ------------------------------------------------------------

// updateMeQueries is the narrow DB surface handleUpdateMe requires.
// Declared here so tests can stub it without constructing *db.Queries.
// Production wiring (NewServer) leaves updateMeOverride nil and the handler
// falls back to s.queries.
type updateMeQueries interface {
	UpdateAccountDisplayName(ctx context.Context, arg db.UpdateAccountDisplayNameParams) error
}

type updateMeIn struct {
	Body struct {
		DisplayName string `json:"displayName"`
	}
}

func (s *Server) handleUpdateMe(ctx context.Context, in *updateMeIn) (*meOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	if err := account.ValidateDisplayName(in.Body.DisplayName); err != nil {
		return nil, authErrToHuma(err)
	}
	q := s.updateMeQueries()
	if err := q.UpdateAccountDisplayName(ctx, db.UpdateAccountDisplayNameParams{
		ID:          sess.Account.ID,
		DisplayName: in.Body.DisplayName,
	}); err != nil {
		return nil, fmt.Errorf("handleUpdateMe: %w", err)
	}
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event":        "auth.profile_updated_self",
		"account_id":   sess.Account.ID,
		"display_name": in.Body.DisplayName,
	}).Info("auth")
	sess.Account.DisplayName = in.Body.DisplayName
	return &meOut{Body: sessionView(sess.Account)}, nil
}

func (s *Server) updateMeQueries() updateMeQueries {
	if s.updateMeOverride != nil {
		return s.updateMeOverride
	}
	return s.queries
}

// ----- GET /me/factors ----------------------------------------------------

// getMyFactorsQueries is the narrow DB surface handleGetMyFactors requires.
// Declared here so tests can stub it without constructing *db.Queries.
// Production wiring (NewServer) leaves getMyFactorsOverride nil and the handler
// falls back to s.queries.
type getMyFactorsQueries interface {
	GetPasswordCredential(ctx context.Context, accountID int32) (db.PasswordCredential, error)
	GetTOTPCredential(ctx context.Context, accountID int32) (db.TotpCredential, error)
	ListRecoveryCodesByAccount(ctx context.Context, accountID int32) ([]db.RecoveryCode, error)
	CountCredentialsByAccount(ctx context.Context, accountID int32) (int64, error)
}

type meFactorsOut struct {
	Body contract.MeFactorsView
}

func (s *Server) handleGetMyFactors(ctx context.Context, _ *struct{}) (*meFactorsOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	id := sess.Account.ID
	q := s.getMyFactorsQueries()
	v := contract.MeFactorsView{}

	if _, err := q.GetPasswordCredential(ctx, id); err == nil {
		v.PasswordSet = true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("handleGetMyFactors: password: %w", err)
	}

	if totp, err := q.GetTOTPCredential(ctx, id); err == nil {
		v.TOTPEnrolled = totp.ConfirmedAt.Valid
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("handleGetMyFactors: totp: %w", err)
	}

	codes, err := q.ListRecoveryCodesByAccount(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("handleGetMyFactors: recovery: %w", err)
	}
	v.RecoveryCodesRemaining = len(codes)

	n, err := q.CountCredentialsByAccount(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("handleGetMyFactors: passkeys: %w", err)
	}
	v.PasskeyCount = int(n)

	return &meFactorsOut{Body: v}, nil
}

func (s *Server) getMyFactorsQueries() getMyFactorsQueries {
	if s.getMyFactorsOverride != nil {
		return s.getMyFactorsOverride
	}
	return s.queries
}

// ----- GET /me/credentials -----------------------------------------------

type credentialsOut struct {
	Body []contract.CredentialView
}

func (s *Server) handleListMyCredentials(ctx context.Context, _ *struct{}) (*credentialsOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	rows, err := s.queries.ListCredentialsByAccount(ctx, sess.Account.ID)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	out := make([]contract.CredentialView, 0, len(rows))
	for _, c := range rows {
		out = append(out, credentialView(&c))
	}
	return &credentialsOut{Body: out}, nil
}

// credentialView projects a db.WebauthnCredential into the public-safe shape.
// Full CredentialID is never returned — only the last 4 chars of base64url for
// forensic display.
func credentialView(c *db.WebauthnCredential) contract.CredentialView {
	suffix := credentialIDSuffix(c.CredentialID)
	// backup_state and attestation_type are nullable in the new schema.
	backupState := c.BackupState.Valid && c.BackupState.Bool
	attType := ""
	if c.AttestationType.Valid {
		attType = c.AttestationType.String
	}
	out := contract.CredentialView{
		ID:                 c.ID,
		CredentialIDSuffix: suffix,
		Transports:         append([]string(nil), c.Transports...),
		BackupState:        backupState,
		AttestationType:    attType,
		CreatedAt:          c.CreatedAt.Time,
	}
	if c.Nickname.Valid {
		v := c.Nickname.String
		out.Nickname = &v
	}
	if c.LastUsedAt.Valid {
		t := c.LastUsedAt.Time
		out.LastUsedAt = &t
	}
	return out
}

func credentialIDSuffix(credID []byte) string {
	enc := base64.RawURLEncoding.EncodeToString(credID)
	if len(enc) <= 4 {
		return enc
	}
	return enc[len(enc)-4:]
}

// ----- POST /me/credentials/register/begin (raw chi) ---------------------

// addPasskeyCeremonyKey builds the KV key for the add-passkey WebAuthn ceremony
// stash. It keys on the opaque SessionData.SessionID (NOT the raw session
// token) so the secret cookie half never appears in the KV keyspace — matching
// the sudo ceremony and closing audit follow-up N6 (the second leak surface of
// N1). Callers MUST have verified sess.Data != nil.
func addPasskeyCeremonyKey(sess *authn.Session) string {
	return "webauthn_ceremony:add:" + sess.Data.SessionID
}

func (s *Server) handleAddCredentialBeginHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if sess == nil || sess.Data == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	// Registering a new authenticator is a credential-adding action — gate it
	// behind fresh sudo, matching device-pairing approval and the sudo threat
	// model (a stolen session must not silently plant a backdoor passkey). T1.3.
	if s.requireFreshSudo(r.Context(), w, sess) {
		return
	}

	creds, err := s.queries.ListCredentialsByAccount(r.Context(), sess.Account.ID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("add credential/begin: list: %w", err))
		return
	}
	exclude := make([]protocol.CredentialDescriptor, 0, len(creds))
	for _, c := range creds {
		exclude = append(exclude, protocol.CredentialDescriptor{
			Type:         protocol.PublicKeyCredentialType,
			CredentialID: c.CredentialID,
		})
	}
	wu := &webauthnauth.WebAuthnAccount{Account: sess.Account, Credentials: creds}

	creation, sessionData, err := s.webauthn.BeginRegistration(wu, webauthnauth.RegistrationOptions(exclude)...)
	if err != nil {
		writeAuthErr(w, webauthnauth.MapRegisterCeremonyError(r.Context(), err))
		return
	}
	payload, err := json.Marshal(sessionData)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("add credential/begin: marshal: %w", err))
		return
	}
	if err := s.kvStore.SetEx(r.Context(), addPasskeyCeremonyKey(sess), string(payload), 5*time.Minute); err != nil {
		writeAuthErr(w, fmt.Errorf("add credential/begin: setex: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(creation.Response)
}

// ----- POST /me/credentials/register/complete (raw chi) ------------------

func (s *Server) handleAddCredentialCompleteHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if sess == nil || sess.Data == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}

	rawStash, err := s.kvStore.Get(r.Context(), addPasskeyCeremonyKey(sess))
	if err != nil {
		writeAuthErr(w, authn.ErrCeremonyExpired())
		return
	}
	var sessionData webauthn.SessionData
	if err := json.Unmarshal([]byte(rawStash), &sessionData); err != nil {
		writeAuthErr(w, authn.ErrCeremonyState())
		return
	}

	nicknameRaw := r.URL.Query().Get("nickname")
	var validatedNickname *string
	if nicknameRaw != "" {
		if err := account.ValidateNickname(&nicknameRaw); err != nil {
			writeAuthErr(w, err)
			return
		}
		validatedNickname = account.NormalizeNickname(&nicknameRaw)
	}

	parsed, err := protocol.ParseCredentialCreationResponseBody(r.Body)
	if err != nil {
		writeAuthErr(w, webauthnauth.MapRegisterCeremonyError(r.Context(), err))
		return
	}

	// Refresh credentials list for the user adapter — between /begin and
	// /complete, no new credentials should have appeared, but stay correct.
	existing, _ := s.queries.ListCredentialsByAccount(r.Context(), sess.Account.ID)
	wu := &webauthnauth.WebAuthnAccount{Account: sess.Account, Credentials: existing}
	cred, err := s.webauthn.CreateCredential(wu, sessionData, parsed)
	if err != nil {
		writeAuthErr(w, webauthnauth.MapRegisterCeremonyError(r.Context(), err))
		return
	}

	transports := make([]string, 0, len(cred.Transport))
	for _, t := range cred.Transport {
		transports = append(transports, string(t))
	}
	var attType pgtype.Text
	if cred.AttestationType != "" {
		attType = pgtype.Text{String: cred.AttestationType, Valid: true}
	}
	row, err := s.queries.InsertCredential(r.Context(), db.InsertCredentialParams{
		AccountID:       sess.Account.ID,
		CredentialID:    cred.ID,
		PublicKey:       cred.PublicKey,
		CoseAlg:         webauthnauth.COSEAlg(cred.PublicKey),
		UserHandle:      sess.Account.WebauthnUserHandle,
		SignCount:       int64(cred.Authenticator.SignCount),
		Transports:      transports,
		Aaguid:          cred.Authenticator.AAGUID,
		AttestationType: attType,
		BackupEligible:  pgtype.Bool{Bool: cred.Flags.BackupEligible, Valid: true},
		BackupState:     pgtype.Bool{Bool: cred.Flags.BackupState, Valid: true},
		UvInitialized:   cred.Flags.UserVerified,
		Nickname:        nicknameParamPtr(validatedNickname),
	})
	if err != nil {
		writeAuthErr(w, fmt.Errorf("add credential/complete: insert: %w", err))
		return
	}

	// Best-effort cleanup of the ceremony stash.
	_ = s.kvStore.Del(r.Context(), addPasskeyCeremonyKey(sess))

	logx.WithContext(r.Context()).WithFields(logrus.Fields{
		"event":         "auth.credential_added",
		"account_id":    sess.Account.ID,
		"credential_id": row.ID,
		"client_ip":     sessstore.ClientIP(r, s.config.TrustProxy),
	}).Info("auth")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(credentialView(&row))
}

// ----- GET /me/sessions --------------------------------------------------

type listMySessionsOut struct {
	Body []contract.SessionListItem
}

func (s *Server) handleListMySessions(ctx context.Context, _ *struct{}) (*listMySessionsOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	records, err := s.sessionStore.ListByAccount(ctx, sess.Account.ID)
	if err != nil {
		return nil, fmt.Errorf("handleListMySessions: %w", err)
	}
	items := make([]contract.SessionListItem, 0, len(records))
	for _, r := range records {
		items = append(items, contract.SessionListItem{
			ID: r.Data.SessionID,
			// Compare opaque SessionIDs, not tokens: post-N1 the KV key (and
			// thus SessionRecord.Token) holds the hashed token, not the raw
			// cookie secret, so a raw-token comparison would never match.
			IsCurrent:  sess.Data != nil && r.Data.SessionID == sess.Data.SessionID,
			IssuedAt:   r.Data.IssuedAt,
			ExpiresAt:  r.Data.ExpiresAt,
			LastSeenIP: r.Data.LastSeenIP,
			UserAgent:  r.Data.UserAgent,
		})
	}
	return &listMySessionsOut{Body: items}, nil
}

// ----- POST /me/sessions/revoke -----------------------------------------

type revokeMySessionIn struct {
	Body struct {
		ID string `json:"id"`
	}
}

func (s *Server) handleRevokeMySession(ctx context.Context, in *revokeMySessionIn) (*struct{}, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	if in.Body.ID == "" {
		return nil, authErrToHuma(authn.ErrSessionNotFound())
	}
	// Refuse to revoke the current session via this endpoint — the standard
	// /auth/logout path handles that cleanly (clears the cookie too). This
	// also prevents an accidental self-lock.
	if sess.Data != nil && sess.Data.SessionID == in.Body.ID {
		return nil, authErrToHuma(authn.ErrCannotRevokeCurrentSession())
	}
	ok, err := s.sessionStore.RevokeBySessionID(ctx, sess.Account.ID, in.Body.ID)
	if err != nil {
		return nil, fmt.Errorf("handleRevokeMySession: %w", err)
	}
	if !ok {
		return nil, authErrToHuma(authn.ErrSessionNotFound())
	}
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event":         "auth.session_revoked_self",
		"account_id":    sess.Account.ID,
		"target_session": in.Body.ID,
	}).Info("auth")
	return &struct{}{}, nil
}

// nicknameParamPtr converts a *string (nil or already-normalized) to a
// pgtype.Text for InsertCredentialParams (NULL when nil).
func nicknameParamPtr(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

// ----- POST /me/credentials/rename ---------------------------------------

type renameMyCredentialIn struct {
	Body struct {
		ID       int32   `json:"id"`
		Nickname *string `json:"nickname,omitempty"`
	}
}

func (s *Server) handleRenameMyCredential(ctx context.Context, in *renameMyCredentialIn) (*struct{}, error) {
	if err := account.ValidateNickname(in.Body.Nickname); err != nil {
		return nil, authErrToHuma(err)
	}
	sess := authn.SessionFromContext(ctx)
	normalized := account.NormalizeNickname(in.Body.Nickname)
	var nickname pgtype.Text
	if normalized != nil {
		nickname = pgtype.Text{String: *normalized, Valid: true}
	}
	n, err := s.queries.UpdateMyCredentialNickname(ctx, db.UpdateMyCredentialNicknameParams{
		ID:        in.Body.ID,
		AccountID: sess.Account.ID,
		Nickname:  nickname,
	})
	if err != nil {
		return nil, fmt.Errorf("handleRenameMyCredential: %w", err)
	}
	if n == 0 {
		return nil, authErrToHuma(authn.ErrCredentialNotFound())
	}
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event":         "auth.credential_renamed_self",
		"account_id":    sess.Account.ID,
		"credential_id": in.Body.ID,
	}).Info("auth")
	return &struct{}{}, nil
}

// ----- POST /me/credentials/delete ---------------------------------------

type deleteMyCredentialIn struct {
	Body struct {
		ID int32 `json:"id"`
	}
}

type emptyOut struct{}

func (s *Server) handleDeleteMyCredential(ctx context.Context, in *deleteMyCredentialIn) (*emptyOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	// dbPool is always set in production (NewServer); this handler has no
	// fake-injection seam and is smoke-tested, so there is no nil-pool path
	// like handleMeRevokePwdTOTPHTTP's.
	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("delete credential: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := s.queries.WithTx(tx)

	// Serialise factor mutations for this account (vs revoke-password-totp) by
	// locking the account row before the count-then-delete, so a concurrent
	// factor removal cannot race this last-passkey guard.
	if _, err := q.GetAccountByIDForUpdate(ctx, sess.Account.ID); err != nil {
		return nil, fmt.Errorf("delete credential: lock account: %w", err)
	}
	count, err := q.CountCredentialsByAccount(ctx, sess.Account.ID)
	if err != nil {
		return nil, fmt.Errorf("count credentials: %w", err)
	}
	if count <= 1 {
		return nil, authErrToHuma(authn.ErrLastPasskey())
	}
	n, err := q.DeleteCredentialByID(ctx, db.DeleteCredentialByIDParams{
		ID:        in.Body.ID,
		AccountID: sess.Account.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("delete credential: %w", err)
	}
	if n == 0 {
		return nil, authErrToHuma(authn.ErrCredentialNotFound())
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("delete credential: commit: %w", err)
	}

	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event":         "auth.credential_revoked_self",
		"account_id":    sess.Account.ID,
		"credential_id": in.Body.ID,
	}).Info("auth")
	return &emptyOut{}, nil
}
