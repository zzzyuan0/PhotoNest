package enrichment

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/photonest/photonest/internal/asset"
	"github.com/photonest/photonest/internal/ingestion"
	"github.com/photonest/photonest/internal/platform/config"
	"github.com/photonest/photonest/internal/platform/persistence"
	providerai "github.com/photonest/photonest/internal/provider/ai"
	"github.com/photonest/photonest/internal/provider/storage"
	"github.com/photonest/photonest/internal/testsupport"
)

func TestRedisQueueSupportsRestartRecoveryAndDuplicateRetry(t *testing.T) {
	dbCfg, dbCleanup := testsupport.NewPostgresDatabase(t)
	defer dbCleanup()

	redisCfg, redisCleanup := testsupport.NewRedisQueueConfig(t)
	defer redisCleanup()

	db := testsupport.OpenPostgres(t, dbCfg)
	defer db.Close()
	repository := persistence.NewPostgresRepository(db)

	provider := storage.NewMemoryProvider("primary-memory", "photonest-main", "libraries/main")
	ingestionService, err := ingestion.NewService(ingestion.ServiceConfig{
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

	queue := persistence.NewRedisQueue(redisCfg)
	defer queue.Close()

	providers := []providerai.Provider{
		providerai.NewDeterministicProvider(
			"caption-ocr",
			providerai.BoundaryLocalSidecar,
			[]providerai.Capability{providerai.CapabilityCaption, providerai.CapabilityOCR},
			"test-model",
		),
		&flakyEmbeddingProvider{name: "embedding-flaky", failTimes: 1},
	}
	enrichmentService, err := NewService(ServiceConfig{
		Repository:          repository,
		Storage:             provider,
		AIProviders:         providers,
		Queue:               queue,
		DownloadTTL:         5 * time.Minute,
		DebugRetention:      24 * time.Hour,
		RetainProviderDebug: true,
	})
	if err != nil {
		t.Fatalf("new enrichment service: %v", err)
	}

	ctx := context.Background()
	accepted := uploadAsset(t, ctx, ingestionService, provider, "queue-restart.png", nil)

	if err := enrichmentService.QueueAsset(ctx, accepted.Asset.ID); err != nil {
		t.Fatalf("queue asset: %v", err)
	}

	restartedQueue := persistence.NewRedisQueue(redisCfg)
	defer restartedQueue.Close()

	restartedService, err := NewService(ServiceConfig{
		Repository:          repository,
		Storage:             provider,
		AIProviders:         providers,
		Queue:               restartedQueue,
		DownloadTTL:         5 * time.Minute,
		DebugRetention:      24 * time.Hour,
		RetainProviderDebug: true,
	})
	if err != nil {
		t.Fatalf("new restarted enrichment service: %v", err)
	}

	if _, err := restartedService.RunPending(ctx); err == nil {
		t.Fatal("expected first worker pass to surface embedding failure")
	}

	record, err := repository.GetAsset(ctx, accepted.Asset.ID)
	if err != nil {
		t.Fatalf("get asset after first pass: %v", err)
	}
	if record.ProcessingStage != asset.StagePartialFailure {
		t.Fatalf("expected partial_failure after first pass, got %s", record.ProcessingStage)
	}
	if !strings.Contains(record.RecognitionStatusNote, "embedding") {
		t.Fatalf("expected failure note to mention embedding, got %q", record.RecognitionStatusNote)
	}

	if err := restartedService.QueueStage(ctx, accepted.Asset.ID, asset.RecognitionStageEmbedding, 1, map[string]any{"reason": "retry"}); err != nil {
		t.Fatalf("queue embedding retry: %v", err)
	}
	if err := restartedService.QueueStage(ctx, accepted.Asset.ID, asset.RecognitionStageEmbedding, 1, map[string]any{"reason": "duplicate"}); err != nil {
		t.Fatalf("queue duplicate embedding retry: %v", err)
	}

	secondWorkerQueue := persistence.NewRedisQueue(redisCfg)
	defer secondWorkerQueue.Close()

	secondWorker, err := NewService(ServiceConfig{
		Repository:          repository,
		Storage:             provider,
		AIProviders:         providers,
		Queue:               secondWorkerQueue,
		DownloadTTL:         5 * time.Minute,
		DebugRetention:      24 * time.Hour,
		RetainProviderDebug: true,
	})
	if err != nil {
		t.Fatalf("new second worker: %v", err)
	}

	if _, err := secondWorker.RunPending(ctx); err != nil {
		t.Fatalf("run pending retry worker: %v", err)
	}

	record, err = repository.GetAsset(ctx, accepted.Asset.ID)
	if err != nil {
		t.Fatalf("get asset after retry: %v", err)
	}
	if record.ProcessingStage != asset.StageIndexed {
		t.Fatalf("expected indexed stage after retry recovery, got %s", record.ProcessingStage)
	}

	embeddingRun, found, err := repository.GetRecognitionRun(ctx, accepted.Asset.ID, asset.RecognitionStageEmbedding)
	if err != nil {
		t.Fatalf("get embedding run: %v", err)
	}
	if !found {
		t.Fatal("expected embedding run to exist")
	}
	if embeddingRun.Status != asset.RecognitionStatusSucceeded {
		t.Fatalf("expected embedding retry to succeed, got %s", embeddingRun.Status)
	}
	if embeddingRun.Attempts != 2 {
		t.Fatalf("expected duplicate retry payload to be ignored after success, got attempts=%d", embeddingRun.Attempts)
	}
}
