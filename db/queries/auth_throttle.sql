-- name: GetAuthThrottle :one
SELECT * FROM auth_throttle WHERE account_id = $1 AND factor = $2;

-- name: BumpAuthThrottle :one
--
-- Atomic increment + lockout-from-just-incremented-count. The previous
-- read-then-UPSERT path lost increments under K-way concurrency (K callers
-- all read the same prior count, then last-write-wins clobbered to count+1)
-- and worse, picked the lockout duration off the racing snapshot rather than
-- the post-increment value. This single round-trip carries the full
-- exponential-backoff schedule as a bigint[] of microsecond durations; the
-- CASE clauses index into it using auth_throttle.failed_attempts + 1, which
-- Postgres evaluates against the just-incremented row.
--
-- $3 layout: schedule[i] is the lockout duration (in microseconds) applied
-- on the i-th failure (1-indexed). 0 means "no lockout yet — still in the
-- free-attempt prefix". Indexing past the array clamps to the last entry,
-- matching the Go-side schedule-walk semantics.
INSERT INTO auth_throttle (account_id, factor, failed_attempts, window_start, locked_until)
VALUES (
    $1, $2, 1, now(),
    CASE
        WHEN array_length(@schedule_micros::bigint[], 1) IS NULL THEN NULL
        WHEN (@schedule_micros::bigint[])[1] > 0 THEN now() + ((@schedule_micros::bigint[])[1] || ' microseconds')::interval
        ELSE NULL
    END
)
ON CONFLICT (account_id, factor) DO UPDATE
SET failed_attempts = auth_throttle.failed_attempts + 1,
    locked_until = (
        SELECT CASE WHEN micros > 0 THEN now() + (micros || ' microseconds')::interval ELSE NULL END
        FROM (
            SELECT (@schedule_micros::bigint[])[LEAST(auth_throttle.failed_attempts + 1, array_length(@schedule_micros::bigint[], 1))] AS micros
        ) sub
    )
RETURNING failed_attempts, locked_until;

-- name: ResetAuthThrottle :exec
DELETE FROM auth_throttle WHERE account_id = $1 AND factor = $2;
