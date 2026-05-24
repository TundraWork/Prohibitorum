# Integrating with Prohibitorum

How a relying party (RP) — a backend service, SPA, or legacy SaaS app —
uses Prohibitorum to authenticate its users.

## Pick a pattern

| Pattern | When | Trust assumption |
|---|---|---|
| **A. OIDC Authorization Code + PKCE** | Any RP with a back-end; mobile apps with a server; CLI tools using a loopback redirect | Standard. Strongest. Start here. |
| **B. Cookie + Introspection** | First-party RP co-located behind the same reverse proxy as Prohibitorum (shared parent domain) | Acceptable for co-located first-party RPs only. |
| **C. SAML 2.0 SP** | Legacy SaaS / on-prem apps that only speak SAML (GitHub Enterprise Server, older Atlassian, etc.) | Use when SAML is the only option. |

For new integrations, **start with A**. The library ecosystem is huge,
and you stop having to think about session theft, cookie domains, and
revocation propagation.

Pattern C is delivered in v0.5 — the schema and route table land in
v0.1, but `/saml/sso` returns 501 until the implementation arrives.
Documented here so SP-side configuration can be planned in parallel.

---

## Pattern A — OIDC Authorization Code + PKCE

### One-time setup

Register a client. In v0.1–v0.6 this is a SQL insert (no admin UI yet):

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

For confidential clients (back-end-only), generate a strong random
secret, hash with argon2id (PHC format), and store in
`client_secret_hash`; set `token_endpoint_auth_method` to
`'client_secret_basic'`.

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
- `attributes` is an opaque, RP-defined map carried verbatim from
  `account.attributes`. Prohibitorum does not interpret it. RPs decide
  which keys are meaningful.
- `amr` reflects the actual factors used: `hwk` for WebAuthn,
  `pwd`+`otp`+`mfa` for password+TOTP, `fed` for federated OIDC.
- `auth_time` is the moment of authentication; RPs that requested
  `max_age=N` should verify `now - auth_time <= N`.

### Access token shape (RFC 9068)

Access tokens are signed JWTs with `typ: at+jwt`. Resource servers MUST
reject any other `typ`. Claims include `iss`, `sub`, `aud`, `exp`,
`iat`, `jti`, `client_id`, `scope`. `auth_time` / `amr` / `acr` are
carried when available. `jti` is mintable per-token; revocation writes
to `revoked_jti`. Self-validating resource servers should consult
`/oauth/introspect` (or pull a `revoked_jti` snapshot) when a token's
identity matters more than its claims.

### Library recommendations

- Go RP: `github.com/zitadel/oidc/v3/pkg/client` — same library Prohibitorum uses on the OP side.
- Node RP: `openid-client`.
- Python RP: `authlib`.
- Browser-only SPA: don't. Always have a thin back-end that holds the refresh token.

### Discovery

```
GET /.well-known/openid-configuration
```

Returns endpoint URLs, supported algorithms, supported scopes, and
`authorization_response_iss_parameter_supported=true`. Your OIDC client
library reads this once at startup and caches it.

---

## Pattern B — Cookie + Introspection

For first-party RPs co-located with Prohibitorum (same parent domain),
the simpler integration is to share the session cookie and let RP
back-ends look the user up per-request via introspection.

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

The RP's back-end caches the introspection response for a short interval
(seconds) and rejects requests when `active=false`.

**Caveats:**
- Cookie must be sent → same parent domain required.
- The cookie is bearer-equivalent within that domain; XSS on either
  side is a total compromise.
- Use Pattern A if these aren't acceptable.

---

## Pattern C — SAML 2.0 SP

For SPs that only speak SAML — GHES is the canonical case. Prohibitorum
acts as the SAML IdP; the SP redirects the user to `/saml/sso`,
Prohibitorum authenticates the user, then POSTs a signed SAML Response
to the SP's ACS URL.

### One-time setup

Register the SP via SQL inserts. The shape covers:

- `saml_sp` — the SP's metadata: entity ID, NameID format, attribute
  map, signing requirements.
- `saml_sp_acs` — one or more AssertionConsumerService endpoints (a
  child table because SAML Metadata §2.4.4 permits >1).
- `saml_sp_key` — the SP's signing certificate(s), used to verify
  signed AuthnRequests.

### GHES example

```sql
INSERT INTO saml_sp (entity_id, display_name, sp_kind, name_id_format, name_id_claim, attribute_map, require_signed_authn_request)
VALUES (
  'https://ghes.example.com',
  'GitHub Enterprise (prod)',
  'ghes',
  'urn:oasis:names:tc:SAML:1.1:nameid-format:persistent',
  'sub',
  '[
    {"local":"username","name":"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name","friendly_name":"username","name_format":"basic","multi":false},
    {"local":"email","name":"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress","friendly_name":"emails","name_format":"basic","multi":true},
    {"local":"full_name","name":"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/displayname","friendly_name":"full_name","name_format":"basic","multi":false},
    {"local":"is_admin","name":"administrator","friendly_name":"administrator","name_format":"basic","multi":false},
    {"local":"public_keys","name":"urn:oid:1.2.840.113549.1.1.1","friendly_name":"public_keys","name_format":"uri","multi":true},
    {"local":"gpg_keys","name":"gpg_keys","friendly_name":"gpg_keys","name_format":"basic","multi":true}
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

GHES is opinionated. Get these wrong and you'll spend an afternoon in
the SAML response inspector:

1. **Persistent NameID, 1.1 namespace URI.** GHES expects
   `urn:oasis:names:tc:SAML:1.1:nameid-format:persistent`, not the
   2.0 form. The default in `saml_sp.name_id_format` is correct.
2. **NameID stability.** GHES re-links accounts when the NameID
   changes. Prohibitorum generates an opaque 32-byte value on first
   SSO and persists it in `saml_subject_id(account_id, sp_id)` — it
   is reused forever, immune to rename / email change.
3. **Signed AuthnRequests required.** GHES self-signs every
   AuthnRequest with a 10-year cert. Set
   `require_signed_authn_request = true` (auto-true when
   `sp_kind='ghes'`) and load the SP's cert into `saml_sp_key`.
4. **Sign both `<Response>` and `<Assertion>`.** GHES only validates
   the `Destination` attribute when the `<Response>` is signed.
   Prohibitorum always signs both.
5. **Attribute names in URI / basic NameFormat per GHES docs.** The
   example above uses the `schemas.xmlsoap.org/...` `name` values
   that GHES expects. `emails`, `public_keys`, and `gpg_keys` are
   multi-valued — set `multi: true`. `public_keys` uses URI
   NameFormat with `Name="urn:oid:1.2.840.113549.1.1.1"`.
6. **`administrator` attribute name is fixed.** GHES does not allow
   renaming it; emit literally as `administrator` (basic NameFormat).
7. **SessionNotOnOrAfter is honored** by GHES. Set
   `saml_sp.session_lifetime` for a per-SP override; NULL = IdP
   default from `configx.SAML.SessionLifetime`.
8. **Entity ID and ACS URL format.** GHES uses
   `entity_id = https://HOSTNAME` and
   `ACS = https://HOSTNAME/saml/consume`.

See `docs/superpowers/specs/2026-05-24-audit-saml.md` "GHES-specific
call-outs" for the full audit-traced list.

### Discovery

```
GET /saml/metadata
```

returns the IdP `<EntityDescriptor>` XML with one
`<IDPSSODescriptor>` element per active+grace-period signing key. SPs
import it once; Prohibitorum republishes during key rotation to
include both old and new keys (see `configx.SAML.MetadataRotationGrace`,
default 7d).

---

## Logout

Two coordinated paths:

1. **RP logout (OIDC)**: clear the RP's own session, then 302 to
   `https://auth.example.com/oidc/logout?id_token_hint=...&post_logout_redirect_uri=https://rp.example.com/`.
   The `post_logout_redirect_uri` must match a value in
   `oidc_client.post_logout_redirect_uris`. The user is logged out
   of Prohibitorum and bounced back.

2. **Prohibitorum-initiated logout**: the user clicks "Sign out" on
   `/me`. Prohibitorum revokes the session. The next time an RP
   introspects the session cookie (Pattern B) it sees `active: false`.
   For Pattern A, the RP's session continues until the RP detects the
   user is gone (typically by the next ID-token refresh failing).

3. **SAML SLO (Pattern C, v0.5)**: stubbed at `/saml/slo`; lands in
   v0.5. The `saml_session` table is populated from day one so that
   SLO can iterate over an account's active SAML SPs without a schema
   migration when the feature ships.

## What Prohibitorum does NOT do for you

- **Authorization decisions.** Prohibitorum hands you `role` +
  `attributes` (OIDC) or a SAML AttributeStatement. You decide what
  those mean for your endpoints.
- **Per-resource access control.** Prohibitorum has no concept of
  "user X has read access to project Y." That's RP-local state.
- **User profile data beyond username + display name + attributes.**
  Prohibitorum is an identity provider, not a user-profile service.

## End-to-end example

A new internal service integrates with Prohibitorum via Pattern A like
this:

1. Operator inserts a client row for `myapp-prod`.
2. The app's config is set to `OIDC_ISSUER=https://auth.example.com`,
   `OIDC_CLIENT_ID=myapp-prod`,
   `OIDC_REDIRECT_URI=https://myapp.example.com/auth/callback`.
3. The app replaces in-house session middleware with one that:
   a. Reads its own RP session cookie.
   b. If absent, redirects to `/authorize`.
   c. On callback, exchanges the code at `/token`, validates the ID
      token, mints its own RP session cookie keyed by `sub`.
4. The app keys its per-user data on `sub`; it never sees a password.

The app goes from "owns the user directory" to "trusts Prohibitorum
for identity, owns its domain-specific data."
