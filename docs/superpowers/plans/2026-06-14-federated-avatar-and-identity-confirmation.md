# Federated Identity Confirmation + Upstream Avatar Inheritance — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On upstream-OIDC federated login, require a new (unconfirmed) identity to be confirmed by the user on a `/welcome` interstitial before any durable session is granted, and inherit the user's avatar from the upstream IdP (id_token `picture`, else UserInfo) in the background.

**Architecture:** A new `account_identity.confirmed_at` flag gates the session: the callback withholds the session for an unconfirmed identity, creates a short-lived KV+cookie confirmation grant, and redirects to `/welcome`; a confirm endpoint sets `confirmed_at` and issues the session. A detached goroutine fetches+normalizes the upstream picture (SSRF-screened) and stores it unless `account.avatar_source='user'`; a short-TTL KV key surfaces fetch progress for a frontend spinner.

**Tech Stack:** Go 1.26, Huma v2 + chi, sqlc/pgx, goose migrations, zitadel/oidc v3 RP, `gen2brain/webp` (WASM, `nodynamic`), in-process/Redis KV, Vue 3 + Vite + shadcn-vue, vitest.

**Spec:** `docs/superpowers/specs/2026-06-14-federated-avatar-and-identity-confirmation-design.md`

## Conventions (verified)

- **Migrations:** goose, in `db/migrations/`. Apply with `mise db:up` (needs `podman compose up -d`). `mise exec -- sqlc generate` regenerates `pkg/db/` from `db/queries/*.sql` + `db/migrations/`. `GetAccountByID` (`SELECT *`) and `GetAccountIdentityByIssuerSub` (`SELECT *`) auto-pick-up new columns.
- **KV:** `kv.Store` interface has `SetNX(ctx,key,val,ttl)(bool,error)`, `SetEx`, `Get→("",ErrKeyNotFound)`, `Pop`, `Del`. Federation flow state uses keys from `pkg/federation/oidc/state.go`.
- **Cookies/session:** `sessstore.FedStateCookie(cfg, r, token)` sets the anti-forgery cookie (`FedStateCookieName`), `sessstore.ClearedFedStateCookie(cfg, r)` clears it, `sessstore.FreshSessionCookie(cfg, r, accountID, token, ttl)`. `s.sessionStore.Issue(ctx, accountID, ip, ua, amr, *upstreamIDPID)(token, *SessionData, error)`. `sessstore.ClientIP(r, s.config.TrustProxy)`.
- **Routes:** `registerOpHTTP(s.router, method, path, contract.AuthRequirement{Kind: contract.AuthPublic|contract.AuthSession}, handler)`. Path params via `chi.URLParam`. Session via `authn.SessionFromContext(r.Context())`.
- **Gate:** `CGO_ENABLED=0 go build -tags nodynamic ./... && go vet ./... && go test ./...`; `cd dashboard && npm run test && npm run build`; smoke `SMOKE_EXIT=0` (needs `PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true`); rebuild + commit `pkg/webui/dist`.
- **No `Co-Authored-By` trailer on commits. Commit directly to master.**

---

### Task 1: Migration numbering fix + schema migration + sqlc queries

**Goal:** Resolve the open `002_avatar.sql` numbering risk, add the three new columns with backfills, and add the avatar-source-aware + identity-confirm queries.

**Files:**
- Rename: `db/migrations/002_avatar.sql` → `db/migrations/011_avatar.sql` (+ add `IF NOT EXISTS`)
- Create: `db/migrations/012_federation_confirm_avatar.sql`
- Modify: `db/queries/account.sql`, `db/queries/account_identity.sql`
- Regenerate: `pkg/db/*`

**Acceptance Criteria:**
- [ ] `mise db:up` applies cleanly on the dev DB; `account` has `avatar_source`, `upstream_idp` has `picture_claim`, `account_identity` has `confirmed_at`; existing avatars backfilled to `avatar_source='user'`, existing identities to `confirmed_at=linked_at`.
- [ ] `mise exec -- sqlc generate` succeeds; `go build ./...` passes with the new query methods.

**Verify:** `podman compose up -d && mise db:up && mise exec -- sqlc generate && CGO_ENABLED=0 go build -tags nodynamic ./...` → no errors.

**Steps:**

- [ ] **Step 1: Renumber the avatar migration (resolves the v10-skip risk).** `git mv db/migrations/002_avatar.sql db/migrations/011_avatar.sql`, then make it idempotent so it applies cleanly on fresh (cols already exist) AND pre-squash (v10) DBs:

```sql
-- +goose Up
ALTER TABLE account ADD COLUMN IF NOT EXISTS avatar_content_type text;
ALTER TABLE account ADD COLUMN IF NOT EXISTS avatar_etag text;

CREATE TABLE IF NOT EXISTS account_avatar (
  account_id int PRIMARY KEY REFERENCES account(id) ON DELETE CASCADE,
  bytes      bytea NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS account_avatar;
ALTER TABLE account DROP COLUMN IF EXISTS avatar_etag;
ALTER TABLE account DROP COLUMN IF EXISTS avatar_content_type;
```

- [ ] **Step 2: Create `db/migrations/012_federation_confirm_avatar.sql`**

```sql
-- +goose Up
-- Avatar provenance: NULL = untouched, 'upstream' = inherited from IdP,
-- 'user' = the account owner uploaded or deliberately removed (locks out upstream).
ALTER TABLE account ADD COLUMN IF NOT EXISTS avatar_source text;
-- Every avatar that exists today was user-uploaded (this feature did not exist),
-- so protect it from upstream clobber on the owner's next federated login.
UPDATE account SET avatar_source = 'user' WHERE avatar_etag IS NOT NULL AND avatar_source IS NULL;

-- Per-IdP claim override for the upstream avatar URL (mirrors username/display_name/email).
ALTER TABLE upstream_idp ADD COLUMN IF NOT EXISTS picture_claim text NOT NULL DEFAULT 'picture';

-- Identity confirmation gate: NULL = pending (must be confirmed on /welcome before a
-- session is issued). Existing identities count as already confirmed.
ALTER TABLE account_identity ADD COLUMN IF NOT EXISTS confirmed_at timestamptz;
UPDATE account_identity SET confirmed_at = linked_at WHERE confirmed_at IS NULL;

-- +goose Down
ALTER TABLE account_identity DROP COLUMN IF EXISTS confirmed_at;
ALTER TABLE upstream_idp DROP COLUMN IF EXISTS picture_claim;
ALTER TABLE account DROP COLUMN IF EXISTS avatar_source;
```

- [ ] **Step 3: Append avatar-source-aware queries to `db/queries/account.sql`.** The existing `SetAccountAvatarMeta` / `ClearAccountAvatarMeta` become source-aware. Replace `SetAccountAvatarMeta` with two source-specific variants and make the clear set `'user'`:

```sql
-- name: SetAccountAvatarMetaUpstream :exec
UPDATE account SET avatar_content_type = $2, avatar_etag = $3, avatar_source = 'upstream', updated_at = now() WHERE id = $1;

-- name: SetAccountAvatarMetaUser :exec
UPDATE account SET avatar_content_type = $2, avatar_etag = $3, avatar_source = 'user', updated_at = now() WHERE id = $1;

-- name: ClearAccountAvatarMeta :exec
UPDATE account SET avatar_content_type = NULL, avatar_etag = NULL, avatar_source = 'user', updated_at = now() WHERE id = $1;
```
> Delete the old `SetAccountAvatarMeta` query (the two `Set...Meta*` replace it). `UpsertAccountAvatarBytes`, `ClearAccountAvatarBytes`, `GetAvatarBySubject` are unchanged. Task 7 updates the `handle_avatar.go` call sites.

- [ ] **Step 4: Add the confirm query to `db/queries/account_identity.sql`**

```sql
-- name: ConfirmAccountIdentity :exec
UPDATE account_identity SET confirmed_at = now() WHERE id = $1 AND confirmed_at IS NULL;
```
> `InsertAccountIdentity` is unchanged — it inserts with `confirmed_at` left NULL (pending). The invite path (Task 5) calls `ConfirmAccountIdentity` in-tx.

- [ ] **Step 5: Add `picture_claim` to the upstream_idp create/update queries** (`db/queries/upstream_idp.sql`). Add `picture_claim` to `CreateUpstreamIDP` (column list + a new `$N` value at the end), and to both `UpdateUpstreamIDP` and `UpdateUpstreamIDPConfig` SET lists (append `picture_claim = $N`). Keep the existing parameter order; append the new param last in each. Example for `UpdateUpstreamIDPConfig` (append after `disabled = $12`):

```sql
    email_claim = $10, require_verified_email = $11, disabled = $12,
    picture_claim = $13
```
> Match the exact existing column/param numbering in the file; the new param is appended last in each statement. Note the resulting Go param names for Task 10 (`PictureClaim`).

- [ ] **Step 6: Apply + regenerate + build**

Run: `podman compose up -d && mise db:up && mise exec -- sqlc generate && CGO_ENABLED=0 go build -tags nodynamic ./...`
Expected: migrations `011`/`012` apply; sqlc writes `db.Account.AvatarSource pgtype.Text`, `db.UpstreamIdp.PictureClaim string`, `db.AccountIdentity.ConfirmedAt pgtype.Timestamptz`, and methods `SetAccountAvatarMetaUpstream`, `SetAccountAvatarMetaUser`, `ConfirmAccountIdentity`, plus the new `picture_claim` params. Build clean.

- [ ] **Step 7: Commit**

```bash
git add db/migrations db/queries pkg/db
git commit -m "feat(db): avatar_source + picture_claim + account_identity.confirmed_at; renumber avatar migration to 011 (IF NOT EXISTS), add 012"
```

---

### Task 2: Claim plumbing — `picture` hoist + `Client.UserInfo`

**Goal:** Make `picture` readable via `ClaimString` (id_token), and add a UserInfo fetch fallback that rides the hardened client.

**Files:**
- Modify: `pkg/federation/oidc/client.go`
- Test: `pkg/federation/oidc/client_test.go`

**Acceptance Criteria:**
- [ ] After `Exchange`, `Tokens.Raw["picture"]` carries the id_token `picture` claim when present.
- [ ] `Client.UserInfo(ctx, accessToken)` returns a `map[string]any` of userinfo claims with `picture` hoisted, usable by `ClaimString`.

**Verify:** `go test ./pkg/federation/oidc/ -run 'Picture|UserInfo' -v` → pass.

**Steps:**

- [ ] **Step 1: Write the failing test** (`client_test.go`). Add a unit test for the hoist on the `raw`-building logic. Since `Exchange` requires a live RP, test the hoist by asserting the existing `raw`-build covers `picture` — add a small exported test seam OR test via the existing `Exchange` test harness if present. Minimal direct test of the hoist rule:

```go
func TestRawHoistsPicture(t *testing.T) {
	// hoistStandardClaims mirrors the hoist block in Exchange; if the package
	// already inlines it, extract it to a helper hoistStandardClaims(raw, claims)
	// and test that. Asserts picture lands in raw.
	raw := map[string]any{}
	hoistStandardClaims(raw, hoistInput{Picture: "https://pic.example/p.png"})
	if raw["picture"] != "https://pic.example/p.png" {
		t.Fatalf("picture = %v, want hoisted", raw["picture"])
	}
}
```
> If extracting a helper is heavier than warranted, instead assert via the package's existing `Exchange`/claims test fixture that a `picture` in the id_token surfaces in `Tokens.Raw`. Pick whichever matches the file's existing test style; the behavior under test is "picture ends up in Raw".

- [ ] **Step 2: Run → fail.** `go test ./pkg/federation/oidc/ -run Picture -v` → FAIL (picture not hoisted / helper undefined).

- [ ] **Step 3: Hoist `picture` in `Exchange`** (`client.go`, in the `raw`-build block after the `name`/`email` hoists, ~line 317):

```go
	if claims.Picture != "" {
		raw["picture"] = claims.Picture
	}
```

- [ ] **Step 4: Add `Client.UserInfo`** (`client.go`). Uses the embedded RP (same hardened client):

```go
// UserInfo fetches the OIDC UserInfo endpoint with the given access token,
// through the same SSRF-hardened HTTP client as discovery/token-exchange. It
// returns a unified claims map (typed standard claims hoisted under their
// JSON-tag keys, plus any extras) so ClaimString can read picture/etc. uniformly.
// Errors are returned for the caller to treat as non-fatal.
func (c *Client) UserInfo(ctx context.Context, accessToken string) (map[string]any, error) {
	info, err := rp.Userinfo[*oidc.UserInfo](ctx, accessToken, oidc.BearerToken, "", c.rp)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: userinfo: %w", err)
	}
	raw := make(map[string]any, len(info.Claims)+4)
	for k, v := range info.Claims {
		raw[k] = v
	}
	if info.Picture != "" {
		raw["picture"] = info.Picture
	}
	if info.PreferredUsername != "" {
		raw["preferred_username"] = info.PreferredUsername
	}
	if info.Email != "" {
		raw["email"] = info.Email
	}
	return raw, nil
}
```
> Confirm the exact `rp.Userinfo` signature/type params against the installed `zitadel/oidc/v3` (v3.47.5) — `rp.Userinfo[*oidc.UserInfo](ctx, accessToken, tokenType, subject, rp)`. The `subject` arg may be passed `""` (zitadel only uses it to cross-check `sub` when non-empty). If the signature differs, adapt; the behavior is "GET userinfo via the RP's client, return a claims map".

- [ ] **Step 5: Test UserInfo.** Add a test that stands up an `httptest` userinfo server returning `{"sub":"u","picture":"https://pic/x"}` and a `Client` pointed at it — OR, if wiring a full RP in a unit test is too heavy, cover `UserInfo`'s map-building by extracting the `info → map` transform into a helper `userInfoToRaw(info *oidc.UserInfo) map[string]any` and unit-testing that with a hand-built `*oidc.UserInfo{ UserInfoProfile: oidc.UserInfoProfile{Picture: "https://pic/x"} }`. Assert `raw["picture"] == "https://pic/x"`.

- [ ] **Step 6: Run → pass + build.** `go test ./pkg/federation/oidc/ -run 'Picture|UserInfo' -v && CGO_ENABLED=0 go build -tags nodynamic ./...`

- [ ] **Step 7: Commit**

```bash
git add pkg/federation/oidc/client.go pkg/federation/oidc/client_test.go
git commit -m "feat(federation): hoist id_token picture into Raw; add Client.UserInfo fallback"
```

---

### Task 3: SSRF-guarded avatar fetch

**Goal:** A bounded, SSRF-screened fetch of the upstream picture URL, reusing the dial-screen.

**Files:**
- Create: `pkg/federation/oidc/avatar_fetch.go`, `pkg/federation/oidc/avatar_fetch_test.go`
- Modify: `pkg/federation/oidc/httpclient.go` (parameterize `maxBytes`)

**Acceptance Criteria:**
- [ ] `fetchUpstreamAvatar` rejects non-https URLs, non-`image/*` responses, and (via the cap) oversized bodies; returns bytes on a valid image.
- [ ] The dial-time IP screen is shared with the existing federation client (no duplicated SSRF logic).

**Verify:** `go test ./pkg/federation/oidc/ -run AvatarFetch -v` → pass.

**Steps:**

- [ ] **Step 1: Parameterize the body cap in `httpclient.go`.** Change `hardenedHTTPClient(allowPrivate bool)` → `hardenedHTTPClient(allowPrivate bool, maxBytes int64)`, thread `maxBytes` into `cappingTransport{base: transport, max: maxBytes}` and `cappedBody`. Update `cappingTransport`/`cappedBody` to carry `max int64` instead of the const. Update the existing caller in `client.go` (`NewClient`) to pass `maxFederationResponseBytes`:

```go
// client.go NewClient: rp.WithHTTPClient(hardenedHTTPClient(allowPrivateNetwork, maxFederationResponseBytes))
```
Add a new const in `avatar_fetch.go`: `const maxAvatarFetchBytes = 5 << 20 // 5 MiB, matches avatar.Process`.

- [ ] **Step 2: Write the failing test** (`avatar_fetch_test.go`):

```go
package oidc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAvatarFetch_RejectsNonHTTPS(t *testing.T) {
	if _, err := fetchUpstreamAvatar(context.Background(), "http://example.com/a.png", true); err == nil {
		t.Fatal("want error for non-https URL")
	}
}

func TestAvatarFetch_RejectsNonImage(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>"))
	}))
	defer srv.Close()
	// allowPrivate=true so the loopback test server is reachable; srv.Client() trusts the cert.
	if _, err := fetchUpstreamAvatarWithClient(context.Background(), srv.URL, srv.Client()); err == nil || !strings.Contains(err.Error(), "content-type") {
		t.Fatalf("want content-type rejection, got %v", err)
	}
}

func TestAvatarFetch_ReturnsImageBytes(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n"))
	}))
	defer srv.Close()
	b, err := fetchUpstreamAvatarWithClient(context.Background(), srv.URL, srv.Client())
	if err != nil || len(b) == 0 {
		t.Fatalf("want bytes, got len=%d err=%v", len(b), err)
	}
}
```
> The seam `fetchUpstreamAvatarWithClient(ctx, url, *http.Client)` lets tests inject `srv.Client()` (which trusts the httptest TLS cert); `fetchUpstreamAvatar(ctx, url, allowPrivate)` builds the hardened client and delegates. The non-https check happens in both before any client use.

- [ ] **Step 3: Run → fail.** `go test ./pkg/federation/oidc/ -run AvatarFetch -v` → FAIL (undefined).

- [ ] **Step 4: Implement `avatar_fetch.go`**

```go
package oidc

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const maxAvatarFetchBytes = 5 << 20 // 5 MiB, matches pkg/avatar input cap.

// fetchUpstreamAvatar GETs an upstream picture URL through the same SSRF-hardened
// dial-screen as the rest of federation, capped to 5 MiB. https-only; rejects
// non-image responses. Returns raw bytes for pkg/avatar.Process.
func fetchUpstreamAvatar(ctx context.Context, rawURL string, allowPrivate bool) ([]byte, error) {
	if err := validateAvatarURL(rawURL); err != nil {
		return nil, err
	}
	return fetchUpstreamAvatarWithClient(ctx, rawURL, hardenedHTTPClient(allowPrivate, maxAvatarFetchBytes))
}

func validateAvatarURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("federation/oidc: avatar url parse: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("federation/oidc: avatar url must be https, got %q", u.Scheme)
	}
	return nil
}

func fetchUpstreamAvatarWithClient(ctx context.Context, rawURL string, client *http.Client) ([]byte, error) {
	if err := validateAvatarURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: avatar request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: avatar fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("federation/oidc: avatar fetch status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		return nil, fmt.Errorf("federation/oidc: avatar content-type %q is not an image", ct)
	}
	b, err := io.ReadAll(resp.Body) // body already byte-capped by the hardened transport
	if err != nil {
		return nil, fmt.Errorf("federation/oidc: avatar read: %w", err)
	}
	return b, nil
}
```

- [ ] **Step 5: Run → pass + build + vet.** `go test ./pkg/federation/oidc/ -run AvatarFetch -v && CGO_ENABLED=0 go build -tags nodynamic ./... && go vet ./pkg/federation/...`

- [ ] **Step 6: Commit**

```bash
git add pkg/federation/oidc/avatar_fetch.go pkg/federation/oidc/avatar_fetch_test.go pkg/federation/oidc/httpclient.go pkg/federation/oidc/client.go
git commit -m "feat(federation): SSRF-guarded upstream avatar fetch (https, image/*, 5 MiB cap)"
```

---

### Task 4: Background avatar-inherit job + KV status

**Goal:** A detached goroutine that resolves the picture (id_token→UserInfo), fetches+processes+stores it (skipping when `avatar_source='user'` or etag unchanged), with a `SetNX`-deduped status key.

**Files:**
- Modify: `pkg/federation/oidc/federation.go` (job methods, `FederatorQueries` additions), `pkg/federation/oidc/state.go` (`avatarFetchKey`)
- Test: `pkg/federation/oidc/federation_test.go`

**Acceptance Criteria:**
- [ ] `runAvatarInherit` stores `(webp, etag, source='upstream')` for a fresh account; is a no-op when `avatar_source='user'`; is a no-op when the new etag equals the stored etag; clears the status key in all cases.
- [ ] Concurrent runs for the same account dedupe via `SetNX` (second exits immediately).

**Verify:** `go test ./pkg/federation/oidc/ -run AvatarInherit -v` → pass.

**Steps:**

- [ ] **Step 1: Add the status key helper to `state.go`**

```go
import "strconv" // add to the import block

// AvatarFetchKey is the KV key marking an in-flight upstream avatar fetch for an
// account. Presence = "pending"; the value is unused. Short TTL backstops a dead
// goroutine; SetNX on this key dedupes concurrent logins.
func AvatarFetchKey(accountID int32) string {
	return "oidc:fed:avatar:" + strconv.Itoa(int(accountID))
}
```

- [ ] **Step 2: Extend `FederatorQueries`** (`federation.go`) with the avatar store + read methods (add to the interface):

```go
	UpsertAccountAvatarBytes(ctx context.Context, arg db.UpsertAccountAvatarBytesParams) error
	SetAccountAvatarMetaUpstream(ctx context.Context, arg db.SetAccountAvatarMetaUpstreamParams) error
```
> `GetAccountByID` is already in the interface (used by `HandleCallback`). `db.Querier` satisfies these implicitly.

- [ ] **Step 3: Write the failing test** (`federation_test.go`) using a fake querier + an injected fetcher. Add an injectable fetcher seam on `Federator` (a field `avatarFetch func(ctx, url string, allowPrivate bool) ([]byte, error)` defaulting to `fetchUpstreamAvatar`) so the test controls the bytes without a network:

```go
func TestAvatarInherit_StoresUpstream(t *testing.T) {
	q := newFakeFederatorQueries() // existing test fake; ensure it records Upsert/SetMeta calls
	f := newTestFederator(q)       // existing helper
	f.avatarFetch = func(_ context.Context, _ string, _ bool) ([]byte, error) { return validPNG(t), nil }
	// account 7 has no avatar, source NULL
	q.accounts[7] = db.Account{ID: 7}
	f.runAvatarInherit(context.Background(), testClientWithPicture("https://pic/x.png"), db.UpstreamIdp{PictureClaim: "picture"}, &Tokens{Raw: map[string]any{"picture": "https://pic/x.png"}}, 7)
	if !q.setMetaUpstreamCalled[7] {
		t.Fatal("want SetAccountAvatarMetaUpstream for account 7")
	}
	if _, pending := f.kvStore.(*kv.MemoryStore); pending { /* key cleared */ }
}

func TestAvatarInherit_SkipsUserSource(t *testing.T) {
	q := newFakeFederatorQueries()
	f := newTestFederator(q)
	called := false
	f.avatarFetch = func(_ context.Context, _ string, _ bool) ([]byte, error) { called = true; return validPNG(t), nil }
	q.accounts[7] = db.Account{ID: 7, AvatarSource: pgtype.Text{String: "user", Valid: true}}
	f.runAvatarInherit(context.Background(), testClientWithPicture("https://pic/x.png"), db.UpstreamIdp{PictureClaim: "picture"}, &Tokens{Raw: map[string]any{"picture": "https://pic/x.png"}}, 7)
	if called {
		t.Fatal("must not fetch when avatar_source='user'")
	}
}
```
> Adapt to the package's existing fake-querier shape (see `modes_test.go` for the hand-written fake pattern). The fake must record `UpsertAccountAvatarBytes`/`SetAccountAvatarMetaUpstream` and serve `GetAccountByID`. `validPNG(t)` produces a small real PNG via `image/png` (reuse from avatar tests if present).

- [ ] **Step 4: Run → fail.** `go test ./pkg/federation/oidc/ -run AvatarInherit -v` → FAIL.

- [ ] **Step 5: Implement the job** (`federation.go`). Add the fetcher seam field to `Federator` (init in the constructor to `fetchUpstreamAvatar`), and the two methods:

```go
// kickoffAvatarInherit launches the background avatar-inherit job for accountID
// unless the user owns their avatar. Non-blocking; safe to call on every
// federated login. client is the already-built RP client (reused for UserInfo).
func (f *Federator) kickoffAvatarInherit(client *Client, idp db.UpstreamIdp, tokens *Tokens, accountID int32) {
	// Cheap pre-check so we don't even spawn a goroutine for user-owned avatars.
	if acct, err := f.q.GetAccountByID(context.Background(), accountID); err == nil &&
		acct.AvatarSource.Valid && acct.AvatarSource.String == "user" {
		return
	}
	go f.runAvatarInherit(context.Background(), client, idp, tokens, accountID)
}

// runAvatarInherit resolves the upstream picture (id_token claim, else UserInfo),
// fetches + normalizes it, and stores it as the account avatar with
// avatar_source='upstream'. All failures are non-fatal (logged, status cleared).
// Deduped via SetNX on the status key. Runs on a detached context with a timeout.
func (f *Federator) runAvatarInherit(parent context.Context, client *Client, idp db.UpstreamIdp, tokens *Tokens, accountID int32) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()

	key := AvatarFetchKey(accountID)
	ok, err := f.kvStore.SetNX(ctx, key, "1", 60*time.Second)
	if err != nil || !ok {
		return // another fetch in flight, or KV error → fail closed (no spinner-hang: TTL backstops)
	}
	defer func() { _ = f.kvStore.Del(ctx, key) }()

	pic := ClaimString(tokens.Raw, idp.PictureClaim)
	if pic == "" {
		if ui, uerr := client.UserInfo(ctx, tokens.AccessToken); uerr == nil {
			pic = ClaimString(ui, idp.PictureClaim)
		}
	}
	if pic == "" {
		return // no picture available; not an error
	}

	raw, err := f.avatarFetch(ctx, pic, f.allowPrivateNetwork)
	if err != nil {
		slog.WarnContext(ctx, "federation: upstream avatar fetch failed", "account_id", accountID, "err", err)
		return
	}
	out, etag, err := avatar.Process(raw)
	if err != nil {
		slog.WarnContext(ctx, "federation: upstream avatar process failed", "account_id", accountID, "err", err)
		return
	}

	// Re-read under the (post-fetch) state: bail if the user took over mid-flight,
	// or if the etag is unchanged (no-op refresh — avoid write churn).
	acct, err := f.q.GetAccountByID(ctx, accountID)
	if err != nil {
		return
	}
	if acct.AvatarSource.Valid && acct.AvatarSource.String == "user" {
		return
	}
	if acct.AvatarEtag.Valid && acct.AvatarEtag.String == etag {
		return
	}

	if err := f.q.UpsertAccountAvatarBytes(ctx, db.UpsertAccountAvatarBytesParams{AccountID: accountID, Bytes: out}); err != nil {
		slog.WarnContext(ctx, "federation: store upstream avatar bytes failed", "account_id", accountID, "err", err)
		return
	}
	if err := f.q.SetAccountAvatarMetaUpstream(ctx, db.SetAccountAvatarMetaUpstreamParams{
		ID: accountID, AvatarContentType: pgtype.Text{String: "image/webp", Valid: true}, AvatarEtag: pgtype.Text{String: etag, Valid: true},
	}); err != nil {
		slog.WarnContext(ctx, "federation: set upstream avatar meta failed", "account_id", accountID, "err", err)
	}
}
```
Add imports: `"log/slog"`, `"prohibitorum/pkg/avatar"`, and ensure `pgtype`/`time` are imported. Add the seam field to the `Federator` struct: `avatarFetch func(ctx context.Context, url string, allowPrivate bool) ([]byte, error)` and initialize it in `NewFederator` to `fetchUpstreamAvatar`.
> Bytes+meta are written via `f.q` (pool-backed) rather than a tx: this is best-effort and a crash between the two writes is benign (meta-without-bytes → public GET 404s gracefully; bytes-without-meta → harmless orphan). Keeping it on `f.q` avoids threading a pool tx into the goroutine.

- [ ] **Step 6: Run → pass + build.** `go test ./pkg/federation/oidc/ -run AvatarInherit -v && CGO_ENABLED=0 go build -tags nodynamic ./...`

- [ ] **Step 7: Commit**

```bash
git add pkg/federation/oidc/federation.go pkg/federation/oidc/state.go pkg/federation/oidc/federation_test.go
git commit -m "feat(federation): background upstream-avatar-inherit job with KV status + dedup + source guard"
```

---

### Task 5: `ResolveOutcome` + invite auto-confirm + re-login confirmed flag

**Goal:** Change the mode policies to report whether the identity is confirmed (→ issue session) vs pending (→ confirmation gate), and auto-confirm invite identities in-tx.

**Files:**
- Modify: `pkg/federation/oidc/modes.go`, `pkg/federation/oidc/federation.go` (`HandleCallback` call sites), `pkg/federation/oidc/modes.go` `ModesQueries` (+ `ConfirmAccountIdentity`)
- Test: `pkg/federation/oidc/modes_test.go`

**Acceptance Criteria:**
- [ ] `Resolve` returns `ResolveOutcome{AccountID, IdentityID, IsNew, Confirmed}`: auto-provision → `Confirmed=false`; existing confirmed identity → `Confirmed=true`; existing unconfirmed → `Confirmed=false` with the existing IDs (no re-insert).
- [ ] `applyInviteOnly` calls `ConfirmAccountIdentity` in-tx → `Confirmed=true`.

**Verify:** `go test ./pkg/federation/oidc/ -run 'Resolve|Provision|Invite' -v` → pass.

**Steps:**

- [ ] **Step 1: Define `ResolveOutcome`** (`modes.go`, near the top):

```go
// ResolveOutcome is the result of identity resolution. Confirmed=false routes the
// HTTP layer to the /welcome confirmation gate (no session yet); Confirmed=true
// means issue a durable session now. IdentityID is the account_identity row to
// confirm on YES.
type ResolveOutcome struct {
	AccountID  int32
	IdentityID int64
	IsNew      bool
	Confirmed  bool
}
```

- [ ] **Step 2: Add `ConfirmAccountIdentity` to `ModesQueries`** (interface in `modes.go`):

```go
	ConfirmAccountIdentity(ctx context.Context, id int64) error
```

- [ ] **Step 3: Write/extend the failing tests** (`modes_test.go`). Add cases asserting the outcome:

```go
func TestResolve_ExistingUnconfirmed_NotConfirmed(t *testing.T) {
	q := newFakeModesQueries()
	q.identityByIssuerSub = db.AccountIdentity{ID: 5, AccountID: 9, UpstreamIdpID: 1, ConfirmedAt: pgtype.Timestamptz{}} // pending
	out, err := Resolve(context.Background(), q, noopWriter{}, &db.UpstreamIdp{ID: 1, Slug: "idp"}, &Tokens{Issuer: "i", Subject: "s"}, nil)
	if err != nil || out.Confirmed || out.AccountID != 9 || out.IdentityID != 5 {
		t.Fatalf("got %+v err=%v; want unconfirmed reuse of account 9 / identity 5", out, err)
	}
}

func TestResolve_ExistingConfirmed_Confirmed(t *testing.T) {
	q := newFakeModesQueries()
	q.identityByIssuerSub = db.AccountIdentity{ID: 5, AccountID: 9, UpstreamIdpID: 1, ConfirmedAt: pgtype.Timestamptz{Time: time.Unix(1, 0), Valid: true}}
	out, _ := Resolve(context.Background(), q, noopWriter{}, &db.UpstreamIdp{ID: 1, Slug: "idp"}, &Tokens{Issuer: "i", Subject: "s"}, nil)
	if !out.Confirmed { t.Fatal("want Confirmed for an existing confirmed identity") }
}

func TestApplyAutoProvision_NotConfirmed(t *testing.T) {
	// existing auto-provision test setup → assert out.Confirmed == false and out.IsNew == true
}

func TestApplyInviteOnly_Confirmed(t *testing.T) {
	// existing invite test setup → assert out.Confirmed == true and ConfirmAccountIdentity was called in-tx
}
```
> Update the existing `Resolve`/auto-provision/invite tests to the new struct return (they currently assert `(accountID, isNew, err)`).

- [ ] **Step 4: Run → fail.** `go test ./pkg/federation/oidc/ -run 'Resolve|Provision|Invite' -v` → FAIL (signature mismatch / fields).

- [ ] **Step 5: Change signatures + logic** (`modes.go`):
  - `Resolve(...) (ResolveOutcome, error)`. On the existing-identity (`err == nil`) branch, after `syncClaims`, return `ResolveOutcome{AccountID: existing.AccountID, IdentityID: existing.ID, IsNew: false, Confirmed: existing.ConfirmedAt.Valid}` (still audit `Use` only when confirmed; for unconfirmed, skip the Use audit — it'll be audited on confirm). The `idp_mismatch_relogin` and error branches return `ResolveOutcome{}, err`.
  - `applyAutoProvision(...) (ResolveOutcome, error)`: capture the inserted identity's ID from `InsertAccountIdentity` (it returns the row); return `ResolveOutcome{AccountID: acct.ID, IdentityID: ident.ID, IsNew: true, Confirmed: false}`.
  - `applyInviteOnly(...) (ResolveOutcome, error)`: after `InsertAccountIdentity`, call `qtx.ConfirmAccountIdentity(ctx, ident.ID)` (in the same tx); return `ResolveOutcome{AccountID: acct.ID, IdentityID: ident.ID, IsNew: true, Confirmed: true}`.
  - `applyLinkOnly` returns `ResolveOutcome{}, authn.ErrLinkRequired()`.
  - `runProvisionTx`'s `fn` return type becomes `(ResolveOutcome, error)`; update its signature and the two callers.

- [ ] **Step 6: Update `HandleCallback` call sites** (`federation.go` ~lines 484-498) to the new return:

```go
	var outcome ResolveOutcome
	if state.EnrollmentToken != "" {
		outcome, err = applyInviteOnly(ctx, f.q, f.audit, &idp, tokens, state.EnrollmentToken, f.dbPool)
	} else {
		outcome, err = Resolve(ctx, f.q, f.audit, &idp, tokens, f.dbPool)
	}
	if err != nil {
		return nil, err
	}
```
Then re-fetch `outcome.AccountID`, keep the disabled check, and extend `CallbackResult` (Task 6 consumes `Confirmed`/`IdentityID`). For now add `Confirmed bool` and `IdentityID int64` to `CallbackResult` and populate from `outcome`.

- [ ] **Step 7: Run → pass + build.** `go test ./pkg/federation/oidc/ -run 'Resolve|Provision|Invite' -v && CGO_ENABLED=0 go build -tags nodynamic ./...`

- [ ] **Step 8: Commit**

```bash
git add pkg/federation/oidc/modes.go pkg/federation/oidc/federation.go pkg/federation/oidc/modes_test.go
git commit -m "feat(federation): ResolveOutcome (Confirmed/IdentityID); invite auto-confirms; re-login carries confirmed state"
```

---

### Task 6: Confirmation grant + callback branch + confirm HTTP endpoints

**Goal:** Withhold the session for unconfirmed identities; create a KV+cookie grant and redirect to `/welcome`; serve confirm GET/POST/decline.

**Files:**
- Modify: `pkg/federation/oidc/state.go` (`ConfirmGrant`, `ConfirmKey`), `pkg/federation/oidc/federation.go` (grant creation API), `pkg/server/handle_federation.go` (callback branch), `pkg/server/server.go` (routes), `pkg/contract/auth.go` (`FederationConfirmView`)
- Create: `pkg/server/handle_federation_confirm.go`, `pkg/server/handle_federation_confirm_test.go`

**Acceptance Criteria:**
- [ ] On `Confirmed=false`, the callback issues NO session cookie, sets the fed-state (anti-forgery) cookie, and redirects to `/welcome`.
- [ ] `GET .../federation/confirm` returns the identity + `avatarUrl`/`avatarPending` for a valid grant; 401 otherwise.
- [ ] `POST .../federation/confirm` (valid grant + matching cookie) confirms the identity, issues a session, returns `{redirect}`; `POST .../decline` pops the grant.

**Verify:** `go test ./pkg/server/ -run FederationConfirm -v` → pass.

**Steps:**

- [ ] **Step 1: Add the grant type + key to `state.go`**

```go
// ConfirmGrant is the short-lived, single-use, browser-bound context for the
// /welcome identity-confirmation step. Created when the callback withholds a
// session for an unconfirmed identity; consumed by the confirm endpoint.
type ConfirmGrant struct {
	AccountID      int32  `json:"account_id"`
	IdentityID     int64  `json:"identity_id"`
	IDPID          int64  `json:"idp_id"`
	IDPSlug        string `json:"idp_slug"`
	ReturnTo       string `json:"return_to"`
	BrowserBinding string `json:"browser_binding"`
}

func (g ConfirmGrant) Encode() (string, error) {
	b, err := json.Marshal(g)
	if err != nil {
		return "", fmt.Errorf("federation/oidc: encode confirm grant: %w", err)
	}
	return string(b), nil
}

func DecodeConfirmGrant(raw string) (*ConfirmGrant, error) {
	var g ConfirmGrant
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		return nil, fmt.Errorf("federation/oidc: decode confirm grant: %w", err)
	}
	return &g, nil
}

// ConfirmKey namespaces the confirmation-grant token (distinct from login/link/sudo).
func ConfirmKey(token string) string { return "oidc:fed:confirm:" + token }
```

- [ ] **Step 2: Add a grant-creation method on `Federator`** (`federation.go`) that the HTTP layer calls when `outcome.Confirmed == false`. It mints a token + anti-forgery value, stores the grant, and returns both:

```go
// CreateConfirmGrant stashes a confirmation grant in KV (15 min TTL) and returns
// the KV token plus the raw anti-forgery value the HTTP layer must set as the
// fed-state cookie (its SHA-256 is bound into the grant). browserBinding mirrors
// the login flow (BrowserBinding helper in secret.go / existing browserBindingOK).
func (f *Federator) CreateConfirmGrant(ctx context.Context, outcome ResolveOutcome, idp db.UpstreamIdp, returnTo string) (token, antiForgery string, err error) {
	token, err = randToken() // reuse the existing random-token helper used by BeginLogin
	if err != nil {
		return "", "", err
	}
	antiForgery, err = randToken()
	if err != nil {
		return "", "", err
	}
	grant := ConfirmGrant{
		AccountID:      outcome.AccountID,
		IdentityID:     outcome.IdentityID,
		IDPID:          idp.ID,
		IDPSlug:        idp.Slug,
		ReturnTo:       returnTo,
		BrowserBinding: hashBinding(antiForgery), // same hash used for FedState.BrowserBinding
	}
	enc, err := grant.Encode()
	if err != nil {
		return "", "", err
	}
	if err := f.kvStore.SetEx(ctx, ConfirmKey(token), enc, 15*time.Minute); err != nil {
		return "", "", fmt.Errorf("federation/oidc: store confirm grant: %w", err)
	}
	return token, antiForgery, nil
}

// PopConfirmGrant single-use-consumes a grant and validates the browser binding.
func (f *Federator) PopConfirmGrant(ctx context.Context, token, antiForgery string) (*ConfirmGrant, error) {
	raw, err := f.kvStore.Pop(ctx, ConfirmKey(token))
	if err != nil {
		return nil, authn.ErrFederationStateInvalid()
	}
	g, err := DecodeConfirmGrant(raw)
	if err != nil || !browserBindingOK(g.BrowserBinding, antiForgery) {
		return nil, authn.ErrFederationStateInvalid()
	}
	return g, nil
}

// PeekConfirmGrant reads (without consuming) a grant for the confirm GET, validating
// the browser binding. Used to render the /welcome identity + avatar status.
func (f *Federator) PeekConfirmGrant(ctx context.Context, token, antiForgery string) (*ConfirmGrant, error) {
	raw, err := f.kvStore.Get(ctx, ConfirmKey(token))
	if err != nil {
		return nil, authn.ErrFederationStateInvalid()
	}
	g, err := DecodeConfirmGrant(raw)
	if err != nil || !browserBindingOK(g.BrowserBinding, antiForgery) {
		return nil, authn.ErrFederationStateInvalid()
	}
	return g, nil
}

// AvatarPending reports whether a background avatar fetch is in flight for accountID.
func (f *Federator) AvatarPending(ctx context.Context, accountID int32) bool {
	_, err := f.kvStore.Get(ctx, AvatarFetchKey(accountID))
	return err == nil
}
```
> Reuse the existing helpers: the random-token generator used by `BeginLogin` (grep `func randToken`/`generateToken` in `federation.go`/`secret.go`), the binding hash used to build `FedState.BrowserBinding`, and `browserBindingOK` (already used in `HandleCallback`). If the binding hash is inlined at the begin site, extract `hashBinding(raw string) string` and reuse it both places. The token is carried in the fed-state cookie (the confirm GET/POST read `r.Cookie(FedStateCookieName)`), so it is NOT exposed in the redirect URL — the grant KV token and the cookie anti-forgery value together gate access.

> **Token transport decision:** put the KV `token` in the redirect as an opaque path/query is unnecessary — instead store BOTH the KV token and the anti-forgery value in the single fed-state cookie as `"<token>.<antiForgery>"`, and have the confirm endpoints split it. This keeps `/welcome` URL clean and the grant fully cookie-bound. Implement a tiny `splitConfirmCookie(v string) (token, antiForgery string)` in `handle_federation_confirm.go`.

- [ ] **Step 3: Branch the callback** (`pkg/server/handle_federation.go`, replacing the unconditional Issue at ~lines 148-161):

```go
	if !result.Confirmed {
		// Withhold the session: create a confirmation grant + cookie, redirect to /welcome.
		token, antiForgery, gerr := s.federator.CreateConfirmGrant(r.Context(), oidc.ResolveOutcome{
			AccountID: result.AccountID, IdentityID: result.IdentityID,
		}, db.UpstreamIdp{ID: result.IDPID, Slug: result.IDPSlug}, result.ReturnTo)
		if gerr != nil {
			writeAuthErr(w, gerr)
			return
		}
		http.SetCookie(w, sessstore.FedStateCookie(s.config, r, token+"."+antiForgery))
		http.Redirect(w, r, "/welcome", http.StatusFound)
		return
	}
	// Confirmed → issue the durable session (existing logic).
	http.SetCookie(w, sessstore.ClearedFedStateCookie(s.config, r))
	ip := sessstore.ClientIP(r, s.config.TrustProxy)
	ua := r.UserAgent()
	amr := result.AMR
	if len(amr) == 0 {
		amr = []string{"federated"}
	}
	idpID := result.IDPID
	token, _, err := s.sessionStore.Issue(r.Context(), result.AccountID, ip, ua, amr, &idpID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	http.SetCookie(w, sessstore.FreshSessionCookie(s.config, r, result.AccountID, token, s.config.SessionTTL))
	http.Redirect(w, r, result.ReturnTo, http.StatusFound)
```
> `CreateConfirmGrant` takes the IDs out of the result (the spec keeps `ResolveOutcome` in the oidc package; pass the fields). Also: after a `Confirmed=true` login AND after the confirm POST below, call `s.federator.KickoffAvatarFromCallback(...)` — but the kickoff happens inside the federation layer at the end of `HandleCallback` (it has the `client`+`tokens`), so add `f.kickoffAvatarInherit(client, idp, tokens, outcome.AccountID)` at the end of `HandleCallback` for BOTH branches (confirmed and unconfirmed) before returning the result. That keeps the avatar job in the federation package; the HTTP layer only handles session/redirect.

- [ ] **Step 4: Add the `FederationConfirmView` contract** (`pkg/contract/auth.go`):

```go
type FederationConfirmView struct {
	IDPDisplayName string  `json:"idpDisplayName"`
	DisplayName    string  `json:"displayName"`
	Username       string  `json:"username"`
	Email          string  `json:"email"`
	AvatarURL      *string `json:"avatarUrl,omitempty"`
	AvatarPending  bool    `json:"avatarPending"`
}
```

- [ ] **Step 5: Write the failing test** (`handle_federation_confirm_test.go`) — follow the sibling `handle_federation_test.go` harness (Server + fake federator/queries + test cookies). Cover: confirm GET with a valid grant cookie returns the identity; GET without/with-bad cookie → 401; confirm POST issues a session (asserts a session cookie set) + returns redirect + `confirmed_at` set; decline pops the grant.

- [ ] **Step 6: Run → fail.** `go test ./pkg/server/ -run FederationConfirm -v` → FAIL (handlers undefined).

- [ ] **Step 7: Implement `pkg/server/handle_federation_confirm.go`**

```go
package server

import (
	"net/http"

	"prohibitorum/pkg/avatar"
	"prohibitorum/pkg/contract"
	sessstore "prohibitorum/pkg/session"
)

// GET /api/prohibitorum/auth/federation/confirm — grant-scoped (fed-state cookie), no session.
func (s *Server) handleFederationConfirmGet(w http.ResponseWriter, r *http.Request) {
	token, anti := splitConfirmCookie(cookieValue(r, sessstore.FedStateCookieName))
	grant, err := s.federator.PeekConfirmGrant(r.Context(), token, anti)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	acct, err := s.queries.GetAccountByID(r.Context(), grant.AccountID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	idp, err := s.queries.GetUpstreamIDPBySlug(r.Context(), grant.IDPSlug)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	view := contract.FederationConfirmView{
		IDPDisplayName: idp.DisplayName,
		DisplayName:    acct.DisplayName,
		Username:       acct.Username,
		Email:          acct.Email.String,
		AvatarPending:  s.federator.AvatarPending(r.Context(), grant.AccountID),
	}
	if u := avatar.AccountURL(acct, s.config.PublicOrigins[0]); u != "" {
		view.AvatarURL = &u
	}
	writeJSON(w, view)
}

// POST /api/prohibitorum/auth/federation/confirm — YES: confirm + issue session.
func (s *Server) handleFederationConfirmPost(w http.ResponseWriter, r *http.Request) {
	token, anti := splitConfirmCookie(cookieValue(r, sessstore.FedStateCookieName))
	grant, err := s.federator.PopConfirmGrant(r.Context(), token, anti)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if err := s.queries.ConfirmAccountIdentity(r.Context(), grant.IdentityID); err != nil {
		writeAuthErr(w, err)
		return
	}
	http.SetCookie(w, sessstore.ClearedFedStateCookie(s.config, r))
	ip := sessstore.ClientIP(r, s.config.TrustProxy)
	idpID := grant.IDPID
	sess, _, err := s.sessionStore.Issue(r.Context(), grant.AccountID, ip, r.UserAgent(), []string{"federated"}, &idpID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	http.SetCookie(w, sessstore.FreshSessionCookie(s.config, r, grant.AccountID, sess, s.config.SessionTTL))
	writeJSON(w, map[string]string{"redirect": grant.ReturnTo})
}

// POST /api/prohibitorum/auth/federation/confirm/decline — NO: invalidate the grant.
func (s *Server) handleFederationConfirmDecline(w http.ResponseWriter, r *http.Request) {
	token, anti := splitConfirmCookie(cookieValue(r, sessstore.FedStateCookieName))
	_, _ = s.federator.PopConfirmGrant(r.Context(), token, anti) // best-effort consume
	http.SetCookie(w, sessstore.ClearedFedStateCookie(s.config, r))
	w.WriteHeader(http.StatusNoContent)
}

func cookieValue(r *http.Request, name string) string {
	if c, err := r.Cookie(name); err == nil {
		return c.Value
	}
	return ""
}

func splitConfirmCookie(v string) (token, antiForgery string) {
	if i := indexByte(v, '.'); i >= 0 {
		return v[:i], v[i+1:]
	}
	return v, ""
}
```
> Adapt `s.queries`/`s.federator`/`writeJSON`/`writeAuthErr` to the package's exact names (grep a sibling handler). `indexByte` = `strings.IndexByte`. `avatar.AccountURL` is the existing helper. The GET is `AuthPublic` (grant-scoped, no session); the POSTs likewise public (they bootstrap the session).

- [ ] **Step 8: Register routes** (`pkg/server/server.go`, near the federation routes):

```go
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/auth/federation/confirm", contract.AuthRequirement{Kind: contract.AuthPublic}, s.handleFederationConfirmGet)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/auth/federation/confirm", contract.AuthRequirement{Kind: contract.AuthPublic}, s.handleFederationConfirmPost)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/auth/federation/confirm/decline", contract.AuthRequirement{Kind: contract.AuthPublic}, s.handleFederationConfirmDecline)
```

- [ ] **Step 9: Run → pass + build + vet.** `podman compose up -d && CGO_ENABLED=0 go build -tags nodynamic ./... && go vet ./... && go test ./pkg/server/ -run FederationConfirm -v && go test ./pkg/federation/...`

- [ ] **Step 10: Commit**

```bash
git add pkg/federation/oidc/state.go pkg/federation/oidc/federation.go pkg/server/handle_federation.go pkg/server/handle_federation_confirm.go pkg/server/handle_federation_confirm_test.go pkg/server/server.go pkg/contract/auth.go
git commit -m "feat(federation): withhold session for unconfirmed identity; /welcome confirmation grant + confirm/decline endpoints"
```

---

### Task 7: Avatar source on self-service + status endpoint + `/me` avatarPending

**Goal:** User upload/remove set `avatar_source='user'`; expose avatar-fetch progress to authed sessions.

**Files:**
- Modify: `pkg/server/handle_avatar.go`, `pkg/server/handle_me.go`, `pkg/server/server.go`, `pkg/contract/auth.go`
- Test: `pkg/server/handle_avatar_test.go`, `pkg/server/handle_me_test.go`

**Acceptance Criteria:**
- [ ] `PUT /me/avatar` sets `avatar_source='user'`; `DELETE /me/avatar` sets `avatar_source='user'`.
- [ ] `GET /me/avatar/status` (authed) returns `{ "pending": bool }`; `GET /me` includes `avatarPending`.

**Verify:** `go test ./pkg/server/ -run 'Avatar|Me' -v` → pass.

**Steps:**

- [ ] **Step 1: Update `handle_avatar.go` store calls.** In `handlePutAvatarHTTP`, change `SetAccountAvatarMeta` → `SetAccountAvatarMetaUser` (params unchanged: ID, content type, etag). The `DELETE` path already calls `ClearAccountAvatarMeta`, which now sets `avatar_source='user'` (Task 1) — no code change beyond regenerated types. Adjust the in-memory `sess.Account.AvatarSource` if the handler refreshes session fields.

- [ ] **Step 2: Add `AvatarPending` to `SessionView`** (`pkg/contract/auth.go`): `AvatarPending bool `json:"avatarPending,omitempty"``. Populate in `s.sessionView` (`handle_me.go`): `v.AvatarPending = s.federator.AvatarPending(ctx, a.ID)`. (Pass `ctx` into `sessionView`, or read in the caller; match the existing method shape.)

- [ ] **Step 3: Write the failing test** (`handle_avatar_test.go`): after `PUT /me/avatar`, assert the account row has `avatar_source='user'`; after `DELETE`, `avatar_source='user'`. (`handle_me_test.go`): with a pending KV key set, `/me` returns `avatarPending: true` and `/me/avatar/status` returns `{pending:true}`.

- [ ] **Step 4: Run → fail.** `go test ./pkg/server/ -run 'Avatar|Me' -v` → FAIL.

- [ ] **Step 5: Implement the status endpoint** (`handle_avatar.go`):

```go
// GET /api/prohibitorum/me/avatar/status — authed; reports the background
// upstream-avatar fetch state for the current account (drives the dashboard spinner).
func (s *Server) handleAvatarStatusHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	writeJSON(w, map[string]bool{"pending": s.federator.AvatarPending(r.Context(), sess.Account.ID)})
}
```
Register in `server.go` (authed):
```go
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/me/avatar/status", contract.AuthRequirement{Kind: contract.AuthSession}, s.handleAvatarStatusHTTP)
```

- [ ] **Step 6: Run → pass + build.** `go test ./pkg/server/ -run 'Avatar|Me' -v && CGO_ENABLED=0 go build -tags nodynamic ./...`

- [ ] **Step 7: Commit**

```bash
git add pkg/server/handle_avatar.go pkg/server/handle_me.go pkg/server/server.go pkg/contract/auth.go pkg/server/handle_avatar_test.go pkg/server/handle_me_test.go
git commit -m "feat(server): avatar_source='user' on upload/remove; /me/avatar/status + /me avatarPending"
```

---

### Task 8: Frontend — `WelcomeView` interstitial + route + i18n

**Goal:** The `/welcome` confirmation page: shows the identity + avatar, polls for the fetch, gates Continue, posts confirm/decline.

**Files:**
- Create: `dashboard/src/pages/WelcomeView.vue`, `dashboard/src/pages/WelcomeView.test.ts`
- Modify: `dashboard/src/router/*` (route table), `dashboard/src/lib/api.ts` (if needed), `dashboard/src/locales/en.ts`

**Acceptance Criteria:**
- [ ] `/welcome` fetches the confirm view, renders identity + avatar, shows a spinner + polls while `avatarPending`, enables Continue when settled or after a ~30 s cap, posts confirm → navigates to `redirect`, decline → `/login`.

**Verify:** `cd dashboard && npx vitest run src/pages/WelcomeView.test.ts && npx vue-tsc -b`.

**Steps:**

- [ ] **Step 1: Add i18n** (`dashboard/src/locales/en.ts`), a new `welcome` block (no apostrophes):

```ts
  welcome: {
    title: 'Confirm your account',
    via: 'Signing in via {idp}',
    description: 'Confirm this is the account you want to connect.',
    fetchingAvatar: 'Setting up your profile picture...',
    continue: 'Continue',
    notMe: 'Not me',
  },
```
Then: `grep -nP "[\x{2018}\x{2019}]" src/locales/en.ts || echo clean`.

- [ ] **Step 2: Add the route.** Register `/welcome` as a standalone route (peer of `/login`, `/consent` — NOT under the dashboard shell). Find the route table (grep `path: '/login'`) and add:

```ts
  { path: '/welcome', name: 'welcome', component: () => import('@/pages/WelcomeView.vue') },
```

- [ ] **Step 3: Write the failing test** (`WelcomeView.test.ts`), mocking the API. Cover: renders identity from the confirm GET; Continue disabled while `avatarPending:true`, enabled after the GET reports `avatarPending:false`; clicking Continue calls the confirm POST and navigates to `redirect`; Not-me calls decline. Mock `api.get`/`api.post`; stub the router.

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import WelcomeView from './WelcomeView.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn() } }))
import { api } from '@/lib/api'

const push = vi.fn()
const assignSpy = vi.fn()
vi.stubGlobal('location', { assign: assignSpy } as unknown as Location)

const i18n = () => createI18n({ legacy: false, locale: 'en', fallbackLocale: 'en', messages: { en } })

beforeEach(() => { vi.mocked(api.get).mockReset(); vi.mocked(api.post).mockReset(); push.mockReset(); assignSpy.mockReset() })

function mountView() {
  return mount(WelcomeView, { global: { plugins: [i18n()], mocks: { $router: { push } } } })
}

it('renders identity and gates Continue until the avatar settles', async () => {
  vi.mocked(api.get)
    .mockResolvedValueOnce({ idpDisplayName: 'Google', displayName: 'Jane', username: 'jane', email: 'j@x.com', avatarPending: true })
    .mockResolvedValueOnce({ idpDisplayName: 'Google', displayName: 'Jane', username: 'jane', email: 'j@x.com', avatarUrl: '/avatar/x?v=ab', avatarPending: false })
  const w = mountView(); await flushPromises()
  expect(w.text()).toContain('Jane')
  expect((w.find('[data-test="welcome-continue"]').element as HTMLButtonElement).disabled).toBe(true)
})
```
> The polling loop should be test-friendly: drive it so a second `api.get` resolving `avatarPending:false` flips Continue to enabled. Use the project's existing fake-timer or await-poll pattern from a sibling test if one exists; otherwise expose the poll interval as a prop defaulting small so the test can advance it.

- [ ] **Step 4: Run → fail.** `cd dashboard && npx vitest run src/pages/WelcomeView.test.ts` → FAIL (component missing).

- [ ] **Step 5: Implement `WelcomeView.vue`** (standalone, centered; reuse `UserAvatar`, `Button`, a spinner icon from `lucide-vue-next`):

```vue
<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Loader2 } from 'lucide-vue-next'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import UserAvatar from '@/components/custom/UserAvatar.vue'

interface ConfirmView {
  idpDisplayName: string; displayName: string; username: string; email: string
  avatarUrl?: string; avatarPending: boolean
}

const { t } = useI18n()
const view = ref<ConfirmView | null>(null)
const busy = ref(false)
const settled = ref(false) // avatar fetch finished or poll cap reached
let timer: ReturnType<typeof setTimeout> | undefined
const POLL_MS = 1500
const CAP_MS = 30000
let elapsed = 0

async function load(): Promise<void> {
  view.value = await api.get<ConfirmView>('/api/prohibitorum/auth/federation/confirm')
  if (!view.value.avatarPending || elapsed >= CAP_MS) { settled.value = true; return }
  elapsed += POLL_MS
  timer = setTimeout(load, POLL_MS)
}

async function confirm(): Promise<void> {
  busy.value = true
  const res = await api.post<{ redirect: string }>('/api/prohibitorum/auth/federation/confirm', {})
  location.assign(res.redirect || '/')
}
async function notMe(): Promise<void> {
  busy.value = true
  await api.post('/api/prohibitorum/auth/federation/confirm/decline', {})
  location.assign('/login')
}

onMounted(load)
onBeforeUnmount(() => { if (timer) clearTimeout(timer) })
</script>

<template>
  <div class="flex min-h-screen items-center justify-center p-6">
    <div v-if="view" class="flex w-full max-w-sm flex-col items-center gap-5 rounded-lg border bg-card p-8 text-center">
      <p class="text-sm text-muted">{{ t('welcome.via', { idp: view.idpDisplayName }) }}</p>
      <div class="relative">
        <UserAvatar :display-name="view.displayName" :username="view.username" :src="view.avatarUrl" class="size-20 text-2xl" />
        <span v-if="!settled" class="absolute inset-0 flex items-center justify-center rounded-md bg-black/40">
          <Loader2 class="size-6 animate-spin text-white" />
        </span>
      </div>
      <div>
        <p class="text-lg font-semibold">{{ view.displayName }}</p>
        <p class="text-sm text-muted">{{ view.email }}</p>
        <p class="text-xs text-muted">{{ view.username }}</p>
      </div>
      <p class="text-sm text-muted">{{ settled ? t('welcome.description') : t('welcome.fetchingAvatar') }}</p>
      <div class="flex w-full gap-2">
        <Button variant="ghost" class="flex-1" :disabled="busy" data-test="welcome-notme" @click="notMe">{{ t('welcome.notMe') }}</Button>
        <Button class="flex-1" :disabled="busy || !settled" data-test="welcome-continue" @click="confirm">{{ t('welcome.continue') }}</Button>
      </div>
    </div>
  </div>
</template>
```
> If `api.get`/`api.post` on a 401 throw and route the SPA to `/login` globally, a direct visit to `/welcome` without a grant lands on `/login` automatically — confirm the api layer's 401 behavior and rely on it (no extra handling needed). Match `UserAvatar` prop names from its current definition.

- [ ] **Step 6: Run → pass + typecheck.** `cd dashboard && npx vitest run src/pages/WelcomeView.test.ts && npx vue-tsc -b`

- [ ] **Step 7: Commit**

```bash
git add dashboard/src/pages/WelcomeView.vue dashboard/src/pages/WelcomeView.test.ts dashboard/src/router dashboard/src/locales/en.ts
git commit -m "feat(web): /welcome federated identity-confirmation interstitial with avatar-fetch gating"
```

---

### Task 9: Frontend — dashboard avatar spinner (returning users)

**Goal:** While a background avatar refresh is pending, show a spinner on the sidebar avatar and reload `/me` when it clears.

**Files:**
- Modify: `dashboard/src/components/custom/UserAvatar.vue` (loading state), `dashboard/src/components/custom/NavUser.vue` (poll), `dashboard/src/stores/auth.ts` (status poll helper)
- Test: `dashboard/src/components/custom/UserAvatar.test.ts`, `dashboard/src/components/custom/NavUser.test.ts`

**Acceptance Criteria:**
- [ ] `UserAvatar` renders a spinner overlay when `loading` is true.
- [ ] `NavUser` starts polling `/me/avatar/status` when `me.avatarPending` is true on load and calls `auth.reload()` when it clears.

**Verify:** `cd dashboard && npx vitest run src/components/custom/UserAvatar.test.ts src/components/custom/NavUser.test.ts && npx vue-tsc -b`.

**Steps:**

- [ ] **Step 1: Write the failing `UserAvatar` test** — add to `UserAvatar.test.ts`:

```ts
  it('shows a spinner overlay when loading', () => {
    const w = mount(UserAvatar, { props: { displayName: 'A B', loading: true } })
    expect(w.find('[data-test="avatar-spinner"]').exists()).toBe(true)
  })
```

- [ ] **Step 2: Run → fail.** `cd dashboard && npx vitest run src/components/custom/UserAvatar.test.ts` → FAIL.

- [ ] **Step 3: Add the `loading` prop to `UserAvatar.vue`.** Add `loading?: boolean` to the props, and in the template wrap the content with an overlay:

```vue
    <span v-if="loading" data-test="avatar-spinner" class="absolute inset-0 flex items-center justify-center bg-black/40">
      <Loader2 class="size-3 animate-spin text-white" />
    </span>
```
Add `position: relative` to the root span (`relative` class) and import `Loader2` from `lucide-vue-next`.

- [ ] **Step 4: Add a status-poll helper to `stores/auth.ts`**:

```ts
  function pollAvatarUntilSettled() {
    if (!me.value?.avatarPending) return
    const tick = async () => {
      try {
        const { pending } = await api.get<{ pending: boolean }>('/api/prohibitorum/me/avatar/status')
        if (pending) { setTimeout(tick, 1500); return }
      } catch { /* stop on error */ }
      await reload()
    }
    setTimeout(tick, 1500)
  }
```
Export it. (`reload()` already exists from the avatar feature.)

- [ ] **Step 5: Wire `NavUser.vue`** — on mount, call `auth.pollAvatarUntilSettled()`, and pass `:loading="auth.me?.avatarPending"` to the trigger `UserAvatar`. Update `NavUser.test.ts` expectations for the new prop/poll (mock `api.get`).

- [ ] **Step 6: Run → pass + typecheck.** `cd dashboard && npx vitest run src/components/custom/UserAvatar.test.ts src/components/custom/NavUser.test.ts && npx vue-tsc -b`

- [ ] **Step 7: Commit**

```bash
git add dashboard/src/components/custom/UserAvatar.vue dashboard/src/components/custom/NavUser.vue dashboard/src/stores/auth.ts dashboard/src/components/custom/UserAvatar.test.ts dashboard/src/components/custom/NavUser.test.ts
git commit -m "feat(web): dashboard avatar spinner while upstream avatar refresh is pending"
```

---

### Task 10: Admin + CLI `pictureClaim`

**Goal:** Expose the per-IdP `picture_claim` override in the admin upstream-IdP form/detail, the CLI, the contract, and dev-seed.

**Files:**
- Modify: `pkg/contract/auth.go` (UpstreamIDP view + create/update requests), `pkg/server/handle_admin_upstream_idps.go`, `cmd/prohibitorum/main.go` (upstream-idp CLI), `cmd/prohibitorum/dev_seed.go`, `dashboard/src/pages/admin/AdminUpstreamIdp*Vue` + TS types, `dashboard/src/locales/en.ts`
- Test: `pkg/server/handle_admin_upstream_idps_test.go`, the relevant FE test

**Acceptance Criteria:**
- [ ] Creating/updating an upstream IdP accepts `pictureClaim` (default `picture`); the admin detail shows + edits it.
- [ ] CLI `upstream-idp` create/update accept a `--picture-claim` flag.

**Verify:** `go test ./pkg/server/ -run UpstreamIDP -v && CGO_ENABLED=0 go build -tags nodynamic ./... && cd dashboard && npx vue-tsc -b`.

**Steps:**

- [ ] **Step 1: Contract.** Add `PictureClaim string `json:"pictureClaim"`` to the UpstreamIDP view struct (next to `EmailClaim`, `pkg/contract/auth.go:475`) and to the create/update request structs in the same file (mirror `EmailClaim`).

- [ ] **Step 2: Admin handler** (`handle_admin_upstream_idps.go`). Wherever `EmailClaim` is read from the request and written to `Create/UpdateUpstreamIDP*Params`, add `PictureClaim` alongside (default to `"picture"` when the request omits it — mirror how a missing `email_claim` is defaulted, or set `if req.PictureClaim == "" { req.PictureClaim = "picture" }`). Add `PictureClaim` to the view projection.

- [ ] **Step 3: CLI** (`cmd/prohibitorum/main.go`). In the `upstream-idp` create/update command, add a `--picture-claim` flag (default `"picture"`) mirroring `--display-name-claim`, and pass it into the params. Add it to `dev_seed.go`'s seeded IdP (default `"picture"`).

- [ ] **Step 4: Tests.** Extend `handle_admin_upstream_idps_test.go`: create with `pictureClaim:"avatar_url"` → persisted + returned; update changes it; omitted → defaults to `picture`.

- [ ] **Step 5: Frontend.** Add `pictureClaim` to the admin upstream-IdP TS type and the create/detail forms (mirror the `emailClaim` input). Add an i18n label (`admin.upstreamIdp.pictureClaim` or wherever the claim labels live) — grep the existing `displayNameClaim` label key and mirror. Grep apostrophes after editing `en.ts`.

- [ ] **Step 6: Run → pass + build + typecheck.** `go test ./pkg/server/ -run UpstreamIDP -v && CGO_ENABLED=0 go build -tags nodynamic ./... && cd dashboard && npx vue-tsc -b`

- [ ] **Step 7: Commit**

```bash
git add pkg/contract/auth.go pkg/server/handle_admin_upstream_idps.go pkg/server/handle_admin_upstream_idps_test.go cmd/prohibitorum dashboard/src/pages/admin dashboard/src/locales/en.ts
git commit -m "feat(admin): per-IdP picture_claim override (admin form + CLI + dev-seed)"
```

---

### Task 11: Smoke coverage + done-gate

**Goal:** Prove the federated confirm→avatar round-trip end-to-end (+ the no-clobber case) and ship.

**Files:**
- Modify: `cmd/smoke/main.go`, `pkg/webui/dist/**` (rebuilt)

**Acceptance Criteria:**
- [ ] Smoke: a federated auto-provision callback redirects to `/welcome` with NO session cookie → `GET .../federation/confirm` returns the identity → poll until `avatarPending=false` → `POST .../confirm` yields a session + redirect → `GET /avatar/{subject}` 200 `image/webp` → OIDC `picture` present. A confirmed user who uploaded an avatar (`avatar_source='user'`) then re-logs in federated is NOT clobbered. `SMOKE_EXIT=0`.
- [ ] Full gate green; `dist` rebuilt + committed.

**Verify:** see steps.

**Steps:**

- [ ] **Step 1: Add the smoke block** in `cmd/smoke/main.go` after the existing federation coverage. The smoke already drives the test OP (grep the federation login step). Extend it to: assert the callback `Location` is `/welcome` and that NO session cookie was set on that response; carry the fed-state cookie into `GET <base>/api/prohibitorum/auth/federation/confirm`, assert the identity fields; poll the confirm GET (or `/me/avatar/status` after confirm) until `avatarPending=false`; `POST .../confirm`, assert a session cookie + a `redirect` body; then `GET <base>/avatar/<subject>` → `200` + `Content-Type: image/webp`; decode an id_token / `/userinfo` with `profile` scope and assert `picture` == the public URL. Add a second sub-case: with that session, `PUT /me/avatar` a PNG (sets `avatar_source='user'`), then re-run a federated login for the same upstream identity (now confirmed → straight session) and assert the avatar etag is unchanged (upstream did not clobber). Use the smoke's `step(...)` + `log.Fatalf` idiom. The test OP must serve a `picture` claim — set it in the smoke OP's id_token/userinfo fixture (grep where the smoke OP mints claims) and the seeded IdP's `picture_claim` defaults to `picture`.

- [ ] **Step 2: Frontend gate + rebuild dist.**

Run: `cd dashboard && npm run test && npm run build`
Expected: vitest green; `vue-tsc -b` 0; `vite build` writes `../pkg/webui/dist`.

- [ ] **Step 3: Go gate.**

Run: `cd /home/tundra/projects/tundra/prohibitorum && CGO_ENABLED=0 go build -tags nodynamic ./... && go vet ./... && go test ./...`
Expected: 0 failures.

- [ ] **Step 4: Smoke.**

Run the smoke per the runbook (`podman compose up -d`; smoke env with `PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true`; build with `-tags nodynamic`; detached runner → poll for `SMOKE_EXIT=`). Expected: `SMOKE_EXIT=0` including the new federated confirm→avatar block.

- [ ] **Step 5: Live check** (`mise dev-server`; configure an upstream IdP that ships `picture`, e.g. via dev-seed): federated first login → lands on `/welcome` showing the identity + avatar spinner → Continue enables after the avatar loads → dashboard shows the avatar. Decline → returns to `/login`, no session. Re-login → straight to dashboard (confirmed).

- [ ] **Step 6: Commit dist + smoke.**

```bash
git add pkg/webui/dist cmd/smoke/main.go
git commit -m "test(smoke): federated confirm + upstream avatar round-trip + no-clobber; rebuild dist"
```

---

## Self-Review

**Spec coverage:** schema (avatar_source + picture_claim + confirmed_at + backfills) → T1; picture hoist + UserInfo → T2; SSRF fetch → T3; background job + KV status + dedup + source/etag guards → T4; ResolveOutcome + invite auto-confirm + re-login → T5; confirmation grant + callback withhold-branch + confirm GET/POST/decline → T6; self-service source + status endpoint + /me flag → T7; WelcomeView interstitial → T8; dashboard spinner → T9; admin/CLI picture_claim → T10; smoke + done-gate → T11. Migration-numbering risk → T1 Step 1. Non-goals (Entra/Graph, SAML-as-login, edit-on-welcome) excluded.

**Placeholder scan:** No "TBD"/"add error handling"; each code step shows code. Two flagged adaptation seams (the `rp.Userinfo` exact signature in T2; the package's exact `randToken`/`hashBinding`/`browserBindingOK` helper names in T6) are real integration points with the existing pattern named — not blanks.

**Type consistency:** `ResolveOutcome{AccountID int32, IdentityID int64, IsNew bool, Confirmed bool}` (T5) is consumed by `CallbackResult` (T5/T6) and `CreateConfirmGrant` (T6); `ConfirmGrant{AccountID, IdentityID, IDPID, IDPSlug, ReturnTo, BrowserBinding}` (T6); `AvatarFetchKey(int32)`/`ConfirmKey(string)` (T4/T6); `SetAccountAvatarMetaUpstream`/`SetAccountAvatarMetaUser`/`ClearAccountAvatarMeta` (T1) used in T4/T7; `ConfirmAccountIdentity(int64)` (T1) used in T5/T6; `FederationConfirmView`/`SessionView.AvatarPending` (T6/T7) consumed by T8/T9; `avatar.AccountURL`/`avatar.Process` (existing) reused. Consistent.

## Review follow-ups (tracked during execution)

- **[from Task 4 code review → do in Task 6]** Thread a `ctx context.Context` parameter into `kickoffAvatarInherit` (it currently uses `context.Background()` for its pre-flight `GetAccountByID`); pass the callback request ctx when wiring it into `HandleCallback`.
- **[from Task 4 code review → do in Task 11]** Add coverage for the **UserInfo-sourced picture fallback** in `runAvatarInherit` (the `client.UserInfo` branch — unit-untested; all Task 4 tests use a nil client + picture-in-id_token). Faithful approach: give the smoke's mock OP a `/userinfo` endpoint (advertised in discovery) that serves `picture`, and a case where the id_token OMITS `picture` so the avatar is inherited via the UserInfo fallback. Asserts the 3-arg `UserInfo(ctx, accessToken, subject)` wiring end-to-end.

## Done-gate

`CGO_ENABLED=0 go build -tags nodynamic ./...` / `go vet` / `go test ./...` (0), `vitest` (green), `vue-tsc -b` (0), smoke `SMOKE_EXIT=0` (incl. federated confirm→avatar + no-clobber + UserInfo-fallback), rebuild + commit `pkg/webui/dist`.
