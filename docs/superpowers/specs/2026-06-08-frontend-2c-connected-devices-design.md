# Spec 2c — Connected Accounts + Devices (incl. new-device pairing)

**Date:** 2026-06-08
**Status:** approved — ready for writing-plans
**Predecessors:** Spec 1 (threshold pages), Spec 2a (auth shell + sudo gate), Spec 2b (Security). All shipped on `master`.

This slice adds the user-facing surfaces for managing **federated identities** and
**device pairing**. It introduces three pages and one entry point, all built on the
patterns established by 2a/2b: `useApi` (`busy`/`error`/`run`), the sudo gate
(`withSudo`/`ensureSudo` + `SudoModal`), `ConfirmDialog`, `errors.<code>` i18n mapping,
`Card` stacks, and the per-slice done-gate.

---

## Backend contracts (verified against handlers — canonical)

Read directly from `pkg/server/handle_me_identities.go`, `pkg/server/handle_pairing.go`,
`pkg/server/handle_federation.go`, and `pkg/authn/errors.go` on 2026-06-08.

### Federated identities — `pkg/server/handle_me_identities.go`
- `GET /api/prohibitorum/me/identities`
  → `[{id:number, idpSlug:string, idpDisplayName:string, upstreamEmail:string|null, linkedAt:RFC3339}]`.
  Empty list serializes as `[]`, never `null`.
- `POST /api/prohibitorum/me/identities/{id}/unlink` → `204`. **Sudo-gated.**
  - Rejects removing the last sign-in method → `last_sign_in_method` (400).
  - `(id, account_id)` no match → `credential_not_found` (404).
- `GET /api/prohibitorum/me/identities/link/{slug}/begin?return_to=` → **`302` to upstream. Sudo-gated.**
  A redirect cannot be transparently retried after a `sudo_required`, so the FE must call
  `ensureSudo()` **proactively** and only then `hardRedirect(...)`.
- `GET /api/prohibitorum/me/identities/link/{slug}/callback` → `302` back to `return_to`.
  Not sudo-gated (completes the ceremony begun under sudo). Handled entirely server-side;
  the FE only needs to point `return_to` at `/connected`.

### Available providers to link — `pkg/server/handle_federation.go`
- `GET /api/prohibitorum/auth/federation` (public)
  → `[{slug:string, displayName:string}]` (`contract.FederationProvider`).
  Reuse this list for the "link a provider" picker; disable slugs already present in the
  linked-identities list.

### Device pairing — `pkg/server/handle_pairing.go`

Approver side (authenticated, the trusted device):
- `GET /api/prohibitorum/me/devices/pair/lookup?code=`
  → `{pairingId, displayCode, initiatorUa, initiatorIp, createdAt:RFC3339, expiresAt:RFC3339, alreadyBound:boolean}`.
  Rate-limited **20/min per account**. Not sudo-gated.
  - Missing/expired/consumed code, **or a code already bound to a different account**, all
    collapse to `pairing_not_found` (404) — no leak of code validity or ownership.
  - `alreadyBound === true` when the pairing is already approved **for this same account**.
- `POST /api/prohibitorum/me/devices/pair/approve {code}` → `204`. **Sudo-gated.**
  Rate-limited **10/min per account** (tighter than lookup).
- `POST /api/prohibitorum/me/devices/pair/cancel {code}` → `204`. Not sudo-gated.
  Code bound to a different account → `pairing_not_found`.

New-device side (anonymous, the device being added):
- `POST /api/prohibitorum/auth/devices/pair/begin`
  → `{pairingId:string, code:string, displayCode:"XXXX-XXXX", expiresAt:RFC3339}`. Anonymous.
- `GET /api/prohibitorum/auth/devices/pair/status?id={pairingId}`
  → `{status:"pending"|"approved"|"expired", expiresAt?:RFC3339}`. Anonymous, polled.
  Not-found collapses to `status:"expired"` (no leak of id validity).
- `POST /api/prohibitorum/auth/devices/pair/complete {pairingId}`
  → `{session: SessionView}` **and sets the session cookie**. Anonymous.
  - `pairing_not_approved` (428) if redeemed before approval.
  - `pairing_not_found` if the pairing was consumed/expired.

### Error codes reachable in this slice (`pkg/authn/errors.go`)
`last_sign_in_method` (400), `credential_not_found` (404), `pairing_not_found` (404),
`pairing_expired` (410), `pairing_not_approved` (428), `pairing_state` (409),
`rate_limited` (429, **already in `en.ts`**). All others map through the existing
`errors.<code>` fallback. Backend `AuthError` messages are Chinese, so every reachable
code needs an English `errors.<code>` entry.

---

## Pages

### 1. `pages/ConnectedAccountsView.vue` — route `/connected` (requiresAuth)

**List.** On mount, `api.get('/api/prohibitorum/me/identities')`. Each row: provider
display name, optional upstream email, linked date (formatted). Apply the `min-w-0` /
`truncate` chain on long values (every flex ancestor of a `truncate` child needs `min-w-0`,
fixed siblings `shrink-0` — the SessionsView rule). Empty state when no identities.

**Unlink.** Per-row action → `ConfirmDialog` → `withSudo(() => api.post('.../{id}/unlink'))`.
On success, refresh the list. Map `last_sign_in_method` and `credential_not_found` through
`errors.<code>`.

**Link.** Fetch `GET /auth/federation`; render a control listing providers, **disabling**
any slug already present in the linked list. On select:
`ensureSudo()` → if elevated, `hardRedirect('/api/prohibitorum/me/identities/link/{slug}/begin?return_to=/connected')`.
The proactive `ensureSudo()` is required because `/begin` is a sudo-gated redirect that
`withSudo`'s XHR-retry cannot replay.

### 2. `pages/DevicesView.vue` — route `/devices` (requiresAuth, approver side)

**Explainer + code entry.** Short explainer of what approving does, then a plain shadcn
`Input` for the `XXXX-XXXX` code (note: `CodeField` is a read-only copy widget, not an
input). Submit → `api.get('.../me/devices/pair/lookup?code=' + encoded)`.

**Confirmation card.** On lookup success, show initiator UA / IP / created / expires and
echo the returned `displayCode` via `CodeField` so the user can compare it against the
other device. Two actions:
- **Approve** → `withSudo(() => api.post('.../me/devices/pair/approve', {code}))` → success
  state ("Device approved — it will be signed in shortly").
- **Cancel** → `api.post('.../me/devices/pair/cancel', {code})` → reset to entry.

**Edge handling.** `alreadyBound === true` → show an "already approved" note, no Approve
button. `rate_limited` and `pairing_not_found` map through `errors.<code>`.

The lookup → confirm → approve sequence *is* the confirmation; sudo supplies the friction,
so no extra `ConfirmDialog` is used here.

### 3. `pages/PairDeviceView.vue` — route `/pair` (public, `CenteredLayout`)

The new-device side. State machine:

1. **Begin.** On mount, `POST /auth/devices/pair/begin`.
   Display the `displayCode` prominently with instructions: "On a device where you're already
   signed in, open **Devices** and enter this code." Show a countdown to `expiresAt`.
2. **Poll.** `GET /auth/devices/pair/status?id={pairingId}` every ~2.5s via `setInterval`.
   The interval is cleared on unmount, on `approved`, and on `expired`. Guard against
   overlapping in-flight polls.
   - `pending` → keep polling.
   - `expired` → expired state + **"Generate a new code"** button (re-run begin).
   - `approved` → `POST /auth/devices/pair/complete {pairingId}` (sets the session cookie),
     then advance to the success step. Handle `pairing_not_approved` / `pairing_not_found`
     by returning to an error/expired state with a retry.
3. **Success + optional passkey.** "This device is now signed in." Offer
   **"Add a passkey to this device"** — `useWebauthn().register()` +
   `POST /me/credentials/register/begin` → `register/complete` (the device now holds a
   session, so these authenticated calls succeed). A **Skip** action and the post-register
   path both navigate to `/` (the dashboard). The device works for the session regardless;
   the passkey is what makes it work standalone next time, hence offered-but-skippable
   (not all devices/browsers have a usable authenticator).

### 4. Login entry point — `pages/LoginView.vue`

Add a secondary action — **"New device? Pair it"** — as a `RouterLink to="/pair"`, placed
below the existing sign-in options. Quiet/utility styling; it is not a primary CTA.

---

## Shell, routing, i18n, gate

**Router (`router/index.ts`).**
- Add `{ path: 'connected', name: 'connected', component: ConnectedAccountsView }` and
  `{ path: 'devices', name: 'devices', component: DevicesView }` as children of the
  `DashboardLayout` route (inherits `requiresAuth`).
- Add `{ path: '/pair', name: 'pair', component: PairDeviceView, meta: { public: true } }`
  as a top-level public route (sibling of `/login`).

**Sidebar (`components/custom/AppSidebar.vue`).** Extend `accountItems` to the order
**Profile · Security · Sessions · Connected · Devices**. Labels `nav.connected = 'Connected'`,
`nav.devices = 'Devices'`. Pick lucide icons consistent with the set (e.g. `Link2` for
Connected, a device glyph for Devices).

**i18n (`locales/en.ts`).** Add `connected.*`, `devices.*`, `pair.*` namespaces plus the
`errors.*` additions listed above. Keep apostrophes curly (U+2019). `rate_limited` already
exists — do not duplicate.

**Tests.** Each new view gets a `*.test.ts` matching the depth of existing view tests
(mock `api`, assert list render, unlink/approve/link flows, sudo-gate interaction, error
mapping, and — for `/pair` — the poll→approved→complete transition and the skippable
passkey offer). Extend `AppSidebar.test.ts` for the two new nav items.

**Done-gate (identical to prior slices).**
- `mise exec -- npm run test` from `dashboard/` → vitest green (all suites).
- `mise exec -- go build ./... && go vet ./...` → exit 0.
- Smoke: `setsid bash /tmp/run_v06.sh`; poll `/tmp/v06.result` for `SMOKE_EXIT=0`.
- Rebuild + **commit** `pkg/webui/dist` once at the gate (Vite hashes are non-deterministic;
  for source-only intermediate commits, `git checkout -- pkg/webui/dist && git clean -fq pkg/webui/dist`).

---

## Plan shape (~5 tasks)

1. `ConnectedAccountsView.vue` + tests (list, unlink via sudo, link via proactive `ensureSudo`+redirect).
2. `DevicesView.vue` + tests (approver: input → lookup → confirm card → approve/cancel; `alreadyBound`, rate-limit, errors).
3. `PairDeviceView.vue` + tests (begin → poll → complete state machine + skippable passkey offer; interval cleanup).
4. `LoginView` entry link + routes (`/connected`, `/devices`, public `/pair`) + sidebar nav + full i18n (`connected.*`/`devices.*`/`pair.*` + `errors.*`).
5. Done-gate: full vitest, `go build/vet`, smoke `SMOKE_EXIT=0`, rebuild + commit `pkg/webui/dist`.

## Out of scope
- Persistent device list / device management (pairing issues a session that shows under
  `/sessions`; there is no separate device registry).
- Any backend change — all endpoints already exist.
- The deferred backend items tracked separately (factor status, AAGUID→provider mapping,
  WebAuthn Signal API) remain out of scope.
