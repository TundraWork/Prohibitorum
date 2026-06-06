-- +goose Up
ALTER TABLE signing_key
  ADD COLUMN status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','active','decommissioning','retired')),
  ADD COLUMN activated_at      TIMESTAMPTZ NULL,
  ADD COLUMN decommissioned_at TIMESTAMPTZ NULL,
  ADD COLUMN retire_after      TIMESTAMPTZ NULL;

-- Defensive: if >1 active row per `use` exists, keep only the newest active so
-- the partial unique index below can build without a conflict.
UPDATE signing_key sk
SET active = false
WHERE active = true
  AND kid <> (
    SELECT kid FROM signing_key s2
    WHERE s2.use = sk.use AND s2.active = true
    ORDER BY created_at DESC LIMIT 1
  );

-- Backfill explicit lifecycle from legacy columns.
UPDATE signing_key SET
  status            = CASE
                        WHEN retired_at IS NOT NULL THEN 'retired'
                        WHEN active = true           THEN 'active'
                        ELSE 'pending'
                      END,
  activated_at      = CASE WHEN active = true AND retired_at IS NULL
                           THEN COALESCE(not_before, created_at) END,
  decommissioned_at = retired_at,
  retire_after      = retired_at;

CREATE UNIQUE INDEX one_active_signing_key
  ON signing_key (use) WHERE status = 'active';

-- +goose Down
DROP INDEX IF EXISTS one_active_signing_key;
ALTER TABLE signing_key
  DROP COLUMN status,
  DROP COLUMN activated_at,
  DROP COLUMN decommissioned_at,
  DROP COLUMN retire_after;
