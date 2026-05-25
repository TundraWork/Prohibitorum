# Audit — OAuth 2.1 / OIDC / WebAuthn / SAML / NIST best-practice checklist

Compliance of the current codebase against authoritative standards.
Status labels:

- **✅** — implemented end-to-end (code enforces the item today)
- **✅ schema** — DB column / table exists; no Go path reads/writes it yet
- **✅ design** — architectural decision locked in spec; no schema or code yet
- **✅ stub** — handler exists and is mounted; returns 501 or partial output
- **✅ planned** — target version named; tracked
- **⚠️ deferred** — intentional v0.x omission with a clear target version
- **❌ gap** — unfinished and needs work before v1.0
- **❌ explicitly forbidden** — the standard forbids this (NIST §3.1.1.2 etc.)

When a bare **✅** appears, read the Notes column: it may still be
schema-only. Suffix labels above qualify what's actually in v0.1.

The full spec-vs-design audits that drove the v0.1 schema decisions
live in:

- `docs/superpowers/specs/2026-05-24-audit-oidc.md` — OIDC OP + RP
  federation (8 critical / 7 recommended findings).
- `docs/superpowers/specs/2026-05-24-audit-credentials.md` —
  WebAuthn / Password / TOTP / Recovery codes (5 critical / 8
  recommended).
- `docs/superpowers/specs/2026-05-24-audit-saml.md` — SAML IdP +
  GHES interop (5 critical / 6 recommended + 10 GHES call-outs).

This file is the running checklist of "what we comply with right now."
Each item below is annotated with the audit-report ID (e.g. "credentials/C1")
when it traces to one of those reports.

---

## WebAuthn (W3C Level 3)

| Item | Status | Notes / source |
|---|---|---|
| `ResidentKey=Required` (discoverable) | ✅ | `pkg/credential/webauthn` |
| `UserVerification=Required` at register, `Preferred` at login | ✅ | FIDO Alliance UV split |
| `AttestationPreference=PreferNoAttestation` | ✅ | No fingerprinting |
| `excludeCredentials` on add-passkey | ✅ | `handle_me.go` |
| Sign-count clone detection → `clone_warning_at` | ✅ | credentials/R8; `webauthn_credential.clone_warning_at` |
| `user_handle` persisted (L3 §4) | ✅ | credentials/R2; `webauthn_credential.user_handle` indexed |
| `cose_alg` persisted | ✅ | credentials/R1; `webauthn_credential.cose_alg`; extracted from the COSE_Key CBOR by `pkg/credential/webauthn.COSEAlg`; smoke-verified at both insert sites (`handle_enrollment.go:531` initial enrollment, `handle_me.go:201` add-second-passkey) |
| `uv_initialized` persisted (L3 §4) | ✅ | credentials/C5; `webauthn_credential.uv_initialized` |
| `backup_eligible` / `backup_state` persisted | ✅ | `webauthn_credential.backup_eligible/state` |
| Full attestation-object retention for MDS3 validation | ⚠️ deferred (v0.7+) | credentials/Optional |
| `created_via` provenance (registration / add / recovery) | ⚠️ deferred (v0.2) | credentials/Optional |

## Password (NIST SP 800-63B-4 draft)

| Item | Status | Notes / source |
|---|---|---|
| argon2id PHC string at rest (self-describing params) | ✅ schema | credentials/R5; `password_credential.hash`; no Go writes it yet |
| Per-row salt embedded in PHC | ✅ design | argon2id PHC format; v0.2 wires it |
| `password_changed_at` distinct from `updated_at` | ✅ schema | credentials/R6; column present, no code updates it yet |
| Configurable params (`PasswordHashParams`) with re-hash on verify | ✅ schema | configx; verify lands v0.2 |
| Persistent failed-attempt counter (cross-restart) | ✅ schema | credentials/R4; `auth_throttle (account_id, factor='password')` |
| Verify endpoint with throttle enforcement | ⚠️ deferred (v0.2) | `pkg/credential/password.Verify` stubbed |
| Breach-corpus check (k-anonymity-style) on set | ⚠️ deferred (v0.2+) | NIST SP 800-63B-4 §3.1.1.2 |
| Periodic rotation forced | ❌ explicitly forbidden | NIST §3.1.1.2 — do not add |
| Password history | ❌ explicitly forbidden | NIST §3.1.1.2 — do not add |
| Composition rules (uppercase / digit / symbol) | ❌ explicitly forbidden | NIST §3.1.1.2 — do not add |
| No email channel for reset; admin enrollment-token only | ✅ design | enrollment intent `reset` |

## TOTP (RFC 6238 / RFC 4226)

| Item | Status | Notes / source |
|---|---|---|
| Secret entropy ≥ 160 bits | ✅ schema | `secret_enc bytea`; generator lands v0.2 |
| AES-256-GCM at rest | ✅ schema | credentials/C3+C4; `secret_enc` + `secret_nonce` |
| Versioned DEK (`key_version` per row) | ✅ schema | credentials/C3; `totp_credential.key_version` |
| AAD bound to row identity (`'totp:'||account_id||':'||key_version`) | ✅ design | credentials/C4; spec §"AES-GCM at-rest" |
| Per-row nonce (12 bytes from `crypto/rand`) | ✅ schema | `totp_credential.secret_nonce` |
| 30-second period, 6 digits | ✅ schema | `period`, `digits` columns; defaults match RFC 6238 §1.2 |
| SHA1 default (Google Authenticator interop) | ✅ schema | credentials/R3; `algorithm` column |
| ±1 period drift tolerance | ✅ planned | `configx.TOTP.DriftSteps=1`; verify lands v0.2 |
| `last_step` defeats same-step replay (RFC 6238 §5.2) | ✅ schema | credentials/C1; `totp_credential.last_step` |
| `confirmed_at` gates the credential until first verify | ✅ schema | `totp_credential.confirmed_at` |
| Persistent throttle (RFC 4226 §7.3) | ✅ schema | credentials/R4; `auth_throttle (account_id, factor='totp')` |
| TOTP issuer / label format in QR codes | ⚠️ deferred (v0.2) | spec §"Open questions" |
| Single TOTP credential per account | ✅ design | industry norm; PRIMARY KEY (account_id) |

## Recovery codes

| Item | Status | Notes / source |
|---|---|---|
| argon2id PHC at rest, per-row salt | ✅ schema | credentials/C2; `recovery_code.hash` |
| Single-use (`used_at` enforced) | ✅ schema | `ConsumeRecoveryCode` query |
| Shown exactly once at enrollment | ✅ design | spec §"Threat model" |
| Redemption context captured (session id, IP) | ✅ schema | credentials/R7; `used_session_id` + `used_ip` |
| Mint count: 10 per account | ✅ planned | `configx.TOTP.RecoveryCodeCount=10` |
| Codes redeemable independently of TOTP | ✅ planned | dedicated endpoint `/auth/recovery-code/verify` (v0.2) |
| Code redemption logic | ⚠️ deferred (v0.2) | `pkg/credential/totp.VerifyRecoveryCode` stubbed |

## Upstream OIDC federation (OIDC Core / RFC 9700)

| Item | Status | Notes / source |
|---|---|---|
| Per-IdP `upstream_idp` row with issuer + client + scopes | ✅ schema | `upstream_idp` |
| Client secret AES-GCM encrypted with versioned DEK | ✅ schema | `client_secret_enc` + `secret_nonce` + `key_version` |
| AAD bound to row identity (`'upstream_idp:'||id||':'||key_version`) | ✅ design | spec §"AES-GCM at-rest" |
| Three provisioning modes (`auto_provision` / `invite_only` / `link_only`) | ✅ schema | CHECK constraint on `upstream_idp.mode` |
| `auto_provision` gated by `allowed_domains` | ✅ schema | `upstream_idp.allowed_domains` |
| `invite_only` binds enrollment to specific IdP | ✅ schema | `enrollment.expected_upstream_idp_slug` |
| `account_identity` keyed on `(upstream_iss, upstream_sub)` (OIDC Core §2) | ✅ schema | oidc/C7; UNIQUE constraint |
| Federation state snapshots `expected_iss` + `expected_token_endpoint` | ✅ design | oidc/C6; spec §"Federation state KV" |
| Strict issuer + audience + nonce validation on upstream ID token | ✅ planned | v0.3 implementation |
| Per-IdP claim-name overrides (`username_claim` etc.) | ✅ schema | `upstream_idp.username_claim/display_name_claim/email_claim` |
| RP flow implementation | ⚠️ deferred (v0.3) | `pkg/federation/oidc` stubbed |
| Local-username collision policy on JIT auto-provision | ⚠️ deferred (v0.3) | spec §"Open questions" |
| Refresh-token storage for upstream tokens | ❌ gap | not yet needed; revisit when /me wants to refresh upstream profile |

## OIDC OP downstream (RFC 6749 / OIDC Core / RFC 9068 / RFC 9700 / RFC 9207 / RFC 8414 / RFC 7636 / RFC 7009 / RFC 7662 / RP-Initiated Logout 1.0)

| Item | Status | Notes / source |
|---|---|---|
| Authorization Code + PKCE only | ✅ schema | `oidc_client.require_pkce` defaults true; CHECK forbids `plain` via allowlist |
| PKCE required for **all** clients (incl. confidential) | ✅ schema | `require_pkce` default true; admin can not turn off in v0.6+ CHECK |
| `code_challenge_method` allowlist rejects `plain` | ✅ schema | oidc/R2; `allowed_code_challenge_methods text[]` default `{S256}` |
| `redirect_uri` exact-match (no wildcards) | ✅ schema | `oidc_client.redirect_uris text[]` |
| `post_logout_redirect_uris` exact-match list | ✅ schema | oidc/C1; `oidc_client.post_logout_redirect_uris` |
| Single-use authorization codes with replay revocation | ✅ design | oidc/C8; spec §"Authorization-code lifecycle" — `consumed_at`, revoke family on replay, audit |
| `iss` parameter in authorization response (RFC 9207) | ✅ design | spec §"HTTP surface"; discovery advertises support |
| Discovery doc (RFC 8414 / OIDC Core) | ✅ stub | `/.well-known/openid-configuration` mounted; advertises planned v0.4 endpoints; `claims_supported` lists `sub/iss/aud/exp/iat/nonce/auth_time/amr/acr/username/displayName/role/attributes` |
| JWKS endpoint | ✅ stub | `/oauth/jwks` mounted; returns empty `keys` array until v0.4 mints signing keys |
| ID token signed with asymmetric alg | ✅ design | RS256; ES256 / EdDSA possible via `signing_key.algorithm` |
| `alg: none` rejected | ✅ design | jwt verification configured for RS256 only |
| ID token claims: signature, `iss`, `aud`, `exp`, `nonce` validated | ✅ design | INTEGRATION.md |
| ID token `auth_time` claim (OIDC Core §2) | ✅ schema | oidc/C2; `session.auth_time` populated on every `sessionStore.Issue` (writer wired; smoke-verified ≥3 rows per account); ID-token mint that emits the claim lands in v0.4 |
| ID token `amr` / `acr` claims | ✅ schema | oidc/C2; `session.amr` populated (WebAuthn → `["hwk"]`; smoke-verified all rows); `session.acr` reserved for v0.2+; ID-token mint v0.4 |
| ID token `azp` when `aud` is multi-valued | ✅ design | oidc/C5; spec §"Access-token issuance" |
| ID token `at_hash` (defense in depth) | ✅ design | oidc/C5; spec |
| `sid` claim sourced from `session.id` | ✅ schema | `session.id` PK; rows inserted on `Issue` (smoke-verified); revoked on `Revoke` (smoke-verified), `RevokeBySessionID` (smoke-verified), `RevokeAllForAccount` (wired, smoke-untested — admin endpoint); claim emission v0.4 |
| RFC 9068 access token `typ: at+jwt` | ✅ design | oidc/C4 |
| RFC 9068 required claims (`iss`, `sub`, `aud`, `exp`, `iat`, `jti`, `client_id`, `scope`) | ✅ design | oidc/C3 |
| `jti` revocation via denylist | ✅ schema | oidc/C3; `revoked_jti` |
| Refresh tokens single-use rotation + reuse detection | ⚠️ deferred (v0.4) | family-revocation logic |
| Refresh tokens stored server-side (opaque) | ✅ design | KV-backed |
| Access tokens short-lived (≤ 15 min) | ✅ design | `configx.OIDC.AccessTokenTTL` default 10m |
| Refresh tokens 30 day default | ✅ design | `configx.OIDC.RefreshTokenTTL` |
| `offline_access` scope gates refresh issuance (OIDC Core §11) | ⚠️ deferred (v0.4) | oidc/R3 |
| argon2id hashing for `client_secret_hash` | ✅ design | `golang.org/x/crypto/argon2` |
| `token_endpoint_auth_method` (`client_secret_basic` default, `none` for public) | ✅ schema | oidc/R1; `oidc_client.token_endpoint_auth_method` |
| `id_token_signed_response_alg` per client | ✅ schema | oidc/R1 |
| `subject_type` (`public` / `pairwise`) | ✅ schema | oidc/R1 |
| `application_type` (`web` / `native`) | ✅ schema | oidc/R1 |
| `default_max_age` / `require_auth_time` per client | ✅ schema | oidc/R1 |
| `contacts` / `logo_uri` / `tos_uri` / `policy_uri` | ✅ schema | oidc/R1 |
| Token introspection (RFC 7662) — `active`, `sub`, `scope`, `client_id`, `exp` | ⚠️ deferred (v0.4) | oidc/R6; endpoint stubbed |
| Token revocation (RFC 7009) | ✅ schema; ⚠️ endpoint v0.4 | `revoked_jti` writes |
| Pushed Authorization Requests (PAR, RFC 9126) | ⚠️ deferred (v0.7+) | not required for v1 first-party clients |
| JAR (RFC 9101) | ⚠️ deferred (v0.7+) | same |
| DPoP (RFC 9449) sender-constrained tokens | ⚠️ deferred (v0.7+) | bearer for v1 |
| mTLS (RFC 8705) | ⚠️ deferred (v0.7+) | bearer for v1 |
| Dynamic client registration (RFC 7591) | ⚠️ deferred (v0.7+) | static config in v0.x |
| Pairwise sub identifiers | ⚠️ deferred (v0.7+) | `subject_type='pairwise'` column reserved |
| Encrypted ID tokens (JWE) | ⚠️ deferred | TLS provides confidentiality on the wire |
| RP-Initiated Logout 1.0 | ⚠️ deferred (v0.4) | `oidc/C1` schema; endpoint stubbed |
| Front-channel / back-channel logout | ⚠️ deferred (v0.7+) | multi-RP coordinated sign-out |
| Mix-up attack resistance | ✅ design | `iss` param (RFC 9207) + federation state snapshots |
| Refresh-token family forensics table | ⚠️ deferred (v0.4) | oidc/R7; KV-only for v0.4 |
| Rate limit on `/oauth/authorize` and `/oauth/token` | ❌ gap | flagged by audit-oidc.md indirectly; tracked for v0.4 |

## SAML IdP (SAML 2.0 Core / Bindings / Metadata / Profiles)

| Item | Status | Notes / source |
|---|---|---|
| SP registry with entity ID, NameID format, attribute map | ✅ schema | `saml_sp` |
| Multi-endpoint ACS (Metadata §2.4.4) | ✅ schema | saml/C1; `saml_sp_acs` child table |
| ACS URL validated by exact match → index lookup → is_default | ✅ design | saml/C1; spec §"SAML assertion construction" |
| Multiple SP signing/encryption certs per SP (rotation-friendly) | ✅ schema | saml/C3; `saml_sp_key (sp_id, use)` |
| `require_signed_authn_request` per SP | ✅ schema | saml/C3; `saml_sp.require_signed_authn_request` |
| `want_assertions_signed` / `authn_requests_signed` mirror SP metadata | ✅ schema | saml/R4 |
| Both `<Response>` and `<Assertion>` signed | ✅ design | saml/GHES-1; spec §"SAML assertion construction" |
| `Destination` on `<Response>` = chosen ACS URL | ✅ design | saml/GHES-2 |
| `<SubjectConfirmationData Recipient>` = chosen ACS URL | ✅ design | Profiles §4.1.4.2 |
| `<Audience>` = `saml_sp.entity_id` verbatim | ✅ design | saml/C2 |
| Stable pairwise NameID (Core §8.3.7) | ✅ schema | saml/C5; `saml_subject_id (account_id, sp_id)` |
| Persistent 1.1-namespace NameID default | ✅ schema | saml/C4; `saml_sp.name_id_format` default `urn:oasis:names:tc:SAML:1.1:nameid-format:persistent` |
| Attribute map as ordered JSONB array (multi + URI NameFormat) | ✅ schema | saml/R1; `saml_sp.attribute_map jsonb` |
| Per-SP `session_lifetime` for `SessionNotOnOrAfter` | ✅ schema | saml/GHES-8 |
| Metadata freshness fields (`metadata_*`) | ✅ schema | saml/R3 |
| `AuthnContextClassRef` per spec (`PasswordProtectedTransport` / `unspecified`) | ✅ design | saml/R5 |
| IdP metadata publishes all live + grace-period signing keys | ✅ design | saml/R6; `configx.SAML.MetadataRotationGrace` |
| GHES `sp_kind='ghes'` auto-sets `require_signed_authn_request=true` | ✅ design | saml/GHES-10 |
| GHES `emails` / `public_keys` / `gpg_keys` multi-valued | ✅ schema | saml/GHES-6; `attribute_map.multi=true` |
| GHES `public_keys` URI NameFormat support | ✅ schema | saml/GHES-7; `attribute_map.name_format='uri'` |
| GHES `administrator` attribute literal | ✅ design | saml/GHES-5; documented in INTEGRATION.md |
| XML signature wrapping (XSW) defense | ✅ planned | crewjam/saml post-canonicalization verification; v0.5 |
| Assertion replay (NotBefore / NotOnOrAfter / InResponseTo / one-use Assertion ID) | ✅ planned | crewjam/saml enforces; v0.5 |
| `saml_session` populated from day one for SLO forward-compat | ✅ schema | spec §"db/migrations/005_saml.sql" |
| Single Logout (SLO) endpoint | ⚠️ deferred (v0.5) | `/saml/slo` stubbed |
| SLO endpoint binding child table (`saml_sp_slo`) | ⚠️ deferred (v0.5) | saml/R2 |
| IdP-initiated SSO | ⚠️ out of scope | saml/Optional |
| AttributeQuery / NameIDMapping / Artifact binding | ⚠️ out of scope | saml/Optional |
| `default_relay_state` per SP (only if IdP-initiated lands) | ⚠️ out of scope | saml/Optional |
| Encrypted assertions (`saml_sp_key.use='encryption'`) | ⚠️ deferred (v0.7+) | schema room ready |
| Implementation | ⚠️ deferred (v0.5) | `pkg/protocol/saml` stubbed |

## Cryptography

| Item | Status | Notes |
|---|---|---|
| All tokens via `crypto/rand` | ✅ | 32 bytes session / enrollment, 16 bytes pairing id, 64 bytes WebAuthn user handle |
| Pairing code: rejection-sampled, unambiguous alphabet, ~40 bits | ✅ | 8 chars from 30-char alphabet |
| JWT signing: RS256 (2048-bit RSA) | ✅ design | asymmetric, widely supported |
| Unified `signing_key` for OIDC + SAML (use sig|enc, kid rotation) | ✅ schema | spec §"db/migrations/002_oidc.sql"; oidc/R4 |
| Key rotation: insert new, flip active, retire old after grace | ✅ design | `signing_key.not_before` + `retired_at` |
| `not_before` on signing keys (oidc/R4) | ✅ schema | `signing_key.not_before` |
| AES-256-GCM at rest with versioned DEK | ✅ design | credentials/C3; `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>` |
| AAD binds ciphertext to row identity | ✅ design | credentials/C4 |
| 12-byte per-row nonce, unique per row | ✅ design | NIST SP 800-38D §5 |
| argon2id PHC for password / recovery / client_secret hashes | ✅ design | credentials/R5 + credentials/C2 |
| HSM / KMS integration | ⚠️ deferred (v0.7+) | private keys in DB column |
| TLS termination | external | reverse-proxy responsibility; Prohibitorum sets `Secure` cookie when TLS detected |
| Time skew tolerance on JWT verification | ✅ design | 30s leeway on `exp` / `iat` / `nbf` |

## Operational

| Item | Status | Notes |
|---|---|---|
| Forward-only migrations via goose | ✅ | embedded `.sql` files; goose installation quirk documented in STATUS.md |
| Structured audit logs via `credential_event` | ✅ schema | credentials/New tables; `pkg/audit.Writer` wired into `server.New`; handler usage lands v0.2 |
| Audit-log fields: who, what, when, IP, UA, detail | ✅ schema | `credential_event.{account_id, factor, event, credential_ref, ip, user_agent, detail jsonb, at}` |
| Session manager for end users (`/me/sessions`) | ✅ | carried from v0.1 skeleton |
| Admin can revoke other-user sessions | ✅ | `/accounts/revoke-sessions` |
| Live `account.disabled` check per request | ✅ | `session.LoadSession` middleware |
| Sudo mode for sensitive actions | ✅ | `pkg/server/handle_sudo.go` |
| Rate limit on auth-sensitive endpoints (`/auth/*`) | ✅ | `pkg/authn/ratelimit` |
| OpenAPI spec for management API | ✅ | huma-generated |
| Admin UI for accounts | ⚠️ deferred (v0.6) | dashboard scaffold empty in v0.1 |
| Admin UI for OIDC clients / SAML SPs / upstream IdPs | ⚠️ deferred (v0.6) | manage via SQL until then |
| Consent screen | ⚠️ deferred | first-party-only deployments don't need it |
| Audit-log export / SIEM | ⚠️ deferred (v0.7+) | append-only PG table for now |
| Versioned DEK rotation procedure documented | ✅ | spec §"DEK compromise / rotation" |

## Web (frontend, v0.6)

| Item | Status | Notes |
|---|---|---|
| Passkey ceremony popup with focus trap + Esc + backdrop-click | ⚠️ deferred (v0.6) | dashboard not yet ported |
| `AbortController` on `navigator.credentials.{create,get}` | ⚠️ deferred (v0.6) | same |
| Body scroll lock during ceremony | ⚠️ deferred (v0.6) | same |
| WCAG 2.1.2 No Keyboard Trap | ⚠️ deferred (v0.6) | same |
| Concurrent ceremony preemption | ⚠️ deferred (v0.6) | SDK aborts prior |
| Method-selection login UX | ⚠️ deferred (v0.6) | WebAuthn vs password+TOTP vs federation |
| CSRF on state-changing `/me/*` | ✅ | `SameSite=Lax` session cookie + same-origin |
| Conditional UI (passkey autofill) | ⚠️ deferred (v0.7+) | identifier-less login |
| Content Security Policy (CSP) | ❌ gap | reverse-proxy responsibility for v0.x; bake into static handler in v0.7+ |
| HSTS, X-Frame-Options, etc. | ❌ gap | same |

## Threats this codebase does NOT protect against (v0.x)

- **HSM-tier private key protection.** RSA private keys live in
  `signing_key.private_pem`. A DB compromise = a complete IdP
  compromise (signing + DEK-encrypted secrets). Move to KMS-backed
  signing (AWS KMS / GCP KMS / Vault Transit) for production — v0.7+.
- **Loss of all DEK versions.** TOTP secrets and upstream-OIDC
  client secrets become undecryptable; users must re-enroll TOTP and
  re-link upstream IdPs. Operator responsibility: keep at least two
  consecutive `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>` versions
  available during rotation.
- **Insider abuse via direct DB access.** A SQL operator can grant
  themselves any role / attributes, mint sessions, or extract signing
  keys. Standard IdP threat — mitigate with DB access controls +
  audit-log monitoring.
- **Sustained credential-stuffing against `/oauth/token`.** Rate
  limiting on OIDC endpoints is gap-flagged above; reverse-proxy WAF
  is the short-term mitigation.
- **Phishing of federated upstream credentials.** Prohibitorum can
  only validate the assertion the upstream IdP returns; it doesn't
  control how the upstream IdP authenticates the user. Pick upstream
  IdPs whose phishing-resistance matches your threat model.
- **Compromise of an SP signing cert (Pattern C).** If a SAML SP's
  signing cert leaks, an attacker can forge AuthnRequests from that
  SP — but they can't forge our signed `<Response>` without our
  signing key, so the blast radius is "spoof the SP's identity to us"
  rather than "log in as a user."

Each gap is tracked in `STATUS.md` with a target version.
