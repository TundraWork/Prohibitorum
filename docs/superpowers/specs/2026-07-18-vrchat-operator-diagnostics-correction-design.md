# VRChat Operator and Diagnostics Correction Design

**Date:** 2026-07-18
**Status:** Approved

## Problem

The merged VRChat provider has three production defects:

1. Operator-session failures use the provider detail page's global request state, so the error panel renders above the page heading instead of beside the operator controls that caused it.
2. The VRChat client requires the inbound `auth` cookie to carry `Secure`, while the current VRChat OpenAPI login response documents `auth=…; Expires=…; Path=/; HttpOnly`. The mock always adds `Secure`, so the incompatible production response is collapsed into `upstream_temporarily_unavailable` without test coverage.
3. The diagnostic store is available for lookup and pruning but no production error path records events. Every request-ID lookup therefore misses, and the lookup handler misclassifies the miss as `account_not_found`.

## Decisions

### Contextual operator errors

Operator start, verification, validation, and replacement use a dedicated `useApi()` state rather than the provider page's global state. The operator error panel renders inside the Operator session card immediately after the active credentials, code, or action controls. The active form remains visible after a recoverable error. Passwords and one-time codes continue to clear after submission; the operator username remains available for correction.

Page-load and general provider-configuration failures remain in the page-level error panel. Successful or unrelated page operations cannot erase an operator error, and successful operator operations cannot erase an unrelated page error.

### VRChat cookie compatibility and containment

Inbound authentication cookies are accepted only from the fixed production HTTPS origin (or the existing loopback HTTPS smoke origin). Cookie names remain limited to `auth` and `twoFactorAuth`; `Path=/`, `HttpOnly`, an absent or exact `api.vrchat.cloud` domain, valid expiry, bounded size, and the existing attribute allowlist remain mandatory.

The inbound `Secure` attribute becomes optional because the current VRChat login response schema omits it. Since the response is received over the fixed HTTPS origin, accepted cookies are normalized to host-only and `Secure=true` before merge, sealing, persistence, or outbound reuse. Outbound and stored-cookie validation continues to require `Secure=true`. The mock login response omits `Secure` so the integration suite exercises the production-compatible path.

### Universal diagnostic recording

A server middleware observes canonical application JSON errors after registry lookup and detail filtering. It records one bounded diagnostic event after the handler returns, keyed by the response request ID. The record contains only:

- request ID;
- registered public code;
- filtered public detail fields;
- HTTP method and matched route;
- a stable operation label derived from method and route;
- authenticated account ID when available;
- registry retryability;
- occurrence and expiry timestamps applied by the store.

The diagnostic recorder never parses arbitrary response bodies and never stores raw errors, headers, credentials, cookies, tokens, request bodies, SQL values, or unchecked strings. Diagnostic insertion failure is logged safely and never changes the original response.

Both raw chi errors and typed Huma errors notify the same capture mechanism. The concrete diagnostic service exposes write, read, and prune behavior; test fakes implement the same narrow contract.

An absent or expired diagnostic returns registered code `diagnostic_not_found`, not `account_not_found`. English and Chinese locale catalogs and the frontend error manifest include the new code.

## Error and security behavior

- VRChat transport, malformed response, rate-limit, and cookie-contract failures remain secret-free public errors.
- A missing inbound `Secure` flag alone is normalized; a missing `HttpOnly`, unsafe path/domain, unexpected cookie name, malformed expiry, duplicate cookie, or unsupported attribute still fails closed.
- Diagnostic lookup remains exact-ID, admin-only, fresh-sudo gated, rate-limited, and audited.
- The diagnostic middleware must not recursively expose diagnostic-store failures.

## Verification

Tests are written red-first for these observable contracts:

1. An operator start failure renders exactly one error panel inside the Operator session card after the active form; the global page error region stays empty.
2. Operator and page request states do not clear or overwrite each other.
3. A documented VRChat `auth` cookie without `Secure` is accepted from the fixed HTTPS origin, normalized to `Secure=true` and host-only, sealed, and reused. The same cookie without `HttpOnly` or with unsafe scope is rejected.
4. A canonical raw-handler error and a typed Huma error each create a retrievable diagnostic record with exact safe fields and no secret-bearing data.
5. A diagnostic insert failure preserves the original status/body.
6. An absent diagnostic returns `diagnostic_not_found`.
7. The mock VRChat operator setup, focused Go/dashboard suites, complete CI gate, and end-to-end smoke all pass. Browser verification confirms contextual placement at desktop and mobile widths.
