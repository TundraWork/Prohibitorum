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
are registered via `s.registerSudoOpHTTP`, and
`TestAdminMutationRoutesRequireSudo`
(`pkg/server/admin_route_policy_test.go`) serves the REAL
`registerOperations()` route table and asserts that each mutation
returns `sudo_required` (HTTP 401) when the session carries no fresh
sudo grant.

All admin routes share the `/api/prohibitorum` prefix. The admin HTTP API uses
role-oriented resource names (`oidc-applications`, `saml-applications`,
`identity-providers`); the equivalent CLI verbs keep their protocol-oriented
names (`oidc-client`, `saml-sp`, `upstream-idp`).

---

## OIDC applications (downstream relying parties)

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/oidc-applications` | 🔓 | List all clients. `client_secret_hash` never present in response (unit-tested: `TestAdminOIDCClients_ViewProjection_NeverExposesSecretHash`). |
| GET | `/api/prohibitorum/oidc-applications/{clientId}` | 🔓 | Get one client. Same no-secret guarantee. |
| POST | `/api/prohibitorum/oidc-applications` | 🔐 | Create a client. Confidential clients (`public: false`): generates a 32-byte `crypto/rand` secret, returns it in `secret` **once only** — not stored plaintext, only the argon2id hash is persisted. Public clients return no secret. Smoke step 114. |
| PUT | `/api/prohibitorum/oidc-applications/{clientId}` | 🔐 | Full replacement of mutable config fields (display name, redirect URIs, scopes, etc). Does not touch the client secret. Smoke step 115. |
| POST | `/api/prohibitorum/oidc-applications/rotate-secret` | 🔐 | Body: `{"clientId": "..."}`. Generates and stores a new secret; returns the new cleartext in `secret` **once only**. New secret is guaranteed ≠ previous secret. Smoke step 116. |
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

## Identity providers (upstream OIDC federation)

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/identity-providers` | 🔓 | List all upstream IdPs. Client secret is **write-only**: never returned; `TestAdminUpstreamIDPs_ViewProjection_NeverExposesSecretBytes` + `TestAdminUpstreamIDPs_ContractType_NoSecretFields` enforce this at the contract level. |
| GET | `/api/prohibitorum/identity-providers/{slug}` | 🔓 | Get one upstream IdP by slug. Same no-secret guarantee. |
| POST | `/api/prohibitorum/identity-providers` | 🔐 | Create an upstream IdP including its client secret. The secret is AES-GCM sealed after insert; AAD binds to the row `id`. **Known caveat:** a crash between insert and seal leaves a row with a placeholder secret that decrypts to a failure (fails closed; best-effort cleanup). |
| PUT | `/api/prohibitorum/identity-providers/{slug}` | 🔐 | Update mutable IdP config. Explicitly **excludes** the client secret — use rotate-secret to change it. |
| POST | `/api/prohibitorum/identity-providers/rotate-secret` | 🔐 | Body: `{"slug": "...", "newSecret": "..."}`. Re-seals the client secret under the active DEK version. |
| POST | `/api/prohibitorum/identity-providers/delete` | 🔐 | Body: `{"slug": "..."}`. Hard-deletes the upstream IdP row and all `account_identity` rows linked to it. |

---

## Signing keys

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/signing-keys` | 🔓 | List all signing keys. Returns `kid`, `status`, `use`, `algorithm`, `publicJwk`, `x509CertPem`, timestamps. `private_pem` is **never** returned; `TestAdminSigningKeys_ViewProjection_NeverExposesPrivateMaterial` + `TestAdminSigningKeys_ContractType_NoPrivatePemField` enforce this. |
| POST | `/api/prohibitorum/signing-keys/generate` | 🔐 | Mint a new RSA-2048 signing key (RFC 7638 thumbprint `kid`, JWK, self-signed x509). The new key enters `status=pending` and is immediately published in JWKS + SAML metadata. The prior active key continues to sign until `activate` is called. Smoke step 118. |
| POST | `/api/prohibitorum/signing-keys/{kid}/activate` | 🔐 | Promote a `pending` key to `active`. In one transaction: the prior `active` key transitions to `decommissioning` (sets `retire_after = now() + grace`), then the target key transitions to `active`. After this call new tokens are signed by the new key; the old key remains in JWKS during the grace window so existing tokens continue to verify. Smoke step 119. Returns 409 if no key exists with the given kid, or if the key is not in `pending` state. |
| POST | `/api/prohibitorum/signing-keys/{kid}/retire` | 🔐 | Transition a `decommissioning` key directly to `decommissioning` with an immediate `retire_after`. Returns 409 if called on the `active` key (refusing to remove the only signer). The background reconcile loop (`pruneRevokedJTILoop`-style loop in `Server.Serve`) promotes `decommissioning` → `retired` once `retire_after` has passed. |

### Signing-key lifecycle states

Migration `008_signing_key_lifecycle.sql` introduced an explicit
`status` column with four values, implemented as an expand→cutover→contract
rotation sequence:

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

**Legacy columns** (`active boolean`, `retired_at`) remain in the schema
and are kept consistent by the application; migration `009` (dropping
them) is a deferred follow-up after the `status` column is confirmed in
production — not yet written.

**Key-cache caveat.** The OP signing-key cache is per-process.
`Provider.InvalidateKeyCache()` is called by every admin key mutation
so the replica processing the mutation picks up the change immediately,
but in a multi-replica deployment other replicas pick up the new or
activated key within the 5-minute cache TTL. This is in the same
family as the in-process rate-limiter multi-replica caveat (see
`docs/superpowers/notes/2026-05-28-v0.2-deployment-notes.md` §1).
The background reconcile loop (decommissioning→retired) does NOT
call `InvalidateKeyCache` — a harmless lag in the safe direction (an
already-non-signing key lingers in JWKS slightly longer than its
`retire_after`).

---

## Audit events

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/audit-events` | 🔓 | Query `credential_event` rows. Filterable by `factor`, `event`, `accountId`, `since`, `until`. Keyset pagination via `cursor` + `limit`. Passes `detail` JSONB through verbatim — no secret material should appear there (write-site invariant; smoke step 120 asserts no secret/key bytes in any event returned). |

Audit coverage for admin mutations: every 🔐 admin mutation writes a
`credential_event` row with:
- `factor` ∈ `oidc_client`, `saml_sp`, `upstream_idp`, `signing_key`
- `event` ∈ `register` (create), `update`, `rotate` (secret/key rotation), `revoke` (delete/force-revoke)
- `account_id` = the admin account performing the action
- `credential_ref` = the target resource ID (client_id, SP ID, slug, kid)
- `detail` = redacted summary (e.g. `{"client_id":"...","display_name":"..."}`) — **no** secret, hash, or private key material

---

## Account credentials (admin view)

| Method | Path | Gate | Notes |
|--------|------|------|-------|
| GET | `/api/prohibitorum/accounts/{id}/credentials` | 🔓 | List WebAuthn credentials for any account. Returns `id`, `credentialIdSuffix` (last 4 characters only, never the full credential ID), `nickname`, `lastUsedAt`, `cloneWarningAt`, `createdAt`. Smoke step 121. |
| POST | `/api/prohibitorum/accounts/credentials/delete` | 🔐 | Body: `{"accountId": <int>, "credentialId": <int>}`. Admin force-revokes a passkey. Promoted to sudo-gated in the Admin Management API phase. Smoke step 121 (sudo-gating assertion). |
