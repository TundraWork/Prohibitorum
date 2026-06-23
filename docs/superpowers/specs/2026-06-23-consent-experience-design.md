# Consent experience — mature OIDC consent, add SAML advisory consent — design

> Make the consent screen tell the user the truth ("approve once, then you're
> signed in automatically unless something changes"), and give SAML its own
> advisory acknowledgement so every protocol has a real, user-managed "connected"
> signal.

Status: approved (brainstorm 2026-06-23). This is **Spec 1 of two**; Spec 2
(*quickdial homepage refinement* — greeting, client-side search, connected-grid
vs. "Add app" picker, bundled backdrop) is deferred and will consume the unified
"connected" signal this spec produces. The homepage prototype does **not** block
on this spec, and this spec is independently valuable.

## Context & motivation

The end-user "My apps" launchpad (`2026-06-22-end-user-app-launchpad-design.md`)
shows every app the account is *authorized* to launch and marks the ones the user
has consented to with a check. A refinement we want is to split the home into
**connected** apps (already agreed) vs. **available to connect** (authorized but
not yet agreed), with an "Add app" affordance to connect more. That split needs a
per-app, user-managed "connected" signal.

- **OIDC** already has one: a consent record (`oidc_consent`), revocable, and the
  authorize flow re-prompts when scopes grow.
- **SAML** has none. `HandleSSO` (SP-initiated) and `HandleIdPInitiated` go
  straight from the RBAC check to issuing the assertion. SAML's protocol-level
  `Consent` attribute is an advisory message marker, not a stored, scoped,
  user-managed grant; attribute release is governed by the SP's admin-configured
  attribute map.
- **Forward-auth** has no interactive surface in its normal flow (pure proxy
  gate) and stays the exception: authorized = connected, no acknowledgement.

So this spec (a) **matures the OIDC consent screen** with honest, reassuring copy
and incremental-consent clarity, and (b) **adds an advisory SAML acknowledgement**
— a one-time "this app will sign you in and receive X — continue?" recorded per
(account, SP), revocable — interposed in both SAML SSO flows. It then (c) unifies
OIDC consents and SAML acks behind `/me/consent` so the App-access page and the
future homepage read one list.

## Verified facts (so the copy stays honest)

- **OIDC consent is remembered.** `HandleAuthorize` (`pkg/protocol/oidc/authorize.go`)
  satisfies consent when the stored grant covers every requested scope; it
  re-prompts only when a requested scope is **not** already granted, or the RP
  sends `prompt=consent`. Approving stores the **union** of old + new scopes
  (`handle_consent.go` → `UpsertConsent`). Trusted clients (`require_consent=false`)
  skip consent entirely.
- **SAML bounce-and-return is a proven pattern.** Both SAML handlers already
  bounce the browser to `/login` with `return_to` = the exact SSO URL (preserving
  the redirect-binding signed raw query **byte-for-byte**) for forced re-auth, and
  consume the single-use AuthnRequest replay ID **only after** that bounce. An
  advisory-consent bounce slots into the same place — no need to stash or rebuild
  the AuthnRequest.

## Decisions (settled in the brainstorm)

1. **SAML re-prompt policy: remembered until revoked.** Re-prompt only when no ack
   row exists (never acknowledged, or revoked). "Re-prompt if the SP's released
   attributes change" is a deferred enhancement (see *Non-goals*) — SAML attribute
   release is admin-config and changes rarely. The per-protocol copy stays honest:
   OIDC says "unless it asks for new permissions"; SAML says "unless you revoke it".
2. **The SAML screen shows the actual attributes** that will be released, derived
   from the SP's `attribute_map`, with a generic "your profile information"
   fallback when the map yields no friendly labels. Advisory only — no toggles.
3. **Declining on the SAML screen returns the user to their launcher home (`/`).**
   They remain signed in to the IdP; they simply don't enter that app. (No SAML RP
   error channel is used for an advisory decline.)
4. **OIDC incremental-consent highlight ships.** When re-prompting for *added*
   scopes, the screen frames it as "additional access" and marks the new scopes.
   (Small backend addition: the consent-context endpoint diffs requested vs.
   already-granted.)
5. **Management unification is in this spec.** `/me/consent` returns OIDC consents
   **＋** SAML acks (tagged by `kind`); revoke handles both; the App-access page
   lists and revokes both. The homepage (Spec 2) consumes this list.

## A. Mature the OIDC consent screen

Frontend + i18n only; the OIDC flow is unchanged except the small context-diff.

- **Reassurance copy** under the scope list: *"You're approving this once — next
  time {App} signs you in automatically, unless it asks for new permissions,"* and
  *"You can review or revoke this anytime in Settings → App access."*
- **Incremental-consent clarity:** when the user already has a grant and the RP
  requests *additional* scopes, title the screen *"{App} is requesting additional
  access"* and visually distinguish the **new** scopes from ones already granted.
- **Backend (small):** `handleConsentContextHTTP` (`pkg/server/handle_consent.go`)
  additionally loads the existing grant (`GetConsent`) and returns, per scope,
  whether it is already granted — e.g. add `alreadyGranted []string` (or a
  per-scope `isNew` flag) to `contract.ConsentContext`. No change to the
  ticket/decision flow or storage.

## B. SAML advisory consent

### Data model
- **New migration `db/migrations/022_saml_consent.sql`** (next incremental number;
  mirrors `oidc_consent`):
  ```sql
  CREATE TABLE saml_consent (
    account_id  integer NOT NULL REFERENCES account(id)  ON DELETE CASCADE,
    sp_id       bigint  NOT NULL REFERENCES saml_sp(id)   ON DELETE CASCADE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, sp_id)
  );
  ```
  Advisory ack = row present. `updated_at` is the "acknowledged on" shown in
  App access (parallel to consent's `GrantedAt`).

### Queries (`db/queries/saml_consent.sql`, sqlc)
- `UpsertSAMLConsent(account_id, sp_id)` — insert or bump `updated_at`.
- `GetSAMLConsent(account_id, sp_id)` — existence check for the interposition gate.
- `ListSAMLConsentsByAccount(account_id)` — join `saml_sp` for display name;
  returns `sp_id, entity_id, display_name, updated_at`.
- `DeleteSAMLConsent(account_id, sp_id)` — revoke (idempotent).

### Ticket helpers (`pkg/authn/saml_consent.go`, mirroring `consent.go`)
- `SAMLConsentTicket { AccountID int32; SPID int64; EntityID, DisplayName string;
  Attributes []string; ReturnTo string }`, KV key prefix `saml:consent:`, same
  10-minute TTL, single-use `Consume`.
- The **`ReturnTo` lives inside the ticket** (server-minted from the exact inbound
  SSO `RequestURI`), so the signed SAML query is never echoed through the browser
  for re-validation and cannot be tampered with — only the opaque nonce travels in
  the URL. `Attributes` are the friendly labels to render.

### Interposition (both SAML flows, `pkg/protocol/saml/`)
Placed **after** the per-app RBAC check and **before** the replay-ID consume /
assertion build, in both `HandleSSO` (sso.go) and `HandleIdPInitiated`
(sso_init.go):

1. `GetSAMLConsent(account, sp)` — if a row exists, proceed unchanged.
2. If absent: derive the attribute labels from `sp.AttributeMap`, mint a
   `SAMLConsentTicket` (with `ReturnTo` = the exact inbound SSO URL — for
   SP-initiated this preserves the signed raw query byte-for-byte, exactly like
   the existing re-auth bounce), and `302` to `/saml-consent?ticket=<nonce>`.
3. The replay-ID consume already happens after such bounces, so the returning
   request re-runs cleanly; on return the ack now exists and the flow issues.

`prompt=consent`-style forced re-ack has no SAML analog and is out of scope.

### Backend endpoints (session-gated, `pkg/server/handle_saml_consent.go`)
Mirror the OIDC consent endpoints:
- `GET  /api/prohibitorum/saml-consent?ticket=` → `{ sp{ id, displayName, logoUri },
  account{ displayName }, attributes[] }` (peek; `no_session`/invalid-ticket map
  like OIDC).
- `POST /api/prohibitorum/saml-consent` `{ ticket, decision: 'approve'|'decline' }`
  → `{ redirect }`. **approve** → `ConsumeSAMLConsent` + `UpsertSAMLConsent`,
  `redirect = ticket.ReturnTo`. **decline** → `ConsumeSAMLConsent`,
  `redirect = "/"` (launcher home). Audit both (`FactorSAMLSP`).

### Frontend (`dashboard/`)
- **Shared layout:** factor a small `ConsentCard.vue` (logo + heading + account
  line + an info list + approve/decline actions + policy/ToS footer) from the
  current `ConsentView`, so OIDC and SAML screens look identical.
- **`SamlConsentView.vue`** at route `/saml-consent` (public threshold route, like
  `/consent`): advisory heading *"{App} will use your account to sign you in,"* the
  read-only **attributes** list (*"{App} will receive:"*) with the generic
  fallback, the *"signed in automatically next time, unless you revoke this"*
  reassurance, and **Continue / Not now** buttons. `Continue`→approve,
  `Not now`→decline; both `hardRedirect` to the server-returned URL.
- **`ConsentView.vue`** updated per section A (reassurance + incremental highlight).

## C. Unify management (the "connected" signal)

- **`contract.ConsentedApp` gains `Kind string`** (`"oidc"` | `"saml"`). For OIDC
  rows, `clientId`/`scopes`/`grantedAt` are unchanged. For SAML rows, `clientId`
  carries the **SP id as a string** (matching how the launchpad already keys SAML
  apps, `kind:id`), `scopes` is the released-attribute labels (or empty), and
  `grantedAt` is `updated_at`. This keeps the SPA's `${kind}:${id}` matching intact.
- **`/me/consent`** (`handle_me_consent.go`) merges `ListConsentsByAccount`
  (tagged `oidc`) and `ListSAMLConsentsByAccount` (tagged `saml`), sorted by name.
- **`contract.RevokeConsentInput` gains `Kind`** (default `"oidc"` for back-compat).
  `POST /me/consent/revoke` routes to `DeleteConsent` or `DeleteSAMLConsent` by
  kind (account from session; idempotent).
- **App-access page (`AppAccessView.vue`)** lists both kinds (a small kind badge:
  OIDC scopes vs. SAML "signs you in") and revokes both via the same call.

## Security considerations

- **SAML acks change UX, not authorization.** Every SAML login still runs the full
  `HandleSSO`/`HandleIdPInitiated` path — signature/replay/RBAC/ACS checks are
  unchanged. The ack gate sits *after* RBAC, so it can never grant access an
  unauthorized user lacks; it only adds a one-time interstitial.
- **No signed-query exposure.** The inbound SAML `ReturnTo` is stored server-side
  in the ticket; only an opaque single-use nonce travels through the browser, so
  the redirect-binding signature is never re-validated from a client-supplied URL.
- **Single-use, account-bound tickets** (10-min TTL), matching OIDC consent.
- **Revoke** deletes only the caller's own row (account from session), idempotent,
  not sudo-gated (re-acknowledge on next sign-in — self-correcting, mirroring OIDC
  consent revoke).
- **No new data disclosure:** the attributes shown are exactly what the SP's
  attribute map already releases.

## Testing & gate

- **Go (unit):** `Upsert/Get/List/DeleteSAMLConsent` round-trips and cascade
  on account/SP delete; the SAML consent endpoints (peek context, approve writes +
  redirects to `ReturnTo`, decline → `/`, invalid/again-consumed ticket,
  cross-account isolation); the interposition in both handlers (no row → bounce to
  `/saml-consent`; row present → issues; ack write makes the re-run issue;
  unauthorized user never reaches the gate); the OIDC context-diff
  (`alreadyGranted`/`isNew`).
- **Smoke (`cmd/smoke`):** extend the SAML arc — first SSO with no ack → lands on
  the consent context; approve → ack recorded → SSO issues; `GET /me/consent`
  shows the SAML entry (kind `saml`); `POST /me/consent/revoke {kind:"saml"}` →
  gone → next SSO re-prompts. Numbered in the existing per-arc local-count style.
- **Frontend (vitest + vue-tsc):** `ConsentCard` shared rendering; `SamlConsentView`
  (attributes list + fallback, approve/decline redirects); `ConsentView` reassurance
  + incremental highlight; `AppAccessView` lists/revokes both kinds; en/zh parity
  (`locales.parity.test.ts`).
- **Green gate:** `go build -tags nodynamic ./... && go vet ./... && go test ./...`;
  `cd dashboard && npm test` + `npm run build`; live smoke `SMOKE_EXIT=0`; rebuild
  + commit `pkg/webui/dist`.

## Non-goals (explicit)

- **Re-prompt when a SAML SP's released attributes change** (the initial version
  is remembered-until-revoked). A later enhancement could store an attribute-set
  hash and re-ack on change, mirroring OIDC's scope-growth re-prompt.
- **Forward-auth acknowledgement** — it has no interactive surface in its proxy
  flow; it stays "authorized = connected".
- **The homepage connected/available split and "Add app" picker** — that is Spec 2.
- **Per-SP scoped attribute *selection*** (toggling which attributes are sent) —
  release is governed by the SP's admin attribute map; the screen is advisory.
- **SLO/session changes** — assertion issuance, `saml_session`, and SLO are
  untouched.
