# Session handoff — v0.6 protocol completeness COMPLETE (11 tasks + post-impl audit)

> Future Claude: v0.6 is fully shipped, audited, and smoke-verified end-to-end.
> This records the end state. The prior chunk (v0.5 SAML IdP) is in
> `docs/superpowers/notes/2026-05-30-session-handoff.md`. The next chunk is a NEW
> version — brainstorm/spec it fresh.

## TL;DR — v0.6 is DONE

**Protocol completeness — the deferred OIDC OP + SAML IdP behaviors, all backend,
all smoke-verified end-to-end.** Forced re-authentication (OIDC `prompt=login` /
`max_age`, SAML `ForceAuthn` — full fresh re-login via a single-use account-bound
KV nonce gate), OIDC PKCE method policy (S256-only, `plain` DB-forbidden) +
public-client introspection now disallowed (RFC 7662), SAML `NameIDPolicy/@Format`
honoring (`InvalidNameIDPolicy`), POST-binding AuthnRequest intake, signed IdP
metadata + `validUntil`/`cacheDuration`, and SAML IdP-initiated SSO (unsolicited
Response, per-SP `allow_idp_initiated` opt-in, default-ACS-only).

- **All 11 plan tasks (0–10)** via subagent-driven development (opus implementers
  → spec + quality reviews → fix loops → `.tasks.json` synced).
- **Smoke GREEN end-to-end:** `cmd/smoke` steps 100–111 (+ extended mock SP in
  `cmd/smoke/saml_mock.go`) drive every v0.6 behavior against live PG + a real
  dev server. Final: `45/45 (v0.2) + 46–69 (v0.3) + 70–87 (v0.4) + 88–99 (v0.5) +
  100–111 (v0.6)`, `SMOKE_EXIT=0`. Re-run: `setsid bash /tmp/run_v06.sh` then
  `cat /tmp/v06.result` (username `smoke-v06-admin`).
- **Post-implementation audit (done):** 4-lens (crypto/XML-DSig + protocol +
  race + deep). **No Critical, no real High in v0.6's own code.** Fixed across 2
  batches: `c1523a0` (re-auth marker now account-bound + atomic `Pop` — the
  convergent race+deep finding), `5643e35` (oidc-client post-logout NOT-NULL
  default, NoPassive→Responder, sloParseError errBadSigAlg, MetadataValidity<=0
  guard, no-PKCE token exchange). Full record in AUDIT.md → "v0.6 post-impl audit
  (2026-05-31) — done".

```
HEAD: 16ad6f7   branch: master   working tree: clean
go build ./... ✓   go vet ./... ✓   go test ./... ✓   smoke ✓
```

## What shipped (anchors)
- Re-auth gate: `pkg/authn/reauth.go` (`DemandReauth`/`ConsumeReauth`, account-bound single-use KV nonce). Used by `pkg/protocol/oidc/authorize.go` (prompt=login/max_age) + `pkg/protocol/saml/sso.go` (ForceAuthn).
- OIDC: `authorize.go` (prompt/max_age/PKCE policy), `introspect.go` (public rejected), `token.go` (no-PKCE gate).
- SAML: `sso.go` (ForceAuthn + NameIDPolicy + POST intake), `authnreq.go` (POST-binding decode + enveloped verify), `sso_init.go` (**new** IdP-initiated), `metadata.go` (signed + validUntil).
- Schema: `saml_sp.allow_idp_initiated` (migration 005), `oidc_client` plain-CHECK (migration 002), `configx.SAML.MetadataValidity`.
- Endpoints: `GET /oauth/authorize`, `POST /oauth/introspect`, `GET|POST /saml/sso`, `GET /saml/metadata`, **new** `GET /saml/sso/init`. CLI: `saml-sp create --allow-idp-initiated`.
- Spec: `docs/superpowers/specs/2026-05-31-v0.6-protocol-completeness-design.md` (D1–D12 + research appendix). Plan: `docs/superpowers/plans/2026-05-31-v0.6-protocol-completeness.md` + `.tasks.json` (11/11).

## ▶ NEXT SESSION — implement the session-cookie scoping fix (spec written, ready to plan)

The v0.6 deep audit surfaced a **session-cookie path vs protocol-route mismatch**
(pre-existing): the cookie is `Path=/api/prohibitorum` but the OIDC/SAML routes
are root-level (`/oauth/authorize`, `/saml/sso`, `/saml/sso/init`, `/saml/slo`),
so a real browser never sends the cookie to them → the session gate loops to
`/login`. `cmd/smoke` masks it by manually re-attaching the cookie.

**This was researched + brainstormed this session; the fix is spec'd and decisions
are LOCKED.** Spec: `docs/superpowers/specs/2026-05-31-session-cookie-scoping-design.md`
(D1–D5). Summary: scope the session cookie `Path=/`; keep `SameSite=Lax`; adopt
the `__Host-` prefix + `Secure` ONLY in secure deployments (`PUBLIC_ORIGIN`=https),
falling back to a plain non-`Secure` `Path=/` cookie in HTTP dev (so Go's
cookie-jar — and the smoke — can send it; `__Host-` requires `Secure` which an
http client/jar won't transmit). `CeremonyCookie` unchanged; no route/issuer/
metadata changes. The smoke then DROPS its manual cookie re-attach and
behaviorally proves the fix.

**Resume:** re-enter `superpowers-extended-cc:writing-plans` on that spec → then
`subagent-driven-development` (same rhythm). It's a small, contained change
(mostly `pkg/session/middleware.go` + a smoke cleanup + unit tests). After it
lands + smoke green, update `AUDIT.md` to CLOSE the v0.6 architectural finding.
Brainstorming is already complete — go straight to writing-plans.

## Accepted / deferred (see AUDIT.md)
max_age no clock-skew (fails stricter); prompt=consent/select_account ignored (consent out of scope); signed-metadata two-read rotation race (narrow); ForceAuthn+POST-binding fails-safe; front-channel SLO + encryption (from v0.5).

## Conventions / runtime quirks (unchanged — these bite)
- Master branch (user-authorized project-wide). opus implementers, sonnet/opus reviewers, never haiku.
- **Trust `go build ./...` exit 0 + `go vet`, NOT gopls `<new-diagnostics>`** — they FALSELY report cross-file "undefined" / "BrokenImport: crewjam" / regenerated-sqlc "undefined method" / "WrongArgCount" mid-edit on code that builds clean. Verified repeatedly this session.
- **NEVER `pkill -f 'prohibitorum'`** (kills the PG at `/tmp/prohibitorum-pg`). Detached `setsid bash /tmp/run_v06.sh` smoke runner (the Bash tool SIGPIPEs on long pipelines).
- `mise exec --` prefix; `mise exec sqlc -- sqlc generate`; pre-deployment squash (amend migrations in place).
- The deep+race audit passes keep earning their keep — they find stateful/integration bugs the schema-resetting smoke structurally can't. Keep doing them.

## What's next
**Immediate (spec ready):** implement the session-cookie scoping fix per the
"NEXT SESSION" section above — go straight to writing-plans on
`docs/superpowers/specs/2026-05-31-session-cookie-scoping-design.md`.

After that, the remaining roadmap candidates (admin UI / dashboard + consent
screen — the "v0.6 — Frontend" text in STATUS.md is SEPARATE frontend work; or
further security/ops hardening) should each be brainstormed + spec'd fresh.
