# Prohibitorum — Architecture

A single-tenant identity provider for a small org. It owns the account directory, authenticates users through local factors (WebAuthn or password+TOTP/recovery codes) and admin-configured upstream OIDC or Steam providers, and uses VRChat profile proof to authorize local WebAuthn registration or recovery. It then issues identity assertions to downstream apps via OIDC OP or SAML 2.0 IdP.

## What this is (and isn't)

**Is:**
- A single-tenant IdP for a small org. Owns the account directory, runs WebAuthn / password+TOTP ceremonies, federates sign-in through OIDC and Steam, and uses VRChat proof to start authoritative local-credential ceremonies.
- A first-party service. No email channel; recovery is initiated by an admin-issued enrollment or a verified, already-linked VRChat profile.
- The source of truth for "who is this user, are they disabled, what attributes do we know about them?"

**Is not:**
- A multi-tenant SaaS IdP (no per-tenant separation).
- A SAML SP — Prohibitorum does **not** consume upstream SAML assertions. Its upstream provider adapters are OIDC, Steam OpenID 2.0, and VRChat profile proof.
- A self-service social-login proxy. OIDC and Steam provisioning modes are explicit; VRChat is fixed `link_only` and never acts as a direct local sign-in credential.
- A general-purpose authorization policy engine (no OPA/Rego). A free-form `attributes` map per account flows into ID-token claims and SAML AttributeStatement, and RPs still govern in-app resources. Prohibitorum additionally supplies a narrowly scoped, app-bound admission policy: it decides whether an active account may obtain credentials for one downstream app and projects only that app's matching group slugs.

## Architecture — three-layer

Industry-convergent layout drawn from Keycloak, Ory Kratos+Hydra, Authelia, Dex, Zitadel. Three layers, acyclic import graph:

1. **Identity store** — directory + credentials + federation links. Facts about users.
2. **Authentication subsystem** — factors + federation. Produces a `session`.
3. **Protocol subsystem** — OIDC OP + SAML IdP. Consumes a `session`.

The `session` package is the contract between layers (2) and (3). Protocols don't know how the user authenticated; factors don't know what RPs consume the result.

```text
                      ┌──────────────────────────────────┐
                      │             Prohibitorum         │
                      │                                  │
        browser  ────►│  Identity store                  │
                      │   pkg/account                    │
                      │   pkg/credential/{webauthn,      │
                      │     password, totp, pairing,     │
                      │     enrollment}                  │
                      │   pkg/federation/{providers/...} │
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

```text
pkg/
  account/                # directory: Account, list, disable, role, attributes
  appaccess/              # live app-bound admission evaluator, rules, claims, manager authorization
  credential/
    webauthn/             # WebAuthn registration + assertion
    password/             # argon2id PHC hash store + verify
    totp/                 # RFC 6238 + recovery codes; AES-GCM at-rest
    pairing/              # device-pairing code (no bearer-token in URL)
    enrollment/           # invite/reset/add-device/bootstrap tokens
  federation/             # protocol-neutral flow, resolver, secrets, registry
    providers/
      oidc/                # upstream OIDC RP
      steam/               # Steam OpenID 2.0 + Web API
      vrchat/              # operator session + exact profile proof
  session/                 # PG + KV-backed session, middleware
  authn/                  # login orchestrator + sudo + rate limit + middleware
  protocol/
    oidc/                 # downstream OIDC OP
    saml/                 # SAML 2.0 IdP, GHES-compatible profile
  server/                 # HTTP wiring, routes mounted from each subsystem
  contract/               # types exposed to dashboard / RPs
  audit/                  # credential_event writer
  kv/  logx/  errorx/  configx/   # utilities
```

## Authentication methods

Local factors are preferred. Upstream providers share the same federation
policy and persistence path.

### WebAuthn (primary)

ResidentKey=Required (discoverable credentials), UV=Required at register / Preferred at login. Sign-count regression detection writes `webauthn_credential.clone_warning_at` so the admin UI can surface suspected cloned authenticators. COSE algorithm, user handle, and `uv_initialized` are persisted per credential per WebAuthn L3 §4.

When a user adds a passkey via `POST /api/prohibitorum/me/credentials/register/{begin,complete}`, Prohibitorum offers to delete the account's password + TOTP + recovery codes in the same transaction (via `authn.DisableNonWebAuthnFallbacks`). Default yes. The decision is captured server-side; no client-side bypass.

### Password + TOTP (fallback)

For users without passkey-capable devices. Both factors required; neither alone produces a session.

- **Password.** argon2id PHC string at rest, salted per row, params tunable via `configx.PasswordHashParams`. On successful verify, if the stored hash uses parameters below the current configured set, re-hash and update. Persistent failed-attempt counter in `auth_throttle` (per RFC 4226 §7.3, cross-restart).
- **TOTP.** AES-256-GCM at rest with versioned DEK (`PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>`); AAD = `'totp:'||account_id||':'||key_version` so ciphertext can't be copied between rows. Per RFC 6238: ±1 period drift, `totp_credential.last_step` defeats same-step replay (§5.2). SHA1 default for Google Authenticator interop.
- **Recovery codes.** 10 codes shown once at TOTP enrollment; argon2id PHC at rest; single-use; redemption context (session, IP) captured for audit (`recovery_code.used_session_id`, `used_ip`).

### Upstream federation

The protocol-neutral core owns browser binding, single-use flow state,
provisioning policy, first-login confirmation, account resolution, identity
metadata persistence, and session inputs. Provider adapters own only their
external protocol and return one bounded verified-identity value.

OIDC and Steam providers support three modes:

- **auto_provision** — create a local account on first verified sign-in. OIDC
  additionally enforces its verified-email/domain policy.
- **invite_only** — consume an admin-issued enrollment whose expected provider
  slug matches the flow.
- **link_only** — never create an account; an unknown identity must first be
  linked from an authenticated local account.

VRChat providers are always `link_only`. Public VRChat proof is not federation
login or auto-provisioning: it authorizes a short-lived local WebAuthn
registration or recovery enrollment. Authenticated Connected Accounts linking
still binds the verified profile directly to the current account and session.

Provider sealed material uses AES-256-GCM with versioned data-encryption keys
and row-bound AAD. `account_identity` is uniquely keyed by provider plus stable
subject and stores only adapter-allowlisted `upstream_data`. Descriptor-declared
fields drive server-side admin identity filtering before pagination.

#### OIDC

Discovery, PKCE, nonce, issuer/audience/signature validation, and token exchange
remain inside the OIDC adapter. Flow state snapshots the expected issuer and
token endpoint, so a mid-flight provider edit cannot defeat RFC 9700 mix-up
resistance.

#### Steam

Steam uses OpenID 2.0 assertion verification followed by a bounded player
summary request authenticated with the sealed Steam Web API key. Its stable
subject is SteamID64.

#### VRChat

VRChat is a profile-proof adapter, not OAuth/OIDC and not a direct local
sign-in credential. An admin establishes a dedicated operator account session;
credentials and 2FA codes are transient, and only the encrypted reusable
cookie jar is retained. Each member ceremony creates a fresh browser-bound
proof URL, requires that exact URL in the requested VRChat profile's
`bioLinks`, and verifies the exact requested `usr_…` subject.

For an unknown profile, successful public proof issues a short-lived
`federated_register` enrollment with only a safe display-name suggestion. For
an already-linked profile, it issues a provider-backed `reset` enrollment whose
public preview omits the target account. Proof completion sets no normal
session: the shared WebAuthn registration ceremony is authoritative. New
registration creates the identity and first session atomically; recovery
replaces the credential, revokes prior sessions, and only then issues one fresh
session. Authenticated Connected Accounts linking remains direct and
session-bound.

Stored identity metadata is limited to user ID, display name, and canonical
profile URL. Operator cookies and credentials, proof tokens, enrollment tokens,
and enrollment snapshots remain opaque server-side values. Shared provider
backoff honors `429`/`Retry-After`; upstream `401`/`403` invalidates operator
readiness.

## Downstream protocols

### OIDC OP

Authorization Code + PKCE only. Implicit / ROPC / Hybrid are not registered as accepted `response_type`s. Discovery + JWKS endpoints published. RS256 signing (key store unified with SAML; see "Cryptography").

- **App-bound access and same-client exchange:** after disabled-account enforcement, the shared app-policy service is consulted at `/oauth/authorize`, authorization-code exchange, refresh, and `/oauth/userinfo`. An authorization code is bound to its issuing `client_id`, redirect URI, session, and PKCE challenge; it cannot be exchanged by another client. A refresh-token family is likewise bound to one client. Policy is re-evaluated at each of those boundaries rather than trusted from the original authorization.
- **ID token and userinfo claims:** `iss`, `sub`, `aud`, `exp`, `iat`, `nonce`, `auth_time`, `amr`, `acr`, `azp` (when `aud` is multi-valued or differs from authorized party), `at_hash`, plus `username`, `displayName`, `role`, and `attributes` carried verbatim from the account. When the owning app grants the `groups` scope, both the ID token and `/userinfo` contain a present-but-empty-or-sorted `groups` array. Its values are only the app-bound, `exposed_to_downstream` groups matching that account for this client; no other app's groups can appear.
- **Access token (RFC 9068):** `typ: at+jwt`. Required claims `iss`, `sub`, `aud`, `exp`, `iat`, `jti`, `client_id`, `scope`; `auth_time` / `amr` / `acr` carried when available. `/userinfo` reads its `client_id` and re-evaluates the same app policy before returning claims.
- **Refresh tokens:** opaque, KV-stored, single-use rotated family members. Reuse, client mismatch, account/session invalidation, or a live policy denial invalidates the entire family. A policy denial responds with OAuth `invalid_grant`; no replacement credential is issued.
- **Authorization codes:** atomically consumed and kept as a replay marker for their TTL. Replay revokes the refresh-token family minted by that code and writes a `credential_event` with `event=fail`, `factor=oidc_client`, and `reason=code_replay` (RFC 9700 §4.5, §4.14.2).
- **Revocation (RFC 7009):** writes to `revoked_jti` for self-contained access tokens; `/oauth/introspect` returns `active: false`.
- **RP-Initiated Logout:** `post_logout_redirect_uri` exact-matched against `oidc_client.post_logout_redirect_uris`.

### SAML IdP

SP-initiated SSO (HTTP-Redirect and HTTP-POST bindings for AuthnRequest; HTTP-POST binding for the Response) plus IdP-initiated SSO (per-SP opt-in). `crewjam/saml` does the protocol heavy lifting.

- **Assertion construction.** Always sign both `<Response>` and `<Assertion>`. `Destination` on `<Response>` = chosen ACS URL. `<SubjectConfirmationData Recipient>` = same ACS URL. `<Audience>` inside `<AudienceRestriction>` = `saml_sp.entity_id` verbatim.
- **NameID stability** via `saml_subject_id(account_id, sp_id)`: 32-byte random opaque value generated on first SSO, reused forever (Core §8.3.7). Defeats GHES account re-linking on rename / email change.
- **ACS lookup precedence** (Profiles §4.1.4.1): explicit `AssertionConsumerServiceURL` in AuthnRequest (must match a `saml_sp_acs` row exactly) → `AssertionConsumerServiceIndex` → `is_default=true`. No wildcard or loose match.
- **Attribute mapping** is an ordered JSONB array of `{local, name, friendly_name, name_format, multi}` (Core §2.7.3). GHES needs URI NameFormat (`public_keys`) and multi-valued attributes (`emails`, `public_keys`, `gpg_keys`); the array shape supports both. A `source: "groups"` mapping receives only the exposed matching groups bound to this SP, and omits the attribute when that set is empty.
- **App-bound gate:** the same shared policy service is evaluated before each SP- or IdP-initiated assertion (and on consent resume). A denial emits no assertion.
- **AuthnContextClassRef:** `…PasswordProtectedTransport` for password+TOTP, `…unspecified` for WebAuthn (no standard passkey ref exists yet), `Comparison="exact"`.
- **Metadata at `/saml/metadata`** publishes every `signing_key` row in the `{pending, active, decommissioning}` set, so verification continues across rotation (a decommissioning key lingers until its `retire_after` grace, default 7d, elapses).

### App-bound access policy

Prohibitorum is not a resource-permission engine: downstream applications remain responsible for their own resource authorization. It does own the final admission decision for an app and uses one protocol-neutral service for OIDC, forward-auth, SAML, launchpad candidates, PAT candidates, ID-token/userinfo claims, and SAML attributes. Protocol handlers do not duplicate policy logic.

#### Roles and application assignments

- `user` has self-service and downstream-authentication capabilities, plus management authority for exactly the apps assigned to that account.
- `admin` has global instance authority and may manage every application's policy. No role bypasses downstream app admission.

Only an admin may add or remove an assignment, and both mutations require fresh sudo. The target may be any enabled account, including an admin. A non-admin assignee cannot promote or edit accounts, change assignments, inspect credentials or secrets, manage providers, view global audit records, or change instance settings. Assignment grants management authority only; it never makes the assignee eligible to use that app.

Assignments are FK-backed: OIDC and forward-auth applications use the backing OIDC client assignment, while SAML applications use an SP assignment. An account can have many assignments and an app can have many assignees. The route kind is checked as well as the backing ID, so an OIDC management path cannot expose a forward-auth app or vice versa. Sessions reload the account on authenticated requests; disablement or deletion takes effect immediately, while changing between `user` and `admin` preserves assignments.

#### Groups, rules, and facts

Every policy group is bound to exactly one downstream application at creation. The binding is immutable: update requests contain no app-binding or kind field, and moving a group means deleting it and creating a replacement. A forward-auth group belongs to its backing OIDC client but is reached only through a `forward_auth` route. Slugs are unique within the owning app, never globally.

Each app has zero or one **manual** group and zero or more **rule** groups:

- A manual group stores one per-account `allow` or `deny` decision; an absent row is neutral. Upsert changes the effect and clear deletes the decision. Manual decisions cannot target a rule group.
- A rule group has no members and accepts no manual decisions. Its versioned rule document is a closed JSON AST: `all` and `any` contain non-empty `children`; `not` has one `child`; leaves are `connection.provider`, `connection.protocol`, `login_method`, or `avatar`.
- Valid leaf values are a known provider slug; protocol `oidc`, `steam`, or `vrchat`; login method `passkey`, `password_totp`, or `federation`; and avatar source `any` or `user_uploaded`. Unknown fields, unknown values, malformed node shapes, nonexistent providers, empty combinators, depth over 8, more than 64 nodes, or more than 32 children are rejected as `invalid_group_rule` with only `{path, reason}`. The existing 64 KiB JSON request limit is the outer bound.

Rule groups have no deny effect and no authorization priority. All rules are independently evaluated from a single live fact snapshot; every matching rule participates in the OR and in claim projection. Presentation order must not be treated as security semantics.

Facts contain no raw secrets or provider metadata:

- A connection fact requires `account_identity.confirmed_at`; disabling a provider does not erase an already verified connection.
- `passkey` requires a usable WebAuthn credential. `password_totp` requires both a password and confirmed TOTP. `federation` requires a confirmed identity for a currently enabled direct-sign-in provider; link-only VRChat does not qualify.
- `avatar.any` requires a usable user upload or verified upstream avatar candidate. `avatar.user_uploaded` requires a usable user upload.
- A disabled account fails before fact loading and is omitted from delegated account search and rule previews.

There is no calculated-membership table or reconciliation job. Connection confirmation/unlinking, login-method changes, provider enablement, and avatar changes affect the next decision. For one request, the service loads facts and rules once and reuses them for both access and claims.

#### Decision, claims, and protocol behavior

For a restricted app and active account:

```text
manual deny                       → deny
manual allow                      → allow
neutral + any matching rule group → allow
otherwise                         → deny
```

An unrestricted app remains open regardless of stored groups. No role, including `admin`, bypasses this decision. A manual allow projects its exposed manual slug plus every exposed matching rule slug; neutral rule-based access projects every exposed matching rule slug. Slugs are sorted and deduplicated. A manual deny produces no credential, assertion, or claim. OIDC requires the `groups` scope; SAML requires an attribute-map `groups` source; forward-auth returns the same app-bound slugs in `Remote-Groups`. These projections are app-aware and never cross an application boundary.

OIDC interactive denials use the IdP error page; `prompt=none` receives protocol-native `access_denied`. Authorization-code exchange rechecks current policy before minting tokens and returns `invalid_grant` on denial. Refresh does the same and revokes the entire refresh family before returning `invalid_grant`; `/userinfo` rejects a now-denied bearer token. SAML interactive denial uses the IdP error page, while passive SSO returns `Responder` / `RequestDenied`. Forward-auth re-evaluates both cookie and PAT requests, returns its existing denial response, and issues no downstream session. Launchpad and PAT app lists omit denied applications.

#### Destructive cutover

This model is a destructive schema migration, not a compatibility layer. It deletes legacy global groups, memberships, and per-app direct grants; resets every OIDC and SAML app to unrestricted; adds the manager-assignment, app-bound-group, and manual-decision schema; and leaves no aliases or legacy routes. A down migration restores only old table shapes and cannot recreate deleted policy data.

## Management API boundaries

All management and delegated routes use the `/api/prohibitorum` prefix. Admin-only routes require an `admin` session. Fresh sudo is required for secrets, irreversible operations, and manager assignment changes; its wrapper also enforces JSON content type and a 64 KiB body limit. Reversible policy mutations use the same content-type/body-size controls but do not require sudo. `api.md` defines the complete wire surface.

### Application configuration and assignments

Application pages are available to every signed-in account:

- OIDC: `/oidc-applications`; forward-auth: `/forward-auth-apps`; SAML: `/saml-applications`. Admins see all apps; other accounts see only their assigned apps.
- Assigned accounts use the normal detail pages to edit the assigned app and its access policy, including sudo-protected operations such as OIDC secret rotation. App creation, assignment management, and directory or instance administration remain admin-only.
- Each kind has admin-only assignment-list, assign, and remove endpoints. Assignment bodies use `{"accountId": <integer>}`; list responses expose only `id`, `username`, `displayName`, `disabled`, and `assignedAt`.

### Delegated app-policy surface

The policy endpoints under `/managed-applications/{kind}/{appId}` are available to signed-in accounts that can manage the exact app, plus admins. App kind is explicit: `oidc`, `forward_auth`, or `saml`; OIDC and forward-auth IDs are URL-escaped client IDs, and SAML IDs are positive decimal IDs. An invalid kind, wrong kind, missing or deleted app, or absent assignment is indistinguishable (`404`) to a non-admin account.

- `GET /managed-applications/{kind}/{appId}/access` returns the app, restriction flag, known provider slugs, optional manual group, and rule groups.
- `POST /managed-applications/{kind}/{appId}/access/set-restricted` accepts `{"restricted": <boolean>}`.
- `GET` and `PUT /managed-applications/{kind}/{appId}/groups` list or replace the app's associated global groups. An assigned account may retain groups already associated with the app and add groups that currently include that account.
- Rule preview, safe rule explanation, and active-account search remain nested under the same app path. Global group definitions and manual decisions are managed through admin-only `/groups` endpoints.

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

```text
pending ──activate──► active ──activate(new)──► decommissioning ──reconcile──► retired
```

The `status` column is the sole lifecycle. Partial unique index `one_active_signing_key (use) WHERE status='active'` enforces a single active signer per `use`. The publish set for JWKS (`/oauth/jwks`) and SAML metadata (`/saml/metadata`) is `{pending, active, decommissioning}`. Signing always uses the single `active` key.

### Audit events

- `GET /audit-events` — admin-only query of `credential_event`, filterable by `factor`, `event`, `accountId`, `since`, and `until`, with keyset pagination.

The audit stream records application assignment/removal with the compatibility factor `app_manager`; restriction changes, group create/update/delete, and manual allow/deny/clear with `factor=app_policy`; and protocol access denials with `factor=oidc_client` or `saml_sp`. App-policy records identify the actor, app kind/ID, target account where applicable, group ID, and action. They never include rule JSON, evaluated facts, credential details, identity metadata, hashes, private keys, or secrets. Writes remain best-effort: an insertion failure becomes a structured error log without the redacted detail payload.

### Account credentials (admin)

- `GET /accounts/{id}/credentials` — list passkeys (🔓, suffix-only: last 4 chars of credential ID)
- `POST /accounts/credentials/delete` — admin force-revoke a passkey (🔐)

## Authentication ceremony

`pkg/authn/flow.go` resolves which methods are available for an account:

1. Any `webauthn_credential` rows → WebAuthn ceremony.
2. Else `password_credential` + confirmed `totp_credential` → password+TOTP fallback.
3. Else ≥1 `account_identity` row → suggest the matching upstream IdP.
4. None → "no usable method, contact admin." Admin issues a recovery enrollment token.

OIDC OP flow:

1. RP redirects the browser to `/oauth/authorize` with its registered `client_id`, exact `redirect_uri`, `openid` scope, and PKCE.
2. Prohibitorum validates the client/redirect URI and session. If needed, it redirects to `/login?return_to=...`; after authentication the browser resumes the same authorization request.
3. The live app-policy service checks the active account for that client before consent or code issuance. A denied user receives the protocol-appropriate result described above.
4. A successful authorization produces a short-lived, PKCE-bound code for that same client and redirects to its registered URI with `code`, `state`, and `iss`.
5. The RP backend posts that code, the same redirect URI, and verifier to `/oauth/token` as the same client. The exchange revalidates client binding, session, account status, and live app policy before issuing ID/access tokens and, with `offline_access`, a refresh family.
6. The RP validates the signed ID token via JWKS. It must treat a later refresh `invalid_grant` as the family being unusable and restart authorization rather than retrying the old refresh token.

SAML IdP flow:

1. The SP sends an AuthnRequest to `/saml/sso` (Redirect or POST binding).
2. If signature required, Prohibitorum verifies against the SP's `saml_sp_key` certs.
3. If no session exists, the browser is redirected to `/login`, then back to `/saml/sso`.
4. Before an assertion is built, the shared live policy check authorizes the account for that exact SP.
5. A successful request produces a signed Response targeted only to the selected ACS URL and renders the HTTP-POST self-submitting form.

## Authorization model

- **`account.role`** is either `user` or `admin`. Roles are flat; application assignments separately grant management authority for exact apps.
- **`account.attributes`** is a JSONB map. It is opaque to Prohibitorum and carried verbatim into ID-token `attributes` claims and SAML AttributeStatements. RPs decide which keys are meaningful.
- **App admission versus application authorization:** Prohibitorum decides whether a user may obtain credentials for a particular downstream app. The RP still decides what the user may do inside that app. It does not evaluate arbitrary account-attribute expressions, scripts, CEL, Rego, or a generic resource/action permission graph.

## Data layout

**Postgres** — durable identity state. The current schema and migrations in `db/migrations` are authoritative.

- `account` — id, username, display_name, webauthn_user_handle, role, attributes jsonb, disabled, timestamps.
- `session` — id, account_id, auth_time, amr text[], acr, upstream_idp_id, created_at, revoked_at. Doubles as the source of OIDC `sid` claim.
- `webauthn_credential` — credential_id, public_key, cose_alg, user_handle, sign_count, transports, AAGUID, attestation_type, backup_eligible / backup_state, uv_initialized, nickname, last_used_at, clone_warning_at.
- `password_credential` — account_id PK, hash (PHC), password_changed_at.
- `totp_credential` — account_id PK, secret_enc + secret_nonce + key_version, period, digits, algorithm, last_step, confirmed_at.
- `recovery_code` — account_id, hash (PHC), used_at, used_session_id, used_ip.
- `enrollment` — token, intent, target_account_id, template_*, template_attributes jsonb, expected_upstream_idp_slug, expires_at, consumed_at.
- `credential_event` — append-only audit log: account_id, factor, event, credential_ref, ip, user_agent, detail jsonb, at.
- `auth_throttle` — `(account_id, factor)` PK, failed_attempts, window_start, locked_until. Persists across restarts.
- `signing_key` — kid, algorithm, use (sig/enc), public_jwk, x509_cert_pem, private_pem_enc + private_pem_nonce + key_version (AES-256-GCM-sealed private key), status (pending/active/decommissioning/retired), activated_at, decommissioned_at, retire_after. One row services both OIDC (via JWK) and SAML (via x509 cert). Partial unique index `one_active_signing_key (use) WHERE status='active'` ensures exactly one active signer per key-use value.
- `oidc_client` — downstream OIDC and forward-auth application metadata, including the restriction flag and client_auth_method (client_secret or none). Confidential clients accept either Basic or POST credentials. Forward-auth is identified by its forward-auth fields rather than a second policy table.
- `saml_sp` + `saml_sp_acs` + `saml_sp_key` + `saml_subject_id` + `saml_session` — SAML SP registry, multi-endpoint ACS list, signing/encryption cert set, stable pairwise NameID, forward-compat SLO session bookkeeping, and the restriction flag.
- `oidc_client_manager` and `saml_sp_manager` — FK-backed delegated-management assignments, including creator and timestamp.
- `user_group` — immutable binding to exactly one OIDC-backed or SAML app, kind (`manual` or `rule`), app-scoped slug, exposure flag, and rule document. Partial uniqueness allows at most one manual group per app.
- `group_manual_decision` — one `allow` or `deny` effect per account in a manual group; its composite foreign key prevents a decision from referencing a rule group.
- `revoked_jti` — jti PK, expires_at, reason. Denylist for self-contained access tokens (RFC 7009 + RFC 9068).
- `upstream_idp` — slug, display_name, issuer_url, client_id, client_secret_enc + secret_nonce + key_version, scopes, mode, allowed_domains, claim-name overrides.
- `account_identity` — account_id, upstream_idp_id, upstream_iss (snapshotted), upstream_sub, upstream_email. UNIQUE `(upstream_iss, upstream_sub)`.

**KV** (KeyDB/Redis or in-process) — ephemeral state:

- `session:<acct>:<token>` → `SessionData` (sliding-refresh metadata).
- `webauthn_ceremony:{login,enroll,add,sudo}:<token>` → go-webauthn `SessionData`.
- `pairing:id:<id>` / `pairing:code:<code>` → device pairing state.
- `oidc:code:<random>` → `AuthCodeData` (account_id, client_id, scope, nonce, code_challenge, redirect_uri, consumed_at).
- `oidc:refresh:<random>` → `RefreshTokenData` (account_id, client_id, scope, family, rotated_from).
- `oidc:fed:state:<random>` → upstream-OIDC RP state with snapshotted `expected_iss` + `expected_token_endpoint`, nonce, code_verifier, return_to.

## Cryptography

- **Random tokens:** 32 bytes (256 bits) from `crypto/rand`, base64url.
- **Pairing codes:** 8 chars from 30-char unambiguous alphabet (rejection-sampled) ≈ 40 bits.
- **WebAuthn user handle:** 64 bytes random per account.
- **Signing keys:** RS256 (2048-bit RSA) unified across OIDC and SAML via `signing_key`. `kid` distinguishes rotation generations (recommend separate kid ranges per protocol, e.g. `oidc-2026-05` / `saml-2026-05`, so rotation can be decoupled). Private keys sealed at rest in `private_pem_enc` (see at-rest encryption below); JWK form in `public_jwk`; self-signed x509 in `x509_cert_pem` for SAML.
- **At-rest encryption** for signing private keys, TOTP secrets, and upstream OIDC client secrets: AES-256-GCM with versioned DEK (`PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>`, 32 bytes base64), 12-byte nonce per row, AAD bound to row identity:
  - Signing key: `'signing_key:'||kid||':'||key_version`
  - TOTP: `'totp:'||account_id||':'||key_version`
  - Upstream IdP: `'upstream_idp:'||id||':'||key_version`
- **Hash storage** (`password_credential.hash`, `recovery_code.hash`, `oidc_client.client_secret_hash`): argon2id PHC strings (`$argon2id$v=19$m=65536,t=3,p=4$<salt>$<tag>`). Defaults from `configx.PasswordHashParams`; re-hash on verify if stored params are below the current configured set.
- **Token lifetimes:** access 10 min, refresh 30 d (single-use rotation), session 8 h sliding refresh, sudo grant 5 min, OIDC code 60 s, federation state 10 min.

## Threat model

- **Password brute-force.** Per-account exponential backoff in `auth_throttle(account_id, factor='password')`, persistent across restarts (RFC 4226 §7.3). argon2id params tuned for ≥250ms/verify on prod hardware. No email-channel reset; recovery requires an admin enrollment or proof of an already-linked VRChat profile followed by credential replacement.
- **TOTP code guessing.** 6-digit space = 10^6. Rate-limit ≤5 attempts / 5 min per account in `auth_throttle`. Same-step replay defeated by `totp_credential.last_step` (RFC 6238 §5.2).
- **Recovery-code theft.** Codes shown exactly once, argon2id-hashed at rest, single-use, redemption context captured.
- **Cross-account ciphertext swap.** AES-GCM AAD binds ciphertext to its row identity — copying ciphertext between rows fails decryption.
- **DEK compromise / rotation.** Versioned key set; row `key_version` selects decryptor. Rotation: deploy `_V2`, re-encrypt rows on next touch, retire `_V1` once `MAX(key_version)` reaches 2.
- **Federated IdP impersonation.** Strict issuer + audience + nonce validation on upstream ID token. Per-IdP client secret AES-GCM encrypted. `expected_iss` + `expected_token_endpoint` snapshotted into KV state at request time (RFC 9700 §4.4.2.1 mix-up resistance). `account_identity` keyed `(iss, sub)` per OIDC Core §2.
- **VRChat operator-session theft or drift.** The cookie jar is allowlisted, bounded, encrypted with provider-row AAD, and never serialized to public/admin views, audit detail, diagnostics, or logs. Upstream `401`/`403` snapshot-invalidates the stored session and fails closed until an admin repeats setup.
- **VRChat profile substitution or proof replay.** Flow state is browser-bound and single-use; the adapter requests one canonical `usr_…` ID, rejects a different returned subject, and accepts only the exact fresh proof URL in `bioLinks`.
- **Account squatting.** OIDC `auto_provision` enforces its configured verified-email/domain policy. VRChat registration first proves the exact stable profile subject, then the enrollment transaction re-checks the user-selected local username before creating and linking the account.
- **Authorization-code replay.** Codes kept (marked `consumed_at`) until TTL; replay revokes the refresh-token family and audit-logs the attempt.
- **Access-token revocation despite stateless JWT.** Every access token mints a `jti`; revocation writes `revoked_jti`. Self-validating resource servers check `jti` against the revocation cache; introspecting RSs get `active: false`.
- **WebAuthn authenticator cloning.** Sign-count regression stamps `clone_warning_at`; admin UI surfaces.
- **SAML assertion replay.** crewjam/saml enforces NotBefore / NotOnOrAfter / InResponseTo / one-use Assertion ID.
- **SAML open-redirect via spoofed ACS URL.** Validated against `saml_sp_acs` rows (exact match → index lookup → is_default fallback) per Profiles §4.1.4.1.
- **SAML NameID drift.** Stable `saml_subject_id(account_id, sp_id)` pairing — renames and email changes don't re-link GHES accounts (Core §8.3.7).
- **SAML XML signature wrapping (XSW).** crewjam/saml's post-canonicalization signature verification; reject assertions with multiple `Signature` elements or unexpected structure.
- **Stolen session cookie.** Live `account.disabled` check on every request + sudo for sensitive actions.
- **Scoped policy administration.** A delegated manager is authorized only after an exact app-kind and assignment check; ambiguous unassigned, wrong-kind, missing, and deleted app lookups fail as the same not-found response. Assignment never supplies downstream access, and live policy checks prevent a stale credential family from restoring it.
- **Bearer-token URL leak.** Device pairing avoids it; admin-issued recovery is the only bearer-token surface, gated by short TTL.

See `AUDIT.md` for the per-layer compliance matrix and `STATUS.md` for delivery status.

## Out of scope

- Multi-tenancy
- Self-service account recovery (admin-issued enrollment is the only path; no email/SMS channel of any kind)
- SAML SP (consuming upstream SAML)
- Self-service upstream-provider registration; only admin-configured OIDC, Steam, and VRChat providers are supported
- Dynamic OIDC client registration (RFC 7591)
- Consent screen (first-party deployment assumption)
- DPoP / PAR / JAR / mTLS / Pairwise sub
- HSM / KMS-backed signing keys (planned)
- Authorization policy engine (RPs enforce; we just supply claims)
- Audit-log export / SIEM integration (planned)

Each is a clean future addition without breaking the existing surface.
