# End-user app launchpad — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "My apps" launcher as the end-user home — a tile grid of the apps each account may open (single-click SSO), with per-tile consent state + revoke and a Settings "App access" page — reusing the RBAC authorization predicate and the cycle-1 app icons.

**Architecture:** A new minimal `LauncherLayout` owns `/`; the existing sidebar shell (Security/Sessions/Connected/Devices + new App access + Admin) moves behind absolute-path child routes so URLs are unchanged. Three new `/me` endpoints (`GET /me/apps`, `GET /me/consent`, `POST /me/consent/revoke`) back the launcher and the App access page. Launch targets: OIDC `launch_url`→origin(`redirect_uris[0]`), forward-auth `https://<host>/`, SAML `/saml/sso/init?sp=<entityId>`.

**Tech Stack:** Go 1.26 + chi + huma + sqlc/pgx + goose; Vue 3 + Vite + Tailwind v4 + shadcn-vue (Reka UI) + vue-i18n; vitest; `cmd/smoke` end-to-end.

**Spec:** `docs/superpowers/specs/2026-06-22-end-user-app-launchpad-design.md`

**Conventions (read once):**
- Regenerate DB code after any `db/queries/*.sql` or `db/migrations/*.sql` change: `mise exec -- sqlc generate` (config at `sqlc.yaml`). Apply migrations to the dev DB: `mise run db:up`.
- Backend gate: `go build -tags nodynamic ./... && go vet ./... && go test ./...`.
- Frontend: `cd dashboard && npm test` (vitest) and `npm run build` (vue-tsc typecheck + build). Rebuild the embedded bundle with `mise run build:web` and **commit `pkg/webui/dist`** (CI fails on drift).
- Huma `/me` JSON handlers register via `registerOp(mgmt, contract.OperationX, s.handleX, sessionReq)`; raw handlers via `registerOpHTTP(s.router, "VERB", path, sessionReq, s.handleXHTTP)`. Session: `authn.SessionFromContext(ctx)`, account id `sess.Account.ID`.
- New `/me` read handlers use a **narrow override interface** + accessor (mirror `getMyFactorsQueries`/`s.getMyFactorsQueries()` in `pkg/server/handle_me.go`) so unit tests stub the DB without a live Postgres.

---

## Task 1: Add `launch_url` to `oidc_client` (migration + setter)

**Goal:** Persist an optional admin-set launch URL on OIDC clients; resolution falls back to the redirect-URI origin at read time.

**Files:**
- Create: `db/migrations/020_oidc_launch_url.sql`
- Modify: `db/queries/oidc.sql` (add `SetOIDCClientLaunchURL`)
- Regenerate: `pkg/db/*` via sqlc

**Acceptance Criteria:**
- [ ] `oidc_client.launch_url` (nullable text) exists after `mise run db:up`.
- [ ] `db.Queries` gains `SetOIDCClientLaunchURL`; `GetOIDCClient`/`GetOIDCClientAny` rows expose `LaunchUrl pgtype.Text`.
- [ ] `go build -tags nodynamic ./...` passes.

**Verify:** `mise run db:up && mise exec -- sqlc generate && go build -tags nodynamic ./...`

**Steps:**

- [ ] **Step 1: Write the migration**

`db/migrations/020_oidc_launch_url.sql`:
```sql
-- +goose Up
ALTER TABLE oidc_client ADD COLUMN launch_url text;

-- +goose Down
ALTER TABLE oidc_client DROP COLUMN launch_url;
```

- [ ] **Step 2: Add the setter query**

Append to `db/queries/oidc.sql`:
```sql
-- name: SetOIDCClientLaunchURL :exec
UPDATE oidc_client SET launch_url = $2 WHERE client_id = $1;
```

- [ ] **Step 3: Apply + regenerate**

Run: `mise run db:up && mise exec -- sqlc generate`
Expected: migration `020` applied; `pkg/db/oidc.sql.go` now has `SetOIDCClientLaunchURL`; `pkg/db/models.go` `OidcClient` has `LaunchUrl pgtype.Text`.

- [ ] **Step 4: Build**

Run: `go build -tags nodynamic ./...`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add db/migrations/020_oidc_launch_url.sql db/queries/oidc.sql pkg/db
git commit -m "feat(launchpad): add oidc_client.launch_url column + setter"
```

---

## Task 2: Authorized-apps + consent list queries

**Goal:** Add the four read queries the launchpad needs, each embedding the same RBAC grant predicate the protocol endpoints enforce.

**Files:**
- Modify: `db/queries/rbac.sql` (3 authorized-list queries)
- Modify: `db/queries/oidc_consent.sql` (consent list)
- Regenerate: `pkg/db/*`

**Acceptance Criteria:**
- [ ] `ListAuthorizedOIDCClientsForAccount`, `ListAuthorizedForwardAuthAppsForAccount`, `ListAuthorizedSAMLSPsForAccount`, `ListConsentsByAccount` exist on `db.Queries`.
- [ ] Each authorized query excludes `disabled` rows and applies `NOT access_restricted OR <direct grant> OR <via-group grant>`.

**Verify:** `mise exec -- sqlc generate && go build -tags nodynamic ./...`

**Steps:**

- [ ] **Step 1: Add the three authorized-list queries**

Append to `db/queries/rbac.sql`:
```sql
-- name: ListAuthorizedOIDCClientsForAccount :many
SELECT c.client_id, c.display_name, c.launch_url, c.redirect_uris
FROM oidc_client c
WHERE c.disabled = false
  AND c.forward_auth_enabled = false
  AND (
    NOT c.access_restricted
    OR EXISTS (SELECT 1 FROM oidc_client_access a
               WHERE a.client_id = c.client_id AND a.account_id = sqlc.arg(account_id))
    OR EXISTS (SELECT 1 FROM oidc_client_access a
               JOIN group_member m ON m.group_id = a.group_id
               WHERE a.client_id = c.client_id AND m.account_id = sqlc.arg(account_id))
  )
ORDER BY c.display_name;

-- name: ListAuthorizedForwardAuthAppsForAccount :many
SELECT c.client_id, c.display_name, c.forward_auth_host
FROM oidc_client c
WHERE c.disabled = false
  AND c.forward_auth_enabled = true
  AND c.forward_auth_host IS NOT NULL
  AND (
    NOT c.access_restricted
    OR EXISTS (SELECT 1 FROM oidc_client_access a
               WHERE a.client_id = c.client_id AND a.account_id = sqlc.arg(account_id))
    OR EXISTS (SELECT 1 FROM oidc_client_access a
               JOIN group_member m ON m.group_id = a.group_id
               WHERE a.client_id = c.client_id AND m.account_id = sqlc.arg(account_id))
  )
ORDER BY c.display_name;

-- name: ListAuthorizedSAMLSPsForAccount :many
SELECT s.id, s.entity_id, s.display_name
FROM saml_sp s
WHERE s.disabled = false
  AND s.allow_idp_initiated = true
  AND (
    NOT s.access_restricted
    OR EXISTS (SELECT 1 FROM saml_sp_access a
               WHERE a.saml_sp_id = s.id AND a.account_id = sqlc.arg(account_id))
    OR EXISTS (SELECT 1 FROM saml_sp_access a
               JOIN group_member m ON m.group_id = a.group_id
               WHERE a.saml_sp_id = s.id AND m.account_id = sqlc.arg(account_id))
  )
ORDER BY s.display_name;
```

- [ ] **Step 2: Add the consent-list query**

Append to `db/queries/oidc_consent.sql`:
```sql
-- name: ListConsentsByAccount :many
SELECT c.client_id, oc.display_name, c.granted_scopes, c.updated_at
FROM oidc_consent c
JOIN oidc_client oc ON oc.client_id = c.client_id
WHERE c.account_id = $1
ORDER BY oc.display_name;
```

- [ ] **Step 3: Regenerate + build**

Run: `mise exec -- sqlc generate && go build -tags nodynamic ./...`
Expected: PASS; new methods + row types (`ListAuthorizedOIDCClientsForAccountRow`, etc.) generated in `pkg/db`.

- [ ] **Step 4: Commit**
```bash
git add db/queries pkg/db
git commit -m "feat(launchpad): authorized-apps + consent list queries"
```

---

## Task 3: Launch-URL resolution helper (pure, TDD)

**Goal:** One pure function that resolves an OIDC client's launch URL.

**Files:**
- Create: `pkg/server/launchpad_url.go`
- Test: `pkg/server/launchpad_url_test.go`

**Acceptance Criteria:**
- [ ] `resolveOIDCLaunchURL(launch string, redirectURIs []string) string` returns the trimmed `launch` if non-empty; else `scheme://host/` of the first parseable redirect URI; else `""`.

**Verify:** `go test ./pkg/server/ -run TestResolveOIDCLaunchURL -v`

**Steps:**

- [ ] **Step 1: Write the failing test**

`pkg/server/launchpad_url_test.go`:
```go
package server

import "testing"

func TestResolveOIDCLaunchURL(t *testing.T) {
	cases := []struct {
		name      string
		launch    string
		redirects []string
		want      string
	}{
		{"explicit launch wins", "https://app.example.com/home", []string{"https://app.example.com/cb"}, "https://app.example.com/home"},
		{"trim explicit", "  https://x/y  ", nil, "https://x/y"},
		{"derive origin from first redirect", "", []string{"https://app.example.com/auth/callback"}, "https://app.example.com/"},
		{"skip unparseable, use first valid", "", []string{"not a url", "https://ok.example/cb"}, "https://ok.example/"},
		{"none → empty", "", nil, ""},
		{"redirect without host → empty", "", []string{"/relative/only"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveOIDCLaunchURL(tc.launch, tc.redirects); got != tc.want {
				t.Fatalf("resolveOIDCLaunchURL(%q, %v) = %q, want %q", tc.launch, tc.redirects, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run → fail**

Run: `go test ./pkg/server/ -run TestResolveOIDCLaunchURL -v`
Expected: FAIL (`resolveOIDCLaunchURL` undefined).

- [ ] **Step 3: Implement**

`pkg/server/launchpad_url.go`:
```go
// Package server — launchpad_url.go
// Pure resolution of an OIDC client's launch URL for the end-user launchpad.
package server

import (
	"net/url"
	"strings"
)

// resolveOIDCLaunchURL picks where the launchpad opens an OIDC app: the explicit
// admin launch_url when set, else the scheme://host origin of the first
// parseable redirect URI (its login start usually lives at the app root), else
// "" meaning "not launchable" (the caller omits the app).
func resolveOIDCLaunchURL(launch string, redirectURIs []string) string {
	if s := strings.TrimSpace(launch); s != "" {
		return s
	}
	for _, ru := range redirectURIs {
		if u, err := url.Parse(strings.TrimSpace(ru)); err == nil && u.Scheme != "" && u.Host != "" {
			return u.Scheme + "://" + u.Host + "/"
		}
	}
	return ""
}
```

- [ ] **Step 4: Run → pass**

Run: `go test ./pkg/server/ -run TestResolveOIDCLaunchURL -v`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add pkg/server/launchpad_url.go pkg/server/launchpad_url_test.go
git commit -m "feat(launchpad): launch-URL resolution helper"
```

---

## Task 4: Contracts — launchpad + consent DTOs and operations

**Goal:** Define the wire shapes and huma operations for the three endpoints.

**Files:**
- Create: `pkg/contract/launchpad.go`

**Acceptance Criteria:**
- [ ] `LaunchpadApp`, `ConsentedApp`, `RevokeConsentInput` types exist with camelCase JSON tags.
- [ ] `OperationListMyApps`, `OperationListMyConsent`, `OperationRevokeConsent` huma operations exist.

**Verify:** `go build -tags nodynamic ./...`

**Steps:**

- [ ] **Step 1: Write the contracts**

`pkg/contract/launchpad.go`:
```go
package contract

import (
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// LaunchpadApp is one launchable app on the end-user "My apps" home. Kind is
// "oidc" | "forward_auth" | "saml" (drives the tile's type chip). LaunchURL is
// always non-empty (non-launchable apps are omitted server-side). IconURL is nil
// when the app has no uploaded icon.
type LaunchpadApp struct {
	Kind      string  `json:"kind"`
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	IconURL   *string `json:"iconUrl,omitempty"`
	LaunchURL string  `json:"launchUrl"`
}

// ConsentedApp is one app the account has granted OIDC consent to.
type ConsentedApp struct {
	ClientID  string    `json:"clientId"`
	Name      string    `json:"name"`
	IconURL   *string   `json:"iconUrl,omitempty"`
	Scopes    []string  `json:"scopes"`
	GrantedAt time.Time `json:"grantedAt"`
}

// RevokeConsentInput is the body of POST /me/consent/revoke.
type RevokeConsentInput struct {
	ClientID string `json:"clientId"`
}

var OperationListMyApps = huma.Operation{
	OperationID: "listMyApps",
	Method:      "GET",
	Path:        "/api/prohibitorum/me/apps",
	Summary:     "List the apps the signed-in account may launch",
	Tags:        []string{"me"},
}

var OperationListMyConsent = huma.Operation{
	OperationID: "listMyConsent",
	Method:      "GET",
	Path:        "/api/prohibitorum/me/consent",
	Summary:     "List the apps the signed-in account has granted access to",
	Tags:        []string{"me"},
}

var OperationRevokeConsent = huma.Operation{
	OperationID: "revokeMyConsent",
	Method:      "POST",
	Path:        "/api/prohibitorum/me/consent/revoke",
	Summary:     "Revoke the signed-in account's consent for an app",
	Tags:        []string{"me"},
}
```
(If `pkg/contract` imports huma under a different alias, match the existing files — check `pkg/contract/auth.go`'s import line and mirror it.)

- [ ] **Step 2: Build**

Run: `go build -tags nodynamic ./...`
Expected: PASS.

- [ ] **Step 3: Commit**
```bash
git add pkg/contract/launchpad.go
git commit -m "feat(launchpad): contracts for /me/apps and /me/consent"
```

---

## Task 5: `GET /me/apps` handler (+ unit tests)

**Goal:** Merge the three authorized sources into one sorted launchable list with resolved launch + icon URLs.

**Files:**
- Create: `pkg/server/handle_me_apps.go`
- Test: `pkg/server/handle_me_apps_test.go`
- Modify: `pkg/server/server.go` (struct field + registration)

**Acceptance Criteria:**
- [ ] `GET /me/apps` returns OIDC (launchable only), forward-auth, and SAML(idp-initiated) apps with correct `kind`, `launchUrl`, and `iconUrl`.
- [ ] OIDC apps with no resolvable launch URL are omitted.
- [ ] Unit test passes with a stubbed `launchpadQueries`.

**Verify:** `go test ./pkg/server/ -run TestHandleMyApps -v`

**Steps:**

- [ ] **Step 1: Add the override field on Server**

In `pkg/server/server.go`, in the `Server` struct (next to `getMyFactorsOverride`), add:
```go
	launchpadOverride launchpadQueries
```

- [ ] **Step 2: Write the failing test**

`pkg/server/handle_me_apps_test.go`:
```go
package server

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"prohibitorum/pkg/db"
)

type fakeLaunchpadQ struct {
	oidc    []db.ListAuthorizedOIDCClientsForAccountRow
	fwd     []db.ListAuthorizedForwardAuthAppsForAccountRow
	saml    []db.ListAuthorizedSAMLSPsForAccountRow
	etags   map[string]string // "kind/id" -> etag
}

func (f *fakeLaunchpadQ) ListAuthorizedOIDCClientsForAccount(_ context.Context, _ int32) ([]db.ListAuthorizedOIDCClientsForAccountRow, error) {
	return f.oidc, nil
}
func (f *fakeLaunchpadQ) ListAuthorizedForwardAuthAppsForAccount(_ context.Context, _ int32) ([]db.ListAuthorizedForwardAuthAppsForAccountRow, error) {
	return f.fwd, nil
}
func (f *fakeLaunchpadQ) ListAuthorizedSAMLSPsForAccount(_ context.Context, _ int32) ([]db.ListAuthorizedSAMLSPsForAccountRow, error) {
	return f.saml, nil
}
func (f *fakeLaunchpadQ) GetEntityIconEtag(_ context.Context, p db.GetEntityIconEtagParams) (string, error) {
	if e, ok := f.etags[p.OwnerKind+"/"+p.OwnerID]; ok {
		return e, nil
	}
	return "", pgx.ErrNoRows
}

func TestHandleMyApps(t *testing.T) {
	s := &Server{launchpadOverride: &fakeLaunchpadQ{
		oidc: []db.ListAuthorizedOIDCClientsForAccountRow{
			{ClientID: "grafana", DisplayName: "Grafana", LaunchUrl: pgtype.Text{}, RedirectUris: []string{"https://grafana.example/login/generic_oauth"}},
			{ClientID: "no-redirect", DisplayName: "Headless", LaunchUrl: pgtype.Text{}, RedirectUris: nil}, // omitted: no launch URL
		},
		fwd:  []db.ListAuthorizedForwardAuthAppsForAccountRow{{ClientID: "wiki", DisplayName: "Wiki", ForwardAuthHost: pgtype.Text{String: "wiki.example", Valid: true}}},
		saml: []db.ListAuthorizedSAMLSPsForAccountRow{{ID: 7, EntityID: "https://ghe.example/saml", DisplayName: "GitHub"}},
		etags: map[string]string{"oidc_client/grafana": "abcdef1234"},
	}}
	apps, err := s.buildLaunchpad(context.Background(), 1)
	if err != nil {
		t.Fatalf("buildLaunchpad: %v", err)
	}
	// Headless omitted → 3 apps.
	if len(apps) != 3 {
		t.Fatalf("want 3 apps, got %d: %+v", len(apps), apps)
	}
	by := map[string]int{}
	for i, a := range apps {
		by[a.ID] = i
	}
	g := apps[by["grafana"]]
	if g.Kind != "oidc" || g.LaunchURL != "https://grafana.example/" {
		t.Fatalf("grafana: kind=%q launch=%q", g.Kind, g.LaunchURL)
	}
	if g.IconURL == nil || *g.IconURL == "" {
		t.Fatalf("grafana icon should be set, got %v", g.IconURL)
	}
	if w := apps[by["wiki"]]; w.Kind != "forward_auth" || w.LaunchURL != "https://wiki.example/" {
		t.Fatalf("wiki: kind=%q launch=%q", w.Kind, w.LaunchURL)
	}
	if sm := apps[by["7"]]; sm.Kind != "saml" || sm.LaunchURL != "/saml/sso/init?sp=https%3A%2F%2Fghe.example%2Fsaml" {
		t.Fatalf("saml: kind=%q launch=%q", sm.Kind, sm.LaunchURL)
	}
}
```

- [ ] **Step 3: Run → fail**

Run: `go test ./pkg/server/ -run TestHandleMyApps -v`
Expected: FAIL (`launchpadQueries`, `buildLaunchpad` undefined).

- [ ] **Step 4: Implement**

`pkg/server/handle_me_apps.go`:
```go
package server

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
)

// launchpadQueries is the narrow DB surface buildLaunchpad needs. Tests stub it
// via s.launchpadOverride; production falls back to s.queries.
type launchpadQueries interface {
	ListAuthorizedOIDCClientsForAccount(ctx context.Context, accountID int32) ([]db.ListAuthorizedOIDCClientsForAccountRow, error)
	ListAuthorizedForwardAuthAppsForAccount(ctx context.Context, accountID int32) ([]db.ListAuthorizedForwardAuthAppsForAccountRow, error)
	ListAuthorizedSAMLSPsForAccount(ctx context.Context, accountID int32) ([]db.ListAuthorizedSAMLSPsForAccountRow, error)
	GetEntityIconEtag(ctx context.Context, arg db.GetEntityIconEtagParams) (string, error)
}

func (s *Server) launchpadQueries() launchpadQueries {
	if s.launchpadOverride != nil {
		return s.launchpadOverride
	}
	return s.queries
}

type myAppsOut struct {
	Body []contract.LaunchpadApp
}

func (s *Server) handleListMyApps(ctx context.Context, _ *struct{}) (*myAppsOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	apps, err := s.buildLaunchpad(ctx, sess.Account.ID)
	if err != nil {
		return nil, err
	}
	return &myAppsOut{Body: apps}, nil
}

// buildLaunchpad merges the three authorized sources into one name-sorted list.
func (s *Server) buildLaunchpad(ctx context.Context, accountID int32) ([]contract.LaunchpadApp, error) {
	q := s.launchpadQueries()
	out := make([]contract.LaunchpadApp, 0, 16)

	icon := func(kind, id string) *string {
		etag, err := q.GetEntityIconEtag(ctx, db.GetEntityIconEtagParams{OwnerKind: kind, OwnerID: id})
		if err != nil {
			return nil // not-found or error → no icon (etag lookup already logs real errors elsewhere)
		}
		return entityIconURLPtr(kind, id, etag)
	}

	oidc, err := q.ListAuthorizedOIDCClientsForAccount(ctx, accountID)
	if err != nil {
		return nil, errors.New("launchpad: list oidc: " + err.Error())
	}
	for _, c := range oidc {
		launch := resolveOIDCLaunchURL(c.LaunchUrl.String, c.RedirectUris)
		if launch == "" {
			continue // not launchable
		}
		out = append(out, contract.LaunchpadApp{
			Kind: "oidc", ID: c.ClientID, Name: c.DisplayName,
			IconURL: icon("oidc_client", c.ClientID), LaunchURL: launch,
		})
	}

	fwd, err := q.ListAuthorizedForwardAuthAppsForAccount(ctx, accountID)
	if err != nil {
		return nil, errors.New("launchpad: list forward-auth: " + err.Error())
	}
	for _, c := range fwd {
		if !c.ForwardAuthHost.Valid || c.ForwardAuthHost.String == "" {
			continue
		}
		out = append(out, contract.LaunchpadApp{
			Kind: "forward_auth", ID: c.ClientID, Name: c.DisplayName,
			IconURL: icon("oidc_client", c.ClientID), LaunchURL: "https://" + c.ForwardAuthHost.String + "/",
		})
	}

	saml, err := q.ListAuthorizedSAMLSPsForAccount(ctx, accountID)
	if err != nil {
		return nil, errors.New("launchpad: list saml: " + err.Error())
	}
	for _, sp := range saml {
		id := strconv.FormatInt(int64(sp.ID), 10)
		out = append(out, contract.LaunchpadApp{
			Kind: "saml", ID: id, Name: sp.DisplayName,
			IconURL:   icon("saml_sp", id),
			LaunchURL: "/saml/sso/init?sp=" + url.QueryEscape(sp.EntityID),
		})
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

var _ = pgx.ErrNoRows // keep pgx import if not otherwise referenced
```
> Note: `sp.ID` is the sqlc Go type for `saml_sp.id` (bigint → `int64`); if the generated field is already `int64`, drop the `int64(...)` conversion. Check `db.ListAuthorizedSAMLSPsForAccountRow` and adjust. Remove the `var _ = pgx.ErrNoRows` line and the `pgx` import if unused after writing.

- [ ] **Step 5: Register the route**

In `pkg/server/server.go` `registerOperations()`, after the existing `/me` block (near `OperationRevokeMySession`), add:
```go
	registerOp(mgmt, contract.OperationListMyApps, s.handleListMyApps, sessionReq)
```

- [ ] **Step 6: Run → pass + build**

Run: `go test ./pkg/server/ -run TestHandleMyApps -v && go build -tags nodynamic ./...`
Expected: PASS.

- [ ] **Step 7: Commit**
```bash
git add pkg/server/handle_me_apps.go pkg/server/handle_me_apps_test.go pkg/server/server.go
git commit -m "feat(launchpad): GET /me/apps handler"
```

---

## Task 6: `GET /me/consent` + `POST /me/consent/revoke` (+ unit tests)

**Goal:** List the account's consents and let it revoke one (no sudo).

**Files:**
- Create: `pkg/server/handle_me_consent.go`
- Test: `pkg/server/handle_me_consent_test.go`
- Modify: `pkg/server/server.go` (struct field + 2 registrations)

**Acceptance Criteria:**
- [ ] `GET /me/consent` returns the account's consents (name + scopes + grantedAt + iconUrl).
- [ ] `POST /me/consent/revoke {clientId}` deletes that account's consent; idempotent; only affects the caller's account.
- [ ] Unit test passes with a stubbed `consentMgmtQueries`.

**Verify:** `go test ./pkg/server/ -run TestHandleMyConsent -v`

**Steps:**

- [ ] **Step 1: Add the override field on Server**

In `pkg/server/server.go` `Server` struct add:
```go
	consentMgmtOverride consentMgmtQueries
```

- [ ] **Step 2: Write the failing test**

`pkg/server/handle_me_consent_test.go`:
```go
package server

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"prohibitorum/pkg/db"
)

type fakeConsentQ struct {
	rows    []db.ListConsentsByAccountRow
	deleted []db.DeleteConsentParams
	etags   map[string]string
}

func (f *fakeConsentQ) ListConsentsByAccount(_ context.Context, _ int32) ([]db.ListConsentsByAccountRow, error) {
	return f.rows, nil
}
func (f *fakeConsentQ) DeleteConsent(_ context.Context, p db.DeleteConsentParams) error {
	f.deleted = append(f.deleted, p)
	return nil
}
func (f *fakeConsentQ) GetEntityIconEtag(_ context.Context, p db.GetEntityIconEtagParams) (string, error) {
	if e, ok := f.etags[p.OwnerKind+"/"+p.OwnerID]; ok {
		return e, nil
	}
	return "", pgx.ErrNoRows
}

func TestHandleMyConsentList(t *testing.T) {
	s := &Server{consentMgmtOverride: &fakeConsentQ{rows: []db.ListConsentsByAccountRow{
		{ClientID: "grafana", DisplayName: "Grafana", GrantedScopes: []string{"openid", "profile"}, UpdatedAt: pgTime(time.Now())},
	}}}
	out, err := s.listConsents(context.Background(), 1)
	if err != nil {
		t.Fatalf("listConsents: %v", err)
	}
	if len(out) != 1 || out[0].ClientID != "grafana" || len(out[0].Scopes) != 2 {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestHandleMyConsentRevoke(t *testing.T) {
	f := &fakeConsentQ{}
	s := &Server{consentMgmtOverride: f}
	if err := s.revokeConsent(context.Background(), 5, "grafana"); err != nil {
		t.Fatalf("revokeConsent: %v", err)
	}
	if len(f.deleted) != 1 || f.deleted[0].AccountID != 5 || f.deleted[0].ClientID != "grafana" {
		t.Fatalf("delete not scoped to caller: %+v", f.deleted)
	}
}
```
> `pgTime` helper: if the repo already has one in `pkg/server` tests, reuse it; otherwise add `func pgTime(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }` to this test file (import `github.com/jackc/pgx/v5/pgtype`). Confirm the generated `UpdatedAt` type on `ListConsentsByAccountRow` (likely `pgtype.Timestamptz`) and match it.

- [ ] **Step 3: Run → fail**

Run: `go test ./pkg/server/ -run TestHandleMyConsent -v`
Expected: FAIL (undefined symbols).

- [ ] **Step 4: Implement**

`pkg/server/handle_me_consent.go`:
```go
package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/sirupsen/logrus"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/logx"
)

type consentMgmtQueries interface {
	ListConsentsByAccount(ctx context.Context, accountID int32) ([]db.ListConsentsByAccountRow, error)
	DeleteConsent(ctx context.Context, arg db.DeleteConsentParams) error
	GetEntityIconEtag(ctx context.Context, arg db.GetEntityIconEtagParams) (string, error)
}

func (s *Server) consentMgmtQueries() consentMgmtQueries {
	if s.consentMgmtOverride != nil {
		return s.consentMgmtOverride
	}
	return s.queries
}

type consentListOut struct {
	Body []contract.ConsentedApp
}

func (s *Server) handleListMyConsent(ctx context.Context, _ *struct{}) (*consentListOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	out, err := s.listConsents(ctx, sess.Account.ID)
	if err != nil {
		return nil, err
	}
	return &consentListOut{Body: out}, nil
}

func (s *Server) listConsents(ctx context.Context, accountID int32) ([]contract.ConsentedApp, error) {
	q := s.consentMgmtQueries()
	rows, err := q.ListConsentsByAccount(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("listConsents: %w", err)
	}
	out := make([]contract.ConsentedApp, 0, len(rows))
	for _, r := range rows {
		var iconURL *string
		if etag, e := q.GetEntityIconEtag(ctx, db.GetEntityIconEtagParams{OwnerKind: "oidc_client", OwnerID: r.ClientID}); e == nil {
			iconURL = entityIconURLPtr("oidc_client", r.ClientID, etag)
		}
		out = append(out, contract.ConsentedApp{
			ClientID:  r.ClientID,
			Name:      r.DisplayName,
			IconURL:   iconURL,
			Scopes:    append([]string(nil), r.GrantedScopes...),
			GrantedAt: r.UpdatedAt.Time,
		})
	}
	return out, nil
}

type revokeConsentIn struct {
	Body contract.RevokeConsentInput
}

func (s *Server) handleRevokeMyConsent(ctx context.Context, in *revokeConsentIn) (*struct{}, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	if in.Body.ClientID == "" {
		return nil, authErrToHuma(errors.New("clientId required"))
	}
	if err := s.revokeConsent(ctx, sess.Account.ID, in.Body.ClientID); err != nil {
		return nil, err
	}
	logx.WithContext(ctx).WithFields(logrus.Fields{
		"event":      "auth.consent_revoked_self",
		"account_id": sess.Account.ID,
		"client_id":  in.Body.ClientID,
	}).Info("auth")
	return &struct{}{}, nil
}

func (s *Server) revokeConsent(ctx context.Context, accountID int32, clientID string) error {
	if err := s.consentMgmtQueries().DeleteConsent(ctx, db.DeleteConsentParams{
		AccountID: accountID, ClientID: clientID,
	}); err != nil {
		return fmt.Errorf("revokeConsent: %w", err)
	}
	return nil
}
```
> Check `authErrToHuma`'s accepted type — if it requires a typed `authn` error rather than a bare `errors.New`, use the matching validation-error constructor (grep `authErrToHuma` usage in `pkg/server`). The `errors`/`fmt` imports: keep only those used.

- [ ] **Step 5: Register the routes**

In `pkg/server/server.go` `registerOperations()`, after the `/me/apps` registration:
```go
	registerOp(mgmt, contract.OperationListMyConsent, s.handleListMyConsent, sessionReq)
	registerOp(mgmt, contract.OperationRevokeConsent, s.handleRevokeMyConsent, sessionReq)
```

- [ ] **Step 6: Run → pass + build**

Run: `go test ./pkg/server/ -run TestHandleMyConsent -v && go build -tags nodynamic ./...`
Expected: PASS.

- [ ] **Step 7: Commit**
```bash
git add pkg/server/handle_me_consent.go pkg/server/handle_me_consent_test.go pkg/server/server.go
git commit -m "feat(launchpad): GET /me/consent + POST /me/consent/revoke"
```

---

## Task 7: Admin "Launch URL" field (backend + form)

**Goal:** Let admins set an OIDC client's `launch_url`.

**Files:**
- Modify: the admin OIDC update contract + handler (grep `func (s *Server) handleUpdateOIDCApplication` / the OIDC update operation in `pkg/contract`; likely `pkg/server/handle_admin_oidc_applications.go` and `pkg/contract/admin_*.go`)
- Modify: `dashboard/src/pages/admin/AdminOidcClientDetailView.vue`

**Acceptance Criteria:**
- [ ] The admin OIDC update request accepts an optional `launchUrl`; the handler persists it via `SetOIDCClientLaunchURL`.
- [ ] The admin OIDC detail form has a "Launch URL" input with a placeholder showing the derived default (origin of the first redirect URI).

**Verify:** `go build -tags nodynamic ./... && cd dashboard && npm run build`

**Steps:**

- [ ] **Step 1: Locate the admin OIDC update path**

Run: `grep -rn "UpdateOIDCClient\|handleUpdateOIDCApplication\|OperationUpdateOIDC" pkg/server pkg/contract | grep -iv test`
Identify the update contract struct (the request `Body`) and the handler.

- [ ] **Step 2: Add `launchUrl` to the update contract**

In the OIDC application update request body struct, add:
```go
	LaunchURL *string `json:"launchUrl,omitempty"`
```

- [ ] **Step 3: Persist in the handler**

After the existing `UpdateOIDCClient` call in the update handler, add:
```go
	// Persist the optional launch URL (NULL when cleared/omitted).
	var lu pgtype.Text
	if in.Body.LaunchURL != nil && strings.TrimSpace(*in.Body.LaunchURL) != "" {
		lu = pgtype.Text{String: strings.TrimSpace(*in.Body.LaunchURL), Valid: true}
	}
	if err := s.queries.SetOIDCClientLaunchURL(ctx, db.SetOIDCClientLaunchURLParams{
		ClientID: <clientID-var>, LaunchUrl: lu,
	}); err != nil {
		return nil, fmt.Errorf("set launch url: %w", err)
	}
```
(Use the handler's existing client-id variable; add `pgtype`/`strings` imports if missing.)

- [ ] **Step 4: Surface `launchUrl` on the admin GET (detail) response**

If the admin OIDC detail response is built from `GetOIDCClientAny`, map the new column:
```go
	if row.LaunchUrl.Valid {
		v.LaunchURL = &row.LaunchUrl.String
	}
```
Add `LaunchURL *string \`json:"launchUrl,omitempty"\`` to the detail view DTO.

- [ ] **Step 5: Add the form field**

In `AdminOidcClientDetailView.vue`, mirror an existing text field (e.g. display name) to add a Launch URL input bound to the form model, with placeholder `t('adminOidc.launchUrlPlaceholder')` and label `t('adminOidc.launchUrl')`; include it in the update payload as `launchUrl`. Add the two i18n keys to `en.ts` + `zh.ts` (see Task 11 for the i18n discipline).

- [ ] **Step 6: Build + commit**

Run: `go build -tags nodynamic ./... && cd dashboard && npm run build && cd ..`
```bash
git add pkg/server pkg/contract dashboard/src
git commit -m "feat(launchpad): admin-editable OIDC launch URL"
```

---

## Task 8: Smoke arc — `launchpad`

**Goal:** End-to-end coverage: authorized list, denial, consent list, revoke.

**Files:**
- Modify: `cmd/smoke/main.go`

**Acceptance Criteria:**
- [ ] A `launchpad N/M` arc: an authorized app appears in `GET /me/apps`; a denied (restricted, ungranted) app does not; after a `require_consent` authorize, `GET /me/consent` lists the app; `POST /me/consent/revoke` empties it.
- [ ] `mise run ci:smoke` → `SMOKE_EXIT=0`.

**Verify:** `mise run ci:smoke`

**Steps:**

- [ ] **Step 1: Add the arc**

Place a new `launchpad` arc after an existing end-user arc (e.g. after the `rbac` arc, reusing its created group/app/admin session). Follow the local-count header style (`launchpad %d/%d`, with a `const nLaunchpad = N`). Concrete checks to implement with the existing smoke HTTP helpers (`c.get`, `c.postJSON`):
```go
// launchpad 1/N — GET /me/apps includes an authorized app, excludes a denied one
var apps []struct {
	Kind, ID, Name, LaunchURL string
	IconURL                   *string `json:"iconUrl"`
}
if err := c.get("/api/prohibitorum/me/apps", &apps); err != nil {
	log.Fatalf("launchpad: GET /me/apps: %v", err)
}
// assert the authorized client_id is present and the denied one is absent.

// launchpad 2/N — GET /me/consent then POST /me/consent/revoke
var consents []struct{ ClientID string `json:"clientId"` }
if err := c.get("/api/prohibitorum/me/consent", &consents); err != nil { log.Fatalf(...) }
// (a require_consent authorize earlier in the OIDC arc recorded a consent;
//  assert it's listed, then:)
if err := c.postJSON("/api/prohibitorum/me/consent/revoke", map[string]any{"clientId": "<id>"}, nil); err != nil {
	log.Fatalf("launchpad: revoke: %v", err)
}
// re-GET /me/consent → assert the client is gone.
```
Use the same client/app/group the `rbac` arc already created so no new admin setup is needed. Update the final `✓ smoke OK — …` summary to add `+ launchpad (…)`.

- [ ] **Step 2: Run the smoke**

Run: `mise run ci:smoke`
Expected: `SMOKE_EXIT=0`, the `launchpad N/N` lines print.

- [ ] **Step 3: Commit**
```bash
git add cmd/smoke/main.go
git commit -m "test(launchpad): smoke arc for /me/apps + consent revoke"
```

---

## Task 9: Frontend — LauncherLayout + router restructure

**Goal:** Make `/` the launcher; keep all settings/admin URLs unchanged behind the sidebar shell.

**Files:**
- Create: `dashboard/src/pages/LauncherLayout.vue`
- Modify: `dashboard/src/router/index.ts`

**Acceptance Criteria:**
- [ ] `/` renders `LauncherLayout` → `MyAppsView` (created in Task 10); post-login/return default is `/`.
- [ ] `/security`, `/sessions`, `/connected`, `/devices`, `/admin/*` keep their exact URLs under `DashboardLayout`.
- [ ] `npm run build` typechecks.

**Verify:** `cd dashboard && npm run build`

**Steps:**

- [ ] **Step 1: Create LauncherLayout**

`dashboard/src/pages/LauncherLayout.vue` — minimal chrome: instance brand (from branding store) left, the existing account dropdown right (reuse the component used in `DashboardLayout.vue` — grep its `<script>` for the account-menu import), `<router-view />` for the page. Model the shell/skeleton on `DashboardLayout.vue` but without the sidebar:
```vue
<script setup lang="ts">
import { RouterView } from 'vue-router'
import { useBrandingStore } from '@/stores/branding'
// import the same account-dropdown component DashboardLayout uses
import AccountMenu from '@/components/custom/AccountMenu.vue' // ← match the real path/name from DashboardLayout
const branding = useBrandingStore()
</script>

<template>
  <div class="min-h-screen bg-canvas">
    <header class="flex items-center justify-between border-b border-line px-4 py-3 sm:px-6">
      <div class="flex items-center gap-2 font-semibold text-ink">
        <img v-if="branding.iconUrl" :src="branding.iconUrl" alt="" class="size-6 rounded" />
        <span>{{ branding.instanceName }}</span>
      </div>
      <AccountMenu />
    </header>
    <main class="mx-auto w-full max-w-5xl px-4 py-8 sm:px-6">
      <RouterView />
    </main>
  </div>
</template>
```
> Replace `AccountMenu` with the actual account-dropdown component + props used by `DashboardLayout.vue` (grep it). Use the branding store's real getters (`instanceName`, and the icon getter — grep `stores/branding.ts`).

- [ ] **Step 2: Restructure the routes**

In `dashboard/src/router/index.ts`, replace the single `path: '/'` dashboard record with **two** records: a launcher at `/` and the existing shell behind a non-root parent with **absolute** child paths (URLs unchanged). Replace the block from `{ path: '/', component: () => import('../pages/DashboardLayout.vue'), … children: [...] }`:
```ts
  // Launcher shell — the end-user home.
  {
    path: '/',
    component: () => import('../pages/LauncherLayout.vue'),
    meta: { requiresAuth: true },
    children: [
      { path: '', name: 'my-apps', component: () => import('../pages/MyAppsView.vue'), meta: { titleKey: 'title.myApps' } },
    ],
  },
  // Settings/admin shell — absolute child paths keep existing URLs.
  {
    path: '/account', // internal parent; never navigated to directly
    component: () => import('../pages/DashboardLayout.vue'),
    meta: { requiresAuth: true },
    children: [
      { path: '/sessions', name: 'sessions', component: () => import('../pages/SessionsView.vue'), meta: { titleKey: 'title.sessions' } },
      { path: '/security', name: 'security', component: () => import('../pages/SecurityView.vue'), meta: { titleKey: 'title.security' } },
      { path: '/connected', name: 'connected', component: () => import('../pages/ConnectedAccountsView.vue'), meta: { titleKey: 'title.connected' } },
      { path: '/devices', name: 'devices', component: () => import('../pages/DevicesView.vue'), meta: { titleKey: 'title.devices' } },
      { path: '/app-access', name: 'app-access', component: () => import('../pages/AppAccessView.vue'), meta: { titleKey: 'title.appAccess' } },
      // …all existing /admin/* children, each with its path prefixed by '/'
      { path: '/admin/accounts', name: 'admin-accounts', component: () => import('../pages/admin/AdminAccountsView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminAccounts' } },
      // … (repeat for every existing admin child, adding the leading '/')
    ],
  },
```
Keep every existing admin child exactly as before but with a leading `/` on its `path`. Remove the old `{ path: '', redirect: { name: 'security' } }` index.

- [ ] **Step 3: Typecheck**

Run: `cd dashboard && npm run build`
Expected: typecheck passes (will fail until `MyAppsView.vue`/`AppAccessView.vue` exist — create stubs now or land Tasks 10–11 before building). Create minimal stubs if needed:
```vue
<template><div /></template>
```

- [ ] **Step 4: Commit**
```bash
git add dashboard/src/pages/LauncherLayout.vue dashboard/src/router/index.ts
git commit -m "feat(launchpad): LauncherLayout + router restructure (/ = home)"
```

---

## Task 10: Frontend — MyAppsView + AppTile (+ vitest)

**Goal:** The tile grid: launch (new tab), type chip, consent glyph, kebab Details/Revoke; empty state.

**Files:**
- Create: `dashboard/src/pages/MyAppsView.vue`
- Create: `dashboard/src/components/custom/AppTile.vue`
- Test: `dashboard/src/components/custom/AppTile.test.ts`

**Acceptance Criteria:**
- [ ] `MyAppsView` loads `GET /me/apps` + `GET /me/consent`, renders a tile per app, greeting heading, empty state when none.
- [ ] `AppTile` launches in a new tab (`target="_blank" rel="noopener"`), shows the type chip from `kind`, shows the consent glyph only when consent exists, and the kebab offers Revoke only when consent exists.
- [ ] vitest passes.

**Verify:** `cd dashboard && npm test -- AppTile`

**Steps:**

- [ ] **Step 1: Write AppTile**

`dashboard/src/components/custom/AppTile.vue`:
```vue
<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/custom/AppIcon.vue'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem,
} from '@/components/ui/dropdown-menu'
import { MoreVertical, KeyRound } from 'lucide-vue-next'

export interface LaunchpadApp { kind: 'oidc' | 'forward_auth' | 'saml'; id: string; name: string; iconUrl?: string | null; launchUrl: string }
export interface ConsentInfo { scopes: string[] }

const props = defineProps<{ app: LaunchpadApp; consent?: ConsentInfo | null }>()
const emit = defineEmits<{ (e: 'revoke', app: LaunchpadApp): void; (e: 'details', app: LaunchpadApp): void }>()
const { t } = useI18n()

const typeLabel = computed(() => t(`myApps.type.${props.app.kind}`))
const hasConsent = computed(() => !!props.consent)
</script>

<template>
  <div class="group relative flex flex-col gap-3 rounded-lg border border-line bg-card p-4 transition hover:border-ink/30 hover:shadow-sm">
    <div class="absolute right-2 top-2 opacity-0 transition group-focus-within:opacity-100 group-hover:opacity-100">
      <DropdownMenu>
        <DropdownMenuTrigger as-child>
          <Button variant="ghost" size="icon" class="size-7" :aria-label="t('myApps.menu')" :data-test="`menu-${app.id}`">
            <MoreVertical class="size-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem :data-test="`details-${app.id}`" @select="emit('details', app)">{{ t('myApps.details') }}</DropdownMenuItem>
          <DropdownMenuItem v-if="hasConsent" :data-test="`revoke-${app.id}`" @select="emit('revoke', app)">{{ t('myApps.revoke') }}</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>

    <a :href="app.launchUrl" target="_blank" rel="noopener" class="flex flex-col items-center gap-3 text-center" :data-test="`launch-${app.id}`">
      <AppIcon :src="app.iconUrl" :name="app.name" size="md" />
      <span class="min-w-0 truncate font-medium text-ink">{{ app.name }}</span>
    </a>

    <div class="flex items-center justify-center gap-2 text-xs text-muted">
      <span class="rounded bg-accent px-1.5 py-0.5">{{ typeLabel }}</span>
      <KeyRound v-if="hasConsent" class="size-3.5" :aria-label="t('myApps.consentGranted')" :data-test="`consent-${app.id}`" />
    </div>
  </div>
</template>
```
> If the kebab `DropdownMenuItem` uses `@click` rather than `@select` in this shadcn-vue version, match the existing usage (grep an admin view that uses `dropdown-menu`).

- [ ] **Step 2: Write MyAppsView**

`dashboard/src/pages/MyAppsView.vue`:
```vue
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useAuthStore } from '@/stores/auth'
import AppTile, { type LaunchpadApp } from '@/components/custom/AppTile.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import { Alert, AlertDescription } from '@/components/ui/alert'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'

interface Consent { clientId: string; scopes: string[] }

const { t } = useI18n()
const { busy, run, errorText } = useApi()
const auth = useAuthStore()

const apps = ref<LaunchpadApp[]>([])
const consents = ref<Map<string, Consent>>(new Map())
const revokeTarget = ref<LaunchpadApp | null>(null)

const firstName = computed(() => (auth.me?.displayName ?? '').split(' ')[0] || auth.me?.username || '')

function consentFor(app: LaunchpadApp): Consent | null {
  return app.kind === 'oidc' ? consents.value.get(app.id) ?? null : null
}

async function load(): Promise<void> {
  const [a, c] = await Promise.all([
    run(() => api.get<LaunchpadApp[]>('/api/prohibitorum/me/apps')),
    api.get<Consent[]>('/api/prohibitorum/me/consent').catch(() => [] as Consent[]),
  ])
  if (a) apps.value = a
  consents.value = new Map(c.map((x) => [x.clientId, x]))
}

async function confirmRevoke(): Promise<void> {
  const app = revokeTarget.value
  if (!app) return
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/consent/revoke', { clientId: app.id })
    return true as const
  })
  revokeTarget.value = null
  if (ok) await load()
}

onMounted(load)
</script>

<template>
  <div class="flex flex-col gap-6">
    <div>
      <p class="text-sm text-muted">{{ t('myApps.greeting', { name: firstName }) }}</p>
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('myApps.title') }}</h1>
    </div>

    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
      <AlertDescription>{{ errorText }}</AlertDescription>
    </Alert>

    <TableSkeleton v-if="busy && !apps.length" :rows="2" :cols="3" />
    <div v-else-if="apps.length" class="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4">
      <AppTile
        v-for="app in apps" :key="`${app.kind}:${app.id}`"
        :app="app" :consent="consentFor(app)"
        @revoke="revokeTarget = $event"
        @details="/* open details popover — minimal: no-op or alert scopes (see note) */ undefined"
      />
    </div>
    <EmptyState v-else-if="!errorText" :title="t('myApps.empty')" :description="t('myApps.emptyHelp')" />

    <ConfirmDialog
      :open="revokeTarget !== null"
      :title="t('myApps.revokeConfirmTitle')"
      :confirm-label="t('myApps.revoke')"
      :busy="busy"
      @update:open="(v) => { if (!v) revokeTarget = null }"
      @cancel="revokeTarget = null"
      @confirm="confirmRevoke"
    >
      {{ t('myApps.revokeConfirmBody', { name: revokeTarget?.name }) }}
    </ConfirmDialog>
  </div>
</template>
```
> "Details" is minimal in v1: either a small popover listing `consentFor(app)?.scopes`, or omit the Details item entirely if scope display isn't wanted yet. If omitting, drop the `details` emit + menu item from `AppTile`. Keep the kebab if Revoke is present.

- [ ] **Step 3: Write the AppTile test**

`dashboard/src/components/custom/AppTile.test.ts`:
```ts
import { mount } from '@vue/test-utils'
import { describe, it, expect } from 'vitest'
import AppTile from './AppTile.vue'
import { i18nTestPlugin } from '@/test/i18n' // ← reuse the repo's vitest i18n helper; match its real path

const base = { id: 'grafana', name: 'Grafana', kind: 'oidc' as const, launchUrl: 'https://grafana.example/', iconUrl: null }

function mountTile(props: Record<string, unknown>) {
  return mount(AppTile, { props, global: { plugins: [i18nTestPlugin] } })
}

describe('AppTile', () => {
  it('launches in a new tab', () => {
    const w = mountTile({ app: base })
    const a = w.get('[data-test="launch-grafana"]')
    expect(a.attributes('href')).toBe('https://grafana.example/')
    expect(a.attributes('target')).toBe('_blank')
    expect(a.attributes('rel')).toContain('noopener')
  })
  it('hides the consent glyph when no consent', () => {
    const w = mountTile({ app: base, consent: null })
    expect(w.find('[data-test="consent-grafana"]').exists()).toBe(false)
  })
  it('shows the consent glyph when consent exists', () => {
    const w = mountTile({ app: base, consent: { scopes: ['openid'] } })
    expect(w.find('[data-test="consent-grafana"]').exists()).toBe(true)
  })
})
```
> Match how existing vitest specs bootstrap i18n (grep `dashboard/src/**/*.test.ts` for `createI18n`/a shared test plugin) and adapt the `global.plugins` accordingly.

- [ ] **Step 4: Run tests + typecheck**

Run: `cd dashboard && npm test -- AppTile && npm run build`
Expected: PASS.

- [ ] **Step 5: Commit**
```bash
git add dashboard/src/components/custom/AppTile.vue dashboard/src/components/custom/AppTile.test.ts dashboard/src/pages/MyAppsView.vue
git commit -m "feat(launchpad): My apps tile grid + AppTile"
```

---

## Task 11: Frontend — App access page + nav + i18n

**Goal:** The Settings "App access" page (consent list + revoke), the sidebar nav entry, the launcher↔settings navigation, and all i18n keys at parity.

**Files:**
- Create: `dashboard/src/pages/AppAccessView.vue`
- Test: `dashboard/src/pages/AppAccessView.test.ts`
- Modify: `dashboard/src/components/custom/AppSidebar.vue` (add "App access" under the account/settings group; ensure brand/logo → `/`)
- Modify: `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts`

**Acceptance Criteria:**
- [ ] `/app-access` lists consents + revoke (reusing `GET /me/consent` + `POST /me/consent/revoke`); empty state.
- [ ] Sidebar shows "App access"; the launcher's account menu links to Settings (→ `/security`) and Admin (admins only); brand/logo returns to `/`.
- [ ] `en.ts`/`zh.ts` have all new keys; `locales.parity.test.ts` passes.

**Verify:** `cd dashboard && npm test && npm run build`

**Steps:**

- [ ] **Step 1: Write AppAccessView**

`dashboard/src/pages/AppAccessView.vue` — model on `ConnectedAccountsView.vue` (list of Cards with `AppIcon` + name + a Revoke button + `ConfirmDialog`; `EmptyState` when none). Data: `GET /api/prohibitorum/me/consent`; revoke: `POST /api/prohibitorum/me/consent/revoke` `{ clientId }` (no `withSudo`). Show `scopes` as small muted text. Use keys under `appAccess.*`.

- [ ] **Step 2: Add the nav entry + i18n keys**

In `AppSidebar.vue`, add an "App access" item (route name `app-access`) to the account/settings group (mirror the existing `connected`/`devices` items). Add to `en.ts`:
```ts
// nav: { … , appAccess: 'App access' }
// title: { … , myApps: 'My apps', appAccess: 'App access' }
myApps: {
  title: 'My apps',
  greeting: 'Welcome back, {name}',
  empty: 'No apps yet',
  emptyHelp: "When an admin grants you access to an app, it'll show up here.",
  menu: 'App options',
  details: 'Details',
  revoke: 'Revoke access',
  consentGranted: 'You have granted this app access',
  revokeConfirmTitle: 'Revoke access?',
  revokeConfirmBody: "{name} will ask for your consent again next time you sign in.",
  type: { oidc: 'OIDC', forward_auth: 'Forward-auth', saml: 'SAML' },
},
appAccess: {
  title: 'Apps with access to your account',
  help: 'Apps you have approved can use your account until you revoke them.',
  empty: "Apps you've approved will appear here.",
  scopes: 'Scopes',
  revoke: 'Revoke',
  revokeConfirmTitle: 'Revoke access?',
  revokeConfirmBody: '{name} will ask for your consent again next time you sign in.',
},
adminOidc: { /* …existing… */ launchUrl: 'Launch URL', launchUrlPlaceholder: 'Defaults to the first redirect URI’s origin' },
```
Add the mirrored Chinese strings to `zh.ts` (same keys). Run `npm test -- locales` to confirm parity.

- [ ] **Step 3: Write AppAccessView test**

`dashboard/src/pages/AppAccessView.test.ts` — mount with a mocked `api.get` returning one consent; assert the row renders and clicking Revoke (then confirming) calls `api.post('/api/prohibitorum/me/consent/revoke', { clientId })`. Mirror an existing page test that mocks `@/lib/api`.

- [ ] **Step 4: Run + typecheck**

Run: `cd dashboard && npm test && npm run build`
Expected: PASS (incl. parity test).

- [ ] **Step 5: Commit**
```bash
git add dashboard/src/pages/AppAccessView.vue dashboard/src/pages/AppAccessView.test.ts dashboard/src/components/custom/AppSidebar.vue dashboard/src/locales
git commit -m "feat(launchpad): App access page + nav + i18n"
```

---

## Task 12: Wire-up verification, bundle, README, full gate

**Goal:** Ship-ready: embedded bundle rebuilt, README updated, all gates green end-to-end.

**Files:**
- Modify: `pkg/webui/dist` (rebuilt)
- Modify: `README.md` (check the launchpad box)

**Acceptance Criteria:**
- [ ] `/` (signed in) shows the launcher with tiles; `/app-access` lists consents; all existing settings/admin URLs still work.
- [ ] Full gate green; smoke `SMOKE_EXIT=0`; bundle committed.

**Verify:** `go build -tags nodynamic ./... && go vet ./... && go test ./... && (cd dashboard && npm test && npm run build) && mise run ci:smoke`

**Steps:**

- [ ] **Step 1: Manual sanity (dev server)**

Run `mise run dev:server` + `mise run dev:enroll-admin -- --new`, sign in, and confirm: `/` renders the launcher; a seeded/authorized app tile launches in a new tab; restricted-but-ungranted apps are absent; `/security` etc. unchanged; `/app-access` lists/【revokes】 a consent (drive one via the consent flow or `dev:seed`).

- [ ] **Step 2: Rebuild the embedded bundle**

Run: `mise run build:web`
```bash
git add pkg/webui/dist
```

- [ ] **Step 3: Tick the README box**

In `README.md`, change `- [ ] End-user app launchpad — launch authorized apps and self-manage access` to `- [x] …`.

- [ ] **Step 4: Full gate**

Run: `go build -tags nodynamic ./... && go vet ./... && go test ./... && (cd dashboard && npm test && npm run build) && mise run ci:smoke`
Expected: all PASS; `SMOKE_EXIT=0`.

- [ ] **Step 5: Commit**
```bash
git add README.md pkg/webui/dist
git commit -m "feat(launchpad): rebuild bundle, mark README done"
```

---

## Self-review notes (for the implementer)

- **sqlc row types:** exact generated field names/types (`LaunchUrl pgtype.Text`, `ForwardAuthHost pgtype.Text`, `GrantedScopes []string`, `UpdatedAt pgtype.Timestamptz`, `ID int64` for `saml_sp`) come from `mise exec -- sqlc generate` — open `pkg/db/*.sql.go` after Task 1–2 and match the test/handler code to the real types (adjust `int64(...)` conversions and `pgType` shapes).
- **`authErrToHuma` input:** confirm whether it takes a typed `authn` error; if so, replace the bare `errors.New("clientId required")` with the repo's validation-error constructor.
- **dropdown-menu API:** `@select` vs `@click` on `DropdownMenuItem` varies by shadcn-vue version — match an existing admin view that uses `@/components/ui/dropdown-menu`.
- **Router absolute children:** verify in the browser that `/security` and `/admin/*` still resolve and `/` renders the launcher (two layout records, one owning `/`).
- **No new sudo:** revoke is intentionally not `withSudo`-wrapped (spec decision).
