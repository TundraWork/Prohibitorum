-- verify_008_backfill.sql
-- Verifies the migration 008 backfill logic by running against a temp table.
-- Safe: uses a TEMP TABLE copy of signing_key schema, never touches real data.
-- Run with: psql "$PROHIBITORUM_DATABASE_URL" -f scripts/verify_008_backfill.sql
-- Expected output: row1→active, row2→retired (decommissioned_at=retire_after=retired_at), row3→pending

BEGIN;

CREATE TEMP TABLE signing_key_bf (
  kid               text PRIMARY KEY,
  algorithm         text NOT NULL DEFAULT 'RS256',
  use               text NOT NULL DEFAULT 'sig',
  public_jwk        jsonb NOT NULL,
  x509_cert_pem     text,
  private_pem       text NOT NULL,
  active            boolean NOT NULL DEFAULT false,
  not_before        timestamptz NOT NULL DEFAULT now(),
  created_at        timestamptz NOT NULL DEFAULT now(),
  retired_at        timestamptz,
  status            text NOT NULL DEFAULT 'pending',
  activated_at      timestamptz,
  decommissioned_at timestamptz,
  retire_after      timestamptz
) ON COMMIT DROP;

-- Row 1: legacy active=true, retired_at IS NULL → expect status='active', activated_at=not_before
INSERT INTO signing_key_bf (kid, use, public_jwk, private_pem, active, not_before, created_at, retired_at)
VALUES ('bf-row1-active', 'sig', '{}', 'PRIVATE', true,
        '2026-01-01 10:00:00+00', '2026-01-01 09:00:00+00', NULL);

-- Row 2: legacy retired_at IS NOT NULL → expect status='retired', decommissioned_at=retire_after=retired_at
INSERT INTO signing_key_bf (kid, use, public_jwk, private_pem, active, not_before, created_at, retired_at)
VALUES ('bf-row2-retired', 'enc', '{}', 'PRIVATE', false,
        '2026-02-01 10:00:00+00', '2026-02-01 09:00:00+00', '2026-05-01 12:00:00+00');

-- Row 3: legacy active=false, retired_at IS NULL → expect status='pending'
INSERT INTO signing_key_bf (kid, use, public_jwk, private_pem, active, not_before, created_at, retired_at)
VALUES ('bf-row3-pending', 'sig', '{}', 'PRIVATE', false,
        '2026-03-01 10:00:00+00', '2026-03-01 09:00:00+00', NULL);

-- Backfill: same logic as migration 008
UPDATE signing_key_bf SET
  status            = CASE
                        WHEN retired_at IS NOT NULL THEN 'retired'
                        WHEN active = true           THEN 'active'
                        ELSE 'pending'
                      END,
  activated_at      = CASE WHEN active = true AND retired_at IS NULL
                           THEN COALESCE(not_before, created_at) END,
  decommissioned_at = retired_at,
  retire_after      = retired_at;

-- Show results
SELECT
  kid,
  active      AS legacy_active,
  retired_at  AS legacy_retired_at,
  status,
  activated_at,
  decommissioned_at,
  retire_after
FROM signing_key_bf
ORDER BY kid;

ROLLBACK;
