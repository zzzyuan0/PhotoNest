-- name: CreateImportSession :one
INSERT INTO import_sessions (
  library_id,
  source,
  status,
  expected_item_count,
  expires_at,
  created_by,
  note
) VALUES (
  $1,
  $2,
  $3,
  $4,
  $5,
  $6,
  $7
)
RETURNING *;

-- name: CreateImportSessionItem :one
INSERT INTO import_session_items (
  session_id,
  object_key,
  original_name,
  content_type,
  content_length,
  content_sha256,
  multipart
) VALUES (
  $1,
  $2,
  $3,
  $4,
  $5,
  $6,
  $7
)
RETURNING *;

-- name: MarkImportSessionItemConfirmed :one
UPDATE import_session_items
SET
  asset_id = $2,
  etag = $3,
  confirmed_at = NOW(),
  failure_reason = NULL
WHERE id = $1
RETURNING *;

-- name: GetImportSessionByID :one
SELECT *
FROM import_sessions
WHERE id = $1;
