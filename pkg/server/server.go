// Package server wires the identity HTTP surface together. Prohibitorum exists
// only to host accounts, run passkey ceremonies, manage sessions, and issue
// OIDC tokens.
package server

import (
	"context"
	"fmt"
	"net/http"
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
	// Audit records credential lifecycle events. Wired in v0.1; handlers
	// begin calling Record() in v0.2.
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

	kvStore, err := kv.New(config.KV.Driver, kv.WithRedisURL(config.KV.RedisURL))
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
	registerSecurityScheme(api)

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
	}
	s.registerOperations()
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
	registerSecurityScheme(s.api)
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

	addr := fmt.Sprintf(":%d", s.config.Port)
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

func registerSecurityScheme(api huma.API) {
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
		Name: sessstore.SessionCookieName,
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

	// v0.3 federation: upstream OIDC login + /me/identities management
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/auth/federation/{slug}/login", publicReq, s.handleFederationLoginHTTP)
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/auth/federation/{slug}/callback", publicReq, s.handleFederationCallbackHTTP)

	// Enrollment
	registerOp(mgmt, contract.OperationPreviewEnrollment, s.handlePreviewEnrollment, publicReq)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/enrollments/{token}/register/begin", publicReq, s.handleEnrollmentBeginHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/enrollments/{token}/register/complete", publicReq, s.handleEnrollmentCompleteHTTP)
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/enrollments/{token}/start-federation", publicReq, s.handleEnrollmentStartFederationHTTP)

	// /me
	registerOp(mgmt, contract.OperationGetMe, s.handleGetMe, sessionReq)
	registerOp(mgmt, contract.OperationListMyCredentials, s.handleListMyCredentials, sessionReq)
	registerOp(mgmt, contract.OperationDeleteMyCredential, s.handleDeleteMyCredential, sessionReq)
	registerOp(mgmt, contract.OperationRenameMyCredential, s.handleRenameMyCredential, sessionReq)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/credentials/register/begin", sessionReq, s.handleAddCredentialBeginHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/credentials/register/complete", sessionReq, s.handleAddCredentialCompleteHTTP)
	registerOp(mgmt, contract.OperationListMySessions, s.handleListMySessions, sessionReq)
	registerOp(mgmt, contract.OperationRevokeMySession, s.handleRevokeMySession, sessionReq)

	// Sudo
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/me/sudo/methods", sessionReq, s.handleSudoMethodsHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/sudo/begin", sessionReq, s.handleSudoBeginHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/sudo/complete", sessionReq, s.handleSudoCompleteHTTP)

	// /me sensitive endpoints (sudo-gated, conditional for TOTP enrollment).
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/password/set", sessionReq, s.handleMePasswordSetHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/totp/begin", sessionReq, s.handleMeTOTPBeginHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/totp/verify", sessionReq, s.handleMeTOTPVerifyHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/recovery-codes/regenerate", sessionReq, s.handleMeRegenerateRecoveryCodesHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/auth/revoke-password-totp", sessionReq, s.handleMeRevokePwdTOTPHTTP)

	// /me/identities — upstream OIDC identity linkage (v0.3). Sudo gating
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

	// Admin: accounts + invitations
	registerOp(mgmt, contract.OperationListAccounts, s.handleListAccounts, admin)
	registerOp(mgmt, contract.OperationGetAccount, s.handleGetAccount, admin)
	registerOp(mgmt, contract.OperationUpdateAccount, s.handleUpdateAccount, admin)
	registerOp(mgmt, contract.OperationDeleteAccount, s.handleDeleteAccount, admin)
	registerOp(mgmt, contract.OperationDeleteAccountCredential, s.handleDeleteAccountCredential, admin)
	registerOp(mgmt, contract.OperationRevokeAccountSessions, s.handleRevokeAccountSessions, admin)
	registerOp(mgmt, contract.OperationReissueEnrollment, s.handleReissueEnrollment, admin)
	registerOp(mgmt, contract.OperationCreateInvitation, s.handleCreateInvitation, admin)
	registerOp(mgmt, contract.OperationListInvitations, s.handleListInvitations, admin)
	registerOp(mgmt, contract.OperationRevokeInvitation, s.handleRevokeInvitation, admin)

	// OIDC OP — v0.4 full surface. Discovery and JWKS are public. Authorize
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
	// SSO-in is HTTP-Redirect (GET) only — HandleSSO decodes the
	// AuthnRequest from the query string; POST-binding AuthnRequest intake
	// is unimplemented and not advertised in IdP metadata.
	s.router.Get("/saml/sso", s.samlIdP.HandleSSO)
	s.router.Get("/saml/slo", s.samlIdP.HandleSLO)
	s.router.Post("/saml/slo", s.samlIdP.HandleSLO)
}
