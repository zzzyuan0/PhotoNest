package discovery

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/photonest/photonest/internal/asset"
	"github.com/photonest/photonest/internal/library"
	"github.com/photonest/photonest/internal/provider/ai"
	"github.com/photonest/photonest/internal/provider/storage"
)

const (
	defaultLimit       = 50
	defaultDownloadTTL = 5 * time.Minute
)

type Repository interface {
	GetAsset(ctx context.Context, assetID string) (asset.Asset, error)
	ListAssetsByLibrary(ctx context.Context, libraryID string) ([]asset.Asset, error)
	ListObjectReferencesByAsset(ctx context.Context, assetID string) ([]asset.ObjectReference, error)
	GetLibraryPolicy(ctx context.Context, libraryID string) (library.Policy, error)
}

type albumRepository interface {
	ListAlbums(ctx context.Context, libraryID string) ([]Album, error)
	GetAlbum(ctx context.Context, albumID string) (Album, error)
	CreateAlbum(ctx context.Context, album Album) (Album, error)
	EnsureSystemAlbum(ctx context.Context, libraryID string, kind AlbumKind) (Album, error)
	AddAssetToAlbum(ctx context.Context, albumID string, assetID string) error
	RemoveAssetFromAlbum(ctx context.Context, albumID string, assetID string) error
	ListAssetIDsByAlbum(ctx context.Context, albumID string) ([]string, error)
}

type ServiceConfig struct {
	Repository  Repository
	Storage     storage.Provider
	DownloadTTL time.Duration
	TokenKey    string
	Now         func() time.Time
}

type Service struct {
	repository  Repository
	albums      albumRepository
	storage     storage.Provider
	downloadTTL time.Duration
	tokenKey    string
	now         func() time.Time
}

type Summary struct {
	Asset          asset.Asset
	ThumbnailToken string
	CaptionPreview string
	OCRPreview     string
	SemanticTags   []string
	SearchReady    bool
}

type Detail struct {
	Asset          asset.Asset
	ThumbnailToken string
	CaptionPreview string
	OCRPreview     string
	SemanticTags   []string
	SearchReady    bool
}

type DownloadGrant struct {
	AssetID   string
	Status    string
	URL       string
	ExpiresAt time.Time
}

type PreviewStream struct {
	AssetID       string
	MediaType     string
	ContentLength int64
	LastModified  time.Time
	Body          io.ReadCloser
}

type SearchQuery struct {
	Text         string
	Stage        string
	BackupStatus string
	Location     string
}

func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Repository == nil {
		return nil, fmt.Errorf("repository is required")
	}
	if cfg.Storage == nil {
		return nil, fmt.Errorf("storage provider is required")
	}
	if cfg.DownloadTTL <= 0 {
		cfg.DownloadTTL = defaultDownloadTTL
	}
	if strings.TrimSpace(cfg.TokenKey) == "" {
		cfg.TokenKey = "photonest-thumbnail-token"
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}

	return &Service{
		repository:  cfg.Repository,
		albums:      resolveAlbumRepository(cfg.Repository),
		storage:     cfg.Storage,
		downloadTTL: cfg.DownloadTTL,
		tokenKey:    cfg.TokenKey,
		now:         cfg.Now,
	}, nil
}

func (s *Service) ListTimeline(ctx context.Context, libraryID string, limit int) ([]Summary, error) {
	return s.ListTimelineWithFilters(ctx, libraryID, TimelineQuery{Limit: limit})
}

func (s *Service) ListTimelineWithFilters(ctx context.Context, libraryID string, query TimelineQuery) ([]Summary, error) {
	records, err := s.repository.ListAssetsByLibrary(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	policy, err := s.repository.GetLibraryPolicy(ctx, libraryID)
	if err != nil {
		return nil, err
	}

	sort.SliceStable(records, func(i int, j int) bool {
		return bestTimeline(records[i]).After(bestTimeline(records[j]))
	})

	filtered := make([]asset.Asset, 0, len(records))
	for _, record := range records {
		if !matchesTimelineQuery(record, policy, query) {
			continue
		}
		filtered = append(filtered, record)
	}

	limit := query.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	items := make([]Summary, 0, len(filtered))
	for _, record := range filtered {
		summary, err := s.summaryForAsset(ctx, record, policy)
		if err != nil {
			return nil, err
		}
		items = append(items, summary)
	}

	return items, nil
}

func (s *Service) Search(ctx context.Context, libraryID string, rawQuery string, limit int) ([]Summary, error) {
	query := ParseQuery(rawQuery)
	if strings.TrimSpace(query.Text) == "" && query.Stage == "" && query.BackupStatus == "" && query.Location == "" {
		return s.ListTimeline(ctx, libraryID, limit)
	}

	records, err := s.repository.ListAssetsByLibrary(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	policy, err := s.repository.GetLibraryPolicy(ctx, libraryID)
	if err != nil {
		return nil, err
	}

	type scoredSummary struct {
		Summary Summary
		Score   float64
	}

	queryEmbedding := ai.HashEmbeddingText(query.Text, 24)
	items := make([]scoredSummary, 0, len(records))
	for _, record := range records {
		score, ok := matchAsset(record, policy, query, queryEmbedding)
		if !ok {
			continue
		}
		summary, err := s.summaryForAsset(ctx, record, policy)
		if err != nil {
			return nil, err
		}
		items = append(items, scoredSummary{
			Summary: summary,
			Score:   score,
		})
	}

	sort.SliceStable(items, func(i int, j int) bool {
		if items[i].Score == items[j].Score {
			return bestTimeline(items[i].Summary.Asset).After(bestTimeline(items[j].Summary.Asset))
		}
		return items[i].Score > items[j].Score
	})

	if limit <= 0 {
		limit = defaultLimit
	}
	if len(items) > limit {
		items = items[:limit]
	}

	summaries := make([]Summary, 0, len(items))
	for _, item := range items {
		summaries = append(summaries, item.Summary)
	}
	return summaries, nil
}

func (s *Service) ListPlaces(ctx context.Context, libraryID string, limit int) ([]PlaceSummary, error) {
	records, err := s.repository.ListAssetsByLibrary(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	policy, err := s.repository.GetLibraryPolicy(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	if !policy.ShouldRunGPS() {
		return nil, nil
	}

	grouped := map[string]*PlaceSummary{}
	for _, record := range records {
		label := strings.TrimSpace(record.LocationLabel)
		if label == "" {
			continue
		}
		summary, err := s.summaryForAsset(ctx, record, policy)
		if err != nil {
			return nil, err
		}
		bestAt := bestTimeline(record)
		item, ok := grouped[label]
		if !ok {
			grouped[label] = &PlaceSummary{
				Label:       label,
				Count:       1,
				LatestAsset: summary,
				LatestAt:    bestAt,
			}
			continue
		}
		item.Count++
		if bestAt.After(item.LatestAt) {
			item.LatestAsset = summary
			item.LatestAt = bestAt
		}
	}

	places := make([]PlaceSummary, 0, len(grouped))
	for _, item := range grouped {
		places = append(places, *item)
	}
	sort.SliceStable(places, func(i int, j int) bool {
		if places[i].Count == places[j].Count {
			return places[i].LatestAt.After(places[j].LatestAt)
		}
		return places[i].Count > places[j].Count
	})
	if limit <= 0 {
		limit = defaultLimit
	}
	if len(places) > limit {
		places = places[:limit]
	}
	return places, nil
}

func (s *Service) ListDuplicateCandidates(ctx context.Context, libraryID string, limit int) ([]DuplicateCandidate, error) {
	records, err := s.repository.ListAssetsByLibrary(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	policy, err := s.repository.GetLibraryPolicy(ctx, libraryID)
	if err != nil {
		return nil, err
	}

	byID := make(map[string]asset.Asset, len(records))
	for _, record := range records {
		byID[record.ID] = record
	}

	items := make([]DuplicateCandidate, 0)
	for _, record := range records {
		primaryID := strings.TrimSpace(record.DuplicateCandidateOf)
		if primaryID == "" {
			continue
		}
		primary, ok := byID[primaryID]
		if !ok {
			continue
		}
		primarySummary, err := s.summaryForAsset(ctx, primary, policy)
		if err != nil {
			return nil, err
		}
		candidateSummary, err := s.summaryForAsset(ctx, record, policy)
		if err != nil {
			return nil, err
		}
		items = append(items, DuplicateCandidate{
			Primary:   primarySummary,
			Candidate: candidateSummary,
			Exact:     record.IsDuplicateExact,
		})
	}

	sort.SliceStable(items, func(i int, j int) bool {
		return bestTimeline(items[i].Candidate.Asset).After(bestTimeline(items[j].Candidate.Asset))
	})
	if limit <= 0 {
		limit = defaultLimit
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *Service) ListAlbums(ctx context.Context, libraryID string) ([]Album, error) {
	if s.albums == nil {
		return nil, nil
	}
	albums, err := s.albums.ListAlbums(ctx, libraryID)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(albums, func(i int, j int) bool {
		if albums[i].Kind == albums[j].Kind {
			return albums[i].CreatedAt.Before(albums[j].CreatedAt)
		}
		return albumKindRank(albums[i].Kind) < albumKindRank(albums[j].Kind)
	})
	return albums, nil
}

func (s *Service) CreateAlbum(ctx context.Context, libraryID string, displayName string) (Album, error) {
	if s.albums == nil {
		return Album{}, fmt.Errorf("album repository is not configured")
	}
	trimmed := strings.TrimSpace(displayName)
	if trimmed == "" {
		return Album{}, fmt.Errorf("album display name is required")
	}
	return s.albums.CreateAlbum(ctx, Album{
		LibraryID:   libraryID,
		Slug:        slugify(trimmed),
		DisplayName: trimmed,
		Kind:        AlbumKindCurated,
		CreatedAt:   s.now().UTC(),
	})
}

func (s *Service) ListAlbumAssets(ctx context.Context, libraryID string, albumID string, limit int) (Album, []Summary, error) {
	if s.albums == nil {
		return Album{}, nil, fmt.Errorf("album repository is not configured")
	}
	album, err := s.albums.GetAlbum(ctx, albumID)
	if err != nil {
		return Album{}, nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(album.LibraryID), strings.TrimSpace(libraryID)) {
		return Album{}, nil, fmt.Errorf("album does not belong to library")
	}
	policy, err := s.repository.GetLibraryPolicy(ctx, libraryID)
	if err != nil {
		return Album{}, nil, err
	}
	assetIDs, err := s.albums.ListAssetIDsByAlbum(ctx, albumID)
	if err != nil {
		return Album{}, nil, err
	}
	items := make([]Summary, 0, len(assetIDs))
	for _, assetID := range assetIDs {
		record, err := s.repository.GetAsset(ctx, assetID)
		if err != nil {
			return Album{}, nil, err
		}
		summary, err := s.summaryForAsset(ctx, record, policy)
		if err != nil {
			return Album{}, nil, err
		}
		items = append(items, summary)
	}
	sort.SliceStable(items, func(i int, j int) bool {
		return bestTimeline(items[i].Asset).After(bestTimeline(items[j].Asset))
	})
	if limit <= 0 {
		limit = defaultLimit
	}
	if len(items) > limit {
		items = items[:limit]
	}
	album.AssetCount = len(assetIDs)
	return album, items, nil
}

func (s *Service) AddAssetToAlbum(ctx context.Context, libraryID string, albumID string, assetID string) error {
	if s.albums == nil {
		return fmt.Errorf("album repository is not configured")
	}
	record, err := s.repository.GetAsset(ctx, assetID)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(record.LibraryID), strings.TrimSpace(libraryID)) {
		return fmt.Errorf("asset does not belong to library")
	}
	return s.albums.AddAssetToAlbum(ctx, albumID, assetID)
}

func (s *Service) SetFavorite(ctx context.Context, libraryID string, assetID string, favorite bool) (Album, error) {
	if s.albums == nil {
		return Album{}, fmt.Errorf("album repository is not configured")
	}
	record, err := s.repository.GetAsset(ctx, assetID)
	if err != nil {
		return Album{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(record.LibraryID), strings.TrimSpace(libraryID)) {
		return Album{}, fmt.Errorf("asset does not belong to library")
	}
	album, err := s.albums.EnsureSystemAlbum(ctx, libraryID, AlbumKindFavorites)
	if err != nil {
		return Album{}, err
	}
	if favorite {
		return album, s.albums.AddAssetToAlbum(ctx, album.ID, assetID)
	}
	return album, s.albums.RemoveAssetFromAlbum(ctx, album.ID, assetID)
}

func (s *Service) GetAssetDetail(ctx context.Context, libraryID string, assetID string) (Detail, error) {
	record, err := s.repository.GetAsset(ctx, assetID)
	if err != nil {
		return Detail{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(record.LibraryID), strings.TrimSpace(libraryID)) {
		return Detail{}, fmt.Errorf("asset does not belong to library")
	}
	policy, err := s.repository.GetLibraryPolicy(ctx, record.LibraryID)
	if err != nil {
		return Detail{}, err
	}
	summary, err := s.summaryForAsset(ctx, record, policy)
	if err != nil {
		return Detail{}, err
	}
	return Detail{
		Asset:          summary.Asset,
		ThumbnailToken: summary.ThumbnailToken,
		CaptionPreview: summary.CaptionPreview,
		OCRPreview:     summary.OCRPreview,
		SemanticTags:   summary.SemanticTags,
		SearchReady:    summary.SearchReady,
	}, nil
}

func (s *Service) RequestDownload(ctx context.Context, libraryID string, assetID string) (DownloadGrant, error) {
	record, err := s.repository.GetAsset(ctx, assetID)
	if err != nil {
		return DownloadGrant{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(record.LibraryID), strings.TrimSpace(libraryID)) {
		return DownloadGrant{}, fmt.Errorf("asset does not belong to library")
	}
	ref, err := s.findOriginalReference(ctx, assetID)
	if err != nil {
		return DownloadGrant{}, err
	}
	presigned, err := s.storage.PresignDownload(ctx, storage.PresignDownloadInput{
		Ref:       storage.ObjectRef{Bucket: ref.Bucket, Key: ref.ObjectKey},
		ExpiresIn: s.downloadTTL,
	})
	if err != nil {
		return DownloadGrant{}, err
	}
	return DownloadGrant{
		AssetID:   assetID,
		Status:    "ready",
		URL:       presigned.URL,
		ExpiresAt: presigned.ExpiresAt.UTC(),
	}, nil
}

func (s *Service) OpenPreview(ctx context.Context, libraryID string, assetID string) (PreviewStream, error) {
	record, err := s.repository.GetAsset(ctx, assetID)
	if err != nil {
		return PreviewStream{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(record.LibraryID), strings.TrimSpace(libraryID)) {
		return PreviewStream{}, fmt.Errorf("asset does not belong to library")
	}

	ref, err := s.findOriginalReference(ctx, assetID)
	if err != nil {
		return PreviewStream{}, err
	}

	info, err := s.storage.HeadObject(ctx, storage.ObjectRef{
		Bucket: ref.Bucket,
		Key:    ref.ObjectKey,
	})
	if err != nil {
		return PreviewStream{}, err
	}

	body, err := s.storage.GetObject(ctx, storage.ObjectRef{
		Bucket: ref.Bucket,
		Key:    ref.ObjectKey,
	})
	if err != nil {
		return PreviewStream{}, err
	}

	mediaType := strings.TrimSpace(info.ContentType)
	if mediaType == "" {
		mediaType = strings.TrimSpace(record.MediaType)
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}

	return PreviewStream{
		AssetID:       assetID,
		MediaType:     mediaType,
		ContentLength: info.ContentLength,
		LastModified:  info.LastModified.UTC(),
		Body:          body,
	}, nil
}

func (s *Service) summaryForAsset(ctx context.Context, record asset.Asset, policy library.Policy) (Summary, error) {
	token, err := s.thumbnailToken(ctx, record.ID)
	if err != nil {
		return Summary{}, err
	}

	captionPreview := ""
	if policy.CaptionVisiblePreview() {
		captionPreview = ai.TextPreview(record.CaptionText, 80)
	}
	ocrPreview := ""
	if policy.OCRVisiblePreview() {
		ocrPreview = ai.TextPreview(record.OCRText, 80)
	}

	return Summary{
		Asset:          record,
		ThumbnailToken: token,
		CaptionPreview: captionPreview,
		OCRPreview:     ocrPreview,
		SemanticTags:   ai.SemanticTags(record.Tags),
		SearchReady:    record.IndexedAt != nil,
	}, nil
}

func (s *Service) thumbnailToken(ctx context.Context, assetID string) (string, error) {
	refs, err := s.repository.ListObjectReferencesByAsset(ctx, assetID)
	if err != nil {
		return "", err
	}
	hasThumbnail := false
	for _, ref := range refs {
		if ref.Purpose == asset.ObjectPurposeThumbnail {
			hasThumbnail = true
			break
		}
	}
	if !hasThumbnail {
		return "", nil
	}

	expiresAt := s.now().UTC().Add(s.downloadTTL).Unix()
	message := fmt.Sprintf("%s:%d", assetID, expiresAt)
	mac := hmac.New(sha256.New, []byte(s.tokenKey))
	_, _ = mac.Write([]byte(message))
	signature := hex.EncodeToString(mac.Sum(nil))

	return base64.RawURLEncoding.EncodeToString([]byte(message + ":" + signature)), nil
}

func (s *Service) findOriginalReference(ctx context.Context, assetID string) (asset.ObjectReference, error) {
	refs, err := s.repository.ListObjectReferencesByAsset(ctx, assetID)
	if err != nil {
		return asset.ObjectReference{}, err
	}
	for _, ref := range refs {
		if ref.Purpose == asset.ObjectPurposeOriginal {
			return ref, nil
		}
	}
	return asset.ObjectReference{}, fmt.Errorf("original object reference not found")
}

func ParseQuery(raw string) SearchQuery {
	query := SearchQuery{}
	parts := strings.Fields(strings.TrimSpace(raw))
	textParts := make([]string, 0, len(parts))
	for _, part := range parts {
		switch {
		case strings.HasPrefix(part, "stage:"):
			query.Stage = strings.TrimSpace(strings.TrimPrefix(part, "stage:"))
		case strings.HasPrefix(part, "backup:"):
			query.BackupStatus = strings.TrimSpace(strings.TrimPrefix(part, "backup:"))
		case strings.HasPrefix(part, "location:"):
			query.Location = strings.TrimSpace(strings.TrimPrefix(part, "location:"))
		default:
			textParts = append(textParts, part)
		}
	}
	query.Text = strings.Join(textParts, " ")
	return query
}

func matchAsset(record asset.Asset, policy library.Policy, query SearchQuery, queryEmbedding []float32) (float64, bool) {
	if query.Stage != "" && !strings.EqualFold(query.Stage, string(record.ProcessingStage)) {
		return 0, false
	}
	if query.BackupStatus != "" && !strings.EqualFold(query.BackupStatus, record.BackupStatus) {
		return 0, false
	}
	if query.Location != "" && (!policy.ShouldRunGPS() || !containsFold(record.LocationLabel, query.Location)) {
		return 0, false
	}

	if strings.TrimSpace(query.Text) == "" {
		return 1, true
	}

	score := 0.0
	textSignals := []string{
		record.OriginalFilename,
		strings.Join(record.Tags, " "),
		record.SearchDocument,
	}
	if policy.CaptionVisiblePreview() {
		textSignals = append(textSignals, record.CaptionText)
	}
	if policy.OCRVisiblePreview() {
		textSignals = append(textSignals, record.OCRText)
	}
	if policy.ShouldRunGPS() {
		textSignals = append(textSignals, record.LocationLabel)
	}
	for _, token := range ai.KeywordTokens(query.Text) {
		for _, signal := range textSignals {
			if containsFold(signal, token) {
				score += 1.2
				break
			}
		}
	}

	searchEmbedding := record.SearchEmbedding
	if len(searchEmbedding) == 0 {
		searchEmbedding = record.Embedding
	}
	if policy.ShouldRunEmbedding() && len(searchEmbedding) > 0 {
		score += ai.CosineSimilarity(queryEmbedding, searchEmbedding)
	}
	if score <= 0 {
		return 0, false
	}
	return score, true
}

func bestTimeline(record asset.Asset) time.Time {
	switch {
	case !record.TimelineAt.IsZero():
		return record.TimelineAt.UTC()
	case record.CapturedAt != nil:
		return record.CapturedAt.UTC()
	default:
		return record.ImportedAt.UTC()
	}
}

func containsFold(value string, target string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(value)), strings.ToLower(strings.TrimSpace(target)))
}

func resolveAlbumRepository(repository Repository) albumRepository {
	repo, _ := repository.(albumRepository)
	return repo
}

func matchesTimelineQuery(record asset.Asset, policy library.Policy, query TimelineQuery) bool {
	if query.Stage != "" && !strings.EqualFold(query.Stage, string(record.ProcessingStage)) {
		return false
	}
	if query.BackupStatus != "" && !strings.EqualFold(query.BackupStatus, record.BackupStatus) {
		return false
	}
	if query.Location != "" {
		if !policy.ShouldRunGPS() || !containsFold(record.LocationLabel, query.Location) {
			return false
		}
	}
	bestAt := bestTimeline(record)
	if query.DateFrom != nil && bestAt.Before(query.DateFrom.UTC()) {
		return false
	}
	if query.DateTo != nil && bestAt.After(query.DateTo.UTC()) {
		return false
	}
	return true
}

func slugify(value string) string {
	tokens := ai.KeywordTokens(value)
	if len(tokens) == 0 {
		return "album"
	}
	return strings.Join(tokens, "-")
}

func albumKindRank(kind AlbumKind) int {
	switch kind {
	case AlbumKindFavorites:
		return 0
	case AlbumKindCurated:
		return 1
	case AlbumKindDuplicatesReview:
		return 2
	default:
		return 3
	}
}
