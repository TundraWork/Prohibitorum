# Spec — Self-service & admin reads (Tier 1)

**Date:** 2026-06-08
**Branch:** `master` (commit directly; no remote, no worktree).
**Status:** approved design, ready for plan.

Second of the decomposed Tier-1+2 backend cycles (after the Tier-2 hardening cycle).
Closes the Tier-1 "UI-blocking, small" gaps from the backend backlog: endpoints that make
already-shipped UI stateful, plus two admin-editing gaps. **Backend Go + the Vue frontend that
consumes each endpoint. No migration** (all columns + most queries already exist).

Contracts/behaviors verified against the code (`pkg/server/handle_me.go`, `handle_account.go`,
`handle_admin_saml_sps.go`, `pkg/session/session.go`, `pkg/db/*`, `pkg/contract/auth.go`).

## Cross-cutting
- New `/me/*` reads/updates: huma typed ops (`registerOp`), `sessionReq`, **no sudo** (low-risk).
- Admin read: `registerOp` + `admin` auth. Admin mutation (per-session revoke): `registerSudoOpHTTP` (admin + fresh sudo).
- FE reuses established patterns: `useApi`/`api`, `withSudo` for sudo-gated mutations, `errors.<code>`→`Alert`, `lib/time`. New i18n keys; **run the en.ts apostrophe guard after every en.ts edit** (`grep -nP "\x{2018}"` / `:\s*\x{2019}` — both empty).
- Done-gate rebuilds + commits `pkg/webui/dist` once (Vite hashes non-deterministic). Run git/dist from repo root.
- **gofmt is repo-wide pre-existing-dirty** — keep edited regions gofmt-clean; do NOT whole-file reformat. `go build/vet ./...` exit 0 is authoritative over stale gopls.

## 1. `PUT /me` — self-service displayName
- `PUT /api/prohibitorum/me` body `{displayName}` → `account.ValidateDisplayName` (1–128, no control chars) → the EXISTING `UpdateAccountDisplayName(ctx, {ID: sess.Account.ID, DisplayName})` query (`:exec`, touches ONLY `display_name`/`updated_at` — never role/attributes/disabled/username) → return the updated `SessionView` (re-fetch the account or patch in place). No sudo. New op `OperationUpdateMe` (Method PUT), `sessionReq`.
- Errors: `bad_request` (validation). 
- **FE:** `ProfileView.vue` — displayName becomes inline-editable (display → edit field + Save/Cancel); on success patch the auth store (`auth.me.displayName`) from the response. Username + role stay read-only.

## 2. `GET /me/factors` — factor status
- `GET /api/prohibitorum/me/factors` (no sudo) → `MeFactorsView{passwordSet bool, totpEnrolled bool, recoveryCodesRemaining int, passkeyCount int}`. Handler: `GetPasswordCredential` (err==nil → set; `pgx.ErrNoRows` → false), `GetTOTPCredential` (`ConfirmedAt.Valid` → enrolled; ErrNoRows → false), `len(ListRecoveryCodesByAccount)` → remaining, `CountCredentialsByAccount` → passkeyCount. New op `OperationGetMyFactors`, `sessionReq`. New contract type `MeFactorsView`.
- **FE:** `SecurityView.vue` fetches `/me/factors` on mount and passes status into the cards as props: PasswordCard → "Password set" badge; TotpCard → "TOTP active" badge (replaces the transient in-session `enabled` ref as the source of truth on load); RecoveryCodesCard → "{n} codes remaining". Cards stay otherwise unchanged; a refetch after a successful card mutation keeps badges current.

## 3. Admin `GET /accounts/{id}/sessions` + per-session revoke
- **List (read):** `GET /api/prohibitorum/accounts/{id}/sessions` (admin) → `[]SessionListItem` via `sessionStore.ListByAccount(ctx, id)`; `isCurrent` always false (admin is not in the target's session set). New op `OperationListAccountSessions`, `admin`. Reuses the existing `SessionListItem` contract.
- **Per-session revoke (mutation, admin+sudo):** `POST /api/prohibitorum/accounts/{id}/sessions/revoke` `{sessionId}` → `sessionStore.RevokeBySessionID(ctx, id, sessionId)`; `ok==false` → `session_not_found` (404). Registered via `registerSudoOpHTTP`. Audit/log consistent with the existing admin `revoke-sessions` handler (reuse whatever it does — `logx` line and/or `credential_event` with an existing factor; do NOT invent a new audit factor). This handler holds no account-row `FOR UPDATE`, so the Tier-2 audit-FK-deadlock lesson does not apply here.
- **FE:** `AdminAccountDetailView.vue` sessions card → a per-session table (time/IP/UA via `lib/time`) with a per-row Revoke button (`withSudo`), keeping the existing bulk "revoke all sessions". Reuses `SessionsView` row styling.

## 4. SAML PUT `attribute_map` / `name_id_claim` (+ alias-bug fix)
- Extend `UpdateSAMLSP` (`db/queries/saml_sp.sql` → `sqlc generate`) to SET `name_id_claim = $N` and `attribute_map = $M`. Extend `UpdateSAMLSPParams`, the handler's `updateSAMLProviderBody` (add `nameIdClaim string`, `attributeMap json.RawMessage`/`[]byte`), and `SAMLProviderView` (add `nameIdClaim`, `attributeMap`). `GET /{id}` already selects both columns.
- **Alias-bug fix:** the current `UpdateSAMLSP` sets `authn_requests_signed = $4` (the SAME param as `require_signed_authn_request`), so editing the "require signed AuthnRequest" flag silently overwrites `authn_requests_signed`. They are DISTINCT columns (`authn_requests_signed` reflects the SP metadata's AuthnRequestsSigned, set at create/reingest; the PUT body has no field for it). **Fix: remove `authn_requests_signed` from the UPDATE SET clause** so it is no longer clobbered (it retains its create/reingest value). (Confirm during impl that no caller depends on PUT changing it.)
- `attribute_map` is jsonb (a list; default `[]`). PUT accepts it as raw JSON; validate it parses as JSON server-side (else `bad_request`).
- **FE:** `AdminSamlProviderDetailView.vue` — add a `name_id_claim` text input and an `attribute_map` **JSON `<textarea>`** (pragmatic for the variable jsonb shape) with client-side JSON-parse validation before save; extend the local `SamlProvider` TS interface + the PUT body. Show current values on load.

## 5. Admin account attribute editing (FE-only; backend already replaces attributes)
- No backend change: `PUT /accounts/{id}` already accepts `attributes map[string]any` and replaces them wholesale (`encode/decodeAttributes`).
- **FE:** `AdminAccountDetailView.vue` — attributes become editable via a **key/value row editor** (add/edit/remove string-valued pairs) that builds the `attributes` object sent in the existing PUT. Attributes are typically string claims; a non-string existing value is rare — render it read-only/as-JSON rather than silently stringifying. Replaces the current read-only round-trip.

## Testing & gate
- **Go:** `PUT /me` (displayName-only persists; validation rejects; role/attributes untouched — assert via the fake/queries). `GET /me/factors` (each field reflects the underlying state). Admin sessions list (admin-gated; isCurrent false) + per-session revoke (admin+sudo-gated; not-found → 404). SAML PUT now persists `attribute_map`/`name_id_claim`; the alias fix leaves `authn_requests_signed` unchanged when only `requireSignedAuthnRequest` changes. (sqlc regen → trust `go build` over stale gopls.)
- **FE:** vitest for ProfileView edit, SecurityView badge wiring, AdminAccountDetailView sessions table + attributes editor, AdminSamlProviderDetailView new fields.
- **Gate:** `go build/vet ./...` 0; `go test ./...`; full `dashboard` vitest; `cmd/smoke` SMOKE_EXIT=0 (extend the smoke for the new `/me/factors`, `PUT /me`, admin sessions endpoints if a cheap assertion fits); `npm run build` + commit `dist`.

## Plan shape (~8–9 tasks, subagent-driven)
1. `PUT /me` (op + handler + Go test).
2. `GET /me/factors` (contract + op + handler + Go test).
3. ProfileView edit + SecurityView factor badges (FE + vitest).
4. Admin `GET /accounts/{id}/sessions` + per-session revoke (ops + handlers + Go tests).
5. AdminAccountDetailView sessions table + per-row revoke (FE + vitest).
6. SAML `UpdateSAMLSP` attr_map/name_id_claim + alias fix (query/sqlc + handler + contract + Go test).
7. AdminSamlProviderDetailView fields (FE + vitest).
8. Admin attributes editor (FE + vitest).
9. Final review + done-gate (build/vet/test/smoke/dist) + handoff.
(Backend tasks 1/2/4/6 can land before their FE consumers 3/5/7/8.)

## Out of scope (deferred / tracked)
- **AAGUID → provider names** (needs a bundled FIDO MDS / AAGUID dataset) and **WebAuthn Signal API on delete** (FE-led, low browser support) — own small cycle.
- **D — password/TOTP enrollment ceremony** (migration + ceremony endpoints) — next backend cycle.
- SAML-as-login (out of scope, downstream-only); breach-list (will-not-implement); `009` migration; multi-replica cache coherence; SIEM; HSM/KMS; Playwright — per the backlog.
