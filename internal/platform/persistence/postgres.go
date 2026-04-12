package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/photonest/photonest/internal/asset"
	"github.com/photonest/photonest/internal/discovery"
	"github.com/photonest/photonest/internal/ingestion"
	"github.com/photonest/photonest/internal/library"
	"github.com/photonest/photonest/internal/platform/config"
)

type PostgresRepository struct {
	db *sql.DB
}

func OpenPostgres(ctx context.Context, cfg config.DatabaseConfig) (*sql.DB, error) {
	dsn, err := cfg.EffectiveDSN(ctx)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) CreateSession(ctx context.Context, session ingestion.ImportSession) (ingestion.ImportSession, error) {
	if err := r.ensureLibrary(ctx, session.LibraryID); err != nil {
		return ingestion.ImportSession{}, err
	}

	const query = `
		INSERT INTO import_sessions (
			id, library_id, source, status, expected_item_count, expires_at, created_by, note, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, library_id, source, status, expected_item_count, expires_at, created_by, note, created_at, updated_at
	`

	row := r.db.QueryRowContext(ctx, query,
		session.ID,
		session.LibraryID,
		string(session.Source),
		string(session.Status),
		session.ExpectedItemCount,
		session.ExpiresAt.UTC(),
		nullString(session.CreatedBy),
		nullString(session.Note),
		session.CreatedAt.UTC(),
		session.UpdatedAt.UTC(),
	)
	record, err := scanSession(row)
	if err != nil {
		return ingestion.ImportSession{}, err
	}
	return record, nil
}

func (r *PostgresRepository) GetSession(ctx context.Context, sessionID string) (ingestion.ImportSession, error) {
	const query = `
		SELECT id, library_id, source, status, expected_item_count, expires_at, created_by, note, created_at, updated_at
		FROM import_sessions
		WHERE id = $1
	`
	record, err := scanSession(r.db.QueryRowContext(ctx, query, strings.TrimSpace(sessionID)))
	if err != nil {
		if err == sql.ErrNoRows {
			return ingestion.ImportSession{}, ingestion.ErrSessionNotFound
		}
		return ingestion.ImportSession{}, err
	}
	return record, nil
}

func (r *PostgresRepository) SaveSession(ctx context.Context, session ingestion.ImportSession) error {
	const query = `
		UPDATE import_sessions
		SET status = $2, expected_item_count = $3, expires_at = $4, created_by = $5, note = $6, updated_at = $7
		WHERE id = $1
	`
	result, err := r.db.ExecContext(ctx, query,
		session.ID,
		string(session.Status),
		session.ExpectedItemCount,
		session.ExpiresAt.UTC(),
		nullString(session.CreatedBy),
		nullString(session.Note),
		session.UpdatedAt.UTC(),
	)
	if err != nil {
		return err
	}
	return expectRowsAffected(result, ingestion.ErrSessionNotFound)
}

func (r *PostgresRepository) CreateItem(ctx context.Context, item ingestion.ImportItem) (ingestion.ImportItem, error) {
	const query = `
		INSERT INTO import_session_items (
			id, session_id, asset_id, object_key, original_name, content_type, content_length,
			content_sha256, etag, multipart, confirmed_at, failure_reason, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id, session_id, asset_id, object_key, original_name, content_type, content_length,
			content_sha256, etag, multipart, confirmed_at, failure_reason, created_at, updated_at
	`
	record, err := scanItem(r.db.QueryRowContext(ctx, query,
		item.ID,
		item.SessionID,
		nullString(item.AssetID),
		item.ObjectKey,
		item.OriginalName,
		item.ContentType,
		item.ContentLength,
		nullString(item.ContentSHA256),
		nullString(item.ETag),
		item.Multipart,
		nullTime(item.ConfirmedAt),
		nullString(item.FailureReason),
		item.CreatedAt.UTC(),
		item.UpdatedAt.UTC(),
	))
	if err != nil {
		return ingestion.ImportItem{}, err
	}
	return record, nil
}

func (r *PostgresRepository) SaveItem(ctx context.Context, item ingestion.ImportItem) error {
	const query = `
		UPDATE import_session_items
		SET asset_id = $2, original_name = $3, content_type = $4, content_length = $5,
			content_sha256 = $6, etag = $7, multipart = $8, confirmed_at = $9,
			failure_reason = $10, updated_at = $11
		WHERE id = $1
	`
	result, err := r.db.ExecContext(ctx, query,
		item.ID,
		nullString(item.AssetID),
		item.OriginalName,
		item.ContentType,
		item.ContentLength,
		nullString(item.ContentSHA256),
		nullString(item.ETag),
		item.Multipart,
		nullTime(item.ConfirmedAt),
		nullString(item.FailureReason),
		item.UpdatedAt.UTC(),
	)
	if err != nil {
		return err
	}
	return expectRowsAffected(result, ingestion.ErrItemNotFound)
}

func (r *PostgresRepository) FindReusableItem(ctx context.Context, sessionID string, lookup ingestion.ReusableItemLookup) (ingestion.ImportItem, bool, error) {
	const query = `
		SELECT id, session_id, asset_id, object_key, original_name, content_type, content_length,
			content_sha256, etag, multipart, confirmed_at, failure_reason, created_at, updated_at
		FROM import_session_items
		WHERE session_id = $1
			AND confirmed_at IS NULL
			AND lower(original_name) = lower($2)
			AND lower(content_type) = lower($3)
			AND content_length = $4
			AND multipart = $5
			AND ($6 = '' OR lower(coalesce(content_sha256, '')) = lower($6))
		ORDER BY created_at DESC
		LIMIT 1
	`
	record, err := scanItem(r.db.QueryRowContext(ctx, query,
		strings.TrimSpace(sessionID),
		strings.TrimSpace(lookup.OriginalName),
		strings.TrimSpace(lookup.ContentType),
		lookup.ContentLength,
		lookup.Multipart,
		strings.TrimSpace(lookup.ContentSHA256),
	))
	if err != nil {
		if err == sql.ErrNoRows {
			return ingestion.ImportItem{}, false, nil
		}
		return ingestion.ImportItem{}, false, err
	}
	return record, true, nil
}

func (r *PostgresRepository) GetItemByObjectKey(ctx context.Context, sessionID string, objectKey string) (ingestion.ImportItem, error) {
	const query = `
		SELECT id, session_id, asset_id, object_key, original_name, content_type, content_length,
			content_sha256, etag, multipart, confirmed_at, failure_reason, created_at, updated_at
		FROM import_session_items
		WHERE session_id = $1 AND object_key = $2
	`
	record, err := scanItem(r.db.QueryRowContext(ctx, query, strings.TrimSpace(sessionID), strings.TrimSpace(objectKey)))
	if err != nil {
		if err == sql.ErrNoRows {
			return ingestion.ImportItem{}, ingestion.ErrItemNotFound
		}
		return ingestion.ImportItem{}, err
	}
	return record, nil
}

func (r *PostgresRepository) ListItemsBySession(ctx context.Context, sessionID string) ([]ingestion.ImportItem, error) {
	const query = `
		SELECT id, session_id, asset_id, object_key, original_name, content_type, content_length,
			content_sha256, etag, multipart, confirmed_at, failure_reason, created_at, updated_at
		FROM import_session_items
		WHERE session_id = $1
		ORDER BY created_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanItems(rows)
}

func (r *PostgresRepository) CreateAsset(ctx context.Context, record asset.Asset) (asset.Asset, error) {
	if err := r.ensureLibrary(ctx, record.LibraryID); err != nil {
		return asset.Asset{}, err
	}

	const query = `
		INSERT INTO assets (
			id, library_id, media_type, original_filename, content_sha256, perceptual_hash, imported_at,
			captured_at, timeline_at, processing_stage, backup_status, width, height, duration_ms,
			gps_latitude, gps_longitude, location_label, caption_text, ocr_text, search_document,
			search_embedding, indexed_at, embedding, is_duplicate_exact, duplicate_candidate_of, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,
			NULLIF($21, '')::vector,$22,NULLIF($23, '')::vector,$24,$25,$26
		)
		RETURNING id, library_id, media_type, original_filename, content_sha256, perceptual_hash, imported_at,
			captured_at, timeline_at, processing_stage, backup_status, width, height, duration_ms,
			gps_latitude, gps_longitude, location_label, caption_text, ocr_text, search_document,
			search_embedding::text, indexed_at, embedding::text, is_duplicate_exact, duplicate_candidate_of
	`
	scanned, err := scanAsset(r.db.QueryRowContext(ctx, query,
		record.ID,
		record.LibraryID,
		record.MediaType,
		record.OriginalFilename,
		record.ContentSHA256,
		nullString(record.PerceptualHash),
		record.ImportedAt.UTC(),
		nullTime(record.CapturedAt),
		record.TimelineAt.UTC(),
		string(record.ProcessingStage),
		record.BackupStatus,
		record.Width,
		record.Height,
		record.DurationMS,
		record.GPSLatitude,
		record.GPSLongitude,
		nullString(record.LocationLabel),
		nullString(record.CaptionText),
		nullString(record.OCRText),
		record.SearchDocument,
		vectorLiteral(record.SearchEmbedding),
		nullTime(record.IndexedAt),
		vectorLiteral(record.Embedding),
		record.IsDuplicateExact,
		nullString(record.DuplicateCandidateOf),
		time.Now().UTC(),
	))
	if err != nil {
		return asset.Asset{}, err
	}
	return scanned, nil
}

func (r *PostgresRepository) SaveAsset(ctx context.Context, record asset.Asset) error {
	const query = `
		UPDATE assets
		SET media_type = $2, original_filename = $3, content_sha256 = $4, perceptual_hash = $5,
			imported_at = $6, captured_at = $7, timeline_at = $8, processing_stage = $9, backup_status = $10,
			width = $11, height = $12, duration_ms = $13, gps_latitude = $14, gps_longitude = $15,
			location_label = $16, caption_text = $17, ocr_text = $18, search_document = $19,
			search_embedding = NULLIF($20, '')::vector, indexed_at = $21, embedding = NULLIF($22, '')::vector,
			is_duplicate_exact = $23, duplicate_candidate_of = $24, updated_at = $25
		WHERE id = $1
	`
	result, err := r.db.ExecContext(ctx, query,
		record.ID,
		record.MediaType,
		record.OriginalFilename,
		record.ContentSHA256,
		nullString(record.PerceptualHash),
		record.ImportedAt.UTC(),
		nullTime(record.CapturedAt),
		record.TimelineAt.UTC(),
		string(record.ProcessingStage),
		record.BackupStatus,
		record.Width,
		record.Height,
		record.DurationMS,
		record.GPSLatitude,
		record.GPSLongitude,
		nullString(record.LocationLabel),
		nullString(record.CaptionText),
		nullString(record.OCRText),
		record.SearchDocument,
		vectorLiteral(record.SearchEmbedding),
		nullTime(record.IndexedAt),
		vectorLiteral(record.Embedding),
		record.IsDuplicateExact,
		nullString(record.DuplicateCandidateOf),
		time.Now().UTC(),
	)
	if err != nil {
		return err
	}
	return expectRowsAffected(result, ingestion.ErrAssetNotFound)
}

func (r *PostgresRepository) GetAsset(ctx context.Context, assetID string) (asset.Asset, error) {
	const query = `
		SELECT id, library_id, media_type, original_filename, content_sha256, perceptual_hash, imported_at,
			captured_at, timeline_at, processing_stage, backup_status, width, height, duration_ms,
			gps_latitude, gps_longitude, location_label, caption_text, ocr_text, search_document,
			search_embedding::text, indexed_at, embedding::text, is_duplicate_exact, duplicate_candidate_of
		FROM assets
		WHERE id = $1
	`
	record, err := scanAsset(r.db.QueryRowContext(ctx, query, strings.TrimSpace(assetID)))
	if err != nil {
		if err == sql.ErrNoRows {
			return asset.Asset{}, ingestion.ErrAssetNotFound
		}
		return asset.Asset{}, err
	}
	return record, nil
}

func (r *PostgresRepository) FindAssetByContentSHA(ctx context.Context, libraryID string, contentSHA string) (asset.Asset, bool, error) {
	const query = `
		SELECT id, library_id, media_type, original_filename, content_sha256, perceptual_hash, imported_at,
			captured_at, timeline_at, processing_stage, backup_status, width, height, duration_ms,
			gps_latitude, gps_longitude, location_label, caption_text, ocr_text, search_document,
			search_embedding::text, indexed_at, embedding::text, is_duplicate_exact, duplicate_candidate_of
		FROM assets
		WHERE library_id = $1 AND content_sha256 = $2
	`
	record, err := scanAsset(r.db.QueryRowContext(ctx, query, strings.TrimSpace(libraryID), strings.TrimSpace(contentSHA)))
	if err != nil {
		if err == sql.ErrNoRows {
			return asset.Asset{}, false, nil
		}
		return asset.Asset{}, false, err
	}
	return record, true, nil
}

func (r *PostgresRepository) ListAssetsByLibrary(ctx context.Context, libraryID string) ([]asset.Asset, error) {
	const query = `
		SELECT id, library_id, media_type, original_filename, content_sha256, perceptual_hash, imported_at,
			captured_at, timeline_at, processing_stage, backup_status, width, height, duration_ms,
			gps_latitude, gps_longitude, location_label, caption_text, ocr_text, search_document,
			search_embedding::text, indexed_at, embedding::text, is_duplicate_exact, duplicate_candidate_of
		FROM assets
		WHERE library_id = $1
		ORDER BY timeline_at DESC, imported_at DESC
	`
	rows, err := r.db.QueryContext(ctx, query, strings.TrimSpace(libraryID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]asset.Asset, 0)
	for rows.Next() {
		record, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (r *PostgresRepository) CreateObjectReference(ctx context.Context, ref asset.ObjectReference) (asset.ObjectReference, error) {
	const query = `
		INSERT INTO object_references (
			id, asset_id, provider_name, bucket_name, object_key, object_version, etag, purpose,
			content_length, content_sha256, metadata, immutable, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		RETURNING id, asset_id, provider_name, bucket_name, object_key, object_version, etag, purpose,
			content_length, content_sha256, metadata, immutable, created_at
	`
	metadata, err := json.Marshal(defaultStringMap(ref.Metadata))
	if err != nil {
		return asset.ObjectReference{}, err
	}
	record, err := scanObjectReference(r.db.QueryRowContext(ctx, query,
		ref.ID,
		ref.AssetID,
		ref.ProviderName,
		ref.Bucket,
		ref.ObjectKey,
		nullString(ref.ObjectVersion),
		nullString(ref.ETag),
		string(ref.Purpose),
		ref.ContentLength,
		nullString(ref.ContentSHA256),
		metadata,
		ref.Immutable,
		ref.CreatedAt.UTC(),
	))
	if err != nil {
		return asset.ObjectReference{}, err
	}
	return record, nil
}

func (r *PostgresRepository) ListObjectReferencesByAsset(ctx context.Context, assetID string) ([]asset.ObjectReference, error) {
	const query = `
		SELECT id, asset_id, provider_name, bucket_name, object_key, object_version, etag, purpose,
			content_length, content_sha256, metadata, immutable, created_at
		FROM object_references
		WHERE asset_id = $1
		ORDER BY created_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query, strings.TrimSpace(assetID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]asset.ObjectReference, 0)
	for rows.Next() {
		record, err := scanObjectReference(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (r *PostgresRepository) SaveRecognitionRun(ctx context.Context, run asset.RecognitionRun) (asset.RecognitionRun, error) {
	payload, err := json.Marshal(run.DebugPayload)
	if err != nil {
		return asset.RecognitionRun{}, err
	}

	const query = `
		INSERT INTO recognition_stage_runs (
			id, asset_id, stage, provider_name, status, policy_reason, attempts, last_error,
			started_at, finished_at, debug_expires_at, debug_payload
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (asset_id, stage) DO UPDATE
		SET provider_name = EXCLUDED.provider_name,
			status = EXCLUDED.status,
			policy_reason = EXCLUDED.policy_reason,
			attempts = EXCLUDED.attempts,
			last_error = EXCLUDED.last_error,
			started_at = EXCLUDED.started_at,
			finished_at = EXCLUDED.finished_at,
			debug_expires_at = EXCLUDED.debug_expires_at,
			debug_payload = EXCLUDED.debug_payload
		RETURNING id, asset_id, stage, provider_name, status, policy_reason, attempts,
			last_error, started_at, finished_at, debug_expires_at, debug_payload
	`
	record, err := scanRecognitionRun(r.db.QueryRowContext(ctx, query,
		run.ID,
		run.AssetID,
		string(run.Stage),
		nullString(run.ProviderName),
		string(run.Status),
		nullString(run.PolicyReason),
		run.Attempts,
		nullString(run.LastError),
		run.StartedAt.UTC(),
		nullTime(run.FinishedAt),
		nullTime(run.DebugExpiresAt),
		payload,
	))
	if err != nil {
		return asset.RecognitionRun{}, err
	}
	return record, nil
}

func (r *PostgresRepository) GetRecognitionRun(ctx context.Context, assetID string, stage asset.RecognitionStage) (asset.RecognitionRun, bool, error) {
	const query = `
		SELECT id, asset_id, stage, provider_name, status, policy_reason, attempts,
			last_error, started_at, finished_at, debug_expires_at, debug_payload
		FROM recognition_stage_runs
		WHERE asset_id = $1 AND stage = $2
	`
	record, err := scanRecognitionRun(r.db.QueryRowContext(ctx, query, strings.TrimSpace(assetID), string(stage)))
	if err != nil {
		if err == sql.ErrNoRows {
			return asset.RecognitionRun{}, false, nil
		}
		return asset.RecognitionRun{}, false, err
	}
	return record, true, nil
}

func (r *PostgresRepository) GetRecognitionRunByID(ctx context.Context, runID string) (asset.RecognitionRun, bool, error) {
	const query = `
		SELECT id, asset_id, stage, provider_name, status, policy_reason, attempts,
			last_error, started_at, finished_at, debug_expires_at, debug_payload
		FROM recognition_stage_runs
		WHERE id = $1
	`
	record, err := scanRecognitionRun(r.db.QueryRowContext(ctx, query, strings.TrimSpace(runID)))
	if err != nil {
		if err == sql.ErrNoRows {
			return asset.RecognitionRun{}, false, nil
		}
		return asset.RecognitionRun{}, false, err
	}
	return record, true, nil
}

func (r *PostgresRepository) ListRecognitionRunsByAsset(ctx context.Context, assetID string) ([]asset.RecognitionRun, error) {
	const query = `
		SELECT id, asset_id, stage, provider_name, status, policy_reason, attempts,
			last_error, started_at, finished_at, debug_expires_at, debug_payload
		FROM recognition_stage_runs
		WHERE asset_id = $1
		ORDER BY started_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query, strings.TrimSpace(assetID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]asset.RecognitionRun, 0)
	for rows.Next() {
		record, err := scanRecognitionRun(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func (r *PostgresRepository) SaveLibraryPolicy(ctx context.Context, libraryID string, policy library.Policy) error {
	if err := r.ensureLibrary(ctx, libraryID); err != nil {
		return err
	}
	policy = policy.WithDefaults()

	const query = `
		INSERT INTO library_policies (
			library_id, mode, allow_remote_caption, allow_remote_ocr, allow_remote_embedding,
			allow_gps_persistence, gps_mode, ocr_mode, caption_mode, embedding_mode, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (library_id) DO UPDATE
		SET mode = EXCLUDED.mode,
			allow_remote_caption = EXCLUDED.allow_remote_caption,
			allow_remote_ocr = EXCLUDED.allow_remote_ocr,
			allow_remote_embedding = EXCLUDED.allow_remote_embedding,
			allow_gps_persistence = EXCLUDED.allow_gps_persistence,
			gps_mode = EXCLUDED.gps_mode,
			ocr_mode = EXCLUDED.ocr_mode,
			caption_mode = EXCLUDED.caption_mode,
			embedding_mode = EXCLUDED.embedding_mode,
			updated_at = EXCLUDED.updated_at
	`
	_, err := r.db.ExecContext(ctx, query,
		strings.TrimSpace(libraryID),
		string(policy.Mode),
		policy.AllowRemoteCaption,
		policy.AllowRemoteOCR,
		policy.AllowRemoteEmbedding,
		policy.AllowGPSPersistence,
		string(policy.GPSMode),
		string(policy.OCRMode),
		string(policy.CaptionMode),
		string(policy.EmbeddingMode),
		time.Now().UTC(),
	)
	return err
}

func (r *PostgresRepository) GetLibraryPolicy(ctx context.Context, libraryID string) (library.Policy, error) {
	const query = `
		SELECT mode, allow_remote_caption, allow_remote_ocr, allow_remote_embedding,
			allow_gps_persistence, gps_mode, ocr_mode, caption_mode, embedding_mode
		FROM library_policies
		WHERE library_id = $1
	`
	var policy library.Policy
	err := r.db.QueryRowContext(ctx, query, strings.TrimSpace(libraryID)).Scan(
		&policy.Mode,
		&policy.AllowRemoteCaption,
		&policy.AllowRemoteOCR,
		&policy.AllowRemoteEmbedding,
		&policy.AllowGPSPersistence,
		&policy.GPSMode,
		&policy.OCRMode,
		&policy.CaptionMode,
		&policy.EmbeddingMode,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return library.DefaultPolicy(), nil
		}
		return library.Policy{}, err
	}
	return policy.WithDefaults(), nil
}

func (r *PostgresRepository) ListAlbums(ctx context.Context, libraryID string) ([]discovery.Album, error) {
	const query = `
		SELECT a.id, a.library_id, a.slug, a.display_name, a.kind, a.created_at, COUNT(aa.asset_id)::INT
		FROM albums a
		LEFT JOIN album_assets aa ON aa.album_id = a.id
		WHERE a.library_id = $1
		GROUP BY a.id
		ORDER BY a.created_at ASC
	`
	rows, err := r.db.QueryContext(ctx, query, strings.TrimSpace(libraryID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var albums []discovery.Album
	for rows.Next() {
		var album discovery.Album
		if err := rows.Scan(&album.ID, &album.LibraryID, &album.Slug, &album.DisplayName, &album.Kind, &album.CreatedAt, &album.AssetCount); err != nil {
			return nil, err
		}
		albums = append(albums, album)
	}
	return albums, rows.Err()
}

func (r *PostgresRepository) GetAlbum(ctx context.Context, albumID string) (discovery.Album, error) {
	const query = `
		SELECT a.id, a.library_id, a.slug, a.display_name, a.kind, a.created_at, COUNT(aa.asset_id)::INT
		FROM albums a
		LEFT JOIN album_assets aa ON aa.album_id = a.id
		WHERE a.id = $1
		GROUP BY a.id
	`
	var album discovery.Album
	if err := r.db.QueryRowContext(ctx, query, strings.TrimSpace(albumID)).Scan(
		&album.ID, &album.LibraryID, &album.Slug, &album.DisplayName, &album.Kind, &album.CreatedAt, &album.AssetCount,
	); err != nil {
		return discovery.Album{}, err
	}
	return album, nil
}

func (r *PostgresRepository) CreateAlbum(ctx context.Context, album discovery.Album) (discovery.Album, error) {
	if err := r.ensureLibrary(ctx, album.LibraryID); err != nil {
		return discovery.Album{}, err
	}
	const query = `
		INSERT INTO albums (id, library_id, slug, display_name, kind, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, library_id, slug, display_name, kind, created_at
	`
	record := discovery.Album{}
	if err := r.db.QueryRowContext(ctx, query,
		album.ID, album.LibraryID, album.Slug, album.DisplayName, string(album.Kind), album.CreatedAt.UTC(),
	).Scan(&record.ID, &record.LibraryID, &record.Slug, &record.DisplayName, &record.Kind, &record.CreatedAt); err != nil {
		return discovery.Album{}, err
	}
	return record, nil
}

func (r *PostgresRepository) EnsureSystemAlbum(ctx context.Context, libraryID string, kind discovery.AlbumKind) (discovery.Album, error) {
	slug := string(kind)
	name := strings.Title(strings.ReplaceAll(slug, "_", " "))
	const query = `
		INSERT INTO albums (id, library_id, slug, display_name, kind, created_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, NOW())
		ON CONFLICT (library_id, slug) DO NOTHING
	`
	if _, err := r.db.ExecContext(ctx, query, strings.TrimSpace(libraryID), slug, name, string(kind)); err != nil {
		return discovery.Album{}, err
	}
	return r.getAlbumByLibraryAndSlug(ctx, libraryID, slug)
}

func (r *PostgresRepository) AddAssetToAlbum(ctx context.Context, albumID string, assetID string) error {
	const query = `
		INSERT INTO album_assets (album_id, asset_id, sort_order, created_at)
		VALUES ($1,$2,0,NOW())
		ON CONFLICT (album_id, asset_id) DO NOTHING
	`
	_, err := r.db.ExecContext(ctx, query, strings.TrimSpace(albumID), strings.TrimSpace(assetID))
	return err
}

func (r *PostgresRepository) RemoveAssetFromAlbum(ctx context.Context, albumID string, assetID string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM album_assets WHERE album_id = $1 AND asset_id = $2`, strings.TrimSpace(albumID), strings.TrimSpace(assetID))
	return err
}

func (r *PostgresRepository) ListAssetIDsByAlbum(ctx context.Context, albumID string) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT asset_id FROM album_assets WHERE album_id = $1 ORDER BY sort_order ASC, created_at ASC`, strings.TrimSpace(albumID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *PostgresRepository) ensureLibrary(ctx context.Context, libraryID string) error {
	libraryID = strings.TrimSpace(libraryID)
	if libraryID == "" {
		return fmt.Errorf("library id is required")
	}
	prefix := libraryID
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	slug := "library-" + strings.ToLower(strings.ReplaceAll(prefix, "_", ""))
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO libraries (id, slug, display_name)
		VALUES ($1, $2, $3)
		ON CONFLICT (id) DO NOTHING
	`, libraryID, slug, "Library "+strings.ToUpper(prefix))
	return err
}

func (r *PostgresRepository) getAlbumByLibraryAndSlug(ctx context.Context, libraryID string, slug string) (discovery.Album, error) {
	const query = `
		SELECT a.id, a.library_id, a.slug, a.display_name, a.kind, a.created_at, COUNT(aa.asset_id)::INT
		FROM albums a
		LEFT JOIN album_assets aa ON aa.album_id = a.id
		WHERE a.library_id = $1 AND a.slug = $2
		GROUP BY a.id
	`
	var album discovery.Album
	if err := r.db.QueryRowContext(ctx, query, strings.TrimSpace(libraryID), strings.TrimSpace(slug)).Scan(
		&album.ID, &album.LibraryID, &album.Slug, &album.DisplayName, &album.Kind, &album.CreatedAt, &album.AssetCount,
	); err != nil {
		return discovery.Album{}, err
	}
	return album, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSession(row scanner) (ingestion.ImportSession, error) {
	var record ingestion.ImportSession
	var createdBy, note sql.NullString
	err := row.Scan(
		&record.ID,
		&record.LibraryID,
		&record.Source,
		&record.Status,
		&record.ExpectedItemCount,
		&record.ExpiresAt,
		&createdBy,
		&note,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	record.CreatedBy = createdBy.String
	record.Note = note.String
	return record, err
}

func scanItem(row scanner) (ingestion.ImportItem, error) {
	var record ingestion.ImportItem
	var assetID, contentSHA, etag, failure sql.NullString
	var confirmedAt sql.NullTime
	err := row.Scan(
		&record.ID,
		&record.SessionID,
		&assetID,
		&record.ObjectKey,
		&record.OriginalName,
		&record.ContentType,
		&record.ContentLength,
		&contentSHA,
		&etag,
		&record.Multipart,
		&confirmedAt,
		&failure,
		&record.CreatedAt,
		&record.UpdatedAt,
	)
	record.AssetID = assetID.String
	record.ContentSHA256 = contentSHA.String
	record.ETag = etag.String
	record.FailureReason = failure.String
	if confirmedAt.Valid {
		value := confirmedAt.Time.UTC()
		record.ConfirmedAt = &value
	}
	return record, err
}

func scanItems(rows *sql.Rows) ([]ingestion.ImportItem, error) {
	records := make([]ingestion.ImportItem, 0)
	for rows.Next() {
		record, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func scanAsset(row scanner) (asset.Asset, error) {
	var record asset.Asset
	var perceptualHash, locationLabel, captionText, ocrText, duplicateCandidate sql.NullString
	var capturedAt, indexedAt sql.NullTime
	var gpsLatitude, gpsLongitude sql.NullFloat64
	var searchEmbedding, embedding sql.NullString
	err := row.Scan(
		&record.ID,
		&record.LibraryID,
		&record.MediaType,
		&record.OriginalFilename,
		&record.ContentSHA256,
		&perceptualHash,
		&record.ImportedAt,
		&capturedAt,
		&record.TimelineAt,
		&record.ProcessingStage,
		&record.BackupStatus,
		&record.Width,
		&record.Height,
		&record.DurationMS,
		&gpsLatitude,
		&gpsLongitude,
		&locationLabel,
		&captionText,
		&ocrText,
		&record.SearchDocument,
		&searchEmbedding,
		&indexedAt,
		&embedding,
		&record.IsDuplicateExact,
		&duplicateCandidate,
	)
	record.PerceptualHash = perceptualHash.String
	record.LocationLabel = locationLabel.String
	record.CaptionText = captionText.String
	record.OCRText = ocrText.String
	record.DuplicateCandidateOf = duplicateCandidate.String
	record.SearchEmbedding = parseVector(searchEmbedding.String)
	record.Embedding = parseVector(embedding.String)
	if capturedAt.Valid {
		value := capturedAt.Time.UTC()
		record.CapturedAt = &value
	}
	if indexedAt.Valid {
		value := indexedAt.Time.UTC()
		record.IndexedAt = &value
	}
	if gpsLatitude.Valid {
		value := gpsLatitude.Float64
		record.GPSLatitude = &value
	}
	if gpsLongitude.Valid {
		value := gpsLongitude.Float64
		record.GPSLongitude = &value
	}
	return record, err
}

func scanObjectReference(row scanner) (asset.ObjectReference, error) {
	var record asset.ObjectReference
	var objectVersion, etag, contentSHA sql.NullString
	var metadata []byte
	err := row.Scan(
		&record.ID,
		&record.AssetID,
		&record.ProviderName,
		&record.Bucket,
		&record.ObjectKey,
		&objectVersion,
		&etag,
		&record.Purpose,
		&record.ContentLength,
		&contentSHA,
		&metadata,
		&record.Immutable,
		&record.CreatedAt,
	)
	record.ObjectVersion = objectVersion.String
	record.ETag = etag.String
	record.ContentSHA256 = contentSHA.String
	record.Metadata = map[string]string{}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &record.Metadata)
	}
	return record, err
}

func scanRecognitionRun(row scanner) (asset.RecognitionRun, error) {
	var record asset.RecognitionRun
	var providerName, policyReason, lastError sql.NullString
	var finishedAt, debugExpiresAt sql.NullTime
	var debugPayload []byte
	err := row.Scan(
		&record.ID,
		&record.AssetID,
		&record.Stage,
		&providerName,
		&record.Status,
		&policyReason,
		&record.Attempts,
		&lastError,
		&record.StartedAt,
		&finishedAt,
		&debugExpiresAt,
		&debugPayload,
	)
	record.ProviderName = providerName.String
	record.PolicyReason = policyReason.String
	record.LastError = lastError.String
	record.DebugPayload = map[string]any{}
	if finishedAt.Valid {
		value := finishedAt.Time.UTC()
		record.FinishedAt = &value
	}
	if debugExpiresAt.Valid {
		value := debugExpiresAt.Time.UTC()
		record.DebugExpiresAt = &value
	}
	if len(debugPayload) > 0 {
		_ = json.Unmarshal(debugPayload, &record.DebugPayload)
	}
	return record, err
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func nullTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC()
}

func expectRowsAffected(result sql.Result, notFound error) error {
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return notFound
	}
	return nil
}

func defaultStringMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return map[string]string{}
	}
	return value
}

func vectorLiteral(values []float32) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%g", value))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func parseVector(value string) []float32 {
	trimmed := strings.Trim(strings.TrimSpace(value), "[]")
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, ",")
	vector := make([]float32, 0, len(parts))
	for _, part := range parts {
		var value float32
		if _, err := fmt.Sscanf(strings.TrimSpace(part), "%f", &value); err != nil {
			return nil
		}
		vector = append(vector, value)
	}
	return vector
}
