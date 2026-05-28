package oidc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

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
	UpdateAccountIdentityEmail(ctx context.Context, arg db.UpdateAccountIdentityEmailParams) error
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
		return applyAutoProvision(ctx, q, w, idp, tokens)
	case ModeInviteOnly:
		return applyInviteOnly(ctx, w, idp, tokens)
	case ModeLinkOnly:
		return applyLinkOnly(ctx, w, idp, tokens)
	default:
		return 0, false, fmt.Errorf("federation/oidc: unknown idp.mode %q", idp.Mode)
	}
}

// applyAutoProvision creates a fresh local account from the upstream
// claims, gated by email_verified, allowed_domains, preferred_username
// presence, and a local username-collision check.
func applyAutoProvision(
	ctx context.Context,
	q ModesQueries,
	w audit.Writer,
	idp *db.UpstreamIdp,
	tokens *Tokens,
) (int32, bool, error) {
	// Per-IdP claim-name overrides (schema defaults: preferred_username/name/email).
	// Admins can point these at non-OIDC-default keys (e.g. Entra ID's "upn")
	// without code changes. Read once at the top so the same values are used
	// for collision check, allowlist check, and the inserts below.
	username := ClaimString(tokens.Raw, idp.UsernameClaim)
	displayName := ClaimString(tokens.Raw, idp.DisplayNameClaim)
	email := ClaimString(tokens.Raw, idp.EmailClaim)

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

	// Username collision check. We don't try to merge — admin must
	// resolve manually (rename, link, or reject).
	if _, err := q.GetAccountByUsername(ctx, username); err == nil {
		emitFail(ctx, w, idp, tokens, "username_collision", map[string]any{
			"username": username,
		})
		return 0, false, authn.ErrUsernameCollision()
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, fmt.Errorf("federation/oidc: check username collision: %w", err)
	}

	if displayName == "" {
		displayName = username
	}

	handle, err := acctpkg.GenerateUserHandle()
	if err != nil {
		return 0, false, fmt.Errorf("federation/oidc: generate webauthn user handle: %w", err)
	}

	acct, err := q.InsertAccount(ctx, db.InsertAccountParams{
		Username:           username,
		DisplayName:        displayName,
		WebauthnUserHandle: handle,
		Role:               "user",
		Attributes:         []byte("{}"),
		Disabled:           false,
	})
	if err != nil {
		return 0, false, fmt.Errorf("federation/oidc: insert account: %w", err)
	}

	_, err = q.InsertAccountIdentity(ctx, db.InsertAccountIdentityParams{
		AccountID:     acct.ID,
		UpstreamIdpID: idp.ID,
		UpstreamIss:   tokens.Issuer,
		UpstreamSub:   tokens.Subject,
		UpstreamEmail: pgtype.Text{String: email, Valid: email != ""},
	})
	if err != nil {
		return 0, false, fmt.Errorf("federation/oidc: insert account_identity: %w", err)
	}

	_ = w.Record(ctx, audit.Record{
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
	_ = w.Record(ctx, audit.Record{
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
}

// applyInviteOnly is a STUB until the v0.3-followup invite_only design
// lands. The actual flow has open questions about how an admin issues
// invites for an upstream IdP, how the invite is consumed mid-callback,
// and how (iss, sub) is pre-bound to the enrollment row.
//
// TODO(invite-only-followup): replace this stub with the real flow.
// See docs/superpowers/notes/2026-05-29-followups-invite-only-federation.md
// for the open design questions and the proposed shape of the solution.
func applyInviteOnly(
	ctx context.Context,
	w audit.Writer,
	idp *db.UpstreamIdp,
	tokens *Tokens,
) (int32, bool, error) {
	emitFail(ctx, w, idp, tokens, "invite_only_not_implemented", nil)
	return 0, false, authn.ErrInviteRequired()
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

	if displayName != "" {
		// Compare against current display_name. Lookup is cheap (1 row by PK)
		// and cheaper than a redundant UPDATE that fires the updated_at
		// trigger every login.
		if acct, err := q.GetAccountByID(ctx, identity.AccountID); err == nil {
			if acct.DisplayName != displayName {
				_ = q.UpdateAccountDisplayName(ctx, db.UpdateAccountDisplayNameParams{
					ID:          identity.AccountID,
					DisplayName: displayName,
				})
			}
		}
	}

	newEmail := pgtype.Text{String: email, Valid: email != ""}
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
