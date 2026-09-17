-- +goose Up
ALTER TABLE enrollment
  DROP CONSTRAINT enrollment_template_intent_check;

ALTER TABLE enrollment
  ADD COLUMN template_username text,
  ADD COLUMN group_ids integer[] NOT NULL DEFAULT '{}',
  ADD COLUMN created_by_account_id integer REFERENCES account(id) ON DELETE SET NULL;

ALTER TABLE enrollment
  ADD CONSTRAINT enrollment_template_intent_check
    CHECK (
      (intent = 'invite') OR (
        template_role IS NULL
        AND template_attributes IS NULL
        AND template_username IS NULL
        AND group_ids = '{}'::integer[]
        AND created_by_account_id IS NULL
      )
    );

-- +goose Down
ALTER TABLE enrollment
  DROP CONSTRAINT enrollment_template_intent_check;

ALTER TABLE enrollment
  DROP COLUMN created_by_account_id,
  DROP COLUMN group_ids,
  DROP COLUMN template_username;

ALTER TABLE enrollment
  ADD CONSTRAINT enrollment_template_intent_check
    CHECK (
      (intent = 'invite') OR (
        template_role IS NULL AND template_attributes IS NULL
      )
    );
