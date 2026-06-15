# RBAC — control which users each app is authorized for

**Date:** 2026-06-16
**Status:** Design approved; ready for implementation plan.
**TODO:** README "RBAC — control which users each app is authorized for".

## Summary

Prohibitorum gains a coarse **per-app access gate**: an admin can mark an app
(OIDC client or SAML SP) as *access-restricted* and then control which users may
sign in to it, by assigning **groups** and/or **individual accounts**. Groups are
a new first-class concept (group + membership). Groups can additionally be
exposed to downstream apps as an OIDC `groups` claim / SAML `groups` attribute,
gated by a per-group flag plus a per-app opt-in.

This evolves — does not replace — the existing model:

- **IdP gates access to the app** — whether this human may obtain a token /
  assertion for it at all.
- **RP still gates what you can do inside the app** — via claims, including the
  new `groups` claim.

The end-user **launchpad** (separate README TODO) stays **out of scope**, but the
authorization query this feature introduces is the one the launchpad will reuse.

## Decisions (resolved during brainstorming)

| # | Decision | Choice |
|---|----------|--------|
| Access model | unit of access control | **Groups (first-class) + individual accounts.** Okta/Entra-style app assignment. |
| Default posture | who can use an app with no assignment | **Per-app `access_restricted` flag, default `false`.** While off, any enrolled user may sign in (today's behavior — existing apps untouched). |
| Groups scope | are groups exposed to RPs | **Yes, with a per-group `exposed_to_downstream` flag.** Membership always gates sign-in; exposed groups additionally flow to apps that opt in. |
| Admin bypass | do `role='admin'` accounts bypass restriction | **No.** Admin governs the IdP console, not app entitlement. Admins are assigned like anyone; they can re-add themselves in the console if locked out. |
| Q1 denied UX | what a denied user sees | **IdP's own `/error` page** for both protocols ("You don't have access to *AppName*. Contact your administrator."). Avoids RP re-login loops. |
| Q2 refresh re-check | when revocation takes effect | **Re-check at the refresh-token grant** too, not only at interactive authorize. De-provisioning then cuts existing sessions within the access-token TTL. |
| Q3 exposed default | default of `exposed_to_downstream` | **`true`** (emit-by-default, flag to suppress). Safe because the app must *also* opt in (scope / attribute-map) before anything leaves the IdP. |
| Q4 account detail | edit membership from account page | **Editable from both** the group detail page (canonical) and the admin account detail page (convenient for onboarding). |
| Q5 CLI parity | CLI for groups/access | **Include.** `group` verb + `--access-restricted`/grant flags on `oidc-client`/`saml-sp`, matching the "every app type has a CLI verb" convention. |

## Data model

New migration `db/migrations/015_rbac.sql`. Forward-only, `IF NOT EXISTS`, all
defaults preserve current behavior (every existing app remains open). `group` is
a SQL reserved word, hence `user_group`.

### Groups + membership

```sql
CREATE TABLE IF NOT EXISTS user_group (
  id                      serial PRIMARY KEY,
  slug                    text NOT NULL UNIQUE,           -- claim-safe identifier
  display_name            text NOT NULL,
  description             text,
  exposed_to_downstream   boolean NOT NULL DEFAULT true,
  created_at              timestamptz NOT NULL DEFAULT now(),
  updated_at              timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT user_group_slug_format CHECK (slug ~ '^[a-z0-9](-?[a-z0-9])*$')
);

CREATE TABLE IF NOT EXISTS group_member (
  group_id    integer NOT NULL REFERENCES user_group(id) ON DELETE CASCADE,
  account_id  integer NOT NULL REFERENCES account(id)    ON DELETE CASCADE,
  created_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (group_id, account_id)
);
CREATE INDEX IF NOT EXISTS group_member_account_idx ON group_member(account_id);
```

- `slug` is the stable identifier that surfaces in claims/attributes
  (lowercase, `[a-z0-9-]`, no leading/trailing/double hyphen). Immutable-by-policy
  after creation is preferable (renaming a slug changes what RPs receive); the
  admin UI renames `display_name` freely but warns on slug change. (Plan decides
  whether to hard-block slug edits or allow-with-warning; default: allow on the
  update endpoint, warn in UI.)
- `exposed_to_downstream` default `true` (decision Q3).

### Per-app access grants

Mirrors the existing two-table app split (`oidc_client.client_id text` PK vs
`saml_sp.id bigint` PK). A grant points at **either** a group **or** an account:

```sql
CREATE TABLE IF NOT EXISTS oidc_client_access (
  client_id   text    NOT NULL REFERENCES oidc_client(client_id) ON DELETE CASCADE,
  group_id    integer          REFERENCES user_group(id)         ON DELETE CASCADE,
  account_id  integer          REFERENCES account(id)            ON DELETE CASCADE,
  created_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT oidc_client_access_one_principal CHECK (num_nonnulls(group_id, account_id) = 1)
);
CREATE UNIQUE INDEX IF NOT EXISTS oidc_client_access_group_uq
  ON oidc_client_access(client_id, group_id) WHERE group_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS oidc_client_access_account_uq
  ON oidc_client_access(client_id, account_id) WHERE account_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS saml_sp_access (
  saml_sp_id  bigint  NOT NULL REFERENCES saml_sp(id)    ON DELETE CASCADE,
  group_id    integer          REFERENCES user_group(id) ON DELETE CASCADE,
  account_id  integer          REFERENCES account(id)    ON DELETE CASCADE,
  created_at  timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT saml_sp_access_one_principal CHECK (num_nonnulls(group_id, account_id) = 1)
);
CREATE UNIQUE INDEX IF NOT EXISTS saml_sp_access_group_uq
  ON saml_sp_access(saml_sp_id, group_id) WHERE group_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS saml_sp_access_account_uq
  ON saml_sp_access(saml_sp_id, account_id) WHERE account_id IS NOT NULL;
```

### Restrict switch

Mirrors the existing `disabled` column + `set-disabled` endpoint pattern:

```sql
ALTER TABLE oidc_client ADD COLUMN IF NOT EXISTS access_restricted boolean NOT NULL DEFAULT false;
ALTER TABLE saml_sp      ADD COLUMN IF NOT EXISTS access_restricted boolean NOT NULL DEFAULT false;
```

## Enforcement

### Authorization query

One shared predicate, no admin bypass (decision: admin not bypassed):

> **authorized(account, app)** = `NOT app.access_restricted`
> **OR** a direct grant `(app, account)` exists
> **OR** the account is a member of any group granted to the app.

Implemented as sqlc queries returning a boolean, one per protocol:

```sql
-- db/queries/rbac.sql
-- name: IsAccountAuthorizedForOIDCClient :one
SELECT
  NOT c.access_restricted
  OR EXISTS (SELECT 1 FROM oidc_client_access a
             WHERE a.client_id = c.client_id AND a.account_id = @account_id)
  OR EXISTS (SELECT 1 FROM oidc_client_access a
             JOIN group_member m ON m.group_id = a.group_id
             WHERE a.client_id = c.client_id AND m.account_id = @account_id)
FROM oidc_client c
WHERE c.client_id = @client_id;

-- name: IsAccountAuthorizedForSAMLSP :one  (symmetric, on saml_sp / saml_sp_access)
```

(The exact SQL is the plan's to finalize; semantics above are binding.)

### Insertion points

Both checks run **after** the session is validated and `account.Disabled` is
already enforced, and **before** anything is issued:

- **OIDC** — `pkg/protocol/oidc/authorize.go::HandleAuthorize`, after `loadClient()`
  and scope validation (`authorize.go:52–86`), before the consent / `mintCode()`
  path. The authenticated account is `authn.SessionFromContext(r.Context()).Account`.
- **SAML** — `pkg/protocol/saml/sso.go::HandleSSO`, after the SP is loaded
  (`parseAuthnRequest`) and the session validated (`sso.go:104–139`), before
  `buildResponse()` (`sso.go:287`). Covers SP-initiated and IdP-initiated.
- **Refresh (decision Q2)** — `pkg/protocol/oidc/token.go` refresh-token grant:
  re-run `IsAccountAuthorizedForOIDCClient`; on failure return `invalid_grant`
  (and revoke the refresh token, consistent with existing rotation/revocation
  handling). SAML has no refresh; assertion lifetime bounds exposure there.

### Denied UX (decision Q1)

A user who is authenticated but not authorized is sent to the **IdP's own
`/error` threshold page** with a clear message ("You don't have access to
*AppName*. Contact your administrator."), for both protocols. Rationale: the user
is already on the IdP origin; a protocol-native `access_denied` bounced to the RP
tends to trigger an immediate re-login loop.

- **OIDC**: redirect (302) to the IdP `/error` SPA route with query params
  conveying a stable reason code + app display name (e.g.
  `/error?reason=app_access_denied&app=<display_name>`). Do **not** redirect
  `access_denied` to the RP `redirect_uri` (the redirect-to-RP error helper
  `redirectError()` is reserved for protocol errors). The `/error` page already
  exists as a threshold page; it gains an `app_access_denied` message string.
- **SAML**: render the same IdP `/error` page rather than auto-POSTing a
  `Responder`/`RequestDenied` SAML Response. (The protocol-native denial —
  `buildStatusResponse(statusResponder, statusRequestDenied)` then `writeAutoPost`
  — is documented as the strict alternative but not the default.)

### Audit

New audit event types, written through the request's tx querier (per the
FOR-UPDATE/audit-FK rule):

- `group.created`, `group.updated`, `group.deleted`
- `group.member.added`, `group.member.removed`
- `app.access.restricted_set` (on/off), `app.access.granted`, `app.access.revoked`
  (each tagged with app kind + id and principal kind + id)
- `app.access.denied` — emitted at authorize/SSO/refresh when a user is turned
  away (security-relevant; includes account id, app kind + id).

## Group exposure to downstreams

Two-level opt-in — nothing leaves the IdP unless **both** are true:

1. **Group side** — `user_group.exposed_to_downstream = true`.
2. **App side** — the app explicitly asks for groups.

### OIDC `groups` claim

- Add **`groups`** to `scopes_supported` in the discovery document
  (`pkg/protocol/oidc/oidc.go`).
- A client receives the claim only if `groups` ∈ its `allowed_scopes` **and** the
  scope is requested **and** consent is granted (normal consent machinery —
  `oidc_consent.granted_scopes`; first-party `require_consent=false` clients skip
  the prompt).
- `pkg/protocol/oidc/claims.go` gains `groupsClaims(account)` returning
  `{"groups": [<slugs>]}` — the **sorted** slugs of the user's groups where
  `exposed_to_downstream = true`. Wired into both `idTokenClaims()` and
  `userinfoClaims()` behind `hasScope(in.Scope, "groups")`, exactly parallel to
  the existing `profile`/`email` blocks (`claims.go:120–151`). Empty list emits
  `groups: []` when the scope is granted (present-but-empty is more useful to RPs
  than absent).

### SAML `groups` attribute

- A new well-known attribute-map **source token `"groups"`** for an SP's
  `attribute_map`, alongside the existing `"attributes.administrator"` special
  case in `pkg/protocol/saml/attributes.go::resolveSource` (`attributes.go:81–92`).
- `multi: true`; resolves to the user's exposed group slugs (one value each).
- Opt-in per SP by adding the map entry — **no new SP column**. The admin SAML
  attribute-map editor offers `groups` as a selectable source.

A group with `exposed_to_downstream = false` never appears in either, regardless
of app opt-in.

## Admin surface

Follows existing conventions: humacli admin API under `/api/prohibitorum/*`,
sudo-guarded mutations, dedicated `set`-style POST endpoints (mirroring
`set-disabled`), and the SPA's shared components (`TableSkeleton`, `EmptyState`,
`BackLink`, Status-card/Danger-zone patterns, `useApi().errorText`,
`useTransientFlag`).

### Groups section (`/admin/groups`)

Admin API (`groups`):
- `GET  /groups` — list (slug, display name, member count, exposed).
- `POST /groups` — create (sudo).
- `GET  /groups/{id}` — detail incl. members.
- `PUT  /groups/{id}` — update display_name / description / exposed_to_downstream
  / slug (sudo).
- `POST /groups/delete` — delete (sudo).
- `GET  /groups/{id}/members` — list members.
- `POST /groups/{id}/members` — add account (sudo).
- `POST /groups/{id}/members/remove` — remove account (sudo).

SPA: list page (table + create dialog) and detail page (edit fields, exposed
toggle, member management with account search/add + remove).

### App detail — Access card

On both the OIDC client detail and SAML SP detail pages, a new **Access** card:

- An `access_restricted` toggle in its own card (like the existing Status/disabled
  card), backed by a dedicated endpoint:
  - `POST /oidc-applications/{clientId}/access/set-restricted` (sudo)
  - `POST /saml-applications/{id}/access/set-restricted` (sudo)
- The assigned principals (groups + individual accounts) with add/remove:
  - `GET  …/access` — restricted flag + grants (groups, accounts).
  - `POST …/access/grant` — add a group or account grant (sudo).
  - `POST …/access/revoke` — remove a grant (sudo).
- When `access_restricted` is off, the list renders with a hint that the gate is
  inactive (assignments are informational until the toggle is on).

### Account detail (decision Q4)

The admin account detail page shows the account's group memberships and allows
**editing membership from there** (add/remove groups), writing the same
`group_member` join. The group detail page remains the canonical member editor.

## CLI (decision Q5)

Matching the "every app type has a CLI verb" convention (`go run ./cmd/prohibitorum …`):

- `group create|list|update|delete` and `group add-member|remove-member`.
- `oidc-client` / `saml-sp` gain `--access-restricted=true|false` and
  grant/revoke flags (e.g. `grant-group`/`grant-account`/`revoke-…`) so app
  access is scriptable.

## Testing & gate

- **Unit (Go)**: authorization query across the matrix — open app (always allow);
  restricted + direct grant; restricted + via-group; restricted + non-member
  (deny); restricted + member of a *different* group (deny). `groupsClaims()` —
  scope-gated, exposed-only, sorted, present-but-empty. SAML `groups` source —
  exposed-only, multi-valued.
- **Frontend (vitest + vue-tsc)**: groups list/detail, member management, Access
  card (restrict toggle + grant/revoke), account-detail membership editor.
- **Smoke (`cmd/smoke`)**: create a group → add a member → mark an app
  `access_restricted` → assert a non-member is denied (lands on `/error`
  `app_access_denied`) and a member is allowed → assert the `groups` claim is
  present for an OIDC client that requests the scope.
- **Green gate** (project discipline): `go build -tags nodynamic ./... && go vet
  ./... && go test ./...`; `cd dashboard && npm test` (vitest) and `npm run build`
  (vue-tsc); live smoke `SMOKE_EXIT=0`; rebuild + commit `pkg/webui/dist`.

## Scope boundaries

- **Out of scope**: the end-user launchpad (separate TODO). This feature provides
  the reusable authorization query it will need; no `/me/apps` endpoint or
  end-user UI ships here.
- **Out of scope**: nested groups / group hierarchies; dynamic/rule-based group
  membership; time-bound or just-in-time grants. Memberships are explicit.
- **Unchanged**: the opaque `attributes` map and existing claim/attribute
  emission; `role` (`user`/`admin`) semantics; the `disabled` gate on accounts
  and apps.

## Affected code (reference)

| Area | File(s) |
|------|---------|
| Migration | `db/migrations/015_rbac.sql` |
| Queries | `db/queries/rbac.sql` (new); touches `oidc.sql`, `saml_sp.sql` for `access_restricted` |
| OIDC enforcement | `pkg/protocol/oidc/authorize.go`, `token.go` |
| OIDC claim | `pkg/protocol/oidc/claims.go`, `oidc.go` (discovery scope) |
| SAML enforcement | `pkg/protocol/saml/sso.go` |
| SAML attribute | `pkg/protocol/saml/attributes.go` |
| Denied UX | IdP `/error` SPA route + a redirect helper for the authorize/SSO handlers |
| Admin API | `pkg/server/handle_admin_groups.go` (new); `handle_admin_oidc_clients.go`, `handle_admin_saml_sps.go` (access sub-endpoints); `handle_admin_accounts.go` (membership) |
| Audit | `pkg/audit` event types |
| SPA | `/admin/groups` list+detail; Access card on app detail; membership on account detail; `/error` message; i18n `en.ts` |
| CLI | `cmd/prohibitorum` `group` verb + access flags on `oidc-client`/`saml-sp` |
