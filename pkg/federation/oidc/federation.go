// Package oidc — federation.go
//
// Federator is the orchestration layer that composes the RP client (client.go),
// secret crypto (secret.go), KV state (state.go), and mode policies (modes.go)
// into the four public flows the HTTP layer drives:
//
//  1. BeginLogin   — generate PKCE+state+nonce, snapshot expected_iss, stash
//     in KV under LoginKey(token), return the upstream /authorize URL.
//  2. HandleCallback — pop+validate state, exchange code, dispatch to Resolve,
//     post-resolve disabled-account check, return a CallbackResult the HTTP
//     layer mints into a real session.
//  3. LinkBegin    — same as BeginLogin but stashes under LinkKey(token) with
//     the current account ID bound into FedState.
//  4. LinkCallback — same as HandleCallback up through code-exchange, then
//     INSERTs an account_identity row directly (no Resolve — Resolve provisions
//     new accounts; link binds to an existing one). Includes a session-swap
//     check against the bound account ID.
//
// Security boundaries enforced here:
//
//   - State KV is single-use via Pop (state.go's MemoryStore.Pop is atomic).
//   - RFC 9207 iss callback parameter is validated against state.ExpectedIss.
//   - Mix-up resistance: state.ExpectedIss is also fed into client.Exchange,
//     which double-checks the id_token issuer claim.
//   - Session-swap defense for link flows: current_account_id from the dashboard
//     session must match the account_id captured at LinkBegin time.
//   - All failure modes return a single generic ErrFederationStateInvalid to
//     prevent state-probing side channels; structured reasons live in audit only.
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

// ErrUnknownIDP is returned by BeginLogin / LinkBegin when the slug doesn't
// resolve to a (non-disabled) upstream_idp row. Exported so the HTTP layer
// can map it to 404.
var ErrUnknownIDP = errors.New("federation: unknown IdP")

// LoginRequest is the BeginLogin / LinkBegin return value: the URL the
// browser should be redirected to, plus the opaque state token the caller
// MUST also stash in a short-lived browser cookie (Task 7 owns the cookie).
type LoginRequest struct {
	AuthorizeURL string
	StateKey     string
}

// CallbackResult is the HandleCallback return value: enough information for
// the HTTP layer to mint a real session and audit the login.
type CallbackResult struct {
	AccountID int32
	AMR       []string
	ReturnTo  string
	IsNew     bool
	IDPID     int64
	IDPSlug   string
}

// LinkResult is the LinkCallback return value: enough for the HTTP layer
// to redirect back to the link-management page with a success flash.
type LinkResult struct {
	ReturnTo string
	IDPSlug  string
}

// FederatorQueries is the DB surface the Federator needs. Wider than
// ModesQueries (which Resolve uses) because the Federator additionally
// looks up upstream_idp rows by slug at callback time and inserts
// account_identity rows directly for the link flow. ListAccountIdentities
// is included so server wiring can reuse this interface for the /me/identities
// handlers (Task 8); the Federator itself does not call it.
type FederatorQueries interface {
	ModesQueries

	GetUpstreamIDPBySlug(ctx context.Context, slug string) (db.UpstreamIdp, error)
	ListAccountIdentitiesByAccount(ctx context.Context, accountID int32) ([]db.ListAccountIdentitiesByAccountRow, error)
}

// Federator orchestrates upstream OIDC federation. Constructed once at
// server startup and reused for every flow.
type Federator struct {
	q            FederatorQueries
	kvStore      kv.Store
	audit        audit.Writer
	cfg          configx.FederationConfig
	deks         map[int][]byte
	publicOrigin string
}

// NewFederator constructs a Federator from its collaborators. publicOrigin is
// the scheme+host the federator uses to build redirect_uris (e.g.
// "https://idp.example.com"); callers should pass cfg.PublicOrigins[0] when
// PublicOrigins is the multi-origin slice from configx.Config.
func NewFederator(
	q FederatorQueries,
	kvStore kv.Store,
	aud audit.Writer,
	cfg configx.FederationConfig,
	deks map[int][]byte,
	publicOrigin string,
) *Federator {
	return &Federator{
		q:            q,
		kvStore:      kvStore,
		audit:        aud,
		cfg:          cfg,
		deks:         deks,
		publicOrigin: publicOrigin,
	}
}

// BeginLogin starts a federated login flow. Caller is the unauthenticated
// /api/prohibitorum/auth/federation/{slug}/start handler (Task 7).
func (f *Federator) BeginLogin(ctx context.Context, idpSlug, returnTo string) (*LoginRequest, error) {
	return f.begin(ctx, idpSlug, returnTo, nil)
}

// LinkBegin starts a link flow for an already-authenticated account. The
// FedState carries accountID so LinkCallback can refuse to bind the upstream
// identity to a different account if the session changes mid-flow.
func (f *Federator) LinkBegin(ctx context.Context, accountID int32, idpSlug, returnTo string) (*LoginRequest, error) {
	id := accountID
	return f.begin(ctx, idpSlug, returnTo, &id)
}

// begin is the shared BeginLogin / LinkBegin body. linkingAccountID==nil
// signals login flow (state stashed under LoginKey); non-nil signals link
// flow (LinkKey + redirect URI built from the link-callback template).
func (f *Federator) begin(ctx context.Context, idpSlug, returnTo string, linkingAccountID *int32) (*LoginRequest, error) {
	idp, err := f.q.GetUpstreamIDPBySlug(ctx, idpSlug)
	if err != nil {
		// Collapse "no row", "disabled", and "DB error" into one code.
		// The slug came from a URL path; a missing slug is a 404 from
		// the HTTP layer's perspective.
		return nil, ErrUnknownIDP
	}

	secret, err := f.decryptSecret(&idp)
	if err != nil {
		return nil, err
	}

	redirectURI := f.redirectURI(idpSlug, linkingAccountID != nil)

	client, err := NewClient(
		ctx,
		idp.ClientID,
		string(secret),
		redirectURI,
		idp.Scopes,
		idp.IssuerUrl,
		DefaultAllowedAlgs(),
	)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: build RP client: %w", err)
	}

	stateToken, err := randB64URL(32)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: state token: %w", err)
	}
	verifier, err := randB64URL(32)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: pkce verifier: %w", err)
	}
	nonce, err := randB64URL(16)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: nonce: %w", err)
	}
	challenge := pkceS256(verifier)

	state := FedState{
		IDPID:                 idp.ID,
		IDPSlug:               idp.Slug,
		ExpectedIss:           client.Issuer(),
		ExpectedTokenEndpoint: client.TokenEndpoint(),
		Nonce:                 nonce,
		CodeVerifier:          verifier,
		ReturnTo:              returnTo,
		LinkingAccountID:      linkingAccountID,
	}
	blob, err := state.Encode()
	if err != nil {
		return nil, err
	}

	key := LoginKey(stateToken)
	if linkingAccountID != nil {
		key = LinkKey(stateToken)
	}
	if err := f.kvStore.SetEx(ctx, key, blob, f.cfg.StateTTL); err != nil {
		return nil, fmt.Errorf("federation/oidc: stash state: %w", err)
	}

	return &LoginRequest{
		AuthorizeURL: client.AuthURL(stateToken, nonce, challenge),
		StateKey:     stateToken,
	}, nil
}

// HandleCallback consumes the login-flow KV state, exchanges the code, and
// dispatches to Resolve. Returns ErrFederationStateInvalid for nearly every
// failure mode — the audit log carries the structured reason.
func (f *Federator) HandleCallback(ctx context.Context, stateToken, code, issParam string) (*CallbackResult, error) {
	blob, err := f.kvStore.Pop(ctx, LoginKey(stateToken))
	if err != nil {
		f.failNoAccount(ctx, "", "state_invalid", nil)
		return nil, authn.ErrFederationStateInvalid()
	}
	state, err := DecodeFedState(blob)
	if err != nil {
		f.failNoAccount(ctx, "", "state_invalid", nil)
		return nil, authn.ErrFederationStateInvalid()
	}

	if issParam != "" && issParam != state.ExpectedIss {
		f.failNoAccount(ctx, state.IDPSlug, "iss_mismatch_callback", map[string]any{
			"expected_iss": state.ExpectedIss,
			"got_iss":      issParam,
		})
		return nil, authn.ErrFederationStateInvalid()
	}

	idp, err := f.q.GetUpstreamIDPBySlug(ctx, state.IDPSlug)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: re-lookup idp %q: %w", state.IDPSlug, err)
	}

	client, err := f.buildClient(ctx, &idp, false)
	if err != nil {
		return nil, err
	}

	tokens, err := client.Exchange(ctx, code, state.CodeVerifier, state.ExpectedIss, state.Nonce)
	if err != nil {
		f.failNoAccount(ctx, state.IDPSlug, "code_exchange_failed", map[string]any{
			"err": err.Error(),
		})
		return nil, authn.ErrFederationStateInvalid()
	}

	accountID, isNew, err := Resolve(ctx, f.q, f.audit, &idp, tokens)
	if err != nil {
		// Resolve already audited its own failure with structured reason
		// (see modes.go). Propagate as-is so the HTTP layer can map the
		// *authn.AuthError to the right status code.
		return nil, err
	}

	acct, err := f.q.GetAccountByID(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: re-fetch account %d: %w", accountID, err)
	}
	if acct.Disabled {
		// Username-enumeration symmetry with the password flow: same code
		// (bad_credentials) whether the username doesn't exist, the
		// password is wrong, or — here — the upstream-resolved account
		// is disabled.
		f.failWithAccount(ctx, accountID, state.IDPSlug, "account_disabled", map[string]any{
			"iss": tokens.Issuer,
			"sub": tokens.Subject,
		})
		return nil, authn.ErrBadCredentials()
	}

	return &CallbackResult{
		AccountID: accountID,
		AMR:       tokens.AMR,
		ReturnTo:  state.ReturnTo,
		IsNew:     isNew,
		IDPID:     idp.ID,
		IDPSlug:   idp.Slug,
	}, nil
}

// LinkCallback consumes the link-flow KV state, exchanges the code, and
// INSERTs an account_identity row binding the upstream identity to
// currentAccountID. Refuses if the bound account in FedState differs from
// currentAccountID (session-swap defense).
func (f *Federator) LinkCallback(ctx context.Context, stateToken, code, issParam string, currentAccountID int32) (*LinkResult, error) {
	blob, err := f.kvStore.Pop(ctx, LinkKey(stateToken))
	if err != nil {
		f.failWithAccount(ctx, currentAccountID, "", "state_invalid", nil)
		return nil, authn.ErrFederationStateInvalid()
	}
	state, err := DecodeFedState(blob)
	if err != nil {
		f.failWithAccount(ctx, currentAccountID, "", "state_invalid", nil)
		return nil, authn.ErrFederationStateInvalid()
	}

	if state.LinkingAccountID == nil || *state.LinkingAccountID != currentAccountID {
		// The state was minted for a different account (or no account at
		// all — i.e., someone fed a login-flow token to the link path).
		// Refuse and audit: this is the canonical session-swap attack.
		f.failWithAccount(ctx, currentAccountID, state.IDPSlug, "session_swap", map[string]any{
			"state_account_id": linkingAccountVal(state.LinkingAccountID),
		})
		return nil, authn.ErrFederationStateInvalid()
	}

	if issParam != "" && issParam != state.ExpectedIss {
		f.failWithAccount(ctx, currentAccountID, state.IDPSlug, "iss_mismatch_callback", map[string]any{
			"expected_iss": state.ExpectedIss,
			"got_iss":      issParam,
		})
		return nil, authn.ErrFederationStateInvalid()
	}

	idp, err := f.q.GetUpstreamIDPBySlug(ctx, state.IDPSlug)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: re-lookup idp %q: %w", state.IDPSlug, err)
	}

	client, err := f.buildClient(ctx, &idp, true)
	if err != nil {
		return nil, err
	}

	tokens, err := client.Exchange(ctx, code, state.CodeVerifier, state.ExpectedIss, state.Nonce)
	if err != nil {
		f.failWithAccount(ctx, currentAccountID, state.IDPSlug, "code_exchange_failed", map[string]any{
			"err": err.Error(),
		})
		return nil, authn.ErrFederationStateInvalid()
	}

	// Self-service link is neither invite_only nor link_only — it's an
	// authenticated user binding a fresh upstream identity. Apply the same
	// gates auto_provision enforces so admin policy (require_verified_email,
	// allowed_domains) can't be bypassed through this surface.
	if idp.RequireVerifiedEmail && !tokens.EmailVerified {
		f.failWithAccount(ctx, currentAccountID, state.IDPSlug, "email_not_verified", map[string]any{
			"upstream_iss": tokens.Issuer,
		})
		return nil, authn.ErrEmailNotVerified()
	}

	if len(idp.AllowedDomains) > 0 && !domainAllowed(tokens.Email, idp.AllowedDomains) {
		// Collapse onto invite_required to match applyAutoProvision's
		// anti-enumeration behavior; the real reason lives in the audit row.
		f.failWithAccount(ctx, currentAccountID, state.IDPSlug, "domain_not_allowed", nil)
		return nil, authn.ErrInviteRequired()
	}

	_, err = f.q.InsertAccountIdentity(ctx, db.InsertAccountIdentityParams{
		AccountID:     currentAccountID,
		UpstreamIdpID: idp.ID,
		UpstreamIss:   tokens.Issuer,
		UpstreamSub:   tokens.Subject,
		UpstreamEmail: pgtype.Text{String: tokens.Email, Valid: tokens.Email != ""},
	})
	if err != nil {
		// Most-likely cause: (upstream_iss, upstream_sub) is already
		// bound to ANOTHER local account. Surface as a generic state
		// failure — revealing "this identity belongs to user X" is an
		// account-existence enumeration channel. The audit log has the
		// (iss, sub) for the operator.
		reason := "link_insert_failed"
		if isUniqueViolation(err) {
			reason = "link_conflict"
		}
		f.failWithAccount(ctx, currentAccountID, state.IDPSlug, reason, map[string]any{
			"iss": tokens.Issuer,
			"sub": tokens.Subject,
		})
		return nil, authn.ErrFederationStateInvalid()
	}

	id := currentAccountID
	_ = f.audit.Record(ctx, audit.Record{
		AccountID: &id,
		Factor:    audit.FactorFederationOIDC,
		Event:     audit.EventLink,
		Detail: map[string]any{
			"idp_slug":     idp.Slug,
			"upstream_iss": tokens.Issuer,
			"upstream_sub": tokens.Subject,
		},
	})

	return &LinkResult{
		ReturnTo: state.ReturnTo,
		IDPSlug:  idp.Slug,
	}, nil
}

// --- internal helpers ------------------------------------------------------

// decryptSecret unwraps idp.client_secret_enc using the DEK for
// idp.key_version. Missing version is wrapped (not a sentinel) — admins
// rotate DEKs in config, so this is a config-time mistake, not a runtime
// security boundary that callers should branch on.
func (f *Federator) decryptSecret(idp *db.UpstreamIdp) ([]byte, error) {
	dek, ok := f.deks[int(idp.KeyVersion)]
	if !ok {
		return nil, fmt.Errorf("federation/oidc: missing DEK version %d (idp=%q)", idp.KeyVersion, idp.Slug)
	}
	plain, err := DecryptClientSecret(dek, idp.ClientSecretEnc, idp.SecretNonce, idp.ID, idp.KeyVersion)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: decrypt client_secret (idp=%q): %w", idp.Slug, err)
	}
	return plain, nil
}

// buildClient does the decrypt+NewClient dance shared by HandleCallback and
// LinkCallback. isLink picks the link- vs login-flavored redirect URI so the
// token endpoint sees the exact value the OP recorded at /authorize.
func (f *Federator) buildClient(ctx context.Context, idp *db.UpstreamIdp, isLink bool) (*Client, error) {
	secret, err := f.decryptSecret(idp)
	if err != nil {
		return nil, err
	}
	redirectURI := f.redirectURI(idp.Slug, isLink)
	client, err := NewClient(
		ctx,
		idp.ClientID,
		string(secret),
		redirectURI,
		idp.Scopes,
		idp.IssuerUrl,
		DefaultAllowedAlgs(),
	)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: build RP client: %w", err)
	}
	return client, nil
}

// redirectURI builds the upstream-facing redirect_uri for {slug}, picking
// the login or link callback template. Must produce identical strings at
// BeginLogin and the matching callback handler — the upstream OP records
// the value and compares it byte-for-byte at code-exchange time.
func (f *Federator) redirectURI(slug string, isLink bool) string {
	template := "/api/prohibitorum/auth/federation/{slug}/callback"
	if isLink {
		template = "/api/prohibitorum/me/identities/link/{slug}/callback"
	}
	return strings.TrimRight(f.publicOrigin, "/") + replaceSlug(template, slug)
}

func replaceSlug(template, slug string) string {
	return strings.ReplaceAll(template, "{slug}", slug)
}

// failNoAccount emits an audit EventFail with no AccountID (used pre-resolve).
func (f *Federator) failNoAccount(ctx context.Context, idpSlug, reason string, extra map[string]any) {
	detail := map[string]any{"reason": reason}
	if idpSlug != "" {
		detail["idp_slug"] = idpSlug
	}
	for k, v := range extra {
		detail[k] = v
	}
	_ = f.audit.Record(ctx, audit.Record{
		Factor: audit.FactorFederationOIDC,
		Event:  audit.EventFail,
		Detail: detail,
	})
}

// failWithAccount emits an audit EventFail with the given account_id (used
// for link-flow failures where we know which account the session belongs to,
// and for the post-resolve disabled-account check).
func (f *Federator) failWithAccount(ctx context.Context, accountID int32, idpSlug, reason string, extra map[string]any) {
	id := accountID
	detail := map[string]any{"reason": reason}
	if idpSlug != "" {
		detail["idp_slug"] = idpSlug
	}
	for k, v := range extra {
		detail[k] = v
	}
	_ = f.audit.Record(ctx, audit.Record{
		AccountID: &id,
		Factor:    audit.FactorFederationOIDC,
		Event:     audit.EventFail,
		Detail:    detail,
	})
}

func linkingAccountVal(p *int32) any {
	if p == nil {
		return nil
	}
	return *p
}

// randB64URL returns n random bytes encoded as base64url without padding.
// n=32 → 43 chars (state, PKCE verifier); n=16 → 22 chars (nonce).
func randB64URL(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// pkceS256 is the RFC 7636 §4.2 S256 transform: base64url(SHA-256(verifier)).
func pkceS256(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// isUniqueViolation reports whether err wraps a Postgres SQLSTATE 23505
// (unique_violation). Duplicated from pkg/server/pgerr.go to keep this
// package's dependencies inside pkg/federation/oidc + pkg/db + pkg/audit
// + pkg/authn + pkg/kv + pkg/configx — no inbound dep on pkg/server.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
