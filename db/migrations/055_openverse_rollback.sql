-- +goose Up
-- 055_openverse_rollback.sql — the Openverse background source is removed
-- from the build (PHB-153 rolled back). Migration 054 stays in the history so
-- databases that already ran it keep their version, but it leaves two things
-- behind in login_appearance: a background.openverse object the current
-- decoder no longer knows, and possibly background.source = 'openverse'.
-- The two are cleaned independently: 054 filled the object into every
-- document, so one can exist without the source ever being openverse.
UPDATE instance_settings
SET login_appearance = login_appearance #- '{background,openverse}'
WHERE id = 1 AND jsonb_exists(login_appearance->'background', 'openverse');

UPDATE instance_settings
SET login_appearance = jsonb_set(
    login_appearance,
    '{background,source}',
    '"none"'::jsonb
)
WHERE id = 1
  AND login_appearance->'background'->>'source' = 'openverse';

-- +goose Down
-- No-op on purpose: the Openverse feature is gone from the build, so going
-- back below 55 must not resurrect settings the current code cannot read.
-- Rolling all the way down past 054 removes the openverse object anyway.
SELECT 1;
