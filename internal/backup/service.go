package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/photonest/photonest/internal/asset"
	"github.com/photonest/photonest/internal/platform/telemetry"
	"github.com/photonest/photonest/internal/provider/storage"
)

const defaultDownloadTTL = 5 * time.Minute

type Target struct {
	ID           string
	ProviderName string
	BucketName   string
	Endpoint     string
	KeyPrefix    string
	PrivateRead  bool
	CreatedAt    time.Time
}

type Record struct {
	ID                      string
	AssetID                 string
	TargetID                string
	SourceObjectReferenceID string
	BackupObjectKey         string
	BackupETag              string
	ChecksumSHA256          string
	Status                  string
	VerifiedAt              *time.Time
	LastError               string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

type ConfiguredProvider struct {
	Provider    storage.Provider
	Name        string
	BucketName  string
	Endpoint    string
	KeyPrefix   string
	PrivateRead bool
}

type Manifest struct {
	Version    string          `json:"version"`
	ExportID   string          `json:"exportId"`
	LibraryID  string          `json:"libraryId"`
	Scope      string          `json:"scope"`
	CreatedAt  time.Time       `json:"createdAt"`
	AssetCount int             `json:"assetCount"`
	Assets     []ManifestAsset `json:"assets"`
}

type ManifestAsset struct {
	AssetID           string           `json:"assetId"`
	MediaType         string           `json:"mediaType"`
	OriginalFilename  string           `json:"originalFilename"`
	ContentSHA256     string           `json:"contentSha256"`
	ImportedAt        time.Time        `json:"importedAt"`
	CapturedAt        *time.Time       `json:"capturedAt,omitempty"`
	TimelineAt        time.Time        `json:"timelineAt"`
	LocationLabel     string           `json:"locationLabel,omitempty"`
	CaptionText       string           `json:"captionText,omitempty"`
	OCRText           string           `json:"ocrText,omitempty"`
	Tags              []string         `json:"tags,omitempty"`
	ProcessingStage   string           `json:"processingStage"`
	BackupStatus      string           `json:"backupStatus"`
	ObjectReferences  []ManifestObject `json:"objectReferences"`
	RecognitionStatus string           `json:"recognitionStatus,omitempty"`
}

type ManifestObject struct {
	Purpose       string `json:"purpose"`
	ProviderName  string `json:"providerName,omitempty"`
	BucketName    string `json:"bucketName,omitempty"`
	ObjectKey     string `json:"objectKey,omitempty"`
	ContentLength int64  `json:"contentLength"`
	ContentSHA256 string `json:"contentSha256,omitempty"`
	ETag          string `json:"etag,omitempty"`
}

type RecoveryPlan struct {
	AssetCount          int      `json:"assetCount"`
	ObjectCount         int      `json:"objectCount"`
	BackupVerifiedCount int      `json:"backupVerifiedCount"`
	RequiredMetadata    []string `json:"requiredMetadata"`
	Warnings            []string `json:"warnings,omitempty"`
}

type ExportScope string

const (
	ExportScopeLibrary   ExportScope = "library"
	ExportScopeAlbum     ExportScope = "album"
	ExportScopeDateRange ExportScope = "date-range"
)

type CreateExportInput struct {
	LibraryID string
	Scope     ExportScope
	AlbumID   string
	DateFrom  *time.Time
	DateTo    *time.Time
}

type ExportJob struct {
	ID                  string       `json:"id"`
	LibraryID           string       `json:"libraryId"`
	Scope               ExportScope  `json:"scope"`
	Status              string       `json:"status"`
	AssetCount          int          `json:"assetCount"`
	ArchiveURL          string       `json:"archiveUrl,omitempty"`
	RedactedManifestURL string       `json:"redactedManifestUrl,omitempty"`
	ExpiresAt           time.Time    `json:"expiresAt"`
	CreatedAt           time.Time    `json:"createdAt"`
	RecoveryPlan        RecoveryPlan `json:"recoveryPlan"`
}

type Repository interface {
	GetAsset(ctx context.Context, assetID string) (asset.Asset, error)
	SaveAsset(ctx context.Context, record asset.Asset) error
	ListAssetsByLibrary(ctx context.Context, libraryID string) ([]asset.Asset, error)
	ListObjectReferencesByAsset(ctx context.Context, assetID string) ([]asset.ObjectReference, error)
	CreateObjectReference(ctx context.Context, ref asset.ObjectReference) (asset.ObjectReference, error)
}

type targetRepository interface {
	GetBackupTargetByProvider(ctx context.Context, providerName string) (Target, bool, error)
	SaveBackupTarget(ctx context.Context, target Target) (Target, error)
	SaveBackupRecord(ctx context.Context, record Record) (Record, error)
	ListBackupRecordsByAsset(ctx context.Context, assetID string) ([]Record, error)
}

type albumAssetRepository interface {
	ListAssetIDsByAlbum(ctx context.Context, albumID string) ([]string, error)
}

type ServiceConfig struct {
	Repository      Repository
	PrimaryStorage  storage.Provider
	ArtifactStore   storage.Provider
	BackupProviders []ConfiguredProvider
	DownloadTTL     time.Duration
	Telemetry       telemetry.Recorder
	Now             func() time.Time
}

type Service struct {
	repository    Repository
	targets       targetRepository
	albums        albumAssetRepository
	primary       storage.Provider
	artifacts     storage.Provider
	backups       map[string]ConfiguredProvider
	downloadTTL   time.Duration
	telemetry     telemetry.Recorder
	now           func() time.Time
}

func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Repository == nil {
		return nil, fmt.Errorf("repository is required")
	}
	if cfg.PrimaryStorage == nil {
		return nil, fmt.Errorf("primary storage is required")
	}
	if cfg.ArtifactStore == nil {
		cfg.ArtifactStore = cfg.PrimaryStorage
	}
	if cfg.DownloadTTL <= 0 {
		cfg.DownloadTTL = defaultDownloadTTL
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	backups := make(map[string]ConfiguredProvider, len(cfg.BackupProviders))
	for _, provider := range cfg.BackupProviders {
		if provider.Provider == nil {
			continue
		}
		name := strings.TrimSpace(provider.Name)
		if name == "" {
			name = provider.Provider.Name()
		}
		provider.Name = name
		backups[name] = provider
	}

	service := &Service{
		repository:  cfg.Repository,
		primary:     cfg.PrimaryStorage,
		artifacts:   cfg.ArtifactStore,
		backups:     backups,
		downloadTTL: cfg.DownloadTTL,
		telemetry:   cfg.Telemetry,
		now:         cfg.Now,
	}
	if repo, ok := cfg.Repository.(targetRepository); ok {
		service.targets = repo
	}
	if repo, ok := cfg.Repository.(albumAssetRepository); ok {
		service.albums = repo
	}
	return service, nil
}

func (s *Service) CopyAsset(ctx context.Context, libraryID string, assetID string) (asset.Asset, error) {
	record, err := s.repository.GetAsset(ctx, assetID)
	if err != nil {
		return asset.Asset{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(record.LibraryID), strings.TrimSpace(libraryID)) {
		return asset.Asset{}, fmt.Errorf("asset does not belong to library")
	}
	if len(s.backups) == 0 {
		return record, nil
	}

	refs, err := s.repository.ListObjectReferencesByAsset(ctx, assetID)
	if err != nil {
		return asset.Asset{}, err
	}

	backupRefs := make([]asset.ObjectReference, 0, len(refs))
	for _, ref := range refs {
		if ref.Purpose == asset.ObjectPurposeBackup {
			continue
		}
		backupRefs = append(backupRefs, ref)
	}
	if len(backupRefs) == 0 {
		return record, fmt.Errorf("no source object references found for backup")
	}

	var firstErr error
	for _, provider := range s.backups {
		target, err := s.ensureTarget(ctx, provider)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, ref := range backupRefs {
			if err := s.copyReference(ctx, record, ref, target, provider); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}

	if firstErr != nil {
		record.BackupStatus = "failed"
		_ = s.repository.SaveAsset(ctx, record)
		s.recordTelemetry(telemetry.Snapshot{
			Metric: "backup.lag",
			Labels: map[string]string{
				"asset_id": record.ID,
			},
			Data: map[string]any{
				"status": "failed",
			},
		})
		return record, firstErr
	}

	record.BackupStatus = "verified"
	if err := s.repository.SaveAsset(ctx, record); err != nil {
		return asset.Asset{}, err
	}
	s.recordTelemetry(telemetry.Snapshot{
		Metric: "backup.lag",
		Labels: map[string]string{
			"asset_id": record.ID,
		},
		Data: map[string]any{
			"status": "verified",
		},
	})
	return record, nil
}

func (s *Service) CreateExport(ctx context.Context, input CreateExportInput) (ExportJob, error) {
	records, err := s.resolveAssets(ctx, input)
	if err != nil {
		return ExportJob{}, err
	}
	now := s.now().UTC()
	jobID := exportID(now)

	manifest, err := s.buildManifest(ctx, jobID, input, records, false)
	if err != nil {
		return ExportJob{}, err
	}
	redactedManifest, err := s.buildManifest(ctx, jobID, input, records, true)
	if err != nil {
		return ExportJob{}, err
	}
	recoveryPlan, err := PlanRecovery(manifest)
	if err != nil {
		return ExportJob{}, err
	}

	archiveBytes, err := s.buildArchive(ctx, jobID, manifest, redactedManifest)
	if err != nil {
		return ExportJob{}, err
	}

	archiveKey := path.Join("exports", jobID, "photonest-export-"+jobID+".zip")
	archiveInfo, err := s.artifacts.PutObject(ctx, storage.PutObjectInput{
		Ref: storage.ObjectRef{
			Key: archiveKey,
		},
		Body:          bytes.NewReader(archiveBytes),
		ContentType:   "application/zip",
		ContentLength: int64(len(archiveBytes)),
		Metadata: map[string]string{
			"photonest-export-id": jobID,
			"photonest-scope":     string(input.Scope),
		},
	})
	if err != nil {
		return ExportJob{}, err
	}

	redactedKey := path.Join("exports", jobID, "manifest.redacted.json")
	redactedInfo, err := s.artifacts.PutObject(ctx, storage.PutObjectInput{
		Ref: storage.ObjectRef{
			Key: redactedKey,
		},
		Body:          bytes.NewReader(redactedManifest),
		ContentType:   "application/json",
		ContentLength: int64(len(redactedManifest)),
		Metadata: map[string]string{
			"photonest-export-id": jobID,
			"photonest-variant":   "redacted",
		},
	})
	if err != nil {
		return ExportJob{}, err
	}

	archiveGrant, err := s.artifacts.PresignDownload(ctx, storage.PresignDownloadInput{
		Ref:       archiveInfo.Ref,
		ExpiresIn: s.downloadTTL,
	})
	if err != nil {
		return ExportJob{}, err
	}
	redactedGrant, err := s.artifacts.PresignDownload(ctx, storage.PresignDownloadInput{
		Ref:       redactedInfo.Ref,
		ExpiresIn: s.downloadTTL,
	})
	if err != nil {
		return ExportJob{}, err
	}

	job := ExportJob{
		ID:                  jobID,
		LibraryID:           input.LibraryID,
		Scope:               input.Scope,
		Status:              "ready",
		AssetCount:          len(records),
		ArchiveURL:          archiveGrant.URL,
		RedactedManifestURL: redactedGrant.URL,
		ExpiresAt:           archiveGrant.ExpiresAt.UTC(),
		CreatedAt:           now,
		RecoveryPlan:        recoveryPlan,
	}
	s.recordTelemetry(telemetry.Snapshot{
		Metric: "export.generated",
		Labels: map[string]string{
			"library_id": input.LibraryID,
			"scope":      string(input.Scope),
		},
		Data: map[string]any{
			"asset_count": len(records),
		},
	})
	return job, nil
}

func PlanRecovery(manifest []byte) (RecoveryPlan, error) {
	var decoded Manifest
	if err := json.Unmarshal(manifest, &decoded); err != nil {
		return RecoveryPlan{}, fmt.Errorf("decode manifest: %w", err)
	}

	objectCount := 0
	backupVerifiedCount := 0
	warnings := make([]string, 0)
	for _, item := range decoded.Assets {
		objectCount += len(item.ObjectReferences)
		if strings.EqualFold(strings.TrimSpace(item.BackupStatus), "verified") {
			backupVerifiedCount++
		}
		hasOriginal := false
		for _, ref := range item.ObjectReferences {
			if ref.Purpose == string(asset.ObjectPurposeOriginal) {
				hasOriginal = true
				break
			}
		}
		if !hasOriginal {
			warnings = append(warnings, "asset "+item.AssetID+" 缺少 original 对象引用")
		}
	}

	required := []string{
		"assetId",
		"contentSha256",
		"mediaType",
		"timelineAt",
		"objectReferences.purpose",
	}

	return RecoveryPlan{
		AssetCount:          decoded.AssetCount,
		ObjectCount:         objectCount,
		BackupVerifiedCount: backupVerifiedCount,
		RequiredMetadata:    required,
		Warnings:            warnings,
	}, nil
}

func (s *Service) resolveAssets(ctx context.Context, input CreateExportInput) ([]asset.Asset, error) {
	records, err := s.repository.ListAssetsByLibrary(ctx, input.LibraryID)
	if err != nil {
		return nil, err
	}
	sortAssets(records)

	switch input.Scope {
	case ExportScopeLibrary:
		return records, nil
	case ExportScopeDateRange:
		filtered := make([]asset.Asset, 0, len(records))
		for _, record := range records {
			bestAt := bestTimeline(record)
			if input.DateFrom != nil && bestAt.Before(input.DateFrom.UTC()) {
				continue
			}
			if input.DateTo != nil && bestAt.After(input.DateTo.UTC()) {
				continue
			}
			filtered = append(filtered, record)
		}
		return filtered, nil
	case ExportScopeAlbum:
		if s.albums == nil {
			return nil, fmt.Errorf("album export is not configured")
		}
		assetIDs, err := s.albums.ListAssetIDsByAlbum(ctx, input.AlbumID)
		if err != nil {
			return nil, err
		}
		filtered := make([]asset.Asset, 0, len(assetIDs))
		for _, assetID := range assetIDs {
			record, err := s.repository.GetAsset(ctx, assetID)
			if err != nil {
				return nil, err
			}
			if !strings.EqualFold(strings.TrimSpace(record.LibraryID), strings.TrimSpace(input.LibraryID)) {
				continue
			}
			filtered = append(filtered, record)
		}
		sortAssets(filtered)
		return filtered, nil
	default:
		return nil, fmt.Errorf("unsupported export scope %q", input.Scope)
	}
}

func (s *Service) buildManifest(ctx context.Context, exportID string, input CreateExportInput, records []asset.Asset, redacted bool) ([]byte, error) {
	manifest := Manifest{
		Version:    "1.0",
		ExportID:   exportID,
		LibraryID:  input.LibraryID,
		Scope:      string(input.Scope),
		CreatedAt:  s.now().UTC(),
		AssetCount: len(records),
		Assets:     make([]ManifestAsset, 0, len(records)),
	}

	for _, record := range records {
		refs, err := s.repository.ListObjectReferencesByAsset(ctx, record.ID)
		if err != nil {
			return nil, err
		}
		objects := make([]ManifestObject, 0, len(refs))
		for _, ref := range refs {
			object := ManifestObject{
				Purpose:       string(ref.Purpose),
				ContentLength: ref.ContentLength,
				ContentSHA256: ref.ContentSHA256,
				ETag:          ref.ETag,
			}
			if !redacted {
				object.ProviderName = ref.ProviderName
				object.BucketName = ref.Bucket
				object.ObjectKey = ref.ObjectKey
			}
			objects = append(objects, object)
		}
		item := ManifestAsset{
			AssetID:           record.ID,
			MediaType:         record.MediaType,
			OriginalFilename:  record.OriginalFilename,
			ContentSHA256:     record.ContentSHA256,
			ImportedAt:        record.ImportedAt.UTC(),
			CapturedAt:        cloneTimePtr(record.CapturedAt),
			TimelineAt:        bestTimeline(record),
			Tags:              slices.Clone(record.Tags),
			ProcessingStage:   strings.ReplaceAll(string(record.ProcessingStage), "_", "-"),
			BackupStatus:      record.BackupStatus,
			ObjectReferences:  objects,
			RecognitionStatus: record.RecognitionStatusNote,
		}
		if !redacted {
			item.LocationLabel = record.LocationLabel
			item.CaptionText = record.CaptionText
			item.OCRText = record.OCRText
		}
		manifest.Assets = append(manifest.Assets, item)
	}

	return json.MarshalIndent(manifest, "", "  ")
}

func (s *Service) buildArchive(ctx context.Context, exportID string, manifest []byte, redactedManifest []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)

	if err := addArchiveFile(writer, "manifest.json", manifest); err != nil {
		return nil, err
	}
	if err := addArchiveFile(writer, "manifest.redacted.json", redactedManifest); err != nil {
		return nil, err
	}

	var decoded Manifest
	if err := json.Unmarshal(manifest, &decoded); err != nil {
		return nil, err
	}
	for _, item := range decoded.Assets {
		for _, ref := range item.ObjectReferences {
			if ref.Purpose != string(asset.ObjectPurposeOriginal) || ref.ObjectKey == "" {
				continue
			}
			body, err := s.primary.GetObject(ctx, storage.ObjectRef{
				Bucket: ref.BucketName,
				Key:    ref.ObjectKey,
			})
			if err != nil {
				return nil, err
			}
			payload, err := io.ReadAll(body)
			_ = body.Close()
			if err != nil {
				return nil, err
			}
			fileName := path.Join("assets", item.AssetID, safeFileName(item.OriginalFilename))
			if err := addArchiveFile(writer, fileName, payload); err != nil {
				return nil, err
			}
		}
	}

	if err := addArchiveFile(writer, path.Join("recovery", exportID+".txt"), []byte("使用 manifest.json 规划恢复即可重建统一资产元数据。")); err != nil {
		return nil, err
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (s *Service) ensureTarget(ctx context.Context, provider ConfiguredProvider) (Target, error) {
	if s.targets == nil {
		return Target{}, fmt.Errorf("backup target repository is not configured")
	}
	target, found, err := s.targets.GetBackupTargetByProvider(ctx, provider.Name)
	if err != nil {
		return Target{}, err
	}
	if found {
		return target, nil
	}
	return s.targets.SaveBackupTarget(ctx, Target{
		ProviderName: provider.Name,
		BucketName:   provider.BucketName,
		Endpoint:     provider.Endpoint,
		KeyPrefix:    provider.KeyPrefix,
		PrivateRead:  provider.PrivateRead,
		CreatedAt:    s.now().UTC(),
	})
}

func (s *Service) copyReference(ctx context.Context, record asset.Asset, ref asset.ObjectReference, target Target, provider ConfiguredProvider) error {
	existing, err := s.targets.ListBackupRecordsByAsset(ctx, record.ID)
	if err != nil {
		return err
	}
	for _, item := range existing {
		if item.TargetID == target.ID && item.SourceObjectReferenceID == ref.ID && strings.EqualFold(item.Status, "verified") {
			return nil
		}
	}

	body, err := s.primary.GetObject(ctx, storage.ObjectRef{
		Bucket: ref.Bucket,
		Key:    ref.ObjectKey,
	})
	if err != nil {
		return err
	}
	payload, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil {
		return err
	}
	checksum := ref.ContentSHA256
	if checksum == "" {
		sum := sha256.Sum256(payload)
		checksum = hex.EncodeToString(sum[:])
	}

	backupKey := path.Join(strings.Trim(provider.KeyPrefix, "/"), record.ID, string(ref.Purpose), path.Base(ref.ObjectKey))
	now := s.now().UTC()
	backupRecord, err := s.targets.SaveBackupRecord(ctx, Record{
		AssetID:                 record.ID,
		TargetID:                target.ID,
		SourceObjectReferenceID: ref.ID,
		BackupObjectKey:         backupKey,
		Status:                  "copying",
		ChecksumSHA256:          checksum,
		CreatedAt:               now,
		UpdatedAt:               now,
	})
	if err != nil {
		return err
	}

	info, err := provider.Provider.PutObject(ctx, storage.PutObjectInput{
		Ref: storage.ObjectRef{
			Bucket: provider.BucketName,
			Key:    backupKey,
		},
		Body:          bytes.NewReader(payload),
		ContentType:   contentTypeForPurpose(ref.Purpose, record.MediaType),
		ContentLength: int64(len(payload)),
		Metadata: map[string]string{
			"photonest-asset-id": record.ID,
			"photonest-purpose":  string(ref.Purpose),
			"photonest-backup":   "true",
		},
	})
	if err != nil {
		backupRecord.Status = "failed"
		backupRecord.LastError = err.Error()
		backupRecord.UpdatedAt = s.now().UTC()
		_, _ = s.targets.SaveBackupRecord(ctx, backupRecord)
		return err
	}

	head, err := provider.Provider.HeadObject(ctx, info.Ref)
	if err != nil {
		return err
	}
	if head.ContentLength != int64(len(payload)) {
		return fmt.Errorf("backup object length mismatch for %s", record.ID)
	}

	if _, err := s.repository.CreateObjectReference(ctx, asset.ObjectReference{
		AssetID:       record.ID,
		ProviderName:  provider.Name,
		Bucket:        head.Ref.Bucket,
		ObjectKey:     head.Ref.Key,
		ObjectVersion: head.VersionID,
		ETag:          head.ETag,
		Purpose:       asset.ObjectPurposeBackup,
		ContentLength: head.ContentLength,
		ContentSHA256: checksum,
		Metadata: map[string]string{
			"backup_target_id": target.ID,
			"source_ref_id":    ref.ID,
		},
		Immutable: true,
		CreatedAt: s.now().UTC(),
	}); err != nil {
		return err
	}

	verifiedAt := s.now().UTC()
	backupRecord.BackupETag = head.ETag
	backupRecord.ChecksumSHA256 = checksum
	backupRecord.Status = "verified"
	backupRecord.VerifiedAt = &verifiedAt
	backupRecord.LastError = ""
	backupRecord.UpdatedAt = verifiedAt
	_, err = s.targets.SaveBackupRecord(ctx, backupRecord)
	return err
}

func (s *Service) recordTelemetry(snapshot telemetry.Snapshot) {
	if s.telemetry == nil {
		return
	}
	s.telemetry.Record(snapshot)
}

func addArchiveFile(writer *zip.Writer, name string, payload []byte) error {
	file, err := writer.Create(name)
	if err != nil {
		return err
	}
	_, err = file.Write(payload)
	return err
}

func bestTimeline(record asset.Asset) time.Time {
	switch {
	case !record.TimelineAt.IsZero():
		return record.TimelineAt.UTC()
	case record.CapturedAt != nil:
		return record.CapturedAt.UTC()
	default:
		return record.ImportedAt.UTC()
	}
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func safeFileName(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "asset.bin"
	}
	return strings.ReplaceAll(trimmed, "/", "-")
}

func sortAssets(records []asset.Asset) {
	slices.SortFunc(records, func(left asset.Asset, right asset.Asset) int {
		leftAt := bestTimeline(left)
		rightAt := bestTimeline(right)
		switch {
		case leftAt.After(rightAt):
			return -1
		case leftAt.Before(rightAt):
			return 1
		default:
			return strings.Compare(left.ID, right.ID)
		}
	})
}

func exportID(now time.Time) string {
	return strings.ReplaceAll(now.UTC().Format("20060102T150405.000000000"), ".", "")
}

func contentTypeForPurpose(purpose asset.ObjectPurpose, mediaType string) string {
	if purpose == asset.ObjectPurposeOriginal && strings.TrimSpace(mediaType) != "" {
		return mediaType
	}
	return "application/octet-stream"
}
