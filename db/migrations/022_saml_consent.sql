-- +goose Up
-- 022_saml_consent.sql — per-(account, SP) advisory acknowledgement that the
-- user agreed to sign in to a SAML service provider. Advisory only: a row's
-- presence means "acknowledged". Mirrors oidc_consent. Revoked by deleting the
-- row; re-acknowledged on the next SSO. CASCADEs with the account and the SP.
CREATE TABLE saml_consent (
  account_id integer NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  sp_id      bigint  NOT NULL REFERENCES saml_sp(id)  ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (account_id, sp_id)
);

-- +goose Down
DROP TABLE IF EXISTS saml_consent;
