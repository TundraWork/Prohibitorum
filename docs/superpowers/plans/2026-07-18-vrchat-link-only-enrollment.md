# VRChat Link-Only Enrollment Implementation Plan

> **For implementers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` for inline execution or `superpowers:subagent-driven-development` for task-by-task execution. Follow `superpowers:test-driven-development` for every behavior change and `superpowers:verification-before-completion` before delivery.

**Goal:** Make VRChat a proof-backed, permanently `link_only` provider: public VRChat verification hands unknown users to local-account enrollment and existing linked users to local-credential recovery, while authenticated Connected Accounts linking remains unchanged and VRChat proof never directly creates a normal session.

**Architecture:** Keep VRChat inside the protocol-neutral federation flow only for browser-bound profile proof. Add an `IntentEnroll` completion branch that issues an opaque, 15-minute enrollment instead of invoking the normal identity resolver. A `federated_register` enrollment carries a server-written identity snapshot for atomic account/credential/identity creation; an existing identity receives the existing account-targeted `reset` enrollment with a VRChat recovery-source marker. The shared enrollment ceremony remains the only component allowed to create or replace the local passkey and issue a session.

**Tech Stack:** Go 1.25, PostgreSQL/Goose, sqlc, Chi/Huma, Vue 3, TypeScript, Pinia, Vitest, Vue Test Utils, Tailwind, project `mise` tasks, existing browser smoke harness.

**Approved design:** `docs/superpowers/specs/2026-07-18-vrchat-link-only-enrollment-design.md`

**Non-goals:** Do not add VRChat password collection, OAuth/OIDC behavior, a VRChat-only credential form, password/TOTP enrollment, compatibility shims for hypothetical legacy users, or changes to OIDC/Steam provisioning behavior.

---

## Task 1: Add database invariants and enrollment query surface

**Files:**
- Create: `db/migrations/033_vrchat_link_only_enrollment.sql`
- Create: `db/migrations/vrchat_link_only_enrollment_test.go`
- Modify: `db/queries/enrollment.sql`
- Modify: `db/queries/upstream_idp.sql`
- Modify: `db/queries/account_identity.sql`
- Regenerate: `pkg/db/enrollment.sql.go`
- Regenerate: `pkg/db/upstream_idp.sql.go`
- Regenerate: `pkg/db/account_identity.sql.go`
- Regenerate: `pkg/db/models.go`
- Regenerate: `pkg/db/querier.go`

**Acceptance criteria:**
- Every persisted VRChat provider has `mode = 'link_only'`; inserts/updates that violate this fail at the database boundary.
- Existing VRChat provider rows are migrated to `link_only`.
- `enrollment.intent` accepts `federated_register`.
- Only `federated_register` rows may carry the verified provider/identity snapshot.
- Only provider-sourced `reset` rows may carry `recovery_source_upstream_idp_id`.
- Federation-factor counting excludes VRChat because it is no longer a direct local sign-in method.
- Goose up/down and sqlc generation succeed.

### Step 1: Write the failing migration integration test

Add a schema-isolated PostgreSQL test following `account_identity_filtering_test.go`. Migrate to version 32, seed:

- a VRChat provider in `auto_provision` mode;
- a normal OIDC provider;
- representative bootstrap/invite/reset enrollments.

Migrate to version 33 and assert:

1. the seeded VRChat row is now `link_only`;
2. a VRChat insert/update with `auto_provision` or `invite_only` is rejected;
3. non-VRChat modes remain valid;
4. a valid `federated_register` row accepts provider ID/slug, issuer, subject, display name, object-shaped metadata, and optional avatar URL;
5. malformed intent/snapshot combinations are rejected;
6. a provider-sourced `reset` accepts only the recovery-source provider ID and target account;
7. deleting the referenced provider invalidates pending provider-bound enrollment rows via the selected FK policy;
8. migration down returns to version 32 and removes all new columns/constraints.

Run to prove RED:

```bash
PROHIBITORUM_TEST_DATABASE_URL="$PROHIBITORUM_TEST_DATABASE_URL" go test ./db/migrations -run TestVRChatLinkOnlyEnrollmentMigrationPostgres -count=1
```

Expected: FAIL because migration 33 and its columns/constraints do not exist.

### Step 2: Add migration 033

The up migration must:

- update `upstream_idp SET mode = 'link_only' WHERE protocol = 'vrchat'`;
- add a named check equivalent to `protocol <> 'vrchat' OR mode = 'link_only'`;
- replace the generated `enrollment_intent_check` so it includes `federated_register`;
- add nullable server-only fields:
  - `federated_upstream_idp_id bigint REFERENCES upstream_idp(id) ON DELETE CASCADE`;
  - `federated_upstream_idp_slug text`;
  - `federated_upstream_iss text`;
  - `federated_upstream_sub text`;
  - `federated_display_name text`;
  - `federated_upstream_data jsonb`;
  - `federated_avatar_url text`;
  - `recovery_source_upstream_idp_id bigint REFERENCES upstream_idp(id) ON DELETE CASCADE`;
- add one named snapshot check that requires all mandatory federated fields, an object-shaped metadata value bounded by the existing 4096-byte identity-data limit, no target account, and no recovery source only when `intent = 'federated_register'`; all snapshot fields must be null for every other intent;
- add one named recovery-source check allowing the recovery source only for `intent = 'reset'` with a non-null target account.

The down migration must restore the exact version-32 intent constraint before dropping columns and the VRChat-mode constraint. Do not weaken existing invite/template checks.

### Step 3: Add dedicated sqlc operations

In `db/queries/enrollment.sql`, add dedicated inserts rather than widening the generic insert used by bootstrap/invite/admin reset:

- `InsertFederatedRegistrationEnrollment` with the immutable provider/identity snapshot;
- `InsertProviderRecoveryEnrollment` with target account and source provider ID.

In `db/queries/upstream_idp.sql`, add `GetUpstreamIDPByIDAny` for completion-time availability checks.

In `db/queries/account_identity.sql`, change `CountUsableSignInFederation` to require `ip.protocol <> 'vrchat'` in addition to `NOT ip.disabled`. Preserve list/search behavior for VRChat identities.

### Step 4: Regenerate bindings

Run:

```bash
mise run sqlc
```

Do not hand-edit generated files. Inspect compile errors rather than modifying generated signatures.

### Step 5: Run focused tests

```bash
PROHIBITORUM_TEST_DATABASE_URL="$PROHIBITORUM_TEST_DATABASE_URL" go test ./db/migrations -run TestVRChatLinkOnlyEnrollmentMigrationPostgres -count=1
go test ./pkg/authn ./pkg/server -run 'AvailableMethods|WouldRemoveLastFactor|Identities_Unlink' -count=1
```

Add or update an authn/server test proving an enabled VRChat identity alone does not surface `federation_oidc` as an available direct sign-in factor.

### Step 6: Commit

```bash
git add db/migrations/033_vrchat_link_only_enrollment.sql db/migrations/vrchat_link_only_enrollment_test.go db/queries/enrollment.sql db/queries/upstream_idp.sql db/queries/account_identity.sql pkg/db
git commit -m "feat: add VRChat enrollment persistence"
```

---

## Task 2: Implement short-lived VRChat enrollment issuance

**Files:**
- Modify: `pkg/credential/enrollment/enrollment.go`
- Modify: `pkg/credential/enrollment/enrollment_test.go`
- Create: `pkg/federation/enrollment.go`
- Create: `pkg/federation/enrollment_test.go`
- Modify: `pkg/federation/types.go`

**Acceptance criteria:**
- Adapter-verified, unknown VRChat identities receive a 15-minute `federated_register` enrollment containing only validated server-side snapshot data.
- Existing identities receive a 15-minute account-targeted `reset` enrollment with `recovery_source_upstream_idp_id`.
- Provider mismatches and disabled accounts fail without exposing account lookup data.
- Issuance audits contain provider/intent metadata but never the token, proof URL, cookie, username, or private account fields.

### Step 1: Write failing enrollment-package tests

Add tests around new issuance helpers using the package's fake query style:

- federated registration creates a 32-byte random bearer token, expires at approximately 15 minutes, writes every snapshot field, and never accepts an account target;
- provider recovery writes `intent = reset`, target account, source provider ID, 15-minute expiry, and no identity snapshot;
- query failures return errors without returning a token.

Run to prove RED:

```bash
go test ./pkg/credential/enrollment -run 'Federated|ProviderRecovery' -count=1
```

### Step 2: Add typed enrollment issuance helpers

Add:

```go
const FederatedEnrollmentTTL = 15 * time.Minute

type FederatedIdentitySnapshot struct {
    UpstreamIDPID int64
    UpstreamIDPSlug string
    Issuer string
    Subject string
    DisplayName string
    UpstreamData []byte
    AvatarURL *string
}
```

Implement `IssueFederatedRegistration` and `IssueProviderRecovery` around the dedicated sqlc inserts. Reuse the existing cryptographic token generator. Require non-empty provider binding, issuer, subject, and display name; require validated canonical object JSON; use 15 minutes when the caller passes no explicit TTL. Do not return or log the inserted snapshot.

### Step 3: Write failing issuer-classification tests

Define a narrow fake for identity/account lookup and enrollment insertion. Cover:

- no `(issuer, subject)` row -> federated registration;
- existing row for the same provider -> provider recovery for its account;
- existing row bound to another provider -> opaque federation-state failure;
- existing disabled account -> `bad_credentials`, not an account-specific response;
- oversized/non-object upstream data -> rejected before insertion;
- storage failure -> no successful grant;
- issued audit record contains only `intent`, `idp_slug`, and protocol-safe identifiers approved by the audit contract, never token/account profile values.

Run to prove RED:

```bash
go test ./pkg/federation -run VRChatEnrollmentIssuer -count=1
```

### Step 4: Implement the federation issuer

Add protocol-neutral service types:

```go
type EnrollmentGrant struct {
    Token string
    Intent string
    ExpiresAt time.Time
}

type EnrollmentIssuer interface {
    Issue(context.Context, Provider, VerifiedIdentity) (EnrollmentGrant, error)
}
```

Implement `VRChatEnrollmentIssuer` in `pkg/federation/enrollment.go`:

1. require `provider.Protocol == "vrchat"` and `provider.Mode == ModeLinkOnly`;
2. canonicalize and bound `VerifiedIdentity.UpstreamData` with `ValidateUpstreamData`;
3. look up `(issuer, subject)`;
4. if absent, call `IssueFederatedRegistration` with provider ID/slug, verified display name, curated metadata, and adapter-produced avatar URL;
5. if present, require the identity's `upstream_idp_id` to match the active provider, load the account, reject disabled accounts with the existing opaque credential error, then call `IssueProviderRecovery`;
6. emit `FactorEnrollment/EventEnrollmentIssued` with safe intent/source detail only after the insert succeeds.

Do not update identity claims during issuance; registration completion owns the first insert, while authenticated linking retains the existing resolver path.

### Step 5: Run focused tests and commit

```bash
go test ./pkg/credential/enrollment ./pkg/federation -run 'Federated|ProviderRecovery|VRChatEnrollmentIssuer' -count=1
git add pkg/credential/enrollment pkg/federation/enrollment.go pkg/federation/enrollment_test.go pkg/federation/types.go
git commit -m "feat: issue VRChat registration and recovery enrollments"
```

---

## Task 3: Route public VRChat proof into enrollment, never login resolution

**Files:**
- Modify: `pkg/federation/types.go`
- Modify: `pkg/federation/state.go`
- Modify: `pkg/federation/state_test.go`
- Modify: `pkg/federation/service.go`
- Modify: `pkg/federation/service_test.go`
- Modify: `pkg/federation/providers/vrchat/adapter.go`
- Modify: `pkg/federation/providers/vrchat/adapter_test.go`
- Modify: `pkg/server/handle_federation.go`
- Modify: `pkg/server/handle_federation_flow.go`
- Modify: `pkg/server/handle_federation_test.go`
- Modify: `pkg/server/handle_federation_flow_test.go`
- Modify: `pkg/server/server.go`

**Acceptance criteria:**
- `GET /auth/federation/{vrchat}/login` starts a browser-bound `IntentEnroll` flow.
- OIDC/Steam public starts remain `IntentLogin`; authenticated linking remains `IntentLink`.
- Successful VRChat proof calls the enrollment issuer, not the normal resolver.
- The proof response redirects to `/enroll/:token` and emits no normal session or `Set-Cookie` session header.
- Missing issuer wiring, wrong protocol/mode, callback-route mismatch, and replay fail closed.

### Step 1: Write failing state and adapter tests

Add `IntentEnroll` expectations:

- encode/decode round-trip preserves it;
- it is accepted only on the local interactive callback route;
- it cannot carry a link account/session binding or invitation token;
- the VRChat adapter accepts only `IntentEnroll` and `IntentLink`;
- direct `IntentLogin` and `IntentInvite` calls to the VRChat adapter are rejected;
- OIDC and Steam behavior is unchanged.

Run to prove RED:

```bash
go test ./pkg/federation ./pkg/federation/providers/vrchat -run 'IntentEnroll|VRChat.*Intent' -count=1
```

### Step 2: Replace the public-begin semantic with protocol dispatch

Rename the public service entry point from the misleading `BeginLogin` to `BeginPublic` using LSP rename so every callsite/test is updated. Inside it:

- load and validate the provider as today;
- choose `IntentEnroll` only for `protocol == "vrchat"`;
- require VRChat `mode == link_only` before constructing flow state;
- choose `IntentLogin` for every other protocol;
- preserve return-to validation, browser binding, readiness checks, and action dispatch.

Do not add a second public route. The existing login-page URL remains stable; only its VRChat server-side intent changes.

### Step 3: Inject enrollment issuance into the service

Make `EnrollmentIssuer` an explicit `NewService` dependency rather than a hidden global. Update every production/test constructor; tests not exercising `IntentEnroll` may use a rejecting fake, but production must wire `NewVRChatEnrollmentIssuer(queries, auditWriter)`.

In final verification:

1. validate/advance the adapter action exactly as today;
2. after obtaining `VerifiedIdentity`, branch on `state.Intent`;
3. for `IntentEnroll`, call the issuer and return a completion carrying the grant; never call `IdentityResolver.ResolveIdentity`, avatar inheritance, or normal account resolution;
4. retain the existing resolver and avatar path for login/invite/link.

The completion type must make enrollment mutually exclusive with `AccountID`/normal session data. Reject impossible mixed results rather than selecting one silently.

### Step 4: Write failing HTTP tests

Use a fake enrollment issuer and existing VRChat flow harness to prove:

- public begin stores `IntentEnroll`;
- verify returns `{ "redirect": "/enroll/<opaque-token>" }`;
- response has no Prohibitorum session cookie;
- the session store issue count stays zero;
- resolver calls stay zero;
- issuer receives the exact provider and adapter-verified identity once;
- replayed verify is rejected and cannot issue a second enrollment;
- authenticated Connected Accounts `IntentLink` still binds the identity and returns there without enrollment.

Also retain one OIDC or Steam test proving normal successful login still issues a session.

Run to prove RED before the handler change:

```bash
go test ./pkg/server -run 'Federation.*VRChat.*Enroll|Federation.*Session' -count=1
```

### Step 5: Map enrollment completion to the shared route

Update federation completion handling:

- local JSON verification returns `/enroll/` plus URL-escaped token;
- redirect-mode handling does the equivalent 302 if ever reached with an enrollment completion;
- clear only the short-lived federation browser cookie as today;
- never call `sessionStore.Issue` or set the normal session cookie for enrollment completion;
- never include the enrollment token in logs, audit records, diagnostics, or errors.

### Step 6: Run package tests and commit

```bash
go test ./pkg/federation ./pkg/federation/providers/vrchat ./pkg/server -run 'Federation|VRChat|IntentEnroll' -count=1
git add pkg/federation pkg/server/handle_federation.go pkg/server/handle_federation_flow.go pkg/server/handle_federation_test.go pkg/server/handle_federation_flow_test.go pkg/server/server.go
git commit -m "feat: hand VRChat proof to local enrollment"
```

---

## Task 4: Complete federated registration and provider-backed recovery atomically

**Files:**
- Modify: `pkg/contract/auth.go`
- Modify: `pkg/server/handle_enrollment.go`
- Modify: `pkg/server/handle_enrollment_test.go`
- Create or modify focused tests: `pkg/server/handle_vrchat_enrollment_test.go`
- Modify as needed: `pkg/federation/provider_store.go`
- Modify as needed: `pkg/federation/avatar.go`
- Modify: `pkg/server/server.go`

**Acceptance criteria:**
- Unknown VRChat users choose a local username, may edit the suggested display name, register a passkey, and atomically create account + credential + confirmed VRChat identity + curated metadata.
- Existing linked users complete the existing reset ceremony; old credentials and sessions are replaced only at commit.
- VRChat registration/recovery rechecks provider protocol, mode, enabled/readiness state at begin and completion.
- Provider-backed recovery does not expose the owning account's username/display name or credential descriptors.
- Registration races leave no partial account/credential/identity.
- Avatar inheritance is best-effort only after successful account transaction commit.

### Step 1: Write failing preview/begin tests

Extend `EnrollmentPreview` with an optional safe field:

```go
SuggestedDisplayName string `json:"suggestedDisplayName,omitempty"`
```

Add tests proving:

- `federated_register` preview returns intent, expiry, and suggested display name only;
- it does not return issuer, subject, metadata, avatar URL, provider ID, or provider slug;
- provider-sourced `reset` suppresses `Target` entirely;
- ordinary administrator-issued reset still returns the existing target preview;
- disabled, deleted, unready, wrong-protocol, or no-longer-link-only providers make begin fail with the existing safe enrollment/provider error.

Run to prove RED:

```bash
go test ./pkg/server -run 'Enrollment.*(Federated|ProviderRecovery|Preview)' -count=1
```

### Step 2: Add one reusable provider recheck

Implement a server helper that loads the enrollment-bound provider by ID and validates all of:

- row exists;
- protocol is exactly `vrchat`;
- mode is exactly `link_only`;
- provider is not disabled;
- the registered VRChat definition reports it ready.

Use the same helper from preview only where needed for safe display, from registration begin, and again inside the completion transaction. Collapse absent/changed provider state to the existing safe enrollment/provider error. Do not duplicate readiness logic in handlers.

If provider-row conversion is needed, expose one narrow, tested conversion/load method from `provider_store.go`; do not manually reconstruct sealed-secret state in multiple server functions.

### Step 3: Extend registration begin without duplicating credential policy

Treat `federated_register` as a new-account ceremony with role `user` and empty local attributes:

- validate browser-submitted local username and display name with existing account validators;
- perform the same soft username uniqueness precheck as invite;
- generate a fresh WebAuthn user handle;
- stash only the proposed local fields, generated handle, nickname, and WebAuthn session data; never copy provider identity snapshot into KV/browser-controlled state.

For provider-backed reset:

- recheck provider before beginning WebAuthn;
- load the real account internally and reject disabled accounts;
- construct creation options with the same opaque user handle but neutral public labels such as `account`, not the stored username/display name;
- omit existing credential descriptors from `excludeCredentials` so public recovery cannot enumerate credential IDs/transports;
- preserve ordinary admin-reset behavior unchanged.

Refactor only enough to share the existing new-account validation/stash code; do not create a new enrollment subsystem.

### Step 4: Write failing atomic-completion tests

Cover the observable transaction contracts:

1. valid registration creates one user account, one passkey, one account identity with the enrollment's provider/issuer/subject, canonical `upstream_data`, `confirmed_at`, and one session;
2. account role is `user`, attributes are `{}`, username is browser-chosen, and display name defaults from but is not forced to VRChat;
3. username collision returns 409 and leaves enrollment unconsumed with no account/credential/identity;
4. identity uniqueness race returns the safe federation identity conflict and rolls back account, credential, and consumption;
5. provider disable/mode/readiness change between begin and complete rolls back everything;
6. malformed or missing ceremony stash cannot substitute identity data;
7. provider-backed reset deletes/replaces credentials, consumes once, revokes all old sessions, and issues exactly one fresh session;
8. disabled target/provider change rejects recovery before credential deletion;
9. replay and concurrent completion have one winner;
10. post-commit avatar failure does not roll back or alter the successful response.

Run to prove RED:

```bash
go test ./pkg/server -run 'VRChatEnrollment|FederatedRegistration|ProviderRecovery' -count=1
```

### Step 5: Implement federated registration completion

Inside the existing enrollment transaction:

1. atomically consume the token;
2. require the federated ceremony stash;
3. recheck provider binding and readiness;
4. create/validate the WebAuthn credential against the stashed local account proposal;
5. insert the local account (`role=user`, attributes `{}`, enabled);
6. insert the credential;
7. authoritative-check/insert `(issuer, subject)` with `upstream_idp_id` and exact stored canonical metadata;
8. confirm the new identity in the same transaction;
9. commit;
10. only then audit enrollment consumption, trigger best-effort avatar inheritance from the stored adapter-approved URL, issue the local session, and return the existing completion payload.

Map unique username and identity violations to their existing public errors. Audit conflict/failure reasons through the outer writer so rollback does not erase security events. Never include the enrollment identity snapshot in an error.

### Step 6: Harden provider-backed reset completion

Before deleting credentials inside the reset transaction:

- if `recovery_source_upstream_idp_id` is present, recheck the provider with the shared helper;
- preserve the existing target-account and disabled-account checks;
- keep delete-old-credentials + insert-new-credential + consume atomic;
- preserve post-commit `RevokeAllForAccount` before issuing the fresh session;
- include only `intent=reset` and a safe `source=vrchat` marker in audit detail.

Ordinary reset rows with null recovery source must remain byte-for-byte equivalent in behavior.

### Step 7: Run package tests and commit

```bash
go test ./pkg/credential/enrollment ./pkg/federation ./pkg/server -run 'Enrollment|VRChat|Recovery' -count=1
git add pkg/contract/auth.go pkg/server/handle_enrollment.go pkg/server/handle_enrollment_test.go pkg/server/handle_vrchat_enrollment_test.go pkg/federation/provider_store.go pkg/federation/avatar.go pkg/server/server.go
git commit -m "feat: complete VRChat account enrollment and recovery"
```

---

## Task 5: Enforce and explain fixed `link_only` administration

**Files:**
- Modify: `pkg/server/handle_admin_upstream_idps.go`
- Modify: `pkg/server/handle_admin_upstream_idps_test.go`
- Modify: `dashboard/src/pages/admin/AdminUpstreamIdpsView.vue`
- Modify: `dashboard/src/pages/admin/AdminUpstreamIdpsView.test.ts`
- Modify: `dashboard/src/pages/admin/AdminUpstreamIdpDetailView.vue`
- Modify: `dashboard/src/pages/admin/AdminUpstreamIdpDetailView.test.ts`
- Modify: `dashboard/src/locales/en.ts`
- Modify: `dashboard/src/locales/zh.ts`
- Modify: locale parity tests if the new key is included in a curated parity table

**Acceptance criteria:**
- Admin API rejects VRChat create/update with any mode other than `link_only`.
- VRChat create/detail screens do not present a meaningful editable mode selector.
- Admin copy explains that VRChat proof links identity and registration/recovery creates a local credential.
- OIDC/Steam mode controls and payloads remain unchanged.

### Step 1: Write failing backend policy tests

Add table-driven create/update tests for:

- VRChat + `link_only` accepted;
- VRChat + `auto_provision` rejected with 400;
- VRChat + `invite_only` rejected with 400;
- OIDC/Steam legal modes unchanged;
- an attempted update of a migrated VRChat row away from `link_only` rejected.

Run to prove RED:

```bash
go test ./pkg/server -run 'AdminUpstream.*VRChat.*Mode' -count=1
```

### Step 2: Enforce at the admin API boundary

In `validateProviderWrite`, after generic mode validation and before persistence, require `body.Mode == federation.ModeLinkOnly` when the selected definition protocol is VRChat. Return the existing bad-request envelope; do not silently rewrite malicious API input. The database check remains the final invariant.

### Step 3: Write failing Vue tests

For create and detail views, assert:

- selecting/loading VRChat shows a fixed `Link only` value or explanatory row instead of an editable selector;
- payload always sends `mode: 'link_only'`;
- explanatory copy is visible;
- switching back to OIDC/Steam restores their existing selector behavior;
- no operator-session, enabled/disabled, metadata, icon, filter, or audit control changes.

Run to prove RED:

```bash
cd dashboard && npm test -- --run src/pages/admin/AdminUpstreamIdpsView.test.ts src/pages/admin/AdminUpstreamIdpDetailView.test.ts
```

### Step 4: Implement fixed-mode UI and localized copy

Use a computed effective mode for payload construction; do not rely on a disabled input as validation. Preserve the stored mode state for non-VRChat protocols when the create form protocol changes. On detail, render the fixed mode and description from the loaded protocol.

English and Chinese copy must state that VRChat profile proof is used for linking/account recovery and that users still create a local sign-in credential. Never call it OAuth, OIDC, or a direct local credential.

### Step 5: Run focused tests and commit

```bash
go test ./pkg/server -run 'AdminUpstream' -count=1
cd dashboard && npm test -- --run src/pages/admin/AdminUpstreamIdpsView.test.ts src/pages/admin/AdminUpstreamIdpDetailView.test.ts src/locales/locales.parity.test.ts src/locales/locales.errors.parity.test.ts
git add pkg/server/handle_admin_upstream_idps.go pkg/server/handle_admin_upstream_idps_test.go dashboard/src/pages/admin dashboard/src/locales
git commit -m "fix: lock VRChat providers to link-only mode"
```

---

## Task 6: Present registration/recovery accurately in the public UI

**Files:**
- Modify: `dashboard/src/pages/FederationFlowView.vue`
- Modify: `dashboard/src/pages/FederationFlowView.test.ts`
- Modify: `dashboard/src/pages/EnrollView.vue`
- Modify: `dashboard/src/pages/EnrollView.test.ts`
- Modify: `dashboard/src/locales/en.ts`
- Modify: `dashboard/src/locales/zh.ts`
- Modify: `dashboard/src/locales/locales.parity.test.ts`
- Modify if needed: `dashboard/src/lib/errors.ts`
- Preserve: `dashboard/src/components/custom/VRChatButton.vue`

**Acceptance criteria:**
- The first VRChat proof screen always explains that proof is not the local sign-in credential and leads to local account creation/recovery.
- `federated_register` renders the shared new-account enrollment form with an editable suggested display name and required local username.
- Provider-backed reset renders recovery without disclosing the account identity.
- Successful proof still uses the existing success/Continue interaction and then opens `/enroll/:token`.
- Connected Accounts linking, OIDC, Steam, error handling, accessibility, and mobile layout remain unchanged.

### Step 1: Write failing federation-flow tests

Add tests for the approved notice:

- identify screen contains the concise primary notice and supporting sign-in/link guidance;
- notice is visible before any profile input submission;
- proof and success screens do not claim that VRChat issued a local session;
- verify success stores the returned `/enroll/...` redirect and Continue navigates there;
- no local-username field appears in the public VRChat proof flow;
- authenticated linking still returns to Connected Accounts.

Run to prove RED:

```bash
cd dashboard && npm test -- --run src/pages/FederationFlowView.test.ts
```

### Step 2: Implement the approved proof notice

Use the existing calm information-panel visual language. Exact semantic content:

- primary: VRChat proof is used only to verify/link identity and help recover access; new users create a local account and sign-in method after proof;
- supporting: existing local-account users should sign in first and link VRChat from Connected Accounts.

Keep the already-approved profile URL guidance, proof-link instructions, external-link safety, ErrorPanel placement, and branded VRChat button unchanged.

### Step 3: Write failing enrollment-view tests

Expand the local `EnrollmentPreview` type and tests:

- `federated_register` is recognized as a new-account intent;
- heading/copy explains local account setup after VRChat verification;
- username is empty and required;
- display name initializes once from `suggestedDisplayName`, remains editable, and is submitted exactly as edited;
- reset with no target renders a neutral recovery heading/form and no blank or guessed username;
- ordinary target-bearing reset remains unchanged;
- invalid/expired/consumed behavior still routes through existing code-driven errors;
- no provider snapshot fields are expected or rendered.

Run to prove RED:

```bash
cd dashboard && npm test -- --run src/pages/EnrollView.test.ts
```

### Step 4: Implement shared enrollment branching

Update the preview union to include `federated_register` and optional `suggestedDisplayName`. Include it in the existing `collectsIdentity` branch; do not create a VRChat-only form or ceremony. Initialize `displayName` after preview load only for this intent. Keep username browser-chosen and never infer it from VRChat subject/display name.

For a provider-backed reset where `target` is omitted, render neutral localized recovery copy and the same passkey action without an account-identity row. The POST body remains empty as for reset.

### Step 5: Verify localization and accessibility

Add English and Simplified Chinese strings together. Run parity tests and component tests. Check:

- one page heading;
- label/input associations;
- visible keyboard focus;
- notice contrast and wrapping;
- no horizontal overflow at 390px;
- error focus behavior unchanged.

```bash
cd dashboard && npm test -- --run src/pages/FederationFlowView.test.ts src/pages/EnrollView.test.ts src/locales/locales.parity.test.ts src/locales/locales.errors.parity.test.ts
```

### Step 6: Commit

```bash
git add dashboard/src/pages/FederationFlowView.vue dashboard/src/pages/FederationFlowView.test.ts dashboard/src/pages/EnrollView.vue dashboard/src/pages/EnrollView.test.ts dashboard/src/locales dashboard/src/lib/errors.ts
git commit -m "feat: guide VRChat users through local enrollment"
```

---

## Task 7: Prove the end-to-end lifecycle and refresh operator documentation

**Files:**
- Modify: `cmd/smoke/main.go`
- Modify: `cmd/vrchatmock/server.go` and tests only if the existing mock lacks deterministic repeat-proof support
- Modify: `ARCHITECTURE.md`
- Modify: `STATUS.md`
- Modify: `api.md`
- Modify if relevant: `PRODUCT.md`
- Generated: `pkg/webui/dist/**`
- Modify generated embed only through `mise run build:web`

**Acceptance criteria:**
- Smoke proves unknown-user registration and existing-user recovery through real HTTP routes and a virtual WebAuthn authenticator.
- No normal session appears between VRChat proof and enrollment completion.
- Recovery revokes the previous session and issues a fresh one only after credential replacement.
- Documentation no longer describes VRChat as direct federation login/provisioning.
- Embedded dashboard matches source.

### Step 1: Extend the smoke scenario before changing docs

Replace the old direct-VRChat-login expectation with this deterministic sequence:

1. configure a ready, enabled `link_only` VRChat provider/operator session;
2. begin public VRChat proof for an unknown mock profile;
3. complete proof and assert HTTP 200 JSON redirect to `/enroll/:token`;
4. assert the proof response did not set the Prohibitorum session cookie and `/me` is still unauthenticated;
5. preview the enrollment and assert `intent=federated_register` plus safe display-name suggestion only;
6. begin/complete WebAuthn registration using the smoke virtual authenticator;
7. assert a normal session now exists and the account identity contains curated VRChat `userId`, `displayName`, and `profileUrl` metadata;
8. sign out or retain the old session identifier, repeat proof for the now-linked identity, and assert a target-hidden `reset` enrollment with no session issued by proof;
9. complete a replacement WebAuthn ceremony;
10. assert the previous session was revoked and exactly one fresh session works;
11. retain the authenticated Connected Accounts link scenario for a separate account or identity to prove it remains session-bound;
12. assert disabled-provider/account and replay paths fail safely without secret output.

The smoke output should name these as registration/recovery checks, not `VRChat login`.

### Step 2: Run focused smoke during development

Use the existing smoke task and mock orchestration. If a narrower VRChat selector exists, run it first; otherwise run the full smoke once after the scenario compiles:

```bash
mise run ci:smoke
```

Capture exact step counts and failures. Do not weaken cookie or session assertions to accommodate the new path.

### Step 3: Update architecture and API documentation

Replace stale claims in `ARCHITECTURE.md`, `STATUS.md`, `api.md`, and `PRODUCT.md`:

- VRChat is a profile-proof adapter, not OAuth/OIDC and not a direct local sign-in credential;
- providers are fixed `link_only`;
- public proof issues a short-lived registration or recovery enrollment;
- the shared local credential ceremony is authoritative;
- authenticated Connected Accounts linking remains direct/session-bound;
- identity metadata/filter fields remain supported;
- operator cookies/credentials and proof/enrollment tokens remain server-only/opaque.

Document the new `federated_register` preview shape and the fact that provider-backed reset omits `target`. Do not document internal cookie values or raw enrollment snapshot fields as public API.

### Step 4: Build the embedded dashboard

```bash
mise run build:web
```

Confirm Vite production build succeeds and `pkg/webui/dist` is the only generated artifact set. Do not add the pre-existing untracked root `package-lock.json`.

### Step 5: Run focused integration verification

```bash
go test ./pkg/credential/enrollment ./pkg/federation ./pkg/federation/providers/vrchat ./pkg/server ./cmd/smoke ./cmd/vrchatmock -count=1
cd dashboard && npm test -- --run src/pages/FederationFlowView.test.ts src/pages/EnrollView.test.ts src/pages/admin/AdminUpstreamIdpsView.test.ts src/pages/admin/AdminUpstreamIdpDetailView.test.ts src/components/custom/VRChatButton.test.ts src/locales/locales.parity.test.ts src/locales/locales.errors.parity.test.ts
```

### Step 6: Commit

```bash
git add cmd/smoke cmd/vrchatmock ARCHITECTURE.md STATUS.md api.md PRODUCT.md pkg/webui/dist
git commit -m "test: cover VRChat enrollment and recovery lifecycle"
```

Omit unchanged optional files from `git add` rather than touching them for completeness.

---

## Task 8: Final verification, browser proof, review, and delivery

**Files:**
- Review all changes from the implementation base through HEAD
- Update no files unless verification or review finds a concrete defect

**Acceptance criteria:**
- Focused backend/frontend tests, production build, full CI, and smoke all pass from fresh commands.
- Desktop and mobile browser checks prove the five approved UX surfaces with no overflow or hidden controls.
- Final review finds no unresolved Critical/Important/Minor issue.
- No secret/token/cookie/account-private data appears in responses, diagnostics, logs, screenshots, or test failure output.

### Step 1: Run fresh focused verification

```bash
go test ./db/migrations ./pkg/credential/enrollment ./pkg/federation ./pkg/federation/providers/vrchat ./pkg/server ./cmd/smoke ./cmd/vrchatmock -count=1
cd dashboard && npm test -- --run src/pages/FederationFlowView.test.ts src/pages/EnrollView.test.ts src/pages/admin/AdminUpstreamIdpsView.test.ts src/pages/admin/AdminUpstreamIdpDetailView.test.ts src/components/custom/ErrorPanel.test.ts src/components/custom/VRChatButton.test.ts src/locales/locales.parity.test.ts src/locales/locales.errors.parity.test.ts
cd .. && mise run build:web
git diff --exit-code -- pkg/webui/dist
```

If migration tests require `PROHIBITORUM_TEST_DATABASE_URL`, start the project-standard isolated PostgreSQL service and rerun them; do not report a skipped database invariant as verified.

### Step 2: Run full gates

```bash
mise run ci
mise run ci:smoke
```

Record the exact file/test and smoke step counts from output. Any failure triggers `superpowers:systematic-debugging`; fix the source and rerun the full failing command.

### Step 3: Browser-verify real backend behavior

Run the real Prohibitorum backend, real built dashboard, isolated database, and deterministic VRChat mock. Use Chromium with a virtual authenticator. Verify at 1440×900 and 390×844:

1. login page branded VRChat action;
2. first proof screen notice and profile guidance;
3. proof success -> Continue -> federated registration form with suggested editable display name;
4. completed registration lands in an authenticated local session and Connected Accounts lists VRChat metadata;
5. repeat proof starts neutral account recovery with no account identity disclosure;
6. admin create/detail show fixed link-only explanation;
7. errors remain contextual, keyboard focus is visible, and no horizontal overflow/clipped action exists.

Inspect response headers/network state to confirm the proof response itself has no normal session cookie. Screenshots must not contain proof URLs, enrollment tokens, VRChat cookies, operator credentials, or private account data.

### Step 4: Request final code review

Use `superpowers:requesting-code-review` over the full implementation range. Give the reviewer:

- the approved design spec;
- this plan;
- base and head SHAs;
- focused/full/smoke/browser evidence;
- explicit security invariants: no proof-time session, forced link-only, opaque enrollment, atomic identity creation, provider recheck, hidden recovery target.

Resolve every substantiated finding in one correction commit, then rerun the affected focused command plus `mise run ci` and `mise run ci:smoke`. Request re-review until the verdict is Ready to merge.

### Step 5: Confirm repository hygiene and deliver

Confirm:

- no placeholder/TODO/compatibility shim was introduced;
- generated sqlc and dashboard assets match sources;
- all exported symbol callsites were migrated;
- root `package-lock.json` remains untracked/untouched if it pre-existed;
- docs match behavior;
- no unrelated file is staged.

If executing on the explicitly approved direct-master workflow, push only after every gate and review is clean:

```bash
git push origin master
```

Final response must state commits, exact verification commands/results, browser scenarios, review verdict, and any remaining concern. Do not claim skipped checks passed.
