-- +goose Up
-- 029_diagnostic_event.sql — curated, bounded request-diagnostic records.
--
-- Each row ties a server-generated request ID to the stable error code,
-- the operation that produced it, and a small JSONB payload of registry-
-- approved detail fields ONLY (validated in pkg/diagnostic/store.go before
-- insert). Raw cause text, secrets, headers, and tokens never enter this
-- table — the store rejects any field key not declared in the weberr
-- registry Definition for the given code.
--
-- Rows expire after seven days. The exact-ID lookup query filters on
-- expires_at > now() so an expired row is invisible (404) even before the
-- hourly prune reaper deletes it. There is intentionally NO list/enumeration
-- query — the only access path is the exact-ID admin lookup.
CREATE TABLE diagnostic_event (
  request_id   text        PRIMARY KEY,
  occurred_at  timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz NOT NULL,
  account_id   integer     REFERENCES account(id) ON DELETE SET NULL,
  method       text        NOT NULL,
  route        text        NOT NULL,
  operation    text        NOT NULL,
  code         text        NOT NULL,
  retryable    boolean     NOT NULL DEFAULT false,
  fields       jsonb       NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX diagnostic_event_expiry_idx ON diagnostic_event (expires_at);

-- +goose Down
DROP TABLE IF EXISTS diagnostic_event;
