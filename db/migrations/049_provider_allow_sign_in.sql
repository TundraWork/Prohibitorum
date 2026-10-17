-- +goose Up
-- PHB-101: providers get a sign-in switch; VRChat's special-cased flows retire.
-- Rows deleted below cannot be restored by Down; see the Down note.
ALTER TABLE upstream_idp
  ADD COLUMN allow_sign_in boolean NOT NULL DEFAULT true;

UPDATE upstream_idp SET allow_sign_in = false WHERE protocol = 'vrchat';

ALTER TABLE upstream_idp
  DROP CONSTRAINT upstream_idp_vrchat_link_only_check,
  ADD CONSTRAINT upstream_idp_sign_in_mode_check
    CHECK (allow_sign_in OR mode <> 'auto_provision');

-- Pending self-service VRChat registrations and provider-sourced resets are
-- dead rows once the login button that issued them is gone.
DELETE FROM enrollment WHERE intent = 'federated_register';
DELETE FROM enrollment WHERE recovery_source_upstream_idp_id IS NOT NULL;

-- DROP the snapshot and recovery constraints before the recovery column:
-- dropping the column first would silently cascade away the checks.
ALTER TABLE enrollment
  DROP CONSTRAINT enrollment_recovery_source_check,
  DROP CONSTRAINT enrollment_intent_check,
  DROP CONSTRAINT enrollment_federated_snapshot_check;

ALTER TABLE enrollment
  DROP COLUMN recovery_source_upstream_idp_id,
  ADD CONSTRAINT enrollment_intent_check
    CHECK (intent IN ('bootstrap','invite','reset','add_device')),
  ADD CONSTRAINT enrollment_federated_snapshot_check CHECK (
    (
      intent = 'invite'
      AND federated_upstream_idp_id IS NOT NULL
      AND federated_upstream_idp_slug IS NOT NULL
      AND federated_upstream_iss IS NOT NULL
      AND federated_upstream_sub IS NOT NULL
      AND federated_display_name IS NOT NULL
      AND federated_upstream_data IS NOT NULL
      AND jsonb_typeof(federated_upstream_data) = 'object'
      AND octet_length(federated_upstream_data::text) <= 4096
    ) OR (
      federated_upstream_idp_id IS NULL
      AND federated_upstream_idp_slug IS NULL
      AND federated_upstream_iss IS NULL
      AND federated_upstream_sub IS NULL
      AND federated_display_name IS NULL
      AND federated_upstream_data IS NULL
      AND federated_avatar_url IS NULL
    )
  ),
  ADD COLUMN require_local_credential boolean NOT NULL DEFAULT false,
  ADD CONSTRAINT enrollment_require_local_credential_check
    CHECK (NOT require_local_credential OR intent = 'invite');

-- +goose Down
-- The federated_register and provider-sourced reset rows deleted in Up are not
-- recoverable; Down only restores the 033 schema shape.
UPDATE enrollment
SET federated_upstream_idp_id = NULL,
    federated_upstream_idp_slug = NULL,
    federated_upstream_iss = NULL,
    federated_upstream_sub = NULL,
    federated_display_name = NULL,
    federated_upstream_data = NULL,
    federated_avatar_url = NULL
WHERE intent = 'invite';

ALTER TABLE enrollment
  DROP CONSTRAINT enrollment_require_local_credential_check,
  DROP COLUMN require_local_credential,
  DROP CONSTRAINT enrollment_intent_check,
  ADD CONSTRAINT enrollment_intent_check
    CHECK (intent IN ('bootstrap','invite','reset','add_device','federated_register')),
  DROP CONSTRAINT enrollment_federated_snapshot_check,
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
  ADD COLUMN recovery_source_upstream_idp_id bigint REFERENCES upstream_idp(id) ON DELETE CASCADE,
  ADD CONSTRAINT enrollment_recovery_source_check CHECK (
    recovery_source_upstream_idp_id IS NULL
    OR (intent = 'reset' AND target_account_id IS NOT NULL)
  );

UPDATE upstream_idp SET mode = 'link_only' WHERE protocol = 'vrchat';

ALTER TABLE upstream_idp
  DROP CONSTRAINT upstream_idp_sign_in_mode_check,
  ADD CONSTRAINT upstream_idp_vrchat_link_only_check
    CHECK (protocol <> 'vrchat' OR mode = 'link_only'),
  DROP COLUMN allow_sign_in;
