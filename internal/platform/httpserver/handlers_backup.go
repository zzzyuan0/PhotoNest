package httpserver

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/photonest/photonest/internal/backup"
	"github.com/photonest/photonest/internal/platform/auth"
)

type exportRequest struct {
	LibraryID string `json:"libraryId"`
	Scope    string `json:"scope"`
	AlbumID  string `json:"albumId,omitempty"`
	DateFrom string `json:"dateFrom,omitempty"`
	DateTo   string `json:"dateTo,omitempty"`
}

type exportJobResponse struct {
	ID                  string              `json:"id"`
	LibraryID           string              `json:"libraryId"`
	Scope               string              `json:"scope"`
	Status              string              `json:"status"`
	AssetCount          int                 `json:"assetCount"`
	ArchiveURL          string              `json:"archiveUrl,omitempty"`
	RedactedManifestURL string              `json:"redactedManifestUrl,omitempty"`
	ExpiresAt           string              `json:"expiresAt"`
	CreatedAt           string              `json:"createdAt"`
	RecoveryPlan        backup.RecoveryPlan `json:"recoveryPlan"`
}

func (s *Server) handleCreateExport(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}
	if s.backup == nil {
		s.writeError(w, http.StatusServiceUnavailable, "export_unavailable", "backup and export service is not configured", nil)
		return
	}

	var request exportRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON", nil)
		return
	}

	scope, err := parseExportScope(request.Scope)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_scope", err.Error(), nil)
		return
	}
	dateFrom, ok := parseFlexibleTime(request.DateFrom, false)
	if strings.TrimSpace(request.DateFrom) != "" && !ok {
		s.writeError(w, http.StatusBadRequest, "invalid_date_from", "dateFrom must be RFC3339 or YYYY-MM-DD", nil)
		return
	}
	dateTo, ok := parseFlexibleTime(request.DateTo, true)
	if strings.TrimSpace(request.DateTo) != "" && !ok {
		s.writeError(w, http.StatusBadRequest, "invalid_date_to", "dateTo must be RFC3339 or YYYY-MM-DD", nil)
		return
	}

	input := backup.CreateExportInput{
		LibraryID: principal.LibraryID,
		Scope:     scope,
		AlbumID:   strings.TrimSpace(request.AlbumID),
	}
	if !dateFrom.IsZero() {
		input.DateFrom = &dateFrom
	}
	if !dateTo.IsZero() {
		input.DateTo = &dateTo
	}

	job, err := s.backup.CreateExport(r.Context(), input)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "export_failed", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusAccepted, exportJobResponse{
		ID:                  job.ID,
		LibraryID:           job.LibraryID,
		Scope:               string(job.Scope),
		Status:              job.Status,
		AssetCount:          job.AssetCount,
		ArchiveURL:          job.ArchiveURL,
		RedactedManifestURL: job.RedactedManifestURL,
		ExpiresAt:           job.ExpiresAt.UTC().Format(time.RFC3339),
		CreatedAt:           job.CreatedAt.UTC().Format(time.RFC3339),
		RecoveryPlan:        job.RecoveryPlan,
	})
}

func parseExportScope(value string) (backup.ExportScope, error) {
	switch strings.TrimSpace(value) {
	case string(backup.ExportScopeLibrary):
		return backup.ExportScopeLibrary, nil
	case string(backup.ExportScopeAlbum):
		return backup.ExportScopeAlbum, nil
	case string(backup.ExportScopeDateRange):
		return backup.ExportScopeDateRange, nil
	default:
		return "", fmt.Errorf("scope must be one of library, album or date-range")
	}
}
