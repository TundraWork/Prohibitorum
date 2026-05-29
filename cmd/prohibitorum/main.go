package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"prohibitorum/db/migrations"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/credential/enrollment"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
	"prohibitorum/pkg/protocol/oidc"
	"prohibitorum/pkg/server"

	"github.com/danielgtaylor/huma/v2/humacli"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type Options struct{}

func main() {
	cli := humacli.New(func(h humacli.Hooks, _ *Options) {
		h.OnStart(func() {
			ctx := context.Background()
			s, err := server.NewServer(ctx)
			if err != nil {
				log.Fatalf("create server: %v", err)
			}
			if err := s.Serve(); err != nil {
				log.Fatalf("serve: %v", err)
			}
		})
	})

	cli.Root().AddCommand(&cobra.Command{
		Use:   "openapi",
		Short: "Print the OpenAPI spec to stdout",
		Run: func(_ *cobra.Command, _ []string) {
			api := server.NewHuma()
			b, _ := api.OpenAPI().DowngradeYAML()
			fmt.Println(string(b))
		},
	})

	var (
		enrollNew      bool
		enrollReset    bool
		enrollUsername string
	)
	enrollCmd := &cobra.Command{
		Use:   "enroll-admin",
		Short: "Issue a passkey enrollment URL for an admin account",
		Long: `Issue a one-time enrollment URL the operator opens in a browser to register
a passkey for an admin account.

Default behavior errors if an admin already exists; pass --new to add another
admin or --reset --username NAME to recover a specific existing admin.`,
		Run: func(_ *cobra.Command, _ []string) {
			ctx := context.Background()
			config, err := configx.Parse()
			if err != nil {
				log.Fatalf("parse config: %v", err)
			}
			if len(config.PublicOrigins) == 0 {
				log.Fatalf("PROHIBITORUM_PUBLIC_ORIGIN is not set; the enrollment URL needs a base origin")
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

			var (
				token, label string
				exp          time.Time
			)
			switch {
			case enrollReset:
				if enrollUsername == "" {
					log.Fatalf("--reset requires --username NAME")
				}
				a, err := q.GetAccountByUsername(ctx, enrollUsername)
				if err != nil {
					if errors.Is(err, pgx.ErrNoRows) {
						log.Fatalf("user %q not found", enrollUsername)
					}
					log.Fatalf("look up user: %v", err)
				}
				if a.Role != "admin" {
					log.Fatalf("user %q has role %q; --reset only handles admin accounts", a.Username, a.Role)
				}
				id := a.ID
				token, exp, err = enrollment.IssueEnrollment(ctx, q, enrollment.IntentReset, &id, enrollment.DefaultEnrollmentTTL, nil)
				if err != nil {
					log.Fatalf("issue enrollment: %v", err)
				}
				label = fmt.Sprintf("Reset enrollment for %q", a.Username)
			case enrollNew:
				token, exp, err = enrollment.IssueEnrollment(ctx, q, enrollment.IntentBootstrap, nil, enrollment.DefaultEnrollmentTTL, nil)
				if err != nil {
					log.Fatalf("issue enrollment: %v", err)
				}
				label = "Additional admin enrollment"
			default:
				has, err := q.HasAnyActiveAdmin(ctx)
				if err != nil {
					log.Fatalf("status check: %v", err)
				}
				if has {
					log.Fatalf("an admin already exists. Pass --new to add another admin, or --reset --username NAME to recover a specific admin.")
				}
				token, exp, err = enrollment.IssueEnrollment(ctx, q, enrollment.IntentBootstrap, nil, enrollment.DefaultEnrollmentTTL, nil)
				if err != nil {
					log.Fatalf("issue enrollment: %v", err)
				}
				label = "Bootstrap admin enrollment"
			}

			logx.WithContext(ctx).WithFields(logrus.Fields{
				"event":  "auth.enrollment_issued",
				"source": "cli",
			}).Info("auth")

			url := config.PublicOrigins[0] + "/enroll/" + token
			fmt.Printf("%s URL (expires %s):\n%s\n", label, exp.Format(time.RFC3339), url)
		},
	}
	enrollCmd.Flags().BoolVar(&enrollNew, "new", false, "Always create a new admin enrollment.")
	enrollCmd.Flags().BoolVar(&enrollReset, "reset", false, "Issue a reset enrollment (with --username).")
	enrollCmd.Flags().StringVar(&enrollUsername, "username", "", "Target username for --reset.")
	cli.Root().AddCommand(enrollCmd)

	signingKeyCmd := &cobra.Command{
		Use:   "signing-key",
		Short: "Manage OIDC signing keys",
	}

	var (
		signingActivate bool
		signingRetire   string
	)
	generateCmd := &cobra.Command{
		Use:   "generate",
		Short: "Mint a new RSA-2048 OIDC signing key",
		Long: `Mint a new RSA-2048 OIDC signing key (JWK + self-signed x509 + PKCS#8 PEM)
and store it in the signing_key table.

The first ever signing key, or any key generated with --activate, becomes the
active key and deactivates any previously active key in the same transaction.

Pass --retire KID to retire an existing key (stamps retired_at) without
generating a new one.`,
		Run: func(_ *cobra.Command, _ []string) {
			ctx := context.Background()
			config, err := configx.Parse()
			if err != nil {
				log.Fatalf("parse config: %v", err)
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

			// Retire mode: retire the named kid and exit, no new key.
			if signingRetire != "" {
				if err := q.RetireSigningKey(ctx, signingRetire); err != nil {
					log.Fatalf("retire signing key: %v", err)
				}
				fmt.Printf("Retired signing key %s\n", signingRetire)
				return
			}

			params, err := oidc.GenerateSigningKey()
			if err != nil {
				log.Fatalf("generate signing key: %v", err)
			}

			// Decide activation: explicit --activate, or no active key exists yet.
			activate := signingActivate
			if !activate {
				_, err := q.GetActiveSigningKey(ctx)
				switch {
				case errors.Is(err, pgx.ErrNoRows):
					activate = true
				case err != nil:
					log.Fatalf("check active signing key: %v", err)
				}
			}

			tx, err := conn.Begin(ctx)
			if err != nil {
				log.Fatalf("begin tx: %v", err)
			}
			defer tx.Rollback(ctx) //nolint:errcheck
			qtx := q.WithTx(tx)

			if activate {
				if err := qtx.DeactivateSigningKeys(ctx); err != nil {
					log.Fatalf("deactivate signing keys: %v", err)
				}
				params.Active = true
			}
			if _, err := qtx.InsertSigningKey(ctx, params); err != nil {
				log.Fatalf("insert signing key: %v", err)
			}
			if err := tx.Commit(ctx); err != nil {
				log.Fatalf("commit tx: %v", err)
			}

			state := "inactive"
			if activate {
				state = "active"
			}
			fmt.Printf("Generated signing key %s (%s)\n", params.Kid, state)
		},
	}
	generateCmd.Flags().BoolVar(&signingActivate, "activate", false, "Make the new key active, deactivating any prior active key.")
	generateCmd.Flags().StringVar(&signingRetire, "retire", "", "Retire the signing key with this kid (no new key is generated).")
	signingKeyCmd.AddCommand(generateCmd)
	cli.Root().AddCommand(signingKeyCmd)

	oidcClientCmd := &cobra.Command{
		Use:   "oidc-client",
		Short: "Manage OIDC clients (relying parties)",
	}

	var (
		clientID             string
		clientDisplayName    string
		clientRedirectURIs   []string
		clientPostLogoutURIs []string
		clientScopes         []string
		clientPublic         bool
		clientRequireConsent bool
	)
	createClientCmd := &cobra.Command{
		Use:   "create",
		Short: "Register a new OIDC client",
		Long: `Register a new OIDC client (relying party).

Confidential clients (the default) get a freshly generated secret that is
printed exactly once; only its argon2id hash is stored. Pass --public for a
client with no secret (token_endpoint_auth_method = "none"). PKCE is required
for every client.`,
		Run: func(_ *cobra.Command, _ []string) {
			ctx := context.Background()
			config, err := configx.Parse()
			if err != nil {
				log.Fatalf("parse config: %v", err)
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

			params, secret, err := oidc.BuildClientParams(oidc.ClientOptions{
				ClientID:               clientID,
				DisplayName:            clientDisplayName,
				RedirectURIs:           clientRedirectURIs,
				PostLogoutRedirectURIs: clientPostLogoutURIs,
				Scopes:                 clientScopes,
				Public:                 clientPublic,
				RequireConsent:         clientRequireConsent,
			})
			if err != nil {
				log.Fatalf("build client params: %v", err)
			}

			if _, err := q.InsertOIDCClient(ctx, params); err != nil {
				log.Fatalf("insert oidc client: %v", err)
			}

			if clientPublic {
				fmt.Printf("Registered public client %q (no secret; token_endpoint_auth_method=none)\n", params.ClientID)
				return
			}
			fmt.Printf("Registered confidential client %q\n", params.ClientID)
			fmt.Printf("Client secret (store this now, it will NOT be shown again):\n%s\n", secret)
		},
	}
	createClientCmd.Flags().StringVar(&clientID, "client-id", "", "Stable client identifier (required).")
	createClientCmd.Flags().StringVar(&clientDisplayName, "display-name", "", "Human-readable client name.")
	createClientCmd.Flags().StringArrayVar(&clientRedirectURIs, "redirect-uri", nil, "Allowed redirect URI (repeatable, at least one required).")
	createClientCmd.Flags().StringArrayVar(&clientPostLogoutURIs, "post-logout-redirect-uri", nil, "Allowed post-logout redirect URI (repeatable).")
	createClientCmd.Flags().StringArrayVar(&clientScopes, "scope", nil, "Allowed scope (repeatable; defaults to openid,profile).")
	createClientCmd.Flags().BoolVar(&clientPublic, "public", false, "Register a public client with no secret (auth method none).")
	createClientCmd.Flags().BoolVar(&clientRequireConsent, "require-consent", false, "Require a consent screen for this client.")
	oidcClientCmd.AddCommand(createClientCmd)

	listClientCmd := &cobra.Command{
		Use:   "list",
		Short: "List registered OIDC clients",
		Run: func(_ *cobra.Command, _ []string) {
			ctx := context.Background()
			config, err := configx.Parse()
			if err != nil {
				log.Fatalf("parse config: %v", err)
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

			clients, err := q.ListOIDCClients(ctx)
			if err != nil {
				log.Fatalf("list oidc clients: %v", err)
			}
			if len(clients) == 0 {
				fmt.Println("No OIDC clients registered.")
				return
			}
			fmt.Printf("%-32s %-32s %-24s %s\n", "CLIENT_ID", "DISPLAY_NAME", "AUTH_METHOD", "DISABLED")
			for _, c := range clients {
				fmt.Printf("%-32s %-32s %-24s %t\n", c.ClientID, c.DisplayName, c.TokenEndpointAuthMethod, c.Disabled)
			}
		},
	}
	oidcClientCmd.AddCommand(listClientCmd)
	cli.Root().AddCommand(oidcClientCmd)

	cli.Run()
}
