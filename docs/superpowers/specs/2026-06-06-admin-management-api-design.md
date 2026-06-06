# Admin Management API — Design

> Date: 2026-06-06
> Status: approved (brainstorm), pre-plan
> Scope: backend. Exposes the IdP-configuration surface (OIDC clients, SAML
> SPs, upstream IdPs, signing keys, audit log, admin credential listing) over
> HTTP, so the dashboard's five greyed "Planned" admin pages can be built and
> the backend management surface is complete. Frontend wiring is a separate,
> later chunk.

## 1. Context

The protocol backend (v0.1–v0.6) is implemented and smoke-verified end-to-end
(113 smoke steps, `SMOKE_EXIT=0`): WebAuthn, password+TOTP, recovery ceremony,
upstream OIDC federation, downstream OIDC OP, SAML IdP, and the OIDC consent
flow. The frontend full-surface scaffold is done and waiting.

The remaining backend gap is the **Admin Management API**. Everything an admin
needs to *configure* the IdP is currently CLI-only (cobra subcommands
`oidc-client`, `saml-sp`, `signing-key`) or SQL-only. The HTTP admin API today
covers only accounts (`/accounts*`) and invitations (`/invitations*`). The
frontend scaffold ships five greyed `PlaceholderView` admin pages
(`/admin/{oidc-clients,saml-providers,signing-keys,audit,settings}`) plus a
`stub`-badged "force-revoke credential" that cannot work because there is no
endpoint to list an account's credentials.

**Guiding principle — reuse, don't rebuild.** The CLI commands are thin
drivers: they parse config, connect to the DB, and call already-extracted
domain functions (`oidc.BuildClientParams`, `oidc.SealClientSecret`, `keygen`,
sqlc queries). HTTP admin handlers call the *same* domain functions. New code
is a handful of missing sqlc queries, thin handlers, one migration, and
contract types.

## 2. Layering boundary

Three layers, each owning its own concerns. **Sudo/session/HTTP concepts never
leak into domain code.**

- **Domain packages** (`pkg/protocol/oidc`, `pkg/protocol/saml`,
  `pkg/federation/oidc`, signing-key helpers): pure operations —
  `GenerateSigningKey`, `ActivateSigningKey`, `RetireSigningKey`,
  `RotateClientSecret`, `UpdateOIDCClient`, `UpdateSAMLSP`,
  `ReingestSAMLMetadata`, `UpdateUpstreamIDP`, etc. They take typed inputs and
  a query/tx handle; they return data or errors. No knowledge of HTTP,
  sessions, sudo, or CLI flags.
- **HTTP layer** (`pkg/server`): admin session auth, fresh-sudo gating, CSRF /
  same-origin posture, request validation, body-size limits, content-type
  checks, error mapping, audit context. Calls domain functions.
- **CLI layer** (`cmd/prohibitorum`): command flags, confirmation / `--yes`,
  local operator context, human-readable output. Calls the *same* domain
  functions.

CLI and API therefore share behavior on one code path while their
session/sudo/operator concerns stay separate. The CLI gains matching verbs on
the same domain code path (e.g. `signing-key {generate,activate,retire}`) — but
this still requires CLI-specific confirmation prompts, flag validation, and
operator audit context; it is not "free."

## 3. Access-control model

Gate sudo on whether an operation **changes who is trusted, where tokens/
assertions go, what identity data is released, whether users can authenticate/
link accounts, or whether state is destroyed** — not merely on whether a secret
is revealed.

**🔓 admin role only**
- `GET` list / detail. Secrets and raw key material are never serialized.
- (No separate cosmetic-edit path is built. See §4.)

**🔐 admin + fresh sudo** (5-min grant; reuses the existing sudo machinery)
- all secret operations and all signing-key mutations
- all deletes / destructive changes
- new RP / SP / upstream-IdP creation
- changes to redirect URIs, ACS URLs, post-logout URLs, issuer, scopes, claim
  mapping, attribute map, NameID format, allowed domains, `require_consent`,
  `require_verified_email`, `require_signed_authn_request`,
  `allow_idp_initiated`, `disabled`
- account credential revocation

Because a config-update body can carry both trust-affecting and cosmetic fields,
**the config-update endpoints are 🔐 as a whole.** A parallel 🔓 cosmetic-only
edit endpoint is intentionally not built (YAGNI).

## 4. Route policy & baseline protections

**Sudo is route policy, not handler logic.** A new wrapper centralizes it so a
🔐 route cannot be created with a missing sudo check:

```go
// registerSudoOpHTTP = registerOpHTTP + a mandatory fresh-sudo gate, applied
// before the handler runs. Used for every admin mutation.
registerSudoOpHTTP(s.router, method, path, admin, handler)
// equivalently expressed as registerOpHTTP(..., admin, s.withFreshSudo(handler))
```

**Two registration tiers:**
- **Reads (🔓)** → typed Huma `registerOp(mgmt, OperationX, handler, admin)` —
  full OpenAPI docs, same as today's account/invitation reads.
- **Mutations (🔐)** → `registerSudoOpHTTP(...)` raw handlers (needed for
  reveal-once secret bodies and uniform sudo policy).

**Baseline protections applied uniformly to every raw mutation route** (via the
wrapper / shared middleware, so the typed-vs-raw split cannot drift):
- admin authentication (`authn.Check` with the `admin` requirement)
- fresh sudo (`requireFreshSudo`)
- CSRF / session posture: the same `SameSite=Lax`, `HttpOnly`, same-origin
  session-cookie model that protects state-changing `/me/*` today. There is no
  separate CSRF token in this codebase; protection is `SameSite=Lax` +
  same-origin. (If a future deployment needs cross-site admin POSTs, a token is
  added then; out of scope here.)
- standard error mapping (`writeAuthErr` / `errorx` envelopes — `{code,message}`)
- audit context (acting account, request id)
- request body-size limit (`http.MaxBytesReader`)
- JSON content-type validation on mutation bodies

Where any of these are not already uniformly applied on the existing `/me/*`
raw handlers, this phase introduces them in the shared wrapper rather than
per-handler.

## 5. Endpoint catalog

Legend: 🔓 admin-role only · 🔐 admin + fresh sudo · ✨ secret revealed once ·
⊕ new sqlc query.

### 5.1 OIDC clients (`oidc_client`)
| Method · Path | Gate | Notes |
|---|---|---|
| `GET /oidc-clients` | 🔓 | list; no secrets |
| `GET /oidc-clients/{clientId}` | 🔓 | detail; full config, no secret |
| `POST /oidc-clients` | 🔐✨ | create; reuses `oidc.BuildClientParams`; confidential → secret once |
| `PUT /oidc-clients/{clientId}` | 🔐 | ⊕`UpdateOIDCClient` — redirect URIs, scopes, post-logout URIs, `require_consent`, `disabled`, display metadata |
| `POST /oidc-clients/rotate-secret` | 🔐✨ | ⊕`UpdateOIDCClientSecret`; new secret once |
| `POST /oidc-clients/delete` | 🔐 | ⊕`DeleteOIDCClient` |

### 5.2 SAML SPs (`saml_sp` + `saml_sp_acs` + `saml_sp_key`)
| Method · Path | Gate | Notes |
|---|---|---|
| `GET /saml-providers` | 🔓 | list |
| `GET /saml-providers/{id}` | 🔓 | detail incl. ACS list + key fingerprints (no raw private material) |
| `POST /saml-providers` | 🔐 | create (new RP + assertion destination); reuses metadata-ingest path (SP + ACS + keys in one tx) |
| `PUT /saml-providers/{id}` | 🔐 | ⊕`UpdateSAMLSP` — `require_signed_authn_request`, `allow_idp_initiated`, `session_lifetime`, attribute map, NameID format |
| `POST /saml-providers/{id}/reingest-metadata` | 🔐 | replace ACS + trusted keys in one tx (⊕`DeleteSAMLSPACSByID` / ⊕`DeleteSAMLSPKeysByID` + re-insert) |
| `POST /saml-providers/delete` | 🔐 | ⊕`DeleteSAMLSP` (+ cascade children) |

### 5.3 Upstream IdPs (`upstream_idp`)
| Method · Path | Gate | Notes |
|---|---|---|
| `GET /upstream-idps` | 🔓 | list; no secrets |
| `GET /upstream-idps/{slug}` | 🔓 | detail; secret write-only, never returned |
| `POST /upstream-idps` | 🔐 | create; secret sealed via `oidc.SealClientSecret` |
| `PUT /upstream-idps/{slug}` | 🔐 | `UpdateUpstreamIDP` (exists) — mode, allowed_domains, scopes, claim overrides, `require_verified_email`. **Excludes secret** |
| `POST /upstream-idps/rotate-secret` | 🔐 | secret-only update (seal new) |
| `POST /upstream-idps/delete` | 🔐 | `DeleteUpstreamIDP` (exists) |

### 5.4 Signing keys (`signing_key`) — see §6 for the lifecycle
| Method · Path | Gate | Notes |
|---|---|---|
| `GET /signing-keys` | 🔓 | ⊕`ListAllSigningKeys`; public material + explicit `status`; never `private_pem` |
| `POST /signing-keys/generate` | 🔐 | reuses `keygen`; inserts a **pending** key (published, not signing) |
| `POST /signing-keys/{kid}/activate` | 🔐 | pending → active; demotes prior active → decommissioning |
| `POST /signing-keys/{kid}/retire` | 🔐 | → decommissioning; **409 if the target is the active key and no replacement is active** |

### 5.5 Audit-events (`credential_event`)
| Method · Path | Gate | Notes |
|---|---|---|
| `GET /audit-events` | 🔓 | ⊕`ListCredentialEvents` — keyset pagination + filters: `factor`, `event`, `accountId`, `since`, `until`. Read-only |

### 5.6 Admin account credentials
| Method · Path | Gate | Notes |
|---|---|---|
| `GET /accounts/{id}/credentials` | 🔓 | reuses `ListCredentialsByAccount`; returns `CredentialView` (suffix only). Pairs with the existing `POST /accounts/credentials/delete` (🔐) to make admin force-revoke driveable |

**Route namespace** matches the existing flat admin convention (no `/admin`
prefix), defined as `huma.Operation` vars in `pkg/contract/auth.go` for the 🔓
reads. Verb convention mirrors today's admin routes (`PUT /x/{id}`,
`POST /x/delete`, action sub-paths).

## 6. Signing-key lifecycle

**Final model = Replace (single source of truth in an explicit DB state),
delivered via expand → cut over → contract.**

### 6.1 States
- **pending** — generated and published in JWKS / SAML metadata, but not used
  for signing.
- **active** — exactly one key (per `use`) used for new signatures.
- **decommissioning** — old key, no longer signing, still published for
  verification during the grace window.
- **retired** — not signing, not published.

### 6.2 Publish vs sign
- JWKS (`pkg/protocol/oidc/keys.go`) and SAML metadata
  (`pkg/protocol/saml/keys_saml.go`) publish `status IN ('pending','active',
  'decommissioning')`. `retired` is dropped.
- Signing selects the single `status='active'` key (per `use`).

### 6.3 Transitions
- **generate** → `pending`.
- **activate(kid)** (one tx, order matters): demote the prior `active` →
  `decommissioning` (`decommissioned_at = now()`, `retire_after = now() +
  rotation_grace`) **first**, then promote the target `pending` → `active`
  (`activated_at = now()`). Doing the demote before the promote keeps the
  partial unique index (§6.5) from ever seeing two actives.
- **retire(kid)** → `decommissioning`. If the target is the active key and no
  replacement is active, return **409 `active_key_no_replacement`** (never
  retire the sole signing key). Emergency force-retire (straight to `retired`)
  is deferred until explicitly requested.
- **reconcile** (background loop in `Serve()`, mirrors `pruneRevokedJTILoop` /
  `pruneExpiredSAMLSessionsLoop`): `decommissioning AND now() >= retire_after →
  retired`. Idempotent; safe to run repeatedly; logs/audits the transitioned
  `kid`s; never touches `active` or `pending` keys.

### 6.4 Migration `008_signing_key_lifecycle.sql` (expand — non-destructive)
```sql
ALTER TABLE signing_key
  ADD COLUMN status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','active','decommissioning','retired')),
  ADD COLUMN activated_at      TIMESTAMPTZ NULL,
  ADD COLUMN decommissioned_at TIMESTAMPTZ NULL,
  ADD COLUMN retire_after      TIMESTAMPTZ NULL;

-- Backfill from the legacy active/retired_at columns:
--   active=true  AND retired_at IS NULL → 'active'   (activated_at = COALESCE(not_before, created_at))
--   retired_at IS NOT NULL              → 'retired'  (decommissioned_at = retire_after = retired_at)
--   else                                → 'pending'
-- Defensive: if more than one active row exists per `use`, keep the newest as
-- 'active' and demote the rest, so the partial unique index below can build.

CREATE UNIQUE INDEX one_active_signing_key
  ON signing_key (use) WHERE status = 'active';
```
- TEXT + CHECK, **not** a Postgres enum.
- **Keeps** `active` and `retired_at` (legacy, untouched) during the expand
  phase. The cutover PR switches all reads/writes to `status`; no dual-write is
  needed because cutover is atomic within a single binary (no rolling deploy of
  mixed versions). A later **`009`** drops `active` / `retired_at` once `status`
  is proven in production.
- `kid` uniqueness is retained.
- `not_before` is retained (JWT `nbf` / publish-time hint; orthogonal to
  lifecycle).
- `rotation_grace` is the existing signing-key rotation-grace configuration
  (default 7d); exact config key confirmed at implementation.

### 6.5 Concurrency
The `one_active_signing_key` partial unique index guarantees at most one active
key per `use` even under concurrent activations: two simultaneous `activate`
transactions cannot both commit a second `active` row — one fails the unique
constraint and rolls back.

## 7. Audit coverage

Every mutation across all six families writes a `credential_event` via the
existing `audit.Writer`. No new table.

- **actor**: `account_id` = the acting admin.
- **target type/id**: in `detail` jsonb (e.g. `{"target":"oidc_client",
  "client_id":"…"}`).
- **action / result**: `event` ∈ {`register` (create), `update`
  (config/metadata), `rotate` (secret/key), `revoke` (delete / retire /
  credential-revoke)}; result/outcome recorded.
- **factor** ∈ {`oidc_client`, `saml_sp`, `upstream_idp`, `signing_key`,
  `webauthn` (admin credential-revoke)}.
- **sudo context** and **request id** recorded.
- **redacted field-diff summary** in `detail`.

**Redaction is mandatory:** no private key material (`private_pem`), client
secrets, access/refresh tokens, authorization codes, or raw SAML assertions ever
enter `detail` jsonb. The `/audit-events` viewer then surfaces config history
and credential history in one timeline.

## 8. Query, contract, and file additions

**New sqlc queries (~11):**
- Signing keys: `InsertPendingSigningKey`, `ActivateSigningKey` (demote prior +
  promote target), `RetireSigningKey` (→ decommissioning, with the active-guard
  check expressed in the handler/domain layer), `ReconcileRetiredSigningKeys`,
  `ListAllSigningKeys`, `GetSigningKeyByKID`.
- OIDC: `UpdateOIDCClient`, `UpdateOIDCClientSecret`, `DeleteOIDCClient`.
- SAML: `UpdateSAMLSP`, `DeleteSAMLSP`, `DeleteSAMLSPACSByID`,
  `DeleteSAMLSPKeysByID`.
- Audit: `ListCredentialEvents` (keyset + filters).
- Reused as-is: upstream-IdP `Update`/`Delete`/`List`/`Get`; webauthn
  `ListCredentialsByAccount`; signing-key `keygen`.

**New contract view types** (`pkg/contract/`): `OIDCClientView`,
`SAMLProviderView` (+ ACS / key sub-views), `UpstreamIDPView`, `SigningKeyView`
(carries `status`; never `private_pem`), `AuditEventView`, and the mutation
request bodies. New `huma.Operation` vars for the 🔓 reads.

**New handler files** (`pkg/server/`): `handle_admin_oidc_clients.go`,
`handle_admin_saml_sps.go`, `handle_admin_upstream_idps.go`,
`handle_admin_signing_keys.go`, `handle_admin_audit.go`; plus
`GET /accounts/{id}/credentials` appended to `handle_account.go`. The
`registerSudoOpHTTP` wrapper lands in `operations.go`. The reconcile loop is
registered in `Serve()`.

**CLI** (`cmd/prohibitorum`): add `signing-key {generate,activate,retire}` and
secret-rotation / update / delete subcommands on the shared domain code path,
with CLI-specific confirmation (`--yes`) and flag validation.

## 9. Testing

**Unit:**
- **route-policy test (critical):** enumerate every admin mutation route and
  assert each is sudo-wrapped (a 🔐 route reached without a fresh sudo grant
  returns `sudo_required`). The whole security model depends on consistent
  wrapping.
- signing-key state machine: activate demotes prior active; retire-active →
  409; reconcile transition; pending/active untouched by reconcile.
- migration/backfill: legacy `active`/`retired_at` rows map to the expected
  `status` + timestamps.
- concurrent activation: two simultaneous activations cannot produce two active
  keys (partial unique index).
- secret reveal-once: create/rotate responses reveal the secret exactly once;
  list/detail/refresh never reveal it.
- audit redaction: no private key material, client secrets, tokens, auth codes,
  or raw SAML assertions enter `detail` jsonb.
- CSRF/session: a cookie-authenticated mutation that violates the same-origin /
  `SameSite=Lax` posture is rejected (token test only "if applicable" — there
  is no separate CSRF token today).
- JWKS / SAML metadata publish-set spans `pending` + `active` +
  `decommissioning`; signing uses only `active`.

**Smoke** (`cmd/smoke`, currently 113 steps) — extend with an admin-API arc.
The smoke already shells `enroll-admin`, so it holds an admin session for sudo:
1. create OIDC client → secret revealed once; list/detail never reveal it.
2. update client (sudo) → config change reflected.
3. rotate client secret (sudo) → new secret once; old rejected.
4. **generate** signing key → assert JWKS publishes *current active + new
   pending*.
5. **activate** the generated key → assert signing now uses the new active key;
   assert the prior active becomes decommissioning; assert JWKS publishes *new
   active + prior decommissioning*; assert tokens signed by the prior key still
   verify during grace.
6. `GET /audit-events` reflects the mutations (actor, action, target,
   redacted).
7. admin list-credentials for an account → force-revoke (sudo) → credential
   gone.

**Regression:** full `go test ./...` and the existing 113-step smoke stay green.
The key-selection cutover is the riskiest change; the smoke's existing OIDC/SAML
token-verify steps are the guard.

## 10. Docs

Update on completion: `AUDIT.md` (admin-management rows + sudo posture),
`STATUS.md` (new phase), `ARCHITECTURE.md` (admin API surface), and `api.md`
for the raw `registerSudoOpHTTP` routes (which lack OpenAPI per the
`operations.go` note).

## 11. Out of scope

- Frontend wiring of the five `PlaceholderView` admin pages (separate chunk
  once these endpoints land).
- Emergency force-retire of a signing key (straight to `retired`) — deferred
  until requested.
- A separate CSRF token mechanism (current `SameSite=Lax` + same-origin posture
  is retained).
- A parallel 🔓 cosmetic-only edit path.
- `009` (drop `active`/`retired_at`) is a follow-up migration after `status` is
  proven, not part of this phase's cutover PR.
- v0.7+ items unchanged: HSM/KMS signing, SAML front-channel SLO, assertion/
  NameID encryption, refresh-token storage for upstream tokens, password
  breach-list check, audit-log SIEM export.
