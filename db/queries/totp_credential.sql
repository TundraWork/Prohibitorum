-- name: GetTOTPCredential :one
SELECT * FROM totp_credential WHERE account_id = $1;

-- name: InsertTOTPCredential :one
INSERT INTO totp_credential (account_id, secret_enc, secret_nonce, key_version, period, digits, algorithm)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ConfirmTOTPCredential :exec
UPDATE totp_credential SET confirmed_at = now()
WHERE account_id = $1 AND confirmed_at IS NULL;

-- name: UpdateTOTPLastStep :one
-- RFC 6238 §5.2: this UPDATE is the atomic gate that prevents a parallel
-- replay of the same code from issuing two sessions. The Go-side
-- `matchedStep <= row.LastStep` check short-circuits the common (serial)
-- replay; this WHERE guarantees that under K-way concurrency only one
-- caller's RETURNING row populates. The remaining racers see pgx.ErrNoRows
-- and the Verify path translates that to ErrTOTPReplay.
UPDATE totp_credential SET last_step = $2
WHERE account_id = $1 AND $2 > last_step
RETURNING last_step;

-- name: DeleteTOTPCredential :exec
DELETE FROM totp_credential WHERE account_id = $1;
