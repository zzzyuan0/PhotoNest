package enrichment

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/photonest/photonest/internal/asset"
	"github.com/photonest/photonest/internal/discovery"
	"github.com/photonest/photonest/internal/ingestion"
	"github.com/photonest/photonest/internal/library"
	"github.com/photonest/photonest/internal/platform/config"
	providerai "github.com/photonest/photonest/internal/provider/ai"
	"github.com/photonest/photonest/internal/provider/storage"
)

const testLibraryID = "11111111-1111-1111-1111-111111111111"

func TestServiceProcessesAssetToIndexedState(t *testing.T) {
	ctx := context.Background()
	store, provider, ingestionService, enrichmentService, discoveryService := newTestServices(t, []providerai.Provider{
		providerai.NewDeterministicProvider("local-ai", providerai.BoundaryLocalSidecar, nil, "test-model"),
	})

	accepted := uploadAsset(t, ctx, ingestionService, provider, "vacation-beach-sunset.png", map[string]string{
		"captured_at":   "2024-07-18T09:30:00Z",
		"device_make":   "OpenAI Camera",
		"device_model":  "Model One",
		"gps_latitude":  "23.1291",
		"gps_longitude": "113.2644",
	})

	preTimeline, err := discoveryService.ListTimeline(ctx, testLibraryID, 10)
	if err != nil {
		t.Fatalf("list timeline before enrichment: %v", err)
	}
	if len(preTimeline) != 1 || preTimeline[0].Asset.ProcessingStage != asset.StageDerivativesReady {
		t.Fatalf("expected newly accepted asset to stay visible before enrichment, got %+v", preTimeline)
	}

	if err := enrichmentService.QueueAsset(ctx, accepted.Asset.ID); err != nil {
		t.Fatalf("queue asset: %v", err)
	}
	processed, err := enrichmentService.RunPending(ctx)
	if err != nil {
		t.Fatalf("run pending: %v", err)
	}
	if processed < 5 {
		t.Fatalf("expected at least 5 jobs to be processed, got %d", processed)
	}

	record, err := store.GetAsset(ctx, accepted.Asset.ID)
	if err != nil {
		t.Fatalf("get asset: %v", err)
	}
	if record.ProcessingStage != asset.StageIndexed {
		t.Fatalf("expected asset to be indexed, got %s", record.ProcessingStage)
	}
	if record.CapturedAt == nil || record.CapturedAt.UTC().Format(time.RFC3339) != "2024-07-18T09:30:00Z" {
		t.Fatalf("expected captured time to be normalized, got %+v", record.CapturedAt)
	}
	if record.LocationLabel == "" {
		t.Fatal("expected location label to be populated")
	}
	if record.DeviceMake != "OpenAI Camera" || record.DeviceModel != "Model One" {
		t.Fatalf("unexpected device info %+v / %+v", record.DeviceMake, record.DeviceModel)
	}
	if !strings.Contains(record.CaptionText, "vacation") {
		t.Fatalf("expected caption text to contain filename tokens, got %q", record.CaptionText)
	}
	if !strings.Contains(record.OCRText, "beach") {
		t.Fatalf("expected ocr text to contain filename tokens, got %q", record.OCRText)
	}
	if len(record.Embedding) == 0 {
		t.Fatal("expected embedding vector to be stored")
	}
	if !contains(record.Tags, "sunset") {
		t.Fatalf("expected normalized tags to include sunset, got %+v", record.Tags)
	}

	timeline, err := discoveryService.ListTimeline(ctx, testLibraryID, 10)
	if err != nil {
		t.Fatalf("list timeline: %v", err)
	}
	if len(timeline) != 1 {
		t.Fatalf("expected exactly one timeline item, got %d", len(timeline))
	}
	if timeline[0].CaptionPreview == "" {
		t.Fatal("expected caption preview to be exposed in discovery summary")
	}

	results, err := discoveryService.Search(ctx, testLibraryID, "beach sunset", 10)
	if err != nil {
		t.Fatalf("search assets: %v", err)
	}
	if len(results) != 1 || results[0].Asset.ID != record.ID {
		t.Fatalf("expected hybrid search to return the indexed asset, got %+v", results)
	}
}

func TestServiceSupportsRetryAfterPartialFailure(t *testing.T) {
	ctx := context.Background()
	flakyProvider := &flakyEmbeddingProvider{name: "embedding-flaky", failTimes: 1}
	store, provider, ingestionService, enrichmentService, discoveryService := newTestServices(t, []providerai.Provider{
		providerai.NewDeterministicProvider(
			"caption-ocr",
			providerai.BoundaryLocalSidecar,
			[]providerai.Capability{providerai.CapabilityCaption, providerai.CapabilityOCR},
			"test-model",
		),
		flakyProvider,
	})

	accepted := uploadAsset(t, ctx, ingestionService, provider, "family-picnic-archive.png", nil)
	if err := enrichmentService.QueueAsset(ctx, accepted.Asset.ID); err != nil {
		t.Fatalf("queue asset: %v", err)
	}
	if _, err := enrichmentService.RunPending(ctx); err == nil {
		t.Fatal("expected first processing pass to surface embedding failure")
	}

	record, err := store.GetAsset(ctx, accepted.Asset.ID)
	if err != nil {
		t.Fatalf("get asset after first pass: %v", err)
	}
	if record.ProcessingStage != asset.StagePartialFailure {
		t.Fatalf("expected partial failure stage, got %s", record.ProcessingStage)
	}
	if !strings.Contains(record.RecognitionStatusNote, "embedding") {
		t.Fatalf("expected recognition note to mention embedding, got %q", record.RecognitionStatusNote)
	}

	results, err := discoveryService.Search(ctx, testLibraryID, "family picnic", 10)
	if err != nil {
		t.Fatalf("search assets after partial failure: %v", err)
	}
	if len(results) != 1 || results[0].Asset.ID != accepted.Asset.ID {
		t.Fatalf("expected asset to remain discoverable after partial failure, got %+v", results)
	}

	embeddingRun, found, err := store.GetRecognitionRun(ctx, accepted.Asset.ID, asset.RecognitionStageEmbedding)
	if err != nil {
		t.Fatalf("get embedding run: %v", err)
	}
	if !found || embeddingRun.Status != asset.RecognitionStatusFailed {
		t.Fatalf("expected failed embedding run, got %+v", embeddingRun)
	}

	if err := enrichmentService.QueueStage(ctx, accepted.Asset.ID, asset.RecognitionStageEmbedding, 1, map[string]any{"reason": "retry"}); err != nil {
		t.Fatalf("queue embedding retry: %v", err)
	}
	if _, err := enrichmentService.RunPending(ctx); err != nil {
		t.Fatalf("run pending retry: %v", err)
	}

	record, err = store.GetAsset(ctx, accepted.Asset.ID)
	if err != nil {
		t.Fatalf("get asset after retry: %v", err)
	}
	if record.ProcessingStage != asset.StageIndexed {
		t.Fatalf("expected retry to recover asset into indexed stage, got %s", record.ProcessingStage)
	}

	embeddingRun, found, err = store.GetRecognitionRun(ctx, accepted.Asset.ID, asset.RecognitionStageEmbedding)
	if err != nil {
		t.Fatalf("get embedding run after retry: %v", err)
	}
	if !found || embeddingRun.Status != asset.RecognitionStatusSucceeded || embeddingRun.Attempts != 2 {
		t.Fatalf("expected embedding retry to succeed on second attempt, got %+v", embeddingRun)
	}
}

func TestServiceHonorsPrivacyPolicyForSensitiveStages(t *testing.T) {
	ctx := context.Background()
	store, provider, ingestionService, enrichmentService, discoveryService := newTestServices(t, []providerai.Provider{
		providerai.NewDeterministicProvider("local-ai", providerai.BoundaryLocalSidecar, nil, "test-model"),
	})

	policy := library.DefaultPolicy()
	policy.GPSMode = library.GPSModeHidden
	policy.AllowGPSPersistence = false
	policy.CaptionMode = library.TextModeHidden
	policy.AllowRemoteCaption = false
	policy.OCRMode = library.TextModeHidden
	policy.AllowRemoteOCR = false
	policy.EmbeddingMode = library.EmbeddingModeDisabled
	policy.AllowRemoteEmbedding = false
	if err := store.SaveLibraryPolicy(ctx, testLibraryID, policy); err != nil {
		t.Fatalf("save policy: %v", err)
	}

	accepted := uploadAsset(t, ctx, ingestionService, provider, "private-diary-scan.png", map[string]string{
		"captured_at":   "2023-12-02T01:02:03Z",
		"gps_latitude":  "23.1200",
		"gps_longitude": "113.2800",
	})

	if err := enrichmentService.QueueAsset(ctx, accepted.Asset.ID); err != nil {
		t.Fatalf("queue asset: %v", err)
	}
	if _, err := enrichmentService.RunPending(ctx); err != nil {
		t.Fatalf("run pending with privacy policy: %v", err)
	}

	record, err := store.GetAsset(ctx, accepted.Asset.ID)
	if err != nil {
		t.Fatalf("get asset: %v", err)
	}
	if record.ProcessingStage != asset.StageIndexed {
		t.Fatalf("expected indexed stage when sensitive stages are skipped, got %s", record.ProcessingStage)
	}
	if record.CaptionText != "" || record.OCRText != "" {
		t.Fatalf("expected text metadata to stay hidden, got caption=%q ocr=%q", record.CaptionText, record.OCRText)
	}
	if len(record.Embedding) != 0 {
		t.Fatalf("expected embedding storage to be disabled, got %d dimensions", len(record.Embedding))
	}
	if record.GPSLatitude != nil || record.GPSLongitude != nil || record.LocationLabel != "" {
		t.Fatalf("expected gps persistence to be disabled, got %+v %+v %q", record.GPSLatitude, record.GPSLongitude, record.LocationLabel)
	}

	for _, stage := range []asset.RecognitionStage{
		asset.RecognitionStageCaption,
		asset.RecognitionStageOCR,
		asset.RecognitionStageEmbedding,
	} {
		run, found, err := store.GetRecognitionRun(ctx, accepted.Asset.ID, stage)
		if err != nil {
			t.Fatalf("get recognition run for %s: %v", stage, err)
		}
		if !found || run.Status != asset.RecognitionStatusSkipped {
			t.Fatalf("expected %s to be skipped under privacy policy, got %+v", stage, run)
		}
	}

	timeline, err := discoveryService.ListTimeline(ctx, testLibraryID, 10)
	if err != nil {
		t.Fatalf("list timeline: %v", err)
	}
	if len(timeline) != 1 || timeline[0].CaptionPreview != "" {
		t.Fatalf("expected timeline summary to hide caption preview, got %+v", timeline)
	}
}

func newTestServices(t *testing.T, providers []providerai.Provider) (*ingestion.MemoryStore, storage.Provider, *ingestion.Service, *Service, *discovery.Service) {
	t.Helper()

	store := ingestion.NewMemoryStore()
	provider := storage.NewMemoryProvider("primary-memory", "photonest-main", "libraries/main")
	ingestionService, err := ingestion.NewService(ingestion.ServiceConfig{
		Repository: store,
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
	enrichmentService, err := NewService(ServiceConfig{
		Repository:          store,
		Storage:             provider,
		AIProviders:         providers,
		Queue:               NewMemoryQueue(),
		DownloadTTL:         5 * time.Minute,
		DebugRetention:      24 * time.Hour,
		RetainProviderDebug: true,
	})
	if err != nil {
		t.Fatalf("new enrichment service: %v", err)
	}
	discoveryService, err := discovery.NewService(discovery.ServiceConfig{
		Repository:  store,
		Storage:     provider,
		DownloadTTL: 5 * time.Minute,
		TokenKey:    "test-token",
	})
	if err != nil {
		t.Fatalf("new discovery service: %v", err)
	}
	return store, provider, ingestionService, enrichmentService, discoveryService
}

func uploadAsset(
	t *testing.T,
	ctx context.Context,
	service *ingestion.Service,
	provider storage.Provider,
	fileName string,
	extraMetadata map[string]string,
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

	metadata := mapFromUploadHeaders(ticket.Plan.Headers)
	for key, value := range extraMetadata {
		metadata[key] = value
	}
	if _, err := provider.PutObject(ctx, storage.PutObjectInput{
		Ref: storage.ObjectRef{
			Key: ticket.Plan.ObjectKey,
		},
		Body:          bytes.NewReader(payload),
		ContentType:   "image/png",
		ContentLength: int64(len(payload)),
		Metadata:      metadata,
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

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type flakyEmbeddingProvider struct {
	name      string
	failTimes int
}

func (p *flakyEmbeddingProvider) Name() string {
	return p.name
}

func (p *flakyEmbeddingProvider) Boundary() providerai.Boundary {
	return providerai.BoundaryLocalSidecar
}

func (p *flakyEmbeddingProvider) Capabilities() []providerai.Capability {
	return []providerai.Capability{providerai.CapabilityEmbedding}
}

func (p *flakyEmbeddingProvider) Health(context.Context) (providerai.ProviderStatus, error) {
	return providerai.ProviderStatus{
		Boundary:  providerai.BoundaryLocalSidecar,
		Healthy:   true,
		CheckedAt: time.Now().UTC(),
		Message:   "healthy",
	}, nil
}

func (p *flakyEmbeddingProvider) Caption(context.Context, providerai.CaptionRequest) (providerai.CaptionResult, error) {
	return providerai.CaptionResult{}, errors.New("caption unsupported")
}

func (p *flakyEmbeddingProvider) OCR(context.Context, providerai.OCRRequest) (providerai.OCRResult, error) {
	return providerai.OCRResult{}, errors.New("ocr unsupported")
}

func (p *flakyEmbeddingProvider) Embedding(_ context.Context, request providerai.EmbeddingRequest) (providerai.EmbeddingResult, error) {
	if p.failTimes > 0 {
		p.failTimes--
		return providerai.EmbeddingResult{}, errors.New("temporary embedding outage")
	}
	return providerai.EmbeddingResult{
		Vector: providerai.HashEmbeddingText(request.FileName, 24),
		RawID:  "retry-success",
	}, nil
}

var _ providerai.Provider = (*flakyEmbeddingProvider)(nil)
