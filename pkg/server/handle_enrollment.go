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

// inviteRedeemableProviders lists the providers an invite may be redeemed
// through: the bound slug only when set (empty list when that provider is
// link_only — it never creates accounts), otherwise every enabled
// auto_provision/invite_only IdP. Icons are best-effort: a lookup failure
// degrades to the initial-letter fallback, never a failed response.
func (s *Server) inviteRedeemableProviders(ctx context.Context, boundSlug string) []contract.FederationProvider {
	q := s.enrollmentQ()
	idps, err := q.ListUpstreamIDPs(ctx)
	if err != nil {
		return nil
	}
	etagBySlug := map[string]string{}
	if icons, ierr := q.ListEntityIconEtags(ctx, "upstream_idp"); ierr == nil {
		for _, ic := range icons {
			etagBySlug[ic.OwnerID] = ic.Etag
		}
	}
	out := make([]contract.FederationProvider, 0, len(idps))
	for _, idp := range idps {
		if boundSlug != "" {
			if idp.Slug != boundSlug {
				continue
			}
			if idp.Mode == federation.ModeLinkOnly {
				return nil
			}
		} else if idp.Mode != federation.ModeAutoProvision && idp.Mode != federation.ModeInviteOnly {
			continue
		}
		out = append(out, contract.FederationProvider{
			Slug:        idp.Slug,
			DisplayName: idp.DisplayName,
			Protocol:    idp.Protocol,
			IconURL:     entityIconURLPtr("upstream_idp", idp.Slug, etagBySlug[idp.Slug]),
		})
	}
	return out
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
	// AuditWriter writes through the open transaction, so audit rows
	// referencing the just-inserted account resolve their foreign key and
	// roll back with the transaction if it fails.
	AuditWriter() audit.Writer
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

func (tx *pgEnrollmentTx) AuditWriter() audit.Writer { return audit.NewWriter(tx.queries) }

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
		Intent:         e.Intent,
		ExpiresAt:      e.ExpiresAt.Time,
		AllowedMethods: enrollmentAllowedMethods(e.Intent),
	}
	switch e.Intent {
	case enrollment.IntentBootstrap:
		// no target — bootstrap creates a brand-new admin
	case enrollment.IntentInvite:
		// No target hint — the invitee registers through a provider (username
		// from the upstream claims) or local credentials. Surface the provider
		// binding and the redeemable provider list so the enroll page can
		// render the choice without a probe round-trip.
		if e.ExpectedUpstreamIdpSlug.Valid && e.ExpectedUpstreamIdpSlug.String != "" {
			out.ExpectedUpstreamIdpSlug = e.ExpectedUpstreamIdpSlug.String
		}
		if e.TemplateUsername.Valid {
			out.Username = e.TemplateUsername.String
		}
		out.RequireLocalCredential = e.RequireLocalCredential
		if e.FederatedUpstreamIdpID.Valid {
			// The snapshot's provider must still be usable; when it is, the
			// page can skip the provider buttons entirely.
			if provider, err := recheckInviteIdentityProvider(ctx, q, *e); err == nil {
				identity := &contract.EnrollmentVerifiedIdentity{
					ProviderSlug:        provider.Slug,
					ProviderDisplayName: provider.DisplayName,
				}
				if e.FederatedDisplayName.Valid {
					identity.DisplayName = e.FederatedDisplayName.String
				}
				if e.FederatedAvatarUrl.Valid {
					avatar := e.FederatedAvatarUrl.String
					identity.AvatarURL = &avatar
				}
				out.VerifiedIdentity = identity
			}
			// A snapshot provider that is gone leaves VerifiedIdentity unset:
			// the page shows the unverified state, and completion re-checks
			// and refuses.
		} else {
			// No snapshot yet: a bound provider with sign-in off still forces
			// the local credential, so reflect that in the preview.
			if out.ExpectedUpstreamIdpSlug != "" {
				if provider, err := q.GetUpstreamIDPBySlug(ctx, out.ExpectedUpstreamIdpSlug); err == nil && !provider.AllowSignIn {
					out.RequireLocalCredential = true
				}
			}
		}
		out.Providers = s.inviteRedeemableProviders(ctx, out.ExpectedUpstreamIdpSlug)
	case enrollment.IntentReset:
		// A reset names the account the new sign-in method is for. A reset
		// without a readable target fails the preview rather than leaving the
		// page to guess whose account it is.
		if !e.TargetAccountID.Valid {
			return nil, authErrToHuma(errors.New("handlePreviewEnrollment: reset has no target account"))
		}
		a, err := q.GetAccountByID(ctx, e.TargetAccountID.Int32)
		if err != nil {
			return nil, authErrToHuma(fmt.Errorf("handlePreviewEnrollment: reset target: %w", err))
		}
		out.Target = &contract.EnrollmentTarget{
			Username:    a.Username,
			DisplayName: a.DisplayName,
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

func applyFixedInvitationUsername(body *enrollBeginBody, enrollmentRow *db.Enrollment) error {
	if !enrollmentRow.TemplateUsername.Valid {
		return nil
	}
	if body.Username != "" && body.Username != enrollmentRow.TemplateUsername.String {
		return authn.ErrUsernameImmutable()
	}
	body.Username = enrollmentRow.TemplateUsername.String
	return nil
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
		// Federation-bound invites must verify their identity through
		// /enrollments/{token}/start-federation first — unless that snapshot
		// already sits on this invite.
		if e.ExpectedUpstreamIdpSlug.Valid && e.ExpectedUpstreamIdpSlug.String != "" &&
			!(e.FederatedUpstreamIdpID.Valid && e.FederatedUpstreamIdpSlug.Valid && e.FederatedUpstreamIdpSlug.String == e.ExpectedUpstreamIdpSlug.String) {
			audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
				Factor: audit.FactorEnrollment,
				Event:  audit.EventFail,
				Detail: map[string]any{"reason": "federation_required"},
			})
			writeAuthErr(w, authn.WithFederationName(authn.ErrEnrollmentFederationRequired(), s.enabledFederationName(r.Context(), e.ExpectedUpstreamIdpSlug.String)))
			return
		}
		role := "user"
		if e.TemplateRole.Valid {
			role = e.TemplateRole.String
		}
		if err := applyFixedInvitationUsername(&body, e); err != nil {
			writeAuthErr(w, err)
			return
		}
		// A verified snapshot's display name is the default; the invitee can
		// still edit it before completion.
		if body.DisplayName == "" && e.FederatedDisplayName.Valid {
			body.DisplayName = e.FederatedDisplayName.String
		}
		wu, stash.Invite, err = prepareNewEnrollmentAccount(r.Context(), q, body, role, "enrollment/begin invite")
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
		creds, _ := q.ListCredentialsByAccount(r.Context(), a.ID)
		wu = &webauthnauth.WebAuthnAccount{Account: &a, Credentials: creds}
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
		// Belt-and-suspenders for the /begin gate: a federation-bound invite
		// without its identity snapshot is still rejected here. The tx rolls
		// back the already-consumed enrollment so the invitee can retry via
		// the correct /enrollments/{token}/start-federation entrypoint.
		if consumed.ExpectedUpstreamIdpSlug.Valid && consumed.ExpectedUpstreamIdpSlug.String != "" &&
			!(consumed.FederatedUpstreamIdpID.Valid && consumed.FederatedUpstreamIdpSlug.Valid && consumed.FederatedUpstreamIdpSlug.String == consumed.ExpectedUpstreamIdpSlug.String) {
			// Use the outer writer: this error rolls back the tx, so a
			// tx-scoped audit row would disappear with it.
			audit.RecordOrLog(r.Context(), s.Audit, audit.Record{
				Factor: audit.FactorEnrollment,
				Event:  audit.EventFail,
				Detail: map[string]any{"reason": "federation_required"},
			})
			writeAuthErr(w, authn.WithFederationName(authn.ErrEnrollmentFederationRequired(), s.enabledFederationName(r.Context(), consumed.ExpectedUpstreamIdpSlug.String)))
			return
		}
		if stash.Invite == nil {
			writeAuthErr(w, authn.ErrCeremonyState())
			return
		}
		if consumed.TemplateUsername.Valid {
			stash.Invite.Username = consumed.TemplateUsername.String
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
		if err := enrollment.ApplyInvitationGroups(r.Context(), qtx, consumed.GroupIds, a.ID, consumed.CreatedByAccountID); err != nil {
			writeAuthErr(w, err)
			return
		}
		if consumed.FederatedUpstreamIdpID.Valid {
			// The verified-identity snapshot: same tx as the account and the
			// credential, so any failure rolls the consume back too.
			if _, err := insertInviteIdentityTx(r.Context(), qtx, tx.AuditWriter(), a.ID, *consumed); err != nil {
				writeAuthErr(w, err)
				return
			}
			provider, err := recheckInviteIdentityProvider(r.Context(), qtx, *consumed)
			if err != nil {
				writeAuthErr(w, err)
				return
			}
			federatedProvider = &provider
			federatedAvatar = inviteSnapshotAvatar(*consumed)
		}

	case enrollment.IntentReset:
		if !consumed.TargetAccountID.Valid {
			writeAuthErr(w, authn.ErrEnrollmentConsumed())
			return
		}
		a, err := qtx.GetAccountByIDForUpdate(r.Context(), consumed.TargetAccountID.Int32)
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
	if federatedProvider != nil && federatedAvatar != "" && s.enrollmentAvatarRefresh != nil {
		s.enrollmentAvatarRefresh(r.Context(), acct.ID, *federatedProvider, s.federatedAvatarDelivery(*federatedProvider, federatedAvatar))
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
	if consumed.Intent == enrollment.IntentInvite {
		auditDetail["fixed_username"] = consumed.TemplateUsername.Valid
		auditDetail["group_ids"] = append([]int32(nil), consumed.GroupIds...)
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
		// Best-effort revocation: an admin-issued reset token is single-use
		// and consumed here; failing the response after commit would strand
		// the invitee without credentials or guidance.
		_, _ = s.sessionStore.RevokeAllForAccount(r.Context(), acct.ID)
	}

	if me := s.maintenanceLockout(r.Context(), acct.ID); me != nil {
		writeAuthErr(w, me)
		return
	}
	// Issue session for the (new or existing) account.
	ip := s.clientIP.IP(r)
	sessionToken, data, err := s.sessionStore.Issue(r.Context(), acct.ID, ip, r.UserAgent(), []string{"hwk"}, nil, false)
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
	http.SetCookie(w, s.sessionStore.SessionCookie(s.config, r, acct.ID, sessionToken, data))

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
