-- +goose Up
UPDATE upstream_idp
SET mode = 'link_only'
WHERE protocol = 'vrchat';

ALTER TABLE upstream_idp
  ADD CONSTRAINT upstream_idp_vrchat_link_only_check
  CHECK (protocol <> 'vrchat' OR mode = 'link_only');

ALTER TABLE enrollment
  DROP CONSTRAINT enrollment_intent_check,
  ADD CONSTRAINT enrollment_intent_check
    CHECK (intent IN ('bootstrap','invite','reset','add_device','federated_register')),
  ADD COLUMN federated_upstream_idp_id bigint REFERENCES upstream_idp(id) ON DELETE CASCADE,
  ADD COLUMN federated_upstream_idp_slug text,
  ADD COLUMN federated_upstream_iss text,
  ADD COLUMN federated_upstream_sub text,
  ADD COLUMN federated_display_name text,
  ADD COLUMN federated_upstream_data jsonb,
  ADD COLUMN federated_avatar_url text,
  ADD COLUMN recovery_source_upstream_idp_id bigint REFERENCES upstream_idp(id) ON DELETE CASCADE,
  ADD CONSTRAINT enrollment_federated_snapshot_check CHECK (
    (
      intent = 'federated_register'
      AND federated_upstream_idp_id IS NOT NULL
      AND federated_upstream_idp_slug IS NOT NULL
      AND federated_upstream_iss IS NOT NULL
      AND federated_upstream_sub IS NOT NULL
      AND federated_display_name IS NOT NULL
      AND federated_upstream_data IS NOT NULL
      AND jsonb_typeof(federated_upstream_data) = 'object'
      AND octet_length(federated_upstream_data::text) <= 4096
      AND target_account_id IS NULL
      AND recovery_source_upstream_idp_id IS NULL
    ) OR (
      intent <> 'federated_register'
      AND federated_upstream_idp_id IS NULL
      AND federated_upstream_idp_slug IS NULL
      AND federated_upstream_iss IS NULL
      AND federated_upstream_sub IS NULL
      AND federated_display_name IS NULL
      AND federated_upstream_data IS NULL
      AND federated_avatar_url IS NULL
    )
  ),
  ADD CONSTRAINT enrollment_recovery_source_check CHECK (
    recovery_source_upstream_idp_id IS NULL
    OR (intent = 'reset' AND target_account_id IS NOT NULL)
  );

-- +goose Down
DELETE FROM enrollment WHERE intent = 'federated_register';

ALTER TABLE enrollment
  DROP CONSTRAINT enrollment_federated_snapshot_check,
  DROP CONSTRAINT enrollment_recovery_source_check,
  DROP CONSTRAINT enrollment_intent_check,
  ADD CONSTRAINT enrollment_intent_check
    CHECK (intent IN ('bootstrap','invite','reset','add_device'));

ALTER TABLE enrollment
  DROP COLUMN federated_upstream_idp_id,
  DROP COLUMN federated_upstream_idp_slug,
  DROP COLUMN federated_upstream_iss,
  DROP COLUMN federated_upstream_sub,
  DROP COLUMN federated_display_name,
  DROP COLUMN federated_upstream_data,
  DROP COLUMN federated_avatar_url,
  DROP COLUMN recovery_source_upstream_idp_id;

ALTER TABLE upstream_idp
  DROP CONSTRAINT upstream_idp_vrchat_link_only_check;
