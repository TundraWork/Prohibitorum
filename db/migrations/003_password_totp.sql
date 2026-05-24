-- +goose Up

CREATE TABLE password_credential (
  account_id          int PRIMARY KEY REFERENCES account(id) ON DELETE CASCADE,
  hash                text NOT NULL,                       -- PHC string: $argon2id$v=19$m=...$salt$tag
  password_changed_at timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE totp_credential (
  account_id    int PRIMARY KEY REFERENCES account(id) ON DELETE CASCADE,
  secret_enc    bytea NOT NULL,                            -- AES-256-GCM ciphertext (AAD: account_id||':'||key_version)
  secret_nonce  bytea NOT NULL,                            -- 12 bytes, unique per row
  key_version   int NOT NULL DEFAULT 1,                    -- DEK version; supports key rotation
  period        int NOT NULL DEFAULT 30,
  digits        int NOT NULL DEFAULT 6,
  algorithm     text NOT NULL DEFAULT 'SHA1',              -- RFC 6238 §1.2
  last_step     bigint NOT NULL DEFAULT 0,                 -- RFC 6238 §5.2: reject any T <= last_step
  confirmed_at  timestamptz,                               -- NULL until first successful verify
  created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE recovery_code (
  id              serial PRIMARY KEY,
  account_id      int NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  hash            text NOT NULL,                           -- argon2id PHC string
  used_at         timestamptz,
  used_session_id text REFERENCES session(id) ON DELETE SET NULL,
  used_ip         inet,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX recovery_code_account_id_idx ON recovery_code (account_id);

-- +goose Down
DROP TABLE IF EXISTS recovery_code;
DROP TABLE IF EXISTS totp_credential;
DROP TABLE IF EXISTS password_credential;
