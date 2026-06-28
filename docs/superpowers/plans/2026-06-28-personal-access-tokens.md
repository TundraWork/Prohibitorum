# Personal Access Tokens — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add user-owned Personal Access Tokens (PATs) that authenticate programmatic requests at the existing forward-auth gateway, resolving to the owning user with reduced privileges.

**Architecture:** A PAT is a high-entropy bearer credential owned by an `account`. It is accepted **only** at the forward-auth verify endpoint (`HandleForwardAuthVerify`), as a branch that takes precedence over the cookie path and is terminal (200/401/403, never a redirect). On success the gateway emits the same authoritative `Remote-*` identity headers a browser session would, plus a new `Remote-Scopes` header carrying the PAT's opaque `upstream_scopes`. Self-service create/list/revoke lives under `/me/tokens` with a dashboard page mirroring Sessions.

**Tech Stack:** Go (chi + huma + sqlc/pgx + goose migrations), Vue 3 + Vite + Tailwind v4 + shadcn-vue, KeyDB/memory KV. Spec: `docs/superpowers/specs/2026-06-28-personal-access-tokens-design.md`.

**User decisions (already made):**
- "PATs, not machine accounts / client credentials" — user-owned, token acts as the owner.
- "Keep the forward-auth verifier model" — no reverse proxy; header-stripping is a documented Traefik responsibility.
- "Use `Remote-Scopes`" in the always-emit authoritative header set (`Remote-User/Name/Email/Groups/Scopes`).
- "If `Authorization: Bearer` is present, treat the request as API/PAT mode regardless of cookie" — terminal precedence.
- "Store a non-secret `token_hint` for the list UI."
- SHA-256 hot-path hashing (not argon2id); prefixed token shown once; sudo-gated create.
- `upstream_scopes` are opaque, upstream-enforced; the caller never sends scope/identity headers.

---

## File Structure

**Backend (create):**
- `db/migrations/023_personal_access_token.sql` — table.
- `db/queries/personal_access_token.sql` — sqlc queries.
- `pkg/credential/pat/pat.go` + `pat_test.go` — token generate/hash/hint (pure crypto, no db).
- `pkg/server/handle_me_tokens.go` + `handle_me_tokens_test.go` — `/me/tokens` handlers.

**Backend (modify):**
- `pkg/db/*` — sqlc-generated (do not hand-edit; regenerate).
- `pkg/protocol/oidc/forward_auth.go` — `writeIdentityHeaders` signature + PAT verify branch.
- `pkg/protocol/oidc/forward_auth_test.go` — update header test; add PAT verify tests.
- `pkg/audit/event.go` — `FactorPAT`.
- `pkg/contract/auth.go` — view structs + operations.
- `pkg/server/server.go` — route registrations.

**Frontend (create):**
- `dashboard/src/pages/TokensView.vue` — list + revoke + create dialog + reveal-once.

**Frontend (modify):**
- `dashboard/src/router/index.ts`, `dashboard/src/components/custom/AppSidebar.vue`,
  `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts`.

**Docs (modify):** `docs/forward-auth.md`, `api.md`.

---

### Task 0: DB migration + sqlc queries for `personal_access_token`

**Goal:** Create the PAT table and its sqlc queries; regenerate `pkg/db`.

**Files:**
- Create: `db/migrations/023_personal_access_token.sql`
- Create: `db/queries/personal_access_token.sql`
- Modify (generated): `pkg/db/models.go`, `pkg/db/personal_access_token.sql.go`, `pkg/db/querier.go`

**Acceptance Criteria:**
- [ ] Migration `023` applies cleanly and `down` drops the table.
- [ ] `sqlc generate` produces `InsertPAT`, `ListPATsByAccount`, `GetPATByTokenHash`, `RevokePAT`, `TouchPATLastUsed` on `db.Querier`.
- [ ] `go build -tags nodynamic ./...` succeeds.

**Verify:** `sqlc generate && go build -tags nodynamic ./...` → no errors; `git status` shows regenerated `pkg/db` files.

**Steps:**

- [ ] **Step 1: Write the migration.** Create `db/migrations/023_personal_access_token.sql`:

```sql
-- +goose Up
-- 023_personal_access_token.sql — user-owned Personal Access Tokens (PATs) for
-- programmatic access at the forward-auth gateway. A PAT authenticates AS its
-- owning account with reduced privileges. token_hash = sha256(raw token);
-- token_hint is a non-secret display aid. allowed_client_ids optionally
-- restricts the PAT to specific forward-auth apps (empty = any app the owner may
-- reach). upstream_scopes are opaque labels emitted as the Remote-Scopes header.
CREATE TABLE personal_access_token (
  id                 serial PRIMARY KEY,
  account_id         integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  name               text NOT NULL,
  token_hash         bytea NOT NULL UNIQUE,
  token_hint         text NOT NULL,
  upstream_scopes    text[] NOT NULL DEFAULT '{}',
  allowed_client_ids text[] NOT NULL DEFAULT '{}',
  created_at         timestamptz NOT NULL DEFAULT now(),
  expires_at         timestamptz,
  last_used_at       timestamptz,
  revoked_at         timestamptz
);
CREATE INDEX personal_access_token_account_idx ON personal_access_token(account_id);

-- +goose Down
DROP TABLE IF EXISTS personal_access_token;
```

- [ ] **Step 2: Write the queries.** Create `db/queries/personal_access_token.sql`:

```sql
-- name: InsertPAT :one
INSERT INTO personal_access_token (
  account_id, name, token_hash, token_hint, upstream_scopes, allowed_client_ids, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListPATsByAccount :many
SELECT * FROM personal_access_token
WHERE account_id = $1 AND revoked_at IS NULL
ORDER BY created_at DESC;

-- name: GetPATByTokenHash :one
SELECT * FROM personal_access_token
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now());

-- name: RevokePAT :execrows
UPDATE personal_access_token
SET revoked_at = now()
WHERE id = $1 AND account_id = $2 AND revoked_at IS NULL;

-- name: TouchPATLastUsed :exec
UPDATE personal_access_token
SET last_used_at = now()
WHERE id = $1
  AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');
```

- [ ] **Step 3: Regenerate sqlc.** Run from the repo root (mise provides sqlc 1.30.0; config is `sqlc.yaml`):

Run: `sqlc generate`
Expected: exit 0; new file `pkg/db/personal_access_token.sql.go`; `PersonalAccessToken` struct added to `pkg/db/models.go`; new methods on `pkg/db/querier.go`.

- [ ] **Step 4: Build.**

Run: `go build -tags nodynamic ./...`
Expected: exit 0.

- [ ] **Step 5: Commit.**

```bash
git add db/migrations/023_personal_access_token.sql db/queries/personal_access_token.sql pkg/db
git commit -m "feat(pat): personal_access_token table + sqlc queries"
```

---

### Task 1: `pat` crypto package (generate / hash / hint)

**Goal:** A leaf package producing PAT plaintext, its SHA-256 hash, and a non-secret hint — no DB dependency, so both the gateway and the handlers can import it.

**Files:**
- Create: `pkg/credential/pat/pat.go`
- Test: `pkg/credential/pat/pat_test.go`

**Acceptance Criteria:**
- [ ] `Generate()` returns `raw` prefixed with `prohibitorum_pat_`, a 32-byte SHA-256 `hash`, and a `hint`.
- [ ] `HashToken(raw)` equals the hash from `Generate()` for the same `raw`.
- [ ] `Hint(raw)` is `prefix + "…" + last 4 chars`; two `Generate()` calls differ.

**Verify:** `go test ./pkg/credential/pat/ -v` → PASS.

**Steps:**

- [ ] **Step 1: Write the failing test.** Create `pkg/credential/pat/pat_test.go`:

```go
package pat

import (
	"crypto/sha256"
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	raw, hash, hint, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.HasPrefix(raw, Prefix) {
		t.Errorf("raw missing prefix: %q", raw)
	}
	want := sha256.Sum256([]byte(raw))
	if string(hash) != string(want[:]) {
		t.Error("hash != sha256(raw)")
	}
	if !strings.HasPrefix(hint, Prefix) || !strings.HasSuffix(hint, raw[len(raw)-4:]) {
		t.Errorf("hint format wrong: %q", hint)
	}
	raw2, _, _, _ := Generate()
	if raw == raw2 {
		t.Error("two Generate() calls must differ")
	}
}

func TestHashTokenDeterministic(t *testing.T) {
	raw, hash, _, _ := Generate()
	if string(HashToken(raw)) != string(hash) {
		t.Error("HashToken(raw) != Generate hash")
	}
}

func TestHintShortInput(t *testing.T) {
	if got := Hint("abc"); got != Prefix+"…abc" {
		t.Errorf("Hint(short) = %q", got)
	}
}
```

- [ ] **Step 2: Run the test (fails — package missing).**

Run: `go test ./pkg/credential/pat/ -v`
Expected: FAIL (build error: undefined `Generate`/`Prefix`/`HashToken`/`Hint`).

- [ ] **Step 3: Write the implementation.** Create `pkg/credential/pat/pat.go`:

```go
// Package pat implements user-owned Personal Access Tokens: a high-entropy
// bearer credential a user presents at the forward-auth gateway. Tokens are
// validated on the request hot path, so they are hashed with SHA-256 (fast,
// safe for 256-bit random secrets) — NOT argon2id, which is for low-entropy
// passwords verified off the hot path.
package pat

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// Prefix is the fixed, recognizable token prefix. It enables secret-scanning
// tools to flag a leaked PAT.
const Prefix = "prohibitorum_pat_"

// Generate returns a new PAT: the plaintext (shown to the user exactly once),
// its SHA-256 hash (stored), and a non-secret hint for the list UI.
func Generate() (raw string, hash []byte, hint string, err error) {
	var buf [32]byte
	if _, err = rand.Read(buf[:]); err != nil {
		return "", nil, "", err
	}
	raw = Prefix + base64.RawURLEncoding.EncodeToString(buf[:])
	return raw, HashToken(raw), Hint(raw), nil
}

// HashToken returns the SHA-256 hash of a raw token for storage and lookup.
func HashToken(raw string) []byte {
	h := sha256.Sum256([]byte(raw))
	return h[:]
}

// Hint returns a non-secret display string: the prefix + an ellipsis + the last
// 4 chars of the token. Too little entropy to aid a brute force.
func Hint(raw string) string {
	last := raw
	if len(raw) > 4 {
		last = raw[len(raw)-4:]
	}
	return Prefix + "…" + last
}
```

- [ ] **Step 4: Run the test (passes).**

Run: `go test ./pkg/credential/pat/ -v`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add pkg/credential/pat
git commit -m "feat(pat): token generate/hash/hint crypto package"
```

---

### Task 2: Gateway PAT verify branch

**Goal:** Accept `Authorization: Bearer <PAT>` at the forward-auth verify endpoint with terminal precedence; emit the always-on authoritative header set including `Remote-Scopes`.

**Files:**
- Modify: `pkg/protocol/oidc/forward_auth.go`
- Test: `pkg/protocol/oidc/forward_auth_test.go`

**Acceptance Criteria:**
- [ ] `writeIdentityHeaders` always emits all five `Remote-*` headers (empty when absent); takes a `scopes []string`.
- [ ] A valid PAT → `200` with `Remote-User/Name/Email/Groups/Scopes`; `Remote-Scopes` = the PAT's `upstream_scopes`.
- [ ] Invalid/expired/revoked PAT or disabled owner → `401`; valid owner not authorized for the app (PAT allow-list or RBAC) → `403`.
- [ ] A present `Authorization: Bearer` is terminal — never falls through to the cookie path or `302`.

**Verify:** `go test ./pkg/protocol/oidc/ -run ForwardAuth -v` → PASS.

**Steps:**

- [ ] **Step 1: Update the header test (it will fail until Step 3).** In `pkg/protocol/oidc/forward_auth_test.go`, replace `TestForwardAuth_IdentityHeaders` with the always-emit + scopes version:

```go
func TestForwardAuth_IdentityHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	writeIdentityHeaders(rec, "alice", "Alice A", "alice@example.com", []string{"admins", "staff"}, []string{"repo:read"})
	h := rec.Header()
	if h.Get("Remote-User") != "alice" || h.Get("Remote-Name") != "Alice A" ||
		h.Get("Remote-Email") != "alice@example.com" || h.Get("Remote-Groups") != "admins,staff" ||
		h.Get("Remote-Scopes") != "repo:read" {
		t.Fatalf("headers: %v", h)
	}
	// All five are emitted unconditionally (even empty) so Traefik overwrites
	// any client-supplied copy.
	rec2 := httptest.NewRecorder()
	writeIdentityHeaders(rec2, "bob", "Bob", "", nil, nil)
	for _, k := range []string{"Remote-Email", "Remote-Groups", "Remote-Scopes"} {
		if _, ok := rec2.Header()[k]; !ok {
			t.Errorf("%s must be emitted even when empty", k)
		}
	}
}
```

- [ ] **Step 2: Add PAT fields + methods to the fake querier.** In `pkg/protocol/oidc/forward_auth_test.go`, add to the `fakeFAQueries` struct (after the `groups []string` field):

```go
	// PAT lookup results for the Bearer path.
	pat    db.PersonalAccessToken
	patErr error
```

and add these methods near the other `fakeFAQueries` methods:

```go
func (f *fakeFAQueries) GetPATByTokenHash(_ context.Context, _ []byte) (db.PersonalAccessToken, error) {
	if f.patErr != nil {
		return db.PersonalAccessToken{}, f.patErr
	}
	return f.pat, nil
}

func (f *fakeFAQueries) TouchPATLastUsed(_ context.Context, _ int32) error { return nil }
```

- [ ] **Step 3: Implement the header change + PAT branch.** In `pkg/protocol/oidc/forward_auth.go`:

(a) add the import `"prohibitorum/pkg/credential/pat"` to the import block.

(b) replace `writeIdentityHeaders` with:

```go
// writeIdentityHeaders sets the Traefik/nginx ForwardAuth identity headers on w.
// ALL headers are emitted unconditionally — even when empty — so a downstream
// Traefik authResponseHeaders config overwrites (or clears) any client-supplied
// copy, preventing identity/scope spoofing. scopes carries a PAT's opaque
// upstream_scopes (nil for cookie/browser sessions).
func writeIdentityHeaders(w http.ResponseWriter, user, name, email string, groups, scopes []string) {
	w.Header().Set("Remote-User", user)
	w.Header().Set("Remote-Name", name)
	w.Header().Set("Remote-Email", email)
	w.Header().Set("Remote-Groups", strings.Join(groups, ","))
	w.Header().Set("Remote-Scopes", strings.Join(scopes, ","))
}
```

(c) in `HandleForwardAuthVerify`, update the cookie-path call site to pass `nil` scopes:

```go
					writeIdentityHeaders(w, acct.Username, acct.DisplayName, accountEmail(acct), groups, nil)
```

(d) in `HandleForwardAuthVerify`, insert the Bearer branch immediately after `secure := proto == "https"` and BEFORE the `if c, cerr := r.Cookie(...)` block:

```go
	// Programmatic path takes precedence and is terminal: a present
	// Authorization: Bearer is always handled as a PAT (API mode) and never
	// falls through to the cookie path or the 302 login redirect.
	if raw := bearerToken(r); raw != "" {
		p.verifyForwardAuthPAT(w, r, raw, client)
		return
	}
```

(e) append these helpers to `forward_auth.go`:

```go
// verifyForwardAuthPAT authenticates a forward-auth request by Personal Access
// Token. It is terminal: it always writes a response. 401 = the credential
// cannot resolve a valid, enabled owner (not found / expired / revoked /
// disabled). 403 = a valid owner that is not authorized for this app (PAT
// allow-list or RBAC). On success it emits the authoritative identity headers
// and best-effort updates last_used_at (throttled in SQL).
func (p *Provider) verifyForwardAuthPAT(w http.ResponseWriter, r *http.Request, raw string, client db.GetForwardAuthClientByHostRow) {
	ctx := r.Context()
	row, err := p.queries.GetPATByTokenHash(ctx, pat.HashToken(raw))
	if err != nil {
		writeBearerError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	acct, err := p.queries.GetAccountByID(ctx, row.AccountID)
	if err != nil || acct.Disabled {
		writeBearerError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	if !patAllowsClient(row.AllowedClientIds, client.ClientID) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	ok, aerr := p.queries.IsAccountAuthorizedForOIDCClient(ctx, db.IsAccountAuthorizedForOIDCClientParams{
		AccountID: pgtype.Int4{Int32: acct.ID, Valid: true}, ClientID: client.ClientID,
	})
	if aerr != nil || !ok.Bool {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	groups, _ := p.queries.ListExposedGroupSlugsByAccount(ctx, acct.ID)
	writeIdentityHeaders(w, acct.Username, acct.DisplayName, accountEmail(acct), groups, row.UpstreamScopes)
	_ = p.queries.TouchPATLastUsed(ctx, row.ID) // best-effort; throttled in SQL
	w.WriteHeader(http.StatusOK)
}

// patAllowsClient reports whether a PAT (with an optional allow-list) may be
// used for clientID. An empty allow-list means "any app the owner may reach".
func patAllowsClient(allowed []string, clientID string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, c := range allowed {
		if c == clientID {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Add PAT verify tests.** Append to `pkg/protocol/oidc/forward_auth_test.go`:

```go
// faBearerRequest builds a ForwardAuth request carrying an Authorization: Bearer
// PAT and (optionally) a cookie, to assert Bearer precedence.
func faBearerRequest(host, raw string, cookie *http.Cookie) *http.Request {
	req := faRequest("https", host, "/api", cookie)
	req.Header.Set("Authorization", "Bearer "+raw)
	return req
}

func TestForwardAuthVerify_PAT_Valid_200WithScopes(t *testing.T) {
	q := &fakeFAQueries{
		faClient:   db.GetForwardAuthClientByHostRow{ClientID: "svc", Disabled: false},
		authorized: true,
		acct:       db.Account{ID: 42, Username: "alice", DisplayName: "Alice", Email: pgtype.Text{String: "a@x", Valid: true}},
		groups:     []string{"staff"},
		pat:        db.PersonalAccessToken{ID: 7, AccountID: 42, UpstreamScopes: []string{"repo:read"}},
	}
	p, _ := newFAProvider(q)
	rec := httptest.NewRecorder()
	p.HandleForwardAuthVerify(rec, faBearerRequest("app.acme.io", "prohibitorum_pat_x", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if rec.Header().Get("Remote-User") != "alice" || rec.Header().Get("Remote-Scopes") != "repo:read" {
		t.Fatalf("headers: %v", rec.Header())
	}
}

func TestForwardAuthVerify_PAT_Invalid_401(t *testing.T) {
	q := &fakeFAQueries{
		faClient: db.GetForwardAuthClientByHostRow{ClientID: "svc"},
		patErr:   pgx.ErrNoRows,
	}
	p, _ := newFAProvider(q)
	rec := httptest.NewRecorder()
	p.HandleForwardAuthVerify(rec, faBearerRequest("app.acme.io", "prohibitorum_pat_bad", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestForwardAuthVerify_PAT_DisabledOwner_401(t *testing.T) {
	q := &fakeFAQueries{
		faClient: db.GetForwardAuthClientByHostRow{ClientID: "svc"},
		acct:     db.Account{ID: 42, Disabled: true},
		pat:      db.PersonalAccessToken{ID: 7, AccountID: 42},
	}
	p, _ := newFAProvider(q)
	rec := httptest.NewRecorder()
	p.HandleForwardAuthVerify(rec, faBearerRequest("app.acme.io", "prohibitorum_pat_x", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestForwardAuthVerify_PAT_AppRestrictionExcludes_403(t *testing.T) {
	q := &fakeFAQueries{
		faClient:   db.GetForwardAuthClientByHostRow{ClientID: "svc"},
		authorized: true,
		acct:       db.Account{ID: 42, Username: "alice"},
		pat:        db.PersonalAccessToken{ID: 7, AccountID: 42, AllowedClientIds: []string{"other"}},
	}
	p, _ := newFAProvider(q)
	rec := httptest.NewRecorder()
	p.HandleForwardAuthVerify(rec, faBearerRequest("app.acme.io", "prohibitorum_pat_x", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rec.Code)
	}
}

func TestForwardAuthVerify_PAT_RBACDenies_403(t *testing.T) {
	q := &fakeFAQueries{
		faClient:   db.GetForwardAuthClientByHostRow{ClientID: "svc"},
		authorized: false,
		acct:       db.Account{ID: 42, Username: "alice"},
		pat:        db.PersonalAccessToken{ID: 7, AccountID: 42},
	}
	p, _ := newFAProvider(q)
	rec := httptest.NewRecorder()
	p.HandleForwardAuthVerify(rec, faBearerRequest("app.acme.io", "prohibitorum_pat_x", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rec.Code)
	}
}

func TestForwardAuthVerify_PAT_PrecedesCookie(t *testing.T) {
	// A bad PAT plus a (would-be valid) cookie must still 401 — Bearer is terminal.
	q := &fakeFAQueries{
		faClient: db.GetForwardAuthClientByHostRow{ClientID: "svc"},
		patErr:   pgx.ErrNoRows,
	}
	p, store := newFAProvider(q)
	token, _ := mintFASession(context.Background(), store, faSession{AccountID: 42, ClientID: "svc"}, time.Hour)
	rec := httptest.NewRecorder()
	p.HandleForwardAuthVerify(rec, faBearerRequest("app.acme.io", "prohibitorum_pat_bad", faCookie(true, token)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 (Bearer terminal), got %d", rec.Code)
	}
}
```

- [ ] **Step 5: Run tests.**

Run: `go test ./pkg/protocol/oidc/ -run ForwardAuth -v`
Expected: PASS (including the updated `TestForwardAuth_IdentityHeaders`).

- [ ] **Step 6: Commit.**

```bash
git add pkg/protocol/oidc/forward_auth.go pkg/protocol/oidc/forward_auth_test.go
git commit -m "feat(pat): forward-auth Bearer (PAT) verify branch + Remote-Scopes"
```

---

### Task 3: Self-service `/me/tokens` API

**Goal:** Let a user create (sudo-gated, plaintext shown once), list (no secret), and revoke their own PATs; audit create/revoke.

**Files:**
- Modify: `pkg/audit/event.go` (add `FactorPAT`)
- Modify: `pkg/contract/auth.go` (views + operations)
- Create: `pkg/server/handle_me_tokens.go`
- Modify: `pkg/server/server.go` (route registrations)
- Test: `pkg/server/handle_me_tokens_test.go`

**Acceptance Criteria:**
- [ ] `POST /me/tokens` requires fresh sudo and returns the plaintext token once + the view.
- [ ] `GET /me/tokens` returns the caller's non-revoked PATs without any secret.
- [ ] `POST /me/tokens/revoke` revokes one of the caller's PATs by id (404-style error if not theirs); create + revoke write a `personal_access_token` audit row.

**Verify:** `go test ./pkg/server/ -run Token -v` → PASS.

**Steps:**

- [ ] **Step 1: Add the audit factor.** In `pkg/audit/event.go`, add to the `Factor` const block (after `FactorGroup`):

```go
	// FactorPAT covers personal-access-token create/revoke.
	FactorPAT Factor = "personal_access_token"
```

- [ ] **Step 2: Add contract views + operations.** In `pkg/contract/auth.go`, add the view structs (near `SessionListItem`):

```go
// PersonalAccessTokenView is a row in /me/tokens. The plaintext token is never
// returned here — only once, in PersonalAccessTokenCreated. TokenHint is a
// non-secret display aid (prefix + last 4 chars).
type PersonalAccessTokenView struct {
	ID               int32      `json:"id"`
	Name             string     `json:"name"`
	TokenHint        string     `json:"tokenHint"`
	UpstreamScopes   []string   `json:"upstreamScopes"`
	AllowedClientIDs []string   `json:"allowedClientIds"`
	CreatedAt        time.Time  `json:"createdAt"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt       *time.Time `json:"lastUsedAt,omitempty"`
}

// PersonalAccessTokenCreated is the create response: the plaintext is revealed
// exactly once and never retrievable again.
type PersonalAccessTokenCreated struct {
	Token string                  `json:"token"`
	PAT   PersonalAccessTokenView `json:"pat"`
}
```

and the operations (near `OperationListMySessions`):

```go
var OperationListMyTokens = huma.Operation{
	OperationID: "listMyTokens",
	Method:      http.MethodGet,
	Path:        "/me/tokens",
	Summary:     "List the caller's personal access tokens (never returns the secret).",
}

var OperationCreateMyToken = huma.Operation{
	OperationID: "createMyToken",
	Method:      http.MethodPost,
	Path:        "/me/tokens",
	Summary:     "Create a personal access token; the plaintext is revealed once.",
}

var OperationRevokeMyToken = huma.Operation{
	OperationID: "revokeMyToken",
	Method:      http.MethodPost,
	Path:        "/me/tokens/revoke",
	Summary:     "Revoke one of the caller's personal access tokens by id.",
}
```

- [ ] **Step 3: Write the handlers.** Create `pkg/server/handle_me_tokens.go`:

```go
package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/credential/pat"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
)

// patView projects a db.PersonalAccessToken into the public-safe shape.
func patView(row db.PersonalAccessToken) contract.PersonalAccessTokenView {
	v := contract.PersonalAccessTokenView{
		ID:               row.ID,
		Name:             row.Name,
		TokenHint:        row.TokenHint,
		UpstreamScopes:   append([]string(nil), row.UpstreamScopes...),
		AllowedClientIDs: append([]string(nil), row.AllowedClientIds...),
		CreatedAt:        row.CreatedAt.Time,
	}
	if row.ExpiresAt.Valid {
		t := row.ExpiresAt.Time
		v.ExpiresAt = &t
	}
	if row.LastUsedAt.Valid {
		t := row.LastUsedAt.Time
		v.LastUsedAt = &t
	}
	return v
}

// nonNilStrings returns a non-nil slice so sqlc binds a text[] NOT NULL column.
func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ----- GET /me/tokens -----------------------------------------------------

type listMyTokensOut struct {
	Body []contract.PersonalAccessTokenView
}

func (s *Server) handleListMyTokens(ctx context.Context, _ *struct{}) (*listMyTokensOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	rows, err := s.queries.ListPATsByAccount(ctx, sess.Account.ID)
	if err != nil {
		return nil, fmt.Errorf("handleListMyTokens: %w", err)
	}
	out := make([]contract.PersonalAccessTokenView, 0, len(rows))
	for _, row := range rows {
		out = append(out, patView(row))
	}
	return &listMyTokensOut{Body: out}, nil
}

// ----- POST /me/tokens (sudo) --------------------------------------------

type createMyTokenIn struct {
	Body struct {
		Name             string   `json:"name"`
		ExpiresInDays    *int     `json:"expiresInDays,omitempty"` // nil/0 = no expiry
		UpstreamScopes   []string `json:"upstreamScopes,omitempty"`
		AllowedClientIDs []string `json:"allowedClientIds,omitempty"`
	}
}

type createMyTokenOut struct {
	Body contract.PersonalAccessTokenCreated
}

func (s *Server) handleCreateMyToken(ctx context.Context, in *createMyTokenIn) (*createMyTokenOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	name := strings.TrimSpace(in.Body.Name)
	if name == "" || len(name) > 128 {
		return nil, authErrToHuma(authn.ErrBadRequest())
	}
	raw, hash, hint, err := pat.Generate()
	if err != nil {
		return nil, fmt.Errorf("handleCreateMyToken: generate: %w", err)
	}
	var expires pgtype.Timestamptz
	if in.Body.ExpiresInDays != nil && *in.Body.ExpiresInDays > 0 {
		expires = pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, *in.Body.ExpiresInDays), Valid: true}
	}
	row, err := s.queries.InsertPAT(ctx, db.InsertPATParams{
		AccountID:        sess.Account.ID,
		Name:             name,
		TokenHash:        hash,
		TokenHint:        hint,
		UpstreamScopes:   nonNilStrings(in.Body.UpstreamScopes),
		AllowedClientIds: nonNilStrings(in.Body.AllowedClientIDs),
		ExpiresAt:        expires,
	})
	if err != nil {
		return nil, fmt.Errorf("handleCreateMyToken: insert: %w", err)
	}
	credRef := int64(row.ID)
	_ = audit.NewWriter(s.queries).Record(ctx, audit.Record{
		AccountID:     &sess.Account.ID,
		Factor:        audit.FactorPAT,
		Event:         audit.EventRegister,
		CredentialRef: &credRef,
		Detail:        map[string]any{"name": name},
	})
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event": "auth.pat_created", "account_id": sess.Account.ID, "pat_id": row.ID,
	}).Info("auth")
	return &createMyTokenOut{Body: contract.PersonalAccessTokenCreated{Token: raw, PAT: patView(row)}}, nil
}

// ----- POST /me/tokens/revoke --------------------------------------------

type revokeMyTokenIn struct {
	Body struct {
		ID int32 `json:"id"`
	}
}

func (s *Server) handleRevokeMyToken(ctx context.Context, in *revokeMyTokenIn) (*emptyOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	n, err := s.queries.RevokePAT(ctx, db.RevokePATParams{ID: in.Body.ID, AccountID: sess.Account.ID})
	if err != nil {
		return nil, fmt.Errorf("handleRevokeMyToken: %w", err)
	}
	if n == 0 {
		return nil, authErrToHuma(authn.ErrCredentialNotFound())
	}
	credRef := int64(in.Body.ID)
	_ = audit.NewWriter(s.queries).Record(ctx, audit.Record{
		AccountID:     &sess.Account.ID,
		Factor:        audit.FactorPAT,
		Event:         audit.EventRevoke,
		CredentialRef: &credRef,
	})
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event": "auth.pat_revoked", "account_id": sess.Account.ID, "pat_id": in.Body.ID,
	}).Info("auth")
	return &emptyOut{}, nil
}
```

- [ ] **Step 4: Register the routes.** In `pkg/server/server.go`, immediately after the `OperationRevokeMySession` registration (line ~391), add:

```go
	registerOp(mgmt, contract.OperationListMyTokens, s.handleListMyTokens, sessionReq)
	registerSudoOp(s, mgmt, contract.OperationCreateMyToken, s.handleCreateMyToken, sessionReq)
	registerOp(mgmt, contract.OperationRevokeMyToken, s.handleRevokeMyToken, sessionReq)
```

- [ ] **Step 5: Write the handler tests.** Create `pkg/server/handle_me_tokens_test.go`, mirroring the DB-backed harness used by the other `pkg/server` `/me` tests (e.g. the setup in `handle_me_test.go` — reuse its server/session constructor and the `prohibitorum_test` DB helper). The test bodies:

```go
package server

import (
	"context"
	"testing"

	"prohibitorum/pkg/credential/pat"
)

// NOTE: reuse the existing pkg/server test harness for a logged-in session +
// real queries (see handle_me_test.go). Replace newTestSession()/testAccountID()
// below with that harness's equivalents if named differently.

func TestCreateAndListMyTokens(t *testing.T) {
	ctx, srv, sess := newAuthedTestServer(t) // harness: server + authed session in ctx
	srv.markFreshSudo(t, sess)               // harness: satisfy the sudo gate (mirror passkey-add tests)

	out, err := srv.handleCreateMyToken(ctx, &createMyTokenIn{Body: struct {
		Name             string   `json:"name"`
		ExpiresInDays    *int     `json:"expiresInDays,omitempty"`
		UpstreamScopes   []string `json:"upstreamScopes,omitempty"`
		AllowedClientIDs []string `json:"allowedClientIds,omitempty"`
	}{Name: "ci-deploy"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if out.Body.Token == "" || out.Body.PAT.TokenHint == "" {
		t.Fatal("create must reveal plaintext once + a hint")
	}
	if got := pat.HashToken(out.Body.Token); len(got) != 32 {
		t.Fatalf("token not hashable: %v", got)
	}

	list, err := srv.handleListMyTokens(ctx, &struct{}{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list.Body) != 1 || list.Body[0].Name != "ci-deploy" {
		t.Fatalf("list = %+v", list.Body)
	}
	// The secret is never echoed by the list.
	if list.Body[0].TokenHint == out.Body.Token {
		t.Fatal("list must not contain the plaintext token")
	}
}

func TestRevokeMyToken(t *testing.T) {
	ctx, srv, sess := newAuthedTestServer(t)
	srv.markFreshSudo(t, sess)
	out, _ := srv.handleCreateMyToken(ctx, &createMyTokenIn{Body: struct {
		Name             string   `json:"name"`
		ExpiresInDays    *int     `json:"expiresInDays,omitempty"`
		UpstreamScopes   []string `json:"upstreamScopes,omitempty"`
		AllowedClientIDs []string `json:"allowedClientIds,omitempty"`
	}{Name: "tmp"}})
	if _, err := srv.handleRevokeMyToken(ctx, &revokeMyTokenIn{Body: struct {
		ID int32 `json:"id"`
	}{ID: out.Body.PAT.ID}}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	list, _ := srv.handleListMyTokens(ctx, &struct{}{})
	if len(list.Body) != 0 {
		t.Fatalf("revoked token must not be listed: %+v", list.Body)
	}
	// Revoking again (or a foreign id) is a not-found error.
	if _, err := srv.handleRevokeMyToken(ctx, &revokeMyTokenIn{Body: struct {
		ID int32 `json:"id"`
	}{ID: out.Body.PAT.ID}}); err == nil {
		t.Fatal("double-revoke must error")
	}
}
```

If the harness helper names differ, adapt `newAuthedTestServer`/`markFreshSudo` to the real ones used by sibling `/me` tests; do not invent new infrastructure.

- [ ] **Step 6: Run tests + build.**

Run: `go test ./pkg/server/ -run Token -v && go build -tags nodynamic ./...`
Expected: PASS; build clean.

- [ ] **Step 7: Commit.**

```bash
git add pkg/audit/event.go pkg/contract/auth.go pkg/server/handle_me_tokens.go pkg/server/handle_me_tokens_test.go pkg/server/server.go
git commit -m "feat(pat): self-service /me/tokens create/list/revoke + audit"
```

---

### Task 4: Dashboard — Personal Access Tokens page

**Goal:** A `/tokens` page that lists/revokes the caller's PATs and creates one via a dialog that reveals the plaintext exactly once; wired into the router, sidebar, and both locales.

**Files:**
- Create: `dashboard/src/pages/TokensView.vue`
- Modify: `dashboard/src/router/index.ts`, `dashboard/src/components/custom/AppSidebar.vue`,
  `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts`

**Acceptance Criteria:**
- [ ] `/tokens` lists PATs (name, hint, created, expires, last used) and revokes via `ConfirmDialog`.
- [ ] A create dialog posts to `/me/tokens` and shows the returned plaintext with copy + an "I've saved it" gate before dismissal; the secret is cleared on close.
- [ ] Sidebar shows a "Tokens" entry under Account; en + zh locale parity holds (the i18n parity test passes).

**Verify:** `cd dashboard && npm run build` → succeeds (vue-tsc clean); `npm test` → PASS.

**Steps:**

- [ ] **Step 1: Add i18n keys (both locales).** In `dashboard/src/locales/en.ts`, add `tokens` to the `nav` block:

```ts
    tokens: 'Tokens',
```

and add a top-level `tokens` block (mirroring the `sessions` block) plus a `title.tokens` key:

```ts
  tokens: {
    title: 'Personal access tokens',
    intro: 'Tokens let your scripts and tools reach gateway-protected apps as you, with reduced access.',
    create: 'New token',
    createTitle: 'New personal access token',
    nameLabel: 'Name',
    namePlaceholder: 'e.g. ci-deploy',
    expiryLabel: 'Expires',
    expiryNever: 'No expiry',
    expiry30: '30 days',
    expiry90: '90 days',
    scopesLabel: 'Upstream scopes (optional)',
    appsLabel: 'Restrict to apps (optional)',
    hint: 'Token',
    created: 'Created',
    expires: 'Expires',
    lastUsed: 'Last used',
    neverUsed: 'Never used',
    revoke: 'Revoke',
    revokeConfirmTitle: 'Revoke this token?',
    revokeConfirmBody: 'Any script using it will immediately lose access.',
    empty: 'No personal access tokens yet.',
    revealTitle: 'Copy your new token',
    revealIntro: 'This is the only time you will see it. Store it somewhere safe.',
    copy: 'Copy',
    savedConfirm: "I've saved this token",
    done: 'Done',
  },
```

Add the title key inside the existing `title` block:

```ts
    tokens: 'Personal access tokens',
```

Mirror **every** key into `dashboard/src/locales/zh.ts` with Chinese strings (the `en.compile.test` / parity test fails otherwise). Suggested zh values: `title: '个人访问令牌'`, `intro: '令牌让你的脚本和工具以你的身份、以更小的权限访问受网关保护的应用。'`, `create: '新建令牌'`, `revoke: '吊销'`, `empty: '还没有个人访问令牌。'`, `savedConfirm: '我已保存此令牌'`, `done: '完成'` (translate the rest in the same register as the surrounding file).

- [ ] **Step 2: Add the route.** In `dashboard/src/router/index.ts`, inside the `/account` children array (after the `sessions` route, line ~103), add:

```ts
      { path: '/tokens', name: 'tokens', component: () => import('../pages/TokensView.vue'), meta: { titleKey: 'title.tokens' } },
```

- [ ] **Step 3: Add the sidebar entry.** In `dashboard/src/components/custom/AppSidebar.vue`, import an icon (add `KeyRound` is already imported; use `Terminal`) — extend the lucide import on line 12 with `Terminal`, then add to `accountItems` (after the `sessions` item):

```ts
  { to: '/tokens', label: t('nav.tokens'), icon: Terminal },
```

- [ ] **Step 4: Create the page.** Create `dashboard/src/pages/TokensView.vue`:

```vue
<script setup lang="ts">
/**
 * TokensView (/tokens) — list personal access tokens; create one (reveal-once)
 * and revoke. GET /me/tokens; POST /me/tokens (sudo); POST /me/tokens/revoke.
 */
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { relativeTime, formatDateTime } from '@/lib/time'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import { Terminal, Copy, Check } from 'lucide-vue-next'

interface TokenView {
  id: number
  name: string
  tokenHint: string
  upstreamScopes: string[]
  allowedClientIds: string[]
  createdAt: string
  expiresAt?: string
  lastUsedAt?: string
}

const { t } = useI18n()
const { busy, run, errorText } = useApi()

const rows = ref<TokenView[]>([])
const confirmRevokeId = ref<number | null>(null)

// Create dialog state.
const createOpen = ref(false)
const newName = ref('')
const newExpiry = ref<number>(90) // days; 0 = never
const created = ref<string | null>(null) // plaintext, shown once
const savedAck = ref(false)

async function load(): Promise<void> {
  const res = await run(() => api.get<TokenView[]>('/api/prohibitorum/me/tokens'))
  if (res) rows.value = res
}
async function create(): Promise<void> {
  const body = { name: newName.value, expiresInDays: newExpiry.value || undefined }
  const res = await run(() => api.post<{ token: string; pat: TokenView }>('/api/prohibitorum/me/tokens', body))
  if (res) {
    created.value = res.token
    savedAck.value = false
    newName.value = ''
    await load()
  }
}
async function revoke(): Promise<void> {
  const id = confirmRevokeId.value
  if (id == null) return
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/tokens/revoke', { id })
    return true as const
  })
  confirmRevokeId.value = null
  if (ok) await load()
}
const copied = ref(false)
async function copyToken(): Promise<void> {
  if (!created.value) return
  try {
    await navigator.clipboard.writeText(created.value)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch { /* clipboard denied — user can select manually */ }
}
function closeCreate(): void {
  createOpen.value = false
  created.value = null
  savedAck.value = false
}
onMounted(load)
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('tokens.title') }}</h1>
      <Button size="sm" data-test="new-token" @click="createOpen = true">{{ t('tokens.create') }}</Button>
    </div>
    <p class="text-sm text-muted">{{ t('tokens.intro') }}</p>
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
      <AlertDescription>{{ errorText }}</AlertDescription>
    </Alert>

    <TableSkeleton v-if="busy && !rows.length" :rows="3" :cols="1" />
    <template v-else-if="rows.length">
      <Card v-for="r in rows" :key="r.id">
        <CardContent class="flex items-center justify-between gap-4 py-4">
          <div class="flex min-w-0 flex-1 flex-col gap-1 text-sm">
            <span class="truncate font-medium text-ink">{{ r.name }}</span>
            <span class="truncate text-muted">{{ t('tokens.hint') }}: <span class="font-mono">{{ r.tokenHint }}</span></span>
            <span class="truncate text-muted">{{ t('tokens.created') }}: {{ relativeTime(r.createdAt) }}</span>
            <span v-if="r.expiresAt" class="truncate text-muted">{{ t('tokens.expires') }}: {{ formatDateTime(r.expiresAt) }}</span>
            <span class="truncate text-muted">{{ t('tokens.lastUsed') }}: {{ r.lastUsedAt ? relativeTime(r.lastUsedAt) : t('tokens.neverUsed') }}</span>
          </div>
          <Button variant="outline" size="sm" class="shrink-0" :disabled="busy"
                  data-test="revoke" @click="confirmRevokeId = r.id">
            {{ t('tokens.revoke') }}
          </Button>
        </CardContent>
      </Card>
    </template>
    <EmptyState v-else-if="!errorText" :icon="Terminal" :title="t('tokens.empty')" />

    <ConfirmDialog
      :open="confirmRevokeId !== null"
      :title="t('tokens.revokeConfirmTitle')"
      :confirm-label="t('tokens.revoke')"
      :busy="busy"
      @update:open="(v) => { if (!v) confirmRevokeId = null }"
      @cancel="confirmRevokeId = null"
      @confirm="revoke"
    >
      {{ t('tokens.revokeConfirmBody') }}
    </ConfirmDialog>

    <!-- Create / reveal-once dialog -->
    <Dialog :open="createOpen" @update:open="(v) => { if (!v) closeCreate() }">
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{{ created ? t('tokens.revealTitle') : t('tokens.createTitle') }}</DialogTitle>
        </DialogHeader>

        <div v-if="!created" class="flex flex-col gap-4">
          <label class="flex flex-col gap-1 text-sm text-ink">
            <span>{{ t('tokens.nameLabel') }}</span>
            <Input v-model="newName" :placeholder="t('tokens.namePlaceholder')" data-test="token-name" />
          </label>
          <label class="flex flex-col gap-1 text-sm text-ink">
            <span>{{ t('tokens.expiryLabel') }}</span>
            <select v-model.number="newExpiry" class="rounded-md border border-border bg-transparent px-3 py-2 text-sm">
              <option :value="30">{{ t('tokens.expiry30') }}</option>
              <option :value="90">{{ t('tokens.expiry90') }}</option>
              <option :value="0">{{ t('tokens.expiryNever') }}</option>
            </select>
          </label>
          <Button :disabled="busy || !newName.trim()" data-test="token-create-submit" @click="create">
            {{ t('tokens.create') }}
          </Button>
        </div>

        <div v-else class="flex flex-col gap-4">
          <p class="text-sm text-muted">{{ t('tokens.revealIntro') }}</p>
          <div class="flex items-center gap-2 rounded-md border border-border bg-sunken p-3">
            <code class="min-w-0 flex-1 truncate font-mono text-sm text-ink">{{ created }}</code>
            <Button type="button" variant="outline" size="sm" @click="copyToken">
              <component :is="copied ? Check : Copy" class="size-4" aria-hidden="true" />
              <span>{{ copied ? t('common.copied') : t('tokens.copy') }}</span>
            </Button>
          </div>
          <label class="flex items-center gap-2 text-sm text-ink">
            <Checkbox v-model="savedAck" data-test="token-saved" />
            <span>{{ t('tokens.savedConfirm') }}</span>
          </label>
          <Button class="w-full" :disabled="!savedAck" data-test="token-done" @click="closeCreate">
            {{ t('tokens.done') }}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  </div>
</template>
```

If the vendored `Dialog`/`Input`/`Select` import paths differ, mirror an existing dialog-using page (e.g. an admin detail view) for the exact component paths — do not invent components.

- [ ] **Step 5: Build + test.**

Run: `cd dashboard && npm run build && npm test`
Expected: build succeeds (vue-tsc clean), tests PASS (incl. en/zh parity).

- [ ] **Step 6: Commit.**

```bash
cd .. && git add dashboard/src
git commit -m "feat(pat): personal access tokens dashboard page"
```

---

### Task 5: Docs — forward-auth deployment + API

**Goal:** Document the PAT credential, the `Remote-Scopes` header, and the mandatory Traefik config (forward `Remote-*`; strip `Authorization`).

**Files:**
- Modify: `docs/forward-auth.md`
- Modify: `api.md`

**Acceptance Criteria:**
- [ ] `docs/forward-auth.md` has a "Personal Access Tokens" section with the verbatim verifier-boundary note and the two Traefik requirements.
- [ ] `api.md` documents `GET/POST /me/tokens`, `POST /me/tokens/revoke`, and the verify endpoint's `Authorization: Bearer` behavior (200/401/403).

**Verify:** `grep -n "Remote-Scopes" docs/forward-auth.md api.md` → both files match; manual read.

**Steps:**

- [ ] **Step 1: Update `docs/forward-auth.md`.** Add a "Personal Access Tokens" section containing:
  - The authoritative header set now includes `Remote-Scopes`; **all five are always emitted** (even empty) and must be listed in the forward-auth middleware's `authResponseHeaders` (or `authResponseHeadersRegex` matching `Remote-*`).
  - The verbatim boundary note: "Because Prohibitorum runs as a forward-auth verifier and is not in the request data path, it cannot unilaterally remove the client's raw PAT from the upstream request. Deployments must configure Traefik to forward the authoritative `Remote-*` headers from Prohibitorum and strip the original `Authorization` header before the request reaches upstream."
  - A Traefik `Headers` middleware example that removes `Authorization` (`customRequestHeaders: { Authorization: "" }`) chained after the forwardAuth middleware on PAT-protected routers.
  - Behavior: `Authorization: Bearer <PAT>` → terminal API mode (200 on success, 401 invalid token, 403 not authorized); no Bearer → existing cookie/redirect browser flow.

- [ ] **Step 2: Update `api.md`.** Add the three `/me/tokens` routes (list 🔓 session, create 🔐 sudo reveal-once, revoke 🔓 session) and a note on the verify endpoint's Bearer behavior, mirroring the existing route-table style.

- [ ] **Step 3: Commit.**

```bash
git add docs/forward-auth.md api.md
git commit -m "docs(pat): forward-auth PAT deployment + /me/tokens API"
```

---

### Task 6: Smoke coverage + full gate + dist commit

**Goal:** Prove the PAT path end-to-end against a live server, then run the full project gate and commit the rebuilt SPA bundle.

**USER-ORDERED GATE — NON-SKIPPABLE.** This task was requested by the user in the current conversation (project convention: every cycle ends with `SMOKE_EXIT=0` + a committed `dist`). It MUST NOT be closed by walking around it, by declaring it "verified inline", or by substituting a cheaper check. Close only after every item in the acceptance criteria has been re-validated independently, with output captured.

**Files:**
- Modify: `cmd/smoke/main.go`
- Modify (generated): `pkg/webui/dist` (rebuilt bundle)

**Acceptance Criteria:**
- [ ] Smoke registers a forward-auth app, creates a PAT (sudo), and asserts: `verify` with `Authorization: Bearer <PAT>` + `X-Forwarded-Host: <fa host>` → `200` with `Remote-User` and `Remote-Scopes`; a bad Bearer → `401`; a Bearer for a non-allowed app → `403`.
- [ ] `mise run ci` passes (go vet/build `-tags nodynamic`/test; dashboard install/test/build; no `pkg/webui/dist` drift).
- [ ] `mise run ci:smoke` exits 0.

**Verify:** `mise run ci && mise run ci:smoke` → both exit 0.

**Steps:**

- [ ] **Step 1: Add a PAT smoke block to `cmd/smoke/main.go`.** Mirror the existing smoke idioms (the `c` HTTP client, `step(...)`, and the existing sudo-before-mutation helper used by the add-passkey step). Add, after the existing self-service block:
  1. Register a forward-auth app for host `smoke-fa.example.test` — reuse the admin create path the smoke already uses for OIDC clients, or call `oidc.RegisterForwardAuthApp` against the smoke DB. The smoke admin account is the owner and `access_restricted` is false by default, so RBAC authorizes it.
  2. Re-establish fresh sudo (mirror the add-passkey sudo step), then `POST /api/prohibitorum/me/tokens` with `{"name":"smoke","expiresInDays":1}`; capture `token` from the response.
  3. Using a raw `http.Client`, `GET {base}/api/prohibitorum/forward-auth/verify` with headers `X-Forwarded-Host: smoke-fa.example.test`, `X-Forwarded-Proto: https`, `Authorization: Bearer <token>`; assert `200`, `Remote-User` non-empty, and the `Remote-Scopes` header present.
  4. Repeat with `Authorization: Bearer prohibitorum_pat_bogus`; assert `401`.
  5. Register a second forward-auth app `smoke-fa2.example.test`, create a PAT restricted to the first app (`allowedClientIds:[<first client_id>]`), call verify against `smoke-fa2.example.test` with that PAT; assert `403`.

  Use the established `step(...)`/`log.Fatalf` assertion style so a failure sets `SMOKE_EXIT != 0`.

- [ ] **Step 2: Run the smoke.**

Run: `mise run ci:smoke`
Expected: exit 0; the new PAT steps print and pass.

- [ ] **Step 3: Rebuild the SPA bundle + run the full gate.**

Run: `mise run prod:build && mise run ci`
Expected: `prod:build` refreshes `pkg/webui/dist`; `mise run ci` exits 0 (no dist drift).

- [ ] **Step 4: Commit.**

```bash
git add cmd/smoke/main.go pkg/webui/dist
git commit -m "test(pat): end-to-end smoke coverage; rebuild dist"
```

---

## Self-Review

- **Spec coverage:** principal=owner (Task 2/3); verifier model + Bearer precedence (Task 2); `Remote-Scopes` always-emit (Task 2); SHA-256 + token format + hint (Task 1, Task 0 column); `personal_access_token` table + app/audience restriction + expiry/revocation (Task 0); 401/403 split (Task 2); self-service create(sudo)/list/revoke + audit (Task 3); dashboard reveal-once + list/revoke (Task 4); Traefik deployment boundary + `Authorization` strip (Task 5); end-to-end + gate (Task 6). All spec sections map to a task.
- **Placeholder scan:** test harness helper names in Task 3 Step 5 are flagged as "adapt to the real harness" rather than invented — this is the one place the engineer must match existing infrastructure; all other code is complete.
- **Type consistency:** `db.PersonalAccessToken` fields (`AllowedClientIds`, `UpstreamScopes`, `TokenHint`) match across Tasks 0/2/3; `writeIdentityHeaders(…, groups, scopes []string)` is consistent between the change site and both call sites; `pat.HashToken`/`pat.Generate` signatures match between Tasks 1/2/3.
