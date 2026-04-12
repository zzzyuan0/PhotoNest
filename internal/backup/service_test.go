package backup_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/photonest/photonest/internal/backup"
	"github.com/photonest/photonest/internal/discovery"
	"github.com/photonest/photonest/internal/ingestion"
	"github.com/photonest/photonest/internal/platform/config"
	"github.com/photonest/photonest/internal/platform/telemetry"
	"github.com/photonest/photonest/internal/provider/storage"
)

const testLibraryID = "11111111-1111-1111-1111-111111111111"

func TestServiceCopiesAssetsAndBuildsExportRecoveryPlan(t *testing.T) {
	ctx := context.Background()
	store := ingestion.NewMemoryStore()
	primary := storage.NewMemoryProvider("primary-memory", "photonest-main", "libraries/main")
	secondary := storage.NewMemoryProvider("backup-memory", "photonest-backup", "archives/secondary")
	collector := telemetry.NewCollector()

	ingestionService, err := ingestion.NewService(ingestion.ServiceConfig{
		Repository: store,
		Provider:   primary,
		ProviderConfig: config.ObjectStorageProviderConfig{
			Name:             "primary-memory",
			Bucket:           "photonest-main",
			KeyPrefix:        "libraries/main",
			UploadPresignTTL: storage.MaxUploadTTL,
		},
	})
	if err != nil {
		t.Fatalf("new ingestion service: %v", err)
	}

	accepted := uploadAsset(t, ctx, ingestionService, primary, "family-trip.png")

	service, err := backup.NewService(backup.ServiceConfig{
		Repository:     store,
		PrimaryStorage: primary,
		ArtifactStore:  primary,
		BackupProviders: []backup.ConfiguredProvider{
			{
				Provider:    secondary,
				Name:        "backup-memory",
				BucketName:  "photonest-backup",
				KeyPrefix:   "archives/secondary",
				PrivateRead: true,
			},
		},
		Telemetry: collector,
	})
	if err != nil {
		t.Fatalf("new backup service: %v", err)
	}

	record, err := service.CopyAsset(ctx, testLibraryID, accepted.Asset.ID)
	if err != nil {
		t.Fatalf("copy asset: %v", err)
	}
	if record.BackupStatus != "verified" {
		t.Fatalf("expected verified backup status, got %s", record.BackupStatus)
	}

	refs, err := store.ListObjectReferencesByAsset(ctx, accepted.Asset.ID)
	if err != nil {
		t.Fatalf("list object refs: %v", err)
	}
	foundBackupRef := false
	for _, ref := range refs {
		if ref.ProviderName == "backup-memory" && ref.Purpose == "backup" {
			foundBackupRef = true
			break
		}
	}
	if !foundBackupRef {
		t.Fatal("expected backup object reference to be persisted")
	}

	album, err := store.CreateAlbum(ctx, discovery.Album{
		LibraryID:   testLibraryID,
		Slug:        "family-picks",
		DisplayName: "家庭精选",
		Kind:        discovery.AlbumKindCurated,
	})
	if err != nil {
		t.Fatalf("create album: %v", err)
	}
	if err := store.AddAssetToAlbum(ctx, album.ID, accepted.Asset.ID); err != nil {
		t.Fatalf("add asset to album: %v", err)
	}

	job, err := service.CreateExport(ctx, backup.CreateExportInput{
		LibraryID: testLibraryID,
		Scope:     backup.ExportScopeAlbum,
		AlbumID:   album.ID,
	})
	if err != nil {
		t.Fatalf("create export: %v", err)
	}
	if job.Status != "ready" || job.AssetCount != 1 {
		t.Fatalf("unexpected export job %+v", job)
	}
	if !strings.Contains(job.ArchiveURL, "memory://") || !strings.Contains(job.RedactedManifestURL, "memory://") {
		t.Fatalf("expected short-lived memory URLs, got %+v", job)
	}
	if job.RecoveryPlan.AssetCount != 1 || job.RecoveryPlan.ObjectCount == 0 {
		t.Fatalf("unexpected recovery plan %+v", job.RecoveryPlan)
	}
	snapshots := collector.Snapshots()
	if !hasMetric(snapshots, "backup.lag") || !hasMetric(snapshots, "export.generated") {
		t.Fatalf("expected backup telemetry snapshots, got %+v", snapshots)
	}
}

func uploadAsset(
	t *testing.T,
	ctx context.Context,
	service *ingestion.Service,
	provider storage.Provider,
	fileName string,
) ingestion.ConfirmResult {
	t.Helper()

	payload := samplePNG(t)
	contentSHA := ingestion.SHA256Hex(payload)

	session, err := service.CreateSession(ctx, ingestion.CreateSessionInput{
		LibraryID:         testLibraryID,
		Source:            ingestion.SourceWebUpload,
		ExpectedItemCount: 1,
		CreatedBy:         "tester",
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	ticket, err := service.CreateUploadTicket(ctx, ingestion.CreateUploadTicketInput{
		SessionID:     session.ID,
		LibraryID:     session.LibraryID,
		FileName:      fileName,
		ContentType:   "image/png",
		ContentLength: int64(len(payload)),
		ContentSHA256: contentSHA,
	})
	if err != nil {
		t.Fatalf("create upload ticket: %v", err)
	}
	if _, err := provider.PutObject(ctx, storage.PutObjectInput{
		Ref: storage.ObjectRef{
			Key: ticket.Plan.ObjectKey,
		},
		Body:          bytes.NewReader(payload),
		ContentType:   "image/png",
		ContentLength: int64(len(payload)),
		Metadata:      mapFromUploadHeaders(ticket.Plan.Headers),
	}); err != nil {
		t.Fatalf("put object: %v", err)
	}
	result, err := service.ConfirmUpload(ctx, ingestion.ConfirmUploadInput{
		SessionID:     session.ID,
		LibraryID:     session.LibraryID,
		ObjectKey:     ticket.Plan.ObjectKey,
		ContentLength: int64(len(payload)),
		ContentSHA256: contentSHA,
	})
	if err != nil {
		t.Fatalf("confirm upload: %v", err)
	}
	return result
}

func samplePNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	for y := 0; y < 40; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{R: 0x25, G: 0x61 + uint8((x+y)%5), B: 0xa8, A: 255})
		}
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encode sample png: %v", err)
	}
	return buffer.Bytes()
}

func mapFromUploadHeaders(headers map[string]string) map[string]string {
	metadata := map[string]string{}
	for name, value := range headers {
		lowered := strings.ToLower(name)
		switch {
		case strings.HasPrefix(lowered, "x-amz-meta-"):
			metadata[strings.TrimPrefix(lowered, "x-amz-meta-")] = value
		case strings.HasPrefix(lowered, "x-cos-meta-"):
			metadata[strings.TrimPrefix(lowered, "x-cos-meta-")] = value
		}
	}
	return metadata
}

func hasMetric(snapshots []telemetry.Snapshot, metric string) bool {
	for _, snapshot := range snapshots {
		if snapshot.Metric == metric {
			return true
		}
	}
	return false
}
