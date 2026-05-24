# Integrating with Prohibitorum

How a relying party (RP) — a backend service or SPA — uses Prohibitorum
to authenticate its users.

## Pick a pattern

| Pattern | When | Trust assumption |
|---|---|---|
| **A. OIDC Authorization Code + PKCE** | Any RP with a back-end, mobile apps with a server, CLI tools using a loopback redirect | Standard. Strongest. Use this. |
| **B. Cookie + Introspection** | RP shares a parent domain with Prohibitorum (behind one reverse proxy) and wants the simplest possible integration | Acceptable for co-located first-party RPs only. |
| **C. ID-token-only (no token endpoint)** | Internal scripts / batch jobs where you control both ends and don't want to manage refresh tokens | Stateless; uses the same JWKS verification as A. |

For new integrations, **start with A**. The library ecosystem is huge,
and you stop having to think about session theft, cookie domains, and
revocation propagation.

## Pattern A — OIDC Authorization Code + PKCE

### One-time setup

Register a client. In v1 this is a SQL insert (no admin UI yet):

```sql
INSERT INTO oidc_client
  (client_id, client_secret_hash, display_name, redirect_uris, allowed_scopes)
VALUES (
  'picotera-prod',
  -- public client: NULL secret_hash; required to use PKCE
  NULL,
  'PicoTera (prod)',
  ARRAY['https://gateway.example.com/auth/callback'],
  ARRAY['openid', 'profile']
);
```

For confidential clients (back-end-only), generate a strong random secret,
hash it with argon2id, and store the hash in `client_secret_hash`.

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
 │   ◄──────────────────────── (login if needed; WebAuthn)
 │                                              │
 │ 2. 302 → redirect_uri?code=...&state=...&iss=https://auth.example.com
 │                                              │
 │ 3. Back-end POST /token                      │
 │      grant_type=authorization_code           │
 │      code=...                                │
 │      redirect_uri=https://... (verbatim)     │
 │      code_verifier=<random>                  │
 │      client_id=picotera-prod                 │
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
 │    id_token's claims. Discard the tokens     │
 │    after extracting what you need.           │
```

### Claims in the ID token

```json
{
  "iss": "https://auth.example.com",
  "sub": "42",
  "aud": "picotera-prod",
  "exp": 1769950000,
  "iat": 1769949400,
  "nonce": "<echoed back from /authorize>",
  "username": "alice",
  "displayName": "Alice Smith",
  "role": "admin",
  "permissions": {
    "view_own_usage": true,
    "manage_own_api_keys": true,
    "view_models": true,
    "view_own_traces": true,
    "manage_own_projects": true
  }
}
```

`sub` is the **stable** account id. Key your local user table on this. Do
not key on `username` (which the user can rename if you support that).

### Library recommendations

- Go RP: `github.com/zitadel/oidc/v3/pkg/client` — same library Prohibitorum uses on the OP side.
- Node RP: `openid-client`.
- Python RP: `authlib`.
- Browser-only SPA: don't. Always have a thin back-end that holds the refresh token.

### Discovery

```
GET /.well-known/openid-configuration
```

returns the endpoint URLs, supported algorithms, supported scopes, etc.
Your OIDC client library reads this once at startup and caches it.

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
                       ◄── { active: true, sub, username, role, permissions, ... }
```

The RP's back-end caches the introspection response for a short interval
(seconds) and rejects requests when `active=false`.

**Caveats:**
- Cookie must be sent → same parent domain required.
- The cookie is bearer-equivalent within that domain; XSS on either
  side is a total compromise.
- Use Pattern A if these aren't acceptable.

## Logout

Two coordinated paths:

1. **RP logout**: clear the RP's own session, then 302 to
   `https://auth.example.com/oidc/logout?post_logout_redirect_uri=https://rp.example.com/`.
   The user is logged out of Prohibitorum and bounced back.

2. **Prohibitorum-initiated logout**: the user clicks "Sign out" on
   `/me`. Prohibitorum revokes the session. The next time an RP
   introspects the session cookie (Pattern B) it sees `active: false`.
   For Pattern A, the RP's session continues until the RP itself
   detects the user is logged out (typically by the next ID token
   refresh failing).

## What Prohibitorum does NOT do for you

- **Authorization decisions.** Prohibitorum hands you `role` and
  `permissions`. You decide what those mean for your endpoints.
- **Per-resource access control.** Prohibitorum has no concept of
  "user X has read access to project Y." That's RP-local state.
- **User profile data beyond username + display name.** Prohibitorum
  is an identity provider, not a user-profile service.

## End-to-end example

A new picotera deployment integrates with Prohibitorum like this:

1. Operator inserts a client row for `picotera-prod`.
2. Picotera's config is set to `OIDC_ISSUER=https://auth.example.com`,
   `OIDC_CLIENT_ID=picotera-prod`, `OIDC_REDIRECT_URI=https://gateway.example.com/auth/callback`.
3. Picotera replaces its in-house `auth.LoadSession` middleware with
   one that:
   a. Reads its own RP session cookie.
   b. If absent, redirects to Prohibitorum's `/authorize`.
   c. On callback, exchanges the code at `/token`, validates the
      ID token, mints its own RP session cookie keyed by `sub`.
4. Picotera's existing `Account` table is dropped; it now relies on
   the `sub` claim as the user identity. Per-user picotera state
   (API keys, projects, etc.) keys on `sub`.

Picotera goes from "owns the user directory" to "trusts Prohibitorum
for identity, owns its domain-specific data."
