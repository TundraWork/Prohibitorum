# Prohibitorum — configuration reference

`PROHIBITORUM_*` env vars; an optional `config.yaml` in the working directory is also honored. Nested YAML keys map by upper-casing and joining with `_` (`oidc.access_token_ttl` → `PROHIBITORUM_OIDC_ACCESS_TOKEN_TTL`). Durations use Go syntax (`10m`, `8h`, `720h`, `60s`). **Only the data-encryption key is strictly required** — boot fails without one.

## Core

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>` | — (**required**) | base64-encoded AES-256 key (32 bytes). Highest `<n>` used for new writes; lower versions remain for decryption. Set at least one (e.g. `_V1`). |
| `PROHIBITORUM_DATABASE_URL` | — | Postgres connection string. Required for the server and every DB-backed CLI command. |
| `PROHIBITORUM_PUBLIC_ORIGIN` | `http://localhost:8080` | Comma-separated public origin(s). Seeds the OIDC issuer, SAML EntityID + endpoints, and the WebAuthn RP ID/origins when those aren't set explicitly. |
| `PROHIBITORUM_HOST` | `""` (all interfaces) | Bind interface; set `127.0.0.1` to listen loopback-only behind a reverse proxy. |
| `PROHIBITORUM_PORT` | `8080` | Bind port. |
| `PROHIBITORUM_SESSION_TTL` | `8h` | Session lifetime (cookie + KV). |

## KV store

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_KV_DRIVER` | `memory` | `memory` (single-process dev) or `redis`. |
| `PROHIBITORUM_KV_REDIS_URL` | `localhost:6379` | Redis address. |
| `PROHIBITORUM_KV_REDIS_USERNAME` | `""` | Redis 6+ ACL username. |
| `PROHIBITORUM_KV_REDIS_PASSWORD` | `""` | Redis password. |
| `PROHIBITORUM_KV_REDIS_TLS` | `false` | Connect to Redis over TLS. |

## OIDC OP

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_OIDC_ISSUER` | `PublicOrigins[0]` | Issuer string embedded in tokens + discovery. |
| `PROHIBITORUM_OIDC_ACCESS_TOKEN_TTL` | `10m` | Access-token lifetime. |
| `PROHIBITORUM_OIDC_ID_TOKEN_TTL` | `10m` | ID-token lifetime. |
| `PROHIBITORUM_OIDC_REFRESH_TOKEN_TTL` | `720h` (30d) | Refresh-token / family lifetime (slides on rotation). |
| `PROHIBITORUM_OIDC_AUTHORIZATION_CODE_TTL` | `60s` | Authorization-code lifetime (single-use). |
| `PROHIBITORUM_OIDC_JWKS_CACHE_MAX_AGE` | `5m` | `Cache-Control: max-age` on `/oauth/jwks` + discovery. |

## WebAuthn

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_WEBAUTHN_RP_ID` | host of `PublicOrigins[0]` | WebAuthn Relying Party ID. Override when the RP ID differs from the origin hostname. |
| `PROHIBITORUM_WEBAUTHN_RP_DISPLAY_NAME` | `Prohibitorum` | RP display name shown by authenticators; also the TOTP issuer fallback. |
| `PROHIBITORUM_WEBAUTHN_RP_ORIGINS` | `PublicOrigins` | Comma-separated allowed WebAuthn origins. |

## Upstream OIDC federation

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_FEDERATION_STATE_TTL` | `10m` | Lifetime of the single-use federation state blob. |
| `PROHIBITORUM_FEDERATION_DEFAULT_SCOPES` | `openid,profile,email` | Scopes requested from an upstream when none are set per-IdP. (List value — prefer `config.yaml`.) |
| `PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK` | `false` | Disable the outbound federation client's SSRF dial-screen. Set `true` only for a trusted internal upstream IdP (or the loopback mock OP in tests). |

## TOTP

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_TOTP_DEFAULT_PERIOD` | `30` | TOTP period (seconds). |
| `PROHIBITORUM_TOTP_DEFAULT_DIGITS` | `6` | TOTP digit count. |
| `PROHIBITORUM_TOTP_DEFAULT_ALGORITHM` | `SHA1` | RFC 6238 HMAC algorithm. |
| `PROHIBITORUM_TOTP_DRIFT_STEPS` | `1` | Accepted ± step drift on verify. |
| `PROHIBITORUM_TOTP_RECOVERY_CODE_COUNT` | `10` | Recovery codes minted per enrollment. |
| `PROHIBITORUM_TOTP_ISSUER` | `webauthn.rp_display_name` | Label in the `otpauth://` URI. |

## Cross-factor auth

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_AUTH_SUDO_TTL` | `15m` | Recent-auth window after a full sign-in or step-up; sensitive actions within it skip re-verification. |
| `PROHIBITORUM_AUTH_PARTIAL_SESSION_TTL` | `5m` | Window a password-only partial session has to complete the TOTP step. |
| `PROHIBITORUM_AUTH_THROTTLE_SCHEDULE` | `0,0,1s,2s,4s,8s,16s,32s,1m,2m,4m,8m,15m` | Per-failure lockout ladder (last entry clamps). List value — prefer `config.yaml`. |

## SAML IdP

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_SAML_ENTITY_ID` | `PublicOrigins[0]` | Stable IdP SAML EntityID. Changing it breaks every registered SP. |
| `PROHIBITORUM_SAML_DEFAULT_NAMEID_FORMAT` | `urn:oasis:names:tc:SAML:1.1:nameid-format:persistent` | Default NameID format. |
| `PROHIBITORUM_SAML_SESSION_LIFETIME` | `8h` | Default `SessionNotOnOrAfter` horizon. |
| `PROHIBITORUM_SAML_METADATA_ROTATION_GRACE` | `168h` (7d) | Signing-key decommission grace advertised in metadata. |
| `PROHIBITORUM_SAML_METADATA_VALIDITY` | `24h` | `validUntil` on published IdP metadata. |

## Password hashing (argon2id)

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_PASSWORD_HASH_MEMORY_KIB` | `65536` (64 MiB) | argon2id memory cost. |
| `PROHIBITORUM_PASSWORD_HASH_ITERATIONS` | `3` | argon2id time cost. |
| `PROHIBITORUM_PASSWORD_HASH_PARALLELISM` | `1` | argon2id lanes. |

## Deployment hardening

The KV store backs session lookups, single-use auth codes, federation state, PKCE verifiers, and enrollment tokens. Session secrets are stored hashed (`session:<id>:<SHA-256(token)>`, never the raw cookie token), but flow secrets live in the KV — in any non-loopback deployment Redis **must be network-isolated and reached over an authenticated, encrypted channel**:

```bash
export PROHIBITORUM_KV_DRIVER="redis"
export PROHIBITORUM_KV_REDIS_URL="redis.internal:6379"
export PROHIBITORUM_KV_REDIS_TLS="true"
export PROHIBITORUM_KV_REDIS_USERNAME="prohibitorum"   # Redis 6+ ACL (optional)
export PROHIBITORUM_KV_REDIS_PASSWORD="$REDIS_PASSWORD"
```

Outbound federation fetches (discovery / JWKS / token exchange) run on an SSRF-hardened client that refuses loopback, private (RFC1918 / ULA), link-local, and cloud-metadata addresses; the admin API rejects non-`https` or IP-literal issuer URLs. To federate with an IdP on a private network, set `PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true`.

Behind a TLS-terminating reverse proxy, keep `PROHIBITORUM_PUBLIC_ORIGIN` on `https://…` so session cookies are issued with the `Secure` flag. Client-IP resolution is no longer controlled by an env var — configure it in **Admin → Settings → Client IP**: the default `direct` strategy uses the TCP peer address; switch to `forwarded` (X-Forwarded-For) or a named header and specify trusted-proxy CIDRs so forwarding headers are honored only when the direct peer is a trusted proxy.
