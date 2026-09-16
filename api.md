# Prohibitorum — HTTP API reference

Routes registered via `registerOpHTTP` / `registerSudoOpHTTP` / `registerAdminBodyOpHTTP` in `pkg/server/operations.go`. These use raw chi handlers (need `Set-Cookie` writes and direct streaming control), so OpenAPI does not cover them.

Route-to-source cross-reference:
- `pkg/server/operations.go` — `registerOpHTTP`, `registerSudoOpHTTP`, `registerAdminBodyOpHTTP`, `withFreshSudo`, `withAdminBodyControls`
- `pkg/server/server.go` — `registerOperations()` mounts every route

**Gate notation:**
- 🔓 = active admin session (`account.role = 'admin'`).
- 🔐 = active admin session plus a **fresh sudo grant** (valid for configured `sudo_ttl`, default 15 min; covers multiple gated actions until expiry).
- `manager` = active `app_manager` or `admin` session. A non-admin manager must also hold an exact assignment for the application addressed by the route.

`registerSudoOpHTTP` centralises admin auth, JSON content type, 64 KiB body limit, and fresh-sudo enforcement. `registerAdminBodyOpHTTP` applies the same JSON/body controls without sudo; it is also used for reversible delegated policy mutations, with the `manager` requirement instead of an admin requirement. A route-policy test asserts that each 🔐 mutation returns `sudo_required` (HTTP 401) without a fresh sudo grant.

All management and delegated routes use the `/api/prohibitorum` prefix. Administrative resource names are `oidc-applications`, `forward-auth-apps`, `saml-applications`, and `identity-providers`; CLI verbs are `oidc-client`, `forward-auth-app`, `saml-sp`, and `upstream-idp`.

---

## OIDC applications (downstream relying parties)

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/oidc-applications` | 🔓 | List all clients. `client_secret_hash` never returned. |
| GET | `/api/prohibitorum/oidc-applications/{clientId}` | 🔓 | Get one client. Same no-secret guarantee. |
| POST | `/api/prohibitorum/oidc-applications` | 🔐 | Create a client. Confidential clients (`public: false`): generates a 32-byte `crypto/rand` secret, returns it in `secret` **once only** — only the argon2id hash is persisted. Public clients return no secret. |
| PUT | `/api/prohibitorum/oidc-applications/{clientId}` | 🔐 | Full replacement of mutable config fields (display name, redirect URIs, scopes, etc). Does not touch the client secret. |
| POST | `/api/prohibitorum/oidc-applications/rotate-secret` | 🔐 | Body: `{"clientId": "..."}`. Generates and stores a new secret; returns new cleartext in `secret` **once only**. Guaranteed ≠ previous secret. |
| POST | `/api/prohibitorum/oidc-applications/delete` | 🔐 | Body: `{"clientId": "..."}`. Hard-deletes the client row. |
| POST | `/api/prohibitorum/oidc-applications/set-disabled` | 🔓 | Body: `{"clientId":"...","disabled":<boolean>}`. Reversibly disables or enables an OIDC application. |
| GET | `/api/prohibitorum/oidc-applications/{clientId}/managers` | 🔓 | List only safe manager-assignment views. |
| POST | `/api/prohibitorum/oidc-applications/{clientId}/managers` | 🔐 | Body: `{"accountId":<integer>}`. Assign an enabled `app_manager` account. Returns 204. |
| POST | `/api/prohibitorum/oidc-applications/{clientId}/managers/remove` | 🔐 | Body: `{"accountId":<integer>}`. Remove the assignment. Returns 204. |

## Forward-auth applications

Forward-auth applications are distinct from normal OIDC application administration, even though each is backed by an OIDC client. They have a fixed OIDC callback and use the same app-bound policy and manager assignment as that backing client. Their scope vocabulary is an ordered `scopes` array of `{name: string, description: string}` pairs. It is opaque to Prohibitorum; a protected service interprets the labels.

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/forward-auth-apps` | 🔓 | List forward-auth apps. |
| GET | `/api/prohibitorum/forward-auth-apps/{clientId}` | 🔓 | Get one app. |
| POST | `/api/prohibitorum/forward-auth-apps` | 🔐 | Create an app and its fixed OIDC-client configuration. |
| PUT | `/api/prohibitorum/forward-auth-apps/{clientId}` | 🔐 | Replace mutable forward-auth configuration and scope vocabulary. |
| POST | `/api/prohibitorum/forward-auth-apps/set-disabled` | 🔓 | Body: `{"clientId":"...","disabled":<boolean>}`. |
| POST | `/api/prohibitorum/forward-auth-apps/delete` | 🔐 | Body: `{"clientId":"..."}`. Hard-delete the app. |
| GET | `/api/prohibitorum/forward-auth-apps/{clientId}/managers` | 🔓 | List manager assignments. |
| POST | `/api/prohibitorum/forward-auth-apps/{clientId}/managers` | 🔐 | Body: `{"accountId":<integer>}`. Assign an enabled `app_manager`; returns 204. |
| POST | `/api/prohibitorum/forward-auth-apps/{clientId}/managers/remove` | 🔐 | Body: `{"accountId":<integer>}`. Remove assignment; returns 204. |

---
## SAML applications (downstream service providers)

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/saml-applications` | 🔓 | List all registered SPs. |
| GET | `/api/prohibitorum/saml-applications/{id}` | 🔓 | Get one SP by numeric ID. |
| POST | `/api/prohibitorum/saml-applications` | 🔓 | Register a new SP. Accepts optional raw SAML metadata XML in `metadataXml` for ACS + cert ingestion (same path as `saml-sp create --metadata-file`). |
| PUT | `/api/prohibitorum/saml-applications/{id}` | 🔓 | Update SP config (display name, attribute map, session lifetime, etc). |
| POST | `/api/prohibitorum/saml-applications/{id}/reingest-metadata` | 🔓 | Re-parse fresh SAML metadata XML for an existing SP (updates ACS endpoints + signing certs). |
| POST | `/api/prohibitorum/saml-applications/delete` | 🔓 | Body: `{"id": <int>}`. Hard-deletes the SP row and child rows (`saml_sp_acs`, `saml_sp_key`). |
| POST | `/api/prohibitorum/saml-applications/set-disabled` | 🔓 | Body: `{"id":<integer>,"disabled":<boolean>}`. Reversibly disables or enables an SP. |
| GET | `/api/prohibitorum/saml-applications/{id}/managers` | 🔓 | List manager assignments. |
| POST | `/api/prohibitorum/saml-applications/{id}/managers` | 🔐 | Body: `{"accountId":<integer>}`. Assign an enabled `app_manager`; returns 204. |
| POST | `/api/prohibitorum/saml-applications/{id}/managers/remove` | 🔐 | Body: `{"accountId":<integer>}`. Remove assignment; returns 204. |

---

## Application-manager assignments

Only an admin can create or remove an assignment, and both operations require fresh sudo. The assignment target must be enabled and currently have `role: "app_manager"`; otherwise the API returns `invalid_manager_role` (400). Reads are admin-only. Every manager list returns:

```json
[
  {
    "id": 42,
    "username": "alex",
    "displayName": "Alex Example",
    "disabled": false,
    "assignedAt": "2026-07-26T12:00:00Z"
  }
]
```

Assignments are management authority only. They do not grant the manager any OIDC, forward-auth, SAML, launchpad, or PAT eligibility. Changing an account away from `app_manager` removes its assignments transactionally; disablement and deletion take effect immediately through the live session/account checks.

---

## App-bound access workspace

The OIDC, SAML (manual or metadata), and forward-auth creation endpoints accept optional boolean `accessRestricted`. Omitted or `false` preserves unrestricted creation; `true` creates the app with no access until its policy grants access. The flag is included in the create response and committed with the app. Forward-auth creation commits its backing client, proxy configuration, scopes and access policy in one transaction.

There are no reusable global groups or direct per-account access grants. A policy group belongs to one immutable application binding:

- an OIDC or forward-auth group binds to its backing OIDC client;
- a SAML group binds to its SP;
- an app has zero or one `manual` group and zero or more `rule` groups;
- group slugs are unique only within their owning app;
- group kind and app binding cannot be changed. To move or change kind, delete and recreate the group.

`manual` groups hold per-account `allow` / `deny` decisions; absent is neutral. `rule` groups have no manual members or decisions. When an app is restricted, the decision is **manual deny → deny; manual allow → allow; neutral plus any matching rule → allow; otherwise → deny**. An unrestricted app is open. Rules have no priority: all matching rules are ORed, and every matching exposed rule is retained for claims.

### Delegated route base and authorization

Let `BASE` be:

```text
/api/prohibitorum/managed-applications/{kind}/{appId}
```

`kind` is exactly `oidc`, `forward_auth`, or `saml`. `appId` is a percent-encoded OIDC client ID for `oidc` and `forward_auth`, or a positive decimal SP ID for `saml`. `manager` routes first require an active `app_manager`/`admin` session; an `app_manager` then needs an assignment for the exact app and kind. Unassigned, wrong-kind, nonexistent, and deleted apps deliberately return the same 404 `client_not_found` response to a manager. Admins can use the same workspace for any app but still use the separate admin routes for protocol configuration and assignments.

All delegated mutations require JSON, are limited to 64 KiB, and do **not** require fresh sudo.

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/managed-applications` | `manager` | List the caller's assigned apps; an admin receives all app summaries. |
| GET | `BASE/access` | `manager` | Return the complete policy workspace. |
| POST | `BASE/access/set-restricted` | `manager` | Body `{"restricted":<boolean>}`; returns the updated app summary. |
| GET | `BASE/groups` | `manager` | List groups bound to this app. |
| POST | `BASE/groups` | `manager` | Create a manual or rule group; returns 201 with the group. |
| GET | `BASE/groups/{groupId}` | `manager` | Get one app-bound group. |
| PUT | `BASE/groups/{groupId}` | `manager` | Update mutable group fields; returns the group. |
| POST | `BASE/groups/{groupId}/delete` | `manager` | Delete the group; returns 204. |
| GET | `BASE/groups/{groupId}/decisions` | `manager` | Page manual decisions; manual group only. |
| POST | `BASE/groups/{groupId}/decisions` | `manager` | Upsert a manual allow or deny; returns the decision. |
| POST | `BASE/groups/{groupId}/decisions/clear` | `manager` | Clear one manual decision; returns 204. |
| GET | `BASE/groups/{groupId}/preview` | `manager` | Page calculated matches; rule group only. |
| GET | `BASE/groups/{groupId}/explain/{accountId}` | `manager` | Return a safe rule explanation for one active account; rule group only. |
| GET | `BASE/accounts` | `manager` | Page active account summaries for manual-decision selection. |

### Workspace and group wire shapes

`GET BASE/access` returns:

```json
{
  "app": {
    "kind": "oidc",
    "appId": "grafana",
    "displayName": "Grafana",
    "accessRestricted": true
  },
  "accessRestricted": true,
  "providers": [{"slug": "corporate-oidc"}],
  "manualGroup": {
    "id": 7,
    "kind": "manual",
    "slug": "exceptions",
    "displayName": "Exceptions",
    "exposedToDownstream": true
  },
  "ruleGroups": [
    {
      "id": 8,
      "kind": "rule",
      "slug": "passkey-users",
      "displayName": "Passkey users",
      "exposedToDownstream": true,
      "rule": {
        "version": 1,
        "condition": {"fact": "login_method", "method": "passkey"}
      }
    }
  ]
}
```

`app` may additionally contain `launchUrl` and `redirectUris` for OIDC, `entityId` for SAML, or `forwardAuthHost` and `forwardAuthScopes` for forward-auth. `manualGroup` is omitted when no manual group exists; `ruleGroups` and `providers` are arrays. A group object always has `id`, `kind`, `slug`, `displayName`, and `exposedToDownstream`; `description` is optional and `rule` exists only for `kind: "rule"`.

Create with:

```json
{
  "kind": "rule",
  "slug": "passkey-users",
  "displayName": "Passkey users",
  "description": "Optional",
  "exposedToDownstream": true,
  "rule": {
    "version": 1,
    "condition": {"fact": "login_method", "method": "passkey"}
  }
}
```

`kind` is `manual` or `rule`; a manual create must omit `rule`, while a rule create requires it. `exposedToDownstream` defaults to `true`. The slug must match `^[a-z0-9](-?[a-z0-9])*$` and be at most 64 characters. Only one manual group can be created per app.

`PUT BASE/groups/{groupId}` cannot change group kind or application binding; clients should omit both. It requires a non-empty `slug` and `displayName`; `description` is a replacement value (an omitted/empty value clears it), `exposedToDownstream` is optional and otherwise retained, and a rule group retains its current rule unless a replacement `rule` is supplied. A manual group must not receive a rule.

Rules are a version-1 closed AST. `all` / `any` use non-empty `children`; `not` has exactly one `child`; leaf facts are:

- `{"fact":"connection.provider","provider":"<known provider slug>"}`
- `{"fact":"connection.protocol","protocol":"oidc"|"steam"|"vrchat"}`
- `{"fact":"login_method","method":"passkey"|"password_totp"|"federation"}`
- `{"fact":"avatar","source":"any"|"user_uploaded"}`

Unknown fields and values are rejected. Maximums are 8 levels of nesting, 64 total nodes, and 32 children per combinator. The workspace's `providers` list is the authoring catalog; it includes known provider slugs so existing verified-connection facts remain expressible even if a provider is disabled or invite-only.

Manual decision mutation body:

```json
{"accountId": 42, "effect": "allow"}
```

`effect` is `allow` or `deny`; the response is `{"account":{"id":42,"username":"alex","displayName":"Alex Example"},"effect":"allow","updatedAt":"..."}`. Clear accepts `{"accountId":42}`. Manual decisions, account search, and rule preview are paginated as `{"items":[...],"nextCursor":"opaque-or-empty"}` with optional `limit` (default 50, clamped to 100) and `cursor`. Preview items use `{account, matched}`; explanations use `{account, explanation}` where an explanation contains only `path`, `label`, `result`, and nested `children`, never raw identity facts.

Stable policy errors are `manual_group_exists` (409), `group_slug_conflict` (409), `group_not_found` (404), and `invalid_group_rule` (400). The latter supplies only `{"path":"...","reason":"..."}` in its error details. Normal API errors use the public envelope `{code, details?, requestId}`.

### Live facts, claims, and protocol enforcement

Rules evaluate current verified facts, not materialized membership: confirmed provider connections, usable passkeys, password-plus-confirmed-TOTP, qualifying federation identities, and usable avatars (any or user-uploaded). A disabled account fails before policy evaluation. Connection, credential, provider-state, or avatar changes affect the next request.

The same app policy is enforced at OIDC authorization, authorization-code exchange, refresh, and userinfo; forward-auth cookie and PAT verification; SAML SP-/IdP-initiated SSO; launchpad enumeration; and PAT app enumeration. An authorization code must be redeemed by its issuing client with the same redirect URI and PKCE verifier. A refresh token can only be used by the client that owns its family. Token exchange re-evaluates live policy; policy denial returns OAuth `invalid_grant`. Refresh denial first revokes the entire family and then returns `invalid_grant`. Userinfo re-evaluates the app identified by the access token's `client_id` and returns bearer `invalid_token` if it is now denied.

When an allowed OIDC client grants `groups`, ID token and userinfo emit the sorted, deduplicated exposed slugs for that client only. A manual allow includes its exposed manual slug and every matching exposed rule slug; rule-based access includes every matching exposed rule slug. SAML emits the same app-local set only through an attribute-map `groups` source. Forward-auth emits it as `Remote-Groups`. A manual deny emits no credential or assertion, so it has no claims.

OIDC interactive denial redirects to the IdP error page, while `prompt=none` returns protocol-native `access_denied`. SAML interactive denial uses the IdP error page and passive denial returns `Responder` / `RequestDenied`. Forward-auth returns its normal denial response and does not issue a downstream session.

### CLI mapping

The CLI has no global group or grant commands. For each of `oidc-client --client-id`, `forward-auth-app --client-id`, and `saml-sp --entity-id`, use:

- `manager list|assign|remove --username <name>` for scoped manager assignments;
- `access set-restricted --restricted=true|false`;
- `group list|create-manual|create-rule|update|delete|preview` (rule creation/update uses `--rule-file`);
- `decision list|set --username <name> --effect allow|deny|clear`.

### Destructive migration

The cutover deletes legacy global-group membership and direct application-access data and resets every existing application to unrestricted. It does not migrate assignments or policy decisions, and it exposes no compatibility aliases. Recreate the desired app-bound policy explicitly after migration.

---

## Identity providers (upstream OIDC, Steam, and VRChat)

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/identity-providers` | 🔓 | List providers with protocol, readiness, secret status, operator support, and searchable fields. Sealed secrets are never returned. |
| GET | `/api/prohibitorum/identity-providers/{slug}` | 🔓 | Get one provider. Same no-secret guarantee. |
| POST | `/api/prohibitorum/identity-providers` | 🔐 | Create a provider with `protocol` (`oidc`, `steam`, or `vrchat`), provider-specific `config`, optional sealed material, and a mode. VRChat accepts only fixed `link_only`; OIDC/Steam retain their supported modes. |
| PUT | `/api/prohibitorum/identity-providers/{slug}` | 🔐 | Replace mutable display name, mode, and provider-specific config without replacing sealed material. A VRChat update must remain `link_only`. |
| POST | `/api/prohibitorum/identity-providers/rotate-secret` | 🔐 | Replace and seal OIDC/Steam secret material, including a secret stored for later use by a public OIDC client. |
| POST | `/api/prohibitorum/identity-providers/set-disabled` | 🔓 | Reversibly hide a provider from sign-in and block all new flows. |
| POST | `/api/prohibitorum/identity-providers/delete` | 🔐 | Hard-delete a provider and its linked `account_identity` rows. |
| POST | `/api/prohibitorum/identity-providers/{slug}/operator-session/start` | 🔐 | VRChat only. Transient Basic-auth login; returns a bounded 2FA challenge when required. Credentials are not retained. |
| POST | `/api/prohibitorum/identity-providers/{slug}/operator-session/verify` | 🔐 | VRChat only. Verify the challenge with an allowlisted 2FA method/code, seal the resulting cookie jar, and mark the provider ready. |
| POST | `/api/prohibitorum/identity-providers/{slug}/operator-session/validate` | 🔐 | VRChat only. Validate the sealed operator session without accepting credentials. |

Public federation flows retain the protocol-neutral entry points. VRChat uses
the browser-bound flow API as profile proof, not OAuth/OIDC or direct sign-in:

| Method | Path | Notes |
|--------|------|-------|
| GET | `/api/prohibitorum/auth/federation` | List enabled, ready entry providers. A listed VRChat provider starts proof-backed local enrollment, not direct sign-in. |
| GET | `/api/prohibitorum/auth/federation/{slug}/login` | Redirect externally for OIDC/Steam or to the local VRChat proof UI. |
| GET | `/api/prohibitorum/auth/federation/{slug}/callback` | Complete external OIDC/Steam callbacks. |
| GET | `/api/prohibitorum/auth/federation/flows/{flow}` | Return the browser-safe local step projection. |
| POST | `/api/prohibitorum/auth/federation/flows/{flow}/prepare` | Submit the requested VRChat profile identity and obtain a fresh proof instruction. |
| POST | `/api/prohibitorum/auth/federation/flows/{flow}/verify` | Verify profile ownership. Public proof returns an opaque registration/recovery enrollment destination and sets no normal session cookie; authenticated linking returns `/connected` without replacing the current session. |
| GET | `/verify/vrchat/{proof}` | Public ownership-proof explanation page; visiting it performs no account action. |
| GET | `/api/prohibitorum/enrollments/{token}` | Public-safe enrollment preview. See the shapes below. |
| POST | `/api/prohibitorum/enrollments/{token}/register/begin` | Begin the authoritative local WebAuthn registration or replacement ceremony. |
| POST | `/api/prohibitorum/enrollments/{token}/register/complete` | Atomically create/register or replace credentials. This is the first point that may issue a session for a proof-backed flow. |

The public preview for a new VRChat-backed account is
`{"intent":"federated_register","expiresAt":"<timestamp>","suggestedDisplayName":"<safe suggestion>"}`.
A provider-backed recovery preview is
`{"intent":"reset","expiresAt":"<timestamp>"}` and deliberately omits
`target`. Neither shape exposes the provider subject, target account, proof
material, operator session, or internal enrollment snapshot. Proof and
enrollment tokens are opaque bearer values and must not be logged or treated
as API-readable state.

`GET /api/prohibitorum/accounts` accepts debounced unified `search` plus
advanced `provider`, `field`, `value`, and `match`
(`exact`/`prefix`/`contains`) query parameters. Provider descriptors constrain
valid fields/operators, and filtering occurs before cursor pagination.

Admin account responses (list, detail, and updates) include `oidcSubject`, the
canonical UUID sent as `sub` in OIDC ID tokens and UserInfo. PostgreSQL generates
it once with `gen_random_uuid()` when the account is created. It is distinct
from numeric `id`, is read-only, and stays unchanged when profile fields change.
The admin account details page displays it with a copy button.

---

## Signing keys

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/signing-keys` | 🔓 | List all signing keys. Returns `kid`, `status`, `use`, `algorithm`, `publicJwk`, `x509CertPem`, timestamps. Sealed private key **never** returned. |
| POST | `/api/prohibitorum/signing-keys/generate` | 🔐 | Mint a new RSA-2048 signing key (RFC 7638 thumbprint `kid`, JWK, self-signed x509). Enters `status=pending`; immediately published in JWKS + SAML metadata. Prior active key continues signing until `activate` is called. |
| POST | `/api/prohibitorum/signing-keys/{kid}/activate` | 🔐 | Promote a `pending` key to `active`. In one transaction: prior `active` → `decommissioning` (sets `retire_after = now() + grace`), target → `active`. New tokens signed by new key; old key stays in JWKS during grace window. Returns 409 if kid not found or not in `pending` state. |
| POST | `/api/prohibitorum/signing-keys/{kid}/retire` | 🔐 | Transition a `decommissioning` key to `decommissioning` with immediate `retire_after`. Returns 409 if called on the `active` key (refuses to remove the only signer). Background reconcile loop promotes `decommissioning` → `retired` once `retire_after` has passed. |

### Signing-key lifecycle states

```
pending ──activate──► active ──activate(new)──► decommissioning ──reconcile──► retired
```

- **pending** — generated, published in JWKS + SAML metadata, NOT signing.
- **active** — the single current signer (partial unique index `one_active_signing_key (use) WHERE status = 'active'`; exactly one per `use` at any time).
- **decommissioning** — retired from signing but still published in JWKS + SAML metadata for verifying tokens signed before cutover. Background loop flips to `retired` once `retire_after < now`.
- **retired** — no longer published; private key is dead weight in DB.

The publish set for `/oauth/jwks` and `/saml/metadata` is `status IN ('pending', 'active', 'decommissioning')`. Signing always uses the single `active` key.

**Key-cache caveat.** The OP signing-key cache is per-process. `Provider.InvalidateKeyCache()` is called by every admin key mutation so the replica processing the mutation picks up the change immediately; in a multi-replica deployment other replicas pick up within the 5-minute cache TTL. The background reconcile loop (decommissioning→retired) does NOT call `InvalidateKeyCache` — harmless lag in the safe direction (a non-signing key lingers in JWKS slightly longer than its `retire_after`).

---

## Audit events

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/audit-events` | 🔓 | Query `credential_event` rows. Filterable by `factor`, `event`, `accountId`, `since`, `until`. Keyset pagination via `cursor` + `limit`. `detail` JSONB passed through verbatim — no secret material (write-site invariant). |

Policy and assignment events are written to `credential_event` for both admins and delegated app managers:

- manager assignment/removal uses `factor: "app_manager"` and events `app_manager_assigned` / `app_manager_removed`;
- restriction changes, group create/update/delete, and manual allow/deny/clear use `factor: "app_policy"` with `access_restricted_set`, `register`, `update`, `revoke`, `access_granted`, `access_denied`, or `access_revoked` as applicable;
- live protocol denial uses `factor: "oidc_client"` or `factor: "saml_sp"` and `event: "access_denied"`.

Policy records identify the actor, app kind/ID, group ID, action, and target account when applicable. They omit rule JSON, evaluated facts, identity metadata, credentials, secrets, hashes, and private keys. Other administrative factors retain their existing audit event semantics.

---

## Account credentials (admin view)

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/accounts/{id}/credentials` | 🔓 | List WebAuthn credentials for any account. Returns `id`, `credentialIdSuffix` (last 4 characters only), `nickname`, `lastUsedAt`, `cloneWarningAt`, `createdAt`. |
| POST | `/api/prohibitorum/accounts/credentials/delete` | 🔐 | Body: `{"accountId": <int>, "credentialId": <int>}`. Admin force-revokes a passkey. |

---

## Application launchpad (self-service)

`GET /api/prohibitorum/me/apps` returns the signed-in account's authorized, enabled,
launchable apps. Entries contain `kind`, `id`, `name`, `launchUrl`, and optional
`iconUrl` / `accentColor`. OIDC entries also contain `requireConsent` (including
explicit `false`); other protocols omit it.

An OIDC app with `requireConsent: false` appears on the home page without a saved
consent grant and has no revoke action there, including when an older grant still
exists. Apps requiring consent keep the existing connect/revoke behavior. This
display policy does not bypass the server's app access controls.

## Personal access tokens (self-service)

Self-service PAT management routes. These are **not** admin-gated — any enrolled user may call them on their own account.

Gate notation for this section:
- 🔓 = active user session (no admin role required)
- 🔐 = active user session + **fresh sudo grant** (same TTL as the admin sudo grant; prevents dormant-session minting)

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/me/tokens` | 🔓 | List the calling user's PATs. Each row (`PersonalAccessTokenView`): `id`, `name`, `tokenHint` (non-secret display aid = token prefix + last 4 chars, e.g. `prohibitorum_pat_…a1b2`), `allApps` (bool), `appGrants` (object: clientId → `[scopes]`), `createdAt`, `expiresAt` (omitted when no expiry), `lastUsedAt` (omitted until first use). The raw token secret is **never returned** here. |
| POST | `/api/prohibitorum/me/tokens` | 🔐 | Create a new PAT. Body: `{name, expiresInDays?, allApps, appGrants}`. `name` is required (1–128 chars). `expiresInDays` is an **integer number of days** (not a timestamp): omitted or `0` = no expiry; valid range 1–3650; a negative value or one above 3650 is rejected (`bad_request`). `allApps` (bool): `true` = token accepted at every forward-auth app the owner can reach; `appGrants` must be empty when `allApps: true`. `appGrants` (object: clientId → `[scopes]`): when `allApps: false`, must specify at least one app; each app must be in the caller's authorized forward-auth app set and each scope must be in that app's declared scope vocabulary — mismatches are rejected (`bad_request`). Generates a cryptographically random token; the response is `{token, pat}` where `token` is the plaintext, revealed **once only** — only the hash is persisted. |
| POST | `/api/prohibitorum/me/tokens/revoke` | 🔓 | Body: `{"id": <int>}`. Revokes the specified PAT. The caller must own the token; revoking another user's token returns 404. |
| GET | `/api/prohibitorum/me/forward-auth-apps` | 🔓 | List the calling user's currently allowed forward-auth apps and their scope vocabulary. Each entry: `clientId`, `displayName`, `scopes: [{name, description}]`. The live app-bound policy, not manager assignment, determines this list. |

`Remote-Scopes` at the verify endpoint carries only the scopes the PAT granted to the **specific app** being accessed (per-app isolation). `allApps` PATs emit an empty `Remote-Scopes`. The gateway does not interpret scope labels — the upstream service enforces them.

---

## Personal access tokens (admin oversight)

Admin routes for inspecting and revoking any user's PATs. Gate notation follows the global conventions at the top of this file.

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/accounts/{id}/tokens` | 🔓 | List all PATs belonging to account `{id}`. Returns the same `PersonalAccessTokenView` shape as `GET /me/tokens` (`id`, `name`, `tokenHint`, `allApps`, `appGrants`, `createdAt`, `expiresAt?`, `lastUsedAt?`). Raw token secret is **never returned**. |
| POST | `/api/prohibitorum/accounts/tokens/revoke` | 🔐 | Body: `{"id": <int>}`. Admin force-revoke of any PAT by its numeric ID. Requires a fresh sudo grant. Returns 404 if the token does not exist. |

---

## Forward-auth verify endpoint

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/forward-auth/verify` | — | Traefik ForwardAuth target. See below for response semantics. |

**Browser (cookie) flow** — no `X-Prohibitorum-PAT` header present:
- `200` + `Remote-*` identity headers: valid forward-auth cookie + live access check passed.
- `302` to login: no valid cookie — browser is redirected into the Prohibitorum OIDC login flow.
- `403`: `X-Forwarded-Host` is not a registered forward-auth service.

**PAT (API) flow** — `X-Prohibitorum-PAT: <token>` header present (raw token, no Bearer prefix). Terminal: never redirects.
- `200` + `Remote-*` identity headers (including `Remote-Scopes`): valid PAT, owner is active and authorized.
- `401`: token is invalid, expired, or revoked; or the owning account is disabled.
- `403`: valid token, but the owner is not authorized for this application by the live app-bound policy or the PAT's app restriction.

The PAT path takes precedence: if an `X-Prohibitorum-PAT` header is present the request is always handled as a PAT regardless of any cookie. Empty or repeated PAT headers return 401. `Authorization` is ignored by this verifier and remains available to the protected application. Existing PAT clients must switch headers; proxies must strip `X-Prohibitorum-PAT` after verification and preserve `Authorization`.


## Sudo grace period

A successful full sign-in or sudo verification permits repeated sensitive actions
for `PROHIBITORUM_AUTH_SUDO_TTL` (default `30m`; explicit deployment settings
remain authoritative). Reading the status does not extend that window.

`GET /api/prohibitorum/me/sudo/methods` requires a session and returns
`{methods: [...], fresh: boolean}` with `Cache-Control: no-store`. The dashboard
uses `fresh` before redirecting to identity linking, so it skips the modal while
a grant is still valid. Every protected endpoint continues to check the grant
server-side; the status response is not a credential and is not cached as one.

### Upstream OIDC endpoint configuration

OIDC provider config requires the existing issuer/client/scopes/claim and network settings, plus:

| Field | Values and behavior |
|---|---|
| configurationMode | discovery reads issuer metadata; manual never requests discovery. |
| endpoints | Object with authorization, token, userinfo and jwks, each an absolute URL or null. In discovery mode null uses metadata and a URL overrides it. Manual requires authorization/token/jwks; null userinfo skips that request. Empty strings are rejected. |
| tokenAuthMethod | discovery, client_secret_basic, client_secret_post or none. discovery is only valid with discovery mode; it selects basic before post, defaults to basic when metadata omits supported methods, and rejects unsupported-only metadata. |
| pkceMethod | S256, plain or off. Public clients (none) require S256. Off omits the challenge, method and verifier; nonce and state remain. |

Public clients can be created without a secret. A stored secret is retained when switching to none, but is neither decrypted nor sent. Rotation can store a secret before switching back to a confidential method. An enabled confidential provider requires a configured secret. Secret material is never returned by reads. Invalid, unknown or duplicate config fields are rejected with bad_request.

Migration 038 fills existing OIDC configs with discovery mode, null endpoint overrides, discovery authentication and S256. **Authentication no longer retries with another method after a token endpoint failure.** Providers with inaccurate metadata must explicitly select basic or post. Login flows retain their resolved endpoint snapshot; changing provider configuration or rotating the secret rejects older flows before exchanging their codes.
