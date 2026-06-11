# OIDC-based sudo step-up — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let any user with a linked, enabled upstream OIDC identity satisfy the sudo gate by force-re-authenticating to that upstream (`prompt=login` + `max_age=0`, fresh `auth_time` verified), unblocking federated-only users who currently have no sudo method.

**Architecture:** Reuse the hardened federation client + `FedState` machinery, adding a third "sudo" flow alongside login/link. A redirect ceremony: `/me/sudo/begin {method:"federation_oidc"}` returns an authorize URL → upstream re-auth → `GET /me/sudo/federation/callback` verifies identity-match + freshness and stamps `SudoUntil`. `webauthn`/`password_totp` sudo are unchanged.

**Tech Stack:** Go (chi, zitadel/oidc rp, sqlc/pgx), Vue 3 SPA, the in-process mock OP in `cmd/smoke/mockop`.

Spec: `docs/superpowers/specs/2026-06-12-oidc-sudo-step-up-design.md`.

---

## File structure

- `pkg/federation/oidc/client.go` — add `AuthTime` to `Tokens`; extend `AuthURL` with step-up params.
- `pkg/federation/oidc/state.go` — `FedState.SudoAccountID`, `FedState.ExpectedSub`, `SudoKey()`.
- `pkg/federation/oidc/federation.go` — `Federator.SudoBegin` / `Federator.SudoCallback`.
- `pkg/authn/errors.go` — `ErrSudoIdentityMismatch`, `ErrSudoReauthStale`.
- `pkg/server/handle_sudo.go` — `availableSudoMethods` (surface federation), `/me/sudo/methods` providers, `/me/sudo/begin` federation branch, the callback handler.
- `pkg/server/server.go` — register `GET /api/prohibitorum/me/sudo/federation/callback`.
- `dashboard/src/components/custom/SudoModal.vue`, `dashboard/src/lib/sudo.ts`, `dashboard/src/locales/en.ts` — FE redirect branch.
- `cmd/smoke/mockop/server.go`, `cmd/smoke/main.go` — mock-OP step-up support + smoke arc.

---

### Task 1: Federation client — expose `auth_time` + step-up authorize params

**Goal:** The code-exchange result carries the upstream's `auth_time`, and `AuthURL` can request a forced re-auth.

**Files:**
- Modify: `pkg/federation/oidc/client.go`
- Test: `pkg/federation/oidc/client_test.go` (create if absent) or `pkg/federation/oidc/federation_test.go`

**Acceptance Criteria:**
- [ ] `Tokens` has an `AuthTime time.Time` field, populated from `claims.AuthTime`.
- [ ] `AuthURL` accepts extra `oauth2.AuthCodeOption`s; passing the step-up options yields a URL containing `prompt=login` and `max_age=0`.

**Verify:** `go test ./pkg/federation/oidc/ -run 'AuthURL|AuthTime' -v` → PASS

**Steps:**

- [ ] **Step 1: Failing test for the step-up AuthURL params.** Add to `federation_test.go`:

```go
func TestClient_AuthURL_StepUpParams(t *testing.T) {
	fx := newFixture(t, federationoidc.ModeAutoProvision)
	// fx exposes the *Client via the federator; if not, build one with the same
	// discovery as newFixture. Use the fixture's client accessor.
	u := fx.client().AuthURL("st", "no", "ch", federationoidc.StepUpAuthOptions()...)
	parsed, _ := url.Parse(u)
	q := parsed.Query()
	if q.Get("prompt") != "login" {
		t.Errorf("prompt = %q, want login", q.Get("prompt"))
	}
	if q.Get("max_age") != "0" {
		t.Errorf("max_age = %q, want 0", q.Get("max_age"))
	}
}
```

(If the fixture has no client accessor, add `func (fx *fixture) client() *federationoidc.Client { return fx.f.ClientForTest() }` plus a small `ClientForTest` test helper, or construct the client directly mirroring `newFixture`. Keep it minimal.)

- [ ] **Step 2: Run → FAIL** (`StepUpAuthOptions` undefined). `go test ./pkg/federation/oidc/ -run AuthURL_StepUp -v`

- [ ] **Step 3: Extend `AuthURL` + add `StepUpAuthOptions`.** In `client.go`:

```go
// AuthURL builds the upstream authorization URL. Extra options let callers
// request a forced re-authentication for step-up (see StepUpAuthOptions).
func (c *Client) AuthURL(state, nonce, codeChallenge string, extra ...oauth2.AuthCodeOption) string {
	opts := []rp.AuthURLOpt{
		rp.WithCodeChallenge(codeChallenge),
		authURLOpt(oauth2.SetAuthURLParam("nonce", nonce)),
	}
	for _, o := range extra {
		opts = append(opts, authURLOpt(o))
	}
	return rp.AuthURL(state, c.rp, opts...)
}

// StepUpAuthOptions forces a fresh upstream re-authentication: prompt=login
// re-challenges the user, and max_age=0 obliges a conformant OP to both
// re-auth AND return a fresh auth_time (which the sudo callback verifies).
func StepUpAuthOptions() []oauth2.AuthCodeOption {
	return []oauth2.AuthCodeOption{
		oauth2.SetAuthURLParam("prompt", "login"),
		oauth2.SetAuthURLParam("max_age", "0"),
	}
}
```

- [ ] **Step 4: Add `AuthTime` to `Tokens` + populate it.** In the `Tokens` struct add `AuthTime time.Time`. Where the wrapper builds the returned `*Tokens` from `claims` (after the iss/nonce/alg re-checks, ~client.go:275+), set `AuthTime: claims.AuthTime.AsTime()` (zitadel `oidc.Time` → `time.Time`). Confirm the exact accessor against the vendored type; if it's a plain field, use `time.Time(claims.AuthTime)`.

- [ ] **Step 5: Failing test for AuthTime propagation** — extend an existing happy-path exchange test (or `TestFederator_HandleCallback_HappyPath_AutoProvision`) to assert the mock OP's `auth_time` reaches `Tokens.AuthTime`. If the mock doesn't emit `auth_time` yet, this assertion waits for Task 7; for now assert the field exists and is zero-valued when absent:

```go
// in an existing exchange test, after obtaining tokens:
// (auth_time wiring is exercised end-to-end in the smoke once the mock emits it)
```

- [ ] **Step 6: Run → PASS.** `go test ./pkg/federation/oidc/ -run 'AuthURL|HappyPath' -v`

- [ ] **Step 7: Commit.**

```bash
git add pkg/federation/oidc/client.go pkg/federation/oidc/federation_test.go
git commit -m "feat(federation): expose id_token auth_time + step-up authorize params"
```

---

### Task 2: `FedState` sudo flow + `Federator.SudoBegin`

**Goal:** A sudo-purpose federation flow can be started: state bound to the session account + the target linked identity, stashed single-use, returning a forced-re-auth authorize URL.

**Files:**
- Modify: `pkg/federation/oidc/state.go` (fields + `SudoKey`)
- Modify: `pkg/federation/oidc/federation.go` (`SudoBegin`; `begin()` step-up + browser-binding for sudo)
- Test: `pkg/federation/oidc/federation_test.go`

**Acceptance Criteria:**
- [ ] `FedState` has `SudoAccountID *int32` and `ExpectedSub string`; `SudoKey(token)` returns `"oidc:fed:sudo:" + token`.
- [ ] `SudoBegin(ctx, accountID, slug, returnTo)` rejects a slug the account hasn't linked (or whose provider is disabled) with `ErrUnknownIDP`/`ErrInviteRequired`-style error, and on success stashes a `FedState` under `SudoKey` carrying `SudoAccountID`, `ExpectedSub` (the linked identity's upstream subject), `ReturnTo`, and a non-empty `BrowserBinding`, with a `prompt=login`/`max_age=0` authorize URL.

**Verify:** `go test ./pkg/federation/oidc/ -run SudoBegin -v` → PASS

**Steps:**

- [ ] **Step 1: Add state fields + key.** In `state.go`, add to `FedState`:

```go
	// SudoAccountID, when non-nil, marks this as a sudo step-up flow: the
	// callback must (a) run against this same authenticated account and
	// (b) resolve the upstream identity to ExpectedSub. No local account is
	// created or mutated — the flow only stamps a fresh sudo grant.
	SudoAccountID *int32 `json:"sudo_account_id,omitempty"`
	// ExpectedSub is the upstream subject of the linked identity the user must
	// re-authenticate as. A re-auth resolving to any other subject is rejected.
	ExpectedSub string `json:"expected_sub,omitempty"`
```

And below `LinkKey`:

```go
// SudoKey returns the KV key under which sudo-flow state lives — a third
// namespace distinct from login/link so a token minted for one purpose can
// never be Pop'd by another (scope-confusion defense).
func SudoKey(token string) string {
	return "oidc:fed:sudo:" + token
}
```

- [ ] **Step 2: Failing test.** Add `TestFederator_SudoBegin_BindsStateAndForcesReauth` to `federation_test.go`: seed an `account_identity` linking account 7 to the `mockop` provider with a known `sub`, call `fx.f.SudoBegin(ctx, 7, "mockop", "/security")`, then decode the `SudoKey` state and assert `SudoAccountID==7`, `ExpectedSub==<sub>`, `BrowserBinding != ""`, and the returned `AuthorizeURL` query has `prompt=login`/`max_age=0`. Mirror the seeding in `TestFederator_HandleCallback_HappyPath_ExistingIdentity` (it already sets up a linked identity).

- [ ] **Step 3: Run → FAIL** (`SudoBegin` undefined).

- [ ] **Step 4: Implement `SudoBegin` + thread step-up through `begin()`.** Add to `federation.go`:

```go
// SudoBegin starts a forced-re-auth federation flow for the sudo step-up. The
// account must already have a linked, enabled identity at idpSlug; the callback
// will require the re-auth to resolve back to that same upstream subject.
func (f *Federator) SudoBegin(ctx context.Context, accountID int32, idpSlug, returnTo string) (*LoginRequest, error) {
	idp, err := f.q.GetUpstreamIDPBySlug(ctx, idpSlug) // excludes disabled
	if err != nil {
		return nil, ErrUnknownIDP
	}
	ident, err := f.q.GetAccountIdentity(ctx, db.GetAccountIdentityParams{
		AccountID:    accountID,
		UpstreamIdpID: idp.ID,
	})
	if err != nil {
		return nil, ErrSudoMethodUnavailable // not linked to this provider
	}
	acct := accountID
	return f.begin(ctx, idpSlug, returnTo, beginOpts{
		sudoAccountID: &acct,
		expectedSub:   ident.UpstreamSubject,
	})
}
```

Refactor `begin()` to take a small `beginOpts` struct instead of growing positional args (the existing callers `BeginLogin`/`LinkBegin`/`BeginInviteRedemption` pass their current values via the struct). `beginOpts` fields: `linkingAccountID *int32`, `enrollmentToken string`, `sudoAccountID *int32`, `expectedSub string`. Inside `begin()`:
- choose the KV key: `LinkKey` when linking, `SudoKey` when `sudoAccountID != nil`, else `LoginKey`;
- populate `BrowserBinding` for login/invite/**sudo** (still empty for link — OIDCFED-1);
- when `sudoAccountID != nil`, pass `StepUpAuthOptions()...` into `client.AuthURL(...)` and set `state.SudoAccountID`/`state.ExpectedSub`.

Use `GetAccountIdentity` (a by-(account,idp) lookup returning `UpstreamSubject`). If that exact query doesn't exist, add it to `db/queries/account_identity.sql` and `sqlc generate` (mirror `ListAccountIdentitiesByAccount`); the implementer verifies the generated method name.

- [ ] **Step 5: Run → PASS.** `go test ./pkg/federation/oidc/ -run 'SudoBegin|HappyPath|LinkBegin|StashesState' -v` (the refactor must keep login/link tests green).

- [ ] **Step 6: Commit.**

```bash
git add pkg/federation/oidc/state.go pkg/federation/oidc/federation.go pkg/federation/oidc/federation_test.go db/queries/account_identity.sql pkg/db
git commit -m "feat(federation): SudoBegin — forced-re-auth step-up flow bound to a linked identity"
```

---

### Task 3: `Federator.SudoCallback` + new sudo error codes

**Goal:** The callback verifies single-use state, browser binding, id_token, **identity match**, and **auth_time freshness**, returning success only for a genuine same-account re-auth.

**Files:**
- Modify: `pkg/federation/oidc/federation.go` (`SudoCallback`, `maxStepUpAuthAge`)
- Modify: `pkg/authn/errors.go` (`ErrSudoIdentityMismatch`, `ErrSudoReauthStale`)
- Test: `pkg/federation/oidc/federation_test.go`

**Acceptance Criteria:**
- [ ] `SudoCallback(ctx, stateToken, code, issParam, browserToken string, currentAccountID int32) error` returns nil only when: state pops from `SudoKey`, browser binding matches, id_token verifies, resolved `sub == state.ExpectedSub`, `state.SudoAccountID == currentAccountID`, and `now - tokens.AuthTime <= maxStepUpAuthAge`.
- [ ] Wrong subject → `ErrSudoIdentityMismatch`; stale/absent `auth_time` → `ErrSudoReauthStale`; bad state/binding/token → `ErrFederationStateInvalid`.

**Verify:** `go test ./pkg/federation/oidc/ -run SudoCallback -v` → PASS

**Steps:**

- [ ] **Step 1: Add error codes.** In `pkg/authn/errors.go`, mirroring `ErrSudoMethodUnavailable` (errors.go:283-287):

```go
// ErrSudoIdentityMismatch — the upstream re-auth resolved to a different
// account/subject than the one being elevated. 401-class; no grant.
func ErrSudoIdentityMismatch() *AuthError {
	return newErr(http.StatusUnauthorized, "sudo_identity_mismatch", "重新验证的身份与当前账户不匹配")
}

// ErrSudoReauthStale — the upstream did not return a fresh auth_time, so the
// re-authentication cannot be proven recent. 401-class; no grant.
func ErrSudoReauthStale() *AuthError {
	return newErr(http.StatusUnauthorized, "sudo_reauth_stale", "上游未确认本次重新验证，请重试")
}
```

- [ ] **Step 2: Failing tests.** Add to `federation_test.go`, seeding a sudo `FedState` under `SudoKey` (reuse Task 2's seeding) and driving `SudoCallback`:
  - `TestFederator_SudoCallback_HappyPath` — mock returns the linked sub + fresh `auth_time`; assert `err == nil`.
  - `TestFederator_SudoCallback_RejectsWrongSubject` — mock returns a different sub; assert `errors.Is(err, ...)` maps to `ErrSudoIdentityMismatch` (compare via the AuthError code).
  - `TestFederator_SudoCallback_RejectsStaleAuthTime` — mock returns `auth_time` older than `maxStepUpAuthAge`; assert `ErrSudoReauthStale`.
  - `TestFederator_SudoCallback_RejectsAccountMismatch` — `currentAccountID` ≠ `state.SudoAccountID`; assert `ErrFederationStateInvalid`.

  (The mock OP gains `auth_time`/`prompt` support in Task 7; until then, drive these via the existing test exchange seam the federation_test fakes already use for HandleCallback, setting the claims directly.)

- [ ] **Step 3: Run → FAIL.**

- [ ] **Step 4: Implement `SudoCallback`.** Add to `federation.go` (model the state-pop + browser-binding + code-exchange on `LinkCallback`/`HandleCallback`, then the sudo-specific checks):

```go
const maxStepUpAuthAge = 120 * time.Second

func (f *Federator) SudoCallback(ctx context.Context, stateToken, code, issParam, browserToken string, currentAccountID int32) error {
	blob, err := f.kvStore.Pop(ctx, SudoKey(stateToken)) // single-use
	if err != nil {
		return ErrFederationStateInvalid
	}
	state, err := DecodeFedState(blob)
	if err != nil {
		return ErrFederationStateInvalid
	}
	if !browserBindingOK(state.BrowserBinding, browserToken) {
		return ErrFederationStateInvalid
	}
	if state.SudoAccountID == nil || *state.SudoAccountID != currentAccountID {
		return ErrFederationStateInvalid
	}
	// RFC 9207 iss + per-flow expected_iss / token_endpoint re-check, mirroring
	// HandleCallback (use the same helper used there).
	client, err := f.buildClient(ctx, /* idp resolved from state */, false)
	if err != nil {
		return ErrFederationStateInvalid
	}
	tokens, err := client.Exchange(ctx, code, state.CodeVerifier, state.Nonce, state.ExpectedIss /*, issParam check */)
	if err != nil {
		return ErrFederationStateInvalid
	}
	if tokens.Subject != state.ExpectedSub {
		return authnErr(ErrSudoIdentityMismatch())
	}
	if tokens.AuthTime.IsZero() || time.Since(tokens.AuthTime) > maxStepUpAuthAge {
		return authnErr(ErrSudoReauthStale())
	}
	return nil
}
```

Match the real signatures of `buildClient` / `client.Exchange` / `browserBindingOK` and the iss/token-endpoint mix-up re-check as used by `HandleCallback` (read it: `federation.go` ~line 314-360). The state must also carry `IDPID`/`IDPSlug`/`ExpectedIss`/`ExpectedTokenEndpoint` (it already does from `begin()`), so `buildClient` can be reconstructed. Return raw `*authn.AuthError` values directly (they implement `error`); the HTTP layer maps them.

- [ ] **Step 5: Run → PASS.** `go test ./pkg/federation/oidc/ ./pkg/authn/ -run 'SudoCallback|Sudo' -v`

- [ ] **Step 6: Commit.**

```bash
git add pkg/federation/oidc/federation.go pkg/authn/errors.go pkg/federation/oidc/federation_test.go
git commit -m "feat(federation): SudoCallback — identity-match + auth_time freshness for step-up"
```

---

### Task 4: Surface `federation_oidc` in sudo methods + provider list

**Goal:** `availableSudoMethods` stops dropping federation, and `/me/sudo/methods` returns the caller's linked + enabled providers.

**Files:**
- Modify: `pkg/server/handle_sudo.go` (`availableSudoMethods`, `handleSudoMethodsHTTP`)
- Test: `pkg/server/handle_sudo_test.go`

**Acceptance Criteria:**
- [ ] `availableSudoMethods` includes `"federation_oidc"` when `AvailableMethods` reports it.
- [ ] `/me/sudo/methods` returns `{"methods":[...],"federationProviders":[{"slug","displayName"}]}`, listing only linked + non-disabled providers; empty when none.

**Verify:** `go test ./pkg/server/ -run SudoMethods -v` → PASS

**Steps:**

- [ ] **Step 1: Failing test.** In `handle_sudo_test.go`, using the existing sudo test Server harness + `fakeSudoQueries`, seed a linked+enabled federation identity and assert the methods response contains `federation_oidc` and a `federationProviders` entry; seed none and assert it's absent/empty. (Extend `fakeSudoQueries` with the provider-list query if needed.)

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement.** In `availableSudoMethods` (handle_sudo.go:92-99), replace the webauthn/password_totp-only filter with: keep all of `MethodWebAuthn`, `MethodPasswordTOTP`, `MethodFederationOIDC`. Update the package/function doc comment — federation IS now a sudo factor *because* OIDC sudo forces a fresh re-auth (`prompt=login`+`max_age=0`, `auth_time` verified). Then enrich `handleSudoMethodsHTTP` to also fetch linked+enabled providers and include `federationProviders`:

```go
type sudoProvider struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
}
// in handleSudoMethodsHTTP, after computing methods:
providers := s.linkedEnabledProviders(r.Context(), sess.Account.ID) // []sudoProvider, never nil
_ = json.NewEncoder(w).Encode(map[string]any{"methods": methods, "federationProviders": providers})
```

Implement `linkedEnabledProviders` via `ListAccountIdentitiesByAccount` (it already joins enabled `upstream_idp` — confirm it returns slug + display name + a disabled flag; filter to enabled). Add the query method to the `sudoFlowQueries` interface (handle_sudo.go:64) and to the fakes.

- [ ] **Step 4: Run → PASS.** `go test ./pkg/server/ -run 'Sudo' -v`

- [ ] **Step 5: Commit.**

```bash
git add pkg/server/handle_sudo.go pkg/server/handle_sudo_test.go
git commit -m "feat(sudo): surface federation_oidc + linked-provider list in /me/sudo/methods"
```

---

### Task 5: `/me/sudo/begin` federation branch + callback route

**Goal:** Begin returns a redirect for `federation_oidc`, and a new authenticated callback route stamps sudo on success.

**Files:**
- Modify: `pkg/server/handle_sudo.go` (begin branch + `handleSudoFederationCallbackHTTP`)
- Modify: `pkg/server/server.go` (register the callback route)
- Test: `pkg/server/handle_sudo_test.go`

**Acceptance Criteria:**
- [ ] `POST /me/sudo/begin {method:"federation_oidc","slug","returnTo"}` returns `{"redirect": "<authorizeURL>"}` and sets the fed-state cookie.
- [ ] `GET /api/prohibitorum/me/sudo/federation/callback` (sessionReq) calls `SudoCallback` with the caller's account id; on success stamps `SudoUntil` and 302s to the validated `returnTo`; on failure 302s to `returnTo` with an error marker (or `/error`), never stamping.

**Verify:** `go test ./pkg/server/ -run 'SudoFederation' -v` → PASS

**Steps:**

- [ ] **Step 1: Failing tests.** In `handle_sudo_test.go`: (a) begin with `method:"federation_oidc"` + a seeded linked provider returns 200 with a `redirect` field (inject a fake federator via a new `s.federator` test seam, or assert it delegates to `SudoBegin`); (b) the callback handler with no fresh-sudo session + a stubbed `SudoCallback` success path stamps `SudoUntil` (assert via the session store) and 302s to `returnTo`.

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement the begin branch.** In `handleSudoBeginHTTP`, extend the request body to `{Method, Slug, ReturnTo string}` and add a case:

```go
case string(authn.MethodFederationOIDC):
	req, err := s.federator.SudoBegin(r.Context(), sess.Account.ID, body.Slug, body.ReturnTo)
	if err != nil {
		writeAuthErr(w, err) // ErrSudoMethodUnavailable / ErrUnknownIDP
		return
	}
	http.SetCookie(w, sessstore.FedStateCookie(s.config, r, req.AntiForgeryToken))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"redirect": req.AuthorizeURL})
```

Note: the federation branch does NOT use the `sudo_intent`/`/me/sudo/complete` path (that stays for webauthn/password_totp). Skip the intent SetEx for this method (move the intent stash into the webauthn/password_totp branches, or guard it).

- [ ] **Step 4: Implement the callback handler.** Add `handleSudoFederationCallbackHTTP`:

```go
func (s *Server) handleSudoFederationCallbackHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	q := r.URL.Query()
	var browserToken string
	if c, err := r.Cookie(sessstore.FedStateCookieName); err == nil {
		browserToken = c.Value
	}
	err := s.federator.SudoCallback(r.Context(), q.Get("state"), q.Get("code"), q.Get("iss"), browserToken, sess.Account.ID)
	returnTo := safeRelativePath(q.Get("return_to")) // server-side same-origin guard; default "/"
	if err != nil {
		// audit auth.sudo_failed{method:federation_oidc, reason}; do NOT stamp.
		http.Redirect(w, r, returnTo+"?sudo=failed", http.StatusFound)
		return
	}
	s.stampSudoUntil(w, r, sess, string(authn.MethodFederationOIDC)) // stamps + audits + 204
	// stampSudoUntil writes 204; for the redirect ceremony we instead 302 to returnTo.
}
```

`stampSudoUntil` currently writes 204 — for this redirect ceremony you need a 302 instead. Extract the stamp+audit core into a helper that does NOT write the response (e.g. `applySudoGrant(ctx,w,r,sess,method) error`), have the existing `stampSudoUntil` call it then write 204, and have the callback call it then `http.Redirect(...returnTo, 302)`. Add `safeRelativePath` (reject absolute/scheme/`//` — same rule the FE `safeReturnTo` enforces) or reuse an existing server-side helper if present.

`returnTo` must survive the upstream round-trip: `SudoBegin` already stored it in `FedState.ReturnTo`; have `SudoCallback` return it (change its signature to `(string, error)`) so the handler doesn't trust the query param. Prefer the state-carried `returnTo` over the query.

- [ ] **Step 5: Register the route.** In `server.go` near the other `/me/sudo/*` registrations:

```go
registerOpHTTP(s.router, "GET", "/api/prohibitorum/me/sudo/federation/callback", sessionReq, s.handleSudoFederationCallbackHTTP)
```

- [ ] **Step 6: Run → PASS.** `go test ./pkg/server/ -run 'Sudo' -v && go build ./...`

- [ ] **Step 7: Commit.**

```bash
git add pkg/server/handle_sudo.go pkg/server/server.go pkg/server/handle_sudo_test.go
git commit -m "feat(sudo): /me/sudo/begin federation redirect + authenticated step-up callback"
```

---

### Task 6: Frontend — OIDC re-auth option in the sudo modal

**Goal:** The sudo modal offers "Re-authenticate with {provider}" for federated users; clicking begins the flow and redirects to the upstream.

**Files:**
- Modify: `dashboard/src/components/custom/SudoModal.vue`
- Modify: `dashboard/src/locales/en.ts` (`sudo.*` strings + the new error codes)
- Test: `dashboard/src/lib/sudo.test.ts` (or a SudoModal test)

**Acceptance Criteria:**
- [ ] When `/me/sudo/methods` returns `federationProviders`, the modal renders a button per provider.
- [ ] Clicking posts `/me/sudo/begin {method:"federation_oidc", slug, returnTo: <current path>}` and calls `hardRedirect(res.redirect)`.

**Verify:** `cd dashboard && npm test` → PASS; `npm run build` → typechecks.

**Steps:**

- [ ] **Step 1: Failing test.** In `sudo.test.ts`, mock `api.get('/me/sudo/methods')` → `{methods:['federation_oidc'], federationProviders:[{slug:'google',displayName:'Google'}]}`, mount SudoModal, assert a "Google" button renders; click it and assert `api.post('/api/prohibitorum/me/sudo/begin', {method:'federation_oidc', slug:'google', returnTo: <path>})` then `hardRedirect` was called with the returned URL. (Reuse the reka/test idioms in the repo; mock `lib/navigate.hardRedirect`.)

- [ ] **Step 2: Run → FAIL.** `cd dashboard && npm test -- sudo`

- [ ] **Step 3: Implement.** In `SudoModal.vue`: type the methods response as `{ methods: string[]; federationProviders?: { slug: string; displayName: string }[] }`; add `const federationProviders = computed(() => methodsResp.value?.federationProviders ?? [])`. Render a button per provider. Handler:

```ts
async function reauthFederation(slug: string) {
  const res = await api.post<{ redirect: string }>('/api/prohibitorum/me/sudo/begin', {
    method: 'federation_oidc',
    slug,
    returnTo: route.fullPath,
  })
  hardRedirect(res.redirect)
}
```

Add `sudo.reauthWith` (`"Re-authenticate with {provider}"`) and `sudo.reauthHint` (`"You'll be sent to {provider} and brought back."`) to `en.ts`, plus `errors.sudo_identity_mismatch` / `errors.sudo_reauth_stale`. **Grep-verify en.ts apostrophes after editing** (`grep -n "’" dashboard/src/locales/en.ts` — the Edit tool has corrupted these before; see project notes).

- [ ] **Step 4: Run → PASS + typecheck.** `cd dashboard && npm test && npm run build`

- [ ] **Step 5: Commit.**

```bash
git add dashboard/src
git commit -m "feat(web): OIDC re-authentication option in the sudo step-up modal"
```

---

### Task 7: Mock OP step-up support + end-to-end smoke arc + gate

**Goal:** The in-process mock OP honors `prompt=login`/`max_age` and emits a fresh `auth_time`, and the smoke proves a federated user can elevate via OIDC sudo and then complete a sudo-gated action. Final full-gate run.

**Files:**
- Modify: `cmd/smoke/mockop/server.go` (emit `auth_time` in the id_token; accept `prompt`/`max_age`)
- Modify: `cmd/smoke/main.go` (new federated-sudo arc)
- Modify: `pkg/webui/dist` (rebuild — FE changed)

**Acceptance Criteria:**
- [ ] The mock OP's issued id_token includes an `auth_time` claim set to "now"; it accepts `prompt=login`/`max_age` without error.
- [ ] A new smoke arc: a federated user (auto-provisioned via mockop) calls `/me/sudo/begin {federation_oidc}`, drives the mock authorize+callback, and then performs a sudo-gated action (e.g. add-passkey) that now succeeds.
- [ ] Full gate green.

**Verify:** the smoke runner → `SMOKE_EXIT=0`.

**Steps:**

- [ ] **Step 1: Mock OP `auth_time`.** In `cmd/smoke/mockop/server.go` `handleAuthorize`/id_token build (~line 354), add `"auth_time": <now unix>` to the id_token claims. Accept (ignore is fine) `prompt`/`max_age` query params — the mock always "re-authenticates", so emitting a fresh `auth_time` satisfies the freshness check.

- [ ] **Step 2: Smoke arc.** In `cmd/smoke/main.go`, after the existing federation arc (which already provisions a federated user via mockop), add steps: (a) as that federated session, `GET /me/sudo/methods` → assert `federation_oidc` + a `federationProviders` entry; (b) `POST /me/sudo/begin {method:"federation_oidc", slug:"mockop", returnTo:"/security"}` → follow the returned `redirect` through the mock authorize → hit `/me/sudo/federation/callback` carrying the fed-state cookie → expect a 302 to `/security`; (c) immediately perform a sudo-gated action (e.g. `POST /me/credentials/register/begin`) and assert it now returns 200 (not `sudo_required`). Reuse the federation-driving helpers already in `main.go`.

- [ ] **Step 3: Run the smoke.** Use the documented runner env (`PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true`, smoke `postgres` DB; see README "Development"). Expect `SMOKE_EXIT=0`.

- [ ] **Step 4: Full gate + rebuild dist.**

```bash
go build ./... && go vet ./... && go test ./...
cd dashboard && npm test && npm run build && cd ..   # or `mise build`
git add pkg/webui/dist
```

- [ ] **Step 5: Commit.**

```bash
git add cmd/smoke/mockop/server.go cmd/smoke/main.go pkg/webui/dist
git commit -m "test(smoke): federated OIDC sudo arc + mock-OP auth_time; rebuild dist"
```

---

## Self-review notes

- **Spec coverage:** method availability (T4), begin (T2/T5), callback + identity-match + freshness (T3/T5), FedState sudo flow (T2), FE redirect (T6), security invariants (T2/T3/T5), testing incl. mock-OP enhancement (T1/T3/T7). All spec sections mapped.
- **Browser binding:** the spec calls for it; T2 populates `BrowserBinding` for the sudo flow and T3 verifies it (unlike the link flow, which omits it per OIDCFED-1 — here it IS checked, so it's real defense-in-depth, not a dead field).
- **`returnTo`:** carried in `FedState.ReturnTo` (server-trusted) and validated same-origin server-side; the FE also passes the current path.
- **Type consistency:** `Tokens.AuthTime` (T1) consumed in `SudoCallback` (T3); `StepUpAuthOptions` (T1) used in `begin()` (T2); `SudoKey` (T2) used in `SudoCallback` (T3); `federationProviders` shape (T4) consumed by the FE (T6) and asserted by the smoke (T7).
- **Open risk:** `GetAccountIdentity` by (account,idp) and the exact `ListAccountIdentitiesByAccount` projection (slug/displayName/disabled) must be confirmed against `db/queries/account_identity.sql`; add a query + `sqlc generate` if missing (T2/T4).
