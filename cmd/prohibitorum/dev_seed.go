package main

// dev-seed: populate the dev database with representative example data so the
// frontend SPA's data-driven elements render during design work.
//
// Dev-only: refuses to run unless config.PublicOrigins[0] is a loopback host.

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"net/url"

	"prohibitorum/db/migrations"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/credential/enrollment"
	"prohibitorum/pkg/db"

	"github.com/jackc/pgx/v5"
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

func init() {
	devSeedCmd := &cobra.Command{
		Use:   "dev-seed",
		Short: "Seed the dev database with example providers, accounts, and invitations (dev-only)",
		Long: `Populate the dev database with representative example data so the frontend
SPA's data-driven elements render during design work.

Seeds:
  - 3 upstream IdP providers (Google, GitHub, Microsoft) — federation buttons
  - 4 example accounts (alice, bob, carol, dave) — admin Accounts list
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

	// Dev guard: only loopback origins allowed.
	origin := config.PublicOrigins[0]
	u, err := url.Parse(origin)
	if err != nil || !loopbackHosts[u.Hostname()] {
		log.Fatalf("dev-seed refuses to run against a non-localhost origin (%s); set PROHIBITORUM_PUBLIC_ORIGIN to http://localhost:…", origin)
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

	fmt.Println("==> dev-seed: seeding invitations")
	seedInvitations(ctx, q, origin)

	fmt.Println("==> dev-seed: done")
}

// seedProviders inserts 3 upstream IdP rows, skipping any that already exist.
func seedProviders(ctx context.Context, q *db.Queries) {
	providers := []db.InsertUpstreamIDPParams{
		{
			Slug:                 "google",
			DisplayName:          "Google",
			IssuerUrl:            "https://accounts.google.com",
			ClientID:             "dev-google",
			ClientSecretEnc:      []byte("dev-seed-placeholder"),
			SecretNonce:          make([]byte, 12),
			KeyVersion:           1,
			Scopes:               []string{"openid", "email", "profile"},
			Mode:                 "auto_provision",
			AllowedDomains:       []string{"example.com"},
			UsernameClaim:        "email",
			DisplayNameClaim:     "name",
			EmailClaim:           "email",
			RequireVerifiedEmail: true,
		},
		{
			Slug:                 "github",
			DisplayName:          "GitHub",
			IssuerUrl:            "https://github.com",
			ClientID:             "dev-github",
			ClientSecretEnc:      []byte("dev-seed-placeholder"),
			SecretNonce:          make([]byte, 12),
			KeyVersion:           1,
			Scopes:               []string{"openid", "email"},
			Mode:                 "link_only",
			AllowedDomains:       []string{"example.com"},
			UsernameClaim:        "preferred_username",
			DisplayNameClaim:     "name",
			EmailClaim:           "email",
			RequireVerifiedEmail: false,
		},
		{
			Slug:                 "microsoft",
			DisplayName:          "Microsoft",
			IssuerUrl:            "https://login.microsoftonline.com/common/v2.0",
			ClientID:             "dev-microsoft",
			ClientSecretEnc:      []byte("dev-seed-placeholder"),
			SecretNonce:          make([]byte, 12),
			KeyVersion:           1,
			Scopes:               []string{"openid", "email", "profile"},
			Mode:                 "invite_only",
			AllowedDomains:       []string{"example.com"},
			UsernameClaim:        "email",
			DisplayNameClaim:     "name",
			EmailClaim:           "email",
			RequireVerifiedEmail: false,
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

// seedInvitations issues 2 pending invitations, but only if there are currently
// zero outstanding invitations (idempotent across re-runs).
func seedInvitations(ctx context.Context, q *db.Queries, origin string) {
	existing, err := q.ListPendingInvitations(ctx)
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
