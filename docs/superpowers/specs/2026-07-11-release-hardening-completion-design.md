# Release Hardening Completion Design

**Date:** 2026-07-11  
**Status:** Approved  
**Scope:** Refresh-token family hardening, universal admin cursor pagination, per-IdP outbound private-network policy, and unified public errors with persistent localized frontend diagnostics.

## Goals

1. Preserve Prohibitorum's existing OAuth refresh-token rotation while making family state atomic, non-recoverable from storage alone, bounded by inactivity and absolute lifetimes, and immediately responsive to security-state changes.
2. Make every administrative collection bounded and consistently cursor-paginated.
3. Keep public outbound federation fetching functional while comprehensively denying unsafe destinations and replacing the global private-network bypass with an audited per-IdP choice.
4. Give every known user-visible failure a unique stable code and curated details, render localized persistent errors in the dashboard, and let a fresh-sudo administrator retrieve the corresponding safe server-side diagnostic by request ID.

## Non-goals

- DPoP or mutual-TLS sender-constrained tokens.
- Offset pagination or compatibility copies of unbounded admin routes.
- Server-side translation of application JSON errors.
- Exposing raw SQL, cryptographic, filesystem, HTTP-header, token, credential, or dependency error strings through APIs or the diagnostic viewer.
- Retaining already-issued legacy-format refresh tokens across the upgrade.

## 1. Hardened rotating refresh-token families

### Standards model

Prohibitorum continues to implement OAuth refresh-token rotation as described by RFC 6749 section 6 and RFC 9700 section 4.14.2. Each successful exchange returns a replacement refresh token; reuse of a superseded token outside the retry grace period revokes the family.

### Token and storage format

A refresh token uses a versioned opaque format containing a random family identifier and random token secret. The family identifier is a lookup handle, not an authenticator. Possession of the random secret remains required.

The KV store contains one versioned family record keyed by family ID. It stores:

- SHA-256 hashes of the current and immediately previous token secrets;
- client ID, account ID, originating session ID, scopes, authentication snapshot, family creation time, last-use time, absolute expiry, and inactivity expiry;
- previous-token grace deadline;
- the current successor encrypted with the active DEK solely so an identical response can be recovered after a lost response or benign duplicate submission;
- DEK version and record revision.

No usable refresh bearer token is stored in plaintext. Token mappings are eliminated because the presented family ID selects the record.

### Atomic rotation

Rotation is a compare-and-swap transition on the complete family record:

1. Parse and validate the versioned token format.
2. Load the family by family ID.
3. Constant-time compare the presented secret hash with the current and previous hashes.
4. If current, generate the successor, update hashes/timestamps/encrypted successor/revision, and CAS the exact prior record.
5. On a CAS loss, reload and classify the request as the permitted previous-token retry, confirmed reuse, or a concurrent in-progress exchange.
6. If previous and inside the short grace window, decrypt and return the already-issued successor without minting another token.
7. If superseded outside the window, delete/revoke the family and emit a `refresh_token_reuse` diagnostic and audit event.

A storage failure never produces tokens. Partial family and token-index writes are impossible because there is only one authoritative record.

### Lifetime and revocation

Two independent limits apply:

- **Inactivity lifetime:** configurable, default 30 days, extended after successful rotation but never beyond absolute expiry.
- **Absolute lifetime:** configurable, default 90 days, fixed when the family is created.

Every exchange re-checks client binding, account status, application authorization, and originating session validity. The family is revoked on confirmed reuse, client mismatch, disabled/deleted account, removed application access, revoked originating session, or account recovery that invalidates credentials/sessions.

Deployment invalidates all existing legacy-format refresh tokens. Clients receive `invalid_grant` and must obtain a fresh authorization grant. No legacy parser or migration shim remains.

## 2. Universal administrative cursor pagination

### Wire contract

Every admin endpoint whose primary response is a collection returns:

```json
{
  "items": [],
  "nextCursor": "opaque-cursor-or-empty"
}
```

Every endpoint accepts `limit` and `cursor`. `limit` defaults to 50 and is clamped to 1–100. Empty results serialize as `items: []`; the final page returns an empty `nextCursor`.

Coverage includes top-level and nested admin collections: accounts, invitations, groups, group members, account groups, account credentials, account sessions, account PATs, account identities, OIDC applications, SAML applications, upstream identity providers, signing keys, forward-auth applications, audit events, and OIDC/SAML access groups and accounts. Any other admin route whose primary body is an array must adopt the same contract before completion.

### Cursor design

Cursors are stateless authenticated payloads protected with the configured DEK. A cursor contains:

- endpoint/collection identifier;
- normalized filters and sort identifier;
- last stable keyset values;
- issue time and expiry;
- format version.

A cursor cannot be reused with another endpoint, filter, or ordering. Invalid, expired, or modified cursors return the dedicated `pagination_cursor_invalid` code. Cursor lifetime is 24 hours.

Queries use stable keyset ordering. Time-ordered collections use a unique tuple such as `(created_at, id)`; name-ordered catalogs use normalized name plus immutable ID. Queries fetch `limit + 1`, return at most `limit`, and derive `nextCursor` from the last returned row. Offset pagination is prohibited.

### Dashboard behavior

A shared typed page contract and pagination composable own cursor state, loading, retry, and filter resets. Collection views provide accessible previous/next or load-more controls appropriate to the existing layout. A filter or sort change clears the cursor chain. No page silently truncates data.

## 3. Per-IdP outbound destination policy

### Public mode

Public outbound federation and avatar requests retain redirects, but every initial connection and hop passes the same policy. The policy rejects:

- loopback;
- link-local and cloud-metadata ranges;
- multicast, unspecified, documentation, benchmark, reserved, and other IANA special-purpose destinations;
- RFC1918 and carrier-grade NAT IPv4 ranges;
- IPv6 ULA and IPv4-mapped equivalents;
- IP-literal URLs, URL userinfo, unsupported schemes, and HTTPS-to-HTTP downgrade.

All DNS answers and the actual dial target are screened. Redirects remain capped at five; request time, response size, and expected content type remain bounded.

### Private IdP mode

The global `federation.allow_private_network` bypass is removed. Each upstream IdP has an admin-managed `allowPrivateNetwork` boolean, default false.

When true, only RFC1918 IPv4 and IPv6 ULA destinations become eligible for that IdP's discovery, JWKS, token, userinfo, and inherited-avatar requests. Loopback, link-local/cloud metadata, multicast, unspecified, reserved, and non-routable special-use destinations remain blocked unconditionally. The setting follows the IdP through cached client construction and every redirected hop.

Creating or changing this setting requires the existing high-impact admin authorization tier, records an audit event, and displays a prominent security explanation in the IdP form. Existing deployments migrate to `false`; operators explicitly enable private access per IdP where required.

## 4. Unified public errors and diagnostics

### Error registry

One registry defines every public error case. Each definition has:

- globally unique stable code;
- HTTP status or protocol-native mapping;
- frontend localization key;
- allowed public detail schema;
- retryability and recovery action;
- safe internal diagnostic category.

Known causes receive dedicated codes. Broad codes such as `bad_request` and `server_error` are used only when the server genuinely cannot classify more precisely. Callers cannot attach arbitrary maps or raw errors to public details.

Application JSON errors use:

```json
{
  "code": "oidc_redirect_uri_mismatch",
  "details": {
    "field": "redirectUri",
    "reason": "not_registered"
  },
  "requestId": "server-generated-id"
}
```

The server does not select a display language. OAuth, OIDC, SAML, and WebAuthn endpoints retain required protocol-native responses and redirects, but adapters map from the same internal registry and preserve a correlation request ID where the protocol safely permits it.

### Request correlation

A top-level middleware generates a cryptographically random request ID for every request, places it in context and structured logs, returns it in `X-Request-ID`, and includes it in application error bodies. Client-supplied IDs are not trusted as the server identifier.

### Server-side diagnostic records

Classified failures create a bounded diagnostic record keyed by request ID. The record contains timestamp, operation, public code, authenticated account ID when known, route/method, retryability, and explicitly whitelisted diagnostic fields. It never stores raw request bodies, cookies, authorization headers, tokens, secrets, SQL arguments, or unreviewed `error.Error()` strings.

Records expire after seven days and are cleaned incrementally. Exact-ID lookup is available at `GET /api/prohibitorum/diagnostics/{requestId}` to administrators with fresh sudo. Lookup is rate-limited and audited. It returns 404 for an absent/expired ID and never supports bulk browsing, which limits the diagnostic store's use as a data-exfiltration surface.

The normal operator log receives the same request ID, so operators with external log retention can correlate beyond the bounded diagnostic record without exposing those logs through the application.

### Persistent frontend errors

A shared error panel replaces transient error popups for API failures. It provides:

- localized title and explanation selected from the stable code;
- dedicated localized context and recovery guidance for each classified case;
- explicit retry/action controls when appropriate;
- a collapsed, keyboard-operable Details disclosure showing curated details and request ID;
- copy-details support;
- an admin-only link to the exact diagnostic lookup when a request ID is present;
- explicit dismissal only, with no automatic timeout;
- inline placement and accessible focus/live-region behavior.

Unknown codes use a localized safe fallback while preserving request ID and curated details. Locale catalogs must cover every registry code; a parity test fails when a code is missing in either supported language.

## Security invariants

- Refresh-token theft cannot be hidden by indefinite reuse; a superseded token revokes its family outside the retry window.
- KV compromise alone does not reveal usable refresh tokens.
- Pagination cursors cannot change endpoint, filters, or order without detection.
- Enabling private IdP access never enables loopback, link-local, or cloud metadata access.
- Public error and diagnostic responses never contain raw internal errors or secret-bearing input.
- Request IDs permit exact correlation but do not grant access; diagnostic lookup still requires admin plus fresh sudo.

## Verification strategy

- Deterministic refresh tests cover atomic winner selection, lost-response retry, reuse revocation, absolute/inactivity expiry, session revocation, storage failures, and legacy-token invalidation on both memory and Redis stores.
- Every admin collection has database keyset boundary tests, cursor tamper/filter/expiry tests, response-schema tests, and dashboard multi-page tests.
- Outbound tests cover every denied address class, mixed DNS answers, redirects, private per-IdP enable/disable behavior, and unconditional metadata/loopback rejection.
- Registry tests enforce unique codes, detail schemas, protocol mappings, and complete English/Chinese frontend locale parity.
- Diagnostic tests enforce fresh sudo, exact-ID lookup, expiry, auditing, rate limiting, and absence of secrets.
- Frontend accessibility tests cover persistence, disclosure controls, focus, localization, copy details, and admin diagnostic navigation.
- Final verification runs focused suites first, then complete Go tests/race, frontend tests/build, vulnerability/workflow/release gates, integration smoke, and browser checks.
