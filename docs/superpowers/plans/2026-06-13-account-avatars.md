# Account Avatars Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Per-account avatars — uploaded, normalized to a 512×512 WebP stored in Postgres, served from a public cache-friendly endpoint, shown across the dashboard, and exposed downstream as the OIDC `picture` claim and a mappable SAML attribute.

**Architecture:** Image bytes live in a dedicated `account_avatar` table (so ordinary account reads never load them); a small `pkg/avatar` package does decode→square-crop→512² resize→WebP-encode and owns the public-URL builder. Three HTTP endpoints (authed `PUT`/`DELETE /me/avatar`, public `GET /avatar/{subject}`). Downstream wiring adds `picture` to OIDC profile claims and an `avatar_url` SAML source. The dashboard gets an upload helper, an `<img>`-capable `UserAvatar`, and an "Edit profile" dialog.

**Tech Stack:** Go 1.26, Huma v2 + chi, sqlc/pgx, `gen2brain/webp` (libwebp→WASM via wazero, `nodynamic`), `golang.org/x/image/draw`, Vue 3 + Vite + shadcn-vue, vitest.

**Spec:** `docs/superpowers/specs/2026-06-13-account-avatars-design.md`

## Conventions (verified)

- **Migrations:** new file `db/migrations/002_avatar.sql` (goose). `001_initial.sql` is immutable. Apply with `mise db:up` (needs `podman compose up -d` first).
- **sqlc:** `mise exec -- sqlc generate` regenerates `pkg/db/` from `db/queries/*.sql` + `db/migrations/`. `GetAccountByID` (`SELECT *`) and `ListAccounts` (`SELECT a.*`) auto-pick-up new `account` columns. After adding the two small columns, `db.Account` gains `AvatarContentType pgtype.Text` and `AvatarEtag pgtype.Text`.
- **Raw HTTP routes:** `registerOpHTTP(router, method, path, contract.AuthRequirement{Kind: ...}, handler)` (pkg/server/operations.go). `contract.AuthPublic` for no-session; the authed default is the same requirement used by existing `/me` ops. Path params via `chi.URLParam(r, "name")`. Session via `authn.SessionFromContext(r.Context())` → `sess.Account` (a `db.Account`).
- **Public origin:** `s.config.PublicOrigins[0]`. OIDC issuer and SAML entityID are both this origin.
- **DB gate:** all of these need the dev DB up: `podman compose up -d`. Backend tests that hit the DB run against it; pure-unit tests (`pkg/avatar`) don't need it.
- **No `Co-Authored-By` trailer on commits.**

---

### Task 1: Migration + avatar SQL queries

**Goal:** `account_avatar` table + the two small `account` columns + sqlc queries, generated into `pkg/db`.

**Files:**
- Create: `db/migrations/002_avatar.sql`
- Modify: `db/queries/account.sql`
- Regenerate: `pkg/db/*` (via sqlc)

**Acceptance Criteria:**
- [ ] `mise db:up` applies `002_avatar.sql` cleanly; `account` has `avatar_content_type`, `avatar_etag`; `account_avatar` exists.
- [ ] `mise exec -- sqlc generate` succeeds; `go build ./...` passes with the new query methods.

**Verify:** `podman compose up -d && mise db:up && mise exec -- sqlc generate && go build ./...` → no errors.

**Steps:**

- [ ] **Step 1: Write `db/migrations/002_avatar.sql`**
```sql
-- +goose Up
ALTER TABLE account ADD COLUMN avatar_content_type text;
ALTER TABLE account ADD COLUMN avatar_etag text;

CREATE TABLE account_avatar (
  account_id int PRIMARY KEY REFERENCES account(id) ON DELETE CASCADE,
  bytes      bytea NOT NULL
);

-- +goose Down
DROP TABLE account_avatar;
ALTER TABLE account DROP COLUMN avatar_etag;
ALTER TABLE account DROP COLUMN avatar_content_type;
```

- [ ] **Step 2: Append queries to `db/queries/account.sql`**
```sql
-- name: UpsertAccountAvatarBytes :exec
INSERT INTO account_avatar (account_id, bytes) VALUES ($1, $2)
ON CONFLICT (account_id) DO UPDATE SET bytes = EXCLUDED.bytes;

-- name: SetAccountAvatarMeta :exec
UPDATE account SET avatar_content_type = $2, avatar_etag = $3, updated_at = now() WHERE id = $1;

-- name: ClearAccountAvatarBytes :exec
DELETE FROM account_avatar WHERE account_id = $1;

-- name: ClearAccountAvatarMeta :exec
UPDATE account SET avatar_content_type = NULL, avatar_etag = NULL, updated_at = now() WHERE id = $1;

-- name: GetAvatarBySubject :one
SELECT av.bytes, a.avatar_content_type, a.avatar_etag, a.disabled
FROM account a JOIN account_avatar av ON av.account_id = a.id
WHERE a.oidc_subject = $1;
```

- [ ] **Step 3: Apply + generate**

Run: `podman compose up -d && mise db:up && mise exec -- sqlc generate`
Expected: migration `002_avatar.sql` OK; sqlc writes updated `pkg/db/account.sql.go` (+ `models.go` gains `AccountAvatar`, `Account.AvatarContentType`, `Account.AvatarEtag`).

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: no errors. (`GetAvatarBySubject` takes a `pgtype.UUID`; note the generated param type for Task 3.)

- [ ] **Step 5: Commit**
```bash
git add db/migrations/002_avatar.sql db/queries/account.sql pkg/db
git commit -m "feat(db): account_avatar table + avatar meta columns + queries"
```

---

### Task 2: `pkg/avatar` — image pipeline + URL helper

**Goal:** A pure package that validates/normalizes an upload to a 512×512 WebP and builds the public avatar URL.

**Files:**
- Create: `pkg/avatar/avatar.go`, `pkg/avatar/avatar_test.go`
- Modify: `go.mod` / `go.sum` (add `github.com/gen2brain/webp`, `golang.org/x/image`)

**Acceptance Criteria:**
- [ ] `Process` rejects `>5 MiB` (`ErrTooLarge`) and undecodable/absurd input (`ErrInvalidImage`); on valid png/jpeg/webp it returns decodable WebP that is exactly 512×512, plus a hex etag.
- [ ] `PublicURL` / `AccountURL` return the cache-busting URL when an etag exists, else "".

**Verify:** `go test ./pkg/avatar/ -v` → all pass.

**Steps:**

- [ ] **Step 1: Add dependencies**
```bash
go get github.com/gen2brain/webp@latest
go get golang.org/x/image@latest
```
(WebP encode runs through the embedded WASM path; force it with the `nodynamic` build tag in Step 6's verify — and document it in the package comment.)

- [ ] **Step 2: Write the failing test — `pkg/avatar/avatar_test.go`**
```go
package avatar

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	_ "github.com/gen2brain/webp" // register webp decoder for image.Decode in assertions
	"github.com/gen2brain/webp"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestProcess_ValidProducesSquareWebP(t *testing.T) {
	out, etag, err := Process(pngBytes(t, 800, 600))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if etag == "" {
		t.Fatal("empty etag")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("output not decodable: %v", err)
	}
	if format != "webp" {
		t.Fatalf("format = %q, want webp", format)
	}
	if cfg.Width != 512 || cfg.Height != 512 {
		t.Fatalf("size = %dx%d, want 512x512", cfg.Width, cfg.Height)
	}
	_ = webp.DefaultQuality
}

func TestProcess_TooLarge(t *testing.T) {
	if _, _, err := Process(make([]byte, 5<<20+1)); err != ErrTooLarge {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestProcess_Garbage(t *testing.T) {
	if _, _, err := Process([]byte("not an image")); err != ErrInvalidImage {
		t.Fatalf("err = %v, want ErrInvalidImage", err)
	}
}

func TestPublicURL(t *testing.T) {
	got := PublicURL("11111111-2222-3333-4444-555555555555", "deadbeefcafe", "https://auth.example.com")
	want := "https://auth.example.com/avatar/11111111-2222-3333-4444-555555555555?v=deadbeef"
	if got != want {
		t.Fatalf("PublicURL = %q, want %q", got, want)
	}
	if PublicURL("sub", "", "https://x") != "" {
		t.Fatal("empty etag must yield empty URL")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./pkg/avatar/ -v`
Expected: FAIL — undefined `Process`, `ErrTooLarge`, `ErrInvalidImage`, `PublicURL`.

- [ ] **Step 4: Implement — `pkg/avatar/avatar.go`**
```go
// Package avatar validates and normalizes uploaded images to a fixed-size
// WebP for storage, and builds the public avatar URL.
//
// WebP encoding uses gen2brain/webp (libwebp compiled to WASM, run via the
// pure-Go wazero runtime). Build with the `nodynamic` tag so it never tries to
// dlopen a system libwebp — embedded WASM only, no cgo.
package avatar

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"  // decode gif input
	_ "image/jpeg" // decode jpeg input
	_ "image/png"  // decode png input

	"github.com/gen2brain/webp" // decode + encode webp
	"golang.org/x/image/draw"

	"prohibitorum/pkg/db"
)

const (
	maxInputBytes = 5 << 20 // 5 MiB
	maxInputDim   = 10000   // decompression-bomb guard
	size          = 512     // output edge
	quality       = 85      // researched: upper end of the lossy sweet spot
	method        = 6       // max effort; fine on an upload path
)

var (
	ErrTooLarge     = errors.New("avatar: input too large")
	ErrInvalidImage = errors.New("avatar: invalid or unsupported image")
)

// Process validates raw image bytes and returns a 512x512 WebP plus the hex
// SHA-256 etag of the output.
func Process(raw []byte) (out []byte, etag string, err error) {
	if len(raw) > maxInputBytes {
		return nil, "", ErrTooLarge
	}
	src, _, derr := image.Decode(bytes.NewReader(raw))
	if derr != nil {
		return nil, "", ErrInvalidImage
	}
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 || b.Dx() > maxInputDim || b.Dy() > maxInputDim {
		return nil, "", ErrInvalidImage
	}
	square := cropSquare(src)
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), square, square.Bounds(), draw.Over, nil)

	var buf bytes.Buffer
	if eerr := encodeAvatar(&buf, dst); eerr != nil {
		return nil, "", ErrInvalidImage
	}
	out = buf.Bytes()
	sum := sha256.Sum256(out)
	return out, hex.EncodeToString(sum[:]), nil
}

// encodeAvatar is the ONLY place that knows the output format/params. Swap the
// encoder here to change formats later — no data migration needed.
func encodeAvatar(buf *bytes.Buffer, img image.Image) error {
	return webp.Encode(buf, img, webp.Options{Quality: quality, Method: method})
}

// cropSquare returns the largest centered square sub-image of src.
func cropSquare(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == h {
		return src
	}
	edge := w
	if h < w {
		edge = h
	}
	x0 := b.Min.X + (w-edge)/2
	y0 := b.Min.Y + (h-edge)/2
	rect := image.Rect(x0, y0, x0+edge, y0+edge)
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	if si, ok := src.(subImager); ok {
		return si.SubImage(rect)
	}
	// Fallback: copy the square region.
	dst := image.NewRGBA(image.Rect(0, 0, edge, edge))
	draw.Copy(dst, image.Point{}, src, rect, draw.Src, nil)
	return dst
}

// PublicURL builds the cache-busting avatar URL, or "" when there is no etag.
func PublicURL(subject, etag, origin string) string {
	if subject == "" || etag == "" {
		return ""
	}
	v := etag
	if len(v) > 8 {
		v = v[:8]
	}
	return origin + "/avatar/" + subject + "?v=" + v
}

// AccountURL is PublicURL for a db.Account (extracts subject + etag).
func AccountURL(a db.Account, origin string) string {
	if !a.AvatarEtag.Valid {
		return ""
	}
	return PublicURL(a.OidcSubject.String(), a.AvatarEtag.String, origin)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./pkg/avatar/ -v`
Expected: PASS (4 tests). If `image.Decode` can't decode webp in the assertion, the blank `_ "github.com/gen2brain/webp"` import registers it — keep it.

- [ ] **Step 6: Confirm the no-cgo WASM build**

Run: `CGO_ENABLED=0 go build -tags nodynamic ./pkg/avatar/` → no errors (proves the embedded-WASM path compiles without cgo).

- [ ] **Step 7: Commit**
```bash
git add pkg/avatar go.mod go.sum
git commit -m "feat(avatar): image pipeline (512 webp q85) + public URL helper"
```

---

### Task 3: Avatar HTTP endpoints + SessionView.avatarUrl

**Goal:** `PUT`/`DELETE /me/avatar` (authed self), public `GET /avatar/{subject}`, and `avatarUrl` on `/me`.

**Files:**
- Create: `pkg/server/handle_avatar.go`, `pkg/server/handle_avatar_test.go`
- Modify: `pkg/contract/auth.go` (SessionView), `pkg/server/handle_me.go` (`sessionView`), `pkg/server/server.go` (route registration)

**Acceptance Criteria:**
- [ ] `PUT /me/avatar` stores the processed WebP (tx: bytes + meta) and refreshes the session; `DELETE` clears both; both require a session.
- [ ] `GET /avatar/{subject}` returns `image/webp` + `ETag` + `Cache-Control: public, max-age=86400`; `304` on matching `If-None-Match`; `404` for unknown subject / no avatar / disabled.
- [ ] `GET /me` includes `avatarUrl` (or omits it when unset).

**Verify:** `go test ./pkg/server/ -run Avatar -v` → pass; `go build ./...`.

**Steps:**

- [ ] **Step 1: Add `AvatarURL` to `SessionView`** (`pkg/contract/auth.go`)
```go
type SessionView struct {
	ID          int32          `json:"id"`
	Username    string         `json:"username"`
	DisplayName string         `json:"displayName"`
	Role        string         `json:"role"`
	Attributes  map[string]any `json:"attributes,omitempty"`
	AvatarURL   *string        `json:"avatarUrl,omitempty"`
}
```

- [ ] **Step 2: Populate it in `sessionView`** (`pkg/server/handle_me.go`). Find `func sessionView(...)` (it builds `contract.SessionView` from the account). It must become a method/closure with access to `s.config.PublicOrigins[0]`, OR take the origin. Simplest: make the call sites pass the server. Change `sessionView(acct)` to `s.sessionView(acct)` and add:
```go
func (s *Server) sessionView(a db.Account) contract.SessionView {
	v := contract.SessionView{
		ID:          a.ID,
		Username:    a.Username,
		DisplayName: a.DisplayName,
		Role:        a.Role,
		Attributes:  decodeAttrs(a.Attributes), // keep the existing attribute decode call used here
	}
	if u := avatar.AccountURL(a, s.config.PublicOrigins[0]); u != "" {
		v.AvatarURL = &u
	}
	return v
}
```
Update the two `/me` handlers (`handleGetMe`, `handleUpdateMe`) to call `s.sessionView(...)`. (Match the existing attribute-decode helper name already used in `sessionView`; don't invent a new one.)

- [ ] **Step 3: Write the failing test — `pkg/server/handle_avatar_test.go`**

Follow the existing `pkg/server` handler-test harness (look at a sibling `handle_*_test.go` for how a `*Server` + test DB + authed session are constructed; reuse that setup). Cover:
```go
// pseudostructure — adapt to the package's existing test harness:
// 1. PUT /me/avatar with a small valid PNG body (authed) → 200/204; row in account_avatar; account.avatar_etag set.
// 2. GET /avatar/{subject} → 200, Content-Type image/webp, ETag set; bytes decode as webp 512x512.
// 3. GET /avatar/{subject} with If-None-Match: <etag> → 304.
// 4. GET /avatar/{unknown-uuid} → 404; GET for a disabled account → 404; GET before any upload → 404.
// 5. DELETE /me/avatar (authed) → 204; subsequent GET → 404; account.avatar_etag NULL.
// 6. PUT /me/avatar with 6 MiB body → 400 avatar_too_large; with garbage → 400 avatar_invalid_image.
// 7. PUT/DELETE without a session → 401.
```
Use `avatar.Process`-shaped inputs (a real PNG via image/png) so decode succeeds.

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./pkg/server/ -run Avatar -v` → FAIL (handlers/routes undefined).

- [ ] **Step 5: Implement — `pkg/server/handle_avatar.go`**
```go
package server

import (
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"prohibitorum/pkg/authn"
	"prohibitorum/pkg/avatar"
	"prohibitorum/pkg/db"
	"prohibitorum/pkg/errorx"
)

const maxAvatarBody = 5<<20 + 1 // allow detecting the 5 MiB overage

// PUT /api/prohibitorum/me/avatar — authed self.
func (s *Server) handlePutAvatarHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxAvatarBody))
	if err != nil {
		writeAvatarErr(w, http.StatusBadRequest, "server_error", "read body")
		return
	}
	out, etag, perr := avatar.Process(raw)
	if perr == avatar.ErrTooLarge {
		writeAvatarErr(w, http.StatusBadRequest, "avatar_too_large", "image too large")
		return
	}
	if perr != nil {
		writeAvatarErr(w, http.StatusBadRequest, "avatar_invalid_image", "invalid image")
		return
	}
	// tx: bytes + meta together via the tx querier (see FOR-UPDATE/audit-FK note —
	// keep both writes on the same connection).
	if err := s.withTx(r.Context(), func(qtx db.Querier) error {
		if e := qtx.UpsertAccountAvatarBytes(r.Context(), db.UpsertAccountAvatarBytesParams{AccountID: sess.Account.ID, Bytes: out}); e != nil {
			return e
		}
		return qtx.SetAccountAvatarMeta(r.Context(), db.SetAccountAvatarMetaParams{
			ID: sess.Account.ID, AvatarContentType: pgtype.Text{String: "image/webp", Valid: true}, AvatarEtag: pgtype.Text{String: etag, Valid: true},
		})
	}); err != nil {
		writeAvatarErr(w, http.StatusInternalServerError, "server_error", "store avatar")
		return
	}
	// refresh in-memory account so the next /me reflects the new etag
	sess.Account.AvatarContentType = pgtype.Text{String: "image/webp", Valid: true}
	sess.Account.AvatarEtag = pgtype.Text{String: etag, Valid: true}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/prohibitorum/me/avatar — authed self.
func (s *Server) handleDeleteAvatarHTTP(w http.ResponseWriter, r *http.Request) {
	sess := authn.SessionFromContext(r.Context())
	if err := s.withTx(r.Context(), func(qtx db.Querier) error {
		if e := qtx.ClearAccountAvatarBytes(r.Context(), sess.Account.ID); e != nil {
			return e
		}
		return qtx.ClearAccountAvatarMeta(r.Context(), sess.Account.ID)
	}); err != nil {
		writeAvatarErr(w, http.StatusInternalServerError, "server_error", "clear avatar")
		return
	}
	sess.Account.AvatarContentType = pgtype.Text{}
	sess.Account.AvatarEtag = pgtype.Text{}
	w.WriteHeader(http.StatusNoContent)
}

// GET /avatar/{subject} — PUBLIC.
func (s *Server) handleGetAvatarHTTP(w http.ResponseWriter, r *http.Request) {
	var sub pgtype.UUID
	if err := sub.Scan(chi.URLParam(r, "subject")); err != nil {
		http.NotFound(w, r)
		return
	}
	row, err := s.queries.GetAvatarBySubject(r.Context(), sub)
	if err != nil || row.Disabled || len(row.Bytes) == 0 || !row.AvatarEtag.Valid {
		http.NotFound(w, r)
		return
	}
	etag := `"` + row.AvatarEtag.String + `"`
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	ct := "image/webp"
	if row.AvatarContentType.Valid {
		ct = row.AvatarContentType.String
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(row.Bytes)
}

func writeAvatarErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = jsonEncodeErr(w, status, msg, errorx.ErrorCode(code)) // match the package's existing error-writing helper
}
```
> Adapt `s.withTx`, `s.queries`, and the error-writer to the names the package actually uses. If there is no `withTx` helper, follow the existing transaction pattern in `pkg/server` (search for `BeginTx`/`qtx`/`audit.NewWriter(qtx)` usage — see the FOR-UPDATE/audit-FK note in project memory: keep multi-statement writes on one tx connection). `s.queries` is the pool-backed `db.Querier`; the public GET is a single read so it can use `s.queries` directly.

- [ ] **Step 6: Register routes** (`pkg/server/server.go`, near the other `registerOpHTTP` / `/me` registrations)
```go
sessionReq := contract.AuthRequirement{Kind: contract.AuthSession} // use the same value the existing /me ops use
registerOpHTTP(s.router, "PUT", "/api/prohibitorum/me/avatar", sessionReq, s.handlePutAvatarHTTP)
registerOpHTTP(s.router, "DELETE", "/api/prohibitorum/me/avatar", sessionReq, s.handleDeleteAvatarHTTP)
registerOpHTTP(s.router, "GET", "/avatar/{subject}", contract.AuthRequirement{Kind: contract.AuthPublic}, s.handleGetAvatarHTTP)
```
> Use the exact `AuthRequirement` the existing authed `/me` HTTP ops use (grep `registerOpHTTP` in server.go for the session kind). The public GET is at root (`/avatar/...`), not under `/api/prohibitorum`.

- [ ] **Step 7: Run tests + build**

Run: `podman compose up -d && go build ./... && go test ./pkg/server/ -run Avatar -v`
Expected: PASS.

- [ ] **Step 8: Commit**
```bash
git add pkg/server/handle_avatar.go pkg/server/handle_avatar_test.go pkg/contract/auth.go pkg/server/handle_me.go pkg/server/server.go
git commit -m "feat(server): avatar endpoints (PUT/DELETE /me/avatar, public GET /avatar/{subject}) + SessionView.avatarUrl"
```

---

### Task 4: Frontend — upload helper, store field, `UserAvatar` image

**Goal:** `api.upload`, `SessionView.avatarUrl`, and `<img>`-capable `UserAvatar`.

**Files:**
- Modify: `dashboard/src/lib/api.ts`, `dashboard/src/stores/auth.ts`, `dashboard/src/components/custom/UserAvatar.vue`
- Test: `dashboard/src/components/custom/UserAvatar.test.ts`

**Acceptance Criteria:**
- [ ] `api.upload(path, blob)` does a raw `PUT` with credentials, no JSON content-type; throws `ApiError` on non-2xx.
- [ ] `UserAvatar` with a `src` renders an `<img>`; on image error or empty `src` it falls back to initials → icon.

**Verify:** `cd dashboard && npx vitest run src/components/custom/UserAvatar.test.ts` → pass.

**Steps:**

- [ ] **Step 1: Add `upload` to `dashboard/src/lib/api.ts`** (after the `request` function / `api` object). Reuse the existing `ApiError`/`isApiError` from the file.
```ts
async function upload<T>(path: string, body: Blob): Promise<T> {
  const res = await fetch(path, { method: 'PUT', credentials: 'include', body })
  const text = await res.text()
  const data = text ? JSON.parse(text) : {}
  if (!res.ok) {
    const err: ApiError = isApiError(data) ? data : { code: 'server_error', message: text || res.statusText }
    throw err
  }
  return data as T
}
// add `upload` to the exported `api` object:
//   export const api = { get, post, put, upload }
```
Also add a `del` if not present, OR reuse `request('DELETE', path)` — check the file; `api.put(path)` with no body works for DELETE-less clears, but the endpoint is DELETE, so add: `del: <T>(path: string) => request<T>('DELETE', path)` if missing.

- [ ] **Step 2: Add `avatarUrl` to `SessionView`** (`dashboard/src/stores/auth.ts`)
```ts
export interface SessionView {
  id: number
  username: string
  displayName: string
  role: string
  attributes?: Record<string, unknown>
  avatarUrl?: string
}
```

- [ ] **Step 3: Write the failing UserAvatar test** — replace the contents of `dashboard/src/components/custom/UserAvatar.test.ts` keeping the existing initials/fallback tests and adding:
```ts
  it('renders an <img> when src is provided', () => {
    const w = mount(UserAvatar, { props: { displayName: 'Alex Smith', src: '/avatar/x?v=ab' } })
    const img = w.find('img')
    expect(img.exists()).toBe(true)
    expect(img.attributes('src')).toBe('/avatar/x?v=ab')
  })

  it('falls back to initials when the image errors', async () => {
    const w = mount(UserAvatar, { props: { displayName: 'Alex Smith', src: '/avatar/x?v=ab' } })
    await w.find('img').trigger('error')
    expect(w.find('img').exists()).toBe(false)
    expect(w.text()).toBe('AS')
  })
```

- [ ] **Step 4: Run → fail**: `cd dashboard && npx vitest run src/components/custom/UserAvatar.test.ts` (no `src` prop / no img).

- [ ] **Step 5: Implement** — add `src` to `UserAvatar.vue`:
```vue
<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { User } from 'lucide-vue-next'
import { cn } from '@/lib/utils'

const props = withDefaults(defineProps<{
  displayName?: string | null
  username?: string | null
  src?: string | null
  size?: 'sm' | 'md'
}>(), { size: 'md' })

const failed = ref(false)
watch(() => props.src, () => { failed.value = false })
const showImg = computed(() => !!props.src && !failed.value)

const initials = computed(() => {
  const name = (props.displayName ?? '').trim()
  if (name) {
    const parts = name.split(/\s+/).filter(Boolean)
    const chars = parts.length >= 2 ? parts[0][0] + parts[parts.length - 1][0] : parts[0].slice(0, 2)
    return chars.toUpperCase()
  }
  const u = (props.username ?? '').trim()
  if (u) return u.slice(0, 2).toUpperCase()
  return ''
})
const sizeClass = computed(() => (props.size === 'sm' ? 'size-6 text-[0.625rem]' : 'size-8 text-xs'))
</script>

<template>
  <span
    aria-hidden="true"
    :class="cn('inline-flex shrink-0 items-center justify-center overflow-hidden rounded-md bg-sidebar-accent font-medium text-sidebar-accent-foreground', sizeClass)"
  >
    <img v-if="showImg" :src="src!" alt="" class="size-full object-cover" @error="failed = true" />
    <template v-else-if="initials">{{ initials }}</template>
    <User v-else class="size-4" />
  </span>
</template>
```

- [ ] **Step 6: Run → pass**: `cd dashboard && npx vitest run src/components/custom/UserAvatar.test.ts`.

- [ ] **Step 7: Commit**
```bash
git add dashboard/src/lib/api.ts dashboard/src/stores/auth.ts dashboard/src/components/custom/UserAvatar.vue dashboard/src/components/custom/UserAvatar.test.ts
git commit -m "feat(web): api upload helper, avatarUrl in store, image-capable UserAvatar"
```

---

### Task 5: Frontend — Edit Profile dialog + NavUser wiring

**Goal:** Rename/expand the edit dialog to "Edit profile" (avatar upload/remove + display name); show the avatar in the sidebar.

**Files:**
- Rename/replace: `dashboard/src/components/custom/EditDisplayNameDialog.vue` → `EditProfileDialog.vue` (+ rename test)
- Modify: `dashboard/src/components/custom/NavUser.vue`, `dashboard/src/locales/en.ts`

**Acceptance Criteria:**
- [ ] The dropdown item reads "Edit profile"; the dialog has an avatar preview + Upload + Remove (Remove only when an avatar exists) + the display-name field.
- [ ] Upload calls `api.upload('/api/prohibitorum/me/avatar', file)`, then re-loads `/me`; Remove calls `api.del('/api/prohibitorum/me/avatar')` then re-loads; client rejects `>5 MB` with the friendly message; server errors map via `errors.${code}`.
- [ ] `NavUser` passes `auth.me.avatarUrl` to both `UserAvatar`s.

**Verify:** `cd dashboard && npx vitest run src/components/custom/EditProfileDialog.test.ts && npx vue-tsc -b`.

**Steps:**

- [ ] **Step 1: Add i18n** (`dashboard/src/locales/en.ts`) — in `accountMenu`, rename `editName`/`editTitle` to profile, add avatar labels + error codes. No apostrophes.
```ts
  accountMenu: {
    trigger: 'Account menu',
    editProfile: 'Edit profile',
    editTitle: 'Edit profile',
    editDescription: 'Your avatar and display name are shown across your account.',
    displayNameLabel: 'Display name',
    avatarLabel: 'Avatar',
    avatarUpload: 'Upload',
    avatarRemove: 'Remove',
    avatarHint: 'PNG, JPEG, or WebP, up to 5 MB. Cropped to a square.',
    avatarTooLargeClient: 'That image is larger than 5 MB.',
  },
```
And in `errors`: `avatar_too_large: 'That image is too large.'`, `avatar_invalid_image: 'That file is not a supported image.'`. Then: `grep -nP "[\x{2018}\x{2019}]" src/locales/en.ts || echo clean`.

- [ ] **Step 2: Create `EditProfileDialog.vue`** — `git mv` the old file then edit:
```bash
git mv dashboard/src/components/custom/EditDisplayNameDialog.vue dashboard/src/components/custom/EditProfileDialog.vue
git mv dashboard/src/components/custom/EditDisplayNameDialog.test.ts dashboard/src/components/custom/EditProfileDialog.test.ts
```
Then add to the script: the auth store avatar src, a file input ref, and upload/remove handlers. Keep the existing display-name logic. New template block above the display-name field:
```vue
<div class="flex items-center gap-3">
  <UserAvatar :display-name="auth.me?.displayName" :username="auth.me?.username" :src="auth.me?.avatarUrl" size="md" class="size-16" />
  <div class="flex flex-col gap-1.5">
    <span class="text-sm text-muted">{{ t('accountMenu.avatarLabel') }}</span>
    <div class="flex gap-2">
      <input ref="fileRef" type="file" accept="image/png,image/jpeg,image/webp,image/gif" class="hidden" data-test="avatar-file" @change="onFile" />
      <Button type="button" size="sm" variant="outline" :disabled="busy" data-test="avatar-upload" @click="fileRef?.click()">{{ t('accountMenu.avatarUpload') }}</Button>
      <Button v-if="auth.me?.avatarUrl" type="button" size="sm" variant="ghost" :disabled="busy" data-test="avatar-remove" @click="removeAvatar">{{ t('accountMenu.avatarRemove') }}</Button>
    </div>
    <span class="text-xs text-muted">{{ t('accountMenu.avatarHint') }}</span>
  </div>
</div>
```
Script handlers:
```ts
import { api } from '@/lib/api'
const fileRef = ref<HTMLInputElement>()
async function onFile(e: Event) {
  const f = (e.target as HTMLInputElement).files?.[0]
  if (fileRef.value) fileRef.value.value = ''
  if (!f) return
  if (f.size > 5 * 1024 * 1024) { error.value = { code: 'avatar_too_large_client', message: t('accountMenu.avatarTooLargeClient') }; return }
  const ok = await run(() => api.upload('/api/prohibitorum/me/avatar', f))
  if (ok !== undefined) await auth.reload()  // re-fetch /me to pick up avatarUrl
}
async function removeAvatar() {
  const ok = await run(() => api.del('/api/prohibitorum/me/avatar'))
  if (ok !== undefined) await auth.reload()
}
```
> The store needs a `reload()` that re-fetches `/me` and updates `me` (bypassing the `_loaded` memo). Add it to `stores/auth.ts`: `async function reload() { _loaded.value = false; await ensureLoaded() }` and export it. The `errorText` computed already maps `errors.${code}`; `avatar_too_large_client` won't match `errors.*`, so it falls back to `message` (the friendly client string) — correct.

Also: change the dropdown trigger in NavUser to use `accountMenu.editProfile` (Step 3), and the dialog title to `accountMenu.editTitle`.

- [ ] **Step 3: Wire `NavUser.vue`**
- Import is now `EditProfileDialog` (update import + the `<EditProfileDialog v-model:open="editOpen" />` tag + component usage).
- The edit menu item label: `{{ t('accountMenu.editProfile') }}`.
- Pass src to both `UserAvatar`: `:src="auth.me.avatarUrl"`.

- [ ] **Step 4: Update the test** — `EditProfileDialog.test.ts` keeps the display-name cases (they still pass) and adds: choosing a >5MB file shows the client message without calling upload; a valid file calls `api.upload` then `auth.reload`; remove calls `api.del`. Mock `api.upload`/`api.del` in the existing `vi.mock('@/lib/api', ...)`.

- [ ] **Step 5: Run + typecheck**

Run: `cd dashboard && npx vitest run src/components/custom/EditProfileDialog.test.ts src/components/custom/NavUser.test.ts && npx vue-tsc -b`
Expected: pass; 0 type errors. (`NavUser.test.ts` may need its import/label expectations updated for the rename — update if so.)

- [ ] **Step 6: Commit**
```bash
git add dashboard/src/components/custom/EditProfileDialog.vue dashboard/src/components/custom/EditProfileDialog.test.ts dashboard/src/components/custom/NavUser.vue dashboard/src/stores/auth.ts dashboard/src/locales/en.ts
git commit -m "feat(web): Edit profile dialog with avatar upload/remove; show avatar in sidebar"
```

---

### Task 6: Admin views — AccountView.avatarUrl + render avatars

**Goal:** Expose `avatarUrl` on the admin account view and render avatars in the admin list/detail.

**Files:**
- Modify: `pkg/contract/auth.go` (AccountView), `pkg/server/handle_account.go` (both projections), `dashboard/src/pages/admin/AdminAccountsView.vue`, `dashboard/src/pages/admin/AdminAccountDetailView.vue`, and their TS types.

**Acceptance Criteria:**
- [ ] `AccountView` carries `avatarUrl` (omitted when unset); both projection functions populate it via `avatar.AccountURL`.
- [ ] The admin accounts list and detail render `UserAvatar :src` per account.

**Verify:** `go build ./... && go test ./pkg/server/ -run Account -v`; `cd dashboard && npx vue-tsc -b`.

**Steps:**

- [ ] **Step 1: `AccountView` field** (`pkg/contract/auth.go`): add `AvatarURL *string `json:"avatarUrl,omitempty"``.

- [ ] **Step 2: Populate in projections** (`pkg/server/handle_account.go`). `accountViewFromAccount(a db.Account)` and `accountViewFromRow(r db.ListAccountsRow)` both have the account fields + (for the row) `OidcSubject`/`AvatarEtag` via `a.*`. Add, in each, after building the view:
```go
if u := avatar.AccountURL(toAccount(src), s.config.PublicOrigins[0]); u != "" {
	v.AvatarURL = &u
}
```
> `accountViewFromRow` operates on `ListAccountsRow` (embeds the account columns). If these are package functions (not methods), make them methods on `*Server` (they need `s.config`), or pass `origin string`. `ListAccountsRow` carries `OidcSubject` + `AvatarEtag` directly; build the URL with `avatar.PublicURL(r.OidcSubject.String(), etagStr(r.AvatarEtag), origin)`. Use whichever shape avoids a `db.Account` round-trip.

- [ ] **Step 3: Frontend types + render.** Add `avatarUrl?: string` to the admin account TS interface(s) used by `AdminAccountsView`/`AdminAccountDetailView`. In the list row and the detail header, render:
```vue
<UserAvatar :display-name="row.displayName" :username="row.username" :src="row.avatarUrl" size="sm" />
```
(Import `UserAvatar` in both views; place it in the user/name cell of the list and the identity header of the detail.)

- [ ] **Step 4: Verify + commit**

Run: `go build ./... && go test ./pkg/server/ -run Account -v && cd dashboard && npx vue-tsc -b`
```bash
git add pkg/contract/auth.go pkg/server/handle_account.go dashboard/src/pages/admin/AdminAccountsView.vue dashboard/src/pages/admin/AdminAccountDetailView.vue
git commit -m "feat(admin): expose + render account avatars"
```

---

### Task 7: OIDC `picture` claim

**Goal:** Emit `picture` in the profile-scoped claims (id_token + userinfo) when the account has an avatar.

**Files:**
- Modify: `pkg/protocol/oidc/claims.go`, the call sites that build `idTokenInput` / call `userinfoClaims`, `pkg/protocol/oidc/claims_test.go` (or sibling test).

**Acceptance Criteria:**
- [ ] With `profile` scope + an avatar, `picture` == `avatar.AccountURL(a, issuer)`; without the scope or without an avatar, `picture` is absent.

**Verify:** `go test ./pkg/protocol/oidc/ -v`.

**Steps:**

- [ ] **Step 1: Thread the origin into `profileClaims`** (`pkg/protocol/oidc/claims.go`):
```go
func profileClaims(a db.Account, origin string) map[string]any {
	c := map[string]any{
		"username":    a.Username,
		"displayName": a.DisplayName,
		"role":        a.Role,
	}
	if attrs := decodeAttributes(a.Attributes); attrs != nil {
		c["attributes"] = attrs
	}
	if pic := avatar.AccountURL(a, origin); pic != "" {
		c["picture"] = pic
	}
	return c
}
```
Add `"prohibitorum/pkg/avatar"` import. Update callers: in `idTokenClaims`, `profileClaims(a, in.Issuer)`. In `userinfoClaims`, add an `origin string` param → `userinfoClaims(a db.Account, scope []string, origin string)` and `profileClaims(a, origin)`.

- [ ] **Step 2: Update `userinfoClaims` caller** — find where `userinfoClaims` is called (pkg/protocol/oidc/userinfo.go) and pass the issuer/origin (`p.config...PublicOrigins[0]` or the provider's issuer). Use the same origin value used as the OIDC issuer.

- [ ] **Step 3: Write/extend the test** (`claims_test.go`):
```go
func TestProfileClaims_Picture(t *testing.T) {
	a := db.Account{Username: "u", DisplayName: "U", Role: "user",
		OidcSubject: mustUUID("11111111-2222-3333-4444-555555555555"),
		AvatarEtag:  pgtype.Text{String: "deadbeefcafe", Valid: true}}
	c := profileClaims(a, "https://auth.example.com")
	if c["picture"] != "https://auth.example.com/avatar/11111111-2222-3333-4444-555555555555?v=deadbeef" {
		t.Fatalf("picture = %v", c["picture"])
	}
	a.AvatarEtag = pgtype.Text{}
	if _, ok := profileClaims(a, "https://auth.example.com")["picture"]; ok {
		t.Fatal("picture must be absent without an avatar")
	}
}
```
(Reuse the package's existing `mustUUID`-style helper, or scan a `pgtype.UUID`.)

- [ ] **Step 4: Run + commit**

Run: `go test ./pkg/protocol/oidc/ -v`
```bash
git add pkg/protocol/oidc
git commit -m "feat(oidc): emit picture claim from account avatar (profile scope)"
```

---

### Task 8: SAML `avatar_url` attribute source

**Goal:** Add an `avatar_url` source so SPs can map the avatar into the AttributeStatement.

**Files:**
- Modify: `pkg/protocol/saml/attributes.go` (+ the caller that builds attributes, to thread `origin`), its test, and the dashboard SAML attribute-map hint i18n.

**Acceptance Criteria:**
- [ ] `resolveSource(a, attrs, "avatar_url", _, origin)` returns `[avatar.AccountURL(a, origin)]` when set, `nil` when unset.

**Verify:** `go test ./pkg/protocol/saml/ -v`.

**Steps:**

- [ ] **Step 1: Thread `origin` + add the source** (`pkg/protocol/saml/attributes.go`). Add `origin string` to `resolveSource` (and to `projectAttributes`, which calls it). Near the top of `resolveSource`:
```go
if source == "avatar_url" {
	if u := avatar.AccountURL(a, origin); u != "" {
		return []string{u}
	}
	return nil
}
```
Add `"prohibitorum/pkg/avatar"` import. Update `projectAttributes` signature + its single caller (the assertion builder) to pass the SAML issuer/origin (`config.PublicOrigins[0]`).

- [ ] **Step 2: Test** (saml attributes_test.go):
```go
func TestResolveSource_AvatarURL(t *testing.T) {
	a := db.Account{OidcSubject: mustUUID("11111111-2222-3333-4444-555555555555"), AvatarEtag: pgtype.Text{String: "deadbeefcafe", Valid: true}}
	got := resolveSource(a, nil, "avatar_url", false, "https://auth.example.com")
	if len(got) != 1 || got[0] != "https://auth.example.com/avatar/11111111-2222-3333-4444-555555555555?v=deadbeef" {
		t.Fatalf("got %v", got)
	}
	a.AvatarEtag = pgtype.Text{}
	if resolveSource(a, nil, "avatar_url", false, "https://auth.example.com") != nil {
		t.Fatal("want nil without avatar")
	}
}
```

- [ ] **Step 3: Document the source** — in `dashboard/src/locales/en.ts`, extend the SAML `attributeMapHint` to mention `avatar_url` as an available source alongside `username` / `attributes.*`. Grep apostrophes after.

- [ ] **Step 4: Run + commit**

Run: `go test ./pkg/protocol/saml/ -v`
```bash
git add pkg/protocol/saml dashboard/src/locales/en.ts
git commit -m "feat(saml): avatar_url attribute source"
```

---

### Task 9: Smoke coverage + done-gate

**Goal:** Prove the avatar round-trip end-to-end and ship.

**Files:**
- Modify: `cmd/smoke/main.go` (append an avatar block), `pkg/webui/dist/**` (rebuilt).

**Acceptance Criteria:**
- [ ] Smoke: `PUT /me/avatar` (small PNG) → `GET /avatar/{subject}` 200 image/webp → `/userinfo` (profile scope) contains `picture` == the public URL. `SMOKE_EXIT=0`.
- [ ] Full gate green; `dist` rebuilt + committed.

**Verify:** see steps.

**Steps:**

- [ ] **Step 1: Add a smoke block** in `cmd/smoke/main.go` after the existing `/me` coverage: generate a tiny PNG in-memory (`image/png`), `PUT` it to `/api/prohibitorum/me/avatar` (raw body) with the authed client, assert `GET <baseURL>/avatar/<subject>` returns `200` + `Content-Type: image/webp`, then assert the userinfo/ID-token `picture` claim equals `<baseURL>/avatar/<subject>?v=...`. Use the smoke's existing subject (from `/me` or the id_token `sub`). Follow the smoke's `step(...)` + `log.Fatalf` idiom.

- [ ] **Step 2: Frontend gate + rebuild dist**

Run: `cd dashboard && npm run test && npm run build`
Expected: vitest green; `vue-tsc -b` 0; `vite build` writes `../pkg/webui/dist`.

- [ ] **Step 3: Go gate**

Run: `cd /home/tundra/projects/tundra/prohibitorum && CGO_ENABLED=0 go build -tags nodynamic ./... && go vet ./... && go test ./...`
Expected: 0 failures. (Build with `nodynamic` so the avatar WASM path is exercised without cgo.)

- [ ] **Step 4: Smoke**

Run the smoke per the runbook (`podman compose up -d`; smoke env with `PROHIBITORUM_FEDERATION_ALLOW_PRIVATE_NETWORK=true`; build with `-tags nodynamic`; detached runner → poll for `SMOKE_EXIT=`). Expected: `SMOKE_EXIT=0` including the new avatar steps.

- [ ] **Step 5: Live check** (`mise dev-server`; `mise enroll-admin -- --new`): upload an avatar in Edit profile → it shows in the sidebar + admin list; open it at `/avatar/<subject>`; remove it → falls back to initials.

- [ ] **Step 6: Commit dist**
```bash
git add pkg/webui/dist cmd/smoke/main.go
git commit -m "test(smoke): avatar upload→public GET→picture claim; rebuild dist"
```

---

## Self-Review

**Spec coverage:** storage (separate `account_avatar`) → T1; pipeline + WebP q85/m6 + URL helper → T2; endpoints + SessionView.avatarUrl → T3; frontend upload/UserAvatar → T4; Edit profile dialog + sidebar → T5; AccountView + admin render → T6; OIDC `picture` → T7; SAML `avatar_url` → T8; smoke + done-gate → T9. Non-goals (federated picture, object storage) excluded. All spec sections map to a task.

**Build tag:** the WASM/no-cgo property is enforced by building with `-tags nodynamic` in T2/T9 verify steps.

**Type consistency:** `avatar.Process(raw) (out, etag, err)`, `avatar.PublicURL(subject, etag, origin)`, `avatar.AccountURL(a, origin)`; `db.Account.AvatarEtag`/`AvatarContentType` (pgtype.Text); `GetAvatarBySubject` row `{Bytes, AvatarContentType, AvatarEtag, Disabled}`; `SessionView.AvatarURL *string` / `avatarUrl`; `EditProfileDialog`; `auth.reload()`; `api.upload`/`api.del` — consistent across tasks.

**Known adaptation points (flagged inline, not placeholders):** exact `AuthRequirement` session-kind value, the `withTx`/`s.queries`/error-writer helper names, and the `userinfoClaims`/`projectAttributes` caller origins must match the names the packages already use — each step says to grep the sibling code for the exact symbol. These are real integration seams, not blanks.

## Done-gate

`CGO_ENABLED=0 go build -tags nodynamic ./...` / `go vet` / `go test ./...` (0), `vitest` (green), `vue-tsc -b` (0), smoke `SMOKE_EXIT=0` (incl. avatar/`picture`), rebuild + commit `pkg/webui/dist`.
