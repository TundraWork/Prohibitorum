package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"prohibitorum/db/migrations"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/credential/enrollment"
	"prohibitorum/pkg/db"
	fedoidc "prohibitorum/pkg/federation/oidc"
	"prohibitorum/pkg/logx"
	"prohibitorum/pkg/protocol/oidc"
	"prohibitorum/pkg/protocol/saml"
	"prohibitorum/pkg/server"

	"github.com/danielgtaylor/huma/v2/humacli"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
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

			grace := config.SAML.MetadataRotationGrace

			// Retire mode: move the named kid toward retirement and exit, no
			// new key. Retiring the active key is refused — activate a
			// replacement first.
			if signingRetire != "" {
				if _, err := oidc.RetireSigningKey(ctx, q, signingRetire, grace); err != nil {
					if errors.Is(err, oidc.ErrActiveKeyNoReplacement) {
						log.Fatalf("retire signing key: %v (activate a replacement first)", err)
					}
					log.Fatalf("retire signing key: %v", err)
				}
				fmt.Printf("Retired signing key %s (decommissioning until %s)\n",
					signingRetire, time.Now().Add(grace).Format(time.RFC3339))
				return
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

			// Always insert as pending first; activation is an explicit promote.
			// Seal the private key at rest with the current DEK version.
			keyVer, dek := mustCurrentDEK()
			pending, err := oidc.InsertPendingKey(ctx, q, dek, keyVer)
			if err != nil {
				log.Fatalf("insert pending signing key: %v", err)
			}

			state := "pending"
			if activate {
				if _, err := oidc.ActivateSigningKey(ctx, conn, q, pending.Kid, grace); err != nil {
					log.Fatalf("activate signing key: %v", err)
				}
				state = "active"
			}
			fmt.Printf("Generated signing key %s (%s)\n", pending.Kid, state)
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

	// oidc-client update — full-replace of the mutable config fields, mirroring
	// the PUT /oidc-clients/{clientId} handler (db.UpdateOIDCClient). The secret
	// is NOT touched here; use `rotate-secret`.
	var (
		updClientID           string
		updClientDisplayName  string
		updClientRedirectURIs []string
		updClientPostLogout   []string
		updClientScopes       []string
		updClientReqConsent   bool
		updClientDisabled     bool
	)
	updateClientCmd := &cobra.Command{
		Use:   "update",
		Short: "Replace an OIDC client's mutable config (full-replace; secret untouched)",
		Long: `Replace the mutable configuration of an existing OIDC client.

This is a FULL-REPLACE (mirrors the PUT admin endpoint): supply the complete
desired displayName, redirect URIs, scopes, requireConsent and disabled state.
At least one --redirect-uri is required. The client secret is not affected.`,
		Run: func(_ *cobra.Command, _ []string) {
			ctx := context.Background()
			q, conn := mustOpenDB(ctx)
			defer conn.Close()

			if updClientID == "" {
				log.Fatalf("--client-id is required")
			}
			if len(updClientRedirectURIs) == 0 {
				log.Fatalf("at least one --redirect-uri is required (full-replace update)")
			}
			postLogout := updClientPostLogout
			if postLogout == nil {
				postLogout = []string{}
			}
			scopes := updClientScopes
			if len(scopes) == 0 {
				scopes = []string{"openid", "profile"}
			}
			updated, err := q.UpdateOIDCClient(ctx, db.UpdateOIDCClientParams{
				ClientID:               updClientID,
				DisplayName:            updClientDisplayName,
				RedirectUris:           updClientRedirectURIs,
				PostLogoutRedirectUris: postLogout,
				AllowedScopes:          scopes,
				RequireConsent:         updClientReqConsent,
				Disabled:               updClientDisabled,
			})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					log.Fatalf("oidc-client update: client %q not found", updClientID)
				}
				log.Fatalf("oidc-client update: %v", err)
			}
			fmt.Printf("Updated OIDC client %q (displayName=%q, %d redirect URI(s), disabled=%t)\n",
				updated.ClientID, updated.DisplayName, len(updated.RedirectUris), updated.Disabled)
		},
	}
	updateClientCmd.Flags().StringVar(&updClientID, "client-id", "", "Client to update (required).")
	updateClientCmd.Flags().StringVar(&updClientDisplayName, "display-name", "", "New human-readable client name.")
	updateClientCmd.Flags().StringArrayVar(&updClientRedirectURIs, "redirect-uri", nil, "Allowed redirect URI (repeatable, at least one required).")
	updateClientCmd.Flags().StringArrayVar(&updClientPostLogout, "post-logout-redirect-uri", nil, "Allowed post-logout redirect URI (repeatable).")
	updateClientCmd.Flags().StringArrayVar(&updClientScopes, "scope", nil, "Allowed scope (repeatable; defaults to openid,profile).")
	updateClientCmd.Flags().BoolVar(&updClientReqConsent, "require-consent", false, "Require a consent screen for this client.")
	updateClientCmd.Flags().BoolVar(&updClientDisabled, "disabled", false, "Disable the client (refuse new authorizations).")
	oidcClientCmd.AddCommand(updateClientCmd)

	// oidc-client rotate-secret — generates a new secret, stores only its hash,
	// prints the cleartext once (mirrors oidc.RotateClientSecret).
	var rotateClientID string
	rotateClientCmd := &cobra.Command{
		Use:   "rotate-secret",
		Short: "Rotate a confidential OIDC client's secret (prints the new secret once)",
		Run: func(_ *cobra.Command, _ []string) {
			ctx := context.Background()
			q, conn := mustOpenDB(ctx)
			defer conn.Close()

			if rotateClientID == "" {
				log.Fatalf("--client-id is required")
			}
			if _, err := q.GetOIDCClientAny(ctx, rotateClientID); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					log.Fatalf("oidc-client rotate-secret: client %q not found", rotateClientID)
				}
				log.Fatalf("oidc-client rotate-secret: lookup: %v", err)
			}
			secret, err := oidc.RotateClientSecret(ctx, q, rotateClientID)
			if err != nil {
				log.Fatalf("oidc-client rotate-secret: %v", err)
			}
			fmt.Printf("Rotated secret for OIDC client %q\n", rotateClientID)
			fmt.Printf("New client secret (store this now, it will NOT be shown again):\n%s\n", secret)
		},
	}
	rotateClientCmd.Flags().StringVar(&rotateClientID, "client-id", "", "Client whose secret to rotate (required).")
	oidcClientCmd.AddCommand(rotateClientCmd)

	// oidc-client delete — hard-delete (mirrors db.DeleteOIDCClient). Destructive,
	// so it requires --yes.
	var (
		delClientID  string
		delClientYes bool
	)
	deleteClientCmd := &cobra.Command{
		Use:   "delete",
		Short: "Hard-delete an OIDC client (requires --yes)",
		Run: func(_ *cobra.Command, _ []string) {
			ctx := context.Background()
			if delClientID == "" {
				log.Fatalf("--client-id is required")
			}
			if !delClientYes {
				log.Fatalf("refusing to delete OIDC client %q without --yes", delClientID)
			}
			q, conn := mustOpenDB(ctx)
			defer conn.Close()

			rows, err := q.DeleteOIDCClient(ctx, delClientID)
			if err != nil {
				log.Fatalf("oidc-client delete: %v", err)
			}
			if rows == 0 {
				log.Fatalf("oidc-client delete: client %q not found", delClientID)
			}
			fmt.Printf("Deleted OIDC client %q\n", delClientID)
		},
	}
	deleteClientCmd.Flags().StringVar(&delClientID, "client-id", "", "Client to delete (required).")
	deleteClientCmd.Flags().BoolVar(&delClientYes, "yes", false, "Confirm the destructive delete.")
	oidcClientCmd.AddCommand(deleteClientCmd)

	cli.Root().AddCommand(oidcClientCmd)

	samlSPCmd := &cobra.Command{
		Use:   "saml-sp",
		Short: "Manage SAML service providers (relying parties)",
	}

	var (
		spMetadataFile  string
		spMetadataURL   string
		spEntityID      string
		spDisplayName   string
		spKind          string
		spNameIDFormat  string
		spRequireSigned bool
		spIdpInitiated  bool
		spACSURL        string
		spACSBinding    string
	)
	createSPCmd := &cobra.Command{
		Use:   "create",
		Short: "Register a new SAML service provider",
		Long: `Register a new SAML service provider (relying party).

Supply the SP's metadata with --metadata-file or --metadata-url to auto-populate
the entity_id, AssertionConsumerService endpoints, and signing certificates.
Without metadata, set --entity-id and at least one --acs-url manually.

--kind ghes installs the GitHub Enterprise Server attribute profile and forces
signed AuthnRequests. Explicit flags (--entity-id, --display-name,
--name-id-format) override values parsed from metadata.`,
		Run: func(cmd *cobra.Command, _ []string) {
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

			var metadataXML []byte
			switch {
			case spMetadataFile != "":
				metadataXML, err = os.ReadFile(spMetadataFile)
				if err != nil {
					log.Fatalf("read metadata file: %v", err)
				}
			case spMetadataURL != "":
				metadataXML, err = fetchMetadata(ctx, spMetadataURL)
				if err != nil {
					log.Fatalf("fetch metadata url: %v", err)
				}
			}

			opts := saml.SPOptions{
				MetadataXML:               metadataXML,
				EntityID:                  spEntityID,
				DisplayName:               spDisplayName,
				Kind:                      spKind,
				NameIDFormat:              spNameIDFormat,
				RequireSignedAuthnRequest: spRequireSigned,
				AllowIdpInitiated:         spIdpInitiated,
			}
			// Manual ACS (no metadata).
			if len(metadataXML) == 0 && spACSURL != "" {
				opts.ManualACS = []saml.SPACSEntry{
					{Binding: spACSBinding, Location: spACSURL, Index: 0, IsDefault: true},
				}
			}
			if len(metadataXML) > 0 && spACSURL != "" {
				log.Printf("warning: --acs-url is ignored when metadata is supplied")
			}

			params, acs, certPEMs, err := saml.BuildSPParams(opts)
			if err != nil {
				log.Fatalf("build sp params: %v", err)
			}

			tx, err := conn.Begin(ctx)
			if err != nil {
				log.Fatalf("begin tx: %v", err)
			}
			defer tx.Rollback(ctx) //nolint:errcheck
			qtx := q.WithTx(tx)

			sp, err := qtx.InsertSAMLSP(ctx, params)
			if err != nil {
				log.Fatalf("insert saml sp: %v", err)
			}
			for _, a := range acs {
				if err := qtx.InsertSAMLSPACS(ctx, db.InsertSAMLSPACSParams{
					SpID:      sp.ID,
					Idx:       int32(a.Index),
					Binding:   a.Binding,
					Location:  a.Location,
					IsDefault: a.IsDefault,
				}); err != nil {
					log.Fatalf("insert saml sp acs: %v", err)
				}
			}
			for _, certPEM := range certPEMs {
				if err := qtx.InsertSAMLSPKey(ctx, db.InsertSAMLSPKeyParams{
					SpID:    sp.ID,
					Use:     "signing",
					CertPem: certPEM,
				}); err != nil {
					log.Fatalf("insert saml sp key: %v", err)
				}
			}
			if err := tx.Commit(ctx); err != nil {
				log.Fatalf("commit tx: %v", err)
			}

			fmt.Printf("Registered SAML SP %q\n", sp.EntityID)
			fmt.Printf("  AssertionConsumerService endpoints: %d\n", len(acs))
			fmt.Printf("  Signing certificates: %d\n", len(certPEMs))
		},
	}
	createSPCmd.Flags().StringVar(&spMetadataFile, "metadata-file", "", "Path to the SP's SAML metadata XML file.")
	createSPCmd.Flags().StringVar(&spMetadataURL, "metadata-url", "", "URL to fetch the SP's SAML metadata XML from.")
	createSPCmd.Flags().StringVar(&spEntityID, "entity-id", "", "SP entity ID (required if no metadata; overrides metadata).")
	createSPCmd.Flags().StringVar(&spDisplayName, "display-name", "", "Human-readable SP name.")
	createSPCmd.Flags().StringVar(&spKind, "kind", "generic", "SP profile: ghes or generic.")
	createSPCmd.Flags().StringVar(&spNameIDFormat, "name-id-format", "", "NameID format URI (defaults to SAML 1.1 persistent).")
	createSPCmd.Flags().BoolVar(&spRequireSigned, "require-signed-authn-request", false, "Require signed AuthnRequests (forced true for --kind ghes).")
	createSPCmd.Flags().BoolVar(&spIdpInitiated, "allow-idp-initiated", false, "Allow IdP-initiated (unsolicited) SSO to this SP (default false).")
	createSPCmd.Flags().StringVar(&spACSURL, "acs-url", "", "Manual AssertionConsumerService URL (when no metadata).")
	createSPCmd.Flags().StringVar(&spACSBinding, "acs-binding", "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST", "Manual ACS binding (when no metadata).")
	samlSPCmd.AddCommand(createSPCmd)

	listSPCmd := &cobra.Command{
		Use:   "list",
		Short: "List registered SAML service providers",
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

			sps, err := q.ListSAMLSPs(ctx)
			if err != nil {
				log.Fatalf("list saml sps: %v", err)
			}
			if len(sps) == 0 {
				fmt.Println("No SAML service providers registered.")
				return
			}
			fmt.Printf("%-64s %-24s %-10s %-5s %s\n", "ENTITY_ID", "DISPLAY_NAME", "KIND", "#ACS", "CREATED_AT")
			for _, sp := range sps {
				acs, err := q.ListSAMLSPACSEndpoints(ctx, sp.ID)
				if err != nil {
					log.Fatalf("list acs for %q: %v", sp.EntityID, err)
				}
				kind := ""
				if sp.SpKind.Valid {
					kind = sp.SpKind.String
				}
				created := ""
				if sp.CreatedAt.Valid {
					created = sp.CreatedAt.Time.Format(time.RFC3339)
				}
				fmt.Printf("%-64s %-24s %-10s %-5d %s\n", sp.EntityID, sp.DisplayName, kind, len(acs), created)
			}
		},
	}
	samlSPCmd.AddCommand(listSPCmd)

	// saml-sp update — update the mutable SP policy fields (mirrors db.UpdateSAMLSP /
	// the PUT handler). The SP is identified by --entity-id (resolved to id);
	// ACS endpoints and certificates are NOT touched (use re-ingest for those).
	var (
		updSPEntityID      string
		updSPDisplayName   string
		updSPNameIDFormat  string
		updSPRequireSigned bool
		updSPIdpInitiated  bool
		updSPSessionSecs   int64
	)
	updateSPCmd := &cobra.Command{
		Use:   "update",
		Short: "Update a SAML SP's policy fields (identified by --entity-id)",
		Long: `Update the mutable policy of a registered SAML service provider:
displayName, NameID format, require-signed-authn-request, allow-idp-initiated,
and optional session lifetime. ACS endpoints and signing certificates are not
modified here.`,
		Run: func(cmd *cobra.Command, _ []string) {
			ctx := context.Background()
			if updSPEntityID == "" {
				log.Fatalf("--entity-id is required")
			}
			if updSPDisplayName == "" {
				log.Fatalf("--display-name is required (full-replace update)")
			}
			q, conn := mustOpenDB(ctx)
			defer conn.Close()

			sp, err := q.GetSAMLSPByEntityID(ctx, updSPEntityID)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					log.Fatalf("saml-sp update: SP %q not found", updSPEntityID)
				}
				log.Fatalf("saml-sp update: lookup: %v", err)
			}

			nameIDFormat := updSPNameIDFormat
			if nameIDFormat == "" {
				nameIDFormat = sp.NameIDFormat
			}

			var sessionLifetime pgtype.Interval
			if updSPSessionSecs > 0 {
				sessionLifetime = pgtype.Interval{Microseconds: updSPSessionSecs * 1_000_000, Valid: true}
			}

			updated, err := q.UpdateSAMLSP(ctx, db.UpdateSAMLSPParams{
				ID:                        sp.ID,
				DisplayName:               updSPDisplayName,
				NameIDFormat:              nameIDFormat,
				RequireSignedAuthnRequest: updSPRequireSigned,
				AllowIdpInitiated:         updSPIdpInitiated,
				SessionLifetime:           sessionLifetime,
				AttributeMap:              sp.AttributeMap,
			})
			if err != nil {
				log.Fatalf("saml-sp update: %v", err)
			}
			fmt.Printf("Updated SAML SP %q (displayName=%q, requireSigned=%t, idpInitiated=%t)\n",
				updated.EntityID, updated.DisplayName, updated.RequireSignedAuthnRequest,
				updated.AllowIdpInitiated)
		},
	}
	updateSPCmd.Flags().StringVar(&updSPEntityID, "entity-id", "", "Entity ID of the SP to update (required).")
	updateSPCmd.Flags().StringVar(&updSPDisplayName, "display-name", "", "New display name (required).")
	updateSPCmd.Flags().StringVar(&updSPNameIDFormat, "name-id-format", "", "NameID format URI (defaults to the existing value).")
	updateSPCmd.Flags().BoolVar(&updSPRequireSigned, "require-signed-authn-request", false, "Require signed AuthnRequests.")
	updateSPCmd.Flags().BoolVar(&updSPIdpInitiated, "allow-idp-initiated", false, "Allow IdP-initiated (unsolicited) SSO.")
	updateSPCmd.Flags().Int64Var(&updSPSessionSecs, "session-lifetime-secs", 0, "Optional SP session lifetime in seconds (0 = server default).")
	samlSPCmd.AddCommand(updateSPCmd)

	// saml-sp delete — hard-delete the SP; ON DELETE CASCADE removes ACS+keys.
	// Destructive, so it requires --yes. Identified by --entity-id.
	var (
		delSPEntityID string
		delSPYes      bool
	)
	deleteSPCmd := &cobra.Command{
		Use:   "delete",
		Short: "Hard-delete a SAML SP and its ACS/keys (requires --yes)",
		Run: func(_ *cobra.Command, _ []string) {
			ctx := context.Background()
			if delSPEntityID == "" {
				log.Fatalf("--entity-id is required")
			}
			if !delSPYes {
				log.Fatalf("refusing to delete SAML SP %q without --yes", delSPEntityID)
			}
			q, conn := mustOpenDB(ctx)
			defer conn.Close()

			sp, err := q.GetSAMLSPByEntityID(ctx, delSPEntityID)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					log.Fatalf("saml-sp delete: SP %q not found", delSPEntityID)
				}
				log.Fatalf("saml-sp delete: lookup: %v", err)
			}
			rows, err := q.DeleteSAMLSP(ctx, sp.ID)
			if err != nil {
				log.Fatalf("saml-sp delete: %v", err)
			}
			if rows == 0 {
				log.Fatalf("saml-sp delete: SP %q not found", delSPEntityID)
			}
			fmt.Printf("Deleted SAML SP %q (id=%d; ACS + signing keys cascaded)\n", delSPEntityID, sp.ID)
		},
	}
	deleteSPCmd.Flags().StringVar(&delSPEntityID, "entity-id", "", "Entity ID of the SP to delete (required).")
	deleteSPCmd.Flags().BoolVar(&delSPYes, "yes", false, "Confirm the destructive delete.")
	samlSPCmd.AddCommand(deleteSPCmd)

	cli.Root().AddCommand(samlSPCmd)

	addUpstreamIDPCommands(cli.Root())

	addDevSeedCmd(cli.Root())

	cli.Run()
}

// mustOpenDB parses the config, applies migrations, opens a pgx pool, and
// returns the sqlc Queries plus the pool (the caller must Close the pool).
// Centralises the boilerplate shared by every admin CLI verb. Fatals on error.
func mustOpenDB(ctx context.Context) (*db.Queries, *pgxpool.Pool) {
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
	return db.New(conn), conn
}

// mustCurrentDEK returns the highest-versioned data-encryption key from config,
// mirroring server.currentDEK. Fatals if no DEK is configured.
func mustCurrentDEK() (int32, []byte) {
	config, err := configx.Parse()
	if err != nil {
		log.Fatalf("parse config: %v", err)
	}
	if len(config.DataEncryptionKeys) == 0 {
		log.Fatalf("no data encryption keys configured (set PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>)")
	}
	var maxVer int
	for v := range config.DataEncryptionKeys {
		if v > maxVer {
			maxVer = v
		}
	}
	return int32(maxVer), config.DataEncryptionKeys[maxVer]
}

// addUpstreamIDPCommands registers the `upstream-idp` command group:
// create | list | update | rotate-secret | delete. These mirror the admin
// HTTP handlers (handle_admin_upstream_idps.go) and share the SAME db queries
// and the SAME AES-GCM sealing path (fedoidc.EncryptClientSecret, AAD bound to
// the row id + key version). Net-new: there was no upstream-idp CLI before.
func addUpstreamIDPCommands(root *cobra.Command) {
	upstreamCmd := &cobra.Command{
		Use:   "upstream-idp",
		Short: "Manage upstream OIDC identity providers (federation)",
	}

	defaultScopes := []string{"openid", "email", "profile"}

	// create
	var (
		uSlug          string
		uDisplayName   string
		uIssuerURL     string
		uClientID      string
		uClientSecret  string
		uScopes        []string
		uMode          string
		uAllowedDomain []string
		uUsernameClaim string
		uDisplayClaim  string
		uEmailClaim    string
		uRequireVerEml bool
	)
	createUpstreamCmd := &cobra.Command{
		Use:   "create",
		Short: "Register a new upstream OIDC IdP (secret is AES-GCM sealed at rest)",
		Run: func(_ *cobra.Command, _ []string) {
			ctx := context.Background()
			if uSlug == "" || uDisplayName == "" || uIssuerURL == "" || uClientID == "" || uClientSecret == "" || uMode == "" {
				log.Fatalf("--slug, --display-name, --issuer-url, --client-id, --client-secret, --mode are all required")
			}
			keyVer, dek := mustCurrentDEK()
			q, conn := mustOpenDB(ctx)
			defer conn.Close()

			scopes := uScopes
			if len(scopes) == 0 {
				scopes = defaultScopes
			}
			allowed := uAllowedDomain
			if allowed == nil {
				allowed = []string{}
			}
			usernameClaim := uUsernameClaim
			if usernameClaim == "" {
				usernameClaim = "preferred_username"
			}
			displayClaim := uDisplayClaim
			if displayClaim == "" {
				displayClaim = "name"
			}
			emailClaim := uEmailClaim
			if emailClaim == "" {
				emailClaim = "email"
			}

			// Insert with placeholder secret bytes to get the auto-assigned id
			// (the AAD is bound to id + key_version), then seal and write back.
			placeholder := make([]byte, 1)
			row, err := q.InsertUpstreamIDP(ctx, db.InsertUpstreamIDPParams{
				Slug:                 uSlug,
				DisplayName:          uDisplayName,
				IssuerUrl:            uIssuerURL,
				ClientID:             uClientID,
				ClientSecretEnc:      placeholder,
				SecretNonce:          placeholder,
				KeyVersion:           keyVer,
				Scopes:               scopes,
				Mode:                 uMode,
				AllowedDomains:       allowed,
				UsernameClaim:        usernameClaim,
				DisplayNameClaim:     displayClaim,
				EmailClaim:           emailClaim,
				RequireVerifiedEmail: uRequireVerEml,
			})
			if err != nil {
				log.Fatalf("upstream-idp create: insert: %v", err)
			}
			ciphertext, nonce, err := fedoidc.EncryptClientSecret(dek, []byte(uClientSecret), row.ID, keyVer)
			if err != nil {
				_ = q.DeleteUpstreamIDP(ctx, row.ID)
				log.Fatalf("upstream-idp create: seal: %v", err)
			}
			if err := q.UpdateUpstreamIDPSecret(ctx, db.UpdateUpstreamIDPSecretParams{
				Slug:            row.Slug,
				ClientSecretEnc: ciphertext,
				SecretNonce:     nonce,
				KeyVersion:      keyVer,
			}); err != nil {
				_ = q.DeleteUpstreamIDP(ctx, row.ID)
				log.Fatalf("upstream-idp create: seal-update: %v", err)
			}
			fmt.Printf("Registered upstream IdP %q (mode=%s, issuer=%s; secret sealed at rest)\n", row.Slug, row.Mode, row.IssuerUrl)
		},
	}
	createUpstreamCmd.Flags().StringVar(&uSlug, "slug", "", "Stable slug identifier (required).")
	createUpstreamCmd.Flags().StringVar(&uDisplayName, "display-name", "", "Human-readable IdP name (required).")
	createUpstreamCmd.Flags().StringVar(&uIssuerURL, "issuer-url", "", "OIDC issuer URL (required).")
	createUpstreamCmd.Flags().StringVar(&uClientID, "client-id", "", "OAuth client_id at the upstream (required).")
	createUpstreamCmd.Flags().StringVar(&uClientSecret, "client-secret", "", "OAuth client secret at the upstream (required; sealed at rest).")
	createUpstreamCmd.Flags().StringArrayVar(&uScopes, "scope", nil, "Requested scope (repeatable; defaults to openid,email,profile).")
	createUpstreamCmd.Flags().StringVar(&uMode, "mode", "", "Provisioning mode: auto_provision or link_only (required).")
	createUpstreamCmd.Flags().StringArrayVar(&uAllowedDomain, "allowed-domain", nil, "Restrict to these email domains (repeatable).")
	createUpstreamCmd.Flags().StringVar(&uUsernameClaim, "username-claim", "", "Claim to map to username (default preferred_username).")
	createUpstreamCmd.Flags().StringVar(&uDisplayClaim, "display-name-claim", "", "Claim to map to display name (default name).")
	createUpstreamCmd.Flags().StringVar(&uEmailClaim, "email-claim", "", "Claim to map to email (default email).")
	createUpstreamCmd.Flags().BoolVar(&uRequireVerEml, "require-verified-email", false, "Require email_verified=true from the upstream.")
	upstreamCmd.AddCommand(createUpstreamCmd)

	// list
	listUpstreamCmd := &cobra.Command{
		Use:   "list",
		Short: "List registered upstream IdPs (including disabled)",
		Run: func(_ *cobra.Command, _ []string) {
			ctx := context.Background()
			q, conn := mustOpenDB(ctx)
			defer conn.Close()
			idps, err := q.ListAllUpstreamIDPs(ctx)
			if err != nil {
				log.Fatalf("upstream-idp list: %v", err)
			}
			if len(idps) == 0 {
				fmt.Println("No upstream IdPs registered.")
				return
			}
			fmt.Printf("%-24s %-24s %-16s %-10s %s\n", "SLUG", "DISPLAY_NAME", "MODE", "DISABLED", "ISSUER_URL")
			for _, i := range idps {
				fmt.Printf("%-24s %-24s %-16s %-10t %s\n", i.Slug, i.DisplayName, i.Mode, i.Disabled, i.IssuerUrl)
			}
		},
	}
	upstreamCmd.AddCommand(listUpstreamCmd)

	// update (config only; secret untouched — use rotate-secret)
	var (
		upSlug          string
		upDisplayName   string
		upIssuerURL     string
		upClientID      string
		upScopes        []string
		upMode          string
		upAllowedDomain []string
		upUsernameClaim string
		upDisplayClaim  string
		upEmailClaim    string
		upRequireVerEml bool
		upDisabled      bool
	)
	updateUpstreamCmd := &cobra.Command{
		Use:   "update",
		Short: "Update an upstream IdP's config (full-replace; secret untouched)",
		Run: func(_ *cobra.Command, _ []string) {
			ctx := context.Background()
			if upSlug == "" || upDisplayName == "" || upIssuerURL == "" || upClientID == "" || upMode == "" {
				log.Fatalf("--slug, --display-name, --issuer-url, --client-id, --mode are all required (full-replace update)")
			}
			q, conn := mustOpenDB(ctx)
			defer conn.Close()

			scopes := upScopes
			if len(scopes) == 0 {
				scopes = defaultScopes
			}
			allowed := upAllowedDomain
			if allowed == nil {
				allowed = []string{}
			}
			usernameClaim := upUsernameClaim
			if usernameClaim == "" {
				usernameClaim = "preferred_username"
			}
			displayClaim := upDisplayClaim
			if displayClaim == "" {
				displayClaim = "name"
			}
			emailClaim := upEmailClaim
			if emailClaim == "" {
				emailClaim = "email"
			}
			updated, err := q.UpdateUpstreamIDPConfig(ctx, db.UpdateUpstreamIDPConfigParams{
				Slug:                 upSlug,
				DisplayName:          upDisplayName,
				IssuerUrl:            upIssuerURL,
				ClientID:             upClientID,
				Scopes:               scopes,
				Mode:                 upMode,
				AllowedDomains:       allowed,
				UsernameClaim:        usernameClaim,
				DisplayNameClaim:     displayClaim,
				EmailClaim:           emailClaim,
				RequireVerifiedEmail: upRequireVerEml,
				Disabled:             upDisabled,
			})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					log.Fatalf("upstream-idp update: IdP %q not found", upSlug)
				}
				log.Fatalf("upstream-idp update: %v", err)
			}
			fmt.Printf("Updated upstream IdP %q (mode=%s, disabled=%t)\n", updated.Slug, updated.Mode, updated.Disabled)
		},
	}
	updateUpstreamCmd.Flags().StringVar(&upSlug, "slug", "", "Slug of the IdP to update (required).")
	updateUpstreamCmd.Flags().StringVar(&upDisplayName, "display-name", "", "Human-readable IdP name (required).")
	updateUpstreamCmd.Flags().StringVar(&upIssuerURL, "issuer-url", "", "OIDC issuer URL (required).")
	updateUpstreamCmd.Flags().StringVar(&upClientID, "client-id", "", "OAuth client_id at the upstream (required).")
	updateUpstreamCmd.Flags().StringArrayVar(&upScopes, "scope", nil, "Requested scope (repeatable; defaults to openid,email,profile).")
	updateUpstreamCmd.Flags().StringVar(&upMode, "mode", "", "Provisioning mode: auto_provision or link_only (required).")
	updateUpstreamCmd.Flags().StringArrayVar(&upAllowedDomain, "allowed-domain", nil, "Restrict to these email domains (repeatable).")
	updateUpstreamCmd.Flags().StringVar(&upUsernameClaim, "username-claim", "", "Claim to map to username (default preferred_username).")
	updateUpstreamCmd.Flags().StringVar(&upDisplayClaim, "display-name-claim", "", "Claim to map to display name (default name).")
	updateUpstreamCmd.Flags().StringVar(&upEmailClaim, "email-claim", "", "Claim to map to email (default email).")
	updateUpstreamCmd.Flags().BoolVar(&upRequireVerEml, "require-verified-email", false, "Require email_verified=true from the upstream.")
	updateUpstreamCmd.Flags().BoolVar(&upDisabled, "disabled", false, "Disable this IdP.")
	upstreamCmd.AddCommand(updateUpstreamCmd)

	// rotate-secret (re-seal a new secret under the current DEK)
	var (
		rSlug   string
		rSecret string
	)
	rotateUpstreamCmd := &cobra.Command{
		Use:   "rotate-secret",
		Short: "Replace an upstream IdP's sealed client secret",
		Run: func(_ *cobra.Command, _ []string) {
			ctx := context.Background()
			if rSlug == "" || rSecret == "" {
				log.Fatalf("--slug and --client-secret are required")
			}
			keyVer, dek := mustCurrentDEK()
			q, conn := mustOpenDB(ctx)
			defer conn.Close()

			row, err := q.GetUpstreamIDPBySlugAny(ctx, rSlug)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					log.Fatalf("upstream-idp rotate-secret: IdP %q not found", rSlug)
				}
				log.Fatalf("upstream-idp rotate-secret: lookup: %v", err)
			}
			ciphertext, nonce, err := fedoidc.EncryptClientSecret(dek, []byte(rSecret), row.ID, keyVer)
			if err != nil {
				log.Fatalf("upstream-idp rotate-secret: seal: %v", err)
			}
			if err := q.UpdateUpstreamIDPSecret(ctx, db.UpdateUpstreamIDPSecretParams{
				Slug:            rSlug,
				ClientSecretEnc: ciphertext,
				SecretNonce:     nonce,
				KeyVersion:      keyVer,
			}); err != nil {
				log.Fatalf("upstream-idp rotate-secret: update: %v", err)
			}
			fmt.Printf("Rotated sealed client secret for upstream IdP %q\n", rSlug)
		},
	}
	rotateUpstreamCmd.Flags().StringVar(&rSlug, "slug", "", "Slug of the IdP (required).")
	rotateUpstreamCmd.Flags().StringVar(&rSecret, "client-secret", "", "New upstream client secret (required; sealed at rest).")
	upstreamCmd.AddCommand(rotateUpstreamCmd)

	// delete (destructive; requires --yes)
	var (
		dSlug string
		dYes  bool
	)
	deleteUpstreamCmd := &cobra.Command{
		Use:   "delete",
		Short: "Hard-delete an upstream IdP (requires --yes)",
		Run: func(_ *cobra.Command, _ []string) {
			ctx := context.Background()
			if dSlug == "" {
				log.Fatalf("--slug is required")
			}
			if !dYes {
				log.Fatalf("refusing to delete upstream IdP %q without --yes", dSlug)
			}
			q, conn := mustOpenDB(ctx)
			defer conn.Close()

			row, err := q.GetUpstreamIDPBySlugAny(ctx, dSlug)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					log.Fatalf("upstream-idp delete: IdP %q not found", dSlug)
				}
				log.Fatalf("upstream-idp delete: lookup: %v", err)
			}
			if err := q.DeleteUpstreamIDP(ctx, row.ID); err != nil {
				log.Fatalf("upstream-idp delete: %v", err)
			}
			fmt.Printf("Deleted upstream IdP %q (id=%d)\n", dSlug, row.ID)
		},
	}
	deleteUpstreamCmd.Flags().StringVar(&dSlug, "slug", "", "Slug of the IdP to delete (required).")
	deleteUpstreamCmd.Flags().BoolVar(&dYes, "yes", false, "Confirm the destructive delete.")
	upstreamCmd.AddCommand(deleteUpstreamCmd)

	root.AddCommand(upstreamCmd)
}

// fetchMetadata fetches SP metadata XML over HTTP(S) with a timeout, capping
// the response body at 1 MiB.
func fetchMetadata(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata url returned status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}
