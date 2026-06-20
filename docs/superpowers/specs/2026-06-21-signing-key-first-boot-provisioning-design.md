# First-boot OIDC signing-key auto-provisioning — Design

Date: 2026-06-21
Status: Approved (brainstorming) — pending spec review

## Problem

A fresh Prohibitorum instance boots with **no active OIDC signing key**, so
`/oauth/authorize`, `/oauth/token`, JWKS, and (transitively) the new native
Traefik ForwardAuth all fail until an admin manually runs
`prohibitorum signing-key generate --activate`. This is a deploy footgun: the OP
is non-functional out of the box. Dev tooling already auto-provisions
(`cmd/prohibitorum/dev_federation.go:ensureSigningKey`); production boot does not.

## Goal

On server startup, if there is no active signing key, generate and activate one
automatically, so a fresh instance has a working OIDC OP (and forward-auth)
without manual intervention.

## Non-goals

- No config opt-out flag (YAGNI): the provisioning only ever acts when there is
  **no** active key — a non-functional state. An operator who manages keys
  manually runs `signing-key generate` first; the boot check then finds the
  active key and does nothing. (Keycloak/Authentik similarly auto-create a
  default key with no opt-out.)
- No change to the key lifecycle, rotation, the reconcile loop, or the CLI.
- No external/KMS key support (keys remain DB-backed + DEK-sealed, as today).

## Design

Add `ensureActiveSigningKey(ctx, pool, q *db.Queries, cfg *configx.Config)`,
invoked in **`NewServer`** (`pkg/server/server.go`) immediately after `queries`
is constructed (post-migration, pre-serve). Logic mirrors the dev
`ensureSigningKey` helper and the `signing-key generate` CLI:

1. `q.GetActiveSigningKey(ctx)`:
   - **no error** → an active key exists → **return** (idempotent no-op).
   - **`pgx.ErrNoRows`** → proceed to provision.
   - **other error** → log a warning and return (don't crash boot on a transient
     DB error; OIDC will surface its own errors).
2. Provision: derive the current DEK = the highest version in
   `cfg.DataEncryptionKeys` (same selection as the CLI's `mustCurrentDEK`). If
   the key set is empty → **log a warning and return** (cannot seal a key
   without a DEK; the operator must configure `PROHIBITORUM_DATA_ENCRYPTION_KEY_V<n>`).
   Otherwise `oidc.InsertPendingKey(ctx, q, dek, keyVer)` then
   `oidc.ActivateSigningKey(ctx, pool, q, pending.Kid, grace)`; log
   `auto-provisioned initial signing key <kid>` at info level.
3. **Warn-not-fatal:** any error during provisioning is logged as a warning and
   boot continues. Rationale: a missing key is already non-fatal today; a
   transient DB/race error at boot should not crash the server, and the operator
   retains the manual `signing-key generate` path.

`grace` is irrelevant for the first key (there is no prior active key to demote),
so it is passed the same default the CLI uses for activation; the value has no
effect when the active set is empty.

### Concurrency (multi-replica boot)
Benign and self-healing: if two replicas both observe `ErrNoRows` and each
activates its own pending key, the result is one active key plus possibly one
`decommissioning` key, which the existing `reconcileSigningKeysLoop` retires
after its grace window. The non-fatal handling tolerates an activation conflict.

## Components / interfaces

| Unit | Responsibility |
|---|---|
| `ensureActiveSigningKey(ctx, pool, q, cfg)` | Idempotently provision+activate an initial signing key when none is active; warn-not-fatal. Lives in `pkg/server` (boot helper). |
| `NewServer` call site | Invoke it after `queries` is built, before returning. |

Reuses existing `oidc.InsertPendingKey`, `oidc.ActivateSigningKey`,
`q.GetActiveSigningKey`, and `cfg.DataEncryptionKeys`.

## Testing

- **Unit:** `ensureActiveSigningKey` provisions+activates when none exists (a
  subsequent `GetActiveSigningKey` succeeds), and is a no-op when an active key
  is already present. Use the existing oidc signing-key test harness / a test
  DB; a DB-free seam can assert the empty-DEK warn-and-return path.
- **Runtime:** boot the server against a fresh DB (no keys) and confirm
  `/.well-known/openid-configuration` + JWKS expose a key and `/oauth/authorize`
  no longer fails for missing-key — i.e. forward-auth's `verify→authorize`
  bootstrap reaches the login rather than erroring. (Subagent-launched server,
  per the env's controller-server limitation.)

## Risks

- **Wrong-DEK sealing:** the key is sealed with the highest DEK version; this is
  the same selection the CLI and all other sealed-material writes use — no new
  risk.
- **Race double-activation:** self-healing (see Concurrency); the reconcile loop
  cleans up the extra decommissioning key.
