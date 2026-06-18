# Sudo step-up: recent-auth window, gate trim, login-aligned modal — design

**Date:** 2026-06-18
**Status:** approved (brainstorm) — pending spec review → implementation plan
**Supersedes:** `2026-06-12-oidc-sudo-step-up-design.md` (and its implementation —
commits `dd5d8cf`, `3193c96`, `c61d1e7`). The `federation_oidc` sudo method that
spec added is **removed** here; the lockout it solved is re-solved more cleanly
(see §1 and "Why removing federation sudo is safe").

## Problem

Two complaints about the sudo step-up gate:

1. **The step-up modal feels passkey-only and out of step with the login screen.**
   In practice a typical account sees only the passkey button: the password+TOTP
   form is hidden behind a "Use password and code instead" link, and federation
   re-auth only appears for accounts with a linked upstream. The login screen, by
   contrast, lays everything out inline (passkey → OR → password+TOTP form →
   federation). And federation-as-a-step-up is awkward: it bounces the user out of
   a modal through a full-page OIDC redirect, and on an unlocked machine with a
   live upstream session `prompt=login` is often barely a speed bump — a *weak*
   presence proof compared with WebAuthn UV or password+TOTP.

2. **Step-up fires far too often.** Three compounding causes:
   - **Never granted at login.** `SudoUntil` is set only by `/me/sudo/complete`
     (`handle_sudo.go` `applySudoGrant`/`stampSudoUntil`); the login-completion
     paths (`sessionStore.Issue(...)` in the WebAuthn-login, federation-callback,
     and federation-confirm handlers) never stamp it. So you re-prove a factor for
     a sensitive action even seconds after signing in.
   - **One-shot consumption.** `consumeFreshSudo` (`handle_sudo.go`) *clears*
     `SudoUntil` after a single gated action, so even within the 5-minute TTL each
     subsequent sensitive action re-prompts.
   - **Too many gates.** 33 admin operations are gated, including reversible
     operational config (group membership, access grants, enable/disable toggles)
     that doesn't move secrets, trust endpoints, privilege, or accounts.

## Goals / non-goals

In scope:
- A **recent-auth window**: any successful authentication (login *or* in-modal
  step-up) grants `SudoUntil = now + window`, and the window is **multi-use** —
  any number of gated actions succeed until it expires by time.
- **Remove federation re-auth from the sudo modal** and from the backend sudo
  surface. Federation lives only on the login screen.
- **Restyle the sudo modal to mirror the login screen's local section** (passkey
  primary → OR divider → inline password+TOTP form).
- **Bounce-to-login fallback**: when a step-up is required but the account has no
  usable *local* factor (the upstream-login-only case on a stale session), send
  the user to the real `/login` to re-authenticate, then return.
- **Trim the gate set** to "posture & high-impact only" — keep all 7 self-service
  credential/identity gates; drop reversible admin operational config.

Out of scope (YAGNI):
- No change to the WebAuthn / password+TOTP step-up *ceremonies* themselves (only
  where/when they're required and how the modal presents them).
- No sliding window (the grant is fixed from the granting event; a gated action
  does not extend it). Revisit only if friction data says otherwise.
- No auto-resume of the originally-blocked action across the bounce-to-login
  redirect — the user re-triggers it on return (same stance as the prior spec).
- No change to the last-method guard, admin-auth requirement, or audit schema.
- No SAML step-up (SAML is downstream-only in this project).

## Current state (anchored)

- `pkg/authn/middleware.go:20-37` — `SessionData{… SudoUntil time.Time}` and
  `HasFreshSudo()` (`!SudoUntil.IsZero() && now.Before(SudoUntil)`).
- `pkg/server/handle_sudo.go` — `availableSudoMethods` (~128), `handleSudoMethodsHTTP`
  (~96), `handleSudoBeginHTTP` (~152), `handleSudoCompleteHTTP` (~268),
  `handleSudoFederationCallbackHTTP` (~467), `applySudoGrant`/`stampSudoUntil`
  (sets `SudoUntil = now + Auth.SudoTTL`, ~406-448), `consumeFreshSudo` (one-shot
  clear, fail-closed on KV save error, ~510-536), `requireFreshSudo` (~541).
- `pkg/configx/configx.go:227` — `auth.sudo_ttl` default `5*time.Minute`.
- Login-completion paths that call `sessionStore.Issue(...)` **without** stamping
  sudo: WebAuthn login complete (`handle_auth.go` ~309), federation login callback
  (`handle_federation.go` ~171), federation first-time confirm
  (`handle_federation_confirm.go` ~114).
- `pkg/server/operations.go` — `registerOp` (23), `registerSudoOp` (56),
  `registerOpHTTP` (97), `registerSudoOpHTTP` (160), `withFreshSudo` (133).
- Self-service gate sites (all via `requireFreshSudo`): `handle_me_password.go:22`
  (set password), `handle_me_identities.go:117` (unlink identity) & `:246` (link
  begin, GET), `handle_me_revoke_pwd_totp.go:40` (revoke password+TOTP),
  `handle_pairing.go:239` (approve device pairing), `handle_me_totp.go:60`
  (`totpRequiresSudo`, shared by `/me/totp/begin` and `/me/totp/verify` — gated only
  when a confirmed TOTP already exists) & `:125` (regenerate recovery codes).
- Admin gate registrations: `server.go:415-482` (33 ops via `registerSudoOp` /
  `s.registerSudoOpHTTP`).
- Federation sudo (to be removed): `federation_oidc` branch in begin/methods,
  `Federator.SudoBegin`/`SudoCallback`, `FedState.SudoAccountID`/`ExpectedSub` +
  `SudoKey`, `maxStepUpAuthAge` (~`federation.go:703`), `prompt=login`/`max_age=0`
  injection (~`federation.go:417`).
- Last-method guard (unchanged): `pkg/authn/flow.go`
  `DisableNonWebAuthnFallbacks` / `ErrWouldRemoveLastFactorAuth` /
  `ErrLastSignInMethod`, backed by `CountUsableSignInFederation`. Independently
  blocks removing the sole login method, so unlink-sole-identity and
  revoke-sole-factor cannot lock anyone out regardless of step-up.
- Frontend: `dashboard/src/components/custom/SudoModal.vue` (3-method modal),
  `dashboard/src/lib/sudo.ts` (`ensureSudo`/`withSudo`/`_resolveSudo`),
  `dashboard/src/pages/LoginView.vue` (the layout to mirror) + `PasskeyButton.vue`,
  `PasswordTotpForm.vue`, `FederationButtons.vue`, `OrDivider.vue`,
  `dashboard/src/locales/en.ts` `sudo:` block (600-633).

## Design

### 1. Recent-auth window (backend)

Three coordinated changes to the `SudoUntil` lifecycle:

1. **Grant on primary login.** Each login-completion path that issues a session
   also stamps `SudoUntil = now + Auth.SudoTTL` on the freshly-issued
   `SessionData`: WebAuthn login complete, federation login callback, federation
   first-time confirm. Implementation note: thread the stamp through (or
   immediately after) `sessionStore.Issue(...)` so the persisted session carries
   the window from its first read. Re-use the same helper that `applySudoGrant`
   uses to compute the deadline, so the policy can't drift.

2. **Multi-use window (drop one-shot).** `consumeFreshSudo` no longer clears
   `SudoUntil`; it becomes a pure read of `HasFreshSudo()`. Within the window any
   number of gated actions succeed; the window expires only by time. This also
   removes the per-gated-action KV write (and the fail-closed-on-save branch is no
   longer reachable for the read path). Rename for honesty: `consumeFreshSudo` →
   `hasFreshSudoOrDeny`-style read (final name in plan); `requireFreshSudo` keeps
   its signature/behavior (deny + write `ErrSudoRequired` when absent).

3. **Window duration.** Keep the `auth.sudo_ttl` knob; **bump the default 5 min →
   15 min**. Rationale: granting at login + multi-use means 5 min would still
   re-prompt anyone touching settings ~6 minutes after sign-in. 15 min is a
   balance; operators can tune it.

The grant is fixed from the granting event (no sliding renewal on use).

### 2. Login-aligned, local-only modal (frontend)

- **Remove federation** from `SudoModal.vue`: delete `federationProviders`,
  `reauthFederation`, the provider buttons, and the `sudo.reauthWith` / `reauthHint`
  copy usage.
- **Mirror the login local section.** Render passkey button (primary) → `OrDivider`
  → password+TOTP form **inline** (not gated behind the "Use password" link), using
  the same components/visual grammar as `LoginView`. The sudo password form stays a
  single-step `current_password` + `totp_code` verify (no username — the session
  identifies the user), so this is a visual realignment, not a literal reuse of the
  two-phase login `PasswordTotpForm`. Show whichever local methods the account has;
  if it has both, show both (passkey prominent, form below the divider).
- **Bounce-to-login when no usable local factor.** On modal open, fetch
  `/me/sudo/methods` (now local-only). If the result contains neither `webauthn`
  nor `password_totp`, do **not** render an empty modal — `hardRedirect` to
  `/login?return_to=<current route>`. The login screen re-runs the user's real
  auth (including federation), which re-grants the window; on return the user
  re-triggers the action (now within the window). Because this is reachable only on
  a *stale* session (a fresh login already carries the window), it is rare.
- `lib/sudo.ts` `ensureSudo`/`withSudo` are unchanged in contract; the modal simply
  resolves `true` on a successful local step-up, or navigates away on the bounce
  (the in-page retry is abandoned, consistent with the prior spec's redirect
  stance).

### 3. Remove federation from the backend sudo surface

- `/me/sudo/methods`: return local methods only; drop `federationProviders` from
  the response shape.
- `/me/sudo/begin`: remove the `federation_oidc` branch (begin now only serves the
  WebAuthn assertion challenge; password+TOTP still needs no challenge).
- Delete `GET /me/sudo/federation/callback` and its route registration.
- Delete `Federator.SudoBegin` / `SudoCallback`, `FedState.SudoAccountID` /
  `ExpectedSub`, the `SudoKey` namespace, `maxStepUpAuthAge`, and the
  `prompt=login`/`max_age=0` sudo-only injection. The login/link/invite federation
  flows are untouched.
- Remove the now-dead error codes `sudo_identity_mismatch`, `sudo_reauth_stale`
  (and their en.ts strings). `sudo_method_unavailable` stays (still used for an
  unavailable local method on begin/complete).

### 4. Gate trim — "posture & high-impact only"

Mechanism: a kept gate stays on `registerSudoOp` / `s.registerSudoOpHTTP`; a
dropped gate swaps to the plain admin-authenticated registration —
`registerSudoOp(s, mgmt, op, h, admin)` → `registerOp(mgmt, op, h, admin)`, and
`s.registerSudoOpHTTP(s.router, m, p, admin, h)` → `registerOpHTTP(s.router, m, p,
admin, h)`. Admin auth (`admin` requirement) is retained on every dropped gate;
only the *fresh-sudo* check is removed.

**Self-service — KEEP all** (all are credential/identity posture; the window makes
them painless after login). These gate *in-handler* via `requireFreshSudo` (not the
registration wrapper), so no registration change — they simply inherit the §1
window behavior:
`POST /me/password/set`, `GET /me/identities/link/{slug}/begin`,
`POST /me/identities/{id}/unlink`, `POST /me/auth/revoke-password-totp`,
`POST /me/devices/pair/approve`, and the conditional TOTP re-enroll pair
`POST /me/totp/begin` + `POST /me/totp/verify` (gated only when a confirmed TOTP
already exists, via `totpRequiresSudo`), `POST /me/recovery-codes/regenerate`.

**Admin — KEEP (secret/key, privilege, trust-endpoint, mints-a-credential, hard
delete, account lockout):**
- `PUT /accounts/{id}` (update — can escalate user→admin)
- `DELETE /accounts/{id}`
- `POST /accounts/set-disabled`
- `POST /accounts/credentials/delete`
- `POST /accounts/reissue-enrollment`
- `POST /invitations` (create — mints an enrollment path)
- `POST /signing-keys/generate`, `…/{kid}/activate`, `…/{kid}/retire`
- `POST /oidc-applications` (create — mints a client secret)
- `PUT /oidc-applications/{clientId}` (update — redirect-URI = token-exfil vector)
- `POST /oidc-applications/rotate-secret`
- `POST /oidc-applications/delete`
- `POST /identity-providers` (create), `PUT /identity-providers/{slug}` (update —
  federation trust = mass-takeover vector), `POST /identity-providers/rotate-secret`
- `POST /identity-providers/delete` (deleting an IdP can lock out its federated users)

**Admin — DROP (reversible operational config; admin auth retained):**
- `POST /accounts/{id}/sessions/revoke`, `POST /accounts/revoke-sessions`
- `POST /invitations/revoke`
- `POST /oidc-applications/set-disabled`
- `POST /oidc-applications/{clientId}/access/set-restricted` | `/grant` | `/revoke`
- `POST /saml-applications` (create), `PUT /saml-applications/{id}` (update),
  `POST /saml-applications/{id}/reingest-metadata`,
  `POST /saml-applications/set-disabled`, `POST /saml-applications/delete`
- `POST /saml-applications/{id}/access/set-restricted` | `/grant` | `/revoke`
- `POST /groups` (create), `PUT /groups/{id}` (update), `POST /groups/delete`,
  `POST /groups/{id}/members` (add), `POST /groups/{id}/members/remove`

**Borderline decisions (confirmed in brainstorm; called out for redline):**
- **OIDC app update — KEEP.** Redirect-URI changes are a token-exfiltration vector.
- **IdP delete — KEEP.** Can lock out all of a provider's federated users.
- *Known asymmetry:* SAML app create/update/reingest are **dropped** while the OIDC
  analogues are kept. The SAML ACS URL is the assertion-delivery analogue of an
  OIDC redirect URI; if parity is preferred, either keep SAML create/update or drop
  OIDC update. Left as the brainstorm's chosen default (OIDC kept, SAML dropped).
- **SAML app delete — DROP** (grouped with the rest of SAML config). If "hard
  delete" should always be gated, move it to KEEP alongside OIDC delete during
  redline.

## Why removing federation sudo is safe (no re-introduced lockout)

The `2026-06-12` spec added `federation_oidc` to fix a chicken-and-egg lockout: a
federated-only user couldn't satisfy any sudo gate, so they couldn't even add a
local factor. The recent-auth window resolves the same problem without an in-modal
federation flow:

- A federated-only user **who just logged in** carries the window, so add-passkey /
  set-password / link-identity succeed with **no** step-up prompt.
- On a **stale** session, the modal's no-local-factor branch **bounces to `/login`**,
  where the user re-authenticates through their real upstream flow; that login
  re-grants the window, and on return the gated action proceeds.

Either way the federated-only user reaches every credential-establishment action.
The sole login method also remains protected by the existing last-method guard.

## Security tradeoffs (deliberate)

- **Window from login + multi-use** widens the borrowed-session window: a cookie
  stolen within `auth.sudo_ttl` of a legitimate login can perform gated actions
  without re-proving a factor (the GitHub "sudo mode" model). Mitigations retained:
  short, tunable TTL (default 15 min); the last-method guard; admin-auth on every
  admin op; gated audit rows still written per action.
- **Federation removed as a step-up factor** is a net security *improvement* for
  step-up quality: the remaining factors (WebAuthn UV, password+TOTP) are genuine
  presence proofs, whereas a `prompt=login` upstream bounce on an unlocked machine
  often is not.
- **Dropped admin gates** retain admin authentication; they lose only the
  fresh-sudo re-proof. The kept set still covers secret/key material, privilege
  escalation, federation trust, credential/enrollment minting, hard deletes, and
  account lockout.

## Error model

- Remove `sudo_identity_mismatch`, `sudo_reauth_stale` (+ en.ts strings).
- Keep `sudo_required` (gate-not-satisfied) and `sudo_method_unavailable`.
- en.ts `sudo:` block: drop `reauthWith` / `reauthHint`; the "Use password and code
  instead" / "Use a passkey instead" toggle copy is no longer needed once the form
  is inline (keep only if a collapsed layout is retained — see redline).

## Testing

- **Unit (`pkg/server`)**:
  - Login-completion handlers stamp `SudoUntil` (WebAuthn login, federation
    callback, federation confirm); a freshly-issued session reports `HasFreshSudo()`.
  - Multi-use: two consecutive gated actions within the window both succeed (was a
    re-prompt under one-shot); after expiry the gate denies with `sudo_required`.
  - `/me/sudo/methods` returns local methods only (no `federationProviders`);
    `/me/sudo/begin` rejects `federation_oidc`.
  - Gate-policy test (`admin_route_policy_test.go` equivalent): the kept set still
    requires fresh sudo; each dropped op succeeds with admin auth and **no** sudo
    grant, and still rejects non-admins.
- **Unit (`pkg/federation/oidc`)**: deleting `SudoBegin`/`SudoCallback` leaves
  login/link/invite flows green (remove the sudo-specific tests).
- **Smoke (`cmd/smoke`)**: federated-only user → fresh login → add-passkey with no
  step-up; let the window lapse (or force-expire) → gated action triggers the
  bounce-to-login → re-login → action proceeds.
- **FE (vitest)**: `SudoModal` renders passkey + inline password+TOTP and resolves
  on success; with no local factor it redirects to `/login?return_to=…`; no
  federation affordance is rendered.

## Migration / rollout

- Config: `auth.sudo_ttl` default changes 5m → 15m. Document in `CONFIG.md`;
  operators pinning the old value are unaffected.
- No DB migration (purely session-KV + route-registration + FE changes).
- SPA must be rebuilt and re-embedded (the binary serves the embedded dist).

## Open questions

1. **SAML parity** for the borderline asymmetry (keep OIDC-update / drop
   SAML-update) and **SAML app delete** (currently dropped) — confirm or redline.
2. **Window duration** — 15 min default acceptable, or prefer 5/30/60?
