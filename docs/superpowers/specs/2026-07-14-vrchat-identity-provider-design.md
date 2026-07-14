# VRChat upstream identity provider and searchable identity data

Date: 2026-07-14
Status: Design approved (pending written-spec review)

## Problem

Prohibitorum supports OIDC and Steam upstream identity providers. VRChat does not offer OAuth or OpenID Connect to public developers. Its unofficially documented login endpoint accepts account credentials directly, but VRChat's published Creator Guidelines explicitly say third-party applications must not request or store users' credentials, auth tokens, or session data, and must not act on behalf of users from a server.

A server-side credential relay is therefore not an acceptable design. VRChat does expose public profile data, including a stable `usr_…` ID, display name, and `bioLinks`, through `GET /api/1/users/{userId}`. That endpoint still requires an authenticated VRChat session. The approved design uses a dedicated operator verification account only to read public profiles. End users prove control of a VRChat profile by temporarily placing a one-time Prohibitorum URL in `bioLinks`.

The current federation architecture is also OIDC-owned: Steam is implemented as protocol branches inside `pkg/federation/oidc`. Adding another multi-step non-OIDC ceremony there would deepen the coupling. This feature will instead perform a breaking, clean-cutover refactor to a protocol-neutral provider registry. The project has no production deployment, so API and internal compatibility shims are explicitly unnecessary.

Finally, `account_identity` stores only subject and email. Admin account search is client-side and limited to the current page. It cannot reliably find an account by a Steam ID, Steam persona, VRChat user ID, or VRChat display name. The identity row needs bounded, provider-owned metadata and the admin list needs server-side unified and advanced filters.

## Goals

1. Add VRChat as a native upstream identity provider without collecting end-user VRChat credentials or session data.
2. Require a fresh, browser-bound, one-time bio-link proof for every VRChat sign-in and explicit identity-link operation.
3. Preserve all three provisioning modes: `auto_provision`, `invite_only`, and `link_only`.
4. Support an admin wizard that establishes and renews a dedicated VRChat operator verification session, including no-2FA, TOTP, email OTP, and recovery-code paths.
5. Refactor OIDC, Steam, and VRChat behind a protocol-neutral adapter state machine and shared identity resolver.
6. Store allowlisted upstream-specific identity data on `account_identity` and refresh it after successful authentication.
7. Add server-side unified account search and precise provider/field filters that operate before pagination.
8. Keep all credentials, codes, cookies, proof tokens, and encrypted secret bytes out of read APIs, logs, diagnostics, and audit details.

## Non-goals

- End-user VRChat username/password, auth-cookie, or 2FA relay.
- OAuth/OpenID emulation for VRChat.
- A persistent VRChat proof link or proof reuse across sign-ins.
- Automatic editing or removal of a user's VRChat profile.
- Background profile polling.
- Storing the complete VRChat user response or unverified VRChat account username. VRChat no longer exposes other users' account usernames; the verified name is the mutable display name.
- VRChat write operations, social graph access, avatars/worlds management, or other API automation.
- Live VRChat credentials in tests or CI.
- Backward-compatible admin payloads, internal package aliases, or deprecated schema columns.

## Locked decisions

- The ownership proof is an exact Prohibitorum URL temporarily placed in VRChat `bioLinks`.
- Every VRChat sign-in uses a fresh proof. After success, the UI tells the user to remove the link.
- `link_only` remains an ordinary per-provider provisioning mode. A known linked identity may sign in after fresh proof; an unknown identity must first be linked from an authenticated local account.
- `auto_provision` asks the user to choose the local Prohibitorum username. The verified VRChat display name initializes the local display name. Invite-only flows use the invitation template username instead.
- Prohibitorum stores the verified VRChat user ID, display name, and canonical profile URL. It does not invent or accept an unverified VRChat username field.
- An admin establishes a dedicated operator verification-account session through a Prohibitorum wizard. Operator credentials and 2FA codes are transient; the resulting cookie jar is encrypted and reused.
- Admin account lookup provides both a unified search and advanced provider/field filters.
- OIDC, Steam, and VRChat are migrated into a full provider-plugin architecture rather than adding another branch to the OIDC federator.

## External constraints

VRChat's API is unofficial and may change without notice. The adapter must:

- identify itself as `Prohibitorum/<build-version> <public-origin>` using `User-Agent` on every VRChat request;
- use only the fixed `https://api.vrchat.cloud/api/1` origin in production;
- issue requests only in response to an explicit user/admin action;
- honor `429` and `Retry-After` and maintain shared per-provider backoff;
- reuse the operator session rather than repeatedly creating VRChat sessions;
- display an admin warning that the integration is unofficial and server-side operator-session use may carry account/moderation risk under VRChat's guidelines.

Relevant sources:

- VRChat current-user/login endpoint: <https://vrchat.community/reference/get-current-user>
- VRChat public user endpoint: <https://vrchat.community/reference/get-user>
- VRChat TOTP verification: <https://vrchat.community/reference/verify2fa>
- VRCX authentication implementation used as protocol-behavior reference: <https://github.com/vrcx-team/VRCX/blob/master/src/stores/auth.js>
- VRChat Creator Guidelines, API Usage / Bots: <https://hello.vrchat.com/creator-guidelines#api-usage>

## Architecture

### Protocol-neutral federation core

Create `pkg/federation` as the sole owner of:

- login, link, and invite flow intent;
- browser anti-forgery binding and single-use state;
- upstream-provider lookup and disabled checks;
- shared provisioning and relogin resolution;
- first-login confirmation grants;
- local session issuance inputs;
- disabled-account enforcement;
- identity and avatar persistence handoff;
- protocol-neutral audit events and error mapping.

Move the protocol implementations behind a registry:

- `pkg/federation/providers/oidc`
- `pkg/federation/providers/steam`
- `pkg/federation/providers/vrchat`

The old `pkg/federation/oidc` orchestration path is removed after OIDC and Steam callers migrate. OIDC-specific discovery, token verification, claims, and hardened HTTP behavior remain in the OIDC adapter. Steam OpenID 2.0 and player-summary behavior remain in the Steam adapter. VRChat operator-session and profile-proof behavior live only in the VRChat adapter.

### Adapter state machine

Adapters implement this state-machine contract rather than a redirect-only contract:

```go
type Adapter interface {
    Protocol() string
    Descriptor() Descriptor
    ValidateConfig(json.RawMessage) error
    Begin(context.Context, Provider, BeginContext) (AdapterState, NextAction, error)
    Advance(context.Context, Provider, AdapterState, ActionInput) (AdvanceResult, error)
}
```

`NextAction` is a discriminated value such as external redirect, local form, bio-link instruction, or completion. `AdvanceResult` contains either updated opaque adapter state plus the next action, or a verified identity. The federation core persists the opaque adapter state inside its existing short-lived KV flow envelope; adapters never own local-account or session policy.

Protocol behavior:

- OIDC: begin returns the authorization redirect; callback advances to a verified identity.
- Steam: begin returns the OpenID redirect; callback verification and summary fetch advance to a verified identity.
- VRChat: begin returns a local identifier form; preparation returns the one-time bio-link instruction; explicit profile verification advances to a verified identity.

These existing public entry points remain:

- `GET /auth/federation/{slug}/login`
- `GET /auth/federation/{slug}/callback`
- `GET /enrollments/{token}/start-federation`
- `GET /me/identities/link/{slug}/begin`
- `GET /me/identities/link/{slug}/callback`

OIDC and Steam login/invite/link begin routes redirect externally. VRChat login/invite/link begin routes redirect to the local browser-bound challenge page. OIDC and Steam complete through their existing callback routes; VRChat completes through the local flow actions defined below.

### Verified identity contract

Every adapter produces a bounded `VerifiedIdentity` with:

- provider row ID and slug;
- canonical issuer namespace;
- stable upstream subject;
- provisioning username or selected local username where applicable;
- display name;
- optional email and verified-email state;
- AMR values;
- optional avatar URL;
- allowlisted `UpstreamData map[string]any`.

Canonical VRChat values:

- issuer: `https://api.vrchat.cloud`
- subject: exact returned `usr_…` ID
- selected username: user-supplied local username for `auto_provision` only
- display name: returned VRChat `displayName`
- email: absent/unverified
- AMR: `vrchat`, `profile_proof`
- upstream data: `userId`, `displayName`, `profileUrl`

Canonical Steam data adds at least `steamId` and `personaName`; the stable subject remains SteamID64. OIDC data includes the configured upstream username and display-name claims, while subject and email remain first-class identity columns.

### Shared resolver

Extract provisioning policy from the OIDC package into the federation core. One resolver handles:

1. Existing `(issuer, subject)` identity: require the identity's provider row to match, refresh changed display/email/upstream data, and return its account.
2. `auto_provision`: validate the adapter-provided or user-selected local username, apply provider gates, create account + identity atomically, and return first-login confirmation.
3. `invite_only`: require and atomically consume the expected provider-bound invitation, create from the invitation template, insert identity/upstream data, and return confirmation.
4. `link_only`: reject an unknown identity during login; known identities continue normally.
5. Explicit authenticated link: verify browser/account binding, refuse identities linked elsewhere, and insert the identity for the current account atomically.

The current unique identity namespace `(upstream_iss, upstream_sub)` remains. Configuring multiple rows for the same protocol does not allow the same upstream identity to bind to multiple accounts or bypass provider-specific provisioning policy.

## Data model

### Protocol-neutral provider rows

Replace OIDC-specific inline provider columns with:

```text
upstream_idp
  id
  slug
  display_name
  protocol
  mode
  provider_config jsonb
  secret_enc
  secret_nonce
  key_version
  secret_status
  secret_validated_at
  disabled
  created_at
```

`provider_config` is object-checked and validated by the registered adapter on every create/update and again before use. Common lifecycle fields remain relational. Protocol-specific examples:

- OIDC: issuer URL, client ID, scopes, allowed domains, claim mappings, verified-email policy, private-network policy.
- Steam: currently no public configuration beyond common fields; its API key occupies the encrypted secret payload.
- VRChat: no admin-configurable API origin; the adapter derives its identifying User-Agent from the build version and public origin.

Rename `client_secret_enc` to `secret_enc`; retain the existing nonce, key version, AES-256-GCM scheme, and row/version-bound AAD. The adapter defines the plaintext envelope:

- OIDC client secret string;
- Steam API key string;
- VRChat JSON cookie jar.

`secret_status` is `unconfigured`, `configured`, `valid`, or `invalid`; `secret_validated_at` records the last successful adapter validation. Existing OIDC/Steam rows migrate to `configured`, while a completed VRChat operator wizard sets `valid` and a later operator-session `401/403` sets `invalid`. Read contracts expose those derived health fields and `secretConfigured` only. They never serialize plaintext or ciphertext.

### Upstream identity data

Add to `account_identity`:

```sql
upstream_data jsonb NOT NULL DEFAULT '{}'::jsonb
```

Requirements:

- value must be a JSON object;
- adapter descriptors define permitted keys and scalar types;
- keys, string lengths, and total encoded size are bounded before persistence;
- no credentials, tokens, cookies, proof URLs, raw upstream responses, or unapproved claims;
- data is inserted in the same transaction as a new identity and refreshed after every successful authentication;
- a GIN index supports exact containment filters; subject and email keep their existing relational indexes/columns.

Metadata belongs to `account_identity`, not `account.attributes`, because it describes one upstream identity and an account may have several providers.

### Migration

A single clean-cutover migration:

1. Adds `provider_config`, common secret-health columns, and `account_identity.upstream_data` with checks/indexes.
2. Renames the encrypted secret column generically without decrypting or rewriting ciphertext.
3. Backfills OIDC config JSON from current OIDC columns.
4. Backfills Steam config and `upstream_data.steamId` from existing Steam subjects.
5. Drops obsolete OIDC-only provider columns and their old checks/defaults.
6. Replaces the protocol check with the registry's initial supported values: `oidc`, `steam`, `vrchat`.

There is no production deployment and no compatibility requirement. Nevertheless, existing development/test rows must migrate without losing provider secrets or linked identities. SQLC is regenerated once after the schema/query cutover.

## VRChat operator-session wizard

### Setup and renewal

A newly created VRChat provider starts disabled with `secret_status='unconfigured'`. It is not usable until the admin completes the operator-session wizard; only then can the admin enable it.

The sudo-gated wizard has two phases:

1. **Start:** accept operator username/password over the existing protected admin API. URL-encode each component before constructing the Basic value, call `GET /auth/user`, capture only cookies scoped to the expected VRChat API host, and discard credentials before returning.
2. **Challenge:** if the response has `requiresTwoFactorAuth`, expose only the returned supported methods: TOTP, email OTP, and recovery OTP. Submit the code to the corresponding fixed endpoint using the temporary cookie jar. Recovery-code formatting follows the endpoint contract rather than TOTP formatting.

Temporary operator cookies are sensitive session data. Before multi-step storage, seal the cookie-jar envelope with the active DEK and bind AAD to provider ID, key version, and challenge ID. Store only the sealed value in short-lived shared KV. Bind the challenge to the acting admin account and current browser/session; every step remains sudo-gated.

After successful 2FA, call the current-user endpoint again with cookies only. Require a full authenticated user object, retain only expected secure VRChat cookies, seal the final cookie jar into `upstream_idp.secret_enc`, and record non-secret health metadata such as configured state and last successful validation time. Passwords and codes are never persisted, retried automatically, or included in audit detail.

### Health behavior

Before a profile lookup, the adapter decrypts the operator jar and uses it against the fixed API host. If VRChat returns `401` or `403`:

- fail the user flow as temporarily unavailable;
- mark provider health as requiring operator re-authentication;
- emit a secret-free diagnostic/admin indication;
- retain the encrypted jar for forensic/renewal context rather than silently replacing it;
- never ask the end user for VRChat credentials.

Admin reads show configured/healthy/re-authentication-required state and the last validated timestamp, not the operator username unless it was returned as intentionally displayable non-secret metadata.

## VRChat sign-in and linking ceremony

### Start and preparation

1. The user selects the VRChat provider from login, invite redemption, or authenticated identity linking.
2. The federation core creates a short-lived intent flow bound to the initiating browser. Login/invite flows use the existing anti-forgery cookie hash. Link flows also bind the current local account/session.
3. The local VRChat challenge page accepts either:
   - an exact `usr_…` ID; or
   - a canonical `https://vrchat.com/home/user/usr_…` profile URL.
4. The server parses and validates the identifier. It never fetches a user-supplied URL and never changes the fixed API origin.
5. For `auto_provision`, the page also asks for the desired local username and validates it using the existing account username rules. Invite flows use the invitation template. Known/link-only flows do not need a new local username.
6. Preparation mints a separate cryptographically random proof token and records only its binding in the flow state.

The displayed proof URL is same-origin and path-only, for example:

```text
https://id.example.test/verify/vrchat/<proof-token>
```

It contains no account ID, provider slug, return target, email, username, or invitation token.

### Verification

The UI does not poll. The user adds the proof URL to VRChat `bioLinks` and presses **Verify profile**.

The server:

1. verifies the flow/browser/account binding and per-flow retry window;
2. enforces any shared per-provider backoff before making a VRChat request;
3. calls `GET /users/{userId}` with the encrypted operator session and required User-Agent;
4. bounds and parses the response;
5. requires the returned ID to equal the requested ID exactly;
6. canonicalizes each returned bio link and requires scheme, effective host/port, decoded path, and proof token to match the issued URL, with no userinfo, query, or fragment;
7. builds the verified identity from only the approved response fields;
8. advances through the shared resolver;
9. consumes the proof/flow after successful evidence and resolution.

A failed "link not present" check leaves the challenge retryable until its ordinary federation-state expiry. Explicit retries are throttled. On success, the completion page tells the user to remove the bio link. Removal cannot be enforced, but the old token is consumed and cannot authenticate a later flow.

Every later VRChat sign-in repeats this process with a new proof URL. A linked identity alone is not sufficient evidence for a new session.

### Public proof-link page

Opening `/verify/vrchat/<proof-token>` never verifies or consumes a flow. It returns a small public page with `Cache-Control: no-store` and `Referrer-Policy: no-referrer` explaining:

- this is a one-time Prohibitorum VRChat ownership-verification link;
- visiting it does not sign anyone in and does not approve access;
- the profile owner should return to Prohibitorum and press **Verify profile**;
- the owner should remove the link after successful verification.

Expired, unknown, and consumed proof tokens return the same generic explanation. The page does not reveal whether a live flow exists.

## API contracts

### Provider administration

Admin create/update payloads use a discriminated shape:

```json
{
  "slug": "vrchat",
  "displayName": "VRChat",
  "protocol": "vrchat",
  "mode": "link_only",
  "config": {}
}
```

OIDC and Steam use the same outer shape with adapter-specific `config`; secret inputs remain write-only operations. VRChat operator-session actions are `POST /identity-providers/{slug}/operator-session/start`, `POST /identity-providers/{slug}/operator-session/verify`, and `POST /identity-providers/{slug}/operator-session/validate`. All three are sudo-gated raw handlers under the existing admin API prefix.

Provider reads include:

- common provider fields;
- validated public config;
- `secretConfigured`;
- adapter health/status;
- adapter descriptor data needed by the dashboard, including searchable identity fields and supported operators.

The provider protocol and slug are immutable after creation.

### Federation challenge actions

The local challenge UI reads `GET /auth/federation/flows/{flow}` and advances only the action named by the server through `POST /auth/federation/flows/{flow}/prepare` or `POST /auth/federation/flows/{flow}/verify`. Browser-bound flow handles are opaque. The server determines protocol, allowed next action, and intent from KV state; clients cannot choose another adapter or skip a step by changing request fields.

OIDC/Steam callbacks continue through `/auth/federation/{slug}/callback`. VRChat uses the two local POST actions above. Successful login follows the existing confirmation/session/return-target behavior; successful explicit linking returns to connected accounts without minting a new session. The public explanatory route is `GET /verify/vrchat/{proof}` outside the admin/API route group.

### Accounts and identities

Account list/detail views gain bounded identity summaries containing provider slug/display name/protocol, subject, email, linked time, and approved upstream data. Self-service identity views expose only useful identity labels and metadata. Operator-session status is admin-provider data and is never attached to user identities.

`GET /accounts` gains:

- `q`: unified, case-insensitive search across local username, display name, email, upstream subject/email, and adapter-approved string/number metadata;
- `provider`: optional provider slug;
- `field`: optional descriptor-approved upstream field;
- `value`: advanced filter value;
- `match`: descriptor-approved operator such as exact or contains.

Rules:

- filters are parameterized and applied before keyset pagination;
- provider/field/operator combinations are validated through adapter descriptors, not arbitrary JSON paths;
- exact ID searches use relational subject/provider predicates;
- JSONB containment supports exact metadata filters;
- bounded contains search is acceptable for this small-org product, with query plans verified against representative data;
- changing any filter resets the dashboard cursor/page stack.

## Dashboard behavior

### Admin provider screens

The provider list/create/detail UI becomes protocol-discriminated rather than rendering OIDC fields for all protocols.

VRChat detail includes:

- provisioning mode and enable state;
- operator-session state and last validation;
- **Set up** / **Re-authenticate operator account** action;
- username/password step followed by only the required VRChat 2FA choices;
- clear notice that credentials/codes are not retained but the resulting VRChat session is encrypted and reused;
- VRChat unofficial-API and moderation-risk warning;
- no generic rotate-secret text, because session renewal is a ceremony rather than pasting a secret.

Enable is unavailable while no valid operator session exists. A session that later expires surfaces as provider health requiring re-authentication.

### End-user VRChat challenge

The local page provides:

- VRChat ID/profile URL input;
- local username input only for unknown `auto_provision` flows;
- copyable proof URL;
- concise numbered bio-link instructions;
- explicit **Verify profile** button;
- visible challenge expiry and retry/backoff time;
- success reminder to remove the link;
- accessible error states for invalid identifier, link missing, expired flow, identity conflict, username conflict, link-only rejection, operator session unavailable, upstream throttling, and temporary upstream failure.

The page never asks for a VRChat password, cookie, auth token, or 2FA code.

### Account filtering and identity display

Replace the current page-only computed filter in `AdminAccountsView.vue` with debounced server search. Add optional provider, field, value, and operator controls. Reset pagination when filters change and cancel/ignore stale requests.

Rows show matched identity context so an admin can see why a result matched, for example:

```text
VRChat · Display Name · usr_…
Steam · Persona Name · 7656…
```

Account detail shows all linked identities and verified metadata read-only. Metadata editing remains adapter-owned; admins cannot forge upstream data through account edit APIs.

English and Chinese translations cover all new labels, instructions, warnings, and error states. VRChat uses the existing configurable provider-icon mechanism and generic accessible provider button; this feature does not bundle or restyle a VRChat trademark asset.

## Security and privacy

### Secrets

- End-user VRChat credentials/session data are never accepted.
- Operator username/password and OTP exist only in request-scoped memory.
- Temporary and persistent operator cookies are DEK-sealed; plaintext never enters KV, logs, audit, diagnostics, or API output.
- Secret AAD binds provider row, key version, and temporary challenge where applicable.
- Proof tokens and full proof URLs are secrets for logging purposes even though the bio link becomes public temporarily.
- API errors never include raw VRChat response bodies or headers.

### CSRF, replay, and account binding

- Separate flow and proof tokens prevent opening/seeing the bio URL from being enough to complete the browser flow.
- Login/invite state is browser-bound through the anti-forgery cookie hash.
- Link state is additionally bound to the authenticated local account/session.
- State and proof are single-use after success and expire under the federation state TTL.
- Exact provider row binding prevents using one configured VRChat row to bypass another row's mode.
- Existing identity conflicts fail closed and never rebind an identity implicitly.

### SSRF and upstream trust

- VRChat API origin and paths are constants.
- User profile URLs are parsed only to extract a validated `usr_…` ID; they are never requested.
- VRChat avatar inheritance uses only `currentAvatarThumbnailImageUrl` from the verified public-user response and the existing bounded/SSRF-screened avatar pipeline.
- JSON bodies and metadata are size bounded before decoding/persistence.

### Rate limiting

- No profile polling.
- A profile lookup occurs only after explicit **Verify profile**.
- Per-flow cooldown prevents click-spam.
- Shared per-provider backoff prevents multiple instances from ignoring the same `429`.
- `Retry-After` is honored; absent/invalid values use capped exponential backoff with jitter.
- Operator wizard attempts remain under existing admin/sudo rate and body-size controls.

## Error behavior

- Invalid ID/profile URL: local validation error; no upstream request.
- Bio link absent: retryable until flow expiry, subject to cooldown.
- Returned user ID mismatch: generic verification failure plus secret-free diagnostic.
- Unknown/expired/replayed/browser-swapped flow: generic invalid/expired flow.
- Identity linked elsewhere: generic identity conflict; no account enumeration.
- Unknown identity in `link_only`: existing link-required behavior.
- Local username collision: conflict before account creation. The unconsumed flow remains valid until its normal expiry so the user can choose another local username and explicitly retry verification.
- Operator `401/403`: provider requires re-authentication; end-user flow temporarily unavailable.
- VRChat `429`: retry time returned; provider backoff recorded.
- VRChat network/5xx/malformed/oversized response: temporary upstream failure; flow remains until expiry.
- Database/resolver failure after evidence: transaction rolls back. Proof is consumed only when the core can commit the successful resolution, avoiding an authenticated-but-unlinked partial state.

## Audit and diagnostics

Record:

- operator setup start/success/failure category and re-authentication state;
- VRChat proof begin/success/failure category;
- provider slug/protocol and public subject where existing federation audit conventions permit it;
- provisioning mode outcome and link/login intent;
- upstream status class, throttling deadline, and bounded error category.

Never record:

- operator username/password or codes;
- Basic headers;
- cookies or cookie attributes;
- proof/flow tokens or full URLs;
- invitation tokens;
- full upstream request/response bodies;
- encrypted secret bytes/nonces.

## Verification strategy

### Adapter contracts and regression coverage

Run OIDC and Steam through the new registry and shared resolver. Existing security and behavior tests remain authoritative, with adapter-contract additions for:

- begin/advance action sequencing;
- state/browser binding;
- all three modes;
- existing identity relogin and provider-row mismatch;
- invite redemption;
- explicit linking and session-swap defense;
- first-login confirmation;
- avatar handoff;
- secret decrypt/config validation failures.

### VRChat adapter tests

Use a fake HTTP transport/server injected at the adapter boundary; production origin remains fixed and non-configurable. Cover:

- required User-Agent and fixed paths;
- operator login without 2FA;
- TOTP, email OTP, and recovery OTP endpoints;
- unsupported challenge methods;
- temporary cookie-jar sealing and admin/browser binding;
- persisted cookie reuse without Basic auth;
- invalid/expired operator session;
- cookie-domain/name filtering;
- malformed, oversized, 401/403, 429, 5xx, and network responses;
- exact/canonical bio-link match;
- wrong returned user ID;
- missing link retry;
- proof replay, expiry, and browser swap;
- metadata allowlist and bounds.

### End-to-end federation tests

Exercise VRChat through the real federation core and database for:

- known-identity login with fresh proof;
- `auto_provision` with user-selected username;
- username validation/collision;
- `invite_only` creation and atomic invitation consumption;
- `link_only` unknown rejection and known login;
- authenticated explicit link;
- identity already linked elsewhere;
- disabled provider/account behavior;
- confirmation and session issuance;
- upstream-data insert and refresh;
- proof consumed only after committed resolution.

### Database and filtering tests

- Migration applies to a schema containing representative OIDC and Steam rows.
- Existing encrypted secret bytes and identity links survive.
- Steam IDs backfill into upstream data.
- JSON type/size/key constraints reject invalid data.
- Unified search covers local and upstream values across pages.
- Advanced provider/field exact and contains filters validate descriptor combinations.
- Filters run before pagination and changing filters resets cursors.
- Identity summaries do not expose secret/provider-health data.

### Dashboard and smoke verification

Vitest covers:

- protocol-discriminated provider forms;
- VRChat operator wizard states;
- proof preparation/instructions/verification/retry/success;
- no end-user credential fields;
- public proof-link explanation;
- unified/advanced account filters and matched identity context;
- English/Chinese locale parity.

Browser smoke-driving uses the existing demo/mock infrastructure to complete the VRChat ceremony without live credentials: create/configure provider, establish a mocked operator session, start proof, emulate the profile link, verify, confirm, observe the local session/account metadata, search the account by VRChat ID/display name, and open the public proof-link page. Existing OIDC and Steam smoke arcs must remain green after the plugin migration.

## Definition of done

- Database migration and SQLC generation complete with no obsolete provider columns or compatibility aliases.
- OIDC and Steam behavior passes through the new provider registry with existing tests green.
- VRChat supports operator setup/renewal, fresh bio-link login, invites, `auto_provision`, `link_only`, explicit linking, confirmation, and session issuance.
- Successful provider logins persist and refresh bounded upstream data.
- Admin unified and advanced account filters work server-side before pagination.
- Proof-link page displays the approved explanation and never authenticates by being opened.
- No secret/proof material appears in read APIs, logs, audits, diagnostics, snapshots, or frontend state beyond request-required transient values.
- Targeted Go/database/dashboard tests pass; project build/typecheck/lint gates pass; browser smoke demonstrates the end-to-end mocked VRChat flow and unchanged OIDC/Steam flows.
- Dashboard distribution/embedded assets are rebuilt using the repository's established release workflow.
