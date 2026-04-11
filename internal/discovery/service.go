package discovery

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
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

type ServiceConfig struct {
	Repository  Repository
	Storage     storage.Provider
	DownloadTTL time.Duration
	TokenKey    string
	Now         func() time.Time
}

type Service struct {
	repository  Repository
	storage     storage.Provider
	downloadTTL time.Duration
	tokenKey    string
	now         func() time.Time
}

type Summary struct {
	Asset          asset.Asset
	ThumbnailToken string
	CaptionPreview string
}

type Detail struct {
	Asset          asset.Asset
	ThumbnailToken string
	CaptionPreview string
}

type DownloadGrant struct {
	AssetID   string
	Status    string
	URL       string
	ExpiresAt time.Time
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
		storage:     cfg.Storage,
		downloadTTL: cfg.DownloadTTL,
		tokenKey:    cfg.TokenKey,
		now:         cfg.Now,
	}, nil
}

func (s *Service) ListTimeline(ctx context.Context, libraryID string, limit int) ([]Summary, error) {
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

	if limit <= 0 {
		limit = defaultLimit
	}
	if len(records) > limit {
		records = records[:limit]
	}

	items := make([]Summary, 0, len(records))
	for _, record := range records {
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

func (s *Service) summaryForAsset(ctx context.Context, record asset.Asset, policy library.Policy) (Summary, error) {
	token, err := s.thumbnailToken(ctx, record.ID)
	if err != nil {
		return Summary{}, err
	}

	captionPreview := ""
	if policy.CaptionVisiblePreview() {
		captionPreview = ai.TextPreview(record.CaptionText, 80)
	}

	return Summary{
		Asset:          record,
		ThumbnailToken: token,
		CaptionPreview: captionPreview,
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
	textSignals := []string{record.OriginalFilename, strings.Join(record.Tags, " ")}
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

	if policy.ShouldRunEmbedding() && len(record.Embedding) > 0 {
		score += ai.CosineSimilarity(queryEmbedding, record.Embedding)
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
