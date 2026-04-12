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

	"github.com/photonest/photonest/internal/backup"
	"github.com/photonest/photonest/internal/ingestion"
	"github.com/photonest/photonest/internal/platform/config"
	"github.com/photonest/photonest/internal/platform/health"
	"github.com/photonest/photonest/internal/provider/storage"
)

func TestDiscoveryAlbumAndExportFlow(t *testing.T) {
	ctx := context.Background()
	store := ingestion.NewMemoryStore()
	provider := storage.NewMemoryProvider("primary-memory", "photonest-main", "libraries/main")
	backupProvider := storage.NewMemoryProvider("backup-memory", "photonest-backup", "archives/secondary")

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
	discoveryService := mustDiscovery(t, store, provider)
	enrichmentService := mustEnrichment(t, store, provider)
	backupService, err := backup.NewService(backup.ServiceConfig{
		Repository:     store,
		PrimaryStorage: provider,
		ArtifactStore:  provider,
		BackupProviders: []backup.ConfiguredProvider{
			{
				Provider:    backupProvider,
				Name:        "backup-memory",
				BucketName:  "photonest-backup",
				KeyPrefix:   "archives/secondary",
				PrivateRead: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("new backup service: %v", err)
	}

	handler, err := NewWithDependencies(newTestConfig(), health.Checker{}, Dependencies{
		Ingestion:  ingestionService,
		Discovery:  discoveryService,
		Enrichment: enrichmentService,
		Backup:     backupService,
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}

	cookies, csrfToken := loginForTests(t, handler)
	assetID := uploadViaHTTP(t, handler, provider, cookies, csrfToken, "guangzhou-river.png", map[string]string{
		"captured_at":   "2024-08-21T19:45:00Z",
		"gps_latitude":  "23.1291",
		"gps_longitude": "113.2644",
	})
	if _, err := enrichmentService.RunPending(ctx); err != nil {
		t.Fatalf("run enrichment: %v", err)
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
	if !strings.Contains(timelineRecorder.Body.String(), "\"backupStatus\":\"verified\"") {
		t.Fatalf("expected verified backup status in timeline, got %s", timelineRecorder.Body.String())
	}

	placesRecorder := httptest.NewRecorder()
	placesRequest := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/places?libraryId=11111111-1111-1111-1111-111111111111", nil)
	for _, cookie := range cookies {
		placesRequest.AddCookie(cookie)
	}
	handler.ServeHTTP(placesRecorder, placesRequest)
	if placesRecorder.Code != http.StatusOK {
		t.Fatalf("places expected 200, got %d: %s", placesRecorder.Code, placesRecorder.Body.String())
	}
	if !strings.Contains(strings.ToLower(placesRecorder.Body.String()), "guang") {
		t.Fatalf("expected place response to include derived location, got %s", placesRecorder.Body.String())
	}

	albumRecorder := httptest.NewRecorder()
	albumRequest := httptest.NewRequest(http.MethodPost, "/api/v1/albums", strings.NewReader(`{"libraryId":"11111111-1111-1111-1111-111111111111","displayName":"旅行精选"}`))
	albumRequest.Header.Set("Content-Type", "application/json")
	albumRequest.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		albumRequest.AddCookie(cookie)
	}
	handler.ServeHTTP(albumRecorder, albumRequest)
	if albumRecorder.Code != http.StatusCreated {
		t.Fatalf("create album expected 201, got %d: %s", albumRecorder.Code, albumRecorder.Body.String())
	}
	var albumPayload map[string]any
	if err := json.Unmarshal(albumRecorder.Body.Bytes(), &albumPayload); err != nil {
		t.Fatalf("decode album payload: %v", err)
	}
	albumID := albumPayload["albumId"].(string)

	addRecorder := httptest.NewRecorder()
	addRequest := httptest.NewRequest(http.MethodPost, "/api/v1/albums/"+albumID+"/assets", strings.NewReader(`{"libraryId":"11111111-1111-1111-1111-111111111111","assetId":"`+assetID+`"}`))
	addRequest.Header.Set("Content-Type", "application/json")
	addRequest.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		addRequest.AddCookie(cookie)
	}
	handler.ServeHTTP(addRecorder, addRequest)
	if addRecorder.Code != http.StatusOK {
		t.Fatalf("add album asset expected 200, got %d: %s", addRecorder.Code, addRecorder.Body.String())
	}

	favoriteRecorder := httptest.NewRecorder()
	favoriteRequest := httptest.NewRequest(http.MethodPut, "/api/v1/assets/"+assetID+"/favorite", strings.NewReader(`{"libraryId":"11111111-1111-1111-1111-111111111111","favorite":true}`))
	favoriteRequest.Header.Set("Content-Type", "application/json")
	favoriteRequest.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		favoriteRequest.AddCookie(cookie)
	}
	handler.ServeHTTP(favoriteRecorder, favoriteRequest)
	if favoriteRecorder.Code != http.StatusOK {
		t.Fatalf("favorite expected 200, got %d: %s", favoriteRecorder.Code, favoriteRecorder.Body.String())
	}

	albumDetailRecorder := httptest.NewRecorder()
	albumDetailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/albums/"+albumID+"?libraryId=11111111-1111-1111-1111-111111111111", nil)
	for _, cookie := range cookies {
		albumDetailRequest.AddCookie(cookie)
	}
	handler.ServeHTTP(albumDetailRecorder, albumDetailRequest)
	if albumDetailRecorder.Code != http.StatusOK {
		t.Fatalf("album detail expected 200, got %d: %s", albumDetailRecorder.Code, albumDetailRecorder.Body.String())
	}
	if !strings.Contains(albumDetailRecorder.Body.String(), assetID) {
		t.Fatalf("expected album detail to include asset %s, got %s", assetID, albumDetailRecorder.Body.String())
	}

	searchRecorder := httptest.NewRecorder()
	searchRequest := httptest.NewRequest(http.MethodGet, "/api/v1/discovery/search?libraryId=11111111-1111-1111-1111-111111111111&query=river", nil)
	for _, cookie := range cookies {
		searchRequest.AddCookie(cookie)
	}
	handler.ServeHTTP(searchRecorder, searchRequest)
	if searchRecorder.Code != http.StatusOK {
		t.Fatalf("search expected 200, got %d: %s", searchRecorder.Code, searchRecorder.Body.String())
	}
	if !strings.Contains(searchRecorder.Body.String(), assetID) {
		t.Fatalf("expected search response to include asset %s, got %s", assetID, searchRecorder.Body.String())
	}

	exportRecorder := httptest.NewRecorder()
	exportRequest := httptest.NewRequest(http.MethodPost, "/api/v1/exports", strings.NewReader(`{"libraryId":"11111111-1111-1111-1111-111111111111","scope":"album","albumId":"`+albumID+`"}`))
	exportRequest.Header.Set("Content-Type", "application/json")
	exportRequest.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		exportRequest.AddCookie(cookie)
	}
	handler.ServeHTTP(exportRecorder, exportRequest)
	if exportRecorder.Code != http.StatusAccepted {
		t.Fatalf("export expected 202, got %d: %s", exportRecorder.Code, exportRecorder.Body.String())
	}
	if !strings.Contains(exportRecorder.Body.String(), "memory://") {
		t.Fatalf("expected short-lived export URL, got %s", exportRecorder.Body.String())
	}
}

func loginForTests(t *testing.T, handler http.Handler) ([]*http.Cookie, string) {
	t.Helper()

	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"secret-password"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("login expected 200, got %d: %s", loginRecorder.Code, loginRecorder.Body.String())
	}

	cookies := loginRecorder.Result().Cookies()
	csrfToken := ""
	for _, cookie := range cookies {
		if cookie.Name == "photonest_csrf" {
			csrfToken = cookie.Value
		}
	}
	if csrfToken == "" {
		t.Fatal("expected csrf cookie")
	}
	return cookies, csrfToken
}

func uploadViaHTTP(
	t *testing.T,
	handler http.Handler,
	provider storage.Provider,
	cookies []*http.Cookie,
	csrfToken string,
	fileName string,
	extraMetadata map[string]string,
) string {
	t.Helper()

	payload := sampleUploadPNG(t)
	payloadSHA := ingestion.SHA256Hex(payload)

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

	uploadRecorder := httptest.NewRecorder()
	uploadRequest := httptest.NewRequest(http.MethodPost, "/api/v1/import/sessions/"+sessionID+"/uploads", strings.NewReader(`{"libraryId":"11111111-1111-1111-1111-111111111111","fileName":"`+fileName+`","contentType":"image/png","contentLength":`+strconv.Itoa(len(payload))+`,"contentSha256":"`+payloadSHA+`"}`))
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

	metadata := mapFromUploadHeaders(headers)
	for key, value := range extraMetadata {
		metadata[key] = value
	}
	if _, err := provider.PutObject(context.Background(), storage.PutObjectInput{
		Ref: storage.ObjectRef{Key: objectKey},
		Body:          bytes.NewReader(payload),
		ContentType:   "image/png",
		ContentLength: int64(len(payload)),
		Metadata:      metadata,
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
	return accepted["assetId"].(string)
}
