# Config-options completeness audit — global `configx` surface + provider-toggle re-verification

**Date:** 2026-06-11. **Branch:** `master`. **Tree:** read/verify only; no code changed.
**Relationship to prior passes:** The 2026-06-10 column-wiring audit and the 2026-06-11 FE-surface
functionality audit both scoped explicitly to the **three provider DB tables** (`upstream_idp`,
`oidc_client`, `saml_sp`). Neither audited the **global `pkg/configx/configx.go` surface** — the env/yaml
knobs (`PROHIBITORUM_*`). This pass fills that gap ("is each global config option actually consumed, or
silently hardcoded?") and re-verifies the prior provider-toggle findings against current code.

**Method:** for every field in `configx.Config` (+ every `viper.SetDefault`), trace the consumption site;
where a hardcoded constant shadows the config value, flag it. Two parallel read-only domain sweeps, then the
headline items were **independently re-grepped by the orchestrator** (`file:line` anchored). `go build` clean.

---

## Headline

**A cluster of global config knobs are dead: present in `Config`, bound to env vars, given defaults and
doc-comments that imply they are configurable — but never read at runtime because a hardcoded constant drives
the actual behavior.** The defaults happen to equal the constants, so a misconfiguration fails **silently**
(the operator sees their value accepted at boot and assumes it took effect). The highest-impact group is the
**OIDC token lifetimes** — an operator tuning `PROHIBITORUM_OIDC_REFRESH_TOKEN_TTL` / `ACCESS_TOKEN_TTL` for
security or interop gets the 30d / 10m constants regardless.

Separately, **all** provider-table findings from the 2026-06-10 / 06-11 audits remain **OPEN** — the
remediation handover was written but not implemented (git log since shows only docs + web-bundle commits).

---

## Remediation status

**C1–C5 are FIXED (2026-06-11, same day).** The four OIDC token-lifetime knobs and the JWKS cache age
are now wired: `pkg/protocol/oidc` resolves each via `p.accessTokenTTL()/idTokenTTL()/refreshTokenTTL()/
authCodeTTL()/jwksCacheMaxAge()` (config value if set, else the compiled-in default const), threaded into
the code/refresh KV-store helpers as a `ttl` parameter. The OIDC TTL config defaults already equalled their
constants, so this is a zero-behavior-change wiring; `oidc.jwks_cache_max_age`'s default was realigned to
5m (the historical hardcoded value) and its false "advertised in discovery" comment corrected — also zero
behavior change. Verified by `TestTTLResolversHonorConfigElseDefault` + `TestTokenGrantHonorsConfiguredTTLs`
(`pkg/protocol/oidc/ttl_config_test.go`) and the full suite.

**C6–C11 are FIXED (2026-06-11), guided by the online best-practice research below.** Decision per knob,
following the tiered model (locked floor / secure-default + per-entity override / operator-pinned identity /
prune the rest): **wired C6, C7, C9, C10, C11; pruned C8.**

- **C7 `SAML.EntityID` — WIRED (the headline correctness fix).** `entityID()` (`pkg/protocol/saml/saml.go`)
  now returns `saml.entity_id` when set, else `PublicOrigins[0]`. Crucially, `entityID()` was split: a new
  `baseURL()` (always `PublicOrigins[0]`) drives every reachable endpoint URL (SSO/SLO/metadata locations,
  AuthnRequest `Destination` checks, login-bounce redirects), while only the SAML **identity** sites
  (assertion/SLO/Response `Issuer`, metadata `EntityID`) read `entityID()`. This lets an operator pin a stable
  EntityID (even a non-resolvable URN) independent of the HTTP origin — federation best practice (the EntityID
  is an identifier, not a location, and changing it breaks every registered SP). Guarded by
  `TestEntityIDBaseURLSplit` (a symbolic URN must NOT leak into endpoint URLs).
- **C8 `SAML.BaseURL` — PRUNED.** Endpoint URLs are correctly the reachable `PublicOrigins[0]`; a separate
  SAML base URL is only for unusual multi-host topologies and would be a silent-misconfig knob. Field + viper
  default removed (zero readers confirmed).
- **C9 `SAML.SessionLifetime` — WIRED.** `sessionNotOnOrAfter` now takes the IdP-wide fallback
  (`i.samlSessionLifetime()` = config value or the 8h default const); per-SP `session_lifetime` still wins.
- **C6 `Federation.DefaultScopes` — WIRED** (server-side): the admin upstream-IdP empty-scopes fallback reads
  config via `defaultFederationScopes()` (minimal OIDC-valid fallback if config empty).
- **C11 `SAML.DefaultNameIDFormat` — WIRED**: SP-create's NameID-format default reads config instead of the
  hardcoded persistent-format const.
- **C10 `Host` — WIRED**: `Serve()` binds `net.JoinHostPort(Host, port)` when set, else `:port`.

All defaults already equalled the constants, so these are zero-behavior-change wirings; every kept knob now has
an accurate doc comment (per the "document or it's a silent misconfig" rule). Verified by `TestEntityIDBaseURLSplit`
+ `TestSAMLSessionLifetimeResolver` (`pkg/protocol/saml/identity_test.go`), `TestDefaultFederationScopes`
(`pkg/server/config_defaults_test.go`), and the full suite.

## NEW findings — dead / ignored global config knobs

🔴 = config field never read at runtime (hardcoded constant wins). 🟡 = read but narrower than implied.
All "never read" verdicts verified by `grep -rn '<field>' pkg cmd` returning nothing outside `configx.go`.

| # | Config field (env var) | Hardcoded value that wins | Evidence | Sev |
|---|---|---|---|---|
| C1 | `OIDC.AccessTokenTTL` (`…_OIDC_ACCESS_TOKEN_TTL`) | `const AccessTokenTTL = 10*time.Minute` | `pkg/protocol/oidc/token.go:25` (used `:244,:271`); 0 config reads | 🔴 |
| C2 | `OIDC.IDTokenTTL` (`…_OIDC_ID_TOKEN_TTL`) | `const IDTokenTTL = 10*time.Minute` | `token.go:26` (used `:291`); 0 config reads | 🔴 |
| C3 | `OIDC.RefreshTokenTTL` (`…_OIDC_REFRESH_TOKEN_TTL`) | `const RefreshTokenTTL = 30*24*time.Hour` | `refresh.go:23` (used `:99,:102`); 0 config reads | 🔴 |
| C4 | `OIDC.AuthorizationCodeTTL` (`…_OIDC_AUTHORIZATION_CODE_TTL`) | `const AuthorizationCodeTTL = 60*time.Second` | `codes.go:19` (used `:71`); 0 config reads | 🔴 |
| C5 | `OIDC.JWKSCacheMaxAge` (`…_OIDC_JWKS_CACHE_MAX_AGE`) | `Cache-Control: public, max-age=300` (hardcoded) | `oidc.go:99` (discovery) + `:116` (JWKS); 0 config reads | 🔴 |
| C6 | `Federation.DefaultScopes` (`…_FEDERATION_DEFAULT_SCOPES`) | admin/CLI hardcode `["openid","email","profile"]` | `handle_admin_upstream_idps.go:179,:319`; `cmd/prohibitorum/main.go:840`; 0 config reads | 🔴 |
| C7 | `SAML.EntityID` (`…_SAML_ENTITY_ID`) | `entityID()` returns `PublicOrigins[0]` | `pkg/protocol/saml/saml.go:60-64`; 0 config reads | 🔴 |
| C8 | `SAML.BaseURL` (`…_SAML_BASE_URL`) | all SAML URLs derive from `entityID()`/`PublicOrigins[0]` | `saml/saml.go:68-90`; 0 config reads | 🔴 |
| C9 | `SAML.SessionLifetime` (`…_SAML_SESSION_LIFETIME`) | `const defaultSessionLifetime = 8*time.Hour` (per-SP column else this) | `assertion.go:46` (used `:74,:80`); 0 config reads | 🔴 |
| C10 | `Host` (`…_HOST`) | server binds `:%d` (port only) | `pkg/server/server.go:229`; 0 reads | 🔴 (minor) |
| C11 | `SAML.DefaultNameIDFormat` | used in **metadata advertisement only**, not assertion NameID selection (per-SP `sp.NameIDFormat` drives that) | read `metadata.go:82` only | 🟡 |

### Why C5 is also a truth-in-labeling bug
`configx.go` documents `JWKSCacheMaxAge` as *"advertised in the discovery doc as a hint to downstream RPs."*
**It is not** — `HandleDiscovery` (`oidc.go:67-97`) emits no cache-hint field, and the actual JWKS
`Cache-Control` is a hardcoded `max-age=300`, not the configured 1h default. The comment is false.

### Impact ranking
- **C1–C4 (token lifetimes): material.** Operators legitimately tune these (shorter access tokens, longer/shorter
  refresh windows, code TTL for slow networks). Today every value is silently ignored. This is the one group
  worth *wiring* rather than pruning.
- **C5 (JWKS cache): low-but-real.** A 5-minute JWKS cache during signing-key rotation is shorter than the
  documented 1h; mostly affects RP key-refresh cadence. Wire it (and emit it in discovery) or fix the comment.
- **C6 (federation default scopes): cosmetic-ish.** Admin always sends explicit scopes from the FE; the knob is
  a fallback that never fires, and the hardcoded fallback order even differs from the config default. Prune or wire.
- **C7–C9 (SAML EntityID/BaseURL/SessionLifetime): prune candidates.** EntityID/BaseURL are fully superseded by
  the `PublicOrigins`-derived identity (arguably the better design); `SessionLifetime` global default is shadowed
  by a constant of the same value. These look like pre-`PublicOrigins` vestiges.
- **C10 (`Host`): trivial.** Either honor it in the bind address or drop it.

### Recommended action for the global knobs
1. **Wire C1–C5** — thread `config.OIDC.*TTL` into `token.go`/`refresh.go`/`codes.go` (replace the package
   consts with config reads, or seed the consts as defaults the config overrides), and use `JWKSCacheMaxAge`
   for the JWKS `Cache-Control` + advertise it (or correct the comment). Small, contained, removes a silent-
   misconfig class.
2. **Prune or wire C6–C10** — decide per knob: drop the field (truth-in-labeling) or honor it. C7/C8 (SAML
   EntityID/BaseURL) are the clearest prune candidates given the `PublicOrigins`-derived design.
3. **Fix C11 / the C5 comment** regardless.

---

## Provider-table findings — re-verified, STILL OPEN at current HEAD

The two prior audits are accurate; the remediation handover
(`2026-06-11-backend-correctness-remediation-handover.md`) was **not implemented**. Confirmed against current
code (so the answer to "are the OIDC/SAML provider toggles supported?" is: mostly yes, with these standing gaps):

- **Admin sudo-gating gap (⚠️ highest):** `UpdateAccount` (user→admin escalation), `DeleteAccount`,
  `RevokeAccountSessions`, `ReissueEnrollment`, `CreateInvitation`, `RevokeInvitation` are `registerOp`
  (admin-role only, **no fresh sudo**) at `server.go:380,381,386,387,388,390` — while single-credential delete
  and single-session revoke ARE sudo-gated. *New nuance:* the bulk/whole-account variants are unprotected while
  their single-item twins are protected.
- **OIDC `email` scope is dead** (no email claim anywhere; not in `scopes_supported`) — `claims.go:54-64`,
  `userinfo.go`, `oidc.go:77`. No server-side scope whitelist (any string accepted) — `clientgen.go:40-43`.
- **OIDC consent "deny" omits RFC 9207 `iss`** — `handle_consent.go:98-110` (success path sets it at
  `authorize.go:316`).
- **OIDC `prompt`** not validated against a supported set; `prompt_values_supported` not advertised —
  `authorize.go:132`.
- **OIDC dead columns** (`contacts`, `application_type`, `id_token_signed_response_alg`, `default_max_age`,
  `require_auth_time`, `subject_type`) still stored-never-read; `logo_uri`/`tos_uri`/`policy_uri` read by the
  consent screen but not settable via the admin API.
- **SAML `want_assertions_signed`** (assertions always signed, `assertion.go:202`) and **`name_id_claim`**
  (NameID always opaque, `subjectid.go:36-47`) are still no-ops accepted in the admin body;
  **`authn_requests_signed`** dead/redundant (`metadata.go:37`); `metadata_*` trio inert (no auto-refresh).
- **SAML redirect-binding LogoutResponse is unsigned** (no detached `SigAlg`/`Signature`) — `slo.go:488-492`,
  violating Bindings §3.4.4.1.
- **Upstream IdP `disabled`** is a hard kill-switch (good), but disabled-mid-flow re-lookup returns a wrapped
  **HTTP 500** instead of a clean `federation_state_invalid` — `federation.go:349-351` → `handle_federation.go`.
- Upstream IdP `mode`/`allowed_domains`/`require_verified_email`/claim-overrides/`scopes` all correctly honored.

See `2026-06-11-backend-functionality-correctness-audit.md` for the full per-finding detail + the verified-
correct list; this section is only a current-state confirmation.

---

## Verification commands
```
grep -rn '\.AccessTokenTTL\|\.IDTokenTTL\|\.RefreshTokenTTL\|\.AuthorizationCodeTTL\|\.JWKSCacheMaxAge' pkg cmd | grep -v configx
grep -rn 'DefaultScopes\|SAML\.EntityID\|SAML\.BaseURL\|SAML\.SessionLifetime' pkg cmd | grep -v configx
pkg/protocol/oidc/{token.go:25,refresh.go:23,codes.go:19,oidc.go:99,116}   # hardcoded TTL/cache consts
pkg/protocol/saml/{saml.go:60-64,assertion.go:46}                          # entityID() + session-lifetime fallback
```
