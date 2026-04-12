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
  caption_text,
  search_document,
  indexed_at
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
  ocr_text,
  search_document,
  indexed_at
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

-- name: ListPlaceSummaries :many
SELECT
  location_label,
  COUNT(*)::BIGINT AS asset_count,
  MAX(timeline_at) AS latest_at
FROM assets
WHERE library_id = $1
  AND location_label IS NOT NULL
  AND location_label <> ''
GROUP BY location_label
ORDER BY asset_count DESC, latest_at DESC
LIMIT $2;

-- name: SearchAssetsHybrid :many
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
  caption_text,
  ocr_text,
  search_document,
  indexed_at
FROM assets
WHERE library_id = $1
  AND ($2 = '' OR search_tsv @@ plainto_tsquery('simple', $2) OR search_document ILIKE '%' || $2 || '%')
  AND ($3 = '' OR processing_stage = $3::asset_processing_stage)
  AND ($4 = '' OR backup_status = $4::backup_status)
  AND ($5 = '' OR location_label ILIKE '%' || $5 || '%')
ORDER BY
  CASE
    WHEN $2 = '' THEN 0
    ELSE ts_rank(search_tsv, plainto_tsquery('simple', $2))
  END DESC,
  timeline_at DESC,
  imported_at DESC
LIMIT $6;
