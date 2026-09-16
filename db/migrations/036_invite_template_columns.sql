-- +goose Up
-- The invite template no longer carries a username or display name: accounts
-- provisioned through an invite + provider take them from the upstream claims
-- (same as auto_provision), and local-credential signups collect them on the
-- enroll form. The constraint is rebuilt in place — it still references the
-- dropped columns, so it must go before the DROP COLUMN. The narrowed form
-- keeps guarding template_role/template_attributes for non-invite intents.
ALTER TABLE enrollment
  DROP CONSTRAINT enrollment_template_intent_check;

ALTER TABLE enrollment
  DROP COLUMN template_username,
  DROP COLUMN template_display_name;

ALTER TABLE enrollment
  ADD CONSTRAINT enrollment_template_intent_check
    CHECK (
      (intent = 'invite') OR (
        (template_role IS NULL) AND (template_attributes IS NULL)
      )
    );

-- +goose Down
ALTER TABLE enrollment
  DROP CONSTRAINT enrollment_template_intent_check;

ALTER TABLE enrollment
  ADD COLUMN template_username text,
  ADD COLUMN template_display_name text;

ALTER TABLE enrollment
  ADD CONSTRAINT enrollment_template_intent_check
    CHECK (
      (intent = 'invite') OR (
        (template_username IS NULL) AND (template_display_name IS NULL)
        AND (template_role IS NULL) AND (template_attributes IS NULL)
      )
    );
