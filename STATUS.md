# Status — what's done, what's pending

This commit is the **v0.1 skeleton** — directory layout, design docs,
identity-layer code lifted from picotera, OIDC discovery+JWKS stubs.
The codebase does NOT compile cleanly yet; several pieces still need
adapting from picotera. See "Pending" below for the next-session
punch list.

## Done

### Design & docs
- `DESIGN.md` — full architecture, threat model, scope, schema.
- `README.md` — overview, quickstart sketch.
- `INTEGRATION.md` — RP-side patterns A (OIDC) and B (cookie+introspect),
  with code examples and library recommendations.
- `AUDIT.md` — OAuth 2.1 / OIDC / WebAuthn checklist with explicit
  ✅ / ⚠️ deferred / ❌ gap labels per item.

### Skeleton
- `go.mod` with picotera-matched library versions.
- `mise.toml`, `sqlc.yaml`, `.gitignore`.
- `cmd/prohibitorum/main.go` with `serve`, `openapi`, `enroll-admin`
  subcommands.

### Schema
- `db/migrations/001_initial.sql` — `account`, `webauthn_credential`,
  `enrollment` (squashed from picotera's 027 + 028 + 030).
- `db/migrations/002_oidc.sql` — `oidc_client`, `oidc_signing_key`.
- `db/migrations/migrations.go` — embedded goose runner.
- `db/queries/{account,enrollment,webauthn_credential,oidc}.sql`.

### Code (carried from picotera, identifiers swept to `prohibitorum/*`)
- `pkg/auth/` — entire identity layer:
  account, errors, enrollment, middleware, pairing, ratelimit,
  session, sudo, webauthn, webauthn_errors. Tests included.
- `pkg/configx/` — rewritten lean for Prohibitorum (no S3, llmbridge,
  JS hook config; added `OIDCConfig`).
- `pkg/kv/`, `pkg/logx/`, `pkg/errorx/` — verbatim utilities.
- `pkg/contract/auth.go` — Permission catalogue, view types,
  operations (paths under `/api/prohibitorum`).
- `pkg/server/` — operations.go, handle_{auth,account,enrollment,
  me,pairing,sudo,auth_ratelimit}.go, pgerr.go. Trimmed
  `handle_account.go` to drop the picotera `projectRouter` call.
  Wrote a lean `server.go` registering only identity ops.

### OIDC scaffolding
- `pkg/oidc/oidc.go` — Provider struct, Discovery and JWKS handlers
  (functional minimum); /authorize, /token, /userinfo, /logout
  return 501.

## Pending — to bring v0.1 to "compiles + smoke tests"

1. **Strip remaining picotera references in copied handlers.** Some
   `handle_account.go`/`handle_enrollment.go` paths still reference
   the api_key column structure via `CanManageOwnApiKeys` etc. — these
   compile against the v1 schema (which preserves those columns) but
   the perm vocabulary is picotera-leaning; rename in v0.2.
2. **Wire `oidc.Provider` into `server.go`'s registerOperations.**
   Add the five chi routes pointing to the Provider methods.
3. **`go mod tidy`** to lock the dep graph. Indirect deps from picotera
   not yet pulled in.
4. **Smoke-test the full path** — apply migrations, run server, hit
   `/.well-known/openid-configuration`, exercise the WebAuthn ceremony.

## Pending — v0.2 OIDC flows

5. **Signing key generation** — `oidc enroll-key` subcommand mints an
   RSA-2048 keypair, inserts into `oidc_signing_key` with `active=true`.
6. **`/jwks`** returns active + non-retired keys' `public_jwk`.
7. **`/oauth/authorize`** — validate query params, check session,
   redirect to `/login?return_to=...` if absent, mint code, redirect.
8. **`/oauth/token`** — `authorization_code` and `refresh_token` grants.
   Mint ID token (claims per `INTEGRATION.md`) and access token
   (RFC 9068 JWT profile).
9. **`/oauth/userinfo`** — verify access token, return claims.
10. **`/oauth/introspect`** — back-end pattern B implementation.
11. **`/oidc/logout`** — clear session, optionally redirect.

## Pending — v0.3 Frontend

12. **`dashboard/`** — pnpm + vite + vue 3 + tailwind v4 setup, mirror
    picotera layout.
13. Copy `dashboard/src/passkey/` SDK, `PasskeyPopupHost`,
    `SessionsCard`, `PairApproveDialog`, `PairingCode`/`PairingCodeInput`.
14. `LoginView` with `?return_to=` handling that posts the user back to
    `/oauth/authorize` after sign-in.
15. `EnrollView`, `MeView`, `AccountsView`, `RecoverChoiceView`,
    `AdminRecoveryView`, `CodeLoginView` — direct copies.
16. New: `ClientsView` for managing OIDC clients (v0.4).

## Pending — v0.4+

- Signing key rotation UX (admin button, scheduled rotation cron).
- Pairwise sub identifiers.
- DPoP / PAR / JAR (only when a low-trust client demands them).
- HSM/KMS backend for signing keys.
- Admin UI for OIDC clients.
- RP-Initiated Logout 1.0 fully wired.
- Front/back-channel logout for coordinated SSO sign-out.

## Why ship the skeleton incomplete

The user-management code from picotera is large and well-tested; the
new value here is (a) the extraction itself, (b) the OIDC layer wiring,
and (c) the documentation. The skeleton commit captures (a) and (c) —
both of which are reviewable independently of (b)'s cryptography. The
OIDC flows are well-specified by RFCs; landing them in stages with
tests is cleaner than landing them in one large commit with the
extraction.
