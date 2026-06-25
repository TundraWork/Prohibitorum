package main

// dev-federation: wire two local prohibitorum instances — an upstream OIDC OP
// and a downstream RP that federates to it — for manual end-to-end testing.
// Connects to BOTH databases in one process and is fully idempotent
// (create-or-rotate clients, UPSERT-and-reseal idp rows, ensure-if-missing
// signing keys). Dev-only: refuses unless both origins resolve to loopback.
//
// Reuses the same library functions the production CLI uses — no new crypto or
// DB code paths. Driven entirely by flags + env so it embeds no real infra.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"prohibitorum/db/migrations"
	"prohibitorum/pkg/configx"
	"prohibitorum/pkg/credential/enrollment"
	"prohibitorum/pkg/db"
	fedoidc "prohibitorum/pkg/federation/oidc"
	"prohibitorum/pkg/protocol/oidc"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

const (
	fedClientID    = "downstream-federation"
	testRPID       = "test-rp"
	testRPRedirect = "http://127.0.0.1:9876/callback"
	slugAuto       = "upstream"
	slugInvite     = "upstream-invite"
	slugLink       = "upstream-link"
)

var _devFederationCmd *cobra.Command

func init() {
	var upstreamDB, downstreamDB, upstreamOrigin, downstreamOrigin string
	cmd := &cobra.Command{
		Use:   "dev-federation",
		Short: "Wire two local instances (upstream OP + downstream RP) for manual federation testing (dev-only)",
		Long: `Idempotently wire two local prohibitorum instances for manual OIDC
federation testing: an upstream OP and a downstream RP. Ensures an active
signing key on each, registers the downstream-federation client on the upstream,
the upstream/upstream-invite/upstream-link IdP rows on the downstream, a test-rp
on each, and a federation-bound invitation. Refuses non-loopback origins.`,
		Run: func(_ *cobra.Command, _ []string) {
			runDevFederation(upstreamDB, downstreamDB, upstreamOrigin, downstreamOrigin)
		},
	}
	cmd.Flags().StringVar(&upstreamDB, "upstream-db", "", "Postgres DSN for the upstream instance (required).")
	cmd.Flags().StringVar(&downstreamDB, "downstream-db", "", "Postgres DSN for the downstream instance (required).")
	cmd.Flags().StringVar(&upstreamOrigin, "upstream-origin", "", "https public origin of the upstream (required).")
	cmd.Flags().StringVar(&downstreamOrigin, "downstream-origin", "", "https public origin of the downstream (required).")
	_devFederationCmd = cmd
}

func addDevFederationCmd(root *cobra.Command) { root.AddCommand(_devFederationCmd) }

func runDevFederation(upstreamDB, downstreamDB, upstreamOrigin, downstreamOrigin string) {
	ctx := context.Background()
	if upstreamDB == "" || downstreamDB == "" || upstreamOrigin == "" || downstreamOrigin == "" {
		log.Fatalf("dev-federation: --upstream-db, --downstream-db, --upstream-origin, --downstream-origin are all required")
	}
	if !isLoopbackOrigin(upstreamOrigin) || !isLoopbackOrigin(downstreamOrigin) {
		log.Fatalf("dev-federation refuses non-loopback origins (upstream=%s downstream=%s)", upstreamOrigin, downstreamOrigin)
	}

	cfg, err := configx.Parse()
	if err != nil {
		log.Fatalf("dev-federation: parse config: %v", err)
	}
	keyVer, dek := mustCurrentDEK()
	grace := cfg.SAML.MetadataRotationGrace

	for _, dsn := range []string{upstreamDB, downstreamDB} {
		if _, err := migrations.UpWithResult(dsn); err != nil {
			log.Fatalf("dev-federation: migrate: %v", err)
		}
	}
	upPool, err := pgxpool.New(ctx, upstreamDB)
	if err != nil {
		log.Fatalf("dev-federation: connect upstream: %v", err)
	}
	defer upPool.Close()
	downPool, err := pgxpool.New(ctx, downstreamDB)
	if err != nil {
		log.Fatalf("dev-federation: connect downstream: %v", err)
	}
	defer downPool.Close()
	upQ := db.New(upPool)
	downQ := db.New(downPool)

	ensureSigningKey(ctx, upPool, upQ, dek, keyVer, grace, "upstream")
	ensureSigningKey(ctx, downPool, downQ, dek, keyVer, grace, "downstream")

	fedSecret := ensureFedClient(ctx, upQ, downstreamOrigin)
	upsertUpstreamIDP(ctx, downQ, slugAuto, "Upstream", "auto_provision", upstreamOrigin, fedSecret, dek, keyVer)
	upsertUpstreamIDP(ctx, downQ, slugInvite, "Upstream (invite)", "invite_only", upstreamOrigin, fedSecret, dek, keyVer)
	upsertUpstreamIDP(ctx, downQ, slugLink, "Upstream (link)", "link_only", upstreamOrigin, fedSecret, dek, keyVer)

	inviteSlug := slugInvite
	inviteToken, inviteExp, err := enrollment.IssueEnrollment(
		ctx, downQ, enrollment.IntentInvite, nil, enrollment.DefaultEnrollmentTTL,
		&enrollment.EnrollmentTemplate{Role: "user", ExpectedUpstreamIDPSlug: &inviteSlug},
	)
	if err != nil {
		log.Fatalf("dev-federation: mint federated invite: %v", err)
	}

	upTestSecret := ensureTestRP(ctx, upQ, "upstream")
	downTestSecret := ensureTestRP(ctx, downQ, "downstream")

	printFederationSummary(upstreamOrigin, downstreamOrigin, fedSecret,
		upTestSecret, downTestSecret, inviteToken, inviteExp)
}

func ensureSigningKey(ctx context.Context, pool *pgxpool.Pool, q *db.Queries, dek []byte, keyVer int32, grace time.Duration, label string) {
	if _, err := q.GetActiveSigningKey(ctx); err == nil {
		fmt.Printf("    [%s] active signing key present\n", label)
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		log.Fatalf("dev-federation: [%s] check signing key: %v", label, err)
	}
	pending, err := oidc.InsertPendingKey(ctx, q, dek, keyVer)
	if err != nil {
		log.Fatalf("dev-federation: [%s] insert signing key: %v", label, err)
	}
	if _, err := oidc.ActivateSigningKey(ctx, pool, q, pending.Kid, grace); err != nil {
		log.Fatalf("dev-federation: [%s] activate signing key: %v", label, err)
	}
	fmt.Printf("    [%s] generated + activated signing key %s\n", label, pending.Kid)
}

func fedRedirectURIs(downstreamOrigin string) []string {
	b := strings.TrimRight(downstreamOrigin, "/")
	return []string{
		b + "/api/prohibitorum/auth/federation/" + slugAuto + "/callback",
		b + "/api/prohibitorum/auth/federation/" + slugInvite + "/callback",
		b + "/api/prohibitorum/auth/federation/" + slugLink + "/callback",
		b + "/api/prohibitorum/me/identities/link/" + slugAuto + "/callback",
		b + "/api/prohibitorum/me/identities/link/" + slugInvite + "/callback",
		b + "/api/prohibitorum/me/identities/link/" + slugLink + "/callback",
	}
}

func ensureFedClient(ctx context.Context, q *db.Queries, downstreamOrigin string) string {
	redirects := fedRedirectURIs(downstreamOrigin)
	postLogout := []string{strings.TrimRight(downstreamOrigin, "/") + "/"}
	scopes := []string{"openid", "profile", "email"}
	if _, err := q.GetOIDCClientAny(ctx, fedClientID); err == nil {
		if _, err := q.UpdateOIDCClient(ctx, db.UpdateOIDCClientParams{
			ClientID: fedClientID, DisplayName: "Downstream federation",
			RedirectUris: redirects, PostLogoutRedirectUris: postLogout,
			AllowedScopes: scopes, RequireConsent: true, Disabled: false,
		}); err != nil {
			log.Fatalf("dev-federation: update fed client: %v", err)
		}
		secret, err := oidc.RotateClientSecret(ctx, q, fedClientID)
		if err != nil {
			log.Fatalf("dev-federation: rotate fed client secret: %v", err)
		}
		fmt.Printf("    [upstream] fed client %q updated + secret rotated\n", fedClientID)
		return secret
	} else if !errors.Is(err, pgx.ErrNoRows) {
		log.Fatalf("dev-federation: check fed client: %v", err)
	}
	params, secret, err := oidc.BuildClientParams(oidc.ClientOptions{
		ClientID: fedClientID, DisplayName: "Downstream federation",
		RedirectURIs: redirects, PostLogoutRedirectURIs: postLogout,
		Scopes: scopes, RequireConsent: true,
	})
	if err != nil {
		log.Fatalf("dev-federation: build fed client: %v", err)
	}
	if _, err := q.InsertOIDCClient(ctx, params); err != nil {
		log.Fatalf("dev-federation: insert fed client: %v", err)
	}
	fmt.Printf("    [upstream] fed client %q created\n", fedClientID)
	return secret
}

func ensureTestRP(ctx context.Context, q *db.Queries, label string) string {
	redirects := []string{testRPRedirect}
	scopes := []string{"openid", "profile", "email"}
	if _, err := q.GetOIDCClientAny(ctx, testRPID); err == nil {
		if _, err := q.UpdateOIDCClient(ctx, db.UpdateOIDCClientParams{
			ClientID: testRPID, DisplayName: "Manual test RP",
			RedirectUris: redirects, PostLogoutRedirectUris: []string{},
			AllowedScopes: scopes, RequireConsent: true, Disabled: false,
		}); err != nil {
			log.Fatalf("dev-federation: [%s] update test rp: %v", label, err)
		}
		secret, err := oidc.RotateClientSecret(ctx, q, testRPID)
		if err != nil {
			log.Fatalf("dev-federation: [%s] rotate test rp secret: %v", label, err)
		}
		fmt.Printf("    [%s] test rp %q updated + secret rotated\n", label, testRPID)
		return secret
	} else if !errors.Is(err, pgx.ErrNoRows) {
		log.Fatalf("dev-federation: [%s] check test rp: %v", label, err)
	}
	params, secret, err := oidc.BuildClientParams(oidc.ClientOptions{
		ClientID: testRPID, DisplayName: "Manual test RP",
		RedirectURIs: redirects, Scopes: scopes, RequireConsent: true,
	})
	if err != nil {
		log.Fatalf("dev-federation: [%s] build test rp: %v", label, err)
	}
	if _, err := q.InsertOIDCClient(ctx, params); err != nil {
		log.Fatalf("dev-federation: [%s] insert test rp: %v", label, err)
	}
	fmt.Printf("    [%s] test rp %q created\n", label, testRPID)
	return secret
}

func upsertUpstreamIDP(ctx context.Context, q *db.Queries, slug, displayName, mode, issuer, plaintext string, dek []byte, keyVer int32) {
	scopes := []string{"openid", "email", "profile"}
	var rowID int64
	existing, err := q.GetUpstreamIDPBySlugAny(ctx, slug)
	switch {
	case err == nil:
		if _, err := q.UpdateUpstreamIDPConfig(ctx, db.UpdateUpstreamIDPConfigParams{
			Slug: slug, DisplayName: displayName, IssuerUrl: issuer, ClientID: fedClientID,
			Scopes: scopes, Mode: mode, AllowedDomains: []string{},
			UsernameClaim: "preferred_username", DisplayNameClaim: "name", EmailClaim: "email",
			RequireVerifiedEmail: false, Disabled: false, PictureClaim: "picture",
		}); err != nil {
			log.Fatalf("dev-federation: update idp %q: %v", slug, err)
		}
		rowID = existing.ID
	case errors.Is(err, pgx.ErrNoRows):
		placeholder := make([]byte, 1)
		row, err := q.InsertUpstreamIDP(ctx, db.InsertUpstreamIDPParams{
			Slug: slug, DisplayName: displayName, IssuerUrl: issuer, ClientID: fedClientID,
			ClientSecretEnc: placeholder, SecretNonce: placeholder, KeyVersion: keyVer,
			Scopes: scopes, Mode: mode, AllowedDomains: []string{},
			UsernameClaim: "preferred_username", DisplayNameClaim: "name", EmailClaim: "email",
			RequireVerifiedEmail: false, PictureClaim: "picture",
		})
		if err != nil {
			log.Fatalf("dev-federation: insert idp %q: %v", slug, err)
		}
		rowID = row.ID
	default:
		log.Fatalf("dev-federation: check idp %q: %v", slug, err)
	}
	ciphertext, nonce, err := fedoidc.EncryptClientSecret(dek, []byte(plaintext), rowID, keyVer)
	if err != nil {
		log.Fatalf("dev-federation: seal idp %q: %v", slug, err)
	}
	if err := q.UpdateUpstreamIDPSecret(ctx, db.UpdateUpstreamIDPSecretParams{
		Slug: slug, ClientSecretEnc: ciphertext, SecretNonce: nonce, KeyVersion: keyVer,
	}); err != nil {
		log.Fatalf("dev-federation: reseal idp %q: %v", slug, err)
	}
	fmt.Printf("    [downstream] upstream_idp %q (mode=%s, issuer=%s) wired\n", slug, mode, issuer)
}

func pkcePair() (verifier, challenge string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func authorizeURL(origin, challenge string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", testRPID)
	q.Set("redirect_uri", testRPRedirect)
	q.Set("scope", "openid profile email")
	q.Set("state", "dev-state")
	q.Set("nonce", "dev-nonce")
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	return strings.TrimRight(origin, "/") + "/oauth/authorize?" + q.Encode()
}

func printFederationSummary(upstreamOrigin, downstreamOrigin, fedSecret, upTestSecret, downTestSecret, inviteToken string, inviteExp time.Time) {
	verifier, challenge, err := pkcePair()
	if err != nil {
		log.Fatalf("dev-federation: pkce: %v", err)
	}
	up := strings.TrimRight(upstreamOrigin, "/")
	down := strings.TrimRight(downstreamOrigin, "/")
	fmt.Println()
	fmt.Println("================ dev-federation wiring complete ================")
	fmt.Printf("Federation: downstream %q --[oidc]--> upstream %q\n", down, up)
	fmt.Printf("  fed client_id=%s  client_secret=%s\n", fedClientID, fedSecret)
	fmt.Printf("  downstream IdP slugs: %q (auto_provision), %q (invite_only), %q (link_only)\n", slugAuto, slugInvite, slugLink)
	fmt.Println()
	fmt.Println("Invite-gated (invite_only) — NOT shown as a sign-in button; reachable only")
	fmt.Println("  via the invite link below (a plain sign-in on it is rejected pre-auth):")
	fmt.Printf("  %s/enroll/%s   (expires %s)\n", down, inviteToken, inviteExp.Format(time.RFC3339))
	fmt.Println()
	fmt.Println("Link-only (link_only) — shown as a sign-in button; a fresh sign-in is")
	fmt.Println("  refused (link_required). Link it first from the downstream's Connected")
	fmt.Printf("  accounts (%s/connected), then sign in with it.\n", down)
	fmt.Println()
	fmt.Println("Direct OP test (PKCE S256, code_verifier shared below):")
	fmt.Printf("  code_verifier=%s\n", verifier)
	for _, x := range []struct{ name, origin, secret string }{
		{"upstream", up, upTestSecret},
		{"downstream", down, downTestSecret},
	} {
		fmt.Printf("  --- %s (%s) ---\n", x.name, x.origin)
		fmt.Printf("    1) open: %s\n", authorizeURL(x.origin, challenge))
		fmt.Printf("    2) read ?code=… from the (failed) %s redirect, then:\n", testRPRedirect)
		fmt.Printf("       curl -s -u '%s:%s' -d grant_type=authorization_code -d code=PASTE_CODE \\\n", testRPID, x.secret)
		fmt.Printf("         -d redirect_uri=%s -d code_verifier=%s %s/oauth/token\n", testRPRedirect, verifier, x.origin)
		fmt.Printf("    3) curl -s -H 'Authorization: Bearer ACCESS_TOKEN' %s/oauth/userinfo\n", x.origin)
	}
	fmt.Println("===============================================================")
}
