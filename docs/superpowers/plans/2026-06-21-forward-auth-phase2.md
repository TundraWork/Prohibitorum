# Forward-Auth Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the operator-facing surface for forward-auth services — a dashboard admin UI (list/create/edit/delete + RBAC + a copy-paste Traefik snippet), a per-app + SSO sign-out path, and a local multi-domain dev harness — on top of the settled Phase 1 backend.

**Architecture:** A forward-auth app is an `oidc_client` with `forward_auth_enabled=true`. New admin endpoints mirror the OIDC-application handlers; the FA section is presented separately and FA apps are excluded from the OIDC list/detail. Per-service RBAC reuses the existing `/oidc-applications/{id}/access/*` endpoints unchanged. Sign-out is a two-hop bounce (clear per-domain cookie on the app domain → terminate SSO session on the IdP domain) with a fail-closed open-redirect guard.

**Tech Stack:** Go (chi + huma + sqlc/pgx), Vue 3 + Vite + Tailwind v4 + shadcn-vue, vue-i18n (en + zh).

**Spec:** `docs/superpowers/specs/2026-06-21-forward-auth-phase2-design.md`

**Gate (every task):** `go vet ./... && go build -tags nodynamic ./... && go test ./...`; frontend tasks add `cd dashboard && npm run test`, `npx vue-tsc -b`, `node scripts/check-contrast.mjs`, and a **dist rebuild + commit** (`npm run build` → commit `pkg/webui/dist`). NEVER add a `Co-Authored-By` trailer. Work continues on `master`. `pkg/server` tests flake ~1/3 under parallel shared-DB runs (memory `reference_flaky_server_suite`) — re-run in isolation if a failure looks DB-contention-related; don't block on it.

---

### Task 1: FA backend data layer — queries, contract, shared create helper, CLI refactor

**Goal:** Add the SQL queries, the wire-view + huma read Operations, and the single-source-of-truth `RegisterForwardAuthApp` helper, and route the CLI `forward-auth-app create` through it.

**Files:**
- Modify: `db/queries/oidc.sql` (4 new queries)
- Regenerate: `pkg/db/oidc.sql.go`, `pkg/db/querier.go` (via `sqlc generate`)
- Modify: `pkg/protocol/oidc/forward_auth.go` (add `ForwardAuthCallbackURI` + `RegisterForwardAuthApp`)
- Modify: `cmd/prohibitorum/main.go:1731-1776` (refactor create `Run` to call the helper)
- Modify: `pkg/contract/auth.go` (add `ForwardAuthAppView` + 2 huma Operations)
- Test: `pkg/protocol/oidc/forward_auth_test.go` (helper test + fake querier extensions)

**Acceptance Criteria:**
- [ ] `sqlc generate` produces `ListForwardAuthClients`, `GetForwardAuthAppByID`, `UpdateForwardAuthApp`, `ListNonForwardAuthOIDCClients` with no other diff.
- [ ] `RegisterForwardAuthApp` builds a public PKCE client (`token_endpoint_auth_method="none"`), `require_consent=false`, scopes `openid email groups`, `redirect_uris=[https://<host>/.prohibitorum-forward-auth/callback]`, then sets `forward_auth_enabled=true` + host.
- [ ] The CLI `forward-auth-app create` produces identical output to before (uses the helper).
- [ ] `go build -tags nodynamic ./...` passes.

**Verify:** `go build -tags nodynamic ./... && go test ./pkg/protocol/oidc/ -run 'RegisterForwardAuth' -v` → PASS

**Steps:**

- [ ] **Step 1: Add the four queries to `db/queries/oidc.sql`** (append after the existing `SetForwardAuthConfig` block, ~line 100)

```sql
-- name: ListForwardAuthClients :many
SELECT client_id, display_name, forward_auth_host, access_restricted, disabled, created_at
FROM oidc_client
WHERE forward_auth_enabled = true
ORDER BY created_at DESC;

-- name: GetForwardAuthAppByID :one
SELECT client_id, display_name, forward_auth_host, access_restricted, disabled, created_at
FROM oidc_client
WHERE client_id = $1 AND forward_auth_enabled = true;

-- name: UpdateForwardAuthApp :one
UPDATE oidc_client
SET display_name = $2, redirect_uris = $3, forward_auth_host = $4
WHERE client_id = $1 AND forward_auth_enabled = true
RETURNING client_id, display_name, forward_auth_host, access_restricted, disabled, created_at;

-- name: ListNonForwardAuthOIDCClients :many
SELECT client_id, display_name, redirect_uris, allowed_scopes,
       token_endpoint_auth_method, disabled, access_restricted, created_at
FROM oidc_client
WHERE forward_auth_enabled = false
ORDER BY created_at DESC;
```

- [ ] **Step 2: Regenerate sqlc** — Run: `sqlc generate` (from repo root; sqlc 1.30.0 via mise). Confirm new methods appear in `pkg/db/querier.go` and `pkg/db/oidc.sql.go`. Note the generated row types: `ListForwardAuthClientsRow`, `GetForwardAuthAppByIDRow`, `UpdateForwardAuthAppRow` (all carry `ClientID, DisplayName, ForwardAuthHost pgtype.Text, AccessRestricted, Disabled, CreatedAt pgtype.Timestamptz`), `UpdateForwardAuthAppParams` (`ClientID, DisplayName, RedirectUris []string, ForwardAuthHost pgtype.Text`), and `ListNonForwardAuthOIDCClientsRow`.

- [ ] **Step 3: Add the helper + URI builder to `pkg/protocol/oidc/forward_auth.go`** (the file already imports `context`, `net/url`, `time`, `pgtype`, `db`; add this after `pkceChallengeS256`)

```go
// ForwardAuthCallbackURI returns the fixed callback redirect_uri for a
// forward-auth app on host. The /oauth/authorize exact-match guard depends on
// this exact value, so it is derived in one place and reused by create + edit.
func ForwardAuthCallbackURI(host string) string {
	return "https://" + host + ForwardAuthPathPrefix + "/callback"
}

// RegisterForwardAuthApp provisions the backing OIDC client for a Traefik
// ForwardAuth application: a public (PKCE, no secret) client with
// require_consent=false, scopes "openid email groups", and the fixed
// forward-auth callback redirect_uri, then flags it as forward-auth + host.
// Shared by the `forward-auth-app create` CLI and the admin HTTP handler so the
// FA client shape has a single source of truth. Returns the inserted client row
// (before the forward-auth flag/host are set — callers that need the FA columns
// should re-read or build the view from the known host).
func RegisterForwardAuthApp(ctx context.Context, q db.Querier, clientID, host, displayName string) (db.OidcClient, error) {
	params, _, err := BuildClientParams(ClientOptions{
		ClientID:               clientID,
		DisplayName:            displayName,
		RedirectURIs:           []string{ForwardAuthCallbackURI(host)},
		PostLogoutRedirectURIs: []string{},
		Scopes:                 []string{"openid", "email", "groups"},
		Public:                 true,
		RequireConsent:         false,
	})
	if err != nil {
		return db.OidcClient{}, err
	}
	c, err := q.InsertOIDCClient(ctx, params)
	if err != nil {
		return db.OidcClient{}, err
	}
	if err := q.SetForwardAuthConfig(ctx, db.SetForwardAuthConfigParams{
		ClientID:           clientID,
		ForwardAuthEnabled: true,
		ForwardAuthHost:    pgtype.Text{String: host, Valid: true},
	}); err != nil {
		return db.OidcClient{}, err
	}
	return c, nil
}
```

- [ ] **Step 4: Refactor the CLI create `Run` body** in `cmd/prohibitorum/main.go` (replace the `BuildClientParams`/`InsertOIDCClient`/`SetForwardAuthConfig` block at ~1745-1768 with the helper call; keep the printout)

```go
			redirectURI := oidc.ForwardAuthCallbackURI(faHost)

			q, conn := mustOpenDB(ctx)
			defer conn.Close()

			if _, err := oidc.RegisterForwardAuthApp(ctx, q, faClientID, faHost, faDisplayName); err != nil {
				log.Fatalf("forward-auth-app create: %v", err)
			}

			fmt.Printf("Registered ForwardAuth application %q\n", faClientID)
			fmt.Printf("  Redirect URI:  %s\n", redirectURI)
			fmt.Printf("  Host:          %s\n", faHost)
			fmt.Printf("\nNext steps:\n")
			fmt.Printf("  1. Grant access:  prohibitorum oidc-client access --client-id %s --grant-group <slug>\n", faClientID)
			fmt.Printf("  2. Configure Traefik ForwardAuth per docs/forward-auth.md\n")
```

(The `redirectURI` local at line 1740 is now built via `oidc.ForwardAuthCallbackURI`; remove the old inline string build. The `pgtype` import in main.go may become unused — if `go build` flags it, drop it.)

- [ ] **Step 5: Add the contract view + huma Operations to `pkg/contract/auth.go`** (after the `OperationGetOIDCApplication` block, ~line 470; the file already imports `huma`, `net/http`, `time`)

```go
// ForwardAuthAppView is the admin-facing projection of a forward-auth
// application (an oidc_client with forward_auth_enabled=true). Forward-auth
// clients are public (PKCE) and carry no secret, so there is no secret material
// to leak.
type ForwardAuthAppView struct {
	ClientID         string    `json:"clientId"`
	DisplayName      string    `json:"displayName"`
	ForwardAuthHost  string    `json:"forwardAuthHost"`
	AccessRestricted bool      `json:"accessRestricted"`
	Disabled         bool      `json:"disabled"`
	CreatedAt        time.Time `json:"createdAt"`
}

var OperationListForwardAuthApps = huma.Operation{
	OperationID: "listForwardAuthApps",
	Method:      http.MethodGet,
	Path:        "/forward-auth-apps",
	Summary:     "List all forward-auth applications (admin only).",
}

var OperationGetForwardAuthApp = huma.Operation{
	OperationID: "getForwardAuthApp",
	Method:      http.MethodGet,
	Path:        "/forward-auth-apps/{clientId}",
	Summary:     "Get one forward-auth application by client_id (admin only).",
}
```

- [ ] **Step 6: Extend the fake querier in `pkg/protocol/oidc/forward_auth_test.go`** (add capture fields + two methods, after the existing `ListExposedGroupSlugsByAccount` method ~line 136)

```go
// Capture fields for RegisterForwardAuthApp tests.
func (f *fakeFAQueries) InsertOIDCClient(_ context.Context, p db.InsertOIDCClientParams) (db.OidcClient, error) {
	f.insertParams = &p
	return db.OidcClient{ClientID: p.ClientID, DisplayName: p.DisplayName}, nil
}

func (f *fakeFAQueries) SetForwardAuthConfig(_ context.Context, p db.SetForwardAuthConfigParams) error {
	f.faConfigParams = &p
	return nil
}
```

Add the two fields to the `fakeFAQueries` struct (after `groups []string`):

```go
	// Captured params for RegisterForwardAuthApp tests.
	insertParams   *db.InsertOIDCClientParams
	faConfigParams *db.SetForwardAuthConfigParams
```

- [ ] **Step 7: Write the helper test** (append to `pkg/protocol/oidc/forward_auth_test.go`)

```go
func TestRegisterForwardAuthApp_BuildsPublicPKCEClient(t *testing.T) {
	f := &fakeFAQueries{}
	_, err := RegisterForwardAuthApp(context.Background(), f, "fa-client", "app.example.test", "App")
	if err != nil {
		t.Fatalf("RegisterForwardAuthApp: %v", err)
	}
	if f.insertParams == nil || f.faConfigParams == nil {
		t.Fatal("expected InsertOIDCClient and SetForwardAuthConfig to be called")
	}
	if got := f.insertParams.TokenEndpointAuthMethod; got != "none" {
		t.Errorf("token_endpoint_auth_method = %q, want \"none\" (public)", got)
	}
	if f.insertParams.RequireConsent {
		t.Error("require_consent must be false")
	}
	wantURI := "https://app.example.test/.prohibitorum-forward-auth/callback"
	if len(f.insertParams.RedirectUris) != 1 || f.insertParams.RedirectUris[0] != wantURI {
		t.Errorf("redirect_uris = %v, want [%q]", f.insertParams.RedirectUris, wantURI)
	}
	if !f.faConfigParams.ForwardAuthEnabled {
		t.Error("forward_auth_enabled must be true")
	}
	if f.faConfigParams.ForwardAuthHost.String != "app.example.test" {
		t.Errorf("forward_auth_host = %q", f.faConfigParams.ForwardAuthHost.String)
	}
}
```

- [ ] **Step 8: Run the gate** — Run: `go build -tags nodynamic ./... && go test ./pkg/protocol/oidc/ -run 'RegisterForwardAuth' -v` → PASS

- [ ] **Step 9: Commit**

```bash
git add db/queries/oidc.sql pkg/db/ pkg/protocol/oidc/forward_auth.go pkg/protocol/oidc/forward_auth_test.go cmd/prohibitorum/main.go pkg/contract/auth.go
git commit -m "feat(forward-auth): FA data layer — queries, view, shared create helper, CLI refactor"
```

---

### Task 2: FA admin HTTP endpoints, routes, and OIDC-list exclusion guards

**Goal:** Add the admin list/get/create/edit/set-disabled/delete endpoints, register them, exclude FA apps from the OIDC list/detail/edit/rotate, and register the new sudo routes in the cross-cutting policy test.

**Files:**
- Create: `pkg/server/handle_admin_forward_auth_apps.go`
- Create: `pkg/server/handle_admin_forward_auth_apps_test.go`
- Modify: `pkg/server/server.go:463-470` (register FA routes after the OIDC block)
- Modify: `pkg/server/handle_admin_oidc_clients.go` (list query swap + FA guards on GET/PUT/rotate)
- Modify: `pkg/server/admin_route_policy_test.go` (add 3 FA sudo routes to `sudoGatedRoutes`)

**Acceptance Criteria:**
- [ ] `GET /forward-auth-apps` lists only FA apps; the OIDC list (`GET /oidc-applications`) excludes FA apps.
- [ ] `GET /oidc-applications/{id}`, `PUT /oidc-applications/{id}`, and `rotate-secret` return `client_not_found` for an FA client id.
- [ ] Create (with host conflict → 409), edit (host change re-derives redirect_uri), set-disabled, delete all work.
- [ ] `TestAdminMutationRoutesRequireSudo` passes with the 3 new FA routes listed.

**Verify:** `go build -tags nodynamic ./... && go vet ./... && go test ./pkg/server/ -run 'ForwardAuth|AdminMutationRoutesRequireSudo' -v` → PASS

**Steps:**

- [ ] **Step 1: Create `pkg/server/handle_admin_forward_auth_apps.go`**

```go
// Package server — handle_admin_forward_auth_apps.go
//
// Admin forward-auth application endpoints. A forward-auth app is an oidc_client
// with forward_auth_enabled=true; it is presented as its own section and
// excluded from the OIDC-applications list (see handle_admin_oidc_clients.go).
// Per-service RBAC reuses the OIDC app-access endpoints
// (/oidc-applications/{clientId}/access/*) unchanged.
//
// Reads are typed (registerOp); mutations are raw and sudo-gated via
// registerSudoOpHTTP (create/update/delete) — except set-disabled which mirrors
// the OIDC set-disabled (admin-only, no sudo). Handlers must NOT call
// requireFreshSudo themselves.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
	"prohibitorum/pkg/db"
	oidc "prohibitorum/pkg/protocol/oidc"
)

// forwardAuthAppView projects the common FA columns into the wire view. Shared
// by every FA row shape (list/get/update) since they select the same columns.
func forwardAuthAppView(clientID, displayName string, host pgtype.Text, accessRestricted, disabled bool, createdAt pgtype.Timestamptz) contract.ForwardAuthAppView {
	v := contract.ForwardAuthAppView{
		ClientID:         clientID,
		DisplayName:      displayName,
		ForwardAuthHost:  host.String, // "" when !Valid
		AccessRestricted: accessRestricted,
		Disabled:         disabled,
	}
	if createdAt.Valid {
		v.CreatedAt = createdAt.Time
	}
	return v
}

func faActorID(ctx context.Context) *int32 {
	if sess := authn.SessionFromContext(ctx); sess != nil {
		return &sess.Account.ID
	}
	return nil
}

// ----- GET /forward-auth-apps (typed, role-only) -----------------------------

type listForwardAuthAppsOut struct {
	Body []contract.ForwardAuthAppView
}

func (s *Server) handleListForwardAuthApps(ctx context.Context, _ *struct{}) (*listForwardAuthAppsOut, error) {
	rows, err := s.queries.ListForwardAuthClients(ctx)
	if err != nil {
		return nil, fmt.Errorf("handler: listForwardAuthApps: %w", err)
	}
	views := make([]contract.ForwardAuthAppView, 0, len(rows))
	for _, r := range rows {
		views = append(views, forwardAuthAppView(r.ClientID, r.DisplayName, r.ForwardAuthHost, r.AccessRestricted, r.Disabled, r.CreatedAt))
	}
	return &listForwardAuthAppsOut{Body: views}, nil
}

// ----- GET /forward-auth-apps/{clientId} (typed, role-only) ------------------

type getForwardAuthAppIn struct {
	ClientID string `path:"clientId"`
}

type forwardAuthAppOut struct {
	Body contract.ForwardAuthAppView
}

func (s *Server) handleGetForwardAuthApp(ctx context.Context, in *getForwardAuthAppIn) (*forwardAuthAppOut, error) {
	r, err := s.queries.GetForwardAuthAppByID(ctx, in.ClientID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrClientNotFound())
		}
		return nil, fmt.Errorf("handleGetForwardAuthApp: %w", err)
	}
	return &forwardAuthAppOut{Body: forwardAuthAppView(r.ClientID, r.DisplayName, r.ForwardAuthHost, r.AccessRestricted, r.Disabled, r.CreatedAt)}, nil
}

// ----- POST /forward-auth-apps (raw, sudo-gated) -----------------------------

type createForwardAuthAppBody struct {
	ClientID    string `json:"clientId"`
	Host        string `json:"host"`
	DisplayName string `json:"displayName"`
}

func (s *Server) handleCreateForwardAuthAppHTTP(w http.ResponseWriter, r *http.Request) {
	var body createForwardAuthAppBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.ClientID == "" || body.Host == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	c, err := oidc.RegisterForwardAuthApp(r.Context(), s.queries, body.ClientID, body.Host, body.DisplayName)
	if err != nil {
		if isUniqueViolation(err) {
			writeAuthErr(w, authn.ErrClientAlreadyExists())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleCreateForwardAuthApp: %w", err))
		return
	}

	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: faActorID(r.Context()),
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventRegister,
		Detail:    map[string]any{"client_id": body.ClientID, "forward_auth": true, "host": body.Host},
	})

	// c is the row returned by InsertOIDCClient (before the FA flag/host are set),
	// so build the view from the known create-time values: a fresh FA app is
	// never access-restricted and is enabled.
	view := contract.ForwardAuthAppView{
		ClientID:        c.ClientID,
		DisplayName:     c.DisplayName,
		ForwardAuthHost: body.Host,
		Disabled:        c.Disabled,
	}
	if c.CreatedAt.Valid {
		view.CreatedAt = c.CreatedAt.Time
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(view)
}

// ----- PUT /forward-auth-apps/{clientId} (raw, sudo-gated) -------------------

type updateForwardAuthAppBody struct {
	DisplayName string `json:"displayName"`
	Host        string `json:"host"`
}

func (s *Server) handleUpdateForwardAuthAppHTTP(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")
	if clientID == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	var body updateForwardAuthAppBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.Host == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}

	row, err := s.queries.UpdateForwardAuthApp(r.Context(), db.UpdateForwardAuthAppParams{
		ClientID:        clientID,
		DisplayName:     body.DisplayName,
		RedirectUris:    []string{oidc.ForwardAuthCallbackURI(body.Host)},
		ForwardAuthHost: pgtype.Text{String: body.Host, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrClientNotFound())
			return
		}
		if isUniqueViolation(err) {
			writeAuthErr(w, authn.ErrClientAlreadyExists())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleUpdateForwardAuthApp: %w", err))
		return
	}

	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: faActorID(r.Context()),
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventUpdate,
		Detail:    map[string]any{"client_id": clientID, "forward_auth": true, "host": body.Host},
	})

	writeJSON(w, forwardAuthAppView(row.ClientID, row.DisplayName, row.ForwardAuthHost, row.AccessRestricted, row.Disabled, row.CreatedAt))
}

// ----- POST /forward-auth-apps/set-disabled (raw, admin-only, no sudo) -------

type setForwardAuthAppDisabledBody struct {
	ClientID string `json:"clientId"`
	Disabled bool   `json:"disabled"`
}

func (s *Server) handleSetForwardAuthAppDisabledHTTP(w http.ResponseWriter, r *http.Request) {
	var body setForwardAuthAppDisabledBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.ClientID == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	// Guard: only operate on a forward-auth app.
	if _, err := s.queries.GetForwardAuthAppByID(r.Context(), body.ClientID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrClientNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleSetForwardAuthAppDisabled: lookup: %w", err))
		return
	}

	c, err := s.queries.SetOIDCClientDisabled(r.Context(), db.SetOIDCClientDisabledParams{
		ClientID: body.ClientID,
		Disabled: body.Disabled,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrClientNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleSetForwardAuthAppDisabled: update: %w", err))
		return
	}

	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: faActorID(r.Context()),
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventUpdate,
		Detail:    map[string]any{"client_id": body.ClientID, "forward_auth": true, "disabled": body.Disabled},
	})

	writeJSON(w, forwardAuthAppView(c.ClientID, c.DisplayName, c.ForwardAuthHost, c.AccessRestricted, c.Disabled, c.CreatedAt))
}

// ----- POST /forward-auth-apps/delete (raw, sudo-gated) ----------------------

type deleteForwardAuthAppBody struct {
	ClientID string `json:"clientId"`
}

func (s *Server) handleDeleteForwardAuthAppHTTP(w http.ResponseWriter, r *http.Request) {
	var body deleteForwardAuthAppBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if body.ClientID == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	// Guard: ensure it's a forward-auth app before dropping the backing client.
	if _, err := s.queries.GetForwardAuthAppByID(r.Context(), body.ClientID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrClientNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleDeleteForwardAuthApp: lookup: %w", err))
		return
	}

	rows, err := s.queries.DeleteOIDCClient(r.Context(), body.ClientID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleDeleteForwardAuthApp: delete: %w", err))
		return
	}
	if rows == 0 {
		writeAuthErr(w, authn.ErrClientNotFound())
		return
	}

	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: faActorID(r.Context()),
		Factor:    audit.FactorOIDCClient,
		Event:     audit.EventRevoke,
		Detail:    map[string]any{"client_id": body.ClientID, "forward_auth": true},
	})

	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 2: Register the routes in `pkg/server/server.go`** (insert after the OIDC-application block, i.e. after the `oidc-applications/delete` line ~470)

```go
	// Admin: forward-auth application management (Phase 2). A forward-auth app
	// is an oidc_client with forward_auth_enabled=true; presented as its own
	// section and excluded from the OIDC-applications list. RBAC reuses the OIDC
	// app-access endpoints (/oidc-applications/{clientId}/access/*).
	registerOp(mgmt, contract.OperationListForwardAuthApps, s.handleListForwardAuthApps, admin)
	registerOp(mgmt, contract.OperationGetForwardAuthApp, s.handleGetForwardAuthApp, admin)
	s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/forward-auth-apps", admin, s.handleCreateForwardAuthAppHTTP)
	s.registerSudoOpHTTP(s.router, "PUT", "/api/prohibitorum/forward-auth-apps/{clientId}", admin, s.handleUpdateForwardAuthAppHTTP)
	registerOpHTTP(s.router, "POST", "/api/prohibitorum/forward-auth-apps/set-disabled", admin, s.handleSetForwardAuthAppDisabledHTTP)
	s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/forward-auth-apps/delete", admin, s.handleDeleteForwardAuthAppHTTP)
```

- [ ] **Step 3: Swap the OIDC list query** in `pkg/server/handle_admin_oidc_clients.go` `handleListOIDCApplications` (line 85): change `s.queries.ListOIDCClients(ctx)` → `s.queries.ListNonForwardAuthOIDCClients(ctx)`. The loop body is unchanged (the row type `ListNonForwardAuthOIDCClientsRow` has identical fields).

```go
	rows, err := s.queries.ListNonForwardAuthOIDCClients(ctx)
```

- [ ] **Step 4: Guard the OIDC GET** in `handleGetOIDCApplication` (after the `GetOIDCClientAny` success, before the return at line 127)

```go
	if c.ForwardAuthEnabled {
		// Forward-auth apps are managed only via /forward-auth-apps.
		return nil, authErrToHuma(authn.ErrClientNotFound())
	}
	return &oidcApplicationOut{Body: oidcApplicationView(c)}, nil
```

- [ ] **Step 5: Guard the OIDC PUT** in `handleUpdateOIDCApplicationHTTP` (insert immediately after the `clientID` empty-check, ~line 222, before decoding the body)

```go
	if existing, err := s.queries.GetOIDCClientAny(r.Context(), clientID); err == nil && existing.ForwardAuthEnabled {
		writeAuthErr(w, authn.ErrClientNotFound())
		return
	}
```

- [ ] **Step 6: Guard the OIDC rotate-secret** in `handleRotateOIDCApplicationSecretHTTP` (the handler currently discards the lookup row at ~line 355 — capture it and check the flag)

```go
	// Verify the client exists and is not a forward-auth app before rotating.
	existing, err := s.queries.GetOIDCClientAny(r.Context(), body.ClientID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrClientNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleRotateOIDCApplicationSecret: lookup: %w", err))
		return
	}
	if existing.ForwardAuthEnabled {
		writeAuthErr(w, authn.ErrClientNotFound())
		return
	}
```

- [ ] **Step 7: Register the new sudo routes in the policy test** — add to `sudoGatedRoutes` in `pkg/server/admin_route_policy_test.go` (in the OIDC-application area of the table)

```go
	// Forward-auth application lifecycle (Phase 2)
	{method: "POST", path: "/api/prohibitorum/forward-auth-apps", body: `{}`},
	{method: "PUT", path: "/api/prohibitorum/forward-auth-apps/test-client", body: `{}`},
	{method: "POST", path: "/api/prohibitorum/forward-auth-apps/delete", body: `{}`},
```

(Do NOT add `set-disabled` — it is admin-only, not sudo-gated.)

- [ ] **Step 8: Create `pkg/server/handle_admin_forward_auth_apps_test.go`** (DB-free view-projection tests, mirroring the OIDC view test style)

```go
package server

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestForwardAuthAppView_MapsAllFields(t *testing.T) {
	t.Parallel()
	now := time.Now()
	v := forwardAuthAppView("fa-client", "My App",
		pgtype.Text{String: "app.example.test", Valid: true},
		true, false,
		pgtype.Timestamptz{Time: now, Valid: true})
	if v.ClientID != "fa-client" || v.DisplayName != "My App" {
		t.Errorf("id/name mismatch: %+v", v)
	}
	if v.ForwardAuthHost != "app.example.test" {
		t.Errorf("host = %q", v.ForwardAuthHost)
	}
	if !v.AccessRestricted || v.Disabled {
		t.Errorf("flags mismatch: restricted=%v disabled=%v", v.AccessRestricted, v.Disabled)
	}
	if !v.CreatedAt.Equal(now) {
		t.Errorf("createdAt = %v, want %v", v.CreatedAt, now)
	}
}

func TestForwardAuthAppView_EmptyHostAndTime(t *testing.T) {
	t.Parallel()
	v := forwardAuthAppView("c", "n", pgtype.Text{}, false, true, pgtype.Timestamptz{})
	if v.ForwardAuthHost != "" {
		t.Errorf("invalid host should map to empty string, got %q", v.ForwardAuthHost)
	}
	if !v.CreatedAt.IsZero() {
		t.Errorf("invalid timestamptz should map to zero time, got %v", v.CreatedAt)
	}
}
```

- [ ] **Step 9: Run the gate** — Run: `go build -tags nodynamic ./... && go vet ./... && go test ./pkg/server/ -run 'ForwardAuth|AdminMutationRoutesRequireSudo' -v` → PASS (if a shared-DB flake appears, re-run in isolation).

- [ ] **Step 10: Commit**

```bash
git add pkg/server/handle_admin_forward_auth_apps.go pkg/server/handle_admin_forward_auth_apps_test.go pkg/server/server.go pkg/server/handle_admin_oidc_clients.go pkg/server/admin_route_policy_test.go
git commit -m "feat(forward-auth): admin CRUD endpoints + OIDC-list exclusion"
```

---

### Task 3: FA admin frontend — views, Traefik snippet, route, sidebar, i18n

**Goal:** Build the list + detail dashboard views (reusing `AppAccessCard`), a multiline copyable Traefik-config block, wire the routes + sidebar, add en/zh i18n, and rebuild the embedded dist.

**Files:**
- Create: `dashboard/src/components/custom/CodeBlock.vue` (multiline copyable code)
- Create: `dashboard/src/pages/admin/AdminForwardAuthAppsView.vue`
- Create: `dashboard/src/pages/admin/AdminForwardAuthAppDetailView.vue`
- Create: `dashboard/src/pages/admin/AdminForwardAuthAppsView.test.ts`
- Modify: `dashboard/src/router/index.ts:98` (add 2 routes after the OIDC pair)
- Modify: `dashboard/src/router/guard.test.ts:46-48` (add the 2 FA paths)
- Modify: `dashboard/src/components/custom/AppSidebar.vue` (import `Waypoints`; add to `applicationItems`)
- Modify: `dashboard/src/locales/en.ts` (title keys, nav key, `admin.forwardAuth` block)
- Modify: `dashboard/src/locales/zh.ts` (parity)
- Modify: `pkg/webui/dist/**` (rebuilt)

**Acceptance Criteria:**
- [ ] `/admin/forward-auth-apps` lists FA apps + inline create (client-id, host, display-name).
- [ ] Detail view edits display-name + host, shows the host-substituted Traefik snippet, reuses `AppAccessCard`, and has a danger zone (disable/enable + delete) with NO rotate-secret.
- [ ] Sidebar "Forward auth" item appears in the Applications group for admins.
- [ ] `vue-tsc -b` clean, vitest passes, `check-contrast.mjs` passes, en/zh parity passes.

**Verify:** `cd dashboard && npx vue-tsc -b && npm run test -- --run && node scripts/check-contrast.mjs` → PASS

**Steps:**

- [ ] **Step 1: Create `dashboard/src/components/custom/CodeBlock.vue`** (multiline variant of CodeField)

```vue
<script setup lang="ts">
/** CodeBlock — a multiline monospace block with a copy-to-clipboard button. */
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Copy, Check } from 'lucide-vue-next'
import { Button } from '@/components/ui/button'

const props = defineProps<{ value: string; label?: string }>()
const { t } = useI18n()
const copied = ref(false)

async function copy(): Promise<void> {
  try {
    await navigator.clipboard.writeText(props.value)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch {
    /* clipboard blocked — no-op; value is visible for manual copy */
  }
}
</script>

<template>
  <div class="flex flex-col gap-1">
    <span v-if="label" class="text-xs text-muted">{{ label }}</span>
    <div class="relative rounded-md border border-border bg-sunken">
      <Button type="button" variant="ghost" size="sm" class="absolute right-1.5 top-1.5"
              :aria-label="t('common.copy')" @click="copy">
        <component :is="copied ? Check : Copy" class="size-4" aria-hidden="true" />
        <span>{{ copied ? t('common.copied') : t('common.copy') }}</span>
      </Button>
      <pre class="overflow-x-auto px-3 py-2 pr-24 font-mono text-xs text-ink"><code>{{ value }}</code></pre>
    </div>
  </div>
</template>
```

- [ ] **Step 2: Add i18n keys to `dashboard/src/locales/en.ts`**

In the `title` block (after `adminSamlApplications`, ~line 678):
```ts
    adminForwardAuthApps: 'Forward auth',
    adminForwardAuthAppDetail: 'Forward-auth application',
```
In the `admin.nav` object (line 142) add `forwardAuthApps: 'Forward auth'` (e.g. after `samlApplications`):
```ts
samlApplications: 'SAML applications', forwardAuthApps: 'Forward auth',
```
Add a new `forwardAuth` block after the `oidc: { ... }` block (after line 261):
```ts
    forwardAuth: {
      title: 'Forward auth', create: 'Register service', createTitle: 'New forward-auth service',
      colName: 'Service', colHost: 'Host', colState: 'State',
      active: 'Active', disabled: 'Disabled', created: 'Service registered.',
      empty: 'No forward-auth services registered.',
      clientId: 'Client ID', clientIdDesc: 'The stable identifier for the backing OIDC client.',
      host: 'Protected host', hostDesc: 'The hostname Traefik forwards (X-Forwarded-Host), e.g. app.example.com. The callback redirect URI is derived from it.',
      hostPlaceholder: 'app.example.com', displayName: 'Display name',
      configTitle: 'Service settings',
      traefikTitle: 'Traefik configuration',
      traefikDesc: 'Paste this into your Traefik dynamic configuration. Define the app and prohibitorum backend services to match your setup.',
      back: 'Back to forward auth', notFound: 'That service no longer exists.',
      save: 'Save changes', saved: 'Saved.',
      statusLabel: 'Service status',
      disabledDesc: 'Rejects new sign-ins for this service. Existing forward-auth sessions last until they expire.',
      disable: 'Disable', enable: 'Enable',
      dangerTitle: 'Danger zone',
      deleteTitle: 'Delete service', deleteHelp: 'Permanently delete this service and its backing client. The protected app will stop authenticating.',
      delete: 'Delete service', deleteConfirmTitle: 'Delete this service?',
      deleteConfirmBody: 'This permanently removes the service. This cannot be undone.',
    },
```

- [ ] **Step 3: Mirror the keys in `dashboard/src/locales/zh.ts`** (same key paths; Chinese values). In `title`:
```ts
    adminForwardAuthApps: '前向认证',
    adminForwardAuthAppDetail: '前向认证应用',
```
In `admin.nav` (line 138): add `forwardAuthApps: '前向认证'`. Add the `forwardAuth` block after `admin.oidc`:
```ts
    forwardAuth: {
      title: '前向认证', create: '注册服务', createTitle: '新建前向认证服务',
      colName: '服务', colHost: '主机', colState: '状态',
      active: '启用', disabled: '禁用', created: '服务已注册。',
      empty: '尚未注册任何前向认证服务。',
      clientId: '客户端 ID', clientIdDesc: '后端 OIDC 客户端的稳定标识符。',
      host: '受保护主机', hostDesc: 'Traefik 转发的主机名（X-Forwarded-Host），例如 app.example.com。回调重定向 URI 由它派生。',
      hostPlaceholder: 'app.example.com', displayName: '显示名称',
      configTitle: '服务设置',
      traefikTitle: 'Traefik 配置',
      traefikDesc: '将以下内容粘贴到你的 Traefik 动态配置中。请按你的部署定义应用与 prohibitorum 后端服务。',
      back: '返回前向认证', notFound: '该服务已不存在。',
      save: '保存更改', saved: '已保存。',
      statusLabel: '服务状态',
      disabledDesc: '拒绝该服务的新登录。已有的前向认证会话将持续到过期。',
      disable: '禁用', enable: '启用',
      dangerTitle: '危险区域',
      deleteTitle: '删除服务', deleteHelp: '永久删除该服务及其后端客户端。受保护的应用将无法再认证。',
      delete: '删除服务', deleteConfirmTitle: '删除此服务？',
      deleteConfirmBody: '此操作将永久删除该服务，且无法撤销。',
    },
```
After editing en.ts, **grep-verify no curly apostrophes were introduced** (memory `reference_en_ts_apostrophe_edit_hazard`): `grep -nP "[\x{2018}\x{2019}]" dashboard/src/locales/en.ts` → no output. No literal `@` appears in these strings, so no `@`-escaping needed.

- [ ] **Step 4: Create `dashboard/src/pages/admin/AdminForwardAuthAppsView.vue`**

```vue
<script setup lang="ts">
/** AdminForwardAuthAppsView (/admin/forward-auth-apps) — list of forward-auth services; inline create. */
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import StatusMessage from '@/components/custom/StatusMessage.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import FormSection from '@/components/custom/FormSection.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import { Waypoints } from 'lucide-vue-next'

interface ForwardAuthApp {
  clientId: string
  displayName: string
  forwardAuthHost: string
  accessRestricted: boolean
  disabled: boolean
  createdAt: string
}

const { t } = useI18n()
const router = useRouter()
const { busy, run, errorText } = useApi()

const rows = ref<ForwardAuthApp[]>([])
const createOpen = ref(false)
const created = ref(false)

const clientId = ref('')
const host = ref('')
const displayName = ref('')

async function load(): Promise<void> {
  const res = await run(() => api.get<ForwardAuthApp[]>('/api/prohibitorum/forward-auth-apps'))
  if (res) rows.value = res
}

function go(id: string): void { router.push(`/admin/forward-auth-apps/${id}`) }

function openCreate(): void {
  clientId.value = ''
  host.value = ''
  displayName.value = ''
  created.value = false
  createOpen.value = true
}

async function create(): Promise<void> {
  created.value = false
  const res = await run(() => withSudo(() => api.post<ForwardAuthApp>('/api/prohibitorum/forward-auth-apps', {
    clientId: clientId.value,
    host: host.value,
    displayName: displayName.value,
  })))
  if (res) {
    createOpen.value = false
    created.value = true
    await load()
  }
}

onMounted(load)
</script>
<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.forwardAuth.title') }}</h1>
      <Button type="button" data-test="create" @click="openCreate">{{ t('admin.forwardAuth.create') }}</Button>
    </div>
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <StatusMessage :show="created">{{ t('admin.forwardAuth.created') }}</StatusMessage>

    <Card v-if="createOpen">
      <CardHeader><CardTitle>{{ t('admin.forwardAuth.createTitle') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-4 py-4">
        <FormSection :title="t('admin.forwardAuth.configTitle')">
          <div class="flex flex-col gap-1.5">
            <Label for="clientId">{{ t('admin.forwardAuth.clientId') }}</Label>
            <Input id="clientId" name="clientId" v-model="clientId" autocomplete="off" />
            <p class="text-xs text-muted">{{ t('admin.forwardAuth.clientIdDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="host">{{ t('admin.forwardAuth.host') }}</Label>
            <Input id="host" name="host" v-model="host" inputmode="url" :placeholder="t('admin.forwardAuth.hostPlaceholder')" />
            <p class="text-xs text-muted">{{ t('admin.forwardAuth.hostDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="displayName">{{ t('admin.forwardAuth.displayName') }}</Label>
            <Input id="displayName" name="displayName" v-model="displayName" />
          </div>
        </FormSection>
        <div class="flex gap-2">
          <Button type="button" :disabled="busy" data-test="create-confirm" @click="create">{{ t('admin.forwardAuth.create') }}</Button>
          <Button type="button" variant="outline" :disabled="busy" data-test="create-cancel" @click="createOpen = false">{{ t('common.cancel') }}</Button>
        </div>
      </CardContent>
    </Card>

    <TableSkeleton v-if="busy && !rows.length" :rows="5" :cols="3" />
    <Table v-else-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.forwardAuth.colName') }} · {{ t('admin.forwardAuth.clientId') }}</TableHead>
          <TableHead>{{ t('admin.forwardAuth.colHost') }}</TableHead>
          <TableHead>{{ t('admin.forwardAuth.colState') }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="c in rows" :key="c.clientId" class="cursor-pointer" tabindex="0"
                  :data-test="`fa-row-${c.clientId}`"
                  @click="go(c.clientId)" @keydown.enter="go(c.clientId)" @keydown.space.prevent="go(c.clientId)">
          <TableCell>
            <div class="flex min-w-0 flex-col">
              <span class="truncate font-medium text-ink">{{ c.displayName }}</span>
              <span class="truncate text-muted">{{ c.clientId }}</span>
            </div>
          </TableCell>
          <TableCell><span class="truncate font-mono text-sm text-muted">{{ c.forwardAuthHost }}</span></TableCell>
          <TableCell>
            <StatusBadge :variant="c.disabled ? 'danger' : 'success'">
              {{ c.disabled ? t('admin.forwardAuth.disabled') : t('admin.forwardAuth.active') }}
            </StatusBadge>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
    <EmptyState v-else-if="!errorText && !createOpen" :icon="Waypoints" :title="t('admin.forwardAuth.empty')" />
  </div>
</template>
```

- [ ] **Step 5: Create `dashboard/src/pages/admin/AdminForwardAuthAppDetailView.vue`**

```vue
<script setup lang="ts">
/**
 * AdminForwardAuthAppDetailView (/admin/forward-auth-apps/:clientId) —
 * edit display-name + host, show the host-substituted Traefik snippet, reuse
 * the OIDC AppAccessCard for RBAC, and a danger zone (disable/enable + delete).
 * No rotate-secret — forward-auth clients are public.
 */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Separator } from '@/components/ui/separator'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import SectionTitle from '@/components/custom/SectionTitle.vue'
import StatusMessage from '@/components/custom/StatusMessage.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import CardSkeleton from '@/components/custom/CardSkeleton.vue'
import BackLink from '@/components/custom/BackLink.vue'
import CodeBlock from '@/components/custom/CodeBlock.vue'
import AppAccessCard from '@/components/custom/AppAccessCard.vue'

interface ForwardAuthApp {
  clientId: string
  displayName: string
  forwardAuthHost: string
  accessRestricted: boolean
  disabled: boolean
  createdAt: string
}

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { busy, error, run, errorText } = useApi()

const clientId = String(route.params.clientId)
const app = ref<ForwardAuthApp | null>(null)
const notFound = ref(false)

const displayName = ref('')
const host = ref('')
const disabled = ref(false)
const { flag: saved, trigger: triggerSaved } = useTransientFlag()
const confirmDelete = ref(false)

// Host-substituted Traefik dynamic-config snippet. The verify address uses the
// current origin (the dashboard is served from the Prohibitorum domain).
const traefikSnippet = computed(() => {
  const origin = window.location.origin
  const h = host.value || app.value?.forwardAuthHost || 'app.example.com'
  return `http:
  middlewares:
    prohibitorum-forward-auth:
      forwardAuth:
        address: "${origin}/api/prohibitorum/forward-auth/verify"
        trustForwardHeader: true
        authResponseHeaders:
          - Remote-User
          - Remote-Name
          - Remote-Email
          - Remote-Groups
  routers:
    # Your protected app (define "app-svc" to point at your backend):
    protected-app:
      rule: "Host(\`${h}\`)"
      service: app-svc
      middlewares:
        - prohibitorum-forward-auth
    # The fixed forward-auth prefix → Prohibitorum (define "prohibitorum-svc"):
    prohibitorum-forward-auth:
      rule: "Host(\`${h}\`) && PathPrefix(\`/.prohibitorum-forward-auth\`)"
      service: prohibitorum-svc`
})

async function load(): Promise<void> {
  const c = await run(() => api.get<ForwardAuthApp>(`/api/prohibitorum/forward-auth-apps/${clientId}`))
  if (!c) { if (error.value?.code === 'client_not_found') notFound.value = true; return }
  app.value = c
  displayName.value = c.displayName
  host.value = c.forwardAuthHost
  disabled.value = c.disabled
}

async function save(): Promise<void> {
  const updated = await run(() => withSudo(() => api.put<ForwardAuthApp>(`/api/prohibitorum/forward-auth-apps/${clientId}`, {
    displayName: displayName.value,
    host: host.value,
  }), t('sudo.reason.saveChanges')))
  if (updated) { app.value = updated; triggerSaved() }
}

async function toggleDisabled(): Promise<void> {
  const next = !disabled.value
  const updated = await run(() => withSudo(() =>
    api.post<ForwardAuthApp>('/api/prohibitorum/forward-auth-apps/set-disabled', { clientId, disabled: next }),
    t('sudo.reason.disableApp')))
  if (updated) { app.value = updated; disabled.value = updated.disabled }
}

async function destroy(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/forward-auth-apps/delete', { clientId })
    return true as const
  }, t('sudo.reason.deleteApp')))
  confirmDelete.value = false
  if (ok) router.push('/admin/forward-auth-apps')
}

onMounted(load)
</script>
<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <BackLink to="/admin/forward-auth-apps" :label="t('admin.forwardAuth.back')" />
    <Alert v-if="errorText && !notFound" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <p v-if="notFound" class="text-sm text-muted" role="status">{{ t('admin.forwardAuth.notFound') }}</p>

    <CardSkeleton v-else-if="busy && !app" />

    <template v-else-if="app">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ app.displayName }}</h1>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.forwardAuth.configTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('admin.forwardAuth.clientId') }}</Label>
            <p class="font-mono text-sm text-muted" data-test="fa-client-id">{{ app.clientId }}</p>
            <p class="text-xs text-muted">{{ t('admin.forwardAuth.clientIdDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="displayName">{{ t('admin.forwardAuth.displayName') }}</Label>
            <Input id="displayName" name="displayName" v-model="displayName" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="host">{{ t('admin.forwardAuth.host') }}</Label>
            <Input id="host" name="host" v-model="host" inputmode="url" :placeholder="t('admin.forwardAuth.hostPlaceholder')" />
            <p class="text-xs text-muted">{{ t('admin.forwardAuth.hostDesc') }}</p>
          </div>
          <div class="flex items-center gap-3">
            <Button type="button" :disabled="busy" data-test="save" @click="save">{{ t('admin.forwardAuth.save') }}</Button>
            <StatusMessage :show="saved">{{ t('admin.forwardAuth.saved') }}</StatusMessage>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.forwardAuth.traefikTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-3">
          <p class="text-xs text-muted">{{ t('admin.forwardAuth.traefikDesc') }}</p>
          <CodeBlock :value="traefikSnippet" />
        </CardContent>
      </Card>

      <AppAccessCard kind="oidc" :app-id="clientId" />

      <!-- Danger zone (kept LAST). No rotate-secret — FA clients are public. -->
      <Card class="border-destructive/30 bg-destructive/[0.02]">
        <CardHeader><CardTitle class="text-destructive">{{ t('admin.forwardAuth.dangerTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-2">
            <div class="flex items-center gap-2">
              <SectionTitle as="h3">{{ t('admin.forwardAuth.statusLabel') }}</SectionTitle>
              <StatusBadge :variant="disabled ? 'danger' : 'success'" data-test="status-badge">
                {{ disabled ? t('admin.forwardAuth.disabled') : t('admin.forwardAuth.active') }}
              </StatusBadge>
            </div>
            <p class="text-xs text-muted">{{ t('admin.forwardAuth.disabledDesc') }}</p>
            <Button type="button" variant="outline" class="w-fit" :disabled="busy" data-test="disable-toggle" @click="toggleDisabled">
              {{ disabled ? t('admin.forwardAuth.enable') : t('admin.forwardAuth.disable') }}
            </Button>
          </div>

          <Separator />
          <div class="flex flex-col gap-2">
            <SectionTitle as="h3">{{ t('admin.forwardAuth.deleteTitle') }}</SectionTitle>
            <p class="text-xs text-muted">{{ t('admin.forwardAuth.deleteHelp') }}</p>
            <Button type="button" variant="destructive" class="w-fit" :disabled="busy" data-test="delete" @click="confirmDelete = true">{{ t('admin.forwardAuth.delete') }}</Button>
          </div>
        </CardContent>
      </Card>
    </template>

    <ConfirmDialog :open="confirmDelete" :title="t('admin.forwardAuth.deleteConfirmTitle')" :confirm-label="t('admin.forwardAuth.delete')" :busy="busy"
      @update:open="(v) => { if (!v) confirmDelete = false }" @cancel="confirmDelete = false" @confirm="destroy">
      {{ t('admin.forwardAuth.deleteConfirmBody') }}
    </ConfirmDialog>
  </div>
</template>
```

- [ ] **Step 6: Add the routes in `dashboard/src/router/index.ts`** (after the OIDC detail route, line 98)

```ts
      { path: 'admin/forward-auth-apps', name: 'admin-forward-auth-apps', component: () => import('../pages/admin/AdminForwardAuthAppsView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminForwardAuthApps' } },
      { path: 'admin/forward-auth-apps/:clientId', name: 'admin-forward-auth-app-detail', component: () => import('../pages/admin/AdminForwardAuthAppDetailView.vue'), meta: { requiresAdmin: true, titleKey: 'title.adminForwardAuthAppDetail' } },
```

- [ ] **Step 7: Extend the guard test** in `dashboard/src/router/guard.test.ts` — add to the `requiresAdmin` path array (lines 46-48):

```ts
    '/admin/forward-auth-apps',
    '/admin/forward-auth-apps/some-client',
```

- [ ] **Step 8: Add the sidebar item** in `dashboard/src/components/custom/AppSidebar.vue` — add `Waypoints` to the lucide import (line 12), and a new entry to `applicationItems` (line 52-55):

```ts
const applicationItems = computed(() => [
  { to: '/admin/oidc-applications', label: t('admin.nav.oidcApplications'), icon: AppWindow },
  { to: '/admin/saml-applications', label: t('admin.nav.samlApplications'), icon: Building2 },
  { to: '/admin/forward-auth-apps', label: t('admin.nav.forwardAuthApps'), icon: Waypoints },
])
```

- [ ] **Step 9: Create `dashboard/src/pages/admin/AdminForwardAuthAppsView.test.ts`** (mirror the OIDC list test idioms — mount, flush, assert)

```ts
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import en from '@/locales/en'
import AdminForwardAuthAppsView from './AdminForwardAuthAppsView.vue'

vi.mock('@/lib/api', () => ({ api: { get: vi.fn(), post: vi.fn() } }))
vi.mock('@/lib/sudo', () => ({ withSudo: (fn: () => unknown) => fn() }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: vi.fn() }) }))

import { api } from '@/lib/api'

const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } })

function mountView() {
  return mount(AdminForwardAuthAppsView, { global: { plugins: [i18n], stubs: { RouterLink: true } } })
}

describe('AdminForwardAuthAppsView', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('lists forward-auth services', async () => {
    ;(api.get as ReturnType<typeof vi.fn>).mockResolvedValue([
      { clientId: 'fa1', displayName: 'App One', forwardAuthHost: 'app.example.test', accessRestricted: false, disabled: false, createdAt: '' },
    ])
    const w = mountView()
    await flushPromises()
    expect(w.html()).toContain('App One')
    expect(w.html()).toContain('app.example.test')
  })

  it('shows the empty state when there are no services', async () => {
    ;(api.get as ReturnType<typeof vi.fn>).mockResolvedValue([])
    const w = mountView()
    await flushPromises()
    expect(w.html()).toContain(en.admin.forwardAuth.empty)
  })
})
```

(If the repo's existing admin list tests use a different mount/i18n helper, mirror that helper instead — match the established test style in `dashboard/src/pages/admin/*.test.ts`.)

- [ ] **Step 10: Run the FE gate** — Run: `cd dashboard && npx vue-tsc -b && npm run test -- --run && node scripts/check-contrast.mjs` → all PASS. Then **grep-verify en.ts apostrophes**: `grep -nP "[\x{2018}\x{2019}]" dashboard/src/locales/en.ts` → no output.

- [ ] **Step 11: Rebuild + commit the dist**

```bash
cd dashboard && npm run build && cd ..
git add dashboard/src pkg/webui/dist
git commit -m "feat(forward-auth): admin dashboard UI (list/detail + Traefik snippet + i18n)"
```

---

### Task 4: Forward-auth sign-out (per-app + SSO logout)

**Goal:** Add the protected-domain `sign_out` (clears per-domain cookie + KV session, bounces to the IdP) and the IdP-domain `sso-logout` (terminates the SSO session, validated redirect back), with a fail-closed open-redirect guard, tests, and docs.

**Files:**
- Modify: `pkg/protocol/oidc/forward_auth.go` (add `HandleForwardAuthSignOut`, `faClearCookie`, `ValidatedForwardAuthReturnURL`)
- Create: `pkg/server/handle_forward_auth_signout.go` (the server `sso-logout` handler)
- Modify: `pkg/server/server.go:394-395` (register the two routes)
- Modify: `pkg/protocol/oidc/forward_auth_test.go` (sign_out + validator tests)
- Modify: `docs/forward-auth.md` (sign-out section)

**Acceptance Criteria:**
- [ ] `GET /.prohibitorum-forward-auth/sign_out` deletes the KV `fa:session`, clears the host-only cookie, and 302s to `<issuer>/api/prohibitorum/forward-auth/sso-logout?rd=<scheme>://<host>/`.
- [ ] `sso-logout` revokes the Prohibitorum session, clears the session cookie, and 302s to `rd` ONLY when `rd`'s host is a registered FA host; otherwise to `/`.
- [ ] `ValidatedForwardAuthReturnURL` rejects unregistered hosts, non-http(s) schemes, and unparseable values.

**Verify:** `go build -tags nodynamic ./... && go test ./pkg/protocol/oidc/ -run 'SignOut|ValidatedForwardAuthReturnURL' -v` → PASS

**Steps:**

- [ ] **Step 1: Add the Provider handler + helpers to `pkg/protocol/oidc/forward_auth.go`** (after `HandleForwardAuthCallback`)

```go
// faClearCookie returns a forward-auth cookie set to expire immediately,
// removing the per-domain session cookie on the protected host.
func faClearCookie(secure bool) *http.Cookie {
	c := faCookie(secure, "")
	c.MaxAge = -1
	return c
}

// HandleForwardAuthSignOut is reached on the protected domain via the routed
// ForwardAuthPathPrefix. It removes the per-domain forward-auth session (KV +
// cookie), then 302s to the IdP-domain sso-logout endpoint to terminate the
// Prohibitorum SSO session. The rd host is the trusted X-Forwarded-Host; the
// sso-logout endpoint re-validates it against the registered FA hosts.
func (p *Provider) HandleForwardAuthSignOut(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	secure := schemeOf(r) == "https"
	if c, err := r.Cookie(faCookieName(secure)); err == nil && c.Value != "" {
		_, _ = p.kv.Pop(ctx, faSessionKey(c.Value)) // best-effort single-use removal
	}
	http.SetCookie(w, faClearCookie(secure))

	rd := schemeOf(r) + "://" + hostOf(r) + "/"
	q := url.Values{}
	q.Set("rd", rd)
	http.Redirect(w, r, p.cfg.OIDC.Issuer+"/api/prohibitorum/forward-auth/sso-logout?"+q.Encode(), http.StatusFound)
}

// ValidatedForwardAuthReturnURL parses rd and returns it only when its host is a
// registered forward-auth host — a fail-closed open-redirect guard mirroring the
// verify host check. Returns ("", false) otherwise. The host is matched on the
// full authority (host[:port]) to mirror verify's X-Forwarded-Host match.
func ValidatedForwardAuthReturnURL(ctx context.Context, q db.Querier, rd string) (string, bool) {
	if rd == "" {
		return "", false
	}
	u, err := url.Parse(rd)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", false
	}
	if _, err := q.GetForwardAuthClientByHost(ctx, pgtype.Text{String: u.Host, Valid: true}); err != nil {
		return "", false
	}
	return rd, true
}
```

- [ ] **Step 2: Create `pkg/server/handle_forward_auth_signout.go`**

```go
// Package server — handle_forward_auth_signout.go
//
// The IdP-domain side of forward-auth sign-out. The protected-domain
// /.prohibitorum-forward-auth/sign_out handler (oidc.Provider) clears the
// per-domain cookie + KV session, then 302s here. This handler terminates the
// Prohibitorum SSO session (mirroring handleLogoutHTTP) and redirects back to a
// validated forward-auth host (fail-closed open-redirect guard).
package server

import (
	"net/http"

	oidc "prohibitorum/pkg/protocol/oidc"
	sessstore "prohibitorum/pkg/session"
)

func (s *Server) handleForwardAuthSSOLogoutHTTP(w http.ResponseWriter, r *http.Request) {
	// Terminate the SSO session (same path as POST /auth/logout).
	if c, err := r.Cookie(sessstore.SessionCookieNameFor(s.config)); err == nil && c.Value != "" {
		if id, tok, ok := sessstore.ParseCookieValue(c.Value); ok {
			_ = s.sessionStore.Revoke(r.Context(), id, tok)
		}
	}
	http.SetCookie(w, sessstore.ClearedSessionCookie(s.config, r))

	// Open-redirect guard: only bounce back to a registered forward-auth host.
	if dest, ok := oidc.ValidatedForwardAuthReturnURL(r.Context(), s.queries, r.URL.Query().Get("rd")); ok {
		http.Redirect(w, r, dest, http.StatusFound)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}
```

- [ ] **Step 3: Register the two routes in `pkg/server/server.go`** (immediately after the existing FA callback registration at line 395)

```go
	s.router.Get(oidcop.ForwardAuthPathPrefix+"/sign_out", s.oidcOP.HandleForwardAuthSignOut)
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/forward-auth/sso-logout", publicReq, s.handleForwardAuthSSOLogoutHTTP)
```

- [ ] **Step 4: Make the fake querier host-aware for the validator test** — in `pkg/protocol/oidc/forward_auth_test.go`, replace the `GetForwardAuthClientByHost` method (lines 113-118) so an optional `knownHost` gates the lookup (existing tests leave it "" → unchanged behavior)

```go
func (f *fakeFAQueries) GetForwardAuthClientByHost(_ context.Context, host pgtype.Text) (db.GetForwardAuthClientByHostRow, error) {
	if f.faClientErr != nil {
		return db.GetForwardAuthClientByHostRow{}, f.faClientErr
	}
	if f.knownHost != "" && host.String != f.knownHost {
		return db.GetForwardAuthClientByHostRow{}, pgx.ErrNoRows
	}
	return f.faClient, nil
}
```

Add the field to the struct (after `faClientErr error`):
```go
	// knownHost, when set, restricts GetForwardAuthClientByHost to that host.
	knownHost string
```

Add the `pgx` import to the test file if not present: `"github.com/jackc/pgx/v5"`.

- [ ] **Step 5: Write the sign_out + validator tests** (append to `pkg/protocol/oidc/forward_auth_test.go`)

```go
func TestHandleForwardAuthSignOut_ClearsSessionAndRedirects(t *testing.T) {
	p, store := newFAProvider(&fakeFAQueries{})
	// Seed a per-domain session keyed by the cookie token.
	const tok = "tok-123"
	if err := store.SetEx(context.Background(), faSessionKey(tok), `{"account_id":1,"client_id":"fa"}`, time.Hour); err != nil {
		t.Fatalf("seed: %v", err)
	}
	req := faRequest("https", "app.example.test", "/", &http.Cookie{Name: faCookieName(true), Value: tok})
	rec := httptest.NewRecorder()
	p.HandleForwardAuthSignOut(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, testIssuer+"/api/prohibitorum/forward-auth/sso-logout?") {
		t.Errorf("Location = %q, want sso-logout on issuer", loc)
	}
	if !strings.Contains(loc, "rd=https%3A%2F%2Fapp.example.test%2F") {
		t.Errorf("Location missing rd=app host: %q", loc)
	}
	// KV session is gone.
	if v, _ := store.Get(context.Background(), faSessionKey(tok)); v != "" {
		t.Error("expected fa:session to be deleted")
	}
	// Cookie cleared (MaxAge<0).
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == faCookieName(true) && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Error("expected cleared forward-auth cookie")
	}
}

func TestValidatedForwardAuthReturnURL(t *testing.T) {
	q := &fakeFAQueries{knownHost: "app.example.test"}
	cases := []struct {
		name string
		rd   string
		want bool
	}{
		{"registered host", "https://app.example.test/foo", true},
		{"unregistered host", "https://evil.example.com/", false},
		{"empty", "", false},
		{"non-http scheme", "javascript:alert(1)", false},
		{"garbage", "://nope", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ValidatedForwardAuthReturnURL(context.Background(), q, c.rd)
			if ok != c.want {
				t.Fatalf("ok = %v, want %v (rd=%q)", ok, c.want, c.rd)
			}
			if ok && got != c.rd {
				t.Errorf("dest = %q, want %q", got, c.rd)
			}
		})
	}
}
```

- [ ] **Step 6: Document sign-out in `docs/forward-auth.md`** — under the "Middleware + router (dynamic config)" section, note the prefix already routes `sign_out`; add a short "### Sign out" subsection after section 2:

```markdown
### Sign out

The forward-auth prefix also serves a sign-out endpoint. Link users to:

    https://<protected-host>/.prohibitorum-forward-auth/sign_out

It clears the per-domain forward-auth cookie + session, then bounces to
Prohibitorum to terminate the SSO session and returns the browser to the app
(now unauthenticated → a fresh login). Note: forward-auth sessions already
established on *other* protected domains remain valid until they expire
(`forward_auth.session_ttl`, default 1h) or the next live authorization check
denies them — signing out is immediate for the dashboard and this app, and
prevents silent re-login elsewhere, but does not retroactively revoke other
domains' per-domain cookies.
```

- [ ] **Step 7: Run the gate** — Run: `go build -tags nodynamic ./... && go vet ./... && go test ./pkg/protocol/oidc/ -run 'SignOut|ValidatedForwardAuthReturnURL' -v` → PASS

- [ ] **Step 8: Commit**

```bash
git add pkg/protocol/oidc/forward_auth.go pkg/protocol/oidc/forward_auth_test.go pkg/server/handle_forward_auth_signout.go pkg/server/server.go docs/forward-auth.md
git commit -m "feat(forward-auth): sign-out (per-app cookie + SSO logout) with open-redirect guard"
```

---

### Task 5: End-to-end runtime verification (2A + 2C)

**Goal:** Prove the admin UI and sign-out work against a running server — registering an FA app via the UI, confirming exclusivity, and driving the verify→authorize→callback→verify→sign_out→sso-logout HTTP sequence. Fix anything found.

**Files:** None (verification; bug fixes land in the relevant task's files with a follow-up commit).

**Acceptance Criteria:**
- [ ] A freshly registered FA app appears in `GET /forward-auth-apps` and is ABSENT from `GET /oidc-applications`.
- [ ] `GET /oidc-applications/{fa-id}` returns `client_not_found`.
- [ ] `verify` (unknown host) → 403; (registered, no cookie) → 302 to `/oauth/authorize` with `client_id`, the FA callback `redirect_uri`, and `code_challenge`.
- [ ] `sign_out` → 302 to sso-logout with `rd`; `sso-logout` with a bogus `rd` host → 302 to `/`.

**Verify:** the curl block below prints the expected statuses; the dashboard DOM (chromium `--dump-dom`) shows the FA list + detail.

**Steps:**

- [ ] **Step 1: Build + launch the server from a subagent** (the controller's own Bash servers get SIGKILLed — dispatch a subagent to run it; podman Postgres is already up on :5432). The subagent should:

```bash
source scripts/dev-env.sh
go build -tags nodynamic -o /tmp/prohibitorum ./cmd/prohibitorum
# fresh dev DB + admin per the dev tasks, then:
setsid /tmp/prohibitorum serve >/tmp/fa-verify.log 2>&1 </dev/null &
# enroll an admin and obtain a session cookie for the admin API calls.
```

Use `mise dev-seed` / `mise enroll-admin -- --new` as in the handoff to get an admin login, then authenticate to obtain a session cookie (the verification subagent drives `/auth/login/*` or reuses an existing dev session).

- [ ] **Step 2: Register an FA app via the admin API and assert exclusivity**

```bash
# (with an admin session cookie + a fresh sudo grant in $C)
curl -fsS -b "$C" -X POST localhost:8080/api/prohibitorum/forward-auth-apps \
  -H 'Content-Type: application/json' \
  -d '{"clientId":"verify-fa","host":"app.example.test","displayName":"Verify FA"}'
curl -fsS -b "$C" localhost:8080/api/prohibitorum/forward-auth-apps | grep verify-fa      # present
curl -fsS -b "$C" localhost:8080/api/prohibitorum/oidc-applications | grep -c verify-fa    # expect 0
curl -s  -b "$C" -o /dev/null -w '%{http_code}\n' localhost:8080/api/prohibitorum/oidc-applications/verify-fa  # expect 404
```

- [ ] **Step 3: Drive the verify/sign_out sequence with simulated Traefik headers**

```bash
# Unknown host → 403
curl -s -o /dev/null -w '%{http_code}\n' -H 'X-Forwarded-Host: nope.example.test' -H 'X-Forwarded-Proto: https' \
  localhost:8080/api/prohibitorum/forward-auth/verify        # expect 403
# Registered, no cookie → 302 to /oauth/authorize
curl -s -D - -o /dev/null -H 'X-Forwarded-Host: app.example.test' -H 'X-Forwarded-Proto: https' -H 'X-Forwarded-Uri: /foo' \
  localhost:8080/api/prohibitorum/forward-auth/verify | grep -i '^location:'   # /oauth/authorize?client_id=verify-fa...&code_challenge=...
# sign_out → 302 to sso-logout with rd
curl -s -D - -o /dev/null -H 'X-Forwarded-Host: app.example.test' -H 'X-Forwarded-Proto: https' \
  localhost:8080/.prohibitorum-forward-auth/sign_out | grep -i '^location:'    # .../forward-auth/sso-logout?rd=https%3A%2F%2Fapp.example.test%2F
# sso-logout with a bogus rd host → 302 to /
curl -s -D - -o /dev/null 'localhost:8080/api/prohibitorum/forward-auth/sso-logout?rd=https://evil.example.com/' | grep -i '^location:'  # Location: /
```

- [ ] **Step 4: Verify the dashboard DOM** (chromium, no Playwright):

```bash
/usr/bin/chromium --headless=new --no-sandbox --dump-dom 'http://localhost:8080/admin/forward-auth-apps' | grep -i 'forward'
```

(The SPA is auth-gated; if the dump shows the login shell, drive a logged-in check via the subagent's session — the primary acceptance is the curl block, which exercises the real handlers.)

- [ ] **Step 5: If any check fails**, fix in the owning task's file, re-run that task's `go test`, and commit the fix with a `fix(forward-auth): …` message. If all pass, record the evidence in the task notes (no commit needed for a clean verification).

---

### Task 6: Multi-domain dev harness (built last)

**Goal:** A local harness that brings up a protected "whoami" app behind a TLS front, wired to Prohibitorum forward-auth, so the full browser flow can be exercised by hand. Modeled on `mise dev:federation`; real hostnames/certs stay out of git.

**Files:**
- Create: `scripts/dev-forward-auth.sh`
- Create: `cmd/prohibitorum/dev_forward_auth.go` (a `dev forward-auth-whoami` hidden subcommand: a tiny HTTP server that echoes the `Remote-*` headers)
- Modify: `mise.toml` (add the `dev:forward-auth` task)
- Modify: `docs/forward-auth.md` (a "Local dev harness" note)
- Modify: `.gitignore` if needed (the `.dev/*.env` pattern already covers it — verify)

**Acceptance Criteria:**
- [ ] `cmd/prohibitorum/dev_forward_auth.go` builds (`go build -tags nodynamic ./...`) and the whoami handler writes back the `Remote-User/Name/Email/Groups` request headers.
- [ ] `scripts/dev-forward-auth.sh` passes `shellcheck` and uses `example.test` placeholders; real hostnames/cert paths are read from a gitignored `.dev/dev-forward-auth.env` (template written on first run).
- [ ] `mise dev:forward-auth` is registered with a description.
- [ ] No real hostnames/cert paths are committed (`git grep` for the dev hostname returns nothing in tracked files).

**Verify:** `go build -tags nodynamic ./... && shellcheck scripts/dev-forward-auth.sh && git grep -n "<your-real-host>" -- . ':!.dev' || echo "no real hosts committed"` → builds clean, shellcheck clean, no real hosts.

**Steps:**

- [ ] **Step 1: Create the whoami dev subcommand `cmd/prohibitorum/dev_forward_auth.go`** (a minimal echo server; the protected "app")

```go
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/spf13/cobra"
)

// addDevForwardAuthCommands registers a hidden `dev forward-auth-whoami` server
// used only by scripts/dev-forward-auth.sh: it echoes the ForwardAuth identity
// headers Traefik injects, so a successful 200 is visible in the browser.
func addDevForwardAuthCommands(root *cobra.Command) {
	var addr string
	whoami := &cobra.Command{
		Use:    "forward-auth-whoami",
		Short:  "DEV: echo ForwardAuth Remote-* headers (protected app stand-in)",
		Hidden: true,
		Run: func(_ *cobra.Command, _ []string) {
			mux := http.NewServeMux()
			mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				fmt.Fprintf(w, "Remote-User:   %s\n", r.Header.Get("Remote-User"))
				fmt.Fprintf(w, "Remote-Name:   %s\n", r.Header.Get("Remote-Name"))
				fmt.Fprintf(w, "Remote-Email:  %s\n", r.Header.Get("Remote-Email"))
				fmt.Fprintf(w, "Remote-Groups: %s\n", r.Header.Get("Remote-Groups"))
			})
			log.Printf("forward-auth-whoami listening on %s", addr)
			if err := http.ListenAndServe(addr, mux); err != nil {
				log.Fatalf("forward-auth-whoami: %v", err)
			}
		},
	}
	whoami.Flags().StringVar(&addr, "addr", "127.0.0.1:8090", "Listen address for the whoami app.")

	devCmd := devRootCommand(root) // reuse the existing hidden `dev` parent if present; else create one
	devCmd.AddCommand(whoami)
}
```

Note: inspect how `cmd/prohibitorum/dev_federation.go` attaches its `dev` subcommand parent and mirror that exactly (the `devRootCommand(root)` line above is a stand-in — use the same parent-command accessor that `dev_federation.go` uses, and register `addDevForwardAuthCommands(cli.Root())` next to `addForwardAuthAppCommands(cli.Root())` in `main.go:784`). Verify by reading `dev_federation.go` before writing this file.

- [ ] **Step 2: Create `scripts/dev-forward-auth.sh`** modeled on `scripts/dev-federation.sh`. Responsibilities (read `dev-federation.sh` first and mirror its structure, env-template handling, and TLS/front-proxy approach):
  - On first run, write a `.dev/dev-forward-auth.env` template (committed code references `app.example.test` / `auth.example.test` placeholders + cert path vars) and exit with instructions.
  - Source `scripts/dev-env.sh` for the DSN; ensure the DB is up.
  - Start the Prohibitorum server, seed an admin, run `prohibitorum forward-auth-app create --client-id dev-fa --host "$FA_APP_HOST" --display-name "Dev FA"` and grant the dev admin access.
  - Start `prohibitorum dev forward-auth-whoami --addr 127.0.0.1:8090`.
  - Bring up the front proxy (reuse the nginx pattern from `dev-federation.sh`, or Traefik) terminating TLS for `$FA_APP_HOST` with the ForwardAuth middleware → the verify endpoint and the `/.prohibitorum-forward-auth/*` prefix → Prohibitorum.
  - Print the URL to open and the expected whoami output.
  - Keep all real hostnames/cert paths in the gitignored env file.

- [ ] **Step 3: Register the mise task** in `mise.toml` (after the `dev:federation` block)

```toml
[tasks."dev:forward-auth"]
description = "DEV: bring up a protected whoami app behind a TLS front wired to Prohibitorum forward-auth, for manual end-to-end testing. Reads local hostnames/cert from .dev/dev-forward-auth.env (a template is written on first run). Start the DB first with `mise run db:start`."
run = "exec scripts/dev-forward-auth.sh \"$@\""
```

- [ ] **Step 4: Add a "Local dev harness" note to `docs/forward-auth.md`** (one short paragraph pointing at `mise dev:forward-auth` and the gitignored env template).

- [ ] **Step 5: Verify** — Run: `go build -tags nodynamic ./... && shellcheck scripts/dev-forward-auth.sh`. Confirm `.dev/` is gitignored and no real hostnames are staged: `git status --porcelain` shows no `.dev/*.env`.

- [ ] **Step 6: Commit**

```bash
git add scripts/dev-forward-auth.sh cmd/prohibitorum/dev_forward_auth.go cmd/prohibitorum/main.go mise.toml docs/forward-auth.md
git commit -m "feat(forward-auth): multi-domain dev harness (whoami app + TLS front)"
```

---

### Task 7: Final gate, dist re-verify, and memory/handoff update

**Goal:** Run the full gate end-to-end, confirm the committed dist is current, and update the project memory + handoff note to reflect Phase 2 complete.

**Files:**
- Modify: `pkg/webui/dist/**` (only if a rebuild produces a diff)
- Modify: memory `…/memory/project_current_state.md`
- Modify: `docs/superpowers/notes/2026-06-21-forward-auth-handoff.md` (mark Phase 2 done)

**Acceptance Criteria:**
- [ ] `go vet ./... && go build -tags nodynamic ./... && go test ./...` all pass (re-run any flaky `pkg/server` test in isolation).
- [ ] `cd dashboard && npm run test -- --run && npx vue-tsc -b && node scripts/check-contrast.mjs` all pass.
- [ ] `npm run build` produces no uncommitted `pkg/webui/dist` diff (dist is current).
- [ ] Memory + handoff updated.

**Verify:** `go test ./... && cd dashboard && npm run build && cd .. && git status --porcelain pkg/webui/dist` → tests pass; no dist diff.

**Steps:**

- [ ] **Step 1: Backend gate** — Run: `go vet ./... && go build -tags nodynamic ./... && go test ./...`. If a `pkg/server` test flakes, re-run `go test ./pkg/server/ -run <name> -count=1` in isolation to confirm it's contention, not a regression.

- [ ] **Step 2: Frontend gate + dist currency** — Run: `cd dashboard && npm run test -- --run && npx vue-tsc -b && node scripts/check-contrast.mjs && npm run build && cd ..` then `git status --porcelain pkg/webui/dist`. If the build produced a diff, `git add pkg/webui/dist && git commit -m "chore(forward-auth): rebuild dist"`.

- [ ] **Step 3: Update memory** `…/memory/project_current_state.md` — append a Phase 2 entry: forward-auth admin UI (list/detail + Traefik snippet + en/zh i18n), per-app+SSO sign-out (`sign_out` + `sso-logout` with open-redirect guard), exclusive listing (FA apps out of the OIDC list), and the `dev:forward-auth` harness; gate-green; dist committed. Convert "Phase 2" to absolute terms.

- [ ] **Step 4: Update the handoff note** `docs/superpowers/notes/2026-06-21-forward-auth-handoff.md` — mark Phase 2 complete (admin UI + sign-out + dev harness shipped), with the commit range.

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/notes/2026-06-21-forward-auth-handoff.md
git commit -m "docs(forward-auth): mark Phase 2 complete"
```

(The memory file lives outside the repo working tree under `~/.claude/...` — it is written directly, not committed.)

---

## Self-Review

**Spec coverage:**
- §2A backend (queries, helper, contract, handlers, routes, exclusion guards) → Tasks 1–2. ✓
- §2A frontend (views, Traefik snippet, route, sidebar, i18n) → Task 3. ✓
- §2C sign-out (sign_out + sso-logout + open-redirect guard + docs) → Task 4. ✓
- Runtime verification → Task 5. ✓
- §2B dev harness → Task 6. ✓
- Testing & final gate + dist → Tasks 1–4 (unit) + 5 (runtime) + 7 (full gate). ✓

**Type consistency:** `forwardAuthAppView(...)` signature is identical in its definition (Task 2 Step 1) and all call sites (list/get/update/set-disabled). `RegisterForwardAuthApp(ctx, q db.Querier, clientID, host, displayName)` matches between definition (Task 1) and callers (CLI Task 1, handler Task 2). `ValidatedForwardAuthReturnURL(ctx, q, rd)` matches between Task 4 Step 1 (def) and Step 2 (call). `ForwardAuthCallbackURI(host)` matches across the helper, the CLI, and the PUT handler. Query param/row type names (`UpdateForwardAuthAppParams`, `ListNonForwardAuthOIDCClientsRow`, etc.) match the sqlc-generated names noted in Task 1 Step 2.

**Placeholder scan:** No TBD/TODO. Task 6 Step 1/2 explicitly instruct the implementer to read `dev_federation.go` first and mirror its `dev`-parent accessor (the only intentionally template-derived code, because it depends on the exact existing harness wiring); every other code block is complete.

**Known-unknowns resolved during planning:** `ListOIDCClients` IS shared with the CLI (`main.go:329`), so a new `ListNonForwardAuthOIDCClients` query is used for the admin list (CLI behavior unchanged). The logout path (`handleLogoutHTTP`) uses `sessstore.{SessionCookieNameFor,ParseCookieValue,ClearedSessionCookie}` + `s.sessionStore.Revoke`, which `sso-logout` mirrors directly.
</content>
