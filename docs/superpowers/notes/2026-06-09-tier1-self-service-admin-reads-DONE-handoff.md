# Handoff — Tier-1 self-service & admin reads — DONE (2026-06-09)

**Branch:** `master` (no remote, commit directly). **HEAD:** `0a31b95`. **Tree:** clean.
Cycle commits: `05d1076`..`0a31b95` (base `f42749f`).

Read project memory first (`MEMORY.md` + `project_current_state.md`), then this note.

## What shipped (the 2nd of the decomposed Tier-1+2 backend cycles)
Closed the Tier-1 "UI-blocking, small" gaps: endpoints that make already-shipped UI
stateful + two admin-editing gaps. **No DB migration** (all columns/queries existed; one
extended `UpdateSAMLSP` sqlc query). 8 tasks via subagent-driven-development (sonnet impl +
spec-then-quality review per task + opus final whole-cycle review).

1. **`PUT /me`** — self-service displayName (`feat(server)` `05d1076`). Session-only, NO sudo.
   Validates via `account.ValidateDisplayName` → `UpdateAccountDisplayName` (display_name only,
   never role/attributes/disabled/username) → returns updated `SessionView`. Emits
   `auth.profile_updated_self` structured log (sibling convention). New op `OperationUpdateMe`.
2. **`GET /me/factors`** — `{passwordSet, totpEnrolled, recoveryCodesRemaining, passkeyCount}`
   (`feat(server)` `10f0c71`). Session-only. `recoveryCodesRemaining` uses
   `ListRecoveryCodesByAccount` (SQL already filters `used_at IS NULL` = unused only).
   New `MeFactorsView` + `OperationGetMyFactors`.
3. **FE Profile edit + Security factor badges** (`feat(web)` `05fecf6`). ProfileView inline
   displayName edit (patches auth store from the PUT RESPONSE, not the draft); SecurityView
   fetches `/me/factors` on mount → Password/TOTP/Recovery cards show status badges; cards
   `emit('changed')` → SecurityView re-fetches. New `auth.setDisplayName` action.
4. **Admin `GET /accounts/{id}/sessions` + per-session revoke** (`feat(server)` `b39cfed`).
   List = admin-only typed op (`OperationListAccountSessions`, isCurrent ALWAYS false, 404
   `account_not_found`). Revoke = raw `registerSudoOpHTTP` handler (admin + FRESH SUDO),
   `RevokeBySessionID`, ok=false→`session_not_found`, logs `auth.session_revoked_admin`.
   Extracted `sessionRecordToItem` (handleListMySessions keeps its own inline copy — derives
   isCurrent from the caller's own token).
5. **FE admin per-session table + row revoke** (`feat(web)` `da7c27c`). AdminAccountDetailView
   sessions Table + per-row `withSudo` revoke + reload; existing bulk "revoke all" preserved
   (now also reloads). Added `session_not_found` to the en.ts errors map.
6. **SAML PUT `attribute_map`/`name_id_claim` + alias-bug fix** (`fix(saml)` `6adbc64`).
   `UpdateSAMLSP` SET now includes name_id_claim + attribute_map; **removed
   `authn_requests_signed = $4`** (it shared $4 with require_signed_authn_request → every PUT
   clobbered the distinct column). Create/reingest use `InsertSAMLSP` (unaffected);
   `authn_requests_signed` has NO runtime reader in pkg/protocol/saml — only set at insert.
   attributeMap = json.RawMessage; empty/`null` normalized to `[]` (NOT NULL jsonb).
   The plan's inner `json.Unmarshal` validation guard was removed as unreachable dead code
   (the outer decoder already rejects malformed JSON for a RawMessage field).
   Also threaded the two new fields through the CLI `saml-sp update` (preserves existing values).
7. **FE SAML provider name_id_claim + attribute_map fields** (`feat(web)` `ebdfa48`).
   AdminSamlProviderDetailView: nameIdClaim Input + attributeMap JSON Textarea (client-side
   JSON-validated before PUT; invalid → inline error, no request). Hint + test fixture use the
   REAL `attrMapEntry` shape `{name, name_format, source, multi, friendly_name}` (from
   pkg/protocol/saml/attributes.go). JSON example brace-escaped for vue-i18n.
8. **FE admin account attributes editor** (`feat(web)` `8537612`). AdminAccountDetailView:
   key/value row editor (add/remove) for STRING attributes; non-string values preserved
   read-only as JSON and merged back on save (`seedAttrs`/`buildAttrs`). buildAttrs SKIPS a
   string row whose key collides with a preserved complex key (no silent data loss). Stable
   `uid` v-for keys (not index → correct DOM patching on mid-row delete). PUT replaces
   attributes wholesale; other fields preserved.

## Auth-gating matrix (confirmed coherent by the final review)
self read/write (PUT /me, GET /me/factors) = session-only, no sudo. admin session LIST =
admin-only (read). admin session REVOKE + all SAML mutations = admin + fresh sudo.

## Gate — GREEN
- `go build ./... && go vet ./...` exit 0; `go test ./...` all pass (pkg/server ~77s).
- `cd dashboard && npx vue-tsc --noEmit` clean; **vitest 199/199** (38 files).
- **smoke SMOKE_EXIT=0** — 121 existing steps + a new appended **Tier-1 coverage block**
  (`test(smoke)` `bd8e6f9`): PUT /me round-trip, GET /me/factors (observed
  `{false,false,0,2}` end-of-run — password/TOTP were revoked at step 42, 2 passkeys remain),
  admin GET /accounts/{id}/sessions (isCurrent all false), SAML attr_map/name_id_claim PUT→GET
  round-trip. This closes the real DB round-trip gap (the unit tests for tasks 1/2/6 are
  DB-free against the `dbPool==nil`/fake-queries seam).
- dist rebuilt + committed once at the gate (`build(web)` `0a31b95`); embed build OK.

## Non-blocking minors the final review surfaced (backlog candidates)
- **M1:** the older bulk `POST /accounts/revoke-sessions` (more destructive) is admin-only/
  no-sudo, while the new per-session revoke is admin+sudo. Promote the bulk endpoint to sudo
  for symmetry (FE already wraps both in withSudo, so UX is uniform today).
- **M2:** `passkeyCount` is in the MeFactors contract + FE type but not surfaced as a badge
  (PasskeysCard renders its own list). A future passkey-count badge could use it.
- Pre-existing: api.md is stale for admin mutation shapes; `errorText` duplicated ~17 views
  (codebase norm); dual lucide dep.

## RESUME POINT (next session) — backend backlog, label D
**D — password/TOTP enrollment ceremony** is the next backend cycle (enrollment is
passkey-only today): needs a credential-requirements column on `enrollment` + new ceremony
endpoints (`/enrollments/{token}/password|totp/...`) + frontend. See
`docs/superpowers/notes/2026-06-08-backend-backlog.md` ([[reference_backend_backlog]]).
After D: AAGUID→provider names + WebAuthn Signal API (own small cycle); email/SMTP channel;
downstream SAML SLO front-channel/encryption/AttributeQuery; OIDC PAR/JAR/DPoP/mTLS/DCR/
pairwise/device; `009` migration; multi-replica cache coherence; SIEM; HSM/KMS; Playwright.
**Open loop:** the live browser visual review of the whole rebuilt UI (no screenshot tool —
the user is the verifier).

## Conventions inherited (unchanged; these bite)
- `go build/vet ./...` exit 0 is authoritative over stale gopls — which false-positives
  "undefined"/"unknown field" after out-of-editor `sqlc generate` (it did again this cycle on
  every backend task; ignore it, trust the build).
- gofmt is repo-wide pre-existing-dirty — keep edited regions clean, don't whole-file reformat.
- After any en.ts edit run the apostrophe guard: `grep -nP "\x{2018}" dashboard/src/locales/en.ts`
  and `grep -nP ":\s*\x{2019}" dashboard/src/locales/en.ts` — both empty.
- Frontend: Vue 3 + Vite + Tailwind v4 + shadcn-vue, embedded via `pkg/webui/dist` (go:embed,
  COMMITTED). Rebuild + `git add pkg/webui/dist` ONCE at the done-gate (Vite hashes
  non-deterministic; discard interim dist dirt with `git checkout -- pkg/webui/dist`).
- The `dbPool==nil` unit seam does NOT cover real tx/FOR UPDATE/FK — the smoke does
  ([[reference_for_update_audit_fk_deadlock]]). This cycle's per-session revoke holds no
  account-row FOR UPDATE, so that deadlock class didn't apply here.
- Smoke: detached `/tmp/run_v06.sh` → poll `/tmp/v06.result` for `SMOKE_EXIT=`/`DONE`; full log
  `/tmp/smoke-v06.log`. NEVER bare `pkill -f prohibitorum` (matches the dev PG `-D /tmp/prohibitorum-pg`);
  use `pkill -f 'go-build.*/prohibitorum'` / `pkill -f 'cmd/prohibitorum'`. Dev PG cluster
  (`/tmp/prohibitorum-pg`, :55432) healthy this session.
