package main

// dev-seed: populate the dev database with representative example data so the
// frontend SPA's data-driven elements render during design work.
//
// Dev-only: refuses to run unless config.PublicOrigins[0] is a loopback host.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"time"

	"prohibitorum/db/migrations"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/credential/enrollment"
	"prohibitorum/pkg/credential/pat"
	"prohibitorum/pkg/db"
	federationoidc "prohibitorum/pkg/federation/providers/oidc"
	"prohibitorum/pkg/protocol/oidc"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

// loopbackHosts is the set of hostnames considered "dev-safe" by the dev-seed
// guard. Anything not in this set aborts the command.
var loopbackHosts = map[string]bool{
	"localhost": true,
	"127.0.0.1": true,
	"::1":       true,
}

// isLoopbackOrigin reports whether origin is dev-safe: its host is a known
// loopback name, a loopback IP literal, or a DNS name that resolves *entirely*
// to loopback addresses. Fail-closed: malformed input, a resolution error, or
// any non-loopback resolved IP returns false. This lets dev harnesses use real
// DNS names pinned to 127.0.0.1 while still refusing genuine public origins.
func isLoopbackOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	if loopbackHosts[host] {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !ip.IsLoopback() {
			return false
		}
	}
	return true
}

func init() {
	devSeedCmd := &cobra.Command{
		Use:   "dev-seed",
		Short: "Seed the dev database with example providers, accounts, and invitations (dev-only)",
		Long: `Populate the dev database with representative example data so the frontend
SPA's data-driven elements render during design work.

Seeds:
  - 3 upstream IdP providers (Google, GitHub, Microsoft) — federation buttons
  - 4 example accounts (alice, bob, carol, dave) — admin Accounts list
  - 1 OIDC client + 1 SAML service provider — admin OIDC/SAML lists & detail
  - 2 forward-auth apps with scope vocabularies — PAT scope picker + admin editor
  - 2 personal access tokens for alice — token list + admin account PATs card
  - 2 pending invitations (only if none already exist) — Invitations list

All operations are idempotent; re-running skips existing rows.

SAFETY: refuses to run unless config.PublicOrigins[0] resolves to a loopback
host (localhost / 127.0.0.1 / ::1).`,
		Run: runDevSeed,
	}
	// Registration happens in main() after cli is constructed; we store the
	// command here and register it from main via addDevSeedCmd().
	_devSeedCmd = devSeedCmd
}

// _devSeedCmd is the cobra command created by init(); main() registers it.
var _devSeedCmd *cobra.Command

// addDevSeedCmd registers the dev-seed subcommand on the root cobra command.
// Called from main() after cli is constructed.
func addDevSeedCmd(root *cobra.Command) {
	root.AddCommand(_devSeedCmd)
}

func runDevSeed(_ *cobra.Command, _ []string) {
	ctx := context.Background()

	config, err := configx.Parse()
	if err != nil {
		log.Fatalf("parse config: %v", err)
	}
	if len(config.PublicOrigins) == 0 {
		log.Fatalf("PROHIBITORUM_PUBLIC_ORIGIN is not set")
	}

	// Dev guard: only loopback origins allowed (incl. DNS names pinned to loopback).
	origin := config.PublicOrigins[0]
	if !isLoopbackOrigin(origin) {
		log.Fatalf("dev-seed refuses to run against a non-loopback origin (%s); set PROHIBITORUM_PUBLIC_ORIGIN to a loopback host", origin)
	}

	if _, err := migrations.UpWithResult(config.DatabaseURL); err != nil {
		log.Fatalf("apply migrations: %v", err)
	}
	conn, err := pgxpool.New(ctx, config.DatabaseURL)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer conn.Close()
	q := db.New(conn)

	fmt.Println("==> dev-seed: seeding upstream IdP providers")
	seedProviders(ctx, q)

	fmt.Println("==> dev-seed: seeding example accounts")
	seedAccounts(ctx, q)

	fmt.Println("==> dev-seed: seeding OIDC client")
	seedOIDCClient(ctx, q)

	fmt.Println("==> dev-seed: seeding SAML service provider")
	seedSAMLSP(ctx, q)

	fmt.Println("==> dev-seed: seeding forward-auth apps")
	seedForwardAuthApps(ctx, q)

	fmt.Println("==> dev-seed: seeding personal access tokens")
	seedTokens(ctx, q)

	fmt.Println("==> dev-seed: seeding invitations")
	seedInvitations(ctx, q, origin)

	fmt.Println("==> dev-seed: done")
}

// seedProviders inserts 3 disabled example providers, skipping existing rows.
func seedProviders(ctx context.Context, q *db.Queries) {
	oidcConfig := func(issuerURL, clientID string, scopes, allowedDomains []string, usernameClaim string, requireVerifiedEmail bool) []byte {
		raw, err := json.Marshal(federationoidc.Config{
			IssuerURL: issuerURL, ClientID: clientID, Scopes: scopes, AllowedDomains: allowedDomains,
			UsernameClaim: usernameClaim, DisplayNameClaim: "name", EmailClaim: "email",
			PictureClaim: "picture", RequireVerifiedEmail: requireVerifiedEmail,
		})
		if err != nil {
			log.Fatalf("encode provider config: %v", err)
		}
		return raw
	}
	providers := []db.InsertUpstreamIDPParams{
		{
			Slug: "google", DisplayName: "Google", Protocol: federationoidc.Protocol,
			Mode: "auto_provision", SecretStatus: "unconfigured", Disabled: true,
			ProviderConfig: oidcConfig(
				"https://accounts.google.com", "dev-google", []string{"openid", "email", "profile"},
				[]string{"example.com"}, "email", true,
			),
		},
		{
			Slug: "github", DisplayName: "GitHub", Protocol: federationoidc.Protocol,
			Mode: "link_only", SecretStatus: "unconfigured", Disabled: true,
			ProviderConfig: oidcConfig(
				"https://github.com", "dev-github", []string{"openid", "email"},
				[]string{"example.com"}, "preferred_username", false,
			),
		},
		{
			Slug: "microsoft", DisplayName: "Microsoft", Protocol: federationoidc.Protocol,
			Mode: "invite_only", SecretStatus: "unconfigured", Disabled: true,
			ProviderConfig: oidcConfig(
				"https://login.microsoftonline.com/common/v2.0", "dev-microsoft",
				[]string{"openid", "email", "profile"}, []string{"example.com"}, "email", false,
			),
		},
	}

	for _, p := range providers {
		_, err := q.GetUpstreamIDPBySlug(ctx, p.Slug)
		if err == nil {
			fmt.Printf("    skip provider %q (already exists)\n", p.Slug)
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			log.Fatalf("check provider %q: %v", p.Slug, err)
		}
		if _, err := q.InsertUpstreamIDP(ctx, p); err != nil {
			log.Fatalf("insert provider %q: %v", p.Slug, err)
		}
		fmt.Printf("    inserted provider %q (%s, mode=%s)\n", p.Slug, p.DisplayName, p.Mode)
	}
}

// seedAccounts inserts 4 example accounts, skipping any that already exist.
func seedAccounts(ctx context.Context, q *db.Queries) {
	type acctSpec struct {
		username    string
		displayName string
		role        string
		disabled    bool
	}
	specs := []acctSpec{
		{"alice", "Alice Anderson", "user", false},
		{"bob", "Bob Brown", "user", false},
		{"carol", "Carol Clark", "user", false},
		{"dave", "Dave Davis", "user", true},
	}

	for _, s := range specs {
		_, err := q.GetAccountByUsername(ctx, s.username)
		if err == nil {
			fmt.Printf("    skip account %q (already exists)\n", s.username)
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			log.Fatalf("check account %q: %v", s.username, err)
		}
		handle := make([]byte, 16)
		if _, err := rand.Read(handle); err != nil {
			log.Fatalf("generate user handle for %q: %v", s.username, err)
		}
		if _, err := q.InsertAccount(ctx, db.InsertAccountParams{
			Username:           s.username,
			DisplayName:        s.displayName,
			WebauthnUserHandle: handle,
			Role:               s.role,
			Attributes:         []byte("{}"),
			Disabled:           s.disabled,
		}); err != nil {
			log.Fatalf("insert account %q: %v", s.username, err)
		}
		disabledStr := ""
		if s.disabled {
			disabledStr = " [disabled]"
		}
		fmt.Printf("    inserted account %q (%s, role=%s%s)\n", s.username, s.displayName, s.role, disabledStr)
	}
}

// seedOIDCClient registers one standard (redirect-based) OIDC client so the
// admin OIDC clients list and detail views render with data. Skips if it exists.
func seedOIDCClient(ctx context.Context, q *db.Queries) {
	const clientID = "dev-app"
	_, err := q.GetOIDCClient(ctx, clientID)
	if err == nil {
		fmt.Printf("    skip OIDC client %q (already exists)\n", clientID)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		log.Fatalf("check OIDC client %q: %v", clientID, err)
	}
	params, _, err := oidc.BuildClientParams(oidc.ClientOptions{
		ClientID:       clientID,
		DisplayName:    "Example App (dev)",
		RedirectURIs:   []string{"https://app.localhost/callback"},
		Scopes:         []string{"openid", "profile", "email"},
		Public:         true,
		RequireConsent: true,
	})
	if err != nil {
		log.Fatalf("build OIDC client params: %v", err)
	}
	if _, err := q.InsertOIDCClient(ctx, params); err != nil {
		log.Fatalf("insert OIDC client %q: %v", clientID, err)
	}
	fmt.Printf("    inserted OIDC client %q (Example App (dev))\n", clientID)
}

// seedSAMLSP registers one SAML service provider so the admin SAML list and
// detail views render with data. Skips if the entity ID already exists.
func seedSAMLSP(ctx context.Context, q *db.Queries) {
	const entityID = "https://sp.localhost/saml/metadata"
	_, err := q.GetSAMLSPByEntityID(ctx, entityID)
	if err == nil {
		fmt.Printf("    skip SAML SP %q (already exists)\n", entityID)
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		log.Fatalf("check SAML SP %q: %v", entityID, err)
	}
	if _, err := q.InsertSAMLSP(ctx, db.InsertSAMLSPParams{
		EntityID:                  entityID,
		DisplayName:               "Example SAML App (dev)",
		SpKind:                    pgtype.Text{String: "manual", Valid: true},
		NameIDFormat:              "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent",
		AttributeMap:              []byte("[]"),
		RequireSignedAuthnRequest: false,
		AllowIdpInitiated:         true,
	}); err != nil {
		log.Fatalf("insert SAML SP %q: %v", entityID, err)
	}
	fmt.Printf("    inserted SAML SP %q (Example SAML App (dev))\n", entityID)
}

// faScope is one entry in a forward-auth app's admin-defined scope vocabulary,
// matching the JSONB shape stored in oidc_client.forward_auth_scopes.
type faScope struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// faApp describes a forward-auth demo app: a normal oidc_client flagged for
// forward-auth, carrying a scope vocabulary. access_restricted stays false so
// every account can target it when minting a PAT.
type faApp struct {
	clientID    string
	host        string
	displayName string
	scopes      []faScope
}

// seedForwardAuthApps registers a couple of forward-auth demo apps so the PAT
// scope picker and the admin forward-auth scope-vocabulary editor render with
// data. Skips any client_id that already exists.
func seedForwardAuthApps(ctx context.Context, q *db.Queries) {
	apps := []faApp{
		{
			clientID:    "dev-grafana",
			host:        "grafana.localhost",
			displayName: "Grafana (dev)",
			scopes: []faScope{
				{"read", "View dashboards and metrics"},
				{"write", "Create and edit dashboards"},
				{"admin", "Manage organisation settings"},
			},
		},
		{
			clientID:    "dev-prometheus",
			host:        "prometheus.localhost",
			displayName: "Prometheus (dev)",
			scopes: []faScope{
				{"read", "Query metrics"},
				{"admin", "Manage alerting rules"},
			},
		},
	}
	for _, a := range apps {
		_, err := q.GetOIDCClient(ctx, a.clientID)
		if err == nil {
			fmt.Printf("    skip forward-auth app %q (already exists)\n", a.clientID)
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			log.Fatalf("check forward-auth app %q: %v", a.clientID, err)
		}
		if _, err := oidc.RegisterForwardAuthApp(ctx, q, a.clientID, a.host, a.displayName); err != nil {
			log.Fatalf("register forward-auth app %q: %v", a.clientID, err)
		}
		scopesJSON, err := json.Marshal(a.scopes)
		if err != nil {
			log.Fatalf("marshal scopes for %q: %v", a.clientID, err)
		}
		if err := q.SetForwardAuthScopes(ctx, db.SetForwardAuthScopesParams{
			ClientID:          a.clientID,
			ForwardAuthScopes: scopesJSON,
		}); err != nil {
			log.Fatalf("set forward-auth scopes for %q: %v", a.clientID, err)
		}
		fmt.Printf("    inserted forward-auth app %q (%s, host=%s, %d scopes)\n", a.clientID, a.displayName, a.host, len(a.scopes))
	}
}

// seedTokens issues a couple of Personal Access Tokens for alice so the user's
// token list and the admin account-detail PATs card render with data. The raw
// token is discarded (PATs are shown only once, at creation); only the hash and
// a non-secret hint are stored. Skips entirely if alice already has any PAT.
func seedTokens(ctx context.Context, q *db.Queries) {
	alice, err := q.GetAccountByUsername(ctx, "alice")
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			fmt.Println("    skip tokens (account \"alice\" not seeded)")
			return
		}
		log.Fatalf("look up account \"alice\": %v", err)
	}
	existing, err := q.ListPATsByAccount(ctx, alice.ID)
	if err != nil {
		log.Fatalf("list PATs for alice: %v", err)
	}
	if len(existing) > 0 {
		fmt.Printf("    skip tokens (alice already has %d)\n", len(existing))
		return
	}

	type patSpec struct {
		name      string
		allApps   bool
		appGrants map[string][]string
		expires   bool
	}
	specs := []patSpec{
		{name: "ci-deploy", allApps: false, appGrants: map[string][]string{"dev-grafana": {"read", "write"}}, expires: true},
		{name: "metrics-readonly", allApps: true, appGrants: map[string][]string{}, expires: false},
	}
	for _, s := range specs {
		_, hash, hint, err := pat.Generate()
		if err != nil {
			log.Fatalf("generate PAT %q: %v", s.name, err)
		}
		grantsJSON, err := json.Marshal(s.appGrants)
		if err != nil {
			log.Fatalf("marshal app_grants for PAT %q: %v", s.name, err)
		}
		var expires pgtype.Timestamptz
		if s.expires {
			expires = pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, 90), Valid: true}
		}
		if _, err := q.InsertPAT(ctx, db.InsertPATParams{
			AccountID: alice.ID,
			Name:      s.name,
			TokenHash: hash,
			TokenHint: hint,
			AllApps:   s.allApps,
			AppGrants: grantsJSON,
			ExpiresAt: expires,
		}); err != nil {
			log.Fatalf("insert PAT %q: %v", s.name, err)
		}
		fmt.Printf("    inserted PAT %q for alice (allApps=%v)\n", s.name, s.allApps)
	}
}

// seedInvitations issues 2 pending invitations, but only if there are currently
// zero outstanding invitations (idempotent across re-runs).
func seedInvitations(ctx context.Context, q *db.Queries, origin string) {
	existing, err := q.ListPendingInvitations(ctx, db.ListPendingInvitationsParams{Limit: 10000})
	if err != nil {
		log.Fatalf("list pending invitations: %v", err)
	}
	if len(existing) > 0 {
		fmt.Printf("    skip invitations (%d already pending)\n", len(existing))
		return
	}

	for i := 0; i < 2; i++ {
		token, exp, err := enrollment.IssueEnrollment(
			ctx, q,
			enrollment.IntentInvite,
			nil,
			0,
			&enrollment.EnrollmentTemplate{Role: "user"},
		)
		if err != nil {
			log.Fatalf("issue invitation %d: %v", i+1, err)
		}
		fmt.Printf("    issued invitation (expires %s):\n      %s/enroll/%s\n", exp.Format("2006-01-02T15:04:05Z"), origin, token)
	}
}
