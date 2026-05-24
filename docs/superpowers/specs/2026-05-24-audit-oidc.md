# Audit report — OIDC OP + RP federation

Produced 2026-05-24 by a research agent dispatched against the multi-protocol
rescope spec. Authoritative specs consulted: RFC 6749, OIDC Core 1.0, OIDC
Discovery 1.0, RFC 9068, RFC 9700, RFC 9207, RFC 8414, RFC 7636, RFC 7009,
RFC 7662, OIDC RP-Initiated Logout 1.0.

## Critical (blocks correctness)

### C1. `oidc_client` cannot answer `id_token_hint` / RP-initiated logout
**Spec:** OIDC RP-Initiated Logout §3, §3.1. **Design:** no `post_logout_redirect_uris` column. **Missing:** mandatory exact-match validation of `post_logout_redirect_uri` against a registered per-client list. **Fix:** add `post_logout_redirect_uris text[] NOT NULL DEFAULT '{}'`.

### C2. ID token missing `auth_time` storage path
**Spec:** OIDC Core §2 — `auth_time` REQUIRED whenever `max_age` requested or it's an Essential Claim. **Design:** no session table holds the authentication timestamp; ID-token claims plan omits `auth_time`. **Fix:** add a `session` table (see below) and include `auth_time` in the ID-token claim set.

### C3. RFC 9068 access token missing `jti`
**Spec:** RFC 9068 §2.2 lists `jti` as REQUIRED. **Design:** "standard claims + scope, client_id" — `jti` not explicit, and there is no `revoked_access_token` table to make `jti` actionable. **Fix:** mint `jti` on every access token; add a `revoked_jti` denylist table for RFC 7009 §2.1 + §3 revocation of self-contained tokens.

### C4. `at+jwt` header type
**Spec:** RFC 9068 §2.1 + §4 — `typ` MUST be `at+jwt`; resource servers MUST reject otherwise. **Design:** not stated. **Fix:** doc/code-level — no schema change.

### C5. `azp` and `at_hash` for OIDC Core conformance
**Spec:** OIDC Core §2 — `azp` required when `aud` has more than one value or differs from the authorized party; `at_hash` OPTIONAL in code flow but RECOMMENDED. **Design:** neither listed. **Fix:** emit `azp` whenever applicable; `at_hash` recommended.

### C6. Federation state insufficient for mix-up resistance
**Spec:** RFC 9700 §4.4, §4.4.2.1; RFC 9207 §2.4. **Design:** `oidc:fed:state:<random>` stores `idp_id, nonce, code_verifier, return_to`. **Missing:** the **issuer URL we expected** at request time. **Fix:** snapshot `expected_iss` (and `token_endpoint`) into the state blob at request time.

### C7. `account_identity` should persist upstream `iss`, not just FK
**Spec:** OIDC Core §2 — subject identifiers are unique only within an issuer. **Design:** `(upstream_idp_id, upstream_sub)` primary key. **Risk:** rotating `upstream_idp.issuer_url` silently aliases sub spaces. **Fix:** add `upstream_iss text NOT NULL` to `account_identity` and pin lookups on `(iss, sub)`.

### C8. Authorization code reuse / single-use
**Spec:** RFC 6749 §4.1.2 + RFC 9700 §4.5 — codes MUST be single-use; reuse SHOULD revoke any tokens already issued from the code. **Design:** `oidc:code:<random>` with 60s TTL. **Missing:** an explicit "consumed" marker. **Fix:** on first use, do not delete — mark `consumed_at` and revoke the issued refresh-token family on replay; sweep on TTL.

## Recommended (cheaper to do once vs migrate later)

### R1. `oidc_client` missing static-registration metadata
Add columns: `token_endpoint_auth_method` (default `client_secret_basic`), `id_token_signed_response_alg` (default `RS256`), `subject_type` (default `public`), `application_type` (default `web`), `post_logout_redirect_uris text[]`, `default_max_age int`, `require_auth_time bool`, `contacts text[]`, `logo_uri`, `tos_uri`, `policy_uri`. Sector identifier only if pairwise is enabled.

### R2. PKCE method allowlist
Add `allowed_code_challenge_methods text[] NOT NULL DEFAULT ARRAY['S256']`. Reject `plain` at admin level.

### R3. Recognize `offline_access` scope
Refresh-token issuance gated by `offline_access` per OIDC Core §11.

### R4. Signing key rotation needs `not_before` / `use`
Add `not_before timestamptz`, `use text NOT NULL DEFAULT 'sig'`.

### R5. Discovery doc fields
Mostly derivable from `oidc_client` + `signing_key` aggregates after R1+R2+R4. Publish `authorization_response_iss_parameter_supported=true`.

### R6. RFC 7662 introspection
Once C3 (jti) and C8 (consumed codes) land, response coverage is complete.

### R7. Refresh-token family forensics (optional)
Persist `refresh_token_family(family_id, account_id, client_id, created_at, revoked_at, revoked_reason)` in Postgres for audit beyond KV TTL.

## Optional / already-deferred

- PAR (RFC 9126), DPoP (RFC 9449), JAR (RFC 9101), mTLS (RFC 8705), pairwise subjects — no schema changes required now; trivial migrations later.

## New tables recommended

```sql
CREATE TABLE session (
  id              text PRIMARY KEY,
  account_id      bigint NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  auth_time       timestamptz NOT NULL,
  amr             text[] NOT NULL DEFAULT '{}',
  acr             text,
  upstream_idp_id bigint REFERENCES upstream_idp(id),
  created_at      timestamptz NOT NULL DEFAULT now(),
  revoked_at      timestamptz
);

CREATE TABLE revoked_jti (
  jti        text PRIMARY KEY,
  expires_at timestamptz NOT NULL,
  reason     text
);
CREATE INDEX revoked_jti_expiry ON revoked_jti (expires_at);
```
