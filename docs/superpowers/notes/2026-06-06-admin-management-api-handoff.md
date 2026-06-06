# Session handoff — Admin Management API: DONE

> This chunk added the admin HTTP management surface (the last major CLI/SQL-only
> backend gap) so the dashboard's five "Planned" admin pages can be built. Backend
> only; the admin dashboard UI pages remain placeholders (separate future chunk).
> All 12 plan tasks done, per-task two-stage reviewed, final whole-implementation
> opus review + fix pass complete. Committed directly to `master` (project convention).

## State
```
HEAD: 459c505   branch: master   NO git remote
phase commits: ce2fdf4..459c505 (13 feature + 1 tracker + 1 fix)
go build/vet ./... exit 0   smoke SMOKE_EXIT=0 (121/121, new arc 114–121)
```
Spec: `docs/superpowers/specs/2026-06-06-admin-management-api-design.md`
Plan: `docs/superpowers/plans/2026-06-06-admin-management-api.md` (+ `.tasks.json`, 12/12 complete)
Final review findings + fixes folded in (commit `459c505`).

> NOTE: the working tree has UNRELATED pre-existing user edits (README.md,
> dashboard/*, pkg/webui/dist deletions) from before this chunk — leave them.

## What shipped — admin HTTP API (all `/api/prohibitorum`, admin-role gated)
Reads (🔓 admin-role) are typed Huma ops; mutations (🔐 admin + fresh sudo) are raw
handlers via the new **`registerSudoOpHTTP`** wrapper (`pkg/server/operations.go`) =
admin auth + fresh sudo + body-size limit + JSON content-type, applied as route policy.

- **OIDC clients** — list/get (🔓); create (🔐, secret once), update, rotate-secret (🔐, new secret once), delete (🔐). `handle_admin_oidc_clients.go`; reuses `oidc.BuildClientParams` + new `RotateClientSecret`.
- **SAML SPs** — list/get (🔓); create, update, reingest-metadata, delete (🔐). `handle_admin_saml_sps.go`; reuses `saml.BuildSPParams` (same path as the CLI); delete via `ON DELETE CASCADE`.
- **Upstream IdPs** — list/get (🔓, secret write-only); create, update (excludes secret), rotate-secret, delete (🔐). `handle_admin_upstream_idps.go`; secret sealed via `oidc.EncryptClientSecret` (AAD bound to row id) in ONE tx.
- **Signing keys** — list (🔓, never `private_pem`); generate, activate, retire (🔐). Explicit lifecycle (below).
- **Audit events** — `GET /audit-events` (🔓), keyset pagination + filters factor/event/accountId/since/until. `handle_admin_audit.go`.
- **Account credentials** — `GET /accounts/{id}/credentials` (🔓); `POST /accounts/credentials/delete` **promoted to 🔐** this phase.

Full route list + bodies: `api.md` (new). 25 routes.

## Signing-key lifecycle (migration `008`, expand→cutover→contract)
- States `pending → active → decommissioning → retired` via explicit `signing_key.status`
  + `activated_at`/`decommissioned_at`/`retire_after`. `008` added columns + backfill +
  partial unique index `one_active_signing_key (use) WHERE status='active'`, **kept legacy
  `active`/`retired_at`** (dual-written during expand). **`009` (drop legacy cols) is a
  DEFERRED follow-up** once `status` is proven.
- Publish set (JWKS + SAML metadata) = pending+active+decommissioning; signing = single active.
- `activate` demotes prior active→decommissioning (retire_after=now+grace) THEN promotes
  (demote-first keeps both the legacy and new partial unique indexes satisfied). `retire`
  → decommissioning (409 `active_key_no_replacement` on the active key). Background
  `reconcileSigningKeysLoop` flips decommissioning→retired past retire_after.
  `pkg/protocol/oidc/keylifecycle.go`.
- Grace = `config.SAML.MetadataRotationGrace` (7d default).

## Audit
Every admin mutation writes a `credential_event` (factor oidc_client/saml_sp/upstream_idp/
signing_key; events register/update/**rotate**/revoke; actor=admin id; target+redacted
summary in detail). **No secret/key material in detail** (write-site invariant; the viewer
passes detail through). New `audit.FactorUpstreamIDP/FactorSigningKey`, `EventUpdate/EventRotate`.

## Security guard
**`TestAdminMutationRoutesRequireSudo`** (`pkg/server/admin_route_policy_test.go`) builds the
REAL `registerOperations()` router (NewHuma pattern) and asserts all 16 mutation routes return
`sudo_required` without a fresh grant. Proven to have teeth (temporarily un-gating a route makes
it fail). Add any new `registerSudoOpHTTP` route to its table.

## Bug found + fixed by the smoke arc
Admin key mutations didn't invalidate the OP's in-memory key cache (5-min TTL) → a new/activated
key wouldn't sign/publish for up to 5 min. Fix: `Provider.InvalidateKeyCache()` (OIDC) +
`IdP.InvalidateKeyCache()` (SAML), called from all three signing-key handlers.

## CLI parity (shared domain code path, `--yes` on destructive)
`signing-key {generate,activate,retire}`, `oidc-client {update,rotate-secret,delete}`,
`saml-sp {update,delete}`, `upstream-idp {create,list,update,rotate-secret,delete}`.

## Known caveats / accepted follow-ups
- **Multi-replica key-cache lag:** invalidation is per-process; other replicas pick up a
  new/activated key within the 5-min TTL (same family as the in-process-limiter caveat).
  The reconcile loop (decommissioning→retired) doesn't invalidate caches — harmless safe-
  direction lag (a non-signing key lingers in JWKS slightly longer).
- **`009`** drop of legacy `active`/`retired_at` — deferred until `status` proven.
- SAML `PUT` doesn't update `attribute_map`/`name_id_claim` (narrower than spec §5.2; accepted).
- No Go concurrency unit test for double-activation (guaranteed by the partial unique index +
  smoke); tx-ordering is smoke-verified not unit-verified.
- Pre-existing repo-wide gofmt drift (many untouched files); no enforced gofmt gate.

## What's next
- **Admin dashboard UI** — wire the 5 greyed `PlaceholderView` pages to these endpoints (frontend chunk).
- The deferred **`009`** migration after a soak on `status`.
- v0.7+ unchanged: HSM/KMS signing, SAML front-channel SLO, assertion/NameID encryption,
  upstream refresh-token storage, password breach-list, audit SIEM export.

## Runtime quirks (unchanged — these bite)
- master, direct commits, NO remote. opus for judgment/review, sonnet mechanical; never haiku.
- Trust `go build/vet ./...` exit 0 over gopls — it FALSELY reports new sqlc methods / contract
  types / `db.SigningKey.Status` etc. as "undefined". Hit constantly; the build is authoritative.
- After editing `db/queries/*.sql` or migrations: `mise exec sqlc -- sqlc generate` then `go build`.
- NEVER `pkill -f 'prohibitorum'` bare (kills dev PG at /tmp). Precise: `pkill -f 'go-build.*/prohibitorum'` + `pkill -f 'cmd/prohibitorum'`. Smoke: `setsid bash /tmp/run_v06.sh`, poll `/tmp/v06.result` for `SMOKE_EXIT=0`.
