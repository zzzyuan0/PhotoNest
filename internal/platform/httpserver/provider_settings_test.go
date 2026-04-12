package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/photonest/photonest/internal/discovery"
	"github.com/photonest/photonest/internal/enrichment"
	"github.com/photonest/photonest/internal/ingestion"
	"github.com/photonest/photonest/internal/platform/audit"
	"github.com/photonest/photonest/internal/platform/auth"
	"github.com/photonest/photonest/internal/platform/config"
	"github.com/photonest/photonest/internal/platform/health"
	"github.com/photonest/photonest/internal/platform/telemetry"
	providerai "github.com/photonest/photonest/internal/provider/ai"
	"github.com/photonest/photonest/internal/provider/storage"
)

func TestUpdateProviderSettingsAppliesConfigAndReturnsRedactedSummary(t *testing.T) {
	server := newProviderSettingsTestServer(t)

	loginRecorder, cookies, csrfToken := loginTestServer(t, server.mux)

	request := httptest.NewRequest(http.MethodPut, "/api/v1/settings/providers/primary-cos", strings.NewReader(`{
		"bucket":"updated-bucket",
		"region":"ap-shanghai",
		"endpoint":"https://cos.example.internal",
		"keyPrefix":"libraries/updated",
		"accessKeyId":"AKID-UPDATED",
		"accessKeySecret":"SECRET-UPDATED",
		"allowedOrigins":["https://app.example.com"],
		"privateRead":true,
		"publicReadBlockMode":"fail-fast"
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	server.mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("login expected 200, got %d", loginRecorder.Code)
	}

	var response providerSettingsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ProviderName != "primary-cos" {
		t.Fatalf("unexpected provider name %q", response.ProviderName)
	}
	if response.Status != "updated" {
		t.Fatalf("unexpected status %q", response.Status)
	}
	if got := server.cfg.StorageProviders.Primary.Bucket; got != "updated-bucket" {
		t.Fatalf("expected bucket to be updated, got %q", got)
	}
	if got := server.cfg.StorageProviders.Primary.AccessKeySecret.Value; got != "SECRET-UPDATED" {
		t.Fatalf("expected secret to be updated in runtime config, got %q", got)
	}
	if summarySecret, _ := response.Summary["accessKeySecret"].(string); strings.Contains(summarySecret, "SECRET-UPDATED") {
		t.Fatalf("expected redacted secret summary, got %q", summarySecret)
	}
}

func TestUpdateProviderSettingsKeepsOldConfigWhenValidationFails(t *testing.T) {
	server := newProviderSettingsTestServer(t)
	server.providerValidator = func(_ context.Context, cfg config.ObjectStorageProviderConfig) error {
		if cfg.Bucket == "broken-bucket" {
			return errProviderNotFound("validation failed")
		}
		return nil
	}

	_, cookies, csrfToken := loginTestServer(t, server.mux)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/settings/providers/primary-cos", strings.NewReader(`{
		"bucket":"broken-bucket",
		"endpoint":"https://broken.example.internal"
	}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrfToken)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	server.mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if got := server.cfg.StorageProviders.Primary.Bucket; got != "photonest-main" {
		t.Fatalf("expected original bucket to be preserved, got %q", got)
	}
}

func newProviderSettingsTestServer(t *testing.T) *Server {
	t.Helper()

	cfg := newTestConfig()
	cfg.Database = config.DatabaseConfig{
		Host:     "127.0.0.1",
		Name:     "photonest",
		User:     "postgres",
		Password: config.SecretValue{Value: "postgres"},
	}
	cfg.Queue = config.QueueConfig{
		Address:   "127.0.0.1:6379",
		Password:  config.SecretValue{AllowEmpty: true},
		Namespace: "photonest-test",
	}
	cfg.StorageProviders.Primary = config.ObjectStorageProviderConfig{
		Name:                "primary-cos",
		Kind:                "s3-compatible",
		Bucket:              "photonest-main",
		Region:              "ap-guangzhou",
		Endpoint:            "https://cos.example.internal",
		KeyPrefix:           "libraries/main",
		AccessKeyID:         config.SecretValue{Value: "AKID-ORIGINAL"},
		AccessKeySecret:     config.SecretValue{Value: "SECRET-ORIGINAL"},
		UploadPresignTTL:    15 * time.Minute,
		DownloadPresignTTL:  5 * time.Minute,
		AllowedOrigins:      []string{"http://localhost:3000"},
		PrivateRead:         true,
		PublicReadBlockMode: "fail-fast",
	}

	store := ingestion.NewMemoryStore()
	provider := storage.NewMemoryProvider("primary-cos", "photonest-main", "libraries/main")
	ingestionService, err := ingestion.NewService(ingestion.ServiceConfig{
		Repository: store,
		Provider:   provider,
		ProviderConfig: config.ObjectStorageProviderConfig{
			Name:             "primary-cos",
			Bucket:           "photonest-main",
			KeyPrefix:        "libraries/main",
			UploadPresignTTL: storage.MaxUploadTTL,
		},
	})
	if err != nil {
		t.Fatalf("new ingestion service: %v", err)
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
	enrichmentService, err := enrichment.NewService(enrichment.ServiceConfig{
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
	authManager, err := auth.NewManager(cfg.Security)
	if err != nil {
		t.Fatalf("new auth manager: %v", err)
	}
	collector := telemetry.NewCollector()
	server := &Server{
		cfg:       cfg,
		checker:   health.Checker{},
		auth:      authManager,
		audit:     audit.NewLogger(collector),
		ingestion: ingestionService,
		discovery: discoveryService,
		enrich:    enrichmentService,
		telemetry: collector,
		providerFactory: func(_ context.Context, providerCfg config.ObjectStorageProviderConfig) (storage.Provider, error) {
			return storage.NewMemoryProvider(providerCfg.Name, providerCfg.Bucket, providerCfg.KeyPrefix), nil
		},
		providerValidator: func(_ context.Context, _ config.ObjectStorageProviderConfig) error { return nil },
		mux:               http.NewServeMux(),
	}
	server.routes()
	return server
}

func loginTestServer(t *testing.T, handler http.Handler) (*httptest.ResponseRecorder, []*http.Cookie, string) {
	t.Helper()

	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"username":"admin","password":"secret-password"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginRecorder, loginRequest)
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

	return loginRecorder, cookies, csrfToken
}
