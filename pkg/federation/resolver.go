package federation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	acctpkg "prohibitorum/pkg/account"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

var ErrLocalUsernameRequired = errors.New("federation: local username required")

// ModesQueries is the narrow DB surface modes.go needs. db.Queries (which
// implements db.Querier) satisfies it implicitly; tests inject a fake.
// Kept minimal so a fake test querier can be written by hand without
// stubbing the full ~70-method db.Querier interface.
type ModesQueries interface {
	GetAccountIdentityByIssuerSub(ctx context.Context, arg db.GetAccountIdentityByIssuerSubParams) (db.AccountIdentity, error)
	GetAccountByID(ctx context.Context, id int32) (db.Account, error)
	GetAccountByUsername(ctx context.Context, username string) (db.Account, error)
	InsertAccount(ctx context.Context, arg db.InsertAccountParams) (db.Account, error)
	InsertAccountIdentity(ctx context.Context, arg db.InsertAccountIdentityParams) (db.AccountIdentity, error)
	ConfirmAccountIdentity(ctx context.Context, id int64) error
	UpdateAccountDisplayName(ctx context.Context, arg db.UpdateAccountDisplayNameParams) error
	UpdateAccountEmail(ctx context.Context, arg db.UpdateAccountEmailParams) error
	UpdateAccountIdentityEmail(ctx context.Context, arg db.UpdateAccountIdentityEmailParams) error
	ConsumeInviteEnrollment(ctx context.Context, token string) (db.Enrollment, error)
}

// ResolveOutcome is the result of identity resolution.
type ResolveOutcome struct {
	AccountID  int32
	IdentityID int64
	ProviderID int64
	AMR        []string
	IsNew      bool
	Confirmed  bool
}

// Mode constants — must match upstream_idp.mode enum in the schema.
const (
	ModeAutoProvision = "auto_provision"
	ModeInviteOnly    = "invite_only"
	ModeLinkOnly      = "link_only"
)

// Resolve is the entry point called by the callback handler. Given the
// upstream IdP row and the verified token claims, it returns the local
// account_id that should be signed in.
//
//  1. If an account_identity row already matches (iss, sub), sync any
//     drifted claims (display_name, upstream_email) and return existing
//     account_id with isNew=false.
//  2. Otherwise dispatch to the mode-specific policy: auto_provision,
//     invite_only (stub), or link_only.
//
// On success, Resolve emits an audit row. On rejection, the policy
// emits a fail row and Resolve returns the *authn.AuthError directly so
// the HTTP layer can map it to a status code + JSON body.
func Resolve(
	ctx context.Context,
	q ModesQueries,
	w audit.Writer,
	idp *db.UpstreamIdp,
	identity *VerifiedIdentity,
	pool *pgxpool.Pool,
) (ResolveOutcome, error) {
	localUsername := ""
	if identity != nil {
		localUsername = identity.Username
	}
	return resolve(ctx, q, w, idp, identity, localUsername, pool)
}

func resolve(
	ctx context.Context,
	q ModesQueries,
	w audit.Writer,
	idp *db.UpstreamIdp,
	identity *VerifiedIdentity,
	localUsername string,
	pool *pgxpool.Pool,
) (ResolveOutcome, error) {
	if idp == nil || identity == nil {
		return ResolveOutcome{}, errors.New("federation: Resolve: nil provider or identity")
	}

	existing, err := q.GetAccountIdentityByIssuerSub(ctx, db.GetAccountIdentityByIssuerSubParams{
		UpstreamIss: identity.Issuer,
		UpstreamSub: identity.Subject,
	})
	switch {
	case err == nil:
		// Scope re-login to the IdP that minted the identity: one issuer = one
		// upstream_idp row. If the (iss,sub) identity is bound to a DIFFERENT row
		// — an admin configured two rows sharing an issuer_url — refuse to
		// re-login through this one, else a user provisioned via IdP-A could log
		// in against IdP-B's slug and dodge B's provisioning gates. T4.3c.
		if existing.UpstreamIdpID != idp.ID {
			audit.RecordOrLog(ctx, w, audit.Record{
				AccountID: new(existing.AccountID),
				Factor:    audit.FactorFederationOIDC,
				Event:     audit.EventFail,
				Detail: map[string]any{
					"reason":           "idp_mismatch_relogin",
					"idp_slug":         idp.Slug,
					"bound_idp_id":     existing.UpstreamIdpID,
					"attempted_idp_id": idp.ID,
					"iss":              identity.Issuer,
					"sub":              identity.Subject,
				},
			})
			return ResolveOutcome{}, authn.ErrFederationStateInvalid()
		}
		// Re-login path. Sync claim drift, then branch on confirmed_at:
		//   - confirmed (confirmed_at NOT NULL): audit Use + issue a session now.
		//   - pending (confirmed_at NULL): the user abandoned the /welcome gate
		//     (or a federated invite is still pending). Report Confirmed=false so
		//     the HTTP layer routes back to the gate; do NOT emit Use — that's
		//     recorded on confirm (Task 6) — and do NOT re-insert anything.
		if err := syncClaims(ctx, q, idp, &existing, identity); err != nil {
			if ae := authn.AsAuthError(err); ae != nil && ae.Code == "bad_credentials" {
				audit.RecordOrLog(ctx, w, audit.Record{
					AccountID: new(existing.AccountID),
					Factor:    audit.FactorFederationOIDC,
					Event:     audit.EventFail,
					Detail: map[string]any{
						"reason":   "account_disabled",
						"idp_slug": idp.Slug,
						"iss":      identity.Issuer,
						"sub":      identity.Subject,
					},
				})
			}
			return ResolveOutcome{}, err
		}
		confirmed := existing.ConfirmedAt.Valid
		if confirmed {
			audit.RecordOrLog(ctx, w, audit.Record{
				AccountID: new(existing.AccountID),
				Factor:    audit.FactorFederationOIDC,
				Event:     audit.EventUse,
				Detail: map[string]any{
					"idp_slug": idp.Slug,
					"iss":      identity.Issuer,
					"sub":      identity.Subject,
				},
			})
		}
		return ResolveOutcome{
			AccountID:  existing.AccountID,
			IdentityID: existing.ID,
			ProviderID: idp.ID,
			AMR:        append([]string(nil), identity.AMR...),
			IsNew:      false,
			Confirmed:  confirmed,
		}, nil
	case errors.Is(err, pgx.ErrNoRows):
		// Fall through to mode dispatch.
	default:
		return ResolveOutcome{}, fmt.Errorf("federation/oidc: lookup identity: %w", err)
	}

	switch idp.Mode {
	case ModeAutoProvision:
		return applyAutoProvision(ctx, q, w, idp, identity, localUsername, pool)
	case ModeInviteOnly:
		// Reaching invite_only via Resolve means HandleCallback did NOT see an
		// EnrollmentToken on the FedState — i.e. someone hit /federation/{slug}/login
		// directly on an invite_only IdP. Dispatch into applyInviteOnly with an
		// empty token; the top of that function audits invite_required_no_token
		// and rejects. Pool is nil — no DB writes happen on the rejection path.
		return applyInviteOnly(ctx, q, w, idp, identity, "", nil)
	case ModeLinkOnly:
		return applyLinkOnly(ctx, w, idp, identity)
	default:
		return ResolveOutcome{}, fmt.Errorf("federation/oidc: unknown idp.mode %q", idp.Mode)
	}
}

// applyAutoProvision creates a fresh local account from the upstream
// claims, gated by email_verified, allowed_domains, preferred_username
// presence, and a local username-collision check.
//
// The collision-check + InsertAccount + InsertAccountIdentity sequence
// runs inside a single transaction via runProvisionTx, so a 23505
// surfacing from a concurrent same-username or same-(iss,sub) callback
// rolls back cleanly — no orphan account row, no burned username slot.
// The 23505 paths surface as ErrUsernameCollision / ErrInviteRequired
// (not raw wrapped 500s) so the HTTP layer can map them correctly.
//
// pool is nil-safe: in tests, runProvisionTx passes the existing querier
// through with no transactional semantics (call order is what's asserted).
func applyAutoProvision(
	ctx context.Context,
	q ModesQueries,
	w audit.Writer,
	idp *db.UpstreamIdp,
	identity *VerifiedIdentity,
	localUsername string,
	pool *pgxpool.Pool,
) (ResolveOutcome, error) {
	username := localUsername
	displayName := identity.DisplayName
	email := identityEmail(identity)

	// GATES — pure read-only / claim-only checks. Run outside the tx
	// because they don't touch the DB and would otherwise widen the
	// transaction window for no benefit.
	if idp.RequireVerifiedEmail && !identity.EmailVerified {
		// EmailVerified is the typed bool; no override (it's a JWT
		// standard claim with a fixed boolean shape).
		emitFail(ctx, w, idp, identity, "email_not_verified", nil)
		return ResolveOutcome{}, authn.ErrEmailNotVerified()
	}

	if len(idp.AllowedDomains) > 0 {
		if !domainAllowed(email, idp.AllowedDomains) {
			emitFail(ctx, w, idp, identity, "domain_not_allowed", nil)
			// Reuse invite_required: from the caller's perspective both
			// "no invite" and "wrong domain" mean "auto-provisioning
			// refused; ask the admin". Distinct codes would help an
			// attacker enumerate domains.
			return ResolveOutcome{}, authn.ErrInviteRequired()
		}
	}


	if displayName == "" {
		displayName = username
	}

	return runProvisionTx(ctx, pool, q, w, func(qtx ModesQueries, txAudit audit.Writer) (ResolveOutcome, error) {
		finalIdentity, err := qtx.GetAccountIdentityByIssuerSub(ctx, db.GetAccountIdentityByIssuerSubParams{
			UpstreamIss: identity.Issuer,
			UpstreamSub: identity.Subject,
		})
		switch {
		case err == nil:
			if finalIdentity.UpstreamIdpID != idp.ID {
				audit.RecordOrLog(ctx, w, audit.Record{
					AccountID: new(finalIdentity.AccountID),
					Factor:    audit.FactorFederationOIDC,
					Event:     audit.EventFail,
					Detail: map[string]any{
						"reason":           "idp_mismatch_relogin",
						"idp_slug":         idp.Slug,
						"bound_idp_id":     finalIdentity.UpstreamIdpID,
						"attempted_idp_id": idp.ID,
						"iss":              identity.Issuer,
						"sub":              identity.Subject,
					},
				})
				return ResolveOutcome{}, authn.ErrFederationStateInvalid()
			}
			if err := syncClaims(ctx, qtx, idp, &finalIdentity, identity); err != nil {
				if ae := authn.AsAuthError(err); ae != nil && ae.Code == "bad_credentials" {
					audit.RecordOrLog(ctx, w, audit.Record{
						AccountID: new(finalIdentity.AccountID),
						Factor:    audit.FactorFederationOIDC,
						Event:     audit.EventFail,
						Detail: map[string]any{
							"reason":   "account_disabled",
							"idp_slug": idp.Slug,
							"iss":      identity.Issuer,
							"sub":      identity.Subject,
						},
					})
				}
				return ResolveOutcome{}, err
			}
			confirmed := finalIdentity.ConfirmedAt.Valid
			if confirmed {
				audit.RecordOrLog(ctx, txAudit, audit.Record{
					AccountID: new(finalIdentity.AccountID),
					Factor:    audit.FactorFederationOIDC,
					Event:     audit.EventUse,
					Detail: map[string]any{
						"idp_slug": idp.Slug,
						"iss":      identity.Issuer,
						"sub":      identity.Subject,
					},
				})
			}
			return ResolveOutcome{
				AccountID: finalIdentity.AccountID, IdentityID: finalIdentity.ID,
				ProviderID: idp.ID, AMR: append([]string(nil), identity.AMR...),
				Confirmed: confirmed,
			}, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return ResolveOutcome{}, fmt.Errorf("federation: authoritative identity lookup: %w", err)
		}
		if username == "" {
			return ResolveOutcome{}, ErrLocalUsernameRequired
		}
		if err := acctpkg.ValidateUsername(username); err != nil {
			return ResolveOutcome{}, err
		}
		// Username collision check. We don't try to merge — admin must
		// resolve manually (rename, link, or reject). READ COMMITTED
		// means another tx can still claim the same username between
		// this check and our InsertAccount below; the 23505 mapping
		// after InsertAccount catches that race.
		if _, err := qtx.GetAccountByUsername(ctx, username); err == nil {
			// Failure audits MUST use the OUTER writer (w), not txAudit:
			// returning an error here rolls back the tx, and a tx-scoped
			// audit row would roll back with it — losing the forensic
			// record of the rejected sign-in. The outer writer commits on
			// its own pooled connection, independent of this rollback.
			emitFail(ctx, w, idp, identity, "username_collision", map[string]any{
				"username": username,
			})
			return ResolveOutcome{}, authn.ErrUsernameCollision()
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return ResolveOutcome{}, fmt.Errorf("federation/oidc: check username collision: %w", err)
		}

		handle, err := acctpkg.GenerateUserHandle()
		if err != nil {
			return ResolveOutcome{}, fmt.Errorf("federation/oidc: generate webauthn user handle: %w", err)
		}

		acct, err := qtx.InsertAccount(ctx, db.InsertAccountParams{
			Username:           username,
			DisplayName:        displayName,
			WebauthnUserHandle: handle,
			Role:               "user",
			Attributes:         []byte("{}"),
			Disabled:           false,
			// Seed the account email from the upstream (T3.2); email_verified
			// only when an address is present AND the OP asserted it verified.
			Email:         pgtype.Text{String: email, Valid: email != ""},
			EmailVerified: email != "" && identity.EmailVerified,
		})
		if err != nil {
			if isUniqueViolation(err) {
				// Lost the race against a concurrent same-username insert
				// (auto_provision callback, invite redemption, or a local
				// register). Emit a clean username_collision audit + AuthError
				// instead of a wrapped 500. Outer writer (w) so the audit
				// survives the tx rollback triggered by the error return.
				emitFail(ctx, w, idp, identity, "username_collision", map[string]any{
					"username": username,
				})
				return ResolveOutcome{}, authn.ErrUsernameCollision()
			}
			return ResolveOutcome{}, fmt.Errorf("federation/oidc: insert account: %w", err)
		}

		ident, err := qtx.InsertAccountIdentity(ctx, db.InsertAccountIdentityParams{
			AccountID:     acct.ID,
			UpstreamIdpID: idp.ID,
			UpstreamIss:   identity.Issuer,
			UpstreamSub:   identity.Subject,
			UpstreamEmail: pgtype.Text{String: email, Valid: email != ""},
		})
		if err != nil {
			if isUniqueViolation(err) {
				// Lost the race on (upstream_iss, upstream_sub) — another
				// concurrent callback for the same upstream identity bound
				// it to a different account between Resolve's lookup and
				// this insert. Collapse onto invite_required (same anti-
				// enumeration treatment as LinkCallback's link_conflict).
				// Tx rollback drops the just-inserted account row above; the
				// fail audit uses the outer writer (w) so it survives.
				emitFail(ctx, w, idp, identity, "identity_conflict", nil)
				return ResolveOutcome{}, authn.ErrInviteRequired()
			}
			return ResolveOutcome{}, fmt.Errorf("federation/oidc: insert account_identity: %w", err)
		}

		// Audit MUST be emitted via the tx-scoped Writer (txAudit) so the
		// credential_event.account_id FK to account.id resolves: the
		// outer-pool Writer would race the FK check against the
		// uncommitted account row (different connection, MVCC snapshot
		// doesn't yet see InsertAccount above) and fail silently. Same
		// invariant as applyInviteOnly.
		audit.RecordOrLog(ctx, txAudit, audit.Record{
			AccountID: new(acct.ID),
			Factor:    audit.FactorFederationOIDC,
			Event:     audit.EventRegister,
			Detail: map[string]any{
				"idp_slug": idp.Slug,
				"iss":      identity.Issuer,
				"sub":      identity.Subject,
				"mode":     ModeAutoProvision,
			},
		})
		audit.RecordOrLog(ctx, txAudit, audit.Record{
			AccountID: new(acct.ID),
			Factor:    audit.FactorFederationOIDC,
			Event:     audit.EventUse,
			Detail: map[string]any{
				"idp_slug": idp.Slug,
				"iss":      identity.Issuer,
				"sub":      identity.Subject,
			},
		})
		// PENDING by design: the inserted identity has confirmed_at=NULL, so the
		// HTTP layer routes to /welcome and issues no session until the user
		// confirms (Task 6). IdentityID is the row to confirm on YES.
		return ResolveOutcome{
			AccountID:  acct.ID,
			IdentityID: ident.ID,
			ProviderID: idp.ID,
			AMR:        append([]string(nil), identity.AMR...),
			IsNew:      true,
			Confirmed:  false,
		}, nil
	})
}

// applyInviteOnly implements the token-bearing invite redemption flow:
// the upstream OIDC dance proves the user controls the IdP identity, then
// the enrollment row is atomically consumed and a fresh local account is
// minted from the admin-supplied template — all inside a single
// transaction so a partial failure can never burn an invite without
// producing the corresponding account.
//
// pool is nil-safe: in tests the fake querier carries through with no
// transactional semantics (the call order is what's asserted); in
// production the pgxpool transaction provides the real atomicity guarantee.
//
// Skips require_verified_email + allowed_domains by design: the admin
// minted this invite specifically for this user, which IS the
// authorization decision. See the federation design spec D11 for rationale.
func applyInviteOnly(
	ctx context.Context,
	q ModesQueries,
	w audit.Writer,
	idp *db.UpstreamIdp,
	identity *VerifiedIdentity,
	enrollmentToken string,
	pool *pgxpool.Pool,
) (ResolveOutcome, error) {
	if enrollmentToken == "" {
		// Reached this branch via Resolve's mode-dispatch — i.e. an
		// invite_only IdP was hit without an invite. Reject and audit.
		emitFail(ctx, w, idp, identity, "invite_required_no_token", nil)
		return ResolveOutcome{}, authn.ErrInviteRequired()
	}

	return runProvisionTx(ctx, pool, q, w, func(qtx ModesQueries, txAudit audit.Writer) (ResolveOutcome, error) {
		// Atomic, intent-scoped consume — the UPDATE ... WHERE intent='invite'
		// AND consumed_at IS NULL AND expires_at > now() guarantees the row is a
		// redeemable INVITE at the instant we claim it. Restricting to
		// intent='invite' in SQL means a bootstrap/reset token can never be
		// marked consumed via this federation path, even if the begin-time intent
		// gate were ever bypassed (audit OIDCFED-2). Any "already consumed",
		// "expired", "wrong intent", or "token unknown" branch collapses onto
		// pgx.ErrNoRows.
		enr, err := qtx.ConsumeInviteEnrollment(ctx, enrollmentToken)
		if errors.Is(err, pgx.ErrNoRows) {
			emitFail(ctx, w, idp, identity, "invite_consumed_or_expired", nil)
			return ResolveOutcome{}, authn.ErrInviteRequired()
		}
		if err != nil {
			return ResolveOutcome{}, fmt.Errorf("federation/oidc: ConsumeInviteEnrollment: %w", err)
		}

		// Defense in depth: the invite must have been minted for THIS IdP.
		// Catches admin slug edits mid-flight, malformed FedState, etc.
		if !enr.ExpectedUpstreamIdpSlug.Valid || enr.ExpectedUpstreamIdpSlug.String != idp.Slug {
			emitFail(ctx, w, idp, identity, "invite_slug_mismatch", map[string]any{
				"enrollment_expected_slug": enr.ExpectedUpstreamIdpSlug.String,
			})
			return ResolveOutcome{}, authn.ErrInviteRequired()
		}

		// Belt-and-suspenders: the schema CHECK constraint at
		// db/migrations/001_initial.sql guarantees template_username NOT NULL
		// when intent='invite', but a missing template here means the schema
		// invariant was violated upstream — surface as a 500 so it gets seen.
		if !enr.TemplateUsername.Valid || enr.TemplateUsername.String == "" {
			return ResolveOutcome{}, fmt.Errorf("federation/oidc: invite missing template_username for token %q", enrollmentToken)
		}
		if !enr.TemplateRole.Valid || enr.TemplateRole.String == "" {
			return ResolveOutcome{}, fmt.Errorf("federation/oidc: invite missing template_role")
		}

		// Username collision is a technical constraint that's checked at
		// invite-create time (handle_invitations.go) but races are possible
		// (two invites for the same name; or a local password account took
		// the slot between mint and redemption). Detect and audit here.
		// Failure audits use the OUTER writer (w): the error return rolls
		// back the tx (un-doing ConsumeEnrollment so the invite stays
		// redeemable), and a tx-scoped audit row would roll back with it —
		// losing the forensic record. The outer writer commits independently.
		if _, err := qtx.GetAccountByUsername(ctx, enr.TemplateUsername.String); err == nil {
			emitFail(ctx, w, idp, identity, "username_collision", map[string]any{
				"username": enr.TemplateUsername.String,
			})
			return ResolveOutcome{}, authn.ErrUsernameCollision()
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return ResolveOutcome{}, fmt.Errorf("federation/oidc: check username collision: %w", err)
		}

		handle, err := acctpkg.GenerateUserHandle()
		if err != nil {
			return ResolveOutcome{}, fmt.Errorf("federation/oidc: generate webauthn user handle: %w", err)
		}

		attrs := []byte("{}")
		if len(enr.TemplateAttributes) > 0 {
			attrs = enr.TemplateAttributes
		}

		displayName := enr.TemplateDisplayName.String
		if displayName == "" {
			displayName = enr.TemplateUsername.String
		}

		email := identityEmail(identity)

		acct, err := qtx.InsertAccount(ctx, db.InsertAccountParams{
			Username:           enr.TemplateUsername.String,
			DisplayName:        displayName,
			WebauthnUserHandle: handle,
			Role:               enr.TemplateRole.String,
			Attributes:         attrs,
			Disabled:           false,
			Email:              pgtype.Text{String: email, Valid: email != ""},
			EmailVerified:      email != "" && identity.EmailVerified,
		})
		if err != nil {
			if isUniqueViolation(err) {
				// Lost the race against a concurrent insert for the same
				// username (another invite redemption, or a federated
				// auto_provision callback in flight). Map to a clean
				// ErrUsernameCollision + audit instead of a wrapped 500.
				// Tx rollback un-does ConsumeEnrollment, so the invite is
				// re-redeemable — matches the previous-step collision branch.
				// Outer writer (w) so the audit survives the rollback.
				emitFail(ctx, w, idp, identity, "username_collision", map[string]any{
					"username": enr.TemplateUsername.String,
				})
				return ResolveOutcome{}, authn.ErrUsernameCollision()
			}
			return ResolveOutcome{}, fmt.Errorf("federation/oidc: insert account: %w", err)
		}

		ident, err := qtx.InsertAccountIdentity(ctx, db.InsertAccountIdentityParams{
			AccountID:     acct.ID,
			UpstreamIdpID: idp.ID,
			UpstreamIss:   identity.Issuer,
			UpstreamSub:   identity.Subject,
			UpstreamEmail: pgtype.Text{String: email, Valid: email != ""},
		})
		if err != nil {
			if isUniqueViolation(err) {
				// (upstream_iss, upstream_sub) already bound to another
				// local account — same anti-enumeration treatment as
				// LinkCallback's link_conflict and applyAutoProvision's
				// identity_conflict. Tx rollback drops the just-inserted
				// account row and the ConsumeEnrollment, so the invite is
				// re-redeemable. Outer writer (w) so the audit survives.
				emitFail(ctx, w, idp, identity, "identity_conflict", nil)
				return ResolveOutcome{}, authn.ErrInviteRequired()
			}
			return ResolveOutcome{}, fmt.Errorf("federation/oidc: insert account_identity: %w", err)
		}

		// Auto-confirm the freshly-inserted identity IN-TX: the admin minted
		// this invite specifically for this user, which IS the authorization
		// decision (same rationale as skipping the D11 gates). A confirmed
		// identity issues a durable session immediately — no /welcome gate. The
		// confirm shares this tx so it rolls back atomically with the insert.
		if err := qtx.ConfirmAccountIdentity(ctx, ident.ID); err != nil {
			return ResolveOutcome{}, fmt.Errorf("federation/oidc: confirm invite identity: %w", err)
		}

		// Audit MUST be emitted via the tx-scoped Writer (txAudit) so the
		// credential_event.account_id FK to account.id resolves: the
		// outer-pool Writer would race the FK check against the
		// uncommitted account row (different connection, MVCC snapshot
		// doesn't yet see the InsertAccount above) and fail silently.
		// The original `_ = w.Record(...)` swallowed that FK error, which
		// surfaced as missing audit rows in the federation smoke (the
		// register-with-invite_only_redemption assertion). runInviteTx
		// hands us txAudit bound to the same tx as InsertAccount; on
		// rollback the audit rows revert too, which is the correct
		// semantic — no orphan audit pointing at non-existent accounts.
		audit.RecordOrLog(ctx, txAudit, audit.Record{
			AccountID: new(acct.ID),
			Factor:    audit.FactorFederationOIDC,
			Event:     audit.EventRegister,
			Detail: map[string]any{
				"idp_slug": idp.Slug,
				"iss":      identity.Issuer,
				"sub":      identity.Subject,
				"mode":     ModeInviteOnly,
				"reason":   "invite_only_redemption",
				"username": enr.TemplateUsername.String,
			},
		})
		audit.RecordOrLog(ctx, txAudit, audit.Record{
			AccountID: new(acct.ID),
			Factor:    audit.FactorFederationOIDC,
			Event:     audit.EventUse,
			Detail: map[string]any{
				"idp_slug": idp.Slug,
				"iss":      identity.Issuer,
				"sub":      identity.Subject,
			},
		})

		// Confirmed=true: the invite auto-confirmed above, so the HTTP layer
		// issues a session now.
		return ResolveOutcome{
			AccountID:  acct.ID,
			IdentityID: ident.ID,
			ProviderID: idp.ID,
			AMR:        append([]string(nil), identity.AMR...),
			IsNew:      true,
			Confirmed:  true,
		}, nil
	})
}

// runProvisionTx is the transactional wrapper shared by applyInviteOnly
// and applyAutoProvision (originally runInviteTx — both apply paths now
// reuse the same wrapper because the shape is identical: collision
// check + InsertAccount + InsertAccountIdentity + audit, all atomic).
// When pool is nil (tests), it just calls fn against the passed querier
// + the outer Writer — no transactional semantics, but the call order
// is preserved for assertion. When pool is non-nil (production), it
// opens a real pgxpool transaction, wraps it as a *db.Queries,
// constructs a tx-scoped audit.Writer that emits via the same tx, and
// commits only if fn returns nil. The tx-scoped audit writer is the
// load-bearing invariant — without it the credential_event FK to
// account.id races the uncommitted InsertAccount on a separate
// connection and silently fails (the audit Writer swallows errors by
// convention). See applyInviteOnly's audit-write site for the rationale.
func runProvisionTx(
	ctx context.Context,
	pool *pgxpool.Pool,
	q ModesQueries,
	w audit.Writer,
	fn func(qtx ModesQueries, txAudit audit.Writer) (ResolveOutcome, error),
) (ResolveOutcome, error) {
	if pool == nil {
		return fn(q, w)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return ResolveOutcome{}, fmt.Errorf("federation/oidc: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // safe after Commit — pgx returns ErrTxClosed which we ignore.

	qtx := db.New(tx)
	txAudit := audit.NewWriter(qtx)
	outcome, err := fn(qtx, txAudit)
	if err != nil {
		return ResolveOutcome{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return ResolveOutcome{}, fmt.Errorf("federation/oidc: commit tx: %w", err)
	}
	return outcome, nil
}

// applyLinkOnly rejects unknown identities under link_only mode. The
// existing-identity check happens in Resolve, so reaching this function
// already implies "no account_identity row" — the only remaining branch
// is reject.
func applyLinkOnly(
	ctx context.Context,
	w audit.Writer,
	idp *db.UpstreamIdp,
	identity *VerifiedIdentity,
) (ResolveOutcome, error) {
	emitFail(ctx, w, idp, identity, "link_required", nil)
	return ResolveOutcome{}, authn.ErrLinkRequired()
}

// syncClaims first verifies that the identity's owning account is enabled,
// then propagates upstream display_name and upstream_email drift into the local
// account / account_identity rows. Drift updates remain best-effort, but the
// account lookup is load-bearing: authentication must fail closed when account
// status cannot be established.
func syncClaims(
	ctx context.Context,
	q ModesQueries,
	idp *db.UpstreamIdp,
	stored *db.AccountIdentity,
	identity *VerifiedIdentity,
) error {
	displayName := identity.DisplayName
	email := identityEmail(identity)
	newEmail := pgtype.Text{String: email, Valid: email != ""}
	newVerified := newEmail.Valid && identity.EmailVerified

	acct, err := q.GetAccountByID(ctx, stored.AccountID)
	if err != nil {
		return fmt.Errorf("federation: lookup resolved account: %w", err)
	}
	if acct.Disabled {
		return authn.ErrBadCredentials()
	}
	if displayName != "" && acct.DisplayName != displayName {
		_ = q.UpdateAccountDisplayName(ctx, db.UpdateAccountDisplayNameParams{
			ID:          stored.AccountID,
			DisplayName: displayName,
		})
	}
	if acct.Email.String != newEmail.String || acct.Email.Valid != newEmail.Valid || acct.EmailVerified != newVerified {
		_ = q.UpdateAccountEmail(ctx, db.UpdateAccountEmailParams{
			ID:            stored.AccountID,
			Email:         newEmail,
			EmailVerified: newVerified,
		})
	}
	if newEmail.String != stored.UpstreamEmail.String || newEmail.Valid != stored.UpstreamEmail.Valid {
		_ = q.UpdateAccountIdentityEmail(ctx, db.UpdateAccountIdentityEmailParams{
			ID:            stored.ID,
			UpstreamEmail: newEmail,
		})
	}
	return nil
}

type ResolveContext struct {
	Intent          Intent
	EnrollmentToken string
	LocalUsername   string
	LinkAccountID   *int32
	// RequireLocalUsername marks a prepared local adapter action. When true,
	// auto-provisioning must not silently fall back to an upstream username if
	// the identity became unknown after its prepare-time lookup.
	RequireLocalUsername bool
}

type IdentityResolver interface {
	IdentityKnown(context.Context, IdentityKey) (bool, error)
	ResolveIdentity(context.Context, Provider, VerifiedIdentity, ResolveContext) (ResolveOutcome, error)
}

type Resolver struct {
	queries ModesQueries
	audit   audit.Writer
	pool    *pgxpool.Pool
}

func NewResolver(queries ModesQueries, writer audit.Writer, pool *pgxpool.Pool) *Resolver {
	return &Resolver{queries: queries, audit: writer, pool: pool}
}

func (r *Resolver) IdentityKnown(ctx context.Context, key IdentityKey) (bool, error) {
	if key.Issuer == "" || key.Subject == "" {
		return false, errors.New("federation: incomplete identity key")
	}
	_, err := r.queries.GetAccountIdentityByIssuerSub(ctx, db.GetAccountIdentityByIssuerSubParams{
		UpstreamIss: key.Issuer,
		UpstreamSub: key.Subject,
	})
	if err == nil {
		return true, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return false, fmt.Errorf("federation: identity lookup: %w", err)
}

func (r *Resolver) ResolveIdentity(ctx context.Context, provider Provider, identity VerifiedIdentity, resolution ResolveContext) (ResolveOutcome, error) {
	if identity.Issuer == "" || identity.Subject == "" {
		return ResolveOutcome{}, errors.New("federation: verified identity missing issuer or subject")
	}
	row, err := providerRow(provider)
	if err != nil {
		return ResolveOutcome{}, err
	}
	switch resolution.Intent {
	case IntentLink:
		if resolution.LinkAccountID == nil || *resolution.LinkAccountID <= 0 {
			return ResolveOutcome{}, authn.ErrFederationStateInvalid()
		}
		return r.resolveLink(ctx, &row, &identity, *resolution.LinkAccountID)
	case IntentInvite:
		return applyInviteOnly(ctx, r.queries, r.audit, &row, &identity, resolution.EnrollmentToken, r.pool)
	case IntentLogin:
		username := resolution.LocalUsername
		if username == "" && !resolution.RequireLocalUsername {
			username = identity.Username
		}
		known, err := r.IdentityKnown(ctx, IdentityKey{Issuer: identity.Issuer, Subject: identity.Subject})
		if err != nil {
			return ResolveOutcome{}, err
		}
		if known {
			return resolve(ctx, r.queries, r.audit, &row, &identity, username, r.pool)
		}
		switch row.Mode {
		case ModeAutoProvision:
			return applyAutoProvision(ctx, r.queries, r.audit, &row, &identity, username, r.pool)
		case ModeInviteOnly:
			return applyInviteOnly(ctx, r.queries, r.audit, &row, &identity, "", nil)
		case ModeLinkOnly:
			return applyLinkOnly(ctx, r.audit, &row, &identity)
		default:
			return ResolveOutcome{}, fmt.Errorf("federation: unknown provider mode %q", row.Mode)
		}
	default:
		return ResolveOutcome{}, authn.ErrFederationStateInvalid()
	}
}

func (r *Resolver) resolveLink(ctx context.Context, provider *db.UpstreamIdp, identity *VerifiedIdentity, accountID int32) (ResolveOutcome, error) {
	email := identityEmail(identity)
	if provider.RequireVerifiedEmail && identity.EmailVerificationSupported && !identity.EmailVerified {
		return ResolveOutcome{}, NewFailure(FailureEmailNotVerified, map[string]any{"upstream_iss": identity.Issuer})
	}
	if len(provider.AllowedDomains) > 0 && !domainAllowed(email, provider.AllowedDomains) {
		return ResolveOutcome{}, NewFailure(FailureDomainNotAllowed, nil)
	}
	return runProvisionTx(ctx, r.pool, r.queries, r.audit, func(qtx ModesQueries, txAudit audit.Writer) (ResolveOutcome, error) {
		_, err := qtx.GetAccountIdentityByIssuerSub(ctx, db.GetAccountIdentityByIssuerSubParams{
			UpstreamIss: identity.Issuer,
			UpstreamSub: identity.Subject,
		})
		switch {
		case err == nil:
			return ResolveOutcome{}, NewFailure(FailureLinkConflict, map[string]any{
				"iss": identity.Issuer, "sub": identity.Subject,
			})
		case !errors.Is(err, pgx.ErrNoRows):
			return ResolveOutcome{}, NewFailure(FailureLinkInsert, map[string]any{
				"iss": identity.Issuer, "sub": identity.Subject,
			})
		}
		account, err := qtx.GetAccountByID(ctx, accountID)
		if err != nil || account.Disabled {
			return ResolveOutcome{}, NewFailure(FailureLinkInsert, map[string]any{
				"iss": identity.Issuer, "sub": identity.Subject,
			})
		}
		linked, err := qtx.InsertAccountIdentity(ctx, db.InsertAccountIdentityParams{
			AccountID: accountID, UpstreamIdpID: provider.ID,
			UpstreamIss: identity.Issuer, UpstreamSub: identity.Subject,
			UpstreamEmail: pgtype.Text{String: email, Valid: email != ""},
		})
		if err != nil {
			reason := FailureLinkInsert
			if isUniqueViolation(err) {
				reason = FailureLinkConflict
			}
			return ResolveOutcome{}, NewFailure(reason, map[string]any{
				"iss": identity.Issuer, "sub": identity.Subject,
			})
		}
		if err := qtx.ConfirmAccountIdentity(ctx, linked.ID); err != nil {
			return ResolveOutcome{}, NewFailure(FailureLinkInsert, map[string]any{
				"iss": identity.Issuer, "sub": identity.Subject,
			})
		}
		audit.RecordOrLog(ctx, txAudit, audit.Record{
			AccountID: new(accountID), Factor: audit.FactorFederationOIDC, Event: audit.EventLink,
			Detail: map[string]any{"idp_slug": provider.Slug, "upstream_iss": identity.Issuer, "upstream_sub": identity.Subject},
		})
		return ResolveOutcome{
			AccountID: accountID, IdentityID: linked.ID, ProviderID: provider.ID,
			AMR: append([]string(nil), identity.AMR...), Confirmed: true,
		}, nil
	})
}


// domainAllowed splits on the last @, lowercases the domain part, and
// checks membership in allowed (case-insensitive). Empty email returns
// false — callers should not call this with an empty email.
func domainAllowed(email string, allowed []string) bool {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 {
		return false
	}
	dom := strings.ToLower(email[at+1:])
	for _, a := range allowed {
		if strings.ToLower(a) == dom {
			return true
		}
	}
	return false
}

// emitFail emits a single EventFail audit row with a structured reason
// code. No AccountID — at the point of failure there is no local
// account context yet (or the failure is pre-provision).
func emitFail(
	ctx context.Context,
	w audit.Writer,
	idp *db.UpstreamIdp,
	identity *VerifiedIdentity,
	reason string,
	extra map[string]any,
) {
	detail := map[string]any{
		"idp_slug": idp.Slug,
		"iss":      identity.Issuer,
		"sub":      identity.Subject,
		"mode":     idp.Mode,
		"reason":   reason,
	}
	for k, v := range extra {
		detail[k] = v
	}
	audit.RecordOrLog(ctx, w, audit.Record{
		Factor: audit.FactorFederationOIDC,
		Event:  audit.EventFail,
		Detail: detail,
	})
}

func identityEmail(identity *VerifiedIdentity) string {
	if identity == nil || identity.Email == nil {
		return ""
	}
	return *identity.Email
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
