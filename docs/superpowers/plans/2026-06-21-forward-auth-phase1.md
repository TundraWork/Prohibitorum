# Native Traefik ForwardAuth (Phase 1) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prohibitorum natively answers Traefik ForwardAuth — `200` + identity headers for an authenticated+authorized user across unrelated domains, bootstrapping via the existing OIDC flow and a per-domain forward-auth cookie.

**Architecture:** A protected service is a normal `oidc_client` flagged for forward-auth. A verify endpoint (the ForwardAuth target) checks a per-domain cookie → `200`+headers, else `302` into our own `/oauth/authorize` (reusing session + `access_restricted`/RBAC); a callback on the protected domain redeems the code **in-process** (`consumeCode`) and plants the per-domain cookie. Authentik embedded-outpost model; reuses the OIDC OP, code store, and RBAC.

**Tech Stack:** Go (chi, pgx, sqlc, goose, KV), reusing `pkg/protocol/oidc` (`mintCode`/`consumeCode`, `IsAccountAuthorizedForOIDCClient`, the `Provider`). Backend-only — **no SPA/dist changes** (login + app-access-denied `/error` are existing).

**Spec:** `docs/superpowers/specs/2026-06-21-forward-auth-phase1-design.md`

**Conventions (project memory):**
- Build: `go build -tags nodynamic ./...`; gate: that + `go vet ./...` + `go test ./...`. NO `Co-Authored-By` trailer.
- Migrations: goose (`-- +goose Up`/`Down`), embedded + auto-applied on boot; next number is **018**.
- sqlc: query source in `db/queries/*.sql`; regenerate with `sqlc generate` (sqlc 1.30.0, available via mise — run from repo root where `sqlc.yaml` lives). IDE diagnostics go stale after gen — trust `go build`/`go test`.
- Runtime verification: the harness kills servers launched from the controller (exit 144) — use a **subagent** to hold a live server; `mise run db:start` is broken (podman Postgres already up on :5432, DB `prohibitorum_dev`, user/pass `prohibitorum`); `source scripts/dev-env.sh` for the DSN.
- Public routes registered via `registerOpHTTP(s.router, "GET", path, contract.AuthRequirement{Kind: contract.AuthPublic}, handler)`; root-level non-API routes via `s.router.Get(path, handler)` (e.g. `/oauth/*`).

---

### Task 1: Schema + DB layer + config

**Goal:** Add the forward-auth columns to `oidc_client`, the two queries to resolve/flag a forward-auth client, and the config block.

**Files:**
- Create: `db/migrations/018_forward_auth.sql`
- Modify: `db/queries/oidc.sql` (add 2 queries), then `sqlc generate` (regenerates `pkg/db/oidc.sql.go`)
- Modify: `pkg/configx/configx.go` (`ForwardAuthConfig`)

**Acceptance Criteria:**
- [ ] Migration `018` adds `forward_auth_enabled boolean NOT NULL DEFAULT false` + `forward_auth_host text NULL` to `oidc_client`, with a partial unique index on `forward_auth_host` where `forward_auth_enabled`.
- [ ] `GetForwardAuthClientByHost(host)` returns `{client_id, display_name, access_restricted, disabled}` for the enabled client matching that host.
- [ ] `SetForwardAuthConfig(client_id, enabled, host)` updates those columns.
- [ ] `configx.Config.ForwardAuth.SessionTTL` defaults to 1h.
- [ ] `go build -tags nodynamic ./...` → 0.

**Verify:** `sqlc generate && go build -tags nodynamic ./...` → 0

**Steps:**

- [ ] **Step 1: Migration** `db/migrations/018_forward_auth.sql`:

```sql
-- +goose Up
-- Forward-auth: a protected service is a normal oidc_client flagged for
-- forward-auth. forward_auth_host is the X-Forwarded-Host the verify endpoint
-- matches to resolve the backing client.
ALTER TABLE oidc_client
  ADD COLUMN IF NOT EXISTS forward_auth_enabled boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS forward_auth_host text NULL;

-- One forward-auth client per host (only among forward-auth-enabled rows).
CREATE UNIQUE INDEX IF NOT EXISTS oidc_client_forward_auth_host_uq
  ON oidc_client(forward_auth_host) WHERE forward_auth_enabled;

-- +goose Down
DROP INDEX IF EXISTS oidc_client_forward_auth_host_uq;
ALTER TABLE oidc_client
  DROP COLUMN IF EXISTS forward_auth_enabled,
  DROP COLUMN IF EXISTS forward_auth_host;
```

- [ ] **Step 2: queries** — append to `db/queries/oidc.sql`:

```sql
-- name: GetForwardAuthClientByHost :one
SELECT client_id, display_name, access_restricted, disabled
FROM oidc_client
WHERE forward_auth_enabled = true AND forward_auth_host = $1;

-- name: SetForwardAuthConfig :exec
UPDATE oidc_client
SET forward_auth_enabled = $2, forward_auth_host = $3
WHERE client_id = $1;
```

- [ ] **Step 3: regenerate** — `sqlc generate` (from repo root). Confirm `pkg/db/oidc.sql.go` gains `GetForwardAuthClientByHost` + `SetForwardAuthConfig` + the row/param types. (Stale IDE diagnostics are expected; trust the build.)

- [ ] **Step 4: config** — in `pkg/configx/configx.go`, add to `Config`:

```go
	ForwardAuth ForwardAuthConfig `mapstructure:"forward_auth"`
```
add the type:
```go
// ForwardAuthConfig configures the native Traefik ForwardAuth provider.
// SessionTTL bounds the per-domain forward-auth cookie/session lifetime.
type ForwardAuthConfig struct {
	SessionTTL time.Duration `mapstructure:"session_ttl"`
}
```
and in the defaults function:
```go
	if config.ForwardAuth.SessionTTL <= 0 {
		config.ForwardAuth.SessionTTL = time.Hour
	}
```

- [ ] **Step 5: build + commit**

```bash
go build -tags nodynamic ./... && go vet ./pkg/db/... ./pkg/configx/...
git add db/migrations/018_forward_auth.sql db/queries/oidc.sql pkg/db/ pkg/configx/configx.go
git commit -m "feat(forward-auth): oidc_client forward-auth columns + queries + config"
```

---

### Task 2: forward-auth building blocks (KV session, cookie, state, PKCE, headers)

**Goal:** The pure, unit-testable pieces in a new `forward_auth.go`: per-domain fa-session in KV, the per-domain cookie helper, the single-use state (carrying the original URL + PKCE verifier), the PKCE S256 helper, and the identity-header writer.

**Files:**
- Create: `pkg/protocol/oidc/forward_auth.go`
- Create: `pkg/protocol/oidc/forward_auth_test.go`

**Acceptance Criteria:**
- [ ] `faSession` round-trips through KV (`mintFASession`/`loadFASession`); a missing/expired token returns nil.
- [ ] `faState` round-trips and is single-use (`mintFAState`/`popFAState` — second pop misses).
- [ ] `pkceChallengeS256(verifier)` matches a known vector; `verifyPKCE(verifier, challenge)` true/false correctly.
- [ ] `faCookie(secure, token)` is host-only (`__Host-` prefix when secure, plain name otherwise), `Secure`/`HttpOnly`/`Path=/`/`SameSite=Lax`.
- [ ] `writeIdentityHeaders` sets `Remote-User/Name/Email/Groups` (Email omitted when empty).

**Verify:** `go test ./pkg/protocol/oidc/ -run ForwardAuth -count=1` → ok

**Steps:**

- [ ] **Step 1: write the failing test** `pkg/protocol/oidc/forward_auth_test.go`:

```go
package oidc

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"prohibitorum/pkg/kv"
)

func TestForwardAuth_PKCE_S256(t *testing.T) {
	// RFC 7636 Appendix B vector.
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := pkceChallengeS256(verifier); got != want {
		t.Fatalf("pkceChallengeS256 = %q, want %q", got, want)
	}
	if !verifyPKCE(verifier, want) {
		t.Fatal("verifyPKCE should accept the matching verifier")
	}
	if verifyPKCE("wrong", want) {
		t.Fatal("verifyPKCE should reject a wrong verifier")
	}
}

func TestForwardAuth_Session_RoundTrip(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	tok, err := mintFASession(ctx, store, faSession{AccountID: 42, ClientID: "svc"}, time.Hour)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	got := loadFASession(ctx, store, tok)
	if got == nil || got.AccountID != 42 || got.ClientID != "svc" {
		t.Fatalf("load = %+v", got)
	}
	if loadFASession(ctx, store, "nonexistent") != nil {
		t.Fatal("missing token should load nil")
	}
}

func TestForwardAuth_State_SingleUse(t *testing.T) {
	ctx := context.Background()
	store := kv.NewMemoryStore()
	id, err := mintFAState(ctx, store, faState{OriginalURL: "https://app.acme.io/foo", ClientID: "svc", Verifier: "v"}, 5*time.Minute)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	st := popFAState(ctx, store, id)
	if st == nil || st.OriginalURL != "https://app.acme.io/foo" {
		t.Fatalf("pop = %+v", st)
	}
	if popFAState(ctx, store, id) != nil {
		t.Fatal("state must be single-use")
	}
}

func TestForwardAuth_Cookie_HostOnly(t *testing.T) {
	c := faCookie(true, "tok")
	if c.Name != "__Host-"+forwardAuthCookieBase || !c.Secure || !c.HttpOnly || c.Path != "/" || c.Domain != "" {
		t.Fatalf("secure cookie wrong: %+v", c)
	}
	if c2 := faCookie(false, "tok"); c2.Name != forwardAuthCookieBase || c2.Secure {
		t.Fatalf("insecure cookie wrong: %+v", c2)
	}
}

func TestForwardAuth_IdentityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	writeIdentityHeaders(rec, "alice", "Alice A", "alice@example.com", []string{"admins", "staff"})
	h := rec.Header()
	if h.Get("Remote-User") != "alice" || h.Get("Remote-Name") != "Alice A" ||
		h.Get("Remote-Email") != "alice@example.com" || h.Get("Remote-Groups") != "admins,staff" {
		t.Fatalf("headers: %v", h)
	}
	rec2 := httptest.NewRecorder()
	writeIdentityHeaders(rec2, "bob", "Bob", "", nil)
	if _, ok := rec2.Header()["Remote-Email"]; ok {
		t.Fatal("empty email must be omitted")
	}
}
```

- [ ] **Step 2: implement** `pkg/protocol/oidc/forward_auth.go` (building blocks; handlers added in Tasks 3–4):

```go
package oidc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"prohibitorum/pkg/kv"
)

// ForwardAuthPathPrefix is the fixed path, routed by the operator on each
// protected domain to Prohibitorum, under which the OIDC callback lands (so the
// per-domain cookie is scoped to the protected host). Mirrors Authentik's
// /outpost.goauthentik.io/* convention.
const ForwardAuthPathPrefix = "/.prohibitorum-forward-auth"

// forwardAuthCookieBase is the per-domain forward-auth cookie's base name; the
// __Host- prefix is added on secure (HTTPS) deployments.
const forwardAuthCookieBase = "prohibitorum_forward_auth"

// faSession is the per-domain forward-auth session stored in KV under fa:<token>.
type faSession struct {
	AccountID int32  `json:"account_id"`
	ClientID  string `json:"client_id"`
}

func faSessionKey(token string) string { return "fa:session:" + token }

func mintFASession(ctx context.Context, store kv.Store, s faSession, ttl time.Duration) (string, error) {
	tok, err := randToken()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	if err := store.SetEx(ctx, faSessionKey(tok), string(payload), ttl); err != nil {
		return "", err
	}
	return tok, nil
}

func loadFASession(ctx context.Context, store kv.Store, token string) *faSession {
	if token == "" {
		return nil
	}
	raw, err := store.Get(ctx, faSessionKey(token))
	if err != nil || raw == "" {
		return nil
	}
	var s faSession
	if json.Unmarshal([]byte(raw), &s) != nil {
		return nil
	}
	return &s
}

// faState binds a forward-auth flow: the single-use OIDC state value carrying
// the original URL to return to + the PKCE verifier. Popped once at callback.
type faState struct {
	OriginalURL string `json:"original_url"`
	ClientID    string `json:"client_id"`
	Verifier    string `json:"verifier"`
}

func faStateKey(id string) string { return "fa:state:" + id }

func mintFAState(ctx context.Context, store kv.Store, s faState, ttl time.Duration) (string, error) {
	id, err := randToken()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	if err := store.SetEx(ctx, faStateKey(id), string(payload), ttl); err != nil {
		return "", err
	}
	return id, nil
}

func popFAState(ctx context.Context, store kv.Store, id string) *faState {
	if id == "" {
		return nil
	}
	raw, err := store.Pop(ctx, faStateKey(id))
	if err != nil || raw == "" {
		return nil
	}
	var s faState
	if json.Unmarshal([]byte(raw), &s) != nil {
		return nil
	}
	return &s
}

// randToken returns 32 bytes of base64url randomness. Reuses the same primitive
// shape as mintCode (see codes.go); kept local to avoid coupling.
func randToken() (string, error) {
	// Reuse the existing helper if codes.go exposes one; otherwise:
	return newRandToken32()
}

// pkceChallengeS256 returns base64url(sha256(verifier)) (RFC 7636 §4.2).
func pkceChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func verifyPKCE(verifier, challenge string) bool {
	return verifier != "" && challenge != "" && pkceChallengeS256(verifier) == challenge
}

// faCookie builds the per-domain forward-auth cookie (host-only). __Host- prefix
// + Secure on HTTPS; plain name on HTTP (dev). SameSite=Lax survives the
// top-level GET callback→original redirect.
func faCookie(secure bool, token string) *http.Cookie {
	name := forwardAuthCookieBase
	if secure {
		name = "__Host-" + forwardAuthCookieBase
	}
	return &http.Cookie{
		Name:     name,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func faCookieName(secure bool) string {
	if secure {
		return "__Host-" + forwardAuthCookieBase
	}
	return forwardAuthCookieBase
}

// writeIdentityHeaders sets the Authelia-convention identity headers on a 200
// allow response. Email is omitted entirely when empty.
func writeIdentityHeaders(w http.ResponseWriter, user, name, email string, groups []string) {
	w.Header().Set("Remote-User", user)
	w.Header().Set("Remote-Name", name)
	if email != "" {
		w.Header().Set("Remote-Email", email)
	}
	w.Header().Set("Remote-Groups", strings.Join(groups, ","))
}

var errFANoState = errors.New("oidc: forward-auth state missing or invalid")
```

- [ ] **Step 3: provide `newRandToken32`.** Check whether `codes.go` already has a reusable random-token helper (grep `RawURLEncoding` / `rand.Read` in `pkg/protocol/oidc`). If yes, replace `randToken()`'s body with a call to it and delete `newRandToken32`. If not, add to `forward_auth.go`:

```go
func newRandToken32() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
```
and add `"crypto/rand"` to the imports.

- [ ] **Step 4: run tests + build**

Run: `go test ./pkg/protocol/oidc/ -run ForwardAuth -count=1 && go build -tags nodynamic ./...`
Expected: PASS + 0.

- [ ] **Step 5: commit**

```bash
git add pkg/protocol/oidc/forward_auth.go pkg/protocol/oidc/forward_auth_test.go
git commit -m "feat(forward-auth): KV session/state, per-domain cookie, PKCE, identity headers"
```

---

### Task 3: `HandleForwardAuthVerify`

**Goal:** The ForwardAuth target endpoint: resolve the client by `X-Forwarded-Host`, validate the per-domain cookie + live RBAC → `200` + identity headers; otherwise `302` into `/oauth/authorize`; unknown host → `403`.

**Files:**
- Modify: `pkg/protocol/oidc/forward_auth.go` (add the method)
- Modify: `pkg/protocol/oidc/forward_auth_test.go` (add tests)

**Acceptance Criteria:**
- [ ] Valid fa-cookie + authorized + enabled account → `200` with `Remote-*` headers.
- [ ] No/invalid cookie → `302` whose `Location` is `<issuer>/oauth/authorize?...` with `client_id`, the exact `redirect_uri` `https://<host>/.prohibitorum-forward-auth/callback`, `response_type=code`, `code_challenge_method=S256`, a non-empty `code_challenge`, and a `state`.
- [ ] Valid cookie but `IsAccountAuthorizedForOIDCClient` now false → `302` (re-auth), not `200`.
- [ ] `X-Forwarded-Host` matching no forward-auth client → `403`.

**Verify:** `go test ./pkg/protocol/oidc/ -run ForwardAuthVerify -count=1` → ok

**Steps:**

- [ ] **Step 1: add the handler** to `forward_auth.go`:

```go
// HandleForwardAuthVerify is the Traefik ForwardAuth target. It is reached via
// the middleware's fixed address; Traefik forwards X-Forwarded-* + the original
// (protected-domain) cookies. 200 = allow (+ identity headers); 302 = bootstrap
// auth via /oauth/authorize; 403 = the host is not a registered forward-auth app.
func (p *Provider) HandleForwardAuthVerify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	host := r.Header.Get("X-Forwarded-Host")
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		proto = "https"
	}
	client, err := p.queries.GetForwardAuthClientByHost(ctx, pgText(host))
	if err != nil || client.Disabled {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	secure := proto == "https"

	// Steady state: a valid per-domain cookie whose session is still authorized.
	if c, cerr := r.Cookie(faCookieName(secure)); cerr == nil {
		if sess := loadFASession(ctx, p.kv, c.Value); sess != nil && sess.ClientID == client.ClientID {
			ok, aerr := p.queries.IsAccountAuthorizedForOIDCClient(ctx, db.IsAccountAuthorizedForOIDCClientParams{
				AccountID: sess.AccountID, ClientID: client.ClientID,
			})
			if aerr == nil && ok {
				if acct, gerr := p.queries.GetAccountByID(ctx, sess.AccountID); gerr == nil && !acct.Disabled {
					groups, _ := p.queries.ListExposedGroupSlugsByAccount(ctx, acct.ID)
					writeIdentityHeaders(w, acct.Username, acct.DisplayName, accountEmail(acct), groups)
					w.WriteHeader(http.StatusOK)
					return
				}
			}
		}
	}

	// Bootstrap: redirect into our own OIDC authorize with PKCE + a single-use
	// state carrying the original URL. The original URL comes from X-Forwarded-*.
	original := proto + "://" + host + r.Header.Get("X-Forwarded-Uri")
	verifier, verr := newRandToken32()
	if verr != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	challenge := pkceChallengeS256(verifier)
	stateID, serr := mintFAState(ctx, p.kv, faState{
		OriginalURL: original, ClientID: client.ClientID, Verifier: verifier,
	}, 5*time.Minute)
	if serr != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	redirectURI := proto + "://" + host + ForwardAuthPathPrefix + "/callback"
	q := url.Values{}
	q.Set("client_id", client.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", "openid email groups")
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", stateID)
	http.Redirect(w, r, p.cfg.OIDC.Issuer+"/oauth/authorize?"+q.Encode(), http.StatusFound)
}
```

- [ ] **Step 2: add helpers** `pgText` and `accountEmail` (confirm `db.Account`'s email field name + type by reading `pkg/db` — likely `Email pgtype.Text`; adapt). Add to `forward_auth.go`:

```go
func pgText(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }

// accountEmail returns the account's email if set+valid, else "".
func accountEmail(a db.Account) string {
	if a.Email.Valid {
		return a.Email.String
	}
	return ""
}
```
Add imports `"net/url"`, `"prohibitorum/pkg/db"`, `"github.com/jackc/pgx/v5/pgtype"`. (If `db.Account` has no `Email` field, use whatever field the OIDC `email` claim reads in `claims.go` — grep `email` in `claims.go` to confirm the source; adapt `accountEmail` accordingly. Do NOT invent a field.)

- [ ] **Step 3: tests** — add to `forward_auth_test.go`. Build a `*Provider` with a fake `db.Querier` (grep existing oidc tests, e.g. `authorize_test.go`, for the fake querier + `newTestProvider` helper; reuse it). The fake must implement `GetForwardAuthClientByHost`, `IsAccountAuthorizedForOIDCClient`, `GetAccountByID`, `ListExposedGroupSlugsByAccount`. Use a memory KV.

```go
func TestForwardAuthVerify_UnknownHost_403(t *testing.T) {
	p := newForwardAuthTestProvider(t /* fake returns ErrNoRows for any host */)
	req := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/forward-auth/verify", nil)
	req.Header.Set("X-Forwarded-Host", "nope.example")
	rec := httptest.NewRecorder()
	p.HandleForwardAuthVerify(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d want 403", rec.Code)
	}
}

func TestForwardAuthVerify_NoCookie_RedirectsToAuthorize(t *testing.T) {
	p := newForwardAuthTestProvider(t /* host app.acme.io → client "svc" */)
	req := httptest.NewRequest(http.MethodGet, "/api/prohibitorum/forward-auth/verify", nil)
	req.Header.Set("X-Forwarded-Host", "app.acme.io")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Uri", "/foo?x=1")
	rec := httptest.NewRecorder()
	p.HandleForwardAuthVerify(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d want 302", rec.Code)
	}
	loc, _ := rec.Result().Location()
	if loc == nil || !strings.Contains(loc.String(), "/oauth/authorize") {
		t.Fatalf("location=%v", loc)
	}
	qq := loc.Query()
	if qq.Get("client_id") != "svc" ||
		qq.Get("redirect_uri") != "https://app.acme.io/.prohibitorum-forward-auth/callback" ||
		qq.Get("code_challenge_method") != "S256" || qq.Get("code_challenge") == "" || qq.Get("state") == "" {
		t.Fatalf("authorize params: %v", qq)
	}
}

func TestForwardAuthVerify_ValidCookie_200WithHeaders(t *testing.T) {
	// Pre-seed a fa-session in the provider's KV for account 42 / client "svc";
	// fake IsAccountAuthorizedForOIDCClient → true; GetAccountByID → enabled acct
	// {Username:"alice", DisplayName:"Alice", Email valid "alice@x"};
	// ListExposedGroupSlugsByAccount → ["admins"].
	// Set the cookie __Host-prohibitorum_forward_auth=<token> (secure path) on the request.
	// Expect 200 + Remote-User=alice + Remote-Groups=admins.
}

func TestForwardAuthVerify_RevokedAccess_Redirects(t *testing.T) {
	// Same as above but IsAccountAuthorizedForOIDCClient → false → expect 302.
}
```

Implement `newForwardAuthTestProvider` mirroring the existing oidc test provider construction (memory KV, fake querier). If the existing fake querier is hard to extend, add the four methods to it.

- [ ] **Step 4: run + commit**

```bash
go test ./pkg/protocol/oidc/ -run ForwardAuth -count=1 && go build -tags nodynamic ./...
git add pkg/protocol/oidc/forward_auth.go pkg/protocol/oidc/forward_auth_test.go
git commit -m "feat(forward-auth): verify endpoint (200/302/403 + live RBAC + identity headers)"
```

---

### Task 4: `HandleForwardAuthCallback`

**Goal:** On the protected domain, redeem the OIDC code in-process, verify PKCE + binding, mint the fa-session, set the per-domain cookie, and 302 to the original URL.

**Files:**
- Modify: `pkg/protocol/oidc/forward_auth.go` (add the method)
- Modify: `pkg/protocol/oidc/forward_auth_test.go` (add tests)

**Acceptance Criteria:**
- [ ] Valid `code`+`state` (PKCE matches, client+redirect_uri match) → mints fa-session, sets the host-only cookie, `302` to `state.OriginalURL`.
- [ ] Missing/used `state` → redirect to `/error?error=server_error` (no cookie set).
- [ ] PKCE mismatch, or `authCode.ClientID` ≠ `state.ClientID`, or redirect_uri mismatch → reject (redirect to `/error`), no cookie.
- [ ] A used `code` (second callback) → reject.

**Verify:** `go test ./pkg/protocol/oidc/ -run ForwardAuthCallback -count=1` → ok

**Steps:**

- [ ] **Step 1: add the handler** to `forward_auth.go`:

```go
// HandleForwardAuthCallback is reached on the protected domain via the
// operator-routed ForwardAuthPathPrefix. It redeems the OIDC code IN-PROCESS
// (consumeCode — no token round-trip), verifies PKCE + the flow binding, mints
// the per-domain forward-auth session, sets the host-only cookie, and 302s to
// the original URL carried in the single-use state.
func (p *Provider) HandleForwardAuthCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	code := r.URL.Query().Get("code")
	stateID := r.URL.Query().Get("state")

	st := popFAState(ctx, p.kv, stateID)
	if st == nil || code == "" {
		p.redirectToErrorPage(w, r, errCodeServerError)
		return
	}
	ac, err := consumeCode(ctx, p.kv, code)
	if err != nil {
		p.redirectToErrorPage(w, r, errCodeServerError)
		return
	}
	// Bind: the code must belong to the same client the state was minted for,
	// PKCE must match the state's verifier, and the redirect_uri must be the
	// callback on that client's host.
	expectedRedirect := schemeOf(r) + "://" + hostOf(r) + ForwardAuthPathPrefix + "/callback"
	if ac.ClientID != st.ClientID || !verifyPKCE(st.Verifier, ac.CodeChallenge) || ac.RedirectURI != expectedRedirect {
		p.redirectToErrorPage(w, r, errCodeServerError)
		return
	}

	tok, mErr := mintFASession(ctx, p.kv, faSession{AccountID: ac.AccountID, ClientID: ac.ClientID}, p.cfg.ForwardAuth.SessionTTL)
	if mErr != nil {
		p.redirectToErrorPage(w, r, errCodeServerError)
		return
	}
	http.SetCookie(w, faCookie(schemeOf(r) == "https", tok))
	http.Redirect(w, r, st.OriginalURL, http.StatusFound)
}

// schemeOf/hostOf read the protected-domain identity. The callback is reached
// directly on the protected domain (routed by Traefik), so the proxy presents
// it via X-Forwarded-* (preferred) or the request Host as a fallback.
func schemeOf(r *http.Request) string {
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		return p
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func hostOf(r *http.Request) string {
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		return h
	}
	return r.Host
}
```

(`redirectToErrorPage`, `errCodeServerError`, `consumeCode`, and `authCode` all already exist in this package — reuse them.)

- [ ] **Step 2: tests** — add to `forward_auth_test.go`:

```go
func TestForwardAuthCallback_Success(t *testing.T) {
	p := newForwardAuthTestProvider(t)
	ctx := context.Background()
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	stateID, _ := mintFAState(ctx, p.kv, faState{
		OriginalURL: "https://app.acme.io/foo", ClientID: "svc", Verifier: verifier,
	}, 5*time.Minute)
	code, _ := mintCode(ctx, p.kv, authCode{
		ClientID: "svc", AccountID: 42,
		RedirectURI:         "https://app.acme.io/.prohibitorum-forward-auth/callback",
		CodeChallenge:       pkceChallengeS256(verifier),
		CodeChallengeMethod: "S256",
	}, 5*time.Minute)

	req := httptest.NewRequest(http.MethodGet, "https://app.acme.io"+ForwardAuthPathPrefix+"/callback?code="+code+"&state="+stateID, nil)
	req.Header.Set("X-Forwarded-Host", "app.acme.io")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	p.HandleForwardAuthCallback(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d want 302; body=%s", rec.Code, rec.Body.String())
	}
	if loc, _ := rec.Result().Location(); loc == nil || loc.String() != "https://app.acme.io/foo" {
		t.Fatalf("location=%v", loc)
	}
	// Cookie set + its session loads account 42.
	var faCookieVal string
	for _, c := range rec.Result().Cookies() {
		if c.Name == "__Host-"+forwardAuthCookieBase {
			faCookieVal = c.Value
		}
	}
	if faCookieVal == "" {
		t.Fatal("forward-auth cookie not set")
	}
	if s := loadFASession(ctx, p.kv, faCookieVal); s == nil || s.AccountID != 42 {
		t.Fatalf("fa-session = %+v", s)
	}
}

func TestForwardAuthCallback_PKCEMismatch_Rejected(t *testing.T) {
	// state.Verifier doesn't match authCode.CodeChallenge → 302 to /error, no cookie.
}

func TestForwardAuthCallback_UsedState_Rejected(t *testing.T) {
	// popFAState already consumed → 302 to /error, no cookie.
}
```

- [ ] **Step 3: run + commit**

```bash
go test ./pkg/protocol/oidc/ -run ForwardAuth -count=1 && go build -tags nodynamic ./...
git add pkg/protocol/oidc/forward_auth.go pkg/protocol/oidc/forward_auth_test.go
git commit -m "feat(forward-auth): callback — in-process code redemption + per-domain cookie"
```

---

### Task 5: route registration

**Goal:** Wire the two endpoints into the chi router.

**Files:**
- Modify: `pkg/server/server.go`

**Acceptance Criteria:**
- [ ] `GET /api/prohibitorum/forward-auth/verify` → `s.oidcOP.HandleForwardAuthVerify` (public).
- [ ] `GET /.prohibitorum-forward-auth/callback` → `s.oidcOP.HandleForwardAuthCallback` (root-level, public — mirrors `/oauth/*`).
- [ ] `go build -tags nodynamic ./...` → 0; existing route tests still pass.

**Verify:** `go build -tags nodynamic ./... && go test ./pkg/server/ -run 'Route|Operations' -count=1`

**Steps:**

- [ ] **Step 1:** in `registerOperations()` (near the other public OIDC routes / `s.router.Get("/oauth/...")` block in `server.go`):

```go
	// Native Traefik ForwardAuth (see docs/forward-auth.md).
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/forward-auth/verify",
		contract.AuthRequirement{Kind: contract.AuthPublic}, s.oidcOP.HandleForwardAuthVerify)
	s.router.Get(oidcop.ForwardAuthPathPrefix+"/callback", s.oidcOP.HandleForwardAuthCallback)
```

Confirm the alias `oidcop` is how `pkg/protocol/oidc` is imported in `server.go` (grep `protocol/oidc` in server.go; it was imported as `oidcop` earlier). Use the actual alias.

- [ ] **Step 2: build + test + commit**

```bash
go build -tags nodynamic ./... && go test ./pkg/server/ -run 'Route|Operations|Sudo' -count=1
git add pkg/server/server.go
git commit -m "feat(server): register forward-auth verify + callback routes"
```

---

### Task 6: `forward-auth-app` CLI registration

**Goal:** A CLI to register a forward-auth app = create the backing OIDC client (public, PKCE, no consent, callback redirect_uri) + flag it for forward-auth.

**Files:**
- Modify: `cmd/prohibitorum/main.go` (add a `forward-auth-app` cobra command)

**Acceptance Criteria:**
- [ ] `prohibitorum forward-auth-app create --client-id svc --host app.acme.io [--display-name ...]` creates a public+PKCE OIDC client with `redirect_uri=https://app.acme.io/.prohibitorum-forward-auth/callback`, `require_consent=false`, then calls `SetForwardAuthConfig(client_id, true, host)`.
- [ ] Prints next-step guidance (grant access via the existing `oidc-client` access commands; the Traefik config in docs).

**Verify:** `go build -tags nodynamic ./... && ./prohibitorum forward-auth-app --help`

**Steps:**

- [ ] **Step 1:** read the existing `oidc-client create` command (`cmd/prohibitorum/main.go`, the `createClientCmd` block) for `BuildClientParams` + `InsertOIDCClient` usage. Add a sibling command that reuses them:

```go
	faAppCmd := &cobra.Command{Use: "forward-auth-app", Short: "Manage forward-auth protected services"}
	var faClientID, faHost, faDisplay string
	faCreate := &cobra.Command{
		Use:   "create",
		Short: "Register a forward-auth protected service (creates a backing OIDC client)",
		Run: func(cmd *cobra.Command, _ []string) {
			if faClientID == "" || faHost == "" {
				log.Fatal("forward-auth-app create: --client-id and --host are required")
			}
			redirect := "https://" + faHost + oidcop.ForwardAuthPathPrefix + "/callback"
			params, err := oidcop.BuildClientParams(oidcop.ClientParamsInput{
				ClientID:       faClientID,
				DisplayName:    faDisplay,
				RedirectURIs:   []string{redirect},
				Scopes:         []string{"openid", "email", "groups"},
				Public:         true,           // PKCE public client
				RequireConsent: false,          // first-party proxy auth — no consent screen
			})
			if err != nil {
				log.Fatalf("forward-auth-app create: build params: %v", err)
			}
			if err := q.InsertOIDCClient(ctx, params); err != nil {
				log.Fatalf("forward-auth-app create: insert client: %v", err)
			}
			if err := q.SetForwardAuthConfig(ctx, db.SetForwardAuthConfigParams{ClientID: faClientID, ForwardAuthEnabled: true, ForwardAuthHost: pgText(faHost)}); err != nil {
				log.Fatalf("forward-auth-app create: set forward-auth config: %v", err)
			}
			fmt.Printf("Registered forward-auth service %q for host %s\n", faClientID, faHost)
			fmt.Printf("Next: grant access (oidc-client … access grant) and configure Traefik per docs/forward-auth.md\n")
		},
	}
	faCreate.Flags().StringVar(&faClientID, "client-id", "", "Stable client identifier (required)")
	faCreate.Flags().StringVar(&faHost, "host", "", "Protected service host, e.g. app.acme.io (required)")
	faCreate.Flags().StringVar(&faDisplay, "display-name", "", "Human-readable name")
	faAppCmd.AddCommand(faCreate)
	cli.Root().AddCommand(faAppCmd)
```

Confirm the exact names of `BuildClientParams` / `ClientParamsInput` / `InsertOIDCClient` params + the `db.SetForwardAuthConfigParams` field names (`ForwardAuthEnabled`/`ForwardAuthHost`) from the sqlc-generated code; adapt. `pgText` is exported from neither package — define a tiny local `pgText` in main.go or build the `pgtype.Text` inline. Confirm `oidcop` is the import alias used in main.go (or import `pkg/protocol/oidc`).

- [ ] **Step 2: build + commit**

```bash
go build -tags nodynamic ./... && ./prohibitorum forward-auth-app --help
git add cmd/prohibitorum/main.go
git commit -m "feat(cli): forward-auth-app create (backing OIDC client + forward-auth flag)"
```

---

### Task 7: Traefik integration docs

**Goal:** An operator guide for wiring Traefik to the native forward-auth endpoints.

**Files:**
- Create: `docs/forward-auth.md`

**Acceptance Criteria:**
- [ ] Documents: register the app (`forward-auth-app create` + grant access); the ForwardAuth middleware → `https://<prohibitorum>/api/prohibitorum/forward-auth/verify`; the per-domain router for `Host(app.acme.io) && PathPrefix(/.prohibitorum-forward-auth/)` → Prohibitorum; `authResponseHeaders: Remote-User, Remote-Name, Remote-Email, Remote-Groups`; the EntryPoint `forwardedHeaders.trustedIPs` + middleware `trustForwardHeader: true`; HTTPS requirement; the "backend reachable only via Traefik / strip client Remote-* headers" rule.

**Verify:** `docs/forward-auth.md` exists with the labeled config blocks and the security-requirements section.

**Steps:**

- [ ] **Step 1:** write `docs/forward-auth.md` covering the items above, with a concrete dynamic-config example:

```yaml
http:
  middlewares:
    prohibitorum-forwardauth:
      forwardAuth:
        address: "https://auth.example.com/api/prohibitorum/forward-auth/verify"
        trustForwardHeader: true
        authResponseHeaders:
          - Remote-User
          - Remote-Name
          - Remote-Email
          - Remote-Groups
  routers:
    app:
      rule: "Host(`app.acme.io`)"
      middlewares: ["prohibitorum-forwardauth"]
      service: app-backend
    app-forwardauth-callback:        # routes the per-domain auth path to Prohibitorum
      rule: "Host(`app.acme.io`) && PathPrefix(`/.prohibitorum-forward-auth/`)"
      service: prohibitorum
```
plus the EntryPoint `forwardedHeaders.trustedIPs` snippet, the HTTPS-only note, and the "Traefik must be the sole ingress; strip any client-supplied `Remote-*`" security note (cite the spec's security section).

- [ ] **Step 2: commit**

```bash
git add docs/forward-auth.md
git commit -m "docs(forward-auth): Traefik integration guide"
```

---

### Task 8: Gate + runtime verification

**Goal:** Full gate green and the forward-auth cycle verified against a live server.

**Files:** none (verification); optional smoke addition to `cmd/smoke/main.go`.

**Acceptance Criteria:**
- [ ] `go vet ./... && go build -tags nodynamic ./... && go test ./...` all 0 (migration 018 applies; forward-auth tests pass).
- [ ] Runtime (subagent-launched server): `GET /api/prohibitorum/forward-auth/verify` with `X-Forwarded-Host` for an unknown host → 403; for a registered host with no cookie → 302 to `/oauth/authorize?...` with the right params; the full verify→authorize(→login)→callback→cookie→verify=200 cycle works with simulated forwarded headers + the cookie.

**Verify:** commands below exit 0; runtime evidence captured.

**Steps:**

- [ ] **Step 1: backend gate**

```bash
go vet ./... && go build -tags nodynamic ./... && go test ./...
```
Expected: all 0/ok (`pkg/server` may flake under parallel shared-DB — re-run in isolation if needed).

- [ ] **Step 2: runtime verification (subagent — the controller's own servers get killed).** Build `/tmp/proh-verify` from HEAD, `source scripts/dev-env.sh`, run on :8080 (migration 018 applies on boot). Then, using `curl` with `--max-redirs 0`:
  - Register a forward-auth app: `/tmp/proh-verify forward-auth-app create --client-id svc --host app.acme.io` (against the dev DB) — or insert directly. Grant access (or leave `access_restricted=false`).
  - `curl -s -o /dev/null -w "%{http_code} %{redirect_url}" -H 'X-Forwarded-Host: unknown.example' http://localhost:8080/api/prohibitorum/forward-auth/verify` → `403`.
  - `... -H 'X-Forwarded-Host: app.acme.io' -H 'X-Forwarded-Proto: https' -H 'X-Forwarded-Uri: /foo'` → `302` to `/oauth/authorize?...client_id=svc&...code_challenge_method=S256&state=...`.
  - (Cookie 200-path: mint a fa-session via a direct KV/DB path is awkward across processes; assert the verify-redirect + the unit tests cover the 200 path. Note this in the report.)
  Capture results; tear down.

- [ ] **Step 3: optional smoke** — if extending `cmd/smoke/main.go`, add a step asserting the unknown-host 403 + the registered-host 302-to-authorize shape, and run `mise run ci:smoke` (`SMOKE_EXIT=0`).

- [ ] **Step 4: final gate**

```bash
go build -tags nodynamic ./... && go test ./...
```
Expected: green. (No SPA/dist changes in Phase 1.)

---

## Self-Review Notes

- **Spec coverage:** entity (Task 1: oidc_client columns + queries), verify (Task 3), callback (Task 4), per-domain cookie + KV session + state + PKCE + headers (Task 2), routes (Task 5), CLI registration (Task 6), Traefik docs (Task 7), gate+runtime (Task 8). RBAC reuse = `IsAccountAuthorizedForOIDCClient` (Tasks 3). Open-redirect safety = signed/KV single-use state + redirect_uri exact-match (Tasks 2–4). Phase 2 (admin UI, dev harness, sign-out) explicitly out of scope.
- **Type consistency:** `faSession`/`faState`/`faCookie`/`faCookieName`/`mintFASession`/`loadFASession`/`mintFAState`/`popFAState`/`pkceChallengeS256`/`verifyPKCE`/`writeIdentityHeaders`/`ForwardAuthPathPrefix`/`forwardAuthCookieBase`/`newRandToken32` defined in Task 2 and used consistently in Tasks 3–5. `GetForwardAuthClientByHost`/`SetForwardAuthConfig` (Task 1) used in Tasks 3,6. `consumeCode`/`mintCode`/`authCode`/`IsAccountAuthorizedForOIDCClient`/`redirectToErrorPage`/`errCodeServerError` are existing symbols.
- **Verify-before-assert flags for the implementer (do NOT invent):** `db.Account`'s email field name/type (Task 3 `accountEmail`); the `oidcop` import alias in server.go + main.go (Tasks 5,6); `BuildClientParams`/`ClientParamsInput`/`InsertOIDCClient` exact shapes + `db.SetForwardAuthConfigParams` field names (Task 6); whether a reusable random-token helper already exists in `pkg/protocol/oidc` (Task 2 Step 3).
