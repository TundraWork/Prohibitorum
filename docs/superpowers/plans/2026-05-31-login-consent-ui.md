# Login + Consent UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship Prohibitorum's first browser frontend — `/login`, OIDC `/consent`, logout, and error pages as a same-origin embedded Vue 3 SPA — plus the net-new OIDC consent backend, so interactive OIDC/SAML flows complete end-to-end.

**Architecture:** Backend-first. Add the consent store + ticket + `authorize` consent step + consent/federation-list APIs (Go, sqlc, goose), validate them with `cmd/smoke`, then build the Vue 3 + Vite + Nuxt UI SPA, embed its build into the Go binary via `go:embed`, and serve it same-origin via the chi `NotFound` handler. The SPA calls the existing JSON auth APIs; the backend owns every redirect/security decision.

**Tech Stack:** Go (chi, pgx/v5, sqlc, goose, huma), Vue 3 + Vite + TypeScript, Nuxt UI v4 standalone (Reka UI + Tailwind v4), vue-i18n (zh-CN + en), Pinia, Vue Router, `@simplewebauthn/browser`, Vitest.

**Spec:** `docs/superpowers/specs/2026-05-31-login-consent-ui-design.md` (D1–D14).

---

## File Structure

| File | Responsibility |
|---|---|
| `db/migrations/007_oidc_consent.sql` | `oidc_consent` table (goose) |
| `db/queries/oidc_consent.sql` | `GetConsent` / `UpsertConsent` / `DeleteConsent` |
| `sqlc.yaml` | add `oidc_consent.account_id → int32` override |
| `pkg/authn/consent.go` (+`_test.go`) | consent KV ticket (mirror `reauth.go`) |
| `pkg/protocol/oidc/authorize.go` | step-5 consent rewrite |
| `pkg/protocol/oidc/authorize_consent_test.go` | consent decision unit tests |
| `pkg/server/handle_consent.go` | GET context + POST decision |
| `pkg/server/handle_federation.go` | add list-providers handler |
| `pkg/contract/*.go` | consent + provider DTOs |
| `pkg/server/server.go` | register consent/federation-list routes + SPA fallback |
| `pkg/webui/webui.go` | `go:embed` dist + SPA fallback handler + CSP |
| `dashboard/**` | Vue SPA (scaffold, pages, components, i18n, lib) |
| `cmd/smoke/main.go` | consent + federation-list e2e steps |
| `mise.toml` | `node` tool + `frontend:build` task |

**Conventions (from prior chunks — these bite):** master branch, direct commits. `mise exec --` prefix; `mise exec sqlc -- sqlc generate`. Trust `go build ./...` exit 0 + `go vet`, NOT gopls `<new-diagnostics>` (false "undefined"/sqlc errors mid-edit). NEVER `pkill -f prohibitorum` bare (kills the PG at `/tmp/prohibitorum-pg`); run smoke detached via `setsid bash /tmp/run_v06.sh`, poll `/tmp/v06.result` for `DONE`/`SMOKE_EXIT=0`. The `mise WARN ... goose` line is harmless.

---

## Task 1: Consent persistence (migration 007 + sqlc queries)

**Goal:** Add the `oidc_consent` stored-grants table and its sqlc queries; regenerate `pkg/db`.

**Files:**
- Create: `db/migrations/007_oidc_consent.sql`
- Create: `db/queries/oidc_consent.sql`
- Modify: `sqlc.yaml` (overrides list)

**Acceptance Criteria:**
- [ ] `oidc_consent (account_id, client_id, granted_scopes text[], created_at, updated_at)` with PK `(account_id, client_id)` and FKs to `account(id)` + `oidc_client(client_id)`, both `ON DELETE CASCADE`.
- [ ] `pkg/db` regenerates with `GetConsent` (`:one`, returns `[]string`), `UpsertConsent` (`:exec`), `DeleteConsent` (`:exec`); `account_id` typed `int32`.
- [ ] `go build ./...` exit 0.

**Verify:** `mise exec sqlc -- sqlc generate && mise exec -- go build ./...` → exit 0; `rg -n 'func .*GetConsent|func .*UpsertConsent' pkg/db` shows the generated methods.

**Steps:**

- [ ] **Step 1: Migration** — create `db/migrations/007_oidc_consent.sql`:

```sql
-- +goose Up
CREATE TABLE oidc_consent (
    account_id     integer     NOT NULL REFERENCES account(id) ON DELETE CASCADE,
    client_id      text        NOT NULL REFERENCES oidc_client(client_id) ON DELETE CASCADE,
    granted_scopes text[]      NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, client_id)
);

-- +goose Down
DROP TABLE oidc_consent;
```

- [ ] **Step 2: Queries** — create `db/queries/oidc_consent.sql`:

```sql
-- name: GetConsent :one
SELECT granted_scopes FROM oidc_consent
WHERE account_id = $1 AND client_id = $2;

-- name: UpsertConsent :exec
INSERT INTO oidc_consent (account_id, client_id, granted_scopes, created_at, updated_at)
VALUES ($1, $2, $3, now(), now())
ON CONFLICT (account_id, client_id)
DO UPDATE SET granted_scopes = $3, updated_at = now();

-- name: DeleteConsent :exec
DELETE FROM oidc_consent WHERE account_id = $1 AND client_id = $2;
```

- [ ] **Step 3: sqlc override** — in `sqlc.yaml`, add to the `overrides:` list (alongside the existing `session.account_id` entry):

```yaml
          - column: "oidc_consent.account_id"
            go_type: "int32"
```

- [ ] **Step 4: Regenerate + build**

Run: `mise exec sqlc -- sqlc generate && mise exec -- go build ./...`
Expected: exit 0. (Ignore the `mise WARN ... goose` line. Trust the build exit code over any IDE diagnostics.)

- [ ] **Step 5: Commit**

```bash
git add db/migrations/007_oidc_consent.sql db/queries/oidc_consent.sql sqlc.yaml pkg/db
git commit -m "feat(oidc): add oidc_consent stored-grants table + queries

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Consent KV ticket helper

**Goal:** A single-use, account-bound consent ticket in KV (mirroring `pkg/authn/reauth.go`) carrying the pending request, with peek (GET context) and consume (POST decision) paths.

**Files:**
- Create: `pkg/authn/consent.go`
- Test: `pkg/authn/consent_test.go`

**Acceptance Criteria:**
- [ ] `DemandConsent` mints a nonce and stores a JSON `ConsentTicket{AccountID, ClientID, Scopes, RedirectURI, State}` under `oidc:consent:<nonce>` (10 min TTL).
- [ ] `PeekConsent` reads (no delete) and returns the ticket iff it belongs to the given account; `ConsumeConsent` atomically pops it with the same account check.
- [ ] Wrong-account, missing, and malformed tickets return `(nil, false, nil)`.
- [ ] `mise exec -- go test ./pkg/authn/...` passes.

**Verify:** `mise exec -- go test ./pkg/authn/ -run Consent -v` → PASS.

**Steps:**

- [ ] **Step 1: Write failing tests** — create `pkg/authn/consent_test.go`:

```go
package authn

import (
	"context"
	"testing"

	"prohibitorum/pkg/kv"
)

func TestConsentTicket_RoundTripPeekAndConsume(t *testing.T) {
	store := kv.NewMemoryStore()
	ctx := context.Background()
	tkt := ConsentTicket{AccountID: 7, ClientID: "rp1", Scopes: []string{"openid", "profile"}, RedirectURI: "https://rp/cb", State: "xyz"}

	nonce, err := DemandConsent(ctx, store, tkt)
	if err != nil {
		t.Fatal(err)
	}
	// Peek does not consume.
	got, ok, err := PeekConsent(ctx, store, nonce, 7)
	if err != nil || !ok {
		t.Fatalf("peek: ok=%v err=%v", ok, err)
	}
	if got.ClientID != "rp1" || len(got.Scopes) != 2 || got.State != "xyz" {
		t.Errorf("peek payload mismatch: %+v", got)
	}
	got2, ok, _ := PeekConsent(ctx, store, nonce, 7)
	if !ok || got2.ClientID != "rp1" {
		t.Error("second peek should still succeed (no consume)")
	}
	// Wrong account rejected.
	if _, ok, _ := PeekConsent(ctx, store, nonce, 99); ok {
		t.Error("peek with wrong account must fail")
	}
	// Consume pops single-use.
	c, ok, err := ConsumeConsent(ctx, store, nonce, 7)
	if err != nil || !ok || c.ClientID != "rp1" {
		t.Fatalf("consume: ok=%v err=%v c=%+v", ok, err, c)
	}
	if _, ok, _ := ConsumeConsent(ctx, store, nonce, 7); ok {
		t.Error("second consume must fail (single-use)")
	}
}

func TestConsentTicket_MissingAndMalformed(t *testing.T) {
	store := kv.NewMemoryStore()
	ctx := context.Background()
	if _, ok, _ := PeekConsent(ctx, store, "", 1); ok {
		t.Error("empty nonce must not peek")
	}
	if _, ok, _ := ConsumeConsent(ctx, store, "nope", 1); ok {
		t.Error("missing nonce must not consume")
	}
}
```

- [ ] **Step 2: Run → fail**

Run: `mise exec -- go test ./pkg/authn/ -run Consent`
Expected: FAIL (undefined `ConsentTicket`/`DemandConsent`/`PeekConsent`/`ConsumeConsent`).

- [ ] **Step 3: Implement** — create `pkg/authn/consent.go`:

```go
package authn

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"prohibitorum/pkg/kv"
)

// ConsentTicketTTL bounds how long a pending consent decision stays valid.
const ConsentTicketTTL = 10 * time.Minute

const consentKeyPrefix = "oidc:consent:"

// ConsentTicket is the server-minted record of a pending OIDC consent decision.
// It is stored in KV under a single-use nonce and carries everything the
// decision needs so the browser SPA never reconstructs flow state. RedirectURI
// + State let a deny produce a correct access_denied RP redirect.
type ConsentTicket struct {
	AccountID   int32    `json:"account_id"`
	ClientID    string   `json:"client_id"`
	Scopes      []string `json:"scopes"`
	RedirectURI string   `json:"redirect_uri"`
	State       string   `json:"state"`
}

// DemandConsent mints a single-use nonce and stores the ticket (10 min TTL).
func DemandConsent(ctx context.Context, store kv.Store, t ConsentTicket) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(buf)
	payload, err := json.Marshal(t)
	if err != nil {
		return "", err
	}
	if err := store.SetEx(ctx, consentKeyPrefix+nonce, string(payload), ConsentTicketTTL); err != nil {
		return "", err
	}
	return nonce, nil
}

// PeekConsent reads (without consuming) the ticket and returns it iff it belongs
// to accountID. Returns (nil,false,nil) for empty/missing/malformed/wrong-account.
func PeekConsent(ctx context.Context, store kv.Store, nonce string, accountID int32) (*ConsentTicket, bool, error) {
	if nonce == "" {
		return nil, false, nil
	}
	val, err := store.Get(ctx, consentKeyPrefix+nonce)
	if errors.Is(err, kv.ErrKeyNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return decodeConsent(val, accountID)
}

// ConsumeConsent atomically pops the ticket (single-use) and returns it iff it
// belongs to accountID.
func ConsumeConsent(ctx context.Context, store kv.Store, nonce string, accountID int32) (*ConsentTicket, bool, error) {
	if nonce == "" {
		return nil, false, nil
	}
	val, err := store.Pop(ctx, consentKeyPrefix+nonce)
	if errors.Is(err, kv.ErrKeyNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return decodeConsent(val, accountID)
}

func decodeConsent(val string, accountID int32) (*ConsentTicket, bool, error) {
	var t ConsentTicket
	if err := json.Unmarshal([]byte(val), &t); err != nil {
		return nil, false, nil // malformed → treat as absent
	}
	if t.AccountID != accountID {
		return nil, false, nil // bound to a different account
	}
	return &t, true, nil
}
```

- [ ] **Step 4: Run → pass**

Run: `mise exec -- go test ./pkg/authn/ -run Consent -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/authn/consent.go pkg/authn/consent_test.go
git commit -m "feat(authn): single-use account-bound OIDC consent ticket (KV)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: `authorize.go` consent step rewrite

**Goal:** Replace the `RequireConsent` stub with the real check: skip when a stored grant covers the requested scopes (and not `prompt=consent`), else `consent_required` for `prompt=none` or bounce to `/consent` with a ticket.

**Files:**
- Modify: `pkg/protocol/oidc/authorize.go` (the step-5 block at ~line 191–195)
- Test: `pkg/protocol/oidc/authorize_consent_test.go`

**Acceptance Criteria:**
- [ ] `RequireConsent` client + stored grant covering requested scopes → proceeds (no bounce).
- [ ] `RequireConsent` + missing/insufficient grant → 302 to `…/consent?ticket=<n>&return_to=<authorizeURL>`.
- [ ] `prompt=consent` forces the bounce even when a grant exists.
- [ ] `prompt=none` + consent needed → `redirectError(consent_required)` to the RP.
- [ ] `RequireConsent=false` → proceeds (trusted-client skip).
- [ ] `go build ./...` + `go vet` clean; unit tests pass.

**Verify:** `mise exec -- go test ./pkg/protocol/oidc/ -run Consent -v` → PASS; `mise exec -- go build ./... && mise exec -- go vet ./...` → exit 0.

**Steps:**

- [ ] **Step 1: Add the import** — ensure `pkg/protocol/oidc/authorize.go` imports `"github.com/jackc/pgx/v5"` and `"prohibitorum/pkg/db"` (db is already used elsewhere in the package; add pgx if absent). Confirm with `rg -n '"github.com/jackc/pgx/v5"' pkg/protocol/oidc/authorize.go` — add to the import block if missing.

- [ ] **Step 2: Replace the stub** — replace these lines in `HandleAuthorize`:

```go
	// (5) Consent is not yet implemented (v0.6). A client that requires it
	// cannot complete the flow yet.
	if client.RequireConsent {
		redirectError(w, r, redirectURI, errCodeConsentRequired, "user consent is required but not yet supported", state, p.cfg.OIDC.Issuer)
		return
	}
```

with:

```go
	// (5) Consent. Trusted clients (RequireConsent=false) skip entirely. Otherwise
	// a stored grant covering every requested scope satisfies consent — unless the
	// RP forced re-consent with prompt=consent. When consent is needed we mint a
	// single-use ticket and bounce to /consent (or, for prompt=none, error to RP).
	if client.RequireConsent {
		granted, gerr := p.queries.GetConsent(r.Context(), db.GetConsentParams{
			AccountID: sess.Data.AccountID,
			ClientID:  client.ClientID,
		})
		if gerr != nil && !errors.Is(gerr, pgx.ErrNoRows) {
			redirectError(w, r, redirectURI, errCodeServerError, "could not load consent", state, p.cfg.OIDC.Issuer)
			return
		}
		needConsent := slices.Contains(prompts, "consent")
		for _, s := range scopes {
			if !slices.Contains(granted, s) {
				needConsent = true
				break
			}
		}
		if needConsent {
			if wantNone {
				redirectError(w, r, redirectURI, errCodeConsentRequired, "user consent is required", state, p.cfg.OIDC.Issuer)
				return
			}
			nonce, derr := authn.DemandConsent(r.Context(), p.kv, authn.ConsentTicket{
				AccountID:   sess.Data.AccountID,
				ClientID:    client.ClientID,
				Scopes:      scopes,
				RedirectURI: redirectURI,
				State:       state,
			})
			if derr != nil {
				redirectError(w, r, redirectURI, errCodeServerError, "could not start consent", state, p.cfg.OIDC.Issuer)
				return
			}
			returnTo := p.cfg.OIDC.Issuer + r.URL.RequestURI()
			consentURL := p.cfg.OIDC.Issuer + "/consent?ticket=" + url.QueryEscape(nonce) +
				"&return_to=" + url.QueryEscape(returnTo)
			http.Redirect(w, r, consentURL, http.StatusFound)
			return
		}
	}
```

(`prompts`, `wantNone`, `scopes`, `state`, `sess` are all already defined earlier in `HandleAuthorize`. `granted` is `[]string` from `GetConsent`; on `pgx.ErrNoRows` it is nil → every scope counts as ungranted.)

- [ ] **Step 3: Tests** — create `pkg/protocol/oidc/authorize_consent_test.go`. Follow the existing test scaffolding in this package (look at how other `authorize` tests build a `Provider` with a fake `db.Querier` + `kv.NewMemoryStore()` and an authenticated request context). Cover, asserting on the `httptest.ResponseRecorder` `Location`/status:

```go
// Pseudocode contract for the four cases — implement against the package's
// existing test harness (fake Querier returning a controllable GetConsent +
// GetOIDCClient with RequireConsent=true, a session in ctx via authn.WithSession):
//
//  1. grant covers scopes, no prompt=consent  -> NOT a 302 to /consent (proceeds)
//  2. grant missing/insufficient              -> 302 Location starts with Issuer+"/consent?ticket="
//  3. prompt=consent, grant covers            -> still 302 to /consent
//  4. prompt=none, consent needed             -> 302 Location is the RP redirect_uri carrying error=consent_required
//  5. client.RequireConsent=false             -> proceeds (no /consent bounce)
```

Write real Go test functions (`TestAuthorize_Consent_*`) using the package's existing helpers; assert `strings.HasPrefix(rr.Header().Get("Location"), issuer+"/consent?ticket=")` for the bounce cases and `strings.Contains(loc, "error=consent_required")` for case 4. If the package lacks a reusable `Provider` test constructor, add a minimal one in the test file (fake `db.Querier` implementing only `GetOIDCClient`, `GetConsent`, `GetSession`).

- [ ] **Step 4: Run + build + vet**

Run: `mise exec -- go test ./pkg/protocol/oidc/ -run Consent -v && mise exec -- go build ./... && mise exec -- go vet ./...`
Expected: tests PASS, build/vet exit 0.

- [ ] **Step 5: Commit**

```bash
git add pkg/protocol/oidc/authorize.go pkg/protocol/oidc/authorize_consent_test.go
git commit -m "feat(oidc): real consent check in authorize (remembered grants + ticket bounce)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Consent app API (context + decision) + routes

**Goal:** `GET /api/prohibitorum/consent?ticket=` returns the client + scopes for the SPA; `POST /api/prohibitorum/consent` records the decision (approve → store grant, deny → access_denied) and returns the redirect target.

**Files:**
- Create: `pkg/server/handle_consent.go`
- Modify: `pkg/contract/auth.go` (consent DTOs)
- Modify: `pkg/server/server.go` (register two routes)

**Acceptance Criteria:**
- [ ] `GET …/consent?ticket=` (session required) → `200 {client:{clientId,displayName,logoUri,policyUri,tosUri}, account:{displayName}, scopes:[...]}`; invalid/expired/wrong-account ticket → 400 `invalid_consent_ticket`; no session → 401 `no_session`.
- [ ] `POST …/consent {ticket, decision}` (session required): approve → `UpsertConsent(account, client, union(granted, requested))` + `200 {redirect:<return_to>}`; deny → `200 {redirect:<redirect_uri>?error=access_denied&state=…}`. Both consume the ticket (single-use).
- [ ] `go build ./... && go vet ./...` exit 0.

**Verify:** `mise exec -- go build ./... && mise exec -- go vet ./...` → exit 0; `rg -n '/api/prohibitorum/consent' pkg/server/server.go` shows both routes.

**Steps:**

- [ ] **Step 1: DTOs** — append to `pkg/contract/auth.go`:

```go
// ConsentContext is GET /api/prohibitorum/consent — the data the consent UI
// needs to render. Scope *descriptions* are owned by the frontend i18n layer.
type ConsentContext struct {
	Client  ConsentClient `json:"client"`
	Account ConsentUser   `json:"account"`
	Scopes  []string      `json:"scopes"`
}

type ConsentClient struct {
	ClientID    string `json:"clientId"`
	DisplayName string `json:"displayName"`
	LogoURI     string `json:"logoUri,omitempty"`
	PolicyURI   string `json:"policyUri,omitempty"`
	TosURI      string `json:"tosUri,omitempty"`
}

type ConsentUser struct {
	DisplayName string `json:"displayName"`
}

// ConsentDecision is the POST body. Decision is "approve" or "deny".
type ConsentDecision struct {
	Ticket   string `json:"ticket"`
	Decision string `json:"decision"`
}

// ConsentResult tells the SPA where to navigate next.
type ConsentResult struct {
	Redirect string `json:"redirect"`
}
```

- [ ] **Step 2: Handlers** — create `pkg/server/handle_consent.go`. These are raw-chi handlers (like the auth handlers); `LoadSession` middleware has already attached the session, so they read it via `authn.SessionFromContext` and reject with `no_session` when absent (the `handle_me.go` raw handlers do exactly this). They use `s.queries`, `s.kvStore`, `s.config`, and `writeAuthErr` / JSON encoding:

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/url"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
)

// GET /api/prohibitorum/consent?ticket=
func (s *Server) handleConsentContextHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if sess == nil || sess.Data == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	ticket, ok, err := authn.PeekConsent(r.Context(), s.kvStore, r.URL.Query().Get("ticket"), sess.Data.AccountID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if !ok {
		writeAuthErr(w, authn.ErrInvalidConsentTicket())
		return
	}
	client, err := s.queries.GetOIDCClient(r.Context(), ticket.ClientID)
	if err != nil {
		writeAuthErr(w, authn.ErrInvalidConsentTicket())
		return
	}
	out := contract.ConsentContext{
		Client: contract.ConsentClient{
			ClientID:    client.ClientID,
			DisplayName: client.DisplayName,
			LogoURI:     textOrEmpty(client.LogoUri),
			PolicyURI:   textOrEmpty(client.PolicyUri),
			TosURI:      textOrEmpty(client.TosUri),
		},
		Account: contract.ConsentUser{DisplayName: sess.Account.DisplayName},
		Scopes:  ticket.Scopes,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// POST /api/prohibitorum/consent
func (s *Server) handleConsentDecisionHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if sess == nil || sess.Data == nil {
		writeAuthErr(w, authn.ErrNoSession())
		return
	}
	var in contract.ConsentDecision
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	ticket, ok, err := authn.ConsumeConsent(r.Context(), s.kvStore, in.Ticket, sess.Data.AccountID)
	if err != nil {
		writeAuthErr(w, err)
		return
	}
	if !ok {
		writeAuthErr(w, authn.ErrInvalidConsentTicket())
		return
	}

	var redirect string
	switch in.Decision {
	case "approve":
		granted, gerr := s.queries.GetConsent(r.Context(), db.GetConsentParams{
			AccountID: sess.Data.AccountID, ClientID: ticket.ClientID,
		})
		if gerr != nil && !errorsIsNoRows(gerr) {
			writeAuthErr(w, gerr)
			return
		}
		union := unionScopes(granted, ticket.Scopes)
		if uerr := s.queries.UpsertConsent(r.Context(), db.UpsertConsentParams{
			AccountID: sess.Data.AccountID, ClientID: ticket.ClientID, GrantedScopes: union,
		}); uerr != nil {
			writeAuthErr(w, uerr)
			return
		}
		redirect = r.URL.Query().Get("return_to") // SPA also posts it back; see below
		if redirect == "" {
			redirect = "" // fallback handled by SPA; deny/approve always carry return_to
		}
	case "deny":
		u, _ := url.Parse(ticket.RedirectURI)
		q := u.Query()
		q.Set("error", "access_denied")
		if ticket.State != "" {
			q.Set("state", ticket.State)
		}
		u.RawQuery = q.Encode()
		redirect = u.String()
	default:
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(contract.ConsentResult{Redirect: redirect})
}
```

NOTE on `return_to` for approve: the SPA holds the original `return_to` (the authorize URL) and includes it in the POST as a query param (`POST …/consent?return_to=…`). The handler echoes it. The handler must validate it is same-origin as the issuer before echoing — add this guard inside the `approve` case:

```go
		rt := r.URL.Query().Get("return_to")
		if !sameOriginAsIssuer(rt, s.config) {
			writeAuthErr(w, authn.ErrBadRequest())
			return
		}
		redirect = rt
```

(Replace the placeholder `redirect = r.URL.Query()...` block above with this guarded version.)

- [ ] **Step 3: Helpers** — add to `pkg/server/handle_consent.go` (or a shared helper file): `textOrEmpty(pgtype.Text) string`, `unionScopes(a, b []string) []string` (dedup preserving order), `errorsIsNoRows(error) bool` (wraps `errors.Is(err, pgx.ErrNoRows)`), and `sameOriginAsIssuer(raw string, cfg *configx.Config) bool` (parse `raw`; true iff scheme+host equal the parsed `cfg.OIDC.Issuer`). Add the needed imports (`github.com/jackc/pgx/v5/pgtype`, `github.com/jackc/pgx/v5`, `prohibitorum/pkg/db`, `prohibitorum/pkg/configx`). Concrete code:

```go
func textOrEmpty(t pgtype.Text) string {
	if t.Valid {
		return t.String
	}
	return ""
}

func unionScopes(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, s := range append(append([]string{}, a...), b...) {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func errorsIsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func sameOriginAsIssuer(raw string, cfg *configx.Config) bool {
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	iss, err := url.Parse(cfg.OIDC.Issuer)
	if err != nil {
		return false
	}
	return u.Scheme == iss.Scheme && u.Host == iss.Host
}
```

- [ ] **Step 4: Error constructor** — add `ErrInvalidConsentTicket` to `pkg/authn/errors.go` (zh message + code), following the file's `newErr` pattern:

```go
// ErrInvalidConsentTicket is returned when a consent ticket is missing, expired,
// already used, or belongs to another account.
func ErrInvalidConsentTicket() *AuthError {
	return newErr(http.StatusBadRequest, "invalid_consent_ticket", "授权请求已失效，请重新发起登录")
}
```

(Confirm `ErrBadRequest()` exists — `rg -n 'func ErrBadRequest' pkg/authn/errors.go`; the file shows a `bad_request` constructor at line ~99. Use whatever its exact name is; if it is `ErrBadRequest`, the calls above are correct.)

- [ ] **Step 5: Routes** — in `pkg/server/server.go` `registerOperations`, alongside the other `registerOpHTTP` auth routes, add:

```go
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/consent", sessionReq, s.handleConsentContextHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/consent", sessionReq, s.handleConsentDecisionHTTP)
```

(Use `sessionReq` — already defined in `registerOperations` — so the operation is documented as session-gated; the handler also defensively checks the session.)

- [ ] **Step 6: Build + vet**

Run: `mise exec -- go build ./... && mise exec -- go vet ./...`
Expected: exit 0.

- [ ] **Step 7: Commit**

```bash
git add pkg/server/handle_consent.go pkg/contract/auth.go pkg/server/server.go pkg/authn/errors.go
git commit -m "feat(server): OIDC consent context + decision API (approve stores grant, deny -> access_denied)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Federation providers list endpoint

**Goal:** `GET /api/prohibitorum/auth/federation` (public) returns the enabled upstream IdPs (`slug` + `displayName`) for the login page's "sign in with" buttons.

**Files:**
- Modify: `pkg/server/handle_federation.go` (add handler)
- Modify: `pkg/contract/auth.go` (DTO)
- Modify: `pkg/server/server.go` (route)

**Acceptance Criteria:**
- [ ] `GET …/auth/federation` → `200 [{slug, displayName}]` from `ListUpstreamIDPs` (already filters disabled). Empty list when none.
- [ ] Public (no session required). `go build ./... && go vet ./...` exit 0.

**Verify:** `mise exec -- go build ./... && mise exec -- go vet ./...` → exit 0; `rg -n 'GET", "/api/prohibitorum/auth/federation"' pkg/server/server.go`.

**Steps:**

- [ ] **Step 1: DTO** — append to `pkg/contract/auth.go`:

```go
// FederationProvider is one entry in GET /api/prohibitorum/auth/federation.
type FederationProvider struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
}
```

- [ ] **Step 2: Handler** — add to `pkg/server/handle_federation.go`:

```go
// GET /api/prohibitorum/auth/federation — public list of enabled upstream IdPs
// for the login page's "sign in with" buttons. ListUpstreamIDPs already filters
// disabled rows and orders by display_name.
func (s *Server) handleListFederationProvidersHTTP(w http.ResponseWriter, r *http.Request) {
	idps, err := s.queries.ListUpstreamIDPs(r.Context())
	if err != nil {
		writeAuthErr(w, fmt.Errorf("list federation providers: %w", err))
		return
	}
	out := make([]contract.FederationProvider, 0, len(idps))
	for _, idp := range idps {
		out = append(out, contract.FederationProvider{Slug: idp.Slug, DisplayName: idp.DisplayName})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
```

(Confirm `handle_federation.go` already imports `fmt`, `encoding/json`, `net/http`, `prohibitorum/pkg/contract` — add any missing.)

- [ ] **Step 3: Route** — in `pkg/server/server.go`, near the existing federation routes (~line 270):

```go
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/auth/federation", publicReq, s.handleListFederationProvidersHTTP)
```

(Register BEFORE the `/auth/federation/{slug}/login` route is fine — chi distinguishes the static `/federation` from `/federation/{slug}/login` by path depth.)

- [ ] **Step 4: Build + vet** → `mise exec -- go build ./... && mise exec -- go vet ./...` → exit 0.

- [ ] **Step 5: Commit**

```bash
git add pkg/server/handle_federation.go pkg/contract/auth.go pkg/server/server.go
git commit -m "feat(server): public GET /auth/federation list of enabled upstream IdPs

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Smoke — consent + federation-list backend e2e

**Goal:** Extend `cmd/smoke` to prove the consent flow end-to-end (against live PG + dev server) and the federation-list endpoint, before any frontend exists.

**Files:**
- Modify: `cmd/smoke/main.go`

**Acceptance Criteria:**
- [ ] New smoke steps: register a `RequireConsent=true` OIDC client; authorize (with session) → assert 302 `Location` to `…/consent?ticket=`; `GET /api/prohibitorum/consent?ticket=` → assert client+scopes; `POST` approve → assert grant; re-authorize → assert it now issues a code (no consent bounce); a second authorize → still no bounce (remembered); `prompt=consent` → bounce again; deny → assert RP redirect carries `error=access_denied`.
- [ ] `GET /api/prohibitorum/auth/federation` → assert `200` + JSON array shape.
- [ ] Full smoke green, `SMOKE_EXIT=0`.

**Verify:** detached `setsid bash /tmp/run_v06.sh`, poll `/tmp/v06.result` for `DONE` → `SMOKE_EXIT=0` + final `✓ smoke OK` line.

**Steps:**

- [ ] **Step 1:** Add a helper to create a consent-requiring client. The smoke already shells out to `oidc-client create` (see `createPublicOIDCClient`); add `createConsentOIDCClient` passing whatever flag the CLI exposes for `RequireConsent` (grep the CLI: `rg -n 'require.consent\|RequireConsent\|require-consent' cmd/prohibitorum`). If the CLI lacks the flag, add `--require-consent` to the `oidc-client create` command (small CLI change in `cmd/prohibitorum`) as part of this task and note it in the commit.

- [ ] **Step 2:** Add the consent flow steps after the existing OIDC interactive section, reusing `authorizeWithSession`/`authorizeRaw` (which now use the jar). The authorize call for a consent client returns a 302 whose `Location` is `…/consent?ticket=…` — capture it with the `Location`-returning helper. Parse `ticket` + `return_to`. Then drive the consent API with the jar-backed client `c.hc` (session cookie auto-sent, Path=/):

```go
// GET consent context
ctxResp := c.getJSON("/api/prohibitorum/consent?ticket=" + url.QueryEscape(ticket))   // assert 200, client.displayName, scopes contains "openid"
// POST approve (carry return_to so the handler can echo it)
appResp := c.postJSON("/api/prohibitorum/consent?return_to=" + url.QueryEscape(returnTo), map[string]string{"ticket": ticket, "decision": "approve"}) // assert {redirect: returnTo}
// re-authorize -> now issues a code (parseAuthorizeRedirect succeeds)
loc, _ := authorizeWithSession(c, authzPathQuery)   // assert RP redirect with code
```

Use the smoke's existing JSON helpers (grep for how it does authenticated GET/POST against `/api/prohibitorum/*` — e.g. `c.cookies()` / the `c.hc` jar client). Add asserts mirroring the existing step style (`log.Fatalf` on mismatch, success `fmt.Printf("step N — …")`). Add the deny + `prompt=consent` + remembered-grant assertions similarly.

- [ ] **Step 3:** Add the federation-list assertion: `GET /api/prohibitorum/auth/federation` → 200, body decodes to `[]struct{Slug,DisplayName string}` (length ≥ 0; the smoke registers an upstream IdP earlier in the federation section — assert that slug appears).

- [ ] **Step 4: Build the smoke + run full suite**

Run: `mise exec -- go build ./cmd/smoke/...` (exit 0), then `setsid bash /tmp/run_v06.sh`, poll:
```bash
for i in $(seq 1 60); do sleep 10; if grep -q '^DONE$' /tmp/v06.result 2>/dev/null; then break; fi; done
grep -E 'SMOKE_EXIT|✓ smoke OK|FATAL|step [0-9]+ FAIL' /tmp/v06.result; tail -12 /tmp/v06.result
```
Expected: `SMOKE_EXIT=0`. If a step fails, read `/tmp/smoke-v06.log` + `/tmp/prohibitorum-v06.log`; report BLOCKED with evidence if unresolved.

- [ ] **Step 5: Commit**

```bash
git add cmd/smoke/main.go cmd/prohibitorum   # include CLI flag change if made
git commit -m "test(smoke): consent flow + federation-list backend e2e

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Frontend scaffold (Vite + Vue 3 + Nuxt UI + Tailwind + Router + i18n + Pinia)

**Goal:** A buildable Vue 3 SPA skeleton in `dashboard/` with Nuxt UI v4, Tailwind v4, Vue Router, vue-i18n (zh+en), Pinia, a typed API client, and a Vitest setup — producing `dashboard/dist/`.

**Files:**
- Create: `dashboard/package.json`, `dashboard/vite.config.ts`, `dashboard/tsconfig.json`, `dashboard/index.html`, `dashboard/src/main.ts`, `dashboard/src/App.vue`, `dashboard/src/router.ts`, `dashboard/src/i18n.ts`, `dashboard/src/locales/{zh,en}.ts`, `dashboard/src/assets/main.css`, `dashboard/src/lib/api.ts`, `dashboard/src/lib/returnTo.ts`, `dashboard/src/stores/session.ts`, `dashboard/src/components/LocaleSwitcher.vue`, `dashboard/src/pages/{LoginView,ConsentView,LogoutView,ErrorView}.vue` (stubs to be filled in later tasks), `dashboard/.gitignore`, `dashboard/vitest.config.ts`, `dashboard/src/lib/returnTo.test.ts`
- Modify: `mise.toml` (node tool + `frontend:build` task), root `.gitignore` (ignore `dashboard/dist`, `dashboard/node_modules`)

**Acceptance Criteria:**
- [ ] `cd dashboard && mise exec -- npm install && mise exec -- npm run build` produces `dashboard/dist/index.html` + assets.
- [ ] `mise exec -- npm run test` (Vitest) runs and the `returnTo` guard test passes.
- [ ] Nuxt UI components render (the App shell uses at least one `<UButton>`); Tailwind v4 active; i18n switches zh/en; routes resolve.
- [ ] `dashboard/dist` and `dashboard/node_modules` are gitignored.

**Verify:** `cd dashboard && mise exec -- npm install && mise exec -- npm run build && mise exec -- npm run test` → build emits `dist/`, Vitest PASS.

**Steps:**

- [ ] **Step 1: Node in mise** — add to `mise.toml` `[tools]`: `node = "20"`. Add a `[tasks.frontend-build]` task: `run = "cd dashboard && npm ci && npm run build"`.

- [ ] **Step 2: Scaffold** — from `dashboard/`, initialize a Vite + Vue + TS project and add deps. Exact `package.json`:

```json
{
  "name": "prohibitorum-dashboard",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vue-tsc -b && vite build",
    "preview": "vite preview",
    "test": "vitest run"
  },
  "dependencies": {
    "@nuxt/ui": "^4",
    "@simplewebauthn/browser": "^13",
    "pinia": "^2",
    "vue": "^3.5",
    "vue-i18n": "^10",
    "vue-router": "^4"
  },
  "devDependencies": {
    "@vitejs/plugin-vue": "^5",
    "@vue/test-utils": "^2",
    "jsdom": "^25",
    "tailwindcss": "^4",
    "typescript": "^5",
    "vite": "^6",
    "vitest": "^2",
    "vue-tsc": "^2"
  }
}
```

(Pin to the latest patch of each at install time. If `@vitejs/plugin-vue` name errors, it is `@vitejs/plugin-vue` — verify spelling.)

- [ ] **Step 3: Vite config** — `dashboard/vite.config.ts` (Nuxt UI Vite plugin + dev proxy per spec D3):

```ts
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import ui from '@nuxt/ui/vite'

export default defineConfig({
  plugins: [vue(), ui()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/oauth': 'http://localhost:8080',
      '/saml': 'http://localhost:8080',
      '/oidc': 'http://localhost:8080',
      '/.well-known': 'http://localhost:8080',
    },
  },
  build: { outDir: 'dist' },
})
```

- [ ] **Step 4: CSS + Nuxt UI** — `dashboard/src/assets/main.css`:

```css
@import "tailwindcss";
@import "@nuxt/ui";
```

- [ ] **Step 5: Entry + plugins** — `dashboard/src/main.ts`:

```ts
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ui from '@nuxt/ui/vue-plugin'
import App from './App.vue'
import { router } from './router'
import { i18n } from './i18n'
import './assets/main.css'

createApp(App).use(createPinia()).use(router).use(i18n).use(ui).mount('#app')
```

- [ ] **Step 6: Router** — `dashboard/src/router.ts`:

```ts
import { createRouter, createWebHistory } from 'vue-router'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('./pages/LoginView.vue') },
    { path: '/consent', name: 'consent', component: () => import('./pages/ConsentView.vue') },
    { path: '/logout', name: 'logout', component: () => import('./pages/LogoutView.vue') },
    { path: '/error', name: 'error', component: () => import('./pages/ErrorView.vue') },
    { path: '/:pathMatch(.*)*', redirect: '/login' },
  ],
})
```

- [ ] **Step 7: i18n** — `dashboard/src/i18n.ts`:

```ts
import { createI18n } from 'vue-i18n'
import zh from './locales/zh'
import en from './locales/en'

const stored = localStorage.getItem('locale')
const nav = navigator.language.startsWith('zh') ? 'zh' : 'en'
export const i18n = createI18n({
  legacy: false,
  locale: stored ?? (navigator.language.startsWith('zh') ? 'zh' : nav),
  fallbackLocale: 'zh',
  messages: { zh, en },
})
```

`dashboard/src/locales/zh.ts` (seed; later tasks extend):

```ts
export default {
  app: { name: 'Prohibitorum' },
  common: { continue: '继续', cancel: '取消', signOut: '退出登录' },
  login: { title: '登录', passkey: '使用通行密钥登录', password: '使用密码登录', or: '或', totp: '请输入动态验证码', signInWith: '使用 {name} 登录' },
  consent: { title: '授权请求', requests: '「{app}」请求以下权限：', continueAs: '以 {account} 身份继续', approve: '允许', deny: '拒绝' },
  logout: { done: '您已退出登录', returnTo: '返回 {app}' },
  error: { title: '出错了', generic: '发生了未知错误' },
  scopes: { openid: '基本身份', profile: '您的个人资料（姓名、昵称）', email: '您的邮箱地址', offline_access: '在您离线时持续访问', address: '您的地址', phone: '您的电话号码' },
  errors: { no_session: '请先登录', invalid_consent_ticket: '授权请求已失效，请重新发起登录', bad_credentials: '凭证无效', factor_locked: '尝试次数过多，请稍后再试' },
}
```

`dashboard/src/locales/en.ts` (mirror keys; English values):

```ts
export default {
  app: { name: 'Prohibitorum' },
  common: { continue: 'Continue', cancel: 'Cancel', signOut: 'Sign out' },
  login: { title: 'Sign in', passkey: 'Sign in with a passkey', password: 'Sign in with password', or: 'or', totp: 'Enter your authenticator code', signInWith: 'Sign in with {name}' },
  consent: { title: 'Authorization request', requests: '"{app}" requests the following permissions:', continueAs: 'Continue as {account}', approve: 'Allow', deny: 'Deny' },
  logout: { done: 'You have signed out', returnTo: 'Return to {app}' },
  error: { title: 'Something went wrong', generic: 'An unknown error occurred' },
  scopes: { openid: 'Basic identity', profile: 'Your profile (name, nickname)', email: 'Your email address', offline_access: 'Offline access while you are away', address: 'Your address', phone: 'Your phone number' },
  errors: { no_session: 'Please sign in first', invalid_consent_ticket: 'This authorization request has expired; please start over', bad_credentials: 'Invalid credentials', factor_locked: 'Too many attempts, try again later' },
}
```

- [ ] **Step 8: API client** — `dashboard/src/lib/api.ts`:

```ts
export interface ApiError { code: string; message: string }

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: 'include',
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (res.status === 204) return undefined as T
  const text = await res.text()
  const data = text ? JSON.parse(text) : undefined
  if (!res.ok) {
    const err = (data ?? {}) as ApiError
    throw err.code ? err : { code: 'server_error', message: err?.message ?? 'request failed' }
  }
  return data as T
}

export const api = {
  get: <T>(p: string) => request<T>('GET', p),
  post: <T>(p: string, b?: unknown) => request<T>('POST', p, b),
}
```

- [ ] **Step 9: returnTo guard + test** — `dashboard/src/lib/returnTo.ts`:

```ts
// Only allow navigation to a same-origin URL. Accepts absolute same-origin URLs
// and root-relative paths. Returns the safe URL or null.
export function safeReturnTo(raw: string | null): string | null {
  if (!raw) return null
  try {
    const u = new URL(raw, window.location.origin)
    return u.origin === window.location.origin ? u.toString() : null
  } catch {
    return null
  }
}
```

`dashboard/src/lib/returnTo.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { safeReturnTo } from './returnTo'

describe('safeReturnTo', () => {
  it('accepts same-origin absolute + relative', () => {
    expect(safeReturnTo(window.location.origin + '/oauth/authorize?x=1')).toContain('/oauth/authorize')
    expect(safeReturnTo('/oauth/authorize')).toContain('/oauth/authorize')
  })
  it('rejects cross-origin + empty', () => {
    expect(safeReturnTo('https://evil.example/x')).toBeNull()
    expect(safeReturnTo(null)).toBeNull()
    expect(safeReturnTo('')).toBeNull()
  })
})
```

`dashboard/vitest.config.ts`:

```ts
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  test: { environment: 'jsdom' },
})
```

- [ ] **Step 10: Session store + App shell + LocaleSwitcher + page stubs.** `dashboard/src/stores/session.ts` (Pinia store caching `/me`); `dashboard/src/App.vue` (a centered card layout using `<UApp>`/`<UCard>` + `<RouterView>` + `<LocaleSwitcher>` in a header, demonstrating one `<UButton>`); `dashboard/src/components/LocaleSwitcher.vue` (toggles `i18n.global.locale`, persists to `localStorage`, updates `document.documentElement.lang`); and the four page stubs each rendering their i18n title (filled in Tasks 8–10). Provide minimal but real Vue SFCs (no placeholders) — e.g. `LoginView.vue` stub:

```vue
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
const { t } = useI18n()
</script>
<template>
  <UCard>
    <h1 class="text-xl font-semibold">{{ t('login.title') }}</h1>
  </UCard>
</template>
```

(Write the analogous stub for `ConsentView`, `LogoutView`, `ErrorView`, `App.vue`, `LocaleSwitcher.vue`, `session.ts`.)

- [ ] **Step 11: `index.html`** — `dashboard/index.html` with `<div id="app">` + `<script type="module" src="/src/main.ts">`. `tsconfig.json` per Vue+Vite defaults (`vue-tsc`).

- [ ] **Step 12: gitignore** — `dashboard/.gitignore` (`node_modules`, `dist`, `*.tsbuildinfo`); confirm root `.gitignore` also ignores `dashboard/dist` and `dashboard/node_modules`.

- [ ] **Step 13: Install, build, test**

Run: `cd dashboard && mise exec -- npm install && mise exec -- npm run build && mise exec -- npm run test`
Expected: `dist/index.html` produced; Vitest PASS (`returnTo` test green).

- [ ] **Step 14: Commit**

```bash
git add dashboard mise.toml .gitignore
git commit -m "feat(dashboard): Vue 3 + Vite + Nuxt UI scaffold (router, i18n zh/en, Pinia, api client)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Go embed + SPA fallback + CSP

**Goal:** Embed `dashboard/dist` into the binary and serve it same-origin via the chi `NotFound` handler with strict security headers; protocol/API routes are unaffected.

**Files:**
- Create: `pkg/webui/webui.go`
- Modify: `pkg/server/server.go` (set `router.NotFound`)

**Acceptance Criteria:**
- [ ] `go:embed` includes `dist`; a `Handler()` serves an embedded asset when the path matches a file, else returns `index.html` (SPA fallback) with `Content-Type: text/html`.
- [ ] The shell response carries `Content-Security-Policy: default-src 'self'; connect-src 'self'; img-src 'self' data'; frame-ancestors 'none'` and `X-Frame-Options: DENY`.
- [ ] `router.NotFound(webui.Handler(...))` — `/api/*`, `/oauth/*`, `/saml/*`, `/oidc/*`, `/.well-known/*` still resolve to their handlers (they are registered, so never reach NotFound).
- [ ] `mise exec -- go build ./...` exit 0; a running server returns `index.html` for `GET /login` and JSON for `GET /.well-known/openid-configuration`.

**Verify:** build the binary, start it, `curl -s localhost:8080/login | grep '<div id="app">'` and `curl -s localhost:8080/.well-known/openid-configuration | grep issuer` both succeed; `curl -sI localhost:8080/login | grep -i content-security-policy`.

**Steps:**

- [ ] **Step 1: webui package** — create `pkg/webui/webui.go`:

```go
// Package webui embeds the built Vue SPA (dashboard/dist) and serves it
// same-origin with a SPA history fallback + strict security headers.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// Handler returns an http.Handler that serves embedded SPA assets, falling back
// to index.html for any path that does not match a built file (client-side
// routing). It is intended to be wired as the chi router's NotFound handler, so
// it is only reached for paths no registered route matched.
func Handler() http.Handler {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic("webui: dist not embedded: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		panic("webui: dist/index.html missing — run the frontend build first")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecurityHeaders(w)
		// If the path maps to an existing embedded file, serve it; else SPA shell.
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, ferr := sub.Open(p); ferr == nil {
				_ = f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}

func setSecurityHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy",
		"default-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}
```

NOTE: Nuxt UI / Vite may inject inline `<style>` or styles requiring `style-src 'unsafe-inline'`. If, after Task 9/10, the browser console reports CSP style violations, relax ONLY `style-src` to `'self' 'unsafe-inline'` (document why in a comment). Keep `script-src`/`default-src` tight.

- [ ] **Step 2: Wire NotFound** — in `pkg/server/server.go`, after `s.registerOperations()` (so all routes are registered first), add:

```go
	s.router.NotFound(webui.Handler().ServeHTTP)
```

Add the import `"prohibitorum/pkg/webui"`. (Place the `NotFound` wiring in `New` right after `s.registerOperations()`; do NOT add it in `NewHuma()`, the config-less openapi-emit path.)

- [ ] **Step 3: Build** — the frontend `dist` must exist for `go:embed`. Run `cd dashboard && mise exec -- npm run build` first (or the `frontend-build` mise task), then `mise exec -- go build ./...` → exit 0.

- [ ] **Step 4: Runtime verify** — start the dev server (reuse the smoke runner's env), then:
```bash
curl -s localhost:8080/login | grep -q 'id="app"' && echo LOGIN_OK
curl -s localhost:8080/.well-known/openid-configuration | grep -q issuer && echo DISCOVERY_OK
curl -sI localhost:8080/login | grep -i 'content-security-policy' && echo CSP_OK
```
Expected: `LOGIN_OK`, `DISCOVERY_OK`, `CSP_OK`.

- [ ] **Step 5: Commit**

```bash
git add pkg/webui/webui.go pkg/server/server.go
git commit -m "feat(webui): embed Vue SPA, serve same-origin via NotFound + strict CSP

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: Login page

**Goal:** Implement `/login` — passkey (prominent), federation buttons, and progressive password→TOTP — calling the existing auth APIs and returning to `return_to`.

**Files:**
- Modify: `dashboard/src/pages/LoginView.vue`
- Create: `dashboard/src/components/{PasskeyButton,PasswordTotpForm,FederationButtons}.vue`, `dashboard/src/lib/webauthn.ts`
- Test: `dashboard/src/components/PasswordTotpForm.test.ts`

**Acceptance Criteria:**
- [ ] On mount: read `return_to` (same-origin-guarded); call `/me` — if a live session AND no `&reauth=` in `return_to`, navigate straight to `return_to`.
- [ ] Passkey: `startAuthentication` against `/auth/login/begin` → `/auth/login/complete`; on success → `return_to`.
- [ ] Federation buttons rendered from `GET /api/prohibitorum/auth/federation`; click → `location = /api/prohibitorum/auth/federation/{slug}/login?return_to=<relative path+query of return_to>`.
- [ ] Password→TOTP: `/auth/password/begin` → `{partial_session_token}` → TOTP step → `/auth/totp/verify` (204) → `return_to`.
- [ ] Errors shown via localized `errors.<code>` (fallback to backend `message`).
- [ ] Vitest: the PasswordTotpForm component test passes (mocked api).

**Verify:** `cd dashboard && mise exec -- npm run test` PASS; `mise exec -- npm run build` exit 0. (UI behavior manually verifiable via the dev server.)

**Steps:**

- [ ] **Step 1: WebAuthn helper** — `dashboard/src/lib/webauthn.ts`:

```ts
import { startAuthentication } from '@simplewebauthn/browser'
import { api } from './api'
import type { SessionView } from '../stores/session'

// Drives the WebAuthn login ceremony. /begin returns PublicKeyCredentialRequestOptions
// JSON (and sets the ceremony cookie); /complete returns the SessionView.
export async function passkeyLogin(): Promise<SessionView> {
  const options = await api.get<any>('/api/prohibitorum/auth/login/begin')
  const assertion = await startAuthentication({ optionsJSON: options.publicKey ?? options })
  return await api.post<SessionView>('/api/prohibitorum/auth/login/complete', assertion)
}
```

(If `/begin` returns the options nested under `publicKey`, the `options.publicKey ?? options` handles both; verify the exact shape against the running server during Step 6 and pin it.)

- [ ] **Step 2: PasswordTotpForm** — `dashboard/src/components/PasswordTotpForm.vue`: two-phase form. Phase 1 fields username+password → `api.post('/api/prohibitorum/auth/password/begin', {username, password})` → store `partial_session_token`, advance to phase 2. Phase 2 field code → `api.post('/api/prohibitorum/auth/totp/verify', {partial_session_token, code})` (204) → `emit('success')`. Surface errors via a `code`→i18n lookup. Use `<UFormField>`, `<UInput>`, `<UButton>`. Emit `success` on completion.

- [ ] **Step 3: FederationButtons** — `dashboard/src/components/FederationButtons.vue`: on mount `api.get<FederationProvider[]>('/api/prohibitorum/auth/federation')`; render a `<UButton>` per provider labelled `t('login.signInWith', {name: p.displayName})`; click sets `window.location.href = '/api/prohibitorum/auth/federation/' + encodeURIComponent(p.slug) + '/login?return_to=' + encodeURIComponent(relativeReturnTo)`, where `relativeReturnTo` is the path+query of the (same-origin) `return_to` (federation requires a relative return_to). Provide a prop `:relative-return-to`.

- [ ] **Step 4: PasskeyButton** — thin `<UButton>` that calls `passkeyLogin()` and emits `success`/`error`.

- [ ] **Step 5: LoginView** — compose: read+guard `return_to` (default `/`); `onMounted` call session store `fetchMe()`; if authenticated and `return_to` has no `reauth` param, `window.location.assign(returnTo)`. Otherwise render PasskeyButton (top), FederationButtons, an "or" divider, and PasswordTotpForm. On any `success`, `window.location.assign(returnTo)`. Compute `relativeReturnTo` = `new URL(returnTo).pathname + search`.

- [ ] **Step 6: Component test** — `dashboard/src/components/PasswordTotpForm.test.ts`: mock `../lib/api` (`vi.mock`), mount with i18n, fill username/password, submit, assert phase advances on a mocked `{partial_session_token}`, fill code, submit, assert `success` emitted on mocked 204. (Use `@vue/test-utils` `mount` + a global i18n plugin.)

- [ ] **Step 7: Test + build** → `cd dashboard && mise exec -- npm run test && mise exec -- npm run build` → PASS + `dist/` emitted.

- [ ] **Step 8: Commit**

```bash
git add dashboard/src
git commit -m "feat(dashboard): /login — passkey + password/TOTP + federation

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: Consent page

**Goal:** Implement `/consent` — fetch context by ticket, render RP + scopes (localized) + account, approve/deny following the returned redirect.

**Files:**
- Modify: `dashboard/src/pages/ConsentView.vue`
- Create: `dashboard/src/components/ConsentScopeList.vue`
- Test: `dashboard/src/components/ConsentScopeList.test.ts`

**Acceptance Criteria:**
- [ ] On mount: read `ticket` + `return_to` (guarded); `GET /api/prohibitorum/consent?ticket=` → render `t('consent.requests',{app})`, the scope list, `t('consent.continueAs',{account})`. Invalid ticket → navigate `/error?code=invalid_consent_ticket`.
- [ ] Approve → `POST /api/prohibitorum/consent?return_to=<encoded>` `{ticket, decision:'approve'}` → `window.location.assign(result.redirect)`.
- [ ] Deny → same POST with `decision:'deny'` → follow `result.redirect`.
- [ ] Scope list shows `t('scopes.<name>')`, falling back to the raw scope name for unknown scopes. Initial-letter avatar for the RP (no remote logo).
- [ ] Vitest: ConsentScopeList renders known + unknown scopes correctly.

**Verify:** `cd dashboard && mise exec -- npm run test && mise exec -- npm run build` → PASS + build.

**Steps:**

- [ ] **Step 1: ConsentScopeList** — `dashboard/src/components/ConsentScopeList.vue`: prop `scopes: string[]`; render each as a list item showing `te('scopes.'+s) ? t('scopes.'+s) : s` (use `useI18n().te` to test key existence). Use Nuxt UI list/`<UIcon>` styling.

- [ ] **Step 2: ConsentView** — read `ticket`/`return_to`; `onMounted` `api.get<ConsentContext>('/api/prohibitorum/consent?ticket='+encodeURIComponent(ticket))`; on error navigate `router.push({name:'error', query:{code:'invalid_consent_ticket'}})`. Render an initial-letter avatar (`<UAvatar :text="client.displayName[0]">`), the localized request line, `<ConsentScopeList :scopes>`, the continue-as line, and Approve/Deny `<UButton>`s. `decide(d)`: `const res = await api.post<{redirect:string}>('/api/prohibitorum/consent?return_to='+encodeURIComponent(returnTo), {ticket, decision:d}); window.location.assign(res.redirect)`.

- [ ] **Step 3: Test** — `ConsentScopeList.test.ts`: mount with i18n + `scopes:['openid','profile','x:custom']`; assert it shows "基本身份"/"Basic identity" for openid and the raw `x:custom` for the unknown.

- [ ] **Step 4: Test + build** → `cd dashboard && mise exec -- npm run test && mise exec -- npm run build` → PASS + build.

- [ ] **Step 5: Commit**

```bash
git add dashboard/src
git commit -m "feat(dashboard): /consent — render scopes, approve/deny follow server redirect

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 11: Logout + Error pages

**Goal:** Implement `/logout` (confirmation/landing; app logout via `/auth/logout`; optional return-to-RP) and `/error` (localized friendly text by `code`).

**Files:**
- Modify: `dashboard/src/pages/LogoutView.vue`, `dashboard/src/pages/ErrorView.vue`

**Acceptance Criteria:**
- [ ] `/logout`: on mount, `POST /api/prohibitorum/auth/logout` (204), show `t('logout.done')`; if `?post_logout_redirect_uri=` present AND same-origin OR (documented) allowed, show a `t('logout.returnTo',{app})` button → that URL. (If cross-origin, still render the message; the button only appears for a provided URI.)
- [ ] `/error`: read `?code=&description=`; show `t('error.title')` + (`te('errors.'+code) ? t('errors.'+code) : (description || t('error.generic'))`).
- [ ] Vitest build green.

**Verify:** `cd dashboard && mise exec -- npm run build && mise exec -- npm run test` → exit 0 / PASS.

**Steps:**

- [ ] **Step 1: LogoutView** — `onMounted`: `await api.post('/api/prohibitorum/auth/logout').catch(()=>{})` (idempotent; ignore errors); set a `done` ref; render `t('logout.done')`; read `post_logout_redirect_uri` from the query and, if non-empty, render a `<UButton>` linking to it labelled `t('logout.returnTo',{app:t('app.name')})`.

- [ ] **Step 2: ErrorView** — read `code`/`description`; compute message via `te`/`t` fallback chain; render `<UCard>` with `t('error.title')` + message + a link back to `/login`.

- [ ] **Step 3: Build + test** → green.

- [ ] **Step 4: Commit**

```bash
git add dashboard/src
git commit -m "feat(dashboard): /logout landing + /error page (localized)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 12: Full integration gate

**Goal:** Build the whole binary with the embedded frontend, run the full backend gate + smoke + Vitest, and verify the interactive surfaces are served.

**Files:** none (verification + any fixes surfaced).

**Acceptance Criteria:**
- [ ] `cd dashboard && mise exec -- npm run build` (dist fresh) then `mise exec -- go build ./... && mise exec -- go vet ./... && mise exec -- go test ./...` all green.
- [ ] `cd dashboard && mise exec -- npm run test` PASS.
- [ ] Full `cmd/smoke` `SMOKE_EXIT=0`.
- [ ] Running server: `/login`, `/consent`, `/logout`, `/error` return the SPA shell; `/.well-known/openid-configuration` still JSON; CSP header present.

**Verify:** the commands below all succeed.

**Steps:**

- [ ] **Step 1: Frontend build + unit** — `cd dashboard && mise exec -- npm ci && mise exec -- npm run build && mise exec -- npm run test`.

- [ ] **Step 2: Backend gate** — `mise exec -- go build ./... && mise exec -- go vet ./... && mise exec -- go test ./...` → all green.

- [ ] **Step 3: Smoke** — `setsid bash /tmp/run_v06.sh`, poll `/tmp/v06.result` for `DONE`; assert `SMOKE_EXIT=0`.

- [ ] **Step 4: Runtime surface check** — with the server up:
```bash
for p in /login /consent /logout /error; do curl -s "localhost:8080$p" | grep -q 'id="app"' && echo "$p OK"; done
curl -s localhost:8080/.well-known/openid-configuration | grep -q issuer && echo DISCOVERY_OK
curl -sI localhost:8080/login | grep -qi 'content-security-policy' && echo CSP_OK
```
Expected: all four routes `OK`, `DISCOVERY_OK`, `CSP_OK`.

- [ ] **Step 5: Commit (if any fixes)** — commit any integration fixes; otherwise note the gate is green. Then update `AUDIT.md`/`STATUS.md` if desired (optional; not required by this plan).

---

## Self-Review

- **Spec coverage:** D1 stack → T7. D2 embed/serve → T8 (+T7 build). D3 dev proxy → T7 vite.config. D4 login → T9. D5 consent store → T1. D6 consent ticket → T2. D7 authorize rewrite → T3. D8 consent API → T4. D9 consent page → T10. D10 logout → T11. D11 error → T11. D12 i18n → T7 (+ keys extended in T9–T11). D13 security (CSP/frame/return_to/no-remote-logo) → T8 (headers) + T7/T9 (returnTo guard) + T10 (initial avatar). D14 federation list → T5. Testing → unit tests in T2/T3/T7/T9/T10, smoke in T6, full gate T12. All covered.
- **Placeholder scan:** backend steps carry full Go; frontend gives full config + bespoke lib/page logic with exact code, and exact commands. The few "write the analogous stub" / "follow the package's existing test harness" notes are bounded, concrete instructions (the shapes and assertions are specified) — acceptable given the SFC/test boilerplate is standard and the domain-specific contracts are spelled out. The consent-handler `return_to` echo was corrected inline to the guarded version.
- **Type/contract consistency:** `GetConsentParams{AccountID,ClientID}`, `UpsertConsentParams{AccountID,ClientID,GrantedScopes}`, `authn.ConsentTicket{AccountID,ClientID,Scopes,RedirectURI,State}`, `authn.{DemandConsent,PeekConsent,ConsumeConsent}`, `contract.{ConsentContext,ConsentClient,ConsentUser,ConsentDecision,ConsentResult,FederationProvider}`, `db.OidcClient.{ClientID,DisplayName,LogoUri,PolicyUri,TosUri}`, `/api/prohibitorum/consent` + `/api/prohibitorum/auth/federation` — all used consistently across tasks and match the real backend signatures verified during planning. Migration is **007** (006 is taken).
