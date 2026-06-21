# App & Provider Icons Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let admins upload an icon for any app or provider (OIDC apps, SAML apps, upstream IdPs), serve it self-hosted, and show upstream-IdP icons on the `/login` "Sign in with…" and `/connected` "Connect…" buttons.

**Architecture:** A single `entity_icon` table keyed by `(owner_kind, owner_id)` holds a processed PNG + etag, reusing `branding.ProcessIcon` (center-crop → PNG 256² → sha256). Admin upload mirrors the instance-icon pattern (raw `PUT` with in-handler fresh-sudo; `DELETE` sudo-gated via the wrapper). A public `GET /icon/{kind}/{id}` serves it with ETag/304. The wire contracts gain a cache-busted `iconUrl`; a shared `AppIcon` component renders the icon or an initials fallback.

**Tech Stack:** Go (chi + huma + sqlc/pgx), Vue 3 + Vite + Tailwind v4 + shadcn-vue, vue-i18n (en + zh).

**Spec:** `docs/superpowers/specs/2026-06-22-app-provider-icons-design.md`

**Gate (every task):** `go vet ./... && go build -tags nodynamic ./... && go test ./...`; frontend tasks add `cd dashboard && npm run test`, `npx vue-tsc -b`, `node scripts/check-contrast.mjs`, and a **dist rebuild + commit** (`npm run build` → commit `pkg/webui/dist`). NEVER add a `Co-Authored-By` trailer. Work on `master`. `pkg/server` tests flake ~1/3 under parallel shared-DB runs (memory `reference_flaky_server_suite`) — re-run a failing one in isolation before treating it as real.

**Owner keys (used throughout):** `owner_kind ∈ {"oidc_client","saml_sp","upstream_idp"}`; `owner_id` = OIDC/forward-auth `client_id` · SAML `saml_sp.id` as a base-10 string · upstream IdP `slug`. Existence checks use `GetOIDCClientAny(clientId)`, `GetSAMLSPByID(id int64)`, `GetUpstreamIDPBySlugAny(slug)`. Audit factors: `audit.FactorOIDCClient`, `audit.FactorSAMLSP`, `audit.FactorUpstreamIDP`.

---

### Task 1: entity_icon storage — migration, queries, URL helper

**Goal:** Create the `entity_icon` table + its sqlc queries, and a pure `entityIconURL` helper for building cache-busted icon URLs.

**Files:**
- Create: `db/migrations/019_entity_icon.sql`
- Create: `db/queries/entity_icon.sql`
- Regenerate: `pkg/db/entity_icon.sql.go`, `pkg/db/models.go`, `pkg/db/querier.go` (via `sqlc generate`)
- Create: `pkg/server/entity_icon_url.go` (helper)
- Test: `pkg/server/entity_icon_url_test.go`

**Acceptance Criteria:**
- [ ] `sqlc generate` emits `SetEntityIcon`, `GetEntityIcon`, `GetEntityIconEtag`, `DeleteEntityIcon`, `ListEntityIconEtags`.
- [ ] `entityIconURL` returns `""` for an empty etag and `/icon/<kind>/<escaped id>?v=<etag[:8]>` otherwise.
- [ ] `go build -tags nodynamic ./...` passes.

**Verify:** `go build -tags nodynamic ./... && go test ./pkg/server/ -run EntityIconURL -v` → PASS

**Steps:**

- [ ] **Step 1: Migration `db/migrations/019_entity_icon.sql`**

```sql
-- +goose Up
CREATE TABLE entity_icon (
  owner_kind text        NOT NULL,
  owner_id   text        NOT NULL,
  png        bytea       NOT NULL,
  etag       text        NOT NULL,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (owner_kind, owner_id)
);

-- +goose Down
DROP TABLE entity_icon;
```

(Confirm the project's migration tool/format by skimming `db/migrations/018_forward_auth.sql` and match its goose annotation style exactly.)

- [ ] **Step 2: Queries `db/queries/entity_icon.sql`**

```sql
-- name: SetEntityIcon :exec
INSERT INTO entity_icon (owner_kind, owner_id, png, etag, updated_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (owner_kind, owner_id)
DO UPDATE SET png = $3, etag = $4, updated_at = now();

-- name: GetEntityIcon :one
SELECT png, etag FROM entity_icon WHERE owner_kind = $1 AND owner_id = $2;

-- name: GetEntityIconEtag :one
SELECT etag FROM entity_icon WHERE owner_kind = $1 AND owner_id = $2;

-- name: DeleteEntityIcon :exec
DELETE FROM entity_icon WHERE owner_kind = $1 AND owner_id = $2;

-- name: ListEntityIconEtags :many
SELECT owner_id, etag FROM entity_icon WHERE owner_kind = $1;
```

- [ ] **Step 3: Regenerate** — Run `sqlc generate` from repo root (sqlc 1.30.0 via mise; if not on PATH, `mise exec -- sqlc generate`). Confirm `pkg/db/querier.go` gained the five methods and `pkg/db/models.go` gained an `EntityIcon` struct. Generated param/row type names: `SetEntityIconParams{OwnerKind, OwnerID, Png, Etag}`, `GetEntityIconParams{OwnerKind, OwnerID}`, `GetEntityIconRow{Png []byte, Etag string}`, `GetEntityIconEtagParams{OwnerKind, OwnerID}`, `ListEntityIconEtagsRow{OwnerID, Etag}`. (Verify exact names by grepping the generated file; adjust later tasks if they differ.)

- [ ] **Step 4: Helper `pkg/server/entity_icon_url.go`**

```go
// Package server — entity_icon_url.go
// Shared helper for building the public, cache-busted icon URL for an entity.
package server

import "net/url"

// entityIconKinds is the fixed allowlist of icon owner kinds.
var entityIconKinds = map[string]bool{
	"oidc_client":  true,
	"saml_sp":      true,
	"upstream_idp": true,
}

// entityIconURL returns the public icon URL for (kind, id), cache-busted by the
// first 8 chars of the etag. Returns "" when etag is empty (no icon), so callers
// can map that to a nil *string in the wire view.
func entityIconURL(kind, id, etag string) string {
	if etag == "" {
		return ""
	}
	v := etag
	if len(v) > 8 {
		v = v[:8]
	}
	return "/icon/" + kind + "/" + url.PathEscape(id) + "?v=" + v
}
```

- [ ] **Step 5: Test `pkg/server/entity_icon_url_test.go`**

```go
package server

import "testing"

func TestEntityIconURL(t *testing.T) {
	t.Parallel()
	if got := entityIconURL("oidc_client", "my-app", ""); got != "" {
		t.Errorf("empty etag should yield empty URL, got %q", got)
	}
	got := entityIconURL("oidc_client", "my-app", "abcdef0123456789")
	if got != "/icon/oidc_client/my-app?v=abcdef01" {
		t.Errorf("got %q", got)
	}
	// id is path-escaped (saml uses a numeric id; upstream a slug — both safe,
	// but a stray space/slash must escape).
	if got := entityIconURL("saml_sp", "a b", "deadbeef"); got != "/icon/saml_sp/a%20b?v=deadbeef" {
		t.Errorf("escape: got %q", got)
	}
	if !entityIconKinds["upstream_idp"] || entityIconKinds["bogus"] {
		t.Error("entityIconKinds allowlist wrong")
	}
}
```

- [ ] **Step 6: Gate + commit**

```bash
go build -tags nodynamic ./... && go vet ./... && go test ./pkg/server/ -run EntityIconURL -v
git add db/migrations/019_entity_icon.sql db/queries/entity_icon.sql pkg/db/ pkg/server/entity_icon_url.go pkg/server/entity_icon_url_test.go
git commit -m "feat(icons): entity_icon table, queries, and URL helper"
```

---

### Task 2: Admin upload + delete handlers, routes, owner-delete cleanup

**Goal:** Per-entity `PUT`/`DELETE …/icon` admin endpoints (reusing `branding.ProcessIcon`), and delete the icon row when its owner is deleted.

**Files:**
- Create: `pkg/server/handle_admin_entity_icon.go`
- Modify: `pkg/server/server.go` (register 6 routes)
- Modify: `pkg/server/handle_admin_oidc_clients.go` (`handleDeleteOIDCApplicationHTTP` — icon cleanup)
- Modify: `pkg/server/handle_admin_saml_sps.go` (`handleDeleteSAMLApplicationHTTP` — icon cleanup)
- Modify: `pkg/server/handle_admin_upstream_idps.go` (`handleDeleteIdentityProviderHTTP` — icon cleanup)
- Modify: `pkg/server/admin_route_policy_test.go` (`sudoGatedRoutes` — the 3 DELETEs)

**Acceptance Criteria:**
- [ ] `PUT …/{oidc-applications/{id}|saml-applications/{id}|identity-providers/{slug}}/icon` stores a processed icon (admin + fresh sudo); 404 for an unknown owner; 413-ish `avatar_too_large` / `avatar_invalid_image` mapping.
- [ ] The 3 `DELETE …/icon` are sudo-gated and remove the row.
- [ ] Deleting an app/IdP removes its `entity_icon` row.
- [ ] `TestAdminMutationRoutesRequireSudo` passes with the 3 DELETE routes listed.

**Verify:** `go build -tags nodynamic ./... && go vet ./... && go test ./pkg/server/ -run 'AdminMutationRoutesRequireSudo' -v` → PASS

**Steps:**

- [ ] **Step 1: Read the template** — open `pkg/server/handle_admin_settings.go` (`handlePutInstanceIconHTTP`, `handleDeleteInstanceIconHTTP`, `maxIconRead`) and mirror its structure: `requireFreshSudo` in-handler for the raw `PUT`, `writeAvatarErr` for the two image errors, `ErrTooLarge` mapping.

- [ ] **Step 2: Create `pkg/server/handle_admin_entity_icon.go`**

```go
// Package server — handle_admin_entity_icon.go
//
// Admin per-entity icon upload/remove for OIDC apps, SAML apps, and upstream
// IdPs. Mirrors the instance-icon pattern (handle_admin_settings.go): the raw
// image PUT is registered via registerOpHTTP(admin) with an in-handler
// requireFreshSudo (the sudo wrapper rejects non-JSON bodies + caps at 64 KiB);
// the DELETE is registered via registerSudoOpHTTP (admin + sudo). Icons are
// processed by branding.ProcessIcon (center-crop → PNG 256²) and stored in
// entity_icon keyed by (owner_kind, owner_id).
package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/branding"
	"prohibitorum/pkg/db"
)

const maxEntityIconRead = 5<<20 + 1

// putEntityIcon is the shared upload core: fresh-sudo gate, read+process the
// image, upsert the row, audit. Callers verify the owner exists first.
func (s *Server) putEntityIcon(w http.ResponseWriter, r *http.Request, kind, id string, factor audit.Factor) {
	sess := authn.SessionFromContext(r.Context())
	if s.requireFreshSudo(r.Context(), w, sess) {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxEntityIconRead))
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	png, etag, perr := branding.ProcessIcon(raw)
	if perr != nil {
		if errors.Is(perr, branding.ErrTooLarge) {
			writeAvatarErr(w, "avatar_too_large", "icon: image exceeds 5 MiB")
			return
		}
		writeAvatarErr(w, "avatar_invalid_image", "icon: invalid or unsupported image format")
		return
	}
	if err := s.queries.SetEntityIcon(r.Context(), db.SetEntityIconParams{
		OwnerKind: kind, OwnerID: id, Png: png, Etag: etag,
	}); err != nil {
		writeAuthErr(w, err)
		return
	}
	s.auditEntityIcon(r, factor, kind, id, "icon_updated")
	w.WriteHeader(http.StatusNoContent)
}

// deleteEntityIcon removes the icon row. Callers verify the owner exists first.
func (s *Server) deleteEntityIcon(w http.ResponseWriter, r *http.Request, kind, id string, factor audit.Factor) {
	if err := s.queries.DeleteEntityIcon(r.Context(), db.DeleteEntityIconParams{OwnerKind: kind, OwnerID: id}); err != nil {
		writeAuthErr(w, err)
		return
	}
	s.auditEntityIcon(r, factor, kind, id, "icon_removed")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) auditEntityIcon(r *http.Request, factor audit.Factor, kind, id, reason string) {
	var acct *int32
	if sess := authn.SessionFromContext(r.Context()); sess != nil && sess.Account != nil {
		v := sess.Account.ID
		acct = &v
	}
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: acct,
		Factor:    factor,
		Event:     audit.EventUpdate,
		IP:        audit.ParseIPOrNil(r.RemoteAddr),
		UserAgent: r.UserAgent(),
		Detail:    map[string]any{"reason": reason, "owner_kind": kind, "owner_id": id},
	})
}

// ----- OIDC (covers forward-auth, which is an oidc_client) -------------------

func (s *Server) handlePutOIDCAppIconHTTP(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "clientId")
	if _, err := s.queries.GetOIDCClientAny(r.Context(), id); err != nil {
		writeAuthErr(w, authn.ErrClientNotFound())
		return
	}
	s.putEntityIcon(w, r, "oidc_client", id, audit.FactorOIDCClient)
}

func (s *Server) handleDeleteOIDCAppIconHTTP(w http.ResponseWriter, r *http.Request) {
	s.deleteEntityIcon(w, r, "oidc_client", chi.URLParam(r, "clientId"), audit.FactorOIDCClient)
}

// ----- SAML ------------------------------------------------------------------

func (s *Server) handlePutSAMLAppIconHTTP(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if _, err := s.queries.GetSAMLSPByID(r.Context(), id); err != nil {
		writeAuthErr(w, samlSPNotFound())
		return
	}
	s.putEntityIcon(w, r, "saml_sp", idStr, audit.FactorSAMLSP)
}

func (s *Server) handleDeleteSAMLAppIconHTTP(w http.ResponseWriter, r *http.Request) {
	s.deleteEntityIcon(w, r, "saml_sp", chi.URLParam(r, "id"), audit.FactorSAMLSP)
}

// ----- Upstream IdP ----------------------------------------------------------

func (s *Server) handlePutIdentityProviderIconHTTP(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if _, err := s.queries.GetUpstreamIDPBySlugAny(r.Context(), slug); err != nil {
		writeAuthErr(w, authn.ErrUpstreamIDPNotFound())
		return
	}
	s.putEntityIcon(w, r, "upstream_idp", slug, audit.FactorUpstreamIDP)
}

func (s *Server) handleDeleteIdentityProviderIconHTTP(w http.ResponseWriter, r *http.Request) {
	s.deleteEntityIcon(w, r, "upstream_idp", chi.URLParam(r, "slug"), audit.FactorUpstreamIDP)
}

var _ = pgx.ErrNoRows // keep pgx import if unused after edits; remove if go vet complains
```

VERIFY while writing: the not-found helpers — OIDC uses `authn.ErrClientNotFound()`, SAML uses `samlSPNotFound()` (used in `handle_admin_app_access.go`), upstream uses whatever `handleGetIdentityProvider` returns for not-found (grep it — likely `authn.ErrUpstreamIDPNotFound()` or an `upstream_idp_not_found` error; match it exactly). Drop the trailing `var _ = pgx.ErrNoRows` line and the `pgx` import if not otherwise needed (it's only a guard; `go build` will tell you).

- [ ] **Step 3: Register routes in `pkg/server/server.go`** — add a block near the OIDC/SAML/identity-provider admin routes (after the forward-auth-apps block is fine):

```go
	// Admin: per-entity icon upload/remove (app & provider icons). PUT is raw
	// image + in-handler fresh sudo (the sudo wrapper rejects non-JSON bodies);
	// DELETE is sudo-gated via the wrapper. Mirrors the instance-icon pattern.
	registerOpHTTP(s.router, "PUT", "/api/prohibitorum/oidc-applications/{clientId}/icon", admin, s.handlePutOIDCAppIconHTTP)
	s.registerSudoOpHTTP(s.router, "DELETE", "/api/prohibitorum/oidc-applications/{clientId}/icon", admin, s.handleDeleteOIDCAppIconHTTP)
	registerOpHTTP(s.router, "PUT", "/api/prohibitorum/saml-applications/{id}/icon", admin, s.handlePutSAMLAppIconHTTP)
	s.registerSudoOpHTTP(s.router, "DELETE", "/api/prohibitorum/saml-applications/{id}/icon", admin, s.handleDeleteSAMLAppIconHTTP)
	registerOpHTTP(s.router, "PUT", "/api/prohibitorum/identity-providers/{slug}/icon", admin, s.handlePutIdentityProviderIconHTTP)
	s.registerSudoOpHTTP(s.router, "DELETE", "/api/prohibitorum/identity-providers/{slug}/icon", admin, s.handleDeleteIdentityProviderIconHTTP)
```

- [ ] **Step 4: Owner-delete cleanup** — in each of the three delete handlers, after the successful delete (before the audit/204), remove the icon row. Use the same `owner_id` form as the routes.

In `handleDeleteOIDCApplicationHTTP` (`handle_admin_oidc_clients.go`), after `DeleteOIDCClient` returns `rows>0`:
```go
	_ = s.queries.DeleteEntityIcon(r.Context(), db.DeleteEntityIconParams{OwnerKind: "oidc_client", OwnerID: body.ClientID})
```
In `handleDeleteSAMLApplicationHTTP` (`handle_admin_saml_sps.go`), after the SP delete (use the int64 id stringified with `strconv.FormatInt(id, 10)`):
```go
	_ = s.queries.DeleteEntityIcon(r.Context(), db.DeleteEntityIconParams{OwnerKind: "saml_sp", OwnerID: strconv.FormatInt(id, 10)})
```
In `handleDeleteIdentityProviderHTTP` (`handle_admin_upstream_idps.go`), after the IdP delete (use the slug from the request body/param):
```go
	_ = s.queries.DeleteEntityIcon(r.Context(), db.DeleteEntityIconParams{OwnerKind: "upstream_idp", OwnerID: slug})
```
Match each handler's existing variable names (`body.ClientID`, the parsed `id`, the `slug`) — read each handler first. Add `strconv` / `db` imports if missing.

- [ ] **Step 5: Policy test** — in `pkg/server/admin_route_policy_test.go`, add to `sudoGatedRoutes` (the DELETEs are wrapper-sudo-gated; the PUTs use in-handler sudo and are NOT in this table, exactly like the instance-icon `PUT`):

```go
	// Entity icon removal (app & provider icons)
	{method: "DELETE", path: "/api/prohibitorum/oidc-applications/test-client/icon", body: `{}`},
	{method: "DELETE", path: "/api/prohibitorum/saml-applications/1/icon", body: `{}`},
	{method: "DELETE", path: "/api/prohibitorum/identity-providers/test-idp/icon", body: `{}`},
```

- [ ] **Step 6: Gate + commit**

```bash
go build -tags nodynamic ./... && go vet ./... && go test ./pkg/server/ -run 'AdminMutationRoutesRequireSudo' -v -count=1
git add pkg/server/handle_admin_entity_icon.go pkg/server/server.go pkg/server/handle_admin_oidc_clients.go pkg/server/handle_admin_saml_sps.go pkg/server/handle_admin_upstream_idps.go pkg/server/admin_route_policy_test.go
git commit -m "feat(icons): admin upload/remove icon endpoints + owner-delete cleanup"
```

---

### Task 3: Public icon serve — `GET /icon/{kind}/{id}`

**Goal:** Serve the stored PNG publicly with ETag/304/404/400.

**Files:**
- Create: `pkg/server/handle_entity_icon.go`
- Modify: `pkg/server/server.go` (register the public route)

**Acceptance Criteria:**
- [ ] `GET /icon/{kind}/{id}` → 200 `image/png` + `ETag` + `Cache-Control: public, max-age=300`; `304` when `If-None-Match` matches; `404` when no icon; `400` on an unknown `kind`.

**Verify:** `go build -tags nodynamic ./... && go vet ./...` (behavior is runtime-verified in Task 7; the public route is covered there).

**Steps:**

- [ ] **Step 1: Read the template** — `pkg/server/handle_branding.go` `handleGetBrandingIconHTTP` (the exact ETag/304/Cache-Control shape).

- [ ] **Step 2: Create `pkg/server/handle_entity_icon.go`**

```go
// Package server — handle_entity_icon.go
// Public icon serve for apps & providers: GET /icon/{kind}/{id} → the stored
// PNG with ETag/304. Public because the /login page (pre-auth) shows IdP icons;
// icons are not sensitive. Mirrors handle_branding.go's serve shape.
package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"prohibitorum/pkg/db"
)

func (s *Server) handleGetEntityIconHTTP(w http.ResponseWriter, r *http.Request) {
	kind := chi.URLParam(r, "kind")
	id := chi.URLParam(r, "id")
	if !entityIconKinds[kind] {
		http.Error(w, "bad kind", http.StatusBadRequest)
		return
	}
	row, err := s.queries.GetEntityIcon(r.Context(), db.GetEntityIconParams{OwnerKind: kind, OwnerID: id})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	quoted := `"` + row.Etag + `"`
	if r.Header.Get("If-None-Match") == quoted {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("ETag", quoted)
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(row.Png)
}
```

- [ ] **Step 3: Register the public route in `pkg/server/server.go`** — next to `GET /branding/icon`:

```go
	registerOpHTTP(s.router, "GET", "/icon/{kind}/{id}", publicReq, s.handleGetEntityIconHTTP)
```

(Confirm `publicReq` / the public `AuthRequirement` is the same value used by `/branding/icon` registration — match it.)

- [ ] **Step 4: Gate + commit**

```bash
go build -tags nodynamic ./... && go vet ./...
git add pkg/server/handle_entity_icon.go pkg/server/server.go
git commit -m "feat(icons): public GET /icon/{kind}/{id} serve with ETag/304"
```

---

### Task 4: Expose `iconUrl` in the wire contracts (backend)

**Goal:** Add `IconURL` to `FederationProvider` (federation list join) and to the four admin app/provider GET views, sourced from the icon etag.

**Files:**
- Modify: `pkg/contract/auth.go` (`FederationProvider`, `OIDCApplicationView`, `ForwardAuthAppView`, `SAMLApplicationView`, `IdentityProviderView`)
- Modify: `pkg/server/handle_federation.go` (`listFedQueries` + `handleListFederationProvidersHTTP`)
- Modify: `pkg/server/handle_admin_oidc_clients.go` (`handleGetOIDCApplication`)
- Modify: `pkg/server/handle_admin_forward_auth_apps.go` (`handleGetForwardAuthApp`)
- Modify: `pkg/server/handle_admin_saml_sps.go` (`handleGetSAMLApplication`)
- Modify: `pkg/server/handle_admin_upstream_idps.go` (`handleGetIdentityProvider`)
- Modify: `pkg/server/handle_federation_test.go` (assert `IconURL`)

**Acceptance Criteria:**
- [ ] `GET /auth/federation` returns `iconUrl` per provider (set when an icon exists, omitted/nil otherwise).
- [ ] Each admin GET view returns `iconUrl` for its entity.

**Verify:** `go build -tags nodynamic ./... && go vet ./... && go test ./pkg/server/ -run 'FederationProviders' -v` → PASS

**Steps:**

- [ ] **Step 1: Contract fields (`pkg/contract/auth.go`)** — add to `FederationProvider`:
```go
	IconURL *string `json:"iconUrl,omitempty"`
```
and add the same `IconURL *string \`json:"iconUrl,omitempty"\`` field to `OIDCApplicationView`, `ForwardAuthAppView`, `SAMLApplicationView`, and `IdentityProviderView`.

- [ ] **Step 2: A small server helper to set the optional pointer** — add to `pkg/server/entity_icon_url.go`:
```go
// entityIconURLPtr returns a *string icon URL (nil when no icon) for a view.
func entityIconURLPtr(kind, id, etag string) *string {
	if u := entityIconURL(kind, id, etag); u != "" {
		return &u
	}
	return nil
}

// lookupEntityIconEtag returns the icon etag for (kind,id), or "" when none.
func (s *Server) lookupEntityIconEtag(ctx context.Context, kind, id string) string {
	etag, err := s.queries.GetEntityIconEtag(ctx, db.GetEntityIconEtagParams{OwnerKind: kind, OwnerID: id})
	if err != nil {
		return ""
	}
	return etag
}
```
Add the `context` + `prohibitorum/pkg/db` imports to that file.

- [ ] **Step 3: Federation list join (`handle_federation.go`)** — extend `listFedQueries`:
```go
type listFedQueries interface {
	ListUpstreamIDPs(ctx context.Context) ([]db.UpstreamIdp, error)
	ListEntityIconEtags(ctx context.Context, ownerKind string) ([]db.ListEntityIconEtagsRow, error)
}
```
and in `handleListFederationProvidersHTTP`, build a slug→etag map and set `IconURL`:
```go
	icons, _ := s.listFedQ().ListEntityIconEtags(r.Context(), "upstream_idp")
	etagBySlug := make(map[string]string, len(icons))
	for _, ic := range icons {
		etagBySlug[ic.OwnerID] = ic.Etag
	}
	out := make([]contract.FederationProvider, 0, len(idps))
	for _, idp := range idps {
		out = append(out, contract.FederationProvider{
			Slug:        idp.Slug,
			DisplayName: idp.DisplayName,
			IconURL:     entityIconURLPtr("upstream_idp", idp.Slug, etagBySlug[idp.Slug]),
		})
	}
```
(The test fake for `listFedOverride` must now also implement `ListEntityIconEtags` — see Step 6.)

- [ ] **Step 4: Admin GET views** — in each of the four GET handlers, after the entity is loaded and its view `v` is built, set the icon URL before returning. OIDC (`handleGetOIDCApplication`, key `c.ClientID`):
```go
	view := oidcApplicationView(c)
	view.IconURL = entityIconURLPtr("oidc_client", c.ClientID, s.lookupEntityIconEtag(ctx, "oidc_client", c.ClientID))
	return &oidcApplicationOut{Body: view}, nil
```
Forward-auth (`handleGetForwardAuthApp`, key the client id): set `IconURL` on the `forwardAuthAppView(...)` result via `entityIconURLPtr("oidc_client", r.ClientID, s.lookupEntityIconEtag(ctx, "oidc_client", r.ClientID))` (forward-auth apps are `oidc_client` rows, so the kind is `"oidc_client"` — the SAME icon as the OIDC view; that's correct, one backing client = one icon). SAML (`handleGetSAMLApplication`, key `strconv.FormatInt(sp.ID, 10)`, kind `"saml_sp"`). Upstream (`handleGetIdentityProvider`, key the slug, kind `"upstream_idp"`). Mirror the OIDC snippet's shape in each; read each handler to match its `ctx`/return-shape (the typed huma handlers use `ctx`; confirm `s.lookupEntityIconEtag(ctx, …)`).

- [ ] **Step 5: Build check** — `go build -tags nodynamic ./...`.

- [ ] **Step 6: Federation test** — in `pkg/server/handle_federation_test.go`, the existing fake backing `listFedOverride` must implement the new interface method:
```go
func (f *fakeListFed) ListEntityIconEtags(_ context.Context, _ string) ([]db.ListEntityIconEtagsRow, error) {
	return f.iconEtags, nil // add an `iconEtags []db.ListEntityIconEtagsRow` field to the fake
}
```
(Match the existing fake's name/shape — grep `listFedOverride` in the test.) Add/extend a test asserting `IconURL` is set when `iconEtags` has the slug and nil when it doesn't.

- [ ] **Step 7: Gate + commit**

```bash
go build -tags nodynamic ./... && go vet ./... && go test ./pkg/server/ -run 'FederationProviders' -v -count=1
git add pkg/contract/auth.go pkg/server/entity_icon_url.go pkg/server/handle_federation.go pkg/server/handle_federation_test.go pkg/server/handle_admin_oidc_clients.go pkg/server/handle_admin_forward_auth_apps.go pkg/server/handle_admin_saml_sps.go pkg/server/handle_admin_upstream_idps.go
git commit -m "feat(icons): expose iconUrl on federation list + admin app/provider views"
```

---

### Task 5: `AppIcon` + `EntityIconUpload` components, admin detail views, i18n

**Goal:** A shared `AppIcon` (icon-or-initials), a reusable `EntityIconUpload` (mirroring the instance-icon `SettingsView` block), wired into the four admin detail views, with en/zh strings.

**Files:**
- Create: `dashboard/src/components/custom/AppIcon.vue` (+ `.test.ts`)
- Create: `dashboard/src/components/custom/EntityIconUpload.vue` (+ `.test.ts`)
- Modify: `dashboard/src/pages/admin/AdminOidcClientDetailView.vue`, `AdminSamlProviderDetailView.vue`, `AdminForwardAuthAppDetailView.vue`, `AdminUpstreamIdpDetailView.vue`
- Modify: `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts`
- Rebuild: `pkg/webui/dist/**`

**Acceptance Criteria:**
- [ ] `AppIcon` shows the image when `src` is set, else an initials-on-tint fallback (mirroring `UserAvatar`).
- [ ] `EntityIconUpload` shows the current icon + Upload/Remove (via `withSudo`); upload `PUT`s the raw file, remove `DELETE`s; emits `changed`.
- [ ] Each of the four detail views shows the icon card; the view's GET supplies `iconUrl`.
- [ ] `vue-tsc -b` clean, vitest passes, contrast passes, en/zh parity passes.

**Verify:** `cd dashboard && npx vue-tsc -b && npm run test -- --run AppIcon EntityIconUpload && node scripts/check-contrast.mjs` → PASS

**Steps:**

- [ ] **Step 1: `dashboard/src/components/custom/AppIcon.vue`** (modeled on `UserAvatar.vue`'s fallback)

```vue
<script setup lang="ts">
/** AppIcon — an app/provider icon: image (src) → initial-letter fallback. */
import { computed, ref, watch } from 'vue'
import { cn } from '@/lib/utils'

const props = withDefaults(defineProps<{
  src?: string | null
  name?: string | null
  size?: 'sm' | 'md'
}>(), { size: 'md' })

const failed = ref(false)
watch(() => props.src, () => { failed.value = false })
const showImg = computed(() => !!props.src && !failed.value)

const initial = computed(() => {
  const n = (props.name ?? '').trim()
  return n ? n[0]!.toUpperCase() : '?'
})

const sizeClass = computed(() => (props.size === 'sm' ? 'size-6 text-xs' : 'size-10 text-base'))
</script>

<template>
  <span
    aria-hidden="true"
    :class="cn('inline-flex shrink-0 items-center justify-center overflow-hidden rounded-md bg-accent font-semibold text-ink', sizeClass)"
  >
    <img v-if="showImg" :src="src!" alt="" loading="lazy" class="size-full object-cover" @error="failed = true" />
    <template v-else>{{ initial }}</template>
  </span>
</template>
```

- [ ] **Step 2: `AppIcon.test.ts`**

```ts
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import AppIcon from './AppIcon.vue'

describe('AppIcon', () => {
  it('renders the image when src is set', () => {
    const w = mount(AppIcon, { props: { src: '/icon/upstream_idp/google?v=abc', name: 'Google' } })
    expect(w.find('img').exists()).toBe(true)
    expect(w.find('img').attributes('src')).toContain('/icon/upstream_idp/google')
  })
  it('falls back to the initial when no src', () => {
    const w = mount(AppIcon, { props: { name: 'Google' } })
    expect(w.find('img').exists()).toBe(false)
    expect(w.text()).toBe('G')
  })
  it('falls back to the initial on image error', async () => {
    const w = mount(AppIcon, { props: { src: '/bad', name: 'Okta' } })
    await w.find('img').trigger('error')
    expect(w.find('img').exists()).toBe(false)
    expect(w.text()).toBe('O')
  })
})
```

- [ ] **Step 3: Read the upload template** — `dashboard/src/pages/admin/SettingsView.vue` (the icon `<Card>`: `onPickFile` → `api.upload`, `removeIcon` → `api.del`, both `withSudo`; the preview `<img>`). `EntityIconUpload` generalizes it for any `(kind,id)`.

- [ ] **Step 4: `dashboard/src/components/custom/EntityIconUpload.vue`**

```vue
<script setup lang="ts">
/**
 * EntityIconUpload — admin icon upload/remove for an app or provider, modeled on
 * the instance-icon block in SettingsView. Shows the current icon (AppIcon) and
 * Upload / Remove buttons; both mutations go through withSudo. Emits `changed`
 * so the parent refetches (which re-supplies iconUrl).
 *
 * basePath examples: /api/prohibitorum/oidc-applications/<clientId>
 *                    /api/prohibitorum/saml-applications/<id>
 *                    /api/prohibitorum/identity-providers/<slug>
 */
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import AppIcon from '@/components/custom/AppIcon.vue'

const props = defineProps<{ basePath: string; name: string; iconUrl?: string | null }>()
const emit = defineEmits<{ changed: [] }>()

const { t } = useI18n()
const { busy, run, errorText } = useApi()
const fileInput = ref<HTMLInputElement | null>(null)

async function onPick(e: Event): Promise<void> {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  const ok = await run(() => withSudo(async () => {
    await api.upload(`${props.basePath}/icon`, file)
    return true as const
  }, t('sudo.reason.saveChanges')))
  if (fileInput.value) fileInput.value.value = ''
  if (ok) emit('changed')
}

async function remove(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.del(`${props.basePath}/icon`)
    return true as const
  }, t('sudo.reason.saveChanges')))
  if (ok) emit('changed')
}
</script>

<template>
  <Card>
    <CardHeader><CardTitle>{{ t('entityIcon.title') }}</CardTitle></CardHeader>
    <CardContent class="flex flex-col gap-3">
      <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
      <div class="flex items-center gap-4">
        <span class="rounded-md ring-1 ring-inset ring-border">
          <AppIcon :src="iconUrl" :name="name" />
        </span>
        <div class="flex flex-col gap-2">
          <p class="text-xs text-muted">{{ t('entityIcon.hint') }}</p>
          <div class="flex gap-2">
            <input ref="fileInput" type="file" accept="image/png,image/jpeg,image/webp" class="hidden" data-test="icon-input" @change="onPick" />
            <Button variant="outline" size="sm" :disabled="busy" data-test="icon-upload" @click="fileInput?.click()">{{ t('entityIcon.upload') }}</Button>
            <Button v-if="iconUrl" variant="outline" size="sm" :disabled="busy" data-test="icon-remove" @click="remove">{{ t('entityIcon.remove') }}</Button>
          </div>
        </div>
      </div>
    </CardContent>
  </Card>
</template>
```

(Confirm `api.upload` / `api.del` signatures in `dashboard/src/lib/api.ts` — `SettingsView` uses both; mirror its calls exactly.)

- [ ] **Step 5: `EntityIconUpload.test.ts`** — mock `@/lib/api` (`upload`, `del`) + `@/lib/sudo` (`withSudo` pass-through), mount with a `basePath`, assert: clicking upload after setting a file calls `api.upload('<base>/icon', file)` and emits `changed`; Remove shows only when `iconUrl` is set and calls `api.del('<base>/icon')`. (Mirror the `AppAccessCard.test.ts` mocking idiom.)

- [ ] **Step 6: i18n** — add an `entityIcon` block to `dashboard/src/locales/en.ts`:
```ts
  entityIcon: {
    title: 'Icon',
    hint: 'Shown on the launchpad and the sign-in buttons. A square PNG, JPEG, or WebP; centered and resized to 256×256.',
    upload: 'Upload',
    remove: 'Remove',
  },
```
and the parity block in `dashboard/src/locales/zh.ts`:
```ts
  entityIcon: {
    title: '图标',
    hint: '显示在应用面板和登录按钮上。支持方形 PNG、JPEG 或 WebP；会居中裁剪并缩放为 256×256。',
    upload: '上传',
    remove: '移除',
  },
```
After editing en.ts, grep-verify no curly apostrophes: `grep -nP "[\x{2018}\x{2019}]" dashboard/src/locales/en.ts` → empty. (No literal `@` in these strings.)

- [ ] **Step 7: Wire into the four detail views** — in each of `AdminOidcClientDetailView.vue`, `AdminSamlProviderDetailView.vue`, `AdminForwardAuthAppDetailView.vue`, `AdminUpstreamIdpDetailView.vue`: import `EntityIconUpload`; add `iconUrl?: string | null` to the view's TS interface; render the card after the main config card (and before/around `AppAccessCard` / the danger zone — place it consistently, e.g. right after the config `Card`), passing `:base-path`, `:name`, `:icon-url`, and `@changed="load"` (call the view's existing reload fn). Examples per view:
  - OIDC: `<EntityIconUpload :base-path="`/api/prohibitorum/oidc-applications/${clientId}`" :name="client?.displayName ?? clientId" :icon-url="client?.iconUrl" @changed="load" />`
  - forward-auth: base `/api/prohibitorum/forward-auth-apps/${clientId}` — BUT the icon UPLOAD route is under `oidc-applications/{clientId}/icon` (forward-auth apps are oidc_clients; there is no `forward-auth-apps/{id}/icon` route). So for the forward-auth detail view use base `/api/prohibitorum/oidc-applications/${clientId}` for the icon component. (The forward-auth GET still returns `iconUrl` pointing at `/icon/oidc_client/<clientId>`.)
  - SAML: base `/api/prohibitorum/saml-applications/${id}`, name = the SP display name, iconUrl from the loaded view.
  - upstream: base `/api/prohibitorum/identity-providers/${slug}`, name = the IdP display name, iconUrl from the loaded view.

  Read each view first and match its variable names + reload-fn name.

- [ ] **Step 8: FE gate + dist rebuild + commit**

```bash
cd dashboard && npx vue-tsc -b && npm run test -- --run && node scripts/check-contrast.mjs && npm run build && cd ..
grep -nP "[\x{2018}\x{2019}]" dashboard/src/locales/en.ts   # expect no output
git add dashboard/src pkg/webui/dist
git commit -m "feat(icons): AppIcon + EntityIconUpload, wired into the 4 admin detail views"
```

---

### Task 6: Show provider icons on the login + connect buttons

**Goal:** Render `AppIcon` on the `/login` "Sign in with…" buttons and the `/connected` "Connect…" buttons + linked-identity rows.

**Files:**
- Modify: `dashboard/src/components/custom/FederationButtons.vue`
- Modify: `dashboard/src/pages/ConnectedAccountsView.vue`
- Modify: `dashboard/src/components/custom/FederationButtons.test.ts` (if present; else add a minimal test)
- Rebuild: `pkg/webui/dist/**`

**Acceptance Criteria:**
- [ ] `/login` federation buttons show the provider icon (or initial fallback) before the label.
- [ ] `/connected` connect buttons (and linked-identity rows) show the provider icon.
- [ ] `vue-tsc -b` clean, vitest passes.

**Verify:** `cd dashboard && npx vue-tsc -b && npm run test -- --run FederationButtons ConnectedAccounts` → PASS

**Steps:**

- [ ] **Step 1: `FederationButtons.vue`** — extend the `FederationProvider` interface with `iconUrl?: string | null`; import `AppIcon`; render it inside the button before the label:
```ts
interface FederationProvider {
  slug: string
  displayName: string
  iconUrl?: string | null
}
```
```vue
      <Button
        v-for="p in providers"
        :key="p.slug"
        type="button"
        variant="outline"
        class="w-full justify-start gap-2"
        @click="startFederation(p.slug)"
      >
        <AppIcon :src="p.iconUrl" :name="p.displayName" size="sm" />
        <span>{{ p.displayName }}</span>
      </Button>
```
(Add `import AppIcon from '@/components/custom/AppIcon.vue'`.)

- [ ] **Step 2: `ConnectedAccountsView.vue`** — extend `interface Provider` with `iconUrl?: string | null`; import `AppIcon`; in the connect buttons (the `v-for="p in providers"` block) render the icon before the name; in the linked-identity cards render `<AppIcon :src="`/icon/upstream_idp/${ident.idpSlug}`" :name="ident.idpDisplayName" size="sm" />` (the linked rows have no iconUrl from `/me/identities`, so build the URL from `idpSlug`; AppIcon falls back to the initial if there's no icon). Keep the existing `justify-between` layout, wrapping the name + icon in a left group:
```vue
        <Button v-for="p in providers" :key="p.slug" type="button" variant="outline" class="w-full justify-between"
                :disabled="linkedSlugs.has(p.slug) || busy" :data-test="`link-${p.slug}`" @click="link(p.slug)">
          <span class="flex min-w-0 items-center gap-2">
            <AppIcon :src="p.iconUrl" :name="p.displayName" size="sm" />
            <span class="truncate">{{ p.displayName }}</span>
          </span>
          <StatusBadge v-if="linkedSlugs.has(p.slug)" variant="success" class="shrink-0">{{ t('connected.alreadyLinked') }}</StatusBadge>
        </Button>
```

- [ ] **Step 3: Tests** — if `FederationButtons.test.ts` exists, add an assertion that a provider with `iconUrl` renders an `AppIcon` img and one without renders the initial; mirror its existing mount/mock. If it doesn't exist, add a minimal one (mock `@/lib/api` `get` to return `[{slug,displayName,iconUrl}]`, mount, assert the icon). Update `ConnectedAccountsView` tests if any assert the connect-button markup.

- [ ] **Step 4: FE gate + dist rebuild + commit**

```bash
cd dashboard && npx vue-tsc -b && npm run test -- --run && node scripts/check-contrast.mjs && npm run build && cd ..
git add dashboard/src pkg/webui/dist
git commit -m "feat(icons): render provider icons on login + connect buttons"
```

---

### Task 7: Runtime verification + final gate + memory

**Goal:** Prove the icon flow end-to-end on the public `/login` page, run the full gate, confirm dist is current, update memory.

**Files:** none (verification; any fix lands in the owning task's files), memory `…/memory/project_current_state.md`.

**Acceptance Criteria:**
- [ ] Uploading an IdP icon makes `/login` show it; `GET /icon/upstream_idp/{slug}` returns the PNG (200) + `304` on `If-None-Match`; an unknown `kind` → 400; no icon → 404.
- [ ] Full gate green; `npm run build` leaves no dist diff.

**Verify:** the runtime block below + `go test ./...` + the FE gate.

**Steps:**

- [ ] **Step 1: Runtime (subagent-launched server — controller servers get SIGKILLed).** Build `-tags nodynamic`, fresh throwaway DB on the podman Postgres :5432, migrate (creates `entity_icon`), launch detached on a free port, `PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true`. Seed an upstream IdP (`prohibitorum upstream-idp create …` or `dev-seed`). Then:
  - Authenticate an admin enough to `PUT` an icon (or, since the upload is sudo-gated and headless WebAuthn is hard, set a row directly: `INSERT INTO entity_icon(owner_kind,owner_id,png,etag) VALUES('upstream_idp','<slug>', decode(<hex of a small png>,'hex'),'testetag1')` — this exercises the same serve + list path).
  - `curl /api/prohibitorum/auth/federation` → the provider has `iconUrl: "/icon/upstream_idp/<slug>?v=testetag"`.
  - `curl -s -o /tmp/i.png -D - /icon/upstream_idp/<slug>` → 200 `image/png` + `ETag`; re-request with `-H 'If-None-Match: "testetag1"'` → `304`; `curl /icon/bogus/x` → 400; `curl /icon/upstream_idp/nope` → 404.
  - chromium `--dump-dom`/screenshot `/login` → the federation button shows the icon `<img src="/icon/upstream_idp/<slug>...">`.
  - Clean up server + DB.
- [ ] **Step 2: Full gate.** `go vet ./... && go build -tags nodynamic ./... && go test ./...` (re-run a flaky `pkg/server` test in isolation if needed). `cd dashboard && npm run test -- --run && npx vue-tsc -b && node scripts/check-contrast.mjs && npm run build && cd .. && git status --porcelain pkg/webui/dist` (commit if the build produced a diff).
- [ ] **Step 3: Memory.** Append to `…/memory/project_current_state.md`: app & provider icons DONE (cycle 1) — `entity_icon` table + reuse of `branding.ProcessIcon`; admin upload/remove for OIDC/SAML/forward-auth/upstream; public `GET /icon/{kind}/{id}`; `iconUrl` on the federation list + admin views; `AppIcon`/`EntityIconUpload`; icons on `/login` + `/connected`. Note the **launchpad is the next cycle** (decisions recorded in the icons spec's Sequencing section). Convert relative dates to absolute.
- [ ] **Step 4: Commit** any dist/memory-adjacent doc changes (`git commit -m "docs(icons): mark app & provider icons cycle complete"` for any tracked docs; the memory file is outside the repo and written directly).

---

## Self-Review

**Spec coverage:** §1 storage → Task 1. §2 admin upload + `EntityIconUpload` on 4 views → Tasks 2 + 5. §3 public serve → Task 3. §4 contracts/display (`FederationProvider.IconURL`, `AppIcon`, federation buttons) → Tasks 4 + 5 + 6. Security (admin+sudo upload, public read, owner-delete cleanup) → Tasks 2 + 3. Testing/verification → each task + Task 7. ✓ The spec's "admin app views gain iconUrl … so the consent screen can display it" is implemented via the GET-view `iconUrl` (Task 4); the consent-screen display itself is explicitly optional/next-cycle and is not a task here (consistent with the spec's "optionally").

**Type consistency:** `entityIconURL(kind,id,etag) string` / `entityIconURLPtr(...) *string` / `lookupEntityIconEtag(ctx,kind,id) string` are defined in Task 1/4 and used in Tasks 3/4. Query/param names (`SetEntityIconParams`, `GetEntityIconParams`, `GetEntityIconEtagParams`, `ListEntityIconEtagsRow{OwnerID,Etag}`) are introduced in Task 1 and reused verbatim. `owner_kind` literals (`"oidc_client"|"saml_sp"|"upstream_idp"`) are consistent across upload (T2), serve (T3), views (T4), and the FE base paths (T5/T6). `IconURL *string json:"iconUrl,omitempty"` is consistent across all five contract views.

**Placeholder scan:** No TBD/TODO. The few "read the handler first / match the existing fake" notes are concrete instructions to align with real, named code (not vague gaps); the new-file code is complete. Per-entity not-found helpers (`samlSPNotFound`, the upstream not-found error) are named with a "grep to confirm exact name" instruction because they're existing symbols the implementer must match — verified to exist in the sibling handlers.

**Known-unknowns to confirm during implementation (named, not vague):** the exact upstream not-found error symbol (mirror `handleGetIdentityProvider`); `api.upload`/`api.del` signatures (mirror `SettingsView`); the `listFedOverride` fake's type name; sqlc-generated field names (grep after regen). Each has a concrete source to copy from.
