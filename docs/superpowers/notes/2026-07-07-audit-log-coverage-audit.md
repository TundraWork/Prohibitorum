# Audit-log (credential_event) coverage & consistency audit

Date: 2026-07-07
Scope: whole backend. Method: 5 parallel read-only subsystem sweeps (auth/session/sudo, self-service /me, admin, OIDC+SAML protocol, federation+RBAC+PAT) against the canonical `pkg/audit` taxonomy, plus coordinator spot-verification of the headline findings.

**What "the Audit log" means here:** the `credential_event` table surfaced by the admin Audit-log viewer (`/audit-events`) — NOT the structured `slog`/`logx` process log. Many actions log to `slog` but write no `credential_event` row; from the operator's Audit-log viewer they are invisible. That slog-vs-audit split is the central theme of this audit.

Baseline: ~30 `audit.Record` emission sites backend-wide. Taxonomy: 15 factors, 19 events (`pkg/audit/event.go`).

---

## Cross-cutting themes

### Theme A — Security-critical actions logged to slog only (not to credential_event)
The biggest class. The action succeeds/fails and emits a structured `slog` line, but writes no `credential_event` row, so the admin Audit viewer shows nothing.

### Theme B — Systemic missing IP + UserAgent
Most records omit `IP` and `UserAgent`. Two structural causes: (1) the credential stores (`pkg/credential/password`, `pkg/credential/totp`) and the `Federator` are pure domain objects with no `*http.Request`; (2) even several raw-HTTP handlers that HAVE `r` omit them. Only the OIDC/SAML protocol handlers (via `p.auditIP(r)`/`i.auditIP(r)`) and two admin sites (entity-icon, settings/branding) populate IP+UA. An IP/UA-based correlation query works for only a handful of event types.

### Theme C — Factor/Event vocabulary drift
Mis-filed factors, defined-but-unused events, and one ad-hoc event string. Undermines "filter by factor/event" in the viewer.

### Theme D — Missing/weak attribution & detail quality
Records missing `AccountID` where it's available; hard-delete records missing the human-readable identifier; spurious/coarse revoke rows.

### Theme E — No centralized redaction guard
Secret-exclusion from `Detail` is a "write-site invariant" enforced only by convention at each of ~30 call sites; no runtime redaction filter and no unit/compile-time guard. `assertAuditDetailNoSecret` exists only in the smoke.

---

## Prioritized backlog

### P0 — Security-critical audit blind spots (attacker-relevant actions invisible to the viewer)

1. **Forward-auth / PAT gateway is entirely unaudited.** `pkg/protocol/oidc/forward_auth.go` does not even import `pkg/audit`. No record for: PAT authentication success (`personal_access_token|use`), PAT rejection (bad/disabled/not-granted/RBAC-denied — 401/403 at `forward_auth.go:402,406,412,419,426,434`), cookie-session allow (`oidc_client|use`, `:236–253`), or cookie-session RBAC deny (silent fall-through to login redirect, `:238–253`). The gateway is the primary API-access path; a PAT is a credential and its use/abuse leaves zero forensic trail. `Provider` already has `p.audit` + `p.auditIP` available — pure omission.
2. **WebAuthn login is unaudited.** `pkg/server/handle_auth.go` has NO `audit.Record` at all. Login success (`handle_auth.go:349–366`), login failures (ceremony missing/corrupt, no-account, account-disabled, FinishLogin error — `:248,258,292,295,303`), and clone-warning (`:329–335`) are slog-only. The primary passkey login path — the single most security-relevant event — is invisible in the viewer. (Password/TOTP `use`/`fail` ARE audited via the stores; WebAuthn is the outlier.) `EventCloneWarning` is defined but never emitted.
3. **OIDC token-endpoint failures largely unaudited.** Client-auth failure (`token.go:94–99`), rate-limit (`:103`), and code-exchange rejections — client mismatch, redirect_uri mismatch, PKCE failure, disabled-account, dead-session (`:156–215`) — emit no `fail`. Only `code_replay` (`:137`) is audited. Refresh: client-mismatch-after-rotation (`refresh.go:294`) and account-not-found/disabled (`:302`) unaudited; `refresh_reuse` (`:278`) is audited but with `AccountID=nil`. These are the credential-stuffing / stolen-code / stolen-refresh signals.
4. **SAML replay + signature failures unaudited.** Replayed AuthnRequest (`sso.go:300–307`), SSO parse/signature errors incl. `ErrBadSignature`/`ErrMissingSignature`/weak-alg on a known SP (`sso.go:356–378`), and SLO signature failures (`slo.go:557–578`) are slog-only. Forged/unsigned/replayed protocol messages are security events.

### P1 — Completeness of session & credential lifecycle

5. **Session start/end never/inconsistently audited.** `EventSessionStart` is defined but NEVER emitted — every login path (`sessionStore.Issue` in webauthn, password+TOTP, enrollment, pairing, recovery, federation-confirm) creates a session with no `session|session_start` row. Session END is inconsistent: SAML SLO emits `session_end` (`slo.go:237`), but OIDC RP-logout uses `EventUse` not `EventSessionEnd` (`logout.go:121`), and self-service logout / per-session revoke / forward-auth signout are slog-only (`handle_auth.go:371`, `handle_me.go:423`, `handle_forward_auth_signout.go`). `session.go` Issue/Revoke emit nothing.
6. **Self-service credential mutations slog-only.** Add passkey (`handle_me.go:351`), remove passkey (`:530`), rename passkey (`:470`), per-session revoke (`:423`), consent revoke (`handle_me_consent.go:112`) — all slog-only; should be `webauthn|register`/`revoke`/`update`, `session|session_end`, `oidc_client|access_revoked`. Avatar upload/select/remove (`handle_avatar.go`) and profile displayName change (`handle_me.go:77`) emit nothing / slog-only.
7. **Admin mutations missing records.** Admin credential delete (`handle_account.go:467` & `:522`) slog-only; admin single-session revoke (`:937`) slog-only (bulk revoke `:604` IS audited — the singular is the outlier); display-name-only account update produces NO record because `changes` map excludes `displayName` (`:260–279`).
8. **Enrollment / pairing / sudo-fail.** Enrollment consume is slog-only (`handle_enrollment.go:510`) — `EventEnrollmentConsumed` defined but never emitted; enrollment-issued is filed under `FactorAccount` not `FactorEnrollment` (`handle_account.go:681`). Enrollment failure gates (federation-required, disabled, consumed, expired) unaudited. Pairing lifecycle (begin/complete/approve/cancel) slog-only (`handle_pairing.go`). Sudo FAILURE is unaudited — only `sudo_granted` success is (`handle_sudo.go`).
9. **/welcome confirm + decline.** Confirm-YES issues a session with no `use`/`session_start` audit (`handle_federation_confirm.go:86–125`); decline (`:130`) emits nothing.

### P2 — Consistency & correctness of existing records

10. **IP + UserAgent (Theme B).** Thread the client-IP resolver + `r.UserAgent()` into the emission path. Options: (a) pass IP/UA down into the credential stores + `Federator` (interface change), or (b) emit the lifecycle records at the handler layer where `r` exists. Highest-leverage consistency fix; scope it deliberately.
11. **`FactorSigningKey` kludge (Theme C).** Seven instance-settings mutations (name/icon/maintenance/login-bg/client-ip) file under `signing_key` via `auditBranding` (`handle_admin_settings.go:162`, `handle_admin_client_ip.go:49`). Add a `FactorSettings` (or `FactorInstance`) factor and reclassify; filtering `factor=signing_key` currently returns unrelated config changes.
12. **Event/factor fixes (Theme C).** `"sudo_granted"` is an ad-hoc string → add `EventSudoGranted`; sudo records use `FactorSession` rather than the verified factor (webauthn/password_totp) — `detail.method` partially compensates. OIDC logout `EventUse`→`EventSessionEnd`. Use `FactorEnrollment`+`EventEnrollmentConsumed` for enrollment. Decide whether Steam warrants a distinct factor vs `federation_oidc` (currently `iss`/`sub` disambiguate).
13. **Missing attribution (Theme D).** OIDC revoke records have `AccountID=nil` always (`revoke.go:71`) though `sub`/`fam.AccountID` are available; `refresh_reuse` drops AccountID (structural — `rotateRefresh` discards the family before returning the error; fixable by returning the AccountID with the error). Admin PAT revoke records the admin, not the PAT owner, and omits the target from detail (`handle_admin_account_tokens.go:85`). Unlink omits idp_slug/iss/sub (`handle_me_identities.go:209`). Group delete omits slug (`handle_admin_groups.go:332`) — unrecoverable after hard-delete.

### P3 — Hardening & hygiene

14. **Centralized redaction guard (Theme E).** Add a runtime redaction/allowlist for `Detail` (or a shared `auditDetail` builder) + a unit/compile-time guard (promote `secretLeakNeedles`/`assertAuditDetailNoSecret` from smoke to a package test over all call sites). Highest-risk site today: the IdP create body carries `ClientSecret`/`ApiKey` five screenfuls from the audit call (`handle_admin_upstream_idps.go`).
15. **Verbatim upstream error strings in Detail.** `"err": err.Error()` in federation fail records (`federation.go:577,605,724,747`) can embed unstructured external data → replace with a structured error code/status.
16. **`DisableNonWebAuthnFallbacks` accuracy.** Emits `password/totp/recovery_code revoke` unconditionally even if the factor wasn't set, and one bulk `recovery_code|revoke` vs per-code elsewhere (`pkg/authn/flow.go:136`). Emit only for factors actually removed; match the per-code granularity used by `totp.Store`.
17. **Detail key unification (optional).** OIDC uses `client_id`, SAML uses `sp` — a shared `resource_type`+`resource_id` pair simplifies cross-protocol viewer queries.
18. **nil-guard consistency.** Only `handle_sudo.go` nil-guards `s.Audit`; harmless in prod (always wired) but inconsistent.

### Adjacent (not an audit-emission bug, but affects audit-trail assurance)
19. **Sudo gating gaps.** SAML mutations (`server.go:552–556`), groups (`:584–588`), app-access grants (`:570–578`), and all `set-disabled` endpoints are `registerOpHTTP` (admin role only) not `registerSudoOpHTTP` — unlike OIDC-client/upstream-IdP create/update/delete. They DO emit audit records, but the actor's session may be stale (no fresh re-auth behind the recorded action).

---

## Notes for remediation planning
- The IP/UA fix (P2 #10) and the slog→credential_event additions (P0/P1) are the two largest efforts; decide the seam (handler-layer emission vs threading request context into stores/Federator) up front — it shapes everything.
- Adding a `FactorSettings` (#11) and `EventSudoGranted`/using `EventSessionStart`/`EventEnrollmentConsumed` (#12) are cheap vocabulary additions but touch `pkg/audit/event.go` + the viewer's filter enums + the frontend i18n `errors.*`/audit filter lists — coordinate.
- Any new emissions on tx-holding paths must follow the tx-scoped-writer rule (tx writer for success inside the tx; outer pooled writer for failure audits that roll back) — see `pkg/federation/oidc/modes.go` and `reference_for_update_audit_fk_deadlock`.
- The smoke's `verify*AuditEvents` + `assertAuditDetailNoSecret` are the regression harness — extend them alongside new emissions.
