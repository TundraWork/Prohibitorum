# Frontend Rebuild — Spec 2b: Security (Passkeys / Password / TOTP / Recovery)

> Scope: frontend. Slice **2b of 3** of the Spec 2 (authenticated dashboard)
> chunk of the from-scratch rebuild. Builds the `/security` page and its four
> cards on shadcn-vue, all behind the Spec-2a sudo step-up gate. No backend
> change — the `/me/*` security endpoints already exist and are verified below.

## 1. Context

Spec 1 (threshold pages) and Spec 2a (authenticated shell + sudo gate + Profile +
Sessions) are DONE. Spec 2a delivered the reusable gate (`lib/sudo.ts`
`withSudo`/`ensureSudo` + `SudoModal`), the config-driven `AppSidebar`,
`StatusBadge`, and the `errors.<code>`+`Alert` error idiom. This slice adds the
**Security** surface that depends on all of that.

The old `dashboard/` (git `e45f356`) is advisory only. Backend endpoints +
security properties + `DESIGN.md`/`PRODUCT.md` are canonical. All backend
shapes below were read from the handlers (`pkg/server/handle_me*.go`,
`handle_sudo.go`), not the old FE.

## 2. Goal & scope

**Goal:** let a signed-in user manage their authentication factors — passkeys,
password, TOTP authenticator, and recovery codes — to a quality bar, with the
sudo gate transparently enforced where the backend requires it.

**In scope:** `/security` route + nav link; `SecurityView` + four cards
(Passkeys, Password, TOTP, Recovery codes); the reusable building blocks
`CodeField`, `RecoveryCodesDisplay`, `ConfirmDialog`, `TotpQr`; the coarse
"remove password & authenticator" destructive action; the `qrcode` dependency.

**Out of scope:** Connected accounts + Devices (Spec 2c); Admin (Spec 3); any
backend HTTP change; dark mode. See §10 for explicitly-deferred items that the
research recommends but the current backend can't support.

## 3. Backend contract (verified — no new endpoints)

Sudo column: `withSudo()` wraps a call and steps up **only** when the server
returns `{code:'sudo_required'}`, so it is a safe no-op on ungated calls.

| Action | Method · path | Body → Response | Sudo |
|---|---|---|---|
| List passkeys | GET `/me/credentials` | → `CredentialView[]` `{id, credentialIdSuffix, nickname?, transports[], backupState, attestationType, createdAt, lastUsedAt?}` | no |
| Add passkey (begin) | POST `/me/credentials/register/begin` | (no body) → WebAuthn creation options (+ server `excludeCredentials`) | no |
| Add passkey (complete) | POST `/me/credentials/register/complete?nickname=` | attestation → `CredentialView` | no |
| Rename passkey | POST `/me/credentials/rename` | `{id, nickname?}` → 204 | no |
| Delete passkey | POST `/me/credentials/delete` | `{id}` → 204; rejects the **last** passkey → `last_passkey` | no |
| Set/replace password | POST `/me/password/set` | `{password}` (8–1024) → 204 | **always** |
| TOTP begin | POST `/me/totp/begin` | (no body) → `{secret_base32, otpauth_uri}` | **conditional** (only if a confirmed TOTP already exists) |
| TOTP verify | POST `/me/totp/verify` | `{code}` → first enrollment: `{recovery_codes:[…]}`; else 204 | **conditional** (same as begin) |
| Regenerate recovery codes | POST `/me/recovery-codes/regenerate` | (no body) → `{recovery_codes:[…]}` | **always** + requires confirmed TOTP (else `bad_request`) |
| Remove password+TOTP | POST `/me/auth/revoke-password-totp` | (no body) → 204; idempotent; deletes password + TOTP + recovery codes, leaving only passkeys | **always** |

Error envelope `{message, code, details}`; messages are Chinese, so the FE maps
codes via `errors.<code>` (add `last_passkey` to `en.ts` — verify others like
`bad_request`/`bad_credentials`/`rate_limited` are already mapped from earlier slices).

**Verify-before-persist (TOTP):** the backend stashes the pending secret
server-side at `/begin` and only commits it at `/verify`. This is the #1 MFA
enrollment best practice (no orphaned secret on abandonment) and the FE relies
on it — never treat `/begin` as "enabled".

## 4. Routing & shell

- Add `/security` as a child of `DashboardLayout` (`meta.requiresAuth`), lazy
  component `SecurityView`.
- Add a **Security** item to `AppSidebar`'s Account nav group (between Profile
  and Sessions): `{ to: '/security', label: t('nav.security'), icon: ShieldCheck }`
  (or `KeyRound`). The config-array structure already supports this.

## 5. Building-block components (`components/custom/`)

Research-backed, reused across cards. Each is independently testable.

### `CodeField.vue`
Monospace value + a copy-to-clipboard button (`navigator.clipboard.writeText`,
with a transient "Copied" affordance). Props: `value: string`, `label?: string`.
Used for the TOTP `secret_base32`. Mono + the backend's unambiguous code
alphabet address transcription errors.

### `RecoveryCodesDisplay.vue`
The codes screen, done to the research bar:
- Renders `codes: string[]` in a monospace grid (`whitespace-nowrap`, no wrap).
- **Copy all** (clipboard) + **Download .txt** (Blob + `<a download>`, no inline
  script) actions.
- Secure-storage microcopy (store in ≥2 places, not beside your password).
- An **"I've saved my codes" confirmation** (checkbox or button) that must be
  acted on before the parent lets the user dismiss; emits `confirmed`.
- A `regenerated?: boolean` prop toggles the "this replaced your old codes"
  warning copy.

### `ConfirmDialog.vue`
Reusable destructive confirmation over the vendored `ui/dialog` primitive
(capability-floor: keep its focus-trap/escape/aria). Props: `open` (v-model),
`title`, `confirmLabel` (descriptive, e.g. "Remove password & authenticator" —
never "Confirm"), optional `busy`; default slot = itemized consequences. The
confirm button uses the **destructive (Rose)** variant; **Cancel gets initial
focus** and is spatially separated from confirm. Emits `confirm` / `cancel`.
This standardizes destructive UX (restate + itemize + red descriptive button +
safe default focus) per NN/g + Smashing guidance.

### `TotpQr.vue`
Renders `uri: string` (the `otpauth_uri`) as a QR code: `qrcode`'s `toDataURL`
→ `data:` PNG `<img>` with descriptive `alt` (CSP-safe: `img-src 'self' data:`
already allows it). Always shown **alongside** the secret `CodeField` so
no-scan / screen-reader users can enroll. Renders nothing (and the card shows
the secret only) if QR generation fails.

## 6. Cards (`pages/security/`)

### `PasskeysCard.vue`
- Lists `GET /me/credentials`; each row: nickname (fallback to a default like
  "Passkey ····<suffix>"), created + last-used dates, a sync/backup `StatusBadge`
  (`backupState` → "Synced" success / "This device" neutral), and the 4-char
  suffix. Long nicknames **truncate** (no wrap — the Spec-2a quality lesson).
- **Add passkey** CTA → `register/begin` → `useWebauthn.register(options)` →
  `register/complete?nickname=<optional>` → reload. (Backend sends
  `excludeCredentials`, preventing duplicate/zombie passkeys.)
- **Rename** inline (`/me/credentials/rename {id, nickname}`).
- **Delete** via `ConfirmDialog` (consequences: "you'll sign in with your other
  passkeys"); the backend rejects the last passkey → surface `errors.last_passkey`.
- These endpoints are session-only (not sudo-gated) → called directly.

### `PasswordCard.vue`
- New-password + confirm fields; client mirror of the 8–1024 length rule (the
  real check is server-side).
- Submit → `withSudo(POST /me/password/set {password})` → success feedback.
  (`withSudo` opens the step-up modal because this endpoint is always gated.)

### `TotpCard.vue`
- "Set up authenticator app" → `withSudo(begin)` → show `TotpQr` + secret
  `CodeField` + a code input → `withSudo(verify {code})`.
- First enrollment returns `recovery_codes` → render `RecoveryCodesDisplay`
  (with its save-confirmation gate) inline.
- Stateless re-enroll: setting up again replaces the existing TOTP (begin is
  sudo-gated in that case — `withSudo` handles it transparently).

### `RecoveryCodesCard.vue`
- "Regenerate recovery codes" → `withSudo(regenerate)` → `RecoveryCodesDisplay`
  (`regenerated`) — warns the old set is now invalid.
- If no confirmed TOTP exists the backend returns `bad_request`; surface a
  plain-language hint ("Set up an authenticator app first").

### `SecurityView.vue`
- Page heading + the four cards stacked.
- At the bottom, the coarse destructive action: **"Remove password &
  authenticator app — sign in with passkeys only"** → `ConfirmDialog` itemizing
  what's removed (password, authenticator, recovery codes; passkeys kept) →
  `withSudo(POST /me/auth/revoke-password-totp)`. Spatially separated from the
  card actions (consequential-vs-benign proximity rule).

## 7. i18n (English-first; `locales/en.ts`)

Add `nav.security` to the existing `nav` namespace (the sidebar link label).
New namespaces: `security` (page + the four card titles/labels/buttons/help),
`recoveryCodes` (heading, copyAll, download, storage guidance, savedConfirm,
regeneratedWarning), `confirm` (generic cancel + the revoke/delete consequence
copy). Add `errors.last_passkey` (verify `bad_request`/`bad_credentials`/
`rate_limited`/`webauthn_error` are already mapped from earlier slices). zh deferred.

## 8. Testing

Per-component (mock `api`/`webauthn`/`sudo`/clipboard):
- `CodeField` — renders value; copy calls `navigator.clipboard.writeText`.
- `RecoveryCodesDisplay` — renders codes; copy-all + download wired; `confirmed`
  only emits after the save-confirmation; download builds a Blob/URL (mock
  `URL.createObjectURL`).
- `ConfirmDialog` — confirm/cancel emit; confirm button is destructive variant;
  cancel is the initial focus.
- `TotpQr` — given a uri, renders an `<img>` with non-empty `src` + `alt`
  (mock `qrcode.toDataURL`).
- `PasskeysCard` — list renders; add (begin→register→complete) reloads; rename
  posts `{id,nickname}`; delete confirm → posts `{id}` + reload; `last_passkey`
  error renders.
- `PasswordCard` — submit → `withSudo` wraps `/password/set`; length validation.
- `TotpCard` — begin→QR+secret render; verify → recovery codes render; (mock
  `ensureSudo`/`withSudo` so the gated path is exercised without a real modal).
- `RecoveryCodesCard` — regenerate → display; no-TOTP `bad_request` → hint.
- `SecurityView` — cards render; revoke action opens `ConfirmDialog` →
  `withSudo(revoke)`.

## 9. Embed / CSP / done-gate

- CSP **unchanged**: QR is a `data:` img (`img-src 'self' data:`); clipboard and
  the `.txt` download (Blob URL + `<a download>`) need no inline script/style.
  Verify the built `dist/index.html` still has zero inline `<script>`/`<style>`.
- Quality bar (from review feedback): **no unnecessary wrapping** — badges
  `whitespace-nowrap`, long nicknames/UA-like fields truncate; aligned spacing.
- Done-gate: `go build/vet/test ./...` exit 0; `npm run test` green; `npm run
  build` clean + `pkg/webui/dist` committed; `cmd/smoke` `SMOKE_EXIT=0`. Manual:
  `mise dev-server` + `mise enroll-admin` → `/security` → add a passkey, set a
  password, enroll TOTP (scan QR), see recovery codes, regenerate, revoke.

## 10. Known limitations & deferred work

These are recommended by current best-practice research but are **out of scope
for this frontend-only slice** because the backend doesn't expose what they
need. Captured here (and in project memory) so they aren't lost.

- **No factor-status / codes-remaining read endpoint.** `/me` returns only the
  `SessionView`; there is no `GET /me/totp`, `/me/factors`, or recovery-code
  count. So the cards are **action-oriented** — they cannot show "TOTP: enabled",
  "Password: set", or "3 of 10 codes left" badges, and re-enroll/regenerate are
  presented unconditionally. **Future:** a `GET /me/factors` endpoint →
  per-factor status badges + remaining-codes count.
- **No AAGUID→provider mapping.** Passkey rows identify by nickname / 4-char
  suffix / created+last-used dates and the `backupState` sync badge — not by
  provider provenance ("iCloud Keychain", "Windows Hello", "1Password"), which
  research recommends. **Future:** an AAGUID→provider lookup table (backend or a
  bundled dataset) to render provenance + provider icons.
- **No WebAuthn Signal API on delete.** Deleting a passkey removes the server
  credential only; the credential may still appear in the user's provider and
  fail at next sign-in. The Signal API
  (`PublicKeyCredential.signalAllAcceptedCredentials`) is emerging and
  low-support. **Future:** call it post-delete where available; until then, copy
  can hint the user to remove it from their provider too.
- **Device-aware QR/secret emphasis** (QR-first on desktop, secret-first on
  mobile) is **not** implemented — we always show both (the accessible default).
  A later polish could adapt emphasis by viewport.
- **Coarse factor removal.** The only "disable" path removes password + TOTP +
  recovery together (`revoke-password-totp`); there is no granular "disable TOTP
  only". Presented honestly as one labeled destructive action. **Future:**
  granular per-factor disable endpoints if the product needs them.

## 11. Sources (research)

MFA/TOTP enrollment: [WorkOS](https://workos.com/blog/ux-best-practices-for-mfa),
[LogRocket 2FA flows](https://blog.logrocket.com/ux-design/2fa-user-flow-best-practices/),
[GitHub 2FA docs](https://docs.github.com/en/authentication/securing-your-account-with-two-factor-authentication-2fa/configuring-two-factor-authentication).
Recovery codes: [LogRocket 2FA recovery](https://blog.logrocket.com/ux-design/2fa-recovery-best-practices/),
[avoidthehack](https://avoidthehack.com/store-backup-codes), [login.gov](https://login.gov/help/create-account/authentication-methods/backup-codes/).
Passkey management: [web.dev](https://web.dev/articles/passkey-management),
[Passkey Central](https://www.passkeycentral.org/design-guidelines/optional-patterns/passkey-management-ui-best-practices-for-combining-all-passkey-types).
Destructive actions: [NN/g](https://www.nngroup.com/articles/proximity-consequential-options/),
[Smashing](https://www.smashingmagazine.com/2024/09/how-manage-dangerous-actions-user-interfaces/),
[DubBot](https://dubbot.com/dubblog/2025/designing-destructive-buttons-balancing-function-and-accessibility.html).
