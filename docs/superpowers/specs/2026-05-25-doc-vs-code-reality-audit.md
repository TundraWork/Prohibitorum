# Doc-vs-code reality audit

Produced 2026-05-25 by a research agent dispatched after the v0.1 commit
series landed. Audits the five user-facing docs (DESIGN/STATUS/AUDIT/
INTEGRATION/README) against actual code state in the repo at commit
`846e7f1`. Driver: STATUS.md overpromised the v0.1.1 smoke test by
claiming endpoints worked that aren't even mounted on the chi router;
user asked whether other docs had similar hallucinations.

Verdict: yes, extensively. The Task 8 doc rewrites were checked against
the spec but not against the code.

## Summary

| Category | Count |
|---|---|
| TRUE | ~30 |
| OVERCLAIM | ~25 |
| HALLUCINATION | ~10 |
| AMBIGUOUS | ~20 |

## Cross-cutting patterns

1. **Unmounted protocol packages.** All OIDC/SAML URLs referenced in the docs (`/oauth/*`, `/oidc/*`, `/saml/*`, `/.well-known/openid-configuration`, `/jwks`) are not mounted. `pkg/server/server.go` does not import `pkg/protocol/oidc` or `pkg/protocol/saml`. Handlers in those packages exist (mostly returning 501; discovery + JWKS would serve content) but are unreachable. Hitting any of those URLs today returns chi 404.

2. **AUDIT.md `✅` overload.** The intro legend collapses schema-only with implemented (line 4–7: "implemented in v0.1 (schema reflects them …)"). Many bare `✅` rows are schema-only without the `schema` suffix the audit reports proposed.

3. **Tense slippage in DESIGN.md.** Multiple sections describe v0.2–v0.5 behavior in present tense, mixed with `(v0.X)` version tags. A reader must scan for the tag to know whether a paragraph is live.

4. **Frontend assumed in README quickstart.** No dashboard, no HTML, no browser flow exists today, but step 4 of the quickstart says "open in browser; register a passkey."

5. **Audit-trail vs running-state separation.** The five user-facing docs sometimes re-quote spec prose verbatim, making them sound implemented.

## Hallucinations (doc references something that doesn't exist)

- **DESIGN.md:** `/me/passkeys/add` endpoint name. Real path: `/api/prohibitorum/me/credentials/register/{begin,complete}`.
- **DESIGN.md:** Transactional delete of password/TOTP/recovery on WebAuthn enrollment — `pkg/authn/flow.go:33` is a stub returning `errors.New("authn.DisableNonWebAuthnFallbacks: TODO(v0.2)")`.
- **STATUS.md:** `pkg/credential/recovery` package. No such package; recovery code helpers live in `pkg/credential/totp/totp.go:42–46`.
- **AUDIT.md:** `pkg/authn/sudo` package reference. No such package; sudo behavior is in `pkg/server/handle_sudo.go`.
- **INTEGRATION.md Pattern B:** `/oauth/introspect` endpoint as if live. Handler doesn't exist (`pkg/protocol/oidc/oidc.go` has Authorize/Token/Userinfo/Logout only, none mounted).
- **INTEGRATION.md:** `zitadel/oidc/v3` "same library Prohibitorum uses on the OP side" — `go.mod` doesn't import it; it's planned for v0.4.
- **README.md step 4:** "Open in browser; register a passkey" — `dashboard/` is empty; no HTML is served by the Go binary.
- **README.md quickstart sequence:** Step 4 (`enroll-admin`) requires `PROHIBITORUM_PUBLIC_ORIGIN` (cmd/prohibitorum/main.go:69–71); README only exports it in step 5.

## Overclaims (doc claims X implemented; code has schema/stub only)

- **DESIGN.md:** Password+TOTP "Cryptography" section reads as live behavior. All v0.2 stubs.
- **DESIGN.md:** OIDC OP "flow" walkthrough presented in present tense. Endpoints not mounted.
- **DESIGN.md:** SAML IdP "flow" walkthrough presented in present tense. Endpoints not mounted.
- **DESIGN.md:** `oidc:code:<random>` KV layout described as fact. No code writes any `oidc:*` keys yet.
- **STATUS.md v0.1.1:** Smoke test step "hit `/.well-known/openid-configuration`" — endpoint returns 404, not a discovery doc.
- **AUDIT.md line 46:** Password argon2id PHC bare `✅`. Schema exists; no Go code writes argon2id PHC strings yet.
- **AUDIT.md line 48:** `password_changed_at` bare `✅`. Schema exists; no code updates it.
- **AUDIT.md lines 117–118:** Discovery and JWKS `✅ stub`. Handler exists but route is not mounted.
- **INTEGRATION.md Pattern A:** Entire flow presented as functional. None of `/authorize`, `/token`, `/.well-known/openid-configuration`, `/userinfo` are mounted.
- **INTEGRATION.md line 19:** "but `/saml/sso` returns 501" — actually returns chi 404 because the route is not mounted.
- **INTEGRATION.md line 277:** `GET /saml/metadata` as live endpoint. Handler exists; not mounted.

## True items

(Recorded for completeness — these claims hold up.)

- All five migrations exist on disk and apply (003 + 004 + 005 verified by the implementer reports against a real Postgres).
- Three-layer package layout (`pkg/account`, `pkg/credential/*`, `pkg/federation/oidc`, `pkg/session`, `pkg/authn`, `pkg/protocol/{oidc,saml}`, `pkg/audit`) exists per DESIGN.md diagram.
- WebAuthn sign-count regression detection at `pkg/server/handle_auth.go:225–229` and `handle_sudo.go:123–127` — AUDIT.md `✅` is accurate.
- WebAuthn `cose_alg`, `user_handle`, `uv_initialized`, `backup_eligible/state` persisted (schema + insert sites at handle_enrollment.go and handle_me.go).
- Session middleware + sliding refresh + disabled-account-per-request check.
- Sudo mode functional via `pkg/server/handle_sudo.go`.
- Rate limiter functional on `/auth/*` endpoints via `pkg/server/handle_auth_ratelimit.go`.
- `pkg/audit.Writer` wired into `server.NewServer` (server.go:97), though no handler calls `s.Audit.Record(...)` yet.
- `configx.Parse()` hard-requires at least one `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>` — STATUS.md operational note is correct.
- `mise.toml` `goose = "3.27.0"` does fail mise's default registry — STATUS.md workaround note is correct.

## Recommended next actions

15 small fixes (each 1–2 lines). Apply via direct Edit in a follow-on commit.

1. **AUDIT.md lines 117–118:** Change "`✅ stub`" to "`⚠️ handler unmounted`" for Discovery and JWKS until server.go wires them.
2. **STATUS.md v0.1.1 smoke step:** Add "(once mounted)" to the `/.well-known/openid-configuration` step.
3. **STATUS.md:** Replace "`pkg/credential/recovery`" with "`pkg/credential/totp` (recovery code helpers live alongside TOTP for now)".
4. **AUDIT.md:** Replace "`pkg/authn/sudo`" with "`pkg/server/handle_sudo.go`".
5. **README.md quickstart:** Move `export PROHIBITORUM_PUBLIC_ORIGIN=...` into step 2 alongside the DEK; required by `enroll-admin`.
6. **README.md step 4:** Replace browser-driven passkey step with HTTP-client instruction; defer browser ceremony to v0.6.
7. **INTEGRATION.md line 19:** Replace "but `/saml/sso` returns 501" with "but the route is not mounted in v0.1 (handler exists in `pkg/protocol/saml` and will return 501 once wired)."
8. **INTEGRATION.md Pattern A:** Insert a status banner: "v0.1 ships the SQL schema for `oidc_client` and handler stubs in `pkg/protocol/oidc`; the OP endpoints land in v0.4."
9. **INTEGRATION.md Pattern B:** Insert a callout: "`/oauth/introspect` ships in v0.4; not present in v0.1."
10. **INTEGRATION.md:** Replace "same library Prohibitorum uses on the OP side" with "(planned for v0.4)".
11. **DESIGN.md:** Fix `/me/passkeys/add` endpoint name; mark transactional-disable as v0.2.
12. **DESIGN.md "Authentication methods":** Move v0.2+ subsections to explicit future tense or add a header banner.
13. **AUDIT.md legend (lines 4–7):** Add a sentence: "Bare `✅` means *some* code path enforces the item; `✅ schema`, `✅ design`, `✅ planned`, `✅ stub` qualify what's actually in v0.1."
14. **AUDIT.md line 46 (password argon2id PHC):** Change bare `✅` to `✅ schema`.
15. **AUDIT.md line 48 (`password_changed_at`):** Change bare `✅` to `✅ schema`.

## Adjacent fix worth considering

Wire `/.well-known/openid-configuration` and `/jwks` on the chi router in a one-line `server.go` edit. The handlers exist; mounting them gives v0.1.1 something real to smoke-test. Other OIDC/SAML routes can stay unmounted (or all be mounted with their 501 stubs — same blast radius). This is the cheapest path to making v0.1 smoke-testable.
