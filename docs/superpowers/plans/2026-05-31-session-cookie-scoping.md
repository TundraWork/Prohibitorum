# Session-cookie Scoping Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Scope the session cookie to `Path=/` so an authenticated browser session reaches the root-level OIDC/SAML protocol endpoints, closing the v0.6 audit's cookie-path-vs-route mismatch finding.

**Architecture:** The session cookie moves from `Path=/api/prohibitorum` to `Path=/`. Its identity (name + `Secure`) becomes deployment-stable, derived from the canonical public origin's scheme: HTTPS deployments get the browser-hardened `__Host-prohibitorum_session` (with `Secure`); HTTP dev keeps the plain `prohibitorum_session` (no `Secure`, so Go's `cookiejar` — and the smoke — can still send it). `SameSite=Lax`, `HttpOnly`, no `Domain`, all unchanged. The ceremony cookie and all routes/issuers/metadata are untouched.

**Tech Stack:** Go, `net/http` cookies, chi router, the existing `cmd/smoke` integration harness running against live Postgres + a real dev server.

**Spec:** `docs/superpowers/specs/2026-05-31-session-cookie-scoping-design.md` (D1–D5).

---

## File Structure

| File | Responsibility | Change |
|---|---|---|
| `pkg/session/middleware.go` | Cookie set/clear/read helpers + session middleware | Core change: new name/secure helpers; `Path=/`; conditional name; cfg-driven `Secure`; read path uses resolved name |
| `pkg/session/middleware_test.go` | Unit tests for cookie attributes (**new file**) | Assert both deployment modes + clear-matches-set + name resolution |
| `pkg/server/handle_auth.go` | Logout handler reads the session cookie by name | Read via resolved name |
| `pkg/server/server.go` | OpenAPI security scheme advertises the cookie name | Advertise resolved name; thread name into `registerSecurityScheme` |
| `cmd/smoke/main.go` | OIDC/SAML interactive smoke helpers | Drop manual cookie re-attach; let the jar auto-send; add a root-path proof assertion; remove dead helper; fix stale comments |
| `cmd/smoke/saml_mock.go` | SAML POST/init/redirect smoke helpers | Drop manual cookie re-attach; let the jar auto-send; fix stale comments |
| `AUDIT.md` | Audit record | Close the architectural finding |

**Key design notes (locked by the spec, applied below):**
- `FreshSessionCookie` / `ClearedSessionCookie` keep their existing signatures — the `*http.Request` parameter is **retained but renamed `_`** because `Secure` now derives from `cfg`, not the request. This keeps the ~7 call sites untouched and the change surgical (the handoff explicitly scoped this as "mostly `pkg/session/middleware.go`"). `LoadSession` still passes `r` positionally; it compiles unchanged.
- `secureCookies` is derived from `strings.HasPrefix(lower(cfg.PublicOrigins[0]), "https://")` — no new import, deployment-stable, TLS-proxy-safe.
- `CeremonyCookie` / `ClearedCeremonyCookie` and `isSecure` are **unchanged** (D4): the ceremony cookie stays `Path=/api/prohibitorum/auth`, `SameSite=Strict`, per-request `Secure`. Do not touch them.

---

## Task 1: Core session-cookie scoping (pkg/session) + unit tests

**Goal:** Change the session cookie to `Path=/` with a deployment-conditional name + `Secure`, and read it back by the same resolved name — all within `pkg/session`, with the package building and all callers untouched.

**Files:**
- Modify: `pkg/session/middleware.go`
- Test: `pkg/session/middleware_test.go` (create)

**Acceptance Criteria:**
- [ ] `FreshSessionCookie` returns `Path=/`, `SameSite=Lax`, `HttpOnly=true`, no `Domain`.
- [ ] In an HTTPS deployment (`PublicOrigins[0]` = `https://…`): name is `__Host-prohibitorum_session`, `Secure=true`.
- [ ] In HTTP dev (`PublicOrigins[0]` = `http://…`, or empty): name is `prohibitorum_session`, `Secure=false`.
- [ ] `ClearedSessionCookie` matches `FreshSessionCookie` on name + path + attributes (so the browser deletes rather than orphans), with `MaxAge=-1`.
- [ ] `LoadSession` reads the cookie by the resolved name in both modes.
- [ ] `go build ./...`, `go vet ./...`, `go test ./pkg/session/...` all green; existing `pkg/session` tests unaffected.

**Verify:** `mise exec -- go test ./pkg/session/... -v` → all PASS (new + existing).

**Steps:**

- [ ] **Step 1: Write the failing tests** — create `pkg/session/middleware_test.go`:

```go
package session

import (
	"net/http"
	"testing"
	"time"

	"prohibitorum/pkg/configx"
)

func secureCfg() *configx.Config {
	return &configx.Config{PublicOrigins: []string{"https://idp.example.com"}}
}

func devCfg() *configx.Config {
	return &configx.Config{PublicOrigins: []string{"http://localhost:8080"}}
}

func TestSessionCookieNameFor(t *testing.T) {
	if got := SessionCookieNameFor(secureCfg()); got != "__Host-"+SessionCookieName {
		t.Errorf("secure name = %q, want %q", got, "__Host-"+SessionCookieName)
	}
	if got := SessionCookieNameFor(devCfg()); got != SessionCookieName {
		t.Errorf("dev name = %q, want %q", got, SessionCookieName)
	}
	if got := SessionCookieNameFor(&configx.Config{}); got != SessionCookieName {
		t.Errorf("no-origin name = %q, want plain %q", got, SessionCookieName)
	}
}

func TestFreshSessionCookie_SecureDeployment(t *testing.T) {
	c := FreshSessionCookie(secureCfg(), nil, 42, "tok", time.Hour)
	if c.Name != "__Host-"+SessionCookieName {
		t.Errorf("Name = %q, want __Host-%s", c.Name, SessionCookieName)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want /", c.Path)
	}
	if !c.Secure {
		t.Error("Secure = false, want true in https deployment")
	}
	if !c.HttpOnly {
		t.Error("HttpOnly = false, want true")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
	if c.Domain != "" {
		t.Errorf("Domain = %q, want empty (__Host- forbids Domain)", c.Domain)
	}
	if c.MaxAge != int(time.Hour.Seconds()) {
		t.Errorf("MaxAge = %d, want %d", c.MaxAge, int(time.Hour.Seconds()))
	}
}

func TestFreshSessionCookie_DevDeployment(t *testing.T) {
	c := FreshSessionCookie(devCfg(), nil, 42, "tok", time.Hour)
	if c.Name != SessionCookieName {
		t.Errorf("Name = %q, want plain %s", c.Name, SessionCookieName)
	}
	if c.Path != "/" {
		t.Errorf("Path = %q, want /", c.Path)
	}
	if c.Secure {
		t.Error("Secure = true, want false over http dev (cookiejar won't send Secure over http)")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Errorf("SameSite = %v, want Lax", c.SameSite)
	}
}

func TestClearedSessionCookie_MatchesFresh(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *configx.Config
	}{{"secure", secureCfg()}, {"dev", devCfg()}} {
		t.Run(tc.name, func(t *testing.T) {
			fresh := FreshSessionCookie(tc.cfg, nil, 42, "tok", time.Hour)
			clear := ClearedSessionCookie(tc.cfg, nil)
			if clear.Name != fresh.Name {
				t.Errorf("clear Name = %q, fresh Name = %q (must match to delete)", clear.Name, fresh.Name)
			}
			if clear.Path != fresh.Path {
				t.Errorf("clear Path = %q, fresh Path = %q (must match to delete)", clear.Path, fresh.Path)
			}
			if clear.Secure != fresh.Secure {
				t.Errorf("clear Secure = %v, fresh Secure = %v", clear.Secure, fresh.Secure)
			}
			if clear.MaxAge != -1 {
				t.Errorf("clear MaxAge = %d, want -1", clear.MaxAge)
			}
			if clear.Value != "" {
				t.Errorf("clear Value = %q, want empty", clear.Value)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `mise exec -- go test ./pkg/session/... -run 'SessionCookieNameFor|FreshSessionCookie|ClearedSessionCookie' -v`
Expected: FAIL — `SessionCookieNameFor` undefined; existing `FreshSessionCookie`/`ClearedSessionCookie` still return `Path=/api/prohibitorum` and unprefixed names.

- [ ] **Step 3: Add the name/secure helpers** — in `pkg/session/middleware.go`, after the `const (...)` cookie-name block (around line 17), add:

```go
// secureCookies reports whether session cookies should be hardened for a
// secure (HTTPS) deployment. Derived from the canonical public origin's scheme
// so the cookie identity is deployment-stable (not per-request) — required
// because the __Host- name must match between the set and read paths, and a
// TLS-terminating proxy must not flip it per request.
func secureCookies(cfg *configx.Config) bool {
	return len(cfg.PublicOrigins) > 0 &&
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(cfg.PublicOrigins[0])), "https://")
}

// sessionCookieName returns the deployment-appropriate session cookie name:
// the browser-hardened __Host- prefix in secure deployments, the plain base
// name otherwise. The __Host- prefix REQUIRES Secure + Path=/ + no Domain
// (all satisfied below) and gives browser-enforced session-fixation /
// subdomain-injection defense.
func sessionCookieName(secure bool) string {
	if secure {
		return "__Host-" + SessionCookieName
	}
	return SessionCookieName
}

// SessionCookieNameFor resolves the session cookie name for cfg. Exported so
// out-of-package readers (the logout handler, the OpenAPI security scheme) name
// the cookie identically to this package's set/clear/read paths.
func SessionCookieNameFor(cfg *configx.Config) string {
	return sessionCookieName(secureCookies(cfg))
}
```

- [ ] **Step 4: Rewrite the set/clear helpers** — replace `FreshSessionCookie` (lines ~87–100) and `ClearedSessionCookie` (lines ~102–115) with:

```go
// FreshSessionCookie constructs the Set-Cookie value for issuing or refreshing
// a session. Path=/ so a real browser sends it to the root-level OIDC/SAML
// protocol endpoints (/oauth/authorize, /saml/sso, …) — it is an opaque
// HttpOnly token, so being sent on all paths is exactly what mainstream IdPs
// do. Name + Secure derive from the deployment scheme (see secureCookies).
// The *http.Request is no longer needed (Secure comes from cfg) but is kept
// for signature stability with the call sites.
func FreshSessionCookie(cfg *configx.Config, _ *http.Request, accountID int32, token string, ttl time.Duration) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieNameFor(cfg),
		Value:    CookieValue(accountID, token),
		Path:     "/",
		HttpOnly: true,
		Secure:   secureCookies(cfg),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl.Seconds()),
	}
}

// ClearedSessionCookie expires the session cookie. Name + Path + attributes
// MUST match FreshSessionCookie or browsers create a new empty cookie rather
// than clearing the existing one.
func ClearedSessionCookie(cfg *configx.Config, _ *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieNameFor(cfg),
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secureCookies(cfg),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
}
```

- [ ] **Step 5: Update the read path** — in `LoadSession`, change the cookie read (line ~34) from:

```go
		c, err := r.Cookie(SessionCookieName)
```

to:

```go
		c, err := r.Cookie(SessionCookieNameFor(cfg))
```

(Leave everything else in `LoadSession` unchanged — the `FreshSessionCookie(cfg, r, …)` / `ClearedSessionCookie(cfg, r)` calls still compile because the signatures are preserved.)

- [ ] **Step 6: Run tests to verify they pass**

Run: `mise exec -- go test ./pkg/session/... -v`
Expected: PASS — new attribute tests + all pre-existing `pkg/session` tests.

- [ ] **Step 7: Build + vet the whole module**

Run: `mise exec -- go build ./... && mise exec -- go vet ./...`
Expected: exit 0, no errors. (Trust this over any gopls `<new-diagnostics>` per the project's runtime quirks.)

- [ ] **Step 8: Commit**

```bash
git add pkg/session/middleware.go pkg/session/middleware_test.go
git commit -m "fix(session): scope session cookie Path=/ with deployment-conditional __Host- name

Session cookie was Path=/api/prohibitorum, so a real browser never sent it to
the root-level /oauth/authorize and /saml/sso* endpoints — the session gate saw
no session and looped to /login. Scope it Path=/ (universal IdP practice) and
make name+Secure deployment-stable: __Host-prohibitorum_session+Secure in HTTPS
deployments, plain prohibitorum_session in HTTP dev (so cookiejars can send it).
SameSite=Lax/HttpOnly/no-Domain unchanged; ceremony cookie untouched.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Resolve the conditional name at the logout-read and OpenAPI sites (pkg/server)

**Goal:** The two remaining places that name the session cookie outside `pkg/session` — the logout handler's cookie read and the OpenAPI security scheme — use the resolved (deployment-conditional) name so they stay correct in HTTPS deployments.

**Files:**
- Modify: `pkg/server/handle_auth.go:280`
- Modify: `pkg/server/server.go` (`registerSecurityScheme` + its two callers at lines ~127 and ~183)

**Acceptance Criteria:**
- [ ] `handleLogoutHTTP` reads the cookie via `sessstore.SessionCookieNameFor(s.config)`.
- [ ] `registerSecurityScheme` advertises the resolved name in the live server (constructor) and the plain base name in the config-less `NewHuma()` openapi-emit path.
- [ ] `go build ./...`, `go vet ./...`, `go test ./pkg/server/...` all green (existing tests assert the plain base name and run with http origins, so they still match).

**Verify:** `mise exec -- go test ./pkg/server/... && mise exec -- go build ./... && mise exec -- go vet ./...` → exit 0.

**Steps:**

- [ ] **Step 1: Update the logout read** — in `pkg/server/handle_auth.go`, change line ~280 from:

```go
	if c, err := r.Cookie(sessstore.SessionCookieName); err == nil && c.Value != "" {
```

to:

```go
	if c, err := r.Cookie(sessstore.SessionCookieNameFor(s.config)); err == nil && c.Value != "" {
```

- [ ] **Step 2: Thread the cookie name into the security scheme** — in `pkg/server/server.go`, change the `registerSecurityScheme` signature (line ~237) and body to accept and use the name:

```go
func registerSecurityScheme(api huma.API, cookieName string) {
	doc := api.OpenAPI()
	if doc.Components == nil {
		doc.Components = &huma.Components{}
	}
	if doc.Components.SecuritySchemes == nil {
		doc.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	doc.Components.SecuritySchemes["prohibitorumSession"] = &huma.SecurityScheme{
		Type: "apiKey",
		In:   "cookie",
		Name: cookieName,
	}
}
```

- [ ] **Step 3: Update the constructor caller** — in `pkg/server/server.go` line ~127, change:

```go
	registerSecurityScheme(api)
```

to:

```go
	registerSecurityScheme(api, sessstore.SessionCookieNameFor(config))
```

- [ ] **Step 4: Update the NewHuma caller** — in `pkg/server/server.go` line ~183 (`NewHuma()` has no config; the openapi-emit doc is informational, so advertise the plain base name):

```go
	registerSecurityScheme(s.api, sessstore.SessionCookieName)
```

- [ ] **Step 5: Build, vet, test**

Run: `mise exec -- go build ./... && mise exec -- go vet ./... && mise exec -- go test ./pkg/server/...`
Expected: exit 0; all `pkg/server` tests PASS (they construct http origins / empty `PublicOrigins`, so the resolved name is the plain base they already assert on).

- [ ] **Step 6: Commit**

```bash
git add pkg/server/handle_auth.go pkg/server/server.go
git commit -m "fix(server): name session cookie via SessionCookieNameFor at logout-read + OpenAPI scheme

So the __Host- prefix in HTTPS deployments is honored everywhere the cookie is
named outside pkg/session.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Drop the smoke's manual cookie re-attach (behavioral proof)

**Goal:** Remove the manual cookie re-attach across all five OIDC/SAML smoke helpers and let the Go `cookiejar` auto-send the now-`Path=/` cookie to the root-mounted endpoints — the behavioral proof the audit asked for. Add one explicit assertion that the jar holds the session cookie at the root path post-login.

**Files:**
- Modify: `cmd/smoke/main.go` — `authorizeWithSession` (~3928), `ssoLocation` (~4021); remove `sessionCookieForOIDC` (~3914); add `assertSessionCookieAtRoot` + call it; fix stale comments (~631, ~970–974)
- Modify: `cmd/smoke/saml_mock.go` — `ssoPostForm` (~457), `ssoInit` (~490), `ssoWithSession` (~633); fix stale comments (~452–455, ~627–631)

**Acceptance Criteria:**
- [ ] None of the five helpers call `sessionCookieForOIDC` / `req.AddCookie`; each instead sets `Jar: c.jar` on its non-redirect-following `http.Client`.
- [ ] `sessionCookieForOIDC` is deleted (no remaining references).
- [ ] A new `assertSessionCookieAtRoot(c)` fails loudly (via `log.Fatalf`) if the jar does not hold the `prohibitorum_session` cookie at `c.base + "/"`; it is called once before the first `authorizeWithSession` in the OIDC interactive flow.
- [ ] Stale "Path=/api/prohibitorum so the jar would not send it" comments are corrected.
- [ ] Full smoke (steps 1–111) green end-to-end, `SMOKE_EXIT=0`.

**Verify:** `mise exec -- go build ./cmd/smoke/...` then run the full smoke (see Step 6) → `SMOKE_EXIT=0`.

**Steps:**

- [ ] **Step 1: Convert the three `saml_mock.go` helpers** — in each of `ssoPostForm`, `ssoInit`, `ssoWithSession`, delete the three lines that fetch + attach the cookie:

```go
	ck := sessionCookieForOIDC(c)
	if ck == nil {
		return 0, "", errors.New("ssoPostForm: no session cookie in jar (is c logged in?)")
	}
	req.AddCookie(ck)
```

(the error message differs per function: `ssoPostForm` / `ssoInit` / `ssoWithSession`) and add `Jar: c.jar,` as the first field of that helper's `hc := &http.Client{…}` literal, e.g.:

```go
	hc := &http.Client{
		Jar:     c.jar,
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
```

Then correct each helper's doc comment — replace the "session cookie is Path=/api/prohibitorum so the jar would not send it … attached by hand" wording with: "the session cookie is Path=/, so c's jar auto-sends it to the root-mounted endpoint (browser-equivalent)."

- [ ] **Step 2: Convert the two `main.go` helpers** — apply the identical transformation to `authorizeWithSession` (~3928) and `ssoLocation` (~4021): delete their `sessionCookieForOIDC` + `req.AddCookie(ck)` block and add `Jar: c.jar,` to their `hc := &http.Client{…}` literal. Update `authorizeWithSession`'s comment to note the jar now sends the `Path=/` cookie automatically (no manual attach).

- [ ] **Step 3: Delete the dead helper** — remove `sessionCookieForOIDC` (`cmd/smoke/main.go` ~3911–3922) entirely. Add the root-path proof assertion in its place:

```go
// assertSessionCookieAtRoot fails the smoke if c's jar does not hold the
// session cookie at the ROOT path. This is the behavioral proof of the
// Path=/ scoping fix: a real browser (and the jar) only sends the cookie to
// /oauth/authorize and /saml/sso* when it is scoped to "/". The dev smoke
// server is http://localhost, so the cookie name is the plain base.
func assertSessionCookieAtRoot(c *client) {
	u, _ := url.Parse(c.base + "/")
	for _, ck := range c.jar.Cookies(u) {
		if ck.Name == "prohibitorum_session" {
			return
		}
	}
	log.Fatalf("session cookie not present at root path %q — Path=/ scoping regressed", c.base+"/")
}
```

- [ ] **Step 4: Call the assertion once** — in `cmd/smoke/main.go`, immediately before the first `authorizeWithSession(c, authzURL)` call (~line 1039), insert:

```go
	assertSessionCookieAtRoot(c)
```

- [ ] **Step 5: Fix remaining stale comments** — correct the now-wrong cookie-path comments at `cmd/smoke/main.go` ~631 and ~970–974 (they describe the cookie as `Path=/api/prohibitorum`). Replace with a one-line note that the session cookie is `Path=/` and the jar sends it to root-mounted endpoints automatically.

- [ ] **Step 6: Build the smoke and run the full suite**

Run (build first):
```bash
mise exec -- go build ./cmd/smoke/...
```
Expected: exit 0, and `grep -rn "sessionCookieForOIDC" cmd/smoke` returns nothing.

Then run the full smoke detached (per project quirk — the Bash tool SIGPIPEs on long pipelines; NEVER `pkill -f prohibitorum`). Reuse the established runner pattern:
```bash
setsid bash /tmp/run_v06.sh ; sleep 2 ; cat /tmp/v06.result
```
(If `/tmp/run_v06.sh` is absent in this session, recreate it to: start the dev server against the live PG at `/tmp/prohibitorum-pg`, run `go run ./cmd/smoke` with `PROHIBITORUM_PUBLIC_ORIGIN=http://localhost:<port>` and the `smoke-v06-admin` account, capture `SMOKE_EXIT` into `/tmp/v06.result`.)
Expected: full suite `45/45 (v0.2) + 46–69 + 70–87 + 88–99 + 100–111`, `SMOKE_EXIT=0`.

- [ ] **Step 7: Commit**

```bash
git add cmd/smoke/main.go cmd/smoke/saml_mock.go
git commit -m "test(smoke): drop manual session-cookie re-attach; jar auto-sends Path=/ cookie

The smoke previously masked the cookie-path bug by hand-attaching the session
cookie to root-path /oauth and /saml requests. With the cookie now Path=/, the
cookiejar sends it automatically (browser-equivalent), so all five OIDC/SAML
helpers use the jar directly. assertSessionCookieAtRoot is the explicit proof.

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Close the v0.6 architectural finding in AUDIT.md

**Goal:** Record that the cookie-path-vs-route mismatch is fixed and verified, so the audit no longer carries it as an open architectural finding.

**Files:**
- Modify: `AUDIT.md` (the "⚠️ Architectural finding…" block at ~line 615)

**Acceptance Criteria:**
- [ ] The finding block is marked resolved, names the fix (`Path=/` + deployment-conditional `__Host-`/`Secure`), references the spec, and notes the verification (unit tests + the smoke's now-unmasked jar auto-send + `assertSessionCookieAtRoot`).
- [ ] The documented D2 limitation is carried forward: a logged-in SAML **HTTP-POST-binding** AuthnRequest is a cross-site POST, so the `SameSite=Lax` cookie isn't sent and the user bounces through `/login` once (same family as the deferred `ForceAuthn`+POST item).
- [ ] No false "verified in a real browser" claim — state that browser behavior follows from `Path=/` + `SameSite=Lax` per the web-platform spec; the verification is the attribute-level unit tests + the dev-mode behavioral smoke (no browser harness).

**Verify:** Re-read the edited AUDIT.md block; confirm it states the resolution, the spec reference, the verification evidence, and the carried-forward POST-binding caveat — and makes no real-browser claim.

**Steps:**

- [ ] **Step 1: Rewrite the finding block** — replace the `### ⚠️ Architectural finding to resolve…` block (`AUDIT.md` ~615–631) with a resolved entry, e.g.:

```markdown
### ✅ Architectural finding RESOLVED (2026-05-31) — session-cookie scoping

The session cookie was scoped `Path=/api/prohibitorum` while the OIDC/SAML
protocol routes are root-level (`/oauth/authorize`, `/saml/sso`, `/saml/sso/init`,
`/saml/slo`), so a real browser never attached the cookie to those paths and the
session gate looped to `/login`. **Fixed:** the session cookie is now `Path=/`
with a deployment-conditional identity — `__Host-prohibitorum_session` + `Secure`
in HTTPS deployments, plain `prohibitorum_session` (no `Secure`) in HTTP dev so
`cookiejar`-based clients can still send it. `SameSite=Lax`, `HttpOnly`, no
`Domain` unchanged; no route/issuer/metadata changes; ceremony cookie untouched.
Spec: `docs/superpowers/specs/2026-05-31-session-cookie-scoping-design.md` (D1–D5);
code: `pkg/session/middleware.go`.

**Verification:** attribute-level unit tests in `pkg/session/middleware_test.go`
(both deployment modes; clear-matches-set), and `cmd/smoke` now DROPS its manual
cookie re-attach — the jar auto-sends the `Path=/` cookie to the root-mounted
endpoints (browser-equivalent), with `assertSessionCookieAtRoot` proving the
scoping. Steps 100–111 stay green, `SMOKE_EXIT=0`. A real-browser HTTPS end-to-end
run is out of scope (no browser harness); production browser behavior follows from
`Path=/` + `SameSite=Lax` per the web-platform spec.

**Carried-forward limitation (D2):** a logged-in user hitting a SAML
**HTTP-POST-binding** AuthnRequest is a cross-site POST, so the `SameSite=Lax`
cookie is not sent and the user bounces through `/login` once — same family as the
deferred `ForceAuthn`+POST-binding item. `SameSite=None` was rejected (broader
cross-site exposure, requires always-`Secure`, increasingly browser-restricted).
```

- [ ] **Step 2: Commit**

```bash
git add AUDIT.md
git commit -m "docs(audit): close v0.6 session-cookie scoping finding (Path=/ + __Host- fix verified)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

- **Spec coverage:** D1 (`Path=/`) → Task 1 Step 4. D2 (`SameSite=Lax` kept + POST-binding caveat) → Task 1 (Lax retained) + Task 4 carry-forward. D3 (deployment-conditional `__Host-`/`Secure` via origin scheme) → Task 1 Steps 3–4 + tests. D4 (ceremony unchanged) → explicitly out of scope, untouched. D5 (no route/issuer/metadata changes) → no such files in any task. Spec "Components" (helpers, set/clear/read, smoke drop) → Tasks 1 & 3. Spec "Testing" (unit both modes, smoke drop, full gate, optional root-cookie assert) → Tasks 1 & 3. AUDIT close → Task 4. All covered.
- **Placeholder scan:** every code step shows concrete code/commands; no TBD/“handle errors”/“similar to”.
- **Type consistency:** `secureCookies(cfg) bool`, `sessionCookieName(secure bool) string`, `SessionCookieNameFor(cfg) string`, `assertSessionCookieAtRoot(c)` used consistently across Tasks 1–3; `FreshSessionCookie`/`ClearedSessionCookie` signatures preserved (`_ *http.Request`) so all existing call sites compile untouched; `registerSecurityScheme(api, cookieName)` updated at both call sites.
- **Risk note:** existing `pkg/server` tests assert the plain base `SessionCookieName` and run with http/empty origins → `secureCookies` is false → resolved name is the base; assertions still hold (verified: no `NewTLSServer` in `pkg/server`).
