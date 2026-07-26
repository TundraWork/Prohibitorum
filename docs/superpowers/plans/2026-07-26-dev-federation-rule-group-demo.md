# Dev Federation Rule-Group Demo Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persistently seed instance A of `dev:federation` with an idempotent, deterministic `dev-app` rule-group showcase and load it into the current `prohibitorum_upstream` database.

**Revision note (2026-07-26):** The implemented topology is reciprocal: instance A has provider `downstream-policy-demo` whose issuer is instance B, and instance B has the reciprocal OIDC client registered for A callbacks. Both base seeds run first, then `dev-federation` wiring, then upstream-only `dev-seed --app-policy-demo`.

**Architecture:** Add an opt-in `--app-policy-demo` mode to the existing loopback-guarded `dev-seed` command. Keep the policy fixture implementation in a focused Go file that performs all account facts, manager assignment, groups, and decisions in one transaction; make the federation harness run base seeds on both instances, wire reciprocity, and pass the policy flag only to the upstream instance.

**Tech Stack:** Go 1.25, Cobra, pgx v5, sqlc-generated queries, PostgreSQL JSONB, embedded Goose migrations, Bash federation harness.

## Global Constraints

- The showcase exists only in federation instance A (`prohibitorum_upstream`), not normal `dev:seed` or instance B.
- Target the existing `dev-app` OIDC client and set `access_restricted = true`.
- Reuse Alice, Bob, Carol, and Dave; do not add policy-only accounts.
- Use stable `demo-*` slugs and preserve unrelated groups, decisions, assignments, credentials, identities, and avatars. Existing named avatar sources are preserved, not overwritten.
- Validate every rule with `appaccess.ParseAndValidateRule` before persistence.
- A stable-slug conflict with the wrong group kind aborts; never delete-and-recreate the conflicting row.
- The policy fixture is atomic: any missing prerequisite, validation error, random-source failure, or database failure rolls back the full showcase transaction.
- Reruns repair named fixture rows and do not duplicate credentials, identities, groups, assignments, or decisions.
- Seeded credential material must not produce a known usable login secret.

---

## File Structure

- Create `cmd/prohibitorum/dev_seed_app_policy.go`: policy-demo constants, rule documents, transactional fixture orchestration, prerequisite lookup, deterministic fact seeding, and group upsert logic.
- Create `cmd/prohibitorum/dev_seed_app_policy_test.go`: PostgreSQL integration coverage for complete data, rule outcomes, idempotency, preservation, and wrong-kind rollback.
- Modify `cmd/prohibitorum/dev_seed.go`: register `--app-policy-demo`, pass the parsed config/pool into the opt-in seeder, and leave the default path unchanged.
- Modify `cmd/prohibitorum/dev_seed_test.go`: assert the opt-in flag is registered and defaults off.
- Modify `scripts/dev-federation.sh`: run both normal `setup_instance` calls base-only, invoke reciprocal `dev-federation`, then run distinct upstream-only `dev-seed --app-policy-demo`.
- Modify `TOOLING.md`: document the upstream-only showcase and its persistence across `--fresh`.

### Task 1: Transactional policy showcase seeder

**Files:**
- Create: `cmd/prohibitorum/dev_seed_app_policy.go`
- Create: `cmd/prohibitorum/dev_seed_app_policy_test.go`

**Interfaces:**
- Consumes: `*pgxpool.Pool`, `configx.Config`, existing `db.Queries`, `appaccess.ParseAndValidateRule`, and base rows seeded by `dev-seed`.
- Produces: `func seedAppPolicyDemo(ctx context.Context, pool *pgxpool.Pool, cfg configx.Config) error`.
- Produces: stable constants `appPolicyDemoClientID = "dev-app"` and `appPolicyDemoManualSlug = "demo-manual-access"` for tests and CLI wiring.

- [ ] **Step 1: Add a PostgreSQL integration-test harness**

In `dev_seed_app_policy_test.go`, create a helper that:

1. Reads `PROHIBITORUM_TEST_DATABASE_URL` and skips when unset.
2. Creates a random PostgreSQL schema.
3. Appends `search_path=<schema>` to a copy of the test DSN.
4. Applies all embedded migrations with `migrations.UpWithResult`.
5. Inserts only the prerequisites: enabled `downstream-policy-demo` provider, `dev-app`, and Alice/Bob/Carol/Dave accounts. Use an issuer pointing at instance B.
6. Returns a schema-scoped `*pgxpool.Pool` and cleanup function.

Use fixed account handles that are unique inside the temporary schema. Build `dev-app` with `oidc.BuildClientParams`; insert the `downstream-policy-demo` provider with the same valid provider-config shape as `seedProviders`.


- [ ] **Step 2: Write the failing complete-fixture test**

Add `TestSeedAppPolicyDemoCreatesCompleteShowcase`. Call:

```go
err := seedAppPolicyDemo(ctx, pool, configx.Config{
    PasswordHashParams: password.DefaultParams(),
})
```

Assert:

- Alice has role `app_manager`.
- `dev-app.access_restricted` is true.
- Alice is the sole seeded manager assignment.
- every persisted rule parses through `appaccess.ParseAndValidateRule` using `downstream-policy-demo` as a known provider.
- exactly the 11 named rule groups exist with the expected exposure flags.
- facts returned by `GetAccountAccessFacts` are:

alice: passkey=true, password_totp=false, federation=true,
       providers=[downstream-policy-demo], protocols=[oidc], any_avatar=true, user_avatar=true
bob:   passkey=false, password_totp=true, federation=false,
       providers=[], protocols=[], any_avatar=true, user_avatar=false
carol: every fact false
```

- `PreviewGroup` yields this matrix for Alice/Bob/Carol:
```text
demo-downstream-connected      true  false false
demo-oidc-connected            true  false false
demo-passkey-login             true  false false
demo-password-totp-login       false true  false
demo-federation-login          true  false false
demo-any-avatar                true  true  false
demo-user-avatar               true  false false
demo-all-strong-profile        true  false false
demo-any-strong-login          true  true  false
demo-no-user-avatar            false true  true
demo-trusted-federated-profile true  false false
```

Assert Dave is absent from every preview result.

- [ ] **Step 3: Run the complete-fixture test to verify it fails**

Run:

```bash
PROHIBITORUM_TEST_DATABASE_URL="$PROHIBITORUM_DATABASE_URL" \
  go test ./cmd/prohibitorum -run '^TestSeedAppPolicyDemoCreatesCompleteShowcase$' -count=1 -v
```

Expected: FAIL because `seedAppPolicyDemo` and the policy constants do not exist.

- [ ] **Step 4: Define the closed showcase rule set**

In `dev_seed_app_policy.go`, define a private `appPolicyDemoGroup` struct:

```go
type appPolicyDemoGroup struct {
    slug        string
    displayName string
    description string
    exposed     bool
    rule        appaccess.Rule
}
```

Return the 11 rules from `appPolicyDemoGroups()` using typed `appaccess.Rule` / `appaccess.Condition` values, not raw JSON strings. Include:

```go
{Version: 1, Condition: appaccess.Condition{Fact: "connection.provider", Provider: "downstream-policy-demo"}}
{Version: 1, Condition: appaccess.Condition{Fact: "connection.protocol", Protocol: "oidc"}}
{Version: 1, Condition: appaccess.Condition{Fact: "login_method", Method: "passkey"}}
{Version: 1, Condition: appaccess.Condition{Fact: "login_method", Method: "password_totp"}}
{Version: 1, Condition: appaccess.Condition{Fact: "login_method", Method: "federation"}}
{Version: 1, Condition: appaccess.Condition{Fact: "avatar", Source: "any"}}
{Version: 1, Condition: appaccess.Condition{Fact: "avatar", Source: "user_uploaded"}}
```

Build exact typed ASTs: `all(passkey, user avatar)`, `any(passkey, password_totp)`, hidden `not(user avatar)`, and nested `all(provider, oidc, any(federation, passkey), not(password_totp))`. Set only `demo-no-user-avatar.exposed = false`; all other rule groups are exposed.

- [ ] **Step 5: Implement prerequisite resolution and transaction ownership**

Implement:

```go
func seedAppPolicyDemo(ctx context.Context, pool *pgxpool.Pool, cfg configx.Config) error
```

Resolve `dev-app`, Alice, Bob, Carol, Dave, and `downstream-policy-demo` by their stable identifiers. Wrap `pgx.ErrNoRows` as an explicit prerequisite error such as:

```text
app-policy demo prerequisite "alice" not found
```

Check that `downstream-policy-demo` is enabled and uses protocol `oidc`; abort otherwise. Its issuer must be instance B and reciprocal OIDC client wiring must already exist.


- [ ] **Step 6: Seed Alice's manager role and deterministic facts**

Preserve Alice's display name, attributes, disabled state, email, and email-verification fields while changing only role through `q.UpdateAccount`.

Ensure one passkey fact without replacing an existing credential:

```go
credentials, err := q.ListCredentialsByAccount(ctx, alice.ID)
if len(credentials) == 0 {
    // generate 32-byte credential ID and 32-byte opaque public-key bytes
    // use alice.WebauthnUserHandle and nickname "App policy demo fixture"
    q.InsertCredential(...)
}
```

Ensure Alice has a confirmed identity from `downstream-policy-demo`:

- inspect `q.ListAccountIdentitiesByAccount(ctx, alice.ID)`;
- if an identity for that provider exists, call `ConfirmAccountIdentity` on it;
- otherwise insert issuer `https://instance-b.example.test` (the configured instance-B origin), subject `dev-seed-app-policy-alice`, email `alice@example.com`, and `{}` upstream data, then confirm it.

Ensure a user-uploaded avatar through `q.UpsertAvatarSource` with source `user`, a tiny valid PNG byte slice, content type `image/png`, and a stable SHA-256-derived ETag. Do not alter Alice's active-avatar selection.

Every random read must use `crypto/rand.Read` and return an error on failure.

- [ ] **Step 7: Seed Bob's password+TOTP and inherited-avatar facts**

If Bob has no password row, generate 32 random bytes, base64url-encode them, hash with `password.HashRaw(secret, cfg.PasswordHashParams)`, discard the plaintext, and insert via `UpsertPasswordCredential`.

If Bob has no TOTP row, insert opaque random ciphertext and a 12-byte nonce with key version 1, the configured period/digits/algorithm, then immediately call `ConfirmTOTPCredential`. The row is intentionally not a recoverable authentication secret; the policy evaluator only observes the confirmed-row invariant. If a TOTP row already exists but is unconfirmed, confirm it without replacing it.

Ensure Bob has an inherited avatar through `q.UpsertAvatarSource` with source `upstream:downstream-policy-demo`, the provider `idp_id`, content type `image/png`, stable ETag, and the same tiny valid PNG fixture. Do not add source `user` and do not alter Bob's active-avatar selection.

Normalize zero-valued test config fields before inserting Bob's TOTP:

```go
period := cfg.TOTP.DefaultPeriod
if period == 0 { period = 30 }
digits := cfg.TOTP.DefaultDigits
if digits == 0 { digits = 6 }
algorithm := cfg.TOTP.DefaultAlgorithm
if algorithm == "" { algorithm = "SHA1" }
```

- [ ] **Step 8: Validate and upsert manager, restriction, groups, and decisions**

- Call `AssignOIDCClientManager` for Alice; it is already `ON CONFLICT DO NOTHING`.
- Call `SetOIDCClientAccessRestricted` with true.
- Load known provider slugs with `ListKnownUpstreamIDPSlugs` into a set.
- For every typed rule: marshal it, parse it with `ParseAndValidateRule`, and marshal the validated value for canonical persistence.
- Load existing `dev-app` groups once and index them by slug.
- Ensure `demo-manual-access` is `kind=manual`; create it if absent, update its display/description/exposure if present, and fail if its existing kind differs.
- For each rule slug, create or update `kind=rule`; fail on a wrong-kind conflict.
- Upsert Bob=`allow` and Carol=`deny` against the manual group. Do not create an Alice decision and do not clear unrelated decisions.

Wrap each error with the operation and stable identifier.

- [ ] **Step 9: Run the complete-fixture test to verify it passes**

Run the Step 3 command again.

Expected: PASS with all fixture, fact, preview-matrix, and disabled-account assertions satisfied.

- [ ] **Step 10: Write idempotency, preservation, and rollback tests**

Add `TestSeedAppPolicyDemoIsIdempotentAndPreservesUnrelatedData`:

1. Create an unrelated `custom-local` rule group on `dev-app` and a second manager assignment before seeding.
2. Run `seedAppPolicyDemo` twice.
3. Assert stable counts for WebAuthn credentials, password/TOTP rows, `downstream-policy-demo` identities, named groups, manager assignments, and manual decisions after the first and second run.
4. Assert the unrelated group and second manager assignment remain unchanged.

Add `TestSeedAppPolicyDemoWrongKindConflictRollsBack`:

1. Insert a manual group with slug `demo-passkey-login` before seeding.
2. Call `seedAppPolicyDemo` and require an error containing `wrong kind` and the slug.
3. Assert Alice is still `user`, `dev-app` remains unrestricted, no manager assignment exists, and none of the other showcase groups or facts committed.

- [ ] **Step 11: Run focused policy-seed tests**

Run:

```bash
PROHIBITORUM_TEST_DATABASE_URL="$PROHIBITORUM_DATABASE_URL" \
  go test ./cmd/prohibitorum -run '^TestSeedAppPolicyDemo' -count=1 -v
```

Expected: all three tests PASS.

- [ ] **Step 12: Commit the transactional seeder**

```bash
git add cmd/prohibitorum/dev_seed_app_policy.go cmd/prohibitorum/dev_seed_app_policy_test.go
git commit -m "feat(dev): seed app policy showcase"
```

### Task 2: Opt-in CLI and upstream-only harness wiring

**Files:**
- Modify: `cmd/prohibitorum/dev_seed.go:73-156`
- Modify: `cmd/prohibitorum/dev_seed_test.go`
- Modify: `scripts/dev-federation.sh:157-188`
- Produces: base-only `setup_instance` calls for A and B, then reciprocal `dev-federation` wiring, then upstream-only `dev-seed --app-policy-demo`; B never receives the flag.
**Interfaces:**
- Consumes: `seedAppPolicyDemo(ctx, conn, config)` from Task 1.
- Produces: `prohibitorum dev-seed --app-policy-demo`.
- Produces: upstream `setup_instance` invocation with `--app-policy-demo`; downstream invocation without it.

- [ ] **Step 1: Write the failing Cobra flag test**

In `dev_seed_test.go`, add:

```go
func TestDevSeedAppPolicyDemoFlagDefaultsOff(t *testing.T) {
    flag := _devSeedCmd.Flags().Lookup("app-policy-demo")
    if flag == nil {
        t.Fatal("dev-seed --app-policy-demo flag is not registered")
    }
    if flag.DefValue != "false" {
        t.Fatalf("default = %q, want false", flag.DefValue)
    }
}
```

- [ ] **Step 2: Run the flag test to verify it fails**

Run:

```bash
go test ./cmd/prohibitorum -run '^TestDevSeedAppPolicyDemoFlagDefaultsOff$' -count=1 -v
```

Expected: FAIL because the flag is absent.

- [ ] **Step 3: Register and execute the opt-in seed**

In `dev_seed.go`:

- add package variable `devSeedAppPolicyDemo bool`;
- register `--app-policy-demo` on `_devSeedCmd` with copy: `Seed the complete app-policy showcase (dev-only).`;
- when the flag is true, execute the policy seeder after the base seed completes. The normal base seed does not enable this flag; the federation harness invokes it separately after reciprocal wiring.

```go
if devSeedAppPolicyDemo {
    fmt.Println("==> dev-seed: seeding app policy showcase")
    if err := seedAppPolicyDemo(ctx, conn, config); err != nil {
        log.Fatalf("seed app policy showcase: %v", err)
    }
}
```

Update the Cobra long description to state that the optional showcase contains the restricted `dev-app`, delegated manager, manual decisions, and complete rule-group set. Do not run it when the flag is false.
- [ ] **Step 4: Run the flag and policy tests**

Run:

```bash
PROHIBITORUM_TEST_DATABASE_URL="$PROHIBITORUM_DATABASE_URL" \
  go test ./cmd/prohibitorum -run '^(TestDevSeedAppPolicyDemoFlagDefaultsOff|TestSeedAppPolicyDemo)' -count=1 -v
```

Expected: PASS.

- [ ] **Step 5: Wire only instance A in the federation script**

Run both normal `setup_instance` calls without policy arguments, then invoke `dev-federation` for reciprocal provider/client wiring, and finally invoke upstream-only `dev-seed --app-policy-demo`:

```bash
setup_instance "$UP_ORIGIN" "$UP_DB" prohibitorum_upstream upstream UP_ENROLL_URL
setup_instance "$DOWN_ORIGIN" "$DOWN_DB" prohibitorum_downstream downstream DOWN_ENROLL_URL
"$BIN" dev-federation --upstream-db "$UP_DB" --downstream-db "$DOWN_DB" \
  --upstream-origin "$UP_ORIGIN" --downstream-origin "$DOWN_ORIGIN"
PROHIBITORUM_PUBLIC_ORIGIN="$UP_ORIGIN" PROHIBITORUM_DATABASE_URL="$UP_DB" \
  "$BIN" dev-seed --app-policy-demo
```

The downstream base seed never receives the policy flag, and the upstream policy invocation is distinct from both base seeds and occurs only after wiring.

- [ ] **Step 6: Check shell syntax**

Run:

```bash
bash -n scripts/dev-federation.sh
```

Expected: exit 0 with no output.

- [ ] **Step 7: Commit CLI and harness wiring**

```bash
git add cmd/prohibitorum/dev_seed.go cmd/prohibitorum/dev_seed_test.go scripts/dev-federation.sh
git commit -m "feat(dev): enable policy demo on federation upstream"
```

### Task 3: Documentation, live load, and end-to-end verification

**Files:**
- Modify: `TOOLING.md:146-166`

**Interfaces:**
- Consumes: the CLI flag and harness wiring from Task 2.
- Produces: current `prohibitorum_upstream` showcase rows and operator documentation.

- [ ] **Step 1: Build the development binary**

Run:

```bash
go build -tags nodynamic -o /tmp/prohibitorum-app-policy-demo ./cmd/prohibitorum
```

Expected: exit 0 and `/tmp/prohibitorum-app-policy-demo` exists.

- [ ] **Step 2: Load the current instance-A database twice**

Source the local federation config and existing shared encryption key, then run the opt-in seed directly without restarting the two servers:

```bash
set -a
. .dev/dev-federation.env
set +a
export PROHIBITORUM_PUBLIC_ORIGIN="https://$DEV_FED_UPSTREAM_HOST"
export PROHIBITORUM_DATABASE_URL='postgres://prohibitorum:prohibitorum@localhost:5432/prohibitorum_upstream?sslmode=disable'
export PROHIBITORUM_DATA_ENCRYPTION_KEY_V1="$(cat .dev/encryption-key)"
/tmp/prohibitorum-app-policy-demo dev-seed --app-policy-demo
/tmp/prohibitorum-app-policy-demo dev-seed --app-policy-demo
```

Expected: both invocations print `dev-seed: done`; the second does not report constraint or duplicate errors.

- [ ] **Step 3: Verify database shape and A-only isolation**

Query `prohibitorum_upstream` through the database container and assert:

```sql
SELECT access_restricted FROM oidc_client WHERE client_id = 'dev-app';
-- true

SELECT a.username, a.role
FROM oidc_client_manager m JOIN account a ON a.id = m.account_id
WHERE m.client_id = 'dev-app' AND a.username = 'alice';
-- alice | app_manager

SELECT kind, slug, exposed_to_downstream
FROM user_group
WHERE oidc_client_id = 'dev-app' AND slug LIKE 'demo-%'
ORDER BY slug;
-- 12 rows: one manual + 11 rules; only demo-no-user-avatar hidden

SELECT a.username, d.effect
FROM group_manual_decision d
JOIN account a ON a.id = d.account_id
JOIN user_group g ON g.id = d.group_id
WHERE g.oidc_client_id = 'dev-app' AND g.slug = 'demo-manual-access'
ORDER BY a.username;
-- bob allow; carol deny
```

Query `prohibitorum_downstream` and assert zero `demo-%` groups and no Alice `dev-app` manager assignment.

- [ ] **Step 4: Verify every live preview through the supported CLI**

For each rule slug, run:

```bash
/tmp/prohibitorum-app-policy-demo oidc-client group preview \
  --client-id dev-app --slug <slug> --limit 500
```

Compare Alice/Bob/Carol with the matrix in the design spec. Confirm Dave is absent. Other local accounts may appear and do not invalidate the named fixture assertions.

Also run:

```bash
/tmp/prohibitorum-app-policy-demo oidc-client group list --client-id dev-app
/tmp/prohibitorum-app-policy-demo oidc-client manager list --client-id dev-app
/tmp/prohibitorum-app-policy-demo oidc-client decision list --client-id dev-app
```

Expected: the complete group list, Alice manager assignment, and Bob/Carol decisions are readable through supported commands.

- [ ] **Step 5: Document the upstream-only showcase**

In `TOOLING.md` under `dev:federation`, add a concise paragraph stating:

- instance A receives the complete `dev-app` app-policy showcase on every start;
- it includes Alice as delegated manager, Bob allow, Carol deny, and leaf/composite rule examples;
- the seed is idempotent and survives reuse or is recreated by `--fresh`;
- instance B and normal `dev:seed` do not receive it;
- operators can run `dev-seed --app-policy-demo` explicitly against a loopback development database.

- [ ] **Step 6: Run focused tests and shell check**

Run:

```bash
PROHIBITORUM_TEST_DATABASE_URL="$PROHIBITORUM_DATABASE_URL" \
  go test ./cmd/prohibitorum -run '^(TestIsLoopbackOrigin|TestDevSeedAppPolicyDemoFlagDefaultsOff|TestSeedAppPolicyDemo)' -count=1 -v
bash -n scripts/dev-federation.sh
```

Expected: all tests PASS; shell syntax exits 0.

- [ ] **Step 7: Run the full package test and smoke the loaded CLI path**

Run:

```bash
go test ./cmd/prohibitorum -count=1
/tmp/prohibitorum-app-policy-demo oidc-client group preview \
  --client-id dev-app --slug demo-trusted-federated-profile --limit 500
```

Expected: package tests PASS; live preview reports Alice=true, Bob=false, Carol=false, and omits Dave.

- [ ] **Step 8: Commit documentation**

```bash
git add TOOLING.md
git commit -m "docs: describe federation policy showcase"
```

- [ ] **Step 9: Request final code review**

Use the `requesting-code-review` skill. Review must check:

- opt-in isolation and loopback safety;
- atomic rollback and wrong-kind conflict behavior;
- no overwrite/deletion of unrelated local data;
- complete fact/combinator coverage;
- A-only live database evidence;
- exact preview matrix and idempotent second run.
