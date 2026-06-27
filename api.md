# Prohibitorum — HTTP API reference

Routes registered via `registerOpHTTP` / `registerSudoOpHTTP` in `pkg/server/operations.go`. These use raw chi handlers (need `Set-Cookie` writes and direct streaming control), so OpenAPI does not cover them.

Route-to-source cross-reference:
- `pkg/server/operations.go` — `registerOpHTTP`, `registerSudoOpHTTP`, `withFreshSudo`
- `pkg/server/server.go` — `registerOperations()` mounts every route

**Gate notation:**
- 🔓 = admin session required (`account.role = 'admin'`)
- 🔐 = admin session + **fresh sudo grant** (valid for configured `sudo_ttl`, default 15 min; covers multiple gated actions until expiry — not consumed per call)

`registerSudoOpHTTP` centralises the triple gate (admin auth + content-type check + 64 KiB body limit + fresh-sudo check) so it cannot drift per-handler. Reads are 🔓 only. High-impact mutations (secrets, PKI/trust config, credentials, irreversible destructive actions) are 🔐; lower-impact reversible mutations (group membership, access grants, SAML CRUD, session/invitation revoke) are 🔓. A route-policy test asserts that each 🔐 mutation returns `sudo_required` (HTTP 401) without a fresh sudo grant.

All admin routes share the `/api/prohibitorum` prefix. Resource names use role-oriented terms (`oidc-applications`, `saml-applications`, `identity-providers`); CLI verbs use protocol-oriented names (`oidc-client`, `saml-sp`, `upstream-idp`).

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

---

## Groups (RBAC)

First-class user groups. Membership gates per-app sign-in (see *Per-app access*); a group flagged `exposedToDownstream` additionally flows to apps that opt in (OIDC `groups` scope / SAML `groups` attribute source).

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/groups` | 🔓 | List groups with member counts. |
| GET | `/api/prohibitorum/groups/{id}` | 🔓 | Get one group. |
| GET | `/api/prohibitorum/groups/{id}/members` | 🔓 | List a group's members (`id`, `username`, `displayName`). |
| GET | `/api/prohibitorum/accounts/{id}/groups` | 🔓 | List the groups an account belongs to. |
| POST | `/api/prohibitorum/groups` | 🔓 | Create a group. Body `{slug, displayName, description?, exposedToDownstream?}` (`exposedToDownstream` defaults `true`). `slug` must match `^[a-z0-9](-?[a-z0-9])*$` — invalid → 400, duplicate → 409. |
| PUT | `/api/prohibitorum/groups/{id}` | 🔓 | Update display name / description / `exposedToDownstream` / slug. Renaming the slug changes the value RPs receive in the `groups` claim/attribute (the admin UI warns on slug change). |
| POST | `/api/prohibitorum/groups/delete` | 🔓 | Body: `{"id": <int>}`. Deletes the group; `ON DELETE CASCADE` removes memberships and access grants. |
| POST | `/api/prohibitorum/groups/{id}/members` | 🔓 | Body: `{"accountId": <int>}`. Add an account to the group (idempotent). |
| POST | `/api/prohibitorum/groups/{id}/members/remove` | 🔓 | Body: `{"accountId": <int>}`. Remove an account; 0 rows affected → 404. |

---

## Per-app access (RBAC)

A coarse per-app access gate on top of the "RP enforces policy" model. An app with `access_restricted = true` admits only users with a direct grant or a grant to a group they belong to; `false` (default — existing apps untouched) allows any enrolled user. **No admin bypass** — admins are assigned like anyone else.

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/oidc-applications/{clientId}/access` | 🔓 | `{accessRestricted, groups:[{id,slug,displayName}], accounts:[{id,username,displayName}]}`. |
| POST | `/api/prohibitorum/oidc-applications/{clientId}/access/set-restricted` | 🔓 | Body: `{"restricted": <bool>}`. Toggles the gate (not part of the config-form PUT). |
| POST | `/api/prohibitorum/oidc-applications/{clientId}/access/grant` | 🔓 | Body: `{"principalKind": "group"\|"account", "principalId": <int>}`. |
| POST | `/api/prohibitorum/oidc-applications/{clientId}/access/revoke` | 🔓 | Same body; 0 rows affected → 404. |
| GET | `/api/prohibitorum/saml-applications/{id}/access` | 🔓 | Same shape, keyed by numeric SP id. |
| POST | `/api/prohibitorum/saml-applications/{id}/access/set-restricted` | 🔓 | Body: `{"restricted": <bool>}`. |
| POST | `/api/prohibitorum/saml-applications/{id}/access/grant` | 🔓 | Body: `{"principalKind": "group"\|"account", "principalId": <int>}`. |
| POST | `/api/prohibitorum/saml-applications/{id}/access/revoke` | 🔓 | Same body; 0 rows affected → 404. |

**Enforcement.** The gate runs after session validation and `account.disabled` check, before anything is issued: at OIDC `/oauth/authorize` (**re-checked at the refresh-token grant** — de-provisioning cuts existing sessions within the access-token TTL) and at SAML SSO (SP-initiated and IdP-initiated). Denied interactive user → redirect to `/error?reason=app_access_denied&app=<name>`; OIDC `prompt=none` → protocol-native `access_denied` at `redirect_uri`; SAML passive (`IsPassive`) → `Responder`/`RequestDenied` status Response. Every denial writes an `access_denied` audit event.

**Group exposure to downstreams** (two-level opt-in): the group has `exposedToDownstream = true` **and** the app requests it — an OIDC client whose `allowed_scopes` include `groups` (emits a sorted `groups` claim in id_token + `/userinfo`, present-but-empty `[]`), or a SAML SP whose attribute map has a `source: "groups"` entry (emits exposed slugs, multi-valued; omitted when empty).

---

## Identity providers (upstream OIDC federation)

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/identity-providers` | 🔓 | List all upstream IdPs. Client secret **never** returned (enforced at the contract level). |
| GET | `/api/prohibitorum/identity-providers/{slug}` | 🔓 | Get one upstream IdP by slug. Same no-secret guarantee. |
| POST | `/api/prohibitorum/identity-providers` | 🔐 | Create an upstream IdP including its client secret. Secret is AES-GCM sealed after insert; AAD binds to the row `id`. **Known caveat:** a crash between insert and seal leaves a row with a placeholder secret that decrypts to a failure (fails closed; best-effort cleanup). |
| PUT | `/api/prohibitorum/identity-providers/{slug}` | 🔐 | Update mutable IdP config. Explicitly **excludes** the client secret — use rotate-secret to change it. |
| POST | `/api/prohibitorum/identity-providers/rotate-secret` | 🔐 | Body: `{"slug": "...", "newSecret": "..."}`. Re-seals the client secret under the active DEK version. |
| POST | `/api/prohibitorum/identity-providers/delete` | 🔐 | Body: `{"slug": "..."}`. Hard-deletes the upstream IdP row and all `account_identity` rows linked to it. |

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

Every admin mutation (🔐 and 🔓) writes a `credential_event` row:
- `factor` ∈ `oidc_client`, `saml_sp`, `upstream_idp`, `signing_key`, `group`
- `event` ∈ `register` (create), `update`, `rotate` (secret/key rotation), `revoke` (delete/force-revoke), `link`/`unlink` (group membership add/remove), `access_granted`/`access_revoked`/`access_restricted_set` (per-app access grants on factor `oidc_client`/`saml_sp`); `access_denied` (factor `oidc_client`/`saml_sp`) written at enforcement time when an authenticated user is turned away from a restricted app
- `account_id` = the admin account performing the action
- `credential_ref` = the target resource ID (client_id, SP ID, slug, kid)
- `detail` = redacted summary (e.g. `{"client_id":"...","display_name":"..."}`) — **no** secret, hash, or private key material

---

## Account credentials (admin view)

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/accounts/{id}/credentials` | 🔓 | List WebAuthn credentials for any account. Returns `id`, `credentialIdSuffix` (last 4 characters only), `nickname`, `lastUsedAt`, `cloneWarningAt`, `createdAt`. |
| POST | `/api/prohibitorum/accounts/credentials/delete` | 🔐 | Body: `{"accountId": <int>, "credentialId": <int>}`. Admin force-revokes a passkey. |
