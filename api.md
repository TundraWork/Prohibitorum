# Prohibitorum — HTTP API reference

This file documents routes registered via `registerOpHTTP` /
`registerSudoOpHTTP` in `pkg/server/operations.go`. Those wrappers use
raw chi handlers rather than Huma's typed I/O (they need `Set-Cookie`
writes and direct streaming control), so OpenAPI does not cover them.

Route-to-source cross-reference:
- `pkg/server/operations.go` — `registerOpHTTP`, `registerSudoOpHTTP`, `withFreshSudo`
- `pkg/server/server.go` — `registerOperations()` mounts every route

**Gate notation:**
- 🔓 = admin session required (`account.role = 'admin'`)
- 🔐 = admin session + **fresh sudo grant** (one-shot; consumed per call)
- `registerSudoOpHTTP` centralises the triple gate (admin auth + content-type check + body-size limit 64 KiB + fresh-sudo verify-and-consume) so it cannot drift per-handler.

**Sudo-gating model.** Reads and cosmetic edits are 🔓 only. Every
mutation that releases a secret, changes trust configuration, or
destroys credentials is 🔐. This is enforced centrally: all 🔐 routes
are registered via `s.registerSudoOpHTTP`, and a route-policy test
serves the real `registerOperations()` route table and asserts that
each mutation returns `sudo_required` (HTTP 401) when the session
carries no fresh sudo grant.

All admin routes share the `/api/prohibitorum` prefix. The admin HTTP API uses
role-oriented resource names (`oidc-applications`, `saml-applications`,
`identity-providers`); the equivalent CLI verbs keep their protocol-oriented
names (`oidc-client`, `saml-sp`, `upstream-idp`).

---

## OIDC applications (downstream relying parties)

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/oidc-applications` | 🔓 | List all clients. `client_secret_hash` is never present in the response. |
| GET | `/api/prohibitorum/oidc-applications/{clientId}` | 🔓 | Get one client. Same no-secret guarantee. |
| POST | `/api/prohibitorum/oidc-applications` | 🔐 | Create a client. Confidential clients (`public: false`): generates a 32-byte `crypto/rand` secret, returns it in `secret` **once only** — not stored plaintext, only the argon2id hash is persisted. Public clients return no secret. |
| PUT | `/api/prohibitorum/oidc-applications/{clientId}` | 🔐 | Full replacement of mutable config fields (display name, redirect URIs, scopes, etc). Does not touch the client secret. |
| POST | `/api/prohibitorum/oidc-applications/rotate-secret` | 🔐 | Body: `{"clientId": "..."}`. Generates and stores a new secret; returns the new cleartext in `secret` **once only**. The new secret is guaranteed ≠ the previous one. |
| POST | `/api/prohibitorum/oidc-applications/delete` | 🔐 | Body: `{"clientId": "..."}`. Hard-deletes the client row. |

---

## SAML applications (downstream service providers)

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/saml-applications` | 🔓 | List all registered SPs. |
| GET | `/api/prohibitorum/saml-applications/{id}` | 🔓 | Get one SP by numeric ID. |
| POST | `/api/prohibitorum/saml-applications` | 🔐 | Register a new SP. Optionally accepts raw SAML metadata XML in `metadataXml` for ACS + cert ingestion (same path as the `saml-sp create --metadata-file` CLI). |
| PUT | `/api/prohibitorum/saml-applications/{id}` | 🔐 | Update SP config (display name, attribute map, session lifetime, etc). |
| POST | `/api/prohibitorum/saml-applications/{id}/reingest-metadata` | 🔐 | Re-parse fresh SAML metadata XML for an existing SP (updates ACS endpoints + signing certs). |
| POST | `/api/prohibitorum/saml-applications/delete` | 🔐 | Body: `{"id": <int>}`. Hard-deletes the SP row and child rows (`saml_sp_acs`, `saml_sp_key`). |

---

## Groups (RBAC)

First-class user groups. Membership gates per-app sign-in (see *Per-app access* below); a group flagged `exposedToDownstream` additionally flows to apps that opt in (OIDC `groups` scope / SAML `groups` attribute source).

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/groups` | 🔓 | List groups with member counts. |
| GET | `/api/prohibitorum/groups/{id}` | 🔓 | Get one group. |
| GET | `/api/prohibitorum/groups/{id}/members` | 🔓 | List a group's members (`id`, `username`, `displayName`). |
| GET | `/api/prohibitorum/accounts/{id}/groups` | 🔓 | List the groups an account belongs to (also editable from the admin account-detail page). |
| POST | `/api/prohibitorum/groups` | 🔐 | Create a group. Body `{slug, displayName, description?, exposedToDownstream?}` (`exposedToDownstream` defaults `true`). `slug` must match `^[a-z0-9](-?[a-z0-9])*$` — invalid → 400, duplicate → 409. |
| PUT | `/api/prohibitorum/groups/{id}` | 🔐 | Update display name / description / `exposedToDownstream` / slug. Renaming the slug changes the value RPs receive in the `groups` claim/attribute (the admin UI warns on slug change). |
| POST | `/api/prohibitorum/groups/delete` | 🔐 | Body: `{"id": <int>}`. Deletes the group; `ON DELETE CASCADE` removes its memberships and access grants. |
| POST | `/api/prohibitorum/groups/{id}/members` | 🔐 | Body: `{"accountId": <int>}`. Add an account to the group (idempotent). |
| POST | `/api/prohibitorum/groups/{id}/members/remove` | 🔐 | Body: `{"accountId": <int>}`. Remove an account; 0 rows affected → 404. |

---

## Per-app access (RBAC)

A coarse per-app access gate layered on top of the "RP enforces policy" model. An app (OIDC client or SAML SP) with `access_restricted = true` admits only users with a **direct grant** or a grant to a **group they belong to**; while `false` (the default — existing apps are untouched) any enrolled user may sign in. There is **no admin bypass** — admins are assigned like anyone else.

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/oidc-applications/{clientId}/access` | 🔓 | `{accessRestricted, groups:[{id,slug,displayName}], accounts:[{id,username,displayName}]}`. |
| POST | `/api/prohibitorum/oidc-applications/{clientId}/access/set-restricted` | 🔐 | Body: `{"restricted": <bool>}`. Toggles the gate (mirrors the `set-disabled` split — not part of the config-form PUT). |
| POST | `/api/prohibitorum/oidc-applications/{clientId}/access/grant` | 🔐 | Body: `{"principalKind": "group"\|"account", "principalId": <int>}`. |
| POST | `/api/prohibitorum/oidc-applications/{clientId}/access/revoke` | 🔐 | Same body; 0 rows affected → 404. |
| GET | `/api/prohibitorum/saml-applications/{id}/access` | 🔓 | Same shape, keyed by numeric SP id. |
| POST | `/api/prohibitorum/saml-applications/{id}/access/set-restricted` | 🔐 | Body: `{"restricted": <bool>}`. |
| POST | `/api/prohibitorum/saml-applications/{id}/access/grant` | 🔐 | Body: `{"principalKind": "group"\|"account", "principalId": <int>}`. |
| POST | `/api/prohibitorum/saml-applications/{id}/access/revoke` | 🔐 | Same body; 0 rows affected → 404. |

**Enforcement.** The gate runs after the session is validated and `account.disabled` is enforced, before anything is issued: at OIDC `/oauth/authorize` (and **re-checked at the refresh-token grant** — de-provisioning cuts existing sessions within the access-token TTL, revoking the refresh family) and at SAML SSO (both SP-initiated and IdP-initiated). A denied **interactive** user is redirected to the IdP's own `/error?reason=app_access_denied&app=<name>` page; an OIDC `prompt=none` request gets a protocol-native `access_denied` at the `redirect_uri`; a SAML passive (`IsPassive`) request gets a `Responder` / `RequestDenied` status Response. Every denial writes an `access_denied` audit event.

**Group exposure to downstreams** (two-level opt-in — nothing leaves the IdP unless both are true): the group has `exposedToDownstream = true`, **and** the app asks for it — an OIDC client whose `allowed_scopes` include `groups` (emits a sorted `groups` claim in the id_token + `/userinfo`, present-but-empty `[]`), or a SAML SP whose attribute map has a `source: "groups"` entry (emits the exposed slugs, multi-valued; omitted when empty).

---

## Identity providers (upstream OIDC federation)

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/identity-providers` | 🔓 | List all upstream IdPs. Client secret is **write-only**: never returned (enforced at the contract level). |
| GET | `/api/prohibitorum/identity-providers/{slug}` | 🔓 | Get one upstream IdP by slug. Same no-secret guarantee. |
| POST | `/api/prohibitorum/identity-providers` | 🔐 | Create an upstream IdP including its client secret. The secret is AES-GCM sealed after insert; AAD binds to the row `id`. **Known caveat:** a crash between insert and seal leaves a row with a placeholder secret that decrypts to a failure (fails closed; best-effort cleanup). |
| PUT | `/api/prohibitorum/identity-providers/{slug}` | 🔐 | Update mutable IdP config. Explicitly **excludes** the client secret — use rotate-secret to change it. |
| POST | `/api/prohibitorum/identity-providers/rotate-secret` | 🔐 | Body: `{"slug": "...", "newSecret": "..."}`. Re-seals the client secret under the active DEK version. |
| POST | `/api/prohibitorum/identity-providers/delete` | 🔐 | Body: `{"slug": "..."}`. Hard-deletes the upstream IdP row and all `account_identity` rows linked to it. |

---

## Signing keys

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/signing-keys` | 🔓 | List all signing keys. Returns `kid`, `status`, `use`, `algorithm`, `publicJwk`, `x509CertPem`, timestamps. The sealed private key is **never** returned (enforced at the contract level). |
| POST | `/api/prohibitorum/signing-keys/generate` | 🔐 | Mint a new RSA-2048 signing key (RFC 7638 thumbprint `kid`, JWK, self-signed x509). The new key enters `status=pending` and is immediately published in JWKS + SAML metadata. The prior active key continues to sign until `activate` is called. |
| POST | `/api/prohibitorum/signing-keys/{kid}/activate` | 🔐 | Promote a `pending` key to `active`. In one transaction: the prior `active` key transitions to `decommissioning` (sets `retire_after = now() + grace`), then the target key transitions to `active`. After this call new tokens are signed by the new key; the old key remains in JWKS during the grace window so existing tokens continue to verify. Returns 409 if no key exists with the given kid, or if the key is not in `pending` state. |
| POST | `/api/prohibitorum/signing-keys/{kid}/retire` | 🔐 | Transition a `decommissioning` key directly to `decommissioning` with an immediate `retire_after`. Returns 409 if called on the `active` key (refusing to remove the only signer). The background reconcile loop (`pruneRevokedJTILoop`-style loop in `Server.Serve`) promotes `decommissioning` → `retired` once `retire_after` has passed. |

### Signing-key lifecycle states

The `status` column drives the four-state lifecycle:

```
pending ──activate──► active ──activate(new)──► decommissioning ──reconcile──► retired
```

- **pending** — key generated, published in JWKS + SAML metadata, NOT signing.
- **active** — the single current signer (enforced by partial unique index
  `one_active_signing_key (use) WHERE status = 'active'`). Exactly one
  per `use` value at any time.
- **decommissioning** — retired from signing but still published in JWKS
  and SAML metadata so relying parties can verify tokens signed before
  the cutover. `retire_after` is set to `now() + grace` by the activate
  path. Background loop flips to `retired` once `retire_after < now`.
- **retired** — no longer published; private key is dead weight in DB.

The publish set for JWKS (`/oauth/jwks`) and SAML metadata
(`/saml/metadata`) is `status IN ('pending', 'active', 'decommissioning')`.
Signing always uses the single `active` key.

**Key-cache caveat.** The OP signing-key cache is per-process.
`Provider.InvalidateKeyCache()` is called by every admin key mutation
so the replica processing the mutation picks up the change immediately,
but in a multi-replica deployment other replicas pick up the new or
activated key within the 5-minute cache TTL.
The background reconcile loop (decommissioning→retired) does NOT
call `InvalidateKeyCache` — a harmless lag in the safe direction (an
already-non-signing key lingers in JWKS slightly longer than its
`retire_after`).

---

## Audit events

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/audit-events` | 🔓 | Query `credential_event` rows. Filterable by `factor`, `event`, `accountId`, `since`, `until`. Keyset pagination via `cursor` + `limit`. Passes `detail` JSONB through verbatim — no secret material should appear there (write-site invariant). |

Audit coverage for admin mutations: every 🔐 admin mutation writes a
`credential_event` row with:
- `factor` ∈ `oidc_client`, `saml_sp`, `upstream_idp`, `signing_key`, `group`
- `event` ∈ `register` (create), `update`, `rotate` (secret/key rotation), `revoke` (delete/force-revoke), `link`/`unlink` (group membership add/remove), `access_granted`/`access_revoked`/`access_restricted_set` (per-app access grants on factor `oidc_client`/`saml_sp`)
- `access_denied` (factor `oidc_client`/`saml_sp`) is additionally written at enforcement time — not an admin mutation — when an authenticated user is turned away from a restricted app at OIDC authorize/refresh or SAML SSO
- `account_id` = the admin account performing the action
- `credential_ref` = the target resource ID (client_id, SP ID, slug, kid)
- `detail` = redacted summary (e.g. `{"client_id":"...","display_name":"..."}`) — **no** secret, hash, or private key material

---

## Account credentials (admin view)

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/accounts/{id}/credentials` | 🔓 | List WebAuthn credentials for any account. Returns `id`, `credentialIdSuffix` (last 4 characters only, never the full credential ID), `nickname`, `lastUsedAt`, `cloneWarningAt`, `createdAt`. |
| POST | `/api/prohibitorum/accounts/credentials/delete` | 🔐 | Body: `{"accountId": <int>, "credentialId": <int>}`. Admin force-revokes a passkey (sudo-gated). |
