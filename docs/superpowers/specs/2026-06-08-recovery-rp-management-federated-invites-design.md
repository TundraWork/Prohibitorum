# Spec — Account recovery + RP management + Federated invites (combined cycle)

**Date:** 2026-06-08
**Status:** approved — ready for writing-plans
**Predecessors:** Frontend rebuild Spec 1 / 2a / 2b / 2c / 3a — all shipped on `master`.

This is a **deliberately batched** cycle covering three backend-ready (or near-ready) areas, to
amortize the brainstorm/spec/finishing ceremony over more work while keeping the per-task TDD +
two-stage review bar. All three are frontend-heavy; the only backend change is one small additive
field (Section C). Three larger backend efforts are explicitly **out of scope** (see end).

The three sections are independent surfaces; they share only the established patterns
(`useApi`/`errorText`, `withSudo`, `ConfirmDialog`, `StatusBadge`, `CodeField`, `Table`, `data-test`,
the admin sidebar group + `requiresAdmin` routes from 3a).

---

## Section A — Account recovery (frontend; backend complete)

**Goal:** Let a user who has lost their TOTP authenticator recover via a backup recovery code and
re-enroll TOTP, all inline on `/login`. Closes a real lockout gap.

**Scope honesty:** only **password+TOTP** accounts can use this — the flow starts from the password
step (`/auth/password/begin` needs a password). Passkey-only and federation-only users cannot
self-recover; their recovery is the admin **reissue-enrollment** shipped in 3a. The UI must not imply
recovery is available to everyone.

**Backend contracts (verified — `pkg/server/handle_auth_password.go` + `handle_auth_recovery.go`):**
- `POST /api/prohibitorum/auth/password/begin {username, password}` → `{partial_session_token}` (5-min, single-use). *Already called by `PasswordTotpForm`.*
- `POST /api/prohibitorum/auth/recovery-code/verify {partial_session_token, code}` → `{recovery_session_token}` (10-min). **Consumes the partial token** (Pop) — a wrong code spends it, so the user must restart from the password step.
- `POST /api/prohibitorum/auth/recovery/totp/begin {recovery_session_token}` → `{secret_base32, otpauth_uri}` (preserves old recovery codes; `recovery_session_token` is NOT consumed here — re-`begin` is safe).
- `POST /api/prohibitorum/auth/recovery/totp/verify {recovery_session_token, code}` → `{recovery_codes: string[]}` **and sets the session cookie** (consumes the recovery token; wrong code → restart from password).

**Design:** a self-contained **`components/custom/AccountRecovery.vue`** (props: `partialToken: string`; emits `success`, `restart`):
- Phase `code`: a `CodeField`-style input (recovery code) → `recovery-code/verify`. On success → store `recovery_session_token`, advance to `reenroll`. On error → emit `restart` (the partial token is spent).
- Phase `reenroll`: on entry, call `recovery/totp/begin` → render **`TotpQr`** (otpauth → QR) + the secret + a code input → `recovery/totp/verify`. On success → store the returned `recovery_codes`, advance to `done`.
- Phase `done`: render **`RecoveryCodesDisplay`** (the new codes, copy/download, "I've saved them" gate) → on dismiss emit `success`.
- `errorText` via `errors.<code>` (`bad_credentials`, etc.).

**`PasswordTotpForm.vue` change:** in the `totp` phase, add a quiet **"Lost your authenticator?"** link. Clicking it swaps the TOTP input for `<AccountRecovery :partial-token="partialToken" @success="emit('success')" @restart="onRecoveryRestart" />`. `onRecoveryRestart` returns to the `password` phase and shows a message ("That code didn't work — please sign in again to retry."). `LoginView` already maps `@success` → `goReturnTo`.

**Tests:** `AccountRecovery.test.ts` (code→reenroll→done→success; restart on bad code; the begin→verify reenroll path with recovery codes shown) + extend `PasswordTotpForm.test.ts` (the link appears in the totp phase and mounts the recovery sub-flow).

**Reuses:** `TotpQr`, `RecoveryCodesDisplay`, `CodeField`, `useApi`, the 2b building blocks.

---

## Section B — Relying-party management (frontend; backend complete) — the planned Spec 3b

**Goal:** Admin pages for the two downstream relying-party types, on the 3a admin pattern (Table list →
detail/create; mutations `withSudo` + `ConfirmDialog`; reveal-once secrets via `CodeField`; added to the
`isAdmin` Admin sidebar group; `requiresAdmin` routes).

**Backend ops (all exist; mutations are `registerSudoOpHTTP` = admin+sudo; api.md is STALE — confirm exact list/get/update structs against `pkg/contract/auth.go` + handlers during planning):**

### B1 — OIDC clients — `pages/admin/AdminOidcClientsView.vue` (`/admin/oidc-clients`) + detail
- `GET /api/prohibitorum/oidc-clients` (list), `GET /oidc-clients/{clientId}` (get).
- `POST /oidc-clients` create — body `{clientId, displayName, redirectUris[], postLogoutRedirectUris[], scopes[], public:bool, requireConsent:bool}` → response reveals the **client secret once** (confidential clients); omitted for public.
- `PUT /oidc-clients/{clientId}` update (mutable config; does NOT touch secret).
- `POST /oidc-clients/rotate-secret` → new secret revealed once.
- `POST /oidc-clients/delete {clientId}`.
- **List:** Table (clientId, displayName, a public/confidential `StatusBadge`, disabled state). Row → detail.
- **Create:** form — clientId, displayName, redirect URIs + post-logout URIs (multi-line **`Textarea`**, one URI per line → split/trim), scopes (checkboxes for the standard set `openid`/`profile`/`email`, with `openid` checked-and-required), public toggle, requireConsent toggle. On success reveal the secret in a `CodeField` with a "copy it now, shown once" note.
- **Detail:** edit mutable config (PUT), **rotate-secret** (reveal-once), **delete** (ConfirmDialog, danger zone).

### B2 — SAML service providers — `pages/admin/AdminSamlProvidersView.vue` (`/admin/saml-providers`) + detail
- `GET /saml-providers` (list), `GET /saml-providers/{id}` (get).
- `POST /saml-providers` create — TWO paths (both supported this cycle):
  - **Metadata path:** `{metadataXML, kind?, displayName?, entityId?, nameIDFormat?, requireSignedAuthnRequest?, allowIdpInitiated?, wantAssertionsSigned?, sessionLifetimeSecs?}` (backend parses ACS + certs from XML).
  - **Manual path:** same fields minus `metadataXML`, plus `acs: [{binding, location, index, isDefault}]`.
- `PUT /saml-providers/{id}` update (config/flags).
- `POST /saml-providers/{id}/reingest-metadata {metadataXML}`.
- `POST /saml-providers/delete {id}`.
- **List:** Table (entityId, displayName, IdP-initiated badge). Row → detail.
- **Create:** a mode toggle — **"Paste metadata XML"** (a `Textarea`) vs **"Enter manually"** (displayName, entityId, nameIDFormat select, a **repeatable ACS rows** editor [binding select + location input + index + default radio], + the boolean flags). Submit posts the matching body shape.
- **Detail:** edit flags via PUT; **re-ingest metadata** (Textarea → reingest-metadata); **delete** (ConfirmDialog).

---

## Section C — Federated (OIDC) invites (small backend + frontend)

**Goal:** An admin can issue an invitation that must be redeemed by signing in through a chosen
**upstream OIDC IdP** (org onboarding). The enrollment machinery + invitee redemption already exist;
this exposes it.

**Backend change (verified gap — `pkg/server/handle_account.go`):** `handleCreateInvitation`'s
`createInvitationIn` body currently only carries `{role, attributes?}`; the `EnrollmentTemplate` it builds
omits `ExpectedUpstreamIDPSlug` (which `IssueEnrollment` already writes when set). Add an **optional
`expectedUpstreamIdpSlug *string`** to the request body and pass it into the template. Add a Go handler
test (invite created with a slug → enrollment row carries it). No migration, no new endpoint.

**Frontend:** the 3a **`AdminInvitationsView`** create form gains an optional **"Require sign-up via"**
`<select>` populated from `GET /api/prohibitorum/upstream-idps` (admin; options = slug + displayName,
plus a default "Any method"). On create, include `expectedUpstreamIdpSlug` only when chosen. The
invitations **list** gains a column showing the bound IdP's displayName (or "—" for any-method). The invitations API list item may
need the slug surfaced — confirm `InvitationView` includes it during planning; if not, a small
read-shape addition is in scope.

**Invitee side:** unchanged — `EnrollView` already detects `enrollment_federation_required` from
`register/begin` and redirects to `start-federation`.

---

## Cross-cutting

- **Vendor a `Textarea` primitive** into `components/ui/textarea/` (shadcn-vue style, token-styled like `Input` with `bg-sunken`) — needed for SAML metadata XML + OIDC URI lists.
- **i18n:** `recovery.*` (Section A), `admin.oidc.*` + `admin.saml.*` (Section B), `admin.invitations.*` additions for the IdP picker (Section C), `admin.nav.{oidcClients,samlProviders}`; plus any new `errors.*` codes reachable from the OIDC/SAML/SAML-metadata handlers (confirm during planning — e.g. invalid redirect URI, metadata parse failure, entityId conflict). Keep apostrophes curly (U+2019); **run the U+2018 grep after every `en.ts` edit** ([[en.ts apostrophe hazard]]).
- **Routes:** `/admin/oidc-clients`, `/admin/oidc-clients/:clientId`, `/admin/saml-providers`, `/admin/saml-providers/:id` — `requiresAdmin` children of `DashboardLayout` (the guard already enforces it). Account recovery adds no route (inline on `/login`).
- **Sidebar:** extend the Admin group with **OIDC clients** + **SAML providers** (order: Accounts · Invitations · OIDC clients · SAML providers). Icons: e.g. `AppWindow`/`Boxes` (OIDC), `Shield`/`KeyRound`-family (SAML) — verify lucide exports at build.
- **Done-gate (per prior slices):** vitest green; `go build ./... && go vet ./...` exit 0 (Section C touches Go); smoke `SMOKE_EXIT=0`; rebuild + commit `pkg/webui/dist` once at the gate. Section C's Go change gets a Go test; the smoke needs no new step (no regression) unless trivially adding a federated-invite assertion is cheap.

## Plan shape (~12–14 tasks, grouped)
1. Foundation: vendor `Textarea` + the full i18n blocks for all three sections.
2. `AccountRecovery.vue` + `PasswordTotpForm` link + tests (Section A).
3. `AdminOidcClientsView` list + create (reveal-once) + tests (Section B1).
4. OIDC client detail (edit/rotate-secret/delete) + tests (Section B1).
5. `AdminSamlProvidersView` list + create (metadata + manual ACS) + tests (Section B2).
6. SAML SP detail (edit/reingest/delete) + tests (Section B2).
7. Federated invites: backend `expectedUpstreamIdpSlug` (+Go test) + `AdminInvitationsView` IdP picker + list display + tests (Section C).
8. Wire routes + sidebar (oidc-clients, saml-providers).
9. Done-gate + dist.

(Plan may split 3–6 further; each list/create/detail is a coherent task.)

## Out of scope (separate future cycles — flagged, not forgotten)
- **D — OTP/password invites:** enrollment is passkey-only today; setting up password/TOTP at enrollment needs a schema column (credential requirements) + new enrollment ceremony endpoints + frontend. A real backend feature.
- **E — SAML-as-login subsystem:** there is no SAML *relying-party* login flow (we're only ever the SAML IdP); "invite/sign-in via SAML" requires building ACS callback + assertion validation + account linking + upstream-SAML config. A major backend effort.
- These two unblock "OTP invites" and "SAML invites" respectively; schedule deliberately.
