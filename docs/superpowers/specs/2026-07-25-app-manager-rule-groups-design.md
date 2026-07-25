# Delegated application managers and app-bound access groups

**Date:** 2026-07-25  
**Status:** Design approved; awaiting written-spec review

## Summary

Prohibitorum gains an `app_manager` account role for delegated administration of
specific downstream applications. A global administrator assigns an application
manager to individual OIDC, forward-auth, or SAML applications. The assignment
grants access-policy management only; it does not grant the manager permission
to use the application or edit its protocol configuration.

The current global/shared-group RBAC model is replaced with app-bound groups.
Each application may have at most one manual group, with per-account `allow`,
`deny`, or neutral decisions, and any number of calculated rule groups. Rule
groups evaluate current verified-connection, enrolled-login-method, and avatar
facts. They do not accept manual members. A restricted application admits a
user when the manual group allows them or, in the absence of a manual decision,
any rule group evaluates true. A manual denial overrides every rule group.

This is a destructive clean cutover. Existing global groups and app-access
assignments are deleted, and every application's access restriction is reset to
open. There is no compatibility or legacy-group path.

## Goals

- Delegate application access management without exposing global admin powers.
- Keep management authority and application usage entitlement independent.
- Make all groups local to one immutable downstream application.
- Support one explicit per-app allowlist/denylist and multiple calculated groups.
- Evaluate policy from current identity facts without stale reconciliation state.
- Emit calculated groups as downstream group claims for their owning app.
- Preserve protocol-correct denial behavior and refresh-time deprovisioning.

## Non-goals

- A generic resource/action permission engine.
- Delegated editing of OIDC, forward-auth, or SAML protocol configuration.
- Arbitrary account-attribute expressions, regexes, scripts, CEL, or Rego.
- Global/shared groups or groups grantable to more than one application.
- Manual member overrides on rule groups.
- Compatibility with existing RBAC group or access-assignment data.
- Background reconciliation, scheduled jobs, or persisted calculated membership.

## Approved decisions

| Area | Decision |
|---|---|
| Delegated role | Add `app_manager` alongside `user` and `admin`. |
| Assignment | Global admins assign managers to specific apps; multiple managers per app and multiple apps per manager are allowed. |
| Usage entitlement | Manager assignment does not grant application access. |
| Security boundary | Existing admin routes remain admin-only; delegated management uses dedicated routes with per-app authorization. |
| Group scope | Every group is bound immutably to exactly one OIDC or SAML application. Forward-auth groups bind to the backing OIDC client. |
| Group kinds | Zero or one manual group per app; zero or more rule groups per app. |
| Manual group | Tri-state account decision: `allow`, `deny`, or absent/neutral. |
| Rule groups | Calculated-only; no manual assignment or rejection of users. |
| Rule combination | Each group evaluates independently; rule-group eligibility is the OR of all group results. |
| Precedence | Manual deny denies; manual allow allows; neutral falls through to OR of rule groups. |
| Claims | A manually allowed user receives the manual-group slug plus every exposed matching rule-group slug. |
| Facts | Confirmed connections, enrolled login methods, any avatar, and user-uploaded avatar. |
| Evaluation | Live from current database facts; no materialized calculated members. |
| Cutover | Delete old group/access data and reset all apps to unrestricted/open. |

## Authorization model

### Roles

The account role domain becomes:

- `user`: self-service and downstream authentication only.
- `app_manager`: user capabilities plus delegated management of assigned apps.
- `admin`: unchanged global authority over the whole instance.

Session middleware already reloads the account on every authenticated request.
Role promotion, demotion, disablement, and deletion therefore affect active
sessions immediately. Changing an `app_manager` to another role transactionally
deletes all of that account's manager assignments.

An app manager cannot assign managers, promote accounts, edit accounts, inspect
credentials, manage providers, view global audit data, or edit instance settings.

### Manager assignments

Use FK-backed protocol tables rather than a polymorphic reference without
referential integrity:

```sql
CREATE TABLE oidc_client_manager (
  client_id  text    NOT NULL REFERENCES oidc_client(client_id) ON DELETE CASCADE,
  account_id integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by integer REFERENCES account(id) ON DELETE SET NULL,
  PRIMARY KEY (client_id, account_id)
);

CREATE TABLE saml_sp_manager (
  saml_sp_id bigint  NOT NULL REFERENCES saml_sp(id) ON DELETE CASCADE,
  account_id integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by integer REFERENCES account(id) ON DELETE SET NULL,
  PRIMARY KEY (saml_sp_id, account_id)
);
```

Forward-auth applications use `oidc_client_manager` because they are OIDC client
rows with `forward_auth_enabled = true`. Route lookup additionally verifies the
requested application kind so an OIDC route cannot expose a forward-auth app or
vice versa.

Only global admins may create or remove manager assignments. Both operations
require fresh sudo. Production handlers accept only accounts whose current role
is `app_manager`; role changes clean stale assignments transactionally.

## Destructive schema cutover

A new forward migration performs these operations in one migration transaction:

1. Drop current OIDC/SAML app-access grant tables and global group-membership
   tables, including all rows.
2. Reset `oidc_client.access_restricted` and `saml_sp.access_restricted` to
   `false` so the deleted policy cannot lock everyone out.
3. Replace the account role check/domain so `app_manager` is valid.
4. Create manager-assignment tables.
5. Create the app-bound group and manual-decision tables described below.
6. Create all FK, check, and partial unique indexes before completing.

The down migration restores the old table shapes only. It cannot recreate the
intentionally deleted data. This limitation is explicit in the migration.

## App-bound group data model

### Groups

```sql
CREATE TABLE user_group (
  id                    serial PRIMARY KEY,
  kind                  text NOT NULL CHECK (kind IN ('manual', 'rule')),
  slug                  text NOT NULL,
  display_name          text NOT NULL,
  description           text,
  exposed_to_downstream boolean NOT NULL DEFAULT true,
  rule                  jsonb,
  oidc_client_id        text REFERENCES oidc_client(client_id) ON DELETE CASCADE,
  saml_sp_id             bigint REFERENCES saml_sp(id) ON DELETE CASCADE,
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now(),
  UNIQUE (id, kind),
  CHECK (num_nonnulls(oidc_client_id, saml_sp_id) = 1),
  CHECK (
    (kind = 'manual' AND rule IS NULL) OR
    (kind = 'rule' AND rule IS NOT NULL AND jsonb_typeof(rule) = 'object')
  ),
  CHECK (slug ~ '^[a-z0-9](-?[a-z0-9])*$')
);
```

Partial indexes provide per-app slug uniqueness and one manual group per app:

```sql
CREATE UNIQUE INDEX user_group_oidc_slug_uq
  ON user_group (oidc_client_id, slug) WHERE oidc_client_id IS NOT NULL;
CREATE UNIQUE INDEX user_group_saml_slug_uq
  ON user_group (saml_sp_id, slug) WHERE saml_sp_id IS NOT NULL;
CREATE UNIQUE INDEX user_group_oidc_manual_uq
  ON user_group (oidc_client_id) WHERE kind = 'manual';
CREATE UNIQUE INDEX user_group_saml_manual_uq
  ON user_group (saml_sp_id) WHERE kind = 'manual';
```

The app-binding fields are accepted only on create. Update contracts contain no
binding fields. Moving a group requires deleting and recreating it.

### Manual decisions

```sql
CREATE TABLE group_manual_decision (
  group_id    integer NOT NULL,
  group_kind  text NOT NULL DEFAULT 'manual' CHECK (group_kind = 'manual'),
  account_id  integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  effect      text NOT NULL CHECK (effect IN ('allow', 'deny')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  created_by  integer REFERENCES account(id) ON DELETE SET NULL,
  PRIMARY KEY (group_id, account_id),
  FOREIGN KEY (group_id, group_kind)
    REFERENCES user_group(id, kind) ON DELETE CASCADE
);
```

The composite FK guarantees that manual decisions cannot target rule groups.
Absence of a row is neutral. Upsert changes allow to deny or deny to allow;
clear deletes the row.

## Rule contract

Rules use a versioned JSON envelope and a closed, recursively validated AST:

```json
{
  "version": 1,
  "condition": {
    "op": "all",
    "children": [
      { "fact": "connection.provider", "provider": "corporate-oidc" },
      {
        "op": "any",
        "children": [
          { "fact": "login_method", "method": "passkey" },
          { "fact": "avatar", "source": "user_uploaded" }
        ]
      }
    ]
  }
}
```

Supported condition nodes:

- `all`: true when every child condition is true; empty children are invalid.
- `any`: true when at least one child condition is true; empty children are invalid.
- `not`: exactly one child; negates it.
- `connection.provider`: confirmed connection to a known provider slug.
- `connection.protocol`: confirmed connection to `oidc`, `steam`, or `vrchat`.
- `login_method`: `passkey`, `password_totp`, or `federation`.
- `avatar`: `any` or `user_uploaded`.

Validation rejects unknown fields and values, empty combinators, malformed node
shapes, nonexistent provider slugs, more than 64 total nodes, nesting deeper than
8 nodes, or more than 32 children in one combinator. The existing 64 KiB request
body limit remains the outer payload bound. Errors use `invalid_group_rule` and
expose only a JSON path and stable reason code.

## Account facts

A normalized `AccountFacts` value is the only evaluator input. It contains no
raw secrets or provider metadata.

- Confirmed connections come from `account_identity.confirmed_at IS NOT NULL`.
  A disabled provider does not invalidate the fact that the connection was
  verified.
- `passkey` is true when at least one usable WebAuthn credential exists.
- `password_totp` is true only when both a password and confirmed TOTP exist.
- `federation` is true when a confirmed identity belongs to at least one
  currently enabled direct-sign-in provider. Link-only VRChat does not count.
- `avatar.any` is true for a currently usable user upload or a verified upstream
  avatar candidate.
- `avatar.user_uploaded` is true only for a usable user-uploaded avatar.
- Disabled accounts fail before policy evaluation and are omitted from delegated
  account search and rule previews.

The implementation reuses the canonical authentication-method semantics in
`pkg/authn.AvailableMethods` rather than introducing a second definition.

## Evaluation and claims

For a restricted app and active account:

```text
manual = decision from the app's optional manual group
matches = every app-bound rule group whose condition(AccountFacts) is true

if manual == deny:  denied
if manual == allow: allowed
if any(matches):    allowed
otherwise:           denied
```

An unrestricted app remains open regardless of configured groups. Its groups
are retained but access controls are inactive, matching the current restriction
toggle behavior.

When an allowed app requests group projection:

- include the exposed manual-group slug when the manual decision is `allow`;
- include every exposed matching rule-group slug, even when manual allow already
  determined access;
- sort and deduplicate slugs;
- never emit a bound group to any app other than its owner;
- retain the existing app-side opt-in: OIDC `groups` scope or SAML attribute-map
  `groups` source.

A manual denial produces no token/assertion, so no claims are emitted.

## Shared policy service

One protocol-neutral service owns:

- app lookup and app-kind validation;
- manager-assignment authorization;
- account-fact loading;
- rule parsing and evaluation;
- manual precedence and final access decision;
- matching-group projection and explanation trees;
- paginated preview evaluation.

OIDC authorize and refresh, forward-auth verification, SAML SSO, launchpad
listing, OIDC claims/userinfo, and SAML attributes call this service. Protocol
handlers do not reimplement policy.

Read-time evaluation prevents stale memberships after passkey/TOTP changes,
identity confirmation/unlinking, provider enablement changes, or avatar changes.
For one authorization request, facts and rules are loaded once and reused for
the access decision and claims.

## HTTP APIs

### Global-admin manager assignment

Existing OIDC, forward-auth, and SAML app detail APIs gain manager list/add/remove
operations. Reads require admin. Add/remove require admin plus fresh sudo.
Responses expose only account ID, username, display name, disabled state, and
assignment timestamp.

### Delegated application surface

Dedicated routes use `/api/prohibitorum/managed-applications`:

- list assigned apps;
- get one assigned app's access workspace;
- set access restriction;
- list/create/get/update/delete bound groups;
- list/upsert/clear manual decisions on the sole manual group;
- preview a rule group against paginated active accounts;
- explain one account's result for one group and the final app decision;
- search active accounts using a minimal identity summary.

Application kind is explicit: `oidc`, `forward_auth`, or `saml`. OIDC and
forward-auth IDs are URL-escaped client IDs; SAML IDs are numeric. Every handler
first validates the caller's role and exact manager assignment. Unassigned,
wrong-kind, deleted, and nonexistent apps all return the same `404` response.

Global admins use the same access-workspace service from existing app detail
pages, but the existing admin-only endpoint boundary remains intact.

All delegated mutations use the existing JSON content-type and 64 KiB body-size
wrapper. Reversible access-policy mutations do not require fresh sudo. Manager
assignment/removal does.

## Frontend

### Navigation and routes

- `app_manager` sees a “Managed applications” navigation section.
- Global-admin navigation remains unchanged except that the standalone Groups
  section is removed.
- Admin app detail pages add manager assignments and embed the shared access
  workspace.
- App managers see only the access workspace for assigned apps; configuration,
  secret, disable/delete, and manager-assignment controls are absent.

### Access workspace

The workspace shows:

1. open/restricted state and a restriction toggle;
2. the optional manual group's allowlist and denylist, with allow/deny/clear
   actions and neutral as the default;
3. rule-group cards with rule summary, exposure state, matching-count preview,
   edit/delete actions, and paginated calculated members;
4. a rule builder for nested all/any/not conditions using the closed fact catalog;
5. an account explanation view showing safe boolean inputs and node results;
6. explicit copy that manual deny overrides every rule, manual allow grants
   access, and a neutral account falls through to OR of rule groups.

Enabling restriction with no manual decisions and no matching-capable rule group
requires an explicit destructive confirmation because it denies every account.
The backend still allows this fail-closed state.

## CLI cutover

Obsolete global `group` commands and shared-group/direct-account grant flags are
removed. App-scoped commands replace them for:

- assigning/removing application managers;
- creating the sole manual group;
- setting/clearing manual allow or deny decisions;
- creating/updating rule groups from a validated JSON rule file;
- listing groups and previewing calculated matches;
- toggling app restriction.

There are no aliases or deprecated compatibility commands.

## Errors

New stable errors:

- `manager_assignment_not_found` for admin-only assignment mutation races;
- `manual_group_exists` (`409`) when an app already has a manual group;
- `invalid_group_kind` (`400`);
- `invalid_group_rule` (`400`) with allowlisted `{path, reason}` details;
- `group_slug_conflict` (`409`) scoped to one app;
- `group_not_found` (`404`);
- generic app-not-found (`404`) for unassigned delegated access.

Database uniqueness/FK violations are mapped to these stable errors. Raw SQL,
provider metadata, subjects, connection email addresses, and rule fact values
never enter public errors.

## Audit

Audit events cover:

- application manager assigned/removed;
- app restriction enabled/disabled;
- manual or rule group created/updated/deleted;
- manual account decision allowed/denied/cleared;
- restricted app access denied.

Records include actor account ID, target account ID when applicable, app kind and
ID, group ID, and action. They do not include raw rule JSON, identity metadata,
credential details, or evaluated fact values. Existing protocol denial records
remain and identify whether the final source was `manual_deny` or `no_matching_group`.

## Protocol denial behavior

The current protocol-specific behavior remains binding:

- OIDC interactive authorization redirects to the IdP error page.
- OIDC `prompt=none` returns protocol-native `access_denied`.
- OIDC refresh re-evaluates current policy; denial revokes the refresh family and
  returns `invalid_grant`.
- SAML interactive denial uses the IdP error page; passive requests return
  `Responder` / `RequestDenied`.
- Forward-auth returns its existing denial response and issues no downstream
  session.
- Launchpad and PAT app candidate lists omit denied apps.

Disabled-account enforcement runs before every policy decision.

## Verification

### Migration and database

- Existing RBAC rows are removed and every app resets to unrestricted.
- Exactly one app FK is non-null per group.
- Slugs are unique per app.
- Partial indexes permit at most one manual group per app.
- Manual decisions cannot reference rule groups.
- App/group/account deletion cascades correctly.
- `app_manager` role transitions clean manager assignments.

### Go unit tests

- Rule parser rejects every malformed node shape, unknown field/fact/value, depth,
  node-count, child-count, and missing-provider boundary.
- Evaluator covers each fact and nested all/any/not truth tables.
- Manual deny, allow, and neutral precedence.
- Multiple rule groups use OR; false never overrides another true.
- Manual allow still projects matching rule groups into claims.
- App-aware claims never leak a group across apps.
- Disabled accounts always fail before evaluation.

### Authorization integration tests

- App manager can mutate an assigned app and receives indistinguishable `404`
  for unassigned/wrong-kind/nonexistent apps.
- Manager assignment alone does not grant application usage.
- OIDC authorize and refresh, forward-auth, SAML interactive/passive, launchpad,
  PAT candidates, OIDC ID token/userinfo, and SAML attributes all use the shared
  result.
- Passkey/TOTP, identity, provider-enabled, and avatar changes affect the next
  decision without reconciliation.
- Refresh denial revokes the family.

### Frontend tests

- Role-aware navigation and route guards.
- Assigned-app list and access-only detail.
- Manager controls absent from delegated view and present for admins.
- One-manual-group behavior; allow/deny/clear transitions.
- Rule-builder shape validation and safe explanations.
- Calculated member preview and pagination.
- Restriction-with-empty-policy confirmation.

### End-to-end smoke

1. Promote an account to `app_manager` and assign one app.
2. Verify it cannot see or mutate another app and cannot edit app configuration.
3. Create the manual group and rule groups.
4. Verify manual allow, manual deny, neutral matching, neutral non-matching, and
   multiple true rule-group claims.
5. Add/remove a login method, confirmed connection, and avatar; verify the next
   authorization reflects each change.
6. Verify manager assignment does not grant app usage.
7. Verify refresh revocation after policy stops allowing the account.

## Documentation updates during implementation

Update `ARCHITECTURE.md`, `api.md`, `STATUS.md`, `PRODUCT.md`, CLI help, and the
embedded dashboard bundle after implementation and verification. Remove every
statement describing global/shared groups or direct per-app account grants.
