package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sirupsen/logrus"

	acctpkg "prohibitorum/pkg/account"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/credential/enrollment"
	webauthnauth "prohibitorum/pkg/credential/webauthn"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/federation"
	"prohibitorum/pkg/logx"
	sessstore "prohibitorum/pkg/session"
)

// enrollCeremonyKey derives the WebAuthn-ceremony KV key from a SHA-256 of the
// enrollment token rather than the raw token, so the bearer secret never appears
// in the KV keyspace — matching the add-passkey/sudo ceremony hardening (audit
// WACER-3). SHA-256 of a high-entropy random token needs no salt.
func enrollCeremonyKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "webauthn_ceremony:enroll:" + hex.EncodeToString(sum[:])
}

// authErrToHuma converts an *authn.AuthError into a *weberr.PublicError so
// typed Huma handlers return the project's {code, details?, requestId}
// envelope with the correct HTTP status (from the registry definition) and
// no message field. PublicError implements huma.StatusError via GetStatus(),
// so huma serializes it directly. Non-AuthError values are returned as-is
// (huma will render them as a 500).
func authErrToHuma(err error) error {
	ae := authn.AsAuthError(err)
	if ae == nil {
		return err
	}
	return ae.PublicError()
}

func (s *Server) enrollmentQ() db.Querier {
	if s.enrollmentQueriesOverride != nil {
		return s.enrollmentQueriesOverride
	}
	return s.queries
}

type enrollmentWebAuthn interface {
	BeginRegistration(webauthn.User, ...webauthn.RegistrationOption) (*protocol.CredentialCreation, *webauthn.SessionData, error)
	CreateCredential(webauthn.User, webauthn.SessionData, *protocol.ParsedCredentialCreationData) (*webauthn.Credential, error)
}

func (s *Server) enrollmentWebAuthn() enrollmentWebAuthn {
	if s.enrollmentWebAuthnOverride != nil {
		return s.enrollmentWebAuthnOverride
	}
	return s.webauthn
}

type enrollmentTx interface {
	Queries() db.Querier
	Commit(context.Context) error
	Rollback(context.Context) error
}

type enrollmentTxRunner interface {
	BeginEnrollmentTx(context.Context) (enrollmentTx, error)
}

type pgEnrollmentTx struct {
	pgx.Tx
	queries db.Querier
}

func (tx *pgEnrollmentTx) Queries() db.Querier { return tx.queries }

func (s *Server) beginEnrollmentTx(ctx context.Context) (enrollmentTx, error) {
	if s.enrollmentTxRunnerOverride != nil {
		return s.enrollmentTxRunnerOverride.BeginEnrollmentTx(ctx)
	}
	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &pgEnrollmentTx{Tx: tx, queries: s.queries.WithTx(tx)}, nil
}

// recheckVRChatEnrollmentProvider is the single provider-binding gate for
// public VRChat registration and recovery. Every invalid, unavailable, or
// changed provider state is intentionally projected to one safe public error.
func (s *Server) recheckVRChatEnrollmentProvider(ctx context.Context, q db.Querier, providerID int64) (federation.Provider, error) {
	if providerID <= 0 || s.federationRegistry == nil {
		return federation.Provider{}, authn.ErrProviderNotReady()
	}
	provider, err := federation.NewProviderStore(q).ByID(ctx, providerID)
	if err != nil || provider.ID != providerID || provider.Protocol != "vrchat" ||
		provider.Mode != federation.ModeLinkOnly || provider.Disabled {
		return federation.Provider{}, authn.ErrProviderNotReady()
	}
	definition, err := s.federationRegistry.Definition("vrchat")
	if err != nil || !definition.Ready(provider) {
		return federation.Provider{}, authn.ErrProviderNotReady()
	}
	return provider, nil
}

// ----- preview (typed) -----------------------------------------------------

type previewIn struct {
	Token string `path:"token"`
}

type previewOut struct {
	Body contract.EnrollmentPreview
}

func (s *Server) handlePreviewEnrollment(ctx context.Context, in *previewIn) (*previewOut, error) {
	q := s.enrollmentQ()
	e, err := enrollment.LoadEnrollment(ctx, q, in.Token)
	if err != nil {
		return nil, authErrToHuma(err)
	}
	out := contract.EnrollmentPreview{
		Intent:    e.Intent,
		ExpiresAt: e.ExpiresAt.Time,
	}
	switch e.Intent {
	case enrollment.IntentBootstrap:
		// no target — bootstrap creates a brand-new admin
	case enrollment.IntentInvite:
		// No target hint — invitee picks their own username/displayName from
		// scratch. The template only carries role + attributes.
	case enrollment.IntentFederatedRegister:
		if !e.FederatedUpstreamIdpID.Valid || !e.FederatedDisplayName.Valid {
			return nil, authErrToHuma(authn.ErrProviderNotReady())
		}
		if _, err := s.recheckVRChatEnrollmentProvider(ctx, q, e.FederatedUpstreamIdpID.Int64); err != nil {
			return nil, authErrToHuma(err)
		}
		out.SuggestedDisplayName = e.FederatedDisplayName.String
	case enrollment.IntentReset:
		if !e.RecoverySourceUpstreamIdpID.Valid && e.TargetAccountID.Valid {
			if a, gerr := q.GetAccountByID(ctx, e.TargetAccountID.Int32); gerr == nil {
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
	Bootstrap *newAccountCeremony  `json:"bootstrap,omitempty"`
	Invite    *newAccountCeremony  `json:"invite,omitempty"`
	Federated *newAccountCeremony  `json:"federated,omitempty"`
	Reset     *resetCeremony       `json:"reset,omitempty"`
}

type newAccountCeremony struct {
	Username           string `json:"username"`
	DisplayName        string `json:"displayName"`
	WebauthnUserHandle []byte `json:"webauthn_user_handle"`
	Nickname           string `json:"nickname,omitempty"`
}

type resetCeremony struct {
	Nickname string `json:"nickname,omitempty"`
}

func prepareNewEnrollmentAccount(ctx context.Context, q db.Querier, body enrollBeginBody, role, checkContext string) (*webauthnauth.WebAuthnAccount, *newAccountCeremony, error) {
	if err := acctpkg.ValidateUsername(body.Username); err != nil {
		return nil, nil, err
	}
	if err := acctpkg.ValidateDisplayName(body.DisplayName); err != nil {
		return nil, nil, err
	}
	if err := acctpkg.ValidateNickname(&body.Nickname); err != nil {
		return nil, nil, err
	}
	if _, err := q.GetAccountByUsername(ctx, body.Username); err == nil {
		return nil, nil, authn.ErrUsernameTaken()
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, fmt.Errorf("%s: check username: %w", checkContext, err)
	}
	handle, err := acctpkg.GenerateUserHandle()
	if err != nil {
		return nil, nil, err
	}
	proposal := &newAccountCeremony{
		Username:           body.Username,
		DisplayName:        body.DisplayName,
		WebauthnUserHandle: handle,
		Nickname:           body.Nickname,
	}
	return &webauthnauth.WebAuthnAccount{Account: &db.Account{
		Username:           body.Username,
		DisplayName:        body.DisplayName,
		WebauthnUserHandle: handle,
		Role:               role,
	}}, proposal, nil
}

func (s *Server) handleEnrollmentBeginHTTP(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	q := s.enrollmentQ()
	e, err := enrollment.LoadEnrollment(r.Context(), q, token)
	if err != nil {
		writeAuthErr(w, err)
		return
	}

	var body enrollBeginBody
	if r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}

	var (
		wu    *webauthnauth.WebAuthnAccount
		stash enrollCeremonyStash
	)
	switch e.Intent {
	case enrollment.IntentBootstrap:
		wu, stash.Bootstrap, err = prepareNewEnrollmentAccount(r.Context(), q, body, "admin", "enrollment/begin")
		if err != nil {
			writeAuthErr(w, err)
			return
		}

	case enrollment.IntentInvite:
		// Federation-bound invites MUST be redeemed via /enrollments/{token}/start-federation.
		if e.ExpectedUpstreamIdpSlug.Valid && e.ExpectedUpstreamIdpSlug.String != "" {
			audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
				Factor: audit.FactorEnrollment,
				Event:  audit.EventFail,
				Detail: map[string]any{"reason": "federation_required"},
			})
			writeAuthErr(w, authn.ErrEnrollmentFederationRequired())
			return
		}
		role := "user"
		if e.TemplateRole.Valid {
			role = e.TemplateRole.String
		}
		wu, stash.Invite, err = prepareNewEnrollmentAccount(r.Context(), q, body, role, "enrollment/begin invite")
		if err != nil {
			writeAuthErr(w, err)
			return
		}

	case enrollment.IntentFederatedRegister:
		if !e.FederatedUpstreamIdpID.Valid {
			writeAuthErr(w, authn.ErrProviderNotReady())
			return
		}
		if _, err := s.recheckVRChatEnrollmentProvider(r.Context(), q, e.FederatedUpstreamIdpID.Int64); err != nil {
			writeAuthErr(w, err)
			return
		}
		wu, stash.Federated, err = prepareNewEnrollmentAccount(r.Context(), q, body, "user", "enrollment/begin federated")
		if err != nil {
			writeAuthErr(w, err)
			return
		}

	case enrollment.IntentReset:
		if err := acctpkg.ValidateNickname(&body.Nickname); err != nil {
			writeAuthErr(w, err)
			return
		}
		if !e.TargetAccountID.Valid {
			writeAuthErr(w, authn.ErrEnrollmentConsumed())
			return
		}
		providerRecovery := e.RecoverySourceUpstreamIdpID.Valid
		if providerRecovery {
			if _, err := s.recheckVRChatEnrollmentProvider(r.Context(), q, e.RecoverySourceUpstreamIdpID.Int64); err != nil {
				writeAuthErr(w, err)
				return
			}
		}
		a, err := q.GetAccountByID(r.Context(), e.TargetAccountID.Int32)
		if err != nil {
			writeAuthErr(w, authn.ErrEnrollmentConsumed())
			return
		}
		if a.Disabled {
			audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
				AccountID: &a.ID,
				Factor:    audit.FactorEnrollment,
				Event:     audit.EventFail,
				Detail:    map[string]any{"reason": "account_disabled"},
			})
			writeAuthErr(w, authn.ErrEnrollmentConsumed())
			return
		}
		if providerRecovery {
			// Public recovery keeps only the opaque WebAuthn user handle. Public
			// labels are deliberately neutral and existing credential IDs are not
			// supplied as exclusions.
			neutral := a
			neutral.Username = "account"
			neutral.DisplayName = "account"
			wu = &webauthnauth.WebAuthnAccount{Account: &neutral}
		} else {
			creds, _ := q.ListCredentialsByAccount(r.Context(), a.ID)
			wu = &webauthnauth.WebAuthnAccount{Account: &a, Credentials: creds}
		}
		stash.Reset = &resetCeremony{Nickname: body.Nickname}

	default:
		writeAuthErr(w, authn.ErrEnrollmentConsumed())
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

	creation, sessionData, err := s.enrollmentWebAuthn().BeginRegistration(wu, webauthnauth.RegistrationOptions(exclude)...)
	if err != nil {
		writeAuthErr(w, webauthnauth.MapRegisterCeremonyError(r.Context(), err))
		return
	}
	stash.Data = *sessionData
	raw, err := json.Marshal(stash)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/begin: marshal: %w", err))
		return
	}
	if err := s.kvStore.SetEx(r.Context(), enrollCeremonyKey(token), string(raw), 5*time.Minute); err != nil {
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
	q := s.enrollmentQ()
	if _, err := enrollment.LoadEnrollment(r.Context(), q, token); err != nil {
		writeAuthErr(w, err)
		return
	}

	raw, err := s.kvStore.Get(r.Context(), enrollCeremonyKey(token))
	if err != nil {
		writeAuthErr(w, authn.ErrCeremonyExpired())
		return
	}
	var stash enrollCeremonyStash
	if err := json.Unmarshal([]byte(raw), &stash); err != nil {
		writeAuthErr(w, authn.ErrCeremonyState())
		return
	}

	parsed, err := protocol.ParseCredentialCreationResponseBody(r.Body)
	if err != nil {
		writeAuthErr(w, webauthnauth.MapRegisterCeremonyError(r.Context(), err))
		return
	}

	tx, err := s.beginEnrollmentTx(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/complete: begin tx: %w", err))
		return
	}
	defer tx.Rollback(r.Context()) //nolint:errcheck

	qtx := tx.Queries()

	// Atomic consume: acquires a row-level lock via the conditional UPDATE,
	// serializing concurrent /complete requests on the same token. One wins
	// (gets the row), all others get pgx.ErrNoRows → enrollment_consumed.
	consumed, err := enrollment.ConsumeEnrollment(r.Context(), qtx, token)
	if err != nil {
		writeAuthErr(w, err)
		return
	}

	var (
		acct              db.Account
		credID            int32
		federatedProvider *federation.Provider
		federatedAvatar   string
	)

	switch consumed.Intent {
	case enrollment.IntentBootstrap:
		if stash.Bootstrap == nil {
			writeAuthErr(w, authn.ErrCeremonyState())
			return
		}
		wu := &webauthnauth.WebAuthnAccount{Account: &db.Account{
			Username:           stash.Bootstrap.Username,
			DisplayName:        stash.Bootstrap.DisplayName,
			WebauthnUserHandle: stash.Bootstrap.WebauthnUserHandle,
		}}
		cred, err := s.enrollmentWebAuthn().CreateCredential(wu, stash.Data, parsed)
		if err != nil {
			writeAuthErr(w, webauthnauth.MapRegisterCeremonyError(r.Context(), err))
			return
		}
		a, err := qtx.InsertAccount(r.Context(), db.InsertAccountParams{
			Username:           stash.Bootstrap.Username,
			DisplayName:        stash.Bootstrap.DisplayName,
			WebauthnUserHandle: stash.Bootstrap.WebauthnUserHandle,
			Role:               "admin",
			Attributes:         []byte("{}"),
			Disabled:           false,
		})
		if err != nil {
			if isUniqueViolation(err) {
				writeAuthErr(w, authn.ErrUsernameTaken())
				return
			}
			writeAuthErr(w, fmt.Errorf("enrollment/complete bootstrap: insert account: %w", err))
			return
		}
		acct = a
		credID, err = insertCredentialForTx(qtx, r.Context(), a.ID, stash.Bootstrap.WebauthnUserHandle, cred, acctpkg.NormalizeNickname(&stash.Bootstrap.Nickname))
		if err != nil {
			writeAuthErr(w, fmt.Errorf("enrollment/complete: insert credential: %w", err))
			return
		}

	case enrollment.IntentInvite:
		// Belt-and-suspenders for the M1-int audit gate at /begin: even if
		// the /begin guard was bypassed (e.g., a stale stash on a freshly
		// federation-bound invite), reject here too. The tx rolls back the
		// already-consumed enrollment so the invitee can retry via the
		// correct /enrollments/{token}/start-federation entrypoint.
		if consumed.ExpectedUpstreamIdpSlug.Valid && consumed.ExpectedUpstreamIdpSlug.String != "" {
			// Use the outer writer: this error rolls back the tx, so a
			// tx-scoped audit row would disappear with it.
			audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
				Factor: audit.FactorEnrollment,
				Event:  audit.EventFail,
				Detail: map[string]any{"reason": "federation_required"},
			})
			writeAuthErr(w, authn.ErrEnrollmentFederationRequired())
			return
		}
		if stash.Invite == nil {
			writeAuthErr(w, authn.ErrCeremonyState())
			return
		}
		// Build the WebAuthn user adapter — same identity the /begin step used,
		// so the assertion verifies against the same rp.id + user.id.
		wu := &webauthnauth.WebAuthnAccount{Account: &db.Account{
			Username:           stash.Invite.Username,
			DisplayName:        stash.Invite.DisplayName,
			WebauthnUserHandle: stash.Invite.WebauthnUserHandle,
		}}
		cred, err := s.enrollmentWebAuthn().CreateCredential(wu, stash.Data, parsed)
		if err != nil {
			writeAuthErr(w, webauthnauth.MapRegisterCeremonyError(r.Context(), err))
			return
		}
		// Role from template; fall back to "user" if somehow not set (defensive).
		role := "user"
		if consumed.TemplateRole.Valid {
			role = consumed.TemplateRole.String
		}
		// Template attributes from enrollment row.
		attrs := enrollment.DecodeTemplateAttributes(consumed.TemplateAttributes)
		a, err := qtx.InsertAccount(r.Context(), db.InsertAccountParams{
			Username:           stash.Invite.Username,
			DisplayName:        stash.Invite.DisplayName,
			WebauthnUserHandle: stash.Invite.WebauthnUserHandle,
			Role:               role,
			Attributes:         encodeAttributes(attrs),
			Disabled:           false,
		})
		if err != nil {
			if isUniqueViolation(err) {
				writeAuthErr(w, authn.ErrUsernameTaken())
				return
			}
			writeAuthErr(w, fmt.Errorf("enrollment/complete invite: insert account: %w", err))
			return
		}
		acct = a
		credID, err = insertCredentialForTx(qtx, r.Context(), a.ID, stash.Invite.WebauthnUserHandle, cred, acctpkg.NormalizeNickname(&stash.Invite.Nickname))
		if err != nil {
			writeAuthErr(w, fmt.Errorf("enrollment/complete: insert credential: %w", err))
			return
		}

	case enrollment.IntentFederatedRegister:
		if stash.Federated == nil {
			audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
				Factor: audit.FactorEnrollment,
				Event:  audit.EventFail,
				Detail: map[string]any{"reason": "ceremony_state"},
			})
			writeAuthErr(w, authn.ErrCeremonyState())
			return
		}
		if !consumed.FederatedUpstreamIdpID.Valid || !consumed.FederatedUpstreamIss.Valid ||
			!consumed.FederatedUpstreamSub.Valid || len(consumed.FederatedUpstreamData) == 0 {
			writeAuthErr(w, authn.ErrCeremonyState())
			return
		}
		provider, err := s.recheckVRChatEnrollmentProvider(r.Context(), qtx, consumed.FederatedUpstreamIdpID.Int64)
		if err != nil {
			audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
				Factor: audit.FactorEnrollment,
				Event:  audit.EventFail,
				Detail: map[string]any{"reason": "provider_unavailable"},
			})
			writeAuthErr(w, err)
			return
		}
		wu := &webauthnauth.WebAuthnAccount{Account: &db.Account{
			Username:           stash.Federated.Username,
			DisplayName:        stash.Federated.DisplayName,
			WebauthnUserHandle: stash.Federated.WebauthnUserHandle,
		}}
		cred, err := s.enrollmentWebAuthn().CreateCredential(wu, stash.Data, parsed)
		if err != nil {
			writeAuthErr(w, webauthnauth.MapRegisterCeremonyError(r.Context(), err))
			return
		}
		a, err := qtx.InsertAccount(r.Context(), db.InsertAccountParams{
			Username:           stash.Federated.Username,
			DisplayName:        stash.Federated.DisplayName,
			WebauthnUserHandle: stash.Federated.WebauthnUserHandle,
			Role:               "user",
			Attributes:         []byte("{}"),
			Disabled:           false,
		})
		if err != nil {
			if isUniqueViolation(err) {
				audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
					Factor: audit.FactorEnrollment,
					Event:  audit.EventFail,
					Detail: map[string]any{"reason": "username_collision"},
				})
				writeAuthErr(w, authn.ErrUsernameTaken())
				return
			}
			writeAuthErr(w, fmt.Errorf("enrollment/complete federated: insert account: %w", err))
			return
		}
		acct = a
		credID, err = insertCredentialForTx(qtx, r.Context(), a.ID, stash.Federated.WebauthnUserHandle, cred, acctpkg.NormalizeNickname(&stash.Federated.Nickname))
		if err != nil {
			writeAuthErr(w, fmt.Errorf("enrollment/complete federated: insert credential: %w", err))
			return
		}
		_, err = qtx.GetAccountIdentityByIssuerSub(r.Context(), db.GetAccountIdentityByIssuerSubParams{
			UpstreamIss: consumed.FederatedUpstreamIss.String,
			UpstreamSub: consumed.FederatedUpstreamSub.String,
		})
		if err == nil {
			audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
				Factor: audit.FactorEnrollment,
				Event:  audit.EventFail,
				Detail: map[string]any{"reason": "identity_conflict"},
			})
			writeAuthErr(w, authn.ErrFederationIdentityConflict())
			return
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, fmt.Errorf("enrollment/complete federated: check identity: %w", err))
			return
		}
		identity, err := qtx.InsertAccountIdentity(r.Context(), db.InsertAccountIdentityParams{
			AccountID:     a.ID,
			UpstreamIdpID: provider.ID,
			UpstreamIss:   consumed.FederatedUpstreamIss.String,
			UpstreamSub:   consumed.FederatedUpstreamSub.String,
			UpstreamData:  append([]byte(nil), consumed.FederatedUpstreamData...),
		})
		if err != nil {
			if isUniqueViolation(err) {
				audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
					Factor: audit.FactorEnrollment,
					Event:  audit.EventFail,
					Detail: map[string]any{"reason": "identity_conflict"},
				})
				writeAuthErr(w, authn.ErrFederationIdentityConflict())
				return
			}
			writeAuthErr(w, fmt.Errorf("enrollment/complete federated: insert identity: %w", err))
			return
		}
		if err := qtx.ConfirmAccountIdentity(r.Context(), identity.ID); err != nil {
			writeAuthErr(w, fmt.Errorf("enrollment/complete federated: confirm identity: %w", err))
			return
		}
		federatedProvider = &provider
		if consumed.FederatedAvatarUrl.Valid {
			federatedAvatar = consumed.FederatedAvatarUrl.String
		}

	case enrollment.IntentReset:
		if consumed.RecoverySourceUpstreamIdpID.Valid {
			if _, err := s.recheckVRChatEnrollmentProvider(r.Context(), qtx, consumed.RecoverySourceUpstreamIdpID.Int64); err != nil {
				audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
					Factor: audit.FactorEnrollment,
					Event:  audit.EventFail,
					Detail: map[string]any{"reason": "provider_unavailable"},
				})
				writeAuthErr(w, err)
				return
			}
		}
		if !consumed.TargetAccountID.Valid {
			writeAuthErr(w, authn.ErrEnrollmentConsumed())
			return
		}
		a, err := qtx.GetAccountByID(r.Context(), consumed.TargetAccountID.Int32)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeAuthErr(w, authn.ErrAccountNotFound())
				return
			}
			writeAuthErr(w, fmt.Errorf("enrollment/complete reset: get account: %w", err))
			return
		}
		// Refuse to complete a reset against a disabled account (T1.2): the token
		// is consumed here, so check before the destructive credential wipe.
		if a.Disabled {
			// Use the outer writer: this error rolls back the tx, so a
			// tx-scoped audit row would disappear with it.
			audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
				AccountID: &a.ID,
				Factor:    audit.FactorEnrollment,
				Event:     audit.EventFail,
				Detail:    map[string]any{"reason": "account_disabled"},
			})
			writeAuthErr(w, authn.ErrEnrollmentConsumed())
			return
		}
		// Reset: delete all existing credentials, then register the new one.
		if err := qtx.DeleteAllCredentialsForAccount(r.Context(), a.ID); err != nil {
			writeAuthErr(w, fmt.Errorf("enrollment/complete: delete creds: %w", err))
			return
		}
		// Build the user adapter with no credentials (we just deleted them).
		wu := &webauthnauth.WebAuthnAccount{Account: &a, Credentials: nil}
		cred, err := s.enrollmentWebAuthn().CreateCredential(wu, stash.Data, parsed)
		if err != nil {
			writeAuthErr(w, webauthnauth.MapRegisterCeremonyError(r.Context(), err))
			return
		}
		acct = a
		var resetNickname *string
		if stash.Reset != nil {
			resetNickname = acctpkg.NormalizeNickname(&stash.Reset.Nickname)
		}
		credID, err = insertCredentialForTx(qtx, r.Context(), a.ID, a.WebauthnUserHandle, cred, resetNickname)
		if err != nil {
			writeAuthErr(w, fmt.Errorf("enrollment/complete: insert credential: %w", err))
			return
		}

	default:
		writeAuthErr(w, authn.ErrEnrollmentConsumed())
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/complete: commit: %w", err))
		return
	}
	if federatedProvider != nil && federatedAvatar != "" && s.enrollmentAvatarOverride != nil {
		_ = s.enrollmentAvatarOverride(acct.ID, *federatedProvider, federation.AvatarDelivery{URL: federatedAvatar})
	}

	logx.WithContext(r.Context()).WithFields(logrus.Fields{
		"event":      "auth.enrollment_consumed",
		"intent":     consumed.Intent,
		"account_id": acct.ID,
		"client_ip":  s.clientIP.IP(r),
	}).Info("auth")

	// Emit enrollment_consumed AFTER commit: the account row is now visible on all
	// connections so the credential_event.account_id FK resolves. Outer s.Audit is
	// correct here — not a tx-scoped writer — because the tx has already committed.
	auditDetail := map[string]any{"intent": string(consumed.Intent)}
	if consumed.Intent == enrollment.IntentReset && consumed.RecoverySourceUpstreamIdpID.Valid {
		auditDetail["source"] = "vrchat"
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: &acct.ID,
		Factor:    audit.FactorEnrollment,
		Event:     audit.EventEnrollmentConsumed,
		Detail:    auditDetail,
	})

	// Ceremony-state cleanup is best-effort after the enrollment transaction commits.
	_ = s.kvStore.Del(r.Context(), enrollCeremonyKey(token))
	if consumed.Intent == enrollment.IntentReset {
		_, revokeErr := s.sessionStore.RevokeAllForAccount(r.Context(), acct.ID)
		if revokeErr != nil && consumed.RecoverySourceUpstreamIdpID.Valid {
			// Credential replacement and enrollment consumption have already
			// committed. Fail closed rather than issue a fresh session while a
			// compromised old session may still be live.
			writeAuthErr(w, fmt.Errorf("enrollment/complete: revoke sessions: %w", revokeErr))
			return
		}
	}

	if me := s.maintenanceLockout(r.Context(), acct.ID); me != nil {
		writeAuthErr(w, me)
		return
	}
	// Issue session for the (new or existing) account.
	ip := s.clientIP.IP(r)
	sessionToken, _, err := s.sessionStore.Issue(r.Context(), acct.ID, ip, r.UserAgent(), []string{"hwk"}, nil)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("enrollment/complete: session issue: %w", err))
		return
	}
	audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
		AccountID: &acct.ID,
		Factor:    audit.FactorSession,
		Event:     audit.EventSessionStart,
		Detail:    map[string]any{"via": "enrollment"},
	})
	http.SetCookie(w, sessstore.FreshSessionCookie(s.config, r, acct.ID, sessionToken, s.config.SessionTTL))

	// Capture the new credential's id so the FE can offer a "name your passkey"
	// prompt without a separate fetch.
	type enrollCompleteResp struct {
		Session         contract.SessionView `json:"session"`
		NewCredentialID int32                `json:"newCredentialId"`
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(enrollCompleteResp{
		Session:         s.sessionView(&acct),
		NewCredentialID: credID,
	})
}

// insertCredentialForTx persists a webauthn.Credential into webauthn_credential
// inside an existing TX. userHandle is the account's WebAuthn user handle.
// The optional nickname is stored as-is (callers should pass acctpkg.NormalizeNickname output).
// Returns the new row's id.
func insertCredentialForTx(q db.Querier, ctx context.Context, accountID int32, userHandle []byte, cred *webauthn.Credential, nickname *string) (int32, error) {
	transports := make([]string, 0, len(cred.Transport))
	for _, t := range cred.Transport {
		transports = append(transports, string(t))
	}
	var n pgtype.Text
	if nickname != nil {
		n = pgtype.Text{String: *nickname, Valid: true}
	}
	var attType pgtype.Text
	if cred.AttestationType != "" {
		attType = pgtype.Text{String: cred.AttestationType, Valid: true}
	}
	row, err := q.InsertCredential(ctx, db.InsertCredentialParams{
		AccountID:       accountID,
		CredentialID:    cred.ID,
		PublicKey:       cred.PublicKey,
		CoseAlg:         webauthnauth.COSEAlg(cred.PublicKey),
		UserHandle:      userHandle,
		SignCount:       int64(cred.Authenticator.SignCount),
		Transports:      transports,
		Aaguid:          cred.Authenticator.AAGUID,
		AttestationType: attType,
		BackupEligible:  pgtype.Bool{Bool: cred.Flags.BackupEligible, Valid: true},
		BackupState:     pgtype.Bool{Bool: cred.Flags.BackupState, Valid: true},
		UvInitialized:   cred.Flags.UserVerified,
		Nickname:        n,
	})
	return row.ID, err
}
