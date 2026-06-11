package oidc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	acctpkg "prohibitorum/pkg/account"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/db"
)

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
	UpdateAccountDisplayName(ctx context.Context, arg db.UpdateAccountDisplayNameParams) error
	UpdateAccountEmail(ctx context.Context, arg db.UpdateAccountEmailParams) error
	UpdateAccountIdentityEmail(ctx context.Context, arg db.UpdateAccountIdentityEmailParams) error
	ConsumeEnrollment(ctx context.Context, token string) (db.Enrollment, error)
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
	tokens *Tokens,
	pool *pgxpool.Pool,
) (accountID int32, isNew bool, err error) {
	if idp == nil || tokens == nil {
		return 0, false, errors.New("federation/oidc: Resolve: nil idp or tokens")
	}

	existing, err := q.GetAccountIdentityByIssuerSub(ctx, db.GetAccountIdentityByIssuerSubParams{
		UpstreamIss: tokens.Issuer,
		UpstreamSub: tokens.Subject,
	})
	switch {
	case err == nil:
		// Scope re-login to the IdP that minted the identity: one issuer = one
		// upstream_idp row. If the (iss,sub) identity is bound to a DIFFERENT row
		// — an admin configured two rows sharing an issuer_url — refuse to
		// re-login through this one, else a user provisioned via IdP-A could log
		// in against IdP-B's slug and dodge B's provisioning gates. T4.3c.
		if existing.UpstreamIdpID != idp.ID {
			_ = w.Record(ctx, audit.Record{
				AccountID: int32Ptr(existing.AccountID),
				Factor:    audit.FactorFederationOIDC,
				Event:     audit.EventFail,
				Detail: map[string]any{
					"reason":           "idp_mismatch_relogin",
					"idp_slug":         idp.Slug,
					"bound_idp_id":     existing.UpstreamIdpID,
					"attempted_idp_id": idp.ID,
					"iss":              tokens.Issuer,
					"sub":              tokens.Subject,
				},
			})
			return 0, false, authn.ErrFederationStateInvalid()
		}
		// Re-login path. Sync claim drift, audit Use, return existing account.
		syncClaims(ctx, q, idp, &existing, tokens)
		_ = w.Record(ctx, audit.Record{
			AccountID: int32Ptr(existing.AccountID),
			Factor:    audit.FactorFederationOIDC,
			Event:     audit.EventUse,
			Detail: map[string]any{
				"idp_slug": idp.Slug,
				"iss":      tokens.Issuer,
				"sub":      tokens.Subject,
			},
		})
		return existing.AccountID, false, nil
	case errors.Is(err, pgx.ErrNoRows):
		// Fall through to mode dispatch.
	default:
		return 0, false, fmt.Errorf("federation/oidc: lookup identity: %w", err)
	}

	switch idp.Mode {
	case ModeAutoProvision:
		return applyAutoProvision(ctx, q, w, idp, tokens, pool)
	case ModeInviteOnly:
		// Reaching invite_only via Resolve means HandleCallback did NOT see an
		// EnrollmentToken on the FedState — i.e. someone hit /federation/{slug}/login
		// directly on an invite_only IdP. Dispatch into applyInviteOnly with an
		// empty token; the top of that function audits invite_required_no_token
		// and rejects. Pool is nil — no DB writes happen on the rejection path.
		return applyInviteOnly(ctx, q, w, idp, tokens, "", nil)
	case ModeLinkOnly:
		return applyLinkOnly(ctx, w, idp, tokens)
	default:
		return 0, false, fmt.Errorf("federation/oidc: unknown idp.mode %q", idp.Mode)
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
	tokens *Tokens,
	pool *pgxpool.Pool,
) (int32, bool, error) {
	// Per-IdP claim-name overrides (schema defaults: preferred_username/name/email).
	// Admins can point these at non-OIDC-default keys (e.g. Entra ID's "upn")
	// without code changes. Read once at the top so the same values are used
	// for collision check, allowlist check, and the inserts below.
	username := ClaimString(tokens.Raw, idp.UsernameClaim)
	displayName := ClaimString(tokens.Raw, idp.DisplayNameClaim)
	email := ClaimString(tokens.Raw, idp.EmailClaim)

	// GATES — pure read-only / claim-only checks. Run outside the tx
	// because they don't touch the DB and would otherwise widen the
	// transaction window for no benefit.
	if idp.RequireVerifiedEmail && !tokens.EmailVerified {
		// EmailVerified is the typed bool; no override (it's a JWT
		// standard claim with a fixed boolean shape).
		emitFail(ctx, w, idp, tokens, "email_not_verified", nil)
		return 0, false, authn.ErrEmailNotVerified()
	}

	if len(idp.AllowedDomains) > 0 {
		if !domainAllowed(email, idp.AllowedDomains) {
			emitFail(ctx, w, idp, tokens, "domain_not_allowed", nil)
			// Reuse invite_required: from the caller's perspective both
			// "no invite" and "wrong domain" mean "auto-provisioning
			// refused; ask the admin". Distinct codes would help an
			// attacker enumerate domains.
			return 0, false, authn.ErrInviteRequired()
		}
	}

	if username == "" {
		// Config bug, not a user error: the IdP's username_claim mapping
		// is wrong or the OP doesn't ship the expected claim. Surface
		// as a 500 so operators see it in logs.
		return 0, false, fmt.Errorf("federation/oidc: upstream provided no %q claim (idp=%q, sub=%q)", idp.UsernameClaim, idp.Slug, tokens.Subject)
	}

	if displayName == "" {
		displayName = username
	}

	return runProvisionTx(ctx, pool, q, w, func(qtx ModesQueries, txAudit audit.Writer) (int32, bool, error) {
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
			emitFail(ctx, w, idp, tokens, "username_collision", map[string]any{
				"username": username,
			})
			return 0, false, authn.ErrUsernameCollision()
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return 0, false, fmt.Errorf("federation/oidc: check username collision: %w", err)
		}

		handle, err := acctpkg.GenerateUserHandle()
		if err != nil {
			return 0, false, fmt.Errorf("federation/oidc: generate webauthn user handle: %w", err)
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
			EmailVerified: email != "" && tokens.EmailVerified,
		})
		if err != nil {
			if isUniqueViolation(err) {
				// Lost the race against a concurrent same-username insert
				// (auto_provision callback, invite redemption, or a local
				// register). Emit a clean username_collision audit + AuthError
				// instead of a wrapped 500. Outer writer (w) so the audit
				// survives the tx rollback triggered by the error return.
				emitFail(ctx, w, idp, tokens, "username_collision", map[string]any{
					"username": username,
				})
				return 0, false, authn.ErrUsernameCollision()
			}
			return 0, false, fmt.Errorf("federation/oidc: insert account: %w", err)
		}

		_, err = qtx.InsertAccountIdentity(ctx, db.InsertAccountIdentityParams{
			AccountID:     acct.ID,
			UpstreamIdpID: idp.ID,
			UpstreamIss:   tokens.Issuer,
			UpstreamSub:   tokens.Subject,
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
				emitFail(ctx, w, idp, tokens, "identity_conflict", nil)
				return 0, false, authn.ErrInviteRequired()
			}
			return 0, false, fmt.Errorf("federation/oidc: insert account_identity: %w", err)
		}

		// Audit MUST be emitted via the tx-scoped Writer (txAudit) so the
		// credential_event.account_id FK to account.id resolves: the
		// outer-pool Writer would race the FK check against the
		// uncommitted account row (different connection, MVCC snapshot
		// doesn't yet see InsertAccount above) and fail silently. Same
		// invariant as applyInviteOnly.
		_ = txAudit.Record(ctx, audit.Record{
			AccountID: int32Ptr(acct.ID),
			Factor:    audit.FactorFederationOIDC,
			Event:     audit.EventRegister,
			Detail: map[string]any{
				"idp_slug": idp.Slug,
				"iss":      tokens.Issuer,
				"sub":      tokens.Subject,
				"mode":     ModeAutoProvision,
			},
		})
		_ = txAudit.Record(ctx, audit.Record{
			AccountID: int32Ptr(acct.ID),
			Factor:    audit.FactorFederationOIDC,
			Event:     audit.EventUse,
			Detail: map[string]any{
				"idp_slug": idp.Slug,
				"iss":      tokens.Issuer,
				"sub":      tokens.Subject,
			},
		})
		return acct.ID, true, nil
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
// authorization decision. See the v0.3 design spec D11 for rationale.
func applyInviteOnly(
	ctx context.Context,
	q ModesQueries,
	w audit.Writer,
	idp *db.UpstreamIdp,
	tokens *Tokens,
	enrollmentToken string,
	pool *pgxpool.Pool,
) (int32, bool, error) {
	if enrollmentToken == "" {
		// Reached this branch via Resolve's mode-dispatch — i.e. an
		// invite_only IdP was hit without an invite. Reject and audit.
		emitFail(ctx, w, idp, tokens, "invite_required_no_token", nil)
		return 0, false, authn.ErrInviteRequired()
	}

	return runProvisionTx(ctx, pool, q, w, func(qtx ModesQueries, txAudit audit.Writer) (int32, bool, error) {
		// Atomic consume — the UPDATE ... WHERE consumed_at IS NULL AND
		// expires_at > now() guarantees the row is in a redeemable state at
		// the instant we claim it. Any "already consumed", "expired", or
		// "token unknown" branch collapses onto pgx.ErrNoRows.
		enr, err := qtx.ConsumeEnrollment(ctx, enrollmentToken)
		if errors.Is(err, pgx.ErrNoRows) {
			emitFail(ctx, w, idp, tokens, "invite_consumed_or_expired", nil)
			return 0, false, authn.ErrInviteRequired()
		}
		if err != nil {
			return 0, false, fmt.Errorf("federation/oidc: ConsumeEnrollment: %w", err)
		}

		// Defense in depth: the invite must have been minted for THIS IdP.
		// Catches admin slug edits mid-flight, malformed FedState, etc.
		if !enr.ExpectedUpstreamIdpSlug.Valid || enr.ExpectedUpstreamIdpSlug.String != idp.Slug {
			emitFail(ctx, w, idp, tokens, "invite_slug_mismatch", map[string]any{
				"enrollment_expected_slug": enr.ExpectedUpstreamIdpSlug.String,
			})
			return 0, false, authn.ErrInviteRequired()
		}

		// Belt-and-suspenders: the schema CHECK constraint at
		// db/migrations/001_initial.sql guarantees template_username NOT NULL
		// when intent='invite', but a missing template here means the schema
		// invariant was violated upstream — surface as a 500 so it gets seen.
		if !enr.TemplateUsername.Valid || enr.TemplateUsername.String == "" {
			return 0, false, fmt.Errorf("federation/oidc: invite missing template_username for token %q", enrollmentToken)
		}
		if !enr.TemplateRole.Valid || enr.TemplateRole.String == "" {
			return 0, false, fmt.Errorf("federation/oidc: invite missing template_role")
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
			emitFail(ctx, w, idp, tokens, "username_collision", map[string]any{
				"username": enr.TemplateUsername.String,
			})
			return 0, false, authn.ErrUsernameCollision()
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return 0, false, fmt.Errorf("federation/oidc: check username collision: %w", err)
		}

		handle, err := acctpkg.GenerateUserHandle()
		if err != nil {
			return 0, false, fmt.Errorf("federation/oidc: generate webauthn user handle: %w", err)
		}

		attrs := []byte("{}")
		if len(enr.TemplateAttributes) > 0 {
			attrs = enr.TemplateAttributes
		}

		displayName := enr.TemplateDisplayName.String
		if displayName == "" {
			displayName = enr.TemplateUsername.String
		}

		// Honor the per-IdP email_claim override (matches applyAutoProvision);
		// used for both the account email (T3.2) and account_identity below.
		email := ClaimString(tokens.Raw, idp.EmailClaim)

		acct, err := qtx.InsertAccount(ctx, db.InsertAccountParams{
			Username:           enr.TemplateUsername.String,
			DisplayName:        displayName,
			WebauthnUserHandle: handle,
			Role:               enr.TemplateRole.String,
			Attributes:         attrs,
			Disabled:           false,
			Email:              pgtype.Text{String: email, Valid: email != ""},
			EmailVerified:      email != "" && tokens.EmailVerified,
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
				emitFail(ctx, w, idp, tokens, "username_collision", map[string]any{
					"username": enr.TemplateUsername.String,
				})
				return 0, false, authn.ErrUsernameCollision()
			}
			return 0, false, fmt.Errorf("federation/oidc: insert account: %w", err)
		}

		_, err = qtx.InsertAccountIdentity(ctx, db.InsertAccountIdentityParams{
			AccountID:     acct.ID,
			UpstreamIdpID: idp.ID,
			UpstreamIss:   tokens.Issuer,
			UpstreamSub:   tokens.Subject,
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
				emitFail(ctx, w, idp, tokens, "identity_conflict", nil)
				return 0, false, authn.ErrInviteRequired()
			}
			return 0, false, fmt.Errorf("federation/oidc: insert account_identity: %w", err)
		}

		// Audit MUST be emitted via the tx-scoped Writer (txAudit) so the
		// credential_event.account_id FK to account.id resolves: the
		// outer-pool Writer would race the FK check against the
		// uncommitted account row (different connection, MVCC snapshot
		// doesn't yet see the InsertAccount above) and fail silently.
		// The original `_ = w.Record(...)` swallowed that FK error, which
		// surfaced as missing audit rows in the v0.3 smoke (step 66
		// register-with-invite_only_redemption assertion). runInviteTx
		// hands us txAudit bound to the same tx as InsertAccount; on
		// rollback the audit rows revert too, which is the correct
		// semantic — no orphan audit pointing at non-existent accounts.
		_ = txAudit.Record(ctx, audit.Record{
			AccountID: int32Ptr(acct.ID),
			Factor:    audit.FactorFederationOIDC,
			Event:     audit.EventRegister,
			Detail: map[string]any{
				"idp_slug": idp.Slug,
				"iss":      tokens.Issuer,
				"sub":      tokens.Subject,
				"mode":     ModeInviteOnly,
				"reason":   "invite_only_redemption",
				"username": enr.TemplateUsername.String,
			},
		})
		_ = txAudit.Record(ctx, audit.Record{
			AccountID: int32Ptr(acct.ID),
			Factor:    audit.FactorFederationOIDC,
			Event:     audit.EventUse,
			Detail: map[string]any{
				"idp_slug": idp.Slug,
				"iss":      tokens.Issuer,
				"sub":      tokens.Subject,
			},
		})

		return acct.ID, true, nil
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
	fn func(qtx ModesQueries, txAudit audit.Writer) (int32, bool, error),
) (int32, bool, error) {
	if pool == nil {
		return fn(q, w)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("federation/oidc: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // safe after Commit — pgx returns ErrTxClosed which we ignore.

	qtx := db.New(tx)
	txAudit := audit.NewWriter(qtx)
	accountID, isNew, err := fn(qtx, txAudit)
	if err != nil {
		return 0, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, false, fmt.Errorf("federation/oidc: commit tx: %w", err)
	}
	return accountID, isNew, nil
}

// applyLinkOnly rejects unknown identities under link_only mode. The
// existing-identity check happens in Resolve, so reaching this function
// already implies "no account_identity row" — the only remaining branch
// is reject.
func applyLinkOnly(
	ctx context.Context,
	w audit.Writer,
	idp *db.UpstreamIdp,
	tokens *Tokens,
) (int32, bool, error) {
	emitFail(ctx, w, idp, tokens, "link_required", nil)
	return 0, false, authn.ErrLinkRequired()
}

// syncClaims propagates upstream display_name and upstream_email drift
// into the local account / account_identity rows. Errors are non-fatal:
// the user has already authenticated, and a transient DB hiccup on a
// best-effort sync should not deny the session. Writes are conditional
// on a diff against the current row — avoids burning row-level write
// amplification on no-op updates and (incidentally) avoids touching
// updated_at when nothing actually changed.
func syncClaims(
	ctx context.Context,
	q ModesQueries,
	idp *db.UpstreamIdp,
	identity *db.AccountIdentity,
	tokens *Tokens,
) {
	// Honor per-IdP claim-name overrides on the drift-sync path too,
	// otherwise an Entra-style OP's existing user would re-login and see
	// their display_name reset to "" (because tokens.Name is the typed
	// OIDC "name" claim, which Entra doesn't ship).
	displayName := ClaimString(tokens.Raw, idp.DisplayNameClaim)
	email := ClaimString(tokens.Raw, idp.EmailClaim)
	newEmail := pgtype.Text{String: email, Valid: email != ""}
	newVerified := newEmail.Valid && tokens.EmailVerified

	// Fetch the current account once (cheap PK lookup) for both the display_name
	// and email drift checks — conditional UPDATEs avoid firing the updated_at
	// trigger on a no-op login.
	if acct, err := q.GetAccountByID(ctx, identity.AccountID); err == nil {
		if displayName != "" && acct.DisplayName != displayName {
			_ = q.UpdateAccountDisplayName(ctx, db.UpdateAccountDisplayNameParams{
				ID:          identity.AccountID,
				DisplayName: displayName,
			})
		}
		// account.email drift (T3.2): keep the account email + verified flag in
		// lockstep with the upstream on re-login, mirroring the upstream_email
		// sync below.
		if acct.Email.String != newEmail.String || acct.Email.Valid != newEmail.Valid || acct.EmailVerified != newVerified {
			_ = q.UpdateAccountEmail(ctx, db.UpdateAccountEmailParams{
				ID:            identity.AccountID,
				Email:         newEmail,
				EmailVerified: newVerified,
			})
		}
	}

	if newEmail.String != identity.UpstreamEmail.String || newEmail.Valid != identity.UpstreamEmail.Valid {
		_ = q.UpdateAccountIdentityEmail(ctx, db.UpdateAccountIdentityEmailParams{
			ID:            identity.ID,
			UpstreamEmail: newEmail,
		})
	}
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
	tokens *Tokens,
	reason string,
	extra map[string]any,
) {
	detail := map[string]any{
		"idp_slug": idp.Slug,
		"iss":      tokens.Issuer,
		"sub":      tokens.Subject,
		"mode":     idp.Mode,
		"reason":   reason,
	}
	for k, v := range extra {
		detail[k] = v
	}
	_ = w.Record(ctx, audit.Record{
		Factor: audit.FactorFederationOIDC,
		Event:  audit.EventFail,
		Detail: detail,
	})
}

func int32Ptr(v int32) *int32 { return &v }
