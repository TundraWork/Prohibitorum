# Audit report — Credentials (WebAuthn, Password, TOTP, Recovery Codes)

Produced 2026-05-24 by a research agent dispatched against the multi-protocol
rescope spec. Authoritative specs consulted: W3C WebAuthn Level 3, RFC 6238,
RFC 4226, NIST SP 800-63B-4 (draft), RFC 9106 (argon2id), FIDO MDS3
references. (OWASP ASVS chapter text was not directly retrievable.)

## Critical (blocks correctness or violates a MUST)

### C1. TOTP code-reuse within a time step is not enforceable
**Spec:** RFC 6238 §5.2 — verifier MUST NOT accept the second attempt of the OTP after the successful validation. **In design:** `totp_credential` stores no last-accepted step counter. **Fix:** add `last_step bigint NOT NULL DEFAULT 0`.

### C2. Recovery codes are unsalted hashes
**Spec:** NIST SP 800-63B-4 §3.1.2. **Fix:** require PHC-string argon2id encoding (per-row salt embedded).

### C3. No AES-GCM key version → rotation breaks decryption
**Spec:** NIST SP 800-63B-4 §3.1.1.2; RFC 9106 §3.1; NIST SP 800-38D §5. **Fix:** add `key_version int NOT NULL DEFAULT 1` to `totp_credential` (and any future encrypted-secret table).

### C4. GCM ciphertext is not bound to the account
**Spec:** NIST SP 800-38D §5.2 — AAD authenticates context. **Fix:** bind `account_id || ':' || key_version` as AAD on encrypt/decrypt. Documentation, not schema.

### C5. WebAuthn `uvInitialized` is missing
**Spec:** WebAuthn L3 §4. **Fix:** add `uv_initialized bool NOT NULL DEFAULT false`.

## Recommended (cheaper now than later)

### R1. COSE algorithm column for WebAuthn
Add `cose_alg int NOT NULL` (e.g. -7 ES256, -257 RS256, -8 EdDSA).

### R2. WebAuthn `user_handle` not stored
**Spec:** L3 §4 — user handle "MUST always be populated for discoverable credentials." With ResidentKey=Required (our policy), every credential is discoverable. **Fix:** `user_handle bytea NOT NULL`, indexed.

### R3. TOTP default SHA1 acceptable; document explicitly
Keep `algorithm text DEFAULT 'SHA1'` for interop (Google Authenticator, etc.).

### R4. TOTP throttle state must persist across restarts
**Spec:** RFC 4226 §7.3. **Fix:** new `auth_throttle` table.

### R5. Password hash as self-describing PHC string
Drop `algorithm` column; require PHC format in `hash` (`$argon2id$v=19$m=...$salt$tag`).

### R6. `password_changed_at` separate from `updated_at`
Add `password_changed_at timestamptz NOT NULL DEFAULT now()`.

### R7. Recovery codes — capture redemption context
Add `used_session_id text`, `used_ip inet`.

### R8. WebAuthn sign-count anomaly tracking
Add `clone_warning_at timestamptz` (nullable).

## Optional / already-deferred

- **Full attestation object retention**: defer until MDS validation is on the roadmap.
- **`created_via`** (registration/add-passkey/recovery): defer; cheap migration later.
- **Multiple TOTP credentials per account**: do not add; industry norm is single.
- **Password history**: NIST SP 800-63B-4 §3.1.1.2 forbids periodic rotation; history nudges users to predictable variants. Do not add.
- **MDS lookup caching**: out of scope until attestation verification lands.

## New tables recommended

```sql
CREATE TABLE auth_throttle (
  account_id        bigint NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  factor            text NOT NULL,            -- 'password' | 'totp' | 'webauthn' | 'recovery_code'
  failed_attempts   int NOT NULL DEFAULT 0,
  window_start      timestamptz NOT NULL DEFAULT now(),
  locked_until      timestamptz,
  PRIMARY KEY (account_id, factor)
);

CREATE TABLE credential_event (
  id           bigserial PRIMARY KEY,
  account_id   bigint NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  factor       text NOT NULL,
  event        text NOT NULL,                -- 'register','use','fail','revoke','clone_warning'
  credential_ref bigint,
  ip           inet,
  user_agent   text,
  at           timestamptz NOT NULL DEFAULT now()
);
```
