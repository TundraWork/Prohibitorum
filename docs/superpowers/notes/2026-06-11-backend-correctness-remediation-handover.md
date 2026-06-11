# Handover — Backend correctness remediation (post FE-surface audit)

**Date:** 2026-06-11. **Branch:** `master` (commit directly; no remote). **Tree:** clean (audit was read-only).
**Read first:** `MEMORY.md` + project memories, then this note, then the full audit it implements:
`docs/superpowers/notes/2026-06-11-backend-functionality-correctness-audit.md`.

## ▶ THE TASK
Implement the remediation for the 2026-06-11 audit. The audit took the **whole frontend surface** (admin +
end-user views) as the behavioral spec and verified backend correctness against OIDC/SAML/WebAuthn/NIST
standards. The core protocol crypto is sound — **no Critical artifact break**. The work is authz-hardening +
interop conformance + truth-in-labeling + dead-control cleanup. Findings are concrete and anchored; this note
is the build order, the exact edit sites, and the gotchas.

**Verification legend (trust calibration for each item):**
🔎 = orchestrator independently re-read the code and confirmed. 📄 = subagent-cited, plausible, **re-read
before acting** (1 file). Do the 🔎 items with confidence; verify the 📄 ones first.

---

## TIER 1 — security / authz (do first)

### T1.1 ⚠️HIGH — fresh-sudo gate the account/invitation mutations 🔎
**Problem:** 6 admin **mutations** use `registerOp` (Huma, admin-role only — no fresh sudo), while every other
admin mutation goes through `registerSudoOpHTTP` (`pkg/server/operations.go:123` — admin auth + fresh sudo +
content-type + 64 KiB cap). Includes **privilege escalation** (`UpdateAccount` user→admin). A stolen/replayed
admin cookie can escalate or delete with no step-up.
**Sites (`pkg/server/server.go`):** `:375` UpdateAccount · `:376` DeleteAccount · `:381` RevokeAccountSessions
· `:382` ReissueEnrollment · `:383` CreateInvitation · `:385` RevokeInvitation. Handlers in
`pkg/server/handle_account.go` (`handleUpdateAccount:140`, `handleDeleteAccount:247`,
`handleRevokeAccountSessions:453`) + `handleReissueEnrollment`/`handleCreateInvitation`/`handleRevokeInvitation`.
Confirmed: no `requireFreshSudo` anywhere in `handle_account.go`.
**Fix:** give each a raw-`net/http` handler variant and register via `s.registerSudoOpHTTP(...)` — copy the
pattern of the already-correct siblings `handleDeleteAccountCredentialHTTP` (`handle_account.go:369`, registered
`server.go:377`) and `handleRevokeAccountSessionHTTP` (`:725`, registered `:380`). These currently use Huma
path/body binding (`{id}` path param, JSON body) — the HTTP variants must re-parse `chi.URLParam` + decode the
body (see the credential-delete sibling for the idiom). Keep the OpenAPI registration if you want the spec
entry, but the *route that executes* must be the sudo-gated one (the siblings register the Huma op for docs AND
the chi route for execution — mirror that).
**Also:** this finding makes **`AUDIT.md:430` false** ("single chokepoint for all 🔐 admin mutations"). Fix the
doc in the same commit (see Tier-4). `admin_route_policy_test.go`'s `TestAdminMutationRoutesRequireSudo`
iterates a hand-maintained allowlist — add the 6 routes to its expected set so it would catch a regression.
**Decision flag:** confirm with product that account-lifecycle SHOULD require sudo (it should — role escalation
is more sensitive than an OIDC redirect-URI edit). Almost certainly yes.

### T1.2 ⚠️ — disabled-account check on reset-enrollment 🔎
**Problem:** `IntentReset` loads the target account but never checks `account.Disabled`. A reset token
(admin-issued, 24h TTL) for a disabled account lets the holder wipe its credentials + register a passkey +
get a session. Mitigated (next-request `LoadSession` revokes the session) but credential destruction +
attacker passkey persist.
**Sites (`pkg/server/handle_enrollment.go`):** begin `:239-258` (`GetAccountByID` at `:248`), complete
`:439-474` (`GetAccountByID` at `:444`). The only `Disabled` refs nearby are `Disabled:false` on NEW
bootstrap/invite accounts (`:366`, `:422`) — not a check.
**Fix:** in both reset branches, after loading the account, `if a.Disabled { writeAuthErr(w,
authn.ErrEnrollmentConsumed()); return }` (or a dedicated sentinel). Match the disabled-reject idiom used in
`handle_auth.go:217` / `handle_auth_password.go:79`.

### T1.3 ⚠️ — add-passkey sudo gating (DECISION, then likely gate) 🔎route/📄logic
**Problem:** `/me/credentials/register/{begin,complete}` is `sessionReq`, not sudo (`server.go:333-334`);
handlers only check `sess != nil` (`handle_me.go:212`, 📄 re-read). Contradicts the code's own sudo threat
model (`handle_sudo.go` package doc) — and `/me/devices/pair/approve` (same outcome: add an authenticator) IS
sudo-gated. FE labels it intentional (`dashboard/src/pages/security/PasskeysCard.vue`).
**Fix (if gating):** wrap both routes with the sudo check (they're `registerOpHTTP` today; you'll need the
sudo-aware variant or an in-handler `requireFreshSudo` like the `/me/password/set` etc. raw handlers do — check
how `handleMePasswordSetHTTP` enforces sudo and mirror it), and make FE `PasskeysCard.add()` use the `withSudo`
retry wrapper (`dashboard/src/lib/sudo.ts`). **Decision flag:** product call — gate, or document the exemption
with rationale. Recommended: gate (consistency with pairing + the threat model).

---

## TIER 2 — protocol / interop conformance

### T2.1 ⚠️ — SAML redirect-binding LogoutResponse detached signature 🔎
**Problem:** `writeRedirectLogoutResponse` (`pkg/protocol/saml/slo.go:454-483`) sets only
`SAMLResponse`(+`RelayState`) — no `SigAlg`/`Signature`. SAML Bindings §3.4.4.1 requires a **detached**
signature over the URL-encoded query for HTTP-Redirect; the enveloped XML sig is ignored post-DEFLATE. Strict
SPs treat the SLO response as unsigned and reject.
**Fix:** sign the deflated query string with the active key (RSA-SHA256) and append
`SigAlg=<rsa-sha256>&Signature=<b64>`. The **inbound** verifier already builds this exact octet string —
`verifyRedirectSignature` in `pkg/protocol/saml/authnreq.go` (search `rsaSHA256SigAlg`); reuse its
construction order. Build the LogoutResponse XML *without* the enveloped sig on this path (or leave it — it's
ignored). Add a `slo_test.go` assertion on the `SigAlg`/`Signature` query params (current tests only check the
enveloped sig inside `SAMLResponse`, so the gap is untested). The **POST**-binding response (`writeAutoPost`,
enveloped) is already correct — don't touch it.
**Scope note:** if all production SPs use POST SLO, real-world impact is low, but the redirect binding is
advertised in metadata and reachable — fix it.

### T2.2 ⚠️ — consent "deny" omits RFC 9207 `iss` 🔎
**Site:** `pkg/server/handle_consent.go:104-109` (deny branch sets only `error`+`state`). OP advertises
`authorization_response_iss_parameter_supported:true` (`pkg/protocol/oidc/oidc.go:86`); RFC 9207 §2 requires
`iss` on error responses too. **Fix:** `q.Set("iss", <issuer>)` — get the issuer the same way `redirectError`
(`pkg/protocol/oidc/errors.go`) does. Trivial.

### T2.3 ⚠️ — strict `prompt` validation 📄
**Site:** `pkg/protocol/oidc/authorize.go:132-138` only handles `login`/`none` (+ rejects `login`+`none`);
ignores `select_account`/unknown and doesn't advertise `prompt_values_supported`. **Fix:** validate tokens
against the supported set, enforce `none`-exclusivity generally, advertise or reject unknowns. Low impact
(single-tenant, no account selection) — lower priority within Tier 2.

---

## TIER 3 — truth-in-labeling + audit completeness

### T3.1 ⚠️ — federation "Disabled" FE text is wrong + 500-on-disabled-mid-flow 🔎 (also corrects prior audit)
**Backend truth (verified):** `GetUpstreamIDPBySlug` = `WHERE slug=$1 AND NOT disabled`
(`db/queries/upstream_idp.sql:2`), re-run at callback (`pkg/federation/oidc/federation.go:313`). So disabling
an upstream IdP is a **hard kill-switch** — already-linked users are locked out too (and the disabled-mid-flow
path 500s, `federation.go:314-316`). **The prior `2026-06-10-...FINDINGS.md` §1 claim ("re-login bypasses the
disabled gate, by design") is WRONG** — note that when you touch that doc.
**Fixes:**
- FE string `dashboard/src/locales/en.ts:264` — currently: *"Hides this provider from the sign-in screen and
  blocks new logins and links. Accounts already linked can still sign in."* → change the last sentence to state
  that disabling blocks ALL sign-in, including existing links. (Mind the en.ts apostrophe lint guard.) Rebuild
  FE (see conventions) since this ships in the embedded bundle.
- Backend `federation.go:314-316`: map `pgx.ErrNoRows` to a clean `ErrFederationStateInvalid()` + a
  `failNoAccount(..., "idp_disabled_or_deleted", ...)` audit row, matching how `begin()` (`:233-239`) handles
  the same lookup error — instead of a wrapped 500 with no audit.

### T3.2 🔴/🟡 — fix the OIDC scope checkbox set 🔎
`email` scope is a **dead control** (FE `AdminOidcClientsView.vue:171` + `AdminOidcClientDetailView.vue:169`):
stored, requestable, consented, but no email claim exists (none in `pkg/protocol/oidc/claims.go`/`userinfo.go`;
discovery `scopes_supported` is `[openid,profile,offline_access]`, `oidc.go:77`). Meanwhile `offline_access`
(live: refresh tokens, `token.go:216`) has **no FE checkbox**. **Fix:** replace the `email` checkbox with
`offline_access` in both Vue forms (or, if email is roadmapped, implement an email attribute→claim→discovery
entry — bigger). Minimally, also reject unsupported scopes server-side in create/update
(`handle_admin_oidc_clients.go` → `clientgen.go`; today no scope whitelist).

### T3.3 🟡 — audit rows for account/invitation mutations 📄
The 6 Tier-1.1 mutations write only `logx` lines, not `credential_event` rows (`handle_account.go:222,294,473,
518,578,655`) — role escalations/deletes are invisible in the admin audit viewer. **Fix:** emit
`audit.Writer.Record` rows (factor e.g. `account`/`invitation`, event `update`/`revoke`/`register`) — mirror
the signing-key/OIDC mutation handlers. Pairs naturally with the T1.1 commit. Keep the no-secret-in-detail
invariant.

### T3.4 🟡 — validate `expectedUpstreamIdpSlug` at invite create 📄
`handleCreateInvitation` (`handle_account.go:551-568`) stores the slug verbatim; an invite bound to a
non-existent/disabled slug is permanently un-redeemable. **Fix:** look up the slug (allow only existing,
arguably non-disabled) at create; 400 otherwise. FE already filters, so this is defense-in-depth.

---

## TIER 4 — cleanup / schema honesty (lowest risk, do last)

### T4.1 🔴 — drop the two SAML no-op fields end-to-end 🔎
`want_assertions_signed` (assertions ALWAYS signed, `assertion.go:202`) and `name_id_claim` (NameID always
opaque per-SP) are no-ops; FE controls already removed, but PUT body + columns remain (dead writes). **Drop**
(not wire — both conflict with the design): remove from the update body `handle_admin_saml_sps.go:161`
(`WantAssertionsSigned *bool`) + the write `:200`/`:341`/`:344`; the create body `:288`/`:291`; the
`SAMLApplicationView` fields `pkg/contract/auth.go:450,453` + their reads `handle_admin_saml_sps.go:58,61`; the
sqlc params; and the columns in migration 009.

### T4.2 🔴 — migration 009 dead-column prune 🔎/📄
Drop columns with no roadmap: `oidc_client.{contacts, application_type, id_token_signed_response_alg,
default_max_age, require_auth_time}`; `saml_sp.{authn_requests_signed, want_assertions_signed, name_id_claim}`.
**Keep** (deferred-feature schema, per `2026-06-08-backend-backlog.md`): `oidc_client.subject_type` (pairwise,
T4) and `saml_sp.{metadata_valid_until,metadata_cache_duration,metadata_fetched_at}` (metadata auto-refresh).
Also keep legacy `signing_key.active`/`retired_at` (separate deferred 009 in older docs — coordinate the
migration number). Run `mise exec -- sqlc generate` after the migration and rebuild.

### T4.3 minor — hygiene 📄
- `password_changed_at` bumped on transparent rehash-on-verify (`pkg/credential/password.go:114` →
  `UpsertPasswordCredential` sets it unconditionally). Add a rehash-only query that leaves `password_changed_at`.
- Add-passkey ceremony stash uses `Get`+best-effort `Del` (`handle_me.go:261,327`) vs login's single-use `Pop`;
  switch to `Pop` for parity (replay already blocked by `credential_id` UNIQUE + one-time challenge).
- Re-login `(iss,sub)` lookup not scoped to the IdP (`modes.go:66`); harmless unless two `upstream_idp` rows
  share an `issuer_url`. Either enforce `existing.UpstreamIdpID == idp.ID` on re-login or document "one issuer ⇒
  one row." **Decision flag.**

---

## DOC corrections owed (fold into the relevant commits)
- **`AUDIT.md:430`** — "single chokepoint for all 🔐 admin mutations" is false until T1.1 lands. After T1.1
  it becomes true; if T1.1 is deferred, change the wording to name the carve-out.
- **`docs/superpowers/notes/2026-06-10-backend-config-audit-FINDINGS.md` §1** — the "disabled re-login bypass,
  by design" ⚠️ is factually wrong (T3.1). Add a correction note pointing at the 2026-06-11 audit.

## Decisions needing a human/product (don't guess)
1. T1.3 add-passkey: gate with sudo, or document the exemption? (recommend gate)
2. T3.2 `email` scope: drop the control, or implement an email claim? (recommend drop for now)
3. T4.3 same-issuer-two-rows: supported config or not? (drives whether to scope the re-login lookup)

---

## Conventions / environment (mostly INHERITED from 2026-06-10 handover — re-verify the ⚠️ ones)
- `go build ./... && go vet ./...` exit 0 is **authoritative** over stale gopls (false `MissingLitField` after
  out-of-editor `sqlc generate` — run `mise exec -- sqlc generate`). gofmt repo-wide pre-existing-dirty.
- sqlc: `sqlc.yaml` at root; regenerate with `mise exec -- sqlc generate` after any `db/queries/*.sql` or
  migration change. Generated code lands in `pkg/db/*.sql.go`.
- **FE:** real typecheck is `cd dashboard && npm run build` (or `vue-tsc -b`), NOT `--noEmit`. After ANY Vue/
  en.ts edit, rebuild + `git add pkg/webui/dist` (the Go binary embeds `pkg/webui/dist` via
  `pkg/webui/webui.go`) — do this only at a done-gate. `en.ts` has an apostrophe lint guard.
- ⚠️ **Smoke harness must be re-established** — the prior session's `/tmp/run_v06.sh` + `/tmp/v06.result` are
  GONE this session, and dev PG (`/tmp/prohibitorum-pg`, :55432) was NOT responding when this handover was
  written. `cmd/smoke/main.go` is the 121-step end-to-end harness (the best "is it really wired" oracle). To run
  it you need: dev PG up, env (`PROHIBITORUM_DATABASE_URL`, `PROHIBITORUM_DATA_ENCRYPTION_KEY_V1`,
  `PROHIBITORUM_PUBLIC_ORIGIN`), the dev server (`mise dev-server`), then `mise exec -- go run ./cmd/smoke`.
  `mise dev-seed` + `mise enroll-admin -- --new` for a live dashboard look. Mise tasks available: server, web,
  build, frontend-build, db:up, db:status, dev-server, dev-seed, enroll-admin, openapi.
- Process hygiene: NEVER bare `pkill -f prohibitorum` (matches the dev PG `-D /tmp/prohibitorum-pg`); use
  `pkill -f 'go-build.*/prohibitorum'` or `pkill -f 'cmd/prohibitorum'`.
- Per-finding verification status (🔎 vs 📄) is in the audit doc's ranked table — trust 🔎, re-read 📄 before editing.

## Suggested commit sequencing
T1.1 (+AUDIT.md fix +T3.3 audit rows, they share the handlers) → T1.2 → T1.3 (after decision) → T2.1 → T2.2 →
T2.3 → T3.1 (+prior-FINDINGS correction; FE rebuild) → T3.2 (FE rebuild) → T3.4 → T4.1+T4.2 (migration 009 +
sqlc regen + rebuild) → T4.3. Build/vet after each; run the smoke before the final gate. FE rebuilds only at
the relevant gates.

## References
- Full audit (every finding, file:line, standard URL, ✅-correct list): `docs/superpowers/notes/2026-06-11-backend-functionality-correctness-audit.md`.
- Prior column-wiring audit (mostly correct; §1 wrong): `docs/superpowers/notes/2026-06-10-backend-config-audit-FINDINGS.md`.
- Deferred-vs-broken reference: `docs/superpowers/notes/2026-06-08-backend-backlog.md`.
- Protocol design specs: `docs/superpowers/specs/` (v0.3 federation, v0.4 OIDC OP, v0.5 SAML, v0.6 completeness).
- Route table: `pkg/server/server.go:298-443` (registerOperations).
