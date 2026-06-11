# Follow-up audit — infrastructure surface (session storage, outbound HTTP, CSRF)

**Date:** 2026-06-11. **Branch:** `master`. **Tree:** clean (read/verify only; no code changed).
**Relationship to the prior pass:** This complements the 2026-06-11 FE-surface-driven audit
(`2026-06-11-backend-functionality-correctness-audit.md`). That pass took the **frontend** as the
behavioral spec and verified **protocol-artifact correctness** (OIDC/SAML/WebAuthn crypto). By
construction it never examined the parts of the system that have no FE control surface: how the
**session token is stored at rest**, how the **outbound federation HTTP client** is configured, and
the **CSRF posture** of cookie-authenticated routes. Those are exactly where the new findings cluster.

**Method:** four parallel domain deep-dives (session/cookie/CSRF; KV/ceremony races; federation
SSRF/outbound; OIDC-token + SAML-parsing edges), each tracing real code with `file:line`. The two
HIGH items (N1, N2) were then **independently re-read by the orchestrator** at the cited lines.

**Headline:** The prior pass's conclusion — *the protocol crypto is sound, no Critical artifact break*
— still holds. But two **HIGH** infrastructure issues were missed by the FE-driven method, and both are
arguably higher-impact than the prior pass's #1 (the sudo-gating gap), because they don't require an
already-stolen admin cookie:

- **N1** — the raw session secret is the literal KV lookup key, so any KV read = session hijack.
- **N2** — the upstream-federation outbound fetches run on an unhardened HTTP client with an
  unvalidated, **publicly-triggerable** issuer URL → SSRF into internal/metadata endpoints.

---

## Ranked new findings

| # | Sev | Area | Finding | Verify |
|---|-----|------|---------|--------|
| N1 | 🔴 **HIGH** | Session storage | Session **secret token is stored RAW as the KV key** (`session:<id>:<token>`) → KV read = full session hijack | 🔎 |
| N2 | 🔴 **HIGH** | Federation SSRF | Outbound discovery/JWKS/token fetches use an **unhardened client** (no internal-IP dialer, follows redirects) against an **unvalidated, publicly-triggerable** issuer URL | 🔎 |
| N3 | ⚠️ HIGH/MED | Federation DoS | **Unbounded `io.ReadAll`** on discovery/JWKS/token responses; no JWKS key-count cap; federation endpoints unrate-limited | 📄 |
| N4 | ⚠️ MED | CSRF | **Federation login `state` not bound to the initiating browser** → login-CSRF / session-fixation into the attacker's upstream account (Lax allows the top-level GET callback) | 🔎 |
| N5 | ⚠️ MED | CSRF | CSRF defense is **SameSite=Lax only** — no Origin/Referer/token/custom-header check anywhere | 🔎 |
| N6 | 🟡 LOW | Session storage | Add-passkey ceremony keyed on the **raw session token** (`handle_me.go:243`) — a second leak surface of N1; sudo path correctly uses `SessionID` | 🔎 |
| N7 | 🟡 LOW | SAML hygiene | **RelayState not length-bounded** (spec ≤80B) + **SAML POST body uncapped** (no `MaxBytesReader` on `/saml/sso`,`/saml/slo` POST) | 📄 |
| N8 | ℹ️ info | OIDC token | Refresh-grant **`scope` param silently ignored** — fails SAFE (no escalation possible); minor down-scoping interop gap only | 🔎 |
| N9 | ℹ️ info | Deploy posture | Redis client built with **no TLS/AUTH**; PKCE verifier + enrollment token sit in **plaintext KV** — confidentiality of N1's keys + flow secrets depends entirely on a trusted Redis | 📄 |

Re-confirmed (already in the prior audit, not new): the global `(iss,sub)` re-login keying not scoped to
the IdP row (`modes.go:66`, prior minor-13) and the add-passkey `Get`+`Del`-vs-`Pop` parity (prior
minor-13). N6 is the *key-material* aspect of that same handler, distinct from the `Pop` parity point.

---

## Detail

### N1 🔴 HIGH — session secret token is the raw KV key 🔎
**Verified:** `pkg/session/session.go:80-82` — `sessionKey(accountID, token) = fmt.Sprintf("session:%d:%s", accountID, token)`,
where `token` is the 32-byte secret from `newToken()` (`session.go:59-66`) — i.e. the secret half of the
cookie (`CookieValue` = `<accountID>.<token>`). `Issue` writes the payload under that key
(`session.go:126`); `LoadSession` surfaces the raw token as `authn.Session.Token`.
**The codebase already knows the right pattern and applies it inconsistently:** the PG `session` table
stores only the opaque `sessionID` (16 random bytes), never the secret (`newSessionID` doc comment at
`session.go:68-71` literally says "so listing doesn't leak the secret cookie token"); `/me/sessions`
returns `sessionID`, never the token. But the KV key undoes that — the secret lives in cleartext as the
key string.
**Exploit:** anyone with **read** access to KV (a Redis `SCAN session:*`, an RDB/AOF dump, a backup, a
logged key, a memory dump, or a shared/multi-tenant Redis) recovers the exact secret token, reassembles
the cookie as `<accountID>.<token>`, and fully impersonates the user — no cracking. The KV store becomes
a plaintext session-secret store, strictly weaker than the standard "store only a hash."
**Fix:** key the session on `session:<accountID>:<SHA-256(token)>`. The token is high-entropy, so a bare
SHA-256 suffices (no per-record salt; lookup stays constant work). Hash on `Issue`/`Load`/`Save`/
`Revoke` and when computing is-current in `ListByAccount`. Cookie keeps the raw token; the KV key never
contains it. (This also closes N6 if the add-passkey stash adopts the same hash, though `SessionID` is
the cleaner choice there.)

### N2 🔴 HIGH — SSRF on the federation outbound fetches (publicly triggerable) 🔎
**Verified:** `NewClient` (`pkg/federation/oidc/client.go:125`) calls `rp.NewRelyingPartyOIDC(...)`
**without** `rp.WithHTTPClient(...)` — so zitadel/oidc uses its `httphelper.DefaultHTTPClient` (a bare
client: a `Timeout`, but **no `CheckRedirect`** and no SSRF-aware dialer). That client performs the
discovery fetch (`{issuer}/.well-known/openid-configuration`), the JWKS fetch, and the token-exchange
POST. The `issuer_url` itself is validated **only for non-emptiness** at create/update
(`pkg/server/handle_admin_upstream_idps.go:146` — `body.IssuerUrl == ""`); there is no `https`
enforcement and no internal-IP screening (no `IsPrivate`/`169.254`/loopback check anywhere in the
federation path), and the DB column has no CHECK.
**Why it's worse than "admin-only":** the fetch **trigger** is unauthenticated — `GET
/api/prohibitorum/auth/federation/{slug}/login` and `/callback` are registered `publicReq`
(`server.go:317-318`), and the client cache is 15 min with errors not cached, so re-discovery is
repeatedly drivable by anyone. Three compounding gaps:
- **(a)** issuer may be `http://169.254.169.254/...`, `http://localhost:port`, RFC1918, or link-local —
  an internal port scanner / cloud-metadata credential exfil primitive.
- **(b)** discovery-returned `token_endpoint`/`jwks_uri`/`authorization_endpoint`/`userinfo_endpoint`
  are trusted wholesale; the existing token-endpoint *drift* check (`federation.go:329`) only prevents
  change mid-flow, not an internal snapshot.
- **(d)** redirects are followed (default Go policy, ≤10 hops), so even an `https://` public issuer can
  302 the JWKS/discovery fetch to `http://169.254.169.254/...` — defeating a naive host allowlist and
  enabling DNS-rebind.
**Fix:** inject a hardened `*http.Client` via `rp.WithHTTPClient(...)` in `NewClient`: a
`Transport.DialContext` that resolves and **rejects loopback/RFC1918/link-local/ULA/metadata IPs at dial
time** (closes rebind + redirect-to-internal), a `CheckRedirect` re-applying the same predicate per hop
(or no redirects on discovery/JWKS), and keep the timeout. Plus validate `issuerUrl` is `https://` with
a non-IP host in create/update, and re-screen the discovery-returned endpoint hosts before first use.

### N3 ⚠️ HIGH/MED — unbounded upstream-response read → memory DoS 📄
The single zitadel fetch path does `io.ReadAll(resp.Body)` with **no `io.LimitReader`** and no
`Content-Length` guard — used by discovery, JWKS, and the token response. No cap on JWKS size or
key-count either. A malicious "IdP" (or a public `…/{slug}/login` storm — the federation routes have no
per-endpoint rate limit, `server.go:316-318`) returns a multi-GB body; `io.ReadAll` buffers it in RAM →
OOM. The 30s timeout bounds wall-clock, not bytes. **Fix:** wrap the injected client's transport to cap
the response body via `io.LimitReader`, add a hard JWKS key-count cap, and consider rate-limiting the
public federation endpoints. (Pairs with N2's hardened-client work.)

### N4 ⚠️ MED — federation login-CSRF (state not bound to the browser) 🔎
**Verified:** the federation `state` token is server-minted and stashed in KV (`federation.go:246,280`)
but is **not bound to the initiating browser** — there is no pre-session anti-forgery cookie compared at
`/callback`. SameSite=Lax **permits** cookies on a top-level cross-site GET navigation, and `/callback`
is a public GET that **issues a session cookie** on success (`handle_federation.go:141,146`). Classic
login-CSRF: the attacker completes the upstream dance with *their own* upstream identity and feeds the
resulting `code`/`state` to the victim via a cross-site top-level GET, logging the victim into the
**attacker's** account; subsequent victim actions (entering data, linking a credential) land in the
attacker-controlled account. `return_to` is constrained to local relative paths
(`validateFederationReturnTo`), which limits *where* the victim lands but not the fixation itself.
**Fix:** bind `state` to a short-lived `SameSite=Strict`, HttpOnly anti-forgery cookie set at `/login`
and required to match at `/callback` — exactly the pattern the local WebAuthn ceremony already uses
(`CeremonyCookie`, SameSite=Strict, `session/middleware.go:155-165`). The `/me/identities/link` flow is
**not** affected: its `begin` is sudo-gated and `callback` binds `state` to the current `account_id`.

### N5 ⚠️ MED — CSRF defense is SameSite=Lax only 🔎
**Verified:** the only router middleware is `LoadSession` (`server.go:142`); a repo-wide search for
`csrf` / `X-Requested-With` / `Origin` / `Referer` / `sec-fetch` enforcement finds **none**. Every
cookie-authed state-changing route relies solely on the browser honoring `SameSite=Lax`
(`session/middleware.go:130,145`). For the JSON `POST` mutations this is *mostly* fine (Lax blocks
cross-site POST; forging `application/json` cross-site needs a CORS preflight no config permits), but it
is brittle (depends on Lax never being relaxed, `__Host-`/Secure actually resolving in prod, and the
content-type gate). N4 is the concrete hole this leaves open. **Fix (defense-in-depth):** add a stateless
`Origin`/`Sec-Fetch-Site` allowlist check (reject cross-site state-changing requests whose `Origin` ∉
`PublicOrigins`) as middleware on the mutating routes; keep Lax as the second layer.

### N6 🟡 LOW — add-passkey ceremony keyed on the raw session token 🔎
**Verified:** `handle_me.go:243` (begin) / `:261`,`:327` (complete/cleanup) build
`"webauthn_ceremony:add:" + sess.Token` — the raw secret again, in a second KV namespace, 5-min TTL,
once per add-passkey start. The **sudo** ceremony correctly keys on the opaque `sess.Data.SessionID`
(`handle_sudo.go`), so add-passkey is the outlier. **Fix:** key on `sess.Data.SessionID`. (Folds in with
the prior audit's `Get`→`Pop` parity note on the same handler.)

### N7 🟡 LOW — RelayState unbounded + SAML POST body uncapped 📄
RelayState (`authnreq.go:193-196`, `slo.go:238-241`, `sso_init.go:181`) is never checked against the
spec ≤80-byte limit, and the `/saml/sso`,`/saml/slo` **POST** form bodies get no `http.MaxBytesReader`
(admin routes get 64 KiB via `operations.go:108`; these public SAML POSTs do not). RelayState **is**
correctly treated as opaque — HTML-escaped into the auto-POST form, never a redirect target — so this is
spec-conformance + a DoS-hygiene gap, not injection. **Fix:** reject `len(relayState) > 80`; wrap the
SAML POST handlers in `MaxBytesReader`.

### N8 ℹ️ info — refresh-grant `scope` silently ignored (fails safe) 🔎
`grantRefreshToken` never reads the request `scope`; it always re-mints with `fam.Scope`. Escalation is
therefore **impossible** (the dangerous direction is closed). The only deviation from RFC 6749 §6 is that
a *narrower* requested scope isn't honored (down-scoping silently ignored). No security fix needed; honor
down-scoping only if a client needs it (verify requested ⊆ `fam.Scope`, reject otherwise).

### N9 ℹ️ info — deploy posture: untrusted-Redis blast radius
`NewRedisStore` (`pkg/kv/redis.go`) builds a bare `redis.NewClient` with **no TLS and no AUTH**. Combined
with N1 (raw session token as key) and the plaintext PKCE verifier + enrollment token in `FedState`
(`federation/oidc/state.go`), a shared/untrusted Redis exposes session secrets, downgrades PKCE, and
leaks account-creation tokens. **Action:** document (and ideally enforce) that the Redis backend MUST be
network-isolated, TLS, and AUTH'd; wire `WithTLSConfig`/`Password` options. N1's hashing fix removes the
session-secret half of this regardless.

---

## What the parallel deep-dives re-verified CORRECT (high-value, on record)
- **KV `Pop` is genuinely atomic** in both backends — memory uses ttlcache `GetAndDelete` under a single
  mutex hold (confirmed in the upstream source), Redis uses `GETDEL`. Every single-use consume (auth
  code, federation login/link state, consent ticket, reauth nonce, partial-session, recovery, login/sudo
  WebAuthn ceremonies) uses atomic `Pop`. No auth-code/state replay window.
- **All single-use / replay checks fail CLOSED on a KV backend error** (SAML replay → 500; refresh
  rotation lock → no rotation; reauth/consent → error surfaced). The one exception (`usedFamily` swallows
  `Get` errors) only skips the defense-in-depth *family revocation* — the replayed code is still denied.
- **KV key namespacing is sound** — every key uses a server-minted fixed-charset suffix; a crafted
  ticket/slug/`id` value can only deepen a key, never escape its prefix, and nonce/ticket payloads are
  account-bound so guessing a key still fails. Login-vs-link and the four ceremony sub-namespaces are
  isolated.
- **Federation RP token validation** — issuer-equality enforced by zitadel `Discover`
  (`discoveryConfig.Issuer != issuer → ErrIssuerInvalid`), per-flow `ExpectedIss`/`token_endpoint`
  snapshot + drift check, alg allowlist `{RS256,ES256,EdDSA}` enforced at `jose.ParseSigned` (no
  none/HS256), alg/key-type confusion blocked, `aud`/`azp`/`exp`/`iat` checked, nonce single-use via the
  atomic state `Pop`. Upstream `client_secret` is AES-GCM AAD-bound, never logged, never in any admin
  read view. Auto-provision username-collision is tx-guarded (a malicious upstream can't merge into
  `admin`), `require_verified_email` + `allowed_domains` enforced on provision AND link.
- **OIDC token endpoint** — confidential-client auth required + constant-time argon2id verify, unknown-
  client timing equalized via dummy-verify, code→client binding (`ac.ClientID != client.ClientID`), PKCE
  constant-time compare with downgrade closed (challenge present ⇒ verify runs unconditionally),
  `redirect_uri` re-matched at token, refresh token client-bound (mismatch → family revoke), no
  `client_credentials`/`password` grant exists, unknown grant → `unsupported_grant_type`.
- **SAML generated assertions** — `AudienceRestriction`/`Recipient`/`NotOnOrAfter` pin the target SP (no
  cross-SP replay); `InResponseTo` bound to the validated request ID for SP-initiated and correctly
  ABSENT for IdP-initiated; Destination pinned to this IdP; ACS resolved only from registered metadata
  (request-supplied ACS that isn't registered → `ErrInvalidACS`); SLO always signature-gated; logout
  `post_logout_redirect_uri` exact-matched (no open redirect).
- **Session lifecycle** — `LoadSession` fetches the LIVE account every request and revokes on
  `Disabled`/deleted (no stale-snapshot authority); login always mints a fresh token (no fixation at
  login); absolute + sliding expiry correct; logout/`revoke` clear cleanly and don't leak cross-account
  existence. Cookie is `__Host-`/Secure/HttpOnly/SameSite=Lax/Path=/ in HTTPS deploys.

---

## Suggested priority (proposed; not implemented — several are product/infra decisions)
1. **N1 (HIGH)** — hash the session token before using it as a KV key. Small, contained, high payoff.
2. **N2 + N3 (HIGH)** — inject a hardened SSRF-aware, size-capped HTTP client into the federation RP;
   validate the issuer URL (https + non-internal); rate-limit the public federation endpoints.
3. **N4 (MED)** — bind federation login `state` to a Strict anti-forgery cookie (reuse `CeremonyCookie`).
4. **N5 (MED)** — Origin-allowlist middleware on state-changing routes (defense-in-depth).
5. **N6 / N7 / N9 (LOW/info)** — key add-passkey on `SessionID`; bound RelayState + cap SAML POST bodies;
   document/enforce trusted-Redis (TLS+AUTH).
6. **N8** — no action required (fails safe).

These sit **alongside** the prior audit's Tier-1 sudo-gating work; N1 and N2 are independent of it and, in
my assessment, should be triaged at the same priority as the sudo gap (prior ⚠️-1) because neither
requires a pre-stolen admin cookie.
