# Delegated Application Managers and App-Bound Groups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace global RBAC groups with app-bound manual/rule groups and let `app_manager` accounts manage access policy only for explicitly assigned downstream applications.

**Architecture:** A destructive PostgreSQL migration resets current app restrictions and installs app-bound groups, manual allow/deny decisions, and per-app manager assignments. A new protocol-neutral `pkg/appaccess` service validates/evaluates bounded rule trees from live account facts and is injected into OIDC, forward-auth, SAML, launchpad, PAT, admin, delegated API, and CLI paths. The Vue dashboard uses one reusable access workspace for global admins and assigned app managers.

**Tech Stack:** Go 1.26, PostgreSQL 18, goose, sqlc 1.30, chi/Huma, Cobra, Vue 3, TypeScript, Pinia, Vue Router, Vitest, Tailwind/shadcn-vue, mise.

## Global Constraints

- Clean cutover only: delete current global groups, memberships, OIDC/SAML access grants, and reset every app's `access_restricted` flag to `false`.
- Every new group is immutably bound to exactly one OIDC client or SAML SP; forward-auth uses its backing OIDC client.
- Each app has at most one manual group and any number of rule groups.
- Manual group decisions are `allow`, `deny`, or absent/neutral; deny wins, allow wins, neutral falls through to OR of all rule groups.
- Rule groups never accept manual membership or rejection and are evaluated from live facts; do not materialize calculated members.
- Supported facts are confirmed provider/protocol connections, `passkey`, `password_totp`, `federation`, `avatar:any`, and `avatar:user_uploaded`.
- Rule limits are version `1`, maximum depth `8`, maximum total nodes `64`, maximum children per combinator `32`, and existing request limit `64 KiB`.
- Manager assignment grants policy-management rights only and never grants app usage.
- Existing global-admin routes remain admin-only; delegated routes return indistinguishable `404` for unassigned, wrong-kind, deleted, and nonexistent apps.
- Group claims are app-aware: manual allow slug plus every exposed matching rule-group slug, only for the owning app and only with the existing OIDC/SAML opt-in.
- No new runtime dependencies, compatibility aliases, deprecated commands, legacy tables, background jobs, or policy caches.
- All database code is generated with `sqlc generate`; never hand-edit `pkg/db/*.sql.go`, `pkg/db/models.go`, or `pkg/db/querier.go`.

---

### Task 1: Destructive RBAC Migration and Generated Queries

**Files:**
- Create: `db/migrations/034_app_manager_rule_groups.sql`
- Create: `db/migrations/app_manager_rule_groups_test.go`
- Replace: `db/queries/rbac.sql`
- Modify generated: `pkg/db/models.go`, `pkg/db/querier.go`, `pkg/db/rbac.sql.go`

**Interfaces:**
- Produces database rows `db.UserGroup`, `db.GroupManualDecision`, `db.OidcClientManager`, and `db.SamlSpManager`.
- Produces sqlc methods used later: app-scoped group CRUD, manual-decision CRUD, manager-assignment CRUD, active account fact lookup/page, app candidate lists, and restriction updates.
- Removes every generated query tied to `group_member`, `oidc_client_access`, `saml_sp_access`, global group listing, and direct account grants.

- [ ] **Step 1: Write the failing migration integration test**

Create a schema-isolated goose test following `vrchat_link_only_enrollment_test.go`. Migrate to version 33, seed one restricted OIDC client, one restricted SAML SP, a legacy group/member, and both legacy access tables. Migrate to 34 and assert destructive reset plus constraints:

```go
func TestAppManagerRuleGroupsMigrationPostgres(t *testing.T) {
    // Use PROHIBITORUM_TEST_DATABASE_URL, a random schema, embedMigrations,
    // goose.UpTo(..., 33), seed legacy RBAC rows, then goose.UpTo(..., 34).

    for _, table := range []string{"group_member", "oidc_client_access", "saml_sp_access"} {
        var exists bool
        err := conn.QueryRowContext(ctx, `
            SELECT to_regclass(current_schema() || '.' || $1) IS NOT NULL`, table,
        ).Scan(&exists)
        if err != nil || exists {
            t.Fatalf("legacy table %s still exists: exists=%v err=%v", table, exists, err)
        }
    }

    var oidcRestricted, samlRestricted bool
    if err := conn.QueryRowContext(ctx, `SELECT access_restricted FROM oidc_client WHERE client_id='legacy-oidc'`).Scan(&oidcRestricted); err != nil { t.Fatal(err) }
    if err := conn.QueryRowContext(ctx, `SELECT access_restricted FROM saml_sp WHERE entity_id='https://legacy.example/saml'`).Scan(&samlRestricted); err != nil { t.Fatal(err) }
    if oidcRestricted || samlRestricted { t.Fatal("cutover must reset every app open") }

    if _, err := conn.ExecContext(ctx, `INSERT INTO account (username,display_name,webauthn_user_handle,role) VALUES ('manager','Manager',decode('aabb','hex'),'app_manager')`); err != nil { t.Fatal(err) }
    var ruleID int32
    if _, err := conn.ExecContext(ctx, `INSERT INTO user_group(kind,slug,display_name,oidc_client_id) VALUES ('manual','manual','Manual','legacy-oidc')`); err != nil { t.Fatal(err) }
    if _, err := conn.ExecContext(ctx, `INSERT INTO user_group(kind,slug,display_name,oidc_client_id) VALUES ('manual','second','Second','legacy-oidc')`); err == nil { t.Fatal("second manual group unexpectedly accepted") }
    if err := conn.QueryRowContext(ctx, `INSERT INTO user_group(kind,slug,display_name,rule,oidc_client_id) VALUES ('rule','passkeys','Passkeys','{"version":1,"condition":{"fact":"login_method","method":"passkey"}}','legacy-oidc') RETURNING id`).Scan(&ruleID); err != nil { t.Fatal(err) }
    if _, err := conn.ExecContext(ctx, `INSERT INTO group_manual_decision(group_id,account_id,effect) SELECT $1,id,'allow' FROM account WHERE username='manager'`, ruleID); err == nil { t.Fatal("manual decision on rule group unexpectedly accepted") }
    if _, err := conn.ExecContext(ctx, `INSERT INTO user_group(kind,slug,display_name,rule,oidc_client_id,saml_sp_id) SELECT 'rule','bad','Bad','{}','legacy-oidc',id FROM saml_sp LIMIT 1`); err == nil { t.Fatal("two app bindings unexpectedly accepted") }
}
```

- [ ] **Step 2: Run the migration test and verify it fails before version 34 exists**

Run:

```bash
PROHIBITORUM_TEST_DATABASE_URL="$PROHIBITORUM_TEST_DATABASE_URL" go test ./db/migrations -run TestAppManagerRuleGroupsMigrationPostgres -count=1
```

Expected: FAIL because goose migration version 34 and the new tables do not exist.

- [ ] **Step 3: Add migration 034**

Implement the approved schema exactly. The up migration must drop old access/group tables, recreate `user_group`, add manager tables and `group_manual_decision`, replace account/enrollment role checks, and reset app flags:

```sql
-- +goose Up
DROP TABLE IF EXISTS saml_sp_access;
DROP TABLE IF EXISTS oidc_client_access;
DROP TABLE IF EXISTS group_member;
DROP TABLE IF EXISTS user_group;

UPDATE oidc_client SET access_restricted = false WHERE access_restricted;
UPDATE saml_sp SET access_restricted = false WHERE access_restricted;

ALTER TABLE account DROP CONSTRAINT account_role_check;
ALTER TABLE account ADD CONSTRAINT account_role_check
  CHECK (role IN ('user', 'app_manager', 'admin'));
ALTER TABLE enrollment DROP CONSTRAINT enrollment_template_role_check;
ALTER TABLE enrollment ADD CONSTRAINT enrollment_template_role_check
  CHECK (template_role IN ('user', 'app_manager', 'admin'));

CREATE TABLE oidc_client_manager (
  client_id text NOT NULL REFERENCES oidc_client(client_id) ON DELETE CASCADE,
  account_id integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by integer REFERENCES account(id) ON DELETE SET NULL,
  PRIMARY KEY (client_id, account_id)
);
CREATE TABLE saml_sp_manager (
  saml_sp_id bigint NOT NULL REFERENCES saml_sp(id) ON DELETE CASCADE,
  account_id integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  created_by integer REFERENCES account(id) ON DELETE SET NULL,
  PRIMARY KEY (saml_sp_id, account_id)
);
CREATE TABLE user_group (
  id serial PRIMARY KEY,
  kind text NOT NULL CHECK (kind IN ('manual','rule')),
  slug text NOT NULL CHECK (slug ~ '^[a-z0-9](-?[a-z0-9])*$'),
  display_name text NOT NULL,
  description text,
  exposed_to_downstream boolean NOT NULL DEFAULT true,
  rule jsonb,
  oidc_client_id text REFERENCES oidc_client(client_id) ON DELETE CASCADE,
  saml_sp_id bigint REFERENCES saml_sp(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (id, kind),
  CHECK (num_nonnulls(oidc_client_id, saml_sp_id) = 1),
  CHECK ((kind='manual' AND rule IS NULL) OR
         (kind='rule' AND rule IS NOT NULL AND jsonb_typeof(rule)='object'))
);
CREATE UNIQUE INDEX user_group_oidc_slug_uq ON user_group(oidc_client_id,slug) WHERE oidc_client_id IS NOT NULL;
CREATE UNIQUE INDEX user_group_saml_slug_uq ON user_group(saml_sp_id,slug) WHERE saml_sp_id IS NOT NULL;
CREATE UNIQUE INDEX user_group_oidc_manual_uq ON user_group(oidc_client_id) WHERE kind='manual';
CREATE UNIQUE INDEX user_group_saml_manual_uq ON user_group(saml_sp_id) WHERE kind='manual';
CREATE TABLE group_manual_decision (
  group_id integer NOT NULL,
  group_kind text NOT NULL DEFAULT 'manual' CHECK (group_kind='manual'),
  account_id integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  effect text NOT NULL CHECK (effect IN ('allow','deny')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by integer REFERENCES account(id) ON DELETE SET NULL,
  PRIMARY KEY (group_id,account_id),
  FOREIGN KEY (group_id,group_kind) REFERENCES user_group(id,kind) ON DELETE CASCADE
);
```

Use the actual constraint names from `\d account`/migration history rather than assuming a generated name; the test must pin them. The down migration restores old table shapes and the `user|admin` checks but explicitly cannot restore deleted rows.

- [ ] **Step 4: Replace RBAC SQL with app-bound queries**

Define narrowly named sqlc operations. The required query surface is:

```sql
-- group CRUD scoped by immutable app binding
-- CreateOIDCAppGroup, CreateSAMLAppGroup
-- GetOIDCAppGroup, GetSAMLAppGroup
-- ListOIDCAppGroups, ListSAMLAppGroups
-- UpdateAppGroup (mutable fields only), DeleteOIDCAppGroup, DeleteSAMLAppGroup

-- manual decisions
-- GetManualDecisionForOIDCApp, GetManualDecisionForSAMLApp
-- ListManualDecisionsPage
-- UpsertManualDecision, ClearManualDecision

-- manager assignments
-- ListOIDCClientManagers, AssignOIDCClientManager, RemoveOIDCClientManager
-- ListSAMLSPManagers, AssignSAMLSPManager, RemoveSAMLSPManager
-- IsOIDCClientManager, IsSAMLSPManager, DeleteManagerAssignmentsForAccount

-- evaluator inputs
-- GetAccountAccessFacts
-- ListActiveAccountAccessFactsPage
-- ListOIDCAppRuleGroups, ListSAMLAppRuleGroups
-- ListOIDCAccessCandidates, ListForwardAuthAccessCandidates, ListSAMLAccessCandidates

-- existing toggle retained
-- SetOIDCClientAccessRestricted, SetSAMLSPAccessRestricted
```

`GetAccountAccessFacts` and the paginated form return booleans and arrays, not raw credential/provider rows:

```sql
SELECT a.id, a.username, a.display_name, a.disabled,
  EXISTS (SELECT 1 FROM webauthn_credential w WHERE w.account_id=a.id) AS has_passkey,
  (EXISTS (SELECT 1 FROM password_credential p WHERE p.account_id=a.id)
   AND EXISTS (SELECT 1 FROM totp_credential t WHERE t.account_id=a.id AND t.confirmed_at IS NOT NULL)) AS has_password_totp,
  EXISTS (
    SELECT 1 FROM account_identity ai JOIN upstream_idp ip ON ip.id=ai.upstream_idp_id
    WHERE ai.account_id=a.id AND ai.confirmed_at IS NOT NULL
      AND NOT ip.disabled AND ip.protocol <> 'vrchat'
  ) AS has_federation,
  -- Return sorted confirmed provider slugs/protocols and avatar booleans.
FROM account a
WHERE a.id = sqlc.arg(account_id);
```

- [ ] **Step 5: Generate database code**

Run:

```bash
sqlc generate
```

Expected: PASS; obsolete RBAC methods disappear from `db.Querier`, new methods and row types compile.

- [ ] **Step 6: Run migration and generated-query checks**

Run:

```bash
go test ./db/migrations -run TestAppManagerRuleGroupsMigrationPostgres -count=1
go test ./pkg/db ./pkg/server -run '^$'
```

Expected: migration test PASS; compile-only server check may fail only at old call sites removed by later tasks. Record those call sites in the task notes; do not add compatibility queries.

- [ ] **Step 7: Commit the database cutover**

```bash
git add db/migrations/034_app_manager_rule_groups.sql db/migrations/app_manager_rule_groups_test.go db/queries/rbac.sql pkg/db
git commit -m "feat: replace shared RBAC with app-bound groups"
```

---

### Task 2: Bounded Rule AST and Pure Evaluator

**Files:**
- Create: `pkg/appaccess/rule.go`
- Create: `pkg/appaccess/rule_test.go`
- Create: `pkg/appaccess/decision.go`
- Create: `pkg/appaccess/decision_test.go`

**Interfaces:**
- Produces `Rule`, `Condition`, `Facts`, `Explanation`, `ManualEffect`, `GroupMatch`, and `Decision`.
- Produces `ParseAndValidateRule(raw []byte, knownProviders map[string]struct{}) (Rule, error)`.
- Produces `EvaluateCondition(c Condition, facts Facts) Explanation` and `Decide(restricted bool, manual ManualEffect, matches []GroupMatch) Decision`.
- No database, HTTP, protocol, or server imports.

- [ ] **Step 1: Write failing parser/validation tests**

Cover valid nested rules and every closed-schema boundary:

```go
func TestParseAndValidateRuleRejectsUnknownAndBounds(t *testing.T) {
    known := map[string]struct{}{"corporate": {}}
    cases := []struct{ name, raw, reason string }{
        {"unknown field", `{"version":1,"condition":{"fact":"avatar","source":"any","secret":true}}`, "unknown_field"},
        {"empty all", `{"version":1,"condition":{"op":"all","children":[]}}`, "empty_children"},
        {"bad provider", `{"version":1,"condition":{"fact":"connection.provider","provider":"missing"}}`, "provider_not_found"},
        {"bad method", `{"version":1,"condition":{"fact":"login_method","method":"sms"}}`, "invalid_method"},
    }
    for _, tc := range cases {
        _, err := ParseAndValidateRule([]byte(tc.raw), known)
        var re *RuleError
        if !errors.As(err, &re) || re.Reason != tc.reason { t.Fatalf("%s: %#v", tc.name, err) }
    }
    // Generate depth 9, 65 nodes, and 33 children; assert stable reasons.
}
```

- [ ] **Step 2: Run parser tests and verify failure**

```bash
go test ./pkg/appaccess -run 'TestParseAndValidateRule' -count=1
```

Expected: FAIL because `pkg/appaccess` does not exist.

- [ ] **Step 3: Implement the closed AST and validator**

Use pointer fields so node shape can be validated without conflating absent and zero values. Decode with `json.Decoder.DisallowUnknownFields()` and reject trailing JSON:

```go
type Rule struct {
    Version   int       `json:"version"`
    Condition Condition `json:"condition"`
}

type Condition struct {
    Op       string      `json:"op,omitempty"`
    Children []Condition `json:"children,omitempty"`
    Child    *Condition  `json:"child,omitempty"`
    Fact     string      `json:"fact,omitempty"`
    Provider string      `json:"provider,omitempty"`
    Protocol string      `json:"protocol,omitempty"`
    Method   string      `json:"method,omitempty"`
    Source   string      `json:"source,omitempty"`
}

type RuleError struct { Path, Reason string }
func (e *RuleError) Error() string { return "invalid group rule at " + e.Path + ": " + e.Reason }
```

Validation constants are `maxRuleDepth=8`, `maxRuleNodes=64`, and `maxRuleChildren=32`. Enforce exactly one node shape: combinator or fact leaf, never both.

- [ ] **Step 4: Write failing evaluator and precedence tests**

```go
func TestDecideManualAndRulePrecedence(t *testing.T) {
    matches := []GroupMatch{{ID: 10, Slug: "passkey", Exposed: true, Matched: true}}
    tests := []struct{
        name string; restricted bool; manual ManualEffect; want bool; source DecisionSource
    }{
        {"open ignores policy", false, ManualDeny, true, SourceOpen},
        {"deny wins", true, ManualDeny, false, SourceManualDeny},
        {"allow wins", true, ManualAllow, true, SourceManualAllow},
        {"neutral rule OR", true, ManualNeutral, true, SourceRule},
        {"neutral no match", true, ManualNeutral, false, SourceNoMatch},
    }
    for _, tc := range tests {
        got := Decide(tc.restricted, tc.manual, matches)
        if got.Allowed != tc.want || got.Source != tc.source { t.Fatalf("%s: %#v", tc.name, got) }
    }
}
```

Also assert `false OR true` allows, every true rule is returned for claims, and manual allow keeps matching rule slugs.

- [ ] **Step 5: Implement evaluator, explanation, and decision projection**

```go
type Facts struct {
    ConfirmedProviders map[string]struct{}
    ConfirmedProtocols map[string]struct{}
    HasPasskey         bool
    HasPasswordTOTP    bool
    HasFederation      bool
    HasAnyAvatar       bool
    HasUserAvatar      bool
}

type Explanation struct {
    Path     string        `json:"path"`
    Label    string        `json:"label"`
    Result   bool          `json:"result"`
    Children []Explanation `json:"children,omitempty"`
}

type Decision struct {
    Allowed            bool
    Source             DecisionSource
    ManualGroup        *GroupMatch
    MatchingRuleGroups []GroupMatch
}

func (d Decision) ExposedGroupSlugs() []string {
    seen := map[string]struct{}{}
    if d.Source == SourceManualAllow && d.ManualGroup != nil && d.ManualGroup.Exposed {
        seen[d.ManualGroup.Slug] = struct{}{}
    }
    for _, group := range d.MatchingRuleGroups {
        if group.Exposed { seen[group.Slug] = struct{}{} }
    }
    out := make([]string, 0, len(seen))
    for slug := range seen { out = append(out, slug) }
    sort.Strings(out)
    return out
}
```

`Decide(false, ...)` always allows because policy is inactive. For restricted apps, deny and allow short-circuit access but do not discard already calculated rule matches. `ExposedGroupSlugs` adds the exposed manual-group slug only for manual allow, adds every exposed matching rule-group slug, sorts, and deduplicates.

- [ ] **Step 6: Run focused package tests**

```bash
go test ./pkg/appaccess -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit the pure policy core**

```bash
git add pkg/appaccess
git commit -m "feat: add bounded app access rule evaluator"
```

---

### Task 3: Live Fact Loader and App Access Service

**Files:**
- Create: `pkg/appaccess/service.go`
- Create: `pkg/appaccess/service_test.go`
- Create: `pkg/appaccess/facts.go`
- Create: `pkg/appaccess/facts_test.go`

**Interfaces:**
- Consumes Task 1 sqlc rows and Task 2 evaluator.
- Produces `AppKind`, `AppRef`, `AppSummary`, `OIDCAuthorizer`, `SAMLAuthorizer`, `AppLister`, and `Service`.
- Protocol packages consume narrow interfaces; the concrete `Service` satisfies all three:

```go
type OIDCAuthorizer interface {
    EvaluateOIDC(context.Context, int32, string) (Decision, error)
}
type SAMLAuthorizer interface {
    EvaluateSAML(context.Context, int32, int64) (Decision, error)
}
type AppLister interface {
    ListAllowedApps(context.Context, int32) ([]AppSummary, error)
}
```

- Produces delegated helpers `AuthorizeManager`, `PreviewGroup`, `ExplainGroup`, and app-aware projected slugs through `Decision.ExposedGroupSlugs()`.

- [ ] **Step 1: Write failing fact projection tests**

Use generated row values to pin normalization and disabled-account behavior:

```go
func TestFactsFromRow(t *testing.T) {
    row := db.GetAccountAccessFactsRow{
        ID: 7, Disabled: false, HasPasskey: true, HasPasswordTotp: true,
        ConfirmedProviderSlugs: []string{"corp", "steam"},
        ConfirmedProtocols: []string{"oidc", "steam"},
        HasFederation: true, HasAnyAvatar: true, HasUserAvatar: false,
    }
    facts, err := factsFromRow(row)
    if err != nil || !facts.HasPasskey || facts.HasUserAvatar { t.Fatalf("facts=%#v err=%v", facts, err) }
}
```

- [ ] **Step 2: Write failing service decision tests with a narrow fake**

Define a private `queries` interface containing only generated methods used by the service. Test open, manual deny, manual allow plus matching claims, neutral multi-rule OR, missing app, malformed persisted rule fail-closed, and app-aware group isolation.

```go
func TestServiceEvaluateOIDCManualAllowStillProjectsRuleMatches(t *testing.T) {
    q := fakeQueries{
        oidcApp: restrictedOIDC("wiki"),
        manual: effect("allow"),
        groups: []db.UserGroup{ruleGroup(2, "passkeys", passkeyRuleJSON, true)},
        facts: accessFacts(42, withPasskey()),
    }
    got, err := NewService(&q).EvaluateOIDC(context.Background(), 42, "wiki")
    if err != nil || !got.Allowed || got.Source != SourceManualAllow { t.Fatalf("%#v %v", got, err) }
    assertSlugs(t, got.MatchingRuleGroups, "passkeys")
}
```

- [ ] **Step 3: Run service tests and verify failure**

```bash
go test ./pkg/appaccess -run 'TestFacts|TestService' -count=1
```

Expected: FAIL because service/fact loader are absent.

- [ ] **Step 4: Implement facts and protocol-neutral service**

```go
type AppKind string
const (
    KindOIDC AppKind = "oidc"
    KindForwardAuth AppKind = "forward_auth"
    KindSAML AppKind = "saml"
)

type AppRef struct {
    Kind AppKind
    OIDCClientID string
    SAMLSPID int64
}

type Scope struct {
    Name string
    Description string
}

type AppSummary struct {
    Ref AppRef
    DisplayName string
    LaunchURL string
    RedirectURIs []string
    EntityID string
    ForwardAuthHost string
    ForwardAuthScopes []Scope
    AccessRestricted bool
}

type Service struct { q queries }
func NewService(q queries) *Service { return &Service{q: q} }
```

`EvaluateOIDC` verifies the OIDC/forward-auth row kind, loads one fact snapshot, manual decision, and all bound rule groups once, then calls Task 2. `EvaluateSAML` is symmetric. Parse persisted rules fail-closed. Return `pgx.ErrNoRows` unchanged for callers to map safely.

`AuthorizeManager` accepts global admins immediately; for `app_manager`, it checks the matching assignment table and app kind. Other roles and unassigned apps return a package sentinel that HTTP maps to generic app-not-found.

`ListAllowedApps` loads all enabled OIDC, forward-auth, and launchable SAML candidates, evaluates each with the same account facts, and omits denied apps.

- [ ] **Step 5: Implement paginated preview and explanation**

`PreviewGroup` requires a rule group scoped to the requested app, loads one page from `ListActiveAccountAccessFactsPage`, evaluates the rule, and returns only safe account summary plus `matched`. `ExplainGroup` returns the Task 2 explanation for one active account. Neither method returns identity subjects, emails, metadata, credential rows, or arbitrary attributes.

- [ ] **Step 6: Run appaccess tests**

```bash
go test ./pkg/appaccess -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit the live policy service**

```bash
git add pkg/appaccess
git commit -m "feat: evaluate app policy from live account facts"
```

---

### Task 4: Role Lifecycle and Manager Assignment APIs

**Files:**
- Modify: `pkg/contract/auth.go`
- Create: `pkg/contract/appaccess.go`
- Modify: `pkg/authn/middleware.go`
- Modify: `pkg/authn/middleware_test.go`
- Modify: `pkg/authn/errors.go`
- Modify: `pkg/server/handle_account.go`
- Modify: `pkg/server/handle_account_test.go`
- Create: `pkg/server/handle_admin_app_managers.go`
- Create: `pkg/server/handle_admin_app_managers_test.go`
- Modify: `pkg/server/server.go`
- Modify: `pkg/audit/event.go`

**Interfaces:**
- Consumes manager sqlc methods and `appaccess.AppKind`.
- Produces auth requirement `contract.AuthAppManager` accepting `app_manager|admin`; per-app assignment remains handler/service enforced.
- Produces manager assignment endpoints for OIDC, forward-auth, and SAML admin app pages.

- [ ] **Step 1: Write failing role middleware and account validation tests**

```go
func TestCheckAppManager(t *testing.T) {
    req := contract.AuthRequirement{Kind: contract.AuthAppManager}
    for _, role := range []string{"app_manager", "admin"} {
        if err := Check(&Session{Account: &db.Account{Role: role}}, req); err != nil { t.Fatalf("%s: %v", role, err) }
    }
    if ae := AsAuthError(Check(&Session{Account: &db.Account{Role: "user"}}, req)); ae == nil || ae.Code != "not_app_manager" {
        t.Fatalf("unexpected error: %#v", ae)
    }
}
```

Extend account/invitation tests to accept `app_manager`, include it in `invalid_role.details.allowed`, and assert demotion calls `DeleteManagerAssignmentsForAccount` inside the account-update transaction.

- [ ] **Step 2: Run focused tests and verify failure**

```bash
go test ./pkg/authn ./pkg/server -run 'AppManager|InvalidRole|UpdateAccount' -count=1
```

Expected: FAIL on unknown role/requirement and missing cleanup.

- [ ] **Step 3: Implement role domain and transactional cleanup**

Add `AuthAppManager`, `ErrNotAppManager`, and allowed roles `[]string{"user","app_manager","admin"}`. Update all role validation in account update and invitation creation. Preserve last-admin invariants exactly; `app_manager` is non-admin for those checks. When current role is `app_manager` and new role is not, delete OIDC/SAML manager rows in the same transaction before commit.

- [ ] **Step 4: Write failing assignment endpoint tests**

Test admin list, sudo-required assign/remove, exact app-kind validation, target role validation, idempotent assign, missing removal, audit fields, and forward-auth isolation:

```go
func TestAssignOIDCManagerRejectsNonManagerRole(t *testing.T) {
    // Seed target account role=user and existing OIDC app.
    rr := runAdminSudoRequest(t, s, "POST", "/api/prohibitorum/oidc-applications/wiki/managers", `{"accountId":7}`)
    assertAPIError(t, rr, http.StatusBadRequest, "invalid_manager_role")
}
```

- [ ] **Step 5: Implement manager assignment contracts and handlers**

Expose the same shape for all app kinds:

```go
type AppManagerView struct {
    ID int32 `json:"id"`
    Username string `json:"username"`
    DisplayName string `json:"displayName"`
    Disabled bool `json:"disabled"`
    AssignedAt time.Time `json:"assignedAt"`
}
```

Routes:

```text
GET/POST /oidc-applications/{clientId}/managers
POST     /oidc-applications/{clientId}/managers/remove
GET/POST /forward-auth-apps/{clientId}/managers
POST     /forward-auth-apps/{clientId}/managers/remove
GET/POST /saml-applications/{id}/managers
POST     /saml-applications/{id}/managers/remove
```

GET is admin-only; POST assign/remove use `registerSudoOpHTTP`. Validate target account is active `app_manager`. Record actor, target account, app kind/id, and event only.

- [ ] **Step 6: Run role and manager tests**

```bash
go test ./pkg/authn ./pkg/server -run 'AppManager|ManagerAssignment|InvalidRole|UpdateAccount' -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit role and assignment support**

```bash
git add pkg/contract pkg/authn pkg/server/handle_account.go pkg/server/handle_account_test.go pkg/server/handle_admin_app_managers.go pkg/server/handle_admin_app_managers_test.go pkg/server/server.go pkg/audit/event.go
git commit -m "feat: add scoped application manager assignments"
```

---

### Task 5: App-Bound Group and Delegated Management APIs

**Files:**
- Create: `pkg/server/handle_app_policies.go`
- Create: `pkg/server/handle_app_policies_test.go`
- Create: `pkg/server/handle_managed_applications.go`
- Create: `pkg/server/handle_managed_applications_test.go`
- Modify: `pkg/contract/appaccess.go`
- Modify: `pkg/server/server.go`
- Modify: `pkg/server/handle_nested_pagination.go`
- Modify: `pkg/server/handle_nested_pagination_test.go`
- Remove: `pkg/server/handle_admin_groups.go`
- Remove: `pkg/server/handle_admin_groups_test.go`
- Remove: `pkg/server/handle_admin_app_access.go`
- Remove: `pkg/server/handle_admin_app_access_test.go`
- Modify: `pkg/authn/errors.go`
- Modify: `pkg/audit/event.go`

**Interfaces:**
- Consumes `appaccess.Service`, group/decision sqlc queries, and `AuthAppManager`.
- Produces admin and delegated access-workspace JSON contracts used by frontend tasks.
- Removes global `/groups`, `/accounts/{id}/groups`, and old `/access/grant|revoke` APIs.

- [ ] **Step 1: Write failing scoped authorization tests**

Table-test every delegated route against assigned, unassigned, wrong-kind, missing, `user`, disabled manager, and global admin sessions. Pin non-enumeration:

```go
func TestManagedAppUnassignedAndMissingAreIndistinguishable(t *testing.T) {
    for _, path := range []string{
        "/api/prohibitorum/managed-applications/oidc/secret/access",
        "/api/prohibitorum/managed-applications/oidc/missing/access",
    } {
        rr := managedRequest(t, s, "GET", path, "", appManagerSession(7))
        assertAPIError(t, rr, http.StatusNotFound, "client_not_found")
    }
}
```

- [ ] **Step 2: Write failing group/manual/rule API tests**

Cover immutable binding, slug uniqueness within app but reuse across apps, one-manual-group `409`, decision upsert/clear, decision rejection on rule group, rule validation errors `{path,reason}`, calculated preview pagination, safe explanation fields, restriction toggle, and audit.

```go
func TestCreateSecondManualGroupConflicts(t *testing.T) {
    rr := managedRequest(t, s, "POST", managedOIDC("wiki", "/groups"),
        `{"kind":"manual","slug":"exceptions","displayName":"Exceptions"}`, assignedManagerSession())
    assertAPIError(t, rr, http.StatusConflict, "manual_group_exists")
}
```

- [ ] **Step 3: Run endpoint tests and verify failure**

```bash
go test ./pkg/server -run 'ManagedApp|AppPolicy|ManualGroup|GroupRule' -count=1
```

Expected: FAIL because routes/handlers are absent.

- [ ] **Step 4: Define wire contracts**

Add closed JSON views:

```go
type AppAccessWorkspace struct {
    App AppSummaryView `json:"app"`
    AccessRestricted bool `json:"accessRestricted"`
    ManualGroup *AppGroupView `json:"manualGroup,omitempty"`
    RuleGroups []AppGroupView `json:"ruleGroups"`
}

type AppGroupView struct {
    ID int32 `json:"id"`
    Kind string `json:"kind"`
    Slug string `json:"slug"`
    DisplayName string `json:"displayName"`
    Description string `json:"description,omitempty"`
    ExposedToDownstream bool `json:"exposedToDownstream"`
    Rule *appaccess.Rule `json:"rule,omitempty"`
}

type ManualDecisionView struct {
    Account AccountSummaryView `json:"account"`
    Effect string `json:"effect"`
    UpdatedAt time.Time `json:"updatedAt"`
}
```

Keep internal package types out of public contracts if that would create a dependency cycle; mirror the rule wire struct in `contract` and convert explicitly.

- [ ] **Step 5: Implement shared app-policy handlers**

One internal handler layer accepts `(actor, AppRef)` and calls `AuthorizeManager` before app/group lookup. Global admin routes call the same layer with admin authority. Register delegated routes under:

```text
GET  /managed-applications
GET  /managed-applications/{kind}/{appId}/access
POST /managed-applications/{kind}/{appId}/access/set-restricted
GET  /managed-applications/{kind}/{appId}/groups
POST /managed-applications/{kind}/{appId}/groups
GET  /managed-applications/{kind}/{appId}/groups/{groupId}
PUT  /managed-applications/{kind}/{appId}/groups/{groupId}
POST /managed-applications/{kind}/{appId}/groups/{groupId}/delete
GET  /managed-applications/{kind}/{appId}/groups/{groupId}/decisions
POST /managed-applications/{kind}/{appId}/groups/{groupId}/decisions
POST /managed-applications/{kind}/{appId}/groups/{groupId}/decisions/clear
GET  /managed-applications/{kind}/{appId}/groups/{groupId}/preview
GET  /managed-applications/{kind}/{appId}/groups/{groupId}/explain/{accountId}
GET  /managed-applications/{kind}/{appId}/accounts
```

Use existing content-type/body-limit wrappers. Mutations do not require sudo. Map DB constraints to `manual_group_exists`, `group_slug_conflict`, `group_not_found`, and safe `invalid_group_rule` details.

- [ ] **Step 6: Remove obsolete global group/access surfaces**

Delete old handlers/tests/routes/contracts and remove group/access methods from `nestedQueries`, its fake, and pagination handlers. Remove account-group listing and old app access grant pagination. Do not leave aliases or forwarding routes.

- [ ] **Step 7: Run server package tests**

```bash
go test ./pkg/server -count=1
```

Expected: PASS, including route-policy and Huma error-envelope tests.

- [ ] **Step 8: Commit delegated policy APIs**

```bash
git add pkg/server pkg/contract pkg/authn/errors.go pkg/audit/event.go
git commit -m "feat: add delegated app policy management API"
```

---

### Task 6: OIDC, Refresh, Userinfo, and Forward-Auth Enforcement

**Files:**
- Modify: `pkg/protocol/oidc/oidc.go`
- Modify: `pkg/protocol/oidc/authorize.go`
- Modify: `pkg/protocol/oidc/authorize_test.go`
- Modify: `pkg/protocol/oidc/refresh.go`
- Modify: `pkg/protocol/oidc/refresh_test.go`
- Modify: `pkg/protocol/oidc/token.go`
- Modify: `pkg/protocol/oidc/token_test.go`
- Modify: `pkg/protocol/oidc/userinfo.go`
- Modify: `pkg/protocol/oidc/userinfo_test.go`
- Modify: `pkg/protocol/oidc/forward_auth.go`
- Modify: `pkg/protocol/oidc/forward_auth_test.go`
- Modify: `pkg/server/server.go`

**Interfaces:**
- Consumes `appaccess.OIDCAuthorizer` from Task 3.
- `oidc.New(...)` gains an `appaccess.OIDCAuthorizer` argument and stores it as `access`.
- Removes protocol use of `IsAccountAuthorizedForOIDCClient` and `ListExposedGroupSlugsByAccount`.

- [ ] **Step 1: Replace OIDC test fakes with an authorizer fake and add failing claim tests**

```go
type fakeAuthorizer struct {
    oidc map[string]appaccess.Decision
    err error
}
func (f *fakeAuthorizer) EvaluateOIDC(_ context.Context, _ int32, clientID string) (appaccess.Decision, error) {
    if f.err != nil { return appaccess.Decision{}, f.err }
    return f.oidc[clientID], nil
}
```

Add tests proving manual deny, rule allow, fail-closed evaluation error, refresh revocation, and group projection containing manual plus all matching exposed rules only for the current client.

- [ ] **Step 2: Run focused OIDC tests and verify failure**

```bash
go test ./pkg/protocol/oidc -run 'AppAccess|Group|ForwardAuth.*Denied|Refresh.*Denied' -count=1
```

Expected: FAIL until provider wiring and call sites change.

- [ ] **Step 3: Inject the authorizer and replace authorize/refresh predicates**

Add the narrow field without changing the existing dependencies:

```go
type Provider struct {
    cfg *configx.Config
    queries db.Querier
    kv kv.Store
    deks map[int][]byte
    sessions *session.SessionStore
    audit audit.Writer
    rl *authn.RateLimiter
    keys *keyCache
    maintenance func(context.Context) bool
    clientIP func(*http.Request) string
    access appaccess.OIDCAuthorizer
}

func New(cfg *configx.Config, queries db.Querier, kvStore kv.Store,
    sessions *session.SessionStore, auditW audit.Writer, rl *authn.RateLimiter,
    clientIP func(*http.Request) string, access appaccess.OIDCAuthorizer) *Provider
```

Authorize and refresh call `EvaluateOIDC`. Preserve current interactive/silent denial and refresh-family revocation. Audit reason becomes `manual_deny` or `no_matching_group` from `Decision.Source`, without logging evaluated facts.

- [ ] **Step 4: Make token and userinfo groups app-aware**

When `groups` is granted, derive sorted slugs from the decision returned for the current client. Include the manual slug only for manual allow and every exposed matching rule slug even when manual allow determined access. Present-but-empty `[]` behavior remains.

- [ ] **Step 5: Replace forward-auth cookie and PAT checks**

Both cookie-session and PAT paths call `EvaluateOIDC` for the backing client and use its projected groups in `Remote-Groups`. Preserve fail-closed behavior and current audit principal kinds.

- [ ] **Step 6: Wire one service instance in server bootstrap**

```go
accessService := appaccess.NewService(queries)
// pass accessService to oidcop.New and retain it on Server for management APIs
```

Do not instantiate separate services per protocol.

- [ ] **Step 7: Run the full OIDC package**

```bash
go test ./pkg/protocol/oidc -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit OIDC and forward-auth enforcement**

```bash
git add pkg/protocol/oidc pkg/server/server.go
git commit -m "feat: enforce live app policy in oidc and forward auth"
```

---

### Task 7: SAML, Launchpad, PAT Candidate, and Claim Enforcement

**Files:**
- Modify: `pkg/protocol/saml/saml.go`
- Modify: `pkg/protocol/saml/sso.go`
- Modify: `pkg/protocol/saml/sso_test.go`
- Modify: `pkg/protocol/saml/consent_saml.go`
- Modify: `pkg/protocol/saml/consent_saml_test.go`
- Modify: `pkg/protocol/saml/assertion.go`
- Modify: `pkg/protocol/saml/assertion_test.go`
- Modify: `pkg/server/handle_me_apps.go`
- Modify: `pkg/server/handle_me_apps_test.go`
- Modify: `pkg/server/handle_me_tokens.go`
- Modify: `pkg/server/handle_me_tokens_test.go`
- Modify: `pkg/server/server.go`

**Interfaces:**
- Consumes the same concrete service through `appaccess.SAMLAuthorizer` and `appaccess.AppLister`.
- `saml.NewIdP(...)` gains an `appaccess.SAMLAuthorizer` argument.
- Launchpad and PAT candidate handlers consume `AppLister.ListAllowedApps` rather than SQL authorization predicates.

- [ ] **Step 1: Add failing SAML decision and claims tests**

Pin interactive redirect, passive `Responder/RequestDenied`, consent-resume recheck, fail-closed errors, and app-aware group attributes:

```go
func TestSAMLManualAllowProjectsRuleGroups(t *testing.T) {
    access := fakeAuthorizer{saml: appaccess.Decision{
        Allowed: true, Source: appaccess.SourceManualAllow,
        ManualGroup: &appaccess.GroupMatch{Slug:"manual", Exposed:true},
        MatchingRuleGroups: []appaccess.GroupMatch{{Slug:"passkeys", Exposed:true}},
    }}
    // Issue assertion for SP 7 and assert both multi-valued group attributes.
}
```

- [ ] **Step 2: Add failing launchpad/PAT list tests**

Seed candidate rows for open, manual-denied, and rule-allowed apps; assert only allowed apps appear and forward-auth PAT scope creation rejects a now-denied app.

- [ ] **Step 3: Run focused tests and verify failure**

```bash
go test ./pkg/protocol/saml ./pkg/server -run 'AppAccess|Group|Launchpad|ForwardAuthApps' -count=1
```

Expected: FAIL until authorizer wiring replaces SQL predicates.

- [ ] **Step 4: Inject and use the authorizer in SAML**

Add `access appaccess.SAMLAuthorizer` to `IdP`. SSO and consent resume call `EvaluateSAML`; assertion construction uses the returned exposed matching slugs. Preserve current passive/interactive behavior and audit envelope.

- [ ] **Step 5: Replace launchpad and PAT candidate SQL authorization**

`handleListMyApps` calls `ListAllowedApps` once and maps `AppSummary` to existing `contract.LaunchpadApp`. `handleListMyForwardAuthApps` and PAT create validation use the allowed forward-auth subset and unchanged scope vocabulary checks.

- [ ] **Step 6: Run SAML and server tests**

```bash
go test ./pkg/protocol/saml ./pkg/server -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit remaining enforcement paths**

```bash
git add pkg/protocol/saml pkg/server/handle_me_apps.go pkg/server/handle_me_apps_test.go pkg/server/handle_me_tokens.go pkg/server/handle_me_tokens_test.go pkg/server/server.go
git commit -m "feat: enforce live app policy in saml and app listings"
```

---

### Task 8: CLI Clean Cutover

**Files:**
- Create: `cmd/prohibitorum/app_policy_commands.go`
- Create: `cmd/prohibitorum/app_policy_commands_test.go`
- Modify: `cmd/prohibitorum/main.go`
- Modify: `cmd/prohibitorum/main_test.go`

**Interfaces:**
- Consumes generated app/group/manager queries and Task 2 rule validation.
- Removes root `group` and old `--grant-group`, `--revoke-group`, `--grant-account`, `--revoke-account` flags.
- Produces app-scoped manager, manual decision, rule group, preview, and restriction commands for OIDC/forward-auth/SAML.

- [ ] **Step 1: Write failing command-tree tests**

```go
func TestLegacyGroupCommandRemoved(t *testing.T) {
    root := buildRootForTest()
    if _, _, err := root.Find([]string{"group"}); err == nil { t.Fatal("legacy group command still registered") }
}

func TestOIDCPolicyCommandsRegistered(t *testing.T) {
    root := buildRootForTest()
    for _, path := range [][]string{
        {"oidc-client", "manager", "assign"},
        {"oidc-client", "group", "create-manual"},
        {"oidc-client", "group", "create-rule"},
        {"oidc-client", "decision", "set"},
        {"oidc-client", "access", "set-restricted"},
    } {
        if _, _, err := root.Find(path); err != nil { t.Fatalf("%v: %v", path, err) }
    }
}
```

- [ ] **Step 2: Run CLI tests and verify failure**

```bash
go test ./cmd/prohibitorum -run 'PolicyCommand|LegacyGroup|ManagerCommand' -count=1
```

Expected: FAIL because old commands remain and new tree is absent.

- [ ] **Step 3: Implement shared app command builders**

Move app-policy command construction out of the already-large `main.go`. Use a small adapter per app kind to resolve `--client-id` or `--entity-id`. Required operations:

```text
manager list|assign|remove --username
access set-restricted --restricted=true|false
group list|create-manual|create-rule|update|delete
decision list|set --username --effect=allow|deny|clear
group preview --slug [--limit]
```

`create-rule`/`update` accepts `--rule-file`, reads a bounded file, validates it with `appaccess.ParseAndValidateRule`, and persists canonical JSON. App binding is supplied by the parent app identifier and never accepted as a mutable flag.

- [ ] **Step 4: Remove obsolete command code and registrations**

Delete `addGroupCommands`, `addOIDCClientAccessCommands`, and `addSAMLSPAccessCommands` from `main.go`. Register only the new app-scoped commands. Do not keep aliases.

- [ ] **Step 5: Run the CLI package**

```bash
go test ./cmd/prohibitorum -count=1
go run ./cmd/prohibitorum --help
go run ./cmd/prohibitorum oidc-client group --help
```

Expected: tests PASS; help shows app-scoped groups and no root `group` command.

- [ ] **Step 6: Commit CLI cutover**

```bash
git add cmd/prohibitorum
git commit -m "feat: replace global group cli with app policies"
```

---

### Task 9: Frontend Role, Routes, and Managed-App List

**Files:**
- Create: `dashboard/src/lib/appAccess.ts`
- Create: `dashboard/src/pages/ManagedApplicationsView.vue`
- Create: `dashboard/src/pages/ManagedApplicationsView.test.ts`
- Create: `dashboard/src/pages/ManagedApplicationDetailView.vue`
- Create: `dashboard/src/pages/ManagedApplicationDetailView.test.ts`
- Modify: `dashboard/src/stores/auth.ts`
- Modify: `dashboard/src/router/index.ts`
- Modify: `dashboard/src/router/guard.test.ts`
- Modify: `dashboard/src/components/custom/AppSidebar.vue`
- Modify: `dashboard/src/components/custom/AppSidebar.test.ts`
- Modify: `dashboard/src/pages/admin/AdminAccountDetailView.vue`
- Modify: `dashboard/src/pages/admin/AdminAccountDetailView.test.ts`
- Modify: `dashboard/src/pages/admin/AdminAccountsView.vue`
- Modify: `dashboard/src/pages/admin/AdminInvitationsView.vue`
- Modify: `dashboard/src/pages/admin/AdminInvitationsView.test.ts`
- Modify: `dashboard/src/locales/en.ts`
- Modify: `dashboard/src/locales/zh.ts`

**Interfaces:**
- Consumes delegated list/workspace API from Task 5.
- Produces shared TypeScript types `AppKind`, `ManagedApplication`, `AppGroup`, `Rule`, `Condition`, `ManualDecision`, and `AppAccessWorkspace`.
- Produces `auth.isAppManager` and route meta `requiresAppManager`.

- [ ] **Step 1: Write failing auth/route/sidebar tests**

Add cases: `app_manager` can enter `/manage/applications`; `user` cannot; `admin` can; app manager sees managed navigation but not global admin navigation; admin sees admin navigation. Account and invitation role selectors include all three roles.

```ts
it('allows an application manager into managed routes but not admin routes', async () => {
  get.mockResolvedValue({ id: 1, username: 'm', displayName: 'Manager', role: 'app_manager' })
  const r = makeRouter()
  await r.push('/manage/applications'); await r.isReady()
  expect(r.currentRoute.value.name).toBe('managed-applications')
  await r.push('/admin/accounts')
  expect(r.currentRoute.value.name).toBe('error')
})
```

- [ ] **Step 2: Run focused frontend tests and verify failure**

```bash
cd dashboard && npm test -- src/router/guard.test.ts src/components/custom/AppSidebar.test.ts src/pages/admin/AdminAccountDetailView.test.ts src/pages/admin/AdminInvitationsView.test.ts
```

Expected: FAIL because role and routes are absent.

- [ ] **Step 3: Add shared app-access types and role helpers**

```ts
export type AppKind = 'oidc' | 'forward_auth' | 'saml'
export type GroupKind = 'manual' | 'rule'
export type ManualEffect = 'allow' | 'deny'
export interface ManagedApplication { kind: AppKind; id: string; displayName: string; accessRestricted: boolean }
export interface Condition {
  op?: 'all' | 'any' | 'not'
  children?: Condition[]
  child?: Condition
  fact?: 'connection.provider' | 'connection.protocol' | 'login_method' | 'avatar'
  provider?: string
  protocol?: 'oidc' | 'steam' | 'vrchat'
  method?: 'passkey' | 'password_totp' | 'federation'
  source?: 'any' | 'user_uploaded'
}
```

Add `isAppManager = computed(() => role==='app_manager' || role==='admin')` while retaining strict `isAdmin`.

- [ ] **Step 4: Add managed routes and list/detail shells**

Register `/manage/applications` and `/manage/applications/:kind/:id` with `requiresAppManager`. The list fetches `/managed-applications`; the detail shell fetches the app access workspace and handles generic not-found. Do not render protocol configuration.

- [ ] **Step 5: Update role forms and translations**

Account details and invitations use three role options. Account lists display a distinct app-manager badge. Add matching English/Chinese strings and keep locale parity tests green.

- [ ] **Step 6: Run frontend foundation tests**

```bash
cd dashboard && npm test -- src/router/guard.test.ts src/components/custom/AppSidebar.test.ts src/pages/ManagedApplicationsView.test.ts src/pages/ManagedApplicationDetailView.test.ts src/pages/admin/AdminAccountDetailView.test.ts src/pages/admin/AdminInvitationsView.test.ts src/locales/locales.parity.test.ts
```

Expected: PASS.

- [ ] **Step 7: Commit frontend role and navigation**

```bash
git add dashboard/src
git commit -m "feat: add managed application dashboard routes"
```

---

### Task 10: Rule Builder and Access Workspace UI

**Files:**
- Create: `dashboard/src/components/custom/RuleConditionEditor.vue`
- Create: `dashboard/src/components/custom/RuleConditionEditor.test.ts`
- Create: `dashboard/src/components/custom/AppPolicyWorkspace.vue`
- Create: `dashboard/src/components/custom/AppPolicyWorkspace.test.ts`
- Create: `dashboard/src/components/custom/ManualDecisionEditor.vue`
- Create: `dashboard/src/components/custom/ManualDecisionEditor.test.ts`
- Modify: `dashboard/src/pages/ManagedApplicationDetailView.vue`
- Modify: `dashboard/src/pages/ManagedApplicationDetailView.test.ts`
- Modify: `dashboard/src/locales/en.ts`
- Modify: `dashboard/src/locales/zh.ts`
- Remove: `dashboard/src/components/custom/AppAccessCard.vue`
- Remove: `dashboard/src/components/custom/AppAccessCard.test.ts`

**Interfaces:**
- Consumes types and delegated endpoints from Tasks 5 and 9.
- Produces `AppPolicyWorkspace` reusable by manager and admin app pages.
- Emits no app configuration mutations; all writes are policy-only.

- [ ] **Step 1: Invoke the frontend design skill before editing UI**

Read `skill://impeccable` and apply its accessibility, responsive-layout, interaction, error-state, and visual-hierarchy checklist to this task. Keep the repository's existing component language rather than creating a second design system.

- [ ] **Step 2: Write failing rule-editor behavior tests**

Cover adding/removing/nesting all/any/not, leaf selection, provider selection, invalid empty combinators, depth/node controls, keyboard labels, and emitted immutable rule values:

```ts
it('builds nested all/any conditions without mutating props', async () => {
  const initial: Condition = { op: 'all', children: [{ fact: 'login_method', method: 'passkey' }] }
  const w = mountEditor(initial)
  await w.get('[data-test="add-any-root"]').trigger('click')
  expect(w.emitted('update:modelValue')).toBeTruthy()
  expect(initial).toEqual({ op: 'all', children: [{ fact: 'login_method', method: 'passkey' }] })
})
```

- [ ] **Step 3: Write failing manual/workspace tests**

Cover create-only-one manual group, allow/deny/clear transitions, neutral display, deny/allow precedence copy, rule CRUD, preview pagination, safe explanation rendering, open/restricted hint, and empty-policy restriction confirmation.

- [ ] **Step 4: Run component tests and verify failure**

```bash
cd dashboard && npm test -- src/components/custom/RuleConditionEditor.test.ts src/components/custom/ManualDecisionEditor.test.ts src/components/custom/AppPolicyWorkspace.test.ts
```

Expected: FAIL because components do not exist.

- [ ] **Step 5: Implement the recursive rule editor**

Use existing `Select`, `Button`, `FormSection`, `ErrorPanel`, and focus-visible conventions. Bound UI additions to depth 8/nodes 64/children 32 before submitting. Provider choices come from the workspace's safe provider descriptors. Every recursive row has an accessible label and remove action; `not` exposes exactly one child.

- [ ] **Step 6: Implement manual decisions**

Render separate Allow and Deny tabs/lists plus account search. One account has at most one decision; changing effect is one upsert, clear is one delete. Disabled accounts are absent from search. Explain neutral as “No manual decision; calculated groups decide.”

- [ ] **Step 7: Implement the policy workspace**

The workspace owns API loading/mutations and renders:

```vue
<AppPolicyWorkspace
  :kind="app.kind"
  :app-id="app.id"
  :display-name="app.displayName"
  mode="manager"
/>
```

Cards: restriction state, optional manual group, rule groups, calculated preview, explanation. Manual allow copy states that matching rule groups still appear in claims; manual deny copy states that no token/assertion is issued. Require `ConfirmDialog` before enabling restriction with empty policy.

- [ ] **Step 8: Run workspace tests and typecheck**

```bash
cd dashboard && npm test -- src/components/custom/RuleConditionEditor.test.ts src/components/custom/ManualDecisionEditor.test.ts src/components/custom/AppPolicyWorkspace.test.ts src/pages/ManagedApplicationDetailView.test.ts
cd dashboard && npm run build
```

Expected: PASS.

- [ ] **Step 9: Commit the access workspace**

```bash
git add dashboard/src
git commit -m "feat: add app-bound access policy workspace"
```

---

### Task 11: Admin App Integration and Global Group UI Removal

**Files:**
- Create: `dashboard/src/components/custom/AppManagerCard.vue`
- Create: `dashboard/src/components/custom/AppManagerCard.test.ts`
- Modify: `dashboard/src/pages/admin/AdminOidcClientDetailView.vue`
- Modify: `dashboard/src/pages/admin/AdminOidcClientDetailView.test.ts`
- Modify: `dashboard/src/pages/admin/AdminSamlProviderDetailView.vue`
- Modify: `dashboard/src/pages/admin/AdminSamlProviderDetailView.test.ts`
- Modify: `dashboard/src/pages/admin/AdminForwardAuthAppDetailView.vue`
- Create: `dashboard/src/pages/admin/AdminForwardAuthAppDetailView.test.ts`
- Modify: `dashboard/src/pages/admin/AdminAccountDetailView.vue`
- Modify: `dashboard/src/pages/admin/AdminAccountDetailView.test.ts`
- Modify: `dashboard/src/components/custom/AppSidebar.vue`
- Modify: `dashboard/src/components/custom/AppSidebar.test.ts`
- Modify: `dashboard/src/router/index.ts`
- Remove: `dashboard/src/pages/admin/AdminGroupsView.vue`
- Remove: `dashboard/src/pages/admin/AdminGroupsView.test.ts`
- Remove: `dashboard/src/pages/admin/AdminGroupDetailView.vue`
- Remove: `dashboard/src/pages/admin/AdminGroupDetailView.test.ts`
- Modify: `dashboard/src/locales/en.ts`
- Modify: `dashboard/src/locales/zh.ts`

**Interfaces:**
- Consumes `AppPolicyWorkspace` in `mode="admin"` and admin manager APIs.
- Removes standalone groups routes/navigation and account-global membership editor.

- [ ] **Step 1: Write failing manager-card and admin integration tests**

Cover list, sudo assignment/removal, target picker restricted to active `app_manager` accounts, one-time sudo retry, and card presence on all three app types. Assert delegated manager controls remain absent from config cards.

- [ ] **Step 2: Run focused admin page tests and verify failure**

```bash
cd dashboard && npm test -- src/components/custom/AppManagerCard.test.ts src/pages/admin/AdminOidcClientDetailView.test.ts src/pages/admin/AdminSamlProviderDetailView.test.ts src/pages/admin/AdminForwardAuthAppDetailView.test.ts
```

Expected: FAIL because the card and embedded workspace are absent.

- [ ] **Step 3: Implement manager assignment card**

`AppManagerCard` receives app kind/id, loads the matching admin endpoint, searches accounts filtered to `role=app_manager`, and wraps assign/remove with `withSudo`. Show disabled status but do not allow assigning a disabled target.

- [ ] **Step 4: Embed manager card and policy workspace in admin app pages**

Replace `AppAccessCard` with `AppPolicyWorkspace mode="admin"`; add `AppManagerCard`. Preserve existing protocol configuration, secret, disable/delete, and metadata cards unchanged.

- [ ] **Step 5: Remove global group UI and account membership remnants**

Delete routes, sidebar link, pages/tests, locale strings used only by shared groups, and account-detail global membership card/API calls. Do not leave redirects from `/admin/groups`.

- [ ] **Step 6: Run affected frontend tests**

```bash
cd dashboard && npm test -- src/components/custom/AppManagerCard.test.ts src/components/custom/AppSidebar.test.ts src/pages/admin/AdminOidcClientDetailView.test.ts src/pages/admin/AdminSamlProviderDetailView.test.ts src/pages/admin/AdminForwardAuthAppDetailView.test.ts src/pages/admin/AdminAccountDetailView.test.ts src/locales/locales.parity.test.ts
```

Expected: PASS.

- [ ] **Step 7: Commit admin integration and removal**

```bash
git add dashboard/src
git commit -m "feat: manage app policies from application pages"
```

---

### Task 12: End-to-End Smoke, Documentation, Bundle, and Final Gate

**Files:**
- Modify: `cmd/smoke/main.go`
- Modify: `ARCHITECTURE.md`
- Modify: `api.md`
- Modify: `STATUS.md`
- Modify: `PRODUCT.md`
- Modify: `README.md` if it contains global-group or old CLI instructions
- Modify generated bundle: `pkg/webui/dist/**`

**Interfaces:**
- Exercises the complete user-visible contract from role assignment through protocol enforcement and claim projection.
- Removes every remaining documented reference to global/shared groups and direct account grants.

- [ ] **Step 1: Extend the smoke harness with a failing end-to-end scenario**

Add a scenario that:

```text
1. Creates manager and member accounts with distinct facts.
2. Promotes manager to app_manager and assigns exactly one OIDC app.
3. Proves manager cannot read/mutate a second app or protocol config.
4. Creates one manual group and two rule groups.
5. Verifies manual allow, manual deny, neutral true, and neutral false.
6. Verifies two matching exposed rule slugs plus manual slug in OIDC claims.
7. Changes a live fact and verifies the next authorize/forward-auth/SAML decision changes.
8. Proves manager assignment alone does not grant app usage.
9. Removes eligibility before refresh and verifies invalid_grant plus family revocation.
```

Use public/admin/delegated APIs rather than direct SQL after initial bootstrap.

- [ ] **Step 2: Run smoke and verify the new scenario fails before final wiring fixes**

```bash
mise run ci:smoke
```

Expected: FAIL at the first uncovered end-to-end contract; fix production code, not the assertion.

- [ ] **Step 3: Update architecture and API documentation**

Document `app_manager`, assignments, app-bound manual/rule groups, live fact semantics, precedence, app-aware claims, delegated routes, CLI commands, audit events, and destructive migration. Remove old `/groups`, shared-grant, account-group, and grant/revoke tables/routes from all docs.

- [ ] **Step 4: Search for obsolete concepts and remove every call site**

Use repository search and require no production matches for obsolete symbols:

```text
AddGroupMember
RemoveGroupMember
ListExposedGroupSlugsByAccount
IsAccountAuthorizedForOIDCClient
IsAccountAuthorizedForSAMLSP
oidc_client_access
saml_sp_access
group_member
/admin/groups
```

Migration history/specs may retain historical text; production Go, current SQL queries, current docs, dashboard source, and CLI must not.

- [ ] **Step 5: Run focused Go and frontend suites**

```bash
go test ./pkg/appaccess ./pkg/authn ./pkg/server ./pkg/protocol/oidc ./pkg/protocol/saml ./cmd/prohibitorum ./db/migrations -count=1
cd dashboard && npm test
cd dashboard && npm run build
```

Expected: PASS.

- [ ] **Step 6: Rebuild the embedded dashboard bundle**

```bash
mise run prod:build
```

Expected: PASS and `pkg/webui/dist` updated to match dashboard source.

- [ ] **Step 7: Run project-wide verification**

```bash
mise run ci
mise run ci:smoke
```

Expected: both PASS with no stale bundle drift.

- [ ] **Step 8: Browser smoke the delegated UI**

Start the dev server with `mise run dev:server`, then use the browser to exercise manager navigation, assigned/unassigned app behavior, manual allow/deny/clear, nested rule editing, calculated preview, explanation, responsive layout, keyboard navigation, focus states, validation errors, empty-policy confirmation, and admin manager assignment. Capture no secrets in screenshots/logs.

- [ ] **Step 9: Commit smoke, docs, and bundle**

```bash
git add cmd/smoke ARCHITECTURE.md api.md STATUS.md PRODUCT.md README.md pkg/webui/dist
git commit -m "docs: complete delegated app access cutover"
```

- [ ] **Step 10: Request final code review**

Invoke `superpowers:requesting-code-review`, review the full implementation against `docs/superpowers/specs/2026-07-25-app-manager-rule-groups-design.md`, fix every correctness/security issue, rerun the affected focused command, then rerun `mise run ci` and `mise run ci:smoke` before declaring completion.
