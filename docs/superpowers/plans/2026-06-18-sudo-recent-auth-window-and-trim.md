# Sudo recent-auth window + gate trim + login-aligned modal — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cut step-up friction (grant a multi-use sudo window at login, trim over-strict gates) and align the step-up modal with the login screen, removing federation re-auth from the sudo surface entirely.

**Architecture:** Three coordinated backend changes turn the one-shot, login-blind sudo grant into a *recent-auth window* read in a single gate chokepoint; the admin gate set is trimmed to posture/high-impact ops; the `federation_oidc` step-up method (modal + backend + db query + error codes) is deleted; and `SudoModal.vue` is restyled to mirror the login screen's local section, bouncing upstream-only users to `/login` when they have no local factor.

**Tech Stack:** Go (chi + huma, sqlc, pgx), Vue 3 + TypeScript (vitest), SPA embedded via `go:embed` into `pkg/webui/dist`.

**Spec:** `docs/superpowers/specs/2026-06-18-sudo-recent-auth-window-and-trim-design.md` (supersedes `2026-06-12-oidc-sudo-step-up-design.md`).

**Implementation note on the window mechanism:** The spec §1 describes "stamp `SudoUntil` at login." During planning we chose the behaviorally-identical but lower-blast-radius mechanism: a **read-side** recent-auth window in the server gate keyed off `SessionData.IssuedAt` (the login timestamp, stable across refreshes). This avoids changing `NewSessionStore`'s signature (15 call sites) and touches no login handlers. Observable behavior matches the spec.

---

### Task 1: Bump the sudo TTL default to 15 minutes

**Goal:** The recent-auth window length (`auth.sudo_ttl`) defaults to 15m instead of 5m.

**Files:**
- Modify: `pkg/configx/configx.go` (the `viper.SetDefault("auth.sudo_ttl", …)` line ~227 and the explanatory comment ~217 and the struct-field comment ~125)
- Modify: `pkg/configx/configx_test.go` (the assertion ~64-65)
- Modify: `CONFIG.md` (the `PROHIBITORUM_AUTH_SUDO_TTL` row ~74)

**Acceptance Criteria:**
- [ ] `auth.sudo_ttl` default is `15 * time.Minute`.
- [ ] `configx_test.go` asserts 15m and passes.
- [ ] CONFIG.md documents `15m` and describes it as the recent-auth/step-up window.

**Verify:** `go test ./pkg/configx/... -run TestDefaults -v` → PASS

**Steps:**

- [ ] **Step 1: Update the default and comments in `pkg/configx/configx.go`**

Change the default line:
```go
	viper.SetDefault("auth.sudo_ttl", 15*time.Minute)
```
Update the nearby block comment (~line 217) that currently says "SudoTTL/PartialSessionTTL are both five minutes…" so it no longer claims 5m for SudoTTL — e.g.:
```go
	// PartialSessionTTL is five minutes: short enough to bound post-compromise
	// blast radius, long enough that the user can complete a follow-up step
	// without re-authenticating. SudoTTL (the recent-auth / step-up window) is
	// longer — 15m — because it is now granted at login and is multi-use, so a
	// 5m window would re-prompt a user still actively managing settings.
```
Update the `SudoTTL` struct-field doc comment (~line 125) to describe it as "the recent-auth window: how long after a full authentication (or an explicit step-up) sensitive endpoints accept the session without a fresh step-up."

- [ ] **Step 2: Update the config test in `pkg/configx/configx_test.go`**

```go
	if cfg.Auth.SudoTTL != 15*time.Minute {
		t.Errorf("Auth.SudoTTL: want 15m, got %v", cfg.Auth.SudoTTL)
	}
```

- [ ] **Step 3: Update `CONFIG.md`**

Change the row to:
```
| `PROHIBITORUM_AUTH_SUDO_TTL` | `15m` | Recent-auth window: how long after a full sign-in (or an explicit step-up) sensitive actions are allowed without re-verifying. |
```

- [ ] **Step 4: Verify**

Run: `go test ./pkg/configx/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/configx/configx.go pkg/configx/configx_test.go CONFIG.md
git commit -m "config(sudo): default the recent-auth window (sudo_ttl) to 15m"
```

---

### Task 2: Recent-auth window + multi-use gate (drop one-shot)

**Goal:** A freshly authenticated session is elevated for `SudoTTL` (recent-auth window), and a step-up grant is multi-use (covers many gated actions until it expires by time), instead of one-shot.

**Files:**
- Modify: `pkg/server/handle_sudo.go` (replace `consumeFreshSudo` with a read-only `hasFreshSudo`; simplify `requireFreshSudo`)
- Modify: `pkg/server/operations.go` (the `registerSudoOp` middleware call site)
- Modify: `pkg/authn/middleware.go` (the `SudoUntil` doc comment)
- Modify: `pkg/server/handle_sudo_test.go` (remove the one-shot fail-closed test; add a window test)
- Modify: `pkg/server/handle_me_password_test.go` (flip one-shot assertions to multi-use)
- Modify: `pkg/server/handle_me_revoke_pwd_totp_test.go` (flip one-shot assertion to multi-use)

**Acceptance Criteria:**
- [ ] The gate is satisfied when `HasFreshSudo()` is true OR the session was issued within `SudoTTL`.
- [ ] The gate no longer writes to the session store (no clear, no Save) — pure read.
- [ ] Two consecutive gated actions within a single window both succeed (was a re-prompt before).
- [ ] A session issued longer ago than `SudoTTL`, with zero `SudoUntil`, is denied (`sudo_required`).
- [ ] Existing `&Server{}`-based gate tests (zero `SudoTTL`) still get 401 — the window is inert at TTL=0.

**Verify:** `go test ./pkg/server/... -run 'Sudo|FreshSudo|Password|RevokePwd' -v` → PASS

**Steps:**

- [ ] **Step 1: Replace `consumeFreshSudo` with a read-only `hasFreshSudo` in `pkg/server/handle_sudo.go`**

Replace the entire `consumeFreshSudo` function (the one-shot version, currently ~lines 511-536) with:
```go
// hasFreshSudo reports whether the session is currently elevated. Two ways
// satisfy it, and BOTH are time-windowed (multi-use — nothing is consumed):
//
//   1. Recent full authentication: the session was issued within SudoTTL. Every
//      login Issues a session with IssuedAt=now (stable across refreshes), so a
//      user who just signed in can perform gated actions without a separate
//      step-up. This is the recent-auth window.
//   2. Explicit step-up: POST /me/sudo/complete stamped SudoUntil = now+SudoTTL
//      (see applySudoGrant), used when the login is older than the window.
//
// It writes NOTHING and consumes nothing — a single elevation covers every gated
// action until the window expires by time. This is THE single chokepoint for the
// fresh-sudo gate: both the raw-HTTP requireFreshSudo path (registerSudoOpHTTP)
// and the typed Huma registerSudoOp path route through it, so the policy can't
// drift between the two registration styles.
//
// With SudoTTL == 0 (the zero-config &Server{} used by unit tests) the
// recent-auth clause is always false, so the gate falls back to SudoUntil only —
// preserving the existing "no fresh sudo → deny" test semantics.
func (s *Server) hasFreshSudo(sess *authn.Session) bool {
	if sess == nil || sess.Data == nil {
		return false
	}
	if sess.Data.HasFreshSudo() {
		return true
	}
	ttl := s.config.Auth.SudoTTL
	return ttl > 0 && time.Since(sess.Data.IssuedAt) < ttl
}
```

- [ ] **Step 2: Simplify `requireFreshSudo` in `pkg/server/handle_sudo.go`**

Replace the body so it calls `hasFreshSudo` (the `ctx` param is now unused but kept to avoid churning the 7 self-service call sites; an unused parameter is legal Go):
```go
// requireFreshSudo is the raw-HTTP fresh-sudo gate: on absence of a fresh grant
// it writes ErrSudoRequired (401) and returns true so the caller returns
// immediately. False means satisfied — proceed. ctx is retained for call-site
// compatibility; the gate is now a pure read (no KV write).
func (s *Server) requireFreshSudo(_ context.Context, w http.ResponseWriter, sess *authn.Session) bool {
	if !s.hasFreshSudo(sess) {
		writeAuthErr(w, authn.ErrSudoRequired())
		return true
	}
	return false
}
```

- [ ] **Step 3: Update the `registerSudoOp` middleware in `pkg/server/operations.go`**

Find the line in `registerSudoOp` (~line 75):
```go
		if !s.consumeFreshSudo(ctx.Context(), sess) {
```
Change to:
```go
		if !s.hasFreshSudo(sess) {
```

- [ ] **Step 4: Update the `SudoUntil` doc comment in `pkg/authn/middleware.go`**

Replace the `SudoUntil` field comment (~lines 27-31) with:
```go
	// SudoUntil is the deadline for an EXPLICIT step-up grant (POST
	// /me/sudo/complete). The gate is also satisfied for SudoTTL after IssuedAt
	// (the recent-auth window), so SudoUntil stays zero for sessions that never
	// needed an explicit step-up. Zero or past means no explicit grant; future
	// means the step-up gate is satisfied. The window check lives in the server
	// gate (hasFreshSudo), which has the configured SudoTTL.
	SudoUntil time.Time `json:"sudo_until,omitempty"`
```

- [ ] **Step 5: Remove the now-obsolete one-shot fail-closed test in `pkg/server/handle_sudo_test.go`**

Search the file for the test that constructs `failingSaveKV` (around line 103) to assert the one-shot clear fails closed (SESS-1). Since the gate no longer Saves, delete that whole test function. Confirm with:
```bash
grep -n "failingSaveKV\|one-shot\|consumeFreshSudo" pkg/server/handle_sudo_test.go
```
Remove the test(s) and the `failingSaveKV` helper type if it is now unused (grep shows no other references).

- [ ] **Step 6: Add a recent-auth window test in `pkg/server/handle_sudo_test.go`**

Append:
```go
func TestHasFreshSudo_RecentAuthWindow(t *testing.T) {
	s := &Server{}
	s.config.Auth.SudoTTL = 15 * time.Minute

	// Recently issued, no explicit SudoUntil → elevated by the window.
	fresh := &authn.Session{Data: &authn.SessionData{IssuedAt: time.Now()}}
	if !s.hasFreshSudo(fresh) {
		t.Fatal("recently-issued session should satisfy the gate (recent-auth window)")
	}

	// Issued longer ago than the window, zero SudoUntil → denied.
	stale := &authn.Session{Data: &authn.SessionData{IssuedAt: time.Now().Add(-30 * time.Minute)}}
	if s.hasFreshSudo(stale) {
		t.Fatal("stale session with no step-up should NOT satisfy the gate")
	}

	// Stale issue but an explicit step-up still in its window → elevated.
	stepped := &authn.Session{Data: &authn.SessionData{
		IssuedAt:  time.Now().Add(-30 * time.Minute),
		SudoUntil: time.Now().Add(5 * time.Minute),
	}}
	if !s.hasFreshSudo(stepped) {
		t.Fatal("explicit step-up window should satisfy the gate")
	}

	// Zero-config (TTL=0): window inert, falls back to SudoUntil only.
	zero := &Server{}
	if zero.hasFreshSudo(fresh) {
		t.Fatal("with SudoTTL=0 the recent-auth window must be inert")
	}
}
```

- [ ] **Step 7: Flip the one-shot assertions in the gated-handler tests to multi-use**

In `pkg/server/handle_me_password_test.go`, find the comments/assertions about "the gate is one-shot" and "requireFreshSudo cleared SudoUntil" (around lines 20, 124-128). The test there mints a session with a future `SudoUntil` and (likely) asserts a second call re-prompts. Update it so that with a future `SudoUntil` (or recent `IssuedAt`) a SECOND `/me/password/set` call also succeeds (gate is multi-use), and remove the "cleared SudoUntil" expectation. Concretely: after the first successful call, re-issue the same request with the same session and assert it does NOT return `sudo_required`.

In `pkg/server/handle_me_revoke_pwd_totp_test.go` (around line 113), do the same: remove the "one-shot consume" expectation; a second gated call within the window must still pass the gate.

Run after editing:
```bash
go test ./pkg/server/... -run 'Password|RevokePwd|Sudo|FreshSudo' -v
```
Expected: PASS (fix any remaining assertion that depended on one-shot clearing).

- [ ] **Step 8: Build + full server package test**

Run: `go build -tags nodynamic ./... && go test ./pkg/server/... ./pkg/authn/... -v`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add pkg/server/handle_sudo.go pkg/server/operations.go pkg/authn/middleware.go \
        pkg/server/handle_sudo_test.go pkg/server/handle_me_password_test.go \
        pkg/server/handle_me_revoke_pwd_totp_test.go
git commit -m "feat(sudo): recent-auth window + multi-use grant (drop one-shot gate)"
```

---

### Task 3: Trim admin gates to posture/high-impact only

**Goal:** Drop the fresh-sudo requirement from reversible admin operational config (group/access/SAML/toggles/session-revoke/invitation-revoke), keeping admin authentication; keep step-up on credential/secret/privilege/destructive ops.

**Files:**
- Modify: `pkg/server/server.go` (swap `registerSudoOp`→`registerOp` and `s.registerSudoOpHTTP`→`registerOpHTTP` for the DROP list, ~lines 415-482)
- Modify: `pkg/server/admin_route_policy_test.go` (remove the dropped routes from `sudoGatedRoutes`)

**Acceptance Criteria:**
- [ ] DROP routes are registered with the plain (admin-auth, no-sudo) helper and still require `admin`.
- [ ] KEEP routes remain on the sudo helper.
- [ ] `TestAdminMutationRoutesRequireSudo` passes with `sudoGatedRoutes` containing ONLY the KEEP routes.

**Verify:** `go test ./pkg/server/... -run TestAdminMutationRoutesRequireSudo -v` → PASS

**Steps:**

- [ ] **Step 1: Swap the DROP registrations in `pkg/server/server.go`**

For each route below, change ONLY the registration helper (keep method, path, `admin`, and handler identical). The conversions:
- Typed Huma: `registerSudoOp(s, mgmt, OP, handler, admin)` → `registerOp(mgmt, OP, handler, admin)` (note: `registerOp` drops the leading `s` arg).
- Raw HTTP: `s.registerSudoOpHTTP(s.router, M, P, admin, h)` → `registerOpHTTP(s.router, M, P, admin, h)` (free function, no `s.` receiver).

DROP list (swap these):
```
# typed (registerSudoOp → registerOp)
contract.OperationRevokeAccountSessions  (POST /accounts/revoke-sessions)
contract.OperationRevokeInvitation       (POST /invitations/revoke)

# raw (s.registerSudoOpHTTP → registerOpHTTP)
POST /api/prohibitorum/accounts/{id}/sessions/revoke
POST /api/prohibitorum/oidc-applications/set-disabled
POST /api/prohibitorum/oidc-applications/{clientId}/access/set-restricted
POST /api/prohibitorum/oidc-applications/{clientId}/access/grant
POST /api/prohibitorum/oidc-applications/{clientId}/access/revoke
POST /api/prohibitorum/identity-providers/set-disabled
POST /api/prohibitorum/saml-applications
PUT  /api/prohibitorum/saml-applications/{id}
POST /api/prohibitorum/saml-applications/{id}/reingest-metadata
POST /api/prohibitorum/saml-applications/set-disabled
POST /api/prohibitorum/saml-applications/delete
POST /api/prohibitorum/saml-applications/{id}/access/set-restricted
POST /api/prohibitorum/saml-applications/{id}/access/grant
POST /api/prohibitorum/saml-applications/{id}/access/revoke
POST /api/prohibitorum/groups
PUT  /api/prohibitorum/groups/{id}
POST /api/prohibitorum/groups/delete
POST /api/prohibitorum/groups/{id}/members
POST /api/prohibitorum/groups/{id}/members/remove
```

Leave ALL other admin routes (the KEEP list) on the sudo helpers: signing-keys generate/activate/retire; oidc-applications create/PUT-update/rotate-secret/delete; identity-providers create/PUT-update/rotate-secret/delete; accounts update(PUT)/set-disabled/delete/credentials-delete/reissue-enrollment; invitations create.

- [ ] **Step 2: Remove the dropped routes from `sudoGatedRoutes` in `pkg/server/admin_route_policy_test.go`**

Delete these entries from the `sudoGatedRoutes` slice (they are no longer sudo-gated):
- SAML application management block (all 4: create, PUT, reingest-metadata, delete)
- `POST /accounts/1/sessions/revoke`
- the entire "Group CRUD + membership management" block (5 entries)
- `POST /accounts/revoke-sessions`
- `POST /invitations/revoke`
- the entire "App-access management — OIDC" block (3 entries)
- the entire "App-access management — SAML" block (3 entries)
- `oidc-applications/set-disabled` and `identity-providers/set-disabled` if present (note: the current list does not include the set-disabled toggles — only remove ones that are actually present)

Keep: signing-keys (3), OIDC create/PUT/rotate-secret/delete (4), IdP create/PUT/rotate-secret/delete (4), accounts credentials/delete, accounts PUT/delete/reissue-enrollment, invitations create.

- [ ] **Step 3: Verify the policy test and build**

Run: `go build -tags nodynamic ./... && go test ./pkg/server/... -run TestAdminMutationRoutesRequireSudo -v`
Expected: PASS. (If a KEEP route is missing from the list, add it; if a removed route still 401s, you missed a swap in server.go.)

- [ ] **Step 4: Sanity-check no DROP route still routes through sudo**

Run:
```bash
grep -nE "registerSudoOp(HTTP)?" pkg/server/server.go | grep -Ei "saml-applications|/groups|access/|sessions/revoke|invitations/revoke|set-disabled"
```
Expected: only `identity-providers`/`oidc-applications` *non-dropped* lines, and NO `saml-applications`, `/groups`, `access/`, `sessions/revoke`, or `invitations/revoke`. (The OIDC/IdP `set-disabled` lines should NOT appear under registerSudoOp anymore.)

- [ ] **Step 5: Commit**

```bash
git add pkg/server/server.go pkg/server/admin_route_policy_test.go
git commit -m "feat(sudo): trim admin step-up gates to posture/high-impact only"
```

---

### Task 4: Remove the federation_oidc step-up method (backend)

**Goal:** Delete the entire federation step-up surface — modal-facing methods response, begin branch, the authenticated callback, the `Federator.Sudo*` methods, the `FedState` sudo fields/key, the sudo-only error codes, and the sudo-only `ListLinkedEnabledIdPs` query — without touching primary federation login.

**Files (delete/edit — verify exact lines, they may have drifted):**
- Modify: `pkg/server/handle_sudo.go`
- Modify: `pkg/server/server.go`
- Modify: `pkg/federation/oidc/federation.go`
- Modify: `pkg/federation/oidc/state.go`
- Modify: `pkg/federation/oidc/client.go`
- Modify: `pkg/authn/errors.go`
- Modify: `db/queries/account_identity.sql` + regenerate `pkg/db/*`
- Modify/Delete tests: `pkg/server/handle_sudo_test.go`, `pkg/federation/oidc/federation_test.go`

**KEEP (do NOT delete — used by primary login/link/invite):** `authn.MethodFederationOIDC`, `validateFederationReturnTo`, `ListAccountIdentitiesByAccount`, `CountUsableSignInFederation`, the entire login/link/invite federation flow.

**Acceptance Criteria:**
- [ ] No code references `SudoBegin`, `SudoCallback`, `SudoKey`, `StepUpAuthOptions`, `maxStepUpAuthAge`, `sudoFederator`, `sudoFederatorOverride`, `sudoFed`, `sudoFederationProvider`, `ListLinkedEnabledIdPs`, `ErrSudoIdentityMismatch`, `ErrSudoReauthStale`, or the `federation_oidc` branch in `/me/sudo/begin`.
- [ ] `/me/sudo/methods` returns `{"methods": [...]}` with no `federationProviders`.
- [ ] Primary federation login/link/invite tests still pass.
- [ ] `go build -tags nodynamic ./...` is clean; `go vet ./...` shows no unused symbols.

**Verify:** `go build -tags nodynamic ./... && go test ./pkg/server/... ./pkg/federation/... -v` → PASS

**Steps:**

- [ ] **Step 1: `pkg/server/handle_sudo.go` — remove the federation surface**

Delete: the `sudoFederator` interface, the `sudoFed()` method, the `sudoFederationProvider` struct, and `handleSudoFederationCallbackHTTP` (the whole function). In `handleSudoMethodsHTTP`, delete the `providers`/`ListLinkedEnabledIdPs` block and change the response to:
```go
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"methods": methods})
```
In the `sudoFlowQueries` interface, delete the `ListLinkedEnabledIdPs` method line (keep `authn.FlowQueries` and `ListRecoveryCodesByAccount`). In `handleSudoBeginHTTP`: delete the `case string(authn.MethodFederationOIDC):` branch, and remove the `if body.Method != string(authn.MethodFederationOIDC) {` guard around the intent stash so the stash runs unconditionally (both remaining methods finish at `/complete`):
```go
	// Stash the chosen method so /complete dispatches the right verifier.
	intent := sudoIntent{Method: body.Method, IssuedAt: time.Now().UTC()}
	payload, err := json.Marshal(intent)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("sudo/begin: marshal intent: %w", err))
		return
	}
	if err := s.kvStore.SetEx(r.Context(), sudoIntentKey(sess.Data.SessionID), string(payload), 5*time.Minute); err != nil {
		writeAuthErr(w, fmt.Errorf("sudo/begin: setex intent: %w", err))
		return
	}
```
Remove the now-unused `fedoidc` and `sessstore` imports IF they become unused (let the compiler tell you — `sessstore` is still used by `applySudoGrant`; `fedoidc` likely becomes unused → remove it).

- [ ] **Step 2: `pkg/server/server.go` — remove the route and the test-override field**

Delete the route registration line:
```go
	registerOpHTTP(s.router, "GET", "/api/prohibitorum/me/sudo/federation/callback", sessionReq, s.handleSudoFederationCallbackHTTP)
```
Delete the `sudoFederatorOverride sudoFederator` field (and its doc comment) from the `Server` struct.

- [ ] **Step 3: `pkg/federation/oidc/federation.go` — remove SudoBegin/SudoCallback and the begin() sudo branches**

Delete the `SudoBegin` method and the `SudoCallback` method in full, and the `const maxStepUpAuthAge`. In `begin()`, remove the sudo dispatch case (the `sudoAccountID != nil → flow = flowSudo` branch), the `SudoAccountID`/`ExpectedSub` assignments in the `FedState` literal, the `if sudoAccountID != nil { key = SudoKey(stateToken) }` block, and the `if sudoAccountID != nil { authorizeURL = client.AuthURL(..., StepUpAuthOptions()...) }` block. Also remove the `sudoAccountID`/`expectedSub` parameters from `begin()`'s signature if they exist and are now unused (update the login/link/invite callers accordingly — they will pass fewer args). Compile to find every caller.

- [ ] **Step 4: `pkg/federation/oidc/state.go` — remove the sudo state fields and key**

Delete the `SudoAccountID` field, the `ExpectedSub` field, and the `SudoKey(token string)` function.

- [ ] **Step 5: `pkg/federation/oidc/client.go` — remove `StepUpAuthOptions`**

Delete the `StepUpAuthOptions()` function and its doc comment (it is sudo-only; confirmed no non-sudo callers).

- [ ] **Step 6: `pkg/authn/errors.go` — remove the sudo-only error constructors**

Delete `ErrSudoIdentityMismatch()` and `ErrSudoReauthStale()`.

- [ ] **Step 7: Remove the `ListLinkedEnabledIdPs` sqlc query and regenerate**

Delete the `-- name: ListLinkedEnabledIdPs :many` query block from `db/queries/account_identity.sql`. Regenerate:
```bash
sqlc generate
```
This removes `ListLinkedEnabledIdPs` (const, `ListLinkedEnabledIdPsRow` type, method) from `pkg/db/account_identity.sql.go` and the interface line from `pkg/db/querier.go`. (Do NOT remove `CountUsableSignInFederation` or `ListAccountIdentitiesByAccount`.)

- [ ] **Step 8: Delete the federation-sudo tests**

In `pkg/server/handle_sudo_test.go`: delete the `fakeSudoFederator` struct, the `linkedIdPs` field + `ListLinkedEnabledIdPs` method on the fake queries, and the federation test functions (methods-with-providers, no-federation, begin-redirect, begin-error, callback-success, callback-failure).
In `pkg/federation/oidc/federation_test.go`: delete the `SudoBegin`/`SudoCallback` test functions, the `seedSudoIdentity` helper, and `TestClient_AuthURLStepUpOptions` (it tests the removed `StepUpAuthOptions`).
Find any stragglers:
```bash
grep -rnE "Sudo(Begin|Callback)|StepUpAuthOptions|ListLinkedEnabledIdPs|sudoFederator|ErrSudo(IdentityMismatch|ReauthStale)|federation_oidc.*sudo|SudoKey" --include="*.go" .
```
Expected after edits: no matches in non-deleted code.

- [ ] **Step 9: Build, vet, test**

Run:
```bash
go build -tags nodynamic ./... && go vet ./... && go test ./pkg/server/... ./pkg/federation/... ./pkg/authn/... ./pkg/db/... -v
```
Expected: PASS, no unused-symbol/import errors.

- [ ] **Step 10: Commit**

```bash
git add -A
git commit -m "feat(sudo): remove federation_oidc step-up method (backend + db query)"
```

---

### Task 5: Login-aligned, local-only SudoModal + bounce-to-login (frontend)

**Goal:** Restyle `SudoModal.vue` to mirror the login screen's local section (passkey → OR → inline password+TOTP), remove all federation UI, and redirect upstream-only sessions to `/login` when no local factor is available.

**Files:**
- Modify: `dashboard/src/components/custom/SudoModal.vue` (rewrite)
- Modify: `dashboard/src/lib/sudo.ts` (doc comment only)
- Modify: `dashboard/src/locales/en.ts` (remove federation + toggle + noMethod keys and the two error codes)
- Modify: `dashboard/src/components/custom/SudoModal.test.ts` (remove federation/toggle tests; add bounce + both-methods tests)

**Acceptance Criteria:**
- [ ] No federation affordance renders in the modal.
- [ ] Passkey button + OR divider + inline password+TOTP form render together when both methods are available.
- [ ] With only `password_totp`, the form renders inline (no toggle). With only `webauthn`, only the button.
- [ ] When `/me/sudo/methods` returns neither local method, the modal calls `hardRedirect('/login?return_to=<current>')`.
- [ ] `npm run test` and `npm run build` (tsc) pass.

**Verify:** `cd dashboard && npm run test` → PASS; `npm run build` → typecheck PASS

**Steps:**

- [ ] **Step 1: Rewrite `dashboard/src/components/custom/SudoModal.vue`**

```vue
<script setup lang="ts">
/**
 * SudoModal — the sudo step-up ceremony, mounted ONCE in DashboardLayout;
 * watches the lib/sudo singleton. Opening fetches the account's LOCAL elevation
 * methods and mirrors the login screen's local section: passkey primary, an OR
 * divider, then the password+TOTP form inline. A 204 from /me/sudo/complete
 * resolves the pending withSudo()/ensureSudo() promise.
 *
 * Upstream-login-only accounts have no local factor to re-prove in a modal, so
 * when neither local method is available we redirect to the real /login (which
 * re-runs the upstream flow and re-grants the recent-auth window), returning to
 * the current route. Federation is NOT a step-up factor here — it lives only on
 * the login screen. Reachable only on a stale session: a recent login already
 * satisfies the gate without opening the modal.
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import type { PublicKeyCredentialRequestOptionsJSON } from '@simplewebauthn/browser'
import { api, type ApiError } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useWebauthn } from '@/composables/useWebauthn'
import { sudoState, _resolveSudo } from '@/lib/sudo'
import { hardRedirect } from '@/lib/navigate'
import { ShieldCheck, Fingerprint } from 'lucide-vue-next'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import OrDivider from '@/components/custom/OrDivider.vue'

const { t, te } = useI18n()
const { busy: netBusy, error: netError, run } = useApi()
const { busy: waBusy, error: waError, authenticate } = useWebauthn()
const route = useRoute()

const open = computed({
  get: () => sudoState.value.open,
  set: (v) => { if (!v) _resolveSudo(false) },
})

type SudoMethodsResponse = { methods: string[] }

const methods = ref<string[] | null>(null)
const password = ref('')
const code = ref('')

const busy = computed(() => netBusy.value || waBusy.value)
const error = computed<ApiError | null>(() => netError.value ?? waError.value)
const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})
const hasPasskey = computed(() => methods.value?.includes('webauthn') ?? false)
const hasPwTotp = computed(() => methods.value?.includes('password_totp') ?? false)

watch(() => sudoState.value.open, async (isOpen) => {
  if (!isOpen) return
  methods.value = null
  password.value = ''
  code.value = ''
  netError.value = null
  waError.value = null
  let available: string[] = []
  try {
    const res = await api.get<SudoMethodsResponse>('/api/prohibitorum/me/sudo/methods')
    available = res.methods ?? []
  } catch {
    available = []
  }
  // Upstream-login-only (no local factor): bounce to the real /login, which
  // re-runs the user's auth and re-grants the recent-auth window, then returns.
  if (!available.includes('webauthn') && !available.includes('password_totp')) {
    hardRedirect(`/login?return_to=${encodeURIComponent(route.fullPath)}`)
    return
  }
  methods.value = available
})

async function doPasskey(): Promise<void> {
  const options = await run(() =>
    api.post<PublicKeyCredentialRequestOptionsJSON>('/api/prohibitorum/me/sudo/begin', { method: 'webauthn' }),
  )
  if (!options) return
  const assertion = await authenticate(options)
  if (!assertion) return
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/sudo/complete', assertion)
    return true as const
  })
  if (ok) _resolveSudo(true)
}

async function doPasswordTotp(): Promise<void> {
  const began = await run(async () => {
    await api.post('/api/prohibitorum/me/sudo/begin', { method: 'password_totp' })
    return true as const
  })
  if (!began) return
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/sudo/complete', {
      current_password: password.value,
      totp_code: code.value,
    })
    return true as const
  })
  if (ok) _resolveSudo(true)
}
</script>

<template>
  <Dialog v-model:open="open">
    <!-- z-[60] (above the default z-50 dialogs): sudo is a step-up that can be
         summoned ON TOP of a destructive ConfirmDialog (e.g. unlink, delete),
         so it must layer above any other open dialog to stay operable. -->
    <DialogContent class="z-[60]" overlay-class="z-[60]">
      <DialogHeader>
        <span class="inline-flex size-10 items-center justify-center rounded-full bg-tide/10 text-tide-strong">
          <ShieldCheck class="size-5" aria-hidden="true" />
        </span>
        <DialogTitle>{{ t('sudo.title') }}</DialogTitle>
        <DialogDescription>{{ sudoState.reason || t('sudo.body') }}</DialogDescription>
      </DialogHeader>

      <p v-if="methods === null" class="text-sm text-muted">{{ t('common.loading') }}</p>

      <div v-else class="flex flex-col gap-4">
        <Button v-if="hasPasskey" size="lg" class="w-full" :disabled="busy" @click="doPasskey">
          <Fingerprint aria-hidden="true" />
          {{ t('sudo.passkeyButton') }}
        </Button>

        <OrDivider v-if="hasPasskey && hasPwTotp" :label="t('login.orDivider')" />

        <form v-if="hasPwTotp" class="flex flex-col gap-3" @submit.prevent="doPasswordTotp">
          <div class="flex flex-col gap-1.5">
            <Label for="sudo-password">{{ t('sudo.passwordLabel') }}</Label>
            <Input id="sudo-password" v-model="password" name="current_password" type="password"
                   autocomplete="current-password" required />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="sudo-code">{{ t('sudo.codeLabel') }}</Label>
            <Input id="sudo-code" v-model="code" name="totp_code" inputmode="numeric"
                   autocomplete="one-time-code" required />
          </div>
          <Button type="submit" class="w-full" :disabled="busy">{{ t('sudo.verify') }}</Button>
        </form>

        <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
          <AlertDescription>{{ errorText }}</AlertDescription>
        </Alert>
      </div>

      <DialogFooter>
        <Button variant="ghost" :disabled="busy" @click="_resolveSudo(false)">{{ t('sudo.cancel') }}</Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
```

- [ ] **Step 2: Update the doc comment in `dashboard/src/lib/sudo.ts`**

Change the contract note (lines ~3-8) so it no longer says "one-shot":
```ts
/**
 * Sudo step-up gate (singleton). The SudoModal — mounted once in
 * DashboardLayout — watches `sudoState`; withSudo()/ensureSudo() open it and
 * await the user's ceremony. Backend contract: sensitive /me actions return
 * {code:'sudo_required'} until the session is within the recent-auth window
 * (granted at login) or holds a fresh step-up grant. The grant is multi-use
 * until it expires.
 */
```

- [ ] **Step 3: Trim `dashboard/src/locales/en.ts`**

In the `sudo:` block, remove the keys: `usePassword`, `usePasskeyInstead`, `reauthWith`, `reauthHint`, and `noMethod` (no longer referenced — the modal bounces to /login instead). Keep `title`, `passkeyButton`, `passwordLabel`, `codeLabel`, `verify`, `cancel`, `body`, and the whole `reason` map.
In the `errors:` block, remove `sudo_identity_mismatch` and `sudo_reauth_stale`.
(The modal now references `login.orDivider`, which already exists.)

- [ ] **Step 4: Update `dashboard/src/components/custom/SudoModal.test.ts`**

Delete these obsolete tests: "shows a terminal message when no method is available" (uses `en.sudo.noMethod`), "use passkey instead button appears…", "use passkey instead does not appear…", and "renders a provider button for each federation provider…".
Keep and confirm still pass: passkey path, cancel, password+TOTP path, custom reason, fallback body.
Add these two tests inside the `describe('SudoModal', …)` block:
```ts
  it('bounces upstream-only sessions to /login when no local factor', async () => {
    get.mockResolvedValue({ methods: [] })
    hardRedirect.mockReset()
    mountModal()
    sudoState.value = { open: true, resolve: vi.fn() }
    await flushPromises()
    expect(hardRedirect).toHaveBeenCalledWith('/login?return_to=%2Fdashboard')
  })

  it('shows passkey button, OR divider, and inline password form when both methods exist', async () => {
    get.mockResolvedValue({ methods: ['webauthn', 'password_totp'] })
    mountModal()
    sudoState.value = { open: true, resolve: vi.fn() }
    await flushPromises()
    const passkeyBtn = Array.from(document.querySelectorAll('button'))
      .find(b => b.textContent?.includes(en.sudo.passkeyButton))
    expect(passkeyBtn).toBeTruthy()
    // password form is inline (no toggle click needed)
    expect(document.querySelector('input[name=current_password]')).toBeTruthy()
    expect(document.querySelector('input[name=totp_code]')).toBeTruthy()
  })
```
(The existing "password+TOTP path resolves true" test mounts with only `password_totp`; the form now renders inline so that test passes unchanged — just remove its stale `// showPwForm should be auto-true` comment.)

- [ ] **Step 5: Verify frontend**

Run: `cd dashboard && npm run test`
Expected: PASS
Run: `cd dashboard && npm run build`
Expected: typecheck + bundle succeed (this also regenerates `pkg/webui/dist` — committed in Task 7).

- [ ] **Step 6: Commit (source only; dist rebuilt in Task 7)**

```bash
git add dashboard/src/components/custom/SudoModal.vue dashboard/src/lib/sudo.ts \
        dashboard/src/locales/en.ts dashboard/src/components/custom/SudoModal.test.ts
git commit -m "feat(web): login-aligned local-only sudo modal + bounce-to-login"
```

---

### Task 6: Update the smoke test for the new step-up behavior

**Goal:** Remove the federation-sudo smoke section and flip the one-shot assertions to multi-use so `cmd/smoke` reflects the recent-auth window.

**Files:**
- Modify: `cmd/smoke/main.go`

**Acceptance Criteria:**
- [ ] No smoke step exercises `/me/sudo/federation/*` or `federation_oidc` step-up.
- [ ] Smoke asserts that a single elevation covers MULTIPLE gated actions (multi-use), not one-shot re-gate.
- [ ] The smoke summary banner and any audit-row count expectations are updated to match.
- [ ] Smoke passes end-to-end.

**Verify:** Run `cmd/smoke` against a throwaway Postgres (see memory `live-smoke-without-podman`): `go run ./cmd/smoke` → "smoke OK".

**Steps:**

- [ ] **Step 1: Delete the fed-sudo section**

Find and delete the "fed-sudo 1-7" block in `cmd/smoke/main.go` (the section that exercises `/me/sudo/methods` federation providers → `/me/sudo/begin` federation → upstream re-auth → `/me/sudo/federation/callback`). Locate it:
```bash
grep -n "fed-sudo\|sudo/federation\|federation_oidc" cmd/smoke/main.go
```
Remove the whole block and any helper used only by it.

- [ ] **Step 2: Flip the one-shot assertions to multi-use**

Find the one-shot assertions:
```bash
grep -n "one-shot\|OneShot\|sudo_required\|re-gate\|verifySudoGrantIsOneShot" cmd/smoke/main.go
```
Where the smoke currently asserts "a second sudo-gated call must 401 sudo_required (one-shot consumed)", change it to assert the SECOND gated call SUCCEEDS within the window (multi-use). Update or delete `verifySudoGrantIsOneShot` accordingly (rename to `verifySudoGrantIsMultiUse` and invert the expectation). Also update inline comments (e.g. lines ~252, ~2845, ~3750) that describe re-asserting before EACH mutation — under the window, one elevation covers subsequent mutations until expiry.

- [ ] **Step 3: Fix the summary banner and audit-row expectations**

Update the final `fmt.Println("✓ smoke OK …")` banner string to drop the "fed-sudo 1-7 (federated OIDC sudo step-up…)" clause and renumber as needed. Adjust any audit-event lower-bound counts that expected `federation_oidc` sudo rows (search for `federation_oidc` count assertions) so they reflect login/link/invite federation only.

- [ ] **Step 4: Run smoke**

Per memory `live-smoke-without-podman`, bring up a throwaway cluster and run:
```bash
go run ./cmd/smoke
```
Expected: prints "✓ smoke OK". Fix any assertion that still expects one-shot behavior or federation sudo.

- [ ] **Step 5: Commit**

```bash
git add cmd/smoke/main.go
git commit -m "test(smoke): drop federation step-up, assert multi-use sudo window"
```

---

### Task 7: Rebuild the embedded SPA + full verification

**Goal:** Regenerate the committed `pkg/webui/dist` from the updated frontend and run the full CI gates so the binary embeds the new modal.

**Files:**
- Modify: `pkg/webui/dist/**` (build artifact)

**Acceptance Criteria:**
- [ ] `pkg/webui/dist` is a fresh build of the current `dashboard/src` (no stale-dist drift).
- [ ] `go build -tags nodynamic ./...` + `go test ./...` pass.
- [ ] `dashboard` vitest + typecheck pass.

**Verify:** `mise run ci:go && mise run ci:frontend` → PASS (ci:frontend asserts the committed dist matches a fresh build).

**Steps:**

- [ ] **Step 1: Rebuild the SPA into the embed dir**

```bash
cd dashboard && npm ci && npm run build
```
This writes the bundle to `pkg/webui/dist` (vite outDir, embedded via `go:embed all:dist`).

- [ ] **Step 2: Full Go gate**

```bash
go build -tags nodynamic ./... && go test ./...
```
Expected: PASS.

- [ ] **Step 3: Full frontend gate + dist-drift check**

```bash
mise run ci:frontend
```
Expected: PASS (install + vitest + typecheck, then asserts `pkg/webui/dist` matches a fresh build — i.e. you committed the rebuild).

- [ ] **Step 4: Commit the rebuilt dist**

```bash
git add pkg/webui/dist
git commit -m "build(webui): rebuild SPA for the login-aligned sudo modal"
```

---

## Self-Review

**Spec coverage:**
- Recent-auth window (grant at login + multi-use) → Task 2 (+ TTL default Task 1). ✓
- Login-aligned local-only modal + bounce-to-login → Task 5. ✓
- Federation removed from sudo surface (backend) → Task 4. ✓
- Federation removed from modal → Task 5. ✓
- Gate trim "posture & high-impact only" (exact keep/drop) → Task 3. ✓
- Error-model cleanup (`sudo_identity_mismatch`, `sudo_reauth_stale`) → Task 4 (backend) + Task 5 (en.ts). ✓
- Self-service gates kept (in-handler `requireFreshSudo` inherits the window) → Task 2 (no registration change). ✓
- Smoke + SPA rebuild + config doc → Tasks 6, 7, 1. ✓
- Security tradeoffs are inherent to Task 2's behavior (documented in spec). ✓

**Open spec items (carry into review, not blockers):** SAML parity (SAML create/update/reingest/**delete** dropped vs OIDC kept) and the 15m default — both already chosen; revisit during code review if desired.

**Placeholder scan:** No TBD/TODO. Deletion steps key off symbols + grep verification rather than fragile line numbers (the agent-reported lines may have drifted); each says "verify exact lines."

**Type consistency:** `hasFreshSudo(sess)` (new name) is used consistently in `requireFreshSudo` and the `registerSudoOp` middleware; `consumeFreshSudo` is fully removed. `/me/sudo/methods` response shape `{methods}` is consistent between backend (Task 4) and the frontend `SudoMethodsResponse` type (Task 5). Registration-helper swaps (`registerSudoOp`→`registerOp`, `s.registerSudoOpHTTP`→`registerOpHTTP`) match the real signatures in `operations.go`.
