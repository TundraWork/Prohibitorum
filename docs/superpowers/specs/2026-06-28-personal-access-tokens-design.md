# Personal Access Tokens — Design

## Background

Prohibitorum is not only an IdP; it also ships an **oauth2-proxy-like forward-auth
gateway** (v0.7, `pkg/protocol/oidc/forward_auth.go`) that protects upstream
services. A protected app is a normal `oidc_client` flagged `forward_auth_enabled`
with a `forward_auth_host`. Browser users reach protected apps via normal IdP login;
the gateway mints a per-domain cookie session and, on each request, verifies the
session, re-checks per-app RBAC, and emits authoritative `Remote-*` identity headers
that Traefik forwards upstream.

**Programmatic clients** — a user's scripts, CLI tools, or automation — need to reach
those same protected APIs without a browser login flow. The hard constraint is that
this must **not** be a separate backdoor: programmatic access goes through the *same*
gateway and the *same* protected-app policy model as browser access.

This requirement is best modeled as **user-owned Personal Access Tokens (PATs)**,
similar to GitHub PATs — *not* machine/service accounts and *not* OAuth client
credentials. A PAT is owned by a user and acts as that user with reduced privileges.
(An earlier exploration of machine accounts / client-credentials was deliberately
dropped: the concrete need is "a user wants their script to call an API protected by
the same gateway," which a user-owned credential models faithfully while preserving
the mental model "all access goes through the gateway; the gateway authenticates the
caller and constructs a trusted identity context.")

## Goals

- Users create, list, and revoke their own PATs (self-service).
- A PAT authenticates a request at the **forward-auth gateway**, resolving to the
  **owning user** as the principal (`auth_method = pat`).
- PATs support expiration, revocation, optional per-app restriction, and optional
  opaque upstream capability labels.
- The PAT plaintext is shown exactly once at creation; only a hash is stored.
- Effective permission is the **intersection** of: the owner's current authorization,
  the PAT's restrictions, and the protected-app policy.
- Disabled owners' PATs stop working immediately.
- API requests with an invalid/missing PAT return `401`; a valid PAT lacking
  authorization returns `403`; neither redirects to login.

## Non-goals

- No machine accounts / service accounts / `account.type`.
- No OAuth client-credentials grant; no `api_key → authorization_code` exchange.
- PATs are **not** a username/password replacement for browser login.
- PATs are accepted **only at the forward-auth verify endpoint** — never at the admin
  API, the OIDC endpoints, or any other Bearer surface. No new bypass path.
- Prohibitorum does not become an in-process reverse proxy. It remains a forward-auth
  **verifier**; header trust is realized by mandated Traefik config (below).
- Upstream services do not need to understand any PAT-specific access path — they
  consume the same normalized `Remote-*` identity context as for browser users.

## The model

A PAT is an alternative credential accepted at the gateway's verify endpoint. A
request carrying:

```http
Authorization: Bearer <PAT>
```

is processed as:

```text
Resolve protected app from X-Forwarded-Host          (existing)
Validate PAT (hash lookup; reject expired/revoked)
Load owner account; reject if disabled
Enforce PAT app restriction (allowed apps, if any)
IsAccountAuthorizedForOIDCClient(owner, app)         (existing RBAC predicate)
Emit authoritative Remote-* identity headers
→ 200   (Traefik forwards the trusted identity context upstream)
```

The resulting identity is always the **owning user**:

```text
subject     = the owner account (Remote-User = owner.username)
auth_method = pat
```

PAT access is therefore not a bypass and not a separate downstream contract — it is
simply another credential type accepted at the same authentication boundary, yielding
the same trusted identity headers a browser session would.

## Architecture — where PATs slot in

The single integration point is `HandleForwardAuthVerify`
(`pkg/protocol/oidc/forward_auth.go`). Today: a valid per-domain cookie → `200` +
identity headers; otherwise → `302` to `/oauth/authorize`. PATs add a Bearer branch
that is evaluated **first** and is **terminal** — a present `Authorization: Bearer`
is always handled in PAT (API) mode and never falls through to the cookie path or the
`302` redirect, regardless of whether a browser cookie is also present:

```text
resolve client from X-Forwarded-Host                  (GetForwardAuthClientByHost)

if Authorization: Bearer <token> is present:           (NEW — programmatic path; takes precedence)
    look up PAT by hash; if invalid/expired/revoked     → 401 invalid_token
    load owner; if disabled                             → 401 invalid_token
    if PAT restricted and this app not in allow-list    → 403
    if NOT IsAccountAuthorizedForOIDCClient(owner,app)  → 403
    emit Remote-* (+ Remote-Scopes) → 200
    # terminal: success or 401/403 — never the cookie path, never 302

else if a valid forward-auth cookie session exists:    (unchanged browser path)
    live RBAC + disabled check → 200 + Remote-* headers

else:                                                   (unchanged)
    302 → /oauth/authorize
```

**A present `Authorization: Bearer` header is the mode switch**: it forces API mode
(`200` on success, else `401`/`403`, never a redirect) *even if a cookie is also
present*. Only when no Bearer header is sent does the browser path apply (cookie →
`200`, else `302`). This makes the switch strictly true and satisfies "API requests
return 401, not a redirect" without sniffing `Accept` or `X-Requested-With`.

Reused as-is: `bearerToken(r)` and `writeBearerError()` (RFC 6750) from
`userinfo.go`; `IsAccountAuthorizedForOIDCClient`, `GetAccountByID`,
`ListExposedGroupSlugsByAccount`, and `writeIdentityHeaders` from the existing verify
path.

### 401 vs 403 (precise)

- **401 `invalid_token`** (with `WWW-Authenticate: Bearer error="invalid_token"`):
  the credential cannot establish a valid, enabled principal — PAT not found, expired,
  revoked, or owner disabled. Fail-closed; do not reveal which condition failed.
- **403**: a valid, enabled principal exists, but is not authorized for *this* app —
  the PAT's app restriction excludes it, or the RBAC predicate denies it.

## Identity headers — the authoritative set

The gateway owns a fixed identity header set and **always emits all of it on a 200,
even when a value is empty**:

| Header | Source |
|---|---|
| `Remote-User` | `account.username` |
| `Remote-Name` | `account.display_name` |
| `Remote-Email` | `account.email` (empty if unset) |
| `Remote-Groups` | comma-joined exposed group slugs |
| `Remote-Scopes` | comma-joined `upstream_scopes` (empty for cookie/browser sessions) |

Always-emit is the mechanism that makes "strip client-supplied values" real: when
Traefik is configured to take these headers from the auth response, an emitted value
(empty or not) **overwrites** any spoofed copy the caller sent. `Remote-Scopes` is new;
the four `Remote-*` identity headers already exist. **Change to `writeIdentityHeaders`:**
add a `scopes []string` parameter and make all five headers unconditional (today
`Remote-Email` is emitted only when non-empty — it becomes always-emit, a small
hardening of the existing browser path too).

`Remote-Scopes` carries the PAT's `upstream_scopes`: **opaque, app-specific capability
labels stored on the PAT. The gateway does not interpret them.** They are not OAuth
scopes; the caller never tells the upstream what it can do — it only presents the PAT,
and the gateway constructs the trusted identity context. The upstream service is solely
responsible for interpreting and enforcing them.

## Trust boundary — deployment requirements

> Because Prohibitorum runs as a forward-auth verifier and is not in the request data
> path, it cannot unilaterally remove the client's raw PAT from the upstream request.
> Deployments must configure Traefik to forward the authoritative `Remote-*` headers
> from Prohibitorum and strip the original `Authorization` header before the request
> reaches upstream.

Concretely, for every PAT-protected (forward-auth) route:

1. List all five authoritative headers in the forward-auth middleware's
   `authResponseHeaders` (or `authResponseHeadersRegex` matching `Remote-*`) so the
   gateway's values replace any client-supplied copies.
2. Add an explicit Traefik **Headers middleware** that removes the inbound
   `Authorization` header before forwarding upstream. (We do **not** rely on
   `authResponseHeaders` clearing a missing `Authorization` — that behavior is not
   guaranteed across our supported Traefik versions, so we strip it explicitly.)

These requirements are documented in `docs/forward-auth.md` as required config for
PAT-protected routes. This gives the intended security boundary without pretending
Prohibitorum can modify the in-flight request directly.

## Data model

New migration `db/migrations/023_personal_access_token.sql` (goose; next free number):

```sql
CREATE TABLE personal_access_token (
  id                 serial PRIMARY KEY,
  account_id         integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  name               text NOT NULL,                       -- user-facing label
  token_hash         bytea NOT NULL UNIQUE,               -- sha256(raw token)
  token_hint         text NOT NULL,                       -- non-secret display aid, e.g. prohibitorum_pat_…a1b2
  upstream_scopes    text[] NOT NULL DEFAULT '{}',        -- opaque labels → Remote-Scopes
  allowed_client_ids text[] NOT NULL DEFAULT '{}',        -- empty = any app the owner may reach
  created_at         timestamptz NOT NULL DEFAULT now(),
  expires_at         timestamptz,                         -- NULL = no expiry
  last_used_at       timestamptz,
  revoked_at         timestamptz
);
CREATE INDEX personal_access_token_account_idx ON personal_access_token(account_id);
```

- **`token_hash`** — see hashing below. `UNIQUE` so lookup is by hash equality.
- **`token_hint`** — a non-secret display string captured at creation (the static
  prefix + the last 4 chars of the secret, e.g. `prohibitorum_pat_…a1b2`) so the list
  UI can disambiguate tokens once the plaintext is gone. Too little entropy to aid a
  brute force; never the full token.
- **`allowed_client_ids`** — optional per-app restriction stored as `oidc_client.client_id`
  values (the stable PK the verify path already resolves from `X-Forwarded-Host`).
  Empty = unrestricted (still gated by RBAC). Stored as an array rather than a join
  table for simplicity; an orphaned id (deleted client) harmlessly never matches.
- **`expires_at`** — user picks at creation; `NULL` allowed (no expiry).
- **`last_used_at`** — best-effort, **throttled** (update only if older than ~1 min):
  the gateway verifies on every request, so an unconditional write per request would
  amplify load on the hot path.

Queries land in `db/queries/personal_access_token.sql` (sqlc → `pkg/db`):

- `GetPATByTokenHash :one` — `SELECT ... WHERE token_hash=$1 AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now())` (expiry/revocation filtered in SQL;
  no row → `401`).
- `ListPATsByAccount :many`, `InsertPAT :one`, `RevokePAT :exec` (sets `revoked_at`,
  scoped to `(id, account_id)` so a user can only revoke their own),
  `TouchPATLastUsed :exec`.

## Token format & hashing

- **Format:** `prohibitorum_pat_` + base64url(32 random bytes from `crypto/rand`). The
  distinctive prefix enables secret-scanning. (Exact prefix string is adjustable.)
- **Hashing:** `token_hash = sha256(raw_token_string)`, stored and `UNIQUE`-indexed;
  validation hashes the presented Bearer value and looks it up by equality.
  **SHA-256, not argon2id** — the gateway validates on every request, so a slow KDF
  (~250ms) would wreck gateway latency; PATs are 256-bit random, so a fast hash is
  cryptographically sufficient (no brute-force surface). Lookup is by full-hash
  equality, so no per-character timing oracle exists.
- This deliberately differs from `oidc_client.client_secret_hash` (argon2id), because
  that secret is low-frequency and verified off the hot path; a PAT is a high-entropy
  bearer token checked per request.

## Self-service surface

### Backend — `/api/prohibitorum/me/tokens` (mirrors the `/me` patterns in `handle_me.go`)

| Method + route | Gate | Behavior |
|---|---|---|
| `GET /me/tokens` | session (`registerOp`) | List the caller's PATs — **never** returns the secret. Returns id, name, `token_hint`, created_at, expires_at, last_used_at, revoked_at, allowed app display names, upstream_scopes. |
| `POST /me/tokens` | session + **fresh sudo** | Create a PAT; returns the plaintext **once**. Mirrors passkey registration, which is sudo-gated to stop a stolen session from minting a long-lived credential. (`registerSudoOp` typed, or the HTTP variant — plan-level choice.) |
| `POST /me/tokens/revoke` | session (`registerOp`) | Revoke one of the caller's PATs by id (sets `revoked_at`). No sudo — revocation is defensive, matching `/me/sessions/revoke`. |

Response/request shapes mirror existing `/me` handlers (`revokeMySessionIn`, the
`contract.*View` structs). A `contract.PersonalAccessTokenView` carries the
non-secret fields.

### Frontend — dashboard

A dedicated **Personal Access Tokens** page at `/tokens`, mirroring `SessionsView.vue`
(list + revoke) and reachable from the sidebar:

- List PATs (name, `token_hint`, created, expires, last used, restrictions); revoke
  via the existing `ConfirmDialog`.
- A **create** dialog (name, expiry preset incl. "no expiry", optional app restriction,
  optional `upstream_scopes` via `ComboboxTokenInput`). The app-restriction picker
  reuses the existing end-user launchpad surface (`handle_me_apps.go` / `MyAppsView`),
  filtered to forward-auth-enabled apps the owner can reach.
- A **reveal-once** view mirroring `components/custom/RecoveryCodesDisplay.vue`: shows
  the plaintext token, copy/download, an "I've saved this token" checkbox gating a
  Done button that clears it. The plaintext is never persisted across navigation.
- API via `useApi()`; strings in `locales/en.ts` + `locales/zh.ts` (keep parity;
  guarded by the existing compile/parity tests).

## Audit

Add `FactorPAT = "personal_access_token"` to `pkg/audit/event.go`.

- **Create** → `credential_event{factor: personal_access_token, event: register}`.
- **Revoke** → `credential_event{factor: personal_access_token, event: revoke}`.
- **Use is not individually audited.** The gateway verifies on every request; auditing
  each success or each failure would flood the log (a single misconfigured client
  loops). `last_used_at` covers successful use; a future rate-limited fail-audit is
  out of scope.

## Security considerations

- **Long-lived bearer credential** is the chief risk (OWASP NHI7). Mitigations:
  shown-once + hash-at-rest; user-chosen expiry; self-service revocation; immediate
  invalidation when the owner is disabled; sudo-gated creation; secret-scanning prefix.
- **No privilege escalation:** a PAT can never exceed its owner — every request is
  re-evaluated through `IsAccountAuthorizedForOIDCClient` (live RBAC) ∩ the PAT's app
  restriction ∩ protected-app policy. De-provisioning the owner (or removing a group)
  takes effect on the next request.
- **No header spoofing:** all five authoritative headers are always emitted and
  overwritten by Traefik; the raw PAT is stripped by mandated Traefik config.
- **Fail-closed:** any error resolving the principal or evaluating authorization
  yields `401`/`403`, never a `200`.

## Testing

- **Unit (`pkg/protocol/oidc`, mirror `forward_auth_test.go`):** PAT valid → 200 with
  the full `Remote-*` set incl. `Remote-Scopes`; expired/revoked/unknown → 401;
  disabled owner → 401; app-restriction excludes app → 403; RBAC denies → 403; cookie
  path still emits an empty `Remote-Scopes`.
- **Handler (`pkg/server`):** `/me/tokens` create requires sudo (401/403 without);
  create returns plaintext once and stores only a hash; list never leaks the secret;
  revoke is owner-scoped.
- **Smoke (`ci:smoke`):** end-to-end against the dev forward-auth harness — create a
  PAT, call the verify endpoint with `Authorization: Bearer`, assert 200 + headers;
  invalid → 401; unauthorized app → 403.

## Out of scope / future

- Admin oversight of other users' PATs (list/revoke). MVP is owner self-service only.
- Coarse gateway-level enforcement of `upstream_scopes` (today purely opaque/upstream).
- Rate-limited audit of failed PAT attempts.
- A configurable maximum PAT lifetime.
