# Native Steam (OpenID 2.0) upstream identity provider

Date: 2026-07-05
Status: Design approved (pending spec review)

## Problem

Users want "Sign in with Steam." Steam does **not** speak OAuth 2.0 or OpenID Connect for
public integrators — it implements the legacy, deprecated **OpenID 2.0** protocol. Steam has no
OIDC discovery document, no `client_id`/`client_secret`, no authorization-code/token endpoint, no
`id_token`/JWKS, no PKCE/nonce, and no `userinfo`/claims. A successful Steam login yields only a
**SteamID64** (via the OpenID 2.0 "Claimed ID" URL). Profile data (persona name, avatar) requires
a **separate Steam Web API call** (`ISteamUser/GetPlayerSummaries`) authenticated with a
server-side API key.

Our upstream federation (`pkg/federation/oidc`) is strictly OIDC: `NewClient` runs discovery,
`Exchange` does a code exchange + verifies a JWT `id_token` against JWKS, and `upstream_idp`
requires `issuer_url`, `client_id`, `client_secret_enc`, and `scopes` (all NOT NULL). Steam
satisfies none of these, so it cannot be added as a normal upstream OIDC IdP row. Confirmed by
research (Steamworks docs; the deprecation is not going to change) and by the codebase map.

## Goal

Add Steam as a **native upstream provider** — a second protocol adapter under the same
`upstream_idp` umbrella — reusing all of the protocol-agnostic downstream machinery
(`account_identity` linking, first-login confirmation + `/welcome`, session issuance, avatar
inheritance, admin CRUD, the login "Sign in with" buttons, self-service link/invite).

## Decisions (locked during brainstorming)

- **Full parity — all three modes.** Steam supports `auto_provision`, `invite_only`, and
  `link_only` (admin-configurable per row), exactly like OIDC IdPs. Email-less accounts are
  allowed; the email-verification gate is disabled for Steam rows.
- **Steam Web API key is required** for a Steam provider (encrypted in the existing secret slot).
  Auth itself doesn't need it, but persona name + avatar enrichment does, and accounts should have
  meaningful profiles.
- **Reuse `upstream_idp`** with a `protocol` discriminator (`oidc` | `steam`); do NOT create a
  parallel table (that would duplicate the `account_identity` / `account_avatar` FKs and the whole
  admin/login surface).
- **Login button:** a bespoke Steam button — our native `Button` styling, **black background with
  the Steam logo + text in white** (per Valve's brand guide), using the provided mono SVG recolored
  grey→white. Generic outline button remains for OIDC providers.
- **Hand-roll the OpenID 2.0 flow** (no general OpenID 2.0 library — those have documented
  spoofing CVEs and we need only this one narrow flow).

## Architecture — protocol adapter seam

`Federator` branches on `upstream_idp.protocol` at exactly **two** points:

1. **Begin** (`BeginLogin` → `begin`): build the redirect URL. OIDC → the existing discovery-driven
   `AuthURL`. Steam → an OpenID 2.0 `checkid_setup` redirect to
   `https://steamcommunity.com/openid/login`.
2. **Callback** (`HandleCallback`): verify + extract the identity tuple. OIDC → the existing code
   exchange + `id_token` verify. Steam → OpenID 2.0 `check_authentication` verification + Claimed-ID
   parse + `GetPlayerSummaries` enrichment.

Both adapters converge on a common result — `(upstream_iss, upstream_sub, email, displayName,
username, pictureURL, amr)` — after which **everything is reused unchanged**: `Resolve`/modes
(`pkg/federation/oidc/modes.go`), `account_identity` linking, `CreateConfirmGrant` + `/welcome`,
session issuance, the slug-based avatar-inheritance job, admin list/CRUD, `GET /auth/federation`,
`FederationButtons`, and self-service link/invite.

New package `pkg/federation/steam` holds the Steam-specific logic (OpenID 2.0 begin/verify + Web
API enrichment). It depends only on the stdlib + the existing hardened HTTP client and secret
crypto — it does NOT import `pkg/federation/oidc` internals.

### Shared identity tuple for Steam
- `upstream_iss = "https://steamcommunity.com/openid"` (fixed constant — keeps
  `UNIQUE(upstream_iss, upstream_sub)` and the `Resolve` lookup working).
- `upstream_sub = <SteamID64>` (17-digit).
- `email = NULL`; `require_verified_email` ignored for Steam.
- `displayName = personaname` (from `GetPlayerSummaries`).
- `username = "steam_" + <SteamID64>` (stable, unique, passes the account username charset rules;
  persona names are non-unique and may contain emoji so they are NOT used as the username).
- `pictureURL = avatarfull` (handed to the existing avatar-inherit job).
- `amr = ["steam"]` (OpenID 2.0 has no `amr`).

## Schema — migration `028_steam_protocol.sql`

```sql
-- +goose Up
ALTER TABLE upstream_idp
  ADD COLUMN IF NOT EXISTS protocol text NOT NULL DEFAULT 'oidc'
    CHECK (protocol IN ('oidc', 'steam'));

-- +goose Down
ALTER TABLE upstream_idp DROP COLUMN IF EXISTS protocol;
```

**Only the `protocol` discriminator is added — the OIDC-only columns stay `NOT NULL`.** A Steam row
stores empty sentinels in the columns that don't apply to it (`issuer_url=''`, `client_id=''`,
`scopes='{}'`); `protocol='steam'` is the authoritative "ignore these" signal, and handler
validation keeps them non-empty for `oidc` rows. This deliberately avoids `DROP NOT NULL`: the
sqlc-generated `UpstreamIdp` struct types those columns as non-null Go (`IssuerUrl string`,
`ClientID string`, `Scopes []string`), and relaxing the constraint would flip them to
`pgtype.Text`/pointers, rippling a breaking change through every OIDC read in
`client.go`/`federation.go`/`modes.go`. Empty sentinels keep the existing OIDC code untouched
(it never runs for `steam` rows) and shrink the migration to one additive column. This is the
concrete realization of the approved "inline in the shared table" decision — the OIDC columns still
live inline; they just hold `''`/`{}` (not NULL) for Steam rows.

`client_secret_enc` / `secret_nonce` / `key_version` are **reused verbatim** to store the encrypted
Steam Web API key (same `oidc.EncryptClientSecret` AES-256-GCM path, AAD = `upstream_idp:<id>:<ver>`).
No new crypto. The sqlc `upstream_idp` row struct gains only `Protocol string`; the OIDC column
types are unchanged. `protocol` is immutable after create (set once, like `slug`); the claim columns
(`username_claim`/`display_name_claim`/`email_claim`/`picture_claim`) keep their schema defaults for
Steam rows and resolve against a synthetic `tokens.Raw`, so no per-protocol claim handling is needed.

## `pkg/federation/steam` — the adapter

**`BuildAuthURL(realm, returnTo string) string`** — OpenID 2.0 `checkid_setup` redirect params:
`openid.ns=http://specs.openid.net/auth/2.0`, `openid.mode=checkid_setup`,
`openid.claimed_id` = `openid.identity` = `http://specs.openid.net/auth/2.0/identifier_select`,
`openid.return_to=<returnTo>`, `openid.realm=<realm>`. `realm` = the public origin;
`returnTo` = `{origin}/api/prohibitorum/auth/federation/{slug}/callback?state=<stateToken>`.

**`Verify(ctx, params url.Values) (steamID string, err error)`** — the security core:
1. Require `params["openid.mode"] == "id_res"` and that `openid.return_to` exactly equals our
   expected callback URL (defense against param tampering).
2. POST all `openid.*` params back to `https://steamcommunity.com/openid/login` with
   `openid.mode` replaced by `check_authentication`; require the response body to contain
   `is_valid:true`. (This is Steam's authoritative signature check — we do NOT re-implement DH.)
3. Parse `openid.claimed_id` with an **anchored** regex `^https://steamcommunity\.com/openid/id/(\d{17})$`
   → SteamID64. Reject anything else (this is the spoofing pitfall the research flagged).
Replay of the whole callback is already prevented by our single-use KV state token (`Pop`).

**`FetchSummary(ctx, apiKey, steamID string) (persona, avatarURL string, err error)`** —
`GET https://api.steampowered.com/ISteamUser/GetPlayerSummaries/v2/?key=<key>&steamids=<id>`,
parse `response.players[0].personaname` + `avatarfull`. Missing player → error (unknown/invalid id).

All Steam HTTP calls use a hardened client with timeouts (reuse the existing hardened dialer for
defense-in-depth; the two hosts — `steamcommunity.com`, `api.steampowered.com` — are fixed and
public, so this is belt-and-suspenders, not the primary control).

## Federator branch + callback plumbing

- `Federator.begin` reads `idp.Protocol`; for `steam`, skips the OIDC client/discovery entirely,
  mints the same KV state + anti-forgery cookie, and returns `steam.BuildAuthURL(realm, returnTo)`
  as the `AuthorizeURL`. The `FedState` for Steam omits OIDC-only fields (nonce, code_verifier,
  expected_token_endpoint) — they're simply unused.
- `handleFederationCallbackHTTP` passes the **full callback query** (`r.URL.Query()`) to
  `HandleCallback` (today it passes only `code`/`state`/`iss`). `HandleCallback` branches: for
  `steam`, it calls `steam.Verify(...)` + `steam.FetchSummary(...)`, decrypts the API key with the
  existing secret path, and builds the identity tuple; for `oidc`, the existing code path is
  unchanged. The single-use KV state `Pop` + browser-binding check run first for **both** protocols.
- The resulting tuple flows into the **existing** `Resolve`/mode dispatch. For Steam:
  `email = ""`/NULL, the `require_verified_email` gate is skipped, `username = steam_<id>`,
  `displayName = persona`. Avatar: the Steam path sets `tokens.Raw["picture"] = avatarfull` (the
  default `picture_claim` is `"picture"`) and calls `kickoffAvatarInherit(nil, idp, tokens,
  accountID)`. `runAvatarInherit` already guards `if pic == "" && client != nil`, so a non-empty
  `picture` means the nil OIDC client is never dereferenced — the existing SSRF-screened fetch →
  `avatar.Process` → `UpsertAvatarSource("upstream:<slug>")` → activate-gate pipeline runs unchanged.

## Admin — backend

Reuse `pkg/server/handle_admin_upstream_idps.go` with a protocol branch:
- Create/update body gains `protocol` (`oidc` default) and `apiKey` (Steam's secret-slot value).
- Validation: `oidc` → existing (issuer/client_id/client_secret/scopes required, issuer validated).
  `steam` → require `apiKey`; ignore/blank issuer/client_id/scopes/claim-mappings/verified-email;
  `slug`/`displayName`/`mode` still required; `mode` may be any of the three.
- Encrypt `apiKey` via the same `EncryptClientSecret` + `UpdateUpstreamIDPSecret` two-step used for
  `clientSecret`. Rotate-secret rotates the API key for Steam rows.
- `identityProviderView` gains `protocol` (and continues to exclude the encrypted secret).
- `GET /auth/federation` (public list) gains `protocol` per provider so the login page can pick the
  button style.

## Admin + login — frontend

- **Admin form** (`AdminUpstreamIdpsView.vue` create + `AdminUpstreamIdpDetailView.vue`): add a
  protocol selector (OIDC / Steam). Steam hides issuer/client-id/scopes/claim-mappings/verified-email
  and shows an **API key** field (password input, in the secret slot); reuses `EntityIconUpload`,
  set-disabled, rotate-secret (labeled "Rotate API key" for Steam), delete. `protocol` is read-only
  on the detail view (like `slug`).
- **Login button** (`FederationButtons.vue`): for `provider.protocol === 'steam'`, render a bespoke
  button — our native `Button` sizing/shape but **black background, white text**, with the Steam
  logo in white to its left. The mono SVG (`/mnt/e/Steam_Symbol_0.svg`, native grey `#C5C3C0`) is
  copied into `dashboard/src/assets/steam-logo.svg` with its fill set to `currentColor` so the
  button's white text color drives the logo color. OIDC providers keep the generic outline button.
  Same treatment reused on `ConnectedAccountsView.vue` (self-service link list).
- i18n: en + zh keys for the Steam admin fields + button label.

## Security considerations

- **Claimed-ID spoofing:** anchored regex + `check_authentication` (never trust a loose match or
  self-verify the signature). This is the single most important correctness property.
- **CSRF/replay:** reuse the existing single-use KV state + browser-bound anti-forgery cookie;
  verify `openid.return_to` equals the expected callback (state token embedded there).
- **SSRF:** Steam calls hit fixed public hosts; still route through the hardened client.
- **Secret at rest:** API key encrypted with the existing versioned AES-GCM path; never returned by
  any admin read.
- **Email-less accounts:** confirmed safe at the data layer — `account.email` is nullable and
  `applyAutoProvision` already stores `Email: {Valid: email != ""}` / `email_verified=false`, so an
  empty email becomes NULL with no code change. The `require_verified_email` gate only fires when the
  row's flag is set, so it's skipped for Steam. (`account.username` and `account.display_name` ARE
  `NOT NULL` — which is exactly why the persona name from the required API key is needed:
  `display_name = persona`, `username = steam_<id>`.) No email-based recovery path exists to break.

## Testing

- **`pkg/federation/steam` unit tests:** `Verify` against a mock Steam endpoint — valid `is_valid:true`,
  `is_valid:false`, tampered/mismatched `openid.return_to`, non-Steam host in claimed_id,
  malformed/short/long claimed_id (regex anchoring), and `check_authentication` transport error.
  `BuildAuthURL` param correctness. `FetchSummary` JSON parsing (present/absent player).
- **Federator branch tests:** `begin`/`HandleCallback` dispatch on `protocol` (steam vs oidc) with
  a stubbed steam adapter; the identity tuple maps correctly into `Resolve` (auto_provision creates
  `steam_<id>` + NULL email; link_only rejects unknown; invite_only redeems).
- **Admin handler tests:** create/update validation for `protocol=steam` (apiKey required; OIDC
  fields optional), secret encryption round-trip, rotate-secret.
- **Frontend vitest:** admin form protocol toggle shows/hides fields; `FederationButtons` renders the
  black Steam button for `protocol=steam` and the generic button otherwise.
- **Live smoke:** stand up a mock Steam server (login redirect target + `check_authentication` +
  `GetPlayerSummaries`) and run begin → callback verify → auto_provision → session; assert the
  account has `username=steam_<id>`, the persona display name, and a pending/stored avatar. (Real
  Steam can't be driven in CI.)

## Gate (Definition of Done)

`go build -tags nodynamic ./...`, `go vet ./...`, `go test ./...` clean; `vitest` green; `vue-tsc` 0;
`check-contrast` unchanged (the black Steam button must still meet contrast — white on black passes);
migration `028` applies; live smoke `SMOKE_EXIT=0` including the Steam mock arc; `dist` rebuilt +
committed.

## Out of scope

- Steam's partner-only OAuth 2.0 (requires a Valve-issued client ID; unavailable to general
  integrators).
- Steam ownership/entitlement checks (`CheckAppOwnership`), friends, inventory, etc.
- A general-purpose OpenID 2.0 client (we implement only the Steam flow).
- Migrating existing OIDC IdPs — they are untouched (`protocol='oidc'` default).
