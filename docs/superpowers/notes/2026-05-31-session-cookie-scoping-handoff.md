# Session handoff — session-cookie scoping fix DONE (4 tasks + 3-stage review)

> Future Claude: the v0.6 audit's cookie-path-vs-route architectural finding is
> CLOSED. The session cookie is now `Path=/` and reaches the root-level
> OIDC/SAML endpoints. This records the end state. The prior chunk (v0.6
> protocol completeness) is in `2026-05-31-session-handoff.md`.

## TL;DR — cookie scoping fix is DONE

The session cookie moved from `Path=/api/prohibitorum` to `Path=/`, with a
deployment-stable identity derived from `PUBLIC_ORIGIN`'s scheme:
`__Host-prohibitorum_session` + `Secure` in HTTPS deployments, plain
`prohibitorum_session` (no `Secure`) in HTTP dev so `cookiejar` clients can send
it. `SameSite=Lax` / `HttpOnly` / no `Domain` unchanged; ceremony cookie
untouched; no route/issuer/metadata changes. This closes the interactive-browser
gap surfaced by the v0.6 deep audit (a real browser now sends the session cookie
to `/oauth/authorize`, `/saml/sso`, `/saml/sso/init`, `/saml/slo`).

```
HEAD: eab0cda   branch: master   working tree: clean
go build ./... ✓   go vet ./... ✓   go test ./... ✓   smoke 1–111 ✓ (SMOKE_EXIT=0)
```

## Commits (this chunk)
- `e518935` fix(session): `Path=/` + deployment-conditional `__Host-` name. Added `secureCookies(cfg)`, `sessionCookieName(secure)`, exported `SessionCookieNameFor(cfg)` in `pkg/session/middleware.go`; rewrote `FreshSessionCookie`/`ClearedSessionCookie` (Path=/, conditional name, cfg-driven Secure, `_ *http.Request` param retained for signature stability); `LoadSession` reads via resolved name. New `pkg/session/middleware_test.go` (both modes + clear-matches-set + name resolution).
- `3cf0c1a` fix(server): name the cookie via `SessionCookieNameFor` at the two out-of-package sites — `handleLogoutHTTP` read (`handle_auth.go`) and the OpenAPI security scheme (`registerSecurityScheme` now takes a `cookieName`; constructor passes the resolved name, config-less `NewHuma()` passes the base).
- `28a9095` test(smoke): dropped the manual cookie re-attach in all SIX OIDC/SAML helpers (`authorizeWithSession`, `ssoLocation`, `ssoPostForm`, `ssoInit`, `ssoWithSession`, `authorizeExpectDirectError`) — each now sets `Jar: c.jar` and the jar auto-sends the `Path=/` cookie (browser-equivalent). Deleted `sessionCookieForOIDC`; added `assertSessionCookieAtRoot` (matches plain OR `__Host-` name, cannot pass vacuously), called once before the first authorize.
- `c961f6f` docs(audit): closed the finding in `AUDIT.md`; clarified the TLS-termination row (session-cookie `Secure` now derives from the public-origin scheme, ceremony cookie stays per-request TLS).
- `eab0cda` docs(plan): the plan + `.tasks.json` record (4/4 done).

Spec: `docs/superpowers/specs/2026-05-31-session-cookie-scoping-design.md` (D1–D5).
Plan: `docs/superpowers/plans/2026-05-31-session-cookie-scoping.md`.

## Review outcome
Each commit passed spec + code-quality review; a final opus whole-change review
APPROVED ("this is done"). Three Minor doc-honesty nuances (all non-blocking):
the `__Host-` arm of `assertSessionCookieAtRoot` is never exercised by the
dev-only smoke; plan said 5 helpers, 6 were correctly converted; the deliberate
unused `_ *http.Request` param is documented.

## Accepted / out of scope (documented)
- **D2 limitation (carried forward):** a logged-in user hitting a SAML
  HTTP-POST-binding AuthnRequest is a cross-site POST → `SameSite=Lax` cookie not
  sent → one `/login` bounce. Same family as the deferred `ForceAuthn`+POST item.
  `SameSite=None` rejected (broader exposure, always-`Secure`, browser-restricted).
- No real-browser HTTPS end-to-end test (no browser harness). Verification is the
  attribute-level unit tests + the dev-mode behavioral smoke; production behavior
  follows from `Path=/` + `SameSite=Lax` per the web-platform spec.
- The `/login` UI itself (frontend) is separate future work — this fix only
  ensures an existing session cookie reaches the protocol endpoints.

## Runtime quirks (unchanged — these still bite)
- Master branch, direct commits (user-authorized project-wide). NO git remote
  configured (`no origin/master`), so push/PR is N/A. opus implementers,
  sonnet/opus reviewers, never haiku.
- Trust `go build ./...` exit 0 + `go vet`, NOT gopls `<new-diagnostics>` (it
  falsely reports cross-file "undefined" / sqlc "undefined method" mid-edit on
  code that builds clean — hit repeatedly this session again).
- NEVER `pkill -f 'prohibitorum'` bare (kills the PG at `/tmp/prohibitorum-pg`).
  Smoke via detached `setsid bash /tmp/run_v06.sh`, then poll `/tmp/v06.result`
  for `DONE` / `SMOKE_EXIT=0` (the runner does precise kills + schema reset).
- `mise exec --` prefix; the `mise WARN ... goose not found` line is harmless.

## What's next (brainstorm/spec fresh)
Roadmap candidates, each to be brainstormed + spec'd fresh:
- Admin UI / dashboard + consent screen (the "v0.6 — Frontend" text in STATUS.md
  is SEPARATE frontend work; the `/login` page lives here).
- Further security/ops hardening (e.g. the v0.7+ HSM/KMS deferral in AUDIT.md;
  front-channel SLO + SAML encryption deferred from v0.5).
