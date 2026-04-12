CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";
CREATE EXTENSION IF NOT EXISTS "vector";

CREATE TYPE asset_processing_stage AS ENUM (
  'accepted',
  'stored',
  'derivatives_ready',
  'metadata_ready',
  'ai_ready',
  'indexed',
  'partial_failure'
);

CREATE TYPE import_session_status AS ENUM (
  'draft',
  'awaiting_upload',
  'uploaded',
  'confirmed',
  'failed'
);

CREATE TYPE object_purpose AS ENUM (
  'original',
  'thumbnail',
  'preview',
  'backup'
);

CREATE TYPE album_kind AS ENUM (
  'favorites',
  'curated',
  'duplicates_review'
);

CREATE TYPE recognition_stage AS ENUM (
  'derivatives',
  'metadata',
  'caption',
  'ocr',
  'embedding',
  'indexing',
  'backup'
);

CREATE TYPE backup_status AS ENUM (
  'pending',
  'copying',
  'verified',
  'failed'
);

CREATE TABLE libraries (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  slug TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE assets (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  library_id UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  media_type TEXT NOT NULL,
  original_filename TEXT NOT NULL,
  content_sha256 TEXT NOT NULL,
  perceptual_hash TEXT,
  imported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  captured_at TIMESTAMPTZ,
  timeline_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  processing_stage asset_processing_stage NOT NULL DEFAULT 'accepted',
  backup_status backup_status NOT NULL DEFAULT 'pending',
  width INTEGER,
  height INTEGER,
  duration_ms INTEGER,
  gps_latitude DOUBLE PRECISION,
  gps_longitude DOUBLE PRECISION,
  location_label TEXT,
  caption_text TEXT,
  ocr_text TEXT,
  search_document TEXT NOT NULL DEFAULT '',
  search_tsv TSVECTOR GENERATED ALWAYS AS (to_tsvector('simple', search_document)) STORED,
  search_embedding VECTOR(24),
  indexed_at TIMESTAMPTZ,
  embedding VECTOR(1536),
  is_duplicate_exact BOOLEAN NOT NULL DEFAULT FALSE,
  duplicate_candidate_of UUID REFERENCES assets(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (library_id, content_sha256)
);

CREATE INDEX assets_timeline_idx ON assets (library_id, timeline_at DESC, imported_at DESC);
CREATE INDEX assets_processing_idx ON assets (processing_stage, backup_status);
CREATE INDEX assets_caption_trgm_idx ON assets USING gin (caption_text gin_trgm_ops);
CREATE INDEX assets_ocr_trgm_idx ON assets USING gin (ocr_text gin_trgm_ops);
CREATE INDEX assets_search_tsv_idx ON assets USING gin (search_tsv);
CREATE INDEX assets_search_embedding_idx ON assets USING ivfflat (search_embedding vector_cosine_ops) WITH (lists = 100);

CREATE TABLE object_references (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  asset_id UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  provider_name TEXT NOT NULL,
  bucket_name TEXT NOT NULL,
  object_key TEXT NOT NULL,
  object_version TEXT,
  etag TEXT,
  purpose object_purpose NOT NULL,
  content_length BIGINT NOT NULL,
  content_sha256 TEXT,
  metadata JSONB NOT NULL DEFAULT '{}'::JSONB,
  immutable BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (provider_name, bucket_name, object_key)
);

CREATE INDEX object_references_asset_idx ON object_references (asset_id, purpose);

CREATE TABLE import_sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  library_id UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  source TEXT NOT NULL,
  status import_session_status NOT NULL DEFAULT 'draft',
  expected_item_count INTEGER,
  expires_at TIMESTAMPTZ NOT NULL,
  created_by TEXT,
  note TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE import_session_items (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id UUID NOT NULL REFERENCES import_sessions(id) ON DELETE CASCADE,
  asset_id UUID REFERENCES assets(id),
  object_key TEXT NOT NULL,
  original_name TEXT NOT NULL,
  content_type TEXT NOT NULL,
  content_length BIGINT NOT NULL,
  content_sha256 TEXT,
  etag TEXT,
  multipart BOOLEAN NOT NULL DEFAULT FALSE,
  confirmed_at TIMESTAMPTZ,
  failure_reason TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX import_session_items_session_idx ON import_session_items (session_id, created_at DESC);
CREATE UNIQUE INDEX import_session_items_object_key_idx ON import_session_items (session_id, object_key);

CREATE TABLE recognition_stage_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  asset_id UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  stage recognition_stage NOT NULL,
  provider_name TEXT,
  status TEXT NOT NULL,
  policy_reason TEXT,
  attempts INTEGER NOT NULL DEFAULT 0,
  started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at TIMESTAMPTZ,
  debug_expires_at TIMESTAMPTZ,
  debug_payload JSONB,
  UNIQUE (asset_id, stage)
);

CREATE TABLE albums (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  library_id UUID NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  display_name TEXT NOT NULL,
  kind album_kind NOT NULL DEFAULT 'curated',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (library_id, slug)
);

CREATE TABLE album_assets (
  album_id UUID NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
  asset_id UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (album_id, asset_id)
);

CREATE TABLE backup_targets (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  provider_name TEXT NOT NULL UNIQUE,
  bucket_name TEXT NOT NULL,
  endpoint TEXT NOT NULL,
  key_prefix TEXT NOT NULL,
  private_read BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE backup_records (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  asset_id UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  target_id UUID NOT NULL REFERENCES backup_targets(id) ON DELETE CASCADE,
  source_object_reference_id UUID NOT NULL REFERENCES object_references(id) ON DELETE CASCADE,
  backup_object_key TEXT NOT NULL,
  backup_etag TEXT,
  checksum_sha256 TEXT,
  status backup_status NOT NULL DEFAULT 'pending',
  verified_at TIMESTAMPTZ,
  last_error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (asset_id, target_id, source_object_reference_id)
);
