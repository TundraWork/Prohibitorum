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
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/avatar"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
)

// clientCacheTTL bounds how long a constructed *Client (and its discovered
// endpoint snapshot) may be reused before we re-run OIDC discovery. Without
// this cache, every BeginLogin / HandleCallback / LinkBegin / LinkCallback /
// BeginInviteRedemption would issue an upstream GET to
// <issuer>/.well-known/openid-configuration — one request to us amplifies
// into one request upstream, and steady-state federation latency carries an
// extra round-trip. Spec D8 (v0.3 upstream OIDC federation design) accepts
// a fixed 15-minute window; if an admin edits client_id/scopes/issuer_url
// without bumping key_version, the cache serves stale config until expiry.
// Audit finding H2-sch.
const clientCacheTTL = 15 * time.Minute

// fedFlow distinguishes the three federation flows that each need their own
// redirect_uri (and therefore their own cached *Client): login, link, and the
// sudo step-up. The flow is threaded through buildClient → redirectURI so the
// value baked into the *Client matches the route the HTTP layer registered.
type fedFlow int

const (
	flowLogin fedFlow = iota
	flowLink
	flowSudo
)

// label maps a fedFlow to the token used in the client-cache key, so login,
// link, and sudo clients cache under distinct keys.
func (f fedFlow) label() string {
	switch f {
	case flowLink:
		return "link"
	case flowSudo:
		return "sudo"
	default:
		return "login"
	}
}

// cachedClient pairs a built *Client with the wall-clock expiry beyond which
// it must be rebuilt. Stored as a value in Federator.clientCache.
type cachedClient struct {
	client    *Client
	expiresAt time.Time
}

// ErrUnknownIDP is returned by BeginLogin / LinkBegin when the slug doesn't
// resolve to a (non-disabled) upstream_idp row. Exported so the HTTP layer
// can map it to 404.
var ErrUnknownIDP = errors.New("federation: unknown IdP")

// LoginRequest is the BeginLogin / LinkBegin return value: the URL the
// browser should be redirected to, the opaque state token, and the
// anti-forgery token the HTTP layer MUST set as a short-lived browser cookie
// and present back at the callback (audit follow-up N4).
type LoginRequest struct {
	AuthorizeURL string
	StateKey     string
	// AntiForgeryToken is the raw value the HTTP layer sets as the federation
	// state cookie. Its SHA-256 is stored in FedState.BrowserBinding;
	// HandleCallback compares the callback request's cookie against it. The
	// HTTP handler decides whether to set the cookie (login + invite flows do;
	// the link flow ignores it).
	AntiForgeryToken string
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
	// Confirmed reports whether the resolved account_identity is confirmed.
	// Confirmed=false routes the HTTP layer to the /welcome confirmation gate
	// (no session yet); Confirmed=true means issue a durable session now.
	Confirmed bool
	// IdentityID is the account_identity row to confirm when the user accepts
	// at the /welcome gate (only meaningful when Confirmed=false).
	IdentityID int64
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
	GetEnrollmentByToken(ctx context.Context, token string) (db.Enrollment, error)

	UpsertAccountAvatarBytes(ctx context.Context, arg db.UpsertAccountAvatarBytesParams) error
	SetAccountAvatarMetaUpstream(ctx context.Context, arg db.SetAccountAvatarMetaUpstreamParams) error
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
	// dbPool is nil-safe for tests; production wires the connection pool so
	// invite redemption (applyInviteOnly) can run ConsumeEnrollment +
	// InsertAccount + InsertAccountIdentity inside a single transaction. A
	// nil pool degrades gracefully — runInviteTx just passes the existing
	// querier through and the call order is what tests assert.
	dbPool *pgxpool.Pool

	// clientCache memoizes *Client instances keyed by
	// slug + ":" + key_version + ":flow=" + flow.label(). sync.Map fits the
	// read-heavy access pattern (one entry per (idp, flow) pair, populated
	// once per TTL window, hit on every subsequent request). Values are
	// *cachedClient. Eviction happens lazily on access — admins configure
	// few upstream_idp rows, so the map stays bounded without a sweeper.
	clientCache sync.Map

	// clientCacheTTL defaults to the package-level clientCacheTTL constant;
	// tests override via export_test.go to exercise expiry behavior.
	clientCacheTTL time.Duration

	// allowPrivateNetwork disables the outbound client's dial-time internal-IP
	// SSRF screen. Sourced from cfg.AllowPrivateNetwork (default false). Set true
	// only when the upstream IdP is on a trusted private/internal network.
	allowPrivateNetwork bool

	// avatarFetch resolves an upstream picture URL to its raw bytes (SSRF-
	// screened, https-only, image/* only, size-capped). Defaults to
	// fetchUpstreamAvatar; the test seam (export_test.go) swaps it for a stub so
	// the avatar-inherit job can run without a live image server.
	avatarFetch func(ctx context.Context, url string, allowPrivate bool) ([]byte, error)
}

// NewFederator constructs a Federator from its collaborators. publicOrigin is
// the scheme+host the federator uses to build redirect_uris (e.g.
// "https://idp.example.com"); callers should pass cfg.PublicOrigins[0] when
// PublicOrigins is the multi-origin slice from configx.Config. dbPool is
// nil-safe in tests; production wires the pool for transactional invite
// redemption.
func NewFederator(
	q FederatorQueries,
	kvStore kv.Store,
	aud audit.Writer,
	cfg configx.FederationConfig,
	deks map[int][]byte,
	dbPool *pgxpool.Pool,
	publicOrigin string,
) *Federator {
	return &Federator{
		q:                   q,
		kvStore:             kvStore,
		audit:               aud,
		cfg:                 cfg,
		deks:                deks,
		publicOrigin:        publicOrigin,
		dbPool:              dbPool,
		clientCacheTTL:      clientCacheTTL,
		allowPrivateNetwork: cfg.AllowPrivateNetwork,
		avatarFetch:         fetchUpstreamAvatar,
	}
}

// BeginLogin starts a federated login flow. Caller is the unauthenticated
// /api/prohibitorum/auth/federation/{slug}/start handler (Task 7).
func (f *Federator) BeginLogin(ctx context.Context, idpSlug, returnTo string) (*LoginRequest, error) {
	return f.begin(ctx, idpSlug, returnTo, nil, "", nil, "")
}

// LinkBegin starts a link flow for an already-authenticated account. The
// FedState carries accountID so LinkCallback can refuse to bind the upstream
// identity to a different account if the session changes mid-flow.
func (f *Federator) LinkBegin(ctx context.Context, accountID int32, idpSlug, returnTo string) (*LoginRequest, error) {
	id := accountID
	return f.begin(ctx, idpSlug, returnTo, &id, "", nil, "")
}

// SudoBegin starts a forced-re-auth federation flow for the sudo step-up. The
// account must already have a linked identity at an enabled idpSlug; the
// callback (Task 3) requires the re-auth to resolve back to that same subject
// (ExpectedSub), so re-authenticating as a different upstream user can't
// satisfy the gate. Unlike LinkBegin, the sudo flow carries the anti-forgery
// browser-binding cookie (linkingAccountID stays nil), and the authorize URL
// forces a fresh credential prompt via StepUpAuthOptions (prompt=login,
// max_age=0).
func (f *Federator) SudoBegin(ctx context.Context, accountID int32, idpSlug, returnTo string) (*LoginRequest, error) {
	if _, err := f.q.GetUpstreamIDPBySlug(ctx, idpSlug); err != nil {
		// Collapse "no row", "disabled", and "DB error" — the query excludes
		// disabled rows, so a disabled provider takes the same not-found path.
		return nil, ErrUnknownIDP
	}
	idents, err := f.q.ListAccountIdentitiesByAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: list identities: %w", err)
	}
	var expectedSub string
	for _, id := range idents {
		if id.IdpSlug == idpSlug {
			expectedSub = id.UpstreamSub
			break
		}
	}
	if expectedSub == "" {
		// Account has no linked identity at this provider — there's no subject
		// to re-verify against, so this method is unavailable for the caller.
		return nil, authn.ErrSudoMethodUnavailable()
	}
	acct := accountID
	return f.begin(ctx, idpSlug, returnTo, nil, "", &acct, expectedSub)
}

// BeginInviteRedemption starts an invite-bound federated login flow. The
// invite token is validated (intent=invite, unconsumed, unexpired, slug
// bound), then the upstream IdP referenced by enrollment.expected_upstream_idp_slug
// is loaded and an authorize URL minted. The token rides in FedState so
// the callback can dispatch to applyInviteOnly atomically.
//
// Returns ErrInviteRequired (NOT ErrUnknownIDP) for every "invite isn't
// redeemable" branch — don't leak whether the token was malformed,
// expired, or already used.
func (f *Federator) BeginInviteRedemption(ctx context.Context, token, returnTo string) (*LoginRequest, error) {
	enr, err := f.q.GetEnrollmentByToken(ctx, token)
	if err != nil {
		// pgx.ErrNoRows or DB error — both collapse onto invite_required
		// from the caller's perspective.
		f.failNoAccount(ctx, "", "invite_lookup_failed", nil)
		return nil, authn.ErrInviteRequired()
	}
	if enr.Intent != "invite" {
		f.failNoAccount(ctx, "", "invite_wrong_intent", map[string]any{"intent": enr.Intent})
		return nil, authn.ErrInviteRequired()
	}
	if enr.ConsumedAt.Valid {
		f.failNoAccount(ctx, "", "invite_already_consumed", nil)
		return nil, authn.ErrInviteRequired()
	}
	if !enr.ExpiresAt.Valid || !enr.ExpiresAt.Time.After(time.Now()) {
		f.failNoAccount(ctx, "", "invite_expired", nil)
		return nil, authn.ErrInviteRequired()
	}
	if !enr.ExpectedUpstreamIdpSlug.Valid || enr.ExpectedUpstreamIdpSlug.String == "" {
		// Non-federated invite (intent=invite but no upstream IdP bound)
		// belongs to the WebAuthn enrollment flow, not the federation flow.
		f.failNoAccount(ctx, "", "invite_not_federated", nil)
		return nil, authn.ErrInviteRequired()
	}

	return f.begin(ctx, enr.ExpectedUpstreamIdpSlug.String, returnTo, nil, token, nil, "")
}

// begin is the shared BeginLogin / LinkBegin / BeginInviteRedemption /
// SudoBegin body.
// linkingAccountID!=nil signals link flow (LinkKey + link-callback template);
// enrollmentToken!="" signals invite flow (LoginKey, EnrollmentToken stashed
// in FedState for HandleCallback to dispatch on); sudoAccountID!=nil signals
// sudo step-up (SudoKey, SudoAccountID+ExpectedSub stashed, and the authorize
// URL forces a fresh credential prompt via StepUpAuthOptions). These signals
// are mutually exclusive — invite flow never has a current account, and sudo
// keeps linkingAccountID nil so it still earns the browser-binding cookie.
func (f *Federator) begin(ctx context.Context, idpSlug, returnTo string, linkingAccountID *int32, enrollmentToken string, sudoAccountID *int32, expectedSub string) (*LoginRequest, error) {
	idp, err := f.q.GetUpstreamIDPBySlug(ctx, idpSlug)
	if err != nil {
		// Collapse "no row", "disabled", and "DB error" into one code.
		// The slug came from a URL path; a missing slug is a 404 from
		// the HTTP layer's perspective.
		return nil, ErrUnknownIDP
	}

	flow := flowLogin
	switch {
	case sudoAccountID != nil:
		flow = flowSudo
	case linkingAccountID != nil:
		flow = flowLink
	}
	client, err := f.buildClient(ctx, &idp, flow)
	if err != nil {
		return nil, err
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

	// Anti-forgery token bound to the initiating browser (N4). The raw value
	// rides in a short-lived cookie set by the HTTP layer; only its hash is
	// persisted in KV, so a KV read never yields the cookie value. This binding
	// applies to the login + invite flows, whose HTTP handlers set the cookie and
	// whose callback re-checks it. The LINK flow (linkingAccountID != nil) is
	// already gated by the authenticated dashboard session + a LinkingAccountID
	// match and never carries this cookie, so leave the binding EMPTY rather than
	// persist a value no code path can satisfy (audit OIDCFED-1; matches the
	// FedState.BrowserBinding doc comment).
	var antiForgery, browserBinding string
	if linkingAccountID == nil {
		antiForgery, err = randB64URL(32)
		if err != nil {
			return nil, fmt.Errorf("federation/oidc: anti-forgery token: %w", err)
		}
		browserBinding = hashAntiForgery(antiForgery)
	}

	state := FedState{
		IDPID:                 idp.ID,
		IDPSlug:               idp.Slug,
		ExpectedIss:           client.Issuer(),
		ExpectedTokenEndpoint: client.TokenEndpoint(),
		Nonce:                 nonce,
		CodeVerifier:          verifier,
		ReturnTo:              returnTo,
		LinkingAccountID:      linkingAccountID,
		EnrollmentToken:       enrollmentToken,
		SudoAccountID:         sudoAccountID,
		ExpectedSub:           expectedSub,
		BrowserBinding:        browserBinding,
	}
	blob, err := state.Encode()
	if err != nil {
		return nil, err
	}

	key := LoginKey(stateToken)
	if linkingAccountID != nil {
		key = LinkKey(stateToken)
	}
	if sudoAccountID != nil {
		key = SudoKey(stateToken)
	}
	if err := f.kvStore.SetEx(ctx, key, blob, f.cfg.StateTTL); err != nil {
		return nil, fmt.Errorf("federation/oidc: stash state: %w", err)
	}

	// The sudo step-up forces a fresh credential prompt at the OP so a stale
	// upstream session can't silently satisfy the gate (prompt=login,
	// max_age=0). Other flows use the OP's normal session behavior.
	authorizeURL := client.AuthURL(stateToken, nonce, challenge)
	if sudoAccountID != nil {
		authorizeURL = client.AuthURL(stateToken, nonce, challenge, StepUpAuthOptions()...)
	}

	return &LoginRequest{
		AuthorizeURL:     authorizeURL,
		StateKey:         stateToken,
		AntiForgeryToken: antiForgery,
	}, nil
}

// HandleCallback consumes the login-flow KV state, exchanges the code, and
// dispatches to Resolve. Returns ErrFederationStateInvalid for nearly every
// failure mode — the audit log carries the structured reason.
//
// browserToken is the value of the anti-forgery cookie the HTTP layer set at
// BeginLogin time. When the stashed state carries a BrowserBinding, the cookie
// MUST hash to it — this binds the flow to the initiating browser and defeats
// login-CSRF / session-fixation (N4). An empty BrowserBinding (e.g. a flow that
// pre-dates the binding) skips the check.
func (f *Federator) HandleCallback(ctx context.Context, stateToken, code, issParam, browserToken string) (*CallbackResult, error) {
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

	if !browserBindingOK(state.BrowserBinding, browserToken) {
		f.failNoAccount(ctx, state.IDPSlug, "browser_binding_mismatch", nil)
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
		// The IdP was disabled or deleted between BeginLogin and here
		// (GetUpstreamIDPBySlug filters WHERE NOT disabled). Collapse to the
		// generic state-invalid code + audit, exactly as begin() does — never a
		// wrapped 500 that leaks an internal error (T3.1).
		f.failNoAccount(ctx, state.IDPSlug, "idp_disabled_or_deleted", nil)
		return nil, authn.ErrFederationStateInvalid()
	}

	client, err := f.buildClient(ctx, &idp, flowLogin)
	if err != nil {
		return nil, err
	}

	// RFC 9700 §4.4.2.1 mix-up defense: the discovery doc may have been
	// edited (or swapped, in an attack scenario) between BeginLogin and
	// here. ExpectedIss is already snapshotted and re-checked in
	// client.Exchange; the token_endpoint must match the same snapshot
	// or we risk sending the code to a different OP than the user
	// authenticated to. Audit finding H3-sch.
	if client.TokenEndpoint() != state.ExpectedTokenEndpoint {
		f.failNoAccount(ctx, state.IDPSlug, "token_endpoint_drift", map[string]any{
			"expected": state.ExpectedTokenEndpoint,
			"got":      client.TokenEndpoint(),
		})
		return nil, authn.ErrFederationStateInvalid()
	}

	tokens, err := client.Exchange(ctx, code, state.CodeVerifier, state.ExpectedIss, state.Nonce)
	if err != nil {
		f.failNoAccount(ctx, state.IDPSlug, "code_exchange_failed", map[string]any{
			"err": err.Error(),
		})
		return nil, authn.ErrFederationStateInvalid()
	}

	// Mode-decoupled dispatch: an EnrollmentToken on the FedState means the
	// user clicked an invite link, regardless of the IdP's configured mode.
	// An auto_provision IdP can still accept invite-bound users; the
	// invite IS the authorization, so D11 gates are skipped inside
	// applyInviteOnly. When there's no token, fall through to mode-based
	// Resolve dispatch.
	var outcome ResolveOutcome
	if state.EnrollmentToken != "" {
		outcome, err = applyInviteOnly(ctx, f.q, f.audit, &idp, tokens, state.EnrollmentToken, f.dbPool)
	} else {
		outcome, err = Resolve(ctx, f.q, f.audit, &idp, tokens, f.dbPool)
	}
	if err != nil {
		// Resolve / applyInviteOnly already audited its own failure with
		// structured reason (see modes.go). Propagate as-is so the HTTP
		// layer can map the *authn.AuthError to the right status code.
		return nil, err
	}

	acct, err := f.q.GetAccountByID(ctx, outcome.AccountID)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: re-fetch account %d: %w", outcome.AccountID, err)
	}
	if acct.Disabled {
		// Username-enumeration symmetry with the password flow: same code
		// (bad_credentials) whether the username doesn't exist, the
		// password is wrong, or — here — the upstream-resolved account
		// is disabled.
		f.failWithAccount(ctx, outcome.AccountID, state.IDPSlug, "account_disabled", map[string]any{
			"iss": tokens.Issuer,
			"sub": tokens.Subject,
		})
		return nil, authn.ErrBadCredentials()
	}

	// Kick off the upstream avatar inherit for BOTH confirmed and unconfirmed
	// outcomes: when the identity is unconfirmed the HTTP layer parks the user on
	// /welcome, and we want the avatar fetched (so /welcome can preview it) by the
	// time they accept. kickoffAvatarInherit no-ops when the user owns their
	// avatar and dedupes concurrent runs via SetNX; the goroutine is detached.
	f.kickoffAvatarInherit(ctx, client, idp, tokens, outcome.AccountID)

	return &CallbackResult{
		AccountID:  outcome.AccountID,
		AMR:        tokens.AMR,
		ReturnTo:   state.ReturnTo,
		IsNew:      outcome.IsNew,
		IDPID:      idp.ID,
		IDPSlug:    idp.Slug,
		Confirmed:  outcome.Confirmed,
		IdentityID: outcome.IdentityID,
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
		// IdP disabled/deleted mid-link-flow → clean state-invalid + audit,
		// not a wrapped 500 (T3.1).
		f.failWithAccount(ctx, currentAccountID, state.IDPSlug, "idp_disabled_or_deleted", nil)
		return nil, authn.ErrFederationStateInvalid()
	}

	client, err := f.buildClient(ctx, &idp, flowLink)
	if err != nil {
		return nil, err
	}

	// RFC 9700 §4.4.2.1 mix-up defense: same check as HandleCallback —
	// the snapshotted token_endpoint must still match. Audit finding H3-sch.
	if client.TokenEndpoint() != state.ExpectedTokenEndpoint {
		f.failWithAccount(ctx, currentAccountID, state.IDPSlug, "token_endpoint_drift", map[string]any{
			"expected": state.ExpectedTokenEndpoint,
			"got":      client.TokenEndpoint(),
		})
		return nil, authn.ErrFederationStateInvalid()
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

	// Honor the per-IdP email_claim override (schema default "email"). The
	// same extraction is exercised on the auto_provision path via the shared
	// ClaimString helper — see modes_test.go for the override-key coverage.
	// Reading once at the top keeps the allowlist check and the stored
	// UpstreamEmail in lockstep: an admin who sets email_claim="mail" wants
	// BOTH gates to use the "mail" value.
	email := ClaimString(tokens.Raw, idp.EmailClaim)

	if len(idp.AllowedDomains) > 0 && !domainAllowed(email, idp.AllowedDomains) {
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
		UpstreamEmail: pgtype.Text{String: email, Valid: email != ""},
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

// maxStepUpAuthAge bounds how recent the upstream's id_token auth_time must be
// for a sudo step-up to count. The federation sudo flow forces a fresh
// credential prompt (prompt=login, max_age=0); the OP is expected to return an
// auth_time within seconds of the callback. A generous 120s window absorbs
// clock skew + the user's typing latency at the OP without admitting a stale
// session. SudoCallback fails closed on a zero or older auth_time.
const maxStepUpAuthAge = 120 * time.Second

// SudoCallback consumes the sudo step-up KV state, verifies the forced re-auth,
// and authorizes the elevation. It is the security core of the OIDC sudo flow:
// it enforces same-account (the session that began the flow owns it),
// same-upstream-identity (the re-auth resolved back to the linked subject), and
// freshness (the upstream returned a recent auth_time, proving it honored the
// forced prompt). On success it returns the stashed ReturnTo.
//
// Unlike HandleCallback, SudoCallback grants no session and provisions no
// account — it only attests that a fresh re-auth occurred. Audit of the
// success/failure is owned by the HTTP handler (Task 5), so SudoCallback
// returns typed errors only and does not call failNoAccount (login semantics
// would be wrong here). Every "flow is invalid" branch collapses onto
// ErrFederationStateInvalid; the two terminal security failures get distinct
// codes (sudo_identity_mismatch, sudo_reauth_stale) the dashboard can surface.
func (f *Federator) SudoCallback(ctx context.Context, stateToken, code, issParam, browserToken string, currentAccountID int32) (returnTo string, err error) {
	blob, err := f.kvStore.Pop(ctx, SudoKey(stateToken))
	if err != nil {
		return "", authn.ErrFederationStateInvalid()
	}
	state, err := DecodeFedState(blob)
	if err != nil {
		return "", authn.ErrFederationStateInvalid()
	}

	// Account match FIRST (fail fast, before any upstream call): the session
	// presenting this callback must own the flow that minted the state. A nil
	// SudoAccountID means the token belongs to a non-sudo flow.
	if state.SudoAccountID == nil || *state.SudoAccountID != currentAccountID {
		return "", authn.ErrFederationStateInvalid()
	}

	if !browserBindingOK(state.BrowserBinding, browserToken) {
		return "", authn.ErrFederationStateInvalid()
	}

	if issParam != "" && issParam != state.ExpectedIss {
		return "", authn.ErrFederationStateInvalid()
	}

	idp, err := f.q.GetUpstreamIDPBySlug(ctx, state.IDPSlug)
	if err != nil {
		// IdP disabled or deleted mid-flow (the query filters disabled rows).
		return "", authn.ErrFederationStateInvalid()
	}

	client, err := f.buildClient(ctx, &idp, flowSudo)
	if err != nil {
		return "", authn.ErrFederationStateInvalid()
	}

	// RFC 9700 mix-up defense: the snapshotted token_endpoint must still match.
	if client.TokenEndpoint() != state.ExpectedTokenEndpoint {
		return "", authn.ErrFederationStateInvalid()
	}

	tokens, err := client.Exchange(ctx, code, state.CodeVerifier, state.ExpectedIss, state.Nonce)
	if err != nil {
		return "", authn.ErrFederationStateInvalid()
	}

	// Identity match: the re-auth must resolve to the SAME upstream subject the
	// account linked at SudoBegin time. A different subject means the user
	// re-authenticated as someone else upstream — must NOT grant sudo.
	if tokens.Subject != state.ExpectedSub {
		return "", authn.ErrSudoIdentityMismatch()
	}

	// Freshness: fail closed. A zero auth_time (OP omitted the claim) or one
	// older than the step-up window means the OP did not honor prompt=login /
	// max_age=0 and may have silently reused a stale session.
	if tokens.AuthTime.IsZero() || time.Since(tokens.AuthTime) > maxStepUpAuthAge {
		return "", authn.ErrSudoReauthStale()
	}

	return state.ReturnTo, nil
}

// kickoffAvatarInherit launches the background avatar-inherit job unless the user
// owns their avatar. Non-blocking; safe on every federated login. client is the
// already-built RP client (reused for the UserInfo fallback).
//
// ctx is used ONLY for the cheap pre-flight GetAccountByID (so the request's
// deadline/cancellation bounds the skip check). The spawned goroutine runs on a
// fresh context.Background() — the request ctx is cancelled when the HTTP
// response is written, which would otherwise abort the detached fetch+store.
func (f *Federator) kickoffAvatarInherit(ctx context.Context, client *Client, idp db.UpstreamIdp, tokens *Tokens, accountID int32) {
	if acct, err := f.q.GetAccountByID(ctx, accountID); err == nil &&
		acct.AvatarSource.Valid && acct.AvatarSource.String == "user" {
		return
	}
	go f.runAvatarInherit(context.Background(), client, idp, tokens, accountID)
}

// CreateConfirmGrant stashes a confirmation grant in KV and returns the KV token
// plus the raw anti-forgery value the HTTP layer must set as the fed-state
// cookie (its hash is bound into the grant). Mirrors the BeginLogin anti-forgery
// pattern: only the hash is persisted, so a KV read never yields the cookie
// value, and the grant is bound to the browser that began the flow.
// amr is the upstream AMR list from the callback result; it is carried through
// so the confirm POST can issue the session with the correct AMR instead of
// always falling back to the generic ["federated"] value.
func (f *Federator) CreateConfirmGrant(ctx context.Context, accountID int32, identityID, idpID int64, idpSlug, returnTo string, amr []string) (token, antiForgery string, err error) {
	token, err = randB64URL(32)
	if err != nil {
		return "", "", err
	}
	antiForgery, err = randB64URL(32)
	if err != nil {
		return "", "", err
	}
	grant := ConfirmGrant{
		AccountID:      accountID,
		IdentityID:     identityID,
		IDPID:          idpID,
		IDPSlug:        idpSlug,
		ReturnTo:       returnTo,
		BrowserBinding: hashAntiForgery(antiForgery),
		AMR:            amr,
	}
	enc, err := grant.Encode()
	if err != nil {
		return "", "", err
	}
	if err := f.kvStore.SetEx(ctx, ConfirmKey(token), enc, 15*time.Minute); err != nil {
		return "", "", fmt.Errorf("federation/oidc: store confirm grant: %w", err)
	}
	return token, antiForgery, nil
}

// PopConfirmGrant single-use-consumes a grant and validates the browser binding.
// Every failure mode (missing token, decode failure, binding mismatch) collapses
// onto ErrFederationStateInvalid to avoid a state-probing side channel.
func (f *Federator) PopConfirmGrant(ctx context.Context, token, antiForgery string) (*ConfirmGrant, error) {
	raw, err := f.kvStore.Pop(ctx, ConfirmKey(token))
	if err != nil {
		return nil, authn.ErrFederationStateInvalid()
	}
	g, err := DecodeConfirmGrant(raw)
	if err != nil || !browserBindingOK(g.BrowserBinding, antiForgery) {
		return nil, authn.ErrFederationStateInvalid()
	}
	return g, nil
}

// PeekConfirmGrant reads (without consuming) a grant for the confirm GET, so the
// /welcome page can render the pending identity before the user accepts. Same
// generic-error and browser-binding semantics as PopConfirmGrant.
func (f *Federator) PeekConfirmGrant(ctx context.Context, token, antiForgery string) (*ConfirmGrant, error) {
	raw, err := f.kvStore.Get(ctx, ConfirmKey(token))
	if err != nil {
		return nil, authn.ErrFederationStateInvalid()
	}
	g, err := DecodeConfirmGrant(raw)
	if err != nil || !browserBindingOK(g.BrowserBinding, antiForgery) {
		return nil, authn.ErrFederationStateInvalid()
	}
	return g, nil
}

// AvatarPending reports whether a background avatar fetch is in flight for
// accountID (presence of the AvatarFetchKey marker). Used by the confirm GET so
// /welcome can show a spinner instead of the (not-yet-stored) avatar.
func (f *Federator) AvatarPending(ctx context.Context, accountID int32) bool {
	_, err := f.kvStore.Get(ctx, AvatarFetchKey(accountID))
	return err == nil
}

// runAvatarInherit resolves the upstream picture (id_token claim, else UserInfo),
// fetches + normalizes it, and stores it with avatar_source='upstream'. All
// failures are non-fatal (logged, status cleared). Deduped via SetNX. Detached
// context with a timeout.
func (f *Federator) runAvatarInherit(parent context.Context, client *Client, idp db.UpstreamIdp, tokens *Tokens, accountID int32) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	key := AvatarFetchKey(accountID)
	ok, err := f.kvStore.SetNX(ctx, key, "1", 60*time.Second)
	if err != nil || !ok {
		// Another login for this account is already inheriting (or the KV is
		// unavailable); a concurrent run owns the key, so exit without touching it.
		return
	}
	defer func() { _ = f.kvStore.Del(ctx, key) }()

	// Cheap pre-fetch guard: if the user already owns their avatar, never even
	// fetch the upstream picture. (kickoffAvatarInherit makes the same check
	// before launching, but re-checking here closes the window where the user
	// uploaded between kickoff and this goroutine actually running, and makes
	// runAvatarInherit a true no-op when driven directly.)
	if acct, err := f.q.GetAccountByID(ctx, accountID); err == nil &&
		acct.AvatarSource.Valid && acct.AvatarSource.String == "user" {
		return
	}

	pic := ClaimString(tokens.Raw, idp.PictureClaim)
	if pic == "" && client != nil {
		// The id_token omitted the picture claim — fall back to the UserInfo
		// endpoint. UserInfo enforces the sub match, so pass tokens.Subject.
		if ui, uerr := client.UserInfo(ctx, tokens.AccessToken, tokens.Subject); uerr == nil {
			pic = ClaimString(ui, idp.PictureClaim)
		} else {
			slog.WarnContext(ctx, "federation: upstream userinfo fallback failed", "account_id", accountID, "err", uerr)
		}
	}
	if pic == "" {
		return
	}

	raw, err := f.avatarFetch(ctx, pic, f.allowPrivateNetwork)
	if err != nil {
		slog.WarnContext(ctx, "federation: upstream avatar fetch failed", "account_id", accountID, "err", err)
		return
	}
	out, etag, err := avatar.Process(raw)
	if err != nil {
		slog.WarnContext(ctx, "federation: upstream avatar process failed", "account_id", accountID, "err", err)
		return
	}

	// Re-read the account after the (slow) fetch+process: the user may have
	// uploaded their own avatar in the meantime, and the stored etag may already
	// match — both make this a no-op.
	acct, err := f.q.GetAccountByID(ctx, accountID)
	if err != nil {
		return
	}
	if acct.AvatarSource.Valid && acct.AvatarSource.String == "user" {
		return
	}
	if acct.AvatarEtag.Valid && acct.AvatarEtag.String == etag {
		return
	}

	if err := f.q.UpsertAccountAvatarBytes(ctx, db.UpsertAccountAvatarBytesParams{AccountID: accountID, Bytes: out}); err != nil {
		slog.WarnContext(ctx, "federation: store upstream avatar bytes failed", "account_id", accountID, "err", err)
		return
	}
	if err := f.q.SetAccountAvatarMetaUpstream(ctx, db.SetAccountAvatarMetaUpstreamParams{
		ID:                accountID,
		AvatarContentType: pgtype.Text{String: "image/webp", Valid: true},
		AvatarEtag:        pgtype.Text{String: etag, Valid: true},
	}); err != nil {
		slog.WarnContext(ctx, "federation: set upstream avatar meta failed", "account_id", accountID, "err", err)
	}
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

// buildClient does the decrypt+NewClient dance shared by begin, HandleCallback,
// LinkCallback, and SudoCallback. flow picks the login-, link-, or sudo-flavored
// redirect URI so the token endpoint sees the exact value the OP recorded at
// /authorize.
//
// Results are memoized in f.clientCache for clientCacheTTL. A DEK rotation that
// bumps key_version is reflected in the cache key and naturally invalidates
// the entry; admin edits to other fields (client_id, scopes, issuer_url) are
// NOT reflected — D8 accepts that staleness window. Errors are never cached:
// a transient network blip during discovery should not poison subsequent
// requests.
func (f *Federator) buildClient(ctx context.Context, idp *db.UpstreamIdp, flow fedFlow) (*Client, error) {
	key := clientCacheKey(idp.Slug, idp.KeyVersion, flow)
	if v, ok := f.clientCache.Load(key); ok {
		entry := v.(*cachedClient)
		if time.Now().Before(entry.expiresAt) {
			return entry.client, nil
		}
		// Expired — evict this one key (no full-map sweep).
		f.clientCache.Delete(key)
	}

	secret, err := f.decryptSecret(idp)
	if err != nil {
		return nil, err
	}
	redirectURI := f.redirectURI(idp.Slug, flow)
	client, err := NewClient(
		ctx,
		idp.ClientID,
		string(secret),
		redirectURI,
		idp.Scopes,
		idp.IssuerUrl,
		DefaultAllowedAlgs(),
		f.allowPrivateNetwork,
	)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: build RP client: %w", err)
	}

	f.clientCache.Store(key, &cachedClient{
		client:    client,
		expiresAt: time.Now().Add(f.clientCacheTTL),
	})
	return client, nil
}

// clientCacheKey builds the composite key for clientCache. KeyVersion is
// included so a DEK rotation forces a fresh client (the decrypted secret may
// change); flow is included because login, link, and sudo flows use different
// redirect_uri values, which are baked into the *Client.
func clientCacheKey(slug string, keyVersion int32, flow fedFlow) string {
	return slug + ":" + strconv.Itoa(int(keyVersion)) + ":flow=" + flow.label()
}

// redirectURI builds the upstream-facing redirect_uri for the given flow,
// picking the login, link, or sudo callback. Must produce identical strings at
// begin() and the matching callback handler — the upstream OP records the value
// and compares it byte-for-byte at code-exchange time.
//
// The sudo callback is registered WITHOUT a {slug} path param (the state carries
// IDPSlug), so its template has no substitution. Operators must register this
// exact sudo callback URI — /api/prohibitorum/me/sudo/federation/callback — as
// an allowed redirect_uri at EACH upstream IdP, in addition to the per-slug
// login (/api/prohibitorum/auth/federation/{slug}/callback) and link
// (/api/prohibitorum/me/identities/link/{slug}/callback) callbacks.
func (f *Federator) redirectURI(slug string, flow fedFlow) string {
	var template string
	switch flow {
	case flowLink:
		template = "/api/prohibitorum/me/identities/link/{slug}/callback"
	case flowSudo:
		template = "/api/prohibitorum/me/sudo/federation/callback"
	default:
		template = "/api/prohibitorum/auth/federation/{slug}/callback"
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

// hashAntiForgery maps the raw anti-forgery cookie value to the digest stored
// in FedState.BrowserBinding. SHA-256 (high-entropy input, no salt needed) so
// the cookie value never lives in KV (N4 + N1 hygiene).
func hashAntiForgery(token string) string {
	h := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// browserBindingOK reports whether the callback's anti-forgery cookie matches
// the binding captured at BeginLogin. An empty binding means the flow carries
// no binding (skip). A non-empty binding requires a cookie that hashes to it,
// compared in constant time.
func browserBindingOK(binding, browserToken string) bool {
	if binding == "" {
		return true
	}
	if browserToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hashAntiForgery(browserToken)), []byte(binding)) == 1
}

// isUniqueViolation reports whether err wraps a Postgres SQLSTATE 23505
// (unique_violation). Duplicated from pkg/server/pgerr.go to keep this
// package's dependencies inside pkg/federation/oidc + pkg/db + pkg/audit
// + pkg/authn + pkg/kv + pkg/configx — no inbound dep on pkg/server.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
