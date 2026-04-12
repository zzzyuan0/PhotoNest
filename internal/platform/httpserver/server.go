package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/photonest/photonest/internal/backup"
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

type Server struct {
	cfg       config.AppConfig
	checker   health.Checker
	auth      *auth.Manager
	audit     *audit.Logger
	ingestion *ingestion.Service
	discovery *discovery.Service
	enrich    *enrichment.Service
	backup    *backup.Service
	telemetry *telemetry.Collector
	mux       *http.ServeMux
}

func New(cfg config.AppConfig, checker health.Checker) (http.Handler, error) {
	return NewWithDependencies(cfg, checker, Dependencies{})
}

type Dependencies struct {
	Ingestion  *ingestion.Service
	Discovery  *discovery.Service
	Enrichment *enrichment.Service
	Backup     *backup.Service
}

func NewWithDependencies(cfg config.AppConfig, checker health.Checker, deps Dependencies) (http.Handler, error) {
	collector := telemetry.NewCollector()
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
	if deps.Discovery == nil && deps.Ingestion != nil {
		deps.Discovery, err = discovery.NewService(discovery.ServiceConfig{
			Repository:  deps.Ingestion.Repository(),
			Storage:     deps.Ingestion.Provider(),
			DownloadTTL: cfg.Security.DownloadCredentialTTL,
			TokenKey:    cfg.Security.Session.CookieName,
		})
		if err != nil {
			return nil, fmt.Errorf("create discovery service: %w", err)
		}
	}
	if deps.Enrichment == nil && deps.Ingestion != nil {
		deps.Enrichment, err = enrichment.NewService(enrichment.ServiceConfig{
			Repository:          deps.Ingestion.Repository(),
			Storage:             deps.Ingestion.Provider(),
			AIProviders:         buildAIProviders(cfg.AIProviders),
			Queue:               enrichment.NewMemoryQueue(),
			DownloadTTL:         cfg.Security.DownloadCredentialTTL,
			DebugRetention:      cfg.Security.DebugRetention,
			RetainProviderDebug: true,
			Telemetry:           collector,
		})
		if err != nil {
			return nil, fmt.Errorf("create enrichment service: %w", err)
		}
	}
	if deps.Backup == nil && deps.Ingestion != nil && len(cfg.StorageProviders.Backup) > 0 {
		backupProviders := make([]backup.ConfiguredProvider, 0, len(cfg.StorageProviders.Backup))
		for _, providerCfg := range cfg.StorageProviders.Backup {
			provider, err := storage.NewProvider(context.Background(), providerCfg)
			if err != nil {
				return nil, fmt.Errorf("create backup storage provider %s: %w", providerCfg.Name, err)
			}
			backupProviders = append(backupProviders, backup.ConfiguredProvider{
				Provider:    provider,
				Name:        providerCfg.Name,
				BucketName:  providerCfg.Bucket,
				Endpoint:    providerCfg.Endpoint,
				KeyPrefix:   providerCfg.KeyPrefix,
				PrivateRead: providerCfg.PrivateRead,
			})
		}
		deps.Backup, err = backup.NewService(backup.ServiceConfig{
			Repository:      deps.Ingestion.Repository(),
			PrimaryStorage:  deps.Ingestion.Provider(),
			ArtifactStore:   deps.Ingestion.Provider(),
			BackupProviders: backupProviders,
			DownloadTTL:     cfg.Security.DownloadCredentialTTL,
			Telemetry:       collector,
		})
		if err != nil {
			return nil, fmt.Errorf("create backup service: %w", err)
		}
	}

	server := &Server{
		cfg:       cfg,
		checker:   checker,
		auth:      authManager,
		audit:     audit.NewLogger(collector),
		ingestion: deps.Ingestion,
		discovery: deps.Discovery,
		enrich:    deps.Enrichment,
		backup:    deps.Backup,
		telemetry: collector,
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
		Operation:   "refreshRecentAuthentication",
		RequireCSRF: true,
		AuditAction: "auth.reauthenticate",
		TargetType:  "session",
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
	s.mux.HandleFunc("GET /api/v1/discovery/places", s.secure(routeSpec{
		Operation:      "listPlaceSummaries",
		Permission:     auth.PermissionLibraryRead,
		RequireLibrary: true,
	}, s.handlePlaces))
	s.mux.HandleFunc("GET /api/v1/discovery/duplicates", s.secure(routeSpec{
		Operation:      "listDuplicateCandidates",
		Permission:     auth.PermissionLibraryRead,
		RequireLibrary: true,
	}, s.handleDuplicates))
	s.mux.HandleFunc("GET /api/v1/discovery/search", s.secure(routeSpec{
		Operation:      "searchAssets",
		Permission:     auth.PermissionLibraryRead,
		RequireLibrary: true,
	}, s.handleSearch))
	s.mux.HandleFunc("GET /api/v1/albums", s.secure(routeSpec{
		Operation:      "listAlbums",
		Permission:     auth.PermissionLibraryRead,
		RequireLibrary: true,
	}, s.handleAlbums))
	s.mux.HandleFunc("POST /api/v1/albums", s.secure(routeSpec{
		Operation:      "createAlbum",
		Permission:     auth.PermissionLibraryWrite,
		RequireCSRF:    true,
		RequireLibrary: true,
		AuditAction:    "album.create",
		TargetType:     "album",
	}, s.handleCreateAlbum))
	s.mux.HandleFunc("GET /api/v1/albums/{albumId}", s.secure(routeSpec{
		Operation:      "getAlbumDetail",
		Permission:     auth.PermissionLibraryRead,
		RequireLibrary: true,
	}, s.handleAlbumDetail))
	s.mux.HandleFunc("POST /api/v1/albums/{albumId}/assets", s.secure(routeSpec{
		Operation:      "addAlbumAsset",
		Permission:     auth.PermissionLibraryWrite,
		RequireCSRF:    true,
		RequireLibrary: true,
		AuditAction:    "album.asset.add",
		TargetType:     "album",
	}, s.handleAddAssetToAlbum))
	s.mux.HandleFunc("GET /api/v1/assets/{assetId}", s.secure(routeSpec{
		Operation:      "getAssetDetail",
		Permission:     auth.PermissionLibraryRead,
		RequireLibrary: true,
	}, s.handleAssetDetail))
	s.mux.HandleFunc("PUT /api/v1/assets/{assetId}/favorite", s.secure(routeSpec{
		Operation:      "setFavoriteAsset",
		Permission:     auth.PermissionLibraryWrite,
		RequireCSRF:    true,
		RequireLibrary: true,
		AuditAction:    "asset.favorite.update",
		TargetType:     "asset",
	}, s.handleSetFavorite))
	s.mux.HandleFunc("POST /api/v1/assets/{assetId}/download", s.secure(routeSpec{
		Operation:      "requestAssetDownload",
		Permission:     auth.PermissionAssetDownload,
		RequireCSRF:    true,
		RequireLibrary: true,
	}, s.handleAssetDownload))
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
	}, s.handleCreateExport))
	s.mux.HandleFunc("PUT /api/v1/settings/providers/{providerName}", s.secure(routeSpec{
		Operation:     "updateProviderSettings",
		Permission:    auth.PermissionManageProvider,
		RequireCSRF:   true,
		RequireRecent: true,
		AuditAction:   "provider.settings.update",
		TargetType:    "provider",
	}, s.notImplemented("updateProviderSettings")))
	s.mux.HandleFunc("PUT /api/v1/settings/privacy-policy", s.secure(routeSpec{
		Operation:      "updatePrivacyPolicy",
		Permission:     auth.PermissionManagePrivacy,
		RequireCSRF:    true,
		RequireLibrary: true,
		AuditAction:    "privacy.policy.update",
		TargetType:     "privacy-policy",
	}, s.handleUpdatePrivacyPolicy))
	s.mux.HandleFunc("GET /api/v1/debug/provider-runs/{runId}", s.secure(routeSpec{
		Operation:   "getProviderRunDebug",
		Permission:  auth.PermissionViewDebug,
		AuditAction: "provider.debug.read",
		TargetType:  "recognition-run",
	}, s.handleGetProviderRunDebug))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	results := s.checker.Check(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    s.healthStatus(results),
		"checks":    results,
		"telemetry": s.telemetry.Snapshots(),
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

func buildAIProviders(configs []config.AIProviderConfig) []providerai.Provider {
	providers := make([]providerai.Provider, 0, len(configs))
	for _, cfg := range configs {
		capabilities := make([]providerai.Capability, 0, len(cfg.Capabilities))
		for _, capability := range cfg.Capabilities {
			switch strings.ToLower(strings.TrimSpace(capability)) {
			case string(providerai.CapabilityCaption):
				capabilities = append(capabilities, providerai.CapabilityCaption)
			case string(providerai.CapabilityOCR):
				capabilities = append(capabilities, providerai.CapabilityOCR)
			case string(providerai.CapabilityEmbedding):
				capabilities = append(capabilities, providerai.CapabilityEmbedding)
			}
		}

		boundary := providerai.BoundaryRemoteService
		if strings.EqualFold(strings.TrimSpace(cfg.ExecutionBoundary), string(providerai.BoundaryLocalSidecar)) {
			boundary = providerai.BoundaryLocalSidecar
		}
		providers = append(providers, providerai.NewDeterministicProvider(cfg.Name, boundary, capabilities, cfg.Model))
	}
	return providers
}
