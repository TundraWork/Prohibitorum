# Backend functionality correctness + completeness audit (FE-surface-driven)

> **REMEDIATED (2026-06-11).** All four tiers of findings below were fixed in a
> tier-by-tier remediation cycle on `master`:
> - **Tier 1 (authz):** the six admin account/invitation mutations are now
>   fresh-sudo-gated (`registerSudoOp` + a shared `consumeFreshSudo` chokepoint);
>   reset-enrollment rejects disabled accounts; add-passkey begin is sudo-gated;
>   audit rows added for all six mutations. (commit `cdf5fc2`)
> - **Tier 2 (interop):** SAML redirect-binding LogoutResponse is now
>   detached-signed; consent-deny carries the RFC 9207 `iss`; `prompt` is strictly
>   validated. (commit `f54f780`)
> - **Tier 3 (truth/claims):** disabled-IdP mid-flow now returns a clean
>   state-invalid error + audit row; invite slug is validated; re-login is scoped
>   to one issuer↔one IdP; the **`email` scope is now a real claim**
>   (account.email column + federation provisioning + scope whitelist +
>   offline_access). (commits `2dd98cf`, `8b54cb8`)
> - **Tier 4 (hygiene):** dropped no-op SAML/OIDC columns (migration 010);
>   password param-upgrade rehash no longer bumps `password_changed_at`.
>   (commit `b07ceaf`)
> - **FE + docs:** dashboard rebuilt; INTEGRATION.md updated. (commit `9c54170`)
>
> The body below is the original audit, preserved as the record of what was found.

**Date:** 2026-06-11. **Branch:** `master`. **Tree:** clean (read/verify only; no code changed).
**HEAD context:** after the v0.6 dashboard ship + the 2026-06-10 column-wiring audit.

## Scope & method

Goal (per request): take the **entire frontend surface** (admin + end-user views, every option and
action) as the behavioral spec, and verify the backend implements each **correctly** — not just "is the
column read" (that was the 2026-06-10 pass) but "is the protocol behavior standards-correct," since an IdP
talking to upstream OPs and downstream RPs/SPs must be exactly right.

Method: five parallel domain reviews (OIDC OP downstream, OIDC upstream federation, SAML IdP downstream,
credential/auth ceremonies, admin lifecycle/accounts), each tracing every FE option/action to its backend
handler with `file:line` citations and checking it against the actual standard (RFCs / OIDC / SAML /
WebAuthn / NIST, researched where subtle). **Every serious finding below was then independently
re-verified against the code by the orchestrator** — flagged per finding as 🔎 *code-verified* vs
📄 *agent-cited (not independently re-read)*. `go build ./... && go vet ./...` clean throughout.

## Headline

**The core protocol crypto is sound and well-tested.** Signature verification (OIDC RS256 alg-pinning,
SAML cert-pinned + positive RSA-SHA256 allowlist + layered XSW/XXE defenses), PKCE S256, auth-code
single-use + replay→family-revoke, refresh rotation + reuse detection, RFC 9068 access tokens, RFC 7662/7009
introspect/revoke, RP-Initiated Logout, stable opaque SAML NameID, ACS open-redirect guard, TOTP AES-GCM +
replay defense, argon2id, session-cookie hardening, and the **signing-key rotation state machine** all
verify as correct. There are **no Critical breaks** in the parts that mint or validate protocol artifacts.

The findings cluster in four areas: **(A) an admin sudo-gating policy gap** (the highest-stakes item),
**(B) a few interop/spec-conformance edges**, **(C) truth-in-labeling**, and **(D) dead / unexposed config
controls**. One of these also **corrects an error in the prior 2026-06-10 FINDINGS doc** (see ⚠️-1).

---

## Ranked findings

| # | Sev | Area | Finding | Verify |
|---|-----|------|---------|--------|
| 1 | ⚠️ **high** | Admin authz | 6 admin mutations incl. **role-escalation** are admin-role-only, **not fresh-sudo gated** | 🔎 |
| 2 | ⚠️ | SAML interop | Redirect-binding **LogoutResponse is not detached-signed** (Bindings §3.4.4.1) | 🔎 |
| 3 | ⚠️ | Federation | **FE "Disabled" text contradicts backend** + disabled-mid-flow → HTTP 500 (and the prior audit's §1 was wrong) | 🔎 |
| 4 | ⚠️ | Credentials | **Reset-enrollment skips the disabled-account check** | 🔎 |
| 5 | ⚠️ | Credentials | **Add-passkey is not sudo-gated** (pairing route to same outcome *is*) | 🔎 route / 📄 logic |
| 6 | ⚠️ | OIDC OP | Consent **"deny" redirect omits the RFC 9207 `iss`** param | 🔎 |
| 7 | 🔴 | OIDC OP | **`email` scope is a dead FE control** — stored, requestable, consented, never produces a claim | 🔎 |
| 8 | 🟡 | OIDC OP | **`offline_access` not exposed** in the admin UI (live backend capability, no checkbox) | 🔎 |
| 9 | 🟡 | Admin audit | Account/invitation mutations **write no `credential_event` rows** → invisible in audit viewer | 📄 |
| 10 | ⚠️ | OIDC OP | `prompt` not strictly validated (`select_account` ignored; `none`-exclusivity only vs `login`) | 📄 |
| 11 | 🟡 | Invitations | `expectedUpstreamIdpSlug` not validated server-side at create | 📄 |
| 12 | 🔴 | OIDC/SAML | Confirmed dead/no-op columns (carried from 2026-06-10) — cleanup, not bugs | 🔎/📄 |
| 13 | minor | various | `password_changed_at` bumped on silent rehash; add-passkey stash `Get` vs `Pop`; same-issuer-two-rows re-login scoping | 📄 |

Severity legend: ⚠️ correctness/security issue worth fixing · 🔴 no-op (FE control does nothing) · 🟡
capability exists but unexposed/uncaptured · "minor" = robustness/hygiene. No ⛔ (FE-implies-but-backend-
entirely-absent) found; #2 is the closest (a feature that is *present but non-conformant* on one binding).

---

## Detail

### ⚠️-1 — Admin sudo-gating is inconsistent; role-escalation is not step-up-protected 🔎
**The single most important finding.** `registerSudoOpHTTP` (`pkg/server/operations.go:123`) is the
documented "single chokepoint" for admin mutations (admin auth + **fresh sudo** + content-type + 64 KiB cap),
and it gates signing-keys, OIDC/SAML/IdP app CRUD, credential force-revoke, and single-session revoke. But
six **mutations** are registered via plain `registerOp` (Huma, admin-role check only — no sudo):

- `UpdateAccount` — **promotes user→admin**, disables, edits attributes (`server.go:375` → `handle_account.go:140`)
- `DeleteAccount` (`server.go:376`)
- `RevokeAccountSessions` — bulk (`server.go:381`)
- `ReissueEnrollment` (`server.go:382`), `CreateInvitation` (`server.go:383`), `RevokeInvitation` (`server.go:385`)

🔎 Verified: those six are `registerOp` (lines confirmed), and `handle_account.go` contains **no**
`requireFreshSudo` call (its only `sudo` mentions are doc-comments on the *other*, sudo-gated raw-HTTP
handlers). So a stolen/replayed admin session cookie can **escalate privilege or delete an account with no
step-up**, while a lower-impact OIDC redirect-URI edit demands a fresh WebAuthn assertion. The FE's `withSudo`
wrapper is a no-op unless the server returns `sudo_required`, which these endpoints never do.

This also makes **`AUDIT.md:430` inaccurate** ("single chokepoint for all 🔐 admin mutations").
`TestAdminMutationRoutesRequireSudo` does not catch it (it iterates a hand-maintained allowlist).
**Action:** move the six to `registerSudoOpHTTP` (needs raw-HTTP handler variants, like the credential-delete
sibling already has) — strongly preferred given role-escalation is in the set — or consciously document and
justify the carve-out. Either way correct the AUDIT.md claim.

### ⚠️-2 — SAML redirect-binding LogoutResponse is not detached-signed 🔎
`writeRedirectLogoutResponse` (`pkg/protocol/saml/slo.go:454-483`) deflates the LogoutResponse XML into
`SAMLResponse` and sets only that (+ optional `RelayState`) — **no `SigAlg` / `Signature` query params**.
Per **SAML 2.0 Bindings §3.4.4.1**, a signed message on HTTP-Redirect MUST carry a *detached* signature over
the URL-encoded query string; an enveloped XML `<ds:Signature>` is not verifiable after DEFLATE+base64 and is
ignored. The IdP's *inbound* redirect verifier (`authnreq.go` `verifyRedirectSignature`) correctly implements
the detached form — so the asymmetry is plain. A strict SP (Shibboleth/ADFS/simplesamlphp) consuming the SLO
response over redirect treats it as unsigned and rejects it. POST-binding SLO response (enveloped) is correct.
**Action:** for the redirect SLO-response path, sign the deflated query string (RSA-SHA256) and emit
`SigAlg`+`Signature` exactly as the inbound verifier reconstructs them. Standard:
http://docs.oasis-open.org/security/saml/v2.0/saml-bindings-2.0-os.pdf §3.4.4.1.

### ⚠️-3 — Federation "Disabled" semantics: FE text is wrong, and so was the prior audit 🔎
**Corrects 2026-06-10 FINDINGS §1.** That doc claimed disabling an upstream IdP still lets already-linked
accounts re-login ("by design", ⚠️). **That is false.** 🔎 Verified: `HandleCallback` re-looks-up the IdP at
`federation.go:313` via `GetUpstreamIDPBySlug`, whose SQL is `WHERE slug=$1 AND NOT disabled`
(`db/queries/upstream_idp.sql:2`). A disabled IdP → `ErrNoRows` → the callback aborts. So **disabling is a
hard kill-switch — existing linked users are locked out too.**

Two consequences:
- The FE description (`dashboard/src/locales/en.ts`, the upstream-IdP "Disabled" help text) says *"Accounts
  already linked can still sign in"* — directly contradicting the backend. An admin disabling an IdP for
  cleanup would unexpectedly lock out a whole population. **Action:** fix the FE text to "blocks all sign-in,
  including existing links."
- The disabled-mid-flow / just-deleted-IdP case returns a wrapped **HTTP 500** with no audit row
  (`federation.go:314-316`), where `begin()` cleanly collapses the same lookup error to
  `federation_state_invalid`. **Action:** map `ErrNoRows` here to a clean `federation_state_invalid` + audit.

(Note: the *local* account `Disabled` flag **is** re-checked on every federated login at `federation.go:371`
→ `bad_credentials`, so a disabled *local account* cannot federate in — that scenario is correctly closed.)

### ⚠️-4 — Reset-enrollment ceremony skips the disabled-account check 🔎
`IntentReset` loads the target account (`handle_enrollment.go:248` begin, `:444` complete) but never checks
`account.Disabled` — the only `Disabled` references in that file set `Disabled:false` on *new* bootstrap/invite
accounts. Every other credential path re-checks disabled (WebAuthn login, password, recovery, pairing). So a
reset token (admin-issued, 24h TTL) issued for an account that is (or becomes) disabled lets the holder **wipe
the account's existing credentials, register a fresh passkey, and receive a session**. Partially mitigated:
`session.LoadSession` re-checks `Disabled` per request and revokes the planted session — but the credential
destruction + attacker passkey persist (and become live if the account is later re-enabled). NIST SP 800-63B
account-lifecycle expects a disabled identity to be unusable across all enrollment surfaces.
**Action:** reject with `ErrEnrollmentConsumed()` (or similar) when `a.Disabled` on both reset begin + complete.

### ⚠️-5 — Add-passkey is not sudo-gated 🔎(route) / 📄(logic)
`/me/credentials/register/{begin,complete}` is `sessionReq`, not sudo (`server.go:333-334`); the handlers only
check `sess != nil` (📄 `handle_me.go:212`). This contradicts the codebase's **own** sudo threat model
(`handle_sudo.go` package doc: defend against a stolen session planting a backdoor authenticator) — and the
*pairing* route to the same outcome (`/me/devices/pair/approve`) **is** sudo-gated. `excludeCredentials` + UV=Required
are correctly applied, and the last-passkey-delete guard is row-locked, so this is purely the missing step-up.
The FE documents it as intentional (`PasskeysCard.vue`), so it needs a **product decision**, but it is an
inconsistent gate. **Action:** gate add-passkey behind fresh sudo (and have the FE `add()` use `withSudo`),
or document why adding an authenticator is exempt when approving a paired device is not.

### ⚠️-6 — Consent "deny" redirect omits the RFC 9207 `iss` 🔎
`handle_consent.go:104-109` builds the `error=access_denied` redirect with only `error` + `state` — no `iss`.
The OP advertises `authorization_response_iss_parameter_supported: true` (`oidc.go:86`), and every other
authorization response (success + errors via `redirectError`) includes `iss`. **RFC 9207 §2**: the `iss`
parameter MUST be included in authorization responses *including error responses*. A strict RP doing mix-up
validation may reject the deny. **Action:** add `q.Set("iss", issuer)` to the deny builder. Low impact,
trivial fix. https://www.rfc-editor.org/rfc/rfc9207.html

### 🔴-7 — `email` scope is a dead FE control 🔎
Both OIDC admin forms render an `email` scope checkbox (`AdminOidcClientsView.vue:171`,
`AdminOidcClientDetailView.vue:169`); create/update store it verbatim (no server-side scope whitelist), and
`/authorize` would grant it + consent would display it. But **no email claim exists anywhere** — no `email`
in `claims.go`/`userinfo.go`, no account email column, and discovery's `scopes_supported` is
`[openid, profile, offline_access]` (no `email`). An operator who ticks "email" gets a granted scope that
delivers nothing (OIDC Core §5.4 defines `email`→`email`/`email_verified`). **Action:** remove the checkbox,
*or* implement an email attribute + claim + discovery entry; minimally, reject unsupported scopes server-side.

### 🟡-8 — `offline_access` (refresh tokens) is unexposed in the admin UI 🔎
The backend issues refresh tokens only when `offline_access` is granted (`token.go:216`) and requires it in
`allowed_scopes`; it's in discovery. But neither admin form offers an `offline_access` checkbox (only
openid/profile/email) — so a refresh-capable client can only be made via CLI/DB. Combined with #7, the scope
checkbox set is simply wrong (offers a dead `email`, hides a live `offline_access`). **Action:** replace the
`email` checkbox with `offline_access`.

### 🟡-9 — Account/invitation mutations produce no audit-viewer rows 📄
`UpdateAccount`/`DeleteAccount`/`RevokeAccountSessions`/`ReissueEnrollment`/`CreateInvitation`/`RevokeInvitation`
write only `logx` structured logs (📄 `handle_account.go:222,294,473,518,578,655`), not `credential_event`
rows — unlike signing-key/OIDC/SAML/IdP mutations and credential-delete, which do. **Role escalations and
account deletions are therefore invisible in the admin audit viewer** (`/audit-events`). **Action:** emit
`credential_event` rows (e.g. factor `account` / `invitation`) for these. (Pairs naturally with the ⚠️-1 fix.)

### ⚠️-10 — `prompt` parameter not strictly validated 📄
`authorize.go:132-138` special-cases only `login` and `none` (+ rejects `login`+`none`); any other token
(`select_account`, `create`, typos) is silently ignored, and `prompt_values_supported` is not advertised.
OIDC Core §3.1.2.1 makes `none` mutually exclusive with *all* other prompts and expects unsupported values to
error. Low impact for a single-tenant IdP (no account selection), but a spec deviation. **Action:** validate
prompt tokens against the supported set; enforce `none`-exclusivity generally; advertise or reject unknowns.

### 🔴-12 — Confirmed dead / no-op columns (cleanup, carried from 2026-06-10) 🔎/📄
Re-confirmed by the domain reviews, not new bugs:
- **OIDC `oidc_client`:** `contacts`, `application_type`, `id_token_signed_response_alg`, `default_max_age`,
  `require_auth_time` are stored, never read (🔴 dead). `subject_type` is dead-but-deferred (pairwise, backlog
  T4 — keep). `logo_uri`/`tos_uri`/`policy_uri` are read by the consent screen but never settable (🟡 inert).
- **SAML `saml_sp`:** `want_assertions_signed` (assertions always signed, `assertion.go:202`) and
  `name_id_claim` (NameID always opaque per-SP) are 🔴 no-ops — FE controls already removed, but the PUT body
  + columns remain (dead writes). `authn_requests_signed` is 🔴 dead/redundant (metadata `WantAuthnRequestsSigned`
  is hardcoded `true`, `metadata.go:37`). `metadata_valid_until`/`cache_duration`/`fetched_at` are inert
  placeholders for the deferred metadata-auto-refresh feature (keep).
- **Action:** a migration-009 schema-prune + drop the two SAML no-op PUT fields (decision: drop, not wire).
  All deferred-by-design items cross-checked against `2026-06-08-backend-backlog.md` — no FE overclaim.

### minor-13 — robustness/hygiene 📄
- `password_changed_at` is bumped on the *transparent* rehash-on-verify (param upgrade looks like a password
  change in forensics) — `password.go:114` → `UpsertPasswordCredential` sets it unconditionally. Add a
  rehash-only query.
- Add-passkey ceremony stash uses `Get` + best-effort `Del` (`handle_me.go:261,327`) vs the login path's
  single-use `Pop`; replay is blocked by the `credential_id` UNIQUE constraint + one-time challenge, so this
  is parity/robustness only.
- Re-login `(iss,sub)` lookup (`modes.go:66`) is not scoped to the IdP being used, and the unique constraint
  is global `(iss,sub)`. Harmless unless an admin configures **two `upstream_idp` rows with the same
  `issuer_url`** (then a user provisioned via IdP-A could complete a login against IdP-B's slug, dodging B's
  provisioning gates — which re-login skips anyway). Either enforce `existing.UpstreamIdpID == idp.ID` on
  re-login, or document "one issuer ⇒ one upstream_idp row." Needs a decision on whether that config is supported.

---

## What was verified CORRECT (high-value, so it's on record)

- **OIDC OP:** authorize open-redirect ordering (validate `redirect_uri` before any redirect-back); code-only;
  PKCE S256-only + DB CHECK forbidding `plain`; constant-time PKCE verify; auth-code single-use (`Pop`) +
  replay→family-revoke; refresh rotation + reuse→family-revoke + disabled re-check; client auth basic/post/none
  with timing-equalized unknown-client burn; ID token claims incl. correct `at_hash`/`azp`-omission/`sid`/
  `auth_time`; RS256 alg-pinned (`alg:none` impossible); RFC 9068 access token; userinfo `aud==issuer`;
  introspect confidential-only + ownership; revoke always-200 + ownership; logout id_token_hint sig+iss
  (expiry-tolerant) + `sid` revoke + exact post-logout match; forced-reauth single-use account-bound nonce.
- **Federation (RP):** PKCE + nonce + state single-use (`Pop`) + cross-namespace (Login/Link) defense; RFC 9207
  `iss` + `token_endpoint` snapshot mix-up resistance; alg allowlist (no HS256/none); identity keyed on
  `(iss,sub)` not email (no email-swap takeover); mode gates correct; link session-swap defense + no new
  session on link; claim-name overrides honored on all paths; return_to open-redirect guard; secret AEAD AAD-bound.
- **SAML IdP:** AuthnRequest sig cert-PINNED on both bindings + positive RSA-SHA256 allowlist + cert-validity
  on both; layered XSW/XXE/duplicate-ID/decompression-bomb defenses; Response+Assertion both signed with correct
  XSD signature placement (round-trips through crewjam SP parse); ACS resolution precedence + open-redirect
  guard; stable opaque persistent NameID w/ NameQualifier/SPNameQualifier; NameIDPolicy/@Format honoring;
  ForceAuthn/IsPassive (IsPassive wins) + Version=="2.0"; AuthnRequest replay single-use; SLO sig-gated before
  any mutation, response location from stored metadata; signed metadata w/ grace-window key publishing.
- **Credentials:** WebAuthn challenge single-use + origin/RP-ID/UV/user-handle checks + sign-count clone
  forensics; TOTP AES-GCM (per-row nonce, AAD-bound, opaque decrypt-failure) + `last_step` replay + ±1 drift;
  argon2id + dummy-verify enumeration defense (disabled = same path); recovery ceremony (narrow token, atomic
  Pop, code-preserving begin, transactional wipe+mint); persistent `auth_throttle` (atomic bump, no IP buckets);
  sudo = webauthn/password_totp only; session cookie `__Host-`/Secure/HttpOnly/SameSite/Path=/ + live disabled check.
- **Admin:** signing-key state machine — activate demotes-before-promotes atomically (partial unique index never
  violated; rollback restores prior active; no externally-visible zero-active window); JWKS publish set exactly
  `{pending,active,decommissioning}` with signer always `active`; SAML metadata publishes non-retired certs across
  grace; legacy `active`/`retired_at` columns never used to pick the signer; last-admin lockout guard (`FOR UPDATE`)
  + self-delete guard + live disable propagation + complete delete cascade; free-form attributes can't inject
  reserved claims (OIDC nests under `attributes`; SAML whitelisted per-SP); no secret/key material in audit detail
  or any admin read view; audit keyset pagination correct.

---

## Recommended follow-up (proposed; not yet implemented)

A focused remediation cycle, roughly in priority order:
1. **(security) ⚠️-1** — put the six account/invitation mutations behind fresh sudo (or document the carve-out);
   correct `AUDIT.md:430`.
2. **(security) ⚠️-4** — disabled-account check on reset-enrollment begin + complete.
3. **(decision) ⚠️-5** — add-passkey sudo gating: gate it or document the exemption.
4. **(interop) ⚠️-2, ⚠️-6, ⚠️-10** — SAML redirect LogoutResponse detached signature; consent `iss`; prompt validation.
5. **(truth-in-labeling) ⚠️-3** — fix the federation "Disabled" FE text + the 500→clean-error; correct the
   prior FINDINGS §1.
6. **(audit completeness) 🟡-9** — emit `credential_event` rows for account/invitation mutations.
7. **(honesty/cleanup) 🔴-7, 🟡-8, 🔴-12, 🟡-11** — fix the scope checkbox set (drop `email`, add `offline_access`);
   migration-009 dead-column prune; drop the two SAML no-op PUT fields; validate invite IdP slug.
8. **(hygiene) minor-13.**

None of these is an in-flight protocol break; the core artifact-minting/validating paths are correct. This is
authz-hardening + interop-conformance + truth-in-labeling work.
