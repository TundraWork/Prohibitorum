# Login-resume (server-validated return_to) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the one remaining client-only `return_to` path (the login ceremony) by adding a shared server-side validator and returning a server-validated redirect the SPA follows — mirroring the existing consent flow.

**Architecture:** One `validateReturnTo` (Go, superset of the two existing narrow validators) becomes the single source of truth; consent + federation converge onto it; the WebAuthn + password login-complete handlers validate `return_to` and return `LoginResult{Redirect}`; the SPA `hardRedirect`s the blessed value (like `ConsentView`). `safeReturnTo` stays as client defense-in-depth (federation + already-authenticated). Spec: `docs/superpowers/specs/2026-06-17-login-resume-redesign.md`.

**Tech Stack:** Go (chi handlers, `pkg/configx`, `net/url`), Vue 3 SPA (vitest), the two-instance federation lab (`/tmp/federation-lab.sh`).

---

### Task 1: Shared `validateReturnTo` server validator + converge call sites

**Goal:** One server-side validator accepting a same-origin relative path OR absolute URL, normalising to a safe relative path; consent + federation use it.

**Files:**
- Create: `pkg/server/returnto.go`, `pkg/server/returnto_test.go`
- Modify: `pkg/server/handle_consent.go` (replace `sameOriginAsIssuer` use at ~:80, remove the now-dead helper at :150), `pkg/server/handle_federation.go` (replace `validateFederationReturnTo` body/use at :61,:202)

**Acceptance Criteria:**
- [ ] `validateReturnTo(raw, cfg) string` returns a same-origin **relative** path or `/`.
- [ ] Test matrix passes (mirror `dashboard/src/lib/returnTo.test.ts`): `""`/`/me/security` (relative kept), `Issuer+"/oauth/authorize?x=1"` → `/oauth/authorize?x=1` (absolute same-origin → relative, query preserved), `https://evil.test/x` → `/`, `//evil.test` → `/`, `javascript:alert(1)` → `/`, `data:text/html,x` → `/`, `/\evil.test` → `/`, cross-scheme same-host → `/`.
- [ ] Consent + federation call sites compile and their existing tests still pass.

**Verify:** `go test ./pkg/server/ -run "ReturnTo|Consent|Federation" -count=1`

**Steps:**
- [ ] **Step 1 (TDD):** Write `returnto_test.go` with the matrix above (table-driven; build `cfg` with `OIDC.Issuer="https://idp.example"` and test `https://idp.example/...` for the same-origin-absolute case). Run → fails (undefined).
- [ ] **Step 2:** Implement `validateReturnTo(raw string, cfg *configx.Config) string` in `returnto.go`: empty or `//`-prefixed → `/`; `url.Parse` raw + issuer; if `u.IsAbs()` require `u.Scheme==iss.Scheme && u.Host==iss.Host`, else require a path-absolute relative ref (`u.Host==""`, raw starts with single `/`); build `EscapedPath()+"?"+RawQuery+"#"+Fragment`; if the result isn't a single-slash path → `/`. (This is the Go twin of the corrected client `safeReturnTo`.)
- [ ] **Step 3:** Converge: in `handle_consent.go`, replace `if !sameOriginAsIssuer(rt, s.config)` logic with `rt := validateReturnTo(r.URL.Query().Get("return_to"), s.config)` and use `rt` (it is always safe; the deny branch is unchanged). Delete `sameOriginAsIssuer`. In `handle_federation.go`, make `validateReturnTo` the implementation behind the federation call site (`s.validateReturnTo`-style or call the package func); the federation flow now also accepts a same-origin absolute `return_to` normalised to relative.
- [ ] **Step 4:** Run the verify command → PASS (incl. existing consent/federation tests).
- [ ] **Step 5: Commit** `feat(auth): shared server-side validateReturnTo; converge consent + federation`

---

### Task 2: Login ceremony returns a server-validated redirect

**Goal:** WebAuthn `login/complete` and the password-login completion read `return_to`, validate it server-side, and return `LoginResult{Redirect}`.

**Files:**
- Modify: `pkg/contract/auth.go` (add `LoginResult`), `pkg/server/handle_auth.go` (`handleLoginCompleteHTTP`, ~:314 response), `pkg/server/handle_auth_password.go` (TOTP/password completion, ~:303 session issue → response)
- Test: `pkg/server/handle_auth_test.go` / `handle_auth_password_test.go`

**Acceptance Criteria:**
- [ ] `contract.LoginResult{ Redirect string \`json:"redirect"\` }` added (mirrors `ConsentResult`).
- [ ] WebAuthn `login/complete` returns `LoginResult{Redirect: validateReturnTo(<req return_to>, cfg)}` (e.g. `redirect:"/oauth/authorize?…"` for a same-origin absolute input; `"/"` default).
- [ ] Password completion returns the same `LoginResult{Redirect}`.
- [ ] Both read `return_to` from the request (query param, consistent with `POST /consent?return_to=`).
- [ ] Existing login tests updated for the new response body; all green.

**Verify:** `go test ./pkg/server/ -run "Login|Password|Totp" -count=1`

**Steps:**
- [ ] **Step 1:** Add `LoginResult` to `pkg/contract/auth.go` next to `ConsentResult`.
- [ ] **Step 2 (TDD):** Update/add handler tests asserting the JSON body carries `redirect` = the validated `return_to` (one same-origin-absolute case → relative path; one cross-origin → `/`). Run → fails.
- [ ] **Step 3:** In `handleLoginCompleteHTTP` (`handle_auth.go`), after the session cookie is set (~:305), replace the `json.NewEncoder(w).Encode(s.sessionView(...))` (~:314) with `LoginResult{Redirect: validateReturnTo(r.URL.Query().Get("return_to"), s.config)}`. (Confirm the SPA does not depend on the sessionView from this response — it re-fetches `/me`; if it does, fold redirect into the existing body instead.)
- [ ] **Step 4:** Apply the same in the password/TOTP completion (`handle_auth_password.go` ~:303) where it issues the session and writes its success response.
- [ ] **Step 5:** Run verify → PASS. Re-run any flaky case isolated (`-count=1`).
- [ ] **Step 6: Commit** `feat(auth): login/complete returns a server-validated redirect (LoginResult)`

---

### Task 3: SPA follows the server redirect (mirror ConsentView)

**Goal:** The login ceremony sends `return_to` and navigates to the server's `redirect`; the success path stops using client `safeReturnTo`.

**Files:**
- Modify: `dashboard/src/components/custom/PasskeyButton.vue`, `dashboard/src/components/custom/PasswordTotpForm.vue` (send `return_to`, surface `redirect`), `dashboard/src/pages/LoginView.vue` (`onSuccess(redirect)` → `hardRedirect(redirect)`)
- Test: the corresponding `*.test.ts` + `LoginView.test.ts`

**Acceptance Criteria:**
- [ ] `PasskeyButton`/`PasswordTotpForm` append the URL's `return_to` to the `login/complete` request and `emit('success', res.redirect)`.
- [ ] `LoginView.onSuccess(redirect)` calls `hardRedirect(redirect)` (from `@/lib/navigate`) — the server-blessed value, NOT `goReturnTo()`.
- [ ] The already-authenticated on-mount branch (existing) still uses `goReturnTo()` (defense-in-depth; unchanged).
- [ ] Tests: ceremony success navigates to the server `redirect`; already-authenticated still redirects.

**Verify:** `cd dashboard && npx vitest run src/pages/LoginView.test.ts src/components/custom/PasskeyButton.test.ts src/components/custom/PasswordTotpForm.test.ts`

**Steps:**
- [ ] **Step 1:** Read `ConsentView.vue` (`hardRedirect(res.redirect)`) as the template. In `PasskeyButton.vue`, change the `login/complete` POST to include `?return_to=<encodeURIComponent(route.query.return_to)>` (or pass via body), change `defineEmits<{ success: [] }>()` → `{ success: [redirect: string] }`, and `emit('success', res.redirect)`. Same in `PasswordTotpForm.vue`.
- [ ] **Step 2:** In `LoginView.vue`, `onSuccess(redirect: string)` → `hardRedirect(redirect)`. Keep the `onMounted` already-authenticated branch calling `goReturnTo()`.
- [ ] **Step 3 (tests):** Update `LoginView.test.ts` (the goReturnTo mock → assert `hardRedirect`/navigation to the server redirect on ceremony success; keep the already-authenticated test). Update the button tests for the new emit signature.
- [ ] **Step 4:** `npm test` (full FE suite) green; `npx vue-tsc -b` clean.
- [ ] **Step 5: Commit** `feat(login): SPA follows the server-validated redirect (mirror consent)`

---

### Task 4: Verify end-to-end + rebuild dist

**Goal:** Full gate green and the two-instance federation flow works through a real passkey login.

**Files:** Modify: `pkg/webui/dist` (rebuilt)

**Acceptance Criteria:**
- [ ] `go build -tags nodynamic ./... && go vet ./... && go test ./...` green.
- [ ] `cd dashboard && npm test && npm run build` green; `pkg/webui/dist` rebuilt + committed.
- [ ] Federation lab: after a passkey login on UP, the browser resumes to the pending `/oauth/authorize` → code → DOWN callback (not stranded on the dashboard).

**Verify:** the gate commands above + a lab run (`bash /tmp/federation-lab.sh`, then the documented browser steps; confirm the post-login resume via the server `redirect`).

**Steps:**
- [ ] **Step 1:** Run the Go gate; fix any fallout.
- [ ] **Step 2:** `npm test` + `npm run build`; `git add pkg/webui/dist`.
- [ ] **Step 3:** Re-run the federation lab; verify `login/complete` returns the `/oauth/authorize?…` redirect and the flow completes. (Interactive passkey leg is manual.)
- [ ] **Step 4: Commit** `build(webui): rebuild SPA for server-validated login redirect`
