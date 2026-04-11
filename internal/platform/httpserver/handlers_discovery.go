package httpserver

import (
	"net/http"
	"time"

	"github.com/photonest/photonest/internal/discovery"
	"github.com/photonest/photonest/internal/platform/auth"
)

type assetSummary struct {
	AssetID           string `json:"assetId"`
	MediaType         string `json:"mediaType"`
	TimelineTimestamp string `json:"timelineTimestamp"`
	ProcessingStage   string `json:"processingStage"`
	BackupStatus      string `json:"backupStatus"`
	ThumbnailToken    string `json:"thumbnailToken,omitempty"`
	CaptionPreview    string `json:"captionPreview,omitempty"`
}

type timelineResponse struct {
	LibraryID string         `json:"libraryId"`
	Items     []assetSummary `json:"items"`
	PageInfo  CursorPageInfo `json:"pageInfo"`
}

type searchResponse struct {
	LibraryID string         `json:"libraryId"`
	Query     string         `json:"query"`
	Items     []assetSummary `json:"items"`
	PageInfo  CursorPageInfo `json:"pageInfo"`
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}
	if s.discovery == nil {
		s.writeError(w, http.StatusServiceUnavailable, "discovery_unavailable", "discovery service is not configured", nil)
		return
	}

	items, err := s.discovery.ListTimeline(r.Context(), principal.LibraryID, 50)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "timeline_failed", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, timelineResponse{
		LibraryID: principal.LibraryID,
		Items:     mapAssetSummaries(items),
		PageInfo: CursorPageInfo{
			HasNextPage: false,
		},
	})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}
	if s.discovery == nil {
		s.writeError(w, http.StatusServiceUnavailable, "discovery_unavailable", "discovery service is not configured", nil)
		return
	}

	query := r.URL.Query().Get("query")
	items, err := s.discovery.Search(r.Context(), principal.LibraryID, query, 50)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "search_failed", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, searchResponse{
		LibraryID: principal.LibraryID,
		Query:     query,
		Items:     mapAssetSummaries(items),
		PageInfo: CursorPageInfo{
			HasNextPage: false,
		},
	})
}

func mapAssetSummaries(items []discovery.Summary) []assetSummary {
	summaries := make([]assetSummary, 0, len(items))
	for _, item := range items {
		summaries = append(summaries, assetSummary{
			AssetID:           item.Asset.ID,
			MediaType:         item.Asset.MediaType,
			TimelineTimestamp: timelineAt(item).Format(time.RFC3339),
			ProcessingStage:   apiProcessingStage(item.Asset.ProcessingStage),
			BackupStatus:      item.Asset.BackupStatus,
			ThumbnailToken:    item.ThumbnailToken,
			CaptionPreview:    item.CaptionPreview,
		})
	}
	return summaries
}

func timelineAt(item discovery.Summary) time.Time {
	switch {
	case !item.Asset.TimelineAt.IsZero():
		return item.Asset.TimelineAt.UTC()
	case item.Asset.CapturedAt != nil:
		return item.Asset.CapturedAt.UTC()
	default:
		return item.Asset.ImportedAt.UTC()
	}
}
