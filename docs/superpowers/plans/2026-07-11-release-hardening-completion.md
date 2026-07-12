# Release Hardening Completion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the remaining release-hardening work with atomic rotating refresh-token families, universal admin cursor pagination, per-IdP private-network policy, and code-driven persistent localized errors with request-ID diagnostics.

**Architecture:** Establish shared error/correlation and cursor foundations first, then migrate backend callsites, database queries, and dashboard consumers through clean-cutover contracts. Outbound policy becomes per-IdP and denylist-based; refresh tokens move to a single CAS-updated, hashed/encrypted family record and intentionally invalidate the legacy format.

**Tech Stack:** Go 1.26.5, chi/huma, PostgreSQL/sqlc/goose, Redis and in-memory KV CAS, Vue 3/TypeScript/Vitest, Pinia, vue-i18n, Tailwind CSS.

**Design:** `docs/superpowers/specs/2026-07-11-release-hardening-completion-design.md`

---

### Task 1: Public error registry and request correlation

**Goal:** Give every application request a server-generated request ID and establish the only permitted typed public-error construction path.

**Files:**
- Create: `pkg/weberr/registry.go`
- Create: `pkg/weberr/registry_test.go`
- Create: `pkg/weberr/requestid.go`
- Create: `pkg/weberr/requestid_test.go`
- Modify: `pkg/weberr/weberr.go`
- Modify: `pkg/server/server.go`
- Modify: `pkg/server/operations.go`
- Modify: `pkg/authn/errors.go`
- Test: `pkg/server/handle_auth_error_test.go`

**Acceptance Criteria:**
- [ ] Every HTTP response carries a cryptographically random server `X-Request-ID`; inbound values never become the server ID.
- [ ] Application errors serialize as `{code, details, requestId}` without a localized message.
- [ ] Registry initialization rejects duplicate codes and undeclared public detail fields.
- [ ] Unexpected failures use an operation-specific registered internal code rather than arbitrary raw prose.

**Verify:** `mise exec -- go test ./pkg/weberr ./pkg/server ./pkg/authn -run 'RequestID|PublicError|Registry|WriteAuthErr' -count=1` → PASS

**Steps:**

- [ ] **Step 1: Write failing request-ID and registry tests.**

```go
func TestRequestIDMiddlewareReplacesInboundValue(t *testing.T) {
    req := httptest.NewRequest(http.MethodGet, "/", nil)
    req.Header.Set("X-Request-ID", "attacker-controlled")
    rec := httptest.NewRecorder()
    RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if got := RequestIDFromContext(r.Context()); got == "" || got == "attacker-controlled" {
            t.Fatalf("server request id = %q", got)
        }
        w.WriteHeader(http.StatusNoContent)
    })).ServeHTTP(rec, req)
    if got := rec.Header().Get("X-Request-ID"); got == "" || got == "attacker-controlled" {
        t.Fatalf("response request id = %q", got)
    }
}

func TestRegistryRejectsDuplicateCode(t *testing.T) {
    err := ValidateDefinitions([]Definition{{Code: "account_disabled"}, {Code: "account_disabled"}})
    if err == nil { t.Fatal("duplicate code accepted") }
}
```

- [ ] **Step 2: Run the focused tests and confirm RED because request middleware and definitions do not exist.**

- [ ] **Step 3: Implement the typed registry and correlation API.**

```go
type Definition struct {
    Code          string
    Status        int
    LocaleKey     string
    DetailKeys    map[string]struct{}
    Retryable     bool
    Recovery      string
    DiagnosticKind string
}

type PublicError struct {
    Code      string         `json:"code"`
    Details   map[string]any `json:"details,omitempty"`
    RequestID string         `json:"requestId"`
}

func New(code string, details map[string]any) error
func DefinitionFor(code string) (Definition, bool)
func RequestID(next http.Handler) http.Handler
func RequestIDFromContext(ctx context.Context) string
```

Use 16 bytes from `crypto/rand`, base64url without padding. `New` validates detail keys against the registered definition. Wrapped causes may remain in-process for classification, but public responses, diagnostic records, and operator logs contain only the registered code, diagnostic category, curated fields, and request ID—not unchecked `err.Error()` text.

- [ ] **Step 4: Install middleware before session/auth routing and adapt `writeAuthErr`/huma conversion to emit the new envelope.**

- [ ] **Step 5: Run focused tests to GREEN, then run `mise exec -- go test ./pkg/weberr ./pkg/authn ./pkg/server -count=1`.**

- [ ] **Step 6: Commit.**

```bash
git add pkg/weberr pkg/authn/errors.go pkg/server/server.go pkg/server/operations.go pkg/server/handle_auth_error_test.go
git commit -m "feat: unify public error contracts"
```

---

### Task 2: Safe request-ID diagnostic store and admin lookup

**Goal:** Persist bounded, curated diagnostic records and provide exact-ID fresh-sudo admin lookup.

**Files:**
- Create: `db/migrations/029_diagnostic_event.sql`
- Create: `db/queries/diagnostic_event.sql`
- Generate: `pkg/db/diagnostic_event.sql.go`, `pkg/db/models.go`, `pkg/db/querier.go`
- Create: `pkg/diagnostic/store.go`
- Create: `pkg/diagnostic/store_test.go`
- Create: `pkg/server/handle_admin_diagnostics.go`
- Create: `pkg/server/handle_admin_diagnostics_test.go`
- Modify: `pkg/server/server.go`
- Modify: `pkg/server/operations.go`
- Modify: `pkg/audit/event.go`

**Acceptance Criteria:**
- [ ] Diagnostic records contain only whitelisted structured fields and expire after seven days.
- [ ] `GET /api/prohibitorum/diagnostics/{requestId}` requires admin plus fresh sudo, exact ID, and rate limiting.
- [ ] Lookup emits an audit event and cannot enumerate records.
- [ ] Tests prove secrets in raw causes, bodies, headers, and tokens are absent from stored and returned data.

**Verify:** `mise exec -- go test ./pkg/diagnostic ./pkg/server -run 'Diagnostic|RequestID' -count=1` → PASS

**Steps:**

- [ ] **Step 1: Add failing store and route tests.**

```go
func TestStoreRejectsUndeclaredOrSecretBearingFields(t *testing.T) {
    rec := Record{RequestID: "rid", Code: "oidc_exchange_failed", Operation: "oidc.exchange",
        Fields: map[string]any{"provider": "corp", "rawCause": "postgres://user:secret@db/private"}}
    if err := store.Record(ctx, rec); err == nil {
        t.Fatal("diagnostic store accepted undeclared rawCause field")
    }
}
```

The route test must exercise no session → 401, admin without sudo → 401, exact lookup → 200, unknown ID → 404, and second lookup audit emission.

- [ ] **Step 2: Run tests to RED.**

- [ ] **Step 3: Add the migration and sqlc queries.**

```sql
CREATE TABLE diagnostic_event (
  request_id text PRIMARY KEY,
  occurred_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  account_id integer REFERENCES account(id) ON DELETE SET NULL,
  method text NOT NULL,
  route text NOT NULL,
  operation text NOT NULL,
  code text NOT NULL,
  retryable boolean NOT NULL DEFAULT false,
  fields jsonb NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX diagnostic_event_expiry_idx ON diagnostic_event (expires_at);
```

Queries: insert, exact non-expired lookup, and bounded expiry deletion. Run `mise exec -- sqlc generate`.

- [ ] **Step 4: Implement `diagnostic.Store.Record(ctx, rec)` so it accepts only registry-approved fields. Callers retain raw causes only for in-process classification; structured logs and stored records receive the safe code, category, curated fields, and request ID, never unchecked `err.Error()` text.**

- [ ] **Step 5: Register the exact-ID route through `registerSudoOpHTTP`, add lookup rate limit, and audit `diagnostic_lookup`.**

- [ ] **Step 6: Run focused tests to GREEN and migration/sqlc consistency checks through `mise exec -- go test ./pkg/diagnostic ./pkg/server ./pkg/db -count=1`.**

- [ ] **Step 7: Commit.**

```bash
git add db/migrations/029_diagnostic_event.sql db/queries/diagnostic_event.sql pkg/db pkg/diagnostic pkg/server/handle_admin_diagnostics.go pkg/server/handle_admin_diagnostics_test.go pkg/server/server.go pkg/server/operations.go pkg/audit/event.go
git commit -m "feat: add request diagnostics lookup"
```

---

### Task 3: Migrate backend failures to dedicated codes

**Goal:** Replace Chinese and opaque shared API messages with unique registered causes and curated detail schemas across application and protocol adapters.

**Files:**
- Modify: `pkg/authn/errors.go`
- Modify: `pkg/account/account.go`
- Modify: `pkg/credential/**`
- Modify: `pkg/federation/**`
- Modify: `pkg/protocol/oidc/**`
- Modify: `pkg/protocol/saml/**`
- Modify: `pkg/server/**`
- Modify: `pkg/weberr/registry.go`
- Create: `pkg/weberr/coverage_test.go`
- Test: affected package tests

**Acceptance Criteria:**
- [ ] Every known user-actionable branch has a dedicated stable registry code and safe details where useful.
- [ ] No application JSON response contains a `message` field or Chinese server prose.
- [ ] Raw internal errors never enter public details or diagnostic records.
- [ ] OAuth/OIDC/SAML responses retain standard wire error values while mapping to internal unique codes and request IDs.

**Verify:** `mise exec -- go test ./pkg/account ./pkg/authn ./pkg/credential/... ./pkg/federation/... ./pkg/protocol/... ./pkg/server ./pkg/weberr -count=1` → PASS

**Steps:**

- [ ] **Step 1: Add a failing coverage test that walks all registry definitions and asserts uniqueness, valid status, localization key, declared details, and protocol mapping. Add representative response tests for formerly shared `bad_request` cases.**

```go
func TestKnownValidationFailuresHaveDistinctCodes(t *testing.T) {
    cases := []error{
        authn.ErrInvalidUsername(), authn.ErrInvalidDisplayName(), authn.ErrInvalidRole(),
    }
    seen := map[string]bool{}
    for _, err := range cases {
        pe := weberr.AsPublic(err)
        if seen[pe.Code] { t.Fatalf("duplicate public code %q", pe.Code) }
        seen[pe.Code] = true
    }
}
```

- [ ] **Step 2: Run representative tests to RED against the current message-bearing/opaque behavior.**

- [ ] **Step 3: Catalogue every constructor and direct writer, then replace branches with registered codes and explicit detail values such as `field`, `reason`, `limit`, `allowed`, and `retryAfterSeconds`. Do not pass `err.Error()` as details.**

- [ ] **Step 4: Add operation-specific internal codes for DB/KV/crypto/outbound failures and call the diagnostic store with whitelisted context plus the request ID.**

- [ ] **Step 5: Adapt OAuth/OIDC/SAML writers: retain standard `error`/status values, omit unsafe descriptions, and log the unique registry code with request ID.**

- [ ] **Step 6: Run the focused package set to GREEN. Search `pkg` for Chinese literals in public error constructors and for `details.*err.Error()`; only operator logs/tests may remain.**

- [ ] **Step 7: Commit.**

```bash
git add pkg/account pkg/authn pkg/credential pkg/federation pkg/protocol pkg/server pkg/weberr
git commit -m "refactor: classify public API failures"
```

---

### Task 4: Persistent localized frontend error panel

**Goal:** Render every API failure persistently from its code, expose curated details in an accessible disclosure, and link admins to exact request diagnostics.

**Files:**
- Create: `dashboard/src/components/custom/ErrorPanel.vue`
- Create: `dashboard/src/components/custom/ErrorPanel.test.ts`
- Create: `dashboard/src/lib/errors.ts`
- Create: `dashboard/src/lib/errors.test.ts`
- Modify: `dashboard/src/lib/api.ts`
- Modify: `dashboard/src/lib/api.test.ts`
- Modify: `dashboard/src/composables/useApi.ts`
- Modify: `dashboard/src/composables/useApi.test.ts`
- Modify: `dashboard/src/components/custom/Toaster.vue`
- Modify: `dashboard/src/lib/toast.ts`
- Modify: `dashboard/src/App.vue`
- Modify: `dashboard/src/locales/en.ts`
- Modify: `dashboard/src/locales/zh.ts`
- Modify: components/pages currently rendering `errorText` or global error toasts

**Acceptance Criteria:**
- [ ] `ApiError` is `{code, details?, requestId?}` and never trusts server prose for display.
- [ ] Errors persist until explicit dismissal or successful retry.
- [ ] The details disclosure is keyboard accessible and shows localized curated fields plus copyable request ID.
- [ ] Admins can open the exact diagnostic lookup; non-admins do not see that action.
- [ ] Locale parity tests cover every backend registry code and detail reason.

**Verify:** `cd dashboard && npm test -- src/components/custom/ErrorPanel.test.ts src/lib/errors.test.ts src/lib/api.test.ts src/composables/useApi.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Write failing component and mapping tests.**

```ts
it('persists and reveals localized curated details', async () => {
  const wrapper = mount(ErrorPanel, { props: { error: {
    code: 'oidc_redirect_uri_mismatch',
    details: { field: 'redirectUri', reason: 'not_registered' },
    requestId: 'rid-123',
  } } })
  expect(wrapper.text()).toContain('redirect URI')
  expect(wrapper.text()).not.toContain('rid-123')
  await wrapper.get('[data-test="error-details-trigger"]').trigger('click')
  expect(wrapper.text()).toContain('rid-123')
  vi.advanceTimersByTime(60_000)
  expect(wrapper.find('[role="alert"]').exists()).toBe(true)
})
```

- [ ] **Step 2: Run tests to RED.**

- [ ] **Step 3: Replace the `ApiError.message` contract and centralize localization.**

```ts
export interface ApiError {
  code: string
  details?: Record<string, string | number | boolean | string[]>
  requestId?: string
}

export function errorTranslationKey(code: string): string {
  return `errors.codes.${code}`
}
```

- [ ] **Step 4: Implement `ErrorPanel` with `role="alert"`, an explicit close button, a native/reka disclosure, copy action, recovery slot, and admin diagnostic route action. Never auto-dismiss.**

- [ ] **Step 5: Replace inline raw Alert/errorText blocks with `ErrorPanel :error="error"`; retain field placement. Stop emitting API 5xx/network errors as timed toasts. Success/info toasts may remain.**

- [ ] **Step 6: Add English and Chinese translations for every code, reason, detail label, recovery action, disclosure label, copy state, and unknown fallback. Add a parity test that imports both locale objects and the generated/shared code list.**

- [ ] **Step 7: Run focused tests to GREEN, then `cd dashboard && npm test`.**

- [ ] **Step 8: Commit.**

```bash
git add dashboard/src
git commit -m "feat: add persistent localized errors"
```

---

### Task 5: Authenticated cursor and page foundations

**Goal:** Define the single admin page envelope and tamper-resistant stateless cursor codec.

**Files:**
- Create: `pkg/pagination/cursor.go`
- Create: `pkg/pagination/cursor_test.go`
- Create: `pkg/pagination/page.go`
- Modify: `pkg/contract/admin.go`
- Modify: `pkg/server/operations.go`

**Acceptance Criteria:**
- [ ] Cursors are authenticated/encrypted with DEK version, collection, filters, sort, keyset, issue time, and 24-hour expiry.
- [ ] Wrong endpoint/filter/order, malformed, modified, or expired cursors return `pagination_cursor_invalid`.
- [ ] Limits default to 50 and clamp to 1–100.
- [ ] Empty/final pages serialize exactly as `{items:[],nextCursor:""}`.

**Verify:** `mise exec -- go test ./pkg/pagination ./pkg/contract ./pkg/server -run 'Cursor|Page|ClampLimit' -count=1` → PASS

**Steps:**

- [ ] **Step 1: Write failing cursor round-trip, tamper, binding, expiry, rotation-key, and page JSON tests.**

```go
type CursorPayload struct {
    Version    int               `json:"v"`
    Collection string            `json:"collection"`
    Filters    map[string]string `json:"filters"`
    Sort       string            `json:"sort"`
    Keys       []string          `json:"keys"`
    IssuedAt   time.Time         `json:"iat"`
    ExpiresAt  time.Time         `json:"exp"`
}
```

- [ ] **Step 2: Run tests to RED.**

- [ ] **Step 3: Implement AES-GCM cursor encoding using the active DEK and version prefix; decode with the addressed retained key. Validate all bindings before exposing keys.**

- [ ] **Step 4: Add generic wire type and limit helper.**

```go
type Page[T any] struct {
    Items      []T   `json:"items"`
    NextCursor string `json:"nextCursor"`
}

func Limit(v int) int { if v <= 0 { return 50 }; if v > 100 { return 100 }; return v }
```

- [ ] **Step 5: Run focused tests to GREEN and commit.**

```bash
git add pkg/pagination pkg/contract/admin.go pkg/server/operations.go
git commit -m "feat: add authenticated cursor pages"
```

---

### Task 6: Paginate top-level admin collections

**Goal:** Convert every top-level admin index to stable keyset SQL and the uniform page envelope.

**Files:**
- Modify: `db/queries/account.sql`
- Modify: `db/queries/enrollment.sql`
- Modify: `db/queries/rbac.sql`
- Modify: `db/queries/oidc.sql`
- Modify: `db/queries/saml_sp.sql`
- Modify: `db/queries/upstream_idp.sql`
- Modify: `db/queries/credential_event.sql`
- Generate: corresponding `pkg/db/*.sql.go`, `pkg/db/querier.go`
- Modify: `pkg/server/handle_account.go`
- Modify: `pkg/server/handle_admin_groups.go`
- Modify: `pkg/server/handle_admin_oidc_clients.go`
- Modify: `pkg/server/handle_admin_saml_sps.go`
- Modify: `pkg/server/handle_admin_upstream_idps.go`
- Modify: `pkg/server/handle_admin_signing_keys.go`
- Modify: `pkg/server/handle_admin_forward_auth_apps.go`
- Modify: `pkg/server/handle_admin_audit.go`
- Test: corresponding server tests

**Acceptance Criteria:**
- [ ] Accounts, invitations, groups, OIDC/SAML apps, IdPs, signing keys, forward-auth apps, and audit events are keyset-paginated.
- [ ] Every query uses deterministic unique ordering and `limit + 1`.
- [ ] Filter changes invalidate prior cursors.
- [ ] No top-level admin collection returns a bare array.

**Verify:** `mise exec -- go test ./pkg/server ./pkg/db -run 'List.*Page|Cursor|Admin.*List' -count=1` → PASS

**Steps:**

- [ ] **Step 1: Add failing handler tests for first/middle/final pages, duplicate timestamps/names, limit clamp, tampered cursor, and filter mismatch.**

- [ ] **Step 2: Run tests to RED against bare-array handlers.**

- [ ] **Step 3: Replace list SQL with keyset parameters. Example for accounts:**

```sql
WHERE ($1::timestamptz IS NULL OR (a.created_at, a.id) < ($1, $2))
ORDER BY a.created_at DESC, a.id DESC
LIMIT $3;
```

Use endpoint-appropriate immutable tuples and pass `limit + 1`. Run `mise exec -- sqlc generate`.

- [ ] **Step 4: Add shared request input fields `Limit int query:"limit"` and `Cursor string query:"cursor"`; decode bound cursors, project at most `limit`, and encode the next tuple only when the extra row exists.**

- [ ] **Step 5: Run focused tests to GREEN, then `mise exec -- go test ./pkg/server ./pkg/db -count=1`.**

- [ ] **Step 6: Commit.**

```bash
git add db/queries pkg/db pkg/server
git commit -m "feat: paginate admin indexes"
```

---

### Task 7: Paginate nested administrative collections

**Goal:** Apply the same page contract to every nested account, group, and access-assignment collection.

**Files:**
- Modify: `db/queries/account.sql`
- Modify: `db/queries/account_identity.sql`
- Modify: `db/queries/personal_access_token.sql`
- Modify: `db/queries/rbac.sql`
- Modify: `db/queries/webauthn_credential.sql`
- Generate: corresponding `pkg/db/*.sql.go`, `pkg/db/querier.go`
- Modify: `pkg/session/session.go`
- Modify: `pkg/server/handle_account.go`
- Modify: `pkg/server/handle_admin_account_tokens.go`
- Modify: `pkg/server/handle_admin_app_access.go`
- Modify: `pkg/server/handle_admin_groups.go`
- Modify: `pkg/server/handle_me_identities.go` only where the admin account endpoint shares projection logic
- Test: corresponding server/session tests

**Acceptance Criteria:**
- [ ] Account credentials, sessions, PATs, identities, groups; group members; and OIDC/SAML access groups/accounts return cursor pages.
- [ ] KV-backed session pagination uses stable `(issuedAt, sessionId)` keysets and bounded scans without loading an unbounded account set.
- [ ] Existence checks still return 404 instead of an empty page for unknown parents.
- [ ] No nested admin collection returns a bare array.

**Verify:** `mise exec -- go test ./pkg/session ./pkg/server -run 'Account.*Page|Group.*Page|Access.*Page|Session.*Page' -count=1` → PASS

**Steps:**

- [ ] **Step 1: Add failing nested page tests, including parent-not-found, exact page boundary, concurrent insertion ordering, and session scan bounds.**

- [ ] **Step 2: Run tests to RED.**

- [ ] **Step 3: Add SQL keyset queries scoped by parent ID and regenerate sqlc. For session KV, expose `ListPageByAccount(ctx, accountID, after, limit)` that uses bounded scan iteration and returns a stable sorted page.**

- [ ] **Step 4: Convert handlers to `pagination.Page[T]`, binding cursors to both collection and parent identifier.**

- [ ] **Step 5: Run focused tests to GREEN and commit.**

```bash
git add db/queries pkg/db pkg/session pkg/server
git commit -m "feat: paginate nested admin data"
```

---

### Task 8: Dashboard pagination integration

**Goal:** Consume the uniform page envelope in every admin index and nested collection without truncation.

**Files:**
- Create: `dashboard/src/lib/pagination.ts`
- Create: `dashboard/src/lib/pagination.test.ts`
- Create: `dashboard/src/composables/useCursorPage.ts`
- Create: `dashboard/src/composables/useCursorPage.test.ts`
- Create: `dashboard/src/components/custom/PaginationControls.vue`
- Create: `dashboard/src/components/custom/PaginationControls.test.ts`
- Modify: `dashboard/src/pages/admin/*.vue`
- Modify: `dashboard/src/components/custom/AppAccessCard.vue`
- Modify: affected frontend tests and locales

**Acceptance Criteria:**
- [ ] Every admin collection consumes `Page<T>` and provides accessible next/previous navigation.
- [ ] Cursor history supports previous pages without fabricating reverse cursors.
- [ ] Filter/sort changes clear history and reload page one.
- [ ] Mutations reload the current valid page or step back when the page becomes empty.

**Verify:** `cd dashboard && npm test -- src/lib/pagination.test.ts src/composables/useCursorPage.test.ts src/components/custom/PaginationControls.test.ts src/pages/admin` → PASS

**Steps:**

- [ ] **Step 1: Write failing composable/control tests for next, previous, filter reset, mutation reload, final page, and empty page.**

```ts
export interface Page<T> { items: T[]; nextCursor: string }

export interface CursorPageState {
  cursor: string
  history: string[]
  nextCursor: string
}
```

- [ ] **Step 2: Run tests to RED.**

- [ ] **Step 3: Implement `useCursorPage<T>(fetcher)` with page index, cursor history, stale-request suppression, filter reset, and current-page reload.**

- [ ] **Step 4: Implement accessible controls with localized labels, disabled busy states, `aria-current` status, and focus retention.**

- [ ] **Step 5: Migrate every admin array fetch to `Page<T>`. Picker components must explicitly page/load all options rather than reading only page one.**

- [ ] **Step 6: Run focused tests and full `npm test` to GREEN; commit.**

```bash
git add dashboard/src
git commit -m "feat: paginate admin dashboard data"
```

---

### Task 9: Per-IdP private-network configuration

**Goal:** Replace the global private-network bypass with a default-deny, audited per-identity-provider setting.

**Files:**
- Create: `db/migrations/030_upstream_idp_private_network.sql`
- Modify: `db/queries/upstream_idp.sql`
- Generate: `pkg/db/upstream_idp.sql.go`, `pkg/db/models.go`, `pkg/db/querier.go`
- Modify: `pkg/configx/configx.go`
- Modify: `pkg/federation/oidc/federation.go`
- Modify: `pkg/server/handle_admin_upstream_idps.go`
- Modify: `pkg/server/operations.go`
- Modify: `dashboard/src/pages/admin/AdminUpstreamIdpDetailView.vue`
- Modify: `dashboard/src/pages/admin/AdminUpstreamIdpsView.vue`
- Modify: corresponding tests/locales

**Acceptance Criteria:**
- [ ] Every IdP stores `allow_private_network`, default false.
- [ ] Create/update requires the high-impact fresh-sudo tier and emits an audit event when the setting changes.
- [ ] Global `federation.allow_private_network` configuration is removed.
- [ ] Cached clients are invalidated/rebuilt when the per-IdP value changes.
- [ ] The dashboard explains the risk and requires an explicit choice.

**Verify:** `mise exec -- go test ./pkg/configx ./pkg/federation/oidc ./pkg/server -run 'PrivateNetwork|UpstreamIDP' -count=1 && cd dashboard && npm test -- src/pages/admin/AdminUpstreamIdpDetailView.test.ts src/pages/admin/AdminUpstreamIdpsView.test.ts` → PASS

**Steps:**

- [ ] **Step 1: Write failing migration/model, handler policy, audit, cache invalidation, and frontend toggle tests.**

- [ ] **Step 2: Run tests to RED.**

- [ ] **Step 3: Add `allow_private_network boolean NOT NULL DEFAULT false`, update sqlc queries, and regenerate. Remove the global config field/default/documented environment path from runtime code.**

- [ ] **Step 4: Thread `idp.AllowPrivateNetwork` into client/avatar construction and cache keys. Register setting mutations through fresh-sudo routing and audit old/new values.**

- [ ] **Step 5: Add the explicit admin control and warning copy in both locales.**

- [ ] **Step 6: Run focused tests to GREEN and commit.**

```bash
git add db/migrations/030_upstream_idp_private_network.sql db/queries/upstream_idp.sql pkg/db pkg/configx pkg/federation/oidc pkg/server dashboard/src/pages/admin dashboard/src/locales
git commit -m "feat: scope private fetching per idp"
```

---

### Task 10: Exhaustive outbound unsafe-destination enforcement

**Goal:** Make the denylist comprehensive for every initial and redirected outbound connection while permitting only explicitly enabled RFC1918/ULA IdP access.

**Files:**
- Modify: `pkg/federation/oidc/httpclient.go`
- Modify: `pkg/federation/oidc/httpclient_test.go`
- Modify: `pkg/federation/oidc/redirect_test.go`
- Modify: `pkg/federation/oidc/avatar_fetch.go`
- Modify: `cmd/prohibitorum/main.go`
- Modify: `cmd/prohibitorum/metadata_fetch_test.go`

**Acceptance Criteria:**
- [ ] All IANA special-purpose, loopback, link-local/metadata, multicast, unspecified, documentation, benchmark, reserved, CGNAT, RFC1918, ULA, and IPv4-mapped forms are correctly classified.
- [ ] Private mode permits only RFC1918 and ULA; it never permits loopback, link-local, metadata, or other special-use destinations.
- [ ] Every DNS answer and actual dial address is checked on every hop.
- [ ] Redirect/TLS/hop/time/size/content constraints remain enforced.

**Verify:** `mise exec -- go test ./pkg/federation/oidc ./cmd/prohibitorum -run 'Outbound|Hardened|Redirect|Metadata|Avatar|PrivateNetwork' -count=1` → PASS

**Steps:**

- [ ] **Step 1: Add table-driven RED tests for every IPv4/IPv6 class, mapped address, mixed public/private DNS results, public-to-private redirects, private-enabled IdP behavior, and unconditional metadata rejection.**

- [ ] **Step 2: Run tests to RED for missing classifications.**

- [ ] **Step 3: Implement one pure classifier returning `public`, `private`, or `alwaysBlocked`; use `net/netip` prefixes and unmap IPv4-mapped addresses before classification.**

```go
type destinationClass uint8
const (
    destinationPublic destinationClass = iota
    destinationPrivate
    destinationAlwaysBlocked
)
```

The dial policy allows public always, private only for that IdP, and never allows always-blocked.

- [ ] **Step 4: Route metadata CLI and avatar fetching through the identical classifier/client. Preserve scheme, redirect count, timeout, capped-body, and content-type checks.**

- [ ] **Step 5: Run focused tests to GREEN and commit.**

```bash
git add pkg/federation/oidc cmd/prohibitorum
git commit -m "fix: enforce outbound destination policy"
```

---

### Task 11: Atomic hashed refresh-family format

**Goal:** Replace split plaintext token mappings with one CAS-updated family record containing hashes and an encrypted retry successor.

**Files:**
- Modify: `pkg/protocol/oidc/refresh.go`
- Modify: `pkg/protocol/oidc/refresh_test.go`
- Modify: `pkg/protocol/oidc/token.go`
- Modify: `pkg/protocol/oidc/introspect.go`
- Modify: `pkg/protocol/oidc/revoke.go`
- Modify: `pkg/protocol/oidc/oidc.go`
- Modify: `pkg/kv/store.go`
- Modify: `pkg/kv/memory.go`
- Modify: `pkg/kv/redis.go`
- Modify: KV tests if CAS return semantics need strengthening

**Acceptance Criteria:**
- [ ] KV contains no plaintext usable refresh token.
- [ ] Family rotation is one authoritative compare-and-swap transition.
- [ ] Lost-response retry returns the exact already-issued successor within the grace window.
- [ ] Superseded reuse outside grace revokes the family.
- [ ] Legacy-format tokens always return `invalid_grant` after deployment.

**Verify:** `mise exec -- go test ./pkg/kv ./pkg/protocol/oidc -run 'Refresh|CAS|Introspect|Revoke' -count=1` → PASS

**Steps:**

- [ ] **Step 1: Write failing tests for storage plaintext absence, one-winner concurrency, CAS failure, identical retry successor, reuse revocation, introspection current/previous semantics, revocation, DEK rotation, and legacy invalidation.**

- [ ] **Step 2: Run tests to RED against token→family mappings and plaintext family tokens.**

- [ ] **Step 3: Implement the versioned token and family records.**

```go
type refreshFamily struct {
    Version            int       `json:"version"`
    Revision           uint64    `json:"revision"`
    FamilyID           string    `json:"family_id"`
    CurrentHash        [32]byte  `json:"current_hash"`
    PreviousHash       [32]byte  `json:"previous_hash"`
    EncryptedSuccessor string    `json:"encrypted_successor"`
    DEKVersion         int32     `json:"dek_version"`
    PreviousValidUntil time.Time `json:"previous_valid_until"`
    CreatedAt          time.Time `json:"created_at"`
    LastUsedAt         time.Time `json:"last_used_at"`
    AbsoluteExpiresAt  time.Time `json:"absolute_expires_at"`
    InactiveExpiresAt  time.Time `json:"inactive_expires_at"`
    ClientID           string    `json:"client_id"`
    AccountID          int32     `json:"account_id"`
    SessionID          string    `json:"session_id"`
    Scope              []string  `json:"scope"`
    AuthTime           time.Time `json:"auth_time"`
    AMR                []string  `json:"amr"`
    ACR                string    `json:"acr"`
}
```

Token format: `prt1.<base64url-family-id>.<base64url-32-byte-secret>`. Hash the secret with SHA-256; compare with `subtle.ConstantTimeCompare`. Encrypt only the complete successor token needed for idempotent recovery using the active DEK and authenticated family/revision context.

- [ ] **Step 4: Implement CAS rotation by marshaling the exact loaded record as expected bytes and the complete successor record as replacement bytes. On loss, reload and classify previous/current/reuse; delete the family on confirmed reuse. Remove token mapping keys and SetNX locks.**

- [ ] **Step 5: Update issue, refresh, introspection, and revocation to parse family ID and operate on the one record. Reject every token without `prt1` format.**

- [ ] **Step 6: Run focused tests to GREEN, then `mise exec -- go test -race ./pkg/kv ./pkg/protocol/oidc -count=1`.**

- [ ] **Step 7: Commit.**

```bash
git add pkg/kv pkg/protocol/oidc
git commit -m "feat: harden rotating refresh families"
```

---

### Task 12: Refresh lifetime and originating-session enforcement

**Goal:** Bound family lifetime and revoke refresh authority immediately when its session or authorization standing ends.

**Files:**
- Modify: `pkg/configx/configx.go`
- Modify: `pkg/configx/configx_test.go`
- Modify: `pkg/session/session.go`
- Modify: `pkg/session/session_test.go`
- Modify: `pkg/protocol/oidc/refresh.go`
- Modify: `pkg/protocol/oidc/refresh_test.go`
- Modify: `pkg/protocol/oidc/ttl_config_test.go`
- Modify: `pkg/server/handle_auth_recovery.go`
- Modify: account/session revocation handlers and tests

**Acceptance Criteria:**
- [ ] Inactivity lifetime defaults to 30 days and absolute lifetime to 90 days; both are configurable and validated.
- [ ] Rotation extends inactivity only up to absolute expiry.
- [ ] Every refresh verifies the originating session ID is still live.
- [ ] Session revoke, revoke-all, account recovery, account disable/delete, and application-access removal make the family unusable immediately or on the first subsequent refresh with durable family revocation.
- [ ] Audit/diagnostic reasons distinguish expiry, session revocation, access removal, account state, and reuse.

**Verify:** `mise exec -- go test ./pkg/configx ./pkg/session ./pkg/protocol/oidc ./pkg/server -run 'Refresh|Session.*Revoke|Recovery|Access.*Revok|Absolute|Inactivity' -count=1` → PASS

**Steps:**

- [ ] **Step 1: Write failing tests for inactivity boundary, absolute boundary, sliding cap, revoked session, revoke-all, recovery, disabled account, and app-access removal.**

- [ ] **Step 2: Run tests to RED.**

- [ ] **Step 3: Add `oidc.refresh_token_inactivity_ttl` and `oidc.refresh_token_absolute_ttl`; reject non-positive values and absolute lifetimes shorter than inactivity.**

- [ ] **Step 4: Add `SessionStore.IsSessionIDLive(ctx, accountID, sessionID)` using a bounded account session scan or a maintained session-ID index; do not accept the historical PostgreSQL row alone because revoked sessions remain as metadata.**

- [ ] **Step 5: In refresh exchange, check absolute/inactivity deadlines before rotation and check live session/account/app access before minting. Delete family on definitive denial; preserve it only for transient DB/KV errors where no authorization decision was made.**

- [ ] **Step 6: Ensure recovery/revoke flows invalidate session state that the refresh check observes; emit registered codes and safe audit reasons.**

- [ ] **Step 7: Run focused tests to GREEN and full affected race tests; commit.**

```bash
git add pkg/configx pkg/session pkg/protocol/oidc pkg/server
git commit -m "fix: bind refresh grants to live sessions"
```
