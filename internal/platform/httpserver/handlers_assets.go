package httpserver

import (
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/photonest/photonest/internal/platform/auth"
)

type assetDetailResponse struct {
	AssetID               string   `json:"assetId"`
	LibraryID             string   `json:"libraryId"`
	MediaType             string   `json:"mediaType"`
	CapturedAt            string   `json:"capturedAt,omitempty"`
	ProcessingStage       string   `json:"processingStage"`
	BackupStatus          string   `json:"backupStatus"`
	CaptionPreview        string   `json:"captionPreview,omitempty"`
	OCRPreview            string   `json:"ocrPreview,omitempty"`
	ThumbnailToken        string   `json:"thumbnailToken,omitempty"`
	LocationLabel         string   `json:"locationLabel"`
	Tags                  []string `json:"tags"`
	SemanticTags          []string `json:"semanticTags"`
	SearchReady           bool     `json:"searchReady"`
	RecognitionStatusNote string   `json:"recognitionStatusNote,omitempty"`
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
		AssetID:               detail.Asset.ID,
		LibraryID:             detail.Asset.LibraryID,
		MediaType:             detail.Asset.MediaType,
		ProcessingStage:       apiProcessingStage(detail.Asset.ProcessingStage),
		BackupStatus:          detail.Asset.BackupStatus,
		CaptionPreview:        detail.CaptionPreview,
		OCRPreview:            detail.OCRPreview,
		ThumbnailToken:        detail.ThumbnailToken,
		LocationLabel:         detail.Asset.LocationLabel,
		Tags:                  append([]string{}, detail.Asset.Tags...),
		SemanticTags:          append([]string{}, detail.SemanticTags...),
		SearchReady:           detail.SearchReady,
		RecognitionStatusNote: detail.Asset.RecognitionStatusNote,
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

func (s *Server) handleAssetPreview(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}
	if s.discovery == nil {
		s.writeError(w, http.StatusServiceUnavailable, "discovery_unavailable", "discovery service is not configured", nil)
		return
	}

	preview, err := s.discovery.OpenPreview(r.Context(), principal.LibraryID, r.PathValue("assetId"))
	if err != nil {
		s.writeError(w, http.StatusNotFound, "asset_not_found", err.Error(), nil)
		return
	}
	defer preview.Body.Close()

	w.Header().Set("Content-Type", preview.MediaType)
	w.Header().Set("Cache-Control", "private, max-age=60")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if preview.ContentLength > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(preview.ContentLength, 10))
	}
	if !preview.LastModified.IsZero() {
		w.Header().Set("Last-Modified", preview.LastModified.Format(http.TimeFormat))
	}

	if _, copyErr := io.Copy(w, preview.Body); copyErr != nil {
		return
	}
}
