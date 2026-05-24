package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/auth"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/errorx"
	"prohibitorum/pkg/logx"
)

// authErrToHuma converts an *auth.AuthError into a huma.StatusError so typed
// Huma handlers return the correct HTTP status code AND the project's
// machine-readable code in the response envelope. Without errorx.ErrorCode,
// huma.NewError defaults the code to "UNKNOWN".
func authErrToHuma(err error) error {
	ae := auth.AsAuthError(err)
	if ae == nil {
		return err
	}
	return huma.NewError(ae.Status, ae.Message, errorx.ErrorCode(ae.Code))
}

// ----- preview (typed) -----------------------------------------------------

type previewIn struct {
	Token string `path:"token"`
}

type previewOut struct {
	Body contract.EnrollmentPreview
}

func (s *Server) handlePreviewEnrollment(ctx context.Context, in *previewIn) (*previewOut, error) {
	e, err := auth.LoadEnrollment(ctx, s.queries, in.Token)
	if err != nil {
		return nil, authErrToHuma(err)
	}
	out := contract.EnrollmentPreview{
		Intent:    e.Intent,
		ExpiresAt: e.ExpiresAt.Time,
	}
	switch e.Intent {
	case auth.IntentBootstrap:
		// no target — bootstrap creates a brand-new admin
	case auth.IntentInvite:
		// No target hint — invitee picks their own username/displayName from
		// scratch. The template only carries role + permissions.
	case auth.IntentReset:
		if e.TargetAccountID.Valid {
			if a, gerr := s.queries.GetAccountByID(ctx, e.TargetAccountID.Int32); gerr == nil {
				out.Target = &contract.EnrollmentTarget{
					Username:    a.Username,
					DisplayName: a.DisplayName,
				}
			}
		}
	}
	return &previewOut{Body: out}, nil
}

// ----- begin (raw chi — sets KV ceremony stash) ----------------------------

// enrollBeginBody carries the username + display_name + optional nickname.
// Used for both bootstrap and invite intents (the invitee chooses; the
// template's suggestion is a preview hint only). Empty body for reset.
type enrollBeginBody struct {
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Nickname    string `json:"nickname,omitempty"` // for the first passkey of this account
}

// enrollCeremonyStash combines the WebAuthn session data with the pending
// account info. Bootstrap and invite both create new accounts at consume time
// (so we must remember the proposed username/displayName + generated user
// handle until then). Reset stashes the optional nickname only.
type enrollCeremonyStash struct {
	Data      webauthn.SessionData `json:"data"`
	Bootstrap *bootstrapCeremony   `json:"bootstrap,omitempty"`
	Invite    *inviteCeremony      `json:"invite,omitempty"`
	Reset     *resetCeremony       `json:"reset,omitempty"`
}

type bootstrapCeremony struct {
	Username           string `json:"username"`
	DisplayName        string `json:"displayName"`
	WebauthnUserHandle []byte `json:"webauthn_user_handle"`
	Nickname           string `json:"nickname,omitempty"`
}

type inviteCeremony struct {
	Username           string `json:"username"`
	DisplayName        string `json:"displayName"`
	WebauthnUserHandle []byte `json:"webauthn_user_handle"`
	Nickname           string `json:"nickname,omitempty"`
}

type resetCeremony struct {
	Nickname string `json:"nickname,omitempty"`
}

func (s *Server) handleEnrollmentBeginHTTP(w http.ResponseWriter, r *http.Request) {
	// Bound per-IP — caps brute-force on token guesses even though tokens
	// are 256-bit random. Generous for legit consumes (one user, one flow).
	if s.rateLimit(w, r, "enroll:ip:"+auth.ClientIP(r, s.config.TrustProxy), 20, time.Minute) {
		return
	}
	token := chi.URLParam(r, "token")
	e, err := auth.LoadEnrollment(r.Context(), s.queries, token)
	if err != nil {
		writeAuthErr(w, err)
		return
	}

	var body enrollBeginBody
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}

	var (
		wu    *auth.WebAuthnAccount
		stash enrollCeremonyStash
	)
	switch e.Intent {
	case auth.IntentBootstrap:
		if err := auth.ValidateUsername(body.Username); err != nil {
			writeAuthErr(w, err)
			return
		}
		if err := auth.ValidateDisplayName(body.DisplayName); err != nil {
			writeAuthErr(w, err)
			return
		}
		if err := auth.ValidateNickname(&body.Nickname); err != nil {
			writeAuthErr(w, err)
			return
		}
		// Username must be unique at /begin time.
		_, gerr := s.queries.GetAccountByUsername(r.Context(), body.Username)
		if gerr == nil {
			writeAuthErr(w, auth.ErrUsernameTaken())
			return
		} else if !errors.Is(gerr, pgx.ErrNoRows) {
			writeAuthErr(w, fmt.Errorf("enrollment/begin: check username: %w", gerr))
			return
		}
		handle, err := auth.GenerateUserHandle()
		if err != nil {
			writeAuthErr(w, err)
			return
		}
		wu = &auth.WebAuthnAccount{
			Account: &db.Account{
				ID:                 0,
				Username:           body.Username,
				DisplayName:        body.DisplayName,
				WebauthnUserHandle: handle,
				Role:               "admin",
			},
		}
		stash.Bootstrap = &bootstrapCeremony{
			Username:           body.Username,
			DisplayName:        body.DisplayName,
			WebauthnUserHandle: handle,
			Nickname:           body.Nickname,
		}

	case auth.IntentInvite:
		if err := auth.ValidateUsername(body.Username); err != nil {
			writeAuthErr(w, err)
			return
		}
		if err := auth.ValidateDisplayName(body.DisplayName); err != nil {
			writeAuthErr(w, err)
			return
		}
		if err := auth.ValidateNickname(&body.Nickname); err != nil {
			writeAuthErr(w, err)
			return
		}
		// Soft pre-check for uniqueness. The hard check at consume time inside
		// the TX serves as the source of truth — two invitees racing on the same
		// chosen username yield a clean 409 on the loser.
		if _, gerr := s.queries.GetAccountByUsername(r.Context(), body.Username); gerr == nil {
			writeAuthErr(w, auth.ErrUsernameTaken())
			return
		} else if !errors.Is(gerr, pgx.ErrNoRows) {
			writeAuthErr(w, fmt.Errorf("enrollment/begin invite: check username: %w", gerr))
			return
		}
		handle, err := auth.GenerateUserHandle()
		if err != nil {
			writeAuthErr(w, err)
			return
		}
		// Build the user adapter as if the account exists, so the WebAuthn library
		// can produce a valid CreationOptions. Role from template (with a safe
		// default to "user" if template_role is somehow NULL).
		role := "user"
		if e.TemplateRole.Valid {
			role = e.TemplateRole.String
		}
		wu = &auth.WebAuthnAccount{
			Account: &db.Account{
				ID:                 0,
				Username:           body.Username,
				DisplayName:        body.DisplayName,
				WebauthnUserHandle: handle,
				Role:               role,
			},
		}
		stash.Invite = &inviteCeremony{
			Username:           body.Username,
			DisplayName:        body.DisplayName,
			WebauthnUserHandle: handle,
			Nickname:           body.Nickname,
		}

	case auth.IntentReset:
		if err := auth.ValidateNickname(&body.Nickname); err != nil {
			writeAuthErr(w, err)
			return
		}
		if !e.TargetAccountID.Valid {
			writeAuthErr(w, auth.ErrEnrollmentConsumed())
			return
		}
		a, err := s.queries.GetAccountByID(r.Context(), e.TargetAccountID.Int32)
		if err != nil {
			writeAuthErr(w, auth.ErrEnrollmentConsumed())
			return
		}
		creds, _ := s.queries.ListCredentialsByAccount(r.Context(), a.ID)
		// On reset, the existing credentials are exclusions for the ceremony (you
		// can't re-register the same authenticator); they're DELETED at /complete
		// commit time, not here.
		wu = &auth.WebAuthnAccount{Account: &a, Credentials: creds}
		stash.Reset = &resetCeremony{Nickname: body.Nickname}

	default:
		writeAuthErr(w, auth.ErrEnrollmentConsumed())
		return
	}

	// Build exclusion list for invite/reset; bootstrap has nothing.
	var exclude []protocol.CredentialDescriptor
	for _, c := range wu.Credentials {
		exclude = append(exclude, protocol.CredentialDescriptor{
			Type:         protocol.PublicKeyCredentialType,
			CredentialID: c.CredentialID,
		})
	}

	creation, sessionData, err := s.webauthn.BeginRegistration(wu, auth.RegistrationOptions(exclude)...)
	if err != nil {
		writeAuthErr(w, auth.MapRegisterCeremonyError(r.Context(), err))
		return
	}
	stash.Data = *sessionData
	raw, err := json.Marshal(stash)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/begin: marshal: %w", err))
		return
	}
	if err := s.kvStore.SetEx(r.Context(), "webauthn_ceremony:enroll:"+token, string(raw), 5*time.Minute); err != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/begin: setex: %w", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(creation.Response)
}

// ----- complete (raw chi — runs the TX, issues session) --------------------

func (s *Server) handleEnrollmentCompleteHTTP(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	// Pre-flight: surface 404/expired/consumed before any heavy work.
	if _, err := auth.LoadEnrollment(r.Context(), s.queries, token); err != nil {
		writeAuthErr(w, err)
		return
	}

	raw, err := s.kvStore.Get(r.Context(), "webauthn_ceremony:enroll:"+token)
	if err != nil {
		writeAuthErr(w, auth.ErrCeremonyExpired())
		return
	}
	var stash enrollCeremonyStash
	if err := json.Unmarshal([]byte(raw), &stash); err != nil {
		writeAuthErr(w, auth.ErrCeremonyState())
		return
	}

	parsed, err := protocol.ParseCredentialCreationResponseBody(r.Body)
	if err != nil {
		writeAuthErr(w, auth.MapRegisterCeremonyError(r.Context(), err))
		return
	}

	tx, err := s.dbPool.Begin(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/complete: begin tx: %w", err))
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := s.queries.WithTx(tx)

	// Atomic consume: acquires a row-level lock via the conditional UPDATE,
	// serializing concurrent /complete requests on the same token. One wins
	// (gets the row), all others get pgx.ErrNoRows → enrollment_consumed.
	consumed, err := auth.ConsumeEnrollment(r.Context(), qtx, token)
	if err != nil {
		writeAuthErr(w, err)
		return
	}

	var (
		account db.Account
		credID  int32
	)

	switch consumed.Intent {
	case auth.IntentBootstrap:
		if stash.Bootstrap == nil {
			writeAuthErr(w, auth.ErrCeremonyState())
			return
		}
		wu := &auth.WebAuthnAccount{Account: &db.Account{
			Username:           stash.Bootstrap.Username,
			DisplayName:        stash.Bootstrap.DisplayName,
			WebauthnUserHandle: stash.Bootstrap.WebauthnUserHandle,
		}}
		cred, err := s.webauthn.CreateCredential(wu, stash.Data, parsed)
		if err != nil {
			writeAuthErr(w, auth.MapRegisterCeremonyError(r.Context(), err))
			return
		}
		a, err := qtx.InsertAccount(r.Context(), db.InsertAccountParams{
			Username:             stash.Bootstrap.Username,
			DisplayName:          stash.Bootstrap.DisplayName,
			WebauthnUserHandle:   stash.Bootstrap.WebauthnUserHandle,
			Role:                 "admin",
			CanViewOwnUsage:      true,
			CanManageOwnApiKeys:  true,
			CanViewModels:        true,
			CanViewOwnTraces:     true,
			CanManageOwnProjects: true,
			Disabled:             false,
		})
		if err != nil {
			if isUniqueViolation(err) {
				writeAuthErr(w, auth.ErrUsernameTaken())
				return
			}
			writeAuthErr(w, fmt.Errorf("enrollment/complete bootstrap: insert account: %w", err))
			return
		}
		account = a
		credID, err = insertCredentialForTx(qtx, r.Context(), a.ID, cred, auth.NormalizeNickname(&stash.Bootstrap.Nickname))
		if err != nil {
			writeAuthErr(w, fmt.Errorf("enrollment/complete: insert credential: %w", err))
			return
		}

	case auth.IntentInvite:
		if stash.Invite == nil {
			writeAuthErr(w, auth.ErrCeremonyState())
			return
		}
		// Build the WebAuthn user adapter — same identity the /begin step used,
		// so the assertion verifies against the same rp.id + user.id.
		wu := &auth.WebAuthnAccount{Account: &db.Account{
			Username:           stash.Invite.Username,
			DisplayName:        stash.Invite.DisplayName,
			WebauthnUserHandle: stash.Invite.WebauthnUserHandle,
		}}
		cred, err := s.webauthn.CreateCredential(wu, stash.Data, parsed)
		if err != nil {
			writeAuthErr(w, auth.MapRegisterCeremonyError(r.Context(), err))
			return
		}
		// Role from template; fall back to "user" if somehow not set (defensive).
		role := "user"
		if consumed.TemplateRole.Valid {
			role = consumed.TemplateRole.String
		}
		a, err := qtx.InsertAccount(r.Context(), db.InsertAccountParams{
			Username:             stash.Invite.Username,
			DisplayName:          stash.Invite.DisplayName,
			WebauthnUserHandle:   stash.Invite.WebauthnUserHandle,
			Role:                 role,
			CanViewOwnUsage:      consumed.TemplateCanViewOwnUsage.Bool,
			CanManageOwnApiKeys:  consumed.TemplateCanManageOwnApiKeys.Bool,
			CanViewModels:        consumed.TemplateCanViewModels.Bool,
			CanViewOwnTraces:     consumed.TemplateCanViewOwnTraces.Bool,
			CanManageOwnProjects: consumed.TemplateCanManageOwnProjects.Bool,
			Disabled:             false,
		})
		if err != nil {
			if isUniqueViolation(err) {
				writeAuthErr(w, auth.ErrUsernameTaken())
				return
			}
			writeAuthErr(w, fmt.Errorf("enrollment/complete invite: insert account: %w", err))
			return
		}
		account = a
		credID, err = insertCredentialForTx(qtx, r.Context(), a.ID, cred, auth.NormalizeNickname(&stash.Invite.Nickname))
		if err != nil {
			writeAuthErr(w, fmt.Errorf("enrollment/complete: insert credential: %w", err))
			return
		}

	case auth.IntentReset:
		if !consumed.TargetAccountID.Valid {
			writeAuthErr(w, auth.ErrEnrollmentConsumed())
			return
		}
		a, err := qtx.GetAccountByID(r.Context(), consumed.TargetAccountID.Int32)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeAuthErr(w, auth.ErrAccountNotFound())
				return
			}
			writeAuthErr(w, fmt.Errorf("enrollment/complete reset: get account: %w", err))
			return
		}
		// Reset: delete all existing credentials, then register the new one.
		if err := qtx.DeleteAllCredentialsForAccount(r.Context(), a.ID); err != nil {
			writeAuthErr(w, fmt.Errorf("enrollment/complete: delete creds: %w", err))
			return
		}
		// Build the user adapter with no credentials (we just deleted them).
		wu := &auth.WebAuthnAccount{Account: &a, Credentials: nil}
		cred, err := s.webauthn.CreateCredential(wu, stash.Data, parsed)
		if err != nil {
			writeAuthErr(w, auth.MapRegisterCeremonyError(r.Context(), err))
			return
		}
		account = a
		var resetNickname *string
		if stash.Reset != nil {
			resetNickname = auth.NormalizeNickname(&stash.Reset.Nickname)
		}
		credID, err = insertCredentialForTx(qtx, r.Context(), a.ID, cred, resetNickname)
		if err != nil {
			writeAuthErr(w, fmt.Errorf("enrollment/complete: insert credential: %w", err))
			return
		}

	default:
		writeAuthErr(w, auth.ErrEnrollmentConsumed())
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/complete: commit: %w", err))
		return
	}

	logx.WithContext(r.Context()).WithFields(logrus.Fields{
		"event":      "auth.enrollment_consumed",
		"intent":     consumed.Intent,
		"account_id": account.ID,
		"client_ip":  auth.ClientIP(r, s.config.TrustProxy),
	}).Info("auth")

	// Post-commit cleanup (best-effort).
	_ = s.kvStore.Del(r.Context(), "webauthn_ceremony:enroll:"+token)
	if consumed.Intent == auth.IntentReset {
		_, _ = s.sessionStore.RevokeAllForAccount(r.Context(), account.ID)
	}

	// Issue session for the (new or existing) account.
	ip := auth.ClientIP(r, s.config.TrustProxy)
	sessionToken, _, err := s.sessionStore.Issue(r.Context(), account.ID, ip, r.UserAgent())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/complete: session issue: %w", err))
		return
	}
	http.SetCookie(w, auth.FreshSessionCookie(s.config, r, account.ID, sessionToken, s.config.SessionTTL))

	// Capture the new credential's id so the FE can offer a "name your passkey"
	// prompt without a separate fetch.
	type enrollCompleteResp struct {
		Session         contract.SessionView `json:"session"`
		NewCredentialID int32                `json:"newCredentialId"`
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(enrollCompleteResp{
		Session:         sessionView(&account),
		NewCredentialID: credID,
	})
}

// insertCredentialForTx persists a webauthn.Credential into webauthn_credential
// inside an existing TX. The optional nickname is stored as-is (callers should
// pass auth.NormalizeNickname output). Returns the new row's id.
func insertCredentialForTx(q db.Querier, ctx context.Context, accountID int32, cred *webauthn.Credential, nickname *string) (int32, error) {
	transports := make([]string, 0, len(cred.Transport))
	for _, t := range cred.Transport {
		transports = append(transports, string(t))
	}
	var n pgtype.Text
	if nickname != nil {
		n = pgtype.Text{String: *nickname, Valid: true}
	}
	row, err := q.InsertCredential(ctx, db.InsertCredentialParams{
		AccountID:       accountID,
		CredentialID:    cred.ID,
		PublicKey:       cred.PublicKey,
		SignCount:       int64(cred.Authenticator.SignCount),
		Transports:      transports,
		Aaguid:          cred.Authenticator.AAGUID,
		AttestationType: cred.AttestationType,
		BackupEligible:  cred.Flags.BackupEligible,
		BackupState:     cred.Flags.BackupState,
		Nickname:        n,
	})
	return row.ID, err
}
