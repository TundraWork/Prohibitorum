# Audit-log coverage & consistency remediation (P0–P2) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the P0/P1/P2 audit-log gaps — emit `credential_event` rows for the security-critical + lifecycle actions that are currently slog-only, and make existing records consistent (IP+UA everywhere, correct factor/event, better attribution).

**Architecture:** A router middleware stashes client IP + User-Agent into the request `context.Context`; a ctx-aware `dbWriter.Record` auto-fills `Record.IP`/`.UserAgent` when a call site left them empty — so all ~30 existing + every new emission gets IP/UA with no store/Federator signature churn. Then per-file tasks add the missing emissions and fix vocabulary/attribution.

**Tech Stack:** Go (net/http, chi middleware, pgx), Vue 3 (audit filter lists), vitest, goose/sqlc (no schema change), the existing `pkg/audit` writer + `mise run ci:smoke`.

**User decisions (already made):**
- **Emission seam = ctx-carried IP/UA + ctx-aware `Record`** (no store/Federator signature churn; explicit IP still wins; background jobs get no IP).
- **Forward-auth gateway = failures/denials only** (no success audit; `TouchPATLastUsed` covers last-used).
- **One spec, phased plan** (Foundation → P0 → P1 → P2).
- **No-secret guard = extend the smoke's `assertAuditDetailNoSecret` only** (centralized runtime redaction is P3, out of scope).
- **`FactorSettings = "settings"`.**
- **Sudo reshape = verified factor (webauthn/password/totp) + `EventSudoGranted`/`EventSudoFailed`** ("use best architecture").
- **Profile/avatar changes audited as `account|update`.**
- **Session end = explicit logout/revoke only** (passive expiry not audited).

Spec: `docs/superpowers/specs/2026-07-07-audit-log-coverage-remediation-design.md`
Findings (file:line anchors per item): `docs/superpowers/notes/2026-07-07-audit-log-coverage-audit.md`

---

## Emission idiom (applies to every task below)

With the Foundation in place, a new audit emission is just:

```go
_ = s.Audit.Record(r.Context(), audit.Record{
    AccountID: &acctID,            // subject or actor; omit if genuinely unknown
    Factor:    audit.FactorWebAuthn,
    Event:     audit.EventUse,
    Detail:    map[string]any{"reason": "login"},
})
```

- **Do NOT set `IP`/`UserAgent`** — the ctx-aware writer fills them from the request ctx. (OIDC/SAML handlers that already pass `p.auditIP(r)` stay as-is.)
- Pass the **request ctx** (`r.Context()` / the handler's ctx) so the fill works. Detached goroutines (avatar inherit) intentionally get none.
- **tx-writer rule:** on a path holding `SELECT…FOR UPDATE` or writing an FK to a just-inserted row, emit success via the tx-scoped `audit.NewWriter(qtx)`; emit failure audits (that roll back) via the outer pooled `s.Audit`. Mirror `pkg/federation/oidc/modes.go`.
- Best-effort: ignore the returned error (`_ =`), matching every existing site.
- **Before editing a file, read its existing `audit.Record` calls and mirror the nearest one** (factor const, detail-key style, actor vs subject).

---

## Phase 1 — Foundation

### Task 1: ctx-carried IP/UA seam + audit vocabulary + middleware

**Goal:** All audit records auto-carry IP+UA from the request ctx; add the new factor/event constants.

**Files:**
- Create: `pkg/audit/reqmeta.go`, `pkg/audit/reqmeta_test.go`
- Modify: `pkg/audit/event.go` (ctx-fill in `dbWriter.Record`; new consts)
- Create: `pkg/server/middleware_reqmeta.go`
- Modify: `pkg/server/server.go` (mount middleware)

**Acceptance Criteria:**
- [ ] `audit.WithRequestMeta(ctx, ip, ua)` + a ctx-aware `dbWriter.Record` fill IP/UA only when the `Record` left them empty; explicit values are preserved.
- [ ] `requestMetaMW` is mounted via `router.Use` before `LoadSession`, so every route sets ctx meta.
- [ ] New consts exist: `FactorSettings="settings"`, `EventSudoGranted="sudo_granted"`, `EventSudoFailed="sudo_failed"`.
- [ ] `go build -tags nodynamic ./... && go test ./pkg/audit/` pass; a unit test proves explicit-wins / ctx-fallback / background-nil.

**Verify:** `go build -tags nodynamic ./... && go test ./pkg/audit/ ./pkg/server/ -run 'Audit|ReqMeta|RequestMeta'`

**Steps:**

- [ ] **Step 1: `pkg/audit/reqmeta.go`**

```go
package audit

import "context"

type ctxKey int

const (
	ipCtxKey ctxKey = iota
	uaCtxKey
)

// WithRequestMeta returns a ctx carrying the client IP + User-Agent for audit
// enrichment. The RequestMeta middleware sets it once per request; dbWriter.Record
// reads it back to auto-fill Record.IP/UserAgent when a call site left them empty.
func WithRequestMeta(ctx context.Context, ip, ua string) context.Context {
	if ip != "" {
		ctx = context.WithValue(ctx, ipCtxKey, ip)
	}
	if ua != "" {
		ctx = context.WithValue(ctx, uaCtxKey, ua)
	}
	return ctx
}

func ipFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(ipCtxKey).(string); ok {
		return v
	}
	return ""
}

func uaFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(uaCtxKey).(string); ok {
		return v
	}
	return ""
}
```

- [ ] **Step 2: ctx-fill in `dbWriter.Record` (`pkg/audit/event.go`)** — add at the very top of the method body (before the `detail` marshal):

```go
func (w *dbWriter) Record(ctx context.Context, r Record) error {
	// Enrich from request ctx when the call site didn't set these explicitly.
	// The RequestMeta middleware stashes them for every HTTP request; detached
	// goroutines (context.Background) carry none, which is correct.
	if r.IP == nil {
		if ip := ipFromCtx(ctx); ip != "" {
			r.IP = ParseIPOrNil(ip)
		}
	}
	if r.UserAgent == "" {
		r.UserAgent = uaFromCtx(ctx)
	}
	// ...existing body unchanged (detail marshal, credRef, ua, InsertCredentialEvent)...
```

- [ ] **Step 3: new consts in `pkg/audit/event.go`** — add to the Factor block:

```go
	// FactorSettings covers instance-settings / branding / client-IP mutations
	// (previously mis-filed under FactorSigningKey).
	FactorSettings Factor = "settings"
```

and to the Event block:

```go
	EventSudoGranted = "sudo_granted"
	EventSudoFailed  = "sudo_failed"
```

(`EventSessionStart`, `EventSessionEnd`, `EventEnrollmentConsumed`, `EventCloneWarning` already exist — no additions, they'll start being emitted in later tasks.)

- [ ] **Step 4: `pkg/server/middleware_reqmeta.go`**

```go
package server

import (
	"net/http"

	"prohibitorum/pkg/audit"
)

// requestMetaMW stashes the resolved client IP + User-Agent into the request ctx
// so audit records auto-carry them (see audit.WithRequestMeta). Mounted for every
// route via router.Use, before LoadSession.
func requestMetaMW(ipOf func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := audit.WithRequestMeta(r.Context(), ipOf(r), r.UserAgent())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
```

- [ ] **Step 5: mount it (`pkg/server/server.go`)** — immediately BEFORE the `LoadSession` line (currently `:196`):

```go
	router.Use(requestMetaMW(clientIPResolver.IP))
	router.Use(sessstore.LoadSession(config, queries, sessionStore, clientIPResolver.IP))
```

- [ ] **Step 6: `pkg/audit/reqmeta_test.go`** — unit-test the fill via a fake `db.Querier` capturing `InsertCredentialEventParams`:

```go
package audit

import (
	"context"
	"net/netip"
	"testing"

	"prohibitorum/pkg/db"
)

type capQuerier struct{ last db.InsertCredentialEventParams }

func (c *capQuerier) InsertCredentialEvent(_ context.Context, p db.InsertCredentialEventParams) error {
	c.last = p
	return nil
}

// embed to satisfy db.Querier for the rest — use the existing test helper if one
// exists; otherwise a minimal stub with only InsertCredentialEvent is enough since
// dbWriter only calls that method.

func TestRecordFillsFromCtx(t *testing.T) {
	q := &capQuerier{}
	w := &dbWriter{q: q}
	ctx := WithRequestMeta(context.Background(), "203.0.113.7", "curl/8")
	if err := w.Record(ctx, Record{Factor: FactorWebAuthn, Event: EventUse}); err != nil {
		t.Fatal(err)
	}
	if q.last.Ip == nil || q.last.Ip.String() != "203.0.113.7" {
		t.Fatalf("IP not filled from ctx: %v", q.last.Ip)
	}
	if !q.last.UserAgent.Valid || q.last.UserAgent.String != "curl/8" {
		t.Fatalf("UA not filled from ctx: %+v", q.last.UserAgent)
	}
}

func TestRecordExplicitWins(t *testing.T) {
	q := &capQuerier{}
	w := &dbWriter{q: q}
	explicit := netip.MustParseAddr("198.51.100.9")
	ctx := WithRequestMeta(context.Background(), "203.0.113.7", "curl/8")
	_ = w.Record(ctx, Record{Factor: FactorWebAuthn, Event: EventUse, IP: &explicit, UserAgent: "explicit-ua"})
	if q.last.Ip.String() != "198.51.100.9" || q.last.UserAgent.String != "explicit-ua" {
		t.Fatalf("explicit values not preserved: ip=%v ua=%+v", q.last.Ip, q.last.UserAgent)
	}
}

func TestRecordBackgroundNoMeta(t *testing.T) {
	q := &capQuerier{}
	w := &dbWriter{q: q}
	_ = w.Record(context.Background(), Record{Factor: FactorWebAuthn, Event: EventUse})
	if q.last.Ip != nil || q.last.UserAgent.Valid {
		t.Fatalf("background ctx should carry no meta: ip=%v ua=%+v", q.last.Ip, q.last.UserAgent)
	}
}
```

NOTE: if `db.Querier` has many methods, embed an existing test fake (search `pkg/audit/event_test.go` for one) rather than hand-stubbing all methods; `dbWriter` only calls `InsertCredentialEvent`.

- [ ] **Step 7: verify + commit**

Run: `go build -tags nodynamic ./... && go test ./pkg/audit/`
```bash
git add pkg/audit/reqmeta.go pkg/audit/reqmeta_test.go pkg/audit/event.go pkg/server/middleware_reqmeta.go pkg/server/server.go
git commit -m "feat(audit): ctx-carried IP/UA seam + FactorSettings/EventSudo* vocabulary"
```

---

### Task 2: propagate new factor/event values to the audit-viewer filters

**Goal:** The admin audit viewer's Factor/Event filter dropdowns list the new values.

**Files:**
- Modify: `dashboard/src/lib/audit.ts` (`AUDIT_FACTORS`, `AUDIT_EVENTS`)
- Test: `dashboard/src/lib/audit.test.ts` (if present; else the AdminAuditView test)

**Acceptance Criteria:**
- [ ] `AUDIT_FACTORS` includes `"settings"`; `AUDIT_EVENTS` includes `"sudo_granted"`, `"sudo_failed"`, and (if missing) `"session_start"`, `"session_end"`, `"enrollment_consumed"`, `"clone_warning"`.
- [ ] The server `/audit-events` filter has no fixed factor/event allowlist that would reject the new values (it filters by string) — confirm by reading the handler; if an allowlist exists, add the values there too.
- [ ] `vitest run` green; `vue-tsc -b` 0.

**Verify:** `cd dashboard && npx vitest run && npx vue-tsc -b`

**Steps:**

- [ ] **Step 1:** Read `dashboard/src/lib/audit.ts`. Add the missing values to `AUDIT_FACTORS` (`settings`) and `AUDIT_EVENTS` (`sudo_granted`, `sudo_failed`, and any of `session_start`/`session_end`/`enrollment_consumed`/`clone_warning` not already present). The viewer renders raw strings (`{{ f }}`), so no i18n label additions are needed.
- [ ] **Step 2:** Read the `/audit-events` handler (`pkg/server/handle_admin_audit.go`) + its query; confirm the `factor`/`event` query params flow to a SQL `WHERE` with no server-side allowlist. If there IS an allowlist, extend it. (Likely none — note the finding either way.)
- [ ] **Step 3:** verify + commit.
```bash
git add dashboard/src/lib/audit.ts $(git ls-files -m dashboard/src 2>/dev/null)
git commit -m "feat(dashboard): add settings/sudo/session audit filter values"
```

---

## Phase 2 — P0 (security-critical blind spots)

> All P0/P1/P2 tasks are `blockedBy` Task 1. Each owns a disjoint file set (parallel-safe).

### Task 3: forward-auth / PAT gateway — audit failures & denials

**Goal:** The forward-auth gateway emits audit rows on PAT/cookie auth failures + RBAC denials (no success emission).

**Files:**
- Modify: `pkg/protocol/oidc/forward_auth.go`
- Test: `pkg/protocol/oidc/forward_auth_test.go`

**Acceptance Criteria:**
- [ ] `forward_auth.go` imports/uses `p.audit`. Emissions (all via `p.audit.Record(r.Context(), ...)`, no explicit IP — the ctx fills it, and `p.auditIP` is available if a non-request ctx is ever used):
  - `personal_access_token|fail` — unresolvable/disabled/not-granted PAT (`:402,406,412,419,426`), detail `{reason}` (e.g. `pat_unknown`, `account_disabled`, `pat_app_not_granted`).
  - `oidc_client|access_denied` — RBAC deny on the PAT path (`:434`) AND the cookie-session path (`:238–253`, the silent fall-through), detail `{reason:"app_access_denied", client_id, principal_kind:"pat"|"session"}`.
  - `oidc_client|fail` — callback state/PKCE/client failures in `HandleForwardAuthCallback`, detail `{reason}`.
- [ ] No emission on successful gateway auth.
- [ ] `go test ./pkg/protocol/oidc/` passes; a test asserts a denied PAT/cookie produces the expected record via a fake writer.

**Verify:** `go test ./pkg/protocol/oidc/ -run 'ForwardAuth'`

**Steps:**
- [ ] Read `forward_auth.go` (`verifyForwardAuthPAT` `:398–441`, `HandleForwardAuthVerify` cookie path `:236–253`, `HandleForwardAuthCallback`). The `Provider` already has `p.audit` (wired in `oidc.go`) and `p.auditIP`.
- [ ] Add the emissions above at each failure/deny branch. Where `client_id` (the FA app) is known, include it; PAT holder account ID is available after PAT resolution for the RBAC-deny case (include `AccountID`); for unresolved-PAT failures leave `AccountID` nil.
- [ ] Add a handler test with a fake `audit.Writer` capturing records; drive a denied PAT + a denied cookie session; assert factor/event/reason. Follow the existing forward_auth_test.go setup.
- [ ] Commit: `git add pkg/protocol/oidc/forward_auth.go pkg/protocol/oidc/forward_auth_test.go && git commit -m "feat(audit): forward-auth gateway failure/deny records"`

---

### Task 4: WebAuthn login — use / fail / clone_warning

**Goal:** The passkey login path writes `credential_event` rows.

**Files:**
- Modify: `pkg/server/handle_auth.go`
- Test: `pkg/server/handle_auth_test.go` (or nearest)

**Acceptance Criteria:**
- [ ] `webauthn|use` on login success (`handle_auth.go:349`), detail `{reason:"login"}`, `AccountID`=the account.
- [ ] `webauthn|fail` on each failure branch — ceremony missing/corrupt (`:248,258`), FinishLogin error (`:292`), no-account (`:295`), account-disabled (`:303`) — detail `{reason}`; `AccountID` when known (disabled path knows it; pre-account-resolution paths leave nil).
- [ ] `webauthn|clone_warning` on sign-count regression (`:329`), detail `{reason:"clone_warning"}`, `AccountID`.
- [ ] `go test ./pkg/server/ -run 'Login'` passes (or build+smoke if the handler isn't unit-testable — note which).

**Verify:** `go build -tags nodynamic ./... && go test ./pkg/server/ -run 'Login|Auth'`

**Steps:**
- [ ] Read `handle_auth.go` login-complete + failure branches. Add the emissions per the AC (no IP/UA — ctx fills). Mirror the password/TOTP store emission style for factor/event.
- [ ] Add/extend a handler test capturing records via the server's `s.Audit` fake if the harness supports it; otherwise rely on the smoke (Task 14) and note it.
- [ ] Commit: `git add pkg/server/handle_auth.go pkg/server/*_test.go && git commit -m "feat(audit): WebAuthn login use/fail/clone_warning records"`

_Note: `handle_auth.go` session_start (login) + session_end (logout) are added in Task 8 (session lifecycle), which also owns this file — Task 8 is `blockedBy` Task 4 to avoid a merge conflict on `handle_auth.go`._

---

### Task 5: OIDC token / refresh / revoke — failure audits + attribution

**Goal:** Token-endpoint failures are audited; refresh-reuse and revoke carry `AccountID`; OIDC logout uses `session_end`.

**Files:**
- Modify: `pkg/protocol/oidc/token.go`, `refresh.go`, `revoke.go`, `logout.go`
- Test: `pkg/protocol/oidc/*_test.go`

**Acceptance Criteria:**
- [ ] `oidc_client|fail` (via the existing `auditTokenEvent` helper where possible) on: client-auth failure (`token.go:94`), rate-limit (`:103`), code-exchange rejections — client mismatch, redirect_uri mismatch, PKCE fail, disabled account, dead session (`:156–215`) — detail `{reason}`.
- [ ] `refresh.go`: `oidc_client|fail` on client-mismatch-after-rotation (`:294`) + account-not-found/disabled (`:302`); `refresh_reuse` (`:278`) now carries `AccountID` (return the family AccountID out of `rotateRefresh`).
- [ ] `revoke.go`: the `revoke` record carries `AccountID` (from the access-token `sub` claim / `fam.AccountID`).
- [ ] `logout.go:121`: `EventUse` → `EventSessionEnd`.
- [ ] `go test ./pkg/protocol/oidc/` passes.

**Verify:** `go test ./pkg/protocol/oidc/`

**Steps:**
- [ ] Read `token.go` (`auditTokenEvent` `:334`, the exchange rejections `:94–215`), `refresh.go` (`rotateRefresh` + `grantRefreshToken`), `revoke.go` (`auditRevoked`), `logout.go` (`auditLogout`). Add fail emissions mirroring the existing `code_replay` fail at `token.go:137`. For `refresh_reuse` AccountID: change `rotateRefresh` to return the family's AccountID alongside the reuse error (or the family), and thread it into the audit call.
- [ ] Extend the OIDC tests (existing `verifyOIDCAuditEvents`-style unit or handler tests) for the new fail rows + the reuse AccountID.
- [ ] Commit: `git add pkg/protocol/oidc/token.go pkg/protocol/oidc/refresh.go pkg/protocol/oidc/revoke.go pkg/protocol/oidc/logout.go pkg/protocol/oidc/*_test.go && git commit -m "feat(audit): OIDC token/refresh/revoke failure records + attribution"`

---

### Task 6: SAML — replay & signature failure audits

**Goal:** SAML protocol-violation rejections write `saml_sp|fail` when the SP is resolved.

**Files:**
- Modify: `pkg/protocol/saml/sso.go`, `slo.go`
- Test: `pkg/protocol/saml/*_test.go`

**Acceptance Criteria:**
- [ ] `saml_sp|fail` on replayed AuthnRequest (`sso.go:300`), detail `{reason:"replayed_request", sp}`.
- [ ] `saml_sp|fail` on signature/parse failures in `ssoParseError` (`sso.go:356`) and `sloParseError` (`slo.go:557`) **only when the SP entity_id is resolved** (`ErrBadSignature`/`ErrMissingSignature`/weak-alg on a known SP); detail `{reason, sp}`. Pure-parse failures with no resolved SP stay slog-only (no meaningful record).
- [ ] `AccountID` may be nil (pre-auth). `go test ./pkg/protocol/saml/` passes.

**Verify:** `go test ./pkg/protocol/saml/`

**Steps:**
- [ ] Read `sso.go` (`HandleSSO` replay `:300`, `ssoParseError` `:356`) + `slo.go` (`sloParseError` `:557`). Add the `i.audit.Record(...)` emissions where the SP is known. Mirror `sso.go:162` (the existing `access_denied` record) for the record shape.
- [ ] Extend SAML tests for the replay + bad-signature-on-known-SP cases.
- [ ] Commit: `git add pkg/protocol/saml/sso.go pkg/protocol/saml/slo.go pkg/protocol/saml/*_test.go && git commit -m "feat(audit): SAML replay/signature failure records"`

---

## Phase 3 — P1 (session + credential lifecycle)

### Task 7: sudo — grant reshape + fail + clone_warning

**Goal:** Sudo records use the verified factor + `EventSudoGranted`/`EventSudoFailed`; the sudo clone-warning is audited.

**Files:**
- Modify: `pkg/server/handle_sudo.go`
- Test: `pkg/server/handle_sudo_test.go`

**Acceptance Criteria:**
- [ ] Sudo success (`:369`): factor = the verified method (`webauthn`|`password`|`totp`), event `EventSudoGranted`, detail `{method}`. (Replaces `FactorSession` + the `"sudo_granted"` literal.)
- [ ] Sudo failure paths (bad password `:314`, bad TOTP `:326`, FinishLogin fail `:267`, ceremony expired `:220`, intent parse `:225`): emit `EventSudoFailed` with the verified/attempted factor + detail `{reason}`.
- [ ] Sudo clone-warning (`:281`): `webauthn|clone_warning`.
- [ ] Remove the `if s.Audit != nil` special guard is NOT required (harmless) — leave or keep consistent; do not introduce new nil-guards.
- [ ] `go test ./pkg/server/ -run 'Sudo'` passes.

**Verify:** `go test ./pkg/server/ -run 'Sudo'`

**Steps:**
- [ ] Read `handle_sudo.go` (grant `:367–385`, fail branches, clone `:281`). Map each verified method to its factor. Replace the grant record; add fail records; add the clone record.
- [ ] Update/extend the sudo test.
- [ ] Commit: `git add pkg/server/handle_sudo.go pkg/server/handle_sudo_test.go && git commit -m "feat(audit): sudo grant reshape + fail + clone_warning records"`

---

### Task 8: session lifecycle — session_start / session_end

**Goal:** Every login emits `session|session_start`; explicit logout/revoke emits `session|session_end`.

**Files:**
- Modify: `pkg/server/handle_auth.go` (login-complete session_start; logout session_end), `handle_auth_password.go` (session_start), `handle_auth_recovery.go` (session_start), `handle_forward_auth_signout.go` (session_end)
- Test: relevant `_test.go` + smoke

_Scope note: `handle_enrollment.go` + `handle_pairing.go` session_start belong to Task 11 (which owns those files completely, incl. their enrollment/pairing lifecycle) — kept out of this task to keep file ownership disjoint. `handle_federation_confirm.go` session_start is also Task 11._

**Acceptance Criteria:**
- [ ] `session|session_start` (detail `{via:"webauthn"|"password_totp"|"recovery"}`) emitted in `handle_auth.go` (login-complete), `handle_auth_password.go`, and `handle_auth_recovery.go` right after `sessionStore.Issue` succeeds, `AccountID`=the account. Emitted at the HANDLER (ctx has IP/UA).
- [ ] `session|session_end` on the dashboard logout (`handle_auth.go:371`) and forward-auth signout (`handle_forward_auth_signout.go`), detail `{reason:"logout"|"forward_auth_signout"}`.
- [ ] `go build -tags nodynamic ./...` clean; `go test ./pkg/server/` builds+runs.
- [ ] `blockedBy` Task 4 (shares `handle_auth.go`).

**Verify:** `go build -tags nodynamic ./... && go test ./pkg/server/ -run 'Login|Logout|Recovery'`

**Steps:**
- [ ] In `handle_auth.go`, `handle_auth_password.go`, `handle_auth_recovery.go`: locate the `sessionStore.Issue(...)` call and emit `session|session_start` on success.
- [ ] Add `session|session_end` on `handle_auth.go` logout (`:371`) and `handle_forward_auth_signout.go`.
- [ ] Commit: `git add pkg/server/handle_auth.go pkg/server/handle_auth_password.go pkg/server/handle_auth_recovery.go pkg/server/handle_forward_auth_signout.go && git commit -m "feat(audit): session_start/session_end lifecycle records"`

---

### Task 9: self-service /me — credential + profile + consent + avatar

**Goal:** `/me` mutations write `credential_event` rows.

**Files:**
- Modify: `pkg/server/handle_me.go`, `handle_me_consent.go`, `handle_avatar.go`
- Test: relevant `_test.go`

**Acceptance Criteria:**
- [ ] `webauthn|register` on add-passkey (`handle_me.go:351`); `webauthn|revoke` on remove (`:530`); `webauthn|update` on rename (`:470`, detail `{reason:"rename"}`).
- [ ] `session|session_end` on `/me/sessions/revoke` (`:423`, detail `{reason:"self_revoke", session_id}`).
- [ ] `oidc_client|access_revoked` / `saml_sp|access_revoked` on consent revoke (`handle_me_consent.go:112`, detail `{client_id|sp, kind}`).
- [ ] `account|update` on avatar upload/select/remove (`handle_avatar.go`, detail `{reason:"avatar_*"}`) and profile displayName (`handle_me.go:77`, detail `{reason:"display_name"}`).
- [ ] `go build -tags nodynamic ./...` clean; existing `/me` tests pass.

**Verify:** `go build -tags nodynamic ./... && go test ./pkg/server/ -run 'Me|Avatar|Consent'`

**Steps:**
- [ ] Add emissions at each site per AC (all `s.Audit.Record(r.Context(), ...)`, no IP/UA). `AccountID`=the acting user (`sess.Account.ID`).
- [ ] Commit: `git add pkg/server/handle_me.go pkg/server/handle_me_consent.go pkg/server/handle_avatar.go pkg/server/*_test.go && git commit -m "feat(audit): self-service credential/profile/consent/avatar records"`

---

### Task 10: admin account handlers — credential delete, single-session revoke, displayName diff

**Goal:** Admin-initiated account mutations that are currently unaudited emit records; enrollment-issued re-factored.

**Files:**
- Modify: `pkg/server/handle_account.go`
- Test: `pkg/server/handle_account_test.go`

**Acceptance Criteria:**
- [ ] Admin credential-delete (`handle_account.go:467` typed + `:522` raw) emits `webauthn|revoke` (or the factor of the deleted credential) with `AccountID`=target, actor in detail `{actor_id, target_id}`.
- [ ] Admin single-session revoke (`:937`) emits `session|session_end` (matching the bulk-revoke at `:604`).
- [ ] `displayName` added to the tracked-changes diff (`:260–279`) so a name-only update emits `account|update`.
- [ ] Enrollment-issued (`:681`) re-filed from `FactorAccount` → `FactorEnrollment` (event stays `enrollment_issued`).
- [ ] `go test ./pkg/server/ -run 'Account'` passes.

**Verify:** `go test ./pkg/server/ -run 'Account'`

**Steps:**
- [ ] Add the two missing emissions; add `displayName` to the `changes` map; change the enrollment-issued factor. Mirror the existing `handle_account.go` `auditActor(sess)` + detail idiom.
- [ ] Commit: `git add pkg/server/handle_account.go pkg/server/handle_account_test.go && git commit -m "feat(audit): admin credential-delete + single-session revoke + displayName/enrollment fixes"`

---

### Task 11: enrollment / pairing / federation-confirm lifecycle

**Goal:** Enrollment consume + failure gates, pairing lifecycle, and /welcome confirm/decline are audited.

**Files:**
- Modify: `pkg/server/handle_enrollment.go`, `handle_pairing.go`, `handle_federation_confirm.go`
- Test: relevant `_test.go`

**Acceptance Criteria:**
- [ ] `enrollment|enrollment_consumed` on successful enrollment (`handle_enrollment.go:510`), `AccountID`=new account; `enrollment|fail` on the security gates (federation-required `:198,407`; disabled `:267,475`; consumed/expired), detail `{reason}`. Also emit `session|session_start` (detail `{via:"enrollment"}`) after the enrollment's `sessionStore.Issue` (`:529`) — enrollment's session_start lives here (Task 8 excludes this file).
- [ ] Pairing lifecycle (`handle_pairing.go`): begin/complete/approve/cancel → records (factor `session` for the pairing session events; `session|session_start` on complete which issues a session — coordinate with Task 8's `via`, use `via:"pairing"`), detail `{reason}`. (Pairing owns its session_start here since Task 8 excludes this file.)
- [ ] `/welcome`: confirm-YES (`handle_federation_confirm.go:86`) → `federation_oidc|use` + `session|session_start` (detail `{via:"federation"}`); decline (`:130`) → `federation_oidc|fail` detail `{reason:"confirm_declined"}`.
- [ ] `go build -tags nodynamic ./...` clean; tests pass.

**Verify:** `go build -tags nodynamic ./... && go test ./pkg/server/ -run 'Enroll|Pair|FederationConfirm|Confirm'`

**Steps:**
- [ ] Add the emissions per AC. Enrollment complete is inside a tx (account+credential insert) — emit `enrollment_consumed` via the tx-scoped writer if inside the tx, else the handler after commit; failure gates use the outer writer. Follow `modes.go` for the tx-writer rule.
- [ ] Commit: `git add pkg/server/handle_enrollment.go pkg/server/handle_pairing.go pkg/server/handle_federation_confirm.go pkg/server/*_test.go && git commit -m "feat(audit): enrollment/pairing/welcome-confirm lifecycle records"`

---

## Phase 4 — P2 (consistency of existing records)

### Task 12: FactorSettings reclassification

**Goal:** Instance-settings / branding / client-IP mutations file under `FactorSettings`, not `FactorSigningKey`.

**Files:**
- Modify: `pkg/server/handle_admin_settings.go` (the `auditBranding` helper), `handle_admin_client_ip.go` (if it sets the factor itself)
- Test: `pkg/server/*_test.go`

**Acceptance Criteria:**
- [ ] `auditBranding` (`handle_admin_settings.go:156`) uses `Factor: audit.FactorSettings`; the 7 settings mutations (name/icon/maintenance/login-bg/client-ip) now emit `factor=settings` with their existing `reason` detail + actor.
- [ ] No emission still mis-files a settings mutation under `signing_key`.
- [ ] `go test ./pkg/server/` passes.

**Verify:** `go build -tags nodynamic ./... && go test ./pkg/server/ -run 'Settings|Branding|Maintenance|ClientIP'`

**Steps:**
- [ ] Change `FactorSigningKey` → `FactorSettings` in `auditBranding`; update its doc comment. Confirm `handle_admin_client_ip.go` routes through `auditBranding` (it does per the audit) — no separate change needed unless it sets its own factor.
- [ ] Commit: `git add pkg/server/handle_admin_settings.go pkg/server/handle_admin_client_ip.go pkg/server/*_test.go && git commit -m "fix(audit): file instance-settings mutations under FactorSettings"`

---

### Task 13: attribution & detail-quality fixes

**Goal:** Records carry the missing identifiers.

**Files:**
- Modify: `pkg/server/handle_me_identities.go`, `handle_admin_groups.go`, `handle_admin_account_tokens.go`
- Test: relevant `_test.go`

**Acceptance Criteria:**
- [ ] Unlink (`handle_me_identities.go:209`) detail gains `idp_slug`, `upstream_iss`, `upstream_sub` (read from the identity row before delete).
- [ ] Group-delete (`handle_admin_groups.go:332`) detail gains `slug` (read before the hard-delete).
- [ ] Admin PAT-revoke (`handle_admin_account_tokens.go:85`) detail gains `target_account_id` (the PAT owner).
- [ ] `go test ./pkg/server/` passes.

**Verify:** `go test ./pkg/server/ -run 'Identit|Group|Token'`

**Steps:**
- [ ] At each site, load the human-readable identifier before the mutation and add it to the detail map. (OIDC revoke AccountID is handled in Task 5.)
- [ ] Commit: `git add pkg/server/handle_me_identities.go pkg/server/handle_admin_groups.go pkg/server/handle_admin_account_tokens.go pkg/server/*_test.go && git commit -m "fix(audit): add idp/group/PAT-owner identifiers to detail"`

---

## Phase 5 — Gate

### Task 14: smoke coverage + full gate + dist

**Goal:** Extend the smoke's audit assertions to the new events, run the full gate, rebuild dist.

**Files:**
- Modify: `cmd/smoke/main.go` (extend `verify*AuditEvents` + `assertAuditDetailNoSecret`)
- Modify: `pkg/webui/dist/**` (rebuilt, if the frontend list changed)

**Acceptance Criteria:**
- [ ] Smoke asserts the new rows appear: webauthn login `use`+`fail`, `session_start`/`session_end`, forward-auth PAT `fail`/`access_denied`, sudo `sudo_granted`/`sudo_failed`, a settings mutation under `factor=settings`, consent-revoke `access_revoked`. Extend `assertAuditDetailNoSecret` + `secretLeakNeedles` to the new sites.
- [ ] Full gate green: `go build -tags nodynamic ./...`, `go vet ./...`, `go test ./...`, vitest, vue-tsc, check-contrast, live `mise run ci:smoke` `SMOKE_EXIT=0`.
- [ ] `pkg/webui/dist` rebuilt + committed (frontend filter list changed in Task 2).
- [ ] `blockedBy` all prior tasks.

**Verify:** full gate (below) + live smoke `SMOKE_EXIT=0`

**Steps:**
- [ ] Read the existing `verifyCoreAuditEvents`/`verifyOIDCAuditEvents`/etc. in `cmd/smoke/main.go` + `assertAuditDetailNoSecret` + `secretLeakNeedles`. Add assertions for the new events where the smoke already drives the action (login, sudo, logout, settings PUT, consent — most are already exercised; assert the new rows now exist). Where a new action isn't yet driven, add a minimal step or rely on the existing arc.
- [ ] `cd dashboard && npm run build`.
- [ ] Full gate:
```bash
go build -tags nodynamic ./... && go vet ./... && go test ./... 2>&1 | tail -20
cd dashboard && npx vitest run && npx vue-tsc -b && node scripts/check-contrast.mjs && cd ..
```
then `mise run ci:smoke` → `SMOKE_EXIT=0` (long timeout; do not kill mid-run).
- [ ] Commit: `git add cmd/smoke/main.go pkg/webui/dist && git commit -m "test(smoke): assert new audit-log coverage; rebuild dist"`

---

## Self-Review

**Spec coverage:**
- Seam (ctx IP/UA + ctx-aware Record) + vocabulary → Task 1. ✓
- Frontend filter propagation → Task 2. ✓
- P0: forward-auth gateway → Task 3; WebAuthn login → Task 4; OIDC token/refresh/revoke/logout → Task 5; SAML replay/signature → Task 6. ✓
- P1: sudo reshape/fail/clone → Task 7; session_start/end → Task 8 (+ pairing/welcome session_start in Task 11); self-service /me → Task 9; admin account → Task 10; enrollment/pairing/welcome → Task 11. ✓
- P2: FactorSettings → Task 12; attribution (unlink/group/PAT) → Task 13; OIDC-revoke AccountID → Task 5; sudo event/factor → Task 7; enrollment factor → Task 10. ✓
- Testing/smoke/gate/dist → Task 14 + per-task tests. ✓
- Out-of-scope (P3, adjacent sudo-gating, introspect/userinfo) → not planned. ✓

**Placeholder scan:** Foundation (Task 1) + Task 2 have full code. Emission tasks give exact sites (file:line from the findings doc) + factor/event/detail per site + the shared idiom + mirror/tx-writer instructions — the concrete "what to emit" is specified for every site; the "how" is the one documented idiom. No TBD/vague-error-handling.

**Type/consistency:** `audit.WithRequestMeta`/`ipFromCtx`/`uaFromCtx`, `requestMetaMW`, `FactorSettings`/`EventSudoGranted`/`EventSudoFailed` names are consistent across Task 1 and consumers. File-ownership is disjoint (each handler file appears in exactly one task) EXCEPT `handle_auth.go` (Tasks 4 + 8) — Task 8 is `blockedBy` Task 4 to serialize. `handle_federation_confirm.go` session_start is assigned to Task 11 (not Task 8) to keep files disjoint; noted in both. Detail keys (`reason`, `client_id`, `sp`, `via`, `session_id`, `target_account_id`, `idp_slug`) are consistent with existing usage.
