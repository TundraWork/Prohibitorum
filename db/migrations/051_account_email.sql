-- +goose Up
-- PHB-148: local account emails. An account holds several addresses in
-- account_email (the 10-per-account cap lives in the write layer); at most one
-- of them is the primary, pointed at by account.primary_email_id. The
-- composite FK keeps the primary inside the owning account; NO ACTION checks
-- at end of statement, so deleting an account (cascade removes its addresses
-- in the same statement) does not trip it. Only verified addresses are unique
-- across accounts (partial index): an unverified copy never blocks the
-- account that verifies the address, and once one account verifies it the
-- copies can never verify.

CREATE TABLE account_email (
  id                 bigserial PRIMARY KEY,
  account_id         int  NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  address            text NOT NULL,
  verified_at        timestamptz,
  verified_via       text CHECK (verified_via IN ('code','admin','upstream')),
  verified_by_idp_id bigint REFERENCES upstream_idp(id) ON DELETE SET NULL,
  -- verified_by_admin_id records which admin asserted the address
  -- (verified_via='admin'); the API surfaces the username on admin views.
  verified_by_admin_id bigint REFERENCES account(id) ON DELETE SET NULL,
  created_at         timestamptz NOT NULL DEFAULT now(),
  CHECK ((verified_at IS NULL) = (verified_via IS NULL)),
  UNIQUE (account_id, id)
);
CREATE UNIQUE INDEX account_email_account_address_uidx ON account_email (account_id, lower(address));
CREATE UNIQUE INDEX account_email_verified_address_uidx ON account_email (lower(address)) WHERE verified_at IS NOT NULL;
CREATE INDEX account_email_search_trgm_idx ON account_email USING gin (address gin_trgm_ops);

ALTER TABLE account ADD COLUMN primary_email_id bigint;
-- NO ACTION (not RESTRICT) already checks at end of statement, so an account
-- delete cascades to its addresses and the check passes in the same statement.
ALTER TABLE account ADD CONSTRAINT account_primary_email_fk
  FOREIGN KEY (id, primary_email_id) REFERENCES account_email (account_id, id);

-- Backfill: one address row per account whose trimmed email is non-blank.
-- Unverified accounts keep an unverified row. Among verified accounts sharing
-- one lower(address), the smallest account id keeps the verified state; the
-- rest are demoted to unverified, each with a NOTICE naming the account.
-- +goose StatementBegin
DO $$
DECLARE
  r record;
BEGIN
  INSERT INTO account_email (account_id, address, verified_at, verified_via)
  SELECT a.id,
         btrim(a.email),
         CASE WHEN a.email_verified AND NOT EXISTS (
           SELECT 1 FROM account other
           WHERE other.id < a.id
             AND other.email_verified
             AND lower(btrim(other.email)) = lower(btrim(a.email))
         ) THEN a.updated_at END,
         CASE WHEN a.email_verified AND NOT EXISTS (
           SELECT 1 FROM account other
           WHERE other.id < a.id
             AND other.email_verified
             AND lower(btrim(other.email)) = lower(btrim(a.email))
         ) THEN 'upstream' END
  FROM account a
  WHERE btrim(a.email) <> '';

  FOR r IN
    SELECT a.id AS account_id, btrim(a.email) AS address
    FROM account a
    WHERE a.email_verified
      AND btrim(a.email) <> ''
      AND EXISTS (
        SELECT 1 FROM account other
        WHERE other.id < a.id
          AND other.email_verified
          AND lower(btrim(other.email)) = lower(btrim(a.email))
      )
  LOOP
    RAISE NOTICE 'account_email: account % demoted, address % already verified on a smaller account id', r.account_id, r.address;
  END LOOP;

  UPDATE account a
  SET primary_email_id = ae.id
  FROM account_email ae
  WHERE ae.account_id = a.id
    AND ae.verified_at IS NOT NULL
    AND lower(ae.address) = lower(btrim(a.email));
END $$;
-- +goose StatementEnd

-- Email leaves the account row, so account search trims it from the trigram
-- expression; address search moves to account_email_search_trgm_idx above.
DROP INDEX account_search_trgm_idx;
CREATE INDEX account_search_trgm_idx
  ON account USING gin ((
    username || E'\n' ||
    display_name
  ) gin_trgm_ops);

ALTER TABLE account DROP COLUMN email_verified;
ALTER TABLE account DROP COLUMN email;

-- +goose Down
-- Restores the 049 shape: the primary address (verified, true) goes back to
-- account.email; accounts without a primary lose the claim until they verify
-- an address again.
ALTER TABLE account ADD COLUMN email text;
ALTER TABLE account ADD COLUMN email_verified boolean NOT NULL DEFAULT false;

UPDATE account a
SET email = ae.address,
    email_verified = true
FROM account_email ae
WHERE ae.id = a.primary_email_id;

UPDATE account SET email = NULL, email_verified = false WHERE primary_email_id IS NULL;

ALTER TABLE account DROP CONSTRAINT account_primary_email_fk;
ALTER TABLE account DROP COLUMN primary_email_id;

DROP INDEX account_email_search_trgm_idx;
DROP INDEX account_email_verified_address_uidx;
DROP INDEX account_email_account_address_uidx;
DROP TABLE account_email;

DROP INDEX account_search_trgm_idx;
CREATE INDEX account_search_trgm_idx
  ON account USING gin ((
    username || E'\n' ||
    display_name || E'\n' ||
    COALESCE(email, '')
  ) gin_trgm_ops);
