# PAT Fine-Grained Maturity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bring Personal Access Tokens to GitHub fine-grained-PAT maturity: the user picks which forward-auth apps a PAT covers and (per app) which admin-defined scopes it carries, with operator trust alerts and admin oversight.

**Architecture:** A PAT's flat `upstream_scopes`/`allowed_client_ids` are replaced by `all_apps boolean` + `app_grants jsonb` (`{client_id: [scopes]}`). Each forward-auth app gains an admin-defined `forward_auth_scopes` vocabulary (`[{name, description}]`). The gateway emits only the *current app's* scopes as `Remote-Scopes`. The PAT feature is unpushed, so its migration is amended in place (squash rule); the forward-auth column lands in a new migration.

**Tech Stack:** Go (chi + huma + sqlc/pgx + goose), Vue 3 + Vite + Tailwind v4 + shadcn-vue. Spec: `docs/superpowers/specs/2026-06-28-pat-fine-grained-maturity-design.md`.

**User decisions (already made):**
- "admin-defined per-app scopes" — each forward-auth app defines its scope vocabulary; users pick from it (no free text).
- "Per-app scope isolation" — `app_grants` is a per-app map; the gateway emits only the requested app's scopes.
- "Least-privilege default (F)" — creating a PAT requires ≥1 selected app, OR the explicit "all my apps" (`all_apps`, identity-only, no scopes).
- "Admin oversight (D)" — admins can list and revoke any user's PATs.
- Vocabulary stored as `jsonb [{name, description}]`; amend the unpushed migration `023` (not a 024 cleanup); new `024` adds `oidc_client.forward_auth_scopes`. Deferring **E** (max-lifetime policy).

---

## File Structure

**Backend (modify):**
- `db/migrations/023_personal_access_token.sql` — amend: `all_apps` + `app_grants` replace the flat columns.
- `db/queries/personal_access_token.sql` — InsertPAT shape; add `RevokePATByID`.
- `db/queries/oidc.sql` — FA queries return/set `forward_auth_scopes`; add a setter; extend the authorized-apps query.
- `pkg/db/*` — sqlc-generated (regenerate, don't hand-edit).
- `pkg/protocol/oidc/forward_auth.go` (+ test) — per-app `verifyForwardAuthPAT`.
- `pkg/contract/auth.go` — PAT view/create types; `ForwardAuthScope`; `ForwardAuthAppView.Scopes`; `MyForwardAuthApp` + op.
- `pkg/server/handle_me_tokens.go` (+ test) — create validation, view, `/me/forward-auth-apps`.
- `pkg/server/handle_admin_forward_auth_apps.go` (+ test) — thread `forward_auth_scopes`.
- `pkg/server/handle_admin_account_tokens.go` (new, + test) — admin list/revoke a user's PATs.
- `pkg/server/server.go` — new routes.

**Backend (create):** `db/migrations/024_forward_auth_scopes.sql`.

**Frontend (modify):**
- `dashboard/src/pages/TokensView.vue` — app + per-app-scope picker; per-app list display.
- `dashboard/src/pages/admin/AdminForwardAuthAppDetailView.vue` (+ create on `AdminForwardAuthAppsView.vue`) — scope-vocabulary editor + trust alert.
- `dashboard/src/pages/admin/AdminAccountDetailView.vue` — a PATs card (list + revoke).
- `dashboard/src/locales/{en,zh}.ts`.

**Docs (modify):** `docs/forward-auth.md`, `api.md`. **Smoke:** `cmd/smoke/main.go`.

---

### Task 0: Schema + queries (per-app PAT grants + FA scope vocabulary)

**Goal:** Reshape the PAT table to `all_apps`+`app_grants`, add `oidc_client.forward_auth_scopes`, and adjust/extend the sqlc queries; regenerate `pkg/db`.

**Files:**
- Modify: `db/migrations/023_personal_access_token.sql`
- Create: `db/migrations/024_forward_auth_scopes.sql`
- Modify: `db/queries/personal_access_token.sql`, `db/queries/oidc.sql`
- Modify (generated): `pkg/db/*`

**Acceptance Criteria:**
- [ ] PAT table has `all_apps boolean NOT NULL DEFAULT false` + `app_grants jsonb NOT NULL DEFAULT '{}'` and no `upstream_scopes`/`allowed_client_ids`.
- [ ] `oidc_client.forward_auth_scopes jsonb NOT NULL DEFAULT '[]'` exists (migration 024).
- [ ] sqlc generates: `InsertPAT` with `AllApps`/`AppGrants`; `RevokePATByID`; `SetForwardAuthScopes`; `GetForwardAuthAppByID`/`ListForwardAuthClients`/`UpdateForwardAuthApp`/`ListAuthorizedForwardAuthAppsForAccount` all carrying `ForwardAuthScopes`.
- [ ] `go build -tags nodynamic ./...` clean.

**Verify:** `sqlc generate && go build -tags nodynamic ./...` → no errors.

**Steps:**

- [ ] **Step 1: Amend migration 023.** Replace lines 14-15 of `db/migrations/023_personal_access_token.sql` (the two `*_scopes`/`*_client_ids` columns + the comment about them) so the table reads:

```sql
-- +goose Up
-- 023_personal_access_token.sql — user-owned Personal Access Tokens (PATs) for
-- programmatic access at the forward-auth gateway. A PAT authenticates AS its
-- owning account with reduced privileges. token_hash = sha256(raw token);
-- token_hint is a non-secret display aid. all_apps=true grants every app the
-- owner can reach (identity only, no scopes); otherwise app_grants maps each
-- granted forward-auth client_id to its chosen scopes (emitted as Remote-Scopes
-- for that app only).
CREATE TABLE personal_access_token (
  id           serial PRIMARY KEY,
  account_id   integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  name         text NOT NULL,
  token_hash   bytea NOT NULL UNIQUE,
  token_hint   text NOT NULL,
  all_apps     boolean NOT NULL DEFAULT false,
  app_grants   jsonb   NOT NULL DEFAULT '{}'::jsonb,
  created_at   timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz,
  last_used_at timestamptz,
  revoked_at   timestamptz
);
CREATE INDEX personal_access_token_account_idx ON personal_access_token(account_id);

-- +goose Down
DROP TABLE IF EXISTS personal_access_token;
```

(Amending a committed-but-unpushed migration is intentional — the PAT feature is undeployed; per the project's squash-pre-deployment-migrations rule. A dev DB that already ran the old 023 must be reset: `mise run db reset`. The smoke's throwaway DB always runs fresh.)

- [ ] **Step 2: Create migration 024.** `db/migrations/024_forward_auth_scopes.sql`:

```sql
-- +goose Up
-- 024_forward_auth_scopes.sql — admin-defined scope vocabulary for a forward-auth
-- app. JSONB array of { "name": "...", "description": "..." }. Opaque to the
-- gateway; surfaced in the user's PAT scope picker and emitted (the chosen subset)
-- as the Remote-Scopes header for that app.
ALTER TABLE oidc_client
  ADD COLUMN IF NOT EXISTS forward_auth_scopes jsonb NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down
ALTER TABLE oidc_client DROP COLUMN IF EXISTS forward_auth_scopes;
```

- [ ] **Step 3: Update PAT queries.** In `db/queries/personal_access_token.sql`: change `InsertPAT` columns and add `RevokePATByID`; leave the rest:

```sql
-- name: InsertPAT :one
INSERT INTO personal_access_token (
  account_id, name, token_hash, token_hint, all_apps, app_grants, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: RevokePATByID :execrows
UPDATE personal_access_token
SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL;
```

(`GetPATByTokenHash`, `ListPATsByAccount`, `RevokePAT`, `TouchPATLastUsed` stay as-is — `SELECT *` now returns the new columns.)

- [ ] **Step 4: Update FA queries.** In `db/queries/oidc.sql`, add `forward_auth_scopes` to the three read shapes and the update, add a setter, and extend the authorized-apps query. Replace those query bodies with:

```sql
-- name: ListForwardAuthClients :many
SELECT client_id, display_name, forward_auth_host, forward_auth_scopes, access_restricted, disabled, created_at
FROM oidc_client
WHERE forward_auth_enabled = true
ORDER BY created_at DESC;

-- name: GetForwardAuthAppByID :one
SELECT client_id, display_name, forward_auth_host, forward_auth_scopes, access_restricted, disabled, created_at
FROM oidc_client
WHERE client_id = $1 AND forward_auth_enabled = true;

-- name: UpdateForwardAuthApp :one
UPDATE oidc_client
SET display_name = $2, redirect_uris = $3, forward_auth_host = $4, forward_auth_scopes = $5
WHERE client_id = $1 AND forward_auth_enabled = true
RETURNING client_id, display_name, forward_auth_host, forward_auth_scopes, access_restricted, disabled, created_at;

-- name: SetForwardAuthScopes :exec
UPDATE oidc_client SET forward_auth_scopes = $2
WHERE client_id = $1 AND forward_auth_enabled = true;
```

In `db/queries/rbac.sql`, extend `ListAuthorizedForwardAuthAppsForAccount`'s SELECT list (line 147) to add the vocabulary column:

```sql
-- name: ListAuthorizedForwardAuthAppsForAccount :many
SELECT c.client_id, c.display_name, c.forward_auth_host, c.forward_auth_scopes
FROM oidc_client c
WHERE c.disabled = false
  AND c.forward_auth_enabled = true
  AND c.forward_auth_host IS NOT NULL
  AND (
    NOT c.access_restricted
    OR EXISTS (SELECT 1 FROM oidc_client_access a
               WHERE a.client_id = c.client_id AND a.account_id = sqlc.arg(account_id))
    OR EXISTS (SELECT 1 FROM oidc_client_access a
               JOIN group_member m ON m.group_id = a.group_id
               WHERE a.client_id = c.client_id AND m.account_id = sqlc.arg(account_id))
  )
ORDER BY c.display_name;
```

(The launchpad `buildLaunchpad` consumes this query and simply ignores the new `ForwardAuthScopes` field — no change needed there.)

- [ ] **Step 5: Regenerate + build.**

Run: `sqlc generate && go build -tags nodynamic ./...`
Expected: exit 0. `db.PersonalAccessToken` now has `AllApps bool`, `AppGrants []byte`; `db.InsertPATParams` has `AllApps`, `AppGrants`; `RevokePATByID`, `SetForwardAuthScopes` exist; the FA rows carry `ForwardAuthScopes []byte`.

(The build will fail in `pkg/server` and `pkg/protocol/oidc` because they still reference the dropped fields — that's expected; Tasks 1–3 fix them. To keep Task 0 self-contained and committable, comment-free temporary breakage is acceptable *only if* you complete through Task 3 before the gate. Alternatively commit Task 0 with `git commit --no-verify` after `sqlc generate` succeeds and the migrations are valid, and let Tasks 1–3 restore the build. Pick the former: do NOT commit a broken build — fold Tasks 0–3 into one commit if needed. See Step 6.)

- [ ] **Step 6: Commit.** Because Task 0 alone leaves `pkg/server`/`pkg/protocol/oidc` referencing dropped fields, commit Task 0's schema+queries+generated code together with Task 1 and Task 2's Go changes (the first point at which `go build` is green again). Until then, stage but do not commit. (The reviewer checks the combined Task 0–2 diff.)

```bash
git add db/migrations db/queries pkg/db
# held until Task 2 restores the build; see Tasks 1–2.
```

---

### Task 1: Gateway per-app scope emission

**Goal:** `verifyForwardAuthPAT` honors `all_apps` + per-app `app_grants`, emitting only the requested app's scopes.

**Files:**
- Modify: `pkg/protocol/oidc/forward_auth.go`
- Test: `pkg/protocol/oidc/forward_auth_test.go`

**Acceptance Criteria:**
- [ ] `all_apps=true` PAT → 200 with empty `Remote-Scopes`.
- [ ] PAT with `app_grants[client_id]` present → 200 with `Remote-Scopes` = exactly those scopes.
- [ ] PAT whose `app_grants` lacks this client_id (and `all_apps=false`) → 403.
- [ ] invalid/expired/revoked/disabled-owner → 401; live-RBAC-deny on a granted app → 403.

**Verify:** `go test ./pkg/protocol/oidc/ -run ForwardAuth -v` → PASS.

**Steps:**

- [ ] **Step 1: Replace the helper.** In `pkg/protocol/oidc/forward_auth.go`, replace `verifyForwardAuthPAT` and delete `patAllowsClient` with:

```go
// verifyForwardAuthPAT authenticates a forward-auth request by Personal Access
// Token, with per-app scope isolation. Terminal: always writes a response.
// 401 = unresolvable/disabled owner; 403 = owner not granted this app (or RBAC).
// On success Remote-Scopes carries only THIS app's chosen scopes.
func (p *Provider) verifyForwardAuthPAT(w http.ResponseWriter, r *http.Request, raw string, client db.GetForwardAuthClientByHostRow) {
	ctx := r.Context()
	row, err := p.queries.GetPATByTokenHash(ctx, pat.HashToken(raw))
	if err != nil {
		writeBearerError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	acct, err := p.queries.GetAccountByID(ctx, row.AccountID)
	if err != nil || acct.Disabled {
		writeBearerError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	var scopes []string
	if !row.AllApps {
		grants := map[string][]string{}
		if len(row.AppGrants) > 0 {
			if jerr := json.Unmarshal(row.AppGrants, &grants); jerr != nil {
				http.Error(w, "forbidden", http.StatusForbidden) // corrupt grant → fail closed
				return
			}
		}
		s, ok := grants[client.ClientID]
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden) // PAT not granted for this app
			return
		}
		scopes = s
	}
	ok, aerr := p.queries.IsAccountAuthorizedForOIDCClient(ctx, db.IsAccountAuthorizedForOIDCClientParams{
		AccountID: pgtype.Int4{Int32: acct.ID, Valid: true}, ClientID: client.ClientID,
	})
	if aerr != nil || !ok.Bool {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	groups, _ := p.queries.ListExposedGroupSlugsByAccount(ctx, acct.ID)
	writeIdentityHeaders(w, acct.Username, acct.DisplayName, accountEmail(acct), groups, scopes)
	_ = p.queries.TouchPATLastUsed(ctx, row.ID)
	w.WriteHeader(http.StatusOK)
}
```

`encoding/json` is already imported in `forward_auth.go`. The Bearer branch in `HandleForwardAuthVerify` (`if raw := bearerToken(r); raw != "" { p.verifyForwardAuthPAT(...); return }`) and `writeIdentityHeaders` are unchanged.

- [ ] **Step 2: Update the fake querier + tests.** In `forward_auth_test.go`, the `fakeFAQueries.pat` field is now `db.PersonalAccessToken` with `AllApps bool` + `AppGrants []byte`. Replace the six PAT tests' `pat:` literals and assertions:

```go
func TestForwardAuthVerify_PAT_GrantedApp_200WithScopes(t *testing.T) {
	q := &fakeFAQueries{
		faClient:   db.GetForwardAuthClientByHostRow{ClientID: "svc"},
		authorized: true,
		acct:       db.Account{ID: 42, Username: "alice", DisplayName: "Alice"},
		groups:     []string{"staff"},
		pat:        db.PersonalAccessToken{ID: 7, AccountID: 42, AppGrants: []byte(`{"svc":["repo:read"]}`)},
	}
	p, _ := newFAProvider(q)
	rec := httptest.NewRecorder()
	p.HandleForwardAuthVerify(rec, faBearerRequest("app.acme.io", "prohibitorum_pat_x", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Remote-Scopes") != "repo:read" {
		t.Fatalf("code=%d scopes=%q", rec.Code, rec.Header().Get("Remote-Scopes"))
	}
}

func TestForwardAuthVerify_PAT_AllApps_200NoScopes(t *testing.T) {
	q := &fakeFAQueries{
		faClient: db.GetForwardAuthClientByHostRow{ClientID: "svc"}, authorized: true,
		acct: db.Account{ID: 42, Username: "alice"},
		pat:  db.PersonalAccessToken{ID: 7, AccountID: 42, AllApps: true},
	}
	p, _ := newFAProvider(q)
	rec := httptest.NewRecorder()
	p.HandleForwardAuthVerify(rec, faBearerRequest("app.acme.io", "prohibitorum_pat_x", nil))
	if rec.Code != http.StatusOK || rec.Header().Get("Remote-Scopes") != "" {
		t.Fatalf("code=%d scopes=%q", rec.Code, rec.Header().Get("Remote-Scopes"))
	}
}

func TestForwardAuthVerify_PAT_NotGrantedApp_403(t *testing.T) {
	q := &fakeFAQueries{
		faClient: db.GetForwardAuthClientByHostRow{ClientID: "svc"}, authorized: true,
		acct: db.Account{ID: 42, Username: "alice"},
		pat:  db.PersonalAccessToken{ID: 7, AccountID: 42, AppGrants: []byte(`{"other":["x"]}`)},
	}
	p, _ := newFAProvider(q)
	rec := httptest.NewRecorder()
	p.HandleForwardAuthVerify(rec, faBearerRequest("app.acme.io", "prohibitorum_pat_x", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rec.Code)
	}
}
```

Keep the existing `_Invalid_401`, `_DisabledOwner_401`, `_PrecedesCookie`, and `_RBACDenies_403` tests, but in the latter set `pat: db.PersonalAccessToken{ID: 7, AccountID: 42, AppGrants: []byte(`{"svc":[]}`)}` (granted, empty scopes) so the 403 comes from `authorized:false`, not the grant check. The `TestForwardAuthVerify_PAT_AppRestrictionExcludes_403` test is superseded by `_NotGrantedApp_403` — delete it.

- [ ] **Step 3: Run + (with Task 0/2) build.**

Run: `go test ./pkg/protocol/oidc/ -run ForwardAuth -v`
Expected: PASS.

- [ ] **Step 4: (commit folded with Task 0 + Task 2 — see Task 2 Step 7.)**

---

### Task 2: Self-service reshape — create validation, view, `/me/forward-auth-apps`

**Goal:** The `/me/tokens` create takes `allApps`+`appGrants` (validated against the owner's authorized apps and each app's vocabulary), the view exposes them, and a new `GET /me/forward-auth-apps` feeds the picker.

**Files:**
- Modify: `pkg/contract/auth.go`
- Modify: `pkg/server/handle_me_tokens.go`
- Modify: `pkg/server/server.go`
- Test: `pkg/server/handle_me_tokens_test.go`

**Acceptance Criteria:**
- [ ] Create with `appGrants` whose apps ⊆ the owner's authorized FA apps and scopes ⊆ each app's vocabulary succeeds; out-of-set app or scope → `bad_request`.
- [ ] `allApps=false` with empty `appGrants` → `bad_request` (least-privilege); `allApps=true` with non-empty `appGrants` → `bad_request`.
- [ ] `GET /me/forward-auth-apps` returns only the caller's authorized FA apps, each with its `scopes` vocabulary.
- [ ] List/created views expose `allApps` + `appGrants`, never a secret.

**Verify:** `go test ./pkg/server/ -run 'Token|MyForwardAuthApps' -v && go build -tags nodynamic ./...` → PASS.

**Steps:**

- [ ] **Step 1: Contract types.** In `pkg/contract/auth.go` replace the PAT view/created block (lines ~88-107) and add the FA-scope + my-apps types:

```go
// ForwardAuthScope is one admin-defined scope label for a forward-auth app.
type ForwardAuthScope struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// PersonalAccessTokenView is a row in /me/tokens. No secret; per-app grants.
type PersonalAccessTokenView struct {
	ID         int32               `json:"id"`
	Name       string              `json:"name"`
	TokenHint  string              `json:"tokenHint"`
	AllApps    bool                `json:"allApps"`
	AppGrants  map[string][]string `json:"appGrants"` // client_id -> scopes
	CreatedAt  time.Time           `json:"createdAt"`
	ExpiresAt  *time.Time          `json:"expiresAt,omitempty"`
	LastUsedAt *time.Time          `json:"lastUsedAt,omitempty"`
}

// PersonalAccessTokenCreated reveals the plaintext exactly once.
type PersonalAccessTokenCreated struct {
	Token string                  `json:"token"`
	PAT   PersonalAccessTokenView `json:"pat"`
}

// MyForwardAuthApp is one forward-auth app the caller may use, with its scope
// vocabulary — the candidate list for the PAT create picker.
type MyForwardAuthApp struct {
	ClientID    string             `json:"clientId"`
	DisplayName string             `json:"displayName"`
	Scopes      []ForwardAuthScope `json:"scopes"`
}

var OperationListMyForwardAuthApps = huma.Operation{
	OperationID: "listMyForwardAuthApps",
	Method:      http.MethodGet,
	Path:        "/me/forward-auth-apps",
	Summary:     "List the forward-auth apps the caller may use, with each app's scope vocabulary.",
}
```

- [ ] **Step 2: Rewrite the handlers.** Replace `pkg/server/handle_me_tokens.go`'s `patQueries`, `patView`, `nonNilStrings`, and `handleCreateMyToken`/`handleListMyTokens` with the per-app versions, and add the my-apps handler. Full file body for the changed parts:

```go
type patQueries interface {
	InsertPAT(ctx context.Context, arg db.InsertPATParams) (db.PersonalAccessToken, error)
	ListPATsByAccount(ctx context.Context, accountID int32) ([]db.PersonalAccessToken, error)
	RevokePAT(ctx context.Context, arg db.RevokePATParams) (int64, error)
	ListAuthorizedForwardAuthAppsForAccount(ctx context.Context, accountID pgtype.Int4) ([]db.ListAuthorizedForwardAuthAppsForAccountRow, error)
}

// patView projects a row, unmarshalling app_grants (jsonb) to a map.
func patView(row db.PersonalAccessToken) contract.PersonalAccessTokenView {
	grants := map[string][]string{}
	if len(row.AppGrants) > 0 {
		_ = json.Unmarshal(row.AppGrants, &grants)
	}
	v := contract.PersonalAccessTokenView{
		ID: row.ID, Name: row.Name, TokenHint: row.TokenHint,
		AllApps: row.AllApps, AppGrants: grants, CreatedAt: row.CreatedAt.Time,
	}
	if row.ExpiresAt.Valid {
		t := row.ExpiresAt.Time
		v.ExpiresAt = &t
	}
	if row.LastUsedAt.Valid {
		t := row.LastUsedAt.Time
		v.LastUsedAt = &t
	}
	return v
}

// parseFAScopes unmarshals an app's forward_auth_scopes jsonb into the wire shape.
func parseFAScopes(raw []byte) []contract.ForwardAuthScope {
	out := []contract.ForwardAuthScope{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

type createMyTokenIn struct {
	Body struct {
		Name          string              `json:"name"`
		ExpiresInDays *int                `json:"expiresInDays,omitempty"`
		AllApps       bool                `json:"allApps"`
		AppGrants     map[string][]string `json:"appGrants"`
	}
}

func (s *Server) handleCreateMyToken(ctx context.Context, in *createMyTokenIn) (*createMyTokenOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	name := strings.TrimSpace(in.Body.Name)
	if name == "" || len(name) > 128 {
		return nil, authErrToHuma(authn.ErrBadRequest())
	}
	if d := in.Body.ExpiresInDays; d != nil && (*d < 0 || *d > 3650) {
		return nil, authErrToHuma(authn.ErrBadRequest())
	}

	q := s.patQueriesFn()
	grants := in.Body.AppGrants
	if grants == nil {
		grants = map[string][]string{}
	}
	if in.Body.AllApps {
		if len(grants) > 0 { // all_apps is identity-only
			return nil, authErrToHuma(authn.ErrBadRequest())
		}
	} else {
		if len(grants) == 0 { // least-privilege: must pick ≥1 app
			return nil, authErrToHuma(authn.ErrBadRequest())
		}
		// Build the owner's authorized app -> allowed-scope-set map.
		rows, err := q.ListAuthorizedForwardAuthAppsForAccount(ctx, pgtype.Int4{Int32: sess.Account.ID, Valid: true})
		if err != nil {
			return nil, fmt.Errorf("handleCreateMyToken: authorized apps: %w", err)
		}
		vocab := map[string]map[string]bool{}
		for _, r := range rows {
			set := map[string]bool{}
			for _, sc := range parseFAScopes(r.ForwardAuthScopes) {
				set[sc.Name] = true
			}
			vocab[r.ClientID] = set
		}
		for cid, scopes := range grants {
			allowed, ok := vocab[cid]
			if !ok {
				return nil, authErrToHuma(authn.ErrBadRequest()) // not an authorized app
			}
			for _, sc := range scopes {
				if !allowed[sc] {
					return nil, authErrToHuma(authn.ErrBadRequest()) // scope not in vocabulary
				}
			}
		}
	}

	raw, hash, hint, err := pat.Generate()
	if err != nil {
		return nil, fmt.Errorf("handleCreateMyToken: generate: %w", err)
	}
	grantsJSON, _ := json.Marshal(grants)
	var expires pgtype.Timestamptz
	if in.Body.ExpiresInDays != nil && *in.Body.ExpiresInDays > 0 {
		expires = pgtype.Timestamptz{Time: time.Now().AddDate(0, 0, *in.Body.ExpiresInDays), Valid: true}
	}
	row, err := q.InsertPAT(ctx, db.InsertPATParams{
		AccountID: sess.Account.ID, Name: name, TokenHash: hash, TokenHint: hint,
		AllApps: in.Body.AllApps, AppGrants: grantsJSON, ExpiresAt: expires,
	})
	if err != nil {
		return nil, fmt.Errorf("handleCreateMyToken: insert: %w", err)
	}
	credRef := int64(row.ID)
	_ = s.Audit.Record(ctx, audit.Record{
		AccountID: &sess.Account.ID, Factor: audit.FactorPAT, Event: audit.EventRegister,
		CredentialRef: &credRef, Detail: map[string]any{"name": name},
	})
	logx.WithContext(ctx).WithFields(logrus.Fields{"event": "auth.pat_created", "account_id": sess.Account.ID, "pat_id": row.ID}).Info("auth")
	return &createMyTokenOut{Body: contract.PersonalAccessTokenCreated{Token: raw, PAT: patView(row)}}, nil
}

// ----- GET /me/forward-auth-apps -----------------------------------------

type listMyFAAppsOut struct {
	Body []contract.MyForwardAuthApp
}

func (s *Server) handleListMyForwardAuthApps(ctx context.Context, _ *struct{}) (*listMyFAAppsOut, error) {
	sess := authn.SessionFromContext(ctx)
	if sess == nil {
		return nil, authErrToHuma(authn.ErrNoSession())
	}
	rows, err := s.patQueriesFn().ListAuthorizedForwardAuthAppsForAccount(ctx, pgtype.Int4{Int32: sess.Account.ID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("handleListMyForwardAuthApps: %w", err)
	}
	out := make([]contract.MyForwardAuthApp, 0, len(rows))
	for _, r := range rows {
		out = append(out, contract.MyForwardAuthApp{
			ClientID: r.ClientID, DisplayName: r.DisplayName, Scopes: parseFAScopes(r.ForwardAuthScopes),
		})
	}
	return &listMyFAAppsOut{Body: out}, nil
}
```

Add `"encoding/json"` to the imports; drop `nonNilStrings` (now unused). `handleListMyTokens`/`handleRevokeMyToken` are unchanged (they already use `patView` / `RevokePAT`).

- [ ] **Step 3: Route.** In `pkg/server/server.go`, after the `OperationRevokeMyToken` registration add:

```go
	registerOp(mgmt, contract.OperationListMyForwardAuthApps, s.handleListMyForwardAuthApps, sessionReq)
```

- [ ] **Step 4: Tests.** Update `pkg/server/handle_me_tokens_test.go`'s `fakePATQ` to implement `ListAuthorizedForwardAuthAppsForAccount` (return a fixed app `svc` with vocabulary `[{repo:read},{repo:write}]`) and store inserted rows with `AllApps`/`AppGrants`. Rewrite the create tests to the per-app shape; add: granted-app+valid-scope → ok; unknown app → error; unknown scope → error; `allApps=false` empty grants → error; `allApps=true` with grants → error; `/me/forward-auth-apps` returns the app+vocab. Mirror the harness already in the file (`newPATServer`, `patCtx`).

- [ ] **Step 5: Run + build.**

Run: `go test ./pkg/server/ -run 'Token|MyForwardAuthApps' -v && go test ./pkg/protocol/oidc/ -run ForwardAuth && go build -tags nodynamic ./...`
Expected: PASS; build clean (Tasks 0+1+2 together restore the build).

- [ ] **Step 6: Commit Tasks 0+1+2 together** (first green build):

```bash
git add db/migrations db/queries pkg/db pkg/protocol/oidc/forward_auth.go pkg/protocol/oidc/forward_auth_test.go pkg/contract/auth.go pkg/server/handle_me_tokens.go pkg/server/handle_me_tokens_test.go pkg/server/server.go
git commit -m "feat(pat): per-app grants + scope vocabulary — schema, gateway, self-service"
```

---

### Task 3: Admin — per-app scope vocabulary on forward-auth apps

**Goal:** Admins set each forward-auth app's `forward_auth_scopes` vocabulary via create/update; it round-trips through the view; names validated.

**Files:**
- Modify: `pkg/contract/auth.go` (`ForwardAuthAppView.Scopes`)
- Modify: `pkg/server/handle_admin_forward_auth_apps.go`
- Test: `pkg/server/handle_admin_forward_auth_apps_test.go`

**Acceptance Criteria:**
- [ ] Create/update accept a `scopes: [{name, description}]` array; it is stored and returned by get/list.
- [ ] Invalid scope name (fails `^[a-zA-Z0-9](:?[a-zA-Z0-9._-]+)*$`) or duplicate name → `bad_request`.
- [ ] `forwardAuthAppView` includes `scopes`.

**Verify:** `go test ./pkg/server/ -run ForwardAuthApp -v && go build -tags nodynamic ./...` → PASS.

**Steps:**

- [ ] **Step 1: View field.** In `pkg/contract/auth.go`, add to `ForwardAuthAppView`:

```go
	Scopes []ForwardAuthScope `json:"scopes"`
```

- [ ] **Step 2: Validation helper + projection.** In `handle_admin_forward_auth_apps.go` add a validator and thread scopes through the view + create + update. Add:

```go
var faScopeNameRe = regexp.MustCompile(`^[a-zA-Z0-9](:?[a-zA-Z0-9._-]+)*$`)

// validateFAScopes returns the normalized scopes (or an error) — names must match
// the label pattern and be unique. nil/empty is valid (no vocabulary).
func validateFAScopes(in []contract.ForwardAuthScope) ([]contract.ForwardAuthScope, error) {
	seen := map[string]bool{}
	out := make([]contract.ForwardAuthScope, 0, len(in))
	for _, sc := range in {
		name := strings.TrimSpace(sc.Name)
		if name == "" || len(name) > 64 || !faScopeNameRe.MatchString(name) || seen[name] {
			return nil, authn.ErrBadRequest()
		}
		seen[name] = true
		out = append(out, contract.ForwardAuthScope{Name: name, Description: strings.TrimSpace(sc.Description)})
	}
	return out, nil
}
```

Change `forwardAuthAppView` to take a `scopes []byte` (the jsonb) and parse it:

```go
func forwardAuthAppView(clientID, displayName string, host pgtype.Text, scopesJSON []byte, accessRestricted, disabled bool, createdAt pgtype.Timestamptz) contract.ForwardAuthAppView {
	v := contract.ForwardAuthAppView{
		ClientID: clientID, DisplayName: displayName, ForwardAuthHost: host.String,
		Scopes: parseFAScopes(scopesJSON), AccessRestricted: accessRestricted, Disabled: disabled,
	}
	if createdAt.Valid {
		v.CreatedAt = createdAt.Time
	}
	return v
}
```

Update all `forwardAuthAppView(...)` call sites to pass the row's `ForwardAuthScopes` (list/get/update). For the create handler (which builds the view from create-time values, not a row), pass the request's scopes marshalled, or `[]byte("[]")` plus set it explicitly.

- [ ] **Step 3: Thread create/update.** Add `Scopes []contract.ForwardAuthScope json:"scopes"` to `createForwardAuthAppBody` and `updateForwardAuthAppBody`. In create: after `RegisterForwardAuthApp` succeeds, `validateFAScopes`, marshal, and `s.queries.SetForwardAuthScopes(ctx, db.SetForwardAuthScopesParams{ClientID: body.ClientID, ForwardAuthScopes: scopesJSON})`. In update: `validateFAScopes`, marshal, and pass `ForwardAuthScopes: scopesJSON` to `UpdateForwardAuthApp` (now 5 args). Return `bad_request` on validation failure.

- [ ] **Step 4: Tests.** Extend `handle_admin_forward_auth_apps_test.go`: create/update with scopes round-trips through the view; invalid name and duplicate name → bad_request. Mirror the existing handler-test harness.

- [ ] **Step 5: Run + commit.**

Run: `go test ./pkg/server/ -run ForwardAuthApp -v && go build -tags nodynamic ./...`
```bash
git add pkg/contract/auth.go pkg/server/handle_admin_forward_auth_apps.go pkg/server/handle_admin_forward_auth_apps_test.go
git commit -m "feat(pat): admin-defined per-app scope vocabulary on forward-auth apps"
```

---

### Task 4: Admin oversight — list/revoke a user's PATs

**Goal:** Admins can list and revoke any account's PATs.

**Files:**
- Create: `pkg/server/handle_admin_account_tokens.go`
- Modify: `pkg/server/server.go`, `pkg/contract/auth.go` (operations)
- Test: `pkg/server/handle_admin_account_tokens_test.go`

**Acceptance Criteria:**
- [ ] `GET /accounts/{id}/tokens` (admin) → the account's non-revoked PATs as `PersonalAccessTokenView` (no secret).
- [ ] `POST /accounts/tokens/revoke {id}` (admin + sudo) revokes by PAT id and audits `FactorPAT`/`EventRevoke` with `detail.actor="admin"`; unknown/already-revoked id → not-found error.

**Verify:** `go test ./pkg/server/ -run AccountTokens -v && go build -tags nodynamic ./...` → PASS.

**Steps:**

- [ ] **Step 1: Operations.** In `pkg/contract/auth.go` add `OperationListAccountTokens` (GET `/accounts/{id}/tokens`) mirroring the existing `OperationListAccountSessions` literal (find it near `handleListAccountSessions`).

- [ ] **Step 2: Handlers.** Create `pkg/server/handle_admin_account_tokens.go` mirroring `handleListAccountSessions` (typed, admin) and a raw sudo-gated revoke mirroring `handleRevokeAccountSessionHTTP`:

```go
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"prohibitorum/pkg/audit"
	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/contract"
)

type accountTokensOut struct {
	Body []contract.PersonalAccessTokenView
}

func (s *Server) handleListAccountTokens(ctx context.Context, in *getAccountIn) (*accountTokensOut, error) {
	id, err := parseAccountID(in.ID) // reuse the same id-parse helper handleGetAccount uses
	if err != nil {
		return nil, authErrToHuma(authn.ErrBadRequest())
	}
	rows, err := s.patQueriesFn().ListPATsByAccount(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("handleListAccountTokens: %w", err)
	}
	out := make([]contract.PersonalAccessTokenView, 0, len(rows))
	for _, r := range rows {
		out = append(out, patView(r))
	}
	return &accountTokensOut{Body: out}, nil
}

type revokeAccountTokenBody struct {
	ID int32 `json:"id"`
}

func (s *Server) handleRevokeAccountTokenHTTP(w http.ResponseWriter, r *http.Request) {
	var body revokeAccountTokenBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == 0 {
		writeAuthErr(w, authn.ErrBadRequest())
		return
	}
	n, err := s.queries.RevokePATByID(r.Context(), body.ID)
	if err != nil {
		writeAuthErr(w, fmt.Errorf("handleRevokeAccountToken: %w", err))
		return
	}
	if n == 0 {
		writeAuthErr(w, authn.ErrCredentialNotFound())
		return
	}
	credRef := int64(body.ID)
	_ = s.Audit.Record(r.Context(), audit.Record{
		AccountID: faActorID(r.Context()), Factor: audit.FactorPAT, Event: audit.EventRevoke,
		CredentialRef: &credRef, Detail: map[string]any{"actor": "admin"},
	})
	w.WriteHeader(http.StatusNoContent)
}
```

Confirm the exact id-parse helper name `parseAccountID` / `getAccountIn` against `handleGetAccount`/`handleListAccountSessions` in `handle_account.go` and reuse it verbatim — do not invent one.

- [ ] **Step 3: Routes.** In `server.go`, near the account-sessions registrations: `registerOp(mgmt, contract.OperationListAccountTokens, s.handleListAccountTokens, admin)` and `s.registerSudoOpHTTP(s.router, "POST", "/api/prohibitorum/accounts/tokens/revoke", admin, s.handleRevokeAccountTokenHTTP)`. Match the exact `admin` requirement var and the `registerSudoOpHTTP` signature used by the sibling account-session revoke.

- [ ] **Step 4: Tests + commit.**

Run: `go test ./pkg/server/ -run AccountTokens -v && go build -tags nodynamic ./...`
```bash
git add pkg/server/handle_admin_account_tokens.go pkg/server/handle_admin_account_tokens_test.go pkg/contract/auth.go pkg/server/server.go
git commit -m "feat(pat): admin oversight — list/revoke a user's PATs"
```

---

### Task 5: Dashboard — PAT create picker (apps + per-app scopes) and per-app list

**Goal:** The PAT create dialog lets the user pick apps and (per app) scopes from the vocabulary, with the least-privilege default; the list shows per-app grants.

**Files:**
- Modify: `dashboard/src/pages/TokensView.vue`
- Modify: `dashboard/src/locales/{en,zh}.ts`

**Acceptance Criteria:**
- [ ] On open, the dialog fetches `GET /api/prohibitorum/me/forward-auth-apps`; each app has an include checkbox; an included app expands to its scope checkboxes (name + description).
- [ ] An "All my apps (no scopes)" switch sets `allApps`; submit is disabled unless `allApps` or ≥1 app is included.
- [ ] Submit posts `{ name, expiresInDays?, allApps, appGrants }`; the list renders each token's per-app grants (app display name → scopes) and the Expired badge; en/zh parity holds.

**Verify:** `cd dashboard && npm run build && npm test` → build clean + tests PASS.

**Steps:**

- [ ] **Step 1: Replace the create form + types in `TokensView.vue`.** Drop the `ScopeSelector` import and `newScopes`. Add a `FAApp` interface `{ clientId, displayName, scopes: {name,description}[] }` and reactive state `apps: FAApp[]`, `allApps: boolean`, and `grants: Record<string, Set<string>>` (or `Record<string,string[]>`). On `openCreate()`, `GET /api/prohibitorum/me/forward-auth-apps` → `apps`; reset `allApps=false`, `grants={}`. The `PersonalAccessTokenView` TS interface changes from `{upstreamScopes, allowedClientIds}` to `{ allApps: boolean; appGrants: Record<string,string[]> }`.

The dialog form state (replace the `ScopeSelector` block):

```vue
<div class="flex flex-col gap-3">
  <label class="flex items-center gap-2 text-sm text-ink">
    <Switch v-model="allApps" data-test="all-apps" />
    <span>{{ t('tokens.allAppsLabel') }}</span>
  </label>
  <template v-if="!allApps">
    <p class="text-xs text-muted">{{ t('tokens.appsHelp') }}</p>
    <div v-for="a in apps" :key="a.clientId" class="rounded-md border border-border p-3">
      <label class="flex items-center gap-2 text-sm font-medium text-ink">
        <Checkbox :model-value="a.clientId in grants" data-test="app"
                  @update:model-value="(v) => toggleApp(a.clientId, v)" />
        <span>{{ a.displayName }}</span>
      </label>
      <div v-if="a.clientId in grants && a.scopes.length" class="mt-2 flex flex-col gap-1 pl-6">
        <label v-for="sc in a.scopes" :key="sc.name" class="flex items-center gap-2 text-sm text-ink">
          <Checkbox :model-value="grants[a.clientId].includes(sc.name)"
                    @update:model-value="(v) => toggleScope(a.clientId, sc.name, v)" />
          <span class="font-mono">{{ sc.name }}</span>
          <span v-if="sc.description" class="text-muted">— {{ sc.description }}</span>
        </label>
      </div>
    </div>
    <EmptyState v-if="!apps.length" :icon="Terminal" :title="t('tokens.noApps')" />
  </template>
</div>
```

with helpers:

```ts
function toggleApp(cid: string, on: boolean) {
  if (on) grants.value = { ...grants.value, [cid]: [] }
  else { const g = { ...grants.value }; delete g[cid]; grants.value = g }
}
function toggleScope(cid: string, name: string, on: boolean) {
  const cur = grants.value[cid] ?? []
  grants.value = { ...grants.value, [cid]: on ? [...cur, name] : cur.filter((s) => s !== name) }
}
const canSubmit = computed(() => newName.value.trim() && (allApps.value || Object.keys(grants.value).length > 0))
```

Submit body: `{ name: newName.value, expiresInDays: newExpiry.value || undefined, allApps: allApps.value, appGrants: allApps.value ? {} : grants.value }`. Submit button `:disabled="busy || !canSubmit"`.

- [ ] **Step 2: List display.** Replace the old flat scopes/allowedClientIds rendering. For each token: if `r.allApps` show `t('tokens.allAppsLabel')`; else for each `(clientId, scopes)` in `r.appGrants`, show the app's display name (resolve via the fetched `apps`, falling back to the raw clientId) and its scopes as small mono chips. Fetch `/me/forward-auth-apps` on mount too (for name resolution), or accept showing client_ids when the apps list isn't loaded.

- [ ] **Step 3: i18n.** In `en.ts` `tokens` block add `allAppsLabel: 'All my apps (no scopes)'`, `appsHelp: 'Choose which apps this token can reach, and which scopes for each.'`, `noApps: 'You don’t have access to any gateway apps.'`. Mirror into `zh.ts` (`allAppsLabel: '我的所有应用（无作用域）'`, `appsHelp: '选择此令牌可访问的应用，以及每个应用的作用域。'`, `noApps: '你没有可访问的网关应用。'`). Remove the now-unused `scopesLabel`/`scopesHint` keys from both. Keep parity.

- [ ] **Step 4: Build, test, rebuild dist, commit.**

Run: `cd dashboard && npm run build && npm test`
```bash
cd /home/tundra/projects/tundra/prohibitorum
git add dashboard/src pkg/webui/dist
git commit -m "feat(pat): per-app app+scope picker on the tokens page"
```

---

### Task 6: Dashboard — admin scope-vocabulary editor + trust alerts

**Goal:** Admins edit each forward-auth app's scope vocabulary; the FA admin pages carry the deployment trust warnings.

**Files:**
- Modify: `dashboard/src/pages/admin/AdminForwardAuthAppDetailView.vue`, `dashboard/src/pages/admin/AdminForwardAuthAppsView.vue`
- Modify: `dashboard/src/locales/{en,zh}.ts`

**Acceptance Criteria:**
- [ ] The detail view has a scope-vocabulary editor (name + description rows, add/remove); it loads from and saves via the FA app PUT (`scopes`).
- [ ] A `warning` Alert states: app must be reachable only through Traefik; all five `Remote-*` in `authResponseHeaders`; strip inbound `Authorization`.
- [ ] en/zh parity holds.

**Verify:** `cd dashboard && npm run build && npm test` → build clean + tests PASS.

**Steps:**

- [ ] **Step 1: Vocabulary editor.** Mirror `dashboard/src/components/custom/AttributeMapEditor.vue` (read it first) for a name+description row list bound to a `scopes: {name,description}[]` model. Add it to `AdminForwardAuthAppDetailView.vue` inside a `FormSection`, seeded from the loaded app's `scopes`, and include `scopes` in the PUT body alongside `displayName`/`host`. Add a matching (optional) editor on the create form in `AdminForwardAuthAppsView.vue`, sending `scopes` in the create body.

- [ ] **Step 2: Trust alert.** Beside the existing Traefik snippet in `AdminForwardAuthAppDetailView.vue`, add:

```vue
<Alert variant="warning" role="note">
  <AlertDescription>
    <p class="font-medium">{{ t('admin.forwardAuth.trustTitle') }}</p>
    <ul class="mt-1 list-disc pl-4">
      <li>{{ t('admin.forwardAuth.trustIsolation') }}</li>
      <li>{{ t('admin.forwardAuth.trustHeaders') }}</li>
      <li>{{ t('admin.forwardAuth.trustStripAuth') }}</li>
    </ul>
  </AlertDescription>
</Alert>
```

(If `Alert` has no `warning` variant, check `StatusBadge`/`Alert` variants and use the caution-equivalent; do not invent a variant.)

- [ ] **Step 3: i18n** (both locales) — `admin.forwardAuth.scopesLabel`, `scopeName`, `scopeDescription`, `addScope`, and `trustTitle`/`trustIsolation`/`trustHeaders`/`trustStripAuth`. en e.g. `trustIsolation: 'The protected app must be reachable only through Traefik — a directly-reachable app lets a client forge the Remote-* identity headers.'`, `trustHeaders: 'Configure Traefik authResponseHeaders to forward all five Remote-* headers.'`, `trustStripAuth: 'Strip the inbound Authorization header in Traefik so a raw PAT never reaches the upstream.'`. Provide zh equivalents. Keep parity.

- [ ] **Step 4: Build, test, rebuild dist, commit.**

Run: `cd dashboard && npm run build && npm test`
```bash
cd /home/tundra/projects/tundra/prohibitorum
git add dashboard/src pkg/webui/dist
git commit -m "feat(pat): admin scope-vocabulary editor + forward-auth trust alerts"
```

---

### Task 7: Dashboard — admin account PATs card

**Goal:** The admin account-detail page lists and revokes the account's PATs.

**Files:**
- Modify: `dashboard/src/pages/admin/AdminAccountDetailView.vue`
- Modify: `dashboard/src/locales/{en,zh}.ts`

**Acceptance Criteria:**
- [ ] A "Personal access tokens" card on the account-detail page lists the account's PATs (`GET /accounts/{id}/tokens`) and revokes via `ConfirmDialog` → `POST /accounts/tokens/revoke {id}` → refetch.
- [ ] en/zh parity holds.

**Verify:** `cd dashboard && npm run build && npm test` → build clean + tests PASS.

**Steps:**

- [ ] **Step 1: Card.** Read the existing sessions card in `AdminAccountDetailView.vue` and mirror it: load `api.get('/api/prohibitorum/accounts/{id}/tokens')`, render name + token hint + per-app grant summary + expiry, a Revoke button → `ConfirmDialog` → `withSudo(() => api.post('/api/prohibitorum/accounts/tokens/revoke', { id }))` (the revoke is sudo-gated server-side; mirror how the per-session admin revoke wraps `withSudo`). Refetch on success.

- [ ] **Step 2: i18n** (both locales) — `admin.account.tokens.title`, `empty`, `revoke`, `revokeConfirmTitle`, `revokeConfirmBody`. Keep parity.

- [ ] **Step 3: Build, test, rebuild dist, commit.**

Run: `cd dashboard && npm run build && npm test`
```bash
cd /home/tundra/projects/tundra/prohibitorum
git add dashboard/src pkg/webui/dist
git commit -m "feat(pat): admin account-detail PATs card (list + revoke)"
```

---

### Task 8: Docs — per-app PAT model, vocabulary, admin oversight

**Goal:** Update the docs to the per-app model and the new surfaces.

**Files:**
- Modify: `docs/forward-auth.md`, `api.md`

**Acceptance Criteria:**
- [ ] `docs/forward-auth.md` describes the per-app scope model (vocabulary on the app; PAT grants per app; `Remote-Scopes` carries only that app's scopes) and keeps the verifier trust-boundary section.
- [ ] `api.md` updates `POST /me/tokens` body to `{ name, expiresInDays?, allApps, appGrants }`; documents `GET /me/forward-auth-apps`, the FA-app `scopes` field, and `GET /accounts/{id}/tokens` / `POST /accounts/tokens/revoke`.

**Verify:** `grep -n "appGrants" docs/forward-auth.md api.md` → matches; manual read for coherence.

**Steps:**

- [ ] **Step 1:** Update `docs/forward-auth.md`'s Personal Access Tokens section: scopes are now an admin-defined per-app vocabulary; a PAT grants per-app scopes; the gateway emits only the current app's scopes as `Remote-Scopes`; `all_apps` = identity-only. Keep the verbatim verifier trust-boundary note and the Traefik strip-Authorization requirement.

- [ ] **Step 2:** Update `api.md`: the create body, the new `/me/forward-auth-apps` row, the FA-app `scopes` field on create/update/get, and the two admin oversight routes (🔓 list, 🔐 revoke).

- [ ] **Step 3: Commit.**
```bash
git add docs/forward-auth.md api.md
git commit -m "docs(pat): per-app scope model + admin oversight + /me/forward-auth-apps"
```

---

### Task 9: Smoke + full gate + dist

**Goal:** Prove the per-app PAT path end-to-end and confirm the full gate is green with a committed bundle.

**USER-ORDERED GATE — NON-SKIPPABLE.** This task was requested by the user in the current conversation (project convention: every cycle ends with SMOKE_EXIT=0 + a committed dist). It MUST NOT be closed by walking around it, by declaring it "verified inline", or by substituting a cheaper check. Close only after every item in the acceptance criteria has been re-validated independently, with output captured.

**Files:**
- Modify: `cmd/smoke/main.go`
- Modify (generated): `pkg/webui/dist`

**Acceptance Criteria:**
- [ ] Smoke: admin sets a `scopes` vocabulary on the FA app; a PAT granting `{app:[scope]}` → verify against that app → 200 with `Remote-Scopes` = the scope; an `all_apps` PAT → 200 with empty `Remote-Scopes`; a PAT not granting an app → that app's verify → 403; admin `GET /accounts/{id}/tokens` lists it and `POST /accounts/tokens/revoke` revokes it (subsequent verify → 401).
- [ ] `mise run ci` exits 0 (go vet/build `-tags nodynamic`/test; dashboard install/test/build; no `pkg/webui/dist` drift).
- [ ] `mise run ci:smoke` exits 0.

**Verify:** `mise run ci && mise run ci:smoke` → both exit 0 (capture exit status + tails).

**Steps:**

- [ ] **Step 1:** Update the existing `pat` arc in `cmd/smoke/main.go` to the per-app shape (the current arc sends `upstreamScopes`/`allowedClientIds`, which no longer exist). After registering the FA app, set its vocabulary via the admin FA update (PUT `/api/prohibitorum/forward-auth-apps/{clientId}` with `scopes:[{name:"smoke:read"}]`). Create a PAT with `{name, expiresInDays:1, allApps:false, appGrants:{"smoke-fa":["smoke:read"]}}`; verify → 200 + `Remote-Scopes: smoke:read`. Create an `allApps:true` PAT; verify → 200 + empty `Remote-Scopes`. Verify the first PAT against the second FA app (`smoke-fa2`, not in its grants) → 403. Then admin `GET /accounts/1/tokens` (expect ≥1), admin `POST /accounts/tokens/revoke {id}`, and re-verify the revoked PAT → 401. Use the existing `step()`/`log.Fatalf`/sudo idioms.

- [ ] **Step 2:** `mise run ci:smoke` → exit 0; iterate until green.

- [ ] **Step 3:** `mise run prod:build` (refresh dist if drifted) then `mise run ci` → exit 0.

- [ ] **Step 4: Commit.**
```bash
git add cmd/smoke/main.go pkg/webui/dist
git commit -m "test(pat): per-app PAT smoke coverage + admin oversight; rebuild dist"
```

---

## Self-Review

- **Spec coverage:** B app-selection (Task 2 create + Task 5 picker); C vocabulary (Task 0 column, Task 3 admin, Task 5 picker); per-app isolation (Task 0 model, Task 1 gateway, Task 2 validation); F least-privilege (Task 2 validation, Task 5 submit gate); D oversight (Task 4 + Task 7); A trust alerts (Task 6); migration amend (Task 0); docs (Task 8); smoke/gate (Task 9). All spec sections map to a task.
- **Placeholder scan:** the only "confirm the real helper name" notes are Task 4 (`parseAccountID`/`getAccountIn`) and the `Alert` warning-variant check — both flagged as "use the real one, don't invent." All other code is concrete.
- **Type consistency:** `app_grants` is `map[string][]string` end-to-end (`appGrants` JSON); `db.PersonalAccessToken.AppGrants` / `db.InsertPATParams.AppGrants` are `[]byte` (jsonb), marshalled/unmarshalled at the handler and gateway; `forward_auth_scopes` is `[]byte` (jsonb) ↔ `[]contract.ForwardAuthScope` via `parseFAScopes` (defined once in `handle_me_tokens.go`, reused by `handle_admin_forward_auth_apps.go`); `RevokePATByID(id int32)` consistent across Task 0/4.
