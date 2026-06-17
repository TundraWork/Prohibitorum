# Login-resume redesign — server-validated `return_to`, mirroring the consent flow

**Date:** 2026-06-17
**Status:** Design approved (revised twice after deep codebase study); ready for a plan.
**Supersedes:** the client-only `return_to` guard on the login ceremony (commit `ef07364`'s `safeReturnTo` relaxation stays — it is needed by federation — but is downgraded to defense-in-depth, no longer the security boundary).

## Problem

`return_to` is reflected through the browser at the OIDC/SAML login bounce
(`authorize.go:124,234`, `sso.go:142`) and the consent bounce (`authorize.go:291`).
It is consumed at several points. Audit of those points:

| Consumer | Server-side validation today? |
|----------|------------------------------|
| Federation login (`/auth/federation/{slug}/login`) | **Yes** — `validateFederationReturnTo` (relative-only), callback redirects server-side |
| Consent decision (`POST /consent`) | **Yes** — `sameOriginAsIssuer` (absolute same-origin), returns `ConsentResult{Redirect}`, SPA `hardRedirect`s |
| **Login ceremony** (`login/complete` WebAuthn + password) | **No** — returns JSON; the SPA navigates via client-only `safeReturnTo` |

So **only the login ceremony lacks a server-side check** — that is the real gap
(and the one that prompted this). RFC 9700 §4.11.2 ("the AS MUST NOT be an open
redirector") is already satisfied on the other two paths.

## Best practice + the precedent to mirror

- **RFC 9700 §4.11.2** — validate redirect targets server-side; the AS is never an
  open redirector. ([RFC 9700](https://www.rfc-editor.org/info/rfc9700/))
- **Ory Hydra**'s opaque-`login_challenge` model ([login](https://www.ory.sh/docs/hydra/concepts/login)/[consent](https://www.ory.sh/docs/hydra/concepts/consent)) exists to decouple a *separately-hosted* login UI. Prohibitorum's UI is first-party and same-origin, so that decoupling buys nothing here — **rejected as over-engineering** (a parallel KV-grant + endpoint that duplicates the existing `return_to` threading).
- **The in-repo precedent is consent:** server validates `return_to`, returns a
  `Redirect` field, the SPA follows it with `hardRedirect` and explicitly does
  *not* client-guard it. **Mirror this for the login ceremony.**

## Why a shared validator is needed

The two existing server validators are each too narrow for the ceremony, whose
`return_to` can be **either** shape:

- `sameOriginAsIssuer` (consent): accepts **absolute** same-origin (`scheme+host == issuer`); rejects relative.
- `validateFederationReturnTo` (federation): accepts **relative** (`/…`, not `//`); rejects absolute.
- Ceremony `return_to`: **absolute** when the OIDC/SAML bounce sent it (`Issuer + RequestURI`), **relative** when the SPA's own auth guard set it (`to.fullPath`).

So introduce one superset validator and converge the others onto it.

## Design

### 1. Shared server validator `validateReturnTo`

A Go port of the corrected client `safeReturnTo` — accepts a same-origin
**relative path OR absolute URL**, normalises to a safe relative path, defaults
everything else to `/`:

```go
// validateReturnTo returns a safe, same-origin RELATIVE path to navigate to
// after an auth flow, from untrusted input. Accepts a relative path ("/x") or a
// same-origin absolute URL (scheme+host == issuer); rejects cross-origin,
// protocol-relative ("//"), non-http schemes, and any "//"-resolving path.
// Empty / invalid → "/". Single source of truth for all return_to consumers.
func validateReturnTo(raw string, cfg *configx.Config) string
```

Place it in `pkg/server` (e.g. `returnto.go`). It supersedes
`sameOriginAsIssuer` and `validateFederationReturnTo`; both call sites switch to
it (consent keeps a non-empty guard since `""`→`/` is fine there).

### 2. Login ceremony returns a server-validated redirect (mirror consent)

`handleLoginCompleteHTTP` (WebAuthn, `handle_auth.go`) and the password-login
completion (`handle_auth_password.go`) read `return_to` from the request
(query param, like `POST /consent?return_to=`), run `validateReturnTo`, and
return it to the SPA. Add a contract type mirroring `ConsentResult`:

```go
// LoginResult tells the SPA where to navigate after a successful login.
type LoginResult struct {
    Redirect string `json:"redirect"`
}
```

WebAuthn `login/complete` returns `LoginResult{Redirect: validated}` (it
currently returns `sessionView` — fold the redirect in or return `LoginResult`;
the SPA only needs the redirect). The password completion does the same.

### 3. SPA follows the server redirect (mirror `ConsentView`)

`PasskeyButton` / `PasswordTotpForm` send the URL's `return_to` to
`login/complete` and surface the server's `redirect`; `LoginView` does
`hardRedirect(redirect)` — exactly like `ConsentView` (`hardRedirect(res.redirect)`,
"deliberately NOT guarded by safeReturnTo: the server already validated it").
The success path no longer uses client `safeReturnTo`.

### 4. `safeReturnTo` stays (defense-in-depth), NOT reverted

It is still needed by **FederationButtons** (normalises the bounce's absolute
`return_to` to the relative form `validateFederationReturnTo`→`validateReturnTo`
expects) and by the **already-authenticated** on-mount redirect (a rare direct
`/login` visit; OIDC never bounces an authenticated user here). It is no longer a
security boundary — the server validates every consequential redirect — so it
remains as a client-side convenience + defense-in-depth.

## Security properties

| Property | Mechanism |
|----------|-----------|
| AS is not an open redirector (RFC 9700 §4.11.2) | `validateReturnTo` enforced server-side at **every** consumption point — federation, consent, **and now the login ceremony** |
| No client-only security control | Ceremony redirect is server-validated + returned; SPA `hardRedirect`s the blessed value (mirrors consent) |
| One source of truth | Single `validateReturnTo`; the two narrow validators converge onto it |
| Defense-in-depth | Client `safeReturnTo` retained for federation normalisation + the rare already-authenticated path |

RP `redirect_uri` (exact-match) and PKCE/nonce are unchanged. Bounces are
unchanged (they emit absolute same-origin, which `validateReturnTo` accepts).

## Alternatives considered

- **Ory Hydra opaque `login_challenge` + KV grant** — gold standard for a
  *decoupled* login UI; over-engineered for a first-party same-origin UI.
- **Keep client-only `safeReturnTo` for the ceremony** — the gap that prompted
  this; rejected (no server backstop on the one unguarded path).
- **Revert `safeReturnTo` to strict relative-only** (earlier draft) — *wrong*:
  would break FederationButtons, which relies on its absolute→relative normalisation.

## Rollout (phased, executed via subagent-driven-development)

1. **Shared validator:** add `validateReturnTo` (+ Go unit tests across the
   relative / absolute-same-origin / cross-origin / `//` / scheme / backslash
   cases). Switch consent (`sameOriginAsIssuer`) and federation
   (`validateFederationReturnTo`) call sites onto it; keep the old names as thin
   wrappers or delete after migration.
2. **Ceremony server-side:** thread + validate `return_to` through WebAuthn
   `login/complete` and password completion; return `LoginResult{Redirect}`.
   (+ Go handler tests.)
3. **SPA:** ceremony components send `return_to` and `hardRedirect` the server's
   `redirect`; `LoginView` success path drops `safeReturnTo`. (+ FE tests.)
4. **Verify:** `go build -tags nodynamic ./... && go vet ./... && go test ./...`;
   `npm test` + `npm run build`; rebuild `pkg/webui/dist`; the two-instance
   federation lab end-to-end (passkey login on UP → resume to the pending
   `/oauth/authorize` → code → DOWN).

No data migration; RP boundary unchanged.
