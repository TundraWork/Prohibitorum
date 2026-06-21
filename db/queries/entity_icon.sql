-- name: SetEntityIcon :exec
INSERT INTO entity_icon (owner_kind, owner_id, png, etag, updated_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (owner_kind, owner_id)
DO UPDATE SET png = $3, etag = $4, updated_at = now();

-- name: GetEntityIcon :one
SELECT png, etag FROM entity_icon WHERE owner_kind = $1 AND owner_id = $2;

-- name: GetEntityIconEtag :one
SELECT etag FROM entity_icon WHERE owner_kind = $1 AND owner_id = $2;

-- name: DeleteEntityIcon :exec
DELETE FROM entity_icon WHERE owner_kind = $1 AND owner_id = $2;

-- name: ListEntityIconEtags :many
SELECT owner_id, etag FROM entity_icon WHERE owner_kind = $1;
