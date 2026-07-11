# Status — capabilities by version

Prohibitorum is a standalone identity provider: four upstream authentication
methods (WebAuthn, password+TOTP, recovery codes, upstream OIDC federation) and
two downstream protocols (OIDC OP, SAML 2.0 IdP), with a self-service + admin
dashboard. This file is the changelog of capabilities each version delivers,
followed by the roadmap.

## v0.1 — rescope + decoupling

The structural foundation: a domain-agnostic identity layer with the schema and
package layout that later versions build on without further migrations.

- Three-layer package layout: `pkg/{account, credential/{webauthn,password,totp,pairing,enrollment}, federation/oidc, session, authn, protocol/{oidc,saml}, audit}`.
- Domain-agnostic data model: `account.attributes` (jsonb) and `enrollment.template_attributes` (jsonb) replace fixed permission columns; admin endpoints gate on `role = 'admin'`, finer checks are per-route attribute inspection. `AccountView` / `EnrollmentTemplate` carry `Attributes map[string]any`.
- Project-agnostic error envelope (`errorx.Error`) and configurable WebAuthn RP display name (default `"Prohibitorum"`).
- `configx` covers OIDC, Federation, TOTP, SAML, password hash params, and versioned at-rest data-encryption keys (`PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>`); multiple key versions load simultaneously and a row's `key_version` selects the decrypt key.
- Migrations:
  - `001_initial.sql` — account, session, webauthn_credential (`user_handle`, `cose_alg`, `uv_initialized`, `clone_warning_at`), enrollment (`template_attributes` + `expected_upstream_idp_slug`), credential_event, auth_throttle.
  - `002_oidc.sql` — `signing_key` (unified, `use sig|enc`, `not_before`); `oidc_client` (`post_logout_redirect_uris`, `allowed_code_challenge_methods`, `token_endpoint_auth_method`, `id_token_signed_response_alg`, `subject_type`, `application_type`, `default_max_age`, `require_auth_time`, `contacts`, `logo_uri`, `tos_uri`, `policy_uri`, `disabled`); `revoked_jti`.
  - `003_password_totp.sql` — `password_credential`, `totp_credential` (`secret_enc` + `secret_nonce` + `key_version` + `last_step`), `recovery_code` (`used_session_id` + `used_ip`).
  - `004_federation.sql` — `upstream_idp` (encrypted `client_secret_enc` + `secret_nonce` + `key_version`, three provisioning modes), `account_identity` keyed `(upstream_iss, upstream_sub)`, forward FK `session.upstream_idp_id`.
  - `005_saml.sql` — `saml_sp` (ordered-array `attribute_map`, `require_signed_authn_request`, metadata-freshness fields, per-SP `session_lifetime`), `saml_sp_acs`, `saml_sp_key`, `saml_subject_id`, `saml_session`.

## v0.2 — password + TOTP

Password + TOTP + recovery-code fallback, plus a sudo step-up that gates
sensitive `/me` operations behind a fresh credential proof.

- Password credential: argon2id PHC at rest with OWASP defaults (`m=64 MiB`, `t=3`, `p=1`); auto re-hash on verify when hash params advance. A package-init dummy hash defeats username enumeration on step 1.
- TOTP: RFC 6238 (SHA-1 / 6-digit / 30s, ±1-step drift); `last_step` defeats same-step replay. Secrets AES-256-GCM with a versioned DEK, AAD bound to `'totp:'||account_id||':'||key_version`. Recovery codes (10/account, 80-bit, `XXXX-XXXX-XXXX-XXXX`, argon2id-hashed) minted at confirmation, regenerable.
- Throttling: exponential backoff per `(account_id, factor)`; a locked row returns `429` + `Retry-After` without running the crypto check, and resets on success.
- Two-step login: `POST /auth/password/begin` returns a single-use 5-min partial-session token; `POST /auth/totp/verify` consumes it and issues a session with `amr=["pwd","otp","mfa"]`. Disabled accounts are rejected after a dummy verify (no timing oracle).
- Recovery ceremony: `/auth/recovery-code/verify` returns a narrow `recovery_session_token` (10-min, separate KV namespace) rather than a session; it is redeemed at `/auth/recovery/totp/{begin,verify}`, which re-enrolls TOTP, wipes the old credential and recovery codes, mints 10 fresh, and issues a session. (NIST SP 800-63B-4 §5.2: knowledge factors are not used for reauth.)
- Sudo step-up: per-account sudo factors enumerated in priority order (`webauthn` → `password_totp`); `recovery_code` is deliberately not a sudo method. `/me/sudo/{begin,complete}` take a `method` discriminator.
- WebAuthn-preferred factor policy: `POST /me/auth/revoke-password-totp` transactionally deletes the caller's password + TOTP + recovery codes (sudo-gated).
- Audit: `credential_event` records `register`/`use`/`fail`/`revoke` across password/TOTP/recovery_code, plus `session:sudo_granted` per sudo completion.

### Endpoints introduced in v0.2

| Method | Path | Notes |
|---|---|---|
| POST | `/api/prohibitorum/auth/password/begin` | step 1 of two-step login |
| POST | `/api/prohibitorum/auth/totp/verify` | step 2: TOTP |
| POST | `/api/prohibitorum/auth/recovery-code/verify` | step 2 of recovery: returns `recovery_session_token` (no session) |
| POST | `/api/prohibitorum/auth/recovery/totp/begin` | recovery: re-enroll TOTP (recovery codes preserved) |
| POST | `/api/prohibitorum/auth/recovery/totp/verify` | recovery: confirm + mint 10 fresh + issue session |
| POST | `/api/prohibitorum/me/password/set` | sudo-gated |
| POST | `/api/prohibitorum/me/totp/begin` | sudo-gated iff confirmed TOTP exists |
| POST | `/api/prohibitorum/me/totp/verify` | confirms enrollment, returns recovery codes |
| POST | `/api/prohibitorum/me/recovery-codes/regenerate` | sudo-gated |
| POST | `/api/prohibitorum/me/auth/revoke-password-totp` | sudo-gated, destructive |
| GET  | `/api/prohibitorum/me/sudo/methods` | enumerate available sudo methods |
| POST | `/api/prohibitorum/me/sudo/begin` | accepts a `method` discriminator |
| POST | `/api/prohibitorum/me/sudo/complete` | dispatches on `method` |

## v0.3 — upstream OIDC federation

Upstream OIDC relying-party support across all three provisioning modes, with
identity linking and at-rest protection of upstream secrets.

- Three provisioning modes:
  - `auto_provision` — JIT account creation gated by `RequireVerifiedEmail` + `AllowedDomains` + `preferred_username` presence + local username-collision check; mints a fresh WebAuthn user handle on JIT.
  - `invite_only` — token-bearing redemption: an admin mints an invite (`intent='invite'` + `expected_upstream_idp_slug`); the user follows `GET /enrollments/{token}/start-federation` to upstream, and the callback atomically consumes the enrollment, mints the account, and inserts the identity in one transaction. Skips email/domain checks by design (the admin-minted invite is the authorization); username collision re-checked at redemption.
  - `link_only` — rejects an unknown `(iss, sub)` with `link_required`.
- Upstream client secrets AES-256-GCM at rest (versioned DEK; AAD `'upstream_idp:'||id||':'||key_version`).
- ID-token alg allowlist (`{RS256, ES256, EdDSA}`) enforced both at the JWT library and re-checked post-decode (defense against a library admitting `HS256`/`none`).
- RFC 9207 `iss` callback validation; federation state keyed per flow with cross-namespace defense and single-use consumption; state snapshots `ExpectedIss` / `ExpectedTokenEndpoint` / `Nonce` / `CodeVerifier` so a mid-flow discovery change can't re-target the user. `ExpectedTokenEndpoint` re-validated at callback (RFC 9700 mix-up resistance).
- Re-login claim sync: `account.display_name` + `account_identity.upstream_email` updated on upstream drift, each conditional on a real diff. Per-IdP claim-name overrides (`username_claim`/`display_name_claim`/`email_claim`) honored across auto-provision, drift sync, link, and invite redemption.
- `/me/identities` flow: list; link-begin is sudo-gated; link-callback validates the current session matches the linking account (session-swap defense) and issues no new session; unlink is sudo-gated and refuses the last remaining sign-in method.
- AMR pass-through with `["federated"]` backfill when upstream omits it (RFC 8176 §2). Disabled-account check post-resolve returns an enumeration-safe error.
- Rate limiting is per-account / per-session (no per-IP buckets; `ClientIP` is untrustworthy behind NAT/CDN); replay/brute-force defense via `auth_throttle` + PKCE + single-use KV state; edge DoS is the proxy/WAF's job.
- Migration `006_federation_v03.sql` adds `upstream_idp.require_verified_email` (default true).

Not implemented: upstream refresh-token storage/refresh — federated accounts re-authenticate via `/login`.

### Endpoints introduced in v0.3

| Method | Path | Notes |
|---|---|---|
| GET | `/api/prohibitorum/auth/federation/{slug}/login` | public; 302 to upstream `/authorize` |
| GET | `/api/prohibitorum/auth/federation/{slug}/callback` | public; handles `?error=`; issues session |
| GET | `/api/prohibitorum/enrollments/{token}/start-federation` | public (token-bearing); validates invite, 302 upstream; `Referrer-Policy: no-referrer` so the token doesn't leak upstream |
| GET | `/api/prohibitorum/me/identities` | session; `[{id, idpSlug, idpDisplayName, upstreamEmail, linkedAt}]` |
| POST | `/api/prohibitorum/me/identities/{id}/unlink` | session + sudo; 204; refuses last sign-in method |
| GET | `/api/prohibitorum/me/identities/link/{slug}/begin` | session + sudo; 302 upstream |
| GET | `/api/prohibitorum/me/identities/link/{slug}/callback` | session (not sudo); validates session==linking account; issues no new session |

## v0.4 — downstream OIDC OP

The first-party OpenID Connect Provider. Routes are root-mounted (not under
`/api/prohibitorum`) because clients expect them at the issuer root.

- Discovery + JWKS: discovery advertises the live surface (`scopes_supported [openid, profile, offline_access]`, introspection/revocation/end-session endpoints, `code_challenge_methods_supported [S256]`, `authorization_response_iss_parameter_supported: true`, `token_endpoint_auth_methods_supported [client_secret_basic, client_secret_post, none]`, the full `claims_supported` set, `id_token_signing_alg_values_supported [RS256]`); `/oauth/jwks` serves the real RSA JWK set from active + cached signing keys.
- `/oauth/authorize`: Authorization Code + PKCE (S256-only); `redirect_uri` exact-match with an open-redirect guard (invalid client / unregistered URI → direct error page, never a redirect to the unvalidated URI); session-gated; 302 with `code`+`state`+`iss` (RFC 9207).
- `/oauth/token`: `authorization_code` (client auth basic/post/none, PKCE verify, single-use code, replay → family revoke) + `refresh_token` (rotation with reuse-detection → family revoke + disabled-account re-check). Access token = RFC 9068 JWT; ID token = OIDC Core JWT with `at_hash`/`sid`/`auth_time`/`amr`; refresh issued only when `offline_access` granted.
- `/oauth/userinfo` (GET+POST): Bearer access-token verification (signature by `kid` + `iss` + `exp` + `typ:at+jwt` + `revoked_jti` denylist), scope-gated claim projection; 401 + `WWW-Authenticate: Bearer error="invalid_token"` on failure.
- `/oauth/introspect` (RFC 7662): client-authenticated, per-client ownership; `{active:false}` with no detail leak.
- `/oauth/revoke` (RFC 7009): client-authenticated, per-client ownership; access → self-pruning `revoked_jti` denylist (TTL=exp), refresh → family revoke; always 200.
- `/oidc/logout` (RP-Initiated Logout 1.0): validates `id_token_hint` signature + `iss` (tolerates expiry), revokes the session named by `sid` (SSO sign-out), exact-match `post_logout_redirect_uri`, 302 with `state`.
- Storage: codes + refresh tokens in KV (codes single-use with replay marker; refresh opaque, rotated, per-family record for reuse detection + family revocation); access tokens stateless RFC 9068 JWTs revoked via `revoked_jti`; ID tokens stateless. `sub` = `account.oidc_subject` (uuid).
- Rate limits keyed on identity, not IP: `/authorize` per account, `/token`/`/introspect`/`/revoke` per client, `/userinfo` per subject.
- Consent: auto-approve once a session exists; per-client `require_consent` honored (`consent_required`) — no consent UI in this version.
- CLIs: `signing-key generate [--activate] [--retire <kid>]` (mints RSA-2048, RFC 7638 `kid`, JWK + self-signed x509 + PKCS#8 PEM); `oidc-client create … [--public] [--require-consent]` (confidential default reveals a 32-byte secret once, stores only the argon2id hash; `--public` → no secret, `none` auth, PKCE required) and `oidc-client list`.

### Endpoints introduced in v0.4

Root-mounted (clients expect them at the issuer root).

| Method | Path | Notes |
|---|---|---|
| GET | `/.well-known/openid-configuration` | discovery |
| GET | `/oauth/jwks` | real RSA JWK set from active+cached keys |
| GET | `/oauth/authorize` | Code + PKCE (S256); session-gated; 302 with `code`+`state`+`iss` |
| POST | `/oauth/token` | `authorization_code` + `refresh_token`; client auth basic/post/none |
| GET / POST | `/oauth/userinfo` | Bearer verify; scope-gated claims |
| POST | `/oauth/introspect` | RFC 7662; client-authenticated; per-client ownership |
| POST | `/oauth/revoke` | RFC 7009; client-authenticated; per-client ownership; always 200 |
| GET | `/oidc/logout` | RP-Initiated Logout 1.0; `id_token_hint` + exact-match `post_logout_redirect_uri` |

## v0.5 — downstream SAML IdP

A SAML 2.0 IdP with a GHES-compatible profile. Routes are root-mounted (SPs
expect the IdP at the issuer root).

- IdP metadata: every non-retired signing cert as a `KeyDescriptor`, SSO + SLO endpoints under HTTP-Redirect + HTTP-POST, persistent-1.1 NameIDFormat, `WantAuthnRequestsSigned=true`.
- SP-initiated SSO: parse + validate an inbound HTTP-Redirect AuthnRequest, session-gate, auto-POST a signed Response + Assertion to the SP ACS. Both signed RSA-SHA256 / exclusive C14N. `Destination`/`Recipient`/`AudienceRestriction`/`InResponseTo` set per the chosen ACS and SP entity ID.
- IdP-local SLO: validate a signed LogoutRequest (Redirect + POST parse), revoke the bound session, delete `saml_session` rows, return a signed LogoutResponse. No front-channel propagation to other SPs.
- Stable opaque NameID per `(account, sp)`: 32-byte `crypto/rand` base64url generated on first SSO, reused forever; default `urn:oasis:names:tc:SAML:1.1:nameid-format:persistent`.
- GHES attribute profile: an ordered JSONB `attribute_map` projects an account to `USERNAME`, `administrator` (`"true"` only when `role=='admin'` or `attributes.administrator` truthy), `emails`, `public_keys` (`urn:oid:1.2.840.113549.1.1.1`, URI NameFormat), and `gpg_keys`. Non-GHES SPs get a minimal default map.
- Hardened XML/DSig: DTD/XXE-off parsing with duplicate-ID rejection, SHA-256-only verify (SHA-1 rejected on signature + digest), XSW defense (the signature reference must resolve to the processed element's own ID), exclusive-C14N, 10 MB DEFLATE bound. SP signatures verified against the registered cert only, never a cert embedded in the message.
- Signing-key reuse: the same active `signing_key` RSA key + `x509_cert_pem` that signs OIDC tokens signs SAML — no separate SAML key infrastructure.
- IdP `entityID` = `PublicOrigins[0]`; endpoints `…/saml/{metadata,sso,slo}`.
- Per-identity rate limiting (per-SP / per-account, not per-IP).
- CLI: `saml-sp create --metadata-file <path>` / `--metadata-url <url>` ingests SP metadata (auto-populating entity ID, ACS, certs), or set `--entity-id` + `--acs-url` manually; `--kind ghes` installs the GHES profile and forces `require_signed_authn_request=true`; `saml-sp list`.

### Endpoints introduced in v0.5

Root-mounted (SPs expect the IdP at the issuer root).

| Method | Path | Notes |
|---|---|---|
| GET | `/saml/metadata` | `EntityDescriptor` (all non-retired certs, SSO/SLO bindings, persistent-1.1 NameIDFormat, `WantAuthnRequestsSigned=true`) |
| GET / POST | `/saml/sso` | SP-initiated SSO; parse+validate AuthnRequest, session-gate, signed Response+Assertion auto-POSTed to ACS |
| GET / POST | `/saml/slo` | IdP-local SLO; validate signed LogoutRequest, revoke the bound session, signed LogoutResponse |

## v0.6 — protocol completeness

Closes the deferred OIDC OP + SAML IdP behaviors: cross-protocol forced
re-authentication, OIDC PKCE-method policy + introspection client-auth, SAML
`NameIDPolicy/@Format` honoring, SAML IdP-initiated SSO, POST-binding AuthnRequest
intake, and signed metadata.

- OIDC forced re-auth: `/oauth/authorize` honors `prompt=login`, `max_age`, and `prompt=none`. The mechanism is a full fresh re-login plus a single-use KV nonce — on demand it stamps a demand instant under the nonce, embeds it in `/login?return_to=…&reauth=<nonce>`, and on return requires the marker present and `session.auth_time >= demand_instant`. A stale session can't satisfy `prompt=login`. `prompt=none` + demand → `login_required` (no bounce); `prompt=login`+`prompt=none` → `invalid_request`; `max_age=0` always demands.
- OIDC PKCE method policy: consults per-client `require_pkce` (if true, `code_challenge` mandatory) + `allowed_code_challenge_methods`; `plain` is forbidden entirely by a DB CHECK (OAuth 2.1 / RFC 9700).
- OIDC introspection requires a confidential client: a public (`none`-auth) client → `invalid_client` (401) per RFC 7662 §2.1. Public clients may still revoke their own tokens (RFC 7009).
- SAML `ForceAuthn`: triggers the same re-auth bounce + single-use nonce; the assertion's `AuthnInstant` reflects the fresh `auth_time`. `ForceAuthn` + `IsPassive` → `NoPassive`, no assertion (IsPassive wins, OASIS normative).
- SAML `NameIDPolicy/@Format`: a concrete requested format the IdP can't produce → `InvalidNameIDPolicy`, no assertion; `unspecified`/absent/matching → a normal assertion.
- SAML POST-binding AuthnRequest: `POST /saml/sso` accepts an enveloped-signed AuthnRequest verified against the registered SP key; the POST SSO binding is re-advertised in metadata.
- SAML signed metadata: the `EntityDescriptor` is signed and carries `validUntil` + `cacheDuration` from `configx.SAML.MetadataValidity`; fails open to unsigned if no active signing key.
- SAML IdP-initiated SSO: `GET /saml/sso/init?sp=<entity_id>&RelayState=<deep-link>` emits an unsolicited Response (no `InResponseTo`) to the SP's default ACS, gated by per-SP `allow_idp_initiated` (default false; non-opted-in → 403); `RelayState` passed through verbatim; rate-limited per-account + per-SP. `saml-sp create --allow-idp-initiated` opts an SP in.

### Endpoints changed in v0.6

| Method | Path | Change |
|---|---|---|
| GET | `/oauth/authorize` | honor `prompt=login`/`prompt=none`/`max_age`; consult `require_pkce` + `allowed_code_challenge_methods` |
| POST | `/oauth/introspect` | reject public (`none`-auth) clients → `invalid_client` 401 |
| GET / POST | `/saml/sso` | POST-binding intake; honor `ForceAuthn`; honor `NameIDPolicy/@Format` |
| GET | `/saml/metadata` | sign the `EntityDescriptor` + `validUntil`/`cacheDuration`; re-advertise POST SSO binding |
| GET | `/saml/sso/init` | new — IdP-initiated SSO app-launcher |

Schema: `saml_sp.allow_idp_initiated boolean NOT NULL DEFAULT false`;
`configx.SAML.MetadataValidity`.

### Known limitations

- `require_pkce=false` + no `code_challenge` cannot complete token exchange (the PKCE verifier rejects an empty challenge); affects only non-default clients (default is `require_pkce=true`).
- A SLO POST LogoutRequest with a non-SHA256/non-SHA1 sig alg maps to 500 instead of 400 (still rejects).
- `ForceAuthn` + POST-binding AuthnRequest fails safe with an error (the login bounce rebuilds `return_to` from the query string, which lacks the POST body).
- `oidc-client create --public` requires a `--post-logout-redirect-uri` (the public path otherwise violates a NOT NULL constraint).

Not implemented: assertion/NameID encryption; front-channel multi-SP SLO propagation.

## v0.6 — frontend dashboard

The web dashboard (Vue 3 + Vite + Tailwind v4).

- Passkey ceremony SDK with `PasskeyPopupHost`, `SessionsCard`, `PairApproveDialog`, `PairingCode` / `PairingCodeInput`.
- `LoginView` with method selection (WebAuthn / password+TOTP / federation) and `?return_to=` to post the user back to `/oauth/authorize` after sign-in.
- `EnrollView`, `MeView` (attributes + linked IdPs + passkeys + password/TOTP setup), `AccountsView`, `RecoverChoiceView`, `AdminRecoveryView`, `CodeLoginView`.
- `ClientsView` (OIDC clients), `IdPsView` (upstream OIDC), `SPsView` (SAML).

## Admin Management API

A full HTTP API for administering OIDC clients, SAML SPs, upstream IdPs, signing
keys, audit events, and account credentials. All handlers are under
`/api/prohibitorum` (admin-role gated); high-impact mutations (secrets, PKI,
credentials, destructive actions) are additionally fresh-sudo gated via a
single chokepoint enforcing admin auth + fresh sudo + 64 KiB body limit + JSON
content-type. Lower-impact reversible mutations (SAML CRUD, group management,
app-access grants) use an admin-only body-control wrapper (no sudo) per
`api.md`.

- OIDC client CRUD: create (secret revealed once, argon2id hash stored), update, rotate-secret (new secret once), delete. Reads never expose the hash/cleartext.
- SAML SP CRUD: create (optional metadata XML ingestion), update, reingest-metadata, delete.
- Upstream IdP CRUD: create (AES-GCM seal after insert; a crash mid-create leaves a fail-closed row), update (excludes secret), rotate-secret, delete. Reads never expose encrypted bytes.
- Signing-key lifecycle: generate (→ `pending`), activate (demotes the prior `active` → `decommissioning`, promotes the target), retire. `status` ∈ {pending, active, decommissioning, retired}, with a partial unique index allowing one active key per use. The publish set for JWKS + SAML metadata is pending+active+decommissioning, so prior-key tokens still verify during the grace period. A background reconcile loop advances decommissioning → retired once `retire_after` passes.
- Audit-events viewer: `GET /audit-events` with `factor`/`event`/`accountId`/`since`/`until` filters + keyset pagination. Every admin mutation writes a `credential_event` (no secret/key material in `detail`).
- Account credentials admin view: `GET /accounts/{id}/credentials` returns the passkey list with only the last-4 suffix of the credential ID; `POST /accounts/credentials/delete` force-revokes a passkey (sudo-gated).
- CLI parity: `signing-key {generate,activate,retire}`, `oidc-client {update,rotate-secret,delete}`, `saml-sp {update,delete}`, `upstream-idp {create,list,update,rotate-secret,delete}` share the same domain path as the HTTP handlers.
- API documentation in `api.md`: full route table, gate notation, reveal-once semantics, signing-key lifecycle states, known caveats.

### Endpoints introduced

See `api.md` for the authoritative table. Summary:

| Method | Path | Gate |
|--------|------|------|
| GET | `/api/prohibitorum/oidc-applications` | 🔓 |
| GET | `/api/prohibitorum/oidc-applications/{clientId}` | 🔓 |
| POST | `/api/prohibitorum/oidc-applications` | 🔐 |
| PUT | `/api/prohibitorum/oidc-applications/{clientId}` | 🔐 |
| POST | `/api/prohibitorum/oidc-applications/rotate-secret` | 🔐 |
| POST | `/api/prohibitorum/oidc-applications/delete` | 🔐 |
| GET | `/api/prohibitorum/saml-applications` | 🔓 |
| GET | `/api/prohibitorum/saml-applications/{id}` | 🔓 |
| POST | `/api/prohibitorum/saml-applications` | 🔓 |
| PUT | `/api/prohibitorum/saml-applications/{id}` | 🔓 |
| POST | `/api/prohibitorum/saml-applications/{id}/reingest-metadata` | 🔓 |
| POST | `/api/prohibitorum/saml-applications/delete` | 🔓 |
| GET | `/api/prohibitorum/identity-providers` | 🔓 |
| GET | `/api/prohibitorum/identity-providers/{slug}` | 🔓 |
| POST | `/api/prohibitorum/identity-providers` | 🔐 |
| PUT | `/api/prohibitorum/identity-providers/{slug}` | 🔐 |
| POST | `/api/prohibitorum/identity-providers/rotate-secret` | 🔐 |
| POST | `/api/prohibitorum/identity-providers/delete` | 🔐 |
| GET | `/api/prohibitorum/signing-keys` | 🔓 |
| POST | `/api/prohibitorum/signing-keys/generate` | 🔐 |
| POST | `/api/prohibitorum/signing-keys/{kid}/activate` | 🔐 |
| POST | `/api/prohibitorum/signing-keys/{kid}/retire` | 🔐 |
| GET | `/api/prohibitorum/audit-events` | 🔓 |
| GET | `/api/prohibitorum/accounts/{id}/credentials` | 🔓 |
| POST | `/api/prohibitorum/accounts/credentials/delete` | 🔐 |

### Known caveats

- Key-cache lag (multi-replica): a cache invalidation runs on the mutating replica; others pick up a new/activated key within the 5-min cache TTL. The reconcile loop also doesn't invalidate the cache, so an already-non-signing key can linger in JWKS slightly past its `retire_after` (in the safe direction).
- Upstream IdP crash mid-create: insert-then-seal-then-update means a crash between insert and seal leaves a placeholder secret that decrypts to a failure (fails closed); cleanup is best-effort.

## v0.7 — RBAC app authorization

A coarse per-app access gate plus first-class groups. An admin marks an OIDC
client / SAML SP `access_restricted` and controls sign-in via groups and/or
individual accounts; exposed groups additionally flow downstream as an OIDC
`groups` claim / SAML `groups` attribute. No admin bypass. The IdP gates whether
you may obtain a token/assertion at all; the RP still gates in-app policy from
claims.

- Admin API: groups CRUD + membership; per-app `set-restricted` / `grant` / `revoke` + a combined `GET …/access`; `accessRestricted` in app detail views. GETs 🔓, mutations 🔓 (admin-only body-controlled, no sudo) + audited.
- Schema (`015_rbac.sql`): `user_group`, `group_member`, `oidc_client_access`, `saml_sp_access` (each grant points at exactly one of group/account, enforced by a CHECK + partial unique indexes), and `access_restricted boolean NOT NULL DEFAULT false` on `oidc_client` + `saml_sp` (every existing app stays open).
- Authorization predicate: one query per protocol (`NOT access_restricted OR direct grant OR via-group grant`).
- Dashboard: `/admin/groups` list + detail (edit, exposed toggle, member management), a reusable per-app Access card (restrict toggle + group/account grants) on both app detail pages, and a group-membership card on account detail.
- Enforcement: gate at OIDC `/authorize`, re-checked at the refresh-token grant (denial revokes the family → `invalid_grant`), and at SAML SSO (SP- + IdP-initiated). Denied interactive → IdP `/error?reason=app_access_denied`; OIDC `prompt=none` → `access_denied` to the RP; SAML passive → `RequestDenied`. Denials write an `access_denied` event.
- Group exposure: two-level opt-in — `exposed_to_downstream` (default true) on the group AND a per-app ask via the OIDC `groups` scope (sorted claim in id_token + `/userinfo`, present-but-empty `[]`) or a SAML attribute-map `source: "groups"` entry (multi-valued, omitted when empty).
- CLI: `group create|list|update|delete|add-member|remove-member`; `access` subcommands on `oidc-client`/`saml-sp` (`--access-restricted`, grant/revoke).

### Endpoints introduced in v0.7

See `api.md` → *Groups (RBAC)* and *Per-app access (RBAC)*. Group CRUD +
membership under `/groups`, per-app access under
`/{oidc-applications,saml-applications}/{id}/access{,/set-restricted,/grant,/revoke}`,
and `GET /accounts/{id}/groups`. The `groups` scope is advertised in OIDC
discovery `scopes_supported`.

### Known gaps

- End-user app launchpad is out of scope; the authorization predicate is the query it will reuse.

## Roadmap

The full IdP shipped through v0.7. What remains is optional production hardening, a
compliance gap, and demand-driven features. None are scheduled.

- Planned: HSM/KMS-backed signing (AWS KMS / GCP KMS / Vault Transit, so the key never leaves the vault) to defend a combined DB + environment compromise. Keys are DEK-sealed at rest today.
- Planned: password breach-list check (NIST SP 800-63B-4 §5.1.1.2; HIBP k-anonymity or a static blocklist) for the password fallback.
- Planned: coordinated single sign-out (OIDC front-/back-channel logout, SAML front-channel multi-SP SLO). Sign-out is IdP-local only today.
- Planned: audit-log export / SIEM integration. The append-only `credential_event` table is the only sink today.

Conditional on external demand (tracked in `AUDIT.md`): DPoP / PAR / JAR / mTLS,
SAML assertion/NameID encryption, pairwise `sub`. Permanent non-goals: dynamic
client registration and upstream refresh-token storage.
