package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/photonest/photonest/internal/ingestion"
	"github.com/photonest/photonest/internal/platform/audit"
	"github.com/photonest/photonest/internal/platform/auth"
	"github.com/photonest/photonest/internal/platform/config"
	"github.com/photonest/photonest/internal/platform/health"
	"github.com/photonest/photonest/internal/provider/storage"
)

type Server struct {
	cfg       config.AppConfig
	checker   health.Checker
	auth      *auth.Manager
	audit     *audit.Logger
	ingestion *ingestion.Service
	mux       *http.ServeMux
}

func New(cfg config.AppConfig, checker health.Checker) (http.Handler, error) {
	return NewWithDependencies(cfg, checker, Dependencies{})
}

type Dependencies struct {
	Ingestion *ingestion.Service
}

func NewWithDependencies(cfg config.AppConfig, checker health.Checker, deps Dependencies) (http.Handler, error) {
	authManager, err := auth.NewManager(cfg.Security)
	if err != nil {
		return nil, fmt.Errorf("create auth manager: %w", err)
	}

	if deps.Ingestion == nil && strings.TrimSpace(cfg.StorageProviders.Primary.Kind) != "" {
		provider, err := storage.NewProvider(context.Background(), cfg.StorageProviders.Primary)
		if err != nil {
			return nil, fmt.Errorf("create storage provider: %w", err)
		}
		deps.Ingestion, err = ingestion.NewService(ingestion.ServiceConfig{
			Provider:            provider,
			ProviderConfig:      cfg.StorageProviders.Primary,
			UploadCredentialTTL: cfg.Security.UploadCredentialTTL,
		})
		if err != nil {
			return nil, fmt.Errorf("create ingestion service: %w", err)
		}
	}

	server := &Server{
		cfg:       cfg,
		checker:   checker,
		auth:      authManager,
		audit:     audit.NewLogger(),
		ingestion: deps.Ingestion,
		mux:       http.NewServeMux(),
	}

	server.routes()

	return server.mux, nil
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth)

	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	s.mux.HandleFunc("GET /api/v1/auth/session", s.secure(routeSpec{
		Operation: "getCurrentSession",
	}, s.handleSession))
	s.mux.HandleFunc("POST /api/v1/auth/recent", s.secure(routeSpec{
		Operation:     "refreshRecentAuthentication",
		RequireCSRF:   true,
		AuditAction:   "auth.reauthenticate",
		TargetType:    "session",
	}, s.handleRefreshRecentAuth))

	s.mux.HandleFunc("POST /api/v1/import/sessions", s.secure(routeSpec{
		Operation:      "createImportSession",
		Permission:     auth.PermissionLibraryWrite,
		RequireCSRF:    true,
		RequireLibrary: true,
	}, s.handleCreateImportSession))
	s.mux.HandleFunc("POST /api/v1/import/sessions/{sessionId}/uploads", s.secure(routeSpec{
		Operation:      "createUploadTicket",
		Permission:     auth.PermissionLibraryWrite,
		RequireCSRF:    true,
		RequireLibrary: true,
	}, s.handleCreateUploadTicket))
	s.mux.HandleFunc("POST /api/v1/import/sessions/{sessionId}/confirm", s.secure(routeSpec{
		Operation:      "confirmUpload",
		Permission:     auth.PermissionLibraryWrite,
		RequireCSRF:    true,
		RequireLibrary: true,
	}, s.handleConfirmUpload))

	s.mux.HandleFunc("GET /api/v1/discovery/timeline", s.secure(routeSpec{
		Operation:      "listTimelineAssets",
		Permission:     auth.PermissionLibraryRead,
		RequireLibrary: true,
	}, s.handleTimeline))
	s.mux.HandleFunc("GET /api/v1/discovery/search", s.secure(routeSpec{
		Operation:      "searchAssets",
		Permission:     auth.PermissionLibraryRead,
		RequireLibrary: true,
	}, s.handleSearch))
	s.mux.HandleFunc("GET /api/v1/assets/{assetId}", s.secure(routeSpec{
		Operation:      "getAssetDetail",
		Permission:     auth.PermissionLibraryRead,
		RequireLibrary: true,
	}, s.notImplemented("getAssetDetail")))
	s.mux.HandleFunc("POST /api/v1/assets/{assetId}/download", s.secure(routeSpec{
		Operation:      "requestAssetDownload",
		Permission:     auth.PermissionAssetDownload,
		RequireCSRF:    true,
		RequireLibrary: true,
	}, s.notImplemented("requestAssetDownload")))
	s.mux.HandleFunc("POST /api/v1/assets/batch-downloads", s.secure(routeSpec{
		Operation:      "createBatchDownload",
		Permission:     auth.PermissionBatchDownload,
		RequireCSRF:    true,
		RequireLibrary: true,
		AuditAction:    "asset.batch_download",
		TargetType:     "library",
	}, s.notImplemented("createBatchDownload")))

	s.mux.HandleFunc("POST /api/v1/exports", s.secure(routeSpec{
		Operation:      "createExportJob",
		Permission:     auth.PermissionLibraryExport,
		RequireCSRF:    true,
		RequireRecent:  true,
		RequireLibrary: true,
		AuditAction:    "library.export",
		TargetType:     "library",
	}, s.notImplemented("createExportJob")))
	s.mux.HandleFunc("PUT /api/v1/settings/providers/{providerName}", s.secure(routeSpec{
		Operation:     "updateProviderSettings",
		Permission:    auth.PermissionManageProvider,
		RequireCSRF:   true,
		RequireRecent: true,
		AuditAction:   "provider.settings.update",
		TargetType:    "provider",
	}, s.notImplemented("updateProviderSettings")))
	s.mux.HandleFunc("PUT /api/v1/settings/privacy-policy", s.secure(routeSpec{
		Operation:   "updatePrivacyPolicy",
		Permission:  auth.PermissionManagePrivacy,
		RequireCSRF: true,
		AuditAction: "privacy.policy.update",
		TargetType:  "privacy-policy",
	}, s.notImplemented("updatePrivacyPolicy")))
	s.mux.HandleFunc("GET /api/v1/debug/provider-runs/{runId}", s.secure(routeSpec{
		Operation:     "getProviderRunDebug",
		Permission:    auth.PermissionViewDebug,
		AuditAction:   "provider.debug.read",
		TargetType:    "recognition-run",
	}, s.notImplemented("getProviderRunDebug")))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	results := s.checker.Check(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"status": s.healthStatus(results),
		"checks": results,
	})
}

func (s *Server) healthStatus(results []health.Status) string {
	return health.OverallStatus(results)
}

func (s *Server) notImplemented(operation string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotImplemented, ErrorResponse{
			Code:    "not_implemented",
			Message: "operation is scaffolded but not implemented yet",
			TraceID: operation,
			Details: map[string]any{
				"operation": operation,
				"method":    r.Method,
				"path":      r.URL.Path,
			},
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
