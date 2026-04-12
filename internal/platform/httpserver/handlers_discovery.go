package httpserver

import (
	"net/http"
	"strconv"
	"strings"
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

type placeSummaryResponse struct {
	Label       string       `json:"label"`
	Count       int          `json:"count"`
	LatestAt    string       `json:"latestAt"`
	LatestAsset assetSummary `json:"latestAsset"`
}

type placesResponse struct {
	LibraryID string                 `json:"libraryId"`
	Items     []placeSummaryResponse `json:"items"`
}

type duplicateCandidateResponse struct {
	Primary   assetSummary `json:"primary"`
	Candidate assetSummary `json:"candidate"`
	Exact     bool         `json:"exact"`
}

type duplicatesResponse struct {
	LibraryID string                       `json:"libraryId"`
	Items     []duplicateCandidateResponse `json:"items"`
}

type albumSummaryResponse struct {
	AlbumID     string `json:"albumId"`
	Slug        string `json:"slug"`
	DisplayName string `json:"displayName"`
	Kind        string `json:"kind"`
	AssetCount  int    `json:"assetCount"`
}

type albumsResponse struct {
	LibraryID string                 `json:"libraryId"`
	Items     []albumSummaryResponse `json:"items"`
}

type albumDetailResponse struct {
	LibraryID string               `json:"libraryId"`
	Album     albumSummaryResponse `json:"album"`
	Items     []assetSummary       `json:"items"`
	PageInfo  CursorPageInfo       `json:"pageInfo"`
}

type createAlbumRequest struct {
	LibraryID   string `json:"libraryId"`
	DisplayName string `json:"displayName"`
}

type addAlbumAssetRequest struct {
	LibraryID string `json:"libraryId"`
	AssetID string `json:"assetId"`
}

type setFavoriteRequest struct {
	LibraryID string `json:"libraryId"`
	Favorite bool `json:"favorite"`
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

	items, err := s.discovery.ListTimelineWithFilters(r.Context(), principal.LibraryID, parseTimelineQuery(r))
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
	items, err := s.discovery.Search(r.Context(), principal.LibraryID, query, parseLimit(r, 50))
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

func (s *Server) handlePlaces(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}
	if s.discovery == nil {
		s.writeError(w, http.StatusServiceUnavailable, "discovery_unavailable", "discovery service is not configured", nil)
		return
	}

	items, err := s.discovery.ListPlaces(r.Context(), principal.LibraryID, parseLimit(r, 20))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "places_failed", err.Error(), nil)
		return
	}

	payload := make([]placeSummaryResponse, 0, len(items))
	for _, item := range items {
		payload = append(payload, placeSummaryResponse{
			Label:       item.Label,
			Count:       item.Count,
			LatestAt:    item.LatestAt.UTC().Format(time.RFC3339),
			LatestAsset: mapAssetSummary(item.LatestAsset),
		})
	}
	writeJSON(w, http.StatusOK, placesResponse{
		LibraryID: principal.LibraryID,
		Items:     payload,
	})
}

func (s *Server) handleDuplicates(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}
	if s.discovery == nil {
		s.writeError(w, http.StatusServiceUnavailable, "discovery_unavailable", "discovery service is not configured", nil)
		return
	}

	items, err := s.discovery.ListDuplicateCandidates(r.Context(), principal.LibraryID, parseLimit(r, 20))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "duplicates_failed", err.Error(), nil)
		return
	}

	payload := make([]duplicateCandidateResponse, 0, len(items))
	for _, item := range items {
		payload = append(payload, duplicateCandidateResponse{
			Primary:   mapAssetSummary(item.Primary),
			Candidate: mapAssetSummary(item.Candidate),
			Exact:     item.Exact,
		})
	}
	writeJSON(w, http.StatusOK, duplicatesResponse{
		LibraryID: principal.LibraryID,
		Items:     payload,
	})
}

func (s *Server) handleAlbums(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}
	if s.discovery == nil {
		s.writeError(w, http.StatusServiceUnavailable, "discovery_unavailable", "discovery service is not configured", nil)
		return
	}

	items, err := s.discovery.ListAlbums(r.Context(), principal.LibraryID)
	if err != nil {
		status := http.StatusInternalServerError
		if isNotConfigured(err) {
			status = http.StatusServiceUnavailable
		}
		s.writeError(w, status, "albums_failed", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, albumsResponse{
		LibraryID: principal.LibraryID,
		Items:     mapAlbums(items),
	})
}

func (s *Server) handleCreateAlbum(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}
	if s.discovery == nil {
		s.writeError(w, http.StatusServiceUnavailable, "discovery_unavailable", "discovery service is not configured", nil)
		return
	}

	var request createAlbumRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON", nil)
		return
	}
	album, err := s.discovery.CreateAlbum(r.Context(), principal.LibraryID, request.DisplayName)
	if err != nil {
		status := http.StatusBadRequest
		if isNotConfigured(err) {
			status = http.StatusServiceUnavailable
		}
		s.writeError(w, status, "album_create_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusCreated, mapAlbum(album))
}

func (s *Server) handleAlbumDetail(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}
	if s.discovery == nil {
		s.writeError(w, http.StatusServiceUnavailable, "discovery_unavailable", "discovery service is not configured", nil)
		return
	}

	album, items, err := s.discovery.ListAlbumAssets(r.Context(), principal.LibraryID, r.PathValue("albumId"), parseLimit(r, 50))
	if err != nil {
		status := http.StatusNotFound
		if isNotConfigured(err) {
			status = http.StatusServiceUnavailable
		}
		s.writeError(w, status, "album_lookup_failed", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, albumDetailResponse{
		LibraryID: principal.LibraryID,
		Album:     mapAlbum(album),
		Items:     mapAssetSummaries(items),
		PageInfo: CursorPageInfo{
			HasNextPage: false,
		},
	})
}

func (s *Server) handleAddAssetToAlbum(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}
	if s.discovery == nil {
		s.writeError(w, http.StatusServiceUnavailable, "discovery_unavailable", "discovery service is not configured", nil)
		return
	}

	var request addAlbumAssetRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON", nil)
		return
	}
	if err := s.discovery.AddAssetToAlbum(r.Context(), principal.LibraryID, r.PathValue("albumId"), request.AssetID); err != nil {
		status := http.StatusBadRequest
		if isNotConfigured(err) {
			status = http.StatusServiceUnavailable
		}
		s.writeError(w, status, "album_update_failed", err.Error(), nil)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"albumId": r.PathValue("albumId"),
		"assetId": request.AssetID,
	})
}

func (s *Server) handleSetFavorite(w http.ResponseWriter, r *http.Request) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		s.writeError(w, http.StatusUnauthorized, "unauthenticated", "authentication is required", nil)
		return
	}
	if s.discovery == nil {
		s.writeError(w, http.StatusServiceUnavailable, "discovery_unavailable", "discovery service is not configured", nil)
		return
	}

	var request setFavoriteRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON", nil)
		return
	}
	album, err := s.discovery.SetFavorite(r.Context(), principal.LibraryID, r.PathValue("assetId"), request.Favorite)
	if err != nil {
		status := http.StatusBadRequest
		if isNotConfigured(err) {
			status = http.StatusServiceUnavailable
		}
		s.writeError(w, status, "favorite_update_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"album":    mapAlbum(album),
		"assetId":  r.PathValue("assetId"),
		"favorite": request.Favorite,
	})
}

func mapAssetSummaries(items []discovery.Summary) []assetSummary {
	summaries := make([]assetSummary, 0, len(items))
	for _, item := range items {
		summaries = append(summaries, mapAssetSummary(item))
	}
	return summaries
}

func mapAssetSummary(item discovery.Summary) assetSummary {
	return assetSummary{
		AssetID:           item.Asset.ID,
		MediaType:         item.Asset.MediaType,
		TimelineTimestamp: timelineAt(item).Format(time.RFC3339),
		ProcessingStage:   apiProcessingStage(item.Asset.ProcessingStage),
		BackupStatus:      item.Asset.BackupStatus,
		ThumbnailToken:    item.ThumbnailToken,
		CaptionPreview:    item.CaptionPreview,
	}
}

func mapAlbums(items []discovery.Album) []albumSummaryResponse {
	albums := make([]albumSummaryResponse, 0, len(items))
	for _, item := range items {
		albums = append(albums, mapAlbum(item))
	}
	return albums
}

func mapAlbum(item discovery.Album) albumSummaryResponse {
	return albumSummaryResponse{
		AlbumID:     item.ID,
		Slug:        item.Slug,
		DisplayName: item.DisplayName,
		Kind:        strings.ReplaceAll(string(item.Kind), "_", "-"),
		AssetCount:  item.AssetCount,
	}
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

func parseTimelineQuery(r *http.Request) discovery.TimelineQuery {
	query := discovery.TimelineQuery{
		Limit:        parseLimit(r, 50),
		Location:     strings.TrimSpace(r.URL.Query().Get("location")),
		Stage:        strings.TrimSpace(strings.ReplaceAll(r.URL.Query().Get("stage"), "-", "_")),
		BackupStatus: strings.TrimSpace(r.URL.Query().Get("backupStatus")),
	}
	if value, ok := parseFlexibleTime(r.URL.Query().Get("dateFrom"), false); ok {
		query.DateFrom = &value
	}
	if value, ok := parseFlexibleTime(r.URL.Query().Get("dateTo"), true); ok {
		query.DateTo = &value
	}
	return query
}

func parseFlexibleTime(value string, endOfDay bool) (time.Time, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, false
	}
	if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return parsed.UTC(), true
	}
	if parsed, err := time.Parse("2006-01-02", trimmed); err == nil {
		if endOfDay {
			return parsed.Add(24*time.Hour - time.Nanosecond).UTC(), true
		}
		return parsed.UTC(), true
	}
	return time.Time{}, false
}

func parseLimit(r *http.Request, fallback int) int {
	value := strings.TrimSpace(r.URL.Query().Get("limit"))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	if parsed > 100 {
		return 100
	}
	return parsed
}

func isNotConfigured(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "not configured")
}
