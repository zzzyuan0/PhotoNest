package httpserver

import (
	"net/http"
	"time"

	"github.com/photonest/photonest/internal/platform/auth"
)

type assetDetailResponse struct {
	AssetID         string `json:"assetId"`
	LibraryID       string `json:"libraryId"`
	MediaType       string `json:"mediaType"`
	CapturedAt      string `json:"capturedAt,omitempty"`
	ProcessingStage string `json:"processingStage"`
	BackupStatus    string `json:"backupStatus"`
	CaptionPreview  string `json:"captionPreview,omitempty"`
	ThumbnailToken  string `json:"thumbnailToken,omitempty"`
}

type downloadGrantResponse struct {
	AssetID   string `json:"assetId"`
	Status    string `json:"status"`
	URL       string `json:"url,omitempty"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

func (s *Server) handleAssetDetail(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}
	if s.discovery == nil {
		s.writeError(w, http.StatusServiceUnavailable, "discovery_unavailable", "discovery service is not configured", nil)
		return
	}

	detail, err := s.discovery.GetAssetDetail(r.Context(), principal.LibraryID, r.PathValue("assetId"))
	if err != nil {
		s.writeError(w, http.StatusNotFound, "asset_not_found", err.Error(), nil)
		return
	}

	response := assetDetailResponse{
		AssetID:         detail.Asset.ID,
		LibraryID:       detail.Asset.LibraryID,
		MediaType:       detail.Asset.MediaType,
		ProcessingStage: apiProcessingStage(detail.Asset.ProcessingStage),
		BackupStatus:    detail.Asset.BackupStatus,
		CaptionPreview:  detail.CaptionPreview,
		ThumbnailToken:  detail.ThumbnailToken,
	}
	if detail.Asset.CapturedAt != nil {
		response.CapturedAt = detail.Asset.CapturedAt.UTC().Format(time.RFC3339)
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleAssetDownload(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}
	if s.discovery == nil {
		s.writeError(w, http.StatusServiceUnavailable, "discovery_unavailable", "discovery service is not configured", nil)
		return
	}

	grant, err := s.discovery.RequestDownload(r.Context(), principal.LibraryID, r.PathValue("assetId"))
	if err != nil {
		s.writeError(w, http.StatusNotFound, "asset_not_found", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, downloadGrantResponse{
		AssetID:   grant.AssetID,
		Status:    grant.Status,
		URL:       grant.URL,
		ExpiresAt: grant.ExpiresAt.UTC().Format(time.RFC3339),
	})
}
