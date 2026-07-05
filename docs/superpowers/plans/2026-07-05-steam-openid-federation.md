# Native Steam (OpenID 2.0) upstream provider — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add "Sign in with Steam" as a native upstream provider — a second protocol adapter under the existing `upstream_idp` umbrella — reusing all downstream federation machinery.

**Architecture:** A `protocol` discriminator (`oidc` | `steam`) on `upstream_idp`. A new leaf package `pkg/federation/steam` hand-rolls Steam's OpenID 2.0 login (verify via Steam's `check_authentication`) + `GetPlayerSummaries` enrichment. The existing `Federator` (in `pkg/federation/oidc`) branches on `protocol` at exactly two points — `begin` (build redirect) and `HandleCallback`/`LinkCallback` (verify + build a `*Tokens`) — after which the shared `Resolve`/modes, `account_identity` linking, `/welcome` confirmation, session issuance, avatar inheritance, admin CRUD, and login buttons are reused unchanged.

**Tech Stack:** Go (net/http, pgx, sqlc, goose), Vue 3 + Vite + Tailwind v4 + shadcn-vue, vitest, vue-tsc.

**User decisions (already made):**
- **Full parity — all three modes** for Steam (`auto_provision`/`invite_only`/`link_only`); email-less accounts allowed; email-verification gate disabled for Steam.
- **Steam Web API key required** for a Steam provider (encrypted in the existing secret slot).
- **Reuse `upstream_idp`** via a `protocol` discriminator (not a separate table).
- **Schema realization = inline empty-sentinel, NOT `DROP NOT NULL`.** Refinement discovered during planning: the sqlc-generated `UpstreamIdp` types `IssuerUrl string`/`ClientID string`/`Scopes []string` as non-null; dropping `NOT NULL` would flip them to `pgtype`/pointers and break every OIDC read. So migration `028` adds ONLY the `protocol` column; Steam rows store `issuer_url=''`, `client_id=''`, `scopes='{}'`. This keeps the OIDC code untouched. (Honors the approved "inline in the shared table" decision — the columns still live inline, holding `''`/`{}` for Steam.)
- **Login button:** bespoke — native `Button` shape, **black background, white Steam logo + white text**, using `/mnt/e/Steam_Symbol_0.svg` recolored grey→white via `currentColor`.

Spec: `docs/superpowers/specs/2026-07-05-steam-openid-federation-design.md`

---

## File Structure

**New files**
- `pkg/federation/steam/steam.go` — `Issuer` const, `BuildAuthURL`, `Verify` (OpenID 2.0), endpoint vars.
- `pkg/federation/steam/webapi.go` — `Summary`, `FetchSummary` (GetPlayerSummaries).
- `pkg/federation/steam/steam_test.go`, `webapi_test.go` — unit tests vs httptest mocks.
- `pkg/federation/steam/export_test.go` — endpoint-override seam for tests.
- `db/migrations/028_steam_protocol.sql` — add `protocol` column.
- `dashboard/src/assets/steam-logo.svg` — the recolored Steam mark.
- `dashboard/src/components/custom/SteamButton.vue` — the bespoke black Steam button.

**Modified files**
- `db/queries/upstream_idp.sql` — add `protocol` to `InsertUpstreamIDP` (+ regenerate sqlc).
- `pkg/federation/oidc/federation.go` — Federator steam seams; `begin` + `HandleCallback` + `LinkCallback` branches.
- `pkg/server/handle_federation.go` — pass `r.URL.Query()` into `HandleCallback`/`LinkCallback`.
- `pkg/server/handle_admin_upstream_idps.go` — create/update/view Steam variant.
- `pkg/contract/auth.go` — `Protocol` on `IdentityProviderView` + `FederationProvider`.
- `dashboard/src/pages/admin/AdminUpstreamIdpsView.vue`, `AdminUpstreamIdpDetailView.vue` — protocol-branched form.
- `dashboard/src/components/custom/FederationButtons.vue` — render `SteamButton` for `protocol=steam`.
- `dashboard/src/locales/en.ts`, `zh.ts` — Steam admin + button i18n.
- `cmd/smoke/main.go` — Steam mock-server smoke arc.

---

### Task 1: Schema + sqlc — `protocol` discriminator

**Goal:** Add the `protocol` column to `upstream_idp`, thread it through the insert query, and regenerate sqlc so `UpstreamIdp.Protocol` exists — with zero change to the OIDC column types.

**Files:**
- Create: `db/migrations/028_steam_protocol.sql`
- Modify: `db/queries/upstream_idp.sql`
- Regenerate: `pkg/db/models.go`, `pkg/db/upstream_idp.sql.go` (via `sqlc generate`)

**Acceptance Criteria:**
- [ ] Migration `028` applies and adds `protocol text NOT NULL DEFAULT 'oidc'` with a CHECK in (`oidc`,`steam`).
- [ ] `InsertUpstreamIDP` accepts a `protocol` parameter; `UpstreamIdp` and `InsertUpstreamIDPParams` gain `Protocol string`.
- [ ] `IssuerUrl`/`ClientID`/`Scopes` remain non-null Go types (unchanged).
- [ ] `go build -tags nodynamic ./...` clean after regen.

**Verify:** `mise run db migrate && sqlc generate && go build -tags nodynamic ./...`

**Steps:**

- [ ] **Step 1: Write `db/migrations/028_steam_protocol.sql`**

```sql
-- +goose Up
-- protocol discriminates the upstream federation protocol: 'oidc' (issuer/client/
-- token exchange, the existing rows) or 'steam' (OpenID 2.0 + Steam Web API). Steam
-- rows leave the OIDC-only columns as empty sentinels ('' / '{}') and carry an
-- encrypted Steam Web API key in the existing client_secret_enc slot.
ALTER TABLE upstream_idp
  ADD COLUMN IF NOT EXISTS protocol text NOT NULL DEFAULT 'oidc'
    CHECK (protocol IN ('oidc', 'steam'));

-- +goose Down
ALTER TABLE upstream_idp DROP COLUMN IF EXISTS protocol;
```

- [ ] **Step 2: Add `protocol` to `InsertUpstreamIDP` in `db/queries/upstream_idp.sql`**

Change the `InsertUpstreamIDP` block to include `protocol` as the final column/param:

```sql
-- name: InsertUpstreamIDP :one
INSERT INTO upstream_idp (slug, display_name, issuer_url, client_id,
  client_secret_enc, secret_nonce, key_version, scopes, mode,
  allowed_domains, username_claim, display_name_claim, email_claim,
  require_verified_email, picture_claim, protocol)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
RETURNING *;
```

(The `SELECT *` queries — `GetUpstreamIDPBySlug`, `ListUpstreamIDPs`, etc. — pick up `protocol` automatically. `UpdateUpstreamIDPConfig` is NOT changed: `protocol` is immutable after create.)

- [ ] **Step 3: Apply migration + regenerate sqlc**

Run: `mise run db migrate`
Expected: applies `028`, reaches version 28 (`mise run db status`).
Run: `sqlc generate`
Expected: `pkg/db/models.go` `UpstreamIdp` gains `Protocol string`; `InsertUpstreamIDPParams` gains `Protocol string`. Confirm with `grep -n "Protocol" pkg/db/models.go pkg/db/upstream_idp.sql.go`.

- [ ] **Step 4: Build**

Run: `go build -tags nodynamic ./...`
Expected: clean (no OIDC column-type breakage, since we only added a column).

- [ ] **Step 5: Commit**

```bash
git add db/migrations/028_steam_protocol.sql db/queries/upstream_idp.sql pkg/db/
git commit -m "feat(federation): add protocol discriminator to upstream_idp"
```

---

### Task 2: `pkg/federation/steam` — OpenID 2.0 + Web API adapter

**Goal:** A self-contained, hand-rolled Steam adapter: build the login redirect, verify the callback via Steam's `check_authentication`, and fetch the player's persona + avatar. No dependency on `pkg/federation/oidc`.

**Files:**
- Create: `pkg/federation/steam/steam.go`, `webapi.go`, `export_test.go`, `steam_test.go`, `webapi_test.go`

**Acceptance Criteria:**
- [ ] `BuildAuthURL(realm, returnTo)` produces a valid OpenID 2.0 `checkid_setup` URL to `steamcommunity.com/openid/login` with `identifier_select`.
- [ ] `Verify` rejects: wrong `openid.mode`, `openid.return_to` mismatch, `is_valid:false`, and any `claimed_id` not matching `^https://steamcommunity\.com/openid/id/(\d{17})$`; accepts a valid `is_valid:true` + well-formed claimed_id, returning the 17-digit SteamID.
- [ ] `FetchSummary` parses `personaname` + `avatarfull`; errors on empty player list / non-200.
- [ ] `go test ./pkg/federation/steam/` passes.

**Verify:** `go test ./pkg/federation/steam/ -v`

**Steps:**

- [ ] **Step 1: Write `pkg/federation/steam/steam.go`**

```go
// Package steam implements the narrow slice of Steam's OpenID 2.0 login flow needed
// to authenticate a user and read their public profile. Steam does NOT speak
// OAuth2/OIDC — this is a hand-rolled adapter (a general OpenID 2.0 library is
// deliberately avoided: several have documented Claimed-ID spoofing CVEs, and we
// need only this one flow). Verification delegates to Steam's own
// check_authentication endpoint; we never re-implement the DH signature.
package steam

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Issuer is the fixed pseudo-issuer stored as account_identity.upstream_iss for
// Steam identities (Steam has no OIDC issuer). Paired with the SteamID64 subject
// under UNIQUE(upstream_iss, upstream_sub).
const Issuer = "https://steamcommunity.com/openid"

const (
	nsOpenID2        = "http://specs.openid.net/auth/2.0"
	identifierSelect = "http://specs.openid.net/auth/2.0/identifier_select"
)

// Endpoints are package vars (not consts) so tests can point them at an httptest
// server via export_test.go. Production values are Steam's real endpoints.
var (
	loginEndpoint   = "https://steamcommunity.com/openid/login"
	summaryEndpoint = "https://api.steampowered.com/ISteamUser/GetPlayerSummaries/v2/"
)

// claimedIDRe anchors the Claimed-ID to Steam's exact format so a look-alike host or
// a non-numeric id cannot pass. THE anti-spoofing control (paired with
// check_authentication). A SteamID64 is exactly 17 digits.
var claimedIDRe = regexp.MustCompile(`^https://steamcommunity\.com/openid/id/(\d{17})$`)

// BuildAuthURL builds the OpenID 2.0 checkid_setup redirect. realm is the origin
// (e.g. "https://idp.example.com"); returnTo is the callback URL (must be under
// realm) carrying our state token as a query param.
func BuildAuthURL(realm, returnTo string) string {
	q := url.Values{
		"openid.ns":         {nsOpenID2},
		"openid.mode":       {"checkid_setup"},
		"openid.return_to":  {returnTo},
		"openid.realm":      {realm},
		"openid.identity":   {identifierSelect},
		"openid.claimed_id": {identifierSelect},
	}
	return loginEndpoint + "?" + q.Encode()
}

// Verify validates an OpenID 2.0 id_res callback and returns the SteamID64. params
// are the raw callback query values; expectedReturnTo is the exact openid.return_to
// we sent at begin (bound to our state token) — a mismatch is rejected before we
// contact Steam.
func Verify(ctx context.Context, hc *http.Client, params url.Values, expectedReturnTo string) (string, error) {
	if params.Get("openid.mode") != "id_res" {
		return "", fmt.Errorf("steam: unexpected openid.mode %q", params.Get("openid.mode"))
	}
	if params.Get("openid.return_to") != expectedReturnTo {
		return "", errors.New("steam: openid.return_to mismatch")
	}
	// Ask Steam to authenticate the assertion (mode=check_authentication). We echo
	// every openid.* param back verbatim, only flipping the mode.
	check := url.Values{}
	for k, v := range params {
		check[k] = v
	}
	check.Set("openid.mode", "check_authentication")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginEndpoint, strings.NewReader(check.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("steam: check_authentication: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	if !isValid(string(body)) {
		return "", errors.New("steam: check_authentication returned is_valid:false")
	}
	m := claimedIDRe.FindStringSubmatch(params.Get("openid.claimed_id"))
	if m == nil {
		return "", errors.New("steam: claimed_id did not match the Steam identifier format")
	}
	return m[1], nil
}

// isValid parses the key-value OpenID 2.0 response body for is_valid:true.
func isValid(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "is_valid:true" {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Write `pkg/federation/steam/webapi.go`**

```go
package steam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Summary is the subset of a Steam player summary we consume.
type Summary struct {
	PersonaName string
	AvatarURL   string
}

// FetchSummary calls ISteamUser/GetPlayerSummaries and returns the player's persona
// name + full-size avatar URL. apiKey is the Steam Web API key; steamID is 17 digits.
func FetchSummary(ctx context.Context, hc *http.Client, apiKey, steamID string) (Summary, error) {
	u := summaryEndpoint + "?" + url.Values{"key": {apiKey}, "steamids": {steamID}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Summary{}, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return Summary{}, fmt.Errorf("steam: player summaries: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Summary{}, fmt.Errorf("steam: player summaries status %d", resp.StatusCode)
	}
	var out struct {
		Response struct {
			Players []struct {
				PersonaName string `json:"personaname"`
				AvatarFull  string `json:"avatarfull"`
			} `json:"players"`
		} `json:"response"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return Summary{}, err
	}
	if len(out.Response.Players) == 0 {
		return Summary{}, errors.New("steam: no player summary returned")
	}
	p := out.Response.Players[0]
	return Summary{PersonaName: p.PersonaName, AvatarURL: p.AvatarFull}, nil
}
```

- [ ] **Step 3: Write `pkg/federation/steam/export_test.go`**

```go
package steam

// SetEndpoints overrides the Steam endpoints for tests (httptest servers). Returns a
// restore func.
func SetEndpoints(login, summary string) func() {
	oldL, oldS := loginEndpoint, summaryEndpoint
	loginEndpoint, summaryEndpoint = login, summary
	return func() { loginEndpoint, summaryEndpoint = oldL, oldS }
}
```

- [ ] **Step 4: Write `pkg/federation/steam/steam_test.go`**

```go
package steam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBuildAuthURL(t *testing.T) {
	got := BuildAuthURL("https://idp.example.com", "https://idp.example.com/cb?state=abc")
	if !strings.HasPrefix(got, "https://steamcommunity.com/openid/login?") {
		t.Fatalf("wrong endpoint: %s", got)
	}
	u, _ := url.Parse(got)
	q := u.Query()
	if q.Get("openid.mode") != "checkid_setup" || q.Get("openid.claimed_id") != identifierSelect ||
		q.Get("openid.return_to") != "https://idp.example.com/cb?state=abc" {
		t.Fatalf("bad params: %v", q)
	}
}

func steamMock(t *testing.T, valid bool) (*httptest.Server, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.FormValue("openid.mode") != "check_authentication" {
			t.Errorf("mock got mode %q, want check_authentication", r.FormValue("openid.mode"))
		}
		if valid {
			_, _ = w.Write([]byte("ns:http://specs.openid.net/auth/2.0\nis_valid:true\n"))
		} else {
			_, _ = w.Write([]byte("ns:http://specs.openid.net/auth/2.0\nis_valid:false\n"))
		}
	}))
	restore := SetEndpoints(srv.URL, srv.URL)
	return srv, func() { srv.Close(); restore() }
}

func validParams(returnTo, claimedID string) url.Values {
	return url.Values{
		"openid.mode":       {"id_res"},
		"openid.return_to":  {returnTo},
		"openid.claimed_id": {claimedID},
		"openid.identity":   {claimedID},
		"openid.sig":        {"whatever"},
		"openid.signed":     {"mode,return_to,claimed_id,identity"},
	}
}

func TestVerify(t *testing.T) {
	const rt = "https://idp.example.com/cb?state=abc"
	const good = "https://steamcommunity.com/openid/id/76561198000000000"

	t.Run("valid", func(t *testing.T) {
		_, done := steamMock(t, true)
		defer done()
		id, err := Verify(context.Background(), http.DefaultClient, validParams(rt, good), rt)
		if err != nil || id != "76561198000000000" {
			t.Fatalf("id=%q err=%v", id, err)
		}
	})
	t.Run("is_valid false", func(t *testing.T) {
		_, done := steamMock(t, false)
		defer done()
		if _, err := Verify(context.Background(), http.DefaultClient, validParams(rt, good), rt); err == nil {
			t.Fatal("expected error on is_valid:false")
		}
	})
	t.Run("return_to mismatch", func(t *testing.T) {
		_, done := steamMock(t, true)
		defer done()
		if _, err := Verify(context.Background(), http.DefaultClient, validParams(rt, good), "https://evil.example/cb"); err == nil {
			t.Fatal("expected return_to mismatch error")
		}
	})
	t.Run("spoofed claimed_id host", func(t *testing.T) {
		_, done := steamMock(t, true)
		defer done()
		bad := "https://steamcommunity.com.evil.example/openid/id/76561198000000000"
		if _, err := Verify(context.Background(), http.DefaultClient, validParams(rt, bad), rt); err == nil {
			t.Fatal("expected claimed_id host rejection")
		}
	})
	t.Run("non-numeric claimed_id", func(t *testing.T) {
		_, done := steamMock(t, true)
		defer done()
		bad := "https://steamcommunity.com/openid/id/notanid"
		if _, err := Verify(context.Background(), http.DefaultClient, validParams(rt, bad), rt); err == nil {
			t.Fatal("expected claimed_id format rejection")
		}
	})
	t.Run("wrong mode", func(t *testing.T) {
		_, done := steamMock(t, true)
		defer done()
		p := validParams(rt, good)
		p.Set("openid.mode", "cancel")
		if _, err := Verify(context.Background(), http.DefaultClient, p, rt); err == nil {
			t.Fatal("expected wrong-mode rejection")
		}
	})
}
```

- [ ] **Step 5: Write `pkg/federation/steam/webapi_test.go`**

```go
package steam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "APIKEY" || r.URL.Query().Get("steamids") != "76561198000000000" {
			t.Errorf("bad query: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"response":{"players":[{"personaname":"Gaben","avatarfull":"https://cdn/avatar.jpg"}]}}`))
	}))
	defer srv.Close()
	defer SetEndpoints(loginEndpoint, srv.URL)()

	s, err := FetchSummary(context.Background(), http.DefaultClient, "APIKEY", "76561198000000000")
	if err != nil || s.PersonaName != "Gaben" || s.AvatarURL != "https://cdn/avatar.jpg" {
		t.Fatalf("summary=%+v err=%v", s, err)
	}
}

func TestFetchSummaryEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"response":{"players":[]}}`))
	}))
	defer srv.Close()
	defer SetEndpoints(loginEndpoint, srv.URL)()

	if _, err := FetchSummary(context.Background(), http.DefaultClient, "APIKEY", "76561198000000000"); err == nil {
		t.Fatal("expected error on empty player list")
	}
}
```

- [ ] **Step 6: Run + commit**

Run: `gofmt -w pkg/federation/steam/*.go && go test ./pkg/federation/steam/ -v`
Expected: PASS.
```bash
git add pkg/federation/steam/
git commit -m "feat(steam): OpenID 2.0 verify + GetPlayerSummaries adapter"
```

---

### Task 3: Federator seams + `begin`/`HandleCallback` Steam branch

**Goal:** Branch the Federator on `idp.Protocol` for the login + invite flows: build the Steam redirect at `begin`, and at `HandleCallback` verify via Steam + enrich + build a `*Tokens` that flows into the existing `Resolve`/`applyInviteOnly` and avatar pipeline.

**Files:**
- Modify: `pkg/federation/oidc/federation.go`
- Modify: `pkg/server/handle_federation.go`
- Test: `pkg/federation/oidc/federation_test.go` (or a new `steam_branch_test.go` in the package)

**Acceptance Criteria:**
- [ ] `Federator` gains injectable seams `steamVerify` + `steamSummary` (default to real `steam.Verify`/`FetchSummary` over a timeout `*http.Client`), mirroring the `avatarFetch` seam.
- [ ] `begin` with `idp.Protocol=="steam"` skips OIDC client/PKCE/nonce, stashes a minimal `FedState`, and returns `steam.BuildAuthURL(realm, returnTo)` where `returnTo` = `{origin}/api/prohibitorum/auth/federation/{slug}/callback?state=<token>`.
- [ ] `HandleCallback` (new final arg `callbackParams url.Values`) with a steam row: verifies, decrypts the API key (existing `DecryptClientSecret`), fetches the summary, builds `*Tokens{Subject:steamID, Issuer:steam.Issuer, EmailVerified:false, AMR:["steam"], Raw:{preferred_username, name, picture}}`, dispatches to `Resolve`/`applyInviteOnly`, and calls `kickoffAvatarInherit(nil, idp, tokens, accountID)`.
- [ ] `handle_federation.go` passes `r.URL.Query()` to `HandleCallback`; OIDC path unchanged.
- [ ] `go test ./pkg/federation/oidc/ ./pkg/server/` build + pass; `go build -tags nodynamic ./...` clean.

**Verify:** `go build -tags nodynamic ./... && go test ./pkg/federation/oidc/`

**Steps:**

- [ ] **Step 1: Add the seams to the `Federator` struct + `NewFederator` (`federation.go`)**

Add imports `"net/http"`, `"net/url"`, `"time"` (if not present), and `"prohibitorum/pkg/federation/steam"`. Add fields near `avatarFetch` (around line 198):

```go
	// steamHTTP is the client for Steam OpenID 2.0 check_authentication + Web API
	// calls (fixed public hosts; a timeout is the main control).
	steamHTTP *http.Client
	// steamVerify/steamSummary are test seams (mirroring avatarFetch), defaulting to
	// the real steam package functions bound to steamHTTP.
	steamVerify  func(ctx context.Context, params url.Values, expectedReturnTo string) (string, error)
	steamSummary func(ctx context.Context, apiKey, steamID string) (steam.Summary, error)
```

In `NewFederator`, after constructing `f := &Federator{...}` (refactor the direct return into a variable if needed), set defaults:

```go
	f.steamHTTP = &http.Client{Timeout: 10 * time.Second}
	f.steamVerify = func(ctx context.Context, params url.Values, expectedReturnTo string) (string, error) {
		return steam.Verify(ctx, f.steamHTTP, params, expectedReturnTo)
	}
	f.steamSummary = func(ctx context.Context, apiKey, steamID string) (steam.Summary, error) {
		return steam.FetchSummary(ctx, f.steamHTTP, apiKey, steamID)
	}
```

- [ ] **Step 2: Add a Steam branch in `begin` (`federation.go`, after the invite_only pre-auth gate, before `buildClient` at ~line 313)**

Read the existing `begin` (lines 288–382). Insert, right after the `ModeInviteOnly` pre-auth gate and before `flow := flowLogin`:

```go
	if idp.Protocol == "steam" {
		return f.beginSteam(ctx, &idp, returnTo, linkingAccountID, enrollmentToken)
	}
```

Then add a new method (mirrors `begin`'s state minting, minus OIDC client/PKCE/nonce):

```go
// beginSteam mints the KV state + anti-forgery cookie for a Steam OpenID 2.0 flow
// and returns the checkid_setup redirect. No discovery/PKCE/nonce (OpenID 2.0 has
// none); the state token is carried in the return_to query and echoed by Steam.
func (f *Federator) beginSteam(ctx context.Context, idp *db.UpstreamIdp, returnTo string, linkingAccountID *int32, enrollmentToken string) (*LoginRequest, error) {
	stateToken, err := randB64URL(32)
	if err != nil {
		return nil, fmt.Errorf("federation/steam: state token: %w", err)
	}
	var antiForgery, browserBinding string
	if linkingAccountID == nil {
		antiForgery, err = randB64URL(32)
		if err != nil {
			return nil, fmt.Errorf("federation/steam: anti-forgery token: %w", err)
		}
		browserBinding = hashAntiForgery(antiForgery)
	}
	state := FedState{
		IDPID:            idp.ID,
		IDPSlug:          idp.Slug,
		ReturnTo:         returnTo,
		LinkingAccountID: linkingAccountID,
		EnrollmentToken:  enrollmentToken,
		BrowserBinding:   browserBinding,
	}
	blob, err := state.Encode()
	if err != nil {
		return nil, err
	}
	key := LoginKey(stateToken)
	if linkingAccountID != nil {
		key = LinkKey(stateToken)
	}
	if err := f.kvStore.SetEx(ctx, key, blob, f.cfg.StateTTL); err != nil {
		return nil, fmt.Errorf("federation/steam: stash state: %w", err)
	}
	authorizeURL := steam.BuildAuthURL(f.publicOrigin, f.steamCallbackURL(idp.Slug, stateToken))
	return &LoginRequest{AuthorizeURL: authorizeURL, StateKey: stateToken, AntiForgeryToken: antiForgery}, nil
}

// steamCallbackURL is the openid.return_to we hand Steam: our callback path with the
// state token, absolute under publicOrigin (Steam requires return_to under realm).
func (f *Federator) steamCallbackURL(slug, stateToken string) string {
	return f.publicOrigin + "/api/prohibitorum/auth/federation/" + url.PathEscape(slug) + "/callback?state=" + url.QueryEscape(stateToken)
}
```

- [ ] **Step 3: Change `HandleCallback` signature + add the Steam branch (`federation.go` ~line 393)**

Change the signature to accept the raw callback params:

```go
func (f *Federator) HandleCallback(ctx context.Context, stateToken, code, issParam, browserToken string, callbackParams url.Values) (*CallbackResult, error) {
```

The state `Pop`, `browserBindingOK`, `issParam` check, and `GetUpstreamIDPBySlug` all run first for both protocols (unchanged). Then, replace the OIDC-specific block (`buildClient` → token_endpoint drift → `client.Exchange`) with a protocol branch that yields `tokens *Tokens`:

```go
	var tokens *Tokens
	if idp.Protocol == "steam" {
		t, terr := f.steamTokens(ctx, &idp, callbackParams, f.steamCallbackURL(idp.Slug, stateToken))
		if terr != nil {
			f.failNoAccount(ctx, state.IDPSlug, "steam_verify_failed", map[string]any{"err": terr.Error()})
			return nil, authn.ErrFederationStateInvalid()
		}
		tokens = t
	} else {
		client, err := f.buildClient(ctx, &idp, flowLogin)
		if err != nil {
			return nil, err
		}
		if client.TokenEndpoint() != state.ExpectedTokenEndpoint {
			f.failNoAccount(ctx, state.IDPSlug, "token_endpoint_drift", map[string]any{"expected": state.ExpectedTokenEndpoint, "got": client.TokenEndpoint()})
			return nil, authn.ErrFederationStateInvalid()
		}
		t, err := client.Exchange(ctx, code, state.CodeVerifier, state.ExpectedIss, state.Nonce)
		if err != nil {
			f.failNoAccount(ctx, state.IDPSlug, "code_exchange_failed", map[string]any{"err": err.Error()})
			return nil, authn.ErrFederationStateInvalid()
		}
		tokens = t
		defer func() { /* preserve any existing post-exchange logic if present */ }()
	}
```

Keep the existing dispatch below UNCHANGED (`applyInviteOnly`/`Resolve`, `GetAccountByID`, disabled check) — it already reads `tokens`. Change the avatar kickoff so it works for both: for the OIDC path keep `client`; for Steam pass `nil`. The simplest uniform change — build a `var avatarClient *Client` set to the OIDC `client` in the else-branch and left nil for steam, then:

```go
	f.kickoffAvatarInherit(avatarClient, idp, tokens, outcome.AccountID)
```

(Declare `var avatarClient *Client` before the branch; assign `avatarClient = client` inside the OIDC else-branch. `runAvatarInherit` already guards `if pic == "" && client != nil`, so a nil client with `tokens.Raw["picture"]` set is safe.)

Add the `steamTokens` helper:

```go
// steamTokens verifies the Steam OpenID 2.0 callback, enriches via the Web API, and
// builds a *Tokens that the shared Resolve/modes path consumes. The claim keys match
// the schema defaults (preferred_username/name/picture) so no per-protocol claim
// handling is needed downstream.
func (f *Federator) steamTokens(ctx context.Context, idp *db.UpstreamIdp, params url.Values, expectedReturnTo string) (*Tokens, error) {
	steamID, err := f.steamVerify(ctx, params, expectedReturnTo)
	if err != nil {
		return nil, err
	}
	apiKey, err := f.decryptSecret(idp)
	if err != nil {
		return nil, err
	}
	sum, err := f.steamSummary(ctx, string(apiKey), steamID)
	if err != nil {
		return nil, err
	}
	return &Tokens{
		Subject:       steamID,
		Issuer:        steam.Issuer,
		EmailVerified: false,
		AMR:           []string{"steam"},
		Raw: map[string]any{
			"preferred_username": "steam_" + steamID,
			"name":               sum.PersonaName,
			"picture":            sum.AvatarURL,
		},
	}, nil
}
```

`decryptSecret` — check whether the package already has a decrypt helper on the Federator (the OIDC path decrypts the client_secret in `buildClient`; find it and reuse). If none is directly reusable, add:

```go
// decryptSecret decrypts the row's secret slot (OIDC client secret OR Steam API key)
// using the versioned DEK + the row's key_version.
func (f *Federator) decryptSecret(idp *db.UpstreamIdp) ([]byte, error) {
	dek, ok := f.deks[int(idp.KeyVersion)]
	if !ok {
		return nil, fmt.Errorf("federation: no DEK for key_version %d", idp.KeyVersion)
	}
	return DecryptClientSecret(dek, idp.ClientSecretEnc, idp.SecretNonce, idp.ID, idp.KeyVersion)
}
```

(Read `buildClient` first — if it already has this exact decrypt, extract/reuse it rather than duplicating, per the reuse rule.)

- [ ] **Step 4: Update the callback HTTP handler (`handle_federation.go` ~line 91)**

Find the `handleFederationCallbackHTTP` call to `s.federator.HandleCallback(ctx, state, code, iss, browserToken)` and add the query:

```go
	result, err := s.federator.HandleCallback(r.Context(), state, code, iss, browserToken, r.URL.Query())
```

- [ ] **Step 5: Add a Federator steam-branch unit test**

In the `oidc` package test file, add a test that constructs a `Federator` with stubbed `steamVerify`/`steamSummary` (via a small exported test seam if needed — mirror how `avatarFetch` is overridden in `export_test.go`; add `SetSteamSeams` there) + a fake `FederatorQueries` returning a `protocol='steam'` idp, drives `HandleCallback` with `openid.mode=id_res` params, and asserts the resulting account has `username=steam_<id>`, display=persona, and that `Resolve` created it. Reuse the existing federation_test.go fakes for the queries. Keep it focused on the branch + tuple mapping.

- [ ] **Step 6: Build + test + commit**

Run: `go build -tags nodynamic ./... && go test ./pkg/federation/oidc/ ./pkg/server/ 2>&1 | tail -20`
(`pkg/server` is flaky under parallel shared-DB runs — re-run flakes in isolation.)
```bash
git add pkg/federation/oidc/ pkg/server/handle_federation.go
git commit -m "feat(federation): Steam branch in begin + HandleCallback (login/invite)"
```

---

### Task 4: `LinkCallback` Steam branch (self-service linking)

**Goal:** Let an authenticated user link a Steam identity to their existing account via the self-service `/me/identities` flow.

**Files:**
- Modify: `pkg/federation/oidc/federation.go` (`LinkCallback`)
- Modify: `pkg/server/handle_me_identities.go` (pass query to `LinkCallback`)
- Test: `pkg/federation/oidc/federation_test.go`

**Acceptance Criteria:**
- [ ] `LinkCallback` (new final arg `callbackParams url.Values`) branches on `idp.Protocol`: steam → `steamTokens` (reused from Task 3); oidc → existing exchange.
- [ ] The link-callback HTTP handler passes `r.URL.Query()`.
- [ ] `go build -tags nodynamic ./... && go test ./pkg/federation/oidc/` pass.

**Verify:** `go build -tags nodynamic ./... && go test ./pkg/federation/oidc/`

**Steps:**

- [ ] **Step 1: Read `LinkCallback` (`federation.go` ~line 510–652)** and mirror the Task-3 branch: after state Pop + binding + `GetUpstreamIDPBySlug`, replace the `buildClient`+`Exchange` with the same `if idp.Protocol == "steam" { tokens, err = f.steamTokens(ctx, &idp, callbackParams, f.steamCallbackURL(idp.Slug, stateToken)) } else { ... existing ... }`. The rest of `LinkCallback` (the `InsertAccountIdentity` + `ConfirmAccountIdentity` + `require_verified_email` gate) is unchanged. NOTE: the link-flow `require_verified_email` gate at ~line 579 must be skipped for steam (steam has no email); guard it: `if idp.Protocol != "steam" && idp.RequireVerifiedEmail && !tokens.EmailVerified { ... }` (steam rows are created with require_verified_email=false anyway, so this is belt-and-suspenders — but add it to be safe and explicit).

- [ ] **Step 2: Change `LinkCallback` signature** to add `callbackParams url.Values` (final arg), matching `HandleCallback`.

- [ ] **Step 3: Update the HTTP handler** in `handle_me_identities.go` (`handleMeIdentitiesLinkCallbackHTTP`, ~line 277) to pass `r.URL.Query()` to `LinkCallback`.

- [ ] **Step 4: Add a link steam-branch test** mirroring Task 3's, asserting an `account_identity` row is inserted + confirmed for the existing account.

- [ ] **Step 5: Build + test + commit**

Run: `go build -tags nodynamic ./... && go test ./pkg/federation/oidc/`
```bash
git add pkg/federation/oidc/federation.go pkg/server/handle_me_identities.go
git commit -m "feat(federation): Steam branch in LinkCallback (self-service link)"
```

---

### Task 5: Admin backend — Steam CRUD variant

**Goal:** Create/read a Steam provider through the admin API: `protocol` + `apiKey` on the create body, protocol-branched validation, empty sentinels for OIDC columns, `require_verified_email=false`, and `protocol` surfaced in the views.

**Files:**
- Modify: `pkg/server/handle_admin_upstream_idps.go`
- Modify: `pkg/contract/auth.go` (`IdentityProviderView` + `FederationProvider` gain `Protocol`)
- Modify: `pkg/server/handle_federation.go` (`handleListFederationProvidersHTTP` sets `Protocol`)
- Test: `pkg/server/handle_admin_upstream_idps_test.go`

**Acceptance Criteria:**
- [ ] `createIdentityProviderBody` gains `Protocol string` + `ApiKey string`.
- [ ] `handleCreateIdentityProviderHTTP`: `protocol=="steam"` → require `apiKey` (not `clientSecret`/`issuerUrl`/`clientId`); insert with `IssuerUrl:""`, `ClientID:""`, `Scopes:[]string{}`, `RequireVerifiedEmail:false`, `Protocol:"steam"`, claim columns defaulted; seal `apiKey` into the secret slot via the existing two-step. `protocol=="oidc"`/empty → existing behavior (validate issuer/client/secret).
- [ ] `identityProviderView` + both contract views include `Protocol`.
- [ ] Steam create does NOT call `validateUpstreamIssuer` (no issuer).
- [ ] `go test ./pkg/server/ -run IdentityProvider` passes.

**Verify:** `go test ./pkg/server/ -run IdentityProvider -v`

**Steps:**

- [ ] **Step 1: Extend `createIdentityProviderBody`** (add after `RequireVerifiedEmail`):

```go
	Protocol string `json:"protocol"`
	ApiKey   string `json:"apiKey"`
```

- [ ] **Step 2: Branch `handleCreateIdentityProviderHTTP`.** After decoding the body, normalize + branch. Replace the current uniform validation with:

```go
	protocol := body.Protocol
	if protocol == "" {
		protocol = "oidc"
	}
	if body.Slug == "" || body.DisplayName == "" || body.Mode == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	var issuerURL, clientID, secretPlaintext string
	var scopes []string
	requireVerifiedEmail := body.RequireVerifiedEmail
	switch protocol {
	case "steam":
		if body.ApiKey == "" {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
		secretPlaintext = body.ApiKey
		issuerURL, clientID = "", ""
		scopes = []string{}
		requireVerifiedEmail = false // Steam has no email
	case "oidc":
		if body.IssuerUrl == "" || body.ClientID == "" || body.ClientSecret == "" {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
		if err := s.validateUpstreamIssuer(body.IssuerUrl); err != nil {
			writeAuthErr(w, err)
			return
		}
		issuerURL, clientID, secretPlaintext = body.IssuerUrl, body.ClientID, body.ClientSecret
		scopes = body.Scopes
		if len(scopes) == 0 {
			scopes = s.defaultFederationScopes()
		}
	default:
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
```

Then in the `InsertUpstreamIDP` params use `IssuerUrl: issuerURL, ClientID: clientID, Scopes: scopes, RequireVerifiedEmail: requireVerifiedEmail, Protocol: protocol` (claim columns keep the existing `preferred_username`/`name`/`email`/`picture` defaulting), and seal `secretPlaintext` (instead of `body.ClientSecret`) in the `EncryptClientSecret` step. Keep the rest of the two-step tx identical.

- [ ] **Step 3: Add `Protocol` to `contract.IdentityProviderView` and `contract.FederationProvider`** (`pkg/contract/auth.go`), and set it in `identityProviderView` (`Protocol: r.Protocol`) and in `handleListFederationProvidersHTTP`'s `contract.FederationProvider{...}` (`Protocol: idp.Protocol`).

- [ ] **Step 4: Update handler tests** (`handle_admin_upstream_idps_test.go`): add a case creating a `protocol:"steam"` provider with an `apiKey` (assert 201, row has `Protocol=="steam"`, `IssuerUrl==""`, `RequireVerifiedEmail==false`, and the sealed secret decrypts to the API key), and assert a steam create with an empty `apiKey` → 400. Confirm the existing OIDC create test still passes (protocol defaults to oidc). Mirror the file's existing Server/DB test harness.

- [ ] **Step 5: Build + test + commit**

Run: `go build -tags nodynamic ./... && go test ./pkg/server/ -run IdentityProvider -v`
```bash
git add pkg/server/handle_admin_upstream_idps.go pkg/contract/auth.go pkg/server/handle_federation.go pkg/server/handle_admin_upstream_idps_test.go
git commit -m "feat(server): Steam provider admin create + protocol in views"
```

---

### Task 6: Frontend — admin Steam form + Steam login button

**Goal:** Admin can create/manage a Steam provider (protocol selector, API-key field, OIDC fields hidden), and the login page renders the bespoke black Steam button.

**Files:**
- Create: `dashboard/src/assets/steam-logo.svg`, `dashboard/src/components/custom/SteamButton.vue`
- Modify: `dashboard/src/pages/admin/AdminUpstreamIdpsView.vue`, `AdminUpstreamIdpDetailView.vue`, `dashboard/src/components/custom/FederationButtons.vue`
- Modify: `dashboard/src/locales/en.ts`, `zh.ts`
- Test: `dashboard/src/components/custom/FederationButtons.test.ts` (or the admin view test)

**Acceptance Criteria:**
- [ ] `steam-logo.svg` is the provided mark with fill = `currentColor` (recolored from grey).
- [ ] Admin create form has a protocol selector (OIDC / Steam); Steam shows an API-key field + hides issuer/client-id/scopes/claims/verified-email; `create()` sends `protocol` + `apiKey`.
- [ ] Detail view shows `protocol` read-only; for Steam, the rotate-secret field is labeled for the API key and OIDC fields are hidden.
- [ ] `FederationButtons` renders `SteamButton` (black bg, white logo + text) for `p.protocol==='steam'`, generic outline for others.
- [ ] `vitest run` green; `vue-tsc -b` 0 errors.

**Verify:** `cd dashboard && npx vitest run && npx vue-tsc -b`

**Steps:**

- [ ] **Step 1: Vendor the SVG.** Copy `/mnt/e/Steam_Symbol_0.svg` → `dashboard/src/assets/steam-logo.svg`. Edit it so the paths use `fill="currentColor"`: replace the `<style>…{fill:#C5C3C0;}</style>` + `class="st0"` with `fill="currentColor"` on each `<path>` (remove the `<style>` block and the `class` attributes). Keep the `viewBox="0 0 88.3 88.5"`.

- [ ] **Step 2: Write `SteamButton.vue`** (native Button shape, black bg, white logo + text):

```vue
<script setup lang="ts">
/** SteamButton — Valve-brand "Sign in through Steam" button: our native button
 *  shape, black background, white Steam mark + white text. */
import { Button } from '@/components/ui/button'
import SteamLogo from '@/assets/steam-logo.svg'

defineProps<{ label: string }>()
defineEmits<{ (e: 'click'): void }>()
</script>

<template>
  <Button
    type="button"
    class="w-full justify-start gap-2 border-0 bg-black text-white hover:bg-black/90"
    @click="$emit('click')"
  >
    <img :src="SteamLogo" alt="" aria-hidden="true" class="size-5 text-white" />
    <span>{{ label }}</span>
  </Button>
</template>
```

Note: importing an `.svg` as a URL is Vite's default; the `currentColor` fill only follows `text-*` when the SVG is inlined, not via `<img>`. Since the vendored SVG is recolored to white directly is simpler — so in Step 1, set `fill="#ffffff"` (white) rather than `currentColor`, and drop the `text-white` on the img. (If an inline-SVG component is preferred later, switch to `currentColor`; white fill is the pragmatic, robust choice for `<img>`.)

- [ ] **Step 3: Wire `FederationButtons.vue`.** Add `protocol?: string` to the `FederationProvider` interface. In the template, branch:

```html
      <SteamButton
        v-for="p in providers.filter(x => x.protocol === 'steam')"
        :key="p.slug"
        :label="p.displayName"
        @click="startFederation(p.slug)"
      />
      <Button
        v-for="p in providers.filter(x => x.protocol !== 'steam')"
        :key="p.slug"
        type="button" variant="outline" class="w-full justify-start gap-2"
        @click="startFederation(p.slug)"
      >
        <AppIcon :src="p.iconUrl" :name="p.displayName" size="sm" />
        <span>{{ p.displayName }}</span>
      </Button>
```

Import `SteamButton`. (Keep the loading skeleton + heading as-is.)

- [ ] **Step 4: Admin create form (`AdminUpstreamIdpsView.vue`).** Add `const protocol = ref('oidc')` and `const apiKey = ref('')`. Add a protocol selector (mirror the `mode` `RadioCardGroup` idiom, or a `Select`) at the top of the create form. Wrap the OIDC-only fields (issuerUrl, clientId, clientSecret, scopes, claim inputs, requireVerifiedEmail) in `v-if="protocol === 'oidc'"`, and add an API-key `Input` (type=password) under `v-if="protocol === 'steam'"`. In `create()`, send `protocol: protocol.value` and, when steam, `apiKey: apiKey.value` (omit the OIDC fields or let the backend ignore them). Read the file first and mirror its existing section/label structure.

- [ ] **Step 5: Admin detail view (`AdminUpstreamIdpDetailView.vue`).** Read `idp.value.protocol`; render it read-only (like slug). For `protocol === 'steam'`, hide the OIDC config fields and relabel the rotate-secret control as "Rotate API key" (the rotate endpoint is protocol-agnostic — it seals whatever `clientSecret` string it's given). The `save()` PUT for steam should keep sending the existing config fields the backend tolerates (display_name/mode/disabled/allowed_domains); issuer/client/scopes are ignored for steam rows server-side. Keep it minimal — mirror existing structure.

- [ ] **Step 6: i18n.** Add `en.ts` + `zh.ts` keys for: protocol selector label + the two options, the API-key field label/hint, and the Steam button label (e.g. `login.signInWithSteam` → "Sign in through Steam"). Verify straight ASCII quotes in en.ts (`grep -n "steam\|Steam" dashboard/src/locales/en.ts`).

- [ ] **Step 7: Tests.** Add a vitest case asserting `FederationButtons` renders a `SteamButton` (find by the steam label / the black-bg class / a `data-test`) when a provider has `protocol: 'steam'`, and the generic button otherwise. Follow the file's existing mock idiom for `api.get('/api/prohibitorum/auth/federation')`.

- [ ] **Step 8: Verify + commit**

Run: `cd dashboard && npx vitest run && npx vue-tsc -b`
```bash
git add dashboard/src/assets/steam-logo.svg dashboard/src/components/custom/SteamButton.vue dashboard/src/components/custom/FederationButtons.vue dashboard/src/pages/admin/AdminUpstreamIdpsView.vue dashboard/src/pages/admin/AdminUpstreamIdpDetailView.vue dashboard/src/locales/en.ts dashboard/src/locales/zh.ts dashboard/src/components/custom/FederationButtons.test.ts
git commit -m "feat(dashboard): Steam provider admin form + Sign in through Steam button"
```

---

### Task 7: Smoke (mock Steam) + full gate + dist

**Goal:** Prove the Steam login arc end-to-end against a mock Steam server, run the full gate, rebuild + commit dist.

**Files:**
- Modify: `cmd/smoke/main.go`
- Modify: `pkg/webui/dist/**` (rebuilt)

**Acceptance Criteria:**
- [ ] Smoke: admin creates a `protocol=steam` provider (auto_provision) with an apiKey; a mock Steam OP (a `httptest`-style server started inside the smoke, OR the smoke drives the begin URL and posts a crafted `id_res` callback) yields a login; assert a session is issued (or `/welcome` confirm arc completes) and the account has `username=steam_<id>` + persona display name.
- [ ] Full gate green: `go build -tags nodynamic ./...`, `go vet ./...`, `go test ./...`, vitest, vue-tsc, check-contrast, live `mise run ci:smoke` `SMOKE_EXIT=0`.
- [ ] `pkg/webui/dist` rebuilt + committed.

**Verify:** full gate below → all green, `SMOKE_EXIT=0`

**Steps:**

- [ ] **Step 1: Design the smoke's Steam mock.** The Steam endpoints are package vars in `pkg/federation/steam` overridable only from within that package's tests — the smoke runs against the real server binary, so it cannot override them in-process. Two viable approaches; pick the one that fits the smoke harness after reading `cmd/smoke/main.go`:
  - (A) **Env-configurable endpoints:** add `PROHIBITORUM_FEDERATION_STEAM_LOGIN_ENDPOINT` / `..._SUMMARY_ENDPOINT` overrides read by `steam` at init (a tiny `func init()` reading env, or wire through `configx.FederationConfig`), then the `ci:smoke` task starts a mock Steam HTTP server and points the server binary at it. This is the robust path and also useful for local dev.
  - (B) If (A) is too invasive for this cycle, cover the Steam login arc with a Go integration test in `pkg/server` (real router + a stubbed federator steam seam) and have the smoke only exercise the **admin create** of a steam provider (PUT/GET) — documenting that the full login arc is covered by unit/integration tests, not the live smoke. Prefer (A); fall back to (B) only if wiring a mock into `ci:smoke` proves impractical.

  Whichever is chosen, `log()` clearly what is and isn't covered (no silent gaps).

- [ ] **Step 2: Implement the chosen smoke coverage**, mirroring the existing admin + federation smoke blocks (sudo-primed admin create; the maintenance/oidc blocks are the pattern). Respect the `/me/sudo/begin` 10/min ceiling — prime sudo once.

- [ ] **Step 3: Rebuild dist.** `cd dashboard && npm run build`.

- [ ] **Step 4: Full gate.**
```bash
go build -tags nodynamic ./... && go vet ./... && go test ./... 2>&1 | tail -20
cd dashboard && npx vitest run && npx vue-tsc -b && node scripts/check-contrast.mjs && cd ..
```
Then `mise run ci:smoke` (or the chosen variant) → `SMOKE_EXIT=0`. (Long timeout; do not kill mid-run — it self-cleans via trap.)

- [ ] **Step 5: Commit.**
```bash
git add cmd/smoke/main.go pkg/webui/dist $(git ls-files -m pkg/federation/steam pkg/configx 2>/dev/null)
git commit -m "test(smoke): Steam login arc; rebuild dashboard dist"
```

---

## Self-Review

**Spec coverage:**
- Protocol discriminator + schema (empty-sentinel realization) → Task 1. ✓
- `pkg/federation/steam` (BuildAuthURL/Verify/FetchSummary, anchored claimed_id, check_authentication) → Task 2. ✓
- Federator branch (begin + HandleCallback, `*Tokens` tuple, avatar via `Raw["picture"]` + nil client) → Task 3. ✓
- Self-service link (LinkCallback branch) → Task 4. ✓
- Admin backend (protocol + apiKey, empty sentinels, require_verified_email=false, views) → Task 5. ✓
- Frontend admin form + Steam button (recolored SVG) + i18n → Task 6. ✓
- Testing (steam unit, federator branch, admin handler, vitest) + smoke + gate + dist → Tasks 2,3,5,6,7. ✓
- Security (anchored regex + check_authentication; reuse KV state + browser binding; secret encryption) → Tasks 2,3,5. ✓
- Out-of-scope (partner OAuth, ownership checks, general OpenID 2.0 lib) → not planned. ✓

**Placeholder scan:** New code (steam pkg, migration, SQL, SteamButton) is complete. Integration edits give exact branch code + anchors; the one genuinely-open item (Task 7 smoke approach A vs B) is a scoped decision with both paths specified and a stated preference — an execution judgment, not a placeholder.

**Type consistency:** `steam.Issuer`, `steam.BuildAuthURL`, `steam.Verify(ctx, *http.Client, url.Values, string)`, `steam.FetchSummary(ctx, *http.Client, apiKey, steamID) (steam.Summary, error)`, `steam.Summary{PersonaName, AvatarURL}` are consistent across Tasks 2/3. The Federator seams `steamVerify(ctx, url.Values, string)` / `steamSummary(ctx, apiKey, steamID)` and `steamTokens`/`steamCallbackURL`/`decryptSecret` are used consistently in Tasks 3/4. `Tokens.Raw` keys (`preferred_username`/`name`/`picture`) match the schema-default claim columns. `Protocol` field name matches across sqlc (`UpstreamIdp.Protocol`), contract views, and the frontend (`protocol`).
