// Package server wires the identity HTTP surface together. Prohibitorum exists
// only to host accounts, run passkey ceremonies, manage sessions, and issue
// OIDC tokens.
package server

import (
	"context"
	"fmt"
	"net/http"

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
	webauthnauth "prohibitorum/pkg/credential/webauthn"
	"prohibitorum/pkg/credential/pairing"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/kv"
	"prohibitorum/pkg/logx"
	oidcop "prohibitorum/pkg/protocol/oidc"
	sessstore "prohibitorum/pkg/session"
)

type Server struct {
	queries      *db.Queries
	dbPool       *pgxpool.Pool
	router       *chi.Mux
	api          huma.API
	config       *configx.Config
	kvStore      kv.Store
	sessionStore *sessstore.SessionStore
	pairingStore *pairing.PairingStore
	rateLimiter  *authn.RateLimiter
	webauthn     *webauthn.WebAuthn
	oidcOP       *oidcop.Provider
	// Audit records credential lifecycle events. Wired in v0.1; handlers
	// begin calling Record() in v0.2.
	Audit audit.Writer
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

	s := &Server{
		queries:      queries,
		dbPool:       conn,
		router:       router,
		api:          api,
		config:       config,
		kvStore:      kvStore,
		sessionStore: sessionStore,
		pairingStore: pairing.NewPairingStore(kvStore),
		rateLimiter:  authn.NewRateLimiter(),
		webauthn:     wa,
		oidcOP:       oidcop.New(config),
		Audit:        audit.NewWriter(queries),
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
	addr := fmt.Sprintf(":%d", s.config.Port)
	logx.WithFields(logrus.Fields{"addr": addr}).Info("serving API")
	return http.ListenAndServe(addr, s.router)
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

	// Enrollment
	registerOp(mgmt, contract.OperationPreviewEnrollment, s.handlePreviewEnrollment, publicReq)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/enrollments/{token}/register/begin", publicReq, s.handleEnrollmentBeginHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/enrollments/{token}/register/complete", publicReq, s.handleEnrollmentCompleteHTTP)

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
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/sudo/begin", sessionReq, s.handleSudoBeginHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/me/sudo/complete", sessionReq, s.handleSudoCompleteHTTP)

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

	// OIDC OP — discovery and JWKS are usable from v0.1. The discovery doc
	// advertises authorize/token/userinfo/logout/jwks URLs; the latter four
	// land mounted-and-functional in v0.4 (currently unmounted; their
	// handlers in pkg/protocol/oidc return 501).
	s.router.Get("/.well-known/openid-configuration", s.oidcOP.HandleDiscovery)
	s.router.Get("/oauth/jwks", s.oidcOP.HandleJWKS)
}
