CREATE EXTENSION IF NOT EXISTS "pg_trgm";
CREATE EXTENSION IF NOT EXISTS "vector";

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_type
    WHERE typname = 'asset_processing_stage'
  ) THEN
    CREATE TYPE asset_processing_stage AS ENUM (
      'accepted',
      'stored',
      'derivatives_ready',
      'metadata_ready',
      'ai_ready',
      'indexed',
      'partial_failure'
    );
  END IF;

  IF NOT EXISTS (
    SELECT 1
    FROM pg_type
    WHERE typname = 'backup_status'
  ) THEN
    CREATE TYPE backup_status AS ENUM (
      'pending',
      'copying',
      'verified',
      'failed'
    );
  END IF;
END $$;

ALTER TABLE assets
  ADD COLUMN IF NOT EXISTS perceptual_hash TEXT,
  ADD COLUMN IF NOT EXISTS imported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN IF NOT EXISTS captured_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS timeline_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN IF NOT EXISTS processing_stage asset_processing_stage NOT NULL DEFAULT 'accepted',
  ADD COLUMN IF NOT EXISTS backup_status backup_status NOT NULL DEFAULT 'pending',
  ADD COLUMN IF NOT EXISTS width INTEGER,
  ADD COLUMN IF NOT EXISTS height INTEGER,
  ADD COLUMN IF NOT EXISTS duration_ms INTEGER,
  ADD COLUMN IF NOT EXISTS gps_latitude DOUBLE PRECISION,
  ADD COLUMN IF NOT EXISTS gps_longitude DOUBLE PRECISION,
  ADD COLUMN IF NOT EXISTS location_label TEXT,
  ADD COLUMN IF NOT EXISTS caption_text TEXT,
  ADD COLUMN IF NOT EXISTS ocr_text TEXT,
  ADD COLUMN IF NOT EXISTS search_document TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS search_embedding VECTOR(24),
  ADD COLUMN IF NOT EXISTS indexed_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS embedding VECTOR(1536),
  ADD COLUMN IF NOT EXISTS recognition_status_note TEXT,
  ADD COLUMN IF NOT EXISTS is_duplicate_exact BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN IF NOT EXISTS duplicate_candidate_of UUID,
  ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE assets
  ADD COLUMN IF NOT EXISTS search_tsv TSVECTOR GENERATED ALWAYS AS (to_tsvector('simple', search_document)) STORED;

CREATE INDEX IF NOT EXISTS assets_timeline_idx
  ON assets (library_id, timeline_at DESC, imported_at DESC);

CREATE INDEX IF NOT EXISTS assets_processing_idx
  ON assets (processing_stage, backup_status);

CREATE INDEX IF NOT EXISTS assets_caption_trgm_idx
  ON assets USING gin (caption_text gin_trgm_ops);

CREATE INDEX IF NOT EXISTS assets_ocr_trgm_idx
  ON assets USING gin (ocr_text gin_trgm_ops);

CREATE INDEX IF NOT EXISTS assets_search_tsv_idx
  ON assets USING gin (search_tsv);

CREATE INDEX IF NOT EXISTS assets_search_embedding_idx
  ON assets USING ivfflat (search_embedding vector_cosine_ops) WITH (lists = 100);
