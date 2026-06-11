-- +goose Up
-- Email becomes a first-class account attribute so the OIDC `email` scope can
-- produce real email / email_verified claims (T3.2). Nullable: not every account
-- has one (WebAuthn-only enrollments capture no email). email_verified tracks
-- whether the address was asserted by a verified upstream (federation) vs set
-- manually (admin / self-service), which resets it to false.
ALTER TABLE account
  ADD COLUMN email          TEXT NULL,
  ADD COLUMN email_verified BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE account
  DROP COLUMN email,
  DROP COLUMN email_verified;
