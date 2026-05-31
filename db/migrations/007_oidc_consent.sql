-- +goose Up
CREATE TABLE oidc_consent (
    account_id     integer     NOT NULL REFERENCES account(id) ON DELETE CASCADE,
    client_id      text        NOT NULL REFERENCES oidc_client(client_id) ON DELETE CASCADE,
    granted_scopes text[]      NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, client_id)
);

-- +goose Down
DROP TABLE IF EXISTS oidc_consent;
