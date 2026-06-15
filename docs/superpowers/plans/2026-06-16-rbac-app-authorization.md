# RBAC — App Authorization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a per-app access gate — admins mark an OIDC client / SAML SP as `access_restricted` and control who may sign in via first-class **groups** and/or **individual accounts**; exposed groups additionally flow to apps as an OIDC `groups` claim / SAML `groups` attribute.

**Architecture:** New tables (`user_group`, `group_member`, `oidc_client_access`, `saml_sp_access`) + an `access_restricted` column on each app table. A single sqlc predicate (`Is…Authorized…`) gates the OIDC authorize, OIDC refresh, and SAML SSO handlers. Denied interactive users land on the IdP's own `/error` page; `prompt=none` / SAML passive denials use the protocol-native channel. Claim/attribute emission threads the account's exposed group slugs into the existing claim builders, gated by a per-group `exposed_to_downstream` flag plus a per-app opt-in (OIDC `groups` scope / SAML attribute-map entry). Admin surface follows the established `registerOp` (typed GET) + `registerSudoOpHTTP` (raw sudo-gated mutation) convention and the shadcn-vue SPA patterns.

**Tech Stack:** Go (chi + humacli + sqlc + goose + pgx), Postgres, Vue 3 + Vite + Tailwind v4 + shadcn-vue/Reka UI, vitest, `cmd/smoke`.

**Spec:** `docs/superpowers/specs/2026-06-16-rbac-design.md`

**Conventions to honor (from memory / repo):**
- No `Co-Authored-By` trailer in any commit.
- Green gate per task where applicable: `go build -tags nodynamic ./... && go vet ./... && go test ./...`; `cd dashboard && npm test` (vitest) + `npm run build` (vue-tsc). The final task rebuilds + commits `pkg/webui/dist`.
- `en.ts`: escape literal `@` as `{'@'}` (prod vue-i18n compiler throws `INVALID_LINKED_FORMAT`); `en.compile.test.ts` guards this. After editing `en.ts`, grep for stray curly apostrophes (U+2018/U+2019) the Edit tool can introduce.
- sqlc: after editing `db/queries/*.sql` or migrations, regenerate with the project's sqlc step (the generated package is `pkg/db`). The repo pins sqlc via `mise`; run `mise exec -- sqlc generate` (config `sqlc.yaml`). IDE diagnostics go stale after generation — trust `go build`/`go test`.
- The `pkg/server` suite flakes ~1/3 under parallel shared-DB runs; re-run in isolation before treating a failure as real.

---

## File Structure

| File | Responsibility | New/Modify |
|------|----------------|-----------|
| `db/migrations/015_rbac.sql` | groups, membership, access grants, `access_restricted` columns | New |
| `db/queries/rbac.sql` | all group / membership / grant / predicate / exposed-slug queries | New |
| `db/queries/oidc.sql`, `db/queries/saml_sp.sql` | surface `access_restricted` in get/list/update rows | Modify |
| `pkg/db/*` (generated) | sqlc models + querier | Regenerate |
| `pkg/audit/event.go` | `FactorGroup`; access/denied event constants | Modify |
| `pkg/protocol/oidc/access.go` | `groupSlugs` fetch helper + `appAccessDenied` redirect helper | New |
| `pkg/protocol/oidc/authorize.go` | access gate after session check | Modify |
| `pkg/protocol/oidc/token.go`, `refresh.go` | re-check at refresh; thread groups into id_token | Modify |
| `pkg/protocol/oidc/claims.go` | `groupsClaims` + `groups`-scope gating | Modify |
| `pkg/protocol/oidc/oidc.go` | advertise `groups` scope in discovery | Modify |
| `pkg/protocol/oidc/userinfo.go` | groups in `/userinfo` | Modify |
| `pkg/protocol/saml/sso.go` | access gate; thread groups into projectAttributes; `statusRequestDenied` | Modify |
| `pkg/protocol/saml/attributes.go` | `"groups"` attribute source | Modify |
| `pkg/contract/*.go` | group/access operations + view structs | Modify |
| `pkg/server/handle_admin_groups.go` | groups CRUD + membership handlers | New |
| `pkg/server/handle_admin_app_access.go` | set-restricted + grant/revoke + GET access (OIDC + SAML) | New |
| `pkg/server/handle_admin_oidc_clients.go`, `handle_admin_saml_sps.go` | add `accessRestricted` to views | Modify |
| `pkg/server/handle_admin_accounts.go` (or `handle_account.go`) | `GET /accounts/{id}/groups` | Modify |
| `pkg/server/server.go` | register new routes | Modify |
| `dashboard/src/pages/admin/AdminGroupsView.vue`, `AdminGroupDetailView.vue` | groups list + detail/members | New |
| `dashboard/src/components/custom/AppAccessCard.vue` | reusable restrict-toggle + grant list | New |
| `dashboard/src/pages/admin/AdminOidcClientDetailView.vue`, `AdminSamlProviderDetailView.vue`, `AdminAccountDetailView.vue` | mount access / membership cards | Modify |
| `dashboard/src/pages/ErrorView.vue` | `app_access_denied` message | Modify |
| `dashboard/src/router/index.ts`, `components/custom/AppSidebar.*` | groups routes + nav | Modify |
| `dashboard/src/locales/en.ts` | i18n strings | Modify |
| `cmd/prohibitorum/*` | `group` CLI verb + access flags | Modify/New |
| `cmd/smoke/*` | RBAC arc | Modify |
| `README.md`, `api.md`, `ARCHITECTURE.md`, `STATUS.md` | docs | Modify |

---

## Phase 1 — Schema, data layer, groups admin

### Task 1: Migration + sqlc queries (schema foundation)

**Goal:** Create the four tables + `access_restricted` columns, all group/membership/grant/predicate/exposed-slug queries, and regenerate sqlc so the rest of the plan has typed accessors.

**Files:**
- Create: `db/migrations/015_rbac.sql`
- Create: `db/queries/rbac.sql`
- Modify: `db/queries/oidc.sql`, `db/queries/saml_sp.sql`
- Regenerate: `pkg/db/*`
- Test: `pkg/db/rbac_query_test.go` (new) — exercises the predicate against the dev DB

**Acceptance Criteria:**
- [ ] `mise db:up` applies `015` cleanly on a populated DB; every existing app remains usable (`access_restricted` defaults false).
- [ ] sqlc generates `IsAccountAuthorizedForOIDCClient`, `IsAccountAuthorizedForSAMLSP`, `ListExposedGroupSlugsByAccount`, group/membership/grant CRUD, and `access_restricted` on the OIDC/SAML get+list+update rows.
- [ ] `go build ./... && go vet ./...` clean.

**Verify:** `mise db:up && mise exec -- sqlc generate && go build ./... && go test ./pkg/db/...`

**Steps:**

- [ ] **Step 1: Write the migration** `db/migrations/015_rbac.sql`

```sql
-- +goose Up
CREATE TABLE IF NOT EXISTS user_group (
  id                    serial PRIMARY KEY,
  slug                  text NOT NULL UNIQUE,
  display_name          text NOT NULL,
  description           text,
  exposed_to_downstream boolean NOT NULL DEFAULT true,
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT user_group_slug_format CHECK (slug ~ '^[a-z0-9](-?[a-z0-9])*$')
);

CREATE TABLE IF NOT EXISTS group_member (
  group_id   integer NOT NULL REFERENCES user_group(id) ON DELETE CASCADE,
  account_id integer NOT NULL REFERENCES account(id)    ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (group_id, account_id)
);
CREATE INDEX IF NOT EXISTS group_member_account_idx ON group_member(account_id);

CREATE TABLE IF NOT EXISTS oidc_client_access (
  client_id  text    NOT NULL REFERENCES oidc_client(client_id) ON DELETE CASCADE,
  group_id   integer          REFERENCES user_group(id)         ON DELETE CASCADE,
  account_id integer          REFERENCES account(id)            ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT oidc_client_access_one_principal CHECK (num_nonnulls(group_id, account_id) = 1)
);
CREATE UNIQUE INDEX IF NOT EXISTS oidc_client_access_group_uq
  ON oidc_client_access(client_id, group_id) WHERE group_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS oidc_client_access_account_uq
  ON oidc_client_access(client_id, account_id) WHERE account_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS saml_sp_access (
  saml_sp_id bigint  NOT NULL REFERENCES saml_sp(id)    ON DELETE CASCADE,
  group_id   integer          REFERENCES user_group(id) ON DELETE CASCADE,
  account_id integer          REFERENCES account(id)    ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT saml_sp_access_one_principal CHECK (num_nonnulls(group_id, account_id) = 1)
);
CREATE UNIQUE INDEX IF NOT EXISTS saml_sp_access_group_uq
  ON saml_sp_access(saml_sp_id, group_id) WHERE group_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS saml_sp_access_account_uq
  ON saml_sp_access(saml_sp_id, account_id) WHERE account_id IS NOT NULL;

ALTER TABLE oidc_client ADD COLUMN IF NOT EXISTS access_restricted boolean NOT NULL DEFAULT false;
ALTER TABLE saml_sp      ADD COLUMN IF NOT EXISTS access_restricted boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE saml_sp      DROP COLUMN IF EXISTS access_restricted;
ALTER TABLE oidc_client  DROP COLUMN IF EXISTS access_restricted;
DROP TABLE IF EXISTS saml_sp_access;
DROP TABLE IF EXISTS oidc_client_access;
DROP TABLE IF EXISTS group_member;
DROP TABLE IF EXISTS user_group;
```

- [ ] **Step 2: Write `db/queries/rbac.sql`**

```sql
-- name: CreateGroup :one
INSERT INTO user_group (slug, display_name, description, exposed_to_downstream)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetGroup :one
SELECT * FROM user_group WHERE id = $1;

-- name: GetGroupBySlug :one
SELECT * FROM user_group WHERE slug = $1;

-- name: ListGroups :many
SELECT g.*, (SELECT count(*) FROM group_member m WHERE m.group_id = g.id) AS member_count
FROM user_group g
ORDER BY g.display_name;

-- name: UpdateGroup :one
UPDATE user_group
SET slug = $2, display_name = $3, description = $4, exposed_to_downstream = $5, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteGroup :execrows
DELETE FROM user_group WHERE id = $1;

-- name: AddGroupMember :exec
INSERT INTO group_member (group_id, account_id)
VALUES ($1, $2)
ON CONFLICT (group_id, account_id) DO NOTHING;

-- name: RemoveGroupMember :execrows
DELETE FROM group_member WHERE group_id = $1 AND account_id = $2;

-- name: ListGroupMembers :many
SELECT a.id, a.username, a.display_name
FROM group_member m
JOIN account a ON a.id = m.account_id
WHERE m.group_id = $1
ORDER BY a.username;

-- name: ListGroupsForAccount :many
SELECT g.*
FROM group_member m
JOIN user_group g ON g.id = m.group_id
WHERE m.account_id = $1
ORDER BY g.display_name;

-- name: ListExposedGroupSlugsByAccount :many
SELECT g.slug
FROM group_member m
JOIN user_group g ON g.id = m.group_id
WHERE m.account_id = $1 AND g.exposed_to_downstream
ORDER BY g.slug;

-- name: GrantOIDCClientAccessGroup :exec
INSERT INTO oidc_client_access (client_id, group_id) VALUES ($1, $2)
ON CONFLICT (client_id, group_id) WHERE group_id IS NOT NULL DO NOTHING;

-- name: GrantOIDCClientAccessAccount :exec
INSERT INTO oidc_client_access (client_id, account_id) VALUES ($1, $2)
ON CONFLICT (client_id, account_id) WHERE account_id IS NOT NULL DO NOTHING;

-- name: RevokeOIDCClientAccessGroup :execrows
DELETE FROM oidc_client_access WHERE client_id = $1 AND group_id = $2;

-- name: RevokeOIDCClientAccessAccount :execrows
DELETE FROM oidc_client_access WHERE client_id = $1 AND account_id = $2;

-- name: ListOIDCClientAccessGroups :many
SELECT g.id, g.slug, g.display_name
FROM oidc_client_access a JOIN user_group g ON g.id = a.group_id
WHERE a.client_id = $1 ORDER BY g.display_name;

-- name: ListOIDCClientAccessAccounts :many
SELECT acc.id, acc.username, acc.display_name
FROM oidc_client_access a JOIN account acc ON acc.id = a.account_id
WHERE a.client_id = $1 ORDER BY acc.username;

-- name: GrantSAMLSPAccessGroup :exec
INSERT INTO saml_sp_access (saml_sp_id, group_id) VALUES ($1, $2)
ON CONFLICT (saml_sp_id, group_id) WHERE group_id IS NOT NULL DO NOTHING;

-- name: GrantSAMLSPAccessAccount :exec
INSERT INTO saml_sp_access (saml_sp_id, account_id) VALUES ($1, $2)
ON CONFLICT (saml_sp_id, account_id) WHERE account_id IS NOT NULL DO NOTHING;

-- name: RevokeSAMLSPAccessGroup :execrows
DELETE FROM saml_sp_access WHERE saml_sp_id = $1 AND group_id = $2;

-- name: RevokeSAMLSPAccessAccount :execrows
DELETE FROM saml_sp_access WHERE saml_sp_id = $1 AND account_id = $2;

-- name: ListSAMLSPAccessGroups :many
SELECT g.id, g.slug, g.display_name
FROM saml_sp_access a JOIN user_group g ON g.id = a.group_id
WHERE a.saml_sp_id = $1 ORDER BY g.display_name;

-- name: ListSAMLSPAccessAccounts :many
SELECT acc.id, acc.username, acc.display_name
FROM saml_sp_access a JOIN account acc ON acc.id = a.account_id
WHERE a.saml_sp_id = $1 ORDER BY acc.username;

-- name: SetOIDCClientAccessRestricted :one
UPDATE oidc_client SET access_restricted = $2 WHERE client_id = $1 RETURNING *;

-- name: SetSAMLSPAccessRestricted :one
UPDATE saml_sp SET access_restricted = $2 WHERE id = $1 RETURNING *;

-- name: IsAccountAuthorizedForOIDCClient :one
SELECT
  NOT c.access_restricted
  OR EXISTS (SELECT 1 FROM oidc_client_access a
             WHERE a.client_id = c.client_id AND a.account_id = sqlc.arg(account_id))
  OR EXISTS (SELECT 1 FROM oidc_client_access a
             JOIN group_member m ON m.group_id = a.group_id
             WHERE a.client_id = c.client_id AND m.account_id = sqlc.arg(account_id))
FROM oidc_client c
WHERE c.client_id = sqlc.arg(client_id);

-- name: IsAccountAuthorizedForSAMLSP :one
SELECT
  NOT s.access_restricted
  OR EXISTS (SELECT 1 FROM saml_sp_access a
             WHERE a.saml_sp_id = s.id AND a.account_id = sqlc.arg(account_id))
  OR EXISTS (SELECT 1 FROM saml_sp_access a
             JOIN group_member m ON m.group_id = a.group_id
             WHERE a.saml_sp_id = s.id AND m.account_id = sqlc.arg(account_id))
FROM saml_sp s
WHERE s.id = sqlc.arg(sp_id);
```

> Note: confirm `ON CONFLICT ... WHERE` partial-index inference compiles with the pinned sqlc/pg; if sqlc rejects the partial-index conflict target, drop the `WHERE` clause from the `ON CONFLICT` (the partial unique index still enforces it) and rely on a plain `ON CONFLICT DO NOTHING` is NOT valid for partial indexes — instead use `ON CONFLICT ON CONSTRAINT` is unavailable for indexes, so fall back to a guarded `INSERT ... WHERE NOT EXISTS (...)`. Prefer the `WHERE`-qualified form; only switch if generation fails.

- [ ] **Step 3: Surface `access_restricted` in existing app queries** — `db/queries/oidc.sql`: ensure `GetOIDCClient`, `GetOIDCClientAny`, and `UpdateOIDCClient` return `*` (they already `RETURNING *` / `SELECT *` per repo style — verify the new column flows through; if any uses an explicit column list, add `access_restricted`). Same for `db/queries/saml_sp.sql` `GetSAMLSPByEntityID`, `GetSAMLSP`, `UpdateSAMLSP`. Do **not** add it to the `UpdateOIDCClient`/`UpdateSAMLSP` SET lists — it is mutated only via `SetOIDCClientAccessRestricted` / `SetSAMLSPAccessRestricted` (independent of the config-form Save), mirroring the existing `disabled` / `set-disabled` split.

- [ ] **Step 4: Regenerate sqlc**

Run: `mise exec -- sqlc generate`
Expected: `pkg/db/models.go` gains `UserGroup`, `GroupMember`; `db.OidcClient`/`db.SamlSp` gain `AccessRestricted bool`; `pkg/db/rbac.sql.go` (or querier.go) gains the new methods.

- [ ] **Step 5: Predicate test** `pkg/db/rbac_query_test.go` — using the existing dev-DB test harness in `pkg/db` (mirror any existing `*_query_test.go`; if none, write a `//go:build integration`-style test or a `t.Skip` when `PROHIBITORUM_DATABASE_URL` is unset). Cover: open client → authorized; restricted + no grant → false; restricted + direct account grant → true; restricted + group grant where account is a member → true; restricted + member of a different group → false. If the `pkg/db` package has no DB-backed test convention, defer this matrix to the unit tests in Task 7 (which run the predicate via the protocol handlers) and the smoke in Task 11 — note the deferral in the commit message rather than leaving an empty test.

- [ ] **Step 6: Commit**

```bash
git add db/migrations/015_rbac.sql db/queries/rbac.sql db/queries/oidc.sql db/queries/saml_sp.sql pkg/db
git commit -m "feat(rbac): schema + queries for groups, app access grants, and the authorization predicate"
```

---

### Task 2: Groups admin API (CRUD + membership)

**Goal:** Admin HTTP surface for groups: list/create/get/update/delete + member add/remove/list, following the `registerOp` (typed GET) + `registerSudoOpHTTP` (raw sudo mutation) convention, with audit.

**Files:**
- Create: `pkg/server/handle_admin_groups.go`
- Modify: `pkg/contract/auth.go` (group operations + view structs — co-locate with the other admin operations)
- Modify: `pkg/server/server.go` (register routes)
- Test: `pkg/server/handle_admin_groups_test.go`

**Acceptance Criteria:**
- [ ] `GET /api/prohibitorum/groups` (admin) lists groups with member counts.
- [ ] `POST /groups`, `PUT /groups/{id}`, `POST /groups/delete`, `POST /groups/{id}/members`, `POST /groups/{id}/members/remove` are admin+sudo; `GET /groups/{id}` and `GET /groups/{id}/members` are admin-only.
- [ ] Invalid slug (fails `^[a-z0-9](-?[a-z0-9])*$`) → 400; duplicate slug → a clear conflict error.
- [ ] Group CRUD + membership changes emit audit events (`FactorGroup`).

**Verify:** `go test ./pkg/server/ -run Group -v`

**Steps:**

- [ ] **Step 1: Contract view structs** (in `pkg/contract/auth.go`, near `OIDCApplicationView`):

```go
type GroupView struct {
	ID                  int32     `json:"id"`
	Slug                string    `json:"slug"`
	DisplayName         string    `json:"displayName"`
	Description         string    `json:"description,omitempty"`
	ExposedToDownstream bool      `json:"exposedToDownstream"`
	MemberCount         int64     `json:"memberCount,omitempty"`
	CreatedAt           time.Time `json:"createdAt"`
}

type GroupMemberView struct {
	ID          int32  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

// PrincipalRef is a member account / group reference reused by the access views.
type GroupRef struct {
	ID          int32  `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
}
type AccountRef struct {
	ID          int32  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}
```

Add typed list/get huma operations `OperationListGroups`, `OperationGetGroup`, `OperationListGroupMembers`, `OperationListAccountGroups` mirroring `OperationListOIDCApplications` (same `huma.Operation` shape: method, path, tags, security). Co-locate with the other `Operation…` definitions.

- [ ] **Step 2: Handlers** `pkg/server/handle_admin_groups.go` — mirror `handle_admin_oidc_clients.go` exactly for structure (typed GET handlers returning `*…Out{Body: …}`; raw `…HTTP` handlers decoding JSON, doing the mutation, recording audit, writing JSON). Key pieces:

```go
func groupView(g db.UserGroup, memberCount int64) contract.GroupView {
	v := contract.GroupView{
		ID: g.ID, Slug: g.Slug, DisplayName: g.DisplayName,
		ExposedToDownstream: g.ExposedToDownstream, MemberCount: memberCount,
	}
	if g.Description.Valid { v.Description = g.Description.String }
	if g.CreatedAt.Valid { v.CreatedAt = g.CreatedAt.Time }
	return v
}

var slugRe = regexp.MustCompile(`^[a-z0-9](-?[a-z0-9])*$`)

func validateSlug(s string) error {
	if s == "" || len(s) > 64 || !slugRe.MatchString(s) {
		return authn.ErrBadRequest()
	}
	return nil
}
```

- Create: decode `{slug, displayName, description, exposedToDownstream}` (default `exposedToDownstream` to `true` when the JSON key is absent — decode into a `*bool` and default nil→true), `validateSlug`, `CreateGroup`; on unique violation return a conflict (`authn.ErrBadRequest()` or a dedicated `ErrGroupExists` if the helper set has room — reuse `isUniqueViolation` as in oidc clients). Audit `FactorGroup`/`EventRegister`.
- Update: `validateSlug`, `UpdateGroup`; audit `FactorGroup`/`EventUpdate`.
- Delete: decode `{id}`, `DeleteGroup` (execrows); 0 rows → not-found; audit `FactorGroup`/`EventRevoke`. (`ON DELETE CASCADE` cleans memberships + grants.)
- Members add: decode `{accountId}` from body + `{id}` from path, `AddGroupMember`; audit `FactorGroup`/`EventLink` with `{group_id, account_id}`.
- Members remove: `RemoveGroupMember`; audit `FactorGroup`/`EventUnlink`.
- Add the audit `FactorGroup` constant in Task 7's audit edit OR here — do it **here** (move the audit-const edit earlier): add to `pkg/audit/event.go`:

```go
FactorGroup Factor = "group"
```

- [ ] **Step 3: Register routes** in `server.go` (in the admin block):

```go
// Admin: group (RBAC) management
registerOp(mgmt, contract.OperationListGroups, s.handleListGroups, admin)
registerOp(mgmt, contract.OperationGetGroup, s.handleGetGroup, admin)
registerOp(mgmt, contract.OperationListGroupMembers, s.handleListGroupMembers, admin)
s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/groups", admin, s.handleCreateGroupHTTP)
s.registerSudoOpHTTP(s.router, "PUT", "/api/prohibitorum/groups/{id}", admin, s.handleUpdateGroupHTTP)
s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/groups/delete", admin, s.handleDeleteGroupHTTP)
s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/groups/{id}/members", admin, s.handleAddGroupMemberHTTP)
s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/groups/{id}/members/remove", admin, s.handleRemoveGroupMemberHTTP)
```

- [ ] **Step 4: Tests** `handle_admin_groups_test.go` — mirror `handle_admin_oidc_clients_test.go` and `admin_route_policy_test.go`. Assert: list/create/update/delete happy paths; bad slug → 400; mutation routes require admin + sudo (add the new mutation paths to whatever `TestAdminMutationRoutesRequireSudo` allowlist exists — see `handle_admin_set_disabled_test.go` / `admin_route_policy_test.go`).

- [ ] **Step 5: Verify + commit**

```bash
go build ./... && go test ./pkg/server/ -run "Group|Sudo|RoutePolicy" -v
git add pkg/server/handle_admin_groups.go pkg/contract pkg/server/server.go pkg/audit/event.go pkg/server/handle_admin_groups_test.go
git commit -m "feat(rbac): admin API for group CRUD and membership"
```

---

### Task 3: Groups admin SPA (list + detail/members)

**Goal:** `/admin/groups` list and `/admin/groups/:id` detail (edit fields + member management), wired into the router and sidebar, with i18n.

**Files:**
- Create: `dashboard/src/pages/admin/AdminGroupsView.vue`, `dashboard/src/pages/admin/AdminGroupDetailView.vue`
- Modify: `dashboard/src/router/index.ts`, `dashboard/src/components/custom/AppSidebar.*` (nav config + its test), `dashboard/src/locales/en.ts`
- Test: `dashboard/src/pages/admin/AdminGroupsView.test.ts` (mirror an existing admin list test)

**Acceptance Criteria:**
- [ ] `/admin/groups` lists groups (TableSkeleton while loading, EmptyState when none), with a create dialog (slug, display name, description, exposed switch).
- [ ] `/admin/groups/:id` edits the group, toggles `exposedToDownstream`, and adds/removes members (account search/add + remove with ConfirmDialog).
- [ ] Sidebar shows a "Groups" admin link for admins only; route has `meta: { requiresAdmin: true }`.
- [ ] `npm test` and `npm run build` (vue-tsc) pass.

**Verify:** `cd dashboard && npm test -- AdminGroups && npm run build`

**Steps:**

- [ ] **Step 1: List view** `AdminGroupsView.vue` — mirror `AdminOidcClientsView.vue`. Uses `useApi()` (`busy`, `errorText`, `run`) + `api.get('/groups')`. Columns: display name (with slug as secondary "User · Username"-style stacked cell), member count, exposed badge. Row → `router.push({ name: 'admin-group-detail', params: { id } })`. Create dialog posts `/groups` then refetches; use `useTransientFlag` for the "Created" hint. Slug field validates `^[a-z0-9](-?[a-z0-9])*$` client-side with an inline error.

- [ ] **Step 2: Detail view** `AdminGroupDetailView.vue` — mirror `AdminOidcClientDetailView.vue`. Sections:
  - `BackLink` to `/admin/groups`.
  - Edit card: display name, description, slug (editable with a warning hint that changing it changes the `groups` claim downstream), `exposedToDownstream` Switch. Save → `PUT /groups/{id}` (sudo via `lib/sudo` `withSudo`, mirroring how other admin detail views wrap mutations).
  - Members card: list from `GET /groups/{id}/members`; an account picker (reuse the account-search pattern from wherever accounts are picked, or a simple `<input>` + `GET /accounts?...` filter — mirror `AdminAccountsView`'s list fetch) that posts `/groups/{id}/members {accountId}`; remove via ConfirmDialog → `/groups/{id}/members/remove`.
  - 404 handling: mirror the "404 double-render fix" pattern already in the other detail views.

- [ ] **Step 3: Router + nav** — add to `dashboard/src/router/index.ts` children (after the audit route):

```ts
{ path: 'admin/groups', name: 'admin-groups', component: () => import('../pages/admin/AdminGroupsView.vue'), meta: { requiresAdmin: true } },
{ path: 'admin/groups/:id', name: 'admin-group-detail', component: () => import('../pages/admin/AdminGroupDetailView.vue'), meta: { requiresAdmin: true } },
```

Add a "Groups" entry to the admin nav group in `AppSidebar` (find the admin items array; add `{ to: '/admin/groups', labelKey: 'nav.groups', icon: <Users-style icon already imported or lucide `Users`> }` matching the existing item shape). Update `AppSidebar.test.ts` to include `/admin/groups` in its routes stub + admin-visible assertion.

- [ ] **Step 4: i18n** — add an `admin.groups.*` block to `en.ts` (title, create, slug, displayName, description, exposed, exposedHint, members, addMember, removeMember, slugInvalid, slugChangeWarning, empty) and `nav.groups`. Escape any literal `@`; after editing run the apostrophe grep.

- [ ] **Step 5: Verify + commit**

```bash
cd dashboard && npm test && npm run build
git add dashboard/src/pages/admin/AdminGroupsView.vue dashboard/src/pages/admin/AdminGroupDetailView.vue dashboard/src/router/index.ts dashboard/src/components/custom/AppSidebar.* dashboard/src/locales/en.ts dashboard/src/pages/admin/AdminGroupsView.test.ts
git commit -m "feat(rbac): groups admin SPA — list, detail, membership"
```

---

### Task 4: Account-detail group membership

**Goal:** Show + edit an account's group memberships from the admin account detail page (membership writes reuse Task 2's member endpoints; needs a read endpoint for an account's groups).

**Files:**
- Modify: `pkg/server/handle_admin_accounts.go` (or wherever `handleGetAccount`/`ListAccountSessions` live) — add `handleListAccountGroups` typed GET
- Modify: `pkg/server/server.go` (register `GET /accounts/{id}/groups`)
- Modify: `dashboard/src/pages/admin/AdminAccountDetailView.vue` (membership card)
- Modify: `dashboard/src/locales/en.ts`
- Test: extend `pkg/server` account tests + an FE test if the detail view has one

**Acceptance Criteria:**
- [ ] `GET /api/prohibitorum/accounts/{id}/groups` (admin) returns the account's groups.
- [ ] Account detail page shows a Groups card; admin can add the account to a group (group picker) and remove it (ConfirmDialog), via the existing `/groups/{id}/members` endpoints.
- [ ] Gate passes.

**Verify:** `go test ./pkg/server/ -run Account -v && cd dashboard && npm run build`

**Steps:**

- [ ] **Step 1: Read endpoint** — add `OperationListAccountGroups` (path `/accounts/{id}/groups`) + `handleListAccountGroups` (typed GET, admin) using `ListGroupsForAccount`. Returns `[]contract.GroupView` (member count optional/omit). Register with `registerOp(mgmt, contract.OperationListAccountGroups, s.handleListAccountGroups, admin)`.

- [ ] **Step 2: FE membership card** in `AdminAccountDetailView.vue` — a "Groups" card listing `GET /accounts/{id}/groups`; an add control (group picker from `GET /groups`) → `POST /groups/{groupId}/members {accountId: <this account>}`; remove → `POST /groups/{groupId}/members/remove`. Reuse the member-management UI pieces from Task 3 (extract a small `MemberPicker`/list if it reduces duplication — DRY).

- [ ] **Step 3: i18n + verify + commit**

```bash
go build ./... && go test ./pkg/server/ -run Account
cd dashboard && npm run build
git add pkg/server pkg/contract dashboard/src/pages/admin/AdminAccountDetailView.vue dashboard/src/locales/en.ts
git commit -m "feat(rbac): manage an account's group membership from account detail"
```

---

## Phase 2 — App access (restrict + grants) + enforcement

### Task 5: App access admin API

**Goal:** Admin endpoints to toggle `access_restricted` and grant/revoke group/account access for both OIDC clients and SAML SPs, plus a combined GET; surface `accessRestricted` in the app views.

**Files:**
- Create: `pkg/server/handle_admin_app_access.go`
- Modify: `pkg/server/handle_admin_oidc_clients.go`, `handle_admin_saml_sps.go` (add `AccessRestricted` to views)
- Modify: `pkg/contract/auth.go` (`AppAccessView` + operations), `pkg/server/server.go`
- Test: `pkg/server/handle_admin_app_access_test.go`

**Acceptance Criteria:**
- [ ] `GET /oidc-applications/{clientId}/access` and `GET /saml-applications/{id}/access` return `{accessRestricted, groups:[GroupRef], accounts:[AccountRef]}` (admin).
- [ ] `POST …/access/set-restricted`, `…/access/grant`, `…/access/revoke` are admin+sudo and audit-logged.
- [ ] `accessRestricted` appears in `OIDCApplicationView` / `SAMLApplicationView`.

**Verify:** `go test ./pkg/server/ -run "AppAccess|RoutePolicy" -v`

**Steps:**

- [ ] **Step 1: Views** in `contract`:

```go
type AppAccessView struct {
	AccessRestricted bool         `json:"accessRestricted"`
	Groups           []GroupRef   `json:"groups"`
	Accounts         []AccountRef `json:"accounts"`
}
```
Add `AccessRestricted bool` json `accessRestricted` to `OIDCApplicationView` and `SAMLApplicationView`; set it in `oidcApplicationView()` / the SAML view builder from the row's new column. (List views may omit it; detail GET must include it.)

- [ ] **Step 2: Handlers** `handle_admin_app_access.go` — for each protocol, a GET (typed or raw) assembling `AppAccessView` from `Set…`/`List…AccessGroups`/`List…AccessAccounts`, and raw sudo handlers:
  - `set-restricted`: decode `{clientId|id, restricted}`, call `SetOIDCClientAccessRestricted`/`SetSAMLSPAccessRestricted`; audit `FactorOIDCClient`/`FactorSAMLSP` + a new `EventAccessRestrictedSet` (add to `pkg/audit/event.go`) with `{restricted}`.
  - `grant`: decode `{clientId|id, principalKind: "group"|"account", principalId}`; dispatch to `Grant…AccessGroup`/`…Account`; audit new `EventAccessGranted`.
  - `revoke`: same shape → `Revoke…AccessGroup`/`…Account` (execrows; 0 → not-found); audit new `EventAccessRevoked`.
  - Add audit consts:

```go
EventAccessGranted       = "access_granted"
EventAccessRevoked       = "access_revoked"
EventAccessRestrictedSet = "access_restricted_set"
EventAccessDenied        = "access_denied"
```

- [ ] **Step 3: Register** the 8 routes (4 per protocol: GET access + 3 mutations) in `server.go` near each app's block. Mutations via `registerSudoOpHTTP`; GETs via `registerOpHTTP`/`registerOp`.

- [ ] **Step 4: Tests** — happy paths for grant→list→revoke (group + account) and set-restricted, for both protocols; add the new mutation paths to the require-sudo allowlist test.

- [ ] **Step 5: Verify + commit**

```bash
go build ./... && go test ./pkg/server/ -run "AppAccess|Sudo|RoutePolicy"
git add pkg/server pkg/contract pkg/audit/event.go
git commit -m "feat(rbac): admin API for per-app access restriction and grants"
```

---

### Task 6: App access SPA (Access card)

**Goal:** A reusable `AppAccessCard` mounted on both app detail pages: restrict toggle + assigned groups/accounts with add/remove.

**Files:**
- Create: `dashboard/src/components/custom/AppAccessCard.vue`
- Modify: `dashboard/src/pages/admin/AdminOidcClientDetailView.vue`, `AdminSamlProviderDetailView.vue`, `dashboard/src/locales/en.ts`
- Test: `dashboard/src/components/custom/AppAccessCard.test.ts`

**Acceptance Criteria:**
- [ ] Card shows a `accessRestricted` Switch (its own card, like the Status/disabled card) → `…/access/set-restricted`.
- [ ] When restricted, shows assigned groups + accounts with add (pickers) and remove (ConfirmDialog) → `…/access/grant` / `…/access/revoke`; when open, shows an "inactive — assignments are informational" hint.
- [ ] Works for both protocols via a `kind: 'oidc' | 'saml'` + `appId` prop (builds the right base path).
- [ ] `npm test` + `npm run build` pass.

**Verify:** `cd dashboard && npm test -- AppAccessCard && npm run build`

**Steps:**

- [ ] **Step 1: Component** `AppAccessCard.vue` — props `{ kind: 'oidc' | 'saml', appId: string }`. Computes base: `oidc` → `/oidc-applications/${appId}`, `saml` → `/saml-applications/${appId}`. On mount `GET ${base}/access`. Switch bound to `accessRestricted` → `POST ${base}/access/set-restricted {restricted}` (wrap in `withSudo`). Group picker (`GET /groups`) + account picker (`GET /accounts`) → `POST ${base}/access/grant {principalKind, principalId}`; remove → `…/access/revoke`. Mirror the Status-card + member-list idioms from Task 3. Use `useApi`, `useTransientFlag`, `ConfirmDialog`.

- [ ] **Step 2: Mount** on both detail views below the existing Status/Danger cards: `<AppAccessCard kind="oidc" :app-id="clientId" />` and `<AppAccessCard kind="saml" :app-id="id" />`.

- [ ] **Step 3: i18n** `admin.access.*` (title, restrictedLabel, restrictedHint, inactiveHint, groups, accounts, addGroup, addAccount, remove, empty).

- [ ] **Step 4: Verify + commit**

```bash
cd dashboard && npm test && npm run build
git add dashboard/src/components/custom/AppAccessCard.vue dashboard/src/components/custom/AppAccessCard.test.ts dashboard/src/pages/admin/AdminOidcClientDetailView.vue dashboard/src/pages/admin/AdminSamlProviderDetailView.vue dashboard/src/locales/en.ts
git commit -m "feat(rbac): Access card for per-app restriction and assignment"
```

---

### Task 7: Enforcement (OIDC authorize + refresh, SAML SSO) + denied UX

**Goal:** Gate token/assertion issuance on the authorization predicate; deny interactive users via the IdP `/error` page, `prompt=none` via `access_denied`, SAML passive via `RequestDenied`; audit denials.

**Files:**
- Create: `pkg/protocol/oidc/access.go`
- Modify: `pkg/protocol/oidc/authorize.go`, `token.go`/`refresh.go`
- Modify: `pkg/protocol/saml/sso.go` (gate + `statusRequestDenied`)
- Modify: `dashboard/src/pages/ErrorView.vue`, `dashboard/src/locales/en.ts`
- Test: `pkg/protocol/oidc/authorize_test.go`, `refresh_test.go`, `pkg/protocol/saml/sso_test.go`

**Acceptance Criteria:**
- [ ] Restricted OIDC client + non-member authenticated user → interactive: 302 to `/error?reason=app_access_denied&app=<displayName>`; `prompt=none`: `redirectError(... access_denied ...)`. Member/open → issues code as before.
- [ ] OIDC refresh grant re-checks; a de-provisioned user's refresh → `invalid_grant` (and the refresh family is revoked).
- [ ] Restricted SAML SP + non-member → interactive: 302 to `/error`; passive (`IsPassive`): terminal `Responder`/`RequestDenied` auto-POST. Member/open → issues assertion.
- [ ] Each denial records `FactorOIDCClient`/`FactorSAMLSP` + `EventAccessDenied`.
- [ ] `ErrorView` renders a clear "no access to <app>" message for `reason=app_access_denied`.

**Verify:** `go test ./pkg/protocol/... -run "Access|Authoriz|Refresh|SSO" -v`

**Steps:**

- [ ] **Step 1: OIDC access helper** `pkg/protocol/oidc/access.go`:

```go
package oidc

import (
	"net/http"
	"net/url"
)

// appAccessDenied sends an authenticated-but-unauthorized user to the IdP's own
// /error page (interactive), or returns the protocol-native access_denied to the
// RP when the RP forbade interactive UI (prompt=none). It is the access-control
// analogue of the login bounce in HandleAuthorize.
func (p *Provider) appAccessDenied(w http.ResponseWriter, r *http.Request, redirectURI, appName, state string, promptNone bool) {
	if promptNone {
		redirectError(w, r, redirectURI, errCodeAccessDenied, "not authorized for this application", state, p.cfg.OIDC.Issuer)
		return
	}
	u := p.cfg.OIDC.Issuer + "/error?reason=app_access_denied&app=" + url.QueryEscape(appName)
	http.Redirect(w, r, u, http.StatusFound)
}
```

- [ ] **Step 2: Gate in `HandleAuthorize`** — insert immediately AFTER the session gate (after `authorize.go:126`, before the prompt parsing at line 132):

```go
	// (4b) Per-app access gate (RBAC). The user is authenticated and enabled;
	// a restricted client requires a direct or via-group grant. No admin bypass.
	ok, aerr := p.queries.IsAccountAuthorizedForOIDCClient(r.Context(), db.IsAccountAuthorizedForOIDCClientParams{
		AccountID: sess.Data.AccountID,
		ClientID:  client.ClientID,
	})
	if aerr != nil {
		redirectError(w, r, redirectURI, errCodeServerError, "could not evaluate access", state, p.cfg.OIDC.Issuer)
		return
	}
	if !ok {
		acctID := sess.Data.AccountID
		_ = p.audit.Record(r.Context(), audit.Record{
			AccountID: &acctID, Factor: audit.FactorOIDCClient, Event: audit.EventAccessDenied,
			IP: audit.ParseIPOrNil(r.RemoteAddr), UserAgent: r.UserAgent(),
			Detail: map[string]any{"reason": "app_access_denied", "client_id": client.ClientID},
		})
		p.appAccessDenied(w, r, redirectURI, client.DisplayName, state, prompt == "none")
		return
	}
```

(Verify the generated param struct name `IsAccountAuthorizedForOIDCClientParams` and field names against sqlc output; adjust if sqlc named them differently.)

- [ ] **Step 3: Re-check at refresh** — in `grantRefreshToken` (read `pkg/protocol/oidc/refresh.go`), after the account is loaded and confirmed enabled and before minting, add:

```go
	ok, aerr := p.queries.IsAccountAuthorizedForOIDCClient(ctx, db.IsAccountAuthorizedForOIDCClientParams{
		AccountID: acct.ID, ClientID: client.ClientID,
	})
	if aerr != nil {
		writeOIDCError(w, http.StatusInternalServerError, errCodeServerError, "could not evaluate access")
		return
	}
	if !ok {
		_ = revokeFamily(ctx, p.kv, <familyID>) // revoke the rotating family so the cut is durable
		acctID := acct.ID
		p.auditTokenEvent(ctx, r, audit.EventAccessDenied, &acctID, map[string]any{"reason": "app_access_denied", "client_id": client.ClientID})
		writeOIDCError(w, http.StatusBadRequest, errCodeInvalidGrant, "not authorized for this application")
		return
	}
```

Wire `<familyID>` to whatever the refresh path already has in scope (the consumed family id). If the family id isn't readily available at that point, place the check right after the family is loaded/rotated so it is. Mirror the existing `acct.Disabled` handling in the same function for placement.

- [ ] **Step 4: groups claim plumbing is Task 8** — leave claim emission to Task 8; this task is the gate only.

- [ ] **Step 5: SAML gate** — in `HandleSSO`, insert AFTER the session gate (after `sso.go:139`, before the rate-limit at line 143). First add the status constant near the others (`sso.go:34`):

```go
	// statusRequestDenied is the second-level status for an authenticated user
	// who is not authorized for this SP (RBAC). Paired under statusResponder.
	statusRequestDenied = "urn:oasis:names:tc:SAML:2.0:status:RequestDenied"
```

Gate:

```go
	// (5b) Per-app access gate (RBAC). User is authenticated + enabled; a
	// restricted SP requires a direct or via-group grant. No admin bypass.
	authorized, aerr := i.queries.IsAccountAuthorizedForSAMLSP(ctx, db.IsAccountAuthorizedForSAMLSPParams{
		AccountID: sess.Data.AccountID, SpID: sp.ID,
	})
	if aerr != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !authorized {
		acctID := sess.Data.AccountID
		_ = i.audit.Record(ctx, audit.Record{
			AccountID: &acctID, Factor: audit.FactorSAMLSP, Event: audit.EventAccessDenied,
			IP: audit.ParseIPOrNil(r.RemoteAddr), UserAgent: r.UserAgent(),
			Detail: map[string]any{"reason": "app_access_denied", "sp": sp.EntityID},
		})
		if req.IsPassive {
			// Passive: cannot show interactive UI → terminal RequestDenied to the ACS.
			if cerr := i.consumeAuthnRequestID(ctx, sp.EntityID, req.RequestID); cerr != nil {
				if errors.Is(cerr, ErrReplayedRequest) {
					http.Error(w, "AuthnRequest replayed", http.StatusBadRequest)
				} else {
					http.Error(w, "internal error", http.StatusInternalServerError)
				}
				return
			}
			respXML, berr := i.buildStatusResponse(ctx, req.ACSURL, req.RequestID, statusResponder, statusRequestDenied)
			if berr != nil {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			i.writeAutoPost(w, req.ACSURL, respXML, req.RelayState)
			return
		}
		// Interactive: the IdP's own error page (the AuthnRequest is abandoned;
		// not consuming its ID is harmless — the user can't productively replay).
		u := i.baseURL() + "/error?reason=app_access_denied&app=" + url.QueryEscape(sp.DisplayName)
		http.Redirect(w, r, u, http.StatusFound)
		return
	}
```

Apply the same gate inside `HandleIdPInitiated` (read it; it builds an unsolicited Response — gate after the session + SP are known, before building the assertion; IdP-initiated is interactive, so use the `/error` redirect path; there's no `req.IsPassive` there).

- [ ] **Step 6: ErrorView** — `dashboard/src/pages/ErrorView.vue` reads `route.query.reason` / `route.query.error`. Add a branch: when `reason === 'app_access_denied'`, show a heading + body using `route.query.app` (e.g. `t('error.appAccessDenied', { app })`). Keep the existing `forbidden` handling. Add `error.appAccessDenied` to `en.ts` (escape any `@`).

- [ ] **Step 7: Tests** — OIDC `authorize_test.go`: restricted+non-member→302 `/error`; restricted+member(direct and via-group)→code; open→code; `prompt=none`+denied→`access_denied` redirect. `refresh_test.go`: de-provisioned→`invalid_grant`. SAML `sso_test.go`: restricted+non-member interactive→302 `/error`; passive→`RequestDenied`; member→assertion. Mirror the existing handler-test harness (these suites already construct a `Provider`/`IdP` with a `queries` fake or a real DB — follow the file's existing setup; the `pkg/db` test DB is available via `PROHIBITORUM_DATABASE_URL`).

- [ ] **Step 8: Verify + commit**

```bash
go build ./... && go test ./pkg/protocol/...
cd dashboard && npm run build
git add pkg/protocol dashboard/src/pages/ErrorView.vue dashboard/src/locales/en.ts
git commit -m "feat(rbac): enforce per-app access at OIDC authorize/refresh and SAML SSO"
```

---

## Phase 3 — Group exposure to downstreams

### Task 8: OIDC `groups` scope + claim

**Goal:** Add a `groups` scope; emit a `groups` claim (sorted exposed slugs) in the ID token and `/userinfo`, gated by scope + per-group `exposed_to_downstream`.

**Files:**
- Modify: `pkg/protocol/oidc/claims.go`, `oidc.go` (discovery), `token.go` (`mintAccessAndIDTokens`), `userinfo.go`
- Modify: `pkg/server/handle_admin_oidc_clients.go` (`supportedOIDCScopes`)
- Test: `pkg/protocol/oidc/claims_test.go`

**Acceptance Criteria:**
- [ ] `groups` ∈ discovery `scopes_supported` and `supportedOIDCScopes`; a client may list it in `allowed_scopes`.
- [ ] With `groups` granted, id_token + userinfo include `"groups": [<sorted exposed slugs>]` (present-but-empty `[]` when the user has none); without the scope, no `groups` claim.
- [ ] Non-exposed groups never appear.

**Verify:** `go test ./pkg/protocol/oidc/ -run "Claims|Groups|Userinfo" -v`

**Steps:**

- [ ] **Step 1: claims.go** — add `Groups []string` to `idTokenInput`; add a `groups` block to `idTokenClaims` and `userinfoClaims`:

```go
// in idTokenClaims, after the email block:
	if hasScope(in.Scope, "groups") {
		c["groups"] = nonNilSlugs(in.Groups)
	}
```
```go
// userinfoClaims gains a groups param:
func userinfoClaims(a db.Account, scope []string, origin string, groups []string) map[string]any {
	// ...
	if hasScope(scope, "groups") {
		c["groups"] = nonNilSlugs(groups)
	}
	return c
}

// nonNilSlugs returns a non-nil empty slice so the claim serializes as [] not null.
func nonNilSlugs(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
```

- [ ] **Step 2: token.go** — in `mintAccessAndIDTokens`, fetch exposed slugs when the scope includes `groups`, then pass into `idTokenInput.Groups`:

```go
	var groups []string
	if hasScope(scope, "groups") {
		gs, gerr := p.queries.ListExposedGroupSlugsByAccount(ctx, acct.ID)
		if gerr != nil {
			return "", "", gerr
		}
		groups = gs
	}
	// ... idTokenInput{ ..., Groups: groups, ... }
```

This covers BOTH the authorization_code and refresh grants (they share `mintAccessAndIDTokens`), so no change to the two call sites is needed.

- [ ] **Step 3: userinfo.go** — read it; where it builds `userinfoClaims(acct, scope, origin)`, fetch exposed slugs (same gated query) and pass as the new `groups` arg.

- [ ] **Step 4: discovery + supported set** — `oidc.go`: add `"groups"` to the `scopes_supported` array in the discovery document. `handle_admin_oidc_clients.go`: add `"groups": true` to `supportedOIDCScopes`.

- [ ] **Step 5: Tests** — `claims_test.go`: groups present when scope granted + exposed members exist; sorted; `[]` when none; absent when scope not granted. (If `idTokenClaims` tests construct `idTokenInput` directly, set `Groups` there; the exposed-only filtering is the query's job and is covered by the smoke + a `pkg/db`/handler test.)

- [ ] **Step 6: Verify + commit**

```bash
go build ./... && go test ./pkg/protocol/oidc/
git add pkg/protocol/oidc pkg/server/handle_admin_oidc_clients.go
git commit -m "feat(rbac): OIDC groups scope and claim (exposed groups only)"
```

---

### Task 9: SAML `groups` attribute source

**Goal:** A `"groups"` attribute-map source that emits the user's exposed group slugs; expose it in the attribute-map editor.

**Files:**
- Modify: `pkg/protocol/saml/attributes.go` (new `"groups"` source), `sso.go` (fetch + pass slugs), `idp_initiated.go` if it also projects attributes
- Modify: `dashboard/src/components/custom/AttributeMapEditor.vue` (offer `groups` as a source option) + `en.ts`
- Test: `pkg/protocol/saml/attributes_test.go`

**Acceptance Criteria:**
- [ ] An SP whose `attribute_map` has an entry with `source: "groups"` receives the user's exposed slugs (multi); non-exposed groups excluded; no entry → no `groups` attribute.
- [ ] The attribute-map editor lists `groups` as a selectable source.

**Verify:** `go test ./pkg/protocol/saml/ -run "Attributes|Groups" -v && cd dashboard && npm test -- AttributeMap`

**Steps:**

- [ ] **Step 1: attributes.go** — change `projectAttributes` signature to accept the slugs:

```go
func projectAttributes(a db.Account, mapJSON []byte, origin string, groups []string) ([]samlAttr, error) {
```
Handle the source inside the loop (before the generic `resolveSource`), alongside the `administrator` special case:

```go
		if e.Source == "groups" {
			vals := groups
			if !e.Multi && len(vals) > 0 {
				vals = vals[:1]
			}
			if len(vals) > 0 {
				out = append(out, samlAttr{Name: e.Name, NameFormat: e.NameFormat, FriendlyName: e.FriendlyName, Values: vals})
			}
			continue
		}
```

- [ ] **Step 2: sso.go** — before the `projectAttributes` call (`sso.go:278`), fetch exposed slugs and pass them:

```go
	groupSlugs, err := i.queries.ListExposedGroupSlugsByAccount(ctx, account.ID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	attrs, err := projectAttributes(account, sp.AttributeMap, i.baseURL(), groupSlugs)
```
Update the `HandleIdPInitiated` call site identically (read `idp_initiated.go`). Update any other `projectAttributes` caller/test.

- [ ] **Step 3: AttributeMapEditor.vue** — add `groups` to the source suggestions/options (it currently knows `username`, `attributes.*`, `avatar_url`, `attributes.administrator`). Add an `admin.saml.attrSourceGroups` label/hint to `en.ts`.

- [ ] **Step 4: Tests** — `attributes_test.go`: with a `groups` entry + slugs → emitted (multi); empty slugs → omitted; non-`groups` entries unchanged. Update existing `projectAttributes` test calls to the new signature (pass `nil` where groups aren't under test).

- [ ] **Step 5: Verify + commit**

```bash
go build ./... && go test ./pkg/protocol/saml/
cd dashboard && npm test && npm run build
git add pkg/protocol/saml dashboard/src/components/custom/AttributeMapEditor.vue dashboard/src/locales/en.ts
git commit -m "feat(rbac): SAML groups attribute source (exposed groups only)"
```

---

## Phase 4 — CLI, smoke, docs, ship

### Task 10: CLI parity

**Goal:** `group` CLI verb (create/list/update/delete/add-member/remove-member) + `--access-restricted` and grant/revoke flags on `oidc-client`/`saml-sp`.

**Files:**
- Modify/Create: `cmd/prohibitorum/*` (mirror the existing `oidc-client`/`saml-sp`/`upstream-idp` command files — read them to match the flag/subcommand framework in use)
- Test: a CLI smoke or unit per the existing CLI test convention (if any)

**Acceptance Criteria:**
- [ ] `go run ./cmd/prohibitorum group create --slug eng --display-name Engineering [--no-expose]`, `group list`, `group add-member --slug eng --username alice`, etc. work against the dev DB.
- [ ] `oidc-client update --access-restricted` and `grant-group/grant-account/revoke-*` flags persist access; same for `saml-sp`.

**Verify:** `go build ./... && go run ./cmd/prohibitorum group list` (against dev DB)

**Steps:**

- [ ] **Step 1: Read** the existing `cmd/prohibitorum` command wiring for `oidc-client`/`upstream-idp` (flag parsing, DB open, `db.New(pool)` usage) and mirror it for a `group` command file with the six subcommands calling the Task 1 queries.
- [ ] **Step 2: Access flags** — extend the `oidc-client`/`saml-sp` commands with `--access-restricted` (calls `Set…AccessRestricted`) and grant/revoke flags (`--grant-group`, `--grant-account`, `--revoke-group`, `--revoke-account` taking slug/username/id) using the grant/revoke queries.
- [ ] **Step 3: Verify + commit**

```bash
go build ./...
git add cmd/prohibitorum
git commit -m "feat(rbac): CLI for groups and per-app access"
```

---

### Task 11: Smoke arc, docs, rebuild dist, final gate

**Goal:** End-to-end smoke coverage of the RBAC arc; update docs; rebuild + commit the embedded SPA; run the full green gate.

**Files:**
- Modify: `cmd/smoke/*` (add an RBAC block)
- Modify: `README.md` (check the RBAC box), `api.md`, `ARCHITECTURE.md`, `STATUS.md`
- Modify: `pkg/webui/dist` (rebuilt)

**Acceptance Criteria:**
- [ ] Smoke arc: create group → add a member → restrict an OIDC app → assert a non-member is denied (lands on `/error` `app_access_denied`) and a member gets a code → assert the `groups` claim is present for a client that requests the `groups` scope. (Add an analogous restrict/deny/allow check for a SAML SP if the smoke already exercises SAML SSO.)
- [ ] `README.md` line 41 RBAC checkbox is checked; `api.md` documents the new endpoints; `STATUS.md`/`ARCHITECTURE.md` reflect the access model.
- [ ] Full gate GREEN: `go build -tags nodynamic ./... && go vet ./... && go test ./...`; `cd dashboard && npm test && npm run build`; live smoke `SMOKE_EXIT=0`; `pkg/webui/dist` rebuilt + committed.

**Verify:** the full gate commands below.

**Steps:**

- [ ] **Step 1: Smoke** — read `cmd/smoke` to learn its admin-bootstrap + OIDC/SAML helpers, then add an RBAC block using the admin API (create group via `POST /groups`, add member, `POST /oidc-applications/{id}/access/set-restricted`, `…/access/grant`) and drive an authorize as a non-member (expect a 302 to `/error?...app_access_denied`) and as a member (expect a code). Then exchange the code and assert the id_token/userinfo `groups` claim for a client whose `allowed_scopes` include `groups` and that requests it. Mirror existing smoke step style and the `avatar-fed N` logging convention.
- [ ] **Step 2: Docs** — tick `README.md:41`; document endpoints in `api.md`; update `ARCHITECTURE.md` "Authorization model" paragraph to note the per-app access gate (IdP gates app access; RP still gates in-app policy) and `STATUS.md`.
- [ ] **Step 3: Rebuild dist + full gate**

```bash
go build -tags nodynamic ./... && go vet ./... && go test ./...
cd dashboard && npm ci && npm test && npm run build && cd ..
mise build                 # rebuild dashboard -> pkg/webui/dist + compile binary
# live smoke (see README "End-to-end smoke"):
podman compose up -d
export PROHIBITORUM_DATABASE_URL="postgres://prohibitorum:prohibitorum@localhost:5432/postgres?sslmode=disable"
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(openssl rand -base64 32)"
export PROHIBITORUM_PUBLIC_ORIGIN="http://localhost:8080"
export PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK="true"
go run ./cmd/prohibitorum &           # auto-migrates
go run ./cmd/smoke --base-url http://localhost:8080   # expect SMOKE_EXIT=0
```
(If `:8080` is held by the user's dev server, run your own server on `:8081` against a fresh throwaway DB, as prior cycles did.)

- [ ] **Step 4: Commit**

```bash
git add cmd/smoke README.md api.md ARCHITECTURE.md STATUS.md pkg/webui/dist
git commit -m "feat(rbac): smoke coverage, docs, and rebuilt dashboard bundle"
```

---

## Self-Review

**Spec coverage:** groups + direct users (T1–T6), per-app `access_restricted` default-false (T1, T5), no admin bypass (T7 predicate has no role branch), denied UX = IdP `/error` + `prompt=none`/passive native (T7), refresh re-check (T7 §3), `exposed_to_downstream` default-true (T1) with two-level opt-in: OIDC `groups` scope (T8) + SAML map entry (T9), admin UI groups + access cards + account-detail membership (T3, T4, T6), audit (T2/T5/T7 constants), CLI (T10), smoke + docs + dist (T11). All spec sections map to a task.

**Placeholder scan:** the only deliberate deferral is the Task 1 predicate test (may defer to T7/T11 if `pkg/db` has no DB-test convention) and `<familyID>` in T7 §3 (wired to the in-scope family id in `refresh.go`) — both are flagged with the concrete fallback, not left vague.

**Type consistency:** `ListExposedGroupSlugsByAccount(ctx, accountID) ([]string,error)` used in T8 (token/userinfo) and T9 (sso) identically. `IsAccountAuthorizedForOIDCClientParams{AccountID, ClientID}` / `IsAccountAuthorizedForSAMLSPParams{AccountID, SpID}` used in T7 — generated names to be confirmed against sqlc output (noted in T7). `projectAttributes(a, mapJSON, origin, groups)` signature changed once (T9) with all call sites updated. `AppAccessView{AccessRestricted, Groups, Accounts}` shared by T5 (API) and T6 (FE). Audit consts (`EventAccess*`, `FactorGroup`) added once (FactorGroup in T2; EventAccess* in T5) and reused.

## Open verification points for the implementer
- Confirm sqlc handles the partial-index `ON CONFLICT … WHERE` target; if not, use the guarded-insert fallback noted in T1 Step 2.
- Confirm the generated `Is…AuthorizedFor…Params` struct/field names; adjust T7 call sites accordingly.
- Confirm `refresh.go`'s in-scope family id for the durable revoke in T7 §3.
- `pkg/server` suite flakes under parallel shared-DB runs — re-run failing cases in isolation before treating as real.
