package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/photonest/photonest/internal/discovery"
	"github.com/photonest/photonest/internal/enrichment"
	"github.com/photonest/photonest/internal/ingestion"
	"github.com/photonest/photonest/internal/platform/config"
	"github.com/photonest/photonest/internal/platform/health"
	"github.com/photonest/photonest/internal/platform/persistence"
	providerai "github.com/photonest/photonest/internal/provider/ai"
	"github.com/photonest/photonest/internal/provider/storage"
	"github.com/photonest/photonest/internal/testsupport"
)

func TestHTTPFlowPersistsUploadAndWorkerConsumptionWithPostgresAndRedis(t *testing.T) {
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

	discoveryService, err := mustDiscoveryWithRepository(repository, provider)
	if err != nil {
		t.Fatalf("new discovery service: %v", err)
	}

	apiQueue := persistence.NewRedisQueue(redisCfg)
	defer apiQueue.Close()

	enrichmentService, err := enrichment.NewService(enrichment.ServiceConfig{
		Repository: repository,
		Storage:    provider,
		AIProviders: []providerai.Provider{
			providerai.NewDeterministicProvider("test-ai", providerai.BoundaryLocalSidecar, nil, "test-model"),
		},
		Queue:               apiQueue,
		DownloadTTL:         5 * time.Minute,
		DebugRetention:      24 * time.Hour,
		RetainProviderDebug: true,
	})
	if err != nil {
		t.Fatalf("new enrichment service: %v", err)
	}

	cfg := newTestConfig()
	cfg.Database = dbCfg
	cfg.Queue = redisCfg

	handler, err := NewWithDependencies(cfg, health.Checker{}, Dependencies{
		Ingestion:  ingestionService,
		Discovery:  discoveryService,
		Enrichment: enrichmentService,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	loginRecorder, cookies, csrfToken := loginTestServer(t, handler)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("login expected 200, got %d", loginRecorder.Code)
	}

	sessionRecorder := httptest.NewRecorder()
	sessionRequest := httptest.NewRequest(http.MethodPost, "/api/v1/import/sessions", strings.NewReader(`{"libraryId":"11111111-1111-1111-1111-111111111111","source":"web-upload","expectedItemCount":1}`))
	sessionRequest.Header.Set("Content-Type", "application/json")
	sessionRequest.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		sessionRequest.AddCookie(cookie)
	}
	handler.ServeHTTP(sessionRecorder, sessionRequest)
	if sessionRecorder.Code != http.StatusCreated {
		t.Fatalf("create session expected 201, got %d: %s", sessionRecorder.Code, sessionRecorder.Body.String())
	}

	var createdSession map[string]any
	if err := json.Unmarshal(sessionRecorder.Body.Bytes(), &createdSession); err != nil {
		t.Fatalf("decode session payload: %v", err)
	}
	sessionID := createdSession["id"].(string)

	payload := sampleUploadPNG(t)
	payloadSHA := ingestion.SHA256Hex(payload)

	uploadRecorder := httptest.NewRecorder()
	uploadRequest := httptest.NewRequest(http.MethodPost, "/api/v1/import/sessions/"+sessionID+"/uploads", strings.NewReader(`{"libraryId":"11111111-1111-1111-1111-111111111111","fileName":"integration.png","contentType":"image/png","contentLength":`+strconv.Itoa(len(payload))+`,"contentSha256":"`+payloadSHA+`"}`))
	uploadRequest.Header.Set("Content-Type", "application/json")
	uploadRequest.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		uploadRequest.AddCookie(cookie)
	}
	handler.ServeHTTP(uploadRecorder, uploadRequest)
	if uploadRecorder.Code != http.StatusCreated {
		t.Fatalf("create upload ticket expected 201, got %d: %s", uploadRecorder.Code, uploadRecorder.Body.String())
	}

	var ticket map[string]any
	if err := json.Unmarshal(uploadRecorder.Body.Bytes(), &ticket); err != nil {
		t.Fatalf("decode upload ticket: %v", err)
	}
	objectKey := ticket["objectKey"].(string)
	headers := map[string]string{}
	if rawHeaders, ok := ticket["headers"].(map[string]any); ok {
		for key, value := range rawHeaders {
			headers[key] = value.(string)
		}
	}

	if _, err := provider.PutObject(context.Background(), storage.PutObjectInput{
		Ref:           storage.ObjectRef{Key: objectKey},
		Body:          bytes.NewReader(payload),
		ContentType:   "image/png",
		ContentLength: int64(len(payload)),
		Metadata:      mapFromUploadHeaders(headers),
	}); err != nil {
		t.Fatalf("put uploaded object: %v", err)
	}

	confirmRecorder := httptest.NewRecorder()
	confirmRequest := httptest.NewRequest(http.MethodPost, "/api/v1/import/sessions/"+sessionID+"/confirm", strings.NewReader(`{"libraryId":"11111111-1111-1111-1111-111111111111","objectKey":"`+objectKey+`","contentLength":`+strconv.Itoa(len(payload))+`,"contentSha256":"`+payloadSHA+`"}`))
	confirmRequest.Header.Set("Content-Type", "application/json")
	confirmRequest.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		confirmRequest.AddCookie(cookie)
	}
	handler.ServeHTTP(confirmRecorder, confirmRequest)
	if confirmRecorder.Code != http.StatusAccepted {
		t.Fatalf("confirm upload expected 202, got %d: %s", confirmRecorder.Code, confirmRecorder.Body.String())
	}

	var accepted map[string]any
	if err := json.Unmarshal(confirmRecorder.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode accepted payload: %v", err)
	}
	assetID := accepted["assetId"].(string)
	if accepted["processingStage"] != "derivatives-ready" {
		t.Fatalf("expected derivatives-ready after confirm, got %v", accepted["processingStage"])
	}

	timelineRecorder := httptest.NewRecorder()
	timelineRequest := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/timeline?libraryId=11111111-1111-1111-1111-111111111111", nil)
	for _, cookie := range cookies {
		timelineRequest.AddCookie(cookie)
	}
	handler.ServeHTTP(timelineRecorder, timelineRequest)
	if timelineRecorder.Code != http.StatusOK {
		t.Fatalf("timeline expected 200, got %d: %s", timelineRecorder.Code, timelineRecorder.Body.String())
	}
	if !strings.Contains(timelineRecorder.Body.String(), `"assetId":"`+assetID+`"`) {
		t.Fatalf("expected timeline to include accepted asset, got %s", timelineRecorder.Body.String())
	}

	workerQueue := persistence.NewRedisQueue(redisCfg)
	defer workerQueue.Close()

	workerService, err := enrichment.NewService(enrichment.ServiceConfig{
		Repository: repository,
		Storage:    provider,
		AIProviders: []providerai.Provider{
			providerai.NewDeterministicProvider("worker-ai", providerai.BoundaryLocalSidecar, nil, "test-model"),
		},
		Queue:               workerQueue,
		DownloadTTL:         5 * time.Minute,
		DebugRetention:      24 * time.Hour,
		RetainProviderDebug: true,
	})
	if err != nil {
		t.Fatalf("new worker service: %v", err)
	}

	if processed, err := workerService.RunPending(context.Background()); err != nil {
		t.Fatalf("worker run pending: %v", err)
	} else if processed == 0 {
		t.Fatal("expected worker to consume queued enrichment tasks")
	}

	detailRecorder := httptest.NewRecorder()
	detailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/assets/"+assetID+"?libraryId=11111111-1111-1111-1111-111111111111", nil)
	for _, cookie := range cookies {
		detailRequest.AddCookie(cookie)
	}
	handler.ServeHTTP(detailRecorder, detailRequest)
	if detailRecorder.Code != http.StatusOK {
		t.Fatalf("asset detail expected 200, got %d: %s", detailRecorder.Code, detailRecorder.Body.String())
	}
	if !strings.Contains(detailRecorder.Body.String(), `"processingStage":"indexed"`) {
		t.Fatalf("expected asset detail to reflect indexed stage after worker consumption, got %s", detailRecorder.Body.String())
	}
}

func mustDiscoveryWithRepository(repository *persistence.PostgresRepository, provider storage.Provider) (*discovery.Service, error) {
	return discovery.NewService(discovery.ServiceConfig{
		Repository:  repository,
		Storage:     provider,
		DownloadTTL: 5 * time.Minute,
		TokenKey:    "test-token",
	})
}
