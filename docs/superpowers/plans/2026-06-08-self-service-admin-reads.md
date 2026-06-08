# Self-service & Admin Reads (Tier 1) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make already-shipped UI stateful + close two admin-editing gaps: `PUT /me` (self displayName), `GET /me/factors`, admin `GET /accounts/{id}/sessions` + per-session revoke, SAML PUT `attribute_map`/`name_id_claim` (+ alias-bug fix), and an admin account attributes editor.

**Architecture:** Backend Go (huma typed ops for reads/updates; one `registerSudoOpHTTP` raw handler for the admin per-session revoke) + the Vue frontend that consumes each endpoint. No DB migration — all columns + most queries already exist. One small new sqlc query (extended `UpdateSAMLSP`).

**Tech Stack:** Go (huma, pgx, sqlc), Vue 3 + Vite + shadcn-vue + vue-i18n, vitest.

**Cross-cutting (every task):**
- Repo root `/home/tundra/projects/tundra/prohibitorum`. Commit per task; `master`, no remote. End commit messages with `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- **`go build ./... && go vet ./...` exit 0 is authoritative** over stale gopls (Task 6 runs `mise exec -- sqlc generate` → gopls will false-positive "undefined" until reload; ignore it).
- **gofmt is repo-wide pre-existing-dirty** — keep your edited region gofmt-clean (`gofmt -d <file>` should show no diff in your lines); do NOT whole-file reformat `cmd/smoke/main.go`, `pkg/server/handle_me.go`, `pkg/server/handle_account.go`, etc.
- After any `dashboard/src/locales/en.ts` edit, run the apostrophe guard: `grep -nP "\x{2018}" dashboard/src/locales/en.ts ; grep -nP ":\s*\x{2019}" dashboard/src/locales/en.ts` — both must print nothing.
- FE verify: `cd dashboard && npx vitest run <file>` + `npx vue-tsc --noEmit`. Backend verify: `go test ./... && go build ./... && go vet ./...`.
- **Dist:** do NOT rebuild per-FE-task; rebuild + commit `pkg/webui/dist` ONCE at the finishing gate (Task 9). Discard incidental dist dirt with `git checkout -- pkg/webui/dist`.
- Contract `Operation*` literals use a RELATIVE `Path` (e.g. `/me`, `/accounts/{id}/credentials`); the `/api/prohibitorum` prefix is added by the API group. `registerOp` auto-injects session/admin security.

---

### Task 1: `PUT /me` — self-service displayName

**Goal:** A session-authed `PUT /me` that updates ONLY the caller's display name.

**Files:**
- Modify: `pkg/contract/auth.go` (new `OperationUpdateMe`)
- Modify: `pkg/server/handle_me.go` (new `handleUpdateMe`)
- Modify: `pkg/server/server.go` (register the op in the `/me` block)
- Test: `pkg/server/handle_me_test.go` (or the existing me-test file; create if absent)

**Acceptance Criteria:**
- [ ] `PUT /api/prohibitorum/me {displayName}` validates via `account.ValidateDisplayName`, calls `UpdateAccountDisplayName`, returns the updated `SessionView`.
- [ ] Only `display_name` changes — role/attributes/disabled/username untouched.
- [ ] Invalid displayName → `invalid_display_name` (the existing AuthError). No sudo.

**Verify:** `go test ./pkg/server/... && go build ./... && go vet ./...`

**Steps:**

- [ ] **Step 1: Contract op.** In `pkg/contract/auth.go`, near `OperationGetMe`, add:
```go
var OperationUpdateMe = huma.Operation{
	OperationID: "updateMe",
	Method:      http.MethodPut,
	Path:        "/me",
	Summary:     "Update the caller's own profile (display name only).",
}
```

- [ ] **Step 2: Handler.** In `pkg/server/handle_me.go`, add (the `meOut` type already exists from `handleGetMe`):
```go
type updateMeIn struct {
	Body struct {
		DisplayName string `json:"displayName"`
	}
}

func (s *Server) handleUpdateMe(ctx context.Context, in *updateMeIn) (*meOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	if err := account.ValidateDisplayName(in.Body.DisplayName); err != nil {
		return nil, authErrToHuma(err)
	}
	if err := s.queries.UpdateAccountDisplayName(ctx, db.UpdateAccountDisplayNameParams{
		ID:          sess.Account.ID,
		DisplayName: in.Body.DisplayName,
	}); err != nil {
		return nil, fmt.Errorf("handleUpdateMe: %w", err)
	}
	// Reflect the change in the response without a re-fetch: the session's
	// account snapshot is updated in place for the returned view.
	sess.Account.DisplayName = in.Body.DisplayName
	return &meOut{Body: sessionView(sess.Account)}, nil
}
```
Ensure imports include `prohibitorum/pkg/account` and `prohibitorum/pkg/db` (handle_me.go already imports `db`; add `account` if missing — check the import block).

- [ ] **Step 3: Register.** In `pkg/server/server.go` `/me` block (after `OperationGetMe`):
```go
registerOp(mgmt, contract.OperationUpdateMe, s.handleUpdateMe, sessionReq)
```

- [ ] **Step 4: Test** in `pkg/server/handle_me_test.go`. Follow the existing server-test harness (grep an existing `handle_me`/`handleGetMe` test for how a `*Server` + session context is built; many server tests use a fake-queries or a test pool). At minimum, a focused test of the validation + that the query is called with display-name only:
```go
func TestHandleUpdateMe_ValidatesAndUpdatesDisplayNameOnly(t *testing.T) {
	// Build a Server with a fake/stub queries that records UpdateAccountDisplayName
	// calls and a session context for account ID 1 (mirror the setup used by other
	// handle_me tests — e.g. authn.ContextWithSession). Assert:
	//  - valid displayName "New Name" → no error, returned Body.DisplayName == "New Name",
	//    and the recorded UpdateAccountDisplayNameParams == {ID:1, DisplayName:"New Name"}.
	//  - displayName "" (or >128 / control char) → authErr with code invalid_display_name,
	//    and UpdateAccountDisplayName was NOT called.
}
```
Implement it concretely against whatever harness the sibling tests use. If no `*Server` unit harness exists for `/me`, add the coverage at the smoke layer instead (Task 9) and note it.

- [ ] **Step 5: Commit.** `git add pkg/contract/auth.go pkg/server/handle_me.go pkg/server/server.go pkg/server/handle_me_test.go` → `git commit -m "feat(server): PUT /me self-service display-name update"` (+ trailer).

---

### Task 2: `GET /me/factors` — factor status

**Goal:** A session-authed read returning the caller's factor status for the Security page.

**Files:**
- Modify: `pkg/contract/auth.go` (`MeFactorsView` + `OperationGetMyFactors`)
- Modify: `pkg/server/handle_me.go` (`handleGetMyFactors`)
- Modify: `pkg/server/server.go` (register)
- Test: `pkg/server/handle_me_test.go`

**Acceptance Criteria:**
- [ ] `GET /api/prohibitorum/me/factors` → `{passwordSet, totpEnrolled, recoveryCodesRemaining, passkeyCount}` (no sudo).
- [ ] `passwordSet` = password row exists; `totpEnrolled` = TOTP row with `ConfirmedAt.Valid`; `recoveryCodesRemaining` = count of unused codes; `passkeyCount` = `CountCredentialsByAccount`.

**Verify:** `go test ./pkg/server/... && go build ./...`

**Steps:**

- [ ] **Step 1: Contract.** In `pkg/contract/auth.go`:
```go
type MeFactorsView struct {
	PasswordSet            bool `json:"passwordSet"`
	TOTPEnrolled           bool `json:"totpEnrolled"`
	RecoveryCodesRemaining int  `json:"recoveryCodesRemaining"`
	PasskeyCount           int  `json:"passkeyCount"`
}

var OperationGetMyFactors = huma.Operation{
	OperationID: "getMyFactors",
	Method:      http.MethodGet,
	Path:        "/me/factors",
	Summary:     "Return the caller's enrolled sign-in factor status.",
}
```

- [ ] **Step 2: Handler** in `pkg/server/handle_me.go`:
```go
type meFactorsOut struct {
	Body contract.MeFactorsView
}

func (s *Server) handleGetMyFactors(ctx context.Context, _ *struct{}) (*meFactorsOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	id := sess.Account.ID
	v := contract.MeFactorsView{}

	if _, err := s.queries.GetPasswordCredential(ctx, id); err == nil {
		v.PasswordSet = true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("handleGetMyFactors: password: %w", err)
	}

	if totp, err := s.queries.GetTOTPCredential(ctx, id); err == nil {
		v.TOTPEnrolled = totp.ConfirmedAt.Valid
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("handleGetMyFactors: totp: %w", err)
	}

	codes, err := s.queries.ListRecoveryCodesByAccount(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("handleGetMyFactors: recovery: %w", err)
	}
	v.RecoveryCodesRemaining = len(codes)

	n, err := s.queries.CountCredentialsByAccount(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("handleGetMyFactors: passkeys: %w", err)
	}
	v.PasskeyCount = int(n)

	return &meFactorsOut{Body: v}, nil
}
```
Ensure `errors` + `github.com/jackc/pgx/v5` are imported in handle_me.go (check the block; add if missing).

- [ ] **Step 3: Register** in server.go `/me` block: `registerOp(mgmt, contract.OperationGetMyFactors, s.handleGetMyFactors, sessionReq)`.

- [ ] **Step 4: Test** — `TestHandleGetMyFactors`: with a fake queries returning (password present, TOTP confirmed, 3 unused recovery rows, 2 credentials) → `{true,true,3,2}`; and the empty case (ErrNoRows for password+totp, 0 codes, 0 creds) → `{false,false,0,0}`. Mirror the harness used by the Task-1 test.

- [ ] **Step 5: Commit** → `feat(server): GET /me/factors (password/TOTP/recovery/passkey status)`.

---

### Task 3: FE — Profile displayName edit + Security factor badges

**Goal:** `ProfileView` lets the user edit their displayName via `PUT /me`; `SecurityView` shows factor-status badges from `GET /me/factors`.

**Files:**
- Modify: `dashboard/src/pages/ProfileView.vue`
- Modify: `dashboard/src/pages/SecurityView.vue`
- Modify: `dashboard/src/pages/security/PasswordCard.vue`, `TotpCard.vue`, `RecoveryCodesCard.vue` (accept a status prop/badge)
- Modify: `dashboard/src/stores/auth.ts` (a setter to patch `me.displayName`, if not already mutable)
- Modify: `dashboard/src/locales/en.ts` (new keys)
- Test: `dashboard/src/pages/ProfileView.test.ts`, `dashboard/src/pages/SecurityView.test.ts` (create/extend)

**Acceptance Criteria:**
- [ ] ProfileView: an Edit control turns displayName into an input + Save/Cancel; Save calls `PUT /api/prohibitorum/me {displayName}` via `useApi`, patches the auth store from the response, exits edit mode; validation error surfaces via `errors.<code>`/Alert.
- [ ] SecurityView: on mount fetches `/me/factors`; Password card shows "Password set"/"Not set", TOTP shows "Active"/"Not set", Recovery shows "{n} codes remaining"; a refetch after a card mutation updates badges.
- [ ] Username + role remain read-only.

**Verify:** `cd dashboard && npx vitest run src/pages/ProfileView.test.ts src/pages/SecurityView.test.ts && npx vue-tsc --noEmit`

**Steps:**
- [ ] **Step 1:** Read the current `ProfileView.vue`, `SecurityView.vue`, and the three card components to match their style/props.
- [ ] **Step 2: ProfileView** — add `useApi` + an `editing` ref + a `draft` ref seeded from `auth.me.displayName`. Render displayName as text with an "Edit" button; in edit mode an `<Input>` + Save/Cancel. `save()` → `run(() => api.put('/api/prohibitorum/me', { displayName: draft.value }))`; on success call the auth store setter (e.g. `auth.setDisplayName(res.displayName)` — add a small action to `stores/auth.ts` that mutates `me.displayName`, or `auth.me!.displayName = res.displayName` if the store exposes `me` mutably) and set `editing=false`. Surface `errorText` (the standard `errors.<code>` computed) in an `Alert`. data-test hooks: `profile-edit`, `profile-displayName-input`, `profile-save`, `profile-cancel`.
- [ ] **Step 3: Security factors** — in `SecurityView.vue`, add `useApi` + a `factors` ref; `onMounted` → `run(() => api.get<MeFactors>('/api/prohibitorum/me/factors'))` (define a local `MeFactors` interface `{passwordSet, totpEnrolled, recoveryCodesRemaining, passkeyCount}`). Pass status into the cards as props: e.g. `<PasswordCard :set="factors?.passwordSet" />`, `<TotpCard :enrolled="factors?.totpEnrolled" />`, `<RecoveryCodesCard :remaining="factors?.recoveryCodesRemaining" />`. Each card renders a `StatusBadge` (success when set/enrolled/`remaining>0`, neutral otherwise) near its title using the new prop; keep all existing card behavior. Expose a `reloadFactors()` and call it after a successful mutation in each card (simplest: have the cards `emit('changed')` on success and SecurityView re-fetch; or re-fetch on a shared event — pick the lightest wiring consistent with the cards).
- [ ] **Step 4: i18n** — add `profile.edit/save/cancel`, `security.factors.passwordSet/passwordUnset/totpActive/totpInactive/recoveryRemaining` (use a count param) under the appropriate namespaces. Run the apostrophe guard.
- [ ] **Step 5: Tests** — ProfileView: edit→save calls PUT with the draft + patches the store + exits edit; validation error shows. SecurityView: mounts → GET /me/factors → asserts the three badges render the expected text for a given factors payload. Mock `@/lib/api` + `@/lib/sudo` per the established test style.
- [ ] **Step 6: Commit** → `feat(web): profile display-name edit + security factor-status badges`.

---

### Task 4: Admin `GET /accounts/{id}/sessions` + per-session revoke

**Goal:** Admin can list a target account's sessions and revoke one (admin+sudo).

**Files:**
- Modify: `pkg/contract/auth.go` (`OperationListAccountSessions`)
- Modify: `pkg/server/handle_account.go` (`handleListAccountSessions` + raw `handleRevokeAccountSessionHTTP`)
- Modify: `pkg/server/server.go` (register both)
- Test: `pkg/server/handle_account_test.go`

**Acceptance Criteria:**
- [ ] `GET /api/prohibitorum/accounts/{id}/sessions` (admin) → `[]SessionListItem` via `sessionStore.ListByAccount(id)`, `isCurrent` always false; 404 `account_not_found` if the account doesn't exist.
- [ ] `POST /api/prohibitorum/accounts/{id}/sessions/revoke {sessionId}` (admin + fresh sudo) → `RevokeBySessionID(id, sessionId)`; `ok==false` → `session_not_found`. Logs like the bulk revoke.

**Verify:** `go test ./pkg/server/... && go build ./...`

**Steps:**

- [ ] **Step 1: Contract op** (list is typed; the revoke is a raw sudo handler so no Operation literal needed):
```go
var OperationListAccountSessions = huma.Operation{
	OperationID: "listAccountSessions",
	Method:      http.MethodGet,
	Path:        "/accounts/{id}/sessions",
	Summary:     "List an account's active sessions (admin only).",
}
```

- [ ] **Step 2: List handler** in `handle_account.go` (reuses `getAccountIn` which is `{ ID int32 \`path:"id"\` }`):
```go
type accountSessionsOut struct {
	Body []contract.SessionListItem
}

func (s *Server) handleListAccountSessions(ctx context.Context, in *getAccountIn) (*accountSessionsOut, error) {
	if _, err := s.queries.GetAccountByID(ctx, in.ID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, authErrToHuma(authn.ErrAccountNotFound())
		}
		return nil, fmt.Errorf("handleListAccountSessions: load: %w", err)
	}
	records, err := s.sessionStore.ListByAccount(ctx, in.ID)
	if err != nil {
		return nil, fmt.Errorf("handleListAccountSessions: list: %w", err)
	}
	items := make([]contract.SessionListItem, 0, len(records))
	for _, r := range records {
		items = append(items, contract.SessionListItem{
			ID:         r.Data.SessionID,
			IsCurrent:  false, // admin viewing another account
			IssuedAt:   r.Data.IssuedAt,
			ExpiresAt:  r.Data.ExpiresAt,
			LastSeenIP: r.Data.LastSeenIP,
			UserAgent:  r.Data.UserAgent,
		})
	}
	return &accountSessionsOut{Body: items}, nil
}
```

- [ ] **Step 3: Per-session revoke (raw, sudo).** Add a raw handler (mirrors the SAML raw handlers' chi.URLParam + JSON decode style):
```go
func (s *Server) handleRevokeAccountSessionHTTP(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id64, err := strconv.ParseInt(idStr, 10, 32)
	if err != nil {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	accountID := int32(id64)
	var body struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SessionID == "" {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	if _, err := s.queries.GetAccountByID(r.Context(), accountID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAuthErr(w, authn.ErrAccountNotFound())
			return
		}
		writeAuthErr(w, fmt.Errorf("handleRevokeAccountSession: load: %w", err))
		return
	}
	ok, err := s.sessionStore.RevokeBySessionID(r.Context(), accountID, body.SessionID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleRevokeAccountSession: %w", err))
		return
	}
	if !ok {
		writeAuthErr(w, authn.ErrSessionNotFound())
		return
	}
	sess := authn.SessionFromContext(r.Context())
	actorID := int32(0)
	if sess != nil {
		actorID = sess.Account.ID
	}
	logx.WithContext(r.Context()).WithFields(logrus.Fields{
		"event":          "auth.session_revoked_admin",
		"actor_id":       actorID,
		"target_id":      accountID,
		"target_session": body.SessionID,
	}).Info("auth")
	w.WriteHeader(http.StatusNoContent)
}
```
Confirm imports: `chi`, `strconv`, `encoding/json`, `logx`, `logrus` are already used in this package (chi via the SAML handlers, the rest in handle_account.go). Add any missing.

- [ ] **Step 4: Register** in server.go admin block:
```go
registerOp(mgmt, contract.OperationListAccountSessions, s.handleListAccountSessions, admin)
s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/accounts/{id}/sessions/revoke", admin, s.handleRevokeAccountSessionHTTP)
```

- [ ] **Step 5: Test** — `TestHandleListAccountSessions` (admin; not-found 404; maps records→items with isCurrent=false), and the revoke path (not-found session → session_not_found). Use the existing handle_account test harness. The sudo gate + chi routing are integration-covered; the unit test can call the handlers directly with a stub sessionStore (check how other tests stub `s.sessionStore`).

- [ ] **Step 6: Commit** → `feat(server): admin GET /accounts/{id}/sessions + per-session revoke`.

---

### Task 5: FE — admin per-session table + row revoke

**Goal:** `AdminAccountDetailView` shows a per-session table with a per-row revoke.

**Files:**
- Modify: `dashboard/src/pages/admin/AdminAccountDetailView.vue`
- Modify: `dashboard/src/locales/en.ts`
- Test: `dashboard/src/pages/admin/AdminAccountDetailView.test.ts`

**Acceptance Criteria:**
- [ ] On load, fetches `GET /accounts/{id}/sessions`; renders a table (time via `lib/time`, IP, UA) of the account's sessions.
- [ ] Per-row Revoke → `withSudo(POST /accounts/{id}/sessions/revoke {sessionId})`, then re-fetches the list; the existing bulk "revoke all" stays.
- [ ] Empty list → an empty-state line.

**Verify:** `cd dashboard && npx vitest run src/pages/admin/AdminAccountDetailView.test.ts && npx vue-tsc --noEmit`

**Steps:**
- [ ] **Step 1:** Read the current `AdminAccountDetailView.vue` (esp. the sessions card ~lines 183-189) + `dashboard/src/pages/SessionsView.vue` (reuse its row layout/`lib/time` usage + the `SessionListItem` shape `{id,isCurrent,issuedAt,expiresAt,lastSeenIp,userAgent}`).
- [ ] **Step 2:** Add a `sessions` ref + `loadSessions()` (`GET /api/prohibitorum/accounts/${id}/sessions`) called on mount. Render a `Table` of rows; per row a Revoke `Button` → `withSudo(() => api.post('/api/prohibitorum/accounts/'+id+'/sessions/revoke', { sessionId: row.id }))` then `loadSessions()`. Keep the existing bulk revoke button (which should also `loadSessions()` after). `errors.session_not_found` surfaces via the existing `errorText`/Alert. data-test: `session-row-{id}`, `session-revoke-{id}`.
- [ ] **Step 3:** i18n keys (`admin.accounts.sessions.*`: title, colTime/colIp/colUa, revoke, empty). Apostrophe guard.
- [ ] **Step 4:** Tests — mounts → GET sessions → rows render; row revoke → POST with sessionId + reload; empty state. Mock api + sudo.
- [ ] **Step 5: Commit** → `feat(web): admin per-account session list + row revoke`.

---

### Task 6: SAML PUT `attribute_map` / `name_id_claim` + alias-bug fix

**Goal:** The SAML SP update persists `name_id_claim` + `attribute_map`, the view exposes them, and the `authn_requests_signed` aliasing bug is fixed.

**Files:**
- Modify: `db/queries/saml_sp.sql` (+ `mise exec -- sqlc generate`)
- Modify: `pkg/server/handle_admin_saml_sps.go` (`updateSAMLProviderBody`, the PUT handler, `samlProviderView`)
- Modify: `pkg/contract/auth.go` (`SAMLProviderView` += `NameIDClaim`, `AttributeMap`)
- Test: `pkg/server/handle_admin_saml_sps_test.go`

**Acceptance Criteria:**
- [ ] `UpdateSAMLSP` SET clause includes `name_id_claim` + `attribute_map`, and **`authn_requests_signed` is removed from the SET** (no longer clobbered by the `require_signed_authn_request` param).
- [ ] PUT body accepts `nameIdClaim` (string) + `attributeMap` (raw JSON, validated as JSON); `SAMLProviderView` + `samlProviderView()` expose both; `GET /{id}` returns them.
- [ ] Updating only `requireSignedAuthnRequest` leaves `authn_requests_signed` unchanged.

**Verify:** `go test ./pkg/server/... && go build ./... && go vet ./...`

**Steps:**

- [ ] **Step 1: Query.** In `db/queries/saml_sp.sql`, change `UpdateSAMLSP` to:
```sql
-- name: UpdateSAMLSP :one
UPDATE saml_sp SET
  display_name                 = $2,
  name_id_format               = $3,
  require_signed_authn_request = $4,
  want_assertions_signed       = $5,
  allow_idp_initiated          = $6,
  session_lifetime             = $7,
  name_id_claim                = $8,
  attribute_map                = $9
WHERE id = $1
RETURNING *;
```
(Removed the `authn_requests_signed = $4` line — it was clobbering a distinct column with no PUT field. It now retains its create/reingest value.) Run `mise exec -- sqlc generate`. Confirm `UpdateSAMLSPParams` gains `NameIDClaim string` + `AttributeMap []byte` and DROPS nothing else.

- [ ] **Step 2: Contract.** In `pkg/contract/auth.go` `SAMLProviderView`, add after `NameIDFormat`:
```go
	NameIDClaim  string          `json:"nameIdClaim"`
	AttributeMap json.RawMessage `json:"attributeMap"`
```
(Import `encoding/json` in auth.go if not present — or use `[]byte` with a json tag; `json.RawMessage` serializes the stored jsonb verbatim, which is what we want.)

- [ ] **Step 3: View projection.** In `samlProviderView()` (handle_admin_saml_sps.go ~lines 48-87), populate the two new fields from `sp.NameIDClaim` and `sp.AttributeMap` (`AttributeMap: json.RawMessage(sp.AttributeMap)`; if `sp.AttributeMap` is empty, emit `json.RawMessage("[]")`).

- [ ] **Step 4: PUT body + handler.** Extend `updateSAMLProviderBody`:
```go
	NameIDClaim  string          `json:"nameIdClaim"`
	AttributeMap json.RawMessage `json:"attributeMap"`
```
In `handleUpdateSAMLProviderHTTP`, after decoding: if `body.AttributeMap` is non-empty, validate it parses (`var js any; json.Unmarshal(body.AttributeMap, &js)` → on error `writeAuthErr(w, authn.ErrBadRequest()); return`); default empty to `[]`. Add to the `UpdateSAMLSPParams`:
```go
		NameIDClaim:  body.NameIDClaim,
		AttributeMap: attrMapBytes, // body.AttributeMap or []byte("[]")
```

- [ ] **Step 5: Test** — `TestUpdateSAMLSP_PersistsAttrMapAndNameIDClaim`: create an SP, PUT with `nameIdClaim:"email"` + `attributeMap:[{...}]`, GET → both reflected. And `TestUpdateSAMLSP_DoesNotClobberAuthnRequestsSigned`: create an SP with `authn_requests_signed=true` (via insert/metadata), PUT changing only `requireSignedAuthnRequest`, assert the row's `authn_requests_signed` is still true. Use the existing SAML SP test harness (real test pool or fake — match siblings). Invalid `attributeMap` JSON → 400.

- [ ] **Step 6: Commit** → `fix(saml): PUT persists attribute_map/name_id_claim; stop clobbering authn_requests_signed`.

---

### Task 7: FE — SAML provider name_id_claim + attribute_map fields

**Goal:** `AdminSamlProviderDetailView` edits `name_id_claim` (text) + `attribute_map` (JSON textarea).

**Files:**
- Modify: `dashboard/src/pages/admin/AdminSamlProviderDetailView.vue`
- Modify: `dashboard/src/locales/en.ts`
- Test: `dashboard/src/pages/admin/AdminSamlProviderDetailView.test.ts`

**Acceptance Criteria:**
- [ ] The config form has a `nameIdClaim` text input and an `attributeMap` `<Textarea>` (JSON), both seeded from the loaded provider.
- [ ] Save sends `nameIdClaim` + `attributeMap` in the PUT body; `attributeMap` is client-side JSON-validated before send (invalid → inline message, no request).

**Verify:** `cd dashboard && npx vitest run src/pages/admin/AdminSamlProviderDetailView.test.ts && npx vue-tsc --noEmit`

**Steps:**
- [ ] **Step 1:** Read the current `AdminSamlProviderDetailView.vue` (the local `SamlProvider` interface + the config card + `save()` body).
- [ ] **Step 2:** Extend the `SamlProvider` interface with `nameIdClaim: string` + `attributeMap: unknown` (the GET returns parsed JSON). Add a `nameIdClaim` ref + an `attributeMapText` ref (seed `attributeMapText` = `JSON.stringify(provider.attributeMap ?? [], null, 2)`). Render a text `<Input>` for nameIdClaim and a `<Textarea>` for attributeMapText. In `save()`: parse `attributeMapText` (try/catch → on error set an inline error and return without calling the API); send `nameIdClaim` + `attributeMap: parsed` in the PUT body.
- [ ] **Step 3:** i18n keys (`admin.saml.nameIdClaim`, `admin.saml.attributeMap`, `admin.saml.attributeMapInvalid`, hint). Apostrophe guard.
- [ ] **Step 4:** Tests — fields seed from the provider; save sends both; invalid JSON in the textarea → inline error + no PUT. Mock api + sudo.
- [ ] **Step 5: Commit** → `feat(web): SAML provider name_id_claim + attribute_map editing`.

---

### Task 8: FE — admin account attributes editor

**Goal:** `AdminAccountDetailView` edits account attributes via a key/value row editor (backend already replaces attributes via `PUT /accounts/{id}`).

**Files:**
- Modify: `dashboard/src/pages/admin/AdminAccountDetailView.vue`
- Modify: `dashboard/src/locales/en.ts`
- Test: `dashboard/src/pages/admin/AdminAccountDetailView.test.ts`

**Acceptance Criteria:**
- [ ] Attributes render as editable key/value rows (add/remove); Save includes the rebuilt `attributes` object in the existing `PUT /accounts/{id}` body.
- [ ] Existing string attributes round-trip; a non-string existing value is shown read-only / as JSON (not silently stringified).

**Verify:** `cd dashboard && npx vitest run src/pages/admin/AdminAccountDetailView.test.ts && npx vue-tsc --noEmit`

**Steps:**
- [ ] **Step 1:** Read the current `AdminAccountDetailView.vue` attributes handling (it currently round-trips `attributes` read-only in the PUT — find where the PUT body is built and where attributes display).
- [ ] **Step 2:** Replace the read-only attributes display with an editor: a list of `{key, value}` string rows derived from the loaded account's `attributes` (string-valued entries become editable rows; non-string values, if any, are listed read-only as JSON with a note). An "Add attribute" button appends an empty row; each row has a remove button. On Save, build `attributes` = the string rows assembled into an object (skip empty keys), MERGED with any preserved non-string entries, and include it in the existing `PUT /accounts/{id}` body (the handler replaces attributes wholesale).
- [ ] **Step 3:** i18n (`admin.accounts.attributes.*`: title, key, value, add, remove, complexNote). Apostrophe guard.
- [ ] **Step 4:** Tests — edit a value + add a row + save → PUT body `attributes` reflects the edits; remove a row drops it. Mock api + sudo.
- [ ] **Step 5: Commit** → `feat(web): admin account attributes editor`.

---

### Finishing (Task 9 / coordinator)
1. Per-task spec + quality review (two-stage) as work proceeds.
2. Final whole-cycle review (opus): cross-cutting — PUT /me really displayName-only; admin sessions/SAML correctly admin-gated; no en.ts apostrophe corruption; FE badge/edit wiring sound.
3. **Done-gate (repo root, all GREEN):** `go build ./... && go vet ./...` exit 0; `go test ./...`; `cd dashboard && npx vue-tsc --noEmit && npx vitest run`; `cmd/smoke` SMOKE_EXIT=0 (add cheap assertions for `PUT /me`, `GET /me/factors`, admin sessions list if they fit — note the corrupted-then-rebuilt smoke PG cluster from the prior cycle is healthy now). `cd dashboard && npm run build && cd .. && git add pkg/webui/dist && git commit` (rebuild dist ONCE here).
4. Memory + handoff.

## Self-review (against the spec)
- **Spec coverage:** §1 PUT /me → T1+T3; §2 GET /me/factors → T2+T3; §3 admin sessions+revoke → T4+T5; §4 SAML attr_map/name_id_claim+alias → T6+T7; §5 admin attributes editor → T8. All covered.
- **No placeholders in backend tasks** (full code). FE tasks give exact endpoints/shapes/data-test/i18n + instruct reading the current component first (the executing session reads these files) — consistent with how prior cycles' FE tasks were specified.
- **Type consistency:** `MeFactorsView{passwordSet,totpEnrolled,recoveryCodesRemaining,passkeyCount}` used identically in T2 (Go) and T3 (FE interface). `SessionListItem` reused for admin sessions (T4/T5). `OperationUpdateMe/GetMyFactors/ListAccountSessions` defined in T1/T2/T4. SAML view fields `nameIdClaim`/`attributeMap` consistent across T6 (Go) + T7 (FE).
- **Decisions honored:** PUT /me no sudo + displayName-only; per-session revoke admin+sudo; attributeMap as JSON textarea; factors include passkeyCount; admin attribute editing FE-only.
