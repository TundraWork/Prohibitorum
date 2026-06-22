// Package server wires the identity HTTP surface together. Prohibitorum exists
// only to host accounts, run passkey ceremonies, manage sessions, and issue
// OIDC tokens.
package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"

	"prohibitorum/db/migrations"
	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/branding"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/credential/pairing"
	"prohibitorum/pkg/credential/password"
	"prohibitorum/pkg/credential/totp"
	webauthnauth "prohibitorum/pkg/credential/webauthn"
	"prohibitorum/pkg/db"
	fedoidc "prohibitorum/pkg/federation/oidc"
	"prohibitorum/pkg/kv"
	"prohibitorum/pkg/logx"
	oidcop "prohibitorum/pkg/protocol/oidc"
	samlidp "prohibitorum/pkg/protocol/saml"
	sessstore "prohibitorum/pkg/session"
	"prohibitorum/pkg/webui"
)

type Server struct {
	queries       *db.Queries
	dbPool        *pgxpool.Pool
	router        *chi.Mux
	api           huma.API
	config        *configx.Config
	kvStore       kv.Store
	sessionStore  *sessstore.SessionStore
	pairingStore  *pairing.PairingStore
	rateLimiter   *authn.RateLimiter
	webauthn      *webauthn.WebAuthn
	oidcOP        *oidcop.Provider
	samlIdP       *samlidp.IdP
	passwordStore *password.Store
	totpStore     *totp.Store
	throttle      *authn.Throttle
	// federator orchestrates upstream OIDC federation. The Federation HTTP
	// handlers (handle_federation.go, Task 7) and /me/identities handlers
	// (Task 8) reach through it. Construction lives in NewServer wiring —
	// Task 9 owns that; this field is nil until Task 9 lands.
	federator *fedoidc.Federator
	// Audit records credential lifecycle events.
	Audit audit.Writer
	// sudoFlowOverride lets tests inject a fake sudoFlowQueries for the
	// /me/sudo/methods computation without standing up *db.Queries. Nil in
	// production — handlers fall back to s.queries.
	sudoFlowOverride sudoFlowQueries
	// meTOTPFlowOverride lets tests inject a fake meTOTPFlowQueries for the
	// /me/totp/* conditional-sudo branch (which reads totp_credential.
	// ConfirmedAt directly). Nil in production — handlers fall back to
	// s.queries. Kept narrow so the unit-test surface doesn't drift from
	// production. See handle_me_totp.go for the seam.
	meTOTPFlowOverride meTOTPFlowQueries
	// revokeFlowOverride lets tests inject a fake authn.FlowQueries for the
	// /me/auth/revoke-password-totp handler. Nil in production — falls back
	// to s.queries.
	revokeFlowOverride authn.FlowQueries
	// accountLookup lets tests inject a fake for the post-partial-session
	// disabled re-check in /auth/{totp,recovery-code}/verify (Bundle 1 /
	// Fix 4). Nil in production — falls back to s.queries.
	accountLookup accountLookupQueries
	// listFedOverride lets tests inject a fake for
	// handleListFederationProvidersHTTP without standing up *db.Queries. Nil
	// in production — falls back to s.queries.
	listFedOverride listFedQueries
	// invitationOverride lets tests inject a fake for the invitation handlers
	// (handleCreateInvitation, handleListInvitations) without standing up
	// *db.Queries. Nil in production — falls back to s.queries.
	invitationOverride db.Querier
	// updateMeOverride lets tests inject a fake updateMeQueries for
	// handleUpdateMe without standing up *db.Queries. Nil in production —
	// falls back to s.queries.
	updateMeOverride updateMeQueries
	// getMyFactorsOverride lets tests inject a fake getMyFactorsQueries for
	// handleGetMyFactors without standing up *db.Queries. Nil in production —
	// falls back to s.queries.
	getMyFactorsOverride getMyFactorsQueries
	// launchpadOverride lets tests inject a fake launchpadQueries for
	// handleListMyApps / buildLaunchpad without standing up *db.Queries. Nil
	// in production — falls back to s.queries.
	launchpadOverride launchpadQueries
	// consentMgmtOverride lets tests inject a fake consentMgmtQueries for
	// handleListMyConsent / handleRevokeMyConsent without standing up *db.Queries.
	// Nil in production — handlers fall back to s.queries.
	consentMgmtOverride consentMgmtQueries
	// avatarQueriesOverride lets tests inject a fake avatarQueries for the
	// avatar handlers without standing up *db.Queries. Nil in production —
	// handlers fall back to s.queries.
	avatarQueriesOverride avatarQueries
	// confirmFedOverride lets tests inject a fake confirmFedQueries for the
	// /welcome federation confirm endpoints without standing up *db.Queries.
	// Nil in production — handlers fall back to s.queries.
	confirmFedOverride confirmFedQueries
	// branding resolves the effective instance name and icon with DB-override →
	// config → built-in precedence. Admin mutation handlers call Invalidate()
	// after writes so changes propagate immediately.
	branding *branding.Resolver
}

// accountLookupQueries is the narrow query surface the step-2 handlers
// (handleTOTPVerifyHTTP, handleRecoveryCodeVerifyHTTP) need for the
// disabled-mid-flow re-check. Declared here so tests can stub it without
// constructing a *db.Queries. Production wiring (NewServer) leaves
// accountLookup nil and handlers fall back to s.queries.
type accountLookupQueries interface {
	GetAccountByID(ctx context.Context, id int32) (db.Account, error)
}

func NewServer(ctx context.Context) (*Server, error) {
	config, err := configx.Parse()
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	logx.WithContext(ctx).Info("running migrations")
	if _, err := migrations.UpWithResult(config.DatabaseURL); err != nil {
		return nil, fmt.Errorf("migrations: %w", err)
	}

	conn, err := pgxpool.New(ctx, config.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect db: %w", err)
	}
	logx.WithContext(ctx).Info("connected to database")

	queries := db.New(conn)

	// First-boot: ensure an active OIDC signing key exists (no-op if one does).
	ensureActiveSigningKey(ctx, conn, queries, config)

	kvStore, err := kv.New(config.KV.Driver,
		kv.WithRedisURL(config.KV.RedisURL),
		kv.WithRedisUsername(config.KV.RedisUsername),
		kv.WithRedisPassword(config.KV.RedisPassword),
		kv.WithRedisTLS(config.KV.RedisTLS),
	)
	if err != nil {
		return nil, fmt.Errorf("kv: %w", err)
	}
	sessionStore := sessstore.NewSessionStore(kvStore, queries, config.SessionTTL)

	wa, err := webauthnauth.NewWebAuthn(config.WebAuthn)
	if err != nil {
		return nil, fmt.Errorf("webauthn: %w", err)
	}
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"rp_id":   config.WebAuthn.RPID,
		"origins": config.WebAuthn.RPOrigins,
	}).Info("auth ready")

	router := chi.NewMux()
	router.Use(sessstore.LoadSession(config, queries, sessionStore))
	api := humachi.New(router, huma.DefaultConfig("Prohibitorum Identity API", "1.0.0"))
	registerSecurityScheme(api, sessstore.SessionCookieNameFor(config))

	auditWriter := audit.NewWriter(queries)
	throttle := authn.NewThrottle(queries, config.Auth.ThrottleSchedule, auditWriter)
	passwordStore := password.NewStore(queries, config.PasswordHashParams, throttle, auditWriter)
	totpTxRunner := &totp.PoolTxRunner{Pool: conn, Queries: queries}
	totpStore := totp.NewStore(queries, totpTxRunner, config.DataEncryptionKeys, config.TOTP, throttle, auditWriter)

	publicOrigin := ""
	if len(config.PublicOrigins) > 0 {
		publicOrigin = config.PublicOrigins[0]
	}
	federator := fedoidc.NewFederator(
		queries,
		kvStore,
		auditWriter,
		config.Federation,
		config.DataEncryptionKeys,
		conn,
		publicOrigin,
	)

	rateLimiter := authn.NewRateLimiter()

	brandingResolver, berr := branding.New(config.Branding.InstanceName, config.Branding.IconPath, branding.NewPGStore(conn))
	if berr != nil {
		logx.WithContext(ctx).WithError(berr).Warn("branding: config icon_path unusable; using built-in default")
		brandingResolver, _ = branding.New(config.Branding.InstanceName, "", branding.NewPGStore(conn))
	}

	s := &Server{
		queries:       queries,
		dbPool:        conn,
		router:        router,
		api:           api,
		config:        config,
		kvStore:       kvStore,
		sessionStore:  sessionStore,
		pairingStore:  pairing.NewPairingStore(kvStore),
		rateLimiter:   rateLimiter,
		webauthn:      wa,
		oidcOP:        oidcop.New(config, queries, kvStore, sessionStore, auditWriter, rateLimiter),
		samlIdP:       samlidp.NewIdP(config, queries, kvStore, sessionStore, auditWriter, rateLimiter),
		passwordStore: passwordStore,
		totpStore:     totpStore,
		throttle:      throttle,
		federator:     federator,
		Audit:         auditWriter,
		branding:      brandingResolver,
	}
	s.registerOperations()
	s.router.NotFound(webui.Handler(s.config.Branding.InstanceName).ServeHTTP)
	logx.WithContext(ctx).Info("registered operations")
	return s, nil
}

// NewHuma returns a bare huma.API for the openapi subcommand to emit specs
// without running migrations / connecting to the DB.
func NewHuma() huma.API {
	router := chi.NewMux()
	s := &Server{
		router: router,
		api:    humachi.New(router, huma.DefaultConfig("Prohibitorum Identity API", "1.0.0")),
	}
	registerSecurityScheme(s.api, sessstore.SessionCookieName)
	s.registerOperations()
	return s.api
}

func (s *Server) Serve() error {
	// Periodically reap expired revoked_jti rows. PruneExpiredRevokedJTI has no
	// other caller, so without this the denylist (queried on every /userinfo and
	// /introspect) grows unbounded. Launched only from Serve() so tests and the
	// openapi subcommand never start it.
	go s.pruneRevokedJTILoop()
	// Periodically reap expired saml_session rows. The schema's session_id
	// FK ON DELETE CASCADE never fires (sessions are soft-deleted, never hard
	// DELETEd), so without this reaper the table grows unbounded even after SLO
	// and the dedup upsert. Launched only from Serve() (not tests/openapi).
	go s.pruneExpiredSAMLSessionsLoop()
	// Periodically advance decommissioning signing keys past their retire_after
	// horizon to 'retired', which drops them from JWKS / SAML metadata. This is
	// the only caller of ReconcileRetiredSigningKeys; the operation is
	// idempotent and never touches active/pending keys. Launched only from
	// Serve() (not tests/openapi).
	go s.reconcileSigningKeysLoop()

	// Bind the configured host interface when set (e.g. 127.0.0.1 to listen
	// loopback-only behind a reverse proxy); an empty host keeps the default
	// all-interfaces bind. net.JoinHostPort handles IPv6 literal bracketing.
	addr := fmt.Sprintf(":%d", s.config.Port)
	if s.config.Host != "" {
		addr = net.JoinHostPort(s.config.Host, strconv.Itoa(s.config.Port))
	}
	logx.WithFields(logrus.Fields{"addr": addr}).Info("serving API")
	return http.ListenAndServe(addr, s.router)
}

// pruneRevokedJTILoop deletes expired entries from the revoked_jti denylist once
// at startup and then hourly. A prune error is logged and retried on the next
// tick — it must never crash the server.
func (s *Server) pruneRevokedJTILoop() {
	ctx := context.Background()
	t := time.NewTicker(1 * time.Hour)
	defer t.Stop()
	for {
		if err := s.queries.PruneExpiredRevokedJTI(ctx); err != nil {
			logx.WithContext(ctx).WithError(err).Warn("prune revoked_jti")
		}
		<-t.C
	}
}

// pruneExpiredSAMLSessionsLoop deletes saml_session rows whose not_on_or_after
// horizon has passed, once at startup and then hourly. These rows are the SLO
// binding state; the FK cascade can't reclaim them (sessions are soft-deleted),
// so this age-based reaper is the only unconditional GC for them. A prune error
// is logged and retried on the next tick — it must never crash the server.
func (s *Server) pruneExpiredSAMLSessionsLoop() {
	ctx := context.Background()
	t := time.NewTicker(1 * time.Hour)
	defer t.Stop()
	for {
		if _, err := s.queries.DeleteExpiredSAMLSessions(ctx); err != nil {
			logx.WithContext(ctx).WithError(err).Warn("prune saml_session")
		}
		<-t.C
	}
}

// reconcileSigningKeysLoop flips decommissioning signing keys whose retire_after
// horizon has passed to 'retired', once at startup and then hourly. Retiring a
// key removes it from the published JWKS / SAML metadata set, so this is what
// finally garbage-collects a rotated-out key once its grace window elapses. The
// query is idempotent and never touches active/pending keys; an error is logged
// and retried on the next tick — it must never crash the server.
func (s *Server) reconcileSigningKeysLoop() {
	ctx := context.Background()
	t := time.NewTicker(1 * time.Hour)
	defer t.Stop()
	for {
		n, err := s.queries.ReconcileRetiredSigningKeys(ctx)
		switch {
		case err != nil:
			logx.WithContext(ctx).WithError(err).Warn("reconcile signing keys")
		case n > 0:
			logx.WithContext(ctx).WithFields(logrus.Fields{"retired": n}).Info("reconciled signing keys")
		}
		<-t.C
	}
}

func registerSecurityScheme(api huma.API, cookieName string) {
	doc := api.OpenAPI()
	if doc.Components == nil {
		doc.Components = &huma.Components{}
	}
	if doc.Components.SecuritySchemes == nil {
		doc.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	doc.Components.SecuritySchemes["prohibitorumSession"] = &huma.SecurityScheme{
		Type: "apiKey",
		In:   "cookie",
		Name: cookieName,
	}
}

func (s *Server) registerOperations() {
	mgmt := huma.NewGroup(s.api, "/api/prohibitorum")
	admin := contract.AuthRequirement{Kind: contract.AuthAdmin}
	sessionReq := contract.AuthRequirement{Kind: contract.AuthSession}
	publicReq := contract.AuthRequirement{Kind: contract.AuthPublic}

	// Auth
	registerOp(mgmt, contract.OperationAuthStatus, s.handleAuthStatus, publicReq)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/auth/login/begin", publicReq, s.handleLoginBeginHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/auth/login/complete", publicReq, s.handleLoginCompleteHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/auth/logout", publicReq, s.handleLogoutHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/auth/password/begin", publicReq, s.handlePasswordBeginHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/auth/totp/verify", publicReq, s.handleTOTPVerifyHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/auth/recovery-code/verify", publicReq, s.handleRecoveryCodeVerifyHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/auth/recovery/totp/begin", publicReq, s.handleAuthRecoveryTOTPBeginHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/auth/recovery/totp/verify", publicReq, s.handleAuthRecoveryTOTPVerifyHTTP)

	// federation: upstream OIDC login + /me/identities management
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/auth/federation", publicReq, s.handleListFederationProvidersHTTP)
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/auth/federation/{slug}/login", publicReq, s.handleFederationLoginHTTP)
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/auth/federation/{slug}/callback", publicReq, s.handleFederationCallbackHTTP)
	// /welcome confirmation step for first-time (unconfirmed) federated identities.
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/auth/federation/confirm", publicReq, s.handleFederationConfirmGet)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/auth/federation/confirm", publicReq, s.handleFederationConfirmPost)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/auth/federation/confirm/decline", publicReq, s.handleFederationConfirmDecline)

	// Enrollment
	registerOp(mgmt, contract.OperationPreviewEnrollment, s.handlePreviewEnrollment, publicReq)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/enrollments/{token}/register/begin", publicReq, s.handleEnrollmentBeginHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/enrollments/{token}/register/complete", publicReq, s.handleEnrollmentCompleteHTTP)
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/enrollments/{token}/start-federation", publicReq, s.handleEnrollmentStartFederationHTTP)

	// /me
	registerOp(mgmt, contract.OperationGetMe, s.handleGetMe, sessionReq)
	registerOp(mgmt, contract.OperationUpdateMe, s.handleUpdateMe, sessionReq)
	registerOp(mgmt, contract.OperationGetMyFactors, s.handleGetMyFactors, sessionReq)
	registerOp(mgmt, contract.OperationListMyCredentials, s.handleListMyCredentials, sessionReq)
	registerOp(mgmt, contract.OperationDeleteMyCredential, s.handleDeleteMyCredential, sessionReq)
	registerOp(mgmt, contract.OperationRenameMyCredential, s.handleRenameMyCredential, sessionReq)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/credentials/register/begin", sessionReq, s.handleAddCredentialBeginHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/credentials/register/complete", sessionReq, s.handleAddCredentialCompleteHTTP)
	registerOp(mgmt, contract.OperationListMySessions, s.handleListMySessions, sessionReq)
	registerOp(mgmt, contract.OperationRevokeMySession, s.handleRevokeMySession, sessionReq)
	registerOp(mgmt, contract.OperationListMyApps, s.handleListMyApps, sessionReq)
	registerOp(mgmt, contract.OperationListMyConsent, s.handleListMyConsent, sessionReq)
	registerOp(mgmt, contract.OperationRevokeConsent, s.handleRevokeMyConsent, sessionReq)

	// Consent app API (OIDC consent UI context + decision).
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/consent", sessionReq, s.handleConsentContextHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/consent", sessionReq, s.handleConsentDecisionHTTP)

	// Sudo
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/me/sudo/methods", sessionReq, s.handleSudoMethodsHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/sudo/begin", sessionReq, s.handleSudoBeginHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/sudo/complete", sessionReq, s.handleSudoCompleteHTTP)

	// Public branding: SPA boot config + icon image.
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/config", publicReq, s.handleGetPublicConfigHTTP)
	registerOpHTTP(s.router, "GET", "/branding/icon", publicReq, s.handleGetBrandingIconHTTP)
	registerOpHTTP(s.router, "GET", "/icon/{kind}/{id}", publicReq, s.handleGetEntityIconHTTP)

	// Native Traefik ForwardAuth (see docs/forward-auth.md). The verify endpoint
	// is the middleware target; the callback is routed by the operator on each
	// protected domain to plant the per-domain forward-auth cookie. Both public.
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/forward-auth/verify", publicReq, s.oidcOP.HandleForwardAuthVerify)
	s.router.Get(oidcop.ForwardAuthPathPrefix+"/callback", s.oidcOP.HandleForwardAuthCallback)
	// Sign-out: the protected-domain sign_out clears the per-domain cookie + KV
	// session, then bounces to the IdP-domain sso-logout (which terminates the
	// SSO session and redirects back only to a validated forward-auth host).
	s.router.Get(oidcop.ForwardAuthPathPrefix+"/sign_out", s.oidcOP.HandleForwardAuthSignOut)
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/forward-auth/sso-logout", publicReq, s.handleForwardAuthSSOLogoutHTTP)

	// Avatar upload/delete (self), source selection, status, and public fetch.
	registerOpHTTP(s.router, "PUT", "/api/prohibitorum/me/avatar", sessionReq, s.handlePutAvatarHTTP)
	registerOpHTTP(s.router, "PUT", "/api/prohibitorum/me/avatar/selection", sessionReq, s.handlePutAvatarSelectionHTTP)
	registerOpHTTP(s.router, "DELETE", "/api/prohibitorum/me/avatar", sessionReq, s.handleDeleteAvatarHTTP)
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/me/avatar/status", sessionReq, s.handleAvatarStatusHTTP)
	registerOpHTTP(s.router, "GET", "/avatar/{subject}", contract.AuthRequirement{Kind: contract.AuthPublic}, s.handleGetAvatarHTTP)

	// /me sensitive endpoints (sudo-gated, conditional for TOTP enrollment).
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/password/set", sessionReq, s.handleMePasswordSetHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/totp/begin", sessionReq, s.handleMeTOTPBeginHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/totp/verify", sessionReq, s.handleMeTOTPVerifyHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/recovery-codes/regenerate", sessionReq, s.handleMeRegenerateRecoveryCodesHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/auth/revoke-password-totp", sessionReq, s.handleMeRevokePwdTOTPHTTP)

	// /me/identities — upstream OIDC identity linkage. Sudo gating
	// lives inside the handlers, not at the route layer.
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/me/identities", sessionReq, s.handleMeIdentitiesListHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/identities/{id}/unlink", sessionReq, s.handleMeIdentitiesUnlinkHTTP)
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/me/identities/link/{slug}/begin", sessionReq, s.handleMeIdentitiesLinkBeginHTTP)
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/me/identities/link/{slug}/callback", sessionReq, s.handleMeIdentitiesLinkCallbackHTTP)

	// Device pairing
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/auth/devices/pair/begin", publicReq, s.handlePairBeginHTTP)
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/auth/devices/pair/status", publicReq, s.handlePairStatusHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/auth/devices/pair/complete", publicReq, s.handlePairCompleteHTTP)
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/me/devices/pair/lookup", sessionReq, s.handlePairLookupHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/devices/pair/approve", sessionReq, s.handlePairApproveHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/devices/pair/cancel", sessionReq, s.handlePairCancelHTTP)

	// Admin: audit events (read-only, filterable, keyset-paginated)
	registerOp(mgmt, contract.OperationListAuditEvents, s.handleListAuditEvents, admin)

	// Admin: accounts + invitations
	registerOp(mgmt, contract.OperationListAccounts, s.handleListAccounts, admin)
	registerOp(mgmt, contract.OperationGetAccount, s.handleGetAccount, admin)
	// UpdateAccount, DeleteAccount, set-disabled, credential delete,
	// reissue-enrollment, and invitation CREATE remain fresh-sudo gated:
	// UpdateAccount can escalate user→admin, the others are destructive or
	// mint credentials/enrollment. Session-revoke, all-sessions-revoke, and
	// invitation REVOKE are reversible operational actions — admin auth only.
	registerSudoOp(s, mgmt, contract.OperationUpdateAccount, s.handleUpdateAccount, admin)
	s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/accounts/set-disabled", admin, s.handleSetAccountDisabledHTTP)
	registerSudoOp(s, mgmt, contract.OperationDeleteAccount, s.handleDeleteAccount, admin)
	s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/accounts/credentials/delete", admin, s.handleDeleteAccountCredentialHTTP)
	registerOp(mgmt, contract.OperationListAccountCredentials, s.handleListAccountCredentials, admin)
	registerOp(mgmt, contract.OperationListAccountSessions, s.handleListAccountSessions, admin)
	registerOp(mgmt, contract.OperationListAccountGroups, s.handleListAccountGroups, admin)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/accounts/{id}/sessions/revoke", admin, s.handleRevokeAccountSessionHTTP)
	registerOp(mgmt, contract.OperationRevokeAccountSessions, s.handleRevokeAccountSessions, admin)
	registerSudoOp(s, mgmt, contract.OperationReissueEnrollment, s.handleReissueEnrollment, admin)
	registerSudoOp(s, mgmt, contract.OperationCreateInvitation, s.handleCreateInvitation, admin)
	registerOp(mgmt, contract.OperationListInvitations, s.handleListInvitations, admin)
	registerOp(mgmt, contract.OperationRevokeInvitation, s.handleRevokeInvitation, admin)

	// Admin: instance-branding settings (name + icon)
	s.registerSudoOpHTTP(s.router, "PUT", "/api/prohibitorum/admin/settings", admin, s.handlePutInstanceNameHTTP)
	registerOpHTTP(s.router, "PUT", "/api/prohibitorum/admin/settings/icon", admin, s.handlePutInstanceIconHTTP)
	s.registerSudoOpHTTP(s.router, "DELETE", "/api/prohibitorum/admin/settings/icon", admin, s.handleDeleteInstanceIconHTTP)

	// Admin: signing-key lifecycle management
	registerOp(mgmt, contract.OperationListSigningKeys, s.handleListSigningKeys, admin)
	s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/signing-keys/generate", admin, s.handleGenerateSigningKeyHTTP)
	s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/signing-keys/{kid}/activate", admin, s.handleActivateSigningKeyHTTP)
	s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/signing-keys/{kid}/retire", admin, s.handleRetireSigningKeyHTTP)

	// Admin: OIDC application management
	registerOp(mgmt, contract.OperationListOIDCApplications, s.handleListOIDCApplications, admin)
	registerOp(mgmt, contract.OperationGetOIDCApplication, s.handleGetOIDCApplication, admin)
	s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/oidc-applications", admin, s.handleCreateOIDCApplicationHTTP)
	s.registerSudoOpHTTP(s.router, "PUT", "/api/prohibitorum/oidc-applications/{clientId}", admin, s.handleUpdateOIDCApplicationHTTP)
	s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/oidc-applications/rotate-secret", admin, s.handleRotateOIDCApplicationSecretHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/oidc-applications/set-disabled", admin, s.handleSetOIDCApplicationDisabledHTTP)
	s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/oidc-applications/delete", admin, s.handleDeleteOIDCApplicationHTTP)

	// Admin: forward-auth application management (Phase 2). A forward-auth app
	// is an oidc_client with forward_auth_enabled=true; presented as its own
	// section and excluded from the OIDC-applications list. RBAC reuses the OIDC
	// app-access endpoints (/oidc-applications/{clientId}/access/*).
	registerOp(mgmt, contract.OperationListForwardAuthApps, s.handleListForwardAuthApps, admin)
	registerOp(mgmt, contract.OperationGetForwardAuthApp, s.handleGetForwardAuthApp, admin)
	s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/forward-auth-apps", admin, s.handleCreateForwardAuthAppHTTP)
	s.registerSudoOpHTTP(s.router, "PUT", "/api/prohibitorum/forward-auth-apps/{clientId}", admin, s.handleUpdateForwardAuthAppHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/forward-auth-apps/set-disabled", admin, s.handleSetForwardAuthAppDisabledHTTP)
	s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/forward-auth-apps/delete", admin, s.handleDeleteForwardAuthAppHTTP)

	// Admin: identity provider management
	registerOp(mgmt, contract.OperationListIdentityProviders, s.handleListIdentityProviders, admin)
	registerOp(mgmt, contract.OperationGetIdentityProvider, s.handleGetIdentityProvider, admin)
	s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/identity-providers", admin, s.handleCreateIdentityProviderHTTP)
	s.registerSudoOpHTTP(s.router, "PUT", "/api/prohibitorum/identity-providers/{slug}", admin, s.handleUpdateIdentityProviderHTTP)
	s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/identity-providers/rotate-secret", admin, s.handleRotateIdentityProviderSecretHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/identity-providers/set-disabled", admin, s.handleSetIdentityProviderDisabledHTTP)
	s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/identity-providers/delete", admin, s.handleDeleteIdentityProviderHTTP)

	// Admin: SAML application management
	registerOp(mgmt, contract.OperationListSAMLApplications, s.handleListSAMLApplications, admin)
	registerOp(mgmt, contract.OperationGetSAMLApplication, s.handleGetSAMLApplication, admin)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/saml-applications", admin, s.handleCreateSAMLApplicationHTTP)
	registerOpHTTP(s.router, "PUT", "/api/prohibitorum/saml-applications/{id}", admin, s.handleUpdateSAMLApplicationHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/saml-applications/{id}/reingest-metadata", admin, s.handleReingestSAMLApplicationHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/saml-applications/set-disabled", admin, s.handleSetSAMLApplicationDisabledHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/saml-applications/delete", admin, s.handleDeleteSAMLApplicationHTTP)

	// Admin: per-entity icon upload/remove (app & provider icons). PUT is raw
	// image + in-handler fresh sudo (the sudo wrapper rejects non-JSON bodies);
	// DELETE is sudo-gated via the wrapper. Mirrors the instance-icon pattern.
	registerOpHTTP(s.router, "PUT", "/api/prohibitorum/oidc-applications/{clientId}/icon", admin, s.handlePutOIDCAppIconHTTP)
	s.registerSudoOpHTTP(s.router, "DELETE", "/api/prohibitorum/oidc-applications/{clientId}/icon", admin, s.handleDeleteOIDCAppIconHTTP)
	registerOpHTTP(s.router, "PUT", "/api/prohibitorum/saml-applications/{id}/icon", admin, s.handlePutSAMLAppIconHTTP)
	s.registerSudoOpHTTP(s.router, "DELETE", "/api/prohibitorum/saml-applications/{id}/icon", admin, s.handleDeleteSAMLAppIconHTTP)
	registerOpHTTP(s.router, "PUT", "/api/prohibitorum/identity-providers/{slug}/icon", admin, s.handlePutIdentityProviderIconHTTP)
	s.registerSudoOpHTTP(s.router, "DELETE", "/api/prohibitorum/identity-providers/{slug}/icon", admin, s.handleDeleteIdentityProviderIconHTTP)

	// Admin: app-access management (restrict + grants) — OIDC
	registerOp(mgmt, contract.OperationGetOIDCClientAccess, s.handleGetOIDCClientAccess, admin)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/oidc-applications/{clientId}/access/set-restricted", admin, s.handleSetOIDCClientAccessRestrictedHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/oidc-applications/{clientId}/access/grant", admin, s.handleGrantOIDCClientAccessHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/oidc-applications/{clientId}/access/revoke", admin, s.handleRevokeOIDCClientAccessHTTP)

	// Admin: app-access management (restrict + grants) — SAML
	registerOp(mgmt, contract.OperationGetSAMLSPAccess, s.handleGetSAMLSPAccess, admin)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/saml-applications/{id}/access/set-restricted", admin, s.handleSetSAMLSPAccessRestrictedHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/saml-applications/{id}/access/grant", admin, s.handleGrantSAMLSPAccessHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/saml-applications/{id}/access/revoke", admin, s.handleRevokeSAMLSPAccessHTTP)

	// Admin: group CRUD + membership management
	registerOp(mgmt, contract.OperationListGroups, s.handleListGroups, admin)
	registerOp(mgmt, contract.OperationGetGroup, s.handleGetGroup, admin)
	registerOp(mgmt, contract.OperationListGroupMembers, s.handleListGroupMembers, admin)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/groups", admin, s.handleCreateGroupHTTP)
	registerOpHTTP(s.router, "PUT", "/api/prohibitorum/groups/{id}", admin, s.handleUpdateGroupHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/groups/delete", admin, s.handleDeleteGroupHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/groups/{id}/members", admin, s.handleAddGroupMemberHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/groups/{id}/members/remove", admin, s.handleRemoveGroupMemberHTTP)

	// OIDC OP — full surface. Discovery and JWKS are public. Authorize
	// benefits from the global LoadSession middleware (already installed on
	// the router) so that authn.SessionFromContext works inside the handler.
	// Token, userinfo (GET+POST per OIDC spec), introspect, revoke, and
	// logout are public endpoints that perform their own bearer/client auth.
	s.router.Get("/.well-known/openid-configuration", s.oidcOP.HandleDiscovery)
	s.router.Get("/oauth/jwks", s.oidcOP.HandleJWKS)
	s.router.Get("/oauth/authorize", s.oidcOP.HandleAuthorize)
	s.router.Post("/oauth/token", s.oidcOP.HandleToken)
	s.router.Get("/oauth/userinfo", s.oidcOP.HandleUserinfo)
	s.router.Post("/oauth/userinfo", s.oidcOP.HandleUserinfo)
	s.router.Post("/oauth/introspect", s.oidcOP.HandleIntrospect)
	s.router.Post("/oauth/revoke", s.oidcOP.HandleRevoke)
	s.router.Get("/oidc/logout", s.oidcOP.HandleLogout)
	s.router.Get("/saml/metadata", s.samlIdP.HandleMetadata)
	// SSO-in accepts both bindings — HandleSSO/parseAuthnRequest dispatches on
	// the request method: GET decodes a HTTP-Redirect AuthnRequest from the
	// query string; POST decodes a HTTP-POST (enveloped-signature) AuthnRequest
	// from the form. Both are advertised in IdP metadata.
	s.router.Get("/saml/sso", s.samlIdP.HandleSSO)
	s.router.Post("/saml/sso", s.samlIdP.HandleSSO)
	// IdP-initiated (unsolicited) SSO app-launcher (spec D11): emits an
	// unsolicited Response to the SP's default ACS, gated by the per-SP
	// allow_idp_initiated opt-in.
	s.router.Get("/saml/sso/init", s.samlIdP.HandleIdPInitiated)
	s.router.Get("/saml/slo", s.samlIdP.HandleSLO)
	s.router.Post("/saml/slo", s.samlIdP.HandleSLO)
}
