package httpserver

import (
	"net/http"

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
	Items    []assetSummary `json:"items"`
	PageInfo CursorPageInfo `json:"pageInfo"`
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"libraryId": principal.LibraryID,
		"items":     []assetSummary{},
		"pageInfo": CursorPageInfo{
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

	writeJSON(w, http.StatusOK, map[string]any{
		"libraryId": principal.LibraryID,
		"query":     r.URL.Query().Get("query"),
		"items":     []assetSummary{},
		"pageInfo": CursorPageInfo{
			HasNextPage: false,
		},
	})
}
