-- name: ListTimelineAssets :many
SELECT
  id,
  library_id,
  media_type,
  original_filename,
  content_sha256,
  perceptual_hash,
  imported_at,
  captured_at,
  timeline_at,
  processing_stage,
  backup_status,
  location_label,
  caption_text
FROM assets
WHERE library_id = $1
ORDER BY timeline_at DESC, imported_at DESC
LIMIT $2;

-- name: GetAssetByID :one
SELECT
  id,
  library_id,
  media_type,
  original_filename,
  content_sha256,
  perceptual_hash,
  imported_at,
  captured_at,
  timeline_at,
  processing_stage,
  backup_status,
  width,
  height,
  duration_ms,
  gps_latitude,
  gps_longitude,
  location_label,
  caption_text,
  ocr_text
FROM assets
WHERE id = $1;

-- name: CreateAsset :one
INSERT INTO assets (
  library_id,
  media_type,
  original_filename,
  content_sha256,
  perceptual_hash,
  captured_at,
  timeline_at,
  processing_stage,
  backup_status
) VALUES (
  $1,
  $2,
  $3,
  $4,
  $5,
  $6,
  COALESCE($6, NOW()),
  $7,
  $8
)
RETURNING *;
