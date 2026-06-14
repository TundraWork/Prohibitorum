# UX Improvements (avatar source choice + admin polish + a11y) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Five UX improvements — user-selectable avatar source (inherited/uploaded/none, retaining both blobs), IdP slug visibility, the disable toggle as its own section, an upstream-scope combobox, and a targeted accessibility sweep.

**Architecture:** Avatar storage moves from one blob/account to **one row per source** (`account_avatar` PK `(account_id, source)`) with `account.avatar_source` repurposed as a 4-state **active pointer** (`NULL`=never chosen, `'none'`, `'upstream'`, `'user'`); `account.avatar_etag`/`content_type` stay as a denormalized cache of the active row so the public-URL plumbing is unchanged. Frontend gets a source picker, a reusable scope combobox, and admin-view polish; an app-wide cursor/a11y pass closes out.

**Tech Stack:** Go 1.26, Huma v2 + chi, sqlc/pgx, goose migrations, Vue 3 + Vite + shadcn-vue (Reka UI) + Tailwind v4, vitest.

**Spec:** `docs/superpowers/specs/2026-06-14-ux-improvements-avatar-admin-a11y-design.md`

## Conventions (verified)

- **Migrations:** goose, `db/migrations/`. Apply `mise db:up` (needs `podman compose up -d`). `mise exec -- sqlc generate` regenerates `pkg/db`. Post-squash migrations start at 11; this is `013`.
- **Build/gate:** `CGO_ENABLED=0 go build -tags nodynamic ./... && go vet ./... && go test ./...`. FE: `cd dashboard && npm run test && npm run build` (`vue-tsc -b` is the real typecheck). Smoke runbook: `podman compose up -d`; build server `-tags nodynamic`; start with `PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true`; `mise exec -- go run ./cmd/smoke -base-url http://localhost:8080` → `SMOKE_EXIT=0`. (The avatar fetch permits `http` only under that flag.)
- **Stale diagnostics:** after `sqlc generate` / new files, IDE compiler diagnostics lag (`pkg/db` cache) — trust `go build`/`go test`.
- **Flaky test:** `pkg/server` `TestSudoComplete_PasswordTOTPSuccess` flakes ~1/3 under parallel runs — re-run `-count=1`; unrelated.
- **No `Co-Authored-By` trailer. Commit directly to master. Don't rebuild `pkg/webui/dist` until the done-gate (Task 10).**
- `account_avatar`/avatar handlers/inherit-job were just added by the federated-avatar feature (HEAD region `5be1ea6`..`a6a1a49`); this plan reworks them.

---

### Task 1: Avatar dual-source schema + queries

**Goal:** `account_avatar` keyed by `(account_id, source)` with per-row meta; `account.avatar_source` as the active pointer; source-aware sqlc queries, generated into `pkg/db`.

**Files:**
- Create: `db/migrations/013_avatar_dual_source.sql`
- Modify: `db/queries/account.sql`
- Regenerate: `pkg/db/*`

**Acceptance Criteria:**
- [ ] `mise db:up` applies `013` cleanly (fresh DB); `account_avatar` PK is `(account_id, source)` with `bytes`/`content_type`/`etag`; existing single blobs migrate into the row for their `avatar_source`.
- [ ] `mise exec -- sqlc generate` succeeds; `go build` passes with the new query methods; the old single-blob query methods are gone.

**Verify:** `podman compose up -d && mise db:up && mise exec -- sqlc generate && CGO_ENABLED=0 go build -tags nodynamic ./...` → (build fails only where Task 2/3/4 callers still reference removed methods — that's expected mid-task; this task's commit also updates those callers minimally to compile. See Step 4.)

**Steps:**

- [ ] **Step 1: Write `db/migrations/013_avatar_dual_source.sql`**
```sql
-- +goose Up
ALTER TABLE account_avatar ADD COLUMN IF NOT EXISTS source       text;
ALTER TABLE account_avatar ADD COLUMN IF NOT EXISTS content_type text;
ALTER TABLE account_avatar ADD COLUMN IF NOT EXISTS etag         text;
UPDATE account_avatar av SET
  source       = COALESCE((SELECT a.avatar_source       FROM account a WHERE a.id = av.account_id), 'user'),
  content_type = (SELECT a.avatar_content_type FROM account a WHERE a.id = av.account_id),
  etag         = (SELECT a.avatar_etag         FROM account a WHERE a.id = av.account_id)
WHERE source IS NULL;
ALTER TABLE account_avatar ALTER COLUMN source SET NOT NULL;
ALTER TABLE account_avatar DROP CONSTRAINT IF EXISTS account_avatar_pkey;
ALTER TABLE account_avatar ADD PRIMARY KEY (account_id, source);

-- +goose Down
DELETE FROM account_avatar a USING account_avatar b
  WHERE a.account_id = b.account_id AND a.source = 'upstream' AND b.source = 'user';
ALTER TABLE account_avatar DROP CONSTRAINT IF EXISTS account_avatar_pkey;
ALTER TABLE account_avatar ADD PRIMARY KEY (account_id);
ALTER TABLE account_avatar DROP COLUMN IF EXISTS etag;
ALTER TABLE account_avatar DROP COLUMN IF EXISTS content_type;
ALTER TABLE account_avatar DROP COLUMN IF EXISTS source;
```

- [ ] **Step 2: Rewrite the avatar queries in `db/queries/account.sql`.** Read the file; find the avatar block (`UpsertAccountAvatarBytes`, `SetAccountAvatarMetaUpstream`, `SetAccountAvatarMetaUser`, `ClearAccountAvatarBytes`, `ClearAccountAvatarMeta`, `GetAvatarBySubject`). DELETE those six and add:
```sql
-- name: UpsertAvatarSource :exec
INSERT INTO account_avatar (account_id, source, bytes, content_type, etag)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (account_id, source) DO UPDATE
  SET bytes = EXCLUDED.bytes, content_type = EXCLUDED.content_type, etag = EXCLUDED.etag;

-- name: SetActiveAvatar :exec
UPDATE account SET
  avatar_source       = $2,
  avatar_etag         = (SELECT etag         FROM account_avatar WHERE account_id = $1 AND source = $2),
  avatar_content_type = (SELECT content_type FROM account_avatar WHERE account_id = $1 AND source = $2),
  updated_at = now()
WHERE id = $1;

-- name: ClearActiveAvatar :exec
UPDATE account SET avatar_source = $2, avatar_etag = NULL, avatar_content_type = NULL, updated_at = now()
WHERE id = $1;

-- name: DeleteAvatarSource :exec
DELETE FROM account_avatar WHERE account_id = $1 AND source = $2;

-- name: GetActiveAvatarBySubject :one
SELECT av.bytes, av.content_type, av.etag, a.disabled
FROM account a JOIN account_avatar av ON av.account_id = a.id AND av.source = a.avatar_source
WHERE a.oidc_subject = $1;

-- name: GetAvatarSourceBySubject :one
SELECT av.bytes, av.content_type, av.etag, a.disabled
FROM account a JOIN account_avatar av ON av.account_id = a.id
WHERE a.oidc_subject = $1 AND av.source = $2;

-- name: ListAvatarSourcesByAccount :many
SELECT source, etag FROM account_avatar WHERE account_id = $1;
```

- [ ] **Step 3: Generate** — `podman compose up -d && mise db:up && mise exec -- sqlc generate`. New types: `UpsertAvatarSourceParams{AccountID, Source, Bytes, ContentType, Etag}`, `SetActiveAvatarParams{ID, AvatarSource}`, `ClearActiveAvatarParams{ID, AvatarSource}`, `DeleteAvatarSourceParams{AccountID, Source}`, `GetActiveAvatarBySubjectRow`, `GetAvatarSourceBySubjectParams`/`Row`, `ListAvatarSourcesByAccountRow{Source, Etag}`. (Confirm exact field names from generated code; `Source`/`AvatarSource` are `pgtype.Text` or `string` per sqlc — note them for Tasks 2-4.)

- [ ] **Step 4: Keep the build compiling.** Removing the six old methods breaks their callers (`pkg/server/handle_avatar.go`, `pkg/federation/oidc/federation.go`, the `avatarQueries`/`FederatorQueries` interfaces, and the avatar tests). To keep THIS commit's `go build` green, do the MINIMAL caller updates here is NOT required — instead, Tasks 2 & 3 own those rewrites. **Decision:** commit Task 1 as schema+queries+regen ONLY, and accept that `go build ./...` fails until Task 2/3 land. Verify Task 1 narrowly: `mise exec -- sqlc generate` succeeds and `go build ./pkg/db/` passes. (Tasks 2 and 3 immediately follow and restore a green full build; they are blockedBy Task 1.) Note this explicitly in the commit body.

- [ ] **Step 5: Commit**
```bash
git add db/migrations/013_avatar_dual_source.sql db/queries/account.sql pkg/db
git commit -m "feat(db): avatar dual-source schema (account_avatar per-source rows) + queries

Reworks the avatar storage to retain both upstream + uploaded blobs with an
active pointer. pkg/server + pkg/federation callers are updated in the next two
commits; full go build is green again after those."
```

---

### Task 2: Avatar handlers + URL helper (upload, selection, delete-fallback, public GET)

**Goal:** `avatar.SourceURL`; rework `handle_avatar.go` to the dual-source model + a new selection endpoint.

**Files:**
- Modify: `pkg/avatar/avatar.go` (+ `avatar_test.go`), `pkg/server/handle_avatar.go` (+ `handle_avatar_test.go`), `pkg/server/server.go`
- (depends on Task 1)

**Acceptance Criteria:**
- [ ] `avatar.SourceURL(subject, source, etag, origin)` → `<origin>/avatar/<subject>?source=<source>&v=<etag8>`; "" when etag empty.
- [ ] `PUT /me/avatar` writes the `user` source + activates it; `PUT /me/avatar/selection {source}` switches active (or `none`, or `400 avatar_source_unavailable`); `DELETE /me/avatar` removes the upload + falls back; `GET /avatar/{subject}` serves active or `?source=`.

**Verify:** `podman compose up -d && go test ./pkg/avatar/ ./pkg/server/ -run 'Avatar' -v && CGO_ENABLED=0 go build -tags nodynamic ./...`

**Steps:**

- [ ] **Step 1: `avatar.SourceURL` (TDD).** In `avatar_test.go` add:
```go
func TestSourceURL(t *testing.T) {
	got := SourceURL("11111111-2222-3333-4444-555555555555", "upstream", "deadbeefcafe", "https://x")
	want := "https://x/avatar/11111111-2222-3333-4444-555555555555?source=upstream&v=deadbeef"
	if got != want { t.Fatalf("SourceURL=%q want %q", got, want) }
	if SourceURL("s", "user", "", "https://x") != "" { t.Fatal("empty etag -> empty") }
}
```
Implement in `avatar.go` (mirror the existing `PublicURL`):
```go
// SourceURL builds the cache-busting avatar URL for a SPECIFIC source, or "" when no etag.
func SourceURL(subject, source, etag, origin string) string {
	if subject == "" || etag == "" { return "" }
	v := etag
	if len(v) > 8 { v = v[:8] }
	return origin + "/avatar/" + subject + "?source=" + source + "&v=" + v
}
```

- [ ] **Step 2: Rewrite `handle_avatar.go` (TDD — write the handler tests first in `handle_avatar_test.go`, adapting the existing fake).** Read the current `handle_avatar_test.go` to see how `fakeAvatarQueries` + the test server are built; rework the fake to the new interface (below) recording: per-source upserts, active selection, deletes; serving active + by-source. Test cases:
  1. `PUT /me/avatar` (valid PNG) → upsert source=`user` + active set to `user`.
  2. `PUT /me/avatar/selection {source:"upstream"}` when an `upstream` row exists → active=`upstream`; when it does NOT exist → `400 avatar_source_unavailable`.
  3. `PUT /me/avatar/selection {source:"none"}` → active cleared to `none`.
  4. `DELETE /me/avatar` when active was `user` and an `upstream` row exists → user row deleted + active=`upstream`; when no upstream row → active=`none`.
  5. `GET /avatar/{subject}` serves the active source; `?source=upstream` serves that row; `404` for none/unknown/disabled.

- [ ] **Step 3: Run → fail.** `go test ./pkg/server/ -run Avatar -v` → FAIL.

- [ ] **Step 4: Implement.** New `avatarQueries` interface + handlers:
```go
type avatarQueries interface {
	UpsertAvatarSource(ctx context.Context, arg db.UpsertAvatarSourceParams) error
	SetActiveAvatar(ctx context.Context, arg db.SetActiveAvatarParams) error
	ClearActiveAvatar(ctx context.Context, arg db.ClearActiveAvatarParams) error
	DeleteAvatarSource(ctx context.Context, arg db.DeleteAvatarSourceParams) error
	GetActiveAvatarBySubject(ctx context.Context, oidcSubject pgtype.UUID) (db.GetActiveAvatarBySubjectRow, error)
	GetAvatarSourceBySubject(ctx context.Context, arg db.GetAvatarSourceBySubjectParams) (db.GetAvatarSourceBySubjectRow, error)
	ListAvatarSourcesByAccount(ctx context.Context, accountID int32) ([]db.ListAvatarSourcesByAccountRow, error)
}
```
- `handlePutAvatarHTTP`: after `avatar.Process`, in the tx (and the `dbPool==nil` seam), `UpsertAvatarSource{AccountID, Source:"user", Bytes:out, ContentType:"image/webp", Etag:etag}` then `SetActiveAvatar{ID, AvatarSource:"user"}`. Refresh `sess.Account.AvatarContentType/AvatarEtag` (active = the upload) + set `sess.Account.AvatarSource` if that field is on the struct.
- **NEW** `handlePutAvatarSelectionHTTP`: decode `{source string}`. If `source == "none"` → `ClearActiveAvatar{ID, AvatarSource:"none"}`. Else (`"upstream"`/`"user"`): verify the row exists (`ListAvatarSourcesByAccount` contains it) → if not, `writeAvatarErr(w,"avatar_source_unavailable","...")`; else `SetActiveAvatar{ID, AvatarSource:source}`. Reject other values with `avatar_source_unavailable`. Refresh session meta (re-read or set from the chosen row). `204`.
- `handleDeleteAvatarHTTP`: `DeleteAvatarSource{AccountID, Source:"user"}`. Then if the prior active was `"user"`: if an `upstream` row exists → `SetActiveAvatar{...,"upstream"}`, else `ClearActiveAvatar{...,"none"}`. (Determine prior active from `sess.Account.AvatarSource` or a read.) Refresh session meta. `204`.
- `handleGetAvatarHTTP`: if `r.URL.Query().Get("source")` is non-empty → `GetAvatarSourceBySubject{OidcSubject, Source:src}`; else `GetActiveAvatarBySubject`. Both rows have `Bytes/ContentType/Etag/Disabled` — keep the existing 404/etag/304/cache logic (note the field is now `row.Etag`/`row.ContentType`, not `row.AvatarEtag`).
- Keep the `dbPool==nil` seam + tx pattern for the mutating handlers.

- [ ] **Step 5: Route** (`server.go`, next to the avatar routes ~line 379):
```go
registerOpHTTP(s.router, "PUT", "/api/prohibitorum/me/avatar/selection", sessionReq, s.handlePutAvatarSelectionHTTP)
```

- [ ] **Step 6: Run → pass + build.** `go test ./pkg/avatar/ ./pkg/server/ -run Avatar -v && CGO_ENABLED=0 go build -tags nodynamic ./...` (build still fails on `pkg/federation` until Task 3 — that's fine; verify `pkg/server`+`pkg/avatar` compile: `go build ./pkg/server/ ./pkg/avatar/`).

- [ ] **Step 7: Commit**
```bash
git add pkg/avatar pkg/server/handle_avatar.go pkg/server/handle_avatar_test.go pkg/server/server.go
git commit -m "feat(server): dual-source avatar handlers (upload/selection/delete-fallback/?source GET) + SourceURL"
```

---

### Task 3: Inherit-job rework (retain both, sticky active)

**Goal:** `runAvatarInherit` upserts the `upstream` source and only auto-activates when the active selection is `NULL` or already `upstream` (never overrides `none`/`user`); full build green again.

**Files:**
- Modify: `pkg/federation/oidc/federation.go` (`runAvatarInherit`, `FederatorQueries`), `pkg/federation/oidc/federation_test.go`

**Acceptance Criteria:**
- [ ] Fresh account (`avatar_source IS NULL`) → upstream row stored + active=`upstream`. Account with active `user`/`none` → upstream row stored, active **unchanged**. Active already `upstream` → row + cache refreshed. Etag unchanged → no-op.
- [ ] `CGO_ENABLED=0 go build -tags nodynamic ./...` is GREEN (all callers migrated).

**Verify:** `go test ./pkg/federation/oidc/ -run AvatarInherit -v && CGO_ENABLED=0 go build -tags nodynamic ./... && go test ./pkg/server/`

**Steps:**

- [ ] **Step 1: Update `FederatorQueries`** — replace `UpsertAccountAvatarBytes`/`SetAccountAvatarMetaUpstream` with:
```go
	UpsertAvatarSource(ctx context.Context, arg db.UpsertAvatarSourceParams) error
	SetActiveAvatar(ctx context.Context, arg db.SetActiveAvatarParams) error
```
(`GetAccountByID` already present.)

- [ ] **Step 2: Rework the tests** (`federation_test.go` `TestAvatarInherit_*` + the `fakeFederatorQueries`). Update the fake's recorder to the new methods. Cases: fresh (NULL active) → upstream upsert + SetActiveAvatar("upstream"); active=`user` → upsert only, NO SetActiveAvatar; active=`upstream` → upsert + SetActiveAvatar; unchanged etag → neither.

- [ ] **Step 3: Run → fail.** `go test ./pkg/federation/oidc/ -run AvatarInherit -v` → FAIL.

- [ ] **Step 4: Implement** in `runAvatarInherit` (replace the store + source-guard block). Read the current account avatar state via `GetAccountByID`; let `active := acct.AvatarSource` (pgtype.Text). Compute `currentUpstreamEtag` (need the upstream row's etag — add to the read, OR read it: simplest, compare against the stored active etag only when active=="upstream"; to skip on unchanged use a `ListAvatarSourcesByAccount`-style read OR just always upsert — but the spec wants etag-skip). Pragmatic: after `avatar.Process` yields `etag`, fetch sources via a small read; if the `upstream` row etag == new etag, return (no-op). Else:
```go
	if err := f.q.UpsertAvatarSource(ctx, db.UpsertAvatarSourceParams{
		AccountID: accountID, Source: "upstream",
		Bytes: out, ContentType: pgtype.Text{String: "image/webp", Valid: true}, Etag: pgtype.Text{String: etag, Valid: true},
	}); err != nil { /* log + return */ }
	// Activate upstream only if the user hasn't chosen otherwise: NULL (never chosen)
	// or already 'upstream' (refresh the cache). Never override 'none' or 'user'.
	if !acct.AvatarSource.Valid || acct.AvatarSource.String == "upstream" {
		_ = f.q.SetActiveAvatar(ctx, db.SetActiveAvatarParams{ID: accountID, AvatarSource: pgtype.Text{String: "upstream", Valid: true}})
	}
```
> The old `avatar_source == "user"` early skip-guard is REMOVED (both blobs coexist now). For the etag-skip read, add `ListAvatarSourcesByAccount` to `FederatorQueries` if needed, OR re-use `GetAccountByID` + accept always-upsert (idempotent). Choose the simplest that satisfies the no-op-on-unchanged test; if you add `ListAvatarSourcesByAccount` to the interface, update the fakes.
> NOTE: `db.SetActiveAvatarParams.AvatarSource` type — match the generated type (`pgtype.Text` vs `string`); adjust the literals accordingly.

- [ ] **Step 5: Run → pass + FULL build + server tests.** `go test ./pkg/federation/oidc/ -run AvatarInherit -v && CGO_ENABLED=0 go build -tags nodynamic ./... && go vet ./... && go test ./pkg/federation/oidc/ ./pkg/server/` (server suite: re-run if only the flaky sudo test fails).

- [ ] **Step 6: Commit**
```bash
git add pkg/federation/oidc/federation.go pkg/federation/oidc/federation_test.go
git commit -m "feat(federation): inherit job retains both avatars; activates upstream only when unchosen/already-upstream"
```

---

### Task 4: `/me` reports avatar source + previews

**Goal:** `SessionView` carries the active source + per-source preview URLs so the picker can render.

**Files:**
- Modify: `pkg/contract/auth.go` (`SessionView`), `pkg/server/handle_auth.go` (`sessionView`)
- Test: `pkg/server/handle_me_test.go`
- (depends on Task 1 for `ListAvatarSourcesByAccount`, Task 2 for `avatar.SourceURL`)

**Acceptance Criteria:**
- [ ] `/me` includes `avatarSource` (active: `upstream`/`user`/`none`, omitted when NULL) and `avatarSourceUrls` (`{source: url}` for each existing row).

**Verify:** `go test ./pkg/server/ -run 'Me|Avatar' -v && CGO_ENABLED=0 go build -tags nodynamic ./...`

**Steps:**

- [ ] **Step 1: Contract** (`pkg/contract/auth.go` `SessionView`, after `AvatarPending`):
```go
	AvatarSource     *string           `json:"avatarSource,omitempty"`
	AvatarSourceUrls map[string]string `json:"avatarSourceUrls,omitempty"`
```

- [ ] **Step 2: Populate in `sessionView`** (`handle_auth.go:41`, `func (s *Server) sessionView(a *db.Account) contract.SessionView`). After building `v` (which already sets `AvatarURL` from `avatar.AccountURL`): if `a.AvatarSource.Valid`, set `v.AvatarSource = &a.AvatarSource.String`. Build per-source URLs:
```go
	if len(s.config.PublicOrigins) > 0 {
		origin := s.config.PublicOrigins[0]
		if rows, err := s.avatarQ().ListAvatarSourcesByAccount(context.Background(), a.ID); err == nil {
			urls := make(map[string]string, len(rows))
			for _, rrow := range rows {
				if u := avatar.SourceURL(a.OidcSubject.String(), rrow.Source, rrow.Etag.String, origin); u != "" {
					urls[rrow.Source] = u
				}
			}
			if len(urls) > 0 { v.AvatarSourceUrls = urls }
		}
	}
```
> `sessionView` takes no ctx; use `context.Background()` for this read (it's a cheap PK lookup; mirror the existing avatar-meta reads). `rrow.Source`/`rrow.Etag` per the generated `ListAvatarSourcesByAccountRow` (adjust if `Etag` is `pgtype.Text`). Add `avatar`/`context` imports if missing. `s.avatarQ()` is defined in handle_avatar.go (same package).

- [ ] **Step 3: Test** (`handle_me_test.go`): with an account that has both `upstream` + `user` rows and active `user`, `/me` returns `avatarSource:"user"` + `avatarSourceUrls` with both keys. (Use the avatar test harness / fake that serves `ListAvatarSourcesByAccount`.)

- [ ] **Step 4: Run + commit**
```bash
go test ./pkg/server/ -run 'Me|Avatar' -v && CGO_ENABLED=0 go build -tags nodynamic ./...
git add pkg/contract/auth.go pkg/server/handle_auth.go pkg/server/handle_me_test.go
git commit -m "feat(server): /me reports active avatarSource + per-source preview URLs"
```

---

### Task 5: Frontend — avatar source picker

**Goal:** `EditProfileDialog` lets the user pick Inherited / Uploaded / None, upload, and remove-upload.

**Files:**
- Modify: `dashboard/src/components/custom/EditProfileDialog.vue` (+ test), `dashboard/src/stores/auth.ts`, `dashboard/src/locales/en.ts`
- (depends on Task 2 selection endpoint, Task 4 SessionView fields)

**Acceptance Criteria:**
- [ ] The dialog shows a selectable option per available source (preview) + always "None"; the active one is highlighted; selecting calls `PUT /me/avatar/selection` then reloads; Upload (crop flow) and Remove-upload work.

**Verify:** `cd dashboard && npx vitest run src/components/custom/EditProfileDialog.test.ts && npx vue-tsc -b`

**Steps:**

- [ ] **Step 1: Store types** (`stores/auth.ts` `SessionView`): add `avatarSource?: string` + `avatarSourceUrls?: Record<string, string>`.

- [ ] **Step 2: i18n** (`en.ts`, in `accountMenu`): `avatarSourceLabel: 'Avatar'`, `avatarInherited: 'Inherited'`, `avatarUploaded: 'Uploaded'`, `avatarNone: 'No avatar'`, `avatarSourceHint: 'Choose which picture to show.'`. No apostrophes; `grep -nP "[\x{2018}\x{2019}]" src/locales/en.ts || echo clean` after.

- [ ] **Step 3: Test-first** (`EditProfileDialog.test.ts`): mock `api`; seed `auth.me.avatarSourceUrls = {upstream:'/avatar/x?source=upstream&v=a', user:'/avatar/x?source=user&v=b'}`, `auth.me.avatarSource='user'`. Assert: an "Inherited" option + an "Uploaded" option + a "None" option render; clicking "Inherited" calls `api.put('/api/prohibitorum/me/avatar/selection',{source:'upstream'})` then `auth.reload()`; "None" → `{source:'none'}`. Keep the existing upload/remove tests (upload still calls `api.upload` then reload; remove still calls `api.del`).

- [ ] **Step 4: Implement.** Replace the single-avatar block with a picker. For each source present in `auth.me?.avatarSourceUrls` render a selectable card (radio semantics) using `UserAvatar :src` for the preview, labeled Inherited/Uploaded; always render a "None" card. Mark the active (`auth.me?.avatarSource`) selected. Handler:
```ts
async function selectSource(source: 'upstream' | 'user' | 'none'): Promise<void> {
  const ok = await run(() => api.put('/api/prohibitorum/me/avatar/selection', { source }))
  if (!error.value) await auth.reload()
}
```
Keep the existing Upload button (crop → `PUT /me/avatar`) and show Remove-upload only when `auth.me?.avatarSourceUrls?.user` exists (`DELETE /me/avatar`). After upload/remove, `auth.reload()` refreshes sources + active.
> Use the project's selectable-card idiom (mirror `RadioCardGroup` if it fits, or labeled `Checkbox`/radio rows) — keep it accessible (the a11y task will sweep it, but render real radios/labels here).

- [ ] **Step 5: Run + commit**
```bash
cd dashboard && npx vitest run src/components/custom/EditProfileDialog.test.ts && npx vue-tsc -b
cd /home/tundra/projects/tundra/prohibitorum
git add dashboard/src/components/custom/EditProfileDialog.vue dashboard/src/components/custom/EditProfileDialog.test.ts dashboard/src/stores/auth.ts dashboard/src/locales/en.ts
git commit -m "feat(web): avatar source picker (inherited / uploaded / none) in Edit profile"
```

---

### Task 6: Frontend — IdP slug (list column + detail display)

**Goal:** Slug as its own list column + a read-only slug on the detail page.

**Files:**
- Modify: `dashboard/src/pages/admin/AdminUpstreamIdpsView.vue` (+ test), `dashboard/src/pages/admin/AdminUpstreamIdpDetailView.vue` (+ test), `dashboard/src/locales/en.ts`

**Acceptance Criteria:**
- [ ] The list has a dedicated Slug column (Name · Slug · Mode · State); the detail page shows the slug (read-only).

**Verify:** `cd dashboard && npx vitest run src/pages/admin/AdminUpstreamIdp && npx vue-tsc -b`

**Steps:**

- [ ] **Step 1: i18n** (`en.ts`): add `admin.upstream.colSlug: 'Slug'` (near `colName`/`colMode`/`colState`). Apostrophe grep after.
- [ ] **Step 2: List** (`AdminUpstreamIdpsView.vue`): add `<TableHead>{{ t('admin.upstream.colSlug') }}</TableHead>` after the Name head; in the row, remove the slug sub-line from the Name cell (keep just `displayName`) and add a new `<TableCell><span class="font-mono text-sm text-muted">{{ i.slug }}</span></TableCell>` after it. Update the list test's expected columns/cells.
- [ ] **Step 3: Detail** (`AdminUpstreamIdpDetailView.vue`): near the top of the edit card, add a read-only slug row:
```vue
<div class="flex flex-col gap-1.5">
  <Label>{{ t('admin.upstream.slug') }}</Label>
  <p class="font-mono text-sm text-ink" data-test="idp-slug">{{ slug }}</p>
</div>
```
(`slug` is already `String(route.params.slug)`.) Add a detail-test assertion that the slug renders.
- [ ] **Step 4: Run + commit**
```bash
cd dashboard && npx vitest run src/pages/admin/AdminUpstreamIdp && npx vue-tsc -b
cd /home/tundra/projects/tundra/prohibitorum
git add dashboard/src/pages/admin/AdminUpstreamIdpsView.vue dashboard/src/pages/admin/AdminUpstreamIdpsView.test.ts dashboard/src/pages/admin/AdminUpstreamIdpDetailView.vue dashboard/src/pages/admin/AdminUpstreamIdpDetailView.test.ts dashboard/src/locales/en.ts
git commit -m "feat(admin-web): IdP slug as a list column + read-only detail display"
```

---

### Task 7: Frontend — disable toggle as its own section

**Goal:** Lift the `disabled` Switch into a dedicated "Status" section in the IdP / OIDC / SAML detail views.

**Files:**
- Modify: `dashboard/src/pages/admin/AdminUpstreamIdpDetailView.vue`, `AdminOidcClientDetailView.vue`, `AdminSamlProviderDetailView.vue` (+ their tests as needed), `dashboard/src/locales/en.ts`

**Acceptance Criteria:**
- [ ] In all three detail views the `disabled` Switch sits in its own titled section, separate from the functional settings; the save payload is unchanged.

**Verify:** `cd dashboard && npx vitest run src/pages/admin && npx vue-tsc -b`

**Steps:**

- [ ] **Step 1: i18n** (`en.ts`): add `statusSection: 'Status'` + `statusSectionDesc: 'Control whether this is available for sign-in.'` under each of `admin.upstream`, `admin.oidc`, `admin.saml` (reuse a shared key if the views share an i18n namespace — check; otherwise per-namespace). Apostrophe grep after.
- [ ] **Step 2: IdP detail** (`AdminUpstreamIdpDetailView.vue`): move the existing `<SettingRow ... for="disabled"><Switch id="disabled" .../></SettingRow>` out of the group with `requireVerifiedEmail` into its own `<FormSection :title="t('admin.upstream.statusSection')">` block (place it as a distinct section, e.g. just above the danger/secret sections).
- [ ] **Step 3: OIDC detail** (`AdminOidcClientDetailView.vue`): same — move the `disabled` SettingRow out of the `requireConsent` group into its own `FormSection`. (Grep `disabled`/`requireConsent` to find it.)
- [ ] **Step 4: SAML detail** (`AdminSamlProviderDetailView.vue`): grep `disabled`; move its toggle into its own `FormSection`. (If SAML has no disabled toggle, note it and skip — verify against the view.)
- [ ] **Step 5: Tests** — if any view test asserts the disabled toggle's location/grouping, update it; otherwise ensure the existing save-payload tests still pass (the toggle still binds the same ref). 
- [ ] **Step 6: Run + commit**
```bash
cd dashboard && npx vitest run src/pages/admin && npx vue-tsc -b
cd /home/tundra/projects/tundra/prohibitorum
git add dashboard/src/pages/admin/AdminUpstreamIdpDetailView.vue dashboard/src/pages/admin/AdminOidcClientDetailView.vue dashboard/src/pages/admin/AdminSamlProviderDetailView.vue dashboard/src/locales/en.ts
git commit -m "feat(admin-web): disable toggle as its own Status section (IdP / OIDC / SAML detail)"
```

---

### Task 8: Frontend — `ComboboxTokenInput` + upstream scopes

**Goal:** A reusable scope combobox (suggestions-with-descriptions + custom entry) wired into the upstream IdP scopes.

**Files:**
- Create: `dashboard/src/components/custom/ComboboxTokenInput.vue` (+ test)
- Modify: `dashboard/src/pages/admin/AdminUpstreamIdpsView.vue`, `AdminUpstreamIdpDetailView.vue`, `dashboard/src/locales/en.ts`

**Acceptance Criteria:**
- [ ] `ComboboxTokenInput` renders chips for `modelValue: string[]`, a dropdown of `suggestions: {value,description}[]`, and accepts a typed custom value; remove-chip works; emits `update:modelValue`.
- [ ] Upstream IdP scopes (create + detail) use it; the submitted `scopes` payload is unchanged in shape (`string[]`).

**Verify:** `cd dashboard && npx vitest run src/components/custom/ComboboxTokenInput.test.ts src/pages/admin/AdminUpstreamIdp && npx vue-tsc -b`

**Steps:**

- [ ] **Step 1: Test-first** (`ComboboxTokenInput.test.ts`): mount with `modelValue:['openid']`, `suggestions:[{value:'profile',description:'Name, picture'},{value:'email',description:'Email address'}]`, `allowCustom`. Assert: a chip for `openid` renders; selecting `profile` from the dropdown emits `update:modelValue` `['openid','profile']`; typing `custom_scope` + Enter emits `[...,'custom_scope']`; removing a chip emits the list without it. Use Reka idioms (the existing TagInput/Combobox test idioms — see `reference_reka_primitive_idioms`).
- [ ] **Step 2: Run → fail.**
- [ ] **Step 3: Implement `ComboboxTokenInput.vue`.** Props `{ modelValue: string[]; suggestions: {value:string;description:string}[]; placeholder?: string; ariaLabel?: string; allowCustom?: boolean }`. Render selected values as removable chips (each remove button has an `aria-label`). A text input with a popover/listbox of suggestions filtered by the typed text, each row showing `value` + muted `description`; Enter or click adds (suggestion value, or the raw typed text when `allowCustom` and no exact suggestion). Keyboard: ↑/↓ to move, Enter to add, Escape to close, Backspace on empty to remove last. Build on the vendored Reka `Combobox`/`Popover`/`Listbox` primitives if present, else a `Popover` + a roving-focus list. Emit `update:modelValue` (new array) on every change. Ensure `focus-visible` + `cursor-pointer` on interactive bits (the a11y task will re-verify).
- [ ] **Step 4: Suggestions const + i18n** — define `COMMON_UPSTREAM_SCOPES` (in the component file or a small `lib/scopes.ts`): `openid`, `profile`, `email`, `offline_access`, `address`, `phone`, each with a description from `en.ts` (`admin.upstream.scopeDesc.openid` etc.). Apostrophe grep after en.ts edit.
- [ ] **Step 5: Wire upstream scopes.** In `AdminUpstreamIdpsView.vue` (create) + `AdminUpstreamIdpDetailView.vue` (edit), replace the `<TagInput ... v-model="scopes" ...>` with `<ComboboxTokenInput v-model="scopes" :suggestions="COMMON_UPSTREAM_SCOPES" :aria-label="t('admin.upstream.scopes')" allow-custom />`. Keep the surrounding Label + description. Update the views' tests if they asserted TagInput specifics; the submitted `scopes` shape is unchanged.
- [ ] **Step 6: Run → pass + commit**
```bash
cd dashboard && npx vitest run src/components/custom/ComboboxTokenInput.test.ts src/pages/admin/AdminUpstreamIdp && npx vue-tsc -b
cd /home/tundra/projects/tundra/prohibitorum
git add dashboard/src/components/custom/ComboboxTokenInput.vue dashboard/src/components/custom/ComboboxTokenInput.test.ts dashboard/src/pages/admin/AdminUpstreamIdpsView.vue dashboard/src/pages/admin/AdminUpstreamIdpDetailView.vue dashboard/src/locales/en.ts dashboard/src/lib/scopes.ts
git commit -m "feat(admin-web): upstream scopes combobox (common scopes + descriptions + custom)"
```

---

### Task 9: Frontend — accessibility sweep (cursors + labels + focus + semantics)

**Goal:** `cursor-pointer` on all interactive primitives + aria-labels on icon-only buttons + focus/semantics audit.

**Files:**
- Modify: `dashboard/src/components/ui/button/index.ts` (+ other `ui/` primitives), `dashboard/src/components/custom/*` (icon-only buttons), and any clickable-div/row found in the sweep
- Test: a small assertion that the Button cva includes `cursor-pointer`

**Acceptance Criteria:**
- [ ] `Button` cva includes `cursor-pointer`; Switch/Checkbox/Select-trigger/Tabs-trigger/dropdown-items/clickable-rows/RadioCard/Segmented/ComboboxTokenInput all show a pointer cursor when enabled; icon-only buttons have aria-labels; no interactive element lacks a focus-visible ring.

**Verify:** `cd dashboard && grep -rn "cursor-pointer" src/components/ui/button/index.ts && npx vitest run && npx vue-tsc -b`

**Steps:**

- [ ] **Step 1: Button cva** (`components/ui/button/index.ts`): add `cursor-pointer` to the base cva string (after `inline-flex items-center ...`). `disabled:pointer-events-none` already suppresses the cursor on disabled.
- [ ] **Step 2: Sweep interactive primitives.** Grep each vendored primitive's root class for a `cursor`; add `cursor-pointer` where the element is clickable and lacks it: `components/ui/switch`, `checkbox`, `select` (trigger), `tabs` (trigger), `dropdown-menu` (item), plus `components/custom/RadioCardGroup`, `SegmentedControl`, `ComboboxTokenInput`, and any `class="cursor-pointer"`-missing clickable `<TableRow>`/`<div @click>` across `pages/` (grep `@click` / `@keydown.enter` on non-button elements). Add `disabled:cursor-not-allowed` where an element renders disabled without `pointer-events-none`.
- [ ] **Step 3: Icon-only buttons** — grep for `<Button` / `<button` containing only an icon (`lucide`) with no text and no `aria-label`; add an `aria-label` (i18n where user-facing). Common spots: chip-remove ×, copy buttons (`CodeField`), the sidebar rail toggle, dialog close. Add labels.
- [ ] **Step 4: Focus + semantics audit** — verify every interactive element resolves a `focus-visible` ring (the Button already does; check Switch/Checkbox/Select/Tabs/inputs/combobox). Verify clickable `<div>`s that act as buttons have `role`/`tabindex`/keydown (or convert to `<button>`); verify each `Dialog` has a `DialogTitle` (+ `DialogDescription`) and each `Alert` a `role`/`aria-live`. Fix gaps found.
- [ ] **Step 5: Regression test** — add a tiny test (e.g. in an existing `ui` test or a new `button.test.ts`) asserting `buttonVariants()` output contains `cursor-pointer`.
- [ ] **Step 6: Run + commit**
```bash
cd dashboard && npx vitest run && npx vue-tsc -b
cd /home/tundra/projects/tundra/prohibitorum
git add dashboard/src/components dashboard/src/pages dashboard/src/locales/en.ts
git commit -m "fix(web): a11y sweep — cursor-pointer on interactive elements, aria-labels on icon buttons, focus/semantics"
```

---

### Task 10: Smoke update (dual-source) + done-gate + dist

**Goal:** Update the avatar smoke to the dual-source model; full gate green; rebuild + commit dist.

**Files:**
- Modify: `cmd/smoke/main.go`, `pkg/webui/dist/**`

**Acceptance Criteria:**
- [ ] Smoke covers: upload (user) + federated inherit (upstream) coexisting; switching active to upstream/user/none and asserting `GET /avatar/{subject}` serves the selected one + `?source=` previews. `SMOKE_EXIT=0`.
- [ ] Full gate green; `dist` rebuilt + committed.

**Verify:** see steps.

**Steps:**

- [ ] **Step 1: Update the avatar smoke block** in `cmd/smoke/main.go`. The existing `avatar 1-4` + `avatar-fed 1-3` steps assume one blob. Rework:
  - Upload an avatar (user source) for a federated user who also inherited (upstream source) → assert BOTH rows coexist (GET `?source=upstream` 200 + `?source=user` 200).
  - `PUT /me/avatar/selection {source:"upstream"}` → assert `GET /avatar/{sub}` (active) now equals the upstream image; `{source:"user"}` → equals the upload; `{source:"none"}` → `GET /avatar/{sub}` → 404.
  - Replace the old "no-clobber" assertion (semantics gone) with "both rows persist; active follows the last selection".
  - Use the smoke's `step(...)`/`log.Fatalf` idiom + existing DB helpers (add `getAvatarSourceEtag(accountID, source)` next to the existing ones if needed).
- [ ] **Step 2: Frontend gate + rebuild dist** — `cd dashboard && npm run test && npm run build` (vitest green, `vue-tsc -b` 0, writes `../pkg/webui/dist`).
- [ ] **Step 3: Go gate** — `cd /home/tundra/projects/tundra/prohibitorum && CGO_ENABLED=0 go build -tags nodynamic ./... && go vet ./... && go test ./...` (0; re-run if only the flaky sudo test fails).
- [ ] **Step 4: Smoke** — runbook: `podman compose down -v && podman compose up -d`; build server `-tags nodynamic`; start with `PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true` (detached); `mise exec -- go run ./cmd/smoke -base-url http://localhost:8080` → `SMOKE_EXIT=0`; kill the server by PID.
- [ ] **Step 5: Live check** (`mise dev-server`; a seeded upstream IdP that serves `picture`): federated login → inherit → open Edit profile → switch Inherited/Uploaded/None → the sidebar avatar updates accordingly; admin: IdP list shows a Slug column, detail shows the slug, disable is its own section, upstream scopes are a combobox; buttons show a pointer cursor.
- [ ] **Step 6: Commit dist + smoke**
```bash
git add pkg/webui/dist cmd/smoke/main.go
git commit -m "test(smoke): dual-source avatar round-trip (coexist + selection); rebuild dist"
```

---

## Self-Review

**Spec coverage:** §1 avatar dual-source → T1 (schema/queries), T2 (handlers/SourceURL), T3 (inherit job), T4 (/me source+previews), T5 (FE picker); §2 IdP slug → T6; §3 disable section → T7; §4 upstream scopes combobox → T8; §5 a11y → T9; testing+smoke+done-gate → T10. All spec sections mapped.

**Placeholder scan:** code shown for the novel/backend parts; FE polish tasks give exact diffs + the component contract. The flagged adaptation points (generated sqlc field types `Source`/`AvatarSource` `pgtype.Text` vs `string`; the existing test-fake shapes; whether SAML has a disabled toggle; whether `RadioCardGroup`/Reka `Combobox` primitives fit) are real integration seams each step names — not blanks.

**Type consistency:** queries `UpsertAvatarSource`/`SetActiveAvatar`/`ClearActiveAvatar`/`DeleteAvatarSource`/`GetActiveAvatarBySubject`/`GetAvatarSourceBySubject`/`ListAvatarSourcesByAccount` (T1) used consistently in T2/T3/T4; `avatar.SourceURL` (T2) used in T4; `SessionView.avatarSource`/`avatarSourceUrls` (T4) consumed in T5; `ComboboxTokenInput` props (T8) consistent; `avatar_source` 4-state (`NULL`/`none`/`upstream`/`user`) consistent across T1-T5 + the inherit rule in T3.

**Build-sequencing note:** Task 1 intentionally leaves `go build ./...` red (only `pkg/db` green) until Tasks 2+3 migrate the callers — called out in T1 Step 4 + the commit body. T2 verifies `pkg/server`+`pkg/avatar`; T3 restores the full green build.

## Review follow-ups (tracked during execution)

- **[from Task 2 review → do in Task 5]** Add `avatar_source_unavailable` to `dashboard/src/locales/en.ts` `errors.*` (next to `avatar_too_large`/`avatar_invalid_image`) so the picker can show a readable error when switching to a missing source.
- **[from Task 2 → MUST do in Task 3]** Task 2 left `pkg/federation/oidc/federation.go` `runAvatarInherit` as a COMPILE-STUB preserving the OLD single-blob semantics (it still early-returns when `avatar_source=='user'` and otherwise unconditionally activates upstream). Task 3 must REWORK it to the dual-source rule: always upsert the `upstream` row (so it's available to pick later — remove the user-skip early return), and `SetActiveAvatar('upstream')` ONLY when `avatar_source` is `NULL` or already `'upstream'` (never override `'none'`/`'user'`), keeping the etag-skip; and rewrite `federation_test.go`'s AvatarInherit tests (currently made-to-compile asserting old behavior) to the new rule.
- **[from Task 7 review → consider in Task 9 UX/a11y sweep]** In the upstream-IdP and OIDC-client detail views the `disabled` toggle now lives in its own "Status" card, but the single Save button stays in the Config card above it. This can read as a self-saving toggle (common settings-UI pattern) when it is not. During the Task 9 sweep, decide whether to add a short helper hint (e.g. "saved with the form above") near the Status toggle, or otherwise clarify that it persists via the existing Save. Do NOT add a second Save button or change the PUT payload.

## Done-gate

`CGO_ENABLED=0 go build -tags nodynamic ./...` / `go vet` / `go test ./...` (0), `vitest` (green), `vue-tsc -b` (0), smoke `SMOKE_EXIT=0` (incl. the dual-source avatar round-trip), rebuild + commit `pkg/webui/dist`.
