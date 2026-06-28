# PAT Fine-Grained Maturity — Design

## Background

The just-shipped Personal Access Tokens feature (unpushed on `master`,
`docs/superpowers/specs/2026-06-28-personal-access-tokens-design.md`) gives a user a
bearer credential for the forward-auth gateway that authenticates **as the owner**
with the live owner-RBAC ceiling. Measured against GitHub's **fine-grained PAT
(FGPAT)** model, our *security ceiling* is mature (owner-RBAC re-checked every
request), but two layers are not:

- **Resource selection** — GitHub lets the user pick *which repositories* a token
  may touch. Our equivalent (`allowed_client_ids` = which forward-auth apps) is
  enforced in the gateway but has **no user UI**, and defaults to *all* reachable
  apps (not least-privilege).
- **Permissions** — GitHub tokens draw from a *provider-defined* permission
  vocabulary the user *picks from*. Our `upstream_scopes` are **free-form opaque
  strings typed by hand** — unusable for a normal user and uncontrolled by the IdP.

This sprint closes those two gaps and adds the adjacent FGPAT-parity items chosen
during brainstorming.

## Goals

- **A. Trust alerts** — operator warnings on the forward-auth admin pages (the
  deployment-critical trust assumptions).
- **B. User app-selection** — the user *chooses which forward-auth apps* a PAT may
  use, at create time (the FGPAT "resource selection").
- **C. Admin-defined per-app scope vocabulary** — each forward-auth app carries an
  admin-defined list of scope labels (the FGPAT "permissions"); the PAT dialog shows
  *those* as checkboxes; create validates against them.
- **Per-app scope isolation** — a PAT holds *per-app* scope selections; the gateway
  emits only the current app's scopes, so app B never sees app A's labels.
- **F. Least-privilege default** — creating a PAT requires selecting ≥1 app (or the
  explicit "all my apps" option), instead of defaulting to all reachable apps.
- **D. Admin oversight** — admins can list and revoke any user's PATs.

## Non-goals (deferred / out of scope)

- **E. Admin max-lifetime policy** — keep the current hardcoded 1..3650-day cap; an
  instance-configurable cap is a clean follow-up.
- **Org approval workflow / pending state / justification** — GitHub's org governance
  doesn't fit a single-tenant, first-party IdP.
- **Read/write/admin scope *levels*** — our scopes stay flat labels (opaque to the
  gateway, enforced by the upstream); GitHub's per-permission levels are not modeled.
- **Creation cap, inactivity auto-removal, hard-delete** — minor hygiene, deferred.

## Data model

The PAT feature is **unpushed / undeployed**, so its migration is *amended* to the
final shape rather than chained with a cleanup migration (per the project's
"squash pre-deployment migrations" rule). The forward-auth column lands in a new
migration because `oidc_client` is a deployed table.

### Amend migration `023_personal_access_token.sql` (unpushed → safe to edit)

Replace the flat `upstream_scopes text[]` + `allowed_client_ids text[]` columns with:

```sql
  all_apps   boolean NOT NULL DEFAULT false,
  app_grants jsonb   NOT NULL DEFAULT '{}'::jsonb,   -- { "<client_id>": ["scope", …], … }
```

- `app_grants` **keys** = the forward-auth apps this PAT may use (resource selection);
  **values** = that app's chosen scopes (emitted as `Remote-Scopes` only for that app).
- `all_apps = true` = the explicit "all my apps" escape hatch: allowed everywhere the
  owner can reach, **identity only, no scopes** (`app_grants` ignored). Per-app scopes
  are meaningless without enumerating apps, so this mode carries none.
- **Least-privilege (F):** `all_apps=false` with an empty `app_grants` is invalid at
  create — the user must pick ≥1 app or choose `all_apps`.

*Operational note:* amending `023` means a dev DB that already ran the old `023` must
be reset (`mise run db reset`); the smoke's throwaway `prohibitorum_smoke` DB always
runs fresh. No deployed DB has `023`.

### New migration `024_forward_auth_scopes.sql`

```sql
-- +goose Up
ALTER TABLE oidc_client
  ADD COLUMN IF NOT EXISTS forward_auth_scopes jsonb NOT NULL DEFAULT '[]'::jsonb;
-- [{ "name": "repo:read", "description": "…" }, …]  — admin-defined vocabulary,
-- opaque to the gateway; descriptions guide the user's PAT scope picker.

-- +goose Down
ALTER TABLE oidc_client DROP COLUMN IF EXISTS forward_auth_scopes;
```

`forward_auth_scopes` is a JSONB array of `{ name, description? }`. `name` validated
`^[a-zA-Z0-9](:?[a-zA-Z0-9._-]+)*$` (a label vocabulary); `description` optional, for
the picker.

## Flows

### Candidate-apps endpoint — `GET /me/forward-auth-apps`

Returns the caller's **authorized** forward-auth apps *with each app's vocabulary*:

```jsonc
[ { "clientId": "ci-deploy", "displayName": "CI Deploy",
    "scopes": [ { "name": "repo:read", "description": "…" }, … ] }, … ]
```

Candidate set = `ListAuthorizedForwardAuthAppsForAccount` (RBAC-filtered) extended to
also select `forward_auth_scopes`, so the picker can never offer an app the owner
can't reach (the owner-ceiling property), nor a scope the app didn't define.

### Create — `POST /me/tokens`

Request body becomes:
```jsonc
{ "name": "…", "expiresInDays": 90,
  "allApps": false,
  "appGrants": { "ci-deploy": ["repo:read"], "metrics": [] } }
```

Validation (rejected with `bad_request` otherwise):
- `name` 1..128, `expiresInDays` 0/nil=never else 1..3650 (unchanged).
- If `allApps=false`: `appGrants` non-empty; every key ∈ the owner's authorized FA
  apps; every scope ∈ that app's `forward_auth_scopes` names.
- If `allApps=true`: `appGrants` must be empty (identity-only).
- Mutually-exclusive: not both `allApps=true` and a non-empty `appGrants`.

Stored as `all_apps` + `app_grants`. Plaintext returned once (unchanged). Audit
`FactorPAT`/`EventRegister` (unchanged).

**Contract changes** (`pkg/contract/auth.go` + the dashboard TS interface): the create
input drops `upstreamScopes`/`allowedClientIds` and gains `allApps bool` +
`appGrants map[string][]string`. `PersonalAccessTokenView` drops `upstreamScopes`/
`allowedClientIds` and gains `allApps` + `appGrants` (client_id → scopes); the
once-only `PersonalAccessTokenCreated` is otherwise unchanged.

### Gateway verify — per-app isolation

`verifyForwardAuthPAT` (replacing `patAllowsClient` + flat scopes):

```text
look up PAT by hash (revoked/expired filtered in SQL) → 401 on miss
load owner; disabled → 401
if pat.all_apps:
    scopes = []                       # identity-only
else:
    grants = json.Unmarshal(pat.app_grants)        // map[string][]string
    scopes, ok = grants[client.ClientID]
    if !ok → 403                       # PAT not granted for this app
IsAccountAuthorizedForOIDCClient(owner, client) live → 403 on deny
emit Remote-* (Remote-Scopes = scopes) ; TouchPATLastUsed ; 200
```

`Remote-Scopes` now carries only the current app's scopes. A scope that an admin
later removes from the vocabulary still emits (opaque; the upstream ignores unknown
labels) — acceptable, not a security issue (scopes never grant gateway access).

## Admin surfaces

### C — per-app scope vocabulary editor

`AdminForwardAuthAppDetailView` (+ the create form) gains a **scope vocabulary
editor**: a list of `{ name, description }` rows (mirror `AttributeMapEditor` /
`ListInput`). Threaded through the FA-app admin create/update handlers and the
`forward_auth_scopes` column. Validated server-side (name pattern, unique names).

### A — trust alerts

A `warning`-styled `Alert` beside the existing Traefik snippet on
`AdminForwardAuthAppDetailView` (and `AdminForwardAuthAppsView` create), stating the
deployment-critical, IdP-uncontrollable trust assumptions:
- the app **must be reachable only through Traefik** (keystone — else identity headers
  can be spoofed directly);
- Traefik must forward **all five** `Remote-*` via `authResponseHeaders`;
- Traefik must **strip the inbound `Authorization`** header (so a raw PAT never
  reaches upstream).

(The earlier user-facing "scopes are opaque" note is now moot — scopes are picked
from a guided, admin-defined list, not typed.)

### D — admin oversight of user PATs

- `GET /accounts/{id}/tokens` — admin-gated (🔓 admin), lists the account's
  non-revoked PATs as `PersonalAccessTokenView` (no secret).
- `POST /accounts/tokens/revoke` `{ id }` — admin + sudo (🔐); new
  `RevokePATByID(id) :execrows` (not owner-scoped); audit `FactorPAT`/`EventRevoke`
  with `detail.actor="admin"`.
- A **Personal access tokens** card on `AdminAccountDetailView` (list + revoke via
  `ConfirmDialog`), mirroring its existing sessions card.

## Frontend (end-user)

`TokensView.vue` create dialog replaces the free-form `ScopeSelector` with the
**app + per-app-scope picker**:
- On open, `GET /me/forward-auth-apps`.
- An **"All my apps (no scopes)"** switch → `allApps`.
- Else a list of the user's apps, each with an include checkbox; an included app
  expands to *its* scope checkboxes (name + description from the vocabulary).
- Submit disabled unless `allApps` or ≥1 app selected (least-privilege).
- Builds `{ allApps, appGrants }`.

The PAT **list** renders per-app grants (app display name → its scopes) instead of the
old flat scopes/`allowedClientIds`; client_ids resolved to display names from the
fetched apps. en + zh parity maintained.

## Effective-permission model (recap)

For a request to app **X** with PAT **P** owned by **U**:

> **allow** ⇔ P not revoked/expired (401) ∧ U not disabled (401)
> ∧ ( P.all_apps ∨ X ∈ keys(P.app_grants) ) (else 403)
> ∧ IsAccountAuthorizedForOIDCClient(U, X) live (else 403)
>
> on allow → `Remote-*` from U; `Remote-Scopes` = P.all_apps ? [] : P.app_grants[X]

Create-time validation (apps ⊆ authorized, scopes ⊆ vocabulary) is a UX/least-surprise
guard; the **live RBAC re-check at verify is the security boundary** (a stale grant to
an app the user later lost access to → 403).

## Security considerations

- **Controlled vocabulary** — scopes are validated against the app's admin-defined set
  at create; no arbitrary self-asserted labels enter via the UI. (The API still treats
  unknown scopes as `bad_request`.)
- **Per-app isolation** — a request to app B never receives app A's scope labels.
- **Owner ceiling preserved** — app_grants keys validated ⊆ owner's authorized apps at
  create *and* re-checked live via RBAC at verify (defense in depth).
- **Least-privilege default** reduces blast radius of a leaked PAT.
- **Admin oversight** — admins can enumerate and revoke any user's PATs; admin revoke
  is sudo-gated and audited.
- Scopes remain **hints the upstream enforces**, never gateway-granting access — the
  authorization boundary is still identity + groups + per-app gate.

## Testing

- **Unit (`pkg/protocol/oidc`)** — verify per-app emit: `all_apps` → 200 empty
  `Remote-Scopes`; granted app → 200 with that app's scopes; non-granted app → 403;
  revoked/expired/disabled → 401; live-RBAC-deny on a granted app → 403.
- **Handler (`pkg/server`)** — create validates apps ⊆ authorized + scopes ⊆ vocab +
  least-privilege (reject empty non-all_apps) + all_apps-with-grants conflict; admin
  list/revoke owner-agnostic + audited; `/me/forward-auth-apps` returns only authorized
  apps with their vocab.
- **Admin FA handlers** — `forward_auth_scopes` round-trips through create/update;
  name-pattern + uniqueness validation.
- **Frontend (vitest)** — picker builds correct `appGrants`; least-privilege disables
  submit; list renders per-app grants; admin PATs card lists + revokes; en/zh parity.
- **Smoke (`ci:smoke`)** — extend the PAT arc: admin sets a vocabulary on the FA app;
  create a PAT granting `app→[scope]`; verify against that app → 200 + `Remote-Scopes`
  = the scope; verify against a non-granted app → 403; an `all_apps` PAT → 200 with
  empty `Remote-Scopes`; admin lists + revokes the user's PAT.

## FGPAT maturity mapping (reference)

| FGPAT dimension | After this sprint |
|---|---|
| Resource selection (repos → apps) | ✅ user picks apps; least-privilege default |
| Permissions (provider vocabulary, picked) | ✅ admin-defined per-app vocabulary, checkbox-picked, validated |
| Per-resource isolation | ✅ per-app scope isolation |
| Privilege ceiling | ✅ (already) live owner-RBAC every request |
| Admin oversight | ✅ admin list/revoke user PATs |
| Expiration max-lifetime policy | ◐ deferred (E) |
| Org approval / pending | ✗ non-goal (single-tenant) |
| Scope read/write levels | ✗ non-goal (flat labels) |

## Out of scope

E (max-lifetime policy), org approval workflow, scope read/write levels, creation cap,
inactivity auto-removal, hard-delete of revoked rows. Each is a clean later addition.
