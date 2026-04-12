package httpserver

import (
	"context"
	"net/http"
	"strings"

	"github.com/photonest/photonest/internal/backup"
	"github.com/photonest/photonest/internal/discovery"
	"github.com/photonest/photonest/internal/enrichment"
	"github.com/photonest/photonest/internal/ingestion"
	"github.com/photonest/photonest/internal/platform/auth"
	"github.com/photonest/photonest/internal/platform/config"
)

type updateProviderSettingsRequest struct {
	Bucket              *string  `json:"bucket,omitempty"`
	Region              *string  `json:"region,omitempty"`
	Endpoint            *string  `json:"endpoint,omitempty"`
	KeyPrefix           *string  `json:"keyPrefix,omitempty"`
	AccessKeyID         *string  `json:"accessKeyId,omitempty"`
	AccessKeySecret     *string  `json:"accessKeySecret,omitempty"`
	SessionToken        *string  `json:"sessionToken,omitempty"`
	ForcePathStyle      *bool    `json:"forcePathStyle,omitempty"`
	AllowedOrigins      []string `json:"allowedOrigins,omitempty"`
	PrivateRead         *bool    `json:"privateRead,omitempty"`
	PublicReadBlockMode *string  `json:"publicReadBlockMode,omitempty"`
	HealthCheckURL      *string  `json:"healthCheckUrl,omitempty"`
	CORSConfigPath      *string  `json:"corsConfigPath,omitempty"`
}

type providerSettingsResponse struct {
	ProviderName string         `json:"providerName"`
	Status       string         `json:"status"`
	Summary      map[string]any `json:"summary"`
}

func (s *Server) handleUpdateProviderSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.PrincipalFromContext(r.Context()); !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}

	var request updateProviderSettingsRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON", nil)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	providerName := strings.TrimSpace(r.PathValue("providerName"))
	current, isPrimary, backupIndex, err := findProviderConfig(s.cfg, providerName)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "provider_not_found", err.Error(), nil)
		return
	}

	candidate, err := applyProviderUpdate(current, request)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if err := candidate.ValidateLeastPrivilege(); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if s.cfg.Security.StrictPrivateObjectCheck && !candidate.PrivateRead {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "provider must keep privateRead enabled", nil)
		return
	}

	if err := s.providerValidator(r.Context(), candidate); err != nil {
		s.writeError(w, http.StatusBadRequest, "provider_validation_failed", err.Error(), nil)
		return
	}

	if isPrimary {
		if err := s.swapPrimaryProvider(r.Context(), candidate); err != nil {
			s.writeError(w, http.StatusInternalServerError, "provider_update_failed", err.Error(), nil)
			return
		}
		s.cfg.StorageProviders.Primary = candidate
	} else {
		s.cfg.StorageProviders.Backup[backupIndex] = candidate
		if err := s.rebuildBackupService(r.Context()); err != nil {
			s.writeError(w, http.StatusInternalServerError, "provider_update_failed", err.Error(), nil)
			return
		}
	}

	summary, err := candidate.RedactedSummary(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "provider_update_failed", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, providerSettingsResponse{
		ProviderName: providerName,
		Status:       "updated",
		Summary:      summary,
	})
}

func findProviderConfig(cfg config.AppConfig, providerName string) (config.ObjectStorageProviderConfig, bool, int, error) {
	if strings.EqualFold(strings.TrimSpace(cfg.StorageProviders.Primary.Name), providerName) {
		return cfg.StorageProviders.Primary, true, -1, nil
	}
	for index, candidate := range cfg.StorageProviders.Backup {
		if strings.EqualFold(strings.TrimSpace(candidate.Name), providerName) {
			return candidate, false, index, nil
		}
	}
	return config.ObjectStorageProviderConfig{}, false, -1, errProviderNotFound(providerName)
}

func applyProviderUpdate(current config.ObjectStorageProviderConfig, request updateProviderSettingsRequest) (config.ObjectStorageProviderConfig, error) {
	updated := current
	if request.Bucket != nil {
		updated.Bucket = strings.TrimSpace(*request.Bucket)
	}
	if request.Region != nil {
		updated.Region = strings.TrimSpace(*request.Region)
	}
	if request.Endpoint != nil {
		updated.Endpoint = strings.TrimSpace(*request.Endpoint)
	}
	if request.KeyPrefix != nil {
		updated.KeyPrefix = strings.TrimSpace(*request.KeyPrefix)
	}
	if request.AccessKeyID != nil {
		updated.AccessKeyID = config.SecretValue{Value: strings.TrimSpace(*request.AccessKeyID)}
	}
	if request.AccessKeySecret != nil {
		updated.AccessKeySecret = config.SecretValue{Value: strings.TrimSpace(*request.AccessKeySecret)}
	}
	if request.SessionToken != nil {
		updated.SessionToken = config.SecretValue{Value: strings.TrimSpace(*request.SessionToken), AllowEmpty: true}
	}
	if request.ForcePathStyle != nil {
		updated.ForcePathStyle = *request.ForcePathStyle
	}
	if request.AllowedOrigins != nil {
		updated.AllowedOrigins = sanitizeOrigins(request.AllowedOrigins)
	}
	if request.PrivateRead != nil {
		updated.PrivateRead = *request.PrivateRead
	}
	if request.PublicReadBlockMode != nil {
		updated.PublicReadBlockMode = strings.TrimSpace(*request.PublicReadBlockMode)
	}
	if request.HealthCheckURL != nil {
		updated.HealthCheckURL = strings.TrimSpace(*request.HealthCheckURL)
	}
	if request.CORSConfigPath != nil {
		updated.CORSConfigPath = strings.TrimSpace(*request.CORSConfigPath)
	}
	return updated, nil
}

func sanitizeOrigins(origins []string) []string {
	clean := make([]string, 0, len(origins))
	for _, origin := range origins {
		if trimmed := strings.TrimSpace(origin); trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	return clean
}

func (s *Server) swapPrimaryProvider(ctx context.Context, providerCfg config.ObjectStorageProviderConfig) error {
	provider, err := s.providerFactory(ctx, providerCfg)
	if err != nil {
		return err
	}

	if s.ingestion == nil {
		return nil
	}
	repository := s.ingestion.Repository()
	newIngestion, err := ingestion.NewService(ingestion.ServiceConfig{
		Repository:          repository,
		Provider:            provider,
		ProviderConfig:      providerCfg,
		UploadCredentialTTL: s.cfg.Security.UploadCredentialTTL,
	})
	if err != nil {
		return err
	}
	newDiscovery, err := discovery.NewService(discovery.ServiceConfig{
		Repository:  repository,
		Storage:     provider,
		DownloadTTL: s.cfg.Security.DownloadCredentialTTL,
		TokenKey:    s.cfg.Security.Session.CookieName,
	})
	if err != nil {
		return err
	}

	var newEnrichment *enrichment.Service
	if s.enrich != nil {
		newEnrichment, err = enrichment.NewService(enrichment.ServiceConfig{
			Repository:          repository,
			Storage:             provider,
			AIProviders:         buildAIProviders(s.cfg.AIProviders),
			Queue:               s.enrich.Queue(),
			DownloadTTL:         s.cfg.Security.DownloadCredentialTTL,
			DebugRetention:      s.cfg.Security.DebugRetention,
			RetainProviderDebug: true,
			Telemetry:           s.telemetry,
		})
		if err != nil {
			return err
		}
	}

	s.ingestion = newIngestion
	s.discovery = newDiscovery
	s.enrich = newEnrichment
	return s.rebuildBackupService(ctx)
}

func (s *Server) rebuildBackupService(ctx context.Context) error {
	if s.ingestion == nil || len(s.cfg.StorageProviders.Backup) == 0 {
		s.backup = nil
		return nil
	}

	backupProviders := make([]backup.ConfiguredProvider, 0, len(s.cfg.StorageProviders.Backup))
	for _, providerCfg := range s.cfg.StorageProviders.Backup {
		provider, err := s.providerFactory(ctx, providerCfg)
		if err != nil {
			return err
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

	service, err := backup.NewService(backup.ServiceConfig{
		Repository:      s.ingestion.Repository(),
		PrimaryStorage:  s.ingestion.Provider(),
		ArtifactStore:   s.ingestion.Provider(),
		BackupProviders: backupProviders,
		DownloadTTL:     s.cfg.Security.DownloadCredentialTTL,
		Telemetry:       s.telemetry,
	})
	if err != nil {
		return err
	}
	s.backup = service
	return nil
}

type providerNotFoundError struct {
	name string
}

func errProviderNotFound(name string) error {
	return providerNotFoundError{name: strings.TrimSpace(name)}
}

func (e providerNotFoundError) Error() string {
	return "provider " + e.name + " is not configured"
}
