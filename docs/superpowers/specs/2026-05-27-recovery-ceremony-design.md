# Recovery ceremony design — draft (deferred)

**Status:** drafted 2026-05-27, deferred for later implementation.
**Motivation:** v0.2 audit (2026-05-27) flagged that accepting `recovery_code`
as a sudo step-up method lets a stolen session + one leaked recovery code
escalate to full account takeover (password change, revoke-password-totp).
This design replaces the recovery-code login path with a dedicated recovery
ceremony so a recovery code becomes strictly a single-use recovery factor,
not a continuous-elevation primitive.

## What changes

### Sudo

Remove `recovery_code` from the sudo methods set. Sudo accepts
`webauthn | password_totp` only.

### Login

`POST /api/prohibitorum/auth/recovery-code/verify` is repurposed: instead
of issuing a normal session, it consumes the recovery code and returns a
narrow-scope **recovery session token**.

```
POST /auth/recovery-code/verify {partial_session_token, code}
  → 200 {recovery_session_token: "<random>"}
  → 401 if code invalid
```

No session cookie is set. The recovery session token is a separate bearer
that only the recovery-ceremony endpoints accept.

### Recovery ceremony

Two new endpoints, scoped to the recovery session token:

```
POST /auth/recovery/totp/begin {recovery_session_token}
  → 200 {secret_base32, otpauth_uri}
  Wipes the old totp_credential row + all remaining recovery_code rows.
  Inserts a fresh unconfirmed totp_credential row.

POST /auth/recovery/totp/verify {recovery_session_token, code}
  → 200 {recovery_codes: [...]} + session cookie
  Confirms the new TOTP, mints 10 fresh recovery codes, deletes the
  recovery_session token (single-use), issues a real session with
  amr=["pwd","otp","mfa"].
  → 401 on bad code (recovery session token remains valid for retry
  within its TTL)
```

### Recovery session token

- KV key: `recovery_session:<token>`.
- Value: `{account_id, issued_at}`. Optionally `{phase: "totp"}` for
  future extension (e.g., WebAuthn recovery enrollment).
- TTL: 10 minutes (longer than partial-session — user is mid-ceremony).
- Single-use: deleted on first successful `/auth/recovery/totp/verify`.
- Scope: ONLY consumable by the two recovery endpoints above. NOT a
  session — no `/me`, no sudo, no anything.
- Atomic consume via the v0.2.1 audit-fix's `kv.Store.Pop` primitive.

### What happens to old credentials

| Credential | When deleted |
|---|---|
| Redeemed recovery code | `/auth/recovery-code/verify` (existing behavior — `used_at` stamped) |
| Old `totp_credential` row | `/auth/recovery/totp/begin` (existing `Store.Begin` wipe behavior) |
| Remaining unused recovery codes | `/auth/recovery/totp/begin` (existing `DeleteAllRecoveryCodesByAccount` in `Store.Begin`) |
| New 10 recovery codes | minted at `/auth/recovery/totp/verify` first-confirm |

The deletion of the remaining unused recovery codes is the key hygiene
improvement: copies of those codes in old screenshots, password-manager
backups, or emails are dead post-recovery.

## Edge cases

- **User abandons mid-recovery:** recovery_session token expires after
  10 min. Account state: old TOTP still present (the `Begin` wipe only
  happens when `/auth/recovery/totp/begin` is called), recovery codes
  minus the redeemed one still present. User can retry with a different
  recovery code.

- **User with no TOTP:** this path isn't reachable — they have no
  recovery codes to redeem.

- **User with WebAuthn fallback:** doesn't enter recovery — uses
  WebAuthn login normally.

- **User whose only auth method was password+TOTP, all recovery codes
  spent, lost authenticator:** admin recovery (v0.1 enrollment token).
  Same as today.

- **Concurrent recovery attempts:** two parallel `/auth/recovery/totp/
  verify` requests with the same recovery_session_token — atomic `Pop`
  (post-audit-fix) ensures only one consumes the token. The losing
  request returns 401 `recovery_session_invalid`.

## Threat-model notes

- A leaked recovery code now buys the attacker exactly ONE recovery
  attempt against the victim's account, AND that attempt requires
  knowing the password (or breaking through `/auth/password/begin`'s
  throttle first). The attacker cannot:
  - Sign in to `/me/*` (no normal session issued at recovery-code/verify).
  - Elevate via sudo (recovery_code dropped from sudo methods).
  - Pivot to password change or revoke-password-totp.
- The recovery code IS still high-value: an attacker who knows the
  password AND has a recovery code can complete the recovery and lock
  the user out (by enrolling their own TOTP). This is the same risk as
  in any "one-shot recovery factor" system — mitigated by user
  diligence (don't write recovery codes in places attackers can read).

## Out of scope

- WebAuthn recovery enrollment (use a recovery code to enroll a new
  passkey instead of TOTP). Future extension; the `recovery_session`
  token's optional `phase` field anticipates this.
- Recovery via email / SMS / out-of-band channel. Out of scope per
  v0.1.1 design — admin enrollment token is the only OOB recovery.

## Implementation notes (for whoever picks this up)

- New file: `pkg/server/handle_auth_recovery.go` (or fold into
  `handle_auth_password.go` if it stays small).
- `totp.Store.Begin` and `RegenerateRecoveryCodes` already do the right
  thing on row wipe; reuse rather than duplicate.
- Remove `case "recovery_code"` from `handle_sudo.go` and from
  `availableSudoMethods`. Drop `ErrRecoveryCodeInvalid`'s only sudo
  call site.
- Smoke extension: replace existing steps 30-31 (recovery-code login
  → /me round-trip) with the new ceremony shape (recovery → enroll new
  TOTP → /me). The throttle observation phase still uses a code from
  the freshly-minted batch.
- Migration impact: none. No schema change. The endpoint paths and
  semantics change but the underlying tables are unchanged.

## Dependencies

- v0.2.1 audit fix bundle must land first (especially the atomic
  `kv.Store.Pop` primitive, since the recovery_session token relies
  on it).

## Versioning

This is a candidate for v0.2.2 (security hardening, no new
functionality) or a small v0.3-prep cycle. Decide based on whether
v0.3 (federation) is closer or further than the recovery rewrite.
