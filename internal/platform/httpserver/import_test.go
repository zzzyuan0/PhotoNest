package httpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
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
	providerai "github.com/photonest/photonest/internal/provider/ai"
	"github.com/photonest/photonest/internal/provider/storage"
)

func TestImportEndpointsCloseTheUploadLoop(t *testing.T) {
	store := ingestion.NewMemoryStore()
	provider := storage.NewMemoryProvider("primary-memory", "photonest-main", "libraries/main")
	service, err := ingestion.NewService(ingestion.ServiceConfig{
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

	handler, err := NewWithDependencies(newTestConfig(), health.Checker{}, Dependencies{
		Ingestion:  service,
		Discovery:  mustDiscovery(t, store, provider),
		Enrichment: mustEnrichment(t, store, provider),
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"secret-password"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("login expected 200, got %d", loginRecorder.Code)
	}

	cookies := loginRecorder.Result().Cookies()
	csrfToken := ""
	for _, cookie := range cookies {
		if cookie.Name == "photonest_csrf" {
			csrfToken = cookie.Value
		}
	}
	if csrfToken == "" {
		t.Fatal("expected csrf cookie to be set")
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
		t.Fatalf("decode created session: %v", err)
	}
	sessionID := createdSession["id"].(string)
	payload := sampleUploadPNG(t)
	payloadSHA := ingestion.SHA256Hex(payload)

	uploadRecorder := httptest.NewRecorder()
	uploadRequest := httptest.NewRequest(http.MethodPost, "/api/v1/import/sessions/"+sessionID+"/uploads", strings.NewReader(`{"libraryId":"11111111-1111-1111-1111-111111111111","fileName":"sample.png","contentType":"image/png","contentLength":`+strconv.Itoa(len(payload))+`,"contentSha256":"`+payloadSHA+`"}`))
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
		t.Fatalf("decode accepted response: %v", err)
	}
	if accepted["assetId"] == "" {
		t.Fatal("expected assetId in confirm response")
	}
	if accepted["processingStage"] != "derivatives-ready" {
		t.Fatalf("expected derivatives-ready stage, got %v", accepted["processingStage"])
	}

	assetID := accepted["assetId"].(string)

	timelineRecorder := httptest.NewRecorder()
	timelineRequest := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/timeline?libraryId=11111111-1111-1111-1111-111111111111", nil)
	for _, cookie := range cookies {
		timelineRequest.AddCookie(cookie)
	}
	handler.ServeHTTP(timelineRecorder, timelineRequest)
	if timelineRecorder.Code != http.StatusOK {
		t.Fatalf("timeline expected 200, got %d: %s", timelineRecorder.Code, timelineRecorder.Body.String())
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

	downloadRecorder := httptest.NewRecorder()
	downloadRequest := httptest.NewRequest(http.MethodPost, "/api/v1/assets/"+assetID+"/download?libraryId=11111111-1111-1111-1111-111111111111", strings.NewReader(`{}`))
	downloadRequest.Header.Set("Content-Type", "application/json")
	downloadRequest.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		downloadRequest.AddCookie(cookie)
	}
	handler.ServeHTTP(downloadRecorder, downloadRequest)
	if downloadRecorder.Code != http.StatusOK {
		t.Fatalf("download grant expected 200, got %d: %s", downloadRecorder.Code, downloadRecorder.Body.String())
	}

	var grant map[string]any
	if err := json.Unmarshal(downloadRecorder.Body.Bytes(), &grant); err != nil {
		t.Fatalf("decode download grant: %v", err)
	}
	if grant["status"] != "ready" {
		t.Fatalf("expected ready download grant, got %v", grant["status"])
	}
	if url, _ := grant["url"].(string); !strings.Contains(url, "memory://") {
		t.Fatalf("expected short-lived memory url, got %v", grant["url"])
	}
}

func mustDiscovery(t *testing.T, store *ingestion.MemoryStore, provider storage.Provider) *discovery.Service {
	t.Helper()

	service, err := discovery.NewService(discovery.ServiceConfig{
		Repository:  store,
		Storage:     provider,
		DownloadTTL: 5 * time.Minute,
		TokenKey:    "test-token",
	})
	if err != nil {
		t.Fatalf("new discovery service: %v", err)
	}
	return service
}

func mustEnrichment(t *testing.T, store *ingestion.MemoryStore, provider storage.Provider) *enrichment.Service {
	t.Helper()

	service, err := enrichment.NewService(enrichment.ServiceConfig{
		Repository:          store,
		Storage:             provider,
		AIProviders:         []providerai.Provider{providerai.NewDeterministicProvider("test-ai", providerai.BoundaryLocalSidecar, nil, "test-model")},
		Queue:               enrichment.NewMemoryQueue(),
		DownloadTTL:         5 * time.Minute,
		DebugRetention:      24 * time.Hour,
		RetainProviderDebug: true,
	})
	if err != nil {
		t.Fatalf("new enrichment service: %v", err)
	}
	return service
}

func sampleUploadPNG(t *testing.T) []byte {
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
