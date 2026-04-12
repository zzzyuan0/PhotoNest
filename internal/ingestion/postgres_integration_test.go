package ingestion_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/photonest/photonest/internal/asset"
	"github.com/photonest/photonest/internal/discovery"
	"github.com/photonest/photonest/internal/ingestion"
	"github.com/photonest/photonest/internal/platform/config"
	"github.com/photonest/photonest/internal/platform/persistence"
	"github.com/photonest/photonest/internal/provider/storage"
	"github.com/photonest/photonest/internal/testsupport"
)

func TestPostgresRepositoryPersistsConfirmedImportAcrossRestart(t *testing.T) {
	dbCfg, cleanup := testsupport.NewPostgresDatabase(t)
	defer cleanup()

	db := testsupport.OpenPostgres(t, dbCfg)
	repository := persistence.NewPostgresRepository(db)

	provider := storage.NewMemoryProvider("primary-memory", "photonest-main", "libraries/main")
	service, err := ingestion.NewService(ingestion.ServiceConfig{
		Repository: repository,
		Provider:   provider,
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

	ctx := context.Background()
	payload := mustPNG(t)
	contentSHA := ingestion.SHA256Hex(payload)

	session, err := service.CreateSession(ctx, ingestion.CreateSessionInput{
		LibraryID:         "11111111-1111-1111-1111-111111111111",
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
		FileName:      "persisted.png",
		ContentType:   "image/png",
		ContentLength: int64(len(payload)),
		ContentSHA256: contentSHA,
	})
	if err != nil {
		t.Fatalf("create upload ticket: %v", err)
	}

	putWithPlan(t, ctx, provider, ticket.Plan, payload, "image/png")

	confirmed, err := service.ConfirmUpload(ctx, ingestion.ConfirmUploadInput{
		SessionID:     session.ID,
		LibraryID:     session.LibraryID,
		ObjectKey:     ticket.Plan.ObjectKey,
		ContentLength: int64(len(payload)),
		ContentSHA256: contentSHA,
	})
	if err != nil {
		t.Fatalf("confirm upload: %v", err)
	}
	if confirmed.Asset.ProcessingStage != asset.StageDerivativesReady {
		t.Fatalf("expected derivatives_ready after confirm, got %s", confirmed.Asset.ProcessingStage)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close first repository connection: %v", err)
	}

	restartedDB := testsupport.OpenPostgres(t, dbCfg)
	defer restartedDB.Close()
	restartedRepository := persistence.NewPostgresRepository(restartedDB)

	reloadedSession, err := restartedRepository.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatalf("reload session after restart: %v", err)
	}
	if reloadedSession.Status != ingestion.SessionConfirmed {
		t.Fatalf("expected session to stay confirmed after restart, got %s", reloadedSession.Status)
	}

	items, err := restartedRepository.ListItemsBySession(ctx, session.ID)
	if err != nil {
		t.Fatalf("list session items after restart: %v", err)
	}
	if len(items) != 1 || items[0].AssetID != confirmed.Asset.ID {
		t.Fatalf("expected one persisted import item bound to asset %s, got %+v", confirmed.Asset.ID, items)
	}

	record, err := restartedRepository.GetAsset(ctx, confirmed.Asset.ID)
	if err != nil {
		t.Fatalf("load asset after restart: %v", err)
	}
	if record.ID != confirmed.Asset.ID {
		t.Fatalf("expected asset %s, got %s", confirmed.Asset.ID, record.ID)
	}

	discoveryService, err := discovery.NewService(discovery.ServiceConfig{
		Repository:  restartedRepository,
		Storage:     provider,
		DownloadTTL: 5 * time.Minute,
		TokenKey:    "test-token",
	})
	if err != nil {
		t.Fatalf("new discovery service: %v", err)
	}

	timeline, err := discoveryService.ListTimeline(ctx, session.LibraryID, 10)
	if err != nil {
		t.Fatalf("list timeline after restart: %v", err)
	}
	if len(timeline) != 1 || timeline[0].Asset.ID != confirmed.Asset.ID {
		t.Fatalf("expected persisted asset in timeline, got %+v", timeline)
	}

	detail, err := discoveryService.GetAssetDetail(ctx, session.LibraryID, confirmed.Asset.ID)
	if err != nil {
		t.Fatalf("get asset detail after restart: %v", err)
	}
	if detail.Asset.ID != confirmed.Asset.ID {
		t.Fatalf("expected detail for asset %s, got %s", confirmed.Asset.ID, detail.Asset.ID)
	}
}

func putWithPlan(t *testing.T, ctx context.Context, provider storage.Provider, plan ingestion.UploadPlan, payload []byte, contentType string) {
	t.Helper()

	metadata := mapFromPresignHeaders(plan.Headers)
	if _, err := provider.PutObject(ctx, storage.PutObjectInput{
		Ref: storage.ObjectRef{
			Key: plan.ObjectKey,
		},
		Body:          bytes.NewReader(payload),
		ContentType:   contentType,
		ContentLength: int64(len(payload)),
		Metadata:      metadata,
	}); err != nil {
		t.Fatalf("put object with plan: %v", err)
	}
}

func mapFromPresignHeaders(headers map[string]string) map[string]string {
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

func mustPNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 48, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			img.Set(x, y, color.RGBA{R: 0x28, G: 0x66 + uint8((x+y)%3), B: 0xa3, A: 255})
		}
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buffer.Bytes()
}
