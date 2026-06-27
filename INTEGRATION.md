# Integrating with Prohibitorum

How a relying party (RP) — backend service, SPA, or legacy SaaS — authenticates its users against Prohibitorum.

## Pick a pattern

| Pattern | When | Trust assumption |
|---|---|---|
| **A. OIDC Authorization Code + PKCE** | Any RP with a back-end; mobile apps with a server; CLI tools using a loopback redirect | Standard. Strongest. Start here. |
| **B. Cookie + Introspection** | First-party RP co-located behind the same reverse proxy as Prohibitorum (shared parent domain) | Acceptable for co-located first-party RPs only. |
| **C. SAML 2.0 SP** | Legacy SaaS / on-prem apps that only speak SAML (GitHub Enterprise Server, older Atlassian, etc.) | Use when SAML is the only option. |

**Start with A** — the largest library ecosystem, and you stop worrying about session theft, cookie domains, and revocation propagation.

---

## Pattern A — OIDC Authorization Code + PKCE

The full OP surface (v0.4, smoke-verified) is mounted at the **issuer root**, NOT under `/api/prohibitorum`: `/oauth/authorize`, `/oauth/token`, `/oauth/userinfo`, `/oauth/introspect`, `/oauth/revoke`, `/oidc/logout`, `/oauth/jwks`, `/.well-known/openid-configuration`. Copy-pasteable curl examples in "OIDC OP (v0.4)" below.

### One-time setup

Register a client with `prohibitorum oidc-client create` (it generates + argon2id-hashes the secret, printing it once — see "OIDC OP (v0.4)"). The admin dashboard also manages OIDC clients. Raw SQL remains possible for advanced cases:

```sql
INSERT INTO oidc_client
  (client_id, client_secret_hash, display_name, redirect_uris,
   post_logout_redirect_uris, allowed_scopes,
   require_pkce, allowed_code_challenge_methods,
   token_endpoint_auth_method, id_token_signed_response_alg,
   subject_type, application_type)
VALUES (
  'gateway-prod',
  -- public client: NULL secret_hash; PKCE is mandatory anyway
  NULL,
  'Gateway (prod)',
  ARRAY['https://gateway.example.com/auth/callback'],
  ARRAY['https://gateway.example.com/'],
  ARRAY['openid', 'profile'],
  true,
  ARRAY['S256'],
  'none',
  'RS256',
  'public',
  'web'
);
```

For confidential (back-end-only) clients: generate a strong random secret, hash with argon2id (PHC format) into `client_secret_hash`, and set `token_endpoint_auth_method` to `'client_secret_basic'`.

### The flow

```
RP ────────────────────────────────────────► Prohibitorum
 │                                              │
 │ 1. Browser-initiated:                        │
 │    302 → /authorize?                         │
 │        client_id=...                         │
 │       &response_type=code                    │
 │       &scope=openid profile                  │
 │       &redirect_uri=https://...              │
 │       &state=<random,csrf>                   │
 │       &code_challenge=<base64url(sha256)>    │
 │       &code_challenge_method=S256            │
 │       &nonce=<random>                        │
 │                                              │
 │   ◄────────────────── (login if needed; user picks a method)
 │                                              │
 │ 2. 302 → redirect_uri?code=...&state=...&iss=https://auth.example.com
 │                                              │
 │ 3. Back-end POST /token                      │
 │      grant_type=authorization_code           │
 │      code=...                                │
 │      redirect_uri=https://... (verbatim)     │
 │      code_verifier=<random>                  │
 │      client_id=gateway-prod                  │
 │      [client_secret if confidential]         │
 │                                              │
 │   ◄── { id_token, access_token, refresh_token, token_type: "Bearer" }
 │                                              │
 │ 4. Validate id_token:                        │
 │      - Fetch JWKS (cache by kid).            │
 │      - Verify signature with alg=RS256.      │
 │      - Verify iss = configured issuer.       │
 │      - Verify aud = client_id.               │
 │      - Verify exp > now.                     │
 │      - Verify nonce = the value sent in (1). │
 │                                              │
 │ 5. Establish your own RP session from the    │
 │    id_token's claims.                        │
```

### Claims in the ID token

```json
{
  "iss": "https://auth.example.com",
  "sub": "42",
  "aud": "gateway-prod",
  "exp": 1769950000,
  "iat": 1769949400,
  "auth_time": 1769949390,
  "amr": ["hwk", "user"],
  "acr": "urn:mace:incommon:iap:silver",
  "nonce": "<echoed back from /authorize>",
  "username": "alice",
  "displayName": "Alice Smith",
  "role": "admin",
  "attributes": {
    "department": "platform",
    "can_admin_models": true
  }
}
```

- `sub` is the **stable** account id. Key your local user table on it.
- `attributes` is an opaque, RP-defined map carried verbatim from `account.attributes`; Prohibitorum does not interpret it.
- `amr` reflects the factors used: `hwk` (WebAuthn), `pwd`+`otp`+`mfa` (password+TOTP), `fed` (federated OIDC).
- `auth_time` is the authentication moment; RPs that requested `max_age=N` must verify `now - auth_time <= N`.
- **Scope-gated claims.** `username` / `displayName` / `role` / `attributes` emit only under the `profile` scope; `email` / `email_verified` only under `email`, and only when the account has an address (verified upstream → `email_verified:true`; admin-set → `false`). Both blocks appear identically at `/oauth/userinfo`. The OP advertises `openid`, `profile`, `email`, `offline_access` in `scopes_supported` and **rejects** any OIDC client whose `allowed_scopes` contains anything outside that set.

### Forced re-authentication: `prompt=login` / `max_age` (v0.6)

- **`prompt=login`** — the IdP refuses to issue from the existing session and bounces the browser to login; the issued `id_token`'s `auth_time` reflects the fresh authentication. A stale session cannot satisfy it (the mechanism is a single-use freshness nonce keyed on the request).
- **`max_age=N`** — if `now - session.auth_time > N` the IdP re-authenticates before issuing. `max_age=0` always re-authenticates.
- **`prompt=none`** — if (re-)authentication would be required, returns `error=login_required` on the redirect rather than prompting.
- **`prompt=login` + `prompt=none`** is `invalid_request`.

Use these when you need guaranteed-fresh authentication (e.g. before a sensitive operation), then validate the returned `auth_time`.

### PKCE policy: S256 only (v0.6)

PKCE is mandatory and **S256-only**. `code_challenge_method=plain` is rejected with `invalid_request` (a DB-level CHECK enforces it, per OAuth 2.1 / RFC 9700). Always send `code_challenge_method=S256`. Per-client policy (`require_pkce`, `allowed_code_challenge_methods`) is consulted at `/oauth/authorize`.

### Access token shape (RFC 9068)

Access tokens are signed JWTs with `typ: at+jwt`; resource servers MUST reject any other `typ`. Claims: `iss`, `sub`, `aud`, `exp`, `iat`, `jti`, `client_id`, `scope`, plus `auth_time` / `amr` / `acr` when available. `jti` is mintable per-token; revocation writes to `revoked_jti`. Self-validating resource servers should consult `/oauth/introspect` (or a `revoked_jti` snapshot) when a token's identity matters more than its claims.

### Library recommendations

- Go RP: `github.com/zitadel/oidc/v3/pkg/client`. (Any conformant OIDC client library works.)
- Node RP: `openid-client`.
- Python RP: `authlib`.
- Browser-only SPA: don't. Always have a thin back-end that holds the refresh token.

### Discovery

```
GET /.well-known/openid-configuration
```

Returns endpoint URLs, supported algorithms/scopes, and `authorization_response_iss_parameter_supported=true`. Your client library reads this once at startup and caches it.

---

## Pattern B — Cookie + Introspection

For first-party RPs co-located with Prohibitorum (same parent domain): share the session cookie and let RP back-ends look the user up per-request via introspection.

> **What actually ships (v0.4).** `/oauth/introspect` (RFC 7662) introspects **OAuth access/refresh tokens this OP issued**, with per-client ownership (a client sees only its own tokens). It does NOT introspect raw session cookies; the `token_type_hint=session` shape below was a v0.1 design sketch, retained only as the conceptual shape — treat its concrete fields as illustrative. The supported pattern for a first-party RP: run Pattern A, hold the issued access token, and introspect THAT.
>
> **v0.6 — public clients cannot introspect.** A public (`none`-auth) client calling `/oauth/introspect` gets `invalid_client` (401), per RFC 7662 §2.1 (the caller is a resource server and must authenticate). Use a confidential client / resource-server credential. (Behavior change from v0.4, which let a public client introspect its own tokens.) Revocation (RFC 7009) is unchanged — a public client may still `/oauth/revoke` its own tokens.

```
Browser ──cookie──► RP back-end ──cookie──► Prohibitorum
                       │
                       │  POST /oauth/introspect
                       │       token=<the session cookie value>
                       │       token_type_hint=session
                       │  Authorization: Basic <client_id:client_secret>
                       │
                       ◄── { active: true, sub, username, role,
                             attributes, ... }
```

The RP back-end caches the introspection response for a short interval (seconds) and rejects requests when `active=false`.

**Caveats:**
- Cookie must be sent → same parent domain required.
- The cookie is bearer-equivalent within that domain; XSS on either side is total compromise.
- Use Pattern A if these aren't acceptable.

---

## Pattern C — SAML 2.0 SP

Prohibitorum is a live SAML 2.0 IdP (v0.5 + v0.6, smoke-verified) with a GHES-compatible profile. The SP redirects the user to `/saml/sso`; Prohibitorum authenticates (reusing an existing session if present), then auto-POSTs a signed SAML Response to the SP's ACS URL. **Scope:** SP-initiated SSO + IdP-initiated SSO (opt-in) + IdP-local SLO + metadata + the `saml-sp` CLI. **Not shipped:** front-channel (multi-SP) SLO, and assertion / NameID encryption.

### IdP coordinates the SP needs

All URLs derive from `configx` `PublicOrigins[0]` (`$ISSUER` — same origin as the OIDC issuer):

| Field | Value |
|---|---|
| IdP `entityID` | `saml.entity_id` if configured, else `$ISSUER` (= `PublicOrigins[0]`). A stable identifier SPs key trust on — need not be reachable; pinning it lets the EntityID survive an origin change. Endpoint URLs below always derive from `$ISSUER`, never from this. |
| IdP metadata | `$ISSUER/saml/metadata` (signed as of v0.6; `validUntil`/`cacheDuration` present) |
| SSO URL (SingleSignOnService) | `$ISSUER/saml/sso` (HTTP-Redirect + HTTP-POST) |
| SLO URL (SingleLogoutService) | `$ISSUER/saml/slo` (HTTP-Redirect + HTTP-POST) |
| IdP-initiated launcher (v0.6, opt-in) | `$ISSUER/saml/sso/init?sp=<entity_id>&RelayState=<target>` (requires `allow_idp_initiated`) |
| NameID format | `urn:oasis:names:tc:SAML:1.1:nameid-format:persistent` (stable, opaque, per `(account, sp)`; IdP default `saml.default_nameid_format`, overridable per-SP) |
| Signing | `<Response>` AND `<Assertion>` both signed, RSA-SHA256; the IdP's signing cert is published in `/saml/metadata` |
| `WantAuthnRequestsSigned` | `true` |

Point the SP at `$ISSUER/saml/metadata` to import all of the above at once.

### One-time setup — `saml-sp create` CLI (recommended)

The preferred path ingests the SP's own metadata, auto-populating `entity_id`, the AssertionConsumerService endpoint(s), and the signing certificate(s) used to verify the SP's signed AuthnRequests:

```bash
# Ingest the SP's metadata from a file …
prohibitorum saml-sp create \
  --kind ghes \
  --display-name 'GitHub Enterprise (prod)' \
  --metadata-file ./ghes-sp-metadata.xml

# … or fetch it from the SP's metadata URL:
prohibitorum saml-sp create \
  --kind ghes \
  --display-name 'GitHub Enterprise (prod)' \
  --metadata-url https://ghes.example.com/saml/metadata
```

`--kind ghes` installs the GHES attribute profile (USERNAME, administrator, emails, public_keys, gpg_keys — see below) and **forces `require_signed_authn_request=true`**. `--kind generic` (default) is a minimal NameID-only map.

If the SP has no metadata document, register it manually with `--entity-id` and at least one `--acs-url`:

```bash
prohibitorum saml-sp create \
  --kind ghes \
  --entity-id 'https://ghes.example.com' \
  --acs-url 'https://ghes.example.com/saml/consume' \
  --acs-binding 'urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST'
```

Other flags: `--name-id-format` (default SAML 1.1 persistent), `--want-assertions-signed` (default true), `--require-signed-authn-request` (auto-true for `--kind ghes`), `--allow-idp-initiated` (default false; see "IdP-initiated SSO"). Explicit flags override metadata-parsed values. List SPs with `prohibitorum saml-sp list`.

> **Verifying the SP's signed AuthnRequests.** Prohibitorum verifies SP signatures **only** against the cert(s) in `saml_sp_key` (ingested from metadata or loaded manually) — never a cert embedded in the incoming message. Load the SP's signing cert (via `--metadata-*` or the `saml_sp_key` insert below) or signed AuthnRequests are rejected.

### One-time setup — raw SQL (low-level reference)

Equivalent to the CLI, for advanced cases or DB scripting. Covers `saml_sp` (entity ID, NameID format, attribute map, signing requirements), `saml_sp_acs` (one or more AssertionConsumerService endpoints — a child table because SAML Metadata §2.4.4 permits >1), and `saml_sp_key` (the SP's signing certificate(s)).

```sql
INSERT INTO saml_sp (entity_id, display_name, sp_kind, name_id_format, name_id_claim, attribute_map, require_signed_authn_request)
VALUES (
  'https://ghes.example.com',
  'GitHub Enterprise (prod)',
  'ghes',
  'urn:oasis:names:tc:SAML:1.1:nameid-format:persistent',
  'sub',  -- NOTE: `name_id_claim` is stored but the v0.5 IdP does NOT read it to compute the NameID.
          -- The NameID is a stable opaque *persistent* identifier generated per (account, sp) and
          -- kept in `saml_subject_id` — it is not derived from this column.
  -- This is exactly the ordered JSONB array that `saml-sp create --kind ghes`
  -- installs (see pkg/protocol/saml/attributes.go). Field shape:
  -- {name, name_format, source, multi}; source names an `account` column or an
  -- `account.attributes` JSONB key. NameFormat URIs: basic =
  -- urn:oasis:names:tc:SAML:2.0:attrname-format:basic, uri = …attrname-format:uri.
  '[
    {"name":"USERNAME","name_format":"urn:oasis:names:tc:SAML:2.0:attrname-format:basic","source":"username","multi":false},
    {"name":"administrator","name_format":"urn:oasis:names:tc:SAML:2.0:attrname-format:basic","source":"attributes.administrator","multi":false},
    {"name":"emails","name_format":"urn:oasis:names:tc:SAML:2.0:attrname-format:basic","source":"attributes.emails","multi":true},
    {"name":"urn:oid:1.2.840.113549.1.1.1","name_format":"urn:oasis:names:tc:SAML:2.0:attrname-format:uri","source":"attributes.public_keys","multi":true},
    {"name":"gpg_keys","name_format":"urn:oasis:names:tc:SAML:2.0:attrname-format:basic","source":"attributes.gpg_keys","multi":true}
  ]'::jsonb,
  true
);

INSERT INTO saml_sp_acs (sp_id, idx, binding, location, is_default) VALUES
  ((SELECT id FROM saml_sp WHERE entity_id='https://ghes.example.com'), 0,
   'urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST',
   'https://ghes.example.com/saml/consume', true);

INSERT INTO saml_sp_key (sp_id, use, cert_pem) VALUES
  ((SELECT id FROM saml_sp WHERE entity_id='https://ghes.example.com'), 'signing',
   '-----BEGIN CERTIFICATE-----...-----END CERTIFICATE-----');
```

### GHES-specific notes

GHES is opinionated. Common ways to get it wrong:

1. **Persistent NameID, 1.1 namespace URI.** GHES expects `urn:oasis:names:tc:SAML:1.1:nameid-format:persistent`, not the 2.0 form. The `saml_sp.name_id_format` default is correct.
2. **NameID stability.** GHES re-links accounts when the NameID changes. Prohibitorum generates an opaque 32-byte value on first SSO, persists it in `saml_subject_id(account_id, sp_id)`, and reuses it forever — immune to rename / email change.
3. **Signed AuthnRequests required.** GHES self-signs every AuthnRequest with a 10-year cert. Set `require_signed_authn_request = true` (auto-true when `sp_kind='ghes'`) and load the SP's cert into `saml_sp_key`.
4. **Sign both `<Response>` and `<Assertion>`.** GHES validates the `Destination` attribute only when the `<Response>` is signed. Prohibitorum always signs both.
5. **GHES attribute profile.** `--kind ghes` installs the ordered map in the SQL above: `USERNAME` (basic), `administrator` (basic), `emails` (basic, multi), `public_keys` (`Name="urn:oid:1.2.840.113549.1.1.1"`, URI NameFormat, multi), `gpg_keys` (basic, multi). Multi-valued fields (`multi:true`) emit one `<AttributeValue>` per array element. Sources are `account` columns (`username`) or `account.attributes` JSONB keys.
6. **`administrator` attribute is fixed.** GHES does not allow renaming it; emitted literally as `administrator` (basic NameFormat), only as the single value `"true"` when `role=='admin'` or `attributes.administrator` is truthy (omitted otherwise).
7. **SessionNotOnOrAfter is honored** by GHES. Set `saml_sp.session_lifetime` for a per-SP override; NULL = IdP default `configx.SAML.SessionLifetime`.
8. **Entity ID and ACS URL format.** GHES uses `entity_id = https://HOSTNAME` and `ACS = https://HOSTNAME/saml/consume`.

Full audit-traced list: `docs/superpowers/specs/2026-05-24-audit-saml.md` "GHES-specific call-outs".

### Discovery

```
GET /saml/metadata
```

Returns the IdP `<EntityDescriptor>` XML: an `<IDPSSODescriptor>` with a `<KeyDescriptor use="signing">` per non-retired signing cert, the `SingleSignOnService` and `SingleLogoutService` endpoints (HTTP-Redirect + HTTP-POST), the persistent-1.1 `NameIDFormat`, and `WantAuthnRequestsSigned="true"`. SPs import it once; during key rotation the old cert keeps publishing until its grace window elapses, so SPs that re-fetch see both keys (`configx.SAML.MetadataRotationGrace`, default 7d).

As of **v0.6** the `<EntityDescriptor>` is **signed** (with the active key) and carries `validUntil` + `cacheDuration` (`configx.SAML.MetadataValidity`), so SPs can integrity-check and cache-bound it. With no active signing key it fails open and serves an unsigned descriptor rather than erroring. The metadata also re-advertises the **HTTP-POST** SSO binding (see "SSO binding").

### SAML SSO behaviors (v0.6)

None require SP-side config beyond what is noted:

- **`ForceAuthn`.** `ForceAuthn="true"` makes the IdP re-authenticate even if a session exists; the assertion's `AuthnInstant` reflects the fresh authentication. (A stale session cannot satisfy it — the freshness check is request-scoped.) `ForceAuthn` + `IsPassive` both true → a `NoPassive` status Response (IsPassive wins, no assertion), per OASIS SAML Core.
- **`NameIDPolicy/@Format`.** Request **`urn:oasis:names:tc:SAML:1.1:nameid-format:persistent`** (the configured format), **`urn:oasis:names:tc:SAML:2.0:nameid-format:unspecified`**, or omit `NameIDPolicy/@Format` entirely — all proceed to a normal assertion. Any other concrete Format (e.g. `emailAddress`) the IdP cannot produce returns a status Response with `InvalidNameIDPolicy` and NO assertion. GHES uses `persistent` (the default), so it is unaffected.
- **SSO binding: POST and Redirect both work.** `/saml/sso` accepts HTTP-Redirect (DEFLATE + signed query string) and **HTTP-POST** (form `SAMLRequest`, base64, **enveloped** signature). Pick whichever your SP emits.

### IdP-initiated SSO (v0.6, opt-in)

Launch a session into an SP without an inbound AuthnRequest (app-launcher / dashboard-tile style). **Off by default** (GHES-style posture); enable per SP:

```bash
prohibitorum saml-sp create \
  --kind ghes \
  --display-name 'GitHub Enterprise (prod)' \
  --metadata-file ./ghes-sp-metadata.xml \
  --allow-idp-initiated
```

(To enable on an existing SP, delete and re-register with `--allow-idp-initiated` — there is no `saml-sp update`.) Launcher URL:

```
GET $ISSUER/saml/sso/init?sp=<SP entity_id>&RelayState=<target>
```

- The IdP emits an **unsolicited** signed `<Response>` (no `InResponseTo`) auto-POSTed to the SP's **registered default ACS** — never a request-supplied location (open-redirect guard).
- **`RelayState` passes through verbatim** as the SP's deep-link / target (the Okta / AWS convention).
- An SP without `allow_idp_initiated=true` → **403**.
- Rate-limited per-account + per-SP; audited with `reason=idp_initiated`.

> **Security note.** IdP-initiated SSO is inherently more exposed to login-CSRF / replay than SP-initiated (an unsolicited Response has no `InResponseTo`; SAML Profiles §4.1.5). Mitigations: short assertion validity window, per-(account,SP) SessionIndex, AudienceRestriction, delivery only to the registered default ACS, and the default-off opt-in. Enable it only for SPs that need it.

### Single Logout (SLO)

The SP sends a signed `LogoutRequest` to `$ISSUER/saml/slo` (HTTP-Redirect or HTTP-POST). Prohibitorum verifies the signature against the SP's `saml_sp_key` cert, resolves the `NameID` to the `saml_session` bound at SSO time, revokes **that** Prohibitorum session, and returns a signed `LogoutResponse` to the SP's `SingleLogoutService` response location (from stored metadata; request-supplied locations are never trusted). If the SP was registered without metadata (no known SLO endpoint), the IdP returns a 200 `text/xml` `LogoutResponse` directly.

This is **IdP-local** logout: it terminates the one Prohibitorum session tied to that `saml_session`. It does NOT propagate a front-channel logout to the user's other SPs — coordinated multi-SP sign-out is a later version.

---

## Password + TOTP (v0.2)

The fallback method for users whose devices don't support passkeys, or the legacy path for users in transition. Any account with both a `password_credential` row and a confirmed `totp_credential` row can use this two-step flow; recovery codes substitute for TOTP when the authenticator app is lost.

WebAuthn is **preferred**. Accounts with a passkey should remove password + TOTP via `POST /me/auth/revoke-password-totp` once the passkey works — not auto-enforced (the user might want the fallback during a trial), but the endpoint exists so the dashboard can offer it.

Examples below assume `http://localhost:8080` and the public API prefix `/api/prohibitorum`.

### Two-step login: password → TOTP

```bash
# Step 1 — verify password, receive partial-session token.
curl -X POST http://localhost:8080/api/prohibitorum/auth/password/begin \
  -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"correct horse battery staple"}'
# 200 OK
# { "partial_session_token": "<43-char base64url>" }
# (KV-backed, single-use, 5-minute TTL, no IP/UA binding.)

# Step 2a — verify TOTP, get session cookie.
curl -X POST http://localhost:8080/api/prohibitorum/auth/totp/verify \
  -H 'Content-Type: application/json' \
  -c cookies.txt \
  -d '{"partial_session_token":"<token>","code":"123456"}'
# 204 No Content
# Set-Cookie: prohibitorum_session=...; HttpOnly; SameSite=Lax
```

The session from step 2a carries `amr=["pwd","otp","mfa"]` (v0.4 ID tokens project this).

### Recovery ceremony: password → recovery code → forced TOTP re-enrollment

When the authenticator app is unavailable, recovery is a **three-step ceremony**, not a one-shot login. A recovery code never issues a session directly; it grants a narrow-scope token (10-min TTL) the user must redeem by re-enrolling TOTP. NIST SP 800-63B-4 §5.2 forbids a knowledge factor for reauthentication, which is why a recovery code cannot mint a session or sudo grant directly.

**Step 1.** `/auth/password/begin` exactly as normal login (above) — returns a `partial_session_token`.

**Step 2.** Redeem one of the 10 codes from TOTP enrollment:

```bash
curl -X POST http://localhost:8080/api/prohibitorum/auth/recovery-code/verify \
  -H 'Content-Type: application/json' \
  -d '{"partial_session_token":"<token>","code":"ABCD-1234-EFGH-5678"}'
# 200 OK
# { "recovery_session_token": "<token>" }
# NO session cookie set yet.
```

The server marks the code consumed (`used_at`, `used_session_id`, `used_ip`) and stashes a `recovery_session:<tok>` KV entry (10-min TTL). The remaining 9 codes are NOT yet wiped — if the user abandons the ceremony, they can retry with another code.

**Step 3a.** Start a fresh TOTP enrollment:

```bash
curl -X POST http://localhost:8080/api/prohibitorum/auth/recovery/totp/begin \
  -H 'Content-Type: application/json' \
  -d '{"recovery_session_token":"<recovery_session_token>"}'
# 200 OK
# { "secret_base32": "…", "otpauth_uri": "otpauth://totp/…" }
```

The old TOTP credential row is wiped (audit: `totp:revoke reason=recovery`) and a fresh unconfirmed row inserted. The recovery_session_token stays valid for the 10-min window — call `/begin` again if the user fails to scan the QR.

**Step 3b.** Confirm the new TOTP and complete the ceremony:

```bash
curl -X POST http://localhost:8080/api/prohibitorum/auth/recovery/totp/verify \
  -H 'Content-Type: application/json' \
  -c cookies.txt \
  -d '{"recovery_session_token":"<recovery_session_token>","code":"123456"}'
# 200 OK
# { "recovery_codes": [ ... 10 fresh codes ... ] }
# Session cookie set; amr=["pwd","otp","mfa"]
```

**Atomically single-use** (the token is Pop'd at entry). On success: new TOTP confirmed, the 9 surviving old recovery codes wiped (audit: `recovery_code:revoke reason=recovery_complete`), 10 fresh codes minted in the same transaction, user logged in with the same `amr` as a normal Password+TOTP login.

**On TOTP failure:** the token is already consumed, so the user must restart from `/auth/password/begin`. This harsher UX is deliberate — keeping a one-shot token live across a failed verify would require an atomicity-hazardous re-stash.

When all 10 codes are eventually consumed via this flow, the user (or an admin) calls `/me/recovery-codes/regenerate` (sudo-gated; below).

Auth failures at either step increment the per-`(account, factor)` row in `auth_throttle` with exponential backoff `[0,0,1s,2s,4s,8s,16s,32s,1m,2m,4m,8m,15m]`. A locked row returns `429 Too Many Requests` with `Retry-After: <seconds>`, and the expensive crypto check is skipped — no oracle for "is this account currently locked?"

### Setting a password

```bash
# Sudo step-up first (see "Sudo step-up" below). Then:
curl -X POST http://localhost:8080/api/prohibitorum/me/password/set \
  -H 'Content-Type: application/json' \
  -b cookies.txt \
  -d '{"password":"correct horse battery staple"}'
# 204 No Content
```

argon2id-hashes with current `PasswordHashParams`, stamps `password_changed_at`. Idempotent — calling again replaces the hash.

### Enrolling TOTP

```bash
# Step 1 — server mints secret + otpauth URI.
# (No sudo when no confirmed totp_credential exists. Sudo required if
# the caller is replacing an existing confirmed credential.)
curl -X POST http://localhost:8080/api/prohibitorum/me/totp/begin \
  -H 'Content-Type: application/json' \
  -b cookies.txt -d '{}'
# 200 OK
# {
#   "secret_base32": "JBSWY3DPEHPK3PXP",
#   "otpauth_uri": "otpauth://totp/Prohibitorum:alice?secret=JBSWY3DPEHPK3PXP&issuer=Prohibitorum&algorithm=SHA1&digits=6&period=30"
# }
# Frontend renders the otpauth URI as a QR code; user scans with
# Google Authenticator / 1Password / etc. and produces a 6-digit code.

# Step 2 — confirm the credential, receive recovery codes.
curl -X POST http://localhost:8080/api/prohibitorum/me/totp/verify \
  -H 'Content-Type: application/json' \
  -b cookies.txt \
  -d '{"code":"123456"}'
# 200 OK
# {
#   "recovery_codes": [
#     "ABCD-1234-EFGH-5678", ... // 10 codes, shown ONCE
#   ]
# }
```

Stamps `totp_credential.confirmed_at` and mints 10 recovery codes in the same transaction. The plaintext codes are returned in this response body and never again — the user must save them before dismissing the dialog.

If `/me/totp/verify` returns 401 (wrong code), the unconfirmed row remains and a fresh `/me/totp/begin` UPSERTs a new secret.

### Regenerating recovery codes

```bash
# Sudo step-up first. Then:
curl -X POST http://localhost:8080/api/prohibitorum/me/recovery-codes/regenerate \
  -H 'Content-Type: application/json' \
  -b cookies.txt -d '{}'
# 200 OK
# { "recovery_codes": [ ... 10 fresh codes ... ] }
# The prior set is invalidated.
```

### Revoking the password + TOTP fallback

Call this once a passkey is confidently working. Transactionally deletes `password_credential`, `totp_credential`, and all `recovery_code` rows for the account.

```bash
# Sudo step-up first. Then:
curl -X POST http://localhost:8080/api/prohibitorum/me/auth/revoke-password-totp \
  -H 'Content-Type: application/json' \
  -b cookies.txt -d '{}'
# 204 No Content
# Subsequent /auth/password/begin for this account returns 401.
```

## Sudo step-up

Sensitive `/me/*` actions (set password, regenerate recovery codes, revoke fallback factors) require a recent credential proof. Sudo accepts **three** methods — pick whichever the account has: `webauthn`, `password_totp`, and `federation_oidc` (forced upstream re-authentication, for accounts with a linked upstream IdP — including federated-only users with no passkey or password).

> **Note (2026-05-28 hardening).** `recovery_code` is intentionally NOT a sudo method. Recovery codes route exclusively through the ceremony at `/auth/recovery/totp/{begin,verify}` (see "Recovery ceremony"). NIST SP 800-63B-4 §5.2 forbids knowledge factors for reauthentication.

```bash
# Discover available methods + the linked providers offerable for OIDC sudo:
curl http://localhost:8080/api/prohibitorum/me/sudo/methods -b cookies.txt
# 200 OK
# {
#   "methods": ["webauthn", "password_totp", "federation_oidc"],
#   "federationProviders": [{ "slug": "google", "displayName": "Google" }]
# }
# federationProviders lists only the caller's linked, ENABLED upstream IdPs;
# it is [] when the account has none (and federation_oidc is then absent).
```

### Sudo via WebAuthn

```bash
curl -X POST http://localhost:8080/api/prohibitorum/me/sudo/begin \
  -H 'Content-Type: application/json' \
  -b cookies.txt -d '{"method":"webauthn"}'
# 200 OK — returns publicKey assertion options
# (Frontend runs navigator.credentials.get and POSTs the result back.)

curl -X POST http://localhost:8080/api/prohibitorum/me/sudo/complete \
  -H 'Content-Type: application/json' \
  -b cookies.txt \
  -d '<the AuthenticatorAssertionResponse as JSON>'
# 204 No Content; session.sudo_until extended
```

### Sudo via password + TOTP

```bash
curl -X POST http://localhost:8080/api/prohibitorum/me/sudo/begin \
  -H 'Content-Type: application/json' \
  -b cookies.txt -d '{"method":"password_totp"}'
# 204 No Content; the server has stashed the intent

curl -X POST http://localhost:8080/api/prohibitorum/me/sudo/complete \
  -H 'Content-Type: application/json' \
  -b cookies.txt \
  -d '{"current_password":"...","totp_code":"123456"}'
# 204 No Content; session.sudo_until extended
```

`/me/sudo/begin` is rate-limited to 10 requests per minute per session; `/me/sudo/complete` runs through the credential's `auth_throttle` row, so wrong codes burn the exponential-backoff curve like `/auth/totp/verify`. Every successful sudo emits a `credential_event` row with `factor='session'`, `event='sudo_granted'`.

## Upstream OIDC Federation (v0.3)

Federate sign-in to an upstream OIDC provider (Google Workspace, Okta, Keycloak, another Prohibitorum). Prohibitorum is the RP; the upstream is the OP. All three provisioning modes (`auto_provision`, `link_only`, `invite_only` — the last via `GET /enrollments/{token}/start-federation`) ship end-to-end, each smoke-verified against an in-process mock OP.

### One-time setup (admin)

Register an upstream IdP via the admin dashboard (Identity Providers) or raw SQL. The client secret must be sealed with the helper in `pkg/federation/oidc/secret.go` — do not paste plaintext into the DB.

> **At the upstream**, register both Prohibitorum redirect_uris in the upstream's OAuth client:
> - `…/api/prohibitorum/auth/federation/{slug}/callback`    (sign-in)
> - `…/api/prohibitorum/me/identities/link/{slug}/callback` (account linking)

```sql
-- Pseudocode: the sealed (enc, nonce, key_version) triple comes from
-- oidc.SealClientSecret(plaintext, idpID, dekVersion). In practice this
-- is done from a short Go program or admin CLI, not raw SQL.
INSERT INTO upstream_idp
  (slug, display_name, issuer_url,
   client_id, client_secret_enc, secret_nonce, key_version,
   scopes, mode, require_verified_email, allowed_domains)
VALUES (
  'google',
  'Google Workspace',
  'https://accounts.google.com',
  '1234567890.apps.googleusercontent.com',
  '\xCIPHERTEXT'::bytea,
  '\xNONCE12'::bytea,
  1,
  ARRAY['openid','email','profile'],
  'auto_provision',
  true,
  ARRAY['example.com']
);
```

Modes:

| `mode`            | Behavior on first-seen `(iss, sub)`                              |
|---|---|
| `auto_provision`  | Create a local account from upstream claims, gated by `require_verified_email`, `allowed_domains`, and a username-collision check |
| `link_only`       | Reject with `403 link_required` — the user must link from a session they already hold |
| `invite_only`     | Reject `/auth/federation/{slug}/login` directly. Accept the user only when they arrive bearing an admin-minted invite token via `/enrollments/{token}/start-federation`. See "Invite redemption". |

### Login flow

```bash
# 1. User clicks "Sign in with Google" → redirect to:
curl -i 'http://localhost:8080/api/prohibitorum/auth/federation/google/login?return_to=/dashboard'
# 302 Found
# Location: https://accounts.google.com/o/oauth2/v2/auth?client_id=...&code_challenge=...&state=...&nonce=...

# 2. User authenticates with Google. Google bounces back to:
#    http://localhost:8080/api/prohibitorum/auth/federation/google/callback
#      ?code=...&state=...&iss=https://accounts.google.com
# Prohibitorum exchanges the code, validates the ID token, resolves the
# local account, issues a session cookie, and 302s to /dashboard.

# 3. The session cookie carries amr=["federated"] (or the upstream's
# amr claim if present). The session looks identical to any other
# Prohibitorum session from this point forward.
curl http://localhost:8080/api/prohibitorum/me -b cookies.txt
# {
#   "id": 42,
#   "username": "alice",
#   "displayName": "Alice Example",
#   ...
# }
```

`return_to` MUST be a relative path starting with `/` (and not `//`); anything else returns `400 invalid_return_to`.

The federation handlers carry no IP-keyed rate limits (client IP is untrustworthy behind NAT / CDN). Replay / brute-force defense lives in PKCE + single-use KV state tokens (`/callback`) and per-account `auth_throttle` rows once a credential failure occurs against a known account. Edge DoS is the reverse proxy / WAF's responsibility — see AUDIT.md "Rate limiting policy (v0.3 audit)".

### Negative cases

```bash
# Unverified upstream email (and require_verified_email=true):
# → 403 { "code": "email_not_verified" }

# Local-username collision:
# → 403 { "code": "username_collision" }

# Upstream rejected the user (e.g. ?error=access_denied):
# → 400 { "code": "upstream_error", "upstreamCode": "access_denied", "upstreamDescription": "..." }

# Invalid return_to:
curl -i 'http://localhost:8080/api/prohibitorum/auth/federation/google/login?return_to=//evil.example/'
# 400 { "code": "invalid_return_to" }

# State token replayed, missing, or cross-namespace (LoginKey/LinkKey):
# → 401 { "code": "federation_state_invalid" }
```

### Invite redemption (`invite_only` mode)

`invite_only` IdPs reject the public `/auth/federation/{slug}/login` entrypoint. Instead, an admin mints a per-user invite bound to a specific IdP, and the user redeems it via a dedicated public endpoint that stashes the invite token in federation state so the callback provisions the account atomically.

```bash
# 1. Admin creates an invite-intent enrollment for the user (via the admin
#    dashboard's Invitations screen or the /admin/enrollments/* API).
#    Required fields:
#      intent='invite'
#      template_username='alice'
#      template_display_name='Alice Example'
#      template_role='user'
#      expected_upstream_idp_slug='google'    -- binds to a specific IdP
#      expires_at = now() + interval '7 days' -- short-lived bearer
#
# 2. Admin shares the invite URL with the prospective user. The URL is
#    a bearer capability — anyone who holds it can redeem it, exactly
#    once, before it expires.
https://idp.example.com/api/prohibitorum/enrollments/<token>/start-federation

# 3. User clicks the URL:
curl -i "https://idp.example.com/api/prohibitorum/enrollments/<token>/start-federation?return_to=/me"
# 302 Found
# Referrer-Policy: no-referrer
# Location: https://accounts.google.com/o/oauth2/v2/auth?...
#
# The Referrer-Policy header keeps the invite token out of the
# upstream's referrer log (defense in depth — the token is also
# short-TTL and single-use by atomic ConsumeEnrollment).

# 4. After Google sign-in completes, callback to:
#    /api/prohibitorum/auth/federation/google/callback?code=...&state=...&iss=...
#    The callback notices the EnrollmentToken on FedState and dispatches
#    applyInviteOnly inside a single pgx transaction:
#      ConsumeEnrollment(token)            -- atomic UPDATE ... WHERE consumed_at IS NULL
#      InsertAccount(template username/role/etc.)
#      InsertAccountIdentity(account_id, upstream_iss, upstream_sub)
#      audit Register/Use (tx-scoped Writer)
#    → 302 to /me + session cookie set.
#
#    After commit: enrollment.consumed_at IS NOT NULL; account 'alice'
#    with role 'user' exists; account_identity links it to the upstream
#    (iss, sub).
```

Failure modes (each returns `403 invite_required` with no upstream hop — the federator collapses every "invite not redeemable" branch onto one opaque code so an attacker can't enumerate state):

```bash
# Token is unknown / consumed / expired / wrong-intent / non-federated
# (intent=invite but no expected_upstream_idp_slug):
curl -i "https://idp.example.com/api/prohibitorum/enrollments/already-redeemed-token/start-federation?return_to=/me"
# 403 { "code": "invite_required" }
```

Mid-flight rejections (after the upstream round-trip) collapse onto the same code but audit with distinct `reason:` fields (`invite_consumed_or_expired`, `invite_slug_mismatch`, `username_collision`) for operators to query.

Notes:

- `applyInviteOnly` skips `RequireVerifiedEmail` + `AllowedDomains` by design — the admin minting the invite for this user IS the authorization decision.
- The invite template overrides upstream claims for `account.username`, `display_name`, and `role`. Upstream `preferred_username` is ignored on this path; upstream `email` is still recorded on the `account_identity` row for the audit trail.
- `expected_upstream_idp_slug` is required for federated invites. An `intent='invite'` enrollment without it belongs to the WebAuthn enrollment flow, and `/start-federation` rejects it as `invite_not_federated`.
- Conversely, a federation-bound invite (i.e. `expected_upstream_idp_slug` set) CANNOT be redeemed via the WebAuthn enrollment path: both `/enrollments/{token}/register/begin` and `/register/complete` reject with `403 enrollment_federation_required`, forcing the invitee through `/start-federation`.

### Listing linked identities

```bash
curl http://localhost:8080/api/prohibitorum/me/identities -b cookies.txt
# 200 OK
# [
#   {
#     "id": 17,
#     "idpSlug": "google",
#     "idpDisplayName": "Google Workspace",
#     "upstreamEmail": "alice@example.com",
#     "linkedAt": "2026-05-29T14:22:08Z"
#   }
# ]
```

`upstreamEmail` is `null` when the upstream OP returned no email claim.

### Linking an additional IdP to an existing account

```bash
# 1. Sudo step-up first (link/begin is sudo-gated):
curl -X POST http://localhost:8080/api/prohibitorum/me/sudo/begin \
  -H 'Content-Type: application/json' \
  -b cookies.txt -d '{"method":"webauthn"}'
# ... (run the WebAuthn ceremony, POST /me/sudo/complete) ...

# 2. Kick off the link flow:
curl -i 'http://localhost:8080/api/prohibitorum/me/identities/link/okta/begin?return_to=/settings/identities' \
  -b cookies.txt
# 302 Found
# Location: https://example.okta.com/oauth2/.../authorize?...

# 3. User authenticates upstream. Okta bounces to:
#    /api/prohibitorum/me/identities/link/okta/callback?code=...&state=...&iss=...
# Prohibitorum validates the state matches the *current session*'s
# account_id (session-swap defense), inserts the account_identity row,
# emits a 'link' audit event, and 302s to /settings/identities.
# IMPORTANT: NO new session is issued — the user remains signed in
# under their original session cookie.
```

The link callback is NOT sudo-gated (a second sudo prompt after the upstream round-trip would force re-elevation in the same flow). The original sudo grant at `/begin` is the load-bearing check.

### Unlinking an identity

```bash
# Sudo step-up first. Then:
curl -X POST http://localhost:8080/api/prohibitorum/me/identities/17/unlink \
  -b cookies.txt
# 204 No Content
```

Refuses (`400 last_sign_in_method`) when the identity being removed is the account's *only remaining* sign-in method — the user would be locked out. Enroll a passkey or password+TOTP first.

### What goes into the audit log

| Event | Emitted by | Notes |
|---|---|---|
| `federation_oidc:register` | first-time provisioning | per fresh `(iss, sub)`. `detail->>'reason'` distinguishes `auto_provision` (implicit; no reason field) from `invite_only_redemption` |
| `federation_oidc:use`      | every successful federated login | re-login claim sync is a `use` event |
| `federation_oidc:fail`     | every structured rejection | `email_not_verified` / `username_collision` / `domain_not_allowed` / `identity_conflict` / `link_required` / `invite_lookup_failed` / `invite_wrong_intent` / `invite_already_consumed` / `invite_expired` / `invite_not_federated` / `invite_required_no_token` / `invite_consumed_or_expired` / `invite_slug_mismatch` / `upstream_error` / `session_swap` / `iss_mismatch_callback` / `token_endpoint_drift` / `code_exchange_failed` / `link_conflict` / `account_disabled` |
| `federation_oidc:link`     | self-service link callback success | written by the federator, not the HTTP handler — do not double-audit |
| `federation_oidc:unlink`   | `POST /me/identities/{id}/unlink` 204 | written by the HTTP handler |

### What Prohibitorum does NOT do for upstream identities

- **Refresh upstream tokens.** Prohibitorum does not store the upstream refresh token. Federated users re-authenticate by hitting `/login` again. There is no `/me/identities/{id}/refresh-profile` endpoint.
- **Upstream sign-out propagation.** Logging out of Prohibitorum does not log the user out of the upstream OP. Back-channel logout is not implemented (unscheduled).

Custom claim-name overrides (`username_claim` / `display_name_claim` / `email_claim` on `upstream_idp`) are honored end-to-end. Defaults are the OIDC standard names (`preferred_username` / `name` / `email`); override only when the upstream ships claims under non-standard keys (e.g. Microsoft Entra ID's `upn`).

## OIDC OP (v0.4)

The concrete, copy-pasteable downstream OP flow. The request shapes below are the exact params, headers, and form fields the server accepts.

Key facts:

- OP endpoints are mounted at the **issuer root**, NOT under `/api/prohibitorum`: `/oauth/authorize`, `/oauth/token`, `/oauth/userinfo`, `/oauth/introspect`, `/oauth/revoke`, `/oidc/logout`, `/oauth/jwks`, `/.well-known/openid-configuration`.
- **PKCE is mandatory and S256-only.** `plain` is rejected; `code_challenge_method` must be `S256`.
- **A refresh token is issued only when the `offline_access` scope is granted.** Without it the token response has no `refresh_token`.
- **Who calls what:** a real RP back-end makes the `/oauth/token`, `/oauth/userinfo`, `/oauth/introspect`, `/oauth/revoke` calls (with its client credentials). `/oauth/authorize` is a **browser** redirect requiring the user to already hold a logged-in Prohibitorum **session cookie** — you cannot drive it with a bare curl unless you attach a valid `prohibitorum_session` cookie (the smoke does this). A no-session `/oauth/authorize` 302s the browser to `Issuer + /login?return_to=<authorize URL>`.

### 0. Provision a signing key and a client (operator, one-time)

```bash
# Mint an RSA-2048 signing key. The first key (or --activate) becomes
# the active key; it is written to the signing_key table and serves at
# /oauth/jwks. Prints "Generated signing key <kid> (active)".
prohibitorum signing-key generate
#   …optionally: --activate (re-activate an existing kid),
#                --retire <kid> (stamp retired_at).

# Register a confidential client. The 32-byte secret is printed ONCE
# (only the argon2id hash is stored). token_endpoint_auth_method is
# client_secret_basic.
prohibitorum oidc-client create \
  --client-id smoke-rp \
  --display-name "Smoke RP" \
  --redirect-uri https://rp.example.com/rp/callback \
  --post-logout-redirect-uri https://rp.example.com/rp/post-logout \
  --scope openid --scope profile --scope offline_access
# Registered confidential client "smoke-rp"
# Client secret (store this now, it will NOT be shown again):
# <secret>
#
#   …--public        → no secret, token_endpoint_auth_method=none, PKCE required
#   …--require-consent → reserved flag; /authorize returns consent_required
#                        (no consent UI until v0.6)

# List clients:
prohibitorum oidc-client list
# client_id   display_name   auth_method            disabled
```

Confirm the key is live:

```bash
curl -s https://auth.example.com/oauth/jwks
# { "keys": [ { "kty":"RSA", "kid":"<thumbprint>", "use":"sig", "alg":"RS256", "n":"…", "e":"AQAB" } ] }
```

### 1. `/oauth/authorize` (browser, session-gated)

The RP redirects the user's browser here. Generate a PKCE pair first: `verifier = base64url(32 random bytes)`, `challenge = base64url(SHA256(verifier))` (no padding).

```
GET https://auth.example.com/oauth/authorize
      ?response_type=code
      &client_id=smoke-rp
      &redirect_uri=https://rp.example.com/rp/callback   (exact match, URL-encoded)
      &scope=openid%20profile%20offline_access
      &state=<random,csrf>
      &nonce=<random>
      &code_challenge=<base64url(sha256(verifier))>
      &code_challenge_method=S256
```

With a valid session cookie attached:

```
302 Found
Location: https://rp.example.com/rp/callback?code=<authcode>&state=<state>&iss=https://auth.example.com
```

`iss` is the RFC 9207 issuer parameter — the RP MUST verify it equals the configured issuer. An **unregistered `redirect_uri` (or unknown client) returns a DIRECT error** (400 `invalid_request`) and never redirects to the unvalidated URI — the open-redirect guard.

### 2. `/oauth/token` — `authorization_code` grant (RP back-end)

`application/x-www-form-urlencoded` body; confidential clients authenticate with **HTTP Basic** (`client_id:client_secret`):

```bash
curl -s -X POST https://auth.example.com/oauth/token \
  -u 'smoke-rp:<client_secret>' \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode grant_type=authorization_code \
  --data-urlencode code=<authcode> \
  --data-urlencode redirect_uri=https://rp.example.com/rp/callback \
  --data-urlencode code_verifier=<the PKCE verifier from step 1>
# 200 OK
# {
#   "access_token": "<RFC 9068 JWT, typ=at+jwt>",
#   "token_type": "Bearer",
#   "expires_in": 600,
#   "id_token": "<OIDC Core JWT>",
#   "refresh_token": "<opaque; present because offline_access was granted>",
#   "scope": "openid profile offline_access"
# }
```

(`client_secret_post` — credentials in the form body — and `none` (public client; PKCE-only) are also accepted; the example uses Basic.)

**Validate the id_token** (the RP does this): fetch `/oauth/jwks`, resolve the key by the token header `kid`, verify the RS256 signature, then check `iss`, `aud == client_id`, `exp > now`, and `nonce ==` the value sent in step 1. The id_token also carries `sub`, `at_hash`, `sid`, `auth_time`, `amr`. The access token is a JWS with JOSE `typ: at+jwt` and a `jti` claim (RFC 9068) — resource servers MUST reject any other `typ`.

### 3. `/oauth/userinfo` (Bearer access token)

```bash
curl -s https://auth.example.com/oauth/userinfo \
  -H 'Authorization: Bearer <access_token>'
# 200 OK
# { "sub": "<uuid, == id_token.sub>", "username": "alice", "displayName": "Alice Smith", ... }
# (profile claims gated by the granted scope; GET or POST both work)
```

A bad / expired / revoked token returns `401` + `WWW-Authenticate: Bearer error="invalid_token"`.

### 4. `/oauth/introspect` (RFC 7662, RP back-end)

Client-authenticated; a client sees only its **own** tokens. Same Basic auth + form-encoded shape as `/oauth/token`:

```bash
curl -s -X POST https://auth.example.com/oauth/introspect \
  -u 'smoke-rp:<client_secret>' \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode token=<access_token>
# 200 OK
# { "active": true, "token_type": "access_token", "client_id": "smoke-rp", "sub": "<uuid>", "scope": "...", "exp": ..., "iat": ... }
```

A revoked / unknown / foreign-owned token returns `{ "active": false }` with no further detail.

### 5. `/oauth/token` — `refresh_token` grant (rotation)

```bash
curl -s -X POST https://auth.example.com/oauth/token \
  -u 'smoke-rp:<client_secret>' \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode grant_type=refresh_token \
  --data-urlencode refresh_token=<refresh_token>
# 200 OK — returns a NEW (rotated) refresh_token, a fresh access_token,
# and a re-issued id_token. The OLD refresh_token is now invalid.
```

**Rotation + reuse detection:** each refresh rotates the token. Replaying a **superseded** refresh token is treated as compromise — it returns `400 invalid_grant` AND revokes the whole family, so even the current (rotated) token is immediately dead. The grant also re-checks the account and rejects if it was disabled (`invalid_grant`).

### 6. `/oauth/revoke` (RFC 7009, RP back-end)

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X POST https://auth.example.com/oauth/revoke \
  -u 'smoke-rp:<client_secret>' \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode token=<access_or_refresh_token>
# 200  (always 200, even for unknown tokens — RFC 7009)
```

Revoking an **access token** writes its `jti` to the `revoked_jti` denylist (so `/userinfo` and `/introspect` reject it immediately; outstanding copies self-expire within `AccessTokenTTL`). Revoking a **refresh token** deletes its family (all descendants die).

### 7. `/oidc/logout` (RP-Initiated Logout 1.0, browser)

```
GET https://auth.example.com/oidc/logout
      ?id_token_hint=<an id_token this OP issued>
      &post_logout_redirect_uri=https://rp.example.com/rp/post-logout   (exact match)
      &state=<optional, echoed back>
```

```
302 Found
Location: https://rp.example.com/rp/post-logout?state=<state>
```

The OP validates the `id_token_hint`'s signature + `iss` (tolerating an expired hint), then **revokes the Prohibitorum session named by the hint's `sid` claim** — the user's IdP session is gone. The `post_logout_redirect_uri` must exactly match one of the client's registered `post_logout_redirect_uris`, or the request is rejected directly (no redirect). Front-/back-channel logout to *other* RPs is not implemented (unscheduled).

### Rate limits (per identity, not per IP)

OP endpoints are rate-limited on **identity**, not client IP (decision D3, consistent with the v0.3 M5 removal of IP-keyed buckets): `/authorize` per `account_id`; `/token` / `/introspect` / `/revoke` per `client_id`; `/userinfo` per `sub`. RPs are machines, so caps are higher than the human-facing auth surfaces. Volumetric / edge DoS remains the reverse-proxy / WAF's responsibility.

### What goes into the audit log

`credential_event` rows are written with **factor `oidc_client`** and a structured `detail->>'reason'`:

| `event` | `reason` | Emitted on |
|---|---|---|
| `use` | `authorize` | every `/oauth/authorize` success |
| `use` | `token_issued` | every `authorization_code` grant |
| `use` | `refresh_rotated` | every successful refresh rotation |
| `use` | `logout` | every `/oidc/logout` |
| `fail` | `refresh_reuse` | a superseded refresh token replayed |
| `fail` | `code_replay` | an authorization code replayed |
| `revoke` | `revoked` | `/oauth/revoke` (access or refresh) |

(plus failure reasons such as `invalid_client` / `invalid_grant` on the respective error paths).

## Logout

Three coordinated paths:

1. **RP logout (OIDC)**: clear the RP's own session, then 302 to `https://auth.example.com/oidc/logout?id_token_hint=...&post_logout_redirect_uri=https://rp.example.com/`. The `post_logout_redirect_uri` must match a value in `oidc_client.post_logout_redirect_uris`. The user is logged out of Prohibitorum and bounced back.
2. **Prohibitorum-initiated logout**: the user clicks "Sign out" on `/me`; Prohibitorum revokes the session. Pattern B: the next RP introspection sees `active: false`. Pattern A: the RP's session continues until it detects the user is gone (typically by the next ID-token refresh failing).
3. **SAML SLO (Pattern C, v0.5)**: a signed `LogoutRequest` to `/saml/slo` revokes the bound Prohibitorum session and returns a signed `LogoutResponse`. This is **IdP-local** — it does NOT propagate a front-channel logout to the user's other SPs (coordinated multi-SP sign-out is unbuilt). See Pattern C → "Single Logout (SLO)".

## What Prohibitorum does NOT do for you

- **Authorization decisions.** Prohibitorum hands you `role` + `attributes` (OIDC) or a SAML AttributeStatement. You decide what those mean.
- **Per-resource access control.** No concept of "user X has read access to project Y" — that's RP-local state.
- **User profile data beyond username + display name + attributes.** It's an identity provider, not a user-profile service.

## End-to-end example

A new internal service integrating via Pattern A:

1. Operator inserts a client row for `myapp-prod`.
2. The app's config: `OIDC_ISSUER=https://auth.example.com`, `OIDC_CLIENT_ID=myapp-prod`, `OIDC_REDIRECT_URI=https://myapp.example.com/auth/callback`.
3. The app replaces in-house session middleware with one that: (a) reads its own RP session cookie; (b) if absent, redirects to `/authorize`; (c) on callback, exchanges the code at `/token`, validates the ID token, mints its own RP session cookie keyed by `sub`.
4. The app keys per-user data on `sub`; it never sees a password.

The app goes from "owns the user directory" to "trusts Prohibitorum for identity, owns its domain-specific data."
