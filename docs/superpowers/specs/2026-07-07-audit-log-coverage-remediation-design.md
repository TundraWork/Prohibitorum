# Audit-log coverage & consistency remediation (P0–P2)

Date: 2026-07-07
Status: Design approved (pending spec review)

Findings source: `docs/superpowers/notes/2026-07-07-audit-log-coverage-audit.md` (exhaustive per-site
file:line anchors live there — this spec references items by their backlog number, e.g. "P0 #2").

## Problem

The admin Audit-log viewer reads the `credential_event` table. A whole-backend audit found that many
security-relevant actions log only to the structured `slog`/`logx` process log (or nothing) and write
no `credential_event` row, so they are invisible in the viewer; and that the records that ARE written
are inconsistent — most omit IP + UserAgent, some mis-file their Factor, and several defined events are
never emitted. This remediation closes the P0 (security-critical blind spots), P1 (session + credential
lifecycle completeness), and P2 (consistency of existing records) items.

## Decisions (locked during brainstorming)

- **Emission seam = ctx-carried IP/UA + ctx-aware `Record`.** A router middleware stashes the client IP
  and User-Agent into the request `context.Context`; the audit writer auto-fills `Record.IP`/`.UserAgent`
  from ctx when the call site left them empty. No signature churn through the stores/Federator.
- **Forward-auth gateway = failures/denials only.** No audit row on successful gateway auth (rely on the
  existing `TouchPATLastUsed` for last-used). Failures + RBAC denials ARE audited.
- **One spec, phased plan:** Foundation → P0 → P1 → P2.
- **No-secret guard:** extend the smoke's `assertAuditDetailNoSecret` + `secretLeakNeedles` to the new
  sites; do NOT build the centralized runtime redaction filter (that is P3, out of scope).
- **Session end scope:** explicit logout/revoke only. Passive KV/session expiry is NOT audited (no reaper).

## Architecture — the seam

### Request-context IP/UA
A new middleware `RequestMeta` is mounted with `router.Use(...)` (alongside `LoadSession`), so it runs for
EVERY route (authenticated + unauthenticated: login, `/oauth/*`, `/saml/*`, forward-auth). It computes
`clientIPResolver.IP(r)` + `r.UserAgent()` once and stores them on the ctx via typed keys:

```go
// pkg/audit (or pkg/audit/reqmeta.go)
type ctxKey int
const (ipKey ctxKey = iota; uaKey)
func WithRequestMeta(ctx context.Context, ip, ua string) context.Context // sets both
func ipFromCtx(ctx context.Context) string   // "" if unset
func uaFromCtx(ctx context.Context) string
```

### ctx-aware fill in the writer
`dbWriter.Record` fills IP/UA from ctx ONLY when the incoming `Record` left them zero:

```go
func (w *dbWriter) Record(ctx context.Context, r Record) error {
    if r.IP == nil {
        if ip := ipFromCtx(ctx); ip != "" { r.IP = ParseIPOrNil(ip) }
    }
    if r.UserAgent == "" { r.UserAgent = uaFromCtx(ctx) }
    // ...existing marshal + InsertCredentialEvent...
}
```

Explicit values win (OIDC/SAML handlers already pass `p.auditIP(r)` — unchanged, and they also match the
ctx value). The tx-scoped writer (`audit.NewWriter(qtx)`) is the same `dbWriter` type, so tx-path emissions
(federation provisioning, etc.) get ctx IP/UA too — as long as the request ctx is threaded into the tx call
(it is; the Federator/stores take the request ctx). Detached goroutines using `context.Background()` get no
IP/UA (correct).

**Net effect:** all ~30 existing emissions + every new emission below carry IP+UA with no store/Federator/
session signature changes.

### Vocabulary additions (`pkg/audit/event.go`)
- `FactorSettings Factor = "settings"` — instance-settings/branding/client-ip mutations (replaces the
  `FactorSigningKey` kludge).
- `EventSudoGranted = "sudo_granted"` (replaces the ad-hoc string literal) + `EventSudoFailed = "sudo_failed"`.
- Begin EMITTING the already-defined-but-unused `EventSessionStart`, `EventSessionEnd`,
  `EventEnrollmentConsumed`, `EventCloneWarning`.

New factor/event values must propagate to: the admin viewer filter enums (`pkg/contract` audit filter
lists, if enumerated server-side), the dashboard audit-filter dropdown source (`AdminAuditView.vue` /
`lib` constant list), and the `errors.*`/audit i18n label maps in `en.ts` + `zh.ts`. (The audit viewer
renders factor/event strings; confirm whether the filter dropdown is a fixed list needing the new values.)

## Phase 1 — Foundation
- `RequestMeta` middleware + `WithRequestMeta`/ctx getters + ctx-aware `dbWriter.Record` fill (+ unit tests:
  explicit-wins, ctx-fallback, background-nil).
- Vocabulary additions above + propagate to contract/frontend filter lists + i18n.
- A shared tiny helper if useful (e.g. `auditActor(sess)` already exists — reuse; add `auditSubject` only
  if it reduces duplication).

## Phase 2 — P0 (security-critical blind spots)
- **Forward-auth/PAT gateway** (`pkg/protocol/oidc/forward_auth.go`): import + use `p.audit`. Emit on
  FAILURE/DENY only: `personal_access_token|fail` for unresolvable/disabled/not-granted PAT
  (`:402,406,412,419,426`); `oidc_client|access_denied` for RBAC deny on the PAT path (`:434`) AND the
  cookie-session path (`:238–253`, currently a silent fall-through); `oidc_client|fail` for callback
  state/PKCE/client failures (`HandleForwardAuthCallback`). No success emission.
- **WebAuthn login** (`pkg/server/handle_auth.go`): `webauthn|use` on login success (`:349`);
  `webauthn|fail` on each failure branch (`:248,258,292,295,303`); `webauthn|clone_warning` on the sign-count
  regression (`:329` login and `handle_sudo.go:281` sudo).
- **OIDC token/refresh** (`token.go`,`refresh.go`): `oidc_client|fail` on client-auth fail (`token.go:94`),
  rate-limit (`:103`), code-exchange rejections (client/redirect_uri/PKCE/disabled/dead-session `:156–215`),
  refresh client-mismatch (`refresh.go:294`), account-not-found/disabled (`:302`); populate `AccountID` on
  `refresh_reuse` (`:278` — return the family's AccountID out of `rotateRefresh`).
- **SAML** (`sso.go`,`slo.go`): `saml_sp|fail` on replayed AuthnRequest (`sso.go:300`) and on
  signature/parse failures where the SP is resolved (`sso.go:356`, `slo.go:557`) — include `sp` entity_id;
  `AccountID` absent is acceptable (pre-auth).

## Phase 3 — P1 (session + credential-lifecycle completeness)
- **Sessions**: emit `session|session_start` from each login path — webauthn (`handle_auth.go`),
  password+TOTP (`handle_auth_password.go`), enrollment (`handle_enrollment.go`), pairing
  (`handle_pairing.go`), recovery (`handle_auth_recovery.go`), federation-confirm
  (`handle_federation_confirm.go`). Emitted at the HANDLER layer (ctx has IP/UA; `sessionStore.Issue`
  returns before the handler emits). `session|session_end` on explicit logout/revoke: OIDC logout
  (`logout.go:121` `EventUse`→`EventSessionEnd`), self-service `/me/sessions/revoke` (`handle_me.go:423`),
  admin single-session revoke (`handle_account.go:937`), forward-auth signout, self-service logout
  (`handle_auth.go:371`). Passive expiry NOT audited.
- **Self-service** (`handle_me*.go`): passkey add (`:351`), remove (`:530`), rename (`:470` → `webauthn|update`),
  per-session revoke (→ `session|session_end`), consent revoke (`handle_me_consent.go:112` →
  `oidc_client|access_revoked` / `saml_sp|access_revoked`), avatar upload/select/remove (`handle_avatar.go`
  → `account|update` w/ reason), profile displayName (`handle_me.go:77` → `account|update`).
- **Admin** (`handle_account.go`): admin credential-delete (`:467`&`:522`), admin single-session-revoke
  (`:937`); add `displayName` to the tracked-changes diff (`:260–279`) so name-only updates emit.
- **Enrollment/pairing/sudo**: `enrollment|enrollment_consumed` on consume (`handle_enrollment.go:510`) +
  the security-relevant failure gates (federation-required, disabled, consumed, expired); pairing lifecycle
  (begin/complete/approve/cancel — choose `session`/`webauthn` factor per action); `EventSudoFailed` on the
  sudo denial paths (`handle_sudo.go` bad password/TOTP/webauthn/expired/intent).
- **/welcome** (`handle_federation_confirm.go`): confirm-YES → `federation_oidc|use` + `session|session_start`
  (`:86–125`); decline → a `federation_oidc|fail` (reason=`confirm_declined`) or dedicated record (`:130`).

## Phase 4 — P2 (consistency of existing records)
- Reclassify the 7 settings/branding/client-ip mutations off `FactorSigningKey` → `FactorSettings`
  (`handle_admin_settings.go`'s `auditBranding`, `handle_admin_client_ip.go`). Keep the `reason` detail.
- Fix events: sudo `"sudo_granted"` literal → `EventSudoGranted` (`handle_sudo.go:372`); enrollment-issued
  → `FactorEnrollment` (currently `FactorAccount`, `handle_account.go:681`).
- Attribution/detail: OIDC revoke → carry `AccountID` (`revoke.go:71`, from `sub`/`fam.AccountID`); admin
  PAT-revoke → add `target_account_id` to detail (`handle_admin_account_tokens.go:85`); unlink → add
  idp_slug/iss/sub (`handle_me_identities.go:209`); group-delete → add slug (`handle_admin_groups.go:332`).
- Sudo factor: emit sudo under the VERIFIED factor (webauthn/password/totp) with `EventSudoGranted`/
  `EventSudoFailed` + `detail.method`, so filtering by the factor shows the re-auth. (Was `FactorSession`.)

## Sudo record shape (explicit — it recurs across phases)
`sudo` records use the factor that was actually verified (`webauthn` | `password` | `totp`), event
`EventSudoGranted` on success / `EventSudoFailed` on denial, `detail:{method: "<webauthn|password_totp>"}`.
This replaces the current `FactorSession` + `"sudo_granted"` literal.

## Testing
- **Unit:** the ctx-aware `Record` fill (explicit-wins / ctx-fallback / background-nil); `RequestMeta`
  middleware sets ctx; per-handler emission tests where the handler is unit-testable (webauthn login
  use/fail with a fake writer capturing records; sudo grant/fail; gateway deny).
- **Smoke:** extend `verify*AuditEvents` to assert the new rows appear — webauthn login `use`+`fail`,
  `session_start`/`session_end`, forward-auth PAT `fail` + `access_denied`, sudo `sudo_granted`/`sudo_failed`,
  settings mutation under `factor=settings`, consent-revoke `access_revoked`. Extend
  `assertAuditDetailNoSecret` + `secretLeakNeedles` to the new emission sites.
- **tx-writer rule:** any new emission on a tx-holding path uses the tx-scoped writer for success inside the
  tx and the outer pooled writer for failure audits that roll back (see `modes.go`,
  `reference_for_update_audit_fk_deadlock`).

## Gate (Definition of Done)
`go build -tags nodynamic ./...`, `go vet ./...`, `go test ./...` clean; vitest green; vue-tsc 0;
check-contrast unchanged; live `mise run ci:smoke` `SMOKE_EXIT=0` with the extended audit assertions;
dist rebuilt + committed (if the frontend filter list / i18n changed).

## Out of scope
- P3 items: centralized runtime redaction filter; verbatim-`err.Error()` cleanup;
  `DisableNonWebAuthnFallbacks` per-code accuracy; detail-key unification (`client_id` vs `sp`);
  nil-guard consistency.
- Adjacent: sudo-gating gaps on SAML/groups/app-access/set-disabled endpoints.
- Introspect/userinfo endpoint auditing (classified LOW in the findings; deferred): these are routine RP
  reads; not P0–P2 blind spots worth the volume. (If wanted, add later.)
- A retention/pruning story for `credential_event` (session_start adds one row per login; acceptable —
  the gateway success flood is avoided by the failures-only decision).
