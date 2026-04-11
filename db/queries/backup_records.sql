-- name: CreateBackupTarget :one
INSERT INTO backup_targets (
  provider_name,
  bucket_name,
  endpoint,
  key_prefix,
  private_read
) VALUES (
  $1,
  $2,
  $3,
  $4,
  $5
)
RETURNING *;

-- name: CreateBackupRecord :one
INSERT INTO backup_records (
  asset_id,
  target_id,
  source_object_reference_id,
  backup_object_key,
  status
) VALUES (
  $1,
  $2,
  $3,
  $4,
  $5
)
RETURNING *;

-- name: MarkBackupRecordVerified :one
UPDATE backup_records
SET
  backup_etag = $2,
  checksum_sha256 = $3,
  status = 'verified',
  verified_at = NOW(),
  updated_at = NOW(),
  last_error = NULL
WHERE id = $1
RETURNING *;
