# Prohibitorum — configuration reference

`PROHIBITORUM_*` env vars; an optional `config.yaml` in the working directory is also honored. Nested YAML keys map by upper-casing and joining with `_` (`oidc.access_token_ttl` → `PROHIBITORUM_OIDC_ACCESS_TOKEN_TTL`). Durations use Go syntax (`10m`, `8h`, `720h`, `60s`). **Only the data-encryption key is strictly required** — boot fails without one.

## Core

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>` | — (**required**) | base64-encoded AES-256 key (32 bytes). Highest `<n>` used for new writes; lower versions remain for decryption. Set at least one (e.g. `_V1`). |
| `PROHIBITORUM_DATABASE_URL` | — | Postgres connection string. Required for the server and every DB-backed CLI command. |
| `PROHIBITORUM_PUBLIC_ORIGIN` | `http://localhost:8080` | Comma-separated public origin(s), each `scheme://host[:port]` with no path or trailing `/`; the server refuses to start otherwise. Seeds the OIDC issuer, SAML EntityID + endpoints, and the WebAuthn RP ID/origins when those aren't set explicitly. |
| `PROHIBITORUM_HOST` | `""` (all interfaces) | Bind interface; set `127.0.0.1` to listen loopback-only behind a reverse proxy. |
| `PROHIBITORUM_PORT` | `8080` | Bind port. |
| `PROHIBITORUM_SERVER_TIMING` | `false` | Add a `Server-Timing` header to every response: `total`, `db` (query time and count), `db-wait` (waiting for a pooled connection) and `kv`. Shows in the browser's network panel for profiling. Leave off where untrusted clients reach the server: the breakdown reveals how much work a request did, for example whether a sign-in found an account. |
| `PROHIBITORUM_SESSION_TTL` | `8h` | Sliding session lifetime when "keep me signed in" is not chosen; the cookie is a browser-session cookie. |
| `PROHIBITORUM_SESSION_REMEMBER_TTL` | `336h` | Sliding session lifetime (and cookie `Max-Age`) when "keep me signed in" is chosen. No absolute cap; `≤0` falls back to the default. |

## KV store

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_KV_DRIVER` | `memory` | `memory` (single-process dev) or `redis`. |
| `PROHIBITORUM_KV_REDIS_URL` | `localhost:6379` | Redis address. |
| `PROHIBITORUM_KV_REDIS_USERNAME` | `""` | Redis 6+ ACL username. |
| `PROHIBITORUM_KV_REDIS_PASSWORD` | `""` | Redis password. |
| `PROHIBITORUM_KV_REDIS_TLS` | `false` | Connect to Redis over TLS. |

## SMTP (outbound mail)

Mail is used only for address-verification codes and the admin test mail; it is never an account-recovery channel. Leaving `host` empty disables delivery entirely — users can still add addresses, but verification then needs an admin assertion.

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_SMTP_HOST` | `""` (disabled) | SMTP server hostname. Empty means no outbound mail. |
| `PROHIBITORUM_SMTP_PORT` | `587` | SMTP port. |
| `PROHIBITORUM_SMTP_SECURITY` | `starttls` | `starttls` (mandatory upgrade on a plain port), `tls` (implicit TLS, usually port 465), or `none` (plaintext; only for a trusted internal relay). |
| `PROHIBITORUM_SMTP_USERNAME` | `""` | SMTP AUTH username; empty skips authentication. |
| `PROHIBITORUM_SMTP_PASSWORD` | `""` | SMTP AUTH password. |
| `PROHIBITORUM_SMTP_FROM` | — (**required with `host`**) | Envelope sender; must be a bare address (`no-reply@example.com`). Boot fails when `host` is set without a valid `from`. |
| `PROHIBITORUM_SMTP_TIMEOUT` | `15s` | Per-send dial+dialog timeout. Sends are synchronous within the request. |

## OIDC OP

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_OIDC_ISSUER` | `PublicOrigins[0]` | Issuer string embedded in tokens + discovery. |
| `PROHIBITORUM_OIDC_ACCESS_TOKEN_TTL` | `10m` | Access-token lifetime. |
| `PROHIBITORUM_OIDC_ID_TOKEN_TTL` | `10m` | ID-token lifetime. |
| `PROHIBITORUM_OIDC_REFRESH_TOKEN_TTL` | `720h` (30d) | Refresh-token / family lifetime (slides on rotation). |
| `PROHIBITORUM_OIDC_AUTHORIZATION_CODE_TTL` | `60s` | Authorization-code lifetime (single-use). |
| `PROHIBITORUM_OIDC_JWKS_CACHE_MAX_AGE` | `5m` | `Cache-Control: max-age` on `/oauth/jwks` + discovery. |

## Forward auth

| Variable | Default | Meaning |
|----------|---------|---------|
| `PROHIBITORUM_FORWARD_AUTH_SESSION_TTL` | `1h` | Sliding server session lifetime. Successful cookie-authenticated requests renew when at most one quarter remains. Configure Traefik cookie forwarding as described in [forward-auth.md](forward-auth.md#session-renewal). |

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
| `PROHIBITORUM_AUTH_SUDO_TTL` | `30m` | Recent-auth window after a full sign-in or step-up; sensitive actions within it skip re-verification. Explicit deployment values override this default. |
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
| `PROHIBITORUM_PASSWORD_HASH_MAX_CONCURRENT` | `0` (`min(4, GOMAXPROCS)`) | argon2id computations that may run at once. |

Every argon2id computation takes one of `PROHIBITORUM_PASSWORD_HASH_MAX_CONCURRENT` slots while it runs: password sign-in, sudo, setting a password, recovery codes and OIDC client-secret checks share them. Memory for hashing is therefore about slots × `PROHIBITORUM_PASSWORD_HASH_MEMORY_KIB`, plus headroom for Go's garbage collector, which lets the heap grow to about twice the live data: at the default 64 MiB, each concurrent hash measured about 120 MiB. The default suits a few GiB of memory; on a smaller deployment, lower the slot count until slots × 120 MiB fits beside the rest of the process. More slots than CPU cores add memory without adding throughput, and `GOMAXPROCS` follows the container's CPU limit. A request that waits 5 seconds without getting a slot is answered `429 rate_limited` on the dashboard API and `503 temporarily_unavailable` on `/oauth/token`, `/oauth/introspect` and `/oauth/revoke`, both with `Retry-After: 1`. The limit is per process, so each replica has its own slots.

## VRChat upstream provider

VRChat does not offer public OAuth/OIDC. Configure a `vrchat` identity
provider in **Admin → Identity Providers**, then use **Operator session** with
a dedicated VRChat verification account. The setup wizard sends that
account's credentials and 2FA code directly to VRChat and discards them; only
the resulting cookie jar is retained, encrypted under the active data
encryption key. Do not use a personal or privileged VRChat account.

The integration is unofficial and may carry account or moderation risk under
VRChat's API-usage guidelines. Production requests are fixed to
`https://api.vrchat.cloud/api/1`, identify Prohibitorum through `User-Agent`,
reuse the operator session, honor shared `429` backoff, and invalidate
readiness on upstream `401`/`403`. Each member sign-in requires a fresh exact
Prohibitorum proof URL in the member's VRChat `bioLinks`; the member should
remove it after verification. The proof URL is built on the first
`PROHIBITORUM_PUBLIC_ORIGIN` entry and uses that entry's scheme (`http` or
`https`); the server refuses to start when that entry is not a bare origin.
Prohibitorum stores the verified VRChat user ID, display name, and canonical
profile URL, not member credentials.

## Deployment hardening

The KV store backs session lookups, single-use auth codes, federation state, PKCE verifiers, and enrollment tokens. Session secrets are stored hashed (`session:<id>:<SHA-256(token)>`, never the raw cookie token), but flow secrets live in the KV — in any non-loopback deployment Redis **must be network-isolated and reached over an authenticated, encrypted channel**:

```bash
export PROHIBITORUM_KV_DRIVER="redis"
export PROHIBITORUM_KV_REDIS_URL="redis.internal:6379"
export PROHIBITORUM_KV_REDIS_TLS="true"
export PROHIBITORUM_KV_REDIS_USERNAME="prohibitorum"   # Redis 6+ ACL (optional)
export PROHIBITORUM_KV_REDIS_PASSWORD="$REDIS_PASSWORD"
```

Outbound federation fetches (discovery / JWKS / token exchange) run on an SSRF-hardened client that refuses loopback, private (RFC1918 / ULA), link-local, and cloud-metadata addresses; the admin API rejects non-`https` or IP-literal issuer URLs. Every outbound request also identifies itself with the same `User-Agent` the server sends VRChat — `Prohibitorum/<version> <public origin>`, e.g. `Prohibitorum/v1.2.3 https://login.example.com` — because some upstream APIs (bgm.tv) reject library-default or missing `User-Agent` headers with `403`. Operator CLI commands, which have no public origin, send `Prohibitorum/<version>`. To federate with an IdP on a private network, enable **Admin → Identity Providers → allowPrivateNetwork** for that IdP (default `false`); the avatar picture a provider hands out is fetched the same way. A provider that serves its pictures only to authenticated clients can enable **Admin → Identity Providers → Connection → authenticateAvatarFetch** (default off): the sign-in's access token is then sent as `Authorization: Bearer …` on the picture fetch, to whatever avatar URL the provider returns — turn it on only for providers you trust with that token. A redirect that changes host drops the header (Go's default); providers without the setting, and the token-less implicit flow, always fetch anonymously.

The sign-in endpoints are not rate-limited per client IP: behind a reverse proxy every user would share one budget, and the argon2id slots above already bound the server's memory under a flood. To stop one source from flooding them, limit by path at the reverse proxy, which sees the real client address. For example, with nginx:

```nginx
# http context
limit_req_zone $binary_remote_addr zone=prohibitorum_signin:10m rate=30r/m;
limit_req_zone $binary_remote_addr zone=prohibitorum_oauth:10m rate=600r/m;

# server context, beside the location that proxies to Prohibitorum
location = /api/prohibitorum/auth/password/begin {
    limit_req zone=prohibitorum_signin burst=10 nodelay;
    limit_req_status 429;
    proxy_pass http://prohibitorum;
}
location ~ ^/oauth/(token|introspect|revoke)$ {
    limit_req zone=prohibitorum_oauth burst=100 nodelay;
    limit_req_status 429;
    proxy_pass http://prohibitorum;
}
```

An application's back-end calls `/oauth/token`, `/oauth/introspect` and `/oauth/revoke` from its own server address for all of its users, and a resource server may introspect on every request it receives, so give those paths a much higher per-address rate than the sign-in page. If nginx itself sits behind another proxy or a CDN, set the client address with its `realip` module first, or every visitor counts as that proxy.

Behind a TLS-terminating reverse proxy, keep `PROHIBITORUM_PUBLIC_ORIGIN` on `https://…` so session cookies are issued with the `Secure` flag. Client-IP resolution is configured in **Admin → Settings → Client IP**, not by an env var: the default `direct` strategy uses the TCP peer address; switch to `forwarded` (X-Forwarded-For) or a named header and specify trusted-proxy CIDRs so forwarding headers are honored only when the direct peer is a trusted proxy.

The sign-in page's background (**Admin → Settings → Sign-in page**) needs no configuration of its own. Its Unsplash access key and Pixabay API key are entered there and stored sealed with the data encryption key (`PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>`, the highest version at the time they are saved); keep the version that sealed them configured, or remove and re-enter the key after retiring that version. Bing's picture of the day and Unsplash photos are loaded by the visitor's browser straight from `https://www.bing.com` and `https://images.unsplash.com`, which the page's `Content-Security-Policy` `img-src` allows; the server itself only fetches their metadata over the same SSRF-hardened outbound client, from `www.bing.com` and `api.unsplash.com`, so an egress allowlist needs both hosts for those sources to work. Pixabay is different: its terms forbid permanent hotlinking, so the server downloads each picture (from `pixabay.com`, the same host as its API, over its own capped outbound client) and serves it from this site — the visitor's browser never contacts Pixabay for a picture and the CSP stays as it is; an egress allowlist needs `pixabay.com` for both the search and the picture downloads.
