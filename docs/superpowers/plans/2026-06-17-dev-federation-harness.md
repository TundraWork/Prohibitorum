# Dev federation harness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One command (`mise run dev:federation`) that brings up two local prohibitorum instances — an upstream OIDC OP and a downstream RP federating to it, behind nginx TLS — fully wired for manual end-to-end testing of OIDC, federated-login enrollment/confirmation, and consent.

**Architecture:** A new idempotent dev-only Go subcommand (`dev-federation`) connects to both instances' databases and wires them (signing keys, the `downstream-federation` OIDC client, two `upstream_idp` rows, a federation-bound invite, a `test-rp` per instance) reusing existing library functions. A bash orchestrator (`scripts/dev-federation.sh`) owns lifecycle (two DBs, build, per-instance `dev-seed`/`enroll-admin`, wiring, nginx-vhost generation, launching both loopback-http backends, banner, Ctrl-C cleanup). nginx terminates TLS. Real hostnames + cert paths live only in the gitignored `.dev/dev-federation.env`; all committed files use `example.test` placeholders.

**Tech Stack:** Go (cobra, pgx/pgxpool, sqlc-generated `pkg/db`), bash, nginx, mise, Postgres.

**Spec:** `docs/superpowers/specs/2026-06-17-dev-federation-harness-design.md`

---

### Task 1: Shared loopback-origin guard

**Goal:** Replace `dev_seed.go`'s `localhost`-only guard with a shared `isLoopbackOrigin` helper that also accepts hostnames resolving entirely to loopback (so real loopback-pinned DNS names pass; public origins still refuse), with a hermetic unit test.

**Files:**
- Modify: `cmd/prohibitorum/dev_seed.go` (add `net` import, add `isLoopbackOrigin`, use it in `runDevSeed`)
- Test: `cmd/prohibitorum/dev_seed_test.go` (create)

**Acceptance Criteria:**
- [ ] `isLoopbackOrigin` returns true for `localhost`/`127.0.0.1`/`::1` and loopback IP literals, false for public IP literals, malformed input, and empty input.
- [ ] `runDevSeed` uses the helper instead of the inline `loopbackHosts[...]` check.
- [ ] `go build -tags nodynamic ./...` and `go test ./cmd/prohibitorum/ -run TestIsLoopbackOrigin -v` pass.

**Verify:** `go test ./cmd/prohibitorum/ -run TestIsLoopbackOrigin -v` → PASS

**Steps:**

- [ ] **Step 1: Write the failing test** — create `cmd/prohibitorum/dev_seed_test.go`

```go
package main

import "testing"

func TestIsLoopbackOrigin(t *testing.T) {
	cases := []struct {
		origin string
		want   bool
	}{
		{"http://localhost:8080", true},
		{"https://127.0.0.1", true},
		{"https://[::1]:9000", true},
		{"http://127.0.0.1:18080", true},
		{"https://8.8.8.8", false},        // public IP literal — no DNS needed
		{"https://93.184.216.34", false},  // public IP literal — no DNS needed
		{"not a url", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isLoopbackOrigin(c.origin); got != c.want {
			t.Errorf("isLoopbackOrigin(%q) = %v, want %v", c.origin, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./cmd/prohibitorum/ -run TestIsLoopbackOrigin -v`
Expected: FAIL — `undefined: isLoopbackOrigin`

- [ ] **Step 3: Add the helper and the `net` import in `cmd/prohibitorum/dev_seed.go`**

Add `"net"` to the import block (it currently imports `"net/url"`). Add this helper (e.g. right after the `loopbackHosts` var):

```go
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
```

- [ ] **Step 4: Use the helper in `runDevSeed`** — replace the existing guard block:

```go
	// Dev guard: only loopback origins allowed.
	origin := config.PublicOrigins[0]
	u, err := url.Parse(origin)
	if err != nil || !loopbackHosts[u.Hostname()] {
		log.Fatalf("dev-seed refuses to run against a non-localhost origin (%s); set PROHIBITORUM_PUBLIC_ORIGIN to http://localhost:…", origin)
	}
```

with:

```go
	// Dev guard: only loopback origins allowed (incl. DNS names pinned to loopback).
	origin := config.PublicOrigins[0]
	if !isLoopbackOrigin(origin) {
		log.Fatalf("dev-seed refuses to run against a non-loopback origin (%s); set PROHIBITORUM_PUBLIC_ORIGIN to a loopback host", origin)
	}
```

- [ ] **Step 5: Run the test + build to verify they pass**

Run: `go test ./cmd/prohibitorum/ -run TestIsLoopbackOrigin -v && go build -tags nodynamic ./...`
Expected: PASS; build clean. (If `url` becomes unused after the edit — it is still used by `isLoopbackOrigin` — no change needed.)

- [ ] **Step 6: Commit**

```bash
git add cmd/prohibitorum/dev_seed.go cmd/prohibitorum/dev_seed_test.go
git commit -m "feat(dev): shared isLoopbackOrigin guard accepting loopback-pinned DNS names"
```

---

### Task 2: `dev-federation` wiring command

**Goal:** A new idempotent dev-only subcommand that connects to both DBs and wires the full federation setup, printing a summary + PKCE recipe.

**Files:**
- Create: `cmd/prohibitorum/dev_federation.go`
- Modify: `cmd/prohibitorum/main.go:782` (register the command next to `addDevSeedCmd`)

**Acceptance Criteria:**
- [ ] `dev-federation` requires all four flags and refuses non-loopback origins.
- [ ] Each DB ends with an active signing key; the `downstream-federation` client exists on the upstream with the five redirect URIs + `require_consent`; `upstream` (auto_provision) + `upstream-invite` (invite_only) rows exist on the downstream sealed with the fed client's current secret; a `test-rp` exists on each; a federation-bound invite is minted on the downstream.
- [ ] Re-running produces no error and no duplicate signing keys.
- [ ] Prints a wiring summary with per-instance PKCE authorize URL + token/userinfo curls and the federation-bound invite URL.

**Verify:** `go build -tags nodynamic ./... && go vet ./...` → clean (full behavior verified live in Task 6).

**Steps:**

- [ ] **Step 1: Create `cmd/prohibitorum/dev_federation.go`**

```go
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
the upstream/upstream-invite IdP rows on the downstream, a test-rp on each, and a
federation-bound invitation. Refuses non-loopback origins.`,
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
		b + "/api/prohibitorum/me/identities/link/" + slugAuto + "/callback",
		b + "/api/prohibitorum/me/identities/link/" + slugInvite + "/callback",
		b + "/api/prohibitorum/me/sudo/federation/callback",
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
	fmt.Printf("  downstream IdP slugs: %q (auto_provision), %q (invite_only)\n", slugAuto, slugInvite)
	fmt.Println()
	fmt.Println("Invite-gated (invite_only) entry point — open on the downstream:")
	fmt.Printf("  %s/enroll/%s   (expires %s)\n", down, inviteToken, inviteExp.Format(time.RFC3339))
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
```

- [ ] **Step 2: Register the command in `cmd/prohibitorum/main.go`** — at line 782, after `addDevSeedCmd(cli.Root())`, add:

```go
	addDevFederationCmd(cli.Root())
```

- [ ] **Step 3: Build + vet**

Run: `go build -tags nodynamic ./... && go vet ./...`
Expected: clean (no errors).

- [ ] **Step 4: Smoke the flag guard**

Run: `go run ./cmd/prohibitorum dev-federation 2>&1 | head -1`
Expected: a fatal line containing `--upstream-db, --downstream-db, --upstream-origin, --downstream-origin are all required`. (`PROHIBITORUM_DATA_ENCRYPTION_KEY_V1` need not be set — the required-flags check runs before config parse.)

- [ ] **Step 5: Commit**

```bash
git add cmd/prohibitorum/dev_federation.go cmd/prohibitorum/main.go
git commit -m "feat(dev): dev-federation command to wire two local instances for OIDC testing"
```

---

### Task 3: Orchestrator script `scripts/dev-federation.sh`

**Goal:** A single foreground script that sources local config, ensures DBs, builds once, seeds + enrolls + wires both instances, generates the nginx vhost, launches both backends, prints a banner, and cleans up on Ctrl-C. No real hostnames/cert paths in the file.

**Files:**
- Create: `scripts/dev-federation.sh` (chmod +x)

**Acceptance Criteria:**
- [ ] First run (no `.dev/dev-federation.env`) writes a commented template with `example.test` placeholders and exits non-zero with instructions.
- [ ] With the four required vars set, it ensures both DBs, builds once, runs `dev-seed` + `enroll-admin` + `dev-federation` per the design, generates `.dev/nginx/prohibitorum-federation.conf`, launches both backends, and tails logs.
- [ ] `shellcheck scripts/dev-federation.sh` reports no errors (warnings acceptable).
- [ ] The script contains no real domain (`git grep` clean — checked in Task 6).

**Verify:** `shellcheck scripts/dev-federation.sh` → no errors; `bash scripts/dev-federation.sh` with no env file → writes template + exits 1.

**Steps:**

- [ ] **Step 1: Create `scripts/dev-federation.sh`**

```bash
#!/usr/bin/env bash
# Bring up two local prohibitorum instances (upstream OP + downstream RP) wired
# for OIDC federation behind nginx TLS, for manual end-to-end testing.
#
# Deployment-specific values (hostnames, cert paths) are read from the gitignored
# .dev/dev-federation.env — NEVER hardcode real infra here. See the spec:
# docs/superpowers/specs/2026-06-17-dev-federation-harness-design.md
#
#   mise run dev:federation            # bring it up (Ctrl-C stops both)
#   mise run dev:federation -- --fresh # wipe + recreate both DBs first
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

FRESH=0
[ "${1:-}" = "--fresh" ] && FRESH=1

LOCAL_ENV=".dev/dev-federation.env"
mkdir -p .dev/nginx .dev/logs

# --- 1. local config (gitignored; never committed) -------------------------
if [ ! -f "$LOCAL_ENV" ]; then
	cat >"$LOCAL_ENV" <<'TEMPLATE'
# .dev/dev-federation.env — LOCAL ONLY, never committed (.dev/ is gitignored).
# Fill in your real values, then re-run `mise run dev:federation`.
DEV_FED_UPSTREAM_HOST=idp-a.example.test
DEV_FED_DOWNSTREAM_HOST=idp-b.example.test
DEV_FED_TLS_CERT=/etc/nginx/ssl.d/wildcard.cer
DEV_FED_TLS_KEY=/etc/nginx/ssl.d/wildcard.key
# Optional overrides:
DEV_FED_UPSTREAM_BACKEND_PORT=18080
DEV_FED_DOWNSTREAM_BACKEND_PORT=18081
DEV_FED_NGINX_DIR=/etc/nginx/hosts.d
TEMPLATE
	echo "Wrote a template to $LOCAL_ENV."
	echo "Fill in your real hostnames + cert paths (both pinned to 127.0.0.1), then re-run."
	exit 1
fi
# shellcheck disable=SC1090
. "$LOCAL_ENV"

: "${DEV_FED_UPSTREAM_HOST:?set DEV_FED_UPSTREAM_HOST in $LOCAL_ENV}"
: "${DEV_FED_DOWNSTREAM_HOST:?set DEV_FED_DOWNSTREAM_HOST in $LOCAL_ENV}"
: "${DEV_FED_TLS_CERT:?set DEV_FED_TLS_CERT in $LOCAL_ENV}"
: "${DEV_FED_TLS_KEY:?set DEV_FED_TLS_KEY in $LOCAL_ENV}"
UP_PORT="${DEV_FED_UPSTREAM_BACKEND_PORT:-18080}"
DOWN_PORT="${DEV_FED_DOWNSTREAM_BACKEND_PORT:-18081}"
NGINX_DIR="${DEV_FED_NGINX_DIR:-/etc/nginx/hosts.d}"
UP_ORIGIN="https://$DEV_FED_UPSTREAM_HOST"
DOWN_ORIGIN="https://$DEV_FED_DOWNSTREAM_HOST"

# --- resolution check (browser + Go both need loopback) --------------------
for h in "$DEV_FED_UPSTREAM_HOST" "$DEV_FED_DOWNSTREAM_HOST"; do
	ip="$(getent hosts "$h" 2>/dev/null | awk '{print $1; exit}')" || true
	case "$ip" in
	127.* | ::1) ;;
	*)
		echo "ERROR: $h does not resolve to loopback (got '${ip:-nothing}'). Point its DNS at 127.0.0.1." >&2
		exit 1
		;;
	esac
done

# --- 2. shared encryption key (reuse dev-env.sh's) -------------------------
[ -f .dev/encryption-key ] || openssl rand -base64 32 >.dev/encryption-key
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(cat .dev/encryption-key)"

# --- DB: ensure the two federation databases on the dev cluster ------------
# PGPASSWORD matches the dev credential (compose.yaml / dev-env.sh). The
# podman-free dev-db.sh cluster uses trust auth and ignores it; the compose
# Postgres requires it — setting it works for both.
export PGHOST=localhost PGPORT=5432 PGUSER=prohibitorum PGPASSWORD=prohibitorum
UP_DB="postgres://prohibitorum:prohibitorum@localhost:5432/prohibitorum_upstream?sslmode=disable"
DOWN_DB="postgres://prohibitorum:prohibitorum@localhost:5432/prohibitorum_downstream?sslmode=disable"

if ! psql -d postgres -tAc 'SELECT 1' >/dev/null 2>&1; then
	echo "ERROR: Postgres not reachable on localhost:5432 — run 'mise run db:start'." >&2
	exit 1
fi
ensure_db() {
	local name="$1"
	if [ "$FRESH" = "1" ]; then
		psql -d postgres -c "DROP DATABASE IF EXISTS $name WITH (FORCE)" >/dev/null
	fi
	if ! psql -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='$name'" | grep -q 1; then
		createdb "$name"
		echo "created database $name"
	fi
}
ensure_db prohibitorum_upstream
ensure_db prohibitorum_downstream

# --- 3. build once (matches smoke/release) ---------------------------------
BIN_DIR="$(mktemp -d)"
BIN="$BIN_DIR/prohibitorum"
echo "building prohibitorum (-tags nodynamic) ..."
go build -tags nodynamic -o "$BIN" ./cmd/prohibitorum

# --- 4. per-instance seed + admin enrollment -------------------------------
setup_instance() {
	local origin="$1" dburl="$2" label="$3"
	echo "==> [$label] dev-seed"
	PROHIBITORUM_PUBLIC_ORIGIN="$origin" PROHIBITORUM_DATABASE_URL="$dburl" "$BIN" dev-seed
	echo "==> [$label] enroll-admin"
	if ! PROHIBITORUM_PUBLIC_ORIGIN="$origin" PROHIBITORUM_DATABASE_URL="$dburl" "$BIN" enroll-admin; then
		echo "    [$label] admin already enrolled — sign in at $origin"
	fi
}
setup_instance "$UP_ORIGIN" "$UP_DB" upstream
setup_instance "$DOWN_ORIGIN" "$DOWN_DB" downstream

# --- 5. wire federation ----------------------------------------------------
echo "==> wiring federation"
PROHIBITORUM_PUBLIC_ORIGIN="$DOWN_ORIGIN" PROHIBITORUM_DATABASE_URL="$DOWN_DB" \
	"$BIN" dev-federation \
	--upstream-db "$UP_DB" --downstream-db "$DOWN_DB" \
	--upstream-origin "$UP_ORIGIN" --downstream-origin "$DOWN_ORIGIN"

# --- 6. generate nginx vhost (single file, both blocks) --------------------
NGINX_CONF=".dev/nginx/prohibitorum-federation.conf"
cat >"$NGINX_CONF" <<EOF
# Generated by scripts/dev-federation.sh — do not commit; re-run regenerates it.
server {
    listen 80; listen [::]:80;
    server_name $DEV_FED_UPSTREAM_HOST;
    return 301 https://\$host\$request_uri;
}
server {
    listen 443 ssl; listen [::]:443 ssl; http2 on;
    server_name $DEV_FED_UPSTREAM_HOST;
    ssl_certificate     $DEV_FED_TLS_CERT;
    ssl_certificate_key $DEV_FED_TLS_KEY;
    location / {
        proxy_pass         http://127.0.0.1:$UP_PORT;
        proxy_http_version 1.1;
        proxy_set_header   Host              \$host;
        proxy_set_header   X-Real-IP         \$remote_addr;
        proxy_set_header   X-Forwarded-For   \$proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto \$scheme;
        proxy_set_header   X-Forwarded-Host  \$host;
    }
}
server {
    listen 80; listen [::]:80;
    server_name $DEV_FED_DOWNSTREAM_HOST;
    return 301 https://\$host\$request_uri;
}
server {
    listen 443 ssl; listen [::]:443 ssl; http2 on;
    server_name $DEV_FED_DOWNSTREAM_HOST;
    ssl_certificate     $DEV_FED_TLS_CERT;
    ssl_certificate_key $DEV_FED_TLS_KEY;
    location / {
        proxy_pass         http://127.0.0.1:$DOWN_PORT;
        proxy_http_version 1.1;
        proxy_set_header   Host              \$host;
        proxy_set_header   X-Real-IP         \$remote_addr;
        proxy_set_header   X-Forwarded-For   \$proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto \$scheme;
        proxy_set_header   X-Forwarded-Host  \$host;
    }
}
EOF
echo
echo "nginx vhost generated: $NGINX_CONF"
echo "  install once (needs root):"
echo "    sudo cp $NGINX_CONF $NGINX_DIR/ && sudo nginx -t && sudo systemctl reload nginx"

# --- 7. launch both backends (loopback http; nginx terminates TLS) ---------
UP_LOG=".dev/logs/upstream.log"
DOWN_LOG=".dev/logs/downstream.log"
PROHIBITORUM_DATABASE_URL="$UP_DB" PROHIBITORUM_PUBLIC_ORIGIN="$UP_ORIGIN" \
	PROHIBITORUM_HOST=127.0.0.1 PROHIBITORUM_PORT="$UP_PORT" PROHIBITORUM_TRUST_PROXY=true \
	"$BIN" >"$UP_LOG" 2>&1 &
UP_PID=$!
PROHIBITORUM_DATABASE_URL="$DOWN_DB" PROHIBITORUM_PUBLIC_ORIGIN="$DOWN_ORIGIN" \
	PROHIBITORUM_HOST=127.0.0.1 PROHIBITORUM_PORT="$DOWN_PORT" PROHIBITORUM_TRUST_PROXY=true \
	PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true \
	"$BIN" >"$DOWN_LOG" 2>&1 &
DOWN_PID=$!

TAIL_PID=""
cleanup() { kill "$UP_PID" "$DOWN_PID" ${TAIL_PID:+"$TAIL_PID"} 2>/dev/null || true; rm -rf "$BIN_DIR"; }
trap cleanup INT TERM EXIT

# --- 8. wait for backends, probe nginx, banner -----------------------------
for url in "http://127.0.0.1:$UP_PORT/.well-known/openid-configuration" \
	"http://127.0.0.1:$DOWN_PORT/.well-known/openid-configuration"; do
	for _ in $(seq 1 60); do curl -sf "$url" >/dev/null 2>&1 && break; sleep 1; done
done
if curl -sf "$UP_ORIGIN/.well-known/openid-configuration" >/dev/null 2>&1; then
	NGINX_NOTE="nginx is routing $UP_ORIGIN"
else
	NGINX_NOTE="NOTE: $UP_ORIGIN not reachable — install the nginx vhost (command above) + reload"
fi
cat <<EOF

============================================================
  prohibitorum dev federation harness is UP
  Upstream  (OP): $UP_ORIGIN   (backend 127.0.0.1:$UP_PORT)
  Downstream(RP): $DOWN_ORIGIN (backend 127.0.0.1:$DOWN_PORT)
  $NGINX_NOTE
  Test: open $DOWN_ORIGIN and click "Upstream".
  Logs: $UP_LOG / $DOWN_LOG   |   Ctrl-C stops both.
============================================================
EOF

tail -n +1 -f "$UP_LOG" "$DOWN_LOG" &
TAIL_PID=$!
wait "$UP_PID" "$DOWN_PID"
```

- [ ] **Step 2: Make it executable**

Run: `chmod +x scripts/dev-federation.sh`

- [ ] **Step 3: shellcheck**

Run: `shellcheck scripts/dev-federation.sh || true`
Expected: no errors (SC1090 is suppressed inline; warnings are acceptable).

- [ ] **Step 4: Verify the first-run template path**

Run: `mv .dev/dev-federation.env /tmp/devfedenv.bak 2>/dev/null; bash scripts/dev-federation.sh; echo "exit=$?"; cat .dev/dev-federation.env`
Expected: prints "Wrote a template…", exits 1, and `.dev/dev-federation.env` contains the `example.test` template. (Restore your real file afterward if you had one: `mv /tmp/devfedenv.bak .dev/dev-federation.env 2>/dev/null || true`.)

- [ ] **Step 5: Commit**

```bash
git add scripts/dev-federation.sh
git commit -m "feat(dev): dev-federation orchestrator script (two backends + nginx vhost gen)"
```

---

### Task 4: mise task `dev:federation`

**Goal:** Expose the orchestrator as `mise run dev:federation`.

**Files:**
- Modify: `mise.toml` (add a `[tasks."dev:federation"]` stanza in the `# ---- dev:` section, after `dev:seed`)

**Acceptance Criteria:**
- [ ] `mise tasks | grep dev:federation` lists the task.
- [ ] `mise run dev:federation -- --help`-style invocation reaches the script (passes args through).

**Verify:** `mise tasks 2>/dev/null | grep -q 'dev:federation' && echo OK`

**Steps:**

- [ ] **Step 1: Add the task to `mise.toml`** — after the `[tasks."dev:seed"]` block:

```toml
[tasks."dev:federation"]
description = "DEV: bring up two prohibitorum instances (upstream + downstream IdP) wired for OIDC federation behind nginx TLS, for manual end-to-end testing. Reads local hostnames/cert from .dev/dev-federation.env (a template is written on first run). Start the DB first with `mise run db:start`; pass -- --fresh to wipe both DBs."
run = "exec scripts/dev-federation.sh \"$@\""
```

- [ ] **Step 2: Verify the task is registered**

Run: `mise tasks 2>/dev/null | grep dev:federation`
Expected: a line for `dev:federation`.

- [ ] **Step 3: Commit**

```bash
git add mise.toml
git commit -m "feat(dev): add mise dev:federation task"
```

---

### Task 5: Docs (TOOLING.md + README)

**Goal:** Document the harness with placeholder domains only.

**Files:**
- Modify: `TOOLING.md` (add a `### dev:federation` subsection)
- Modify: `README.md` (one-line pointer near the existing dev commands)

**Acceptance Criteria:**
- [ ] TOOLING.md explains the topology, the `.dev/dev-federation.env` local config, the one-time nginx install, and the manual-test paths — using only `example.test` placeholders.
- [ ] README has a one-line pointer to `mise run dev:federation`.
- [ ] No real domain anywhere (verified in Task 6).

**Verify:** `grep -q 'dev:federation' TOOLING.md README.md && echo OK`

**Steps:**

- [ ] **Step 1: Add to `TOOLING.md`** (place near the other `dev:*` task docs; adjust the heading depth to match the surrounding file):

```markdown
### `mise run dev:federation` — two-instance OIDC federation harness

Brings up two local instances for manual end-to-end testing: an **upstream** OP
(`https://idp-a.example.test`) and a **downstream** RP
(`https://idp-b.example.test`) that federates to it. Distinct hostnames give
each its own cookie jar (independent sessions); nginx terminates TLS and proxies
each to a loopback http backend (`127.0.0.1:18080` / `:18081`); the two
databases (`prohibitorum_upstream` / `prohibitorum_downstream`) are separate
from your `prohibitorum_dev`.

**Local config (never committed).** Real hostnames + cert paths live in the
gitignored `.dev/dev-federation.env`. First run writes a commented template
(`example.test` placeholders) and exits — fill in your real values (DNS names
pinned to `127.0.0.1`, plus the wildcard cert nginx serves) and re-run.

**Setup:**

1. `mise run db:start` (the dev Postgres).
2. `mise run dev:federation` — first run writes `.dev/dev-federation.env`; edit it.
3. `mise run dev:federation` again — it seeds, wires, generates
   `.dev/nginx/prohibitorum-federation.conf`, and prints a one-time
   `sudo cp … && sudo nginx -t && sudo systemctl reload nginx` command. Run it.
4. Open the printed admin-enrollment URLs to register a passkey on each.

**Manual-test paths** (see the spec for detail):

- Federated login (auto_provision): open the downstream → **Upstream** →
  consent on the upstream → `/welcome` confirm → session.
- Invite-gated (invite_only): open the federation-bound invite URL the harness
  prints → **Upstream (invite)** → invite redeemed + identity linked.
- Direct OP test: paste the printed `test-rp` authorize URL → consent → read the
  `code` from the address bar → run the printed token + userinfo `curl`s.

Re-runnable; `mise run dev:federation -- --fresh` wipes both DBs first.
```

- [ ] **Step 2: Add a one-line pointer to `README.md`** near the existing dev commands (e.g. after the `mise dev-seed` / dev-server mention):

```markdown
- `mise run dev:federation` — two wired instances (upstream OP + downstream RP) for manual OIDC/enrollment/consent testing (see TOOLING.md).
```

- [ ] **Step 3: Verify**

Run: `grep -q 'dev:federation' TOOLING.md && grep -q 'dev:federation' README.md && echo OK`
Expected: `OK`

- [ ] **Step 4: Commit**

```bash
git add TOOLING.md README.md
git commit -m "docs(dev): document the dev:federation two-instance harness"
```

---

### Task 6: Verification gate (build/test + live run + no-leak check)

**Goal:** Prove the harness works end-to-end and leaks no real infra into git.

**Files:** none (verification only).

**Acceptance Criteria:**
- [ ] `go build -tags nodynamic ./...`, `go vet ./...`, `go test ./...` all pass.
- [ ] A live `mise run dev:federation` brings both backends up with an active signing key, the wiring summary prints, cross-instance discovery resolves, and a second run is clean.
- [ ] `git grep` finds no real hostname/cert path in tracked files.

**Verify:** see steps (each command's expected output noted).

**Steps:**

- [ ] **Step 1: Go gate**

Run: `go build -tags nodynamic ./... && go vet ./... && go test ./...`
Expected: build/vet clean; tests pass (the `pkg/server` suite may flake under parallel shared-DB runs per the project's known-flaky note — re-run that package in isolation if needed).

- [ ] **Step 2: Live run** (requires the operator's real `.dev/dev-federation.env` + `mise run db:start`)

Run: `mise run db:start` then `mise run dev:federation -- --fresh` in one terminal.
Expected: both backends boot; the wiring summary prints (fed client secret, two IdP slugs, the federation-bound invite URL, the per-instance PKCE recipe); the banner shows both origins. In another terminal confirm active keys + cross-instance discovery:

```bash
curl -sf http://127.0.0.1:18080/oauth/jwks | grep -q '"keys"' && echo "upstream key OK"
curl -sf http://127.0.0.1:18081/oauth/jwks | grep -q '"keys"' && echo "downstream key OK"
# after installing the nginx vhost:
curl -sf https://idp-a.example.test/.well-known/openid-configuration | grep -q '"issuer"' && echo "nginx+TLS OK"
```

(Substitute your real hostname for `idp-a.example.test`.) Then exercise the browser paths from the banner (federation login → consent → `/welcome`; the invite URL; the test-RP authorize URL) — these passkey/consent steps are inherently manual.

- [ ] **Step 3: Idempotency** — stop (Ctrl-C) and re-run **without** `--fresh`:

Run: `mise run dev:federation`
Expected: no errors; `[upstream]/[downstream] active signing key present`; clients "updated + secret rotated"; IdP rows "wired"; a fresh invite URL. Confirm exactly one active signing key each:

```bash
psql "postgres://prohibitorum@localhost:5432/prohibitorum_upstream?sslmode=disable" \
  -tAc "SELECT count(*) FROM signing_key WHERE use='sig' AND status='active'"   # → 1
```

- [ ] **Step 4: No-leak check**

Run: `git grep -nI "$(awk -F= '/DEV_FED_UPSTREAM_HOST/{print $2}' .dev/dev-federation.env | tr -d '[:space:]' | cut -d. -f2-)" -- . ':!.dev' || echo "no real domain in tracked files"`
Also run: `git status --porcelain .dev` → expected: empty (the `.dev/` tree is gitignored, nothing staged).
Expected: "no real domain in tracked files".

- [ ] **Step 5: Done** — no commit (verification only). If any check failed, fix in the owning task and re-verify.

---

## Notes for the implementer

- **Build tag:** always `-tags nodynamic` (the avatar WebP pipeline) — matches smoke/release.
- **Two packages named `oidc`:** import `prohibitorum/pkg/protocol/oidc` as `oidc` and `prohibitorum/pkg/federation/oidc` as `fedoidc` (mirrors `main.go`).
- **`mustCurrentDEK()` / `isLoopbackOrigin` / `loopbackHosts`** are package-`main` symbols already in `cmd/prohibitorum/` — call them directly from `dev_federation.go`.
- **Never commit** real hostnames or cert paths; they live only in `.dev/` (already gitignored). All committed files use `example.test`.
- **Commit messages:** no `Co-Authored-By` / AI-attribution trailer (firm project rule). Work stays on `master`.
