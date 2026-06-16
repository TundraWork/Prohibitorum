# Prohibitorum — Architecture

> Index Librorum Prohibitorum. The list of who's allowed and what they can do.

A single-tenant identity provider for a small org. Owns the account
directory, authenticates users via one of four upstream methods
(WebAuthn, Password, TOTP, upstream OIDC federation), and issues
identity assertions to downstream apps via OIDC OP or SAML 2.0 IdP.

## What this is (and isn't)

**Is:**
- A single-tenant IdP for a small org. Owns the account directory,
  runs WebAuthn / password+TOTP ceremonies, can federate identity
  from upstream OIDC providers, and issues sessions plus protocol
  responses.
- A first-party service. No email channel; admin-issued enrollment
  is the only account-recovery path.
- The source of truth for "who is this user, are they disabled, what
  attributes do we know about them?"

**Is not:**
- A multi-tenant SaaS IdP (no per-tenant separation).
- A SAML SP — Prohibitorum does **not** consume upstream SAML
  assertions; only OIDC federation goes upstream.
- A social-login proxy in the consumer sense; "Sign in with Google"
  works only if an admin has registered Google as an upstream IdP and
  configured a provisioning mode.
- An authorization policy engine (no OPA/Rego). Prohibitorum carries
  a free-form `attributes` map per account into ID-token claims and
  SAML AttributeStatement; RPs enforce policy from those claims. The
  RBAC feature (v0.7) adds a *coarse per-app access gate* — whether a
  user may obtain a token/assertion for an app at all — but the RP still
  governs what you can do **inside** the app from claims (including the
  new `groups` claim). The two layers are distinct: IdP gates app
  access; RP gates in-app policy.

## Architecture — three-layer (Approach A)

Industry-convergent layout drawn from Keycloak, Ory Kratos+Hydra,
Authelia, Dex, Zitadel. Three layers, acyclic import graph:

1. **Identity store** — directory + credentials + federation links.
   Facts about users.
2. **Authentication subsystem** — factors + federation. Produces a
   `session`.
3. **Protocol subsystem** — OIDC OP + SAML IdP. Consumes a `session`.

The `session` package is the contract between layers (2) and (3).
Protocols don't know how the user authenticated; factors don't know
what RPs will consume the result.

```
                      ┌──────────────────────────────────┐
                      │             Prohibitorum         │
                      │                                  │
        browser  ────►│  Identity store                  │
                      │   pkg/account                    │
                      │   pkg/credential/{webauthn,      │
                      │     password, totp, pairing,     │
                      │     enrollment}                  │
                      │   pkg/federation/oidc            │
                      │                                  │
                      │  Authentication subsystem        │
                      │   pkg/authn         pkg/session  │
                      │                                  │
                      │  Protocol subsystem              │
       RP ──OIDC────► │   pkg/protocol/oidc              │
       SP ──SAML────► │   pkg/protocol/saml              │
                      │                                  │
                      │  ┌────────┐  ┌────────┐  ┌─────┐ │
                      │  │ pgx    │  │ KV     │  │jose │ │
                      │  │postgres│  │keydb/  │  │RS256│ │
                      │  │        │  │memory  │  │     │ │
                      │  └────────┘  └────────┘  └─────┘ │
                      └──────────────────────────────────┘
```

### Package layout

```
pkg/
  account/                # directory: Account, list, disable, role, attributes
  credential/
    webauthn/             # WebAuthn registration + assertion
    password/             # argon2id PHC hash store + verify (v0.2)
    totp/                 # RFC 6238 + recovery codes; AES-GCM at-rest (v0.2)
    pairing/              # device-pairing code (no bearer-token in URL)
    enrollment/           # invite/reset/add-device/bootstrap tokens
  federation/
    oidc/                 # upstream OIDC RP (v0.3)
  session/                # PG + KV-backed session, middleware
  authn/                  # login orchestrator + sudo + rate limit + middleware
  protocol/
    oidc/                 # downstream OIDC OP (v0.4)
    saml/                 # SAML 2.0 IdP (v0.5), GHES-compatible profile
  server/                 # HTTP wiring, routes mounted from each subsystem
  contract/               # types exposed to dashboard / RPs
  audit/                  # credential_event writer
  kv/  logx/  errorx/  configx/   # utilities
```

## Authentication methods

The four upstream methods, in preference order. Subsections tagged
**(v0.X)** describe the planned behavior for that version; v0.1 ships
the schema and stub packages only for those methods.

### WebAuthn (primary)

ResidentKey=Required (discoverable credentials), UV=Required at
register / Preferred at login. Sign-count regression detection writes
`webauthn_credential.clone_warning_at` so the admin UI can surface
suspected cloned authenticators. COSE algorithm, user handle, and
`uv_initialized` are persisted per credential per WebAuthn L3 §4.

When a user adds a passkey via
`POST /api/prohibitorum/me/credentials/register/{begin,complete}`,
v0.2 will offer to delete the account's password + TOTP + recovery
codes in the same transaction (via `authn.DisableNonWebAuthnFallbacks`,
currently stubbed). Default yes. The decision is captured server-side;
there's no client-side bypass.

### Password + TOTP (fallback, v0.2)

For users without passkey-capable devices. Both factors required;
neither alone produces a session.

- **Password.** argon2id PHC string at rest, salted per row, params
  tunable via `configx.PasswordHashParams`. On successful verify, if
  the stored hash uses parameters below the current configured set,
  re-hash and update. Persistent failed-attempt counter in
  `auth_throttle` (per RFC 4226 §7.3, cross-restart).
- **TOTP.** AES-256-GCM at rest with versioned DEK
  (`PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>`); AAD =
  `'totp:'||account_id||':'||key_version` so ciphertext can't be
  copied between rows. Per RFC 6238: ±1 period drift,
  `totp_credential.last_step` defeats same-step replay (§5.2). SHA1
  default for Google Authenticator interop.
- **Recovery codes.** 10 codes shown once at TOTP enrollment;
  argon2id PHC at rest; single-use; redemption context (session, IP)
  captured for audit (`recovery_code.used_session_id`, `used_ip`).

### Upstream OIDC federation (v0.3)

Per-IdP configuration in `upstream_idp` with three provisioning modes:

- **auto_provision** — create a local account on first sign-in if the
  upstream `email` claim domain is in `allowed_domains` (or
  `allowed_domains` is empty). Link `(upstream_iss, upstream_sub)` to
  the new account.
- **invite_only** — look up a pending enrollment whose
  `expected_upstream_idp_slug` matches this IdP. No matching invite
  → 403.
- **link_only** — never auto-create. User must already have an
  account and a pre-existing link → 403 otherwise, with a hint to
  sign in via another method then link from `/me`.

Upstream client secrets are AES-256-GCM encrypted at rest with the
same versioned DEK scheme and a different AAD prefix
(`'upstream_idp:'||id||':'||key_version`). `account_identity` is
keyed on `(upstream_iss, upstream_sub)` per OIDC Core §2 — admin
re-pointing an IdP's `issuer_url` cannot collide sub spaces.

Federation state stored in KV at request time snapshots
`expected_iss` and `expected_token_endpoint` from the discovery doc;
mid-flight admin edits to `upstream_idp` cannot break mix-up
resistance (RFC 9700 §4.4.2.1).

## Downstream protocols

### OIDC OP (v0.4)

Authorization Code + PKCE only. Implicit / ROPC / Hybrid are not
registered as accepted `response_type`s. Discovery + JWKS endpoints
published. RS256 signing (key store unified with SAML; see
"Cryptography").

- **ID token claims:** `iss`, `sub`, `aud`, `exp`, `iat`, `nonce`,
  `auth_time`, `amr`, `acr`, `azp` (when `aud` is multi-valued or
  differs from authorized party), `at_hash`, plus `username`,
  `displayName`, `role`, and `attributes` carried verbatim from the
  account. With the `groups` scope granted, a sorted `groups` array of
  the user's `exposed_to_downstream` group slugs (present-but-empty `[]`;
  emitted in both the ID token and `/userinfo`).
- **Access token (RFC 9068):** `typ: at+jwt`. Required claims `iss`,
  `sub`, `aud`, `exp`, `iat`, `jti`, `client_id`, `scope`;
  `auth_time` / `amr` / `acr` carried when available.
- **Refresh tokens:** opaque, KV-stored, rotated on use; reuse
  detection revokes the entire family.
- **Authorization codes:** marked `consumed_at` on first use (not
  deleted), kept until TTL. Replay attempts revoke the refresh-token
  family minted from the code and write a `credential_event` with
  `event=fail, factor=oidc_client, reason=code_reuse` (RFC 9700 §4.5,
  §4.14.2).
- **Revocation (RFC 7009):** writes to `revoked_jti` for self-contained
  access tokens; `/oauth/introspect` returns `active: false`.
- **RP-Initiated Logout:** `post_logout_redirect_uri` exact-matched
  against `oidc_client.post_logout_redirect_uris`.

### SAML IdP (v0.5, extended v0.6)

SP-initiated SSO (HTTP-Redirect and HTTP-POST bindings for AuthnRequest;
HTTP-POST binding for the Response) plus IdP-initiated SSO (v0.6,
per-SP opt-in). `crewjam/saml` does the protocol heavy lifting.

- **Assertion construction.** Always sign both `<Response>` and
  `<Assertion>`. `Destination` on `<Response>` = chosen ACS URL.
  `<SubjectConfirmationData Recipient>` = same ACS URL. `<Audience>`
  inside `<AudienceRestriction>` = `saml_sp.entity_id` verbatim.
- **NameID stability** via `saml_subject_id(account_id, sp_id)`: 32-byte
  random opaque value generated on first SSO, reused forever (Core
  §8.3.7). Defeats GHES account re-linking on rename / email change.
- **ACS lookup precedence** (Profiles §4.1.4.1): explicit
  `AssertionConsumerServiceURL` in AuthnRequest (must match a
  `saml_sp_acs` row exactly) → `AssertionConsumerServiceIndex` →
  `is_default=true`. No wildcard or loose match.
- **Attribute mapping** is an ordered JSONB array of
  `{local, name, friendly_name, name_format, multi}` (Core §2.7.3).
  GHES needs URI NameFormat (`public_keys`) and multi-valued
  attributes (`emails`, `public_keys`, `gpg_keys`); the array shape
  supports both.
- **AuthnContextClassRef:** `…PasswordProtectedTransport` for
  password+TOTP, `…unspecified` for WebAuthn (no standard passkey
  ref exists yet), `Comparison="exact"`.
- **Metadata at `/saml/metadata`** publishes every `signing_key` row in
  the `{pending, active, decommissioning}` set, so verification continues
  across rotation (a decommissioning key lingers until its `retire_after`
  grace, default 7d, elapses).

### Per-app access gate (RBAC, v0.7)

A coarse access gate on top of the downstream protocols. New tables
`user_group` + `group_member` (first-class groups and membership) and
`oidc_client_access` / `saml_sp_access` (grants pointing at **either** a
group or an account, `CHECK num_nonnulls(group_id, account_id) = 1`),
plus an `access_restricted boolean NOT NULL DEFAULT false` column on
`oidc_client` and `saml_sp`. A single sqlc predicate per protocol
(`IsAccountAuthorizedFor{OIDCClient,SAMLSP}`) decides:

> **authorized** = `NOT access_restricted` **OR** a direct `(app, account)`
> grant exists **OR** the account is a member of any group granted to the
> app.

No admin bypass — `role='admin'` is not special-cased. The predicate is
evaluated after the session is validated and `account.disabled` enforced,
before anything is issued: at OIDC `/oauth/authorize`, **again at the
refresh-token grant** (so de-provisioning cuts existing sessions within
the access-token TTL — the refresh family is revoked), and at SAML SSO
(SP- and IdP-initiated). Denied **interactive** users are redirected to
the IdP's own `/error?reason=app_access_denied` page; OIDC `prompt=none`
gets a protocol-native `access_denied` at the `redirect_uri`; SAML passive
(`IsPassive`) gets a `Responder` / `RequestDenied` status Response. Every
denial writes an `access_denied` `credential_event`.

**Group exposure to downstreams** is a two-level opt-in: a group with
`exposed_to_downstream = true` (default true) flows to apps that ask —
an OIDC client with the `groups` scope (claim above) or a SAML SP whose
`attribute_map` has a `source: "groups"` entry (emits the exposed slugs,
multi-valued). Neither leaks unless both the group flag and the per-app
opt-in are set. The authorization predicate is also the query a future
end-user launchpad ("which apps may I launch?") will reuse.

## Admin management API

All routes are under `/api/prohibitorum`, admin-role gated. Mutations are
also fresh-sudo gated. The `registerSudoOpHTTP` wrapper in
`pkg/server/operations.go` is the single chokepoint for admin mutations
(admin auth + fresh sudo + 64 KiB body-size cap + JSON content-type
check). See `api.md` for the full route table and gate notation.

### OIDC clients

- `GET /oidc-clients`, `GET /oidc-clients/{clientId}` — read (🔓, secret never returned)
- `POST /oidc-clients` — create (🔐, secret revealed once in response)
- `PUT /oidc-clients/{clientId}` — update config (🔐, does not touch secret)
- `POST /oidc-clients/rotate-secret` — new secret (🔐, revealed once)
- `POST /oidc-clients/delete` — hard-delete (🔐)

### SAML service providers

- `GET /saml-providers`, `GET /saml-providers/{id}` — read (🔓)
- `POST /saml-providers` — create, optionally with metadata XML ingestion (🔐)
- `PUT /saml-providers/{id}` — update (🔐)
- `POST /saml-providers/{id}/reingest-metadata` — re-parse fresh SP metadata (🔐)
- `POST /saml-providers/delete` — hard-delete (🔐)

### Groups & per-app access (RBAC)

- `GET /groups`, `GET /groups/{id}`, `GET /groups/{id}/members`, `GET /accounts/{id}/groups` — read (🔓)
- `POST /groups`, `PUT /groups/{id}`, `POST /groups/delete` — group CRUD (🔐); slug validated `^[a-z0-9](-?[a-z0-9])*$`
- `POST /groups/{id}/members`, `POST /groups/{id}/members/remove` — membership (🔐)
- `GET /oidc-applications/{clientId}/access`, `GET /saml-applications/{id}/access` — read the restricted flag + grants (🔓)
- `POST …/access/set-restricted`, `…/access/grant`, `…/access/revoke` — toggle the gate and grant/revoke group/account access (🔐)

CLI parity: a `group` verb (`create|list|update|delete|add-member|remove-member`) and `access` subcommands on `oidc-client`/`saml-sp` (`--access-restricted`, `--grant-group`/`--grant-account`, `--revoke-*`). The SPA surfaces a groups admin section, a reusable per-app **Access** card, and a group-membership card on the account-detail page.

### Upstream IdPs

- `GET /upstream-idps`, `GET /upstream-idps/{slug}` — read (🔓, secret write-only: never returned)
- `POST /upstream-idps` — create including AES-GCM-sealed secret (🔐)
- `PUT /upstream-idps/{slug}` — update config excluding secret (🔐)
- `POST /upstream-idps/rotate-secret` — re-seal with new secret value (🔐)
- `POST /upstream-idps/delete` — hard-delete + cascade `account_identity` (🔐)

### Signing keys

- `GET /signing-keys` — read all, public material only (🔓, sealed private key never returned)
- `POST /signing-keys/generate` — mint RSA-2048 key, enters `pending` (🔐)
- `POST /signing-keys/{kid}/activate` — promote `pending`→`active`, prior `active`→`decommissioning` (🔐)
- `POST /signing-keys/{kid}/retire` — transition `decommissioning` key; 409 on the `active` key (🔐)

#### Signing-key lifecycle states

```
pending ──activate──► active ──activate(new)──► decommissioning ──reconcile──► retired
```

The `status` column is the sole lifecycle. Partial unique index
`one_active_signing_key (use) WHERE status='active'` enforces a single
active signer per `use`. The publish set for JWKS (`/oauth/jwks`) and
SAML metadata (`/saml/metadata`) is `{pending, active, decommissioning}`.
Signing always uses the single `active` key.

### Audit events

- `GET /audit-events` — query `credential_event` (🔓), filterable by
  `factor`, `event`, `accountId`, `since`, `until`; keyset pagination

Every admin mutation writes a `credential_event` row (`factor` ∈
`oidc_client` / `saml_sp` / `upstream_idp` / `signing_key`; `event` ∈
`register` / `update` / `rotate` / `revoke`). The `detail` JSONB
contains redacted metadata only — no secret, hash, or private key
material (enforced at the write site).

### Account credentials (admin)

- `GET /accounts/{id}/credentials` — list passkeys (🔓, suffix-only: last 4 chars of credential ID)
- `POST /accounts/credentials/delete` — admin force-revoke a passkey (🔐)

## Authentication ceremony

`pkg/authn/flow.go` resolves "which methods are available for this
account?":

1. Account has any `webauthn_credential` rows → WebAuthn ceremony.
2. Otherwise, `password_credential` + confirmed `totp_credential` →
   password+TOTP fallback.
3. Otherwise, at least one `account_identity` row → suggest the
   matching upstream IdP.
4. None of the above → "no usable method, contact admin." Admin
   issues a recovery enrollment token.

OIDC OP flow:

1. RP redirects user to `/oauth/authorize?...` with PKCE.
2. Prohibitorum checks session cookie. If absent, redirects to
   `/login?return_to=...` and presents available methods.
3. User authenticates; session minted and persisted in `session`
   table with `auth_time`, `amr`, `acr`.
4. Browser returns to `/authorize`; code minted (PKCE-bound, KV-stored
   with 60s TTL).
5. Redirects to `redirect_uri?code=...&state=...&iss=...`.
6. RP back-end POSTs to `/oauth/token` with code + verifier; receives
   ID token + access token + (optionally) refresh token.
7. RP validates ID token via JWKS.

SAML IdP flow:

1. SP sends AuthnRequest to `/saml/sso` (Redirect or POST binding).
2. If signature required, Prohibitorum verifies against the SP's
   `saml_sp_key` certs.
3. If no session, redirect to `/login`, then back to `/saml/sso`.
4. Build signed Response targeting the ACS URL; render HTTP-POST
   self-submitting form.

## Authorization model

- **`account.role`** ∈ `{user, admin}`. Admin gates server-side
  admin-only endpoints. Roles are flat, not hierarchical.
- **`account.attributes`** is a JSONB map. Opaque to Prohibitorum,
  carried verbatim into ID-token `attributes` claim and SAML
  AttributeStatement. RPs decide which keys are meaningful.
- **RPs enforce authorization** themselves using the claims.
  Prohibitorum doesn't decide whether user X can perform action Y on
  resource Z. (No OPA/Rego; the attribute map is a feature flag bag.)

## Data layout

**Postgres** — durable identity state. Detailed schemas live in
`db/migrations/001..005`; see
`docs/superpowers/specs/2026-05-24-multi-protocol-rescope-design.md`
§"Data model" for the full SQL and rationale per column.

- `account` — id, username, display_name, webauthn_user_handle,
  role, attributes jsonb, disabled, timestamps.
- `session` — id, account_id, auth_time, amr text[], acr,
  upstream_idp_id, created_at, revoked_at. Doubles as the source of
  OIDC `sid` claim.
- `webauthn_credential` — credential_id, public_key, cose_alg,
  user_handle, sign_count, transports, AAGUID, attestation_type,
  backup_eligible / backup_state, uv_initialized, nickname,
  last_used_at, clone_warning_at.
- `password_credential` — account_id PK, hash (PHC),
  password_changed_at.
- `totp_credential` — account_id PK, secret_enc + secret_nonce +
  key_version, period, digits, algorithm, last_step, confirmed_at.
- `recovery_code` — account_id, hash (PHC), used_at,
  used_session_id, used_ip.
- `enrollment` — token, intent, target_account_id, template_*,
  template_attributes jsonb, expected_upstream_idp_slug,
  expires_at, consumed_at.
- `credential_event` — append-only audit log: account_id, factor,
  event, credential_ref, ip, user_agent, detail jsonb, at.
- `auth_throttle` — `(account_id, factor)` PK, failed_attempts,
  window_start, locked_until. Persists across restarts.
- `signing_key` — kid, algorithm, use (sig/enc), public_jwk,
  x509_cert_pem, private_pem_enc + private_pem_nonce + key_version
  (AES-256-GCM-sealed private key), status
  (pending/active/decommissioning/retired), activated_at,
  decommissioned_at, retire_after. One row services both OIDC (via JWK)
  and SAML (via x509 cert). Partial unique index
  `one_active_signing_key (use) WHERE status='active'` ensures exactly
  one active signer per key-use value.
- `oidc_client` — RFC 8414 / OIDC Discovery static-registration metadata:
  redirect_uris, post_logout_redirect_uris, allowed_scopes, require_pkce,
  allowed_code_challenge_methods, token_endpoint_auth_method, subject_type,
  logo_uri, tos_uri, policy_uri.
- `revoked_jti` — jti PK, expires_at, reason. Denylist for
  self-contained access tokens (RFC 7009 + RFC 9068).
- `upstream_idp` — slug, display_name, issuer_url, client_id,
  client_secret_enc + secret_nonce + key_version, scopes, mode,
  allowed_domains, claim-name overrides.
- `account_identity` — account_id, upstream_idp_id, upstream_iss
  (snapshotted), upstream_sub, upstream_email. UNIQUE
  `(upstream_iss, upstream_sub)`.
- `saml_sp` + `saml_sp_acs` + `saml_sp_key` + `saml_subject_id` +
  `saml_session` — SAML SP registry, multi-endpoint ACS list,
  signing/encryption cert set, stable pairwise NameID, and
  forward-compat SLO session bookkeeping.

**KV** (KeyDB/Redis or in-process) — ephemeral state:

- `session:<acct>:<token>` → `SessionData` (sliding-refresh metadata).
- `webauthn_ceremony:{login,enroll,add,sudo}:<token>` → go-webauthn `SessionData`.
- `pairing:id:<id>` / `pairing:code:<code>` → device pairing state.
- `oidc:code:<random>` → `AuthCodeData` (account_id, client_id,
  scope, nonce, code_challenge, redirect_uri, consumed_at).
- `oidc:refresh:<random>` → `RefreshTokenData` (account_id, client_id,
  scope, family, rotated_from).
- `oidc:fed:state:<random>` → upstream-OIDC RP state with snapshotted
  `expected_iss` + `expected_token_endpoint`, nonce, code_verifier,
  return_to.

## Cryptography

- **Random tokens:** 32 bytes (256 bits) from `crypto/rand`, base64url.
- **Pairing codes:** 8 chars from 30-char unambiguous alphabet
  (rejection-sampled) ≈ 40 bits.
- **WebAuthn user handle:** 64 bytes random per account.
- **Signing keys:** RS256 (2048-bit RSA) unified across OIDC and SAML
  via `signing_key`. `kid` distinguishes rotation generations
  (recommend separate kid ranges per protocol, e.g.
  `oidc-2026-05` / `saml-2026-05`, so rotation can be decoupled).
  Private keys are sealed at rest in `private_pem_enc` (see at-rest
  encryption below); JWK form in `public_jwk`; self-signed x509 in
  `x509_cert_pem` for SAML.
- **At-rest encryption** for signing private keys, TOTP secrets, and
  upstream OIDC client secrets: AES-256-GCM with versioned DEK
  (`PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>`, 32 bytes base64), 12-byte
  nonce per row, AAD bound to row identity:
  - Signing key: `'signing_key:'||kid||':'||key_version`
  - TOTP: `'totp:'||account_id||':'||key_version`
  - Upstream IdP: `'upstream_idp:'||id||':'||key_version`
- **Hash storage** (`password_credential.hash`, `recovery_code.hash`,
  `oidc_client.client_secret_hash`): argon2id PHC strings
  (`$argon2id$v=19$m=65536,t=3,p=4$<salt>$<tag>`). Defaults from
  `configx.PasswordHashParams`; re-hash on verify if stored params
  are below current configured set.
- **Token lifetimes:** access 10 min, refresh 30 d (single-use
  rotation), session 8 h sliding refresh, sudo grant 5 min, OIDC
  code 60 s, federation state 10 min.

## Threat model

New surfaces beyond v0.1's WebAuthn-only model:

- **Password brute-force.** Per-account exponential backoff in
  `auth_throttle(account_id, factor='password')` — persistent across
  restarts (RFC 4226 §7.3). argon2id params tuned for ≥250ms/verify
  on prod hardware. No email-channel reset; admin enrollment token
  is the only recovery.
- **TOTP code guessing.** 6-digit space = 10^6. Rate-limit ≤5
  attempts / 5 min per account in `auth_throttle`. Same-step replay
  defeated by `totp_credential.last_step` (RFC 6238 §5.2).
- **Recovery-code theft.** Codes shown exactly once, argon2id-hashed
  at rest, single-use, redemption context captured.
- **Cross-account ciphertext swap.** AES-GCM AAD binds ciphertext
  to its row identity — copying ciphertext between rows fails
  decryption.
- **DEK compromise / rotation.** Versioned key set; row
  `key_version` selects decryptor. Rotation: deploy `_V2`, re-encrypt
  rows on next touch, retire `_V1` once `MAX(key_version)` reaches 2.
- **Federated IdP impersonation.** Strict issuer + audience + nonce
  validation on upstream ID token. Per-IdP client secret AES-GCM
  encrypted. `expected_iss` + `expected_token_endpoint` snapshotted
  into KV state at request time (RFC 9700 §4.4.2.1 mix-up
  resistance). `account_identity` keyed `(iss, sub)` per OIDC Core §2.
- **JIT account squatting.** `auto_provision` mode gated by
  `allowed_domains` against the email claim. Username collisions
  with existing unlinked accounts → reject; admin intervention
  required.
- **Authorization-code replay.** Codes kept (marked `consumed_at`)
  until TTL; replay revokes the refresh-token family and audit-logs
  the attempt.
- **Access-token revocation despite stateless JWT.** Every access
  token mints a `jti`; revocation writes `revoked_jti`. Self-
  validating resource servers check `jti` against the revocation
  cache; introspecting RSs get `active: false`.
- **WebAuthn authenticator cloning.** Sign-count regression stamps
  `clone_warning_at`; admin UI surfaces.
- **SAML assertion replay.** crewjam/saml enforces NotBefore /
  NotOnOrAfter / InResponseTo / one-use Assertion ID.
- **SAML open-redirect via spoofed ACS URL.** Validated against
  `saml_sp_acs` rows (exact match → index lookup → is_default
  fallback) per Profiles §4.1.4.1.
- **SAML NameID drift.** Stable `saml_subject_id(account_id, sp_id)`
  pairing — renames and email changes don't re-link GHES accounts
  (Core §8.3.7).
- **SAML XML signature wrapping (XSW).** crewjam/saml's
  post-canonicalization signature verification; reject assertions
  with multiple `Signature` elements or unexpected structure.
- **Stolen session cookie.** Live `account.disabled` check on every
  request + sudo for sensitive actions (carried over).
- **Bearer-token URL leak.** Device pairing avoids it;
  admin-issued recovery is the only bearer-token surface, gated by
  short TTL.

See `AUDIT.md` for the per-layer compliance matrix and `STATUS.md`
for the version-by-version delivery plan.

## Out of scope

- Multi-tenancy
- Self-service account recovery (admin-issued enrollment is the only
  path; no email/SMS channel of any kind)
- SAML SP (consuming upstream SAML)
- Social-login UX as a consumer feature (only admin-configured
  upstream OIDC IdPs)
- Dynamic OIDC client registration (RFC 7591)
- Consent screen (first-party deployment assumption)
- DPoP / PAR / JAR / mTLS / Pairwise sub
- HSM / KMS-backed signing keys (optional production hardening; unscheduled)
- Authorization policy engine (RPs enforce; we just supply claims)
- Audit-log export / SIEM integration (unscheduled)

Each is a clean future addition without breaking the v0.x surface.
